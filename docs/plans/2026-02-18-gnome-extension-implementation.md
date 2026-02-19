# GNOME Quick Settings Extension Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Add a GNOME Shell Quick Settings toggle for managing the intuneme container, plus a CLI command to install the extension.

**Architecture:** Pure CLI wrapper — the GNOME extension calls `intuneme` CLI commands via `Gio.Subprocess`. D-Bus signals from `org.freedesktop.machine1` provide instant state updates, with periodic polling as fallback. A new `intuneme extension install` command embeds the extension files in the Go binary and deploys them.

**Tech Stack:** GJS (GNOME JavaScript), GNOME Shell 47 QuickSettings API, Go `embed` package, polkit

---

### Task 1: Create extension metadata and entry point

**Files:**
- Create: `gnome-extension/metadata.json`
- Create: `gnome-extension/extension.js`
- Create: `gnome-extension/stylesheet.css`

**Step 1: Create metadata.json**

```json
{
    "uuid": "intuneme@frostyard.org",
    "name": "Intuneme",
    "description": "Manage the Intune container from Quick Settings",
    "shell-version": ["47"],
    "version": 1,
    "url": "https://github.com/frostyard/intuneme"
}
```

**Step 2: Create stylesheet.css (empty placeholder)**

```css
/* Intuneme extension styles */
```

**Step 3: Create extension.js skeleton**

This is the entry point. It creates the `SystemIndicator` and registers it with the Quick Settings panel.

```javascript
import GObject from 'gi://GObject';
import * as Main from 'resource:///org/gnome/shell/ui/main.js';
import * as QuickSettings from 'resource:///org/gnome/shell/ui/quickSettings.js';

import {Extension} from 'resource:///org/gnome/shell/extensions/extension.js';
import {IntuneToggle} from './quickToggle.js';
import {ContainerManager} from './containerManager.js';

const IntuneIndicator = GObject.registerClass(
class IntuneIndicator extends QuickSettings.SystemIndicator {
    _init(extensionObject) {
        super._init();

        this._manager = new ContainerManager();
        this._toggle = new IntuneToggle(this._manager);
        this.quickSettingsItems.push(this._toggle);
    }

    destroy() {
        this._manager.destroy();
        this.quickSettingsItems.forEach(item => item.destroy());
        super.destroy();
    }
});

export default class IntuneExtension extends Extension {
    enable() {
        this._indicator = new IntuneIndicator(this);
        Main.panel.statusArea.quickSettings.addExternalIndicator(this._indicator);
    }

    disable() {
        this._indicator.destroy();
        this._indicator = null;
    }
}
```

**Step 4: Commit**

```bash
git add gnome-extension/metadata.json gnome-extension/extension.js gnome-extension/stylesheet.css
git commit -m "feat(extension): add GNOME extension skeleton with metadata and entry point"
```

---

### Task 2: Create containerManager.js — subprocess helpers and D-Bus watcher

**Files:**
- Create: `gnome-extension/containerManager.js`

**Step 1: Create containerManager.js**

This module provides:
- `execCommand(argv)` — async helper that runs a subprocess and returns stdout
- D-Bus signal subscription to `org.freedesktop.machine1` for `MachineNew`/`MachineRemoved`
- Periodic polling of `intuneme status` every 5 seconds
- GObject properties: `containerRunning` (bool), `brokerRunning` (bool)
- Methods: `start()`, `stop()`, `openShell()`

