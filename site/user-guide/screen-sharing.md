# Screen Sharing

Edge in the container can share only browser tabs on its own. On a Wayland desktop, a browser shares screens and windows through the desktop's screen sharing portal. The container has its own portal, and that one cannot capture the host desktop. The screencast proxy is an opt-in feature that connects Edge in the container to your host's screen sharing portal.

The main use is sharing your screen or a window in a Teams meeting in the container's Edge.

## What it does

When enabled, intuneme starts a proxy on the host. It takes over the portal name on the container's session bus and passes screen sharing requests to your host's portal. Your desktop shows its normal screen or window picker. The picked screen or window is streamed to Edge through PipeWire.

## Enable the proxy

```bash
intuneme config screencast-proxy enable
intuneme stop && intuneme start
```

If the container is already running, the stop and start are required. `intuneme start` exposes the container's session bus at boot and then starts the proxy.

!!! important
    Start Edge after `intuneme start` has finished. If Edge was already running when the proxy started, close and reopen Edge.

After enabling, `intuneme status` includes a line showing whether the proxy is running.

## Share your screen

1. Join a meeting in Teams in the container's Edge.
2. Select **Share** and pick a screen or window. The browser asks first, then your desktop shows its own picker.
3. Pick the screen or window in the desktop picker.

Your desktop asks every time. The proxy does not let the container save the choice.

## Disable the proxy

```bash
intuneme config screencast-proxy disable
intuneme stop && intuneme start
```

## Requirements

- Container must be running (`intuneme start`)
- A host portal with screen sharing (`xdg-desktop-portal` with a GNOME, KDE, wlroots or Hyprland backend). Check with:

  ```bash
  busctl --user introspect org.freedesktop.portal.Desktop /org/freedesktop/portal/desktop | grep ScreenCast
  ```

!!! note
    While the proxy runs, the container's own portal is replaced. Apps in the container can then use only screen sharing through the portal. Edge uses its own file dialog for uploads and downloads instead. The proxy stops when your host session ends, for example at logout. Screen sharing then stops working until the next `intuneme start`.
