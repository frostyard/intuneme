# Container Images

Each image defines everything that runs inside its nspawn container. The
default Microsoft image is based on Ubuntu 24.04 LTS; both Himmelblau images
are based on Ubuntu 26.04. All are built as multi-stage OCI images.

## Build Process

The `ubuntu-intune/Containerfile` uses a multi-stage build:

1. **scratch AS ctx** — Copies `build_files/` (build script and chunk helper) into a minimal stage, mounted into the builder via `--mount=type=bind`
2. **ubuntu:24.04 AS builder** — Static `system_files/` are copied directly into this image, then the build script runs with cache mounts for `/var/cache/apt`, `/var/log`, `/var/lib/apt`, `/var/lib/dpkg/updates` (persistent across builds) and `/tmp` (tmpfs). After the build script finishes, the `chunk` helper tags every file with its owning package name (`setfattr -n user.component`) so chunkah can group files by package into layers.
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
- `intune-agent.timer` (user) — Timer override for compliance check schedule

#### Microsoft Edge Wrapper (Microsoft image only)
- `/usr/local/bin/microsoft-edge` — Wrapper script that adds `--disable-gpu-sandbox` (nspawn cannot create nested user namespaces) and enables Wayland/WebRTC PipeWire features when `WAYLAND_DISPLAY` is set

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

The terminal-passkey variant is local-build-only. It uses the same image recipe
and signed nightly package set, but replaces `himmelblau` and `pam-himmelblau`
with Debian packages built from the commit in
`internal/version/himmelblau_terminal_passkey_source.txt` (`dae5bf49889a4f5d4d6735b9006da14f50dc7a9d`).
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
directory. The directory bind lasts until container shutdown, but the socket
is removed when PAM authentication ends; the container's own system bus and
the rest of the host system bus remain unchanged. The pinned PAM fork starts
USB security-key authentication while probing Bluetooth, adds QR/Bluetooth
authentication only when Bluetooth is powered, and preserves the USB fallback
when Bluetooth is unavailable. Both transport attempts share one deadline and
cancel their pending work when the PAM attempt ends.

All images are signed with cosign. The build is automated via
`.github/workflows/build-container.yml`.
Registry pushes retry transient upload failures so a temporary GHCR error does not fail the scheduled build.

## Profile Script

The container image works in concert with `internal/provision/intuneme-profile.sh` (embedded in the Go binary, written to rootfs during init). This script runs on every login shell session and:

1. Reads host DISPLAY from `/etc/intuneme-host-display`, extends PATH with Intune and Azure VPN bins
2. Sets accessibility env vars (`NO_AT_BRIDGE=1`, `GTK_A11Y=none`) — also present in `/etc/environment` for non-login contexts
3. Sets `XAUTHORITY=/run/host-xauthority` for X11 auth
4. Imports display/audio vars into systemd user session so services (broker) see them
5. Detects Wayland, PipeWire, PulseAudio from `/run/host-*` sockets
6. Sets Nvidia PRIME offload vars when `/run/host-nvidia` exists
7. On first login per boot (marker at `/tmp/.intuneme-keyring-init-done`):
   - Ensures default keyring points to `login`
   - Initializes `gnome-keyring-daemon` with `--replace --unlock --components=secrets,pkcs11`
   - Stores a test secret via `secret-tool` to force default collection creation
   - Restarts both identity brokers to pick up the initialized keyring
8. Starts the `intune-agent` timer for compliance checks if not already active

## Experimental Himmelblau Image (`ubuntu-himmelblau/`)

`ghcr.io/frostyard/ubuntu-himmelblau` is an opt-in alternative, not an image
variant layered on the Microsoft rootfs. It is built from a pinned Ubuntu
26.04 base and the same chunkah pipeline, but has a separate build script and
PAM configuration.

It installs `himmelblau`, `himmelblau-broker`, `himmelblau-sso`,
`himmelblau-sso-policies`, `nss-himmelblau`, `pam-himmelblau`,
`himmelblau-apparmor`, and `pamtester` from the signed Community Nightly flat
APT repository for Ubuntu 26.04 and Ubuntu repositories.
It includes the GUI/device packages needed by intuneme, but no Microsoft Edge.
It does **not** install `microsoft-identity-broker` or
`intune-portal`; Himmelblau's broker replaces the former and its own compliance
client replaces the latter's enrollment path.
The shared Himmelblau build installs `binutils` and verifies that
`/usr/bin/strings` is available for tenant custom-compliance scripts that
inspect the Secure Boot database.
`pamtester` is required for the explicit fail-closed PAM step in
`intuneme enroll`; the image build and CLI both verify that the executable is
present. Enrollment can boot a stopped container with its new user map.

At runtime, `intuneme start` binds the host's `/run/udev/data` read-only into
Himmelblau containers when present. This supplements the existing read-only
block-device and EFI mounts; it does not disable Himmelblau's task-service
device isolation.

When intuneme creates the effective Himmelblau config, it overrides the
package's `apply_policy` default to `true`. When installing saved config, it
defaults the option to true if absent or commented. This gates Himmelblau's
Intune enrollment and policy application; an active `apply_policy = false` is
preserved as an explicit opt-out. The setting is applied by the CLI, not baked
into either image.
Intuneme likewise sets `enable_passwordless_qr_bluetooth = true` in fresh
configs and defaults saved configs to true when the option is absent or
commented; an active `false` remains an opt-out. The CLI's runtime check treats
an omitted option as enabled. The terminal-passkey image is still selected
separately, and only that image supports terminal QR authentication.

