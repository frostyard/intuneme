# MCP Servers

`intuneme mcp` runs a [Model Context Protocol](https://modelcontextprotocol.io/) (MCP) server **inside the container**, in the foreground, with its standard input/output wired straight to the caller. This lets a host-side client such as VS Code talk to an MCP server that must authenticate against the container's enrolled tenant — without enabling the broker proxy.

## When to use this

Use `intuneme mcp` instead of the [broker proxy](broker-proxy.md) when the MCP server has to use the **container's** Intune identity. The most common reason is running two tenants at once: one enrolled on the host and a second, separate tenant inside intuneme. The broker proxy can only forward a single broker onto the host session bus, so a server that needs the container tenant has to run *inside* the container — which is exactly what this command does.

If you only have one tenant and it is enrolled in the container, the broker proxy is usually simpler: it lets host-native apps (including a host-native MCP server) get SSO without entering the container at all.

## How it works

1. The server binary lives on the **host**, outside the container rootfs.
2. `intuneme mcp` bind-mounts the binary's directory into the running container at `/opt/intuneme-mcp/` (read-only). The mount is runtime-only and is re-established on demand, so it **survives `intuneme recreate`** and never becomes part of the image layer.
3. The binary is launched in the container's namespaces (via the same `nsenter` path used by `intuneme open`), in the **foreground** with no TTY, so JSON-RPC flows cleanly over stdio.
4. The container session is initialized first (display, D-Bus, keyring) so a server that triggers interactive sign-in can reach the container's identity broker.

```
HOST                                     intuneme container (enrolled tenant)
────                                     ───────────────────────────────────
VS Code
  └─ spawns  intuneme mcp ──nsenter──▶   server binary  (stdio ⇄ VS Code)
                                              └─ MSAL → container session bus
                                                       microsoft-identity-broker
```

## Provide a server binary

Point intuneme at a **self-contained** server binary on the host (one that needs no runtime installed in the container). Any MCP server works — there is no assumption about a particular tool.

For a .NET server, publish a single-file, self-contained build:

```bash
dotnet publish -r linux-x64 --self-contained -p:PublishSingleFile=true
```

!!! note
    A self-contained .NET `linux-x64` build targets a low glibc baseline, so building on the host and running it in the Ubuntu 24.04 container works. If you hit a glibc/ABI error, build or extract the binary from inside the container instead (`intuneme shell`).

### Point intuneme at the binary

Place the binary anywhere on the host outside the rootfs and `~/Intune` (e.g. `~/.local/share/intuneme/mcp/`). Set the path and any server arguments in `config.toml`:

```toml
mcp_binary = "/home/alice/.local/share/intuneme/mcp/server"
mcp_args = ["mcp"]   # arguments handed to the server binary; e.g. a stdio subcommand
```

`mcp_args` are the arguments handed to the server binary. If the server starts its stdio mode via a subcommand, put it here; a server that starts with no subcommand can leave `mcp_args` empty. Override per-invocation with `intuneme mcp --binary <path>` or trailing `intuneme mcp -- <args>`.

## Configure VS Code

Add the server to `.vscode/mcp.json` (workspace) or your user `mcp.json`. The JSON key is the server's display name; the server's own subcommand lives in `mcp_args`, so the config stays minimal:

```json
{
  "servers": {
    "my-server": {
      "type": "stdio",
      "command": "intuneme",
      "args": ["mcp"]
    }
  }
}
```

Here `"my-server"` is just the name shown in VS Code, and `["mcp"]` is the `intuneme` subcommand. The server's own arguments come from `mcp_args` in `config.toml`. To pass server arguments inline instead, append them after `--`: `"args": ["mcp", "--", "<server-arg>"]`.

## Verify before wiring up the client

The approach only yields a compliant token if the server's identity stack actually uses the **Linux broker**. Confirm the broker is reachable from inside the container first:

```bash
intuneme shell
# inside the container:
dbus-send --session --print-reply --dest=com.microsoft.identity.broker1 \
  /com/microsoft/identity/broker1 \
  com.microsoft.identity.Broker1.getLinuxBrokerVersion \
  string:"1.0" string:"test" string:"{}"
```

A version string back means the broker is live. Then run the server once inside the container and confirm it authenticates silently rather than falling back to a browser or device-code flow.

## Requirements

- Container must be running (`intuneme start`)
- The tenant must be enrolled in Intune (run `intune-portal` from `intuneme shell` if not yet enrolled)
- A self-contained server binary on the host (`mcp_binary` or `--binary`)

!!! warning
    The server runs with the container user's session and can mint tokens against the enrolled tenant. Only run servers you trust.
