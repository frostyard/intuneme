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

Open Intune Portal (for compliance checks and re-enrollment):

```bash
intuneme open portal
```

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

Shows whether the container is initialized, whether it is running, and (if the [broker proxy](broker-proxy.md) is enabled) the proxy state.

## Stop the container

```bash
intuneme stop
```

Shuts down the container and removes the udev hotplug rules. Enrollment state and browser profiles in `~/Intune/` are always preserved.

## Typical session

```bash
intuneme start          # boot the container
intuneme open edge      # use Edge for corporate resources
# ... do your work ...
intuneme stop           # shut down when done
```

!!! note
    The container does not need to be stopped between reboots — `intuneme start` is idempotent and re-installs any missing configuration on each boot.
