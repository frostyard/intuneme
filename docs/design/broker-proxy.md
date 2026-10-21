# Broker Proxy

The broker proxy forwards the Microsoft identity broker D-Bus service (`com.microsoft.identity.broker1`) from the container's session bus to the host's session bus. This enables host-side applications (Edge, VS Code, MSAL libraries) to transparently use the container's Intune enrollment for SSO.

## How It Works

```
Host app (Edge)  →  Host session bus  →  intuneme broker-proxy  →  Container session bus  →  microsoft-identity-broker
```

The proxy listens on the host's session bus under the well-known name `com.microsoft.identity.broker1` and forwards method calls to the same service running inside the container.

## D-Bus Interface

Bus name: `com.microsoft.identity.broker1`
Object path: `/com/microsoft/identity/broker1`
Interface: `com.microsoft.identity.Broker1` (note: capital B)

**Methods (8 total):**
- `acquireTokenInteractively` — Interactive auth (browser popup)
- `acquireTokenSilently` — Silent token refresh
- `acquirePrtSsoCookie` — Primary Refresh Token SSO cookie
- `generateSignedHttpRequest` — Signed HTTP request for device auth
- `getAccounts` — List enrolled accounts
- `removeAccount` — Remove an account
- `getLinuxBrokerVersion` — Broker version string
- `cancelInteractiveFlow` — Cancel in-progress interactive auth

All methods accept and return string parameters (JSON-encoded payloads).

## Setup Requirements

The broker proxy requires several pieces of infrastructure:

### Runtime Directory

A host-side directory (`~/.local/share/intuneme/runtime/`) is bind-mounted as `/run/user/<uid>` inside the container. This makes the container's session bus socket accessible from the host at `~/.local/share/intuneme/runtime/bus`.

### Systemd Linger

Linger must be enabled for the container user so that the user session (and session bus) persists even without an active login.

### Login Session

A login session must be created inside the container so that the session bus socket is initialized. The `start` command creates this via `machinectl shell <user>@<machine> /bin/bash --login -c "exec sleep infinity"`. The login shell sources `intuneme.sh` (initializing keyring, display, etc.) and stays alive via `sleep infinity` so the keyring daemon persists. The `start` command waits up to 30 seconds for the session bus socket to appear.

### D-Bus Service Activation

When enabled, a D-Bus service file is installed at `~/.local/share/dbus-1/services/com.microsoft.identity.broker1.service` that triggers `intuneme broker-proxy --root <root>` on first access to `com.microsoft.identity.broker1`. This enables lazy activation — the proxy starts automatically when an app needs it.

`DBusServiceFileContent(execPath, root)` passes `--root` so a D-Bus activated instance writes the same PID file (`<root>/broker-proxy.pid`) as the one `intuneme start` uses. Both the executable path and the root go through `quoteExecArg`: dbus-daemon splits `Exec=` with shell-like rules, so an argument with a space or other special character is wrapped in single quotes (an embedded `'` becomes `'\''`).

**Upgrade note:** only `intuneme config broker-proxy enable` writes the service file. `start` does not rewrite it. A service file written by an older release has no `--root`; run `intuneme config broker-proxy enable` again to refresh it.

## Enabling/Disabling

```bash
intuneme config broker-proxy enable   # Sets flag, installs D-Bus service file
intuneme config broker-proxy disable  # Clears flag, removes D-Bus service file, stops proxy
```

The `start` command automatically launches the proxy if the flag is set.

## Process Management

- PID file: `~/.local/share/intuneme/broker-proxy.pid`
- Started as background process with `setsid` for terminal independence
- Startup verified by polling PID file (up to 5 seconds, 500ms intervals)
- Stopped gracefully by `stop` command before container poweroff
- `status` command reports proxy running state (see below)

## Diagnostics: who owns the bus name?

`broker.LookupNameOwner` (a function variable, replaced in unit tests so CI needs no session bus) asks the host session bus `org.freedesktop.DBus.GetNameOwner(com.microsoft.identity.broker1)` and then `org.freedesktop.DBus.GetConnectionUnixProcessID(owner)`. `broker.FindNameOwner` adds the process name from `/proc/<pid>/comm` and whether the process is an `intuneme broker-proxy` (from `/proc/<pid>/cmdline`).

- **`broker-proxy` error.** When `RequestName` does not return primary owner, the error names the owner: `bus name com.microsoft.identity.broker1 already owned by PID 12345 (microsoft-identity-broker); another identity broker or another intuneme broker-proxy is probably running on the host`. If the lookup fails, the old message `bus name ... already owned` is kept.
- **`status`.** Only when `broker_proxy` is enabled in config, and only when the PID file shows no running proxy, `status` queries the bus. Output: `running (PID N, D-Bus activated)` if the owner is an `intuneme broker-proxy`, `not running (bus name owned by PID N (name))` for any other owner, else `not running`. The JSON key `broker_proxy` stays a string.

## Key Implementation Files

| File | Purpose |
|------|---------|
| `internal/broker/proxy.go` | Core proxy logic — bus connections, method forwarding, D-Bus constants, introspection XML, PID management, service file generation |
| `internal/broker/session.go` | Runtime directory helpers, session bus socket path, machinectl session/linger args |
| `cmd/broker_proxy.go` | CLI command (`intuneme broker-proxy`) that runs the proxy in the foreground |
| `cmd/config.go` | Enable/disable subcommands |
