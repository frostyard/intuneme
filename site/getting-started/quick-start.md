# Quick Start

This guide walks you from a fresh install to a running Intune enrollment in six steps.

## 1. Provision the container

```bash
intuneme init
```

This pulls the Ubuntu 24.04 container image, which already contains Microsoft Edge and Intune Portal, from GHCR, extracts the rootfs, creates a container user matching your host UID, and writes the configuration file. It also installs a polkit rule so you can open an interactive shell in the container without repeated password prompts; host shells and broad machine management are not granted.

If image preparation fails, `init` removes a new rootfs directory, or a previously empty rootfs directory owned by your user, so you can retry without `--force`. It accepts an empty directory owned by another user but never changes its mode before extraction or removes it on failure. A partial extraction needs sudo for cleanup. If cleanup fails, a warning shows the error. A pre-existing non-empty rootfs is never removed on init failure and still requires `--force` to extract over it. The rootfs directory mode is explicitly `0755` and its owner is `root:root`, even with a restrictive host umask.

See the [init command reference](../reference/cli/intuneme_init.md) for the full list of what `init` does.

!!! tip "Fedora / tmpfs systems"
    If `init` fails with "disk quota exceeded", your `/tmp` is likely a size-limited tmpfs. Use `--tmp-dir` to redirect temporary files to disk:
    ```bash
    intuneme init --tmp-dir /var/tmp
    ```
    See [Troubleshooting](../troubleshooting.md) for details.

!!! note "Insiders channel"
    To use the insiders (pre-release) container image, add the `--insiders` flag:
    ```bash
    intuneme init --insiders
    ```
    The insiders image may include newer versions of Edge or the Intune agent. The channel is saved in `config.toml`, so `intuneme recreate` keeps it without the flag.

## 2. Start the container

```bash
intuneme start
```

This boots the container (systemd as PID 1), mounts host display/audio/GPU resources into it, configures Nvidia GPU forwarding if detected, and installs udev rules for device hotplug (YubiKey, webcam). The container is ready when `intuneme status` shows it as running.

Every non-dry-run start also repairs the rootfs directory owner and mode for older installations, even when the container is already running. Start enables the container user's session services with a 30-second retry budget. A D-Bus attempt already in progress can run past this deadline (about 25 seconds per blocked call), for a worst-case wait of about 55 seconds. An app launch has a separate 30-second user-bus wait, which can end first in that degraded case. A final failure includes the command output.

## 3. Open a shell inside the container

```bash
intuneme shell
```

This drops you into an interactive login shell as the container user. From here you can run commands inside the container to check the state of services or troubleshoot enrollment.

## 4. Enroll in Intune

Inside the container shell (or directly from the host with `intuneme open portal`):

```bash
intune-portal
```

The Intune Portal window opens on your host display. Follow the on-screen steps to sign in with your corporate account and complete device enrollment.

## 5. Access corporate resources

Once enrolled, open Edge to access corporate web resources:

```bash
microsoft-edge
```

Or from the host without entering a shell first:

```bash
intuneme open edge
```

## 6. Stop the container

When you're done, shut down the container:

```bash
intuneme stop
```

This gracefully shuts down the container systemd, unmounts resources, and removes the udev hotplug rules. Enrollment state is preserved in `~/Intune/` and will be available the next time you run `intuneme start`.

## What's next

- [Daily Workflow](../user-guide/daily-workflow.md) — day-to-day start/stop and app launching
- [GNOME Extension](../user-guide/gnome-extension.md) — start/stop from Quick Settings without a terminal
- [Broker Proxy](../user-guide/broker-proxy.md) — enable SSO for host-side apps like VS Code
