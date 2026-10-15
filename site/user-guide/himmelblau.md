# Experimental Himmelblau mode

intuneme can use a separate Ubuntu 26.04 image with Himmelblau's own Entra
authentication, broker, and compliance stack. This replaces the Microsoft
identity broker and Intune Portal packages; it is not a way to add Himmelblau
alongside the Microsoft image.

!!! warning "Experimental and not upstream-validated"
    Himmelblau does not document support for systemd-nspawn. PAM enrollment,
    headless D-Bus broker activation, compliance behavior, and enrollment
    preservation in this container are experimental. Himmelblau Community
    Nightly packages are used because the stable repository requires a paid
    entitlement.

## Map the local account

Himmelblau's `user_map_file` option maps a local Unix account name to an Entra
UPN. `init` creates an empty `/etc/himmelblau/user-map`; `enroll` writes one
line in that file in the upstream
`local_username:upn@domain` format, using the host-matched local account that
already owns `~/Intune`. UPN lookups resolve through the local account's
UID/GID, while intuneme continues to log in and launch apps as the local
username, preserving its local groups and `~/Intune` home. No RFC2307
`uidNumber` synchronization is required.

For Himmelblau containers, `init` and `recreate` also make the primary group
name match the local username (for example, `alice:alice`) while preserving its GID.
This prevents the common case where a differently named primary group with
the same numeric ID as the user's UID is treated as a separate local identity.
Existing containers need `intuneme recreate` to receive this provisioning
change; the Microsoft image's group names are not changed.

The image disables `cn_name_mapping`: that option only rewrites short names
that are not already local `/etc/passwd` accounts, so it does not attach
Himmelblau to intuneme's existing local user. The daemon caches its
name-reservation list at startup, so `enroll` boots a stopped container with
the mapping already installed, but reboots a running container after changing
the map. An in-place service restart or reload leaves the daemon's
socket-activation units inactive under systemd-nspawn.

!!! warning "Same local identity"
    The mapped Entra UPN is an authentication alias for the host-matched
    container user, not an isolated second Unix account. It inherits that
    account's groups, sudo rule, and `~/Intune` home. The container is not
    user-namespaced, so container root is host root; only map the intended
    trusted Entra account.

## Initialize and enroll

`init --himmelblau` prompts for the Entra sign-in domain (for example,
`example.com` or `tenant.onmicrosoft.com`). Use the primary domain users will
sign in with. For scripted setup, pass `--himmelblau-domain`. After init,
customize Himmelblau's configuration with:

```bash
intuneme edit --himmelblau
```

This command can be run during initial setup or later when you need to change
settings. The editor opens the package's commented configuration with the
domain and intuneme-required account-mapping settings already set. Add or
uncomment options in `[global]`, for example:

```ini
enable_passwordless_qr_bluetooth = true
```

Himmelblau uses an INI-style config format, not TOML; keep its documented
unquoted values (the setting above is in `[global]`). Intuneme checks the
required `domain`, `cn_name_mapping = false`, and
`user_map_file = /etc/himmelblau/user-map` settings without rejecting other
bare string values in the package defaults. It uses `$VISUAL` or `$EDITOR` if
set. Otherwise,
in a graphical session, it opens the host's default application for text files
with `xdg-open`; after saving and closing that app, press Enter in the intuneme
terminal to continue. Without a graphical session, intuneme tries the host's
`sensible-editor`, `editor`, `micro`, `nano`, `nvim`, `vim`, then `vi`.
Before opening the editor, intuneme rewrites its three required settings on
the temporary copy; this also repairs older configs that had an unquoted
`user_map_file` path. Other options are passed through unchanged and parsed by
Himmelblau when the daemon starts.

Intuneme sets `apply_policy = true` over the package default. Himmelblau uses it
to enroll the device with Intune and apply Intune policies during
authentication. For saved configs, intuneme inserts `true` when the setting is
missing or commented, including at boot or during `recreate`; an explicit
`apply_policy = false` remains an opt-out. Intune enrollment and compliance
still depend on tenant permissions, device state, and organization policy.

