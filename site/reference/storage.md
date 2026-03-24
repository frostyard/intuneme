# Storage Layout

intuneme uses two directories on the host: a data directory for container state, and a shared home directory for persistent enrollment data.

## Data directory

```
~/.local/share/intuneme/
├── config.toml      # Machine name, rootfs path, host UID, flags
├── rootfs/          # Ubuntu 24.04 rootfs with Intune and Edge
└── runtime/         # Bind-mounted as /run/user/<uid> in the container
                     # when broker_proxy is enabled; exposes session bus socket
```

| Path | Description |
|------|-------------|
| `config.toml` | Configuration file. See [Configuration Reference](configuration.md). |
| `rootfs/` | The container root filesystem. Extracted from the OCI image by `intuneme init`. This directory is the nspawn container root. Removed and recreated by `intuneme recreate` or `intuneme destroy`. |
| `runtime/` | Bind-mounted into the container as `/run/user/<uid>` when the broker proxy is enabled. This makes the container's session D-Bus socket visible on the host at `runtime/bus`. Not used when `broker_proxy = false`. |

The data root can be overridden with `--root <path>` on any command.

## Runtime paths (while container is running)

| Path | Description |
|------|-------------|
| `/run/intuneme/devices/` | Udev forwarded device state (tmpfs, only while running) |
| `/run/host-nvidia/0/`, `/run/host-nvidia/1/`, ... | Nvidia host library directories bind-mounted read-only into the container (only on Nvidia systems) |

## Container home directory

```
~/Intune/
├── .config/
│   ├── intune/      # Intune enrollment state and certificates
│   └── microsoft-edge/  # Edge browser profile
├── .local/
│   ├── share/
│   │   └── keyrings/    # gnome-keyring data (broker credentials)
│   └── ...
├── Downloads/       # File exchange between container and host
└── ...
```

`~/Intune/` on the host is bind-mounted as `/home/<user>/` inside the container — the container user's home directory. This means enrollment state, browser profiles, keyring data, and downloads all live on the host filesystem and survive container rebuilds.

!!! note
    `intuneme destroy` removes the rootfs, udev rules, polkit rule, and sudoers rule. It also cleans Intune enrollment state from `~/Intune/`, including `.config/intune/`, `.local/share/intune/`, `.local/share/intune-portal/`, `.local/share/keyrings/`, `.local/state/microsoft-identity-broker/`, and `.cache/intune-portal/`. Other files in `~/Intune/` (Downloads, Edge profile, etc.) are preserved.

## What persists across `recreate`

`intuneme recreate` replaces the rootfs with a new image while preserving the most important state:

- Intune enrollment (device registration database)
- Container user password hash
- `~/Intune/` contents (untouched — it lives on the host)

No re-enrollment is required after `recreate`.
