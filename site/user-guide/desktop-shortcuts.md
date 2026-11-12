# Desktop Shortcuts

intuneme can install `.desktop` entries for Microsoft Edge and Intune Portal so they appear in the GNOME Activities overview (or any XDG-compliant application launcher).

!!! note
    The shortcuts are optional. In the Microsoft image, launch apps with `intuneme open edge` and `intuneme open portal` from a terminal. In Himmelblau mode, use `intuneme enroll --user <UPN>` for identity sign-in and enrollment.

## Install shortcuts

```bash
bash scripts/install-desktop-items.sh
```

After running this with the default Microsoft image, Edge and Intune Portal
shortcuts appear. With the experimental Himmelblau image, only the identity
sign-in shortcut appears; it opens a terminal for `intuneme enroll`, prompts
for an Entra UPN, and performs explicit PAM authentication before opening a
container shell.

!!! warning
    The container must already be running (`intuneme start`) before clicking a shortcut. The shortcuts do not start the container automatically. Consider using the [GNOME extension](gnome-extension.md) for a start/stop toggle alongside the shortcuts.

## Remove shortcuts

```bash
bash scripts/install-desktop-items.sh --uninstall
```

This removes the `.desktop` entries from the application grid.

## How passwordless launch works

Clicking a shortcut runs `intuneme open`, which uses `sudo nsenter` to enter the container's namespaces. The sudoers rule installed by `intuneme init` (`/etc/sudoers.d/intuneme-exec`) makes this passwordless, so no terminal window appears to prompt for authentication. See [GNOME Extension — Passwordless app launch](gnome-extension.md#passwordless-app-launch) for details on the sudoers rule.