The image's `/etc/pam.d/machine-shell` routes non-allowlisted authentication
through `pam_himmelblau`; intuneme invokes this service with `pamtester` after
removing the mapped local user from the allowlist. The image contract includes
an executable `/usr/bin/pamtester`, checked during image build, init, recreate,
and enrollment. `machinectl shell` itself is
polkit-authorized and can open a shell without running PAM authentication, so
its exit status is not an enrollment signal. Common account and session stacks
still run for home creation and user services.
`/etc/himmelblau/himmelblau.conf` disables `cn_name_mapping` and points
`user_map_file` at `/etc/himmelblau/user-map`. `intuneme enroll --user <UPN>`
writes `<host-user>:<UPN>`. If stopped, it runs the normal start flow with that
mapping already present; if running with a new mapping, it reboots so the
daemon loads it at startup. Enrollment then waits for the daemon, tasks service,
and socket units before invoking the fail-closed `machine-shell` PAM auth
stack through `pamtester`. A successful PAM result is required before
intuneme opens a shell or saves the UPN. Rebooting a running container closes
its apps and shells; in-place daemon restart/reload leaves socket activation
broken in systemd-nspawn. `init` creates an empty user map before enrollment;
`start` also restores it if the installation has no saved UPN yet. Enrollment
verifies that only root remains on the PAM bypass allowlist and verifies the
local account UID after PAM authentication.
intuneme keeps using the local username for machinectl shell and app launch so its existing supplementary groups and
`/home/<host-user>` bind are retained. There is no RFC2307 `uidNumber`
requirement or separate home bind.

During Himmelblau `init` and `recreate`, intuneme renames the group entry for
the local account's primary GID to `<host-user>` (or creates it if missing),
without changing that GID. This makes the primary group name match the local
`user-map` username while preserving the numeric account identity. If a group
with that name already exists at a different GID, provisioning fails rather
than silently changing an unrelated group. The Microsoft image is unaffected.

The image contains no tenant-specific `domain` value. Himmelblau's package
provides a commented default at `/usr/lib/himmelblau/himmelblau.conf`; the
image must not replace it with a reduced, uncommented static file.
`intuneme init --himmelblau` obtains the Entra sign-in domain interactively or
via `--himmelblau-domain`, stores it in `config.toml`, and writes the effective
`/etc/himmelblau/himmelblau.conf` into the extracted rootfs before first boot
while preserving the package's comments and unrelated defaults.
`intuneme edit --himmelblau` opens that effective config in the user's editor
whenever a configuration change is needed, including after initial setup. The
customized file is stored outside the rootfs at `<root>/himmelblau.conf`, is
reapplied by `start` and `recreate`, and is not applied to a running daemon.
Intuneme validates only the mapping settings it owns using Himmelblau's
INI-style semantics.
Local Docker/Podman image builds can be tested with
`--himmelblau-local-image localhost/<name>:<tag>`; that override is persisted
and reused by `recreate`.

The image tracks Community Nightly packages; it is experimental and has no
upstream systemd-nspawn support contract. Package changes are incorporated by
the scheduled image build. The broker interface has the same D-Bus name used
by intuneme's proxy, but headless activation and compliance behavior inside
nspawn are not verified upstream.

## Terminal-passkey variant (`ubuntu-himmelblau-terminal-passkey/`)

This opt-in image uses the standard Himmelblau package set, then replaces
`himmelblau` and `pam-himmelblau` with Debian packages compiled from the
commit in `internal/version/himmelblau_terminal_passkey_source.txt`. The
packages are built by
[`build-pam-package`](../../ubuntu-himmelblau-terminal-passkey/build_files/build-pam-package)
using the upstream Ubuntu 26.04 packaging container. The pinned source commit
already contains the fallback; the package build does not apply local patches.
The shared
`retry_edge_prt_exchange` helper retries `AcquireTokenFailed` (except the
existing consent-required case) and `AADSTSError` results only when the
requested client is `EDGE_BROWSER_CLIENT_ID`. The retry uses the same PRT and
scopes with `DEFAULT_APP_ID`; it does not issue another PRT or reset its cache
age. Both registered and decoupled Hello-key validation use the helper.
This closes the decoupled-key path's missing fallback that caused a fresh
FIDO QR after PIN creation on `AADSTS530003`. Success still passes normal
token/account validation; default-client errors propagate to the existing
denial/reauthentication handling. Consent scope fallback, non-Edge clients,
and network handling retain their previous behavior. The pinned fork includes
the `retry_edge_prt_exchange_*` Rust regression tests.

The OCI image records the upstream fork source
commit in
`/usr/share/intuneme/himmelblau-terminal-passkey-source`. Rebuild the local
image and use `recreate` to install updates while preserving enrollment state.

The local `build-local-image` helper builds the `builder` target. It supports
Podman, Buildah, or Docker Buildx (`--load`); this local target is not
re-chunked by chunkah. The full Containerfile retains OCI-archive export
stages, but the terminal-passkey variant is not part of the hosted image
workflow.

The image remains tenant-agnostic and does not install or start BlueZ. At
enrollment time, the CLI requires host `xdg-dbus-proxy` and an active host
`org.bluez` service. It creates a filtered proxy exposing only `org.bluez`,
bind-mounts the proxy directory read-only to
`/run/intuneme/bluez-proxy`, and points `DBUS_SYSTEM_BUS_ADDRESS` to that
socket only for `pamtester`. The container's own system bus remains separate.
