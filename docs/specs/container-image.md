# Container Image (`ubuntu-intune/`)

The container image defines everything that runs inside the nspawn container. It's based on Ubuntu 24.04 LTS and built as a multi-stage OCI image.

## Build Process

The `Containerfile` uses a multi-stage build:

1. **scratch AS ctx** — Copies `build_files/` (build script and chunk helper) into a minimal stage, mounted into the builder via `--mount=type=bind`
2. **ubuntu:24.04 AS builder** — Static `system_files/` are copied directly into this image, then the build script runs with cache mounts for `/var/cache/apt`, `/var/log`, `/var/lib/apt`, `/var/lib/dpkg/updates` (persistent across builds) and `/tmp` (tmpfs). After the build script finishes, the `chunk` helper tags every file with its owning package name (`setfattr -n user.component`) so chunkah can group files by package into layers.
3. **chunkah** — Rechunks the builder output into max 64 layers for efficient OCI distribution
4. **final** — Outputs the rechunked OCI archive

## Packages Installed

The build script (`build_files/build`) adds Microsoft repos and installs:

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
| `sudo`, `cracklib-runtime` | User management, password validation |
| `upower` | Power state reporting (required by some Microsoft services) |
| `unattended-upgrades` | Automatic security updates (includes Edge stable repo) |

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

#### Edge Wrapper
- `/usr/local/bin/microsoft-edge` — Wrapper script that adds `--disable-gpu-sandbox` (nspawn cannot create nested user namespaces) and enables Wayland/WebRTC PipeWire features when `WAYLAND_DISPLAY` is set

#### Polkit
- `50-pcscd.rules` — Allows smart card daemon access

#### Apt
- `90-nologs.conf` — Logging disabled
- `99-keep.conf` — Package retention configured

### Build-script configuration (`build_files/build`)

These are configured by the build script during image creation (not static files):

#### PAM & Password Quality
- Password quality via `pam_pwquality`: `minlen=12`, `dcredit=-1`, `ucredit=-1`, `lcredit=-1`, `ocredit=-1` (each requires at least one digit, uppercase, lowercase, special character)
- `/etc/security/pwquality.conf` — enforcing mode with dictionary and username checks
- PAM profile for pwquality is restored after intune-portal install (its postinst overwrites the custom config)
- PAM modules enabled via `pam-auth-update`: `pwquality`, `mkhomedir`, `gnome-keyring`, `intune`, `unix`

#### Intune Portal Patching
- The `intune-portal` package is downloaded, extracted, and patched to remove the `systemctl restart polkit.service` line from its postinst script (breaks inside containers), then reinstalled from the patched deb

#### Unattended Upgrades
- Edge stable repo (`edge:stable`) is added to the allowed origins for automatic security updates

#### Services
- `microsoft-identity-device-broker.service` is enabled at build time

## Image Extraction

The `init` and `recreate` commands pull the OCI image and extract it to `rootfs/`. The `--tmp-dir` flag allows overriding the temp directory used during extraction (defaults to system tmp). This is useful on hosts where `/tmp` is a small tmpfs — the extracted image can be several GB before being moved to the final rootfs path.

## Image Distribution

Images are published to `ghcr.io/frostyard/ubuntu-intune` with tags:
- `:v1.2.3` — Stable release matching CLI version
- `:insiders` — Insiders channel (newer packages, less tested)
- `:latest` — Development builds

Images are signed with cosign. The build is automated via `.github/workflows/build-container.yml`.
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
   - Restarts only `microsoft-identity-device-broker.service` (system, through the polkit rule). The user-session broker is D-Bus-activated and not a unit, so it re-activates with the new environment
   - Caps the unlock at 3 attempts per boot (counter in `/tmp/.intuneme-keyring-unlock-attempts` — `/tmp` is tmpfs, so it resets at boot). A failed attempt logs `keyring unlock attempt N of 3 failed` to stderr and to the journal (`logger -t intuneme-session-setup`); at the limit the script stops retrying for that boot and logs the recovery command. Before the limit, every app launch and shell login would otherwise pay a `pkill` plus about 2 s of sleep. A verified unlock removes the counter
   - Holds an `flock` on `$XDG_RUNTIME_DIR/intuneme-keyring.lock` for the whole block, so concurrent session entries cannot both read the counter and both attempt the unlock. The block runs in a subshell, so the `exit` calls cannot close a login shell that sourced the script. The counter is written to a temp file and moved into place, so a concurrent reader never sees a partial file
8. Touches `$XDG_RUNTIME_DIR/intuneme-session-env` once `dbus-update-activation-environment` has succeeded and the session bus exists, then starts the `intune-agent` timer for compliance checks if not already active. The timer's condition (see above) is why the agent cannot run before this point

## Broker restart authorization

`system_files/etc/polkit-1/rules.d/49-intuneme-broker.rules` grants the container
sudo group only `org.freedesktop.systemd1.manage-units` with unit
`microsoft-identity-device-broker.service` and verb `restart`. All other requests
fall through to the normal policy. The same file is embedded by the
`containerimage` Go package for upgrades. `TestBrokerRestartAuthorization`
evaluates the shipped JavaScript against allowed and denied requests.
See [ADR-0001](../adr/0001-make-helper-privilege-drop-irreversible.md).
