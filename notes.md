# intuneme Development Log

## 2026-02-13: First successful init + start

### What worked
- `intuneme init` successfully pulls `ghcr.io/frostyard/ubuntu-intune:latest`, extracts rootfs via podman, configures user/PAM/keyring/systemd, installs polkit rules
- `intuneme start` boots nspawn container, discovers host session (DISPLAY, XAUTHORITY, DBUS), launches `intune-portal` GUI on host display
- Intune Portal launched and is functional

### Issues hit during integration testing

1. **sudo needs TTY** — `r.Run()` captures output so sudo can't prompt for password. Fixed by using `r.RunAttached()` for all sudo operations.

2. **OCI image has existing UID 1000 user** — The frostyard image ships with `ubuntu:1000`. `useradd --uid 1000` fails with "UID is not unique". Fixed by detecting existing users by UID and renaming with `usermod --login`.

3. **`--pipe` console warning** — `systemd-nspawn --pipe` warns when stdin is a TTY. Changed to `--console=pipe`. Warning still appears (cosmetic, harmless).

4. **`tee` doesn't take content as arg** — `InstallPolkitRule` passed rule content as a positional argument to `sudo tee`. Fixed by writing to a temp file and `sudo cp`.

5. **Leftover podman containers** — If init fails partway through, `podman create --name intuneme-extract` fails on retry because the container already exists. Fixed by pre-cleaning with `podman rm intuneme-extract` before create.

6. **Identity broker error 501271** — Broker not running when intune-portal launches. Fixed by creating `start-intune.sh` that starts broker before portal.

7. **Java errors + STATE_DIRECTORY null** — Broker is a Java 11 app needing JAVA_HOME and systemd env vars. Fixed by setting JAVA_HOME=/usr/lib/jvm/java-11-openjdk-amd64 and STATE_DIRECTORY/RUNTIME_DIRECTORY/LOGS_DIRECTORY.

8. **Boot steals terminal** — `RunAttached` for nspawn boot shows login prompt, blocks shell. Fixed by adding `RunBackground` to Runner interface, `--console=pipe` to boot args, and `sudo -v` to pre-cache credentials.

9. **Gnome keyring write failure (BIGGEST ISSUE)** — Multiple root causes:
   - $HOME bind mount hides rootfs keyring files → create at runtime
   - Host's D-Bus forwarded but keyring needs container's own bus → override DBUS_SESSION_BUS_ADDRESS
   - `--unlock` doesn't CREATE keyrings, only opens existing → use `--login` first
   - `--login` incompatible with `--replace` → two-step: --login then --replace --unlock -d
   - Empty password (0 bytes) doesn't work → use real password ("intuneme") stored in ~/.keyring-password
   - Stale keyring from previous sessions with different password → always delete login.keyring before --login
   - `secret-tool` not installed → use busctl for verification instead

10. **Password compliance** — Intune requires password policies. Fixed by writing `/etc/security/pwquality.conf` in WriteFixups (minlen=12, dcredit/ucredit/lcredit/ocredit=-1).

## 2026-02-13: Enrollment success
- Intune enrollment completes when keyring initializes correctly
- Still testing: pwquality compliance checks, stop/start lifecycle

### Previous attempts
- Bubblewrap approach (yesterday) — got tangled quickly, abandoned

## 2026-02-13: v2 rework — Edge inside container

### Motivation
v1 approach of sharing $HOME and $XDG_RUNTIME_DIR created cascading complexity:
host-vs-container D-Bus confusion, keyring two-step dance, machinectl shell broken,
stale state from bind-mounted home. Scaled back to run everything (including Edge)
inside the container.

### Architecture change
- `~/Intune` on host → bind-mounted as container user's home (was: full $HOME)
- Individual socket binds for Wayland/PipeWire/Xauthority (was: entire XDG_RUNTIME_DIR)
- Container's own systemd/D-Bus/keyring (was: forwarding host session)
- `/etc/profile.d/intuneme.sh` sets env on login (was: start-intune.sh manual launcher)
- `machinectl shell` works for interactive access (was: broken in v1)
- No more host session discovery (`internal/session` package deleted)

### What worked immediately
- `machinectl shell` — gives real logind session with XDG_RUNTIME_DIR, D-Bus, keyring
- Profile.d script — DISPLAY, XAUTHORITY, keyring init all handled automatically
- gnome-keyring — starts and unlocks via profile.d, no more two-step password dance

### Issues found during smoke test
1. **X11 auth missing** — needed XAUTHORITY bind mount (`/run/host-xauthority`) and profile.d to set it. Without it, GTK init fails with "no authorization protocol specified".
2. **Keyring default collection not created** — `ReadAlias "default"` returns `/` (no collection). `secret-tool` not installed. Need to either install `libsecret-tools` or find another way to initialize the collection. Testing whether enrollment works regardless.

