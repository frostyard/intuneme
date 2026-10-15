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
| `himmelblau` | bool | `false` | Use the experimental Himmelblau image (`ghcr.io/frostyard/ubuntu-himmelblau`) instead of the Microsoft Intune default. Set during `init` with `--himmelblau`; the image family cannot be switched by `recreate`. |
| `himmelblau_terminal_passkey` | bool | `false` | Use the local-build-only fork-based terminal-passkey variant. Requires `himmelblau = true` and a local image supplied with `--himmelblau-local-image`; preserved by `recreate`. |
| `himmelblau_terminal_passkey_source` | string | _(unset)_ | Fork commit recorded from the selected terminal-passkey image. Managed by `init`; `start` and `enroll` require the rootfs marker to match it, while `recreate` updates it if the saved local image was rebuilt from a new source commit. |
| `himmelblau_domain` | string | _(unset)_ | Entra sign-in domain configured during `init --himmelblau` (prompted, or supplied with `--himmelblau-domain`). UPNs used by `intuneme enroll` must match this domain. |
| `himmelblau_local_image` | string | _(unset)_ | Optional `localhost/...` image override, set with `init` or `recreate --himmelblau-local-image`. Required for terminal-passkey mode and persisted so `recreate` reuses the same local image. |
| `himmelblau_upn` | string | _(unset)_ | Entra UPN mapped to the host-matched local account by `intuneme enroll` after explicit `machine-shell` PAM authentication succeeds. This is a saved enrollment marker, not a live cloud-token check. |
| `mcp_binary` | string | _(unset)_ | Host path to a self-contained MCP server binary that `intuneme mcp` runs inside the container. Any MCP server works; there is no built-in default. The binary's directory is bind-mounted into the container at runtime, so it stays out of the rootfs and survives `recreate`. Override per-invocation with `intuneme mcp --binary`. See [MCP Servers](../user-guide/mcp-servers.md). |
| `mcp_args` | array of strings | _(empty)_ | Default arguments passed to the MCP server binary by `intuneme mcp`. For a server whose stdio mode is a subcommand, set e.g. `mcp_args = ["mcp"]` so the VS Code config can be just `["mcp"]`. Trailing `intuneme mcp -- args...` override these. |

## Example

A typical config file after `intuneme init`:

```toml
machine_name = "intuneme"
rootfs_path = "/home/alice/.local/share/intuneme/rootfs"
host_uid = 1000
host_user = "alice"
broker_proxy = false
insiders = false
himmelblau = false
himmelblau_terminal_passkey = false
himmelblau_terminal_passkey_source = ""
himmelblau_domain = ""
himmelblau_local_image = ""
himmelblau_upn = ""
mcp_binary = ""
mcp_args = []
```

Most fields are set once by `intuneme init` and rarely need changing. The `broker_proxy` field is managed by:

```bash
intuneme config broker-proxy enable
intuneme config broker-proxy disable
```

For other fields, edit `config.toml` directly. Changes to `machine_name` or `rootfs_path` require stopping the container first.

!!! warning
    Changing `host_uid` or `host_user` after init is not supported without re-running `intuneme init --force`, as the container user is provisioned to match these values.

!!! warning "Himmelblau local account mapping"
    Himmelblau mode maps the local container account to an Entra UPN through `user_map_file`. It keeps the local account's existing UID, GID, groups, and home. See the [Himmelblau guide](../user-guide/himmelblau.md).

## Error handling

If `config.toml` contains invalid TOML syntax, intuneme will report a parse error and refuse to continue. This prevents silent fallback to default values, which could lead to unexpected behavior (e.g., operating on the wrong rootfs path).

If the home directory cannot be determined (e.g., `$HOME` is unset), intuneme reports an error rather than constructing a relative path. This prevents dangerous operations like `sudo rm -rf` on a relative path.
