# Container Lifecycle

Detailed flows for each lifecycle command. All commands share the `--root` flag (default: `~/.local/share/intuneme`) and use the `runner.Runner` interface for command execution.

## `intuneme init`

One-time provisioning that creates the container from scratch.

**Flags:** `--force` (reinit), `--password-file` (read password from file), `--insiders` (use insiders channel), `--tmp-dir` (temp directory for image extraction)

**Steps:**

1. **Check prerequisites** — Verify `systemd-nspawn` and `machinectl` are available (`prereq.Check()`)
2. **Create home bind mount** — `mkdir ~/Intune`
3. **Pull OCI image** — Auto-detect puller (podman → skopeo+umoci → docker), pull from `ghcr.io/frostyard/ubuntu-intune:<tag>`
4. **Extract rootfs** — Unpack image to `~/.local/share/intuneme/rootfs/`
5. **Configure GPU access** — Detect host render group GID, create matching group in container via `EnsureRenderGroup()` (resolves GID conflicts by reassigning the conflicting group to a free system GID 999–100), add user to it
6. **Create container user** — Match host UID/GID. Handles three cases: (a) rename existing user with same UID (e.g., `ubuntu` from OCI base) via `usermod --login --move-home`, (b) create new user with `useradd`, (c) update existing user's groups with `usermod --append`
7. **Set password** — Validate locally (12+ chars, at least one digit/uppercase/lowercase/special char, no username substring), then pass via bind-mounted read-only temp file to `chpasswd` inside the container (avoids shell injection)
8. **Write fixups** — `<hostname>LXC` to `/etc/hostname`, `/etc/hosts`, `profile.d/intuneme.sh`, `fix-home-ownership.service` (oneshot unit to chown home dir), container-side sudoers at `/etc/sudoers.d/intuneme` (`<user> ALL=(ALL) NOPASSWD: ALL`)
9. **Install polkit rule** — `50-intuneme.rules` to `/etc/polkit-1/rules.d/` (allows sudo group to use machinectl)
10. **Install sudoers rule** — `/etc/sudoers.d/intuneme-exec` for passwordless nsenter (validated with `visudo -c`)
11. **SELinux** (if detected) — Label rootfs as `container_file_t` via `semanage fcontext` + `restorecon`, install `intuneme-machined` policy module granting `systemd_machined_t` PTY access (`user_devpts_t`) and `/tmp` symlink traversal (`user_tmp_t`)
12. **Save config** — Write `config.toml`

## `intuneme start`

Boots the container and sets up runtime environment.

**Flow:**

1. **Load config** — Read `config.toml`, verify rootfs exists
2. **Detect host sockets** — Auto-detect Wayland, PipeWire, PulseAudio, X11 auth (`nspawn.DetectHostSockets()`)
3. **Detect Nvidia GPU** — If `/dev/nvidiactl` exists, detect device nodes, parse `ldconfig -p` for host libraries, prepare bind mounts for lib dirs and ICD files
4. **Prepare broker proxy** (if enabled) — Create runtime directory, add bind mount
5. **Validate sudo** — Prompt for password if needed (`nspawn.ValidateSudo()`)
6. **Write display marker** — Write host `$DISPLAY` to `rootfs/etc/intuneme-host-display` (read by profile.d script on container login)
7. **Boot container** — `systemd-nspawn` with all bind mounts, DRI device cgroup rules, Nvidia device binds with explicit `DeviceAllow`, `--boot` flag
8. **Wait for registration** — Poll `machinectl` up to 30 seconds until container is listed
9. **Clean stale Nvidia symlinks** — Always runs (even on non-Nvidia boots) to remove symlinks from previous sessions
10. **Setup Nvidia libraries** (if detected) — Create symlinks in container's `/usr/lib/x86_64-linux-gnu/` → `/run/host-nvidia/<index>/`, then run `ldconfig`
11. **Install udev rules** — YubiKey (`70-intuneme-yubikey.rules`) and video (`70-intuneme-video.rules`) hotplug rules + helper script (`/usr/local/lib/intuneme/usb-hotplug`)
12. **Ensure sudoers** — Reinstall sudoers rule if missing (handles upgrades from older versions)
13. **Forward existing YubiKeys** — Scan sysfs for Yubico vendor ID `1050`, forward USB device nodes + associated hidraw devices
14. **Forward existing video devices** — Glob `/dev/video*` and `/dev/media*`, forward each with `0660 root:video` permissions
15. **Start broker proxy** (if enabled):
    - Enable systemd linger for container user
    - Create login session via `machinectl`
    - Wait for session bus socket to appear
    - Launch `intuneme broker-proxy` as background process with PID file

## `intuneme stop`

Graceful shutdown via `runStop()` (shared between `stop` command and internal use).

**Flow:**

1. **Stop broker proxy** (if enabled) — Kill process by PID file, remove PID file
2. **Remove udev rules** — Delete rules files, helper script, and state dir (`/run/intuneme/devices`), reload udev (idempotent)
3. **Power off container** — `machinectl poweroff <machine>`
4. **Wait for deregistration** — Poll `machinectl` every 500ms, up to 60 attempts (30 seconds max)

