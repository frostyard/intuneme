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
