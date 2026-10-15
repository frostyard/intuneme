# Quick Start

This guide walks you from a fresh install to a running Intune enrollment in six steps.

## 1. Provision the container

```bash
intuneme init
```

This pulls the Ubuntu 24.04 Microsoft container image from GHCR, extracts the rootfs, installs Microsoft Edge and Intune Portal, creates a container user matching your host UID, and writes the configuration file. It also installs a polkit rule so you can use `machinectl` without repeated password prompts. The alternative Himmelblau images do not include Edge or Intune Portal.

During setup, intuneme asks you to set a password for interactive container login. It accepts any non-empty password. A strong password is recommended because it protects interactive access to the container.

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
    The insiders image may include newer versions of Edge or the Intune agent. Use the same flag with `intuneme recreate` if you ever upgrade.

!!! warning "Experimental Himmelblau alternative"
    `intuneme init --himmelblau` selects a separate image that replaces Microsoft's broker and Intune Portal with Himmelblau's PAM, broker, and compliance stack. It prompts for your Entra sign-in domain; use `--himmelblau-domain` for automated setup. Use `intuneme edit --himmelblau` during setup or later to edit the commented package config. It is not upstream-validated under systemd-nspawn and tracks Community Nightly packages. Review the [Himmelblau guide](../user-guide/himmelblau.md) before using it.

## 2. Start the container

```bash
intuneme start
```

This boots the container (systemd as PID 1), mounts host display/audio/GPU resources into it, configures Nvidia GPU forwarding if detected, and installs udev rules for device hotplug (YubiKey, webcam). The container is ready when `intuneme status` shows it as running.

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
