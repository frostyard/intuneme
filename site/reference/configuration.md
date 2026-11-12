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
| `rootfs_path` | string | `~/.local/share/intuneme/rootfs` | Absolute path to the container root filesystem directory. intuneme refuses a value that is not absolute and clean, that is `/` or a top-level directory, or that is or contains the home directory or the data root, also after symlinks in the parent path are resolved. Every command then stops with an error that names the key. See [Error handling](#error-handling). |
| `host_uid` | int | current user UID | UID of the host user. Used to create the matching container user and set up bind mounts for `/run/user/<uid>`. Set automatically by `intuneme init`. |
| `host_user` | string | `$USER` | Username of the host user. Set automatically by `intuneme init`. |
| `broker_proxy` | bool | `false` | Enable the host-side D-Bus broker proxy. When `true`, `intuneme start` sets up the identity broker forwarding so host applications (Edge, VS Code) can use the container's Intune enrollment for SSO. See [Broker Proxy](../user-guide/broker-proxy.md). |
| `notify_proxy` | bool | `false` | Forward desktop notifications from the container to the host. When `true`, `intuneme start` runs a proxy so web notifications from the container's Edge, such as Outlook mail and calendar reminders, show on the host desktop. See [Desktop Notifications](../user-guide/notifications.md). |
| `screencast_proxy` | bool | `false` | Let apps in the container share host screens and windows. When `true`, `intuneme start` runs a proxy that forwards screen sharing requests from the container's Edge to the host's portal. See [Screen Sharing](../user-guide/screen-sharing.md). |
| `insiders` | bool | `false` | Use the insiders channel container image (`ghcr.io/frostyard/ubuntu-intune:insiders`) instead of the stable release. Set at init time with `--insiders`. `intuneme recreate` keeps it, and changes it only when `--insiders` or `--insiders=false` is given. |
| `mcp_binary` | string | _(unset)_ | Host path to a self-contained MCP server binary that `intuneme mcp` runs inside the container. Any MCP server works; there is no built-in default. The binary's directory is bind-mounted into the container at runtime, so it stays out of the rootfs and survives `recreate`. Override per-invocation with `intuneme mcp --binary`. See [MCP Servers](../user-guide/mcp-servers.md). |
| `mcp_args` | array of strings | _(empty)_ | Default arguments passed to the MCP server binary by `intuneme mcp`. For a server whose stdio mode is a subcommand, set e.g. `mcp_args = ["mcp"]` so the VS Code config can be just `["mcp"]`. Trailing `intuneme mcp -- args...` override these. |
| `hostname` | string | `<host hostname>LXC` | Container host name written to the container's `/etc/hostname` by `intuneme init` and `intuneme recreate`. It becomes the device name in Intune. The value must be a valid RFC 1123 host name of at most 64 characters. Leave it unset for the default. See [Container hostname](#container-hostname). |
| `product_name` | string | _(unset)_ | PC model presented to Intune through `/sys/class/dmi/id/product_name`. Optional; applied at cold boot only. See [Product name](#product-name). |

## Example

A typical config file after `intuneme init`:

```toml
machine_name = "intuneme"
rootfs_path = "/home/alice/.local/share/intuneme/rootfs"
host_uid = 1000
host_user = "alice"
broker_proxy = false
notify_proxy = false
screencast_proxy = false
insiders = false
mcp_binary = ""
mcp_args = []
```

Most fields are set once by `intuneme init` and rarely need changing. The `broker_proxy`, `notify_proxy` and `screencast_proxy` fields are managed by:

```bash
intuneme config broker-proxy enable
intuneme config broker-proxy disable
intuneme config notify-proxy enable
intuneme config notify-proxy disable
intuneme config screencast-proxy enable
intuneme config screencast-proxy disable
```

For other fields, edit `config.toml` directly. Changes to `machine_name` or `rootfs_path` require stopping the container first.

!!! warning
    Changing `host_uid` or `host_user` after init is not supported without re-running `intuneme init --force`, as the container user is provisioned to match these values.

## Container hostname

By default the container host name is your host name with an `LXC` suffix, for example `myworkstationLXC`. Set `hostname` to choose your own:

```toml
hostname = "corp-laptop-01"
```

The value is written to the container's `/etc/hostname` and `/etc/hosts`, and Intune shows it as the device name. It must be a valid RFC 1123 host name: 1 to 64 characters, dot-separated labels of ASCII letters, digits and `-`, and no label that starts or ends with `-`. An invalid value stops `intuneme init` and `intuneme recreate` before they change anything. The key takes effect only at `init` and `recreate`, not at `start`.

!!! warning
    Set `hostname` before enrollment. A change after enrollment, by `intuneme recreate` or a new `intuneme init`, can show in the Intune tenant as a renamed or new device. `intuneme recreate` prints a warning when the host name changes.

## Product name

Set `product_name` to choose the PC model that the Intune portal reads from `/sys/class/dmi/id/product_name`:

```toml
product_name = "ThinkPad X1 Carbon Gen 11"
```

The value must be 1 to 64 bytes of printable ASCII (0x20 through 0x7E), with no leading or trailing space. Control characters, newlines, tabs, DEL and non-ASCII characters are not allowed. An invalid value stops `intuneme start` before sudo or container changes, including in dry run. The key changes only `/sys/class/dmi/id/product_name`: other DMI sources in the container, such as `/sys/class/dmi/id/sys_vendor`, `/sys/class/dmi/id/modalias` and `/sys/firmware/dmi/tables/DMI`, still show the host values.

The override takes effect at each `intuneme start` that cold-boots the container. If it is already running, run `intuneme stop`, then `intuneme start`. An empty or unset key changes nothing on the next boot: the container sees the real host model. A file from an earlier boot stays in `<root>/dmi/` and is not used. The host must expose `/sys/class/dmi/id/product_name`; otherwise start fails before it asks for your password, and asks you to remove the key. The host file itself is never changed.

!!! warning "Tenant policy"
    The model your tenant sees is a tenant policy matter. Ask your Intune administrator before you present a model other than the real hardware.

## Error handling

If `config.toml` contains invalid TOML syntax, intuneme will report a parse error and refuse to continue. This prevents silent fallback to default values, which could lead to unexpected behavior (e.g., operating on the wrong rootfs path).

If the home directory cannot be determined (e.g., `$HOME` is unset), intuneme reports an error rather than constructing a relative path. This prevents dangerous operations like `sudo rm -rf` on a relative path.

`rootfs_path` gets the same protection. `destroy`, `recreate` and the cleanup of a failed `init` delete the rootfs directory with `sudo rm -rf`, so a wrong value would delete the wrong directory as root. intuneme refuses a `rootfs_path` that:

- is empty or not absolute,
- is not clean: it holds `.` or `..` elements, or doubled or trailing slashes,
- is `/` or a top-level directory such as `/home`, `/usr` or `/var`,
- is, or contains, the home directory or the data root (`~/.local/share/intuneme`).

A symlink in the parent path of `rootfs_path` is resolved first, and the same rules apply to the resolved directory, because `rm -rf` deletes the last element inside the resolved parent. A `rootfs_path` that is itself a symlink is allowed: `rm -rf` removes only the link. A parent path that does not exist skips those resolved checks.

Every command applies these rules when it reads `config.toml`, so it stops with an error that names `rootfs_path` before it changes anything. Every delete then checks the path again right before `sudo rm -rf`.