```javascript
import GObject from 'gi://GObject';
import Gio from 'gi://Gio';
import GLib from 'gi://GLib';

const MACHINE_NAME = 'intuneme';
const POLL_INTERVAL_SECONDS = 5;
const INTUNEME_BIN = 'intuneme';

// Terminal emulators to try, in order of preference.
const TERMINALS = ['ptyxis', 'kgx', 'gnome-terminal', 'xterm'];

Gio._promisify(Gio.Subprocess.prototype, 'communicate_utf8_async');

/**
 * Run a command and return [success, stdout, stderr].
 */
async function execCommand(argv) {
    try {
        const proc = Gio.Subprocess.new(
            argv,
            Gio.SubprocessFlags.STDOUT_PIPE | Gio.SubprocessFlags.STDERR_PIPE,
        );
        const [stdout, stderr] = await proc.communicate_utf8_async(null, null);
        return [proc.get_successful(), stdout?.trim() ?? '', stderr?.trim() ?? ''];
    } catch (e) {
        return [false, '', e.message];
    }
}

/**
 * Find a terminal emulator on $PATH.
 * Checks $TERMINAL env var first, then a built-in list.
 */
function findTerminal() {
    const envTerminal = GLib.getenv('TERMINAL');
    if (envTerminal && GLib.find_program_in_path(envTerminal))
        return envTerminal;

    for (const term of TERMINALS) {
        if (GLib.find_program_in_path(term))
            return term;
    }
    return null;
}

export const ContainerManager = GObject.registerClass({
    Properties: {
        'container-running': GObject.ParamSpec.boolean(
            'container-running', '', '',
            GObject.ParamFlags.READABLE,
            false,
        ),
        'broker-running': GObject.ParamSpec.boolean(
            'broker-running', '', '',
            GObject.ParamFlags.READABLE,
            false,
        ),
        'transitioning': GObject.ParamSpec.boolean(
            'transitioning', '', '',
            GObject.ParamFlags.READABLE,
            false,
        ),
    },
}, class ContainerManager extends GObject.Object {
    _init() {
        super._init();

        this._containerRunning = false;
        this._brokerRunning = false;
        this._transitioning = false;

        this._setupDBusWatch();
        this._startPolling();
        // Do an immediate status check
        this._pollStatus();
    }

    get container_running() {
        return this._containerRunning;
    }

    get broker_running() {
        return this._brokerRunning;
    }

    get transitioning() {
        return this._transitioning;
    }

    _setContainerRunning(value) {
        if (this._containerRunning !== value) {
            this._containerRunning = value;
            this.notify('container-running');
        }
    }

    _setBrokerRunning(value) {
        if (this._brokerRunning !== value) {
            this._brokerRunning = value;
            this.notify('broker-running');
        }
    }

    _setTransitioning(value) {
        if (this._transitioning !== value) {
            this._transitioning = value;
            this.notify('transitioning');
        }
    }

    /**
     * Subscribe to MachineNew / MachineRemoved signals on the system bus.
     */
    _setupDBusWatch() {
        try {
            this._systemBus = Gio.DBus.system;
            this._machineNewId = this._systemBus.signal_subscribe(
                'org.freedesktop.machine1',
                'org.freedesktop.machine1.Manager',
                'MachineNew',
                '/org/freedesktop/machine1',
                null,
                Gio.DBusSignalFlags.NONE,
                (_conn, _sender, _path, _iface, _signal, params) => {
                    const name = params.get_child_value(0).get_string()[0];
                    if (name === MACHINE_NAME) {
                        this._setContainerRunning(true);
                        this._setTransitioning(false);
                    }
                },
            );
            this._machineRemovedId = this._systemBus.signal_subscribe(
                'org.freedesktop.machine1',
                'org.freedesktop.machine1.Manager',
                'MachineRemoved',
                '/org/freedesktop/machine1',
                null,
                Gio.DBusSignalFlags.NONE,
                (_conn, _sender, _path, _iface, _signal, params) => {
                    const name = params.get_child_value(0).get_string()[0];
                    if (name === MACHINE_NAME) {
                        this._setContainerRunning(false);
                        this._setBrokerRunning(false);
                        this._setTransitioning(false);
                    }
                },
            );
        } catch (e) {
            console.warn(`[intuneme] D-Bus signal watch failed, using polling only: ${e.message}`);
        }
    }

    /**
     * Poll `intuneme status` every POLL_INTERVAL_SECONDS.
     */
    _startPolling() {
        this._pollSourceId = GLib.timeout_add_seconds(
            GLib.PRIORITY_DEFAULT,
            POLL_INTERVAL_SECONDS,
            () => {
                this._pollStatus();
                return GLib.SOURCE_CONTINUE;
            },
        );
    }

    async _pollStatus() {
        const [ok, stdout] = await execCommand([INTUNEME_BIN, 'status']);
        if (!ok)
            return;

        const containerMatch = stdout.match(/^Container:\s+(\w+)/m);
        if (containerMatch) {
            const running = containerMatch[1] === 'running';
            if (!this._transitioning)
                this._setContainerRunning(running);
        }

        const brokerMatch = stdout.match(/^Broker proxy:\s+(\w+)/m);
        this._setBrokerRunning(brokerMatch ? brokerMatch[1] === 'running' : false);
    }

    /**
     * Start the container via pkexec.
     */
    async start() {
        if (this._containerRunning || this._transitioning)
            return;

        this._setTransitioning(true);
        const [ok, , stderr] = await execCommand(['pkexec', INTUNEME_BIN, 'start']);
        if (!ok) {
            console.warn(`[intuneme] start failed: ${stderr}`);
            this._setTransitioning(false);
            // Poll to reconcile state
            this._pollStatus();
        }
        // On success, D-Bus MachineNew signal will flip state
    }

    /**
     * Stop the container.
     */
    async stop() {
        if (!this._containerRunning || this._transitioning)
            return;

        this._setTransitioning(true);
        const [ok, , stderr] = await execCommand([INTUNEME_BIN, 'stop']);
        if (!ok) {
            console.warn(`[intuneme] stop failed: ${stderr}`);
            this._setTransitioning(false);
            this._pollStatus();
        }
        // On success, D-Bus MachineRemoved signal will flip state
    }

    /**
     * Open a terminal with `intuneme shell`.
     */
    openShell() {
        const terminal = findTerminal();
        if (!terminal) {
            console.error('[intuneme] No terminal emulator found');
            return;
        }

        try {
            // Most terminals use `-- command args` to run a command
            const proc = Gio.Subprocess.new(
                [terminal, '--', INTUNEME_BIN, 'shell'],
                Gio.SubprocessFlags.NONE,
            );
            proc.wait_async(null, null);
        } catch (e) {
            console.error(`[intuneme] Failed to launch terminal: ${e.message}`);
        }
    }

    destroy() {
        if (this._pollSourceId) {
            GLib.source_remove(this._pollSourceId);
            this._pollSourceId = null;
        }
        if (this._systemBus) {
            if (this._machineNewId)
                this._systemBus.signal_unsubscribe(this._machineNewId);
            if (this._machineRemovedId)
                this._systemBus.signal_unsubscribe(this._machineRemovedId);
        }
    }
});
```