Intuneme also defaults `enable_passwordless_qr_bluetooth = true` when creating
new effective configs and when installing saved configs that omit or comment
out the setting. An explicit `enable_passwordless_qr_bluetooth = false`
remains an opt-out. The option only enables the terminal QR flow with the
terminal-passkey image; the standard image's upstream PAM module remains
GDM-gated.

!!! info "Terminal QR/Bluetooth passkey image"
    The standard `--himmelblau` image uses the upstream nightly PAM module,
    which only renders QR in GDM flows. The terminal-passkey variant is
    local-build-only; intuneme does not pull it from a hosted registry. Build
    it first and pass the local tag to init:

    ```bash
    ./ubuntu-himmelblau-terminal-passkey/build_files/build-local-image \
      localhost/ubuntu-himmelblau-terminal-passkey:dev
    intuneme init --himmelblau \
      --himmelblau-local-image localhost/ubuntu-himmelblau-terminal-passkey:dev
    ```

    The option defaults to true; set
    `enable_passwordless_qr_bluetooth = false` in `[global]` to opt out. Run
    `intuneme enroll` from an interactive terminal. The variant is pinned to an
    unmerged Himmelblau fork commit; it does not change existing standard
    Himmelblau installations.

    When the option is enabled, enrollment requires host `xdg-dbus-proxy` and
    an active host BlueZ service with a Bluetooth adapter. It exposes only the
    `org.bluez` D-Bus name to the PAM process through a filtered proxy; it
    never bind-mounts the host's full system bus. During FIDO authentication,
    the fork starts USB security-key authentication while probing Bluetooth;
    powered Bluetooth adds a possible QR method, while an unavailable or
    powered-off adapter preserves the USB fallback. The QR challenge can
    appear after an Entra password prompt; cancelling before the FIDO challenge
    means the QR step is never reached. The setting does not force a passkey or
    skip other tenant-required authentication steps.

The edited config is saved outside the replaceable rootfs at
`~/.local/share/intuneme/himmelblau.conf`. `start` reapplies it to the
container, and `recreate` carries it to the new image. You may edit this host
file directly; changes take effect after stopping the container and running
`intuneme start` again. `destroy` removes the saved config.

During `init`, intuneme also asks you to set a password for interactive
container login. It accepts any non-empty password. A strong password is
recommended because it protects interactive access to the container.

```bash
intuneme init --himmelblau
intuneme edit --himmelblau  # Optional: configure additional Himmelblau settings
intuneme enroll --user user@example.com
```

