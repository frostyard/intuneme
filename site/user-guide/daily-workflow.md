# Daily Workflow

This page covers the day-to-day commands for working with intuneme once you have completed the [Quick Start](../getting-started/quick-start.md).

## Start the container

```bash
intuneme start
```

This boots the container, installs udev hotplug rules for USB devices, and configures [Nvidia GPU forwarding](nvidia-gpu.md) if a GPU is detected. The container runs a full systemd instance — it takes a few seconds to be ready.

## Launch apps

Open Microsoft Edge inside the container:

```bash
intuneme open edge
```

Pass arguments to the app after `--`. For example, open Outlook on the web as a standalone app window:

```bash
intuneme open edge -- --app=https://outlook.office.com
```

Open Intune Portal (for compliance checks and re-enrollment):

```bash
intuneme open portal
```

## Open Edge web apps

Sites you installed as Edge apps ("Apps > Install this site as an app" in Edge inside the container) are launched with their Edge ID:

```bash
intuneme open app --list                   # ID and name of each installed web app
intuneme open app msedge-ahjdipcglkflohmjbcjmjpidpbbiocpk-Default
```

The IDs come from Edge, not from intuneme. Use `--list --json` for machine-readable output; the [GNOME extension](gnome-extension.md) uses it to fill its Apps submenu.

The launch runs the container's `microsoft-edge` wrapper with `--profile-directory` and `--app-id`, the same way `intuneme open edge` starts Edge, so the container's flags apply. intuneme never runs the desktop file's `Exec` line (`gtk-launch`), which would start Edge without them.

!!! note
    intuneme reads the desktop files in `~/Intune/.local/share/applications/`, the host side of the container's home directory, so `--list` works with the container stopped.

!!! tip
    If you have the [GNOME extension](gnome-extension.md) or [desktop shortcuts](desktop-shortcuts.md) installed, you can launch these apps directly from the Activities overview without opening a terminal.

## Get a shell

```bash
intuneme shell
```

Opens an interactive login shell inside the container as the container user. This gives you a full D-Bus session with gnome-keyring, so you can run enrollment commands (`intune-portal`) or manage YubiKeys (`ykman`).

## Check status

```bash
intuneme status
```

Shows whether the container is initialized, whether it is running, and the state of the [broker proxy](broker-proxy.md), the [notify proxy](notifications.md) and the [screencast proxy](screen-sharing.md) when they are enabled.

## Stop the container

```bash
intuneme stop
```

Shuts down the container and removes the udev hotplug rules. Enrollment state and browser profiles in `~/Intune/` are always preserved.

## Typical session

```bash
intuneme start            # boot the container
intuneme open edge        # use Edge for corporate resources
intuneme open app --list  # see the Edge web apps you can launch
# ... do your work ...
intuneme stop             # shut down when done
```

!!! note
    The container does not need to be stopped between reboots — `intuneme start` is idempotent and re-installs any missing configuration on each boot.
