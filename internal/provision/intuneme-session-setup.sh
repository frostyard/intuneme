#!/bin/bash
# /usr/local/bin/intuneme-session-setup — shared container session initialization.
#
# This is invoked from BOTH session-entry paths so they behave identically:
#   - /etc/profile.d/intuneme.sh sources it on interactive login (machinectl shell).
#   - nspawn.Exec() runs it before launching a GUI app via nsenter (the path the
#     GNOME extension uses). That path is a NON-login shell, so without this it
#     would never source /etc/profile.d and the steps below would be skipped.
#
# Why this matters: the Microsoft identity broker is a GTK app that the session
# D-Bus daemon activates on demand. It only inherits DISPLAY/XAUTHORITY if those
# were pushed into the D-Bus *activation* environment — otherwise it dies on
# startup with "cannot open display" and Edge can't authenticate. It also needs
# an unlocked gnome-keyring to read/write tokens.
#
# Safe to source or execute, and idempotent: env propagation runs every time,
# keyring init runs at most once per boot.

# Ensure we can reach the user session bus even from a non-login shell.
: "${XDG_RUNTIME_DIR:=/run/user/$(id -u)}"
export XDG_RUNTIME_DIR
: "${DBUS_SESSION_BUS_ADDRESS:=unix:path=${XDG_RUNTIME_DIR}/bus}"
export DBUS_SESSION_BUS_ADDRESS

# Display environment — host DISPLAY written by `intuneme start` into a marker.
if [ -f /etc/intuneme-host-display ]; then
    # shellcheck source=/dev/null
    . /etc/intuneme-host-display
    export DISPLAY
else
    export DISPLAY=:0
fi
export NO_AT_BRIDGE=1
export GTK_A11Y=none
export PATH="/opt/microsoft/intune/bin:/opt/microsoft/microsoft-azurevpnclient:$PATH"

# X11 auth — bind-mounted from host at /run/host-xauthority
if [ -f /run/host-xauthority ]; then
    export XAUTHORITY=/run/host-xauthority
fi

# Wayland / PipeWire / PulseAudio sockets (bind-mounted from host)
if [ -S /run/host-wayland ]; then
    export WAYLAND_DISPLAY=/run/host-wayland
    # WebRTC (Edge screen sharing) uses the ScreenCast portal only when
    # XDG_SESSION_TYPE is "wayland". Without it, it falls back to X11
    # capture, which finds nothing because the Edge wrapper unsets DISPLAY,
    # and only browser tabs can be shared.
    export XDG_SESSION_TYPE=wayland
fi
if [ -S /run/host-pipewire ]; then
    export PIPEWIRE_REMOTE=/run/host-pipewire
fi
if [ -S /run/host-pulse ]; then
    export PULSE_SERVER=unix:/run/host-pulse
fi

# Nvidia GPU (libraries symlinked from host by intuneme start)
if [ -d /run/host-nvidia ]; then
    export __NV_PRIME_RENDER_OFFLOAD=1
    export __GLX_VENDOR_LIBRARY_NAME=nvidia
fi

# WebKitGTK rendering workaround for the identity broker's sign-in window.
#
# The broker (microsoft-identity-broker) renders its interactive auth window
# with WebKitGTK 4.1. Because WAYLAND_DISPLAY is exported above, GTK picks the
# Wayland backend and WebKit connects straight to the *host* compositor,
# handing it dmabufs produced by the container's Mesa across the container
# boundary. That combination paints nothing: the window maps, the web process
# starts (it even activates at-spi), no EGL/GBM error is logged — and the page
# stays blank white, so sign-in is impossible.
#
# The GPU stack itself is fine (gbm_create_device + eglInitialize both succeed
# inside the container), so this is a buffer-handoff problem, not a driver one.
# Forcing WebKit onto its SHM path sidesteps it. Only the auth window uses
# WebKit here — Edge is Chromium and unaffected — so the lost acceleration
# costs nothing that matters.
export WEBKIT_DISABLE_DMABUF_RENDERER=1
export WEBKIT_DISABLE_COMPOSITING_MODE=1

