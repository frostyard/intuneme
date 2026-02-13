# intuneme

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

intuneme CLI                      systemd (PID 1)
                                   ├─ microsoft-identity-broker
                                   ├─ microsoft-identity-device-broker
                                   ├─ intune-agent (timer)
                                   ├─ gnome-keyring-daemon
                                   └─ intune-portal / microsoft-edge
```

## Prerequisites

The host needs:

- **systemd-nspawn** and **machinectl** (package: `systemd-container`)
- **podman** (used to pull and extract the OCI base image)
- A graphical session (X11 or Wayland)

On Debian/Ubuntu:

```bash
sudo apt install systemd-container podman
```

## Install

```bash
go install github.com/frostyard/intune@latest
```

Or build from source:

```bash
git clone https://github.com/frostyard/intune.git
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
```

## Commands

| Command | Description |
|---------|-------------|
| `intuneme init` | Pull the OCI image, extract rootfs, install Edge, configure user/PAM/services |
| `intuneme start` | Boot the container |
| `intuneme shell` | Open an interactive shell (real logind session with D-Bus and keyring) |
| `intuneme stop` | Shut down the container |
| `intuneme status` | Show whether the container is initialized and running |
| `intuneme destroy` | Stop the container, remove the rootfs, clean enrollment state |

### Flags

- `--root <path>` — Override the data directory (default: `~/.local/share/intuneme/`)
- `--force` — Force re-initialization (with `init`)

## What `init` does

1. Checks that `systemd-nspawn`, `machinectl`, and `podman` are installed
2. Creates `~/Intune` on the host
3. Pulls `ghcr.io/frostyard/ubuntu-intune:latest`
4. Extracts the rootfs into `~/.local/share/intuneme/rootfs/`
5. Creates a container user matching your host UID/GID
6. Adds the Microsoft Edge apt repo and installs `microsoft-edge-stable`, `libsecret-tools`, and `sudo`
7. Enables the system identity device broker service
8. Applies configuration: hostname, password policy, PAM modules, intune-agent timer, display environment, Edge Wayland wrapper, broker display override, login profile script
9. Installs a polkit rule so `sudo` group members can use machinectl without repeated password prompts
10. Saves configuration to `~/.local/share/intuneme/config.toml`

## Storage

```
~/.local/share/intuneme/
├── rootfs/          # Ubuntu 24.04 rootfs with Intune + Edge
└── config.toml      # machine name, rootfs path, host UID

~/Intune/            # Container user's home (persists across rebuilds)
├── .config/intune/  # Enrollment state
├── .config/         # Edge profile, app config
├── .local/          # Keyring, broker state
├── Downloads/       # Downloads, file exchange with host
└── ...
```

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

**No sound in Edge**
Check that PipeWire is forwarded. The host needs a PipeWire socket at `/run/user/$UID/pipewire-0`. Inside the container, verify `$PIPEWIRE_REMOTE` is set.

## How it differs from mkosi-intune

[mkosi-intune](https://github.com/4nd3r/mkosi-intune) builds the entire rootfs from scratch with mkosi and debootstrap. `intuneme` uses a pre-built OCI image and installs Edge on top, which is faster to set up. Both approaches run a booted nspawn container with Edge inside.

## License

MIT
