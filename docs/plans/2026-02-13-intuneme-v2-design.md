# intuneme v2 Design

## Motivation

The v1 approach — binding the host's `$HOME` and `$XDG_RUNTIME_DIR` into the container, running Edge on the host, forwarding the host's D-Bus session — created cascading complexity:

- `$HOME` bind mount hides rootfs files, requiring runtime creation of keyrings, password files, etc.
- Host-vs-container D-Bus confusion: keyring and broker need the container's own session bus, but display forwarding was set up on the host's bus.
- Two-step gnome-keyring initialization dance (login + replace/unlock) with password file management.
- `machinectl shell` produces no output in this configuration.
- Stale intune state in host `$HOME` causes 400 errors on re-enrollment.

## v2 Approach

Run everything inside the container: Intune services AND Microsoft Edge. The host provides only display, audio, and GPU access via individual socket/device binds. A dedicated `~/Intune` directory on the host serves as the container user's home, providing persistence and host-container file exchange.

This eliminates the `$HOME` bind mount collision, the D-Bus forwarding, the keyring password dance, and the host Edge SSO plumbing.

## Architecture

```
Host                          Container (nspawn -b)
─────────────────────        ─────────────────────────
~/Intune/  ──bind──────────→ /home/<user>/
/tmp/.X11-unix ──bind──────→ /tmp/.X11-unix
/run/user/$UID/wayland-0 ──→ /run/host-wayland (if present)
/run/user/$UID/pipewire-0 ─→ /run/host-pipewire (if present)
/dev/dri ──bind────────────→ /dev/dri

intuneme CLI                  systemd (PID 1)
  └─ sudo nspawn -b             ├─ microsoft-identity-broker.service
  └─ machinectl shell            ├─ microsoft-identity-device-broker.service
     (real user session)         ├─ intune-agent.timer
                                 ├─ gnome-keyring-daemon (via PAM)
                                 ├─ dbus session bus (via systemd --user)
                                 └─ intune-portal / edge (launched by user)
```

## CLI Commands

| Command | Description |
|---------|-------------|
| `intuneme init` | Pull OCI image, extract rootfs, create `~/Intune`, create container user, apply fixups, install polkit rules |
| `intuneme start` | Boot nspawn container with minimal bind mounts |
| `intuneme shell` | `machinectl shell` into the running container as the user (real logind/PAM session) |
| `intuneme stop` | `machinectl poweroff` |
| `intuneme status` | Check if machine is registered |
| `intuneme destroy` | Stop if running, remove rootfs, clean intune state from `~/Intune` |

`intuneme start` does NOT auto-launch intune-portal or Edge. The user runs `intuneme shell` and launches what they need.

## Storage Layout

```
~/.local/share/intuneme/
├── rootfs/          # extracted Ubuntu 24.04 rootfs
└── config.toml      # rootfs path, host UID, machine name

~/Intune/            # container user's home (bind-mounted)
├── .config/intune/  # intune enrollment state
├── .config/         # Edge profile, other app config
├── .local/          # keyring, broker state
├── Downloads/       # downloads, file exchange
└── ...
```

## Provisioning (`intuneme init`)

1. **Check prerequisites** — verify `systemd-nspawn`, `machinectl`, `podman` on host.
2. **Pull OCI image** — `podman pull ghcr.io/frostyard/ubuntu-intune:latest`.
3. **Extract rootfs** — create temp container, copy filesystem, remove temp container.
4. **Create `~/Intune`** — `mkdir -p ~/Intune` on the host.
5. **Create container user** — UID-matching logic (detect/rename existing UID 1000 user).
6. **Apply fixups:**
   - `/etc/hostname`, `/etc/hosts`
   - PAM pwquality profile + `/etc/security/pwquality.conf`
   - intune-agent timer override to `default.target`
   - Edge wrapper script (Wayland/Ozone flags)
   - `/etc/environment` — `DISPLAY=:0`, `NO_AT_BRIDGE=1`, `GTK_A11Y=none`
   - fix-home-ownership service
   - Edge SSO policy file (`/etc/opt/edge/policies/managed/intune-sso.json`)
7. **Configure PAM** — `pam-auth-update` inside nspawn.
8. **Set container password**.
9. **Install polkit rules** on host.
10. **Write config.toml**.

### Dropped from v1 provisioning
- `start-intune.sh` embedded script (no longer needed)
- Keyring directory/password file pre-creation in rootfs
- PAM login file rewrite

## nspawn Boot (`intuneme start`)

```
sudo systemd-nspawn \
  -D <rootfs> \
  --machine=intuneme \
  --bind=/home/<user>/Intune:/home/<user> \
  --bind=/tmp/.X11-unix \
  --bind=/run/user/<uid>/wayland-0:/run/host-wayland \
  --bind=/run/user/<uid>/pipewire-0:/run/host-pipewire \
  --bind=/dev/dri \
  --console=pipe \
  -b
```

Optional binds (wayland-0, pipewire-0) are only added if the socket exists on the host.

### What the container's own systemd provides
- logind session and `XDG_RUNTIME_DIR` at `/run/user/<uid>`
- D-Bus session bus at `/run/user/<uid>/bus`
- gnome-keyring-daemon activated via PAM on login
- systemd user services (broker, agent timer)

## Shell Access (`intuneme shell`)

Uses `machinectl shell <user>@intuneme` to get a real logind/PAM session. This provides:
- Proper `XDG_RUNTIME_DIR`
- D-Bus session bus
- PAM modules fired (gnome-keyring unlock)

If machinectl shell proves unreliable, fallback is `systemd-run -M intuneme --uid=<user> -t /bin/bash -l`.

## Code Changes

### Delete
- `internal/session/` — no longer needed (no host session discovery)
- `internal/provision/start-intune.sh` — no longer needed

### Gut and simplify
- `internal/nspawn/nspawn.go` — `BuildBootArgs` uses new bind mount list, drop `BuildShellArgs`/`LaunchIntune`, add `Shell` function using machinectl shell
- `internal/provision/provision.go` — `WriteFixups` drops keyring/password/start-intune.sh setup, adds Edge SSO policy

### Add
- `cmd/shell.go` — new cobra command

### Modify
- `cmd/start.go` — remove session discovery, remove intune-portal launch
- `cmd/destroy.go` — clean `~/Intune/.config/intune/` instead of host `$HOME`
- `cmd/init.go` — create `~/Intune`, remove start-intune.sh install

## Error Handling

Same as v1 with simplifications:
- No more XAUTHORITY-not-found errors (not needed)
- No more D-Bus discovery errors (not needed)
- machinectl shell failure → clear error message suggesting fallback

## Success Criteria

1. `intuneme init` completes — rootfs extracted, `~/Intune` created, user configured.
2. `intuneme start` boots the container.
3. `intuneme shell` drops into a real user session with working D-Bus and keyring.
4. User runs `intune-portal`, authenticates, enrolls.
5. User runs `microsoft-edge`, can access corporate resources (Conditional Access passes).
6. `intuneme stop` + `intuneme start` + `intuneme shell` works without stale state.
7. Files placed in `~/Intune/` on host are visible inside the container and vice versa.

## Out of Scope

- VPN client (runs on host separately)
- Smartcard/OpenSC support
- Multiple users
- Auto-start on login
- Auto-launch intune-portal or Edge on start