# Propagate the environment to the systemd user manager AND the D-Bus activation
# environment. The latter is what D-Bus-activated services (the identity broker)
# inherit — without it the broker has no DISPLAY and crashes on activation.
_env_vars="DISPLAY XAUTHORITY NO_AT_BRIDGE GTK_A11Y PATH WAYLAND_DISPLAY \
PIPEWIRE_REMOTE PULSE_SERVER __NV_PRIME_RENDER_OFFLOAD __GLX_VENDOR_LIBRARY_NAME \
WEBKIT_DISABLE_DMABUF_RENDERER WEBKIT_DISABLE_COMPOSITING_MODE"
# Only pass variables that are actually set, so we don't clear them.
_set_vars=""
for _v in $_env_vars; do
    [ -n "${!_v+x}" ] && _set_vars="$_set_vars $_v"
done
if [ -n "$_set_vars" ]; then
    # shellcheck disable=SC2086
    systemctl --user import-environment $_set_vars 2>/dev/null
    # shellcheck disable=SC2086
    if dbus-update-activation-environment --systemd $_set_vars 2>/dev/null; then
        # Gate for intune-agent.timer: a user manager started by linger at
        # container boot has no activation environment yet, so the agent would
        # run, activate the broker without DISPLAY, and the broker would die.
        # ConditionPathExists=%t/intuneme-session-env holds the timer back until
        # this script has pushed the environment and touched the file below.
        [ -S "$XDG_RUNTIME_DIR/bus" ] && touch "$XDG_RUNTIME_DIR/intuneme-session-env" 2>/dev/null
    fi
fi

# Initialize gnome-keyring once per boot. The keyring must be unlocked for the
# identity broker to store/read credentials. Marker lives in /tmp (tmpfs) so it
# resets every boot; the keyring dir is on the persistent bind-mounted home.
#
# The marker is written ONLY after the login collection reads back unlocked.
# The nsenter launch path (`intuneme open`, the GNOME extension) can run before
# the container user's systemd manager exists; then there is no session bus and
# no usable /run/user/<uid>, every step below fails silently, and a marker
# written anyway would suppress the unlock for the rest of the boot while
# activated keyring daemons come up locked. Skip until the bus exists and retry
# on the next session entry instead.
#
# Retries are capped per boot: every attempt kills the keyring daemons and
# sleeps 2 s, so an unlock that keeps failing would slow every app launch and
# every shell login for the whole boot. After _keyring_max_attempts failed
# attempts the script stops trying until the attempts file is removed (see
# site/troubleshooting.md). Each attempt and the give-up are logged to stderr
# and to the journal (`journalctl -t intuneme-session-setup`).
#
# The whole block runs in a subshell that holds an flock on
# $XDG_RUNTIME_DIR/intuneme-keyring.lock (the runtime dir is private to the
# user, /tmp is not). Session entries can overlap — an app launch, an MCP
# session and a login shell at once — and without the lock both could read the
# same counter and both attempt the unlock, so the cap would not hold. The
# subshell is required because this script is SOURCED by a login shell: a bare
# `exit` would close the user's shell, but inside `( ... )` it only ends the
# subshell. Variables set inside do not leak into the login shell.
_keyring_dir="$HOME/.local/share/keyrings"
_keyring_marker="/tmp/.intuneme-keyring-init-done"
_keyring_attempts_file="/tmp/.intuneme-keyring-unlock-attempts"
_keyring_max_attempts=3

_intuneme_log() { echo "intuneme-session-setup: $*" >&2; logger -t intuneme-session-setup -- "$*" 2>/dev/null || :; }

_keyring_locked() {
    busctl --user get-property org.freedesktop.secrets \
        /org/freedesktop/secrets/collection/login \
        org.freedesktop.Secret.Collection Locked 2>/dev/null
}