**Step 2: Commit**

```bash
git add gnome-extension/containerManager.js
git commit -m "feat(extension): add container manager with D-Bus signals and CLI subprocess calls"
```

---

### Task 3: Create quickToggle.js — the Quick Settings UI

**Files:**
- Create: `gnome-extension/quickToggle.js`

**Step 1: Create quickToggle.js**

```javascript
import GObject from 'gi://GObject';
import * as QuickSettings from 'resource:///org/gnome/shell/ui/quickSettings.js';
import * as PopupMenu from 'resource:///org/gnome/shell/ui/popupMenu.js';

export const IntuneToggle = GObject.registerClass(
class IntuneToggle extends QuickSettings.QuickMenuToggle {
    _init(manager) {
        super._init({
            title: 'Intune',
            subtitle: 'Stopped',
            iconName: 'computer-symbolic',
            toggleMode: true,
        });

        this._manager = manager;

        // --- Popup menu ---
        this.menu.setHeader('computer-symbolic', 'Intune Container');

        // Status section
        this._statusSection = new PopupMenu.PopupMenuSection();

        this._containerStatusItem = new PopupMenu.PopupMenuItem('Container: Stopped', {
            reactive: false,
        });
        this._statusSection.addMenuItem(this._containerStatusItem);

        this._brokerStatusItem = new PopupMenu.PopupMenuItem('Broker Proxy: Unknown', {
            reactive: false,
        });
        this._statusSection.addMenuItem(this._brokerStatusItem);

        this.menu.addMenuItem(this._statusSection);
        this.menu.addMenuItem(new PopupMenu.PopupSeparatorMenuItem());

        // Open Shell action
        this._shellItem = this.menu.addAction('Open Shell', () => {
            this.menu.close();
            this._manager.openShell();
        });
        this._shellItem.sensitive = false;

        // --- Bind to manager state ---
        this._managerSignals = [];

        this._managerSignals.push(
            this._manager.connect('notify::container-running', () => this._sync()),
        );
        this._managerSignals.push(
            this._manager.connect('notify::broker-running', () => this._sync()),
        );
        this._managerSignals.push(
            this._manager.connect('notify::transitioning', () => this._sync()),
        );

        // Handle toggle clicks
        this.connect('clicked', () => {
            if (this._manager.transitioning)
                return;

            if (this._manager.container_running)
                this._manager.stop();
            else
                this._manager.start();
        });

        // Initial sync
        this._sync();
    }

    _sync() {
        const running = this._manager.container_running;
        const transitioning = this._manager.transitioning;

        // Toggle state
        this.checked = running;
        this.reactive = !transitioning;

        // Subtitle
        if (transitioning)
            this.subtitle = running ? 'Stopping\u2026' : 'Starting\u2026';
        else
            this.subtitle = running ? 'Running' : 'Stopped';

        // Menu items
        this._containerStatusItem.label.text = `Container: ${
            transitioning
                ? (running ? 'Stopping\u2026' : 'Starting\u2026')
                : (running ? 'Running' : 'Stopped')
        }`;

        this._brokerStatusItem.label.text = `Broker Proxy: ${
            this._manager.broker_running ? 'Running' : 'Stopped'
        }`;

        // Shell item only available when running and not transitioning
        this._shellItem.sensitive = running && !transitioning;
    }

    destroy() {
        for (const id of this._managerSignals)
            this._manager.disconnect(id);
        this._managerSignals = [];
        super.destroy();
    }
});
```

