# intuneme — Developer Overview

## Purpose

intuneme is a Go CLI tool that provisions and manages a `systemd-nspawn` container running Microsoft Intune on immutable Linux hosts. It isolates Intune Portal, Microsoft Edge, and the Microsoft identity broker inside a container while providing transparent access to host display, audio, GPU, and USB devices via bind mounts and namespace forwarding.

The tool handles the full container lifecycle — init, start, stop, destroy, recreate — with minimal host modifications (a polkit rule, a sudoers rule, and udev rules while running).

## Architecture

```
cmd/                  CLI command definitions (cobra commands)
├── extension/        Embedded GNOME Shell extension files
internal/
├── broker/           D-Bus broker proxy (container→host SSO forwarding)
├── config/           Config struct + TOML load/save (~/.local/share/intuneme/config.toml)
├── nspawn/           systemd-nspawn wrapper (boot, stop, exec, shell, bind mounts)
├── prereq/           Prerequisite checks (systemd-nspawn, machinectl)
├── provision/        Container provisioning (user, fixups, password, polkit, SELinux, backup/restore)
├── puller/           OCI image pull + extraction (podman → skopeo+umoci → docker)
├── nvidia/           Nvidia GPU detection, library bind mounts, and container-side symlink setup
├── runner/           Command execution abstraction (mockable interface)
├── sudo/             Helper for writing files via temp file + sudo install (used by provision, nspawn, udev)
├── sudoers/          Sudoers rule install/remove for passwordless nsenter
├── udev/             Udev rules + hotplug script for YubiKey and video devices
└── version/          Build version + OCI image ref resolution
ubuntu-intune/        Container image definition
├── Containerfile     Multi-stage build (Ubuntu 24.04 base)
├── build_files/      Build script (package install, PAM config, patches)
└── system_files/     Static config files copied into image
polkit/               Polkit rule reference for machinectl access (actual rule generated inline)
scripts/              Build helpers (completions, manpages, SELinux, desktop files)
```

### Component Responsibilities

| Component | Role |
|-----------|------|
| **Go CLI** (`cmd/`, `internal/`) | Container lifecycle, host-specific setup (user creation, hostname, polkit, sudoers) |
| **Container image** (`ubuntu-intune/`) | Static content: packages, systemd overrides, PAM config, Edge wrapper |

**Rule of thumb:** If something is static and doesn't depend on the host, it belongs in `ubuntu-intune/`. If it depends on the host user/UID/hostname, it stays in `internal/provision/`.

## Key Patterns

### Command Execution Inside the Container

`nspawn.Exec()` uses `sudo nsenter` to enter the container's namespaces and run commands as the user via `su`. This is the only reliable approach for launching GUI apps non-interactively:

```
sudo nsenter -t <leader_pid> -m -u -i -n -p -- \
  /bin/su -s /bin/bash <user> -c "export DISPLAY=... && nohup <app> >/dev/null 2>&1 &"
```

A sudoers rule at `/etc/sudoers.d/intuneme-exec` makes this passwordless so the GNOME extension can launch apps without a terminal. See [CLAUDE.md](../CLAUDE.md) for why alternatives (`machinectl shell`, `systemd-run`) don't work.

### Bind Mount Strategy

| Type | Host Path | Container Path | Lifecycle |
|------|-----------|----------------|-----------|
| Home directory | `~/Intune` | `/home/<user>` | Persistent (survives recreate) |
| X11 sockets | `/tmp/.X11-unix` | `/tmp/.X11-unix` | Always |
| Wayland | `$XDG_RUNTIME_DIR/wayland-0` | `/run/host-wayland` | Auto-detected on start |
| PipeWire | `$XDG_RUNTIME_DIR/pipewire-0` | `/run/host-pipewire` | Auto-detected on start |
| PulseAudio | `$XDG_RUNTIME_DIR/pulse/native` | `/run/host-pulse` | Auto-detected on start |
| X11 auth | `$XAUTHORITY` (see search order below) | `/run/host-xauthority` | Auto-detected on start |
| GPU (DRI) | `/dev/dri/card*`, `/dev/dri/renderD*` | Same | Individual devices for cgroup |
| GPU (Nvidia) | `/dev/nvidia*` | Same | When Nvidia detected; explicit `DeviceAllow` |
| Nvidia libs | Host lib dirs (from `ldconfig`) | `/run/host-nvidia/<index>/` | Read-only; when Nvidia detected |
| Nvidia ICD | `/usr/share/vulkan/icd.d/nvidia_icd.json` etc. | Same | Read-only; when Nvidia detected |
| Broker runtime | `~/.local/share/intuneme/runtime` | `/run/user/<uid>` | When broker proxy enabled |

