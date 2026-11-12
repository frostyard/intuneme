# Container Images

Each image defines everything that runs inside its nspawn container. The
default Microsoft image is based on Ubuntu 24.04 LTS; both Himmelblau images
are based on Ubuntu 26.04. All are built as multi-stage OCI images.

## Build Process

The `ubuntu-intune/Containerfile` uses a multi-stage build:

1. **scratch AS ctx** — Copies `build_files/` (build script and chunk helper) into a minimal stage, mounted into the builder via `--mount=type=bind`
2. **ubuntu:24.04 AS builder** — Static `system_files/` are copied directly into this image, then the build script runs with cache mounts for `/var/cache/apt`, `/var/log`, `/var/lib/apt`, `/var/lib/dpkg/updates` (persistent across builds) and `/tmp` (tmpfs). The cache mount is `/var/cache/apt`, never all of `/var/cache`: a cache mount hides what the build writes under it, and `cracklib-runtime` writes its dictionaries to `/var/cache/cracklib`. Without them `chpasswd` reports `cracklib_dict.pwd: No such file or directory` during `init`, and Intune reports the password policy as not compliant. After the build script finishes, the `chunk` helper tags every file with its owning package name (`setfattr -n user.component`) so chunkah can group files by package into layers.
3. **chunkah** — Rechunks the builder output into max 64 layers for efficient OCI distribution
4. **final** — Outputs the rechunked OCI archive

## Microsoft Image Packages

The Microsoft image build script (`ubuntu-intune/build_files/build`) adds
Microsoft repositories and installs:

| Package | Purpose |
|---------|---------|
| `microsoft-identity-broker` | Microsoft identity broker (device enrollment, token management) |
| `microsoft-edge-stable` | Microsoft Edge browser |
| `intune-portal` | Intune Portal app (downloaded, patched to remove polkit restart from postinst, installed via `dpkg -i` — not a standard `apt install`) |
| `pipewire-audio-client-libraries`, `libpulse0` | Audio support (PipeWire forwarded from host) |
| `mesa-vulkan-drivers`, `mesa-va-drivers`, `intel-media-va-driver` | GPU/Vulkan/VA-API support |
| `xdg-desktop-portal-gtk` | Desktop integration portal |
| `pcscd`, `yubikey-manager`, `opensc` | Smart card / YubiKey support |
| `libnss3-tools`, `openssl` | Certificate and TLS tooling |
| `libsecret-tools` | Secret Service CLI (`secret-tool`) for keyring initialization |
| `sudo` | User management |
| `upower` | Power state reporting (required by some Microsoft services) |
| `unattended-upgrades` | Automatic security updates (includes Edge stable repo) |

## Himmelblau Image Packages

The standard Himmelblau image and its terminal-passkey variant use separate
Ubuntu 26.04 builds. They install Himmelblau's authentication, broker, PAM,
NSS, SSO, and compliance packages from the signed Community Nightly APT
repository, plus the shared GUI/device dependencies described in
[`ubuntu-himmelblau/README.md`](../../ubuntu-himmelblau/README.md).
The terminal-passkey variant replaces the daemon and PAM packages with
locally built packages from its pinned fork. Neither Himmelblau image adds
Microsoft's Edge APT repository or installs Microsoft Edge, Microsoft
Identity Broker, or Intune Portal.

## System Configuration

### Static files (`system_files/`)

These are copied directly into the image at the start of the builder stage:

#### Environment
- `/etc/environment` — Disables accessibility bridge (`NO_AT_BRIDGE=1`, `GTK_A11Y=none`)

#### PAM
- `/etc/pam.d/machine-shell` — PAM stack for `machinectl shell` sessions (passwordless, polkit-controlled)

#### Systemd Overrides
- `microsoft-identity-device-broker.service` — System-level broker override
- `microsoft-identity-broker.service` (user) — Sets `DISPLAY` from `/etc/intuneme-host-display` marker
- `intune-agent.timer` (user) — Timer override for compliance check schedule; `ConditionPathExists=%t/intuneme-session-env` holds the timer back until `intuneme-session-setup` has pushed the environment into the D-Bus activation environment (the script touches that file, then starts the timer)

#### Microsoft Edge Wrapper (Microsoft image only)
- `/usr/local/bin/microsoft-edge` — Wrapper script that adds `--disable-gpu-sandbox` (nspawn cannot create nested user namespaces) and enables Wayland/WebRTC PipeWire features when `WAYLAND_DISPLAY` is set
- `/etc/opt/edge/policies/managed/intuneme-command-line-flags.json` — Sets `CommandLineFlagSecurityWarningsEnabled` to false; embedded in the Go binary and reinstalled by `intuneme start`

