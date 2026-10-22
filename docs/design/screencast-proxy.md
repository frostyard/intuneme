# Screencast Proxy

The screencast proxy lets apps in the container share host screens and windows. The main client is Edge, for screen sharing in Teams and other WebRTC apps. The proxy serves the ScreenCast portal on the container's session bus and forwards it to the xdg-desktop-portal on the host session bus.

## Why It Is Needed

- Edge runs as a native Wayland client with the WebRTC PipeWire capturer (`ubuntu-intune/system_files/usr/local/bin/microsoft-edge`). On Wayland, it can capture screens and windows only through `org.freedesktop.portal.ScreenCast` on its session bus.
- The container session bus has its own xdg-desktop-portal with only the `gtk` backend. That backend has no ScreenCast, so Edge can share only tabs.
- WebRTC uses the portal only when `XDG_SESSION_TYPE` starts with `wayland` and `WAYLAND_DISPLAY` is set (`IsRunningUnderWayland()` in `desktop_capturer.cc`). Otherwise it uses X11 capture, which finds nothing because the Edge wrapper unsets `DISPLAY`. The app launch script (`buildSessionEnvScript()` in `internal/nspawn/nspawn.go`) exports `XDG_SESSION_TYPE=wayland` when the host Wayland socket is bound. `intuneme-session-setup` exports it too, for login shells. The launch script runs `intuneme-session-setup` as a child process, so the launch script must set the variable itself.
- The host portal has a ScreenCast backend (for example GNOME, KDE, wlroots or Hyprland). The host PipeWire socket is already bind-mounted into the container, and the container has `libpipewire-0.3`.

## How It Works

```
Edge (container)  →  container session bus  →  intuneme screencast-proxy  →  host session bus  →  host xdg-desktop-portal
```

The proxy is a host process with one connection to each bus. Nothing is installed or run inside the container, and the host bus is not exposed to the container.

### Name Takeover

Chromium uses the fixed name `org.freedesktop.portal.Desktop` (`kDesktopBusName` in WebRTC). On the container bus, the container's own xdg-desktop-portal owns that name. Only one process can own it.

The container portal requests the name with `ALLOW_REPLACEMENT`. It quits when it loses the name or is put in the queue: `on_name_lost` calls `g_main_loop_quit`, and GLib calls the lost handler for an `IN_QUEUE` reply too. So it cannot run under another name.

The proxy requests the name with `ReplaceExisting|DoNotQueue` and without `AllowReplacement`:

- A running container portal loses the name and quits.
- While the proxy owns the name, the container bus does not start a new container portal (D-Bus activation runs only for an unowned name). A portal started by hand is put in the queue and quits.
- When the proxy exits, the name is free. The next portal call starts the container portal again, as before.
- If an owner that does not allow replacement holds the name, the proxy fails to start.
- If the proxy loses the name, it exits.

No container image change and no `recreate` are needed.

### What Is Missing While It Runs

The proxy serves only ScreenCast. All other portal interfaces of the container portal are missing while it runs, for example FileChooser, OpenURI, Inhibit and Print. Calls to them fail with an error. Chromium checks for the FileChooser portal at startup, and when it is missing, Chromium uses its own GTK file dialog, which shows the container's files.

## D-Bus Interface

Object path `/org/freedesktop/portal/desktop`:

| Interface | Proxy behaviour |
|---|---|
| `org.freedesktop.portal.ScreenCast` | Methods forwarded to the host portal (see below) |
| `org.freedesktop.DBus.Properties` | `Get` and `GetAll` for ScreenCast return the host portal's `AvailableSourceTypes`, `AvailableCursorModes` and `version`. Any other interface is an error. |
| `org.freedesktop.DBus.Introspectable` | Lists only the interfaces above |

Chromium creates its proxy with `G_DBUS_PROXY_FLAGS_NONE`, so it reads the properties with `GetAll`.

### Methods and Options