**Step 2: Commit**

```bash
git add gnome-extension/quickToggle.js
git commit -m "feat(extension): add Quick Settings toggle with popup menu"
```

---

### Task 4: Create the polkit action file

**Files:**
- Create: `gnome-extension/org.frostyard.intuneme.policy`

This polkit policy file gets installed to `/usr/share/polkit-1/actions/` and allows `pkexec intuneme start` to show a proper authentication dialog.

**Step 1: Create the polkit policy file**

```xml
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE policyconfig PUBLIC
 "-//freedesktop//DTD PolicyKit Policy Configuration 1.0//EN"
 "http://www.freedesktop.org/standards/PolicyKit/1/policyconfig.dtd">
<policyconfig>
  <action id="org.frostyard.intuneme.start">
    <description>Start the Intune container</description>
    <message>Authentication is required to start the Intune container</message>
    <defaults>
      <allow_any>auth_admin</allow_any>
      <allow_inactive>auth_admin</allow_inactive>
      <allow_active>auth_admin_keep</allow_active>
    </defaults>
    <annotate key="org.freedesktop.policykit.exec.path">/usr/local/bin/intuneme</annotate>
    <annotate key="org.freedesktop.policykit.exec.allow_gui">true</annotate>
  </action>
</policyconfig>
```

**Step 2: Commit**

```bash
git add gnome-extension/org.frostyard.intuneme.policy
git commit -m "feat(extension): add polkit policy for pkexec start"
```

---

### Task 5: Create the `intuneme extension install` CLI command

**Files:**
- Create: `cmd/extension.go`

This command embeds all files from `gnome-extension/` and copies them to the right locations. It follows the same patterns as the existing commands in `cmd/` (uses `config.DefaultRoot()`, `runner.SystemRunner`, `cobra.Command`, registers via `init()`).

**Step 1: Create cmd/extension.go**

```go
package cmd

import (
	"embed"
	"fmt"
	"io/fs"
	"os"
	"os/user"
	"path/filepath"

	"github.com/frostyard/intuneme/internal/runner"
	"github.com/spf13/cobra"
)

//go:embed extension/*
var extensionFS embed.FS

const extensionUUID = "intuneme@frostyard.org"

var extensionCmd = &cobra.Command{
	Use:   "extension",
	Short: "Manage the GNOME Shell extension",
}

var extensionInstallCmd = &cobra.Command{
	Use:   "install",
	Short: "Install the GNOME Shell Quick Settings extension",
	RunE: func(cmd *cobra.Command, args []string) error {
		r := &runner.SystemRunner{}

		u, err := user.Current()
		if err != nil {
			return fmt.Errorf("get current user: %w", err)
		}

		// Install extension files to ~/.local/share/gnome-shell/extensions/<uuid>/
		extDir := filepath.Join(u.HomeDir, ".local", "share", "gnome-shell", "extensions", extensionUUID)
		if err := os.MkdirAll(extDir, 0755); err != nil {
			return fmt.Errorf("create extension dir: %w", err)
		}

		err = fs.WalkDir(extensionFS, "extension", func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}

			// Compute the relative path under gnome-extension/
			rel, _ := filepath.Rel("extension", path)
			dest := filepath.Join(extDir, rel)

			if d.IsDir() {
				return os.MkdirAll(dest, 0755)
			}

			data, err := extensionFS.ReadFile(path)
			if err != nil {
				return err
			}

			return os.WriteFile(dest, data, 0644)
		})
		if err != nil {
			return fmt.Errorf("install extension files: %w", err)
		}
		fmt.Printf("Extension files installed to %s\n", extDir)

		// Install polkit policy (needs sudo)
		policyData, err := extensionFS.ReadFile("extension/org.frostyard.intuneme.policy")
		if err != nil {
			return fmt.Errorf("read polkit policy: %w", err)
		}

		tmpFile, err := os.CreateTemp("", "intuneme-policy-*.xml")
		if err != nil {
			return fmt.Errorf("create temp file: %w", err)
		}
		defer func() { _ = os.Remove(tmpFile.Name()) }()

		if _, err := tmpFile.Write(policyData); err != nil {
			_ = tmpFile.Close()
			return fmt.Errorf("write temp file: %w", err)
		}
		_ = tmpFile.Close()

		policyDest := "/usr/share/polkit-1/actions/org.frostyard.intuneme.policy"
		if _, err := r.Run("sudo", "cp", tmpFile.Name(), policyDest); err != nil {
			return fmt.Errorf("install polkit policy (sudo cp): %w", err)
		}
		fmt.Printf("Polkit policy installed to %s\n", policyDest)

		// Enable the extension
		if _, err := r.Run("gnome-extensions", "enable", extensionUUID); err != nil {
			fmt.Fprintf(os.Stderr, "warning: could not enable extension: %v\n", err)
			fmt.Println("You may need to enable it manually via GNOME Extensions app.")
		} else {
			fmt.Println("Extension enabled.")
		}

		fmt.Println("\nLog out and back in to activate the extension.")
		return nil
	},
}

func init() {
	extensionCmd.AddCommand(extensionInstallCmd)
	rootCmd.AddCommand(extensionCmd)
}
```