### Issues found and fixed during smoke test
1. **X11 auth missing** — needed XAUTHORITY bind mount (`/run/host-xauthority`) and profile.d to set it. Without it, GTK init fails with "no authorization protocol specified".
2. **Keyring default collection not created** — `ReadAlias "default"` returns `/` (no collection). Fixed by installing `libsecret-tools` and using `secret-tool store` in profile.d to force collection creation.
3. **Edge not installed** — frostyard image doesn't include Edge. Added `InstallPackages` provisioning step with apt-get install.
4. **Edge apt repo not configured** — separate repo URL from intune packages. Added sources.list entry during provisioning.
5. **Edge GPG key missing** — different signing key than intune repo. Downloaded on host side and wrote into rootfs (container lacks curl/gpg).
6. **sudo not installed** — frostyard image doesn't include sudo. Added to package install list.
7. **System device broker not running** — needed `systemctl enable microsoft-identity-device-broker` during provisioning.

### Status: WORKING
- Enrollment succeeds
- Compliance check passes
- Edge launches inside container with display on host
- intune-portal launches and authenticates
- machinectl shell provides real user session with D-Bus, keyring, XDG_RUNTIME_DIR
- ~/Intune persists state across container restarts

## 2026-02-13: Audio support + module rename + bug fixes

### Audio support
Added PulseAudio socket forwarding so Edge has full-duplex audio (playback + mic for Teams calls):
- Detect host PulseAudio socket (`/run/user/{uid}/pulse/native`) in `DetectHostSockets()`
- Bind-mount to `/run/host-pulse` in container
- Set `PULSE_SERVER=unix:/run/host-pulse` in profile.d script
- Install `libpulse0` (PulseAudio client library) during provisioning
- PipeWire socket forwarding was already in place; PulseAudio socket is the one Edge actually uses

### Module rename
Changed Go module from `github.com/bjk/intuneme` to `github.com/frostyard/intune` for public push.

### Bug fixes found during reprovision
1. **Device broker not starting on boot** — `microsoft-identity-device-broker.service` is a "static" unit (no `[Install]` section), so `systemctl enable` was a silent no-op. Fixed by creating the `multi-user.target.wants` symlink directly during provisioning.

2. **Keyring collection never created** — `printf ''` sends 0 bytes to `gnome-keyring-daemon --unlock`, but it needs a newline to create the login collection. Changed to `echo ""`. Also added broker restart after keyring init — the broker starts before first login and fails with `storage_keyring_write_failure`, then never retries.

## 2026-02-13: Release pipeline — fang, goreleaser, CI

### charmbracelet/fang integration
Replaced `cmd.Execute()` with `fang.Execute()` to get batteries-included CLI features:
- Styled help pages and error messages
- `--version` flag with build metadata (version/commit/date/builtBy via ldflags)
- Hidden `man` command (mango-powered single man page)
- Hidden `completion` command (bash/zsh/fish/powershell)
- Signal handling via `signal.NotifyContext` + `fang.WithNotifySignal`

Refactored `cmd/root.go` to export `RootCmd()` — subcommand files unchanged.

### Goreleaser scripts
Created `scripts/completions.sh` and `scripts/manpages.sh` for goreleaser `before.hooks`. Use `go run .` to invoke fang's hidden commands before the binary is built.

### Goreleaser config cleanup
Adapted `.goreleaser.yaml` from igloo reference:
- Removed `pkg/dracut/95etc-overlay/` references from nfpms (not applicable)
- Fixed release footer URL (`frostyard/` not `bketelsen/`)

### Makefile updates
- Replaced broken `docs` target (referenced nonexistent `gendocs` command)
- Added `completions`, `manpages`, `docs` targets using fang commands

### CI cleanup
Rewrote `.github/workflows/test.yml` — removed igloo-specific jobs (docker build, integration tests with loop devices, `nbc` binary references, `./pkg/...` test paths). Simplified to 4 jobs: lint, test, verify, build.

### Lint fixes
Fixed all 18 errcheck violations across 6 files. Added `CLAUDE.md` with `make fmt` / `make lint` reminder.

### Module rename
Renamed Go module from `github.com/frostyard/intune` to `github.com/frostyard/intuneme` to match the actual repo name. The mismatch caused goreleaser's `gomod.proxy` to fail — it tried to fetch the module via the Go proxy and couldn't resolve `frostyard/intune` since that repo doesn't exist.

### v0.1.0 released
First tagged release. GoReleaser pipeline passes: builds binary, generates completions + man page, packages deb/rpm/apk, publishes to frostyard repo.

