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

## Uninstall

The extension is removed when you run a full uninstall:

```bash
intuneme destroy --all
```

This also removes the polkit policy action that enables the extension's passwordless operations. See [Upgrading — Re-enrollment](upgrading.md#re-enrollment) for details on what `destroy --all` removes.

## What the extension provides

- **Quick Settings toggle** — Displays the current container state. Clicking it starts or stops the container.
- **Status details** — The popup menu shows whether the container is running and enrollment status.
- **App shortcuts** — Buttons to open a shell, launch Microsoft Edge, or launch Intune Portal directly from Quick Settings.

The extension monitors container state via D-Bus signals from `systemd-machined` for instant updates, with periodic polling as a fallback.

## Supported terminal emulators

The shell shortcut in the extension opens an interactive terminal inside the container. The extension checks `$TERMINAL` first, then tries the following terminals in order: **Ghostty**, **Ptyxis**, **GNOME Console (kgx)**, **GNOME Terminal**, and **xterm**.

Set the `TERMINAL` environment variable to override the default search order — for example, if you prefer a terminal that is not in the built-in list.

## Passwordless app launch

`intuneme init` installs a sudoers rule at `/etc/sudoers.d/intuneme-exec` that allows the host user to launch container apps without a password prompt. This is what enables the extension (and [desktop shortcuts](desktop-shortcuts.md)) to launch Edge and Intune Portal without a terminal window appearing to ask for a sudo password.

Instead of authorizing `nsenter` directly, the rule grants passwordless `sudo` for a single root-owned helper script at `/usr/local/libexec/intuneme/nsenter-exec`. The helper hard-codes the exact `nsenter`/`su` invocation and accepts only the container's process ID and the command to run. This is required because `sudo-rs` (the Rust `sudo`/`su` that ships by default on Ubuntu 25.10 and later) rejects wildcards inside command arguments, which the previous rule relied on and which caused an error on every `sudo` command.

The rule is narrowly scoped:

- It only permits the single fixed helper script; the nsenter flags are baked into the helper, not supplied by the caller.
- The helper only runs `su` to the host user (not arbitrary users), and is installed root-owned and not user-writable.
- It persists across `intuneme start` and `intuneme stop` cycles.
- It is removed (along with the helper) when you run `intuneme destroy`.

!!! tip
    If the rule or helper goes missing (for example, after upgrading from an older version that used the wildcard rule), running `intuneme start` will reinstall both automatically.

