# intuneme CLI Design

## Goal

Run Intune Portal, Intune Agent, and Microsoft Identity Broker in a systemd-nspawn container on an immutable Debian host. Microsoft Edge runs on the host. The contained Intune services handle authorization so Edge can access corporate resources.

A Go CLI (`intuneme`) automates provisioning and lifecycle management so the process is repeatable for any user.

## Approach

Single Go binary wrapping `systemd-nspawn` directly. Rootfs provisioned by extracting the `ghcr.io/frostyard/ubuntu-intune:latest` OCI image. This is closest to a colleague's known-working nspawn setup and gives full control over container flags without intermediate abstractions.

## CLI Commands

| Command | Description |
|---------|-------------|
| `intuneme init` | Pull OCI image, extract rootfs, create container user, apply fixups, install polkit rules |
| `intuneme start` | Boot nspawn container, forward host session, launch intune-portal |
| `intuneme stop` | Stop intune-portal and shut down the container |
| `intuneme status` | Show container and intune-portal state |
| `intuneme destroy` | Stop if running, remove rootfs and all state |

## Storage Layout

Default: `~/.local/share/intuneme/`. Configurable via `--root` flag.

```
~/.local/share/intuneme/
├── rootfs/          # extracted Ubuntu 24.04 rootfs with Intune installed
└── config.toml      # rootfs path, host UID, machine name overrides
```

## Provisioning (`intuneme init`)

1. **Check prerequisites** — verify `systemd-nspawn`, `machinectl`, and `podman` are on the host.
2. **Pull OCI image** — `podman pull ghcr.io/frostyard/ubuntu-intune:latest`.
3. **Extract rootfs** — create a temporary container, copy its filesystem into `rootfs/`, remove the temporary container.
4. **Create container user** — user inside the rootfs with UID/GID matching the host user.
5. **Apply fixups:**
   - `/etc/hosts` and `/etc/hostname` configured.
   - PAM configured for gnome-keyring unlock (gnome-keyring PAM module in common-auth and common-session).
   - Pre-create `~/.local/share/keyrings/` directory structure with `login` as default keyring.
   - Write `/etc/environment` with `DISPLAY=:0`, `NO_AT_BRIDGE=1`, `GTK_A11Y=none`.
   - Install a `fix-home-ownership.service` systemd unit to chown the home directory on first boot (chown doesn't work in chroot due to UID mapping).
6. **Install polkit rules** — copy polkit rule to `/etc/polkit-1/rules.d/50-intuneme.rules` on the host so `sudo` group members can use machinectl without password prompts.
7. **Write config** — save rootfs path, host UID, machine name to `config.toml`.

### What the OCI image provides

From `build_files/build` in the frostyard/ubuntu-intune repo:

- Microsoft repo GPG key and apt source configured
- `microsoft-identity-broker` installed
- `intune-portal` installed with the polkit postinst fix applied
- `pwquality.conf` already in place (minlen=14, ucredit/lcredit/dcredit/ocredit=-1)

### What provisioning adds on top

- Container user matching host UID
- PAM/keyring configuration (OCI image is for container use, not nspawn boot)
- Hostname setup
- First-boot ownership fix service

## Session Environment Discovery (`intuneme start`)

The host's graphical session must be detected and forwarded into the container.

### Environment variable discovery

| Variable | Discovery |
|----------|-----------|
| `XDG_RUNTIME_DIR` | `$XDG_RUNTIME_DIR` from host, fall back to `/run/user/$UID` |
| `DBUS_SESSION_BUS_ADDRESS` | `$DBUS_SESSION_BUS_ADDRESS` from host, fall back to `unix:path=$XDG_RUNTIME_DIR/bus` |
| `DISPLAY` | `$DISPLAY` from host |
| `WAYLAND_DISPLAY` | `$WAYLAND_DISPLAY` from host (may be unset on pure X11) |
| `XAUTHORITY` | `$XAUTHORITY` from host if set, otherwise search `$XDG_RUNTIME_DIR` for: `.mutter-Xwaylandauth.*`, `xauth_*`, `.Xauthority`. Error with clear message if none found. |

### Bind mounts

| Host path | Container path | Purpose |
|-----------|---------------|---------|
| `$HOME` | `$HOME` | Full home directory |
| `/tmp/.X11-unix` | `/tmp/.X11-unix` | X11 socket |
| `$XDG_RUNTIME_DIR` | `/run/user-external/$UID` | Host D-Bus, Wayland, PulseAudio sockets |

### nspawn invocation

```
sudo systemd-nspawn \
  -D <rootfs> \
  --machine=intuneme \
  --bind=$HOME \
  --bind=/tmp/.X11-unix \
  --bind=$XDG_RUNTIME_DIR:/run/user-external/$UID \
  -b
```

### intune-portal launch

After container boot completes, run via `machinectl shell` with `--setenv` flags:

- `XDG_RUNTIME_DIR=/run/user-external/$UID`
- `DBUS_SESSION_BUS_ADDRESS=unix:path=/run/user-external/$UID/bus`
- `DISPLAY=<discovered>`
- `WAYLAND_DISPLAY=<discovered>` (if present)
- `XAUTHORITY=<discovered file path remapped to /run/user-external/...>`

Environment variables are set only for the `intune-portal` process, not session-wide. Setting them session-wide breaks things when the container session ends.

## Error Handling

### Prerequisite checks

- Missing `systemd-nspawn`/`machinectl` — error with install hint (`systemd-container` package).
- Missing `podman` — error with install hint.
- Already initialized — `init` refuses unless `--force` passed.
- Not initialized — `start` tells user to run `init` first.

### Runtime errors

- No graphical session — error explaining Intune needs a graphical session.
- No XAUTHORITY found — error listing patterns searched, suggesting user set `$XAUTHORITY`.
- Container already running — report it's up, re-launch `intune-portal` if it's not running.
- `intune-portal` crashes — report exit code, user can `start` again without rebooting container.

### Lifecycle edge cases

- Host session ends while container running — container stays up but intune-portal will fail (sockets gone). `stop` cleans up. User restarts after logging back in.
- `stop` when nothing running — no-op with message.
- `destroy` while running — stops first, then destroys.

### Permissions

`systemd-nspawn -b` requires root. CLI uses `sudo` for nspawn and machinectl operations. Polkit rules (installed during `init`) allow `sudo` group members to use machinectl without repeated password prompts.

## Polkit Configuration

Installed to `/etc/polkit-1/rules.d/50-intuneme.rules` on the host during `intuneme init`:

```javascript
polkit.addRule(function(action, subject) {
    if ((action.id == "org.freedesktop.machine1.manage-machines" ||
         action.id == "org.freedesktop.machine1.manage-images" ||
         action.id == "org.freedesktop.machine1.login" ||
         action.id == "org.freedesktop.machine1.shell" ||
         action.id == "org.freedesktop.machine1.host-shell") &&
        subject.isInGroup("sudo")) {
        return polkit.Result.YES;
    }
});
```

## Success Criteria

1. `intuneme init` completes — rootfs extracted, user created, fixups applied.
2. `intuneme start` boots the container and launches `intune-portal` — GUI appears on host display.
3. User authenticates through `intune-portal` and enrolls the device.
4. Microsoft Edge on the host can access corporate resources (Conditional Access passes).
5. `intuneme stop` cleanly shuts everything down.
6. `intuneme start` works again after stop (no stale state).

## Out of Scope (first iteration)

- VPN client (runs on host separately)
- Smartcard/OpenSC support
- Multiple users on the same machine
- Auto-start on login
