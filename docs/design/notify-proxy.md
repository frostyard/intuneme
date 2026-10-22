# Notify Proxy

The notify proxy forwards desktop notifications from the container to the host. Apps in the container send them to `org.freedesktop.Notifications` on the container's session bus. The proxy owns that name there and passes each call to the notification server on the host session bus. The main client is Edge, for web notifications such as Outlook mail and calendar reminders.

## How It Works

```
Edge (container)  →  container session bus  →  intuneme notify-proxy  →  host session bus  →  host notification server
```

The proxy is a host process with one connection to each bus. Nothing is installed or run inside the container, and the host bus is not exposed to the container.

## D-Bus Interface

Desktop Notifications spec 1.2.

Bus name: `org.freedesktop.Notifications`
Object path: `/org/freedesktop/Notifications`
Interface: `org.freedesktop.Notifications`

**Methods** (served on the container bus, forwarded to the host server):

| Method | Proxy behaviour |
|---|---|
| `GetCapabilities() → as` | Forwarded. `body-images` is removed from the result. |
| `GetServerInformation() → ssss` | Forwarded unchanged. |
| `Notify(susssasa{sv}i) → u` | Icon paths rewritten (see below), then forwarded. The returned ID is remembered. |
| `CloseNotification(u)` | Forwarded unchanged. |

**Signals** (received on the host bus, re-emitted on the container bus):

| Signal | Use |
|---|---|
| `ActionInvoked(us)` | A click on the notification or one of its buttons |
| `ActivationToken(us)` | xdg-activation token, so the clicked app can take focus |
| `NotificationClosed(uu)` | The notification is gone. The proxy forgets the ID. |

Notification IDs are the host server's IDs, passed through unchanged. `replaces_id` and `CloseNotification` need no mapping. The proxy re-emits a signal only when its ID came from one of its own `Notify` calls, so signals for host apps' notifications stay on the host.

The host signal match uses the well-known sender name, so it follows a restarted host notification server.

Chromium uses this server only when `GetCapabilities` reports both `body` and `actions`. Otherwise it falls back to the notification portal. `body-images` is removed because with it Chromium puts `<img src="file:///tmp/...">` into the body, and the proxy does not rewrite body markup.

## Icon Paths

Chromium writes notification images into a new temp directory in `/tmp` and passes:

- `app_icon`: `file:///tmp/<dir>/logo.png`
- hints `image-path` and `image_path`: `/tmp/<dir>/icon.png`

The container's `/tmp` is a tmpfs in the container's mount namespace, so the rootfs path on the host does not show it. The host user can read it through `/proc/<pid>/root/tmp/...` for any of its processes inside the container.

The proxy gets that PID from `SO_PEERCRED` on its socket to the container bus. The kernel reports the listener's PID in the host PID namespace. The listener is the container user's `systemd --user` (the bus is socket-activated), which shares the mount namespace with the apps.

Rewrite rules (`internal/notify/paths.go`):

- An `app_icon` that is a `file://` URL or an absolute path is mapped to `/proc/<pid>/root<path>`, keeping the URL form. An icon theme name is unchanged.
- `image-path` and `image_path` values are mapped the same way.
- When the mapped path does not exist, `app_icon` becomes empty and an image hint is dropped. The notification is still shown.

## Setup Requirements

The proxy uses the same container session infrastructure as the [broker proxy](broker-proxy.md): the runtime directory bind-mounted as `/run/user/<uid>`, linger for the container user, and a login session. `start` sets these up when either proxy is enabled.

There is no D-Bus activation file. The name lives on the container bus, and a host service file cannot activate a process for it.

## Enabling/Disabling

```bash
intuneme config notify-proxy enable   # Sets notify_proxy = true
intuneme config notify-proxy disable  # Clears the flag, stops the proxy
```

A change takes effect on the next `intuneme start`.

## Process Management

- PID file: `~/.local/share/intuneme/notify-proxy.pid`
- Started by `start` with `setsid`, after the container session bus socket appears
- Startup verified by polling the PID file (up to 5 seconds)
- Stopped by `stop` and `destroy` before container poweroff
- `status` reports the proxy state
- Exits when either bus connection closes

## Operational Notes

- Chromium looks for `org.freedesktop.Notifications` once, at browser startup. If Edge starts before the proxy owns the name, Edge does not use the proxy until Edge restarts. `start` launches the proxy before it returns for this reason.
- To check the path end to end without Edge, call `Notify` on the container bus from the host:

  ```bash
  busctl --address=unix:path=$HOME/.local/share/intuneme/runtime/bus call \
    org.freedesktop.Notifications /org/freedesktop/Notifications \
    org.freedesktop.Notifications Notify 'susssasa{sv}i' \
    test 0 "" Summary Body 0 0 -1
  ```

- Run `intuneme notify-proxy` in a terminal to see its log.

## Key Implementation Files

| File | Purpose |
|------|---------|
| `internal/notify/proxy.go` | Forwarder, signal relay, D-Bus constants, introspection XML, `Run` |
| `internal/notify/paths.go` | Icon path rewrite into `/proc/<pid>/root` |
| `internal/busproxy/dial.go` | Container bus dial and `SO_PEERCRED` lookup, shared by the proxies |
| `cmd/notify_proxy.go` | CLI command (`intuneme notify-proxy`) that runs the proxy in the foreground |
| `cmd/config.go` | Enable/disable subcommands |
| `cmd/start.go` | `startProxy()`, shared with the broker proxy |

## References

- Chromium client: [`notification_platform_bridge_linux.cc`](https://chromium.googlesource.com/chromium/src/+/refs/heads/main/chrome/browser/notifications/notification_platform_bridge_linux.cc)
- [Desktop Notifications Specification](https://specifications.freedesktop.org/notification-spec/latest/)