For terminal QR/Bluetooth authentication, build the terminal-passkey image
locally and pass its tag with `--himmelblau-local-image`. Intuneme detects the
image's source marker and records the commit for `recreate`; there is no hosted
terminal-passkey image. The optional `--himmelblau-terminal-passkey` switch
asserts that terminal-passkey mode is intended and rejects a local image
without the marker; it is not needed for a correctly built local image.
See the [terminal-passkey image instructions](https://github.com/frostyard/intuneme/tree/main/ubuntu-himmelblau-terminal-passkey).
This variant also includes a daemon fix so the local UID mapped by `user-map`
can be resolved by PAM and Intune compliance when its numeric value collides
with a local group GID. The standard image still uses the upstream daemon.
When `enable_passwordless_qr_bluetooth = true`, this mode requires host
`xdg-dbus-proxy` and BlueZ. Intuneme passes a filtered BlueZ-only D-Bus proxy
to the PAM authentication process; the full host system bus is not exposed to
the container.

`enroll` works whether the container is stopped or already running. When
stopped, it writes the user map first and starts the container so Himmelblau
loads that mapping on its initial boot. When already running with a new mapping,
it reboots to reload the map; this closes running apps and shells. If
authentication is cancelled after an automatic start, the container remains
running; stop it explicitly if desired.

It waits for the daemon, tasks service, and socket units to become active,
removes the local username from the PAM allowlist, and verifies that only root
remains. It then uses `pamtester` to invoke the fail-closed `machine-shell` PAM
service for the mapped local username. This explicit authentication step is
necessary because `machinectl shell` can open a shell without authenticating
the user. Himmelblau performs first-login enrollment during PAM
authentication; there is no separate join command. Omit `--user` to be
prompted for the UPN in an interactive terminal.

Complete the authentication prompts in that terminal. Depending on the
account's registration and tenant policy, Himmelblau may ask for the Entra
password, show a terminal QR for a cross-device passkey, or present a
device-login URL and code. The terminal-passkey variant does not skip the
password prompt if the selected authentication flow requests one. Complete any
required password-change or MFA steps. Only after PAM succeeds does intuneme
open a shell inside the container. Type `exit` (or press Ctrl+D) to return to
intuneme; it then verifies that the local account has the host-matched UID and
saves the enrollment.

Do not run `su - <local-user>` to start enrollment. `su` uses a different PAM
service that can fall back to local Unix-password authentication; only the
explicit `machine-shell` PAM authentication started by `intuneme enroll` is
part of this flow.

If you only see a connection message or a `Connection refused` error for
`/var/run/himmelblaud/socket`, PAM authentication did not complete. A
successful shell connection alone is not sufficient. `intuneme status` reports
the last UPN whose explicit PAM authentication succeeded; it does not query
Himmelblau for a live token or cloud session.

On success, intuneme stores the UPN in `config.toml`; subsequent starts restore
the map and PAM policy. `intuneme shell` continues to use the local username,
which Himmelblau maps to the enrolled Entra identity. The Himmelblau images
contain neither Microsoft Edge nor Intune Portal; use `intuneme enroll` for
Himmelblau sign-in and enrollment.

## Test a local image build with Docker

The normal image build workflow publishes to GHCR. For local iteration, build
the Ubuntu rootfs stage and point intuneme at its `localhost/` tag:

```bash
docker build --target builder \
  -f ubuntu-himmelblau/Containerfile \
  -t localhost/ubuntu-himmelblau:dev .
go run . init --himmelblau \
  --himmelblau-domain YOUR_ENTRA_SIGN_IN_DOMAIN \
  --himmelblau-local-image localhost/ubuntu-himmelblau:dev
go run . enroll --user USER@YOUR_ENTRA_SIGN_IN_DOMAIN
```

`--himmelblau-local-image` is persisted, so `intuneme recreate` uses the
locally built image too. The `localhost/` prefix is required; it tells the
puller to use a local Docker/Podman image and never attempt a registry pull.
The image must include `pamtester`, which intuneme uses to run the fail-closed
PAM authentication check. After rebuilding an image for an already initialized
installation, run `intuneme recreate` and `intuneme start` before enrolling;
`recreate` replaces the rootfs from the persisted local tag. Use a real
verified Entra sign-in domain and UPN for enrollment.

The Microsoft Intune Portal is not installed in this image. To reauthenticate,
run `intuneme enroll --user USER@YOUR_ENTRA_SIGN_IN_DOMAIN` again. `intuneme recreate`
preserves the selected image family and backs up Himmelblau state; it does not
switch to or from the Microsoft image.

## Known limitations

- No upstream contract verifies Himmelblau operation under systemd-nspawn.
- Himmelblau's compliance client is an independent implementation and may not
  behave identically to Microsoft's Intune agent. Check with your IT
  administrator before relying on its compliance result.
- The image does not pass through the host TPM. Himmelblau's default policy
  permits a software-TPM fallback; this is not equivalent to hardware-backed
  key protection.
- Device compliance, broker activation without a graphical session, and
  interactions with organization-specific PAM policy require real-tenant
  testing.
