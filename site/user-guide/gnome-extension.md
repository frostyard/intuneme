# GNOME Extension

The intuneme GNOME Shell extension adds a Quick Settings toggle for starting and stopping the container, along with buttons to launch Edge and Intune Portal — all without opening a terminal.

!!! note
    The extension is entirely optional. All intuneme functionality is available through the CLI, which works on any desktop environment.

## Requirements

- GNOME Shell 47 or later
- intuneme must be initialized (`intuneme init` completed)

## Install

```bash
intuneme extension install
```

Log out and back in to activate the extension. After logging back in, the intuneme toggle appears in the Quick Settings panel (the panel that opens from the top-right corner of the screen).

## What the extension provides

- **Quick Settings toggle** — Displays the current container state. Clicking it starts or stops the container.
- **Status details** — The popup menu shows whether the container is running and enrollment status.
- **App shortcuts** — Buttons to open a shell, launch Microsoft Edge, or launch Intune Portal directly from Quick Settings.

The extension monitors container state via D-Bus signals from `systemd-machined` for instant updates, with periodic polling as a fallback.

## Passwordless app launch

`intuneme init` installs a sudoers rule at `/etc/sudoers.d/intuneme-exec` that allows the host user to run `nsenter` into the container without a password prompt. This is what enables the extension (and [desktop shortcuts](desktop-shortcuts.md)) to launch Edge and Intune Portal without a terminal window appearing to ask for a sudo password.

The rule is narrowly scoped:

- It only permits the specific `nsenter` flag pattern used by `intuneme open`.
- It only allows `su` to the host user (not arbitrary users).
- It persists across `intuneme start` and `intuneme stop` cycles.
- It is removed when you run `intuneme destroy`.

!!! tip
    If the rule goes missing (for example, after upgrading from an older version), running `intuneme start` will reinstall it automatically.