## 2026-02-15: Consolidation — move static config into container image

### Motivation
After merging the `ubuntu-intune/` container build into the repo, consolidated responsibilities: the container image handles everything static (packages, PAM, pwquality, systemd overrides, Edge wrapper), the Go CLI handles only host-specific setup (user, hostname, polkit) and lifecycle.

### What moved
- `/etc/environment` → `system_files/etc/environment`
- PAM pwquality profile → `system_files/usr/share/pam-configs/pwquality`
- intune-agent.timer override → `system_files/etc/systemd/user/intune-agent.timer.d/override.conf`
- intune-agent.timer enable symlink → `system_files/etc/systemd/user/default.target.wants/`
- device-broker override → `system_files/etc/systemd/system/microsoft-identity-device-broker.service.d/override.conf`
- broker display override → `system_files/etc/systemd/user/microsoft-identity-broker.service.d/override.conf`
- Edge wrapper → `system_files/usr/local/bin/microsoft-edge`
- Package installation (Edge, libsecret-tools, sudo, libpulse0) → `build_files/build`
- `pam-auth-update` → `build_files/build`
- `systemctl enable microsoft-identity-device-broker` → `build_files/build`
- `pwquality.conf` → generated inline in `build_files/build`

~210 lines removed from `internal/provision/provision.go`, ~15 from `cmd/init.go`.

## 2026-02-17: Post-consolidation debugging — three bugs found and fixed

### Bug 1: Missing cracklib dictionaries (compliance failure)
**Symptom**: Password enforcement non-compliant in Intune. `chpasswd` during init showed `cracklib_dict.pwd: No such file or directory`.
**Root cause**: Two problems:
1. `cracklib-runtime` package was never installed — only `libcrack2` (the library) was present
2. Containerfile used `--mount=type=cache,dst=/var/cache` which swallowed any files written to `/var/cache/` during build (including cracklib dictionaries)
**Fix**: Added `cracklib-runtime` to package list in build script. Narrowed cache mount from `/var/cache` to `/var/cache/apt` so cracklib dictionary files persist in the final image.

### Bug 2: PAM pwquality profile overwritten by package install
**Symptom**: `/etc/pam.d/common-password` had `pam_pwquality.so retry=3` instead of the full parameter line with `dcredit=-1 ocredit=-1 ucredit=-1 lcredit=-1 minlen=12`.
**Root cause**: `COPY system_files /` runs before `RUN /ctx/build`. The `libpam-pwquality` package install overwrites the custom `/usr/share/pam-configs/pwquality` with its default.
**Fix**: Added a `tee` command in the build script to re-write the custom PAM profile after package installation, before `pam-auth-update`.

### Bug 3: Destroy missed broker state directory
**Symptom**: Decryption errors (`Failed to decrypt with key:LinuxBrokerRegularUserSecretKey`, `WorkplaceJoinFailure: [-100]`) after destroy + re-init.
**Root cause**: `cmd/destroy.go` cleaned `~/Intune/.local/share/microsoft-identity-broker` but the broker actually stores state in `~/Intune/.local/state/microsoft-identity-broker` (via `StateDirectory=`).
**Fix**: Changed destroy to clean `.local/state/microsoft-identity-broker` instead of `.local/share/microsoft-identity-broker`.

### Status: WORKING
- Enrollment succeeds
- All three fixes verified on fresh destroy → init → start → shell → enroll cycle

## 2026-02-17: Post-reboot failures — two bugs in shell and keyring init

### Bug 1: `intuneme shell` gave non-login shell
**Symptom**: `intune-portal` failed with `Authorization required, but no authorization protocol specified` / `Unable to initialize GTK+` after host reboot.
**Root cause**: `BuildShellArgs` ran `machinectl shell user@machine` which starts a non-login shell. `/etc/profile.d/intuneme.sh` never executed, so `XAUTHORITY` was never set — X11 auth failure.
**Fix**: Pass `/bin/bash --login` to `machinectl shell` so profile.d scripts run.

### Bug 2: Keyring init marker survived reboots
**Symptom**: After host reboot, brokers failed with `storage_keyring_write_failure` and intune-portal showed "Get the app" (credential invalid, default account not found).
**Root cause**: The `.init_done` marker was stored in `~/.local/share/keyrings/` on the persistent bind-mounted home dir. After reboot, profile.d saw the stale marker and skipped keyring initialization entirely — gnome-keyring never unlocked.
**Fix**: Moved marker to `/tmp/.intuneme-keyring-init-done` (tmpfs, resets every boot). Also added restart of the system-level `microsoft-identity-device-broker` service after keyring init — it was only restarting the user-level broker before.
