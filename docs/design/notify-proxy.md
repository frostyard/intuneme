# Notify Proxy

The notify proxy forwards desktop notifications from the container to the host. Apps in the container send them to `org.freedesktop.Notifications` on the container's session bus. The proxy owns that name there and passes each call to the notification server on the host session bus. The main client is Edge, for web notifications such as Outlook mail and calendar reminders.

## How It Works

```
Edge (container)  →  container session bus  →  intuneme notify-proxy  →  host session bus  →  host notification server
```

The proxy is a host process with one connection to each bus. Nothing is installed or run inside the container, and the host bus is not exposed to the container.

## Trust Model

The container is the untrusted side. The proxy runs on the host as the host user, and a process in the container controls every argument of every call it sends. So the proxy:

- passes only allowed hints, with the expected types (see below),
- never gives the host server a path inside the container. It copies icon files into a host directory it owns, after safe checks,
- relays, replaces and closes only notifications it created itself. The container cannot see or touch the notifications of host apps,
- keeps bounded state and rate limits `Notify`.

The container can still show any text as a host notification while the proxy runs. That is the feature.

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
| `Notify(susssasa{sv}i) → u` | Rate limited. Hints filtered and icons copied (see below), then forwarded. A `replaces_id` the proxy did not create becomes `0`. The returned ID is remembered. |
| `CloseNotification(u)` | Forwarded for an ID the proxy created. Ignored, with success, for any other ID. |

**Signals** (received on the host bus, re-emitted on the container bus):

| Signal | Use |
|---|---|
| `ActionInvoked(us)` | A click on the notification or one of its buttons |
| `ActivationToken(us)` | xdg-activation token, so the clicked app can take focus |
| `NotificationClosed(uu)` | The notification is gone. The proxy forgets the ID. |

Notification IDs are the host server's IDs, passed through unchanged, so `replaces_id` and `CloseNotification` need no mapping table. The proxy re-emits a signal only when its ID came from one of its own `Notify` calls, so signals for host apps' notifications stay on the host.

The host server can emit a signal for a new ID before the proxy has read the `Notify` reply. A signal for an unknown ID is therefore buffered for up to 2 seconds (at most 32 signals). When `Notify` returns that ID, the buffered signals are emitted in order. Signals for host apps' notifications expire from the buffer.

The host signal match uses the well-known sender name, so it follows a restarted host notification server.

Chromium uses this server only when `GetCapabilities` reports both `body` and `actions`. Otherwise it falls back to the notification portal. `body-images` is removed because with it Chromium puts `<img src="file:///tmp/...">` into the body, and the proxy does not rewrite body markup.

## Hints

Only these hints are passed, and only with the listed type. All other hints are dropped.

| Hint | Type |
|---|---|
| `urgency` | `y` |
| `category` | `s` |
| `transient`, `resident`, `suppress-sound`, `action-icons` | `b` |
| `image-path`, `image_path` | `s`, then mapped to an icon copy |

Dropped hints include `desktop-entry`, `sound-file`, `sound-name`, `image-data` and all `x-*` hints. `sound-file` and `desktop-entry` would name host files or host apps chosen by the container. Chromium sends `urgency`, `suppress-sound`, `desktop-entry`, both image path hints and `x-kde-reply-placeholder-text`.

## Icons

Chromium writes notification images into a new temp directory in `/tmp` and passes:

- `app_icon`: `file:///tmp/<dir>/logo.png`
- hints `image-path` and `image_path`: `/tmp/<dir>/icon.png`

The container's `/tmp` is a tmpfs in the container's mount namespace, so the rootfs path on the host does not show it. The host user can reach it through `/proc/<pid>/root/tmp/...` for any of its processes inside the container.

The proxy gets that PID from `SO_PEERCRED` on its socket to the container bus. The kernel reports the listener's PID in the host PID namespace. The listener is the container user's `systemd --user` (the bus is socket-activated), which shares the mount namespace with the apps.