**Important note about embed path:** The `//go:embed extension/*` directive in `cmd/extension.go` embeds files relative to the `cmd/` directory. This means the `gnome-extension/` directory needs to be accessible as `cmd/extension/` at embed time. There are two approaches:

**Option A (symlink):** Create a symlink `cmd/extension -> ../gnome-extension` so the embed finds the files.

**Option B (move files):** Put the extension files directly in `cmd/extension/` instead of `gnome-extension/`.

Go's `embed` package does NOT follow symlinks, so **Option B is the actual approach**: the extension files must live at `cmd/extension/` rather than `gnome-extension/`. Update the directory structure accordingly:

```
cmd/
  extension/              # GNOME extension files (embedded in binary)
    metadata.json
    extension.js
    quickToggle.js
    containerManager.js
    stylesheet.css
    org.frostyard.intuneme.policy
  extension.go            # CLI command
```

All previous tasks should create files in `cmd/extension/` instead of `gnome-extension/`.

**Step 2: Verify it builds**

Run: `cd /home/bjk/projects/frostyard/intuneme && go build ./...`
Expected: Clean build, no errors.

**Step 3: Run fmt and lint**

Run: `cd /home/bjk/projects/frostyard/intuneme && make fmt && make lint`
Expected: No errors.

**Step 4: Commit**

```bash
git add cmd/extension.go
git commit -m "feat: add intuneme extension install command with embedded files"
```

---

### Task 6: Manual testing checklist

These are manual verification steps — not automated tests.

**Step 1: Build and install**

```bash
cd /home/bjk/projects/frostyard/intuneme
make build
sudo cp intuneme /usr/local/bin/
./intuneme extension install
```

**Step 2: Verify extension files are in place**

```bash
ls ~/.local/share/gnome-shell/extensions/intuneme@frostyard.org/
# Should show: metadata.json extension.js quickToggle.js containerManager.js stylesheet.css org.frostyard.intuneme.policy
```

**Step 3: Verify polkit policy installed**

```bash
ls /usr/share/polkit-1/actions/org.frostyard.intuneme.policy
```

**Step 4: Log out and back in, open Quick Settings**

- The "Intune" toggle should appear in Quick Settings
- If container is stopped: toggle shows unchecked, subtitle "Stopped"
- Click toggle: polkit dialog appears, enter password, container starts, toggle flips to checked
- Click toggle again: container stops, toggle flips to unchecked
- Open the popup menu: "Open Shell" should launch a terminal with `intuneme shell`

**Step 5: Verify D-Bus signals work**

```bash
# In one terminal, watch the extension:
journalctl -f GNOME_SHELL_EXTENSION_UUID=intuneme@frostyard.org
# In another terminal:
intuneme start   # Toggle should flip instantly
intuneme stop    # Toggle should flip instantly
```

---

### Summary of all files to create

```
cmd/
  extension/
    metadata.json
    extension.js
    quickToggle.js
    containerManager.js
    stylesheet.css
    org.frostyard.intuneme.policy
  extension.go
```

### Task dependency order

Tasks 1-4 create the extension files (can be done in any order). Task 5 creates the Go CLI command and depends on tasks 1-4 being complete. Task 6 is manual testing after all other tasks.
