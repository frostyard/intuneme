# intuneme — Developer Overview

## Purpose

intuneme is a Go CLI tool that provisions and manages a `systemd-nspawn` container running Microsoft Intune on immutable Linux hosts. It isolates Intune Portal, Microsoft Edge, and the Microsoft identity broker inside a container while providing transparent access to host display, audio, GPU, and USB devices via bind mounts and namespace forwarding.

The tool handles the full container lifecycle — init, start, stop, destroy, recreate — with minimal host modifications (a polkit rule, a sudoers rule, and udev rules while running). `destroy --all` performs a full uninstall of all artifacts.

## Architecture

```
cmd/                  CLI command definitions (cobra commands)
├── extension/        Embedded GNOME Shell extension files
internal/
├── broker/           D-Bus broker proxy (container→host SSO forwarding)
├── busproxy/         Shared code for proxies on the container bus (dial, PID file)
├── notify/           Desktop notification proxy (container→host notifications)
├── screencast/       ScreenCast portal proxy (screen sharing from the container)
├── config/           Config struct + TOML load/save (~/.local/share/intuneme/config.toml)
├── nspawn/           systemd-nspawn wrapper (boot, stop, exec, shell, bind mounts)
├── prereq/           Prerequisite checks (systemd-nspawn, machinectl)
├── provision/        Container provisioning (user, fixups, password, polkit, SELinux, backup/restore)
├── puller/           OCI image pull + extraction (podman → skopeo+umoci → docker)
├── nvidia/           Nvidia GPU detection, library bind mounts, and container-side symlink setup
├── runner/           Command execution abstraction (mockable interface)
├── sudo/             Helper for writing files via temp file + sudo install (used by provision, nspawn, udev)
├── sudoers/          Sudoers rule install/remove for passwordless nsenter
├── udev/             Udev rules + hotplug script for YubiKey and video devices
└── version/          Build version + OCI image ref resolution
ubuntu-intune/        Container image definition
├── Containerfile     Multi-stage build (Ubuntu 24.04 base)
├── build_files/      Build script (package install, PAM config, patches)
└── system_files/     Static config files copied into image
polkit/               Polkit rule reference for machinectl access (actual rule generated inline)
scripts/              Build helpers (completions, manpages, SELinux, desktop item installer)
site/                 MkDocs documentation site content (user-facing)
mkdocs.yml            MkDocs config (materialx theme, published to GitHub Pages)
```

### Component Responsibilities

| Component | Role |
|-----------|------|
| **Go CLI** (`cmd/`, `internal/`) | Container lifecycle, host-specific setup (user creation, hostname, polkit, sudoers) |
| **Container image** (`ubuntu-intune/`) | Static content: packages, systemd overrides, PAM config, Edge wrapper |

**Rule of thumb:** If something is static and doesn't depend on the host, it belongs in `ubuntu-intune/`. If it depends on the host user/UID/hostname, it stays in `internal/provision/`.

## Key Patterns

### Rootfs Preparation and Upgrade Repair

`cmd.prepareInitRootfs` creates the extraction target, explicitly sets mode
`0755` without relying on the umask, extracts through a `puller.Puller`, and
checks `puller.VerifyRootOwned` before reconciling the directory itself.
`puller.ReconcileRootfs` resolves a symlinked path, checks the resolved
directory with `Lstat`, and runs `sudo chown -h 0:0 -- <path>` and
`sudo chmod 0755 -- <path>` only when ownership or mode differs. Container
services such as `messagebus` must traverse `/`; a rootfs created under umask
`077` otherwise leaves `/` inaccessible and the system bus cannot start.
`recreate` also explicitly sets the initial mode and reconciles after extraction.

Preparation keeps the initial `Lstat` metadata. Only an absent target or an
empty directory owned by the running UID counts as its own: init applies the
initial user `chmod` and removes that target if preparation fails. An empty
directory of another owner is accepted, but init never applies the initial
user `chmod` or removes it; reconciliation repairs its metadata after extraction.
Cleanup first uses `os.Remove`. Only `ENOTEMPTY` or `EEXIST` permits the
`sudo rm -rf` fallback for a partial extraction. Other removal errors only
warn; all cleanup errors preserve the original failure. A pre-existing
non-empty rootfs or symlink requires `--force`; extraction still runs over it,
and failure never removes it. A directory that cannot be read because of
permissions counts as an existing installation, so `--force` can proceed.
A custom rootfs has the same rules. Verification is injectable for tests;
production uses `VerifyRootOwned`.

On every non-dry-run `start`, the same reconciliation follows
`refreshStartSecurity` and precedes `IsRunning`, so both cold boots and the
already-running return repair old directory metadata. Failure is fatal without
a problem record: setup and sudo errors are not internal failures.

### Command Execution Inside the Container

`nspawn.Exec()` enters the container's namespaces and runs commands as the user via a single root-owned helper, `/usr/local/libexec/intuneme/nsenter-exec <script>`, invoked through passwordless `sudo`. The helper is a copy of the intuneme binary dispatched by `argv[0]` (`internal/nshelper`). Its machine, user, UID, home, and rootfs are baked into a root-owned TOML config (`nsenter-exec.toml`) at install time; **the caller supplies only the script**.

