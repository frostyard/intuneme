# Container Lifecycle

Detailed flows for each lifecycle command. All commands share the `--root` flag (default: `~/.local/share/intuneme`) and use the `runner.Runner` interface for command execution.

## `intuneme init`

One-time provisioning that creates the container from scratch.

**Flags:** `--force` (reinit), `--password-file` (read password from file), `--insiders` (use Microsoft insiders channel), `--himmelblau` (experimental alternative image), `--himmelblau-terminal-passkey` (optional assertion for the local-build terminal QR variant; requires `--himmelblau` and `--himmelblau-local-image`), `--himmelblau-local-image` (locally built Himmelblau image), `--tmp-dir` (temp directory for image extraction)

**Steps:**

1. **Check prerequisites** — Verify `systemd-nspawn` and `machinectl` are available (`prereq.Check()`)
2. **Create home bind mount** — `mkdir ~/Intune`
3. **Pull OCI image** — Auto-detect puller (podman → skopeo+umoci → docker). `version.ImageRef()` selects the Microsoft or standard Himmelblau registry image; a marked local terminal-passkey image is detected automatically when passed with `--himmelblau-local-image`. The explicit `--himmelblau-terminal-passkey` switch additionally asserts that the image must carry the marker.
4. **Extract rootfs** — Unpack image to `~/.local/share/intuneme/rootfs/`, then assert with `puller.VerifyRootOwned()` that `rootfs/etc/passwd` is owned by `0:0`
5. **Configure GPU access** — Detect host render group GID, create matching group in container via `EnsureRenderGroup()` (resolves GID conflicts by reassigning the conflicting group to a free system GID 999–100), add user to it
6. **Create container user** — Match the host username and UID. Handles three cases: (a) rename existing user with same UID (e.g., `ubuntu` from OCI base) via `usermod --login --move-home`, (b) create new user with `useradd`, (c) update existing user's groups with `usermod --append`. In Himmelblau mode only, rename the group entry for that account's primary GID to the same username, preserving the numeric GID; fail if that group name is already used by another GID.
7. **Set password** — Prompt for a non-empty password (or read it from `--password-file`), then pass it via a bind-mounted read-only temp file to `chpasswd` inside the container (avoids shell injection). No password-quality rules are enforced. Recommend a strong password because it protects interactive login to the container.
8. **Write fixups** — `<hostname>LXC` to `/etc/hostname`, `/etc/hosts`, session scripts (`/usr/local/bin/intuneme-session-setup` + `profile.d/intuneme.sh` that sources it, via `provision.InstallSessionScripts`), `fix-home-ownership.service` (oneshot unit to chown home dir), container-side sudoers at `/etc/sudoers.d/intuneme` (`<user> ALL=(ALL) NOPASSWD: ALL`)
9. **Install polkit rule** — `50-intuneme.rules` to `/etc/polkit-1/rules.d/` (allows sudo group to use machinectl)
10. **Install sudoers rule + helper**: root-owned helper at `/usr/local/libexec/intuneme/nsenter-exec` (`0755`) plus `/etc/sudoers.d/intuneme-exec` granting passwordless sudo for that single wildcard-free path (validated with `visudo -c`). The helper indirection is required by sudo-rs, which rejects wildcards in command arguments (issue #168).
11. **SELinux** (if enabled — enforcing or permissive) — Label rootfs as `container_file_t` via `semanage fcontext` + `restorecon`, install `intuneme-machined` policy module granting `systemd_machined_t` PTY access (`user_devpts_t`) and `/tmp` symlink traversal (`user_tmp_t`)
12. **Save config** — Write `config.toml`

### Experimental Himmelblau image

`intuneme init --himmelblau` selects a separate Ubuntu 26.04 image with Himmelblau's PAM/NSS, broker, and compliance packages. It deliberately omits Microsoft's `intune-portal` and `microsoft-identity-broker`; it uses the signed community-nightly package repository and is not upstream-validated under systemd-nspawn.

During Himmelblau init and recreate, intuneme aligns the primary group's name
with the local username (for example, `alice:alice`) without changing its GID. This
avoids the common user-map resolver collision where a differently named local
group contributes a GID equal to the mapped user's UID. The Microsoft image is
unchanged. Existing Himmelblau containers need `recreate` for this rootfs
provisioning change.

Init verifies that the extracted image contains executable
`/usr/bin/pamtester`, required for fail-closed authentication in `enroll`.
The Himmelblau image build also asserts this dependency after package
installation. For an initialized local-image installation, rebuild the local
tag and run `recreate` followed by `start` to replace the existing rootfs.

The packaged, commented configuration at
`/usr/lib/himmelblau/himmelblau.conf` is the baseline for
`/etc/himmelblau/himmelblau.conf`. Init sets the configured domain,
`cn_name_mapping = false`, and `user_map_file = /etc/himmelblau/user-map`
without dropping the package comments. `intuneme edit --himmelblau` opens this
effective file in `$VISUAL` or `$EDITOR`; in a graphical session it uses
`xdg-open` and the host's default text application, otherwise it searches
`sensible-editor`, `editor`, `micro`, `nano`, `nvim`, `vim`, and `vi`. The
desktop path prompts for Enter after the user saves and closes the app. Use
the command during setup or later to change Himmelblau settings. The saved host copy at
`<root>/himmelblau.conf` is the durable source of truth, is installed on every
container boot and after `recreate`, and is removed by `destroy`. Users can edit
that host copy directly; stop and start the container to apply changes.
Himmelblau reads this INI-style key/value config, not TOML.
Intuneme checks the three required mapping settings using that syntax and
leaves unrelated options to Himmelblau's parser; it also reads the QR flag to
prepare host Bluetooth access during enrollment. It defaults
`global.apply_policy = true` over the package baseline and when a saved config
omits or comments out the setting. This applies to existing saved configs when
`start` or `recreate` installs them. An explicit `apply_policy = false`
remains an opt-out. This enables Himmelblau's Intune device enrollment and
policy application.
Intuneme also defaults `global.enable_passwordless_qr_bluetooth = true` in
fresh configs and inserts it into saved configs when absent or commented; an
explicit `false` remains an opt-out. Only the terminal-passkey image supports
the terminal QR handler, and enrollment uses host BlueZ through the filtered
proxy described below.
Before opening the editor, `PrepareHimmelblauConfig` rewrites those required
settings in the temporary file, so prior installations with an unquoted
`user_map_file` path are repaired without losing the user's other options.
This outside-the-rootfs persistence is the reason custom options survive image
replacement. Intuneme validates only the mapping settings it owns using
Himmelblau's INI semantics and leaves unrelated values to Himmelblau.
Stopped-container enrollment boots with a new user map; enrollment on a
running container reboots so the daemon loads the map.

## `intuneme edit`

`intuneme edit --himmelblau` opens the active or saved Himmelblau config in the
user's editor. It requires an initialized Himmelblau installation and an
interactive terminal. The command validates and saves the result at
`<root>/himmelblau.conf`; it does not change a running daemon's live config.
Stop and start the container to apply edits. Invalid files can be edited and
repaired, but only a valid file with intuneme's required mapping settings is
saved.

Himmelblau enrolls during PAM authentication. After `init`, run `intuneme enroll --user user@example.com` from a terminal; the container may be stopped or running. The command verifies the image has `pamtester`, then writes a `host_user:UPN` mapping to `/etc/himmelblau/user-map`. If stopped, it runs the normal `start` flow to boot with the new map. If already running with a new mapping, it reboots so the daemon loads it; this closes running apps and shells. If already mapped, it keeps the container running and checks that the services are ready. It then removes the local username from the `machine-shell` PAM allowlist and invokes that fail-closed PAM authentication service directly with `pamtester`; a successful `machinectl shell` connection by itself does not authenticate the user. Only after PAM succeeds does intuneme open the usual local shell. In systemd-nspawn, restarting or reloading `himmelblaud` in place leaves socket activation broken, so the running-container path requires reboot. After the user exits the shell, intuneme verifies that the existing local account still has the host-matched UID and saves the UPN; the Entra session does not create a second Unix account. No RFC2307 UID attribute is required.

The standard `ubuntu-himmelblau` image uses upstream's signed nightly PAM
module, whose QR/Bluetooth terminal handler is GDM-gated. The explicit
`ubuntu-himmelblau-terminal-passkey` variant builds the daemon and
`pam-himmelblau` from the pinned fork and supports local terminal PAM
conversations. Its pinned fork commit includes the cached-PRT client fallback;
intuneme applies no source patches. The standard image continues to use signed
upstream packages.
For a locally built image, init detects the embedded source marker and records
its commit; a caller needs only the existing `--himmelblau-local-image` flag.
The option defaults to true; when enabled in this variant's effective config,
`enroll` starts host
`xdg-dbus-proxy`, filters the host system bus to `org.bluez`, and bind-mounts
only that proxy directory read-only at `/run/intuneme/bluez-proxy`. It sets
`DBUS_SYSTEM_BUS_ADDRESS` only for the foreground `pamtester` process and stops
the proxy after the PAM attempt; it never exposes the host system bus socket to
container services. This requires an active host BlueZ service and an adapter,
and it is only supported from an interactive terminal. The QR step may follow
an Entra password prompt, depending on the account and tenant policy; enabling
`enable_passwordless_qr_bluetooth` does not skip earlier authentication steps.

`user-map` reuses the host-matched local account, so `~/Intune` and its existing UID/GID/groups remain authoritative. The Entra UPN therefore inherits the same local sudo and host authority; nspawn does not isolate container root from host root. There is no TPM device passthrough; Himmelblau's default TPM policy can fall back to software TPM. Broker D-Bus activation, compliance behavior, and device enrollment inside nspawn remain experimental. See the user-facing [Himmelblau guide](../../site/user-guide/himmelblau.md).

## `intuneme enroll`

Interactive first-login enrollment for the experimental Himmelblau image.

**Usage:** `intuneme enroll --user user@example.com` (or omit `--user` to prompt in a terminal)

**Flow:**

1. Verify the container is initialized in Himmelblau mode and the image contains executable `/usr/bin/pamtester`; the container itself may be stopped
2. Validate the UPN and write the host-local username to Entra UPN map at `/etc/himmelblau/user-map`
3. If stopped, boot through `intuneme start` with the map already written. If running with a new mapping, reboot and wait for `himmelblaud`, `himmelblau-tasks`, and their sockets; the daemon snapshots its name-reservation list at startup. If already mapped and running, wait for those services without rebooting.
4. Restrict the `machine-shell` PAM allowlist to root and verify the mapped local username is not bypassed
5. Run `/usr/bin/pamtester machine-shell <host-user> authenticate` through the foreground nsenter helper as the host-matched local user; this invokes Himmelblau for the mapped identity and preserves the helper's real exit status
6. Only after PAM succeeds, open the normal local-user shell. When it exits, verify the local account retains the host UID, persist the UPN, enable linger, and wait for the session bus

`machinectl shell` is polkit-authorized and can open a shell without authenticating the user; it also reports success when the remote command fails, so it must not be used for the PAM check. The foreground nsenter helper preserves `pamtester`'s real status, including Ctrl-C. Do not replace the explicit PAM call with `su`: the general `su` stack permits local Unix-password fallback.

The root-owned app-launch helper continues to run as the local host-matched username. The user map makes PAM/NSS associate that local identity with the Entra account; `start` reinstalls the user map and corresponding PAM allowlist from config before boot.

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
4. **Prepare broker proxy** (if enabled) — Create runtime directory, add bind mount
5. **Validate sudo** — Prompt for password if needed (`nspawn.ValidateSudo()`)
6. **Write display marker** — Write host `$DISPLAY` to `rootfs/etc/intuneme-host-display` (read by `intuneme-session-setup` on login and on every app launch)
7. **Override X11 tmpfiles rule** — `provision.OverrideX11Tmpfiles()` writes `/etc/tmpfiles.d/x11.conf` into the rootfs: the vendor `x11.conf` minus its `/tmp/.X11-unix` directive. That directory is bind-mounted from the host, and the vendor `D!` type would empty it on every container boot, costing the host its Xwayland socket. Fails the start if it cannot be written. See [overview.md](overview.md) → "The X11 Socket Directory Is the Host's"
8. **Boot container** — `systemd-nspawn` with all bind mounts, DRI device binds plus explicit `DeviceAllow=<dev> rwm` rules, Nvidia device binds with explicit `DeviceAllow`, `--boot` flag. For Himmelblau mode, also bind the host's `/run/udev/data` to the same path read-only when present, so compliance checks can identify filesystem types from host block-device metadata.
9. **Wait for registration** — Poll `machinectl` up to 30 seconds until container is listed
10. **Clean stale Nvidia symlinks** — Always runs (even on non-Nvidia boots) to remove symlinks from previous sessions
11. **Setup Nvidia libraries** (if detected) — Create symlinks in container's `/usr/lib/x86_64-linux-gnu/` → `/run/host-nvidia/<index>/`, then run `ldconfig`
12. **Install udev rules** — YubiKey (`70-intuneme-yubikey.rules`) and video (`70-intuneme-video.rules`) hotplug rules + helper script (`/usr/local/lib/intuneme/usb-hotplug`)
13. **Ensure sudoers**: Reinstall sudoers rule and nsenter helper if either is missing (handles upgrades from the old wildcard-only rule; `IsInstalled` requires both)
13b. **Ensure session scripts** — Unconditionally reinstall `/usr/local/bin/intuneme-session-setup` + `profile.d/intuneme.sh` (`provision.InstallSessionScripts`, idempotent), so the rootfs copy always matches the embedded one and CLI upgrades reach existing containers without a `recreate`
14. **Reconcile user groups** — `provision.EnsureUserGroups()` reads the container user's groups via `id -nG` (run as root inside the container's mount namespace via nsenter) and runs `usermod -aG <group> <user>` for any missing group in `requiredRuntimeGroups()` (currently just `plugdev`, required for pcscd access per issue #146). Idempotent; warns and continues on failure.
15. **Forward existing YubiKeys** — Scan sysfs for Yubico vendor ID `1050`, forward USB device nodes + associated hidraw devices
16. **Forward existing video devices** — Glob `/dev/video*` and `/dev/media*`, forward each with `0660 root:video` permissions
17. **Start broker proxy** (if enabled):
    - Enable systemd linger for container user
    - Create login session via `machinectl`
    - Wait for session bus socket to appear
    - Launch `intuneme broker-proxy` as background process with PID file
    - Poll PID file to verify broker proxy started (up to 5 seconds)

## `intuneme stop`

Graceful shutdown via `runStop()` (shared between `stop` command and internal use).

**Flow:**

1. **Stop broker proxy** (if enabled) — Kill process by PID file, remove PID file
2. **Remove udev rules** — Delete rules files, helper script, and state dir (`/run/intuneme/devices`), reload udev (idempotent)
3. **Power off container and wait** — `nspawn.StopAndWait()`: `machinectl poweroff <machine>`, then poll `machinectl show` every 500ms for up to 60 attempts (30 seconds max) until the machine deregisters from `systemd-machined`

## `intuneme destroy`

Removes container and host modifications. By default preserves user files in `~/Intune`; with `--all` performs a full uninstall.

**Flags:** `--all` (full uninstall — remove all intuneme artifacts including GNOME extension, D-Bus service, and `~/Intune`)

**Flow (default):**

1. **Stop broker proxy** (if enabled) — Kill process by PID file
2. **Stop container** if running — `nspawn.StopAndWait()`
3. **Remove udev rules** — Delete hotplug rules and helper script via `udev.Remove()` (graceful, handles missing files)
4. **Remove polkit rule** — Delete `/etc/polkit-1/rules.d/50-intuneme.rules`
5. **Remove sudoers rule + helper**: Delete `/etc/sudoers.d/intuneme-exec` and `/usr/local/libexec/intuneme/nsenter-exec`
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

Updates the selected container image while preserving its enrollment state. The image family cannot be switched with `recreate`.

**Flags:** `--insiders` (switch Microsoft image to insiders; unavailable for Himmelblau), `--tmp-dir` (temp directory for image extraction)

**Flow:**

1. **Early validation** — Verify initialized, validate sudo access
2. **Stop container** if running — stops broker proxy first (if enabled), then `nspawn.StopAndWait()`
3. **Backup state:**
   - Password hash from container's `rootfs/etc/shadow` (`provision.BackupShadowEntry()`)
   - Device broker state from `rootfs/var/lib/microsoft-identity-device-broker` for Microsoft mode, or Himmelblau daemon, policy, and NSS state under `/var/lib` and `/var/cache` for Himmelblau mode. The daemon's `DynamicUser` state restores under `/var/lib/private/himmelblaud` and `/var/cache/private/himmelblaud`; restoring ordinary public directories collides with systemd's symlink setup. Protected HSM PIN inspection retries through `sudo` when the host user cannot traverse the private state directory. If the source has `/var/lib/systemd/credential.secret`, backup verifies a regular copy exists in the staged directory; it also rejects a staged encrypted PIN without its key before the old rootfs is deleted.
4. **Delete old rootfs** — `sudo rm -rf`
5. **Pull and extract new image** — Same as init step (channel can be switched via `--insiders` flag)
6. **Re-provision** — GPU render group, user creation, hostname (`<host>LXC`), fixups, polkit rule
7. **Restore state:**
   - Write backed-up password hash into new `rootfs/etc/shadow`
   - Copy backed-up device broker state back into `rootfs/var/lib/microsoft-identity-device-broker`
   - For Himmelblau, restore daemon/policy/NSS state and `credential.secret`; verify the restored key is a regular file byte-for-byte equal to the staged copy before deleting the backup
8. **Update config** — Save the selected channel while retaining the initialized image family

Note: `recreate` reinstalls the host polkit rule but does NOT reinstall the host sudoers rule — `start` handles that idempotently.

For Himmelblau, `recreate` refuses before deleting the rootfs if the encrypted
HSM PIN exists but its systemd credential key is missing. That credential
cannot be copied to the fresh image in a usable state; see [the recovery
procedure](../../site/troubleshooting.md#himmelblau-fails-with-failed-to-determine-local-credential-key).
If an incomplete staged backup or failed destination-key verification is
detected during restore, `recreate` returns an error and retains the backup
instead of reporting success.

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

**Output fields (JSON keys):** `initialized` (bool), `root` (string), `rootfs` (string), `machine` (string), `container` ("running"/"stopped"), `channel` ("stable"/"insiders"), `broker_proxy` ("running (PID X)"/"not running", omitted if disabled)

Supports `--json` for machine-readable output via `clix.OutputJSON()`.

## `intuneme shell`

Opens an interactive bash login shell inside the container.

Uses `machinectl shell <user>@<machine> /bin/bash --login` for the ordinary host-matched container user. The login shell sources `/etc/profile.d/intuneme.sh`, which in turn sources the shared `/usr/local/bin/intuneme-session-setup` to set up DISPLAY, audio, the D-Bus activation environment, and the keyring.

## `intuneme open edge` / `intuneme open portal`

`open edge` launches Microsoft Edge via `nspawn.Exec()` as the host-matched local container account; Edge is included only in the Microsoft image. `open edge` and `open portal` reject Himmelblau mode because neither app is included there. Use `intuneme enroll --user <UPN>` for Himmelblau sign-in and enrollment. Because the nsenter path is a *non-login* shell, `nspawn.Exec()` runs `/usr/local/bin/intuneme-session-setup` before launching Microsoft-image apps — this gives the D-Bus-activated identity broker a DISPLAY and an unlocked keyring, without which authentication fails.

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

The extension provides a Quick Settings toggle (start/stop), status display, and shortcuts for shell, Edge, and identity sign-in. The sign-in action opens Intune Portal in the Microsoft image and runs `intuneme enroll` (explicit Himmelblau PAM authentication followed by the local shell) in the experimental image. It monitors container state via systemd-machined D-Bus signals with a 5-second polling fallback.

## `intuneme broker-proxy`

Runs the D-Bus broker proxy in the foreground. This is the daemon process that forwards `com.microsoft.identity.broker1` method calls from the host session bus to the container session bus.

Normally launched automatically by `start` (as a background process) or by D-Bus service activation. Can be run manually for debugging.

See [Broker Proxy](broker-proxy.md) for the full proxy architecture.

## `intuneme gendocs` (hidden)

Generates markdown CLI reference pages for the MkDocs documentation site.

**Usage:** `intuneme gendocs <output-dir>`
