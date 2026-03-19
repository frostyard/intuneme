# intuneme
[![OpenSSF Scorecard](https://api.scorecard.dev/projects/github.com/frostyard/intuneme/badge)](https://scorecard.dev/viewer/?uri=github.com/frostyard/intuneme)

Run Microsoft Intune on an immutable Linux host.

`intuneme` provisions and manages a [systemd-nspawn](https://www.freedesktop.org/software/systemd/man/systemd-nspawn.html) container running Ubuntu 24.04 with Intune Portal, the Microsoft Identity Broker, and Microsoft Edge. The container handles enrollment, compliance, and corporate resource access while making minimal changes to the host.

## How it works

The container boots a full systemd instance with its own D-Bus session, gnome-keyring, and user services. The host provides display access (X11/Wayland), audio (PipeWire), and GPU acceleration through individual socket bind mounts. A dedicated `~/Intune` directory on the host serves as the container user's home, persisting enrollment state, browser profiles, and downloads across container rebuilds.

```
Host                              Container (systemd-nspawn)
────────────────────             ────────────────────────────
~/Intune/ ──────────────bind──→  /home/<user>/
/tmp/.X11-unix ─────────bind──→  /tmp/.X11-unix
/run/user/$UID/wayland-0 bind──→ /run/host-wayland
/run/user/$UID/pipewire-0 bind─→ /run/host-pipewire
Xauthority file ────────bind──→  /run/host-xauthority
/dev/dri ───────────────bind──→  /dev/dri
/dev/video* ─────────nsenter──→  /dev/video*  (hotplug)
/dev/media* ─────────nsenter──→  /dev/media*  (hotplug)
/dev/bus/usb/* ──────nsenter──→  /dev/bus/usb/*  (YubiKey hotplug)
/dev/hidraw* ────────nsenter──→  /dev/hidraw*    (YubiKey hotplug)

intuneme CLI                      systemd (PID 1)
  └─ broker-proxy (opt-in) ─D-Bus─→ ├─ microsoft-identity-broker
                                   ├─ microsoft-identity-device-broker
                                   ├─ intune-agent (timer)
                                   ├─ gnome-keyring-daemon
                                   └─ intune-portal / microsoft-edge
```

## Broker proxy (host-side SSO)

By default, the identity broker only runs inside the container. If you want host-side apps like Microsoft Edge or VS Code to authenticate via your Intune enrollment, enable the broker proxy:

```bash
intuneme config broker-proxy enable
intuneme stop && intuneme start
```

This starts a D-Bus forwarding proxy on the host session bus that relays `com.microsoft.identity.broker1` calls to the container's broker. Host apps using MSAL (Edge, VS Code, etc.) will transparently use the container's enrollment for SSO and conditional access — no changes needed on the app side.

To disable:

```bash
intuneme config broker-proxy disable
intuneme stop && intuneme start
```

When enabled, `intuneme start` also creates a login session inside the container (for gnome-keyring and the broker's user services) and `intuneme status` shows the proxy state.

## Device hotplug

YubiKey USB security keys and video capture devices (webcams) are automatically forwarded into the container via udev hotplug rules. When you run `intuneme start`, udev rules are installed on the host that detect these devices and forward them into the running container via `nsenter`.

### YubiKey passthrough

Yubico devices (vendor ID `1050`) are detected by USB vendor ID. This works regardless of which physical USB port the key is plugged into.

### Webcam passthrough

V4L2 video devices (`/dev/video*`) and media controllers (`/dev/media*`) are forwarded automatically. This means you can dock/undock your laptop without restarting the container — the camera will appear inside the container when connected and be cleaned up when disconnected.

### Common behavior

- **Hot-plug**: Plugging in a device while the container is running automatically forwards it.
- **Hot-unplug**: Removing the device cleans up the device node inside the container.
- **Already-plugged devices**: Devices present before `intuneme start` are detected and forwarded at boot.
- **Automatic lifecycle**: Rules are installed on `intuneme start` and removed on `intuneme stop`.

For manual control (e.g., cleaning up stray rules after a crash):

```bash
# Install rules without starting the container
intuneme udev install

# Remove rules without stopping the container
intuneme udev remove
```

Both commands are idempotent and graceful — `udev remove` succeeds even if no rules are installed.

To check forwarding logs: `journalctl -t intuneme-hotplug`.

## Prerequisites

The host needs:

- **systemd-nspawn** and **machinectl** (package: `systemd-container`)
- A container engine (**podman, docker, umoci** - used to pull and extract the OCI base image)
- A graphical session (X11 or Wayland)

On Debian/Ubuntu:

```bash
sudo apt install systemd-container podman
```

## Install

```bash
go install github.com/frostyard/intuneme@latest
```

Or build from source:

```bash
git clone https://github.com/frostyard/intuneme.git
cd intuneme
go build -o intuneme .
```

## Quick start

```bash
# 1. Provision the container (pulls image, installs Edge, configures services)
intuneme init

# 2. Boot the container
intuneme start

# 3. Open a shell inside the container
intuneme shell

# 4. Inside the container — enroll in Intune
intune-portal

# 5. Inside the container — browse corporate resources
microsoft-edge

# 6. Inside the container - manage Yubikeys
ykman
```

## GNOME extension

A Quick Settings toggle lets you start/stop the container and open a shell without touching the terminal.

```bash
intuneme extension install
```

Log out and back in to activate. The toggle appears in Quick Settings with the container's current state. Clicking it starts or stops the container, and the popup menu shows status details and shortcuts to open a shell, launch Edge, or launch Intune Portal.

The extension monitors container state via D-Bus signals from `systemd-machined` for instant updates, with periodic polling as a fallback. Requires GNOME Shell 47+.

### Passwordless app launch

`intuneme init` installs a sudoers rule at `/etc/sudoers.d/intuneme-exec` that allows the host user to run `nsenter` into the container without a password prompt. This enables the GNOME extension (and `.desktop` shortcuts) to launch Edge and Intune Portal directly — no terminal window needed for sudo authentication.

The rule is scoped to the specific `nsenter` flag pattern used by `intuneme open` and only allows `su` to the host user. It persists across start/stop cycles and is removed by `intuneme destroy`.

## Desktop shortcuts

Install `.desktop` entries for Edge and Intune Portal so they appear in the GNOME application grid:

```bash
bash scripts/install-desktop-items.sh
```

Clicking either entry runs `intuneme open edge` or `intuneme open portal` — the container must already be running. To remove the entries:

```bash
bash scripts/install-desktop-items.sh --uninstall
```

## Commands

| Command | Description |
|---------|-------------|
| `intuneme init` | Pull the OCI image, extract rootfs, install Edge, configure user/PAM/services |
| `intuneme start` | Boot the container (installs udev hotplug rules) |
| `intuneme shell` | Open an interactive shell (real logind session with D-Bus and keyring) |
| `intuneme open edge` | Launch Microsoft Edge inside the container |
| `intuneme open portal` | Launch Intune Portal inside the container |
| `intuneme stop` | Shut down the container |
| `intuneme status` | Show whether the container is initialized and running |
| `intuneme udev install` | Install udev rules for device hotplug forwarding (normally automatic) |
| `intuneme udev remove` | Remove udev rules (graceful, safe to run anytime) |
| `intuneme recreate` | Upgrade the container image, preserving enrollment state |
| `intuneme destroy` | Stop the container, remove the rootfs, clean enrollment state |
| `intuneme config broker-proxy enable` | Enable the host-side broker proxy for SSO |
| `intuneme config broker-proxy disable` | Disable the broker proxy |
| `intuneme extension install` | Install the GNOME Shell Quick Settings extension |

### Flags

- `--root <path>` — Override the data directory (default: `~/.local/share/intuneme/`)
- `--force` — Force re-initialization (with `init`)
- `--insiders` — Use the insiders channel container image (with `init` or `recreate`)

## What `init` does

1. Checks that `systemd-nspawn`, `machinectl`, and `podman` are installed
2. Creates `~/Intune` on the host
3. Pulls `ghcr.io/frostyard/ubuntu-intune:latest` (or `:insiders` with `--insiders`)
4. Extracts the rootfs into `~/.local/share/intuneme/rootfs/`
5. Configures a `render` group matching the host for GPU access
6. Creates a container user matching your host UID/GID
7. Enables the system identity device broker service
8. Applies configuration: hostname, broker display override, login profile script, Edge wrapper
9. Installs a polkit rule so `sudo` and `wheel` group members can use machinectl without repeated password prompts
10. On SELinux systems (Fedora, Bazzite): relabels the rootfs as `container_file_t` and installs a policy module granting `systemd-machined` the PTY access needed for `machinectl shell`
11. Saves configuration to `~/.local/share/intuneme/config.toml`

## Storage

```
~/.local/share/intuneme/
├── rootfs/          # Ubuntu 24.04 rootfs with Intune + Edge
├── runtime/         # Bind-mounted as /run/user/<uid> in container (broker proxy)
└── config.toml      # machine name, rootfs path, host UID, broker_proxy, insiders flags

~/Intune/            # Container user's home (persists across rebuilds)
├── .config/intune/  # Enrollment state
├── .config/         # Edge profile, app config
├── .local/          # Keyring, broker state
├── Downloads/       # Downloads, file exchange with host
└── ...
```

## Upgrading the container

When a new container image is released, use `recreate` to upgrade without losing your Intune enrollment:

```bash
intuneme recreate
intuneme start
```

This stops the running container, backs up the password hash and device enrollment database, pulls the new image, re-provisions, and restores the backed-up state. No re-enrollment needed.

## Re-enrollment

If you need to start fresh with Intune:

```bash
intuneme destroy
intuneme init
intuneme start
intuneme shell
# Inside: intune-portal
```

`destroy` removes the rootfs and cleans Intune enrollment state from `~/Intune`. Your other files in `~/Intune` (Downloads, etc.) are preserved.

## Troubleshooting

**`intuneme shell` fails on Fedora/Bazzite (SELinux)**
`intuneme init` handles this automatically: it relabels the rootfs and installs a policy module for `systemd-machined`. If you upgraded from an older install and didn't re-run `init`, use the provided script: `bash scripts/fix-selinux.sh`.

**intune-portal crashes with "No authorization protocol specified"**
The XAUTHORITY file isn't being forwarded. Check that your host has an Xauthority file in `$XAUTHORITY` or `/run/user/$UID/` (patterns: `.mutter-Xwaylandauth.*`, `xauth_*`).

**intune-portal shows error 1001 or "UI web navigation failed"**
The identity broker services aren't running. Inside the container:
```bash
sudo systemctl status microsoft-identity-device-broker
systemctl --user status microsoft-identity-broker
```

**Compliance check fails**
The intune-agent timer may not be running. Inside the container:
```bash
systemctl --user start intune-agent.timer
/opt/microsoft/intune/bin/intune-agent
```

**Edge crashes with "Trace/breakpoint trap"**
Try launching with a fresh profile first: `microsoft-edge --user-data-dir=/tmp/edge-test`. If that works, the issue is a corrupted profile — move `~/.config/microsoft-edge/` aside and restart Edge.

**No sound in Edge**
Check that PipeWire is forwarded. The host needs a PipeWire socket at `/run/user/$UID/pipewire-0`. Inside the container, verify `$PIPEWIRE_REMOTE` is set.

**Webcam not available in Teams/Edge**
Check that udev rules are installed: `ls /etc/udev/rules.d/70-intuneme-video.rules`. If missing, run `intuneme udev install`. Verify the camera is detected on the host with `ls /dev/video*`. Check forwarding logs with `journalctl -t intuneme-hotplug`.

**YubiKey not detected inside the container**
Check that udev rules are installed: `ls /etc/udev/rules.d/70-intuneme-yubikey.rules`. If missing, run `intuneme udev install`. Check forwarding logs with `journalctl -t intuneme-hotplug`. Verify the key is detected on the host with `lsusb | grep Yubico`.

## How it differs from mkosi-intune

[mkosi-intune](https://github.com/4nd3r/mkosi-intune) builds the entire rootfs from scratch with mkosi and debootstrap. `intuneme` uses a pre-built OCI image which is faster to set up. Both approaches run a booted nspawn container with Edge inside.

## License

MIT