## `intuneme destroy`

Removes container and all host modifications, preserves user files.

**Flow:**

1. **Stop container** if running — calls `nspawn.Stop()` directly (does NOT use `runStop()`, so no broker proxy stop)
2. **Remove udev rules** — Delete hotplug rules and helper script via `udev.Remove()` (graceful, handles missing files)
3. **Remove polkit rule** — Delete `/etc/polkit-1/rules.d/50-intuneme.rules`
4. **Remove sudoers rule** — Delete `/etc/sudoers.d/intuneme-exec`
5. **Delete rootfs** — `sudo rm -rf` the rootfs directory
6. **Remove config** — Delete `config.toml`
7. **Clean enrollment state** from `~/Intune`:
   - `.config/intune/` (enrollment database)
   - `.local/share/intune/`, `.local/share/intune-portal/` (app state)
   - `.local/share/keyrings/` (gnome-keyring)
   - `.local/state/microsoft-identity-broker/` (broker state)
   - `.cache/intune-portal/` (cache)
8. **Preserve user files** — Downloads, documents, etc. remain in `~/Intune`

## `intuneme recreate`

Updates the container image while preserving enrollment. Can switch channels.

**Flags:** `--insiders` (switch to insiders channel), `--tmp-dir` (temp directory for image extraction)

**Flow:**

1. **Early validation** — Verify initialized, validate sudo access
2. **Stop container** if running — stops broker proxy first (if enabled), then `nspawn.Stop()` directly
3. **Backup state:**
   - Password hash from container's `rootfs/etc/shadow` (`provision.BackupShadowEntry()`)
   - Device broker state from `rootfs/var/lib/microsoft-identity-device-broker` to temp dir (`provision.BackupDeviceBrokerState()`)
4. **Delete old rootfs** — `sudo rm -rf`
5. **Pull and extract new image** — Same as init step (channel can be switched via `--insiders` flag)
6. **Re-provision** — GPU render group, user creation, hostname (`<host>LXC`), fixups, polkit rule
7. **Restore state:**
   - Write backed-up password hash into new `rootfs/etc/shadow`
   - Copy backed-up device broker state back into `rootfs/var/lib/microsoft-identity-device-broker`
8. **Update config** — Save with potentially new insiders flag

Note: `recreate` reinstalls the host polkit rule but does NOT reinstall the host sudoers rule — `start` handles that idempotently.

## `intuneme status`

Reports container state without modifying anything.

**Output fields (JSON keys):** `initialized` (bool), `root` (string), `rootfs` (string), `machine` (string), `container` ("running"/"stopped"), `channel` ("stable"/"insiders"), `broker_proxy` ("running (PID X)"/"not running", omitted if disabled)

Supports `--json` for machine-readable output via `clix.OutputJSON()`.

## `intuneme shell`

Opens an interactive bash login shell inside the container.

Uses `machinectl shell <user>@<machine> /bin/bash --login`. The login shell sources `/etc/profile.d/intuneme.sh` which sets up DISPLAY, audio, and keyring.

## `intuneme open edge` / `intuneme open portal`

Launches GUI apps via `nspawn.Exec()`. Uses the nsenter pattern described in [OVERVIEW.md](OVERVIEW.md#command-execution-inside-the-container). Both are built from a shared `makeOpenAppCmd()` factory.

## `intuneme udev install` / `intuneme udev remove`

Manually install or remove udev rules for device hotplug forwarding. Normally called automatically by `start`/`stop`, but can be run independently.

**`install`:**
1. Load config to get machine name
2. Validate sudo access
3. Install YubiKey rules (`70-intuneme-yubikey.rules`), video rules (`70-intuneme-video.rules`), and helper script (`/usr/local/lib/intuneme/usb-hotplug`) via `udev.Install()`
4. Reload udev

**`remove`:**
1. Check if rules are installed (`udev.IsInstalled()`)
2. Validate sudo access
3. Remove rules files and helper script via `udev.Remove()`
4. Reload udev

## `intuneme config broker-proxy enable` / `disable`

Toggle the D-Bus broker proxy for host-side SSO. See [Broker Proxy](broker-proxy.md) for details.

**`enable`:** Sets `broker_proxy = true` in config, installs D-Bus service file for auto-activation.

**`disable`:** Clears the flag, removes D-Bus service file, stops the proxy if running.

## `intuneme extension install`

Installs the GNOME Shell Quick Settings extension for managing the container from the desktop.

**Steps:**
1. Copy embedded extension files to `~/.local/share/gnome-shell/extensions/intuneme@frostyard.org/`
2. Install polkit policy to `/etc/polkit-1/actions/org.frostyard.intuneme.policy` (requires sudo)
3. Enable extension via `gnome-extensions enable`

The extension provides a Quick Settings toggle (start/stop), status display, and shortcuts for shell, Edge, and Intune Portal. It monitors container state via systemd-machined D-Bus signals with a 5-second polling fallback.

## `intuneme gendocs` (hidden)

Generates markdown CLI reference pages for the MkDocs documentation site.

**Usage:** `intuneme gendocs <output-dir>`
