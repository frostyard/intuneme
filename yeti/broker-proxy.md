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
Interface: `com.microsoft.identity.broker1`

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

A login session must be created inside the container so that the session bus socket is initialized. The `start` command creates this via `machinectl` and waits for the socket to appear.

### D-Bus Service Activation

When enabled, a D-Bus service file is installed on the host that triggers `intuneme broker-proxy` on first access to `com.microsoft.identity.broker1`. This enables lazy activation — the proxy starts automatically when an app needs it.

## Enabling/Disabling

```bash
intuneme config broker-proxy enable   # Sets flag, installs D-Bus service file
intuneme config broker-proxy disable  # Clears flag, removes D-Bus service file, stops proxy
```

The `start` command automatically launches the proxy if the flag is set.

## Process Management

- PID file: `~/.local/share/intuneme/broker-proxy.pid`
- Started as background process with `setsid` for terminal independence
- Stopped gracefully by `stop` command before container poweroff
- `status` command reports proxy running state

## Key Implementation Files

| File | Purpose |
|------|---------|
| `internal/broker/proxy.go` | Core proxy logic — bus connections, method forwarding |
| `internal/broker/session.go` | D-Bus constants, introspection XML, session bus paths, PID management |
| `cmd/broker_proxy.go` | CLI command that calls `broker.Run()` |
| `cmd/config.go` | Enable/disable subcommands |