| Method | Options passed to the host |
|---|---|
| `CreateSession(a{sv}) → o` | `handle_token` and `session_handle_token`, both replaced by proxy tokens |
| `SelectSources(oa{sv}) → o` | `handle_token` (replaced), `types` (`u`), `multiple` (`b`), `cursor_mode` (`u`) |
| `Start(osa{sv}) → o` | `handle_token` (replaced). `parent_window` is always `""`, because a container window handle means nothing to the host portal. |
| `OpenPipeWireRemote(oa{sv}) → h` | none |

Every option that is not listed is dropped. This includes `persist_mode` and `restore_token`, so every capture shows the host picker and needs the user's confirmation.

### Object Paths

The portal builds Request and Session paths from the caller's unique name:

```
/org/freedesktop/portal/desktop/request/<sender>/<token>
/org/freedesktop/portal/desktop/session/<sender>/<token>
```

`<sender>` is the unique name without `:` and with `.` replaced by `_`. Chromium computes the request path itself and subscribes to `Response` there before the call (`PrepareSignalHandle` in WebRTC `xdg_desktop_portal_utils.cc`).

On the host, the caller of every request is the proxy, so the two buses see different paths:

- **Container path:** built from the container caller's unique name and the caller's token. A token must match `[A-Za-z0-9_]{1,64}`. A missing token gets a generated one.
- **Host path:** the proxy sends its own tokens (`intuneme_<n>`) to the host and builds the path from its own host unique name.

The proxy stores the mapping before it calls the host. A host `Response` that arrives before the call returns is therefore not lost. If the host returns a different request path than the computed one, the mapping moves to the returned path.

### Signals

| Host signal | Proxy behaviour |
|---|---|
| `Request.Response(ua{sv})` on a mapped request path | Emitted on the container request path. The mapping is removed. |
| `Session.Closed(a{sv})` on a mapped session path | Emitted on the container session path. The session is removed. |

Results are rebuilt, not copied:

- `CreateSession`: only `session_handle`, set to the container session path.
- `SelectSources`: empty.
- `Start`: only `streams`. Each stream keeps its PipeWire node ID and the spec properties `source_type`, `id`, `mapping_id`, `position` and `size`. godbus decodes the `a(ua{sv})` structs into `[]any` and cannot encode those as structs again, so the proxy converts them back into typed Go structs.

The host match rules use sender `org.freedesktop.portal.Desktop` and a path namespace under the proxy's own host unique name.

Both signals are sent to the container caller as unicast signals, with the caller's unique name as destination, the way the real portal sends them. WebRTC subscribes to `Response` with `G_DBUS_SIGNAL_FLAGS_NO_MATCH_RULE`, so Edge adds no match rule for it, and the bus does not deliver a broadcast `Response` to Edge. A broadcast reply leaves Edge waiting after `CreateSession`: it never calls `SelectSources`, and the session stays open. The tests use a client without match rules for this reason.

### Container Objects

- For each pending request, a `Request` object at the container path. `Close` forwards to the host request.
- For each open session, a `Session` object at the container path, exported once `CreateSession` succeeds. `Close` forwards to the host session.

## Trust Model

The container is the untrusted side. The proxy runs on the host as the host user, and a process in the container controls every argument of every call. So the proxy:

- passes only the listed options and result keys,
- never passes a container window handle or a restore token to the host, so every capture needs the host picker,
- lets only the container caller that created a session use or close it,
- allows at most 4 open sessions at a time.

## PipeWire FD

`OpenPipeWireRemote` returns a host fd to the PipeWire daemon. The host portal limits that connection to the session's streams. The proxy returns the fd as `dbus.UnixFD`, and godbus passes it to the container caller with `SCM_RIGHTS`. Both bus connections negotiate unix fd passing during authentication.

godbus does not close a sent fd. The proxy keeps its copy with the session and closes it when the session ends.

## Cleanup

- When a container caller leaves the bus (`NameOwnerChanged` with an empty new owner), the proxy closes its host sessions. The host portal cannot see this, because on the host every session belongs to the proxy.
- When the proxy stops, it closes all host sessions.

## Setup Requirements