### Device Hotplug Forwarding

YubiKeys and video capture devices (webcams) can be forwarded into the running container, both at start (already-plugged devices) and at runtime via udev hotplug rules.

**Device types:**

| Type | Detection | Udev Rule | Permissions |
|------|-----------|-----------|-------------|
| YubiKey (USB + HIDraw) | Scan sysfs for vendor `1050` | `70-intuneme-yubikey.rules` | `0666` (world-accessible) |
| Video (`/dev/video*`, `/dev/media*`) | Glob `/dev/video*`, `/dev/media*` | `70-intuneme-video.rules` | `0660 root:video` |

**Forwarding mechanism** (`udev.ForwardDevice()`):
1. Get container leader PID via `nspawn.LeaderPID()`
2. Get device major:minor via `stat`
3. Add `DeviceAllow` to the container's cgroup scope dynamically (`systemctl set-property machine-<name>.scope DevicePolicy=auto DeviceAllow=<dev> rwm`) — returns error on failure
4. Create the device node inside the container via `nsenter` + `mknod`
5. Set permissions (restrictive `0660 root:video` for video devices, `0666` for others)
6. Record in state directory (`/run/intuneme/devices/`) via `sudo.WriteFile()` for cleanup

**Udev hotplug flow:** The helper script at `/usr/local/lib/intuneme/usb-hotplug` is triggered by udev rules when devices are added/removed. It calls `ForwardDevice()` for adds and cleans up state for removes. All forwarding operations go through `nspawn.LeaderPID()` to locate the container's init process for namespace entry.

### Nvidia GPU Support

On hosts with Nvidia GPUs, the container needs the device nodes and host userspace libraries (which must match the kernel module version exactly). This is handled by `internal/nvidia/` using a detect-at-start, bind-mount, symlink approach (similar to distrobox):

1. **Detection** — `nvidia.IsPresent()` checks for `/dev/nvidiactl`
2. **Device bind mounts** — `DetectDevices()` globs `/dev/nvidia*` and `/dev/nvidia-caps/*`. Unlike DRI devices, nspawn does not auto-allow Nvidia devices in cgroups, so each gets an explicit `--property=DeviceAllow=<dev> rwm` boot arg
3. **Library discovery** — `HostLibraries()` parses `ldconfig -p` output for Nvidia libraries (x86-64 only), deduplicating by basename
4. **Library bind mounts** — `LibDirMounts()` maps unique host library directories to `/run/host-nvidia/0/`, `/run/host-nvidia/1/`, etc. (read-only). The indexed paths avoid basename collisions
5. **ICD files** — `HostICDFiles()` and `ICDMounts()` bind-mount Vulkan/EGL vendor JSON files at their standard paths
6. **Post-boot setup** — After boot, `CleanStaleLinks()` removes any symlinks from previous Nvidia sessions (always, even on non-Nvidia boots). Then `Setup()` creates symlinks in `/usr/lib/x86_64-linux-gnu/` pointing into `/run/host-nvidia/<index>/`, skipping package-owned regular files. Finishes with `ldconfig`
7. **Environment** — Both the profile script and `Exec()` set `__NV_PRIME_RENDER_OFFLOAD=1` and `__GLX_VENDOR_LIBRARY_NAME=nvidia` when `/run/host-nvidia` exists

### X11 Authority File Search Order

`findXAuthority()` in `nspawn.go` locates the host Xauthority file:
1. `$XAUTHORITY` environment variable (if file exists)
2. Glob `$XDG_RUNTIME_DIR/.mutter-Xwaylandauth.*` (Mutter/GNOME Wayland)
3. Glob `$XDG_RUNTIME_DIR/xauth_*` (other Xwayland implementations)
4. `~/.Xauthority` (classic X11)

