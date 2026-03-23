# Desktop Shortcuts

intuneme can install `.desktop` entries for Microsoft Edge and Intune Portal so they appear in the GNOME Activities overview (or any XDG-compliant application launcher).

!!! note
    The shortcuts are optional. You can always launch apps with `intuneme open edge` and `intuneme open portal` from a terminal. The shortcuts are a convenience for users who prefer not to open a terminal for routine app launches.

## Install shortcuts

```bash
bash scripts/install-desktop-items.sh
```

After running this, Edge and Intune Portal will appear in the Activities overview. Clicking either entry runs `intuneme open edge` or `intuneme open portal`.

!!! warning
    The container must already be running (`intuneme start`) before clicking a shortcut. The shortcuts do not start the container automatically. Consider using the [GNOME extension](gnome-extension.md) for a start/stop toggle alongside the shortcuts.

## Remove shortcuts

```bash
bash scripts/install-desktop-items.sh --uninstall
```

This removes the `.desktop` entries from the application grid.

## How passwordless launch works

Clicking a shortcut runs `intuneme open`, which uses `sudo nsenter` to enter the container's namespaces. The sudoers rule installed by `intuneme init` (`/etc/sudoers.d/intuneme-exec`) makes this passwordless, so no terminal window appears to prompt for authentication. See [GNOME Extension — Passwordless app launch](gnome-extension.md#passwordless-app-launch) for details on the sudoers rule.
