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
├── runner/           Command execution abstraction (mockable interface)
├── sudoers/          Sudoers rule install/remove for passwordless nsenter
├── udev/             Udev rules + hotplug script for YubiKey and video devices
└── version/          Build version + OCI image ref resolution
ubuntu-intune/        Container image definition
├── Containerfile     Multi-stage build (Ubuntu 24.04 base)
├── build_files/      Build script (package install, PAM config, patches)
└── system_files/     Static config files copied into image
polkit/               Polkit rule template for machinectl access
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
| X11 auth | `$XAUTHORITY` | `/run/host-xauthority` | Auto-detected on start |
| GPU | `/dev/dri/card*`, `/dev/dri/renderD*` | Same | Individual devices for cgroup |
| Broker runtime | `~/.local/share/intuneme/runtime` | `/run/user/<uid>` | When broker proxy enabled |

### Profile Script Environment

The container profile script (`/etc/profile.d/intuneme.sh`, embedded in Go binary) runs on every login shell session and:

1. Reads `DISPLAY` from `/etc/intuneme-host-display` (written by `start`), defaults to `:0`
2. Extends `PATH` with `/opt/microsoft/intune/bin` and `/opt/microsoft/microsoft-azurevpnclient`
3. Sets `XAUTHORITY=/run/host-xauthority` if bind-mounted
4. Imports display/audio vars into systemd user session so services see them
5. Detects Wayland (`WAYLAND_DISPLAY`), PipeWire (`PIPEWIRE_REMOTE`), PulseAudio (`PULSE_SERVER`) from `/run/host-*` sockets
6. On first login per boot (marker in `/tmp`):
   - Initializes `gnome-keyring-daemon` with `--replace --unlock`
   - Stores a test secret to force default keyring collection creation
   - Restarts identity brokers to pick up the initialized keyring
7. Starts `intune-agent.timer` for compliance checks

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

## Storage Layout

```
~/.local/share/intuneme/
├── config.toml          Configuration
├── rootfs/              Extracted container filesystem
├── runtime/             Broker proxy runtime dir (bind-mounted as /run/user/<uid>)
├── broker-proxy.pid     PID file for broker proxy process
└── udev/                Hotplug helper script + state

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
| Udev rules | `/etc/udev/rules.d/70-intuneme-*.rules` | `start` | `stop` |
| Udev helper script | `/usr/local/lib/intuneme/usb-hotplug` | `start` | `stop` |
| Extension polkit policy | `/etc/polkit-1/actions/org.frostyard.intuneme.policy` | `extension install` | Manual |
| SELinux policy | System policy store | `init` (if SELinux) | Manual |

## Detailed Subsystem Docs

- [Container Lifecycle](container-lifecycle.md) — init, start, stop, destroy, recreate flows
- [Broker Proxy](broker-proxy.md) — D-Bus forwarding for host-side SSO
- [Container Image](container-image.md) — Build process, packages, and system configuration
