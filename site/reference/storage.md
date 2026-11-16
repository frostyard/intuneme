# Storage Layout

intuneme uses two directories on the host: a data directory for container state, and a shared home directory for persistent enrollment data.

## Data directory

```
~/.local/share/intuneme/
├── config.toml      # Machine name, rootfs path, host UID, image family/variant
├── himmelblau.conf # Optional customized Himmelblau config (Himmelblau mode)
├── rootfs/          # Selected Ubuntu identity image and Edge
└── runtime/
    ├── bus/          # Broker-proxy session bus socket, when enabled
    └── himmelblau-bluez/ # Filtered host BlueZ proxy socket directory
```

| Path | Description |
|------|-------------|
| `config.toml` | Configuration file. See [Configuration Reference](configuration.md). |
| `himmelblau.conf` | Optional, comment-preserving Himmelblau config override; installed into the rootfs before each container boot and carried across `recreate`. Created or updated with `edit --himmelblau`; editable directly on the host and removed by `destroy`. |
| `rootfs/` | The container root filesystem. Extracted from the OCI image by `intuneme init`. This directory is the nspawn container root. Removed and recreated by `intuneme recreate` or `intuneme destroy`. |
| `runtime/` | Bind-mounted into the container as `/run/user/<uid>` when the broker, notify or screencast proxy is enabled. This makes the container's session D-Bus socket visible on the host at `runtime/bus`. |
| `runtime/himmelblau-bluez/` | Private directory prepared and bound read-only at container boot only for terminal-passkey images, independently of the session-bus proxies. The filtered BlueZ socket exists only during enrollment; it does not expose the full host system bus. |

The data root can be overridden with `--root <path>` on any command.

## Runtime paths (while container is running)

| Path | Description |
|------|-------------|
| `/run/intuneme/devices/` | Udev forwarded device state (tmpfs, only while running) |
| `/opt/intuneme-mcp/` | Host directory of an MCP server binary, bind-mounted read-only by `intuneme mcp` (runtime-only; re-established on demand, never enters the rootfs) |
| `/run/host-nvidia/0/`, `/run/host-nvidia/1/`, ... | Nvidia host library directories bind-mounted read-only into the container (only on Nvidia systems) |
| `$XDG_RUNTIME_DIR/intuneme-notify/` (host) | Private copies of notification images, made by the notify proxy. Emptied when the proxy starts and removed when it exits. |

## Container home directory

```
~/Intune/
├── .config/
│   ├── intune/      # Intune enrollment state and certificates
│   └── microsoft-edge/  # Edge profile (Microsoft image only)
├── .local/
│   ├── share/
│   │   └── keyrings/    # gnome-keyring data (broker credentials)
│   └── ...
├── Downloads/       # File exchange between container and host
└── ...
```

`~/Intune/` on the host is bind-mounted as `/home/<user>/` inside the container — the container user's home directory. This means enrollment state, browser profiles, keyring data, and downloads all live on the host filesystem and survive container rebuilds.

In the experimental Himmelblau mode, the same host directory remains
bind-mounted at `/home/<host-user>`. `intuneme enroll` maps that existing local
account to an Entra UPN, so its UID, GID, groups, and home remain unchanged.
Himmelblau's daemon uses systemd `DynamicUser` state under
`rootfs/var/lib/private/himmelblaud/` and
`rootfs/var/cache/private/himmelblaud/`, exposed through systemd-managed public
paths while running. Policy and NSS caches remain under `rootfs/var/cache/`.
`recreate` backs up through the public paths and restores dynamic-user data to
the private backing paths so systemd can recreate the symlinks. It also backs
up `/var/lib/systemd/credential.secret`, needed to decrypt the daemon's HSM PIN
credential. If that key is missing while the encrypted PIN remains, recreate
refuses to replace the rootfs.

!!! note
    `intuneme destroy` removes the rootfs, saved Himmelblau config, udev rules, polkit rule, and sudoers rule. It also cleans Microsoft Intune enrollment state from `~/Intune/`, including `.config/intune/`, `.local/share/intune/`, `.local/share/intune-portal/`, `.local/share/keyrings/`, `.local/state/microsoft-identity-broker/`, and `.cache/intune-portal/`. Other files in `~/Intune/` (Downloads, Edge profile, etc.) are preserved. Himmelblau daemon caches are inside the rootfs and are removed with it.

    `intuneme destroy --all` additionally removes the GNOME extension, polkit policy action, D-Bus broker service file, the entire `~/Intune/` directory, and the entire data root directory (default `~/.local/share/intuneme/`) — a full uninstall of all intuneme artifacts from the host.

## What persists across `recreate`

`intuneme recreate` replaces the rootfs with a new image while preserving the selected identity stack's state:

- Microsoft device-broker enrollment state, or Himmelblau daemon/policy/NSS/HSM state
- Container user password hash
- `~/Intune/` contents (untouched — it lives on the host)

No re-enrollment is required after `recreate`.
