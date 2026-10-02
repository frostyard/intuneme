# Container Lifecycle

Detailed flows for each lifecycle command. All commands share the `--root` flag (default: `~/.local/share/intuneme`) and use the `runner.Runner` interface for command execution.

## `intuneme init`

One-time provisioning that creates the container from scratch.

**Flags:** `--force` (reinit), `--password-file` (read password from file), `--insiders` (use insiders channel), `--tmp-dir` (temp directory for image extraction)

**Steps:**

1. **Check prerequisites** — Verify `systemd-nspawn` and `machinectl` are available (`prereq.Check()`)
2. **Create home bind mount** — `mkdir ~/Intune`
3. **Pull OCI image** — Auto-detect puller (podman → skopeo+umoci → docker), pull from `ghcr.io/frostyard/ubuntu-intune:<tag>`
4. **Extract rootfs** — Unpack image to `~/.local/share/intuneme/rootfs/`, then assert with `puller.VerifyRootOwned()` that `rootfs/etc/passwd` is owned by `0:0`
5. **Configure GPU access** — Detect host render group GID, create matching group in container via `EnsureRenderGroup()` (resolves GID conflicts by reassigning the conflicting group to a free system GID 999–100), add user to it
6. **Create container user** — Match host UID/GID. Handles three cases: (a) rename existing user with same UID (e.g., `ubuntu` from OCI base) via `usermod --login --move-home`, (b) create new user with `useradd`, (c) update existing user's groups with `usermod --append`
7. **Set password** — Validate locally (12+ chars, at least one digit/uppercase/lowercase/special char, no username substring), then pass via bind-mounted read-only temp file to `chpasswd` inside the container (avoids shell injection)
8. **Write fixups** — `<hostname>LXC` to `/etc/hostname`, `/etc/hosts`, session scripts (`/usr/local/bin/intuneme-session-setup` + `profile.d/intuneme.sh` that sources it, via `provision.InstallSessionScripts`), `fix-home-ownership.service` (oneshot unit to chown home dir), container-side sudoers at `/etc/sudoers.d/intuneme` (a comment-only file with no grants; interactive sudo still requires the container password)
9. **Install polkit rule** — `50-intuneme.rules` grants only the configured caller UID a shell as the configured non-root user in the configured machine
10. **Install sudoers rule + helper**: root-owned helper binary at `/usr/local/libexec/intuneme/nsenter-exec` (`0755`), its root-owned config at `nsenter-exec.toml` (`0644`), and `/etc/sudoers.d/intuneme-exec` granting passwordless sudo for that single wildcard-free path (validated with `visudo -c`). The config bakes the machine name, user, numeric UID/GID, home, and rootfs. The helper resolves and validates the container leader itself and drops to the numeric UID before executing the script, so the caller cannot supply a PID or influence the target UID. The helper indirection is required by sudo-rs, which rejects wildcards in command arguments (issue #168).
11. **SELinux** (if enabled — enforcing or permissive) — Label rootfs as `container_file_t` via `semanage fcontext` + `restorecon`, install `intuneme-machined` policy module granting `systemd_machined_t` PTY access (`user_devpts_t`) and `/tmp` symlink traversal (`user_tmp_t`). A real `semanage` failure aborts `init` before the config save with the `semanage` output; the error names `policycoreutils-python-utils` and `intuneme init --force`, because the rootfs now exists. Only a single legacy "already defined"/"already exists" line is ignored (`cmd.finishInit`, `provision.InstallSELinuxPolicy`)
12. **Save config and report success** — Write `config.toml`, then print "Initialized intuneme at <root>"; neither happens when the SELinux step fails

### Provisioning execs must set `PATH`

Every provisioning step that runs a container tool (`groupmod`, `groupadd`,
`useradd`, `usermod`, `chpasswd`) goes through `provision.containerExec`, which
passes `--setenv=PATH=<nspawn.StandardPATH>` to `systemd-nspawn -D`. Without it
nspawn hands the command the host systemd's default `PATH`, which on NixOS has
no `/usr/sbin` and no `/usr/bin`; `init` then dies with
`execv(groupmod) failed: No such file or directory`. A new provisioning exec
must use `containerExec` rather than calling `systemd-nspawn` directly —
`TestProvisioningNspawnCommandsSetPATH` fails otherwise.

### Extraction must produce a root-owned rootfs

Extraction (`sudo tar -xf`, `sudo umoci raw unpack`) preserves whatever UIDs
the archive carries. If the pull runs inside a nested user namespace — a
rootless container, `podman unshare`, distrobox/toolbox, a sandboxed agent
shell — then "root" in that namespace is the invoking host user, so every
container-root file is extracted owned by that user instead of `0`.

Nothing downstream notices on its own. The container still boots, but `sudo`
inside it is dead (`/usr/bin/sudo must be owned by uid 0 and have the setuid
bit set`), `/etc/sudoers*` is rejected, and because the container runs without
`--private-users` (see `nspawn.buildBootArgs`) container root **is** host
root — so the host user owns ~23k files that later execute as real root.

`puller.VerifyRootOwned()` therefore hard-fails `init` and `recreate` right
after extraction rather than handing back a silently poisoned rootfs. The
diagnostic is the invoking shell's own `cat /proc/self/uid_map`, which must
read `0 0 4294967295` (the initial user namespace). Do **not** check this
through `podman unshare`: that subcommand exists to enter a user namespace,
so under rootless podman it always reports `0 <your-uid> 1` regardless of
where it is run.

## `intuneme start`

Boots the container and sets up runtime environment.

**Flow:**

1. **Load config** — Read `config.toml`, verify rootfs exists
2. **Detect host sockets** — Auto-detect Wayland, PipeWire, PulseAudio, X11 auth (`nspawn.DetectHostSockets()`)
3. **Detect Nvidia GPU** — If `/dev/nvidiactl` exists, detect device nodes, parse `ldconfig -p` for host libraries, prepare bind mounts for lib dirs and ICD files
4. **Prepare runtime bind mount** (if the broker, notify or screencast proxy is enabled) — Create runtime directory, add bind mount
5. **Validate sudo** — Prompt for password if needed (`nspawn.ValidateSudo()`)
6. **Write display marker** — Write host `$DISPLAY` to `rootfs/etc/intuneme-host-display` (read by `intuneme-session-setup` on login and on every app launch)
7. **Override X11 tmpfiles rule** — `provision.OverrideX11Tmpfiles()` writes `/etc/tmpfiles.d/x11.conf` into the rootfs: the vendor `x11.conf` minus its `/tmp/.X11-unix` directive. That directory is bind-mounted from the host, and the vendor `D!` type would empty it on every container boot, costing the host its Xwayland socket. Fails the start if it cannot be written. See [overview.md](overview.md) → "The X11 Socket Directory Is the Host's"
8. **Boot container** — `systemd-nspawn` with all bind mounts, DRI device binds plus explicit `DeviceAllow=<dev> rwm` rules, Nvidia device binds with explicit `DeviceAllow`, `--boot` flag
9. **Wait for registration** — Poll `machinectl` up to 30 seconds until container is listed
10. **Clean stale Nvidia symlinks** — Always runs (even on non-Nvidia boots) to remove symlinks from previous sessions
11. **Setup Nvidia libraries** (if detected) — Create symlinks in container's `/usr/lib/x86_64-linux-gnu/` → `/run/host-nvidia/<index>/`, then run `ldconfig`
12. **Install udev rules** — YubiKey (`70-intuneme-yubikey.rules`) and video (`70-intuneme-video.rules`) hotplug rules + helper script (`/usr/local/lib/intuneme/usb-hotplug`)
13. **Ensure sudoers**: Reinstall sudoers rule and nsenter helper if either is missing (handles upgrades from the old wildcard-only rule; `IsInstalled` requires both)
13b. **Ensure session scripts** — Unconditionally reinstall `/usr/local/bin/intuneme-session-setup` + `profile.d/intuneme.sh` (`provision.InstallSessionScripts`, idempotent), so the rootfs copy always matches the embedded one and CLI upgrades reach existing containers without a `recreate`
Before boot or the already-running return, authenticate and reconcile session scripts and broker policy, then rewrite `/etc/sudoers.d/intuneme` (`provision.EnsureContainerSudoers`) on every start, so existing containers lose all passwordless sudo grants without a `recreate`. Reconciliation failure is fatal
14. **Reconcile user groups** — `provision.EnsureUserGroups()` reads the container user's groups via `id -nG` (run as root inside the container's mount namespace via nsenter) and runs `usermod -aG <group> <user>` for any missing group in `requiredRuntimeGroups()` (currently just `plugdev`, required for pcscd access per issue #146). Idempotent; warns and continues on failure.
15. **Forward existing YubiKeys** — Scan sysfs for Yubico vendor ID `1050`, forward USB device nodes + associated hidraw devices
16. **Forward existing video devices** — Glob `/dev/video*` and `/dev/media*`, forward each with `0660 root:video` permissions
17. **Open the container session bus** (if the broker, notify or screencast proxy is enabled):
    - Enable systemd linger for container user
    - Create login session via `machinectl`
    - Wait for session bus socket to appear
18. **Start proxies** (each if enabled) — `startProxy()` launches `intuneme broker-proxy`, `intuneme notify-proxy` and `intuneme screencast-proxy` as background processes with `setsid`, then polls each PID file (up to 5 seconds). The notify and screencast proxies must own their names before Edge starts, because Chromium looks for a notification server and checks the portal only at browser startup

## `intuneme stop`

Graceful shutdown via `runStop()` (shared between `stop` command and internal use).

**Flow:**

1. **Stop broker, notify and screencast proxies** (each if enabled) — Kill process by PID file, remove PID file
2. **Remove udev rules** — Delete rules files, helper script, and state dir (`/run/intuneme/devices`), reload udev (idempotent)
3. **Power off container and wait** — `nspawn.StopAndWait()`: `machinectl poweroff <machine>`, then poll `machinectl show` every 500ms for up to 60 attempts (30 seconds max) until the machine deregisters from `systemd-machined`

## `intuneme destroy`

Removes container and host modifications. By default preserves user files in `~/Intune`; with `--all` performs a full uninstall.

**Flags:** `--all` (full uninstall — remove all intuneme artifacts including GNOME extension, D-Bus service, and `~/Intune`)

**Flow (default):**

1. **Stop broker, notify and screencast proxies** (each if enabled) — Kill process by PID file
2. **Stop container** if running — `nspawn.StopAndWait()`
3. **Remove udev rules** — Delete hotplug rules and helper script via `udev.Remove()` (graceful, handles missing files)
4. **Remove polkit rule** — Delete `/etc/polkit-1/rules.d/50-intuneme.rules`
5. **Remove sudoers rule + helper**: Delete `/etc/sudoers.d/intuneme-exec`, `/usr/local/libexec/intuneme/nsenter-exec`, and `nsenter-exec.toml`
6. **Delete rootfs** — `sudo rm -rf` the rootfs directory
7. **Remove config** — Delete `config.toml`
8. **Clean enrollment state** from `~/Intune`:
   - `.config/intune/` (enrollment database)
   - `.local/share/intune/`, `.local/share/intune-portal/` (app state)
   - `.local/share/keyrings/` (gnome-keyring)
   - `.local/state/microsoft-identity-broker/` (broker state)
   - `.cache/intune-portal/` (cache)
9. **Preserve user files** — Downloads, documents, etc. remain in `~/Intune`

**Additional steps with `--all`** (replaces steps 8–9):

8. **Disable and remove GNOME extension** — `gnome-extensions disable` + remove `~/.local/share/gnome-shell/extensions/intuneme@frostyard.org/` (best-effort, GNOME may not be running)
9. **Remove polkit policy action** — Delete `/etc/polkit-1/actions/org.frostyard.intuneme.policy` (installed by `extension install`)
10. **Remove D-Bus broker service file** — Delete the user-level D-Bus activation file for `com.microsoft.identity.broker1`
11. **Remove `~/Intune` entirely** — All user files, Downloads, etc.
12. **Remove data root entirely** — Delete `~/.local/share/intuneme/` (rootfs already removed above)

## `intuneme recreate`

Updates the container image while preserving enrollment. Can switch channels.

**Flags:** `--insiders` (switch to insiders channel), `--tmp-dir` (temp directory for image extraction)

**Flow:**

1. **Early validation** — Verify initialized, validate sudo access
2. **Stop container** if running — stops broker proxy first (if enabled), then `nspawn.StopAndWait()`
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

`machinectl poweroff` only signals the container's init and returns
immediately, so `systemd-nspawn` is still unwinding the rootfs (its `/sys`,
`/proc` and cgroup mounts) when it does. Both commands that delete the rootfs
— `recreate` and `destroy` — must therefore go through
`nspawn.StopAndWait()`, never bare `nspawn.Stop()`. Calling `rm -rf` inside
that window aborts partway with `Directory not empty` and leaves a
half-erased tree; in `recreate` that used to also discard the only copy of
the enrollment, because the deferred cleanup deleted the device-broker backup
on the error path. The backup is now retained (and its path reported) unless
the restore actually succeeded.

## `intuneme status`

Reports container state without modifying anything.

**Output fields (JSON keys):** `initialized` (bool), `root` (string), `rootfs` (string), `machine` (string), `container` ("running"/"stopped"), `channel` ("stable"/"insiders"), `broker_proxy`, `notify_proxy` and `screencast_proxy` ("running (PID X)"/"not running", empty if disabled)

Supports `--json` for machine-readable output via `clix.OutputJSON()`.

## `intuneme shell`

Opens an interactive bash login shell inside the container.

Uses `machinectl shell <user>@<machine> /bin/bash --login`. The login shell sources `/etc/profile.d/intuneme.sh`, which in turn sources the shared `/usr/local/bin/intuneme-session-setup` to set up DISPLAY, audio, the D-Bus activation environment, and the keyring.

## `intuneme open edge` / `intuneme open portal`

Launches GUI apps via `nspawn.Exec()`. Uses the nsenter pattern described in [overview.md](overview.md#command-execution-inside-the-container). Both are built from a shared `makeOpenAppCmd()` factory. Arguments after `--` are passed to the app: `runOpen()` quotes each one with `nspawn.ShellQuote()` and appends it to the command, so `intuneme open edge -- --app=<url>` opens a site as a standalone Edge window. The quoting matters because `nspawn.Exec()` runs the command through `bash -c`. Because the nsenter path is a *non-login* shell, `nspawn.Exec()` runs `/usr/local/bin/intuneme-session-setup` before launching the app — this is what gives the D-Bus-activated identity broker a DISPLAY and an unlocked keyring, without which authentication fails.

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

## `intuneme config notify-proxy enable` / `disable`

Toggle forwarding of container desktop notifications to the host. See [Notify Proxy](notify-proxy.md) for details.

**`enable`:** Sets `notify_proxy = true` in config. The proxy starts on the next `start`. For a running container, the output tells the user to run `stop` and then `start`, because the runtime bind mount is set up at boot. There is no D-Bus activation file: the proxy owns its name on the container bus, which a host service file cannot activate. `containerBusProxyConfigCmd()` in `cmd/config.go` builds both subcommands.

**`disable`:** Clears the flag, stops the proxy if running.

## `intuneme config screencast-proxy enable` / `disable`

Toggle forwarding of the ScreenCast portal to the host. See [Screencast Proxy](screencast-proxy.md) for details.

**`enable`:** Sets `screencast_proxy = true` in config. The proxy starts on the next `start`. For a running container, the output tells the user to run `stop` and then `start`. Built by `containerBusProxyConfigCmd()`, like the notify proxy commands.

**`disable`:** Clears the flag, stops the proxy if running. The container's own portal starts again on the next portal call.

## `intuneme extension install`

Installs the GNOME Shell Quick Settings extension for managing the container from the desktop.

**Steps:**
1. Copy embedded extension files to `~/.local/share/gnome-shell/extensions/intuneme@frostyard.org/`
2. Install polkit policy to `/etc/polkit-1/actions/org.frostyard.intuneme.policy` (requires sudo)
3. Enable extension via `gnome-extensions enable`

The extension provides a Quick Settings toggle (start/stop), status display, and shortcuts for shell, Edge, and Intune Portal. It monitors container state via systemd-machined D-Bus signals with a 5-second polling fallback.

## `intuneme broker-proxy`

Runs the D-Bus broker proxy in the foreground. This is the daemon process that forwards `com.microsoft.identity.broker1` method calls from the host session bus to the container session bus.

Normally launched automatically by `start` (as a background process) or by D-Bus service activation. Can be run manually for debugging.

See [Broker Proxy](broker-proxy.md) for the full proxy architecture.

## `intuneme notify-proxy`

Runs the notification proxy in the foreground. It owns `org.freedesktop.Notifications` on the container session bus and forwards calls to the host notification server.

Normally launched automatically by `start` as a background process. Can be run manually for debugging, after `intuneme config notify-proxy disable` or `stop` of the background one: a second instance refuses to start while the PID file names a live proxy. `runContainerBusProxy()` in `cmd/proxy_run.go` writes the PID file only after the proxy owns its bus name.

See [Notify Proxy](notify-proxy.md) for the full proxy architecture.

## `intuneme screencast-proxy`

Runs the screencast proxy in the foreground. It takes over `org.freedesktop.portal.Desktop` on the container session bus and forwards the ScreenCast portal to the host portal.

Normally launched automatically by `start` as a background process. Can be run manually for debugging, with the same PID file rules as `intuneme notify-proxy`.

See [Screencast Proxy](screencast-proxy.md) for the full proxy architecture.

## `intuneme gendocs` (hidden)

Generates markdown CLI reference pages for the MkDocs documentation site.

**Usage:** `intuneme gendocs <output-dir>`

### Security migration before lifecycle operations

`start` authenticates and runs `refreshStartSecurity` before either booting or
returning "already running". Both helper binaries, their baked config, host
shell authorization, container broker authorization, session scripts and the
comment-only container sudoers file are reconciled. An error stops the command.
A user-owned executable is not an acceptable helper refresh source.

`StopAndWait`, used by stop/destroy/recreate, authenticates and falls back to
`sudo machinectl poweroff <machine>` only if the machine helper is absent.
This permits upgrading a running installation without needing to start it
first. Helper rejections are never bypassed. Deregistration is still awaited
before deleting or replacing the rootfs. See [ADR-0002](../adr/0002-pin-machine-operations-and-scope-shell-authorization.md).