The proxy never passes a `/proc/<pid>/root` path to the host server. The container controls that tree, and an absolute symlink in it resolves against the host root when a host process opens it. So a container process could make `/tmp/x` point at a host file, a FIFO or `/dev/zero`. Instead, `internal/notify/paths.go`:

1. Strips `file://`. A value without a leading `/` is an icon theme name and passes unchanged.
2. Accepts only a clean path under `/tmp/`.
3. Opens `/proc/<pid>/root` as an `O_PATH` directory, then the file with `openat2`, `RESOLVE_IN_ROOT|RESOLVE_NO_MAGICLINKS` and `O_NONBLOCK`. Symlinks resolve inside the container root, and a FIFO does not block the open.
4. Requires a regular file of at most 1 MiB, checked with `fstat` on the open file.
5. Copies the bytes to `$XDG_RUNTIME_DIR/intuneme-notify/` (mode `0700`, files `0600`, random names) and passes the copy's path, in the same URL or plain form.

When a check fails, `app_icon` becomes empty and an image hint is dropped. The notification is still shown. A file named by several arguments is copied once.

The copies are removed on `NotificationClosed`, when a new notification replaces the ID, when the ID is evicted, and when the proxy exits. The directory is emptied at proxy start.

## Limits

- `Notify`: a token bucket with a burst of 10 and a refill of 1 per second. Over the limit, `Notify` returns a D-Bus error and nothing reaches the host.
- Owned IDs: at most 256. When the table is full, the oldest ID is forgotten and its icon copies removed. This bounds the state if the host server never sends `NotificationClosed`.
- Early signals: at most 32, each for at most 2 seconds.

## Setup Requirements

The proxy uses the same container session infrastructure as the [broker proxy](broker-proxy.md): the runtime directory bind-mounted as `/run/user/<uid>` and a login session. `start` always enables linger for the container user, with or without proxies; it sets up the runtime directory bind mount and the login session when a proxy is enabled.

There is no D-Bus activation file. The name lives on the container bus, and a host service file cannot activate a process for it.

## Enabling/Disabling

```bash
intuneme config notify-proxy enable   # Sets notify_proxy = true
intuneme config notify-proxy disable  # Clears the flag, stops the proxy
```

A change takes effect on the next `intuneme start`. For a running container, that means `intuneme stop` and then `intuneme start`: the runtime directory bind mount that exposes the container bus is set up at boot.

## Process Management

- PID file: `~/.local/share/intuneme/notify-proxy.pid`
- Started by `start` with `setsid`, after the container session bus socket appears
- The PID file is written only after the proxy owns the bus name, so `start` polling it (up to 5 seconds) waits for a working proxy
- A second instance refuses to start when the PID file names a live `notify-proxy` process. On exit, a proxy removes the PID file only if it holds its own PID
- Stopped by `stop` and `destroy` before container poweroff
- `status` reports the proxy state
- Exits when either bus connection closes. When the host session bus closes, for example at logout, notifications stop until the next `intuneme start`

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
| `internal/notify/paths.go` | Safe icon copy from the container's `/tmp` |
| `internal/notify/limits.go` | Hint allowlist and `Notify` rate limit |
| `internal/notify/ids.go` | Owned ID table and early signal buffer |
| `internal/busproxy/pidfile.go` | PID file rules shared by the container bus proxies |
| `cmd/proxy_run.go` | Foreground runner shared by the container bus proxies |
| `internal/busproxy/dial.go` | Container bus dial and `SO_PEERCRED` lookup, shared by the proxies |
| `cmd/notify_proxy.go` | CLI command (`intuneme notify-proxy`) that runs the proxy in the foreground |
| `cmd/config.go` | Enable/disable subcommands |
| `cmd/start.go` | `startProxy()`, shared with the broker proxy |

## References

- Chromium client: [`notification_platform_bridge_linux.cc`](https://chromium.googlesource.com/chromium/src/+/refs/heads/main/chrome/browser/notifications/notification_platform_bridge_linux.cc)
- [Desktop Notifications Specification](https://specifications.freedesktop.org/notification-spec/latest/)
