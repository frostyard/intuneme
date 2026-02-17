# Host Broker Proxy via xdg-dbus-proxy

## Goal

Run Edge natively on the host with full desktop integration (taskbar, file associations, default browser) while keeping Intune enrollment, compliance, and the identity broker inside the container. Host apps (Edge, VS Code, others) get SSO via a D-Bus proxy that exposes `com.microsoft.identity.broker1` on the host's session bus.

## Motivation

- Edge as a native host app for proper desktop integration
- SSO tokens available to host-side applications (Edge, VS Code, future MSAL-aware apps)
- All Microsoft identity/compliance components stay containerized

## Approach

D-Bus forwarder service on the host session bus. A Go binary (`intuneme broker-proxy` subcommand) claims `com.microsoft.identity.broker1` on the host's session bus and forwards all method calls to the real broker running inside the container. D-Bus activation starts the proxy on demand.

### Why Not xdg-dbus-proxy Directly

xdg-dbus-proxy creates a filtered proxy socket, but MSAL expects the broker on the standard session bus (`$DBUS_SESSION_BUS_ADDRESS`). There's no way to point MSAL at a custom socket. A forwarder that claims the well-known name on the host bus is transparent to all clients.

### Why Not Move the Broker to the Host

Installing `microsoft-identity-broker` on the host defeats containerization. The broker and device broker share state (keyring, enrollment tokens) — splitting them across host and container would break enrollment coherence.

## Opt-In Design

This is controlled by a config flag. Default behavior is unchanged.

```toml
# ~/.local/share/intuneme/config.toml
broker_proxy = false   # default: everything in container as today
```

Enable/disable via:

```
intuneme config broker-proxy enable    # sets flag, installs D-Bus activation file
intuneme config broker-proxy disable   # sets flag, removes activation file, stops proxy
```

## Architecture

```
Host session bus                          Container session bus
─────────────────                         ──────────────────────

[Edge]                                    [microsoft-identity-broker]
[VS Code]  ──D-Bus──▶ [intuneme          (com.microsoft.identity.broker1)
[other]                 broker-proxy] ──▶
                            │
                  Owns com.microsoft.identity.broker1
                  on the HOST session bus.
                  Forwards calls to container bus at
                  <rootfs>/run/user/<uid>/bus
```

## D-Bus Interface Proxied

All 8 methods on `com.microsoft.identity.Broker1` — identical signature (3 strings in, 1 string out):

- `acquireTokenInteractively`
- `acquireTokenSilently`
- `getAccounts`
- `removeAccount`
- `acquirePrtSsoCookie`
- `generateSignedHttpRequest`
- `cancelInteractiveFlow`
- `getLinuxBrokerVersion`

The proxy passes JSON payloads through verbatim — no parsing or transformation.

## Container Session Bootstrap

The container's session bus only exists with an active user session. When `broker_proxy = true`, `intuneme start` bootstraps this automatically:

1. Boot container (existing)
2. Wait for container ready (existing)
3. `loginctl enable-linger <user>` inside container
4. Create login session + unlock gnome-keyring (pipe hardcoded password to `gnome-keyring-daemon --unlock`)
5. Wait for session bus socket at `<rootfs>/run/user/<uid>/bus`
6. Start `intuneme broker-proxy` as background process
7. Wait for proxy to claim name on host bus

The login session is kept alive by a backgrounded process inside the `machinectl shell` session. Container shutdown tears it down.

## Lifecycle

### `intuneme start` (broker_proxy = true)

After existing boot + wait: enable linger, create session, unlock keyring, start proxy. PID written to `~/.local/share/intuneme/broker-proxy.pid`.

### `intuneme stop` (broker_proxy = true)

Kill proxy (via PID file), remove PID file, then poweroff container. Proxy killed first so host apps get clean errors.

### `intuneme status` (broker_proxy = true)

Shows proxy state: `Broker proxy: running (PID 12345)` or `Broker proxy: not running`.

### D-Bus Activation

`~/.local/share/dbus-1/services/com.microsoft.identity.broker1.service` installed by `enable`, removed by `disable`. Allows on-demand proxy start even if `intuneme start` didn't pre-start it.

```ini
[D-BUS Service]
Name=com.microsoft.identity.broker1
Exec=/path/to/intuneme broker-proxy
```

## Error Handling

| Scenario | Behavior |
|---|---|
| Container not running, app calls broker | Proxy starts via D-Bus activation, fails to connect, returns D-Bus error |
| Container running, broker not ready | Proxy retries briefly, then returns error |
| Proxy crashes | D-Bus activation restarts on next call |
| `intuneme stop` during active call | Proxy killed, app gets disconnection error |

## Changes

### `internal/broker/proxy.go` (new)

D-Bus proxy logic using `godbus/dbus/v5`:
- Connect to host session bus and container session bus
- Claim `com.microsoft.identity.broker1` on host bus
- Export handler at `/com/microsoft/identity/broker1`
- Forward all method calls to container's broker, return responses

### `cmd/broker-proxy.go` (new)

`intuneme broker-proxy` subcommand — runs proxy in foreground.

### `cmd/config.go` (new or extended)

`intuneme config broker-proxy enable/disable` — toggle flag, manage D-Bus activation file.

### `internal/config/config.go`

Add `BrokerProxy bool` field to config struct.

### `cmd/start.go`

When `broker_proxy = true`: enable linger, create login session, unlock keyring, start proxy after container boot.

### `cmd/stop.go`

When `broker_proxy = true`: kill proxy before container poweroff.

### `cmd/status.go`

Show proxy state when `broker_proxy = true`.

### No Changes

- Container image / build — unchanged
- `intuneme init`, `intuneme shell`, `intuneme destroy` — unchanged
- All existing bind mounts and socket detection — unchanged
- Default behavior when `broker_proxy = false` — unchanged

## New Dependency

`github.com/godbus/dbus/v5`

## Out of Scope

- **Device broker proxying** — `com.microsoft.identity.devicebroker1` stays in the container on the system bus. Host apps don't need it.
- **Edge installation on host** — user installs Edge themselves.
- **Edge removal from container image** — still present, just unused in proxy mode.
- **Keyring migration** — container keyring stays in the container.
- **Multi-user support** — same single-user assumption as today.
- **TODO:** Discover session bus socket paths for non-GNOME desktop environments.
