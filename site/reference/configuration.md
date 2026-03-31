# Configuration Reference

intuneme stores its configuration at `~/.local/share/intuneme/config.toml`. The file is created automatically by `intuneme init` and updated by `intuneme config` subcommands. You can also edit it manually.

## File location

```
~/.local/share/intuneme/config.toml
```

The location can be overridden with the `--root` flag on any command. If you use a custom root, the config file will be at `<root>/config.toml`.

## Fields

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `machine_name` | string | `intuneme` | The systemd-nspawn machine name. Used by `machinectl` and `systemd-machined` to identify the container. |
| `rootfs_path` | string | `~/.local/share/intuneme/rootfs` | Absolute path to the container root filesystem directory. |
| `host_uid` | int | current user UID | UID of the host user. Used to create the matching container user and set up bind mounts for `/run/user/<uid>`. Set automatically by `intuneme init`. |
| `host_user` | string | `$USER` | Username of the host user. Set automatically by `intuneme init`. |
| `broker_proxy` | bool | `false` | Enable the host-side D-Bus broker proxy. When `true`, `intuneme start` sets up the identity broker forwarding so host applications (Edge, VS Code) can use the container's Intune enrollment for SSO. See [Broker Proxy](../user-guide/broker-proxy.md). |
| `insiders` | bool | `false` | Use the insiders channel container image (`ghcr.io/frostyard/ubuntu-intune:insiders`) instead of the stable release. Can be set at init time with `--insiders` and affects `intuneme recreate`. |

## Example

A typical config file after `intuneme init`:

```toml
machine_name = "intuneme"
rootfs_path = "/home/alice/.local/share/intuneme/rootfs"
host_uid = 1000
host_user = "alice"
broker_proxy = false
insiders = false
```

## Modifying the configuration

Most fields are set once by `intuneme init` and rarely need changing. The `broker_proxy` field is managed by:

```bash
intuneme config broker-proxy enable
intuneme config broker-proxy disable
```

For other fields, edit `config.toml` directly. Changes to `machine_name` or `rootfs_path` require stopping the container first.

!!! warning
    Changing `host_uid` or `host_user` after init is not supported without re-running `intuneme init --force`, as the container user is provisioned to match these values.

## Error handling

If `config.toml` contains invalid TOML syntax, intuneme will report a parse error and refuse to continue. This prevents silent fallback to default values, which could lead to unexpected behavior (e.g., operating on the wrong rootfs path).

If the home directory cannot be determined (e.g., `$HOME` is unset), intuneme reports an error rather than constructing a relative path. This prevents dangerous operations like `sudo rm -rf` on a relative path.