#### Polkit
- `50-pcscd.rules` — Allows smart card daemon access

#### Apt
- `90-nologs.conf` — Logging disabled
- `99-keep.conf` — Package retention configured

### Build-script configuration (`build_files/build`)

These are configured by the build script during image creation (not static files):

#### PAM & Local Password
- Microsoft image enables PAM modules via `pam-auth-update`: `mkhomedir`, `gnome-keyring`, `intune`, `unix`
- Himmelblau image enables `mkhomedir`, `gnome-keyring`, `himmelblau`, `unix`
- No password-quality policy is configured. `init` sets a non-empty local password for interactive container login; a strong password is recommended to protect that access.
- The Himmelblau image keeps Himmelblau's PAM authentication, account, and session modules, but omits its password-change module because `init` sets the local password before Himmelblau services start.

#### Intune Portal Patching
- The `intune-portal` package is downloaded, extracted, and patched to remove the `systemctl restart polkit.service` line from its postinst script (breaks inside containers), then reinstalled from the patched deb

#### Unattended Upgrades (Microsoft image only)
- Edge stable repo (`edge:stable`) is added to the allowed origins for automatic security updates

#### Services
- `microsoft-identity-device-broker.service` is enabled at build time

## Himmelblau Build Configuration

`ubuntu-himmelblau/build_files/build` installs only Himmelblau and Ubuntu
packages. The local terminal-passkey build passes its daemon/PAM debs to the
same build script; neither build configures Microsoft's Edge repository or
Edge's unattended-upgrade origin.

## Image Extraction

The `init` and `recreate` commands pull the selected OCI image and extract it to `rootfs/`. The default is `ubuntu-intune`; `intuneme init --himmelblau` selects the separate experimental `ubuntu-himmelblau` image. The `--tmp-dir` flag allows overriding the temp directory used during extraction (defaults to system tmp). This is useful on hosts where `/tmp` is a small tmpfs — the extracted image can be several GB before being moved to the final rootfs path.

## Microsoft Image Distribution

The Microsoft image is published to `ghcr.io/frostyard/ubuntu-intune` with tags:
- `:v1.2.3` — Stable release matching CLI version
- `:insiders` — Insiders channel (newer packages, less tested)
- `:latest` — Development builds

The standard Himmelblau image is published to
`ghcr.io/frostyard/ubuntu-himmelblau` with the same CLI-versioned tags and
`:latest`. It installs Himmelblau Community Nightly packages from a signed flat
APT repository and is experimental.

### Terminal-passkey variant (ubuntu-himmelblau-terminal-passkey)

The terminal-passkey variant is local-build-only. It uses the same image recipe
and signed nightly package set, but replaces `himmelblau` and `pam-himmelblau`
with Debian packages built from the commit in
[`internal/version/himmelblau_terminal_passkey_source.txt`](../../internal/version/himmelblau_terminal_passkey_source.txt).
That fork commit is rebased onto official upstream and includes the cached-PRT
client fallback and durable decoupled-Hello PRT renewal during online PIN
unlock. Renewed credentials are PIN-protected on disk; neither the PIN nor the
unlocked key is retained for background renewal. Intuneme applies no local
source patches. Local image builds can
choose another HTTPS repository and Git ref through
`build-local-image --source-repo ... --source-ref ...`. The package build
resolves that ref to a commit; then the image build installs it and writes
`/usr/share/intuneme/himmelblau-terminal-passkey-source`; init,
start, enroll, and recreate validate that marker. Init auto-detects a marked
local image passed with `--himmelblau-local-image` and saves its commit in
`config.toml`. There is no hosted terminal-passkey image or registry fallback;
users must build the image locally and supply it through `--himmelblau-local-image`.
This variant is intentionally separate so existing Himmelblau installations
retain the signed upstream PAM package.

When `enable_passwordless_qr_bluetooth = true`, the terminal-passkey PAM
process reaches host Bluetooth through a short-lived `xdg-dbus-proxy` instance.
Only the `org.bluez` D-Bus name is exposed through the bind-mounted socket
directory, prepared and bound read-only during terminal-passkey container boot
by `start` ([ADR-0005](../adr/0005-bind-bluez-at-boot.md)).
The directory bind lasts until container shutdown, but the socket
is removed when PAM authentication ends; the container's own system bus and
the rest of the host system bus remain unchanged. The pinned PAM fork starts
USB security-key authentication while probing Bluetooth, adds QR/Bluetooth
authentication only when Bluetooth is powered, and preserves the USB fallback
when Bluetooth is unavailable. Both transport attempts share one deadline and
cancel their pending work when the PAM attempt ends.

