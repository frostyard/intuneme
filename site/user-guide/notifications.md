# Desktop Notifications

Apps inside the container cannot show desktop notifications on their own. The container has its own D-Bus session bus, and no notification server runs there. The notify proxy is an opt-in feature that forwards notifications from the container to your host desktop.

The main use is Outlook on the web in the container's Edge: new mail and calendar reminders show as normal host notifications.

## What it does

When enabled, intuneme starts a proxy on the host. It claims `org.freedesktop.Notifications` on the container's session bus and passes each notification to your host's notification server (GNOME Shell, KDE Plasma, mako, dunst, and so on).

Clicks come back too. A click on a notification opens the mail or event in Edge, and buttons on the notification work.

## Enable the proxy

```bash
intuneme config notify-proxy enable
intuneme stop && intuneme start
```

The restart is required so `intuneme start` can expose the container's session bus and start the proxy.

!!! important
    Edge looks for a notification server only once, when it starts. Start Edge after `intuneme start` has finished. If Edge was already running when the proxy started, close and reopen Edge.

After enabling, `intuneme status` includes a line showing whether the proxy is running.

## Outlook on the web

1. Open Outlook on the web in the container's Edge.
2. Allow notifications for `outlook.office.com` when Edge asks, or in Edge site settings.
3. In Outlook, open **Settings → General → Notifications** and turn on desktop notifications for mail and calendar.

Outlook on the web sends notifications only while its tab or window is open.

## Disable the proxy

```bash
intuneme config notify-proxy disable
intuneme stop && intuneme start
```

## Requirements

- Container must be running (`intuneme start`)
- A notification server on the host session bus (standard on any desktop)

!!! note
    Any app in the container can show notifications on the host while the proxy runs. Notification images are read from the container's `/tmp` through `/proc`, so they show with the right icon.