### Render Group GID Conflict Resolution

During provisioning, `EnsureRenderGroup()` matches the container's `render` group GID to the host's so DRI render devices work across the bind mount. If the target GID is already occupied by a different group in the container, that group is reassigned to a free system GID (999–100) before the render group is created or modified.

### Container Hostname

During provisioning (`WriteFixups`), the container hostname is set to `<host-hostname>LXC` — e.g., if the host is `myworkstation`, the container gets `myworkstationLXC`. This prevents hostname collisions when both host and container are visible on the same network.

### Container User Groups

The container user is added to: `adm,sudo,video,audio` (plus `render` if a render group exists matching the host's render GID). The container-side sudoers rule at `/etc/sudoers.d/intuneme` grants `<user> ALL=(ALL) NOPASSWD: ALL` for passwordless operations inside the container.

### SELinux Support

On SELinux-enforcing systems (Fedora, Bazzite), `InstallSELinuxPolicy()` during `init`:

1. Labels the rootfs tree as `container_file_t` via `semanage fcontext` + `restorecon -RF`
2. Installs a custom policy module (`intuneme-machined`) that allows `systemd_machined_t` to:
   - Open/read/write/ioctl PTY devices (`user_devpts_t`) — required for `machinectl shell`
   - Read symlinks in `/tmp` (`user_tmp_t`) — required for `/tmp/ptmx` traversal

### Password Setting

Container password is set via a bind-mounted temp file to avoid shell injection: the CLI writes `user:password` to a host temp file, bind-mounts it read-only as `/run/chpasswd-input`, and runs `chpasswd < /run/chpasswd-input` inside the container via `systemd-nspawn`.

Password validation (both CLI-side and container PAM): minimum 12 chars, at least one digit, uppercase, lowercase, and special character, no username substring.

### Profile Script Environment

The container profile script (`/etc/profile.d/intuneme.sh`, embedded in Go binary) runs on every login shell session and:

1. Reads `DISPLAY` from `/etc/intuneme-host-display` (written by `start`), defaults to `:0`
2. Extends `PATH` with `/opt/microsoft/intune/bin` and `/opt/microsoft/microsoft-azurevpnclient`
3. Sets `XAUTHORITY=/run/host-xauthority` if bind-mounted
4. Imports display/audio vars into systemd user session so services see them
5. Detects Wayland (`WAYLAND_DISPLAY`), PipeWire (`PIPEWIRE_REMOTE`), PulseAudio (`PULSE_SERVER`) from `/run/host-*` sockets
6. Sets `__NV_PRIME_RENDER_OFFLOAD=1` and `__GLX_VENDOR_LIBRARY_NAME=nvidia` when `/run/host-nvidia` exists, and imports them into the systemd user session
7. On first login per boot (marker in `/tmp`):
   - Initializes `gnome-keyring-daemon` with `--replace --unlock`
   - Stores a test secret to force default keyring collection creation
   - Restarts identity brokers to pick up the initialized keyring
8. Starts `intune-agent.timer` for compliance checks

### State Preservation Across Recreate

`recreate` updates the container image while preserving enrollment:
1. Backs up password hash (from `rootfs/etc/shadow`) and device broker state (from `rootfs/var/lib/microsoft-identity-device-broker`)
2. Deletes old rootfs, pulls new image, re-provisions
3. Restores password hash and broker state into the new rootfs

Enrollment data (Intune database, app state) persists via the `~/Intune` bind mount. The keyring is re-initialized fresh on every boot (marker file in `/tmp`).

### Runner Abstraction

All shell commands go through the `runner.Runner` interface (`internal/runner/`), which provides `Run()`, `RunAttached()`, `RunBackground()`, and `LookPath()`. This makes command execution mockable for testing.

### OCI Image Resolution

`version.ImageRef()` resolves the container image tag from the build version:
- Insiders channel → `ghcr.io/frostyard/ubuntu-intune:insiders`
- Clean semver (e.g., v1.2.3) → `ghcr.io/frostyard/ubuntu-intune:v1.2.3`
- Dev builds → `ghcr.io/frostyard/ubuntu-intune:latest`

### Image Pull Strategy

The puller detects available tools in order: podman → skopeo+umoci → docker. Each implements the `Puller` interface with `PullAndExtract()` to download the OCI image and extract the rootfs.

### Edge Wrapper

The container ships `/usr/local/bin/microsoft-edge` which wraps the real binary:
- Always adds `--disable-gpu-sandbox` (nspawn cannot create nested user namespaces; renderer sandbox stays active)
- When `WAYLAND_DISPLAY` is set: unsets `DISPLAY`, adds `--enable-features=UseOzonePlatform,WebRTCPipeWireCapturer --ozone-platform=wayland`

### GNOME Shell Extension

The embedded extension (`cmd/extension/`, UUID `intuneme@frostyard.org`, GNOME 47-50) installs to `~/.local/share/gnome-shell/extensions/` and provides:
- Quick Settings toggle (start/stop container)
- Status display (container running/stopped, broker proxy state)
- Menu shortcuts for shell, Edge, Intune Portal
- Monitors container state via `org.freedesktop.machine1` D-Bus signals with 5-second polling fallback
- Finds a terminal emulator (checks `$TERMINAL`, then ghostty/ptyxis/kgx/gnome-terminal/xterm)

## Configuration

Single TOML file at `~/.local/share/intuneme/config.toml`:

| Field | Type | Description |
|-------|------|-------------|
| `machine_name` | string | nspawn machine name (default: "intuneme") |
| `rootfs_path` | string | Path to extracted rootfs |
| `host_uid` | int | Host user's UID (matched in container) |
| `host_user` | string | Host username |
| `broker_proxy` | bool | Enable D-Bus broker proxy for host-side SSO |
| `insiders` | bool | Use insiders channel image |

The `--root` persistent flag overrides the default data directory (`~/.local/share/intuneme`).

Note: The polkit rule (`50-intuneme.rules`) is generated inline by `provision.InstallPolkitRule()` and allows both `sudo` and `wheel` groups to manage nspawn machines. The `polkit/` directory in the repo contains a reference copy that only checks `sudo` — the installed version is authoritative.

## Storage Layout

```
~/.local/share/intuneme/
├── config.toml          Configuration
├── rootfs/              Extracted container filesystem
├── runtime/             Broker proxy runtime dir (bind-mounted as /run/user/<uid>)
└── broker-proxy.pid     PID file for broker proxy process

/run/intuneme/devices/   Udev forwarded device state (tmpfs, only while running)

~/Intune/                Bind-mounted as container home directory
├── .config/intune/      Enrollment database
├── .local/share/intune/ App state
├── .local/state/microsoft-identity-broker/  Broker device state
├── .local/share/keyrings/  gnome-keyring data
├── Downloads/           User files (preserved on destroy)
└── ...
```

## Host Modifications

intuneme installs these on the host (all reversible via `destroy`):

| Artifact | Path | Installed by | Removed by |
|----------|------|--------------|------------|
| Polkit rule | `/etc/polkit-1/rules.d/50-intuneme.rules` | `init` | `destroy` |
| Sudoers rule | `/etc/sudoers.d/intuneme-exec` | `init` (reinstalled by `start`) | `destroy` |
| Udev rules (YubiKey) | `/etc/udev/rules.d/70-intuneme-yubikey.rules` | `start` | `stop`, `destroy` |
| Udev rules (video) | `/etc/udev/rules.d/70-intuneme-video.rules` | `start` | `stop`, `destroy` |
| Udev helper script | `/usr/local/lib/intuneme/usb-hotplug` | `start` | `stop`, `destroy` |
| Extension polkit policy | `/etc/polkit-1/actions/org.frostyard.intuneme.policy` | `extension install` | Manual |
| SELinux policy | System policy store | `init` (if SELinux) | Manual |

## Detailed Subsystem Docs

- [Container Lifecycle](container-lifecycle.md) — init, start, stop, destroy, recreate flows
- [Broker Proxy](broker-proxy.md) — D-Bus forwarding for host-side SSO
- [Container Image](container-image.md) — Build process, packages, and system configuration