The proxy uses the same container session infrastructure as the [broker proxy](broker-proxy.md) and the [notify proxy](notify-proxy.md): the runtime directory bind-mounted as `/run/user/<uid>`, linger for the container user, and a login session. `start` sets these up when any of the three proxies is enabled.

There is no D-Bus activation file. The name lives on the container bus, and a host service file cannot activate a process for it.

## Enabling/Disabling

```bash
intuneme config screencast-proxy enable   # Sets screencast_proxy = true
intuneme config screencast-proxy disable  # Clears the flag, stops the proxy
```

A change takes effect on the next `intuneme start`. For a running container, that means `intuneme stop` and then `intuneme start`.

## Process Management

- PID file: `~/.local/share/intuneme/screencast-proxy.pid`
- Started by `start` with `setsid`, after the container session bus socket appears
- The PID file is written only after the proxy owns the bus name. A second instance refuses to start while the PID file names a live `screencast-proxy` process. Both rules come from `runContainerBusProxy()` in `cmd/proxy_run.go`, shared with the notify proxy
- Stopped by `stop` and `destroy` before container poweroff
- `status` reports the proxy state
- Exits when either bus connection closes, or when it loses the portal name

## Operational Notes

- If Edge offers only tabs and the proxy sees no calls, check that Edge has `XDG_SESSION_TYPE=wayland`. An Edge started before the session script exported it must be restarted.
- Start Edge after `intuneme start`. Chromium checks the portal at startup. An Edge that started while the container portal was running can try its FileChooser, which is now missing, until Edge restarts.
- The host portal needs a ScreenCast backend. Check with:

  ```bash
  busctl --user introspect org.freedesktop.portal.Desktop /org/freedesktop/portal/desktop | grep ScreenCast
  ```

- To check the container side, list the interfaces the proxy serves:

  ```bash
  busctl --address=unix:path=$HOME/.local/share/intuneme/runtime/bus introspect \
    org.freedesktop.portal.Desktop /org/freedesktop/portal/desktop
  ```

- Run `intuneme screencast-proxy` in a terminal to see its log, after `intuneme config screencast-proxy disable` or `stop` of the background one.

## Key Implementation Files

| File | Purpose |
|------|---------|
| `internal/screencast/proxy.go` | Forwarder, path mapping, signal relay, name takeover, `Run` |
| `internal/busproxy/dial.go` | Container bus dial, shared by the proxies |
| `cmd/screencast_proxy.go` | CLI command (`intuneme screencast-proxy`) that runs the proxy in the foreground |
| `cmd/proxy_run.go` | Foreground runner with the PID file rules |
| `cmd/config.go` | Enable/disable subcommands |
| `cmd/start.go` | `startProxy()`, shared with the other proxies |

## References

- [ScreenCast portal](https://flatpak.github.io/xdg-desktop-portal/docs/doc-org.freedesktop.portal.ScreenCast.html), [Request](https://flatpak.github.io/xdg-desktop-portal/docs/doc-org.freedesktop.portal.Request.html) and [Session](https://flatpak.github.io/xdg-desktop-portal/docs/doc-org.freedesktop.portal.Session.html) interfaces
- WebRTC Wayland detection: [`desktop_capturer.cc`](https://webrtc.googlesource.com/src/+/refs/heads/main/modules/desktop_capture/desktop_capturer.cc)
- WebRTC client: [`screencast_portal.cc`](https://webrtc.googlesource.com/src/+/refs/heads/main/modules/desktop_capture/linux/wayland/screencast_portal.cc) and [`xdg_desktop_portal_utils.cc`](https://webrtc.googlesource.com/src/+/refs/heads/main/modules/portal/xdg_desktop_portal_utils.cc)
- Container portal name handling: [`xdg-desktop-portal.c` 1.18.4](https://github.com/flatpak/xdg-desktop-portal/blob/1.18.4/src/xdg-desktop-portal.c) and [GLib `gdbusnameowning.c`](https://gitlab.gnome.org/GNOME/glib/-/blob/2.80.0/gio/gdbusnameowning.c)
