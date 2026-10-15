# intuneme
[![OpenSSF Scorecard](https://api.scorecard.dev/projects/github.com/frostyard/intuneme/badge)](https://scorecard.dev/viewer/?uri=github.com/frostyard/intuneme)

`intuneme` provisions and manages a [systemd-nspawn](https://www.freedesktop.org/software/systemd/man/systemd-nspawn.html) container running Microsoft Intune on an immutable Linux host. The container handles enrollment, compliance, and corporate resource access while making minimal changes to the host. An opt-in experimental mode uses Himmelblau's own Entra authentication and compliance stack instead of Microsoft's broker and Intune Portal.

## TL;DR: local terminal-passkey Himmelblau

Build the CLI and terminal-passkey image locally; intuneme does not pull a
hosted terminal-passkey image. Its source marker selects the variant
automatically, so no extra mode switch is needed.

```bash
make build
./ubuntu-himmelblau-terminal-passkey/build_files/build-local-image \
  localhost/ubuntu-himmelblau-terminal-passkey:dev
./intuneme init --himmelblau \
  --himmelblau-local-image localhost/ubuntu-himmelblau-terminal-passkey:dev
# Optional: adjust the generated configuration.
./intuneme edit --himmelblau
./intuneme enroll --user <UPN>
./intuneme shell
# Inside the container, authenticate the local user before checking compliance.
su - <local-user>
aad-tool compliance-check
```

Replace `<UPN>` and `<local-user>` with the Entra sign-in and the existing local account.

## Features

- Container lifecycle management (`init`, `start`, `stop`, `destroy`, `recreate`)
- Experimental Himmelblau image and PAM enrollment (`init --himmelblau`, `enroll --user`)
- Local-build terminal QR/Bluetooth passkey variant (auto-detected from its local image marker)
- Broker proxy for host-side single sign-on (Edge, VS Code, and other MSAL apps)
- Run MCP servers inside the container, wired to host stdio (`mcp`) — for tools that must authenticate against the container's tenant
- Device hotplug — YubiKey and webcam passthrough via udev rules
- GNOME Quick Settings extension for start/stop and app launch without a terminal
- Desktop shortcuts for Microsoft Edge and Intune Portal

## Documentation

Full documentation is at **https://frostyard.github.io/intuneme/**

## Experimental Himmelblau alternative

The default `intuneme init` uses the Microsoft Intune image. Both Himmelblau
image variants are based on Ubuntu 26.04; the default Microsoft image remains
on Ubuntu 24.04. To opt into the separate experimental Himmelblau image:

```bash
intuneme init --himmelblau
intuneme edit --himmelblau # Optional: configure additional Himmelblau settings
intuneme enroll --user user@example.com
```

`enroll` starts the container if it is stopped; if a new mapping is added to a
running container, it reboots to load the mapping before PAM authentication.

`init --himmelblau` prompts for the Entra sign-in domain; pass
`--himmelblau-domain` for automation or `--himmelblau-local-image` when testing
a locally built `localhost/...` image. Himmelblau enrollment occurs during
PAM authentication. intuneme maps the existing local container account to the
supplied Entra UPN through Himmelblau's `user-map` file, preserving its Unix
identity and `~/Intune` home. `init` creates the empty `user-map` file;
`enroll` adds the account mapping; if the container is stopped, it starts it
with that map, and if running it reboots to load the change. It then uses
`pamtester` to invoke the fail-closed `machine-shell` PAM authentication
directly. A successful `machinectl shell` connection alone is not proof of
authentication. After PAM succeeds, `enroll` opens the usual container shell;
when you exit, it verifies the local account still has the host UID and saves
the UPN. Rebooting a running container closes its apps and shells. This mode
uses Community Nightly packages, omits Microsoft's Intune Portal, and is not
upstream-validated under systemd-nspawn.
In Himmelblau mode, `init` and `recreate` also name the local account's primary
group after the username (for example, `alice:alice`) while preserving its GID; this
avoids a common Himmelblau UID/GID resolver collision. Existing containers
need `recreate` to apply the group-name change. The Microsoft image is
unaffected.
intuneme defaults Himmelblau's `apply_policy = true`, enabling Intune
enrollment and policy application on successful authentication. Set
`apply_policy = false` in `[global]` to opt out.
Intuneme also defaults `enable_passwordless_qr_bluetooth = true`; set it to
`false` in `[global]` to opt out. Terminal QR/Bluetooth authentication still
requires the separate terminal-passkey image and its host BlueZ prerequisites.
`recreate` preserves Himmelblau's protected `DynamicUser` state and encrypted
HSM PIN credential; permission-restricted PIN inspection is retried through
`sudo`. It verifies the staged `credential.secret` copy and rejects an
incomplete PIN/key backup before removing the old rootfs. After restore, it
verifies the destination key matches the staged copy before deleting that
backup.
Himmelblau starts also pass the host udev database read-only for filesystem
type detection; both Himmelblau images include `strings` for Secure Boot
database compliance scripts.
See the [Himmelblau setup and limitations](https://frostyard.github.io/intuneme/user-guide/himmelblau/).

Run `intuneme edit --himmelblau` during setup or later to open Himmelblau's
packaged, commented configuration in your editor. The required domain and
local-account mapping settings are prefilled and validated. The QR/Bluetooth
option defaults to true; set it to false to opt out. The edited file is saved
as `~/.local/share/intuneme/himmelblau.conf`, reapplied by `start` and
`recreate`, and can also be edited directly on the host. Stop and start the
container to apply changes. intuneme uses `$VISUAL` or `$EDITOR` when set;
otherwise, in a graphical session it opens the host's default text editor and
waits for you to press Enter after saving and closing the editor.

Terminal QR/Bluetooth passkeys require a locally built image; intuneme does
not pull or publish a hosted terminal-passkey image. Build it first, then pass
the local tag to init:

```bash
./ubuntu-himmelblau-terminal-passkey/build_files/build-local-image \
  localhost/ubuntu-himmelblau-terminal-passkey:dev
intuneme init --himmelblau \
  --himmelblau-local-image localhost/ubuntu-himmelblau-terminal-passkey:dev
```

Intuneme detects the image marker and persists its source commit for
`recreate`. The regular Himmelblau image remains on signed upstream nightly
packages. The terminal-passkey image pins a Himmelblau fork commit that
includes the cached-PRT client fallback, avoiding a redundant post-PIN QR
when the default-client retry succeeds. Rebuild the local image and run
`recreate` to install daemon updates.
The optional `--himmelblau-terminal-passkey` switch
asserts that terminal-passkey mode is intended and rejects a local image
without the marker; it is not needed for a correctly built local image. The
QR/Bluetooth option defaults to true; set it to false to opt out. When enabled,
enrollment requires host `xdg-dbus-proxy`, an
active BlueZ service, and a Bluetooth adapter. Intuneme filters forwarded
D-Bus access to `org.bluez`; it does not expose the host system bus. The
passkey may be offered after an Entra password prompt, depending on the
authentication flow. See the [terminal-passkey image
notes](ubuntu-himmelblau-terminal-passkey/README.md).
The local image helper supports Docker Buildx, Podman, or Buildah.

During `init`, intuneme asks for a password for interactive container access.
It accepts any non-empty password. A strong password is recommended because
it protects interactive login to the container.

## Quick install

```bash
go install github.com/frostyard/intuneme@latest
```

Alternatively, Arch Linux users can install `intuneme` or `intuneme-bin` from the Arch User Repository (AUR):

- [intuneme](https://aur.archlinux.org/packages/intuneme) (build from source)
- [intuneme-bin](https://aur.archlinux.org/packages/intuneme-bin) (pre-compiled binary)

## Contributing

See the [contributing guide](https://frostyard.github.io/intuneme/contributing/) on the docs site.

## License

MIT
