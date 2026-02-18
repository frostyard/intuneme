# GNOME Quick Settings Extension Design

## Overview

A GNOME Shell extension that adds an Intune container toggle to Quick Settings. The toggle shows active when the container is running, clicking it starts or stops the container, and a popup menu provides status details and a shell shortcut.

## Target

GNOME Shell 47+ only (Debian Trixie).

## Architecture: Pure CLI Wrapper

The extension is a thin UI layer that calls `intuneme` CLI commands via `Gio.Subprocess`. No changes to the Go CLI are needed for the core extension. A new `intuneme extension install` command embeds and deploys the extension files.

### Why not a D-Bus service or direct machinectl calls?

- D-Bus service: Major scope increase (persistent daemon, activation, new interface). Overkill for a toggle button.
- Direct machinectl: Duplicates logic — `intuneme start` does bind mounts, socket detection, broker proxy setup. Calling machinectl directly skips all of that.

## File Structure

```
gnome-extension/
  metadata.json          # UUID: intuneme@frostyard.org, shell-version: ["47"]
  extension.js           # Entry point: enable/disable, creates SystemIndicator
  quickToggle.js         # QuickMenuToggle subclass (UI + menu items)
  containerManager.js    # CLI subprocess calls + D-Bus signal watcher
  stylesheet.css         # Optional styling overrides
```

No GSettings schema or preferences UI. All configuration lives in `intuneme` config.

## UI: QuickMenuToggle

- **Icon:** `computer-symbolic`
- **Title:** "Intune"
- **Subtitle:** Dynamic — "Running", "Stopped", "Starting...", "Stopping..."
- **Toggle mode:** true (clicking toggles start/stop)
- **Checked state:** Bound to container running state

### Popup Menu

1. Header: "Intune Container" with icon
2. Status section (non-interactive labels):
   - Container state (Running / Stopped)
   - Broker proxy state (Running / Stopped / Not configured)
3. Separator
4. "Open Shell" action — opens a terminal window running `intuneme shell` (disabled when container is stopped)

## State Management

### Dual detection: D-Bus signals (primary) + polling (fallback)

**D-Bus signals** from `org.freedesktop.machine1` on the system bus:
- `MachineNew(name, path)` — container started
- `MachineRemoved(name, path)` — container stopped
- Filter for signals where `name` matches the configured machine name (default: "intuneme")
- Sub-second response time

**Polling** via `intuneme status` every 5 seconds:
- Safety net if D-Bus signals are missed
- Uses `Gio.Subprocess.communicate_utf8_async()` to avoid blocking
- Parses output for container state and broker proxy state

### State transitions

```
Stopped --[toggle on]--> Starting --[MachineNew signal]--> Running
Running --[toggle off]--> Stopping --[MachineRemoved signal]--> Stopped
```

- During "Starting", toggle is insensitive (not clickable) and subtitle shows "Starting..."
- During "Stopping", same treatment with "Stopping..."
- On error (start fails, pkexec cancelled): revert toggle, subtitle briefly shows "Error"

## Privilege Elevation

### `intuneme start` — needs pkexec

The extension runs `pkexec intuneme start` which triggers GNOME's native polkit authentication dialog.

Requires a polkit action file at `/usr/share/polkit-1/actions/org.frostyard.intuneme.policy`:
- Action ID: `org.frostyard.intuneme.start`
- Default: `auth_admin_keep` (caches authentication for a few minutes)
- Message: "Authentication is required to start the Intune container"

### `intuneme stop` — no elevation needed

The existing polkit rule at `/etc/polkit-1/rules.d/50-intuneme.rules` already grants sudo group members permission to run `machinectl poweroff` without a password.

### `intuneme shell` — no elevation needed

Same polkit rule covers `machinectl shell`.

## Terminal Detection for "Open Shell"

Detection order for launching `<terminal> -- intuneme shell`:
1. `$TERMINAL` environment variable
2. `ptyxis` (GNOME's newer terminal)
3. `kgx` (GNOME Console)
4. `gnome-terminal`
5. `xterm`

Checks if each binary exists on `$PATH` using `GLib.find_program_in_path()`.

## CLI: `intuneme extension install`

New command in `cmd/extension.go`:

1. Copies embedded extension files to `~/.local/share/gnome-shell/extensions/intuneme@frostyard.org/`
2. Installs polkit action file to `/usr/share/polkit-1/actions/org.frostyard.intuneme.policy` (via `sudo cp`)
3. Runs `gnome-extensions enable intuneme@frostyard.org`
4. Prints message: "Extension installed. Log out and back in to activate."

Extension files are embedded in the Go binary via `//go:embed gnome-extension/*`, so installation works without the source repo present.

## Error Handling

- pkexec cancelled by user: revert toggle state, no error shown
- `intuneme start` subprocess fails: revert toggle, log error, subtitle shows "Error" for 3 seconds
- `intuneme stop` fails: revert toggle, log error
- D-Bus signal subscription fails: fall back to polling only (log warning)
- Terminal not found for shell: log error, no action
- Polling subprocess fails: skip cycle, try again next interval

## Testing

Manual testing against a running GNOME Shell 47 session. The extension is pure JavaScript with no build step — edit, reload GNOME Shell (or log out/in), verify.