if [ ! -f "$_keyring_marker" ] && [ -S "$XDG_RUNTIME_DIR/bus" ]; then
    (
        # Serialize concurrent session entries. Where flock is missing the block
        # still runs, unserialized: the worst case is the old behaviour, a few
        # attempts too many.
        if command -v flock >/dev/null 2>&1; then
            flock -w 30 9 || exit 0
        fi

        # Re-check under the lock: a run that held it before us may have
        # unlocked the keyring or used up the attempts.
        [ -f "$_keyring_marker" ] && exit 0

        mkdir -p "$_keyring_dir"
        [ -f "$_keyring_dir/default" ] || echo "login" > "$_keyring_dir/default"

        # Is the login collection already unlocked? If so, leave the running daemon
        # alone — don't disrupt an in-use keyring.
        _locked=$(_keyring_locked)

        # Failed attempts so far this boot, from the attempts file.
        _keyring_read_attempts() {
            _attempts=0
            if [ -f "$_keyring_attempts_file" ]; then
                read -r _attempts_saved < "$_keyring_attempts_file" 2>/dev/null
                case "$_attempts_saved" in
                    ''|*[!0-9]*) _attempts=0 ;;
                    *) _attempts=$_attempts_saved ;;
                esac
            fi
            echo "$_attempts"
        }
        _attempts=$(_keyring_read_attempts)
        _keyring_attempted=0

        if [ "$_locked" != "b false" ] && [ "$_attempts" -lt "$_keyring_max_attempts" ]; then
            _attempts=$((_attempts + 1))
            # Write to a temp file and move it into place, so a concurrent reader
            # never sees a half-written counter.
            echo "$_attempts" > "$_keyring_attempts_file.tmp.$$" 2>/dev/null
            mv -f "$_keyring_attempts_file.tmp.$$" "$_keyring_attempts_file" 2>/dev/null
            _keyring_attempted=1
            # Reliable unlock: the well-known org.freedesktop.secrets name is held by
            # whichever gnome-keyring daemon claimed it first (often the systemd
            # socket-activated one). A `--replace --unlock` daemon does NOT take that
            # name, so its unlock never reaches the daemon the broker talks to.
            # Kill ALL keyring daemons (matched by truncated comm to avoid a pkill -f
            # self-match) and start exactly one that owns the service and is unlocked.
            pkill -u "$(id -u)" -x gnome-keyring-d 2>/dev/null
            sleep 1
            # `echo ""` sends a newline = real empty-string password (creates/unlocks
            # the login keyring). `printf ""` would send EOF = "no password" and do
            # nothing. The container's login keyring uses an empty password.
            # `9>&-` closes the lock descriptor for the daemon: `-d` daemonizes, and a
            # daemon that inherited fd 9 would hold the lock after this subshell exits.
            echo "" | gnome-keyring-daemon --unlock --components=secrets,pkcs11 -d 9>&- 2>/dev/null
            sleep 1
        fi

        # Force-create the default collection so ReadAlias("default") resolves and the
        # broker can store credentials. A no-op if it already exists. Skipped when
        # the attempt limit is reached: the daemon is not ours then, so this write
        # would fail anyway.
        if [ "$_locked" = "b false" ] || [ "$_keyring_attempted" = 1 ]; then
            if ! secret-tool lookup _keyring_init _keyring_init >/dev/null 2>&1; then
                echo "init" | secret-tool store --label="Keyring Init" _keyring_init _keyring_init 2>/dev/null
            fi
        fi

        if [ "$(_keyring_locked)" = "b false" ]; then
            touch "$_keyring_marker"
            rm -f "$_keyring_attempts_file" 2>/dev/null

            # Restart the system device broker so it re-reads the now-initialized
            # keyring. The user-session identity broker is D-Bus-activated (not a
            # systemd unit), so it is NOT restarted here — it re-activates with a
            # fresh process, and now working environment, on the next call from Edge.
            systemctl --no-ask-password restart microsoft-identity-device-broker.service 2>/dev/null
        elif [ "$_keyring_attempted" = 1 ]; then
            _intuneme_log "keyring unlock attempt $_attempts of $_keyring_max_attempts failed"
            if [ "$_attempts" -ge "$_keyring_max_attempts" ]; then
                _intuneme_log "keyring still locked after $_keyring_max_attempts attempts: no more retries this boot. To recover, run: rm -f /tmp/.intuneme-keyring-init-done /tmp/.intuneme-keyring-unlock-attempts; /usr/local/bin/intuneme-session-setup"
            fi
        fi
    ) 9>"$XDG_RUNTIME_DIR/intuneme-keyring.lock"
fi

# Start the intune agent timer if it isn't already running.
if ! systemctl -q --user is-active intune-agent.timer 2>/dev/null; then
    systemctl --user start intune-agent.timer 2>/dev/null
fi