At runtime the helper resolves the container leader from the baked machine name itself, verifies that the leader is in the host's initial user namespace and that `/proc/<pid>/root` is the configured rootfs, unshares `CLONE_FS` on a locked Go OS thread, enters the container's mount/UTS/IPC/net/PID namespaces, sets `no_new_privs`, clears capability bounding/ambient/inheritable sets, drops to the numeric configured UID/GID (supplementary groups come from the container's `/etc/group`), and only then execs `/bin/bash -c <script>`. It never runs `su`, never reads the UID from the container's `/etc/passwd`, and never executes a container-controlled binary while privileged. This matters: accepting a caller-supplied PID previously let a user-created user+mount namespace with a fake `/bin/su` run as host root; a container-root user could also set their passwd UID to 0 and have `su` execute as root. The numeric UID must be nonzero and neither UID nor GID may be the kernel sentinel `UINT32_MAX`. Setuid and file-capability execution cannot recover privileges. `TestHelperKernelBoundary` runs real namespace transitions and verifies descendant IDs, capabilities, namespace identities and exit status. See [ADR-0001](../adr/0001-make-helper-privilege-drop-irreversible.md).

This indirection exists because **sudo-rs** (the default `sudo`/`su` on Ubuntu 25.10+) forbids wildcards inside command arguments. Fixed wildcard-free helper paths satisfy sudo-rs without exposing a caller-controlled PID. The helper binary and config are installed root-owned (`0755`/`0644`) and are not user-writable; `start` reinstalls both helpers (`nsenter-exec` and `machine-exec`), their config and the sudoers rule on every run, so upgrades from the old shell-script helper self-heal. The script the helper runs looks like:

```
export DISPLAY=... XAUTHORITY=... WAYLAND_DISPLAY=... PIPEWIRE_REMOTE=... \
  PULSE_SERVER=... XDG_RUNTIME_DIR=... DBUS_SESSION_BUS_ADDRESS=... \
  [__NV_PRIME_RENDER_OFFLOAD=1 __GLX_VENDOR_LIBRARY_NAME=nvidia]
/usr/local/bin/intuneme-session-setup
nohup <app> >/dev/null 2>&1 &
```

The script conditionally sets Wayland, PipeWire, PulseAudio, Nvidia, and D-Bus variables based on detected host sockets and GPU. It then runs `intuneme-session-setup` (see [Session Setup](#session-setup-shared-between-login-and-launch-paths)) before launching the app. Because this is a non-login shell, that script is what pushes `DISPLAY`/`XAUTHORITY` into the D-Bus activation environment (so the GTK identity broker can start) and unlocks the keyring. Without it the broker crashes on activation and authentication fails.

A sudoers rule at `/etc/sudoers.d/intuneme-exec` makes the helper passwordless so the GNOME extension can launch apps without a terminal. `sudoers.Install` writes the helper binary, its config, and the rule; `start` reinstalls them on every boot so CLI upgrades and config changes reach existing installs. `sudoers.IsInstalled()` requires the binary, config, and rule to exist. See [AGENTS.md](../../AGENTS.md) for why alternatives (`machinectl shell`, `systemd-run`) don't work.

`nspawn.ExecForeground()` is the foreground sibling of `Exec()`: same helper invocation (so it reuses the same sudoers rule), same session prologue, but it runs the command with `exec` and the caller's stdin/stdout/stderr attached instead of `nohup … &`. This is what `intuneme mcp` uses to run an MCP server in the container with its stdio wired to a host client (e.g. VS Code); backgrounding or a TTY would break JSON-RPC framing. The shared session prologue is `buildSessionEnvScript()`; for the foreground path the `intuneme-session-setup` output is sent to stderr so stdout carries only protocol traffic.

### Bind Mount Strategy

| Type | Host Path | Container Path | Lifecycle |
|------|-----------|----------------|-----------|
| Home directory | `~/Intune` | `/home/<user>` | Persistent (survives recreate) |
| X11 sockets | `/tmp/.X11-unix` | `/tmp/.X11-unix` | Always (see the tmpfiles note below) |
| Wayland | `$WAYLAND_DISPLAY` (see search order below) | `/run/host-wayland` | Auto-detected on start |
| PipeWire | `$XDG_RUNTIME_DIR/pipewire-0` | `/run/host-pipewire` | Auto-detected on start |
| PulseAudio | `$XDG_RUNTIME_DIR/pulse/native` | `/run/host-pulse` | Auto-detected on start |
| X11 auth | `$XAUTHORITY` (see search order below) | `/run/host-xauthority` | Auto-detected on start |
| GPU (DRI) | `/dev/dri/card*`, `/dev/dri/renderD*` | Same | Individual devices with explicit `DeviceAllow=<dev> rwm` |
| GPU (Nvidia) | `/dev/nvidia*` | Same | When Nvidia detected; explicit `DeviceAllow` |
| Nvidia libs | Host lib dirs (from `ldconfig`) | `/run/host-nvidia/<index>/` | Read-only; when Nvidia detected |
| Nvidia ICD | `/usr/share/vulkan/icd.d/nvidia_icd.json` etc. | Same | Read-only; when Nvidia detected |
| Proxy runtime | `~/.local/share/intuneme/runtime` | `/run/user/<uid>` | When any of the broker, notify or screencast proxies is enabled |
| MCP binary dir | dir of `mcp_binary` (or `--binary`) | `/opt/intuneme-mcp` (read-only) | Runtime-only; bound on demand by `intuneme mcp`, survives recreate |

### Device Hotplug Forwarding

YubiKeys and video capture devices (webcams) can be forwarded into the running container, both at start (already-plugged devices) and at runtime via udev hotplug rules.

**Device types:**

| Type | Detection | Udev Rule | Permissions |
|------|-----------|-----------|-------------|
| YubiKey (USB + HIDraw) | Scan sysfs for vendor `1050` | `70-intuneme-yubikey.rules` | `0666` (world-accessible) |
| Video (`/dev/video*`, `/dev/media*`) | Glob `/dev/video*`, `/dev/media*` | `70-intuneme-video.rules` | `0660 root:video` |

**Forwarding mechanism** (`udev.ForwardDevice()`):
1. Get container leader PID via `nspawn.LeaderPID()`
2. Get device major:minor via `stat`
3. Resolve the container's cgroup scope unit via `nspawn.MachineUnit()` (`machinectl show <name> -p Unit --value`) and add `DeviceAllow` to it dynamically (`systemctl set-property <unit> DevicePolicy=auto DeviceAllow=<dev> rwm`) — returns error on failure. **The unit name must be looked up, never guessed:** systemd < 256 names it `machine-<name>.scope` (created by machined), but systemd ≥ 256 has nspawn allocate its own scope named `<name>.scope`. nspawn boots with `DevicePolicy=closed`, so a device node that is mknod'd but not in `DeviceAllow` exists in the container yet every `open()` fails with `EPERM` — pcscd reports "No smart card readers found" and Edge's FIDO discovery sees nothing
4. Create the device node inside the container via `nsenter` + `mknod`
5. Set permissions (restrictive `0660 root:video` for video devices, `0666` for others)
6. Record in state directory (`/run/intuneme/devices/`) via `sudo.WriteFile()` for cleanup

**Udev hotplug flow:** The helper script at `/usr/local/lib/intuneme/usb-hotplug` (bash, rendered from `internal/udev/usb-hotplug.sh.tmpl`) is triggered by udev rules when devices are added/removed. It re-implements the same six steps as `ForwardDevice()` in shell (udev `RUN+=` handlers can't call the Go binary — no config, no sudo, tight timeout), including the `machinectl show -p Unit` lookup, and logs to the journal under the `intuneme-hotplug` tag. A `set-property` failure is logged loudly rather than swallowed, because it is the difference between a working and a dead device. Both paths locate the container's init process via the machine's leader PID for namespace entry.

The USB `remove` rule matches on kernel-supplied uevent variables (`ENV{DEVTYPE}=="usb_device"`, `ENV{PRODUCT}=="1050/*"`) and rebuilds the node path from `%E{BUSNUM}`/`%E{DEVNUM}` rather than relying on udev-db properties (`ID_VENDOR_ID`, `DEVNAME`), which proved unreliable on remove and left stale `/dev/bus/usb/BBB/DDD` nodes and state files behind after a re-enumeration. The hidraw remove rule uses `%k` (kernel name) and is unaffected.

### Nvidia GPU Support

On hosts with Nvidia GPUs, the container needs the device nodes and host userspace libraries (which must match the kernel module version exactly). This is handled by `internal/nvidia/` using a detect-at-start, bind-mount, symlink approach (similar to distrobox):

1. **Detection** — `nvidia.IsPresent()` checks for `/dev/nvidiactl`
2. **Device bind mounts** — `DetectDevices()` globs `/dev/nvidia*` and `/dev/nvidia-caps/*`. Each gets an explicit `--property=DeviceAllow=<dev> rwm` boot arg. DRI devices use the same explicit `rwm` pattern because nspawn's automatic DRI allowlist may grant only `rw`, which is insufficient for WebKitGTK auth-browser GBM/KMS buffer creation.
3. **Library discovery** — `HostLibraries()` parses `ldconfig -p` output for Nvidia libraries (x86-64 only), deduplicating by basename
4. **Library bind mounts** — `LibDirMounts()` maps unique host library directories to `/run/host-nvidia/0/`, `/run/host-nvidia/1/`, etc. (read-only). The indexed paths avoid basename collisions
5. **ICD files** — `HostICDFiles()` and `ICDMounts()` bind-mount Vulkan/EGL vendor JSON files at their standard paths
6. **Post-boot setup** — After boot, `CleanStaleLinks()` removes any symlinks from previous Nvidia sessions (always, even on non-Nvidia boots). Then `Setup()` creates symlinks in `/usr/lib/x86_64-linux-gnu/` pointing into `/run/host-nvidia/<index>/`, skipping package-owned regular files. Finishes with `ldconfig`
7. **Environment** — Both the profile script and `Exec()` set `__NV_PRIME_RENDER_OFFLOAD=1` and `__GLX_VENDOR_LIBRARY_NAME=nvidia` when `/run/host-nvidia` exists

### Wayland Socket Discovery

`HostWaylandSocket()` in `nspawn.go` resolves the host's Wayland socket from
`$WAYLAND_DISPLAY`:

1. Unset — assume `<runtime dir>/wayland-0`.
2. Absolute path — used as given.
3. Anything else — a socket name, joined onto the runtime dir.

The name must be read from the environment, never assumed. A compositor takes
the first free socket number, so Mutter usually lands on `wayland-0` while
Sway and Hyprland commonly use `wayland-1`. Assuming `wayland-0` makes
`DetectHostSockets()` find nothing, so the `--bind` is skipped,
`/run/host-wayland` never exists in the container, and
`intuneme-session-setup` leaves `WAYLAND_DISPLAY` unset. Edge then fails to
connect to any compositor, and GTK apps silently fall back to X11.

Both forms are accepted because the Wayland protocol permits either, and
libwayland takes an absolute path in `WAYLAND_DISPLAY` as-is.

`os.Stat` resolves the path, so a symlinked socket name works.

### The X11 Socket Directory Is the Host's

`/tmp/.X11-unix` is bind-mounted, not copied, so anything the container does
to that directory happens to the host. The base image's x11-common package
ships `/usr/lib/tmpfiles.d/x11.conf`, whose first directive is:

```
D! /tmp/.X11-unix 1777 root root 10d
```

Type `D` empties the directory when `systemd-tmpfiles` runs with `--remove`,
and the container's `systemd-tmpfiles-setup.service` runs exactly that at
every boot. With that directive in force, starting the container deletes the
host's Xwayland socket. Xwayland keeps the listening socket object and
already-connected clients survive, but the pathname is gone, so no new X11
client can connect anywhere on the host until the display server restarts. It
is a host-wide break, not an intuneme one.

The same file also carries `D!` directives for `/tmp/.ICE-unix`,
`/tmp/.XIM-unix` and `/tmp/.font-unix`, and an `r!` rule that unlinks
`/tmp/.X[0-9]*-lock`. Those paths are container-private and must keep working.
`/tmp/.X11-unix` is the only bind under `/tmp`, and nspawn gives the container
its own tmpfs on `/tmp`, so the rest of the file reaches nothing on the host.
Container `/proc/<leader>/mounts` shows both: a private tmpfs on `/tmp`, and
the host's tmpfs on `/tmp/.X11-unix` alone.

`provision.OverrideX11Tmpfiles()` therefore writes a copy of the vendor file
to `/etc/tmpfiles.d/x11.conf` in the rootfs, with the `/tmp/.X11-unix` line
dropped and every other directive reproduced. A file in `/etc/tmpfiles.d`
replaces the same-named file in `/usr/lib/tmpfiles.d`, so the copy wins as a
whole. `start` calls it before `nspawn.Boot()` and fails the start if it
cannot, since booting anyway costs the user their X11 session. Writing it on
every start also repairs a rootfs created by an older CLI without a
`recreate`.

Two tests hold the shape. `TestX11TmpfilesOverrideOmitsX11Unix` fails if any
directive names `/tmp/.X11-unix`.
`TestX11TmpfilesOverrideKeepsVendorDirectives` fails if one of the other four
paths goes missing.

Binding only the single socket file instead would scope the damage, but the
container would then be stuck with whichever socket existed at boot: a
directory bind picks up an Xwayland that starts later.

### X11 Authority File Search Order

`findXAuthority()` in `nspawn.go` locates the host Xauthority file:
1. `$XAUTHORITY` environment variable (if file exists)
2. Glob `/run/user/<uid>/.mutter-Xwaylandauth.*` (Mutter/GNOME Wayland)
3. Glob `/run/user/<uid>/xauth_*` (other Xwayland implementations)
4. `~/.Xauthority` (classic X11)

Note: Steps 2–3 hardcode `/run/user/<uid>` rather than reading `$XDG_RUNTIME_DIR`.

### Render Group GID Conflict Resolution

During provisioning, `EnsureRenderGroup()` matches the container's `render` group GID to the host's so DRI render devices work across the bind mount. If the target GID is already occupied by a different group in the container, that group is reassigned to a free system GID (999–100) before the render group is created or modified.

### Container Hostname

During provisioning (`WriteFixups`), the container hostname defaults to `<host-hostname>LXC` — e.g., if the host is `myworkstation`, the container gets `myworkstationLXC`. This prevents hostname collisions when both host and container are visible on the same network.

The optional `hostname` key in `config.toml` overrides that default. `config.Config.ContainerHostname(hostHostname)` returns the key value when it is set, and `<host-hostname>LXC` when it is empty. A set value is checked by `config.ValidateHostname` (RFC 1123, 1 to 64 bytes, dot-separated labels of ASCII letters, digits and `-`, no label starting or ending with `-`, no empty label, whitespace rejected). `init` and `recreate` call it right after `config.Load`, before the dry-run return and before any container work, so an invalid value stops the command with nothing changed — recreate never deletes the old rootfs for a bad value. `config.Load` does not validate, so `stop`, `destroy` and the rest still work with a bad value. `ProvisionContainer` takes the final host name and writes it unchanged.

Intune shows the container host name as the device name, so `recreate` reads the old `<rootfs>/etc/hostname` early, before the dry-run return and long before deleting the rootfs (`containerHostnameFromRootfs`, `warnIfHostnameChanges`), and prints a warning when the value differs from the new name: the tenant can show the device as renamed or new.

### Product Name

The optional `product_name` key controls the PC model that Intune reads from `/sys/class/dmi/id/product_name` (only that file: `modalias`, `sys_vendor` and `/sys/firmware/dmi/tables/DMI` still show the host values) ([issue #156](https://github.com/frostyard/intuneme/issues/156)). It is off by default because the presented model is a tenant policy matter. `config.ContainerProductName` accepts an empty key unchanged, or validates 1 to 64 bytes of printable ASCII (0x20..0x7E), without leading or trailing spaces. `runStart` validates after the rootfs check and before sudo, even in dry run, and then checks that the host has `/sys/class/dmi/id/product_name` (`nspawn.CheckHostProductName`), so a host without DMI fails before the password prompt; `config.Load` never validates it. A bad value and file preparation errors produce no problem record.

On a cold boot, `nspawn.WriteProductNameFile` writes `<root>/dmi/product_name` with the value plus a newline. The directory is explicitly 0700 and the file 0644 so container processes can read it. A temporary file plus rename replaces a final-path symlink without following it, and a `dmi` directory that is a symlink is refused (`MkdirAll` and `Chmod` would follow it). The source path must be absolute and must not contain `:` (the nspawn bind separator) or `\` (nspawn reads it as an escape). The host DMI file must exist: nspawn cannot create the target in read-only sysfs. A missing target fails with an instruction to remove `product_name`; `WriteProductNameFile` checks it again. `ProductNameMount` adds `--bind-ro=<root>/dmi/product_name:/sys/class/dmi/id/product_name` through the existing sockets list. An unset key writes no file and adds no boot argument; a file from an earlier boot stays in `<root>/dmi` and is not used; dry run prints the model without writing a file. An already-running container gets a next-boot message instead of a changed mount.

### Container User Groups

The container user is added to: `adm,sudo,video,audio` (plus `render` if a render group exists matching the host's render GID). The container-side sudoers rule at `/etc/sudoers.d/intuneme` contains only a comment; broker restart is authorized by the container polkit policy. Interactive `sudo` still requires the container password set by `init`. Do not broaden this to `NOPASSWD: ALL`: the host-side helper runs user commands outside nspawn, so unrestricted container sudo is effectively unrestricted host root.

### SELinux Support

On SELinux-enabled systems (Fedora, Bazzite — both enforcing and permissive modes), `InstallSELinuxPolicy()` during `init`:

1. Labels the rootfs tree as `container_file_t` via `semanage fcontext` + `restorecon -RF`. A non-zero `semanage fcontext -a` exit is ignored only when its output is exactly one non-empty line that contains "already defined" or "already exists" and does not contain "modifying instead" — the legacy error older semanage versions printed for an existing rule. Current semanage prints `File context for <path> already defined, modifying instead` and then re-adds the rule; a successful re-add exits zero, so that line in a non-zero exit means the later modify or commit step failed. Every other failure (missing tool, unmanaged policy store, modify or commit error) returns an error with the `semanage` output instead of letting `restorecon` apply the wrong label.
2. Installs a custom policy module (`intuneme-machined`) that allows `systemd_machined_t` to:
   - Open/read/write/ioctl PTY devices (`user_devpts_t`) — required for `machinectl shell`
   - Read symlinks in `/tmp` (`user_tmp_t`) — required for `/tmp/ptmx` traversal

A SELinux failure stops `init` before it saves `config.toml` and before it prints the success message. The error names the likely cause (`policycoreutils-python-utils`, which provides `semanage`) and tells the user to run `intuneme init --force`. The force flag is required because the rootfs already exists at that point, so a plain re-run refuses with "already initialized". The SELinux step is the last fallible step of `init`; `cmd.finishInit` owns it together with the config save and success message, and takes its SELinux detection and installer as injected dependencies so tests can prove that a failure never reaches either.

### Password Setting

Container password is set via a bind-mounted temp file to avoid shell injection: the CLI writes `user:password` to a host temp file, bind-mounts it read-only as `/run/chpasswd-input`, and runs `chpasswd < /run/chpasswd-input` inside the container via `systemd-nspawn`.

Password validation (both CLI-side and container PAM): minimum 12 chars, at least one digit, uppercase, lowercase, and special character, no username substring.

### Session Setup (shared between login and launch paths)

All session initialization lives in **`/usr/local/bin/intuneme-session-setup`** (embedded in the Go binary as `intuneme-session-setup.sh`, installed by `provision.InstallSessionScripts`). It is invoked from **both** session-entry paths so they behave identically:

- **`/etc/profile.d/intuneme.sh`** *sources* it on interactive login (`machinectl shell`), so the exported env lands in the login shell too.
- **`nspawn.Exec()`** *executes* it before launching a GUI app via nsenter. **This is the critical part:** the nsenter path is a *non-login* shell, so `/etc/profile.d` is never sourced. Before this script existed, the steps below ran only on login — which is why launching Edge from the GNOME extension (nsenter, no login) left the broker without a display and the keyring locked, so **authentication failed**.

The script (safe to source or execute; idempotent) does:

1. Sets `XDG_RUNTIME_DIR` / `DBUS_SESSION_BUS_ADDRESS` if unset, so it works from any caller
2. Reads `DISPLAY` from `/etc/intuneme-host-display` (written by `start`), defaults to `:0`
3. Extends `PATH`; sets `XAUTHORITY=/run/host-xauthority` if bind-mounted
4. Detects Wayland (`WAYLAND_DISPLAY`, and with it `XDG_SESSION_TYPE=wayland`: WebRTC in Edge captures screens through the ScreenCast portal only under that session type, otherwise it falls back to X11 capture and can share only tabs), PipeWire (`PIPEWIRE_REMOTE`), PulseAudio (`PULSE_SERVER`), Nvidia (`__NV_PRIME_RENDER_OFFLOAD` / `__GLX_VENDOR_LIBRARY_NAME`) from `/run/host-*`
5. Propagates the env with **both** `systemctl --user import-environment` **and `dbus-update-activation-environment --systemd`**. The latter is essential: the identity broker is a **GTK app that the session D-Bus daemon activates on demand**, and it inherits `DISPLAY`/`XAUTHORITY` only from the D-Bus activation environment. Without it the broker dies on startup with `cannot open display` and Edge cannot authenticate. When that call succeeds and `$XDG_RUNTIME_DIR/bus` exists, the script touches `$XDG_RUNTIME_DIR/intuneme-session-env`. The file is the gate for `intune-agent.timer` (see step 8).
6. Sets `WEBKIT_DISABLE_DMABUF_RENDERER=1` and `WEBKIT_DISABLE_COMPOSITING_MODE=1` — see below
7. Until the keyring is verifiably unlocked this boot (marker at `/tmp/.intuneme-keyring-init-done`), and only once the user session bus socket `$XDG_RUNTIME_DIR/bus` exists:
   - Ensures `~/.local/share/keyrings/default` points to `login`
   - **Kills all keyring daemons (`pkill -x gnome-keyring-d`) and starts exactly one** via `echo "" | gnome-keyring-daemon --unlock --components=secrets,pkcs11 -d`. The well-known `org.freedesktop.secrets` name is held by whichever daemon claimed it first (often the systemd socket-activated one); a `--replace --unlock` daemon does *not* take that name, so its unlock never reaches the daemon the broker talks to. Note `echo ""` (newline = empty password) — `printf ""` would send EOF = "no password" and create nothing.
   - Forces default-collection creation via `secret-tool` (without this, `ReadAlias("default")` returns `/` and the broker can't store credentials)
   - Re-reads the login collection's `Locked` property. **Only if it is `b false`** does it write the marker, remove the attempt counter, and continue to the restart below. Otherwise it leaves no marker, and the next session entry (another launch, `intuneme shell`) retries. Writing the marker unconditionally caused a real outage: a GNOME-extension launch straight after `start` ran before the container user's systemd manager existed, so the unlock failed (`couldn't create socket directory: /run/user/<uid>/keyring: Permission denied`), the marker was still written, and the keyring daemons D-Bus and socket activation started later came up locked for the rest of the boot. The broker then reported an empty token cache and asked the user to sign in again. `session_setup_test.go` runs the real script against stub binaries to pin this.
   - **Retries are capped at 3 per boot.** Every attempt kills the daemons and sleeps about 2 s, so an unlock that never succeeds would otherwise slow every app launch and every shell login for the rest of the boot. The count is kept in `/tmp/.intuneme-keyring-unlock-attempts` (tmpfs, so it resets at boot) and is incremented before each attempt. A failed attempt logs `keyring unlock attempt N of 3 failed`; at the limit the script also logs that it stops retrying for this boot and prints the recovery command (`rm -f /tmp/.intuneme-keyring-init-done /tmp/.intuneme-keyring-unlock-attempts; /usr/local/bin/intuneme-session-setup`). At the limit the `pkill`, the daemon start, and the `secret-tool` step are skipped, but the final `Locked` check still runs, so a keyring that another process unlocked is still marked. Lines go to stderr (the login shell) and to the journal via `logger -t intuneme-session-setup`, so `journalctl -t intuneme-session-setup` shows them. A verified unlock removes the counter file. The whole block holds an `flock` on `$XDG_RUNTIME_DIR/intuneme-keyring.lock` (a subshell, because the script is sourced and an `exit` must not close the login shell), so overlapping session entries cannot skip past the cap; the counter is written to a temp file and moved into place, so no reader sees a partial file.
   - Restarts **only** `microsoft-identity-device-broker.service` (system) using `systemctl --no-ask-password` and the container polkit rule in `ubuntu-intune/system_files/etc/polkit-1/rules.d/49-intuneme-broker.rules`. `InstallSessionScripts` installs the embedded canonical rule for existing containers. This avoids setuid sudo, which `no_new_privs` deliberately disables. The user-session `com.microsoft.identity.broker1` is **D-Bus-activated, not a systemd unit**, so it is not restarted — it re-activates with a fresh process (and now-correct environment) on the next call from Edge. (The previous `systemctl --user restart microsoft-identity-broker.service` always errored with "Unit not found".)
8. Starts `intune-agent.timer` for compliance checks if not already active. The timer's drop-in (`ubuntu-intune/system_files/etc/systemd/user/intune-agent.timer.d/override.conf`) carries `ConditionPathExists=%t/intuneme-session-env`, which is `$XDG_RUNTIME_DIR/intuneme-session-env` for a user unit. The Microsoft timer has `OnStartupSec=5m` with `RandomizedDelaySec=10m` and `OnUnitActiveSec=1h`, and the image enables it in `default.target`. With linger the user manager starts at container boot, so without the condition the agent could run before any session setup: it would activate the identity broker with no `DISPLAY` in the D-Bus activation environment, and the broker would die. The condition holds the timer back at boot; this script touches the file (step 5) and then starts the timer here. The drop-in ships only in the image, so a container created before it needs `intuneme recreate` to get the gate (the script itself is reinstalled by every `start`).

#### The user session must exist before the first launch

The nsenter path never creates a logind session, so by itself nothing starts
the container user's `user@<uid>.service`, `/run/user/<uid>`, or the session
bus. Without them, `systemctl --user`, `dbus-update-activation-environment`,
and the keyring unlock all fail silently. `start` therefore enables **linger**
for the container user on every run (`enableUserLinger` in `cmd/start.go`,
`sudo systemd-run --machine=<machine> --wait --pipe --quiet --collect
/bin/loginctl enable-linger <user>` under start's authenticated sudo session),
both after boot and on the already-running return. Linger used to be enabled
only when a proxy was configured, and that gap is what left an extension-first
launch with no session. The proxies still additionally open the `sleep infinity`
login session (`broker.LoginSessionArgs`).

Linger runs right after the machine registers, before the Nvidia, udev, group,
YubiKey and video steps. At that moment the container's system bus and logind
may not answer yet, so `enableUserLinger` uses a 30-second total budget for
starting attempts, including command time and 1-second retry delays. It starts
no retry at or after the deadline. An in-flight attempt can finish after the
deadline because a D-Bus call can block for about 25 seconds. The degraded
worst case is therefore about 55 seconds: the 30-second budget plus 1 blocked
attempt. A final failure is fatal. It uses `systemd-run --wait` because that returns the
exit status of the inner `loginctl`; `machinectl shell` does not report it, so
an inner failure would look like success. The error text carries the command
output.

The launch prologue in `buildSessionEnvScript` also waits, bounded, for the user
session bus: `userBusWaitScript` polls `$XDG_RUNTIME_DIR/bus` for up to 30
seconds before it runs `intuneme-session-setup`. The GNOME extension can launch
an app while `start` is still enabling linger, or while `user@<uid>` is still
starting after boot. In the degraded 55-second linger case, this 30-second
user-bus wait can end first and fail the launch even if linger later succeeds.
This separate wait does not extend the linger retry budget in `cmd/start.go`. If the bus never appears the launch **fails**: the prologue
writes one line to stderr and exits 1, rather than start an app or an MCP server
without a session bus, where the broker cannot authenticate and the keyring
stays locked. `Exec` returns that line in its error, so `intuneme open` shows it.
Nothing in the prologue writes to stdout, because a foreground stdio server
(MCP) uses stdout for JSON-RPC.

`start` reinstalls the script via `provision.InstallSessionScripts` on **every**
boot, unconditionally, so the copy in the rootfs always matches the embedded
one. It used to install only when the file was *missing*, which froze the
script at whatever CLI version first created the container: later fixes (the
WebKit workaround below, for instance) could not reach an existing container
without a full `recreate`. `InstallSessionScripts` is idempotent, so
re-running it costs two small writes.

#### WebKitGTK sign-in window

The identity broker renders its interactive sign-in window with **WebKitGTK
4.1** (`libwebkit2gtk-4.1`). Because the script exports `WAYLAND_DISPLAY`, GTK
selects the Wayland backend and WebKit talks straight to the *host*
compositor, handing it dmabufs produced by the container's Mesa across the
container boundary. On a GNOME/Wayland host that paints nothing: the window
maps, the web process starts and even activates at-spi, nothing is logged —
and the page stays blank white, so sign-in is impossible.

This is not a driver fault. Inside the container `gbm_create_device()` and
`eglInitialize()` on `/dev/dri/renderD128` both succeed (Mesa 25.2.8, EGL
1.5), and no EGL/GBM/dmabuf error appears in the container journal. The
failure is in the buffer handoff, so forcing WebKit onto its SHM path fixes
it. Only the auth window uses WebKit in this container — Edge is Chromium and
unaffected — so the lost acceleration costs nothing.

Both variables **must** appear in the script's `_env_vars` propagation list,
not merely be exported: the broker is D-Bus activated, so a plain `export`
never reaches it. `TestSessionSetupPropagatesWebKitWorkaround` guards exactly
that distinction.

### State Preservation Across Recreate

`recreate` updates the container image while preserving enrollment:
1. Backs up password hash (from `rootfs/etc/shadow`) and device broker state (from `rootfs/var/lib/microsoft-identity-device-broker`)
2. Deletes old rootfs, pulls new image, re-provisions
3. Restores password hash and broker state into the new rootfs

Enrollment data (Intune database, app state) persists via the `~/Intune` bind mount. The keyring is unlocked fresh on every boot (marker file in `/tmp`, written only after a verified unlock).

### CLI Framework (clix + reporter)

The CLI uses `frostyard/clix` and `frostyard/std/reporter` for consistent output and common flags:

- **Reporter** — A `reporter.Reporter` (initialized in `PersistentPreRunE` via `clix.NewReporter()`) is passed through the command tree and into `internal/` packages. All user-facing output goes through `rep.Message()` and `rep.Warning()` rather than direct `fmt.Print` calls.
- **DryRun** — `clix.DryRun` flag enables dry-run mode across commands.
- **Verbose** — `clix.Verbose` flag enables verbose output.
- **JSON output** — `clix.OutputJSON()` provides `--output json` support (used by `status`).

### Problem Records

intuneme records its own problems in the local systemd journal through `github.com/frostyard/std/problems` (frostyard/core ADR-0069, schema `frostyard.problem/1`). The wrapper is `internal/problemrec`; the decision is [ADR-0003](../adr/0003-record-problems-with-std-problems.md).

- **Where the recorder is made.** `main()` calls `problemrec.Init(version, problemOpts...)` after the `nsenter-exec`/`machine-exec` dispatch and after `pkgversion.Version = version`, then `defer rec.RecoverPanic()` and `_ = rec.CrashOutput()`. The helpers return through `os.Exit` before that point, so the root helpers (run under sudo) never make a recorder, never write a crash file and never record. `RecoverPanic` must be deferred directly (Go's `recover` rule); it records the panic and panics again, so a panic still prints the original trace (with `[recovered, repanicked]`) and exits 2. A command error still exits 1 through `os.Exit(1)`, which runs no deferred call.
- **Crash output.** `CrashOutput` sets `runtime/debug.SetCrashOutput` to a locked crash file under `$STATE_DIRECTORY`, else `$XDG_STATE_HOME`, else `~/.local/state`, in `frostyard-problems/org.frostyard.intuneme/`. A panic in another goroutine (the proxies run goroutines) or a fatal runtime error leaves that file; the next intuneme run turns it into a `panic` record and removes it (at most 8 per start). A file of a clean exit holds no crash and is removed with no record. A `CrashOutput` error is ignored: it never changes output or exit status.
- **Coded errors.** `problemrec.Error(code, err)` is called with the original error just before the return, at 4 points: `start.boot_failed` (`nspawn.Boot`, or no machine after the 30-second wait), `start.linger_failed` (`enableUserLinger`, on the already-running path and the cold-boot path), `start.proxy_failed` (`startProxy` for each of the 3 proxies, at the call site in `runStart`), `open.session_bus_timeout` (`nspawn.Exec` in `runOpen` fails with `errors.Is(err, nspawn.ErrSessionBusTimeout)`).
- **Rule: only internal failures after validation and authentication.** A code is recorded only for an internal failure after config validation and sudo authentication passed; user setup and authentication errors are never recorded. So the config load, the rootfs check, `sudo -v` and `refreshStartSecurity` get no record: `refreshStartSecurity` fails for a bad config identity (`host_uid = -1` gives "invalid host uid") or a user-owned helper source, too. The 3 `start` codes run only after `sudo -v` and `refreshStartSecurity` passed, and dry run never reaches them. `open` records only the session bus timeout: every other `nspawn.Exec` failure can be a missing sudoers rule or a sudo password prompt. `Exec` returns an `execError` that adds no text; its `Is` matches `nspawn.ErrSessionBusTimeout` only when 1 output line, trimmed, is exactly the diagnostic for the `uid` of the call (`isSessionBusTimeout`: `^intuneme: the container user session bus \(/run/user/<uid>/bus\) is not ready after [0-9]+ s; run 'intuneme start' and try again$`). A substring match is wrong: sudo quotes the submitted script, which holds the `echo` of that message, in a policy-denial message. The pattern and `userBusWaitScript` are built from the same constants (`sessionBusTimeoutPrefix`, `sessionBusTimeoutMiddle`, `sessionBusTimeoutTail`), and `TestUserBusWaitDiagnosticMatches` runs the real script against the matcher.
- **Rule: only a consistent identity.** Every record in `runStart` and `runOpen` goes through `recordInternal(cfg, code, err)`, which records only when `identityConsistent(cfg)` (`cmd/problems.go`) holds: `host_uid` is greater than 0, equal to `os.Getuid()` and below `MaxUint32`; `host_user` is not empty, not `root`, and has no `:` or whitespace; and `<rootfs>/etc/passwd` (opened with `O_RDONLY|O_NOFOLLOW|O_NONBLOCK|O_CLOEXEC`, rejected before any read unless `Stat` says it is a regular file, so a FIFO or a symlink cannot block or redirect the read; at most 1 MiB) has exactly 1 line whose field 1 is `host_user`, and that line is a strict entry (`passwdEntryMatches`): exactly 7 fields, field 3 the exact `strconv.Itoa(host_uid)` (no sign, no leading zero), field 4 a GID that `strconv.ParseUint(s, 10, 32)` accepts and that is not `MaxUint32`, and field 6 an absolute home. A malformed or duplicate entry for the user, or any read or parse error, gives false: a lost record is better than a wrong one. With an inert recorder (a dev build, or none) `recordInternal` returns before the check, so it reads no file. This closes 2 user-setup paths that pass `refreshStartSecurity`: a `host_user` with no container account (linger then fails), and `host_uid = -1` on `open` (the prologue waits for `/run/user/-1/bus` and times out). The returned errors and `rep.Message` output are unchanged. Codes are a stable contract: add new ones, never rename or reuse one.
- **Fields.** `MESSAGE` holds the error text or panic value (local journal only). The `FROSTYARD_*` fields hold the app ID, version, runtime `go`, component `cli`, class, and the code or the panic frames (function names mapped by `problems.MapFrame`: no path, line, offset or argument; a non-frostyard, non-stdlib frame becomes `other`).
- **Version rule.** `problemrec.ReleaseVersion` trims 1 leading `v`: GoReleaser stamps `0.21.0`, `make build` stamps `git describe` output such as `v0.21.0`. A `dev`, `-N-gHASH`, `-dirty` or snapshot version fails the schema's semver pattern, so the recorder is inert: it records nothing and makes no crash file.
- **Rate cap.** std allows 10 records a minute per process, shared by all recorders. A record that the journal socket cannot take at once is dropped; nothing blocks or retries.
- **No network.** A record is 1 datagram to `/run/systemd/journal/socket`. intuneme sends nothing anywhere else and adds no flag or output; chairlift is the only sender, after the user opts in. `TestNoNetworkImports` (in `internal/problemrec`) parses the package and fails on a network import.
- **Tests.** `internal/problemrec/problemrectest` binds a `unixgram` test socket in `t.TempDir()` (`Use` installs a recorder that writes to it and restores the old one; `Check` asserts the fields and runs `problems.Validate`). `cmd/problemrec_test.go` triggers each code through the existing runner mocks, and proves each exclusion with the test socket and `ExpectNone` (`TestStartUserErrorsAreNotRecorded`, `TestStartLingerFailureWithoutContainerAccountIsNotRecorded`, `TestOpenBadHostUIDTimeoutIsNotRecorded`, `TestOpenSudoFailureIsNotRecorded`), never with a nil recorder. The positive tests use `identityRoot`, which writes a consistent config and `rootfs/etc/passwd`; `TestIdentityConsistent` covers the guard. `main_test.go` re-executes the test binary (`INTUNEME_TEST_PROBLEM_CHILD`) to check a forced panic (exit 2, 1 record), a goroutine panic recorded from its crash file at the next start, and a dev build that records nothing. Because the rate cap is per process and cannot be reset, a test package must write at most 10 records; every other test keeps the nil recorder, which is inert.

### Sudoers Rule Safety

The `internal/sudoers/` package writes the sudoers rule via a safe workflow: write to a temp file, validate syntax with `visudo -c -f <tmpfile>`, then `sudo install` to `/etc/sudoers.d/intuneme-exec`. This prevents a broken sudoers file from locking out sudo. The rule authorizes exactly one root-owned helper path with no wildcards: `<user> ALL=(root) NOPASSWD: /usr/local/libexec/intuneme/nsenter-exec`. All target selection (machine, leader, rootfs, UID) is baked into the helper and its root-owned config, never supplied by the caller.

### Runner Abstraction

All shell commands go through the `runner.Runner` interface (`internal/runner/`), which is mockable for testing:

| Method | Behavior | Used for |
|--------|----------|----------|
| `Run()` | Captures combined output | Most commands (install, machinectl, nvidia detection) |
| `RunAttached()` | Stdin/stdout/stderr attached to terminal | Interactive commands (shell, password prompt) |
| `RunBackground()` | Starts detached, returns immediately | Broker proxy daemon |
| `LookPath()` | Checks if binary is in PATH | Puller tool detection |

### OCI Image Resolution

`version.ImageRef()` resolves the container image tag from the build version:
- Insiders channel → `ghcr.io/frostyard/ubuntu-intune:insiders`
- Clean semver (e.g., v1.2.3) → `ghcr.io/frostyard/ubuntu-intune:v1.2.3`
- Dev builds → `ghcr.io/frostyard/ubuntu-intune:latest`

### Image Pull Strategy

The puller detects available tools in order: podman → skopeo+umoci → docker. Each implements the `Puller` interface with `PullAndExtract()` to download the OCI image and extract the rootfs.

### Edge Wrapper

The container ships `/usr/local/bin/microsoft-edge` which wraps the real binary:
- Always adds `--disable-gpu-sandbox` (nspawn cannot create nested user namespaces; renderer sandbox stays active)
- The image ships `/etc/opt/edge/policies/managed/intuneme-command-line-flags.json`; `provision.InstallSessionScripts` reinstalls it on every `intuneme start`, so existing containers receive it with no recreate
- The flag stays because the GPU process sandbox crashes inside nspawn with `SIGTRAP`: it tries to create a user namespace (`CLONE_NEWUSER`), which nspawn restricts, and Chromium crashes before it can fall back to software rendering. The GPU process stays unsandboxed as before, and the policy also hides the banner for any other unsafe flag that a user adds
- When `WAYLAND_DISPLAY` is set: unsets `DISPLAY`, adds `--enable-features=UseOzonePlatform,WebRTCPipeWireCapturer --ozone-platform=wayland`

### GNOME Shell Extension

The embedded extension (`cmd/extension/`, UUID `intuneme@frostyard.org`, GNOME Shell 47–50) installs to `~/.local/share/gnome-shell/extensions/` and provides:
- Quick Settings toggle (start/stop container)
- Status display (container running/stopped, broker proxy state; when the PID file shows no proxy it also reports the owner of the broker bus name, see [Broker Proxy](broker-proxy.md))
- Menu shortcuts for shell, Edge, Intune Portal
- Monitors container state via `org.freedesktop.machine1` D-Bus signals with 5-second polling fallback
- Finds a terminal emulator (checks `$TERMINAL`, then ghostty/ptyxis/kgx/gnome-terminal/xterm)

## Configuration

Single TOML file at `~/.local/share/intuneme/config.toml`:

| Field | Type | Description |
|-------|------|-------------|
| `machine_name` | string | nspawn machine name (default: "intuneme") |
| `rootfs_path` | string | Path to extracted rootfs |
| `host_uid` | int | Host user's UID (matched in container) |
| `host_user` | string | Host username |
| `broker_proxy` | bool | Enable D-Bus broker proxy for host-side SSO |
| `notify_proxy` | bool | Forward container desktop notifications to the host |
| `screencast_proxy` | bool | Forward the container's ScreenCast portal to the host |
| `insiders` | bool | Use insiders channel image |
| `hostname` | string | Container host name; unset means `<host hostname>LXC` (validated by `config.ValidateHostname`, RFC 1123, max 64 characters; used by `init` and `recreate` only) |
| `product_name` | string | Optional PC model override; unset changes nothing. Printable ASCII, 1–64 bytes, no surrounding spaces; validated at use by `start`, applied at cold boot through a read-only DMI bind |

The `--root` persistent flag overrides the default data directory (`~/.local/share/intuneme`). `config.DefaultRoot()` returns `(string, error)` — it propagates `os.UserHomeDir()` errors rather than silently producing a relative path, preventing accidental destructive operations (e.g., `sudo rm -rf` on a relative path) when `$HOME` is unset.

### Key Dependencies

| Module | Version | Purpose |
|--------|---------|---------|
| `frostyard/clix` | v0.5.0 | CLI helpers (reporter, dry-run, verbose, JSON output) |
| `frostyard/std` | v0.4.0 | Standard library (reporter interface; `problems` for journal problem records) |
| `godbus/dbus/v5` | v5.2.4 | D-Bus bindings for the broker, notify and screencast proxies |
| `spf13/cobra` | v1.10.2 | CLI command framework |
| `BurntSushi/toml` | v1.6.0 | TOML config parsing |
| `charmbracelet/x/term` | v0.2.2 | Terminal password input |

Go version: 1.26

Note: The polkit rule (`50-intuneme.rules`) is generated inline by `provision.InstallPolkitRule()` and allows both `sudo` and `wheel` groups to open an interactive shell in a **local container** only (`org.freedesktop.machine1.shell`). Host shells, image management, and broad machine management are deliberately not granted: `stop` and the MCP bind go through the scoped root-owned `machine-exec` helper. The `polkit/` directory contains a reference copy of the same rule.

## Storage Layout

```
~/.local/share/intuneme/
├── config.toml          Configuration
├── rootfs/              Extracted container filesystem
├── runtime/             Proxy runtime dir (bind-mounted as /run/user/<uid>)
├── broker-proxy.pid     PID file for broker proxy process
├── notify-proxy.pid     PID file for notify proxy process
└── screencast-proxy.pid PID file for screencast proxy process

/run/intuneme/devices/   Udev forwarded device state (tmpfs, only while running)

~/.local/state/frostyard-problems/org.frostyard.intuneme/
└── *.crash              Crash output of a running release build ($XDG_STATE_HOME
                         if set); turned into a problem record at the next run

~/Intune/                Bind-mounted as container home directory
├── .config/intune/      Enrollment database
├── .local/share/intune/ App state
├── .local/state/microsoft-identity-broker/  Broker device state
├── .local/share/keyrings/  gnome-keyring data
├── Downloads/           User files (preserved on destroy)
└── ...
```

## Host Modifications

intuneme installs these on the host (all reversible via `destroy`):

| Artifact | Path | Installed by | Removed by |
|----------|------|--------------|------------|
| Polkit rule | `/etc/polkit-1/rules.d/50-intuneme.rules` | `init` | `destroy` |
| Sudoers rule | `/etc/sudoers.d/intuneme-exec` | `init` (reinstalled by `start`) | `destroy` |
| Udev rules (YubiKey) | `/run/udev/rules.d/70-intuneme-yubikey.rules` | `start` | `stop`, `destroy` |
| Udev rules (video) | `/run/udev/rules.d/70-intuneme-video.rules` | `start` | `stop`, `destroy` |
| Udev helper script | `/usr/local/lib/intuneme/usb-hotplug` | `start` | `stop`, `destroy` |
| Extension polkit policy | `/etc/polkit-1/actions/org.frostyard.intuneme.policy` | `extension install` | `destroy --all` |
| GNOME extension | `~/.local/share/gnome-shell/extensions/intuneme@frostyard.org/` | `extension install` | `destroy --all` |
| D-Bus broker service | `~/.local/share/dbus-1/services/com.microsoft.identity.broker1.service` | `config broker-proxy enable` | `config broker-proxy disable`, `destroy --all` |
| SELinux policy | System policy store | `init` (if SELinux) | Manual |

## Release and Package Publication

Releases are tagged with `make bump` (svu, `.svu.yml`). The tag push runs `.github/workflows/release.yml`, a single `goreleaser` job:

1. **GoReleaser Pro** builds the static (`CGO_ENABLED=0`) linux/amd64 binary, the archive and its SBOM, and the nfpm packages `frostyard-intuneme` in deb, rpm and apk formats (`Depends: systemd-container`). It signs every artifact with cosign and creates the GitHub release with all of them as assets.
2. **`actions/attest-build-provenance`** attests the archives, packages, SBOMs and `checksums.txt`. The job grants `id-token: write` and `attestations: write` for this.
3. **Request APT publication** sends a `publish-deb` `repository_dispatch` to `frostyard/apt-publisher` with `{"repo", "tag"}`, authenticated by `APT_PUBLISH_TOKEN`. It is unguarded and not `continue-on-error`: if it fails, nothing was published, and the release run fails.

[frostyard/apt-publisher](https://github.com/frostyard/apt-publisher) is the single writer of Frostyard's Debian metadata ([core ADR-0055](https://github.com/frostyard/core/blob/main/docs/adr/0055-publish-debian-packages-through-the-apt-publisher.md)). For each request it:

- downloads every `*.deb` asset of the release (the `.sigstore` bundles, rpm and apk files are ignored);
- because intuneme is registered `attested=yes`, runs `gh attestation verify --source-ref refs/tags/<tag> --deny-self-hosted-runners` on each one. The tag must therefore point at the commit the release run built, on a GitHub-hosted runner;
- checks the package name, architecture and codenames against intuneme's registration in its `config/producers.tsv` (`frostyard-intuneme`, `trixie,forky`);
- publishes to `https://repository.frostyard.org/debian/` under `trixie` and `forky`. The version carries no `~debNN` marker because the binary is static, so the same file goes to both codenames;
- verifies the published suite, purges the CDN and installs the package with apt as a canary; and
- dispatches `build` to frostyard/snosi, which installs `frostyard-intuneme` in its snow and sundog images ([ADR-0056](https://github.com/frostyard/core/blob/main/docs/adr/0056-rebuild-images-after-apt-publication.md)).

This repository therefore holds no package-signing key or R2 credentials and does not dispatch to snosi. Don't re-tag to recover a failed publication. A failed publish run is re-run from apt-publisher's Actions page. If the request step itself failed, for example because the token was missing, the GitHub release already exists: run apt-publisher's `publish` workflow by hand with `repo=frostyard/intuneme` and the tag. apt-publisher's nightly audit reports any release whose `.deb` was never published.

`snapshot.yml` runs `goreleaser release --nightly` after Tests pass on `main` and replaces the rolling `dev` prerelease. It attests its assets but never requests APT publication. Its `.deb` is versioned from the next major (for example `1.0.0.dev`), so it would sort above every real release, and its provenance names `refs/heads/main`, not a tag.

Every snapshot run for a successful Tests completion joins the concurrency group `snapshot-dev`, with `cancel-in-progress: false`. A run for a failed Tests completion skips its job, so it gets a group of its own (`snapshot-skip-<run_id>`): in the shared group it would cancel a pending run that publishes, and then publish nothing. Every run uploads assets with the same names to the same `dev` prerelease, so 2 parallel runs (several merges close together) collide and 1 fails with `422 already_exists` (run 41258966652). The group runs them 1 at a time. GitHub keeps at most 1 pending run in a group and cancels an older pending run when a newer one queues; that loses nothing, because a `workflow_run` run builds the commit that `main` points to when the run is triggered, so the newer run builds the same or a later commit. The run in progress is never cancelled, because a cancel midway through the uploads leaves `dev` with a partial asset set.

`release_workflow_contract_test.go` (package `main`) pins the trigger, the attestation over `dist/*.deb`, the ordering, and the shape of the request, and refuses a repogen step, a direct snosi dispatch, or a publication request from `snapshot.yml`. It also pins the snapshot concurrency group.

## Gate and CI

[ADR-0004](../adr/0004-adopt-the-repository-baseline.md) records this shape (frostyard/core's repository baseline).

- `make verify` runs `tidy-check` (`go mod tidy -diff`), `vet`, `format-check` (`gofmt -l`), `lint`, `verify-template` and `race` (`go test -v -race ./...`). It changes no file. `make check` runs `fmt`, then `verify`.
- `make ci` is `verify`, `corrections-base`, `vulncheck` (govulncheck v1.9.0) and `build`. It has no recipe. `.github/workflows/ci.yml` (workflow name `Tests`, which `snapshot.yml` follows) runs each prerequisite as its own `make <target>` step: the `Verify` job runs `make verify` and `make corrections-base BASE=<PR base>`, and the `Vulncheck` and `Build` jobs run the other 2. A new prerequisite of `ci` needs a step in `ci.yml` in the same change.
- The `Verify` job prepares subordinate UID/GID maps and sets `INTUNEME_REQUIRE_KERNEL_TESTS=1`, so `TestHelperKernelBoundary` (`internal/nshelper`) fails there instead of skipping.
- `mise.toml` pins golangci-lint and Node (the template gate); `mise.lock` holds their checksums. Go comes from `go.mod`. `make lint` fails unless the installed golangci-lint is the pin and was built with a Go minor no older than `go.mod`'s.
- `verify-template` runs `scripts/check-template.mjs` and its Node test. They are byte-identical copies from bketelsen/agentic-template and check the agent aliases, the `docs/README.md` index, `.github/risk-paths.json`, `.memory/corrections.jsonl` and the hash pins of the 2 files copied from core. `corrections-base` fails when `BASE` does not resolve, and skips with a notice when `BASE` has no `scripts/check-corrections.mjs`.
- `.github/risk-paths.json` lists the paths whose pull requests get a level label from the `.github/workflows/risk-paths.yml` caller of core's `pr-triage.yml`: governance and CI files, the release configuration and its hooks, every file that runs `sudo` (`internal/nshelper`, `internal/sudoers`, `internal/sudo`, `internal/nspawn`, `internal/provision`, `internal/udev`, `internal/nvidia`, `internal/puller`, and `cmd/start.go`, `cmd/destroy.go`, `cmd/recreate.go`, `cmd/extension.go`), the other privileged entry points (`main.go`, `cmd/init.go`, `cmd/udev.go`, `cmd/stop.go`), the polkit files, the scripts in `scripts/`, `ubuntu-intune/` and its cosign key. Each entry has a `level` (L1 or L2 here, `default_level` L3 for any other path); the caller adds `level:L1` to `level:L4` and `risk:high` for L1 and L2, and removes `ready-for-human` when the head moves (core ADR-0068, ADR-0077, ADR-0078). Each `why` of an L1 or L2 entry starts with a category of core's risk-paths spec. `risk_paths_test.go` fails when a non-test Go file that calls `"sudo"` or an install, remove or helper function of `sudoers`, `udev`, `nshelper` or `provision`, an entry point in its `privilegedRequired` list, or a `scripts/*.sh` matches no glob, or when an L1 or L2 `why` names no allowed category.

## Documentation Site

The project has a MkDocs documentation site (`site/` directory, `mkdocs.yml`) published to GitHub Pages at `https://frostyard.github.io/intuneme/`. It uses the materialx theme and covers user-facing guides (getting started, daily workflow, troubleshooting), reference (CLI commands auto-generated by `intuneme gendocs`, configuration, storage), and contributing guidelines. The `make docs` target generates completions, man pages, and CLI reference markdown in `site/reference/cli/`.

### SBOM Generation

Both artifact pipelines generate SBOMs (Software Bill of Materials) using [Syft](https://github.com/anchore/syft):

- **Container image** (`build-container.yml`) — Syft scans the exported container filesystem. The SBOM is uploaded as an OCI artifact via `oras attach` (artifact type `application/vnd.spdx+json`) and signed with cosign. Consumers discover it via the OCI referrers API.
- **Go binary** (`release.yml`, `snapshot.yml`) — GoReleaser's native `sboms` config invokes Syft per archive. SBOMs are included as release assets (`.sbom.json`) and covered by cosign signing and build provenance attestation.

The container workflow uses `actions/attest` for attestation (matching ublue-os/aurora). Binary workflows use `actions/attest-build-provenance`.

## Detailed Subsystem Docs

- [Container Lifecycle](container-lifecycle.md) — init, start, stop, destroy, recreate flows
- [Broker Proxy](broker-proxy.md) — D-Bus forwarding for host-side SSO
- [Notify Proxy](notify-proxy.md) — container desktop notifications on the host
- [Screencast Proxy](screencast-proxy.md) — screen sharing from the container's Edge
- [Container Image](../specs/container-image.md) — Build process, packages, and system configuration

### Scoped machine operations and migration

`machine-exec` is a copy of the Go binary with its own argv[0] dispatch and the
same root-owned config as `nsenter-exec`. It resolves and validates the target
before stop/bind, sanitizes the machinectl environment, and pins bind sources
with `openat2` plus `fstat`. Symlinks in any source component are rejected.
The helper clones the pinned source with `open_tree`, sets read-only/nosuid/nodev
with `mount_setattr`, then enters the validated mount namespace on a locked
thread after `unshare(CLONE_FS)`. It opens the fixed `/opt/intuneme-mcp`
destination beneath the pinned target root without symlinks and attaches the
clone using `move_mount` with empty-path descriptor flags. No validated object
is converted back to a pathname. Older machined implementations resolve even
`/proc/<pid>/fd` sources back to pathnames, so they cannot provide this guarantee.
The CLI resolves normal host aliases first. Linux 5.12+ is required for binds.

`refreshStartSecurity` runs after sudo authentication, before boot or the
already-running return. It replaces the broad host polkit rule, refreshes both
trusted helpers and config, installs the container broker restart policy and
session script, and revokes legacy container sudo grants. Errors are fatal;
untrusted helper sources also trigger helper authorization removal. An absent
machine helper permits authenticated poweroff for stop/recreate; a present
helper's error does not. Root linger setup explicitly uses authenticated sudo.

`TestHostShellAuthorizationDecisions` evaluates generated policy JavaScript.
`TestBindSourcePinsDirectoryAcrossReplacement` and the kernel bind case check
source replacement and read-only enforcement. Start and stop tests exercise
migration ordering, failures, dry-run, and authentication denial. See
[ADR-0002](../adr/0002-pin-machine-operations-and-scope-shell-authorization.md).
