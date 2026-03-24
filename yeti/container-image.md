# Container Image (`ubuntu-intune/`)

The container image defines everything that runs inside the nspawn container. It's based on Ubuntu 24.04 LTS and built as a multi-stage OCI image.

## Build Process

The `Containerfile` uses a multi-stage build:

1. **scratch AS ctx** — Copies `build_files/` and `system_files/` into a minimal stage for mount access
2. **ubuntu:24.04 AS builder** — Base image with system files copied in, build script runs with cache mounts
3. **chunkah** — Rechunks image to max 64 layers for efficient OCI distribution
4. **final** — Outputs the rechunked OCI archive

Cache mounts are used for `/var/cache/apt` (persistent across builds) and `/tmp` (tmpfs).

## Packages Installed

The build script (`build_files/build`) adds Microsoft repos and installs:

| Package | Purpose |
|---------|---------|
| `microsoft-identity-broker` | Microsoft identity broker (device enrollment, token management) |
| `microsoft-edge-stable` | Microsoft Edge browser |
| `intune-portal` | Intune Portal app (patched — removes problematic polkit restart in postinst) |
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

Static config files in `system_files/` are copied into the image:

### Environment
- `/etc/environment` — Disables accessibility bridge (`NO_AT_BRIDGE=1`, `GTK_A11Y=none`)

### PAM
- `/etc/pam.d/machine-shell` — PAM stack for `machinectl shell` sessions (passwordless, polkit-controlled)
- Password quality via `pam_pwquality`: `minlen=12`, `dcredit=-1`, `ucredit=-1`, `lcredit=-1`, `ocredit=-1` (each requires at least one digit, uppercase, lowercase, special character)
- PAM modules enabled: `pwquality`, `mkhomedir`, `gnome-keyring`, `intune`, `unix`
- `/etc/security/pwquality.conf` — enforcing mode with dictionary and username checks

### Systemd Overrides
- `microsoft-identity-device-broker.service` — System-level broker override
- `microsoft-identity-broker.service` (user) — Sets `DISPLAY` from `/etc/intuneme-host-display` marker
- `intune-agent.timer` (user) — Timer override for compliance check schedule

### Edge Wrapper
- `/usr/local/bin/microsoft-edge` — Wrapper script that adds `--disable-gpu-sandbox` (nspawn cannot create nested user namespaces) and enables Wayland/WebRTC PipeWire features when `WAYLAND_DISPLAY` is set

### Polkit
- `50-pcscd.rules` — Allows smart card daemon access

### Apt
- Logging disabled, package retention configured

## Image Distribution

Images are published to `ghcr.io/frostyard/ubuntu-intune` with tags:
- `:v1.2.3` — Stable release matching CLI version
- `:insiders` — Insiders channel (newer packages, less tested)
- `:latest` — Development builds

Images are signed with cosign. The build is automated via `.github/workflows/build-container.yml`.

## Profile Script

The container image works in concert with `internal/provision/intuneme-profile.sh` (embedded in the Go binary, written to rootfs during init). This script runs on every login shell session and:

1. Reads host DISPLAY from `/etc/intuneme-host-display`
2. Sets `XAUTHORITY=/run/host-xauthority` for X11 auth
3. Imports Wayland, PipeWire, PulseAudio sockets into systemd user session
4. On first login per boot (marker in `/tmp`):
   - Initializes `gnome-keyring-daemon` with `--replace --unlock`
   - Stores a test secret to force default keyring collection creation
   - Restarts identity brokers to pick up the initialized keyring
5. Starts the `intune-agent` timer for compliance checks