Published Microsoft and standard Himmelblau images are signed with cosign. The build is automated via
`.github/workflows/build-container.yml`.
Registry pushes retry transient upload failures so a temporary GHCR error does not fail the scheduled build.

## Profile Script

The container image works in concert with `internal/provision/intuneme-session-setup.sh` (embedded in the Go binary as `/usr/local/bin/intuneme-session-setup`, written to the rootfs by `provision.InstallSessionScripts`, and reinstalled by every `intuneme start`). `/etc/profile.d/intuneme.sh` sources it on a login shell, and `nspawn.Exec()` runs it on the nsenter launch path, so both session entries behave the same. It runs these steps on every session entry:

1. Reads host DISPLAY from `/etc/intuneme-host-display`, extends PATH with Intune and Azure VPN bins
2. Sets accessibility env vars (`NO_AT_BRIDGE=1`, `GTK_A11Y=none`) — also present in `/etc/environment` for non-login contexts
3. Sets `XAUTHORITY=/run/host-xauthority` for X11 auth
4. Imports display/audio vars into systemd user session so services (broker) see them
5. Detects Wayland, PipeWire, PulseAudio from `/run/host-*` sockets
6. Sets Nvidia PRIME offload vars when `/run/host-nvidia` exists
7. Initializes the keyring once per boot (marker at `/tmp/.intuneme-keyring-init-done`; `/tmp` is tmpfs, so it resets at boot). It waits until the user session bus socket `$XDG_RUNTIME_DIR/bus` exists, because without it every step fails and a marker written then would suppress the unlock for the rest of the boot. It then:
   - Ensures default keyring points to `login`
   - Kills all keyring daemons (`pkill -x gnome-keyring-d`) and starts exactly 1 with `echo "" | gnome-keyring-daemon --unlock --components=secrets,pkcs11 -d`. There is no `--replace`: a `--replace --unlock` daemon does not take the well-known `org.freedesktop.secrets` name, so Edge's broker never sees its unlock. `echo ""` sends a real empty password; `printf ""` would send EOF and do nothing
   - Stores a test secret via `secret-tool` to force default collection creation, so `ReadAlias("default")` resolves and the broker can store credentials
   - Re-reads the `Locked` property and writes the marker only when it reads `b false`
   - In the Microsoft image, restarts only `microsoft-identity-device-broker.service` (system, through the polkit rule). The user-session broker is D-Bus-activated and not a unit, so it re-activates with the new environment. Himmelblau skips this Microsoft-specific restart.
   - Caps the unlock at 3 attempts per boot (counter in `/tmp/.intuneme-keyring-unlock-attempts` — `/tmp` is tmpfs, so it resets at boot). A failed attempt logs `keyring unlock attempt N of 3 failed` to stderr and to the journal (`logger -t intuneme-session-setup`); at the limit the script stops retrying for that boot and logs the recovery command. Before the limit, every app launch and shell login would otherwise pay a `pkill` plus about 2 s of sleep. A verified unlock removes the counter
   - Holds an `flock` on `$XDG_RUNTIME_DIR/intuneme-keyring.lock` for the whole block, so concurrent session entries cannot both read the counter and both attempt the unlock. The block runs in a subshell, so the `exit` calls cannot close a login shell that sourced the script. The counter is written to a temp file and moved into place, so a concurrent reader never sees a partial file
8. Touches `$XDG_RUNTIME_DIR/intuneme-session-env` once `dbus-update-activation-environment` has succeeded and the session bus exists, then starts the matching compliance timer (`intune-agent.timer` or `himmelblau-compliance-check.timer`) if not already active. The Microsoft timer's condition (see above) prevents it running before session setup.

## Broker restart authorization

`system_files/etc/polkit-1/rules.d/49-intuneme-broker.rules` grants the container
sudo group only `org.freedesktop.systemd1.manage-units` with unit
`microsoft-identity-device-broker.service` and verb `restart`. All other requests
fall through to the normal policy. The same file is embedded by the
`containerimage` Go package for upgrades. `TestBrokerRestartAuthorization`
evaluates the shipped JavaScript against allowed and denied requests.
See [ADR-0001](../adr/0001-make-helper-privilege-drop-irreversible.md).
