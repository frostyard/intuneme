# Broker Proxy (Host-Side SSO)

By default, the Microsoft Identity Broker only runs inside the container. The broker proxy is an opt-in feature that forwards broker calls from host applications to the container, enabling single sign-on (SSO) for apps like Microsoft Edge and VS Code running natively on the host.

## What it does

When enabled, intuneme starts a D-Bus forwarding proxy on the host session bus that claims `com.microsoft.identity.broker1` and relays all calls to the broker service inside the container. Host applications using MSAL (such as Edge, VS Code, and Teams) will transparently use the container's enrollment for SSO and conditional access — no changes are needed on the application side.

!!! note
    The container must be running and the user must be enrolled in Intune before the proxy is useful. The proxy itself starts and stops automatically with the container.

## How it works

1. `intuneme start` bind-mounts `~/.local/share/intuneme/runtime/` into the container as `/run/user/<uid>`, making the container's session bus socket visible on the host at `runtime/bus`.
2. A D-Bus activation file is installed at `~/.local/share/dbus-1/services/` so the proxy starts on demand when a host app first calls the broker interface.
3. The proxy process forwards all broker method calls over the exposed socket and returns the container's responses to the calling host app.

## Enable the proxy

```bash
intuneme config broker-proxy enable
intuneme stop && intuneme start
```

The restart is required so `intuneme start` can set up the bind mount and create a login session inside the container (needed for gnome-keyring and the user broker service).

After enabling, `intuneme status` will include a line showing whether the proxy is running.

## Disable the proxy

```bash
intuneme config broker-proxy disable
intuneme stop && intuneme start
```

This removes the D-Bus activation file and stops the proxy. The bind mount is also removed on the next start.

## Requirements

- Container must be running (`intuneme start`)
- User must be enrolled in Intune (run `intune-portal` from `intuneme shell` if not yet enrolled)
- Host session bus must be available (standard on any GNOME or KDE desktop)

!!! warning
    Enabling the proxy means host applications can access your corporate SSO tokens via the container's broker. Only enable this if you intend to use Microsoft host apps with your Intune identity.
