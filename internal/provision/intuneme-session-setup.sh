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

# Propagate the environment to the systemd user manager AND the D-Bus activation
# environment. The latter is what D-Bus-activated services (the identity broker)
# inherit — without it the broker has no DISPLAY and crashes on activation.
_env_vars="DISPLAY XAUTHORITY NO_AT_BRIDGE GTK_A11Y PATH WAYLAND_DISPLAY \
PIPEWIRE_REMOTE PULSE_SERVER __NV_PRIME_RENDER_OFFLOAD __GLX_VENDOR_LIBRARY_NAME"
# Only pass variables that are actually set, so we don't clear them.
_set_vars=""
for _v in $_env_vars; do
    [ -n "${!_v+x}" ] && _set_vars="$_set_vars $_v"
done
if [ -n "$_set_vars" ]; then
    # shellcheck disable=SC2086
    systemctl --user import-environment $_set_vars 2>/dev/null
    # shellcheck disable=SC2086
    dbus-update-activation-environment --systemd $_set_vars 2>/dev/null
fi

# Initialize gnome-keyring once per boot. The keyring must be unlocked for the
# identity broker to store/read credentials. Marker lives in /tmp (tmpfs) so it
# resets every boot; the keyring dir is on the persistent bind-mounted home.
_keyring_dir="$HOME/.local/share/keyrings"
_keyring_marker="/tmp/.intuneme-keyring-init-done"

if [ ! -f "$_keyring_marker" ]; then
    mkdir -p "$_keyring_dir"
    [ -f "$_keyring_dir/default" ] || echo "login" > "$_keyring_dir/default"

    # Is the login collection already unlocked? If so, leave the running daemon
    # alone — don't disrupt an in-use keyring.
    _locked=$(busctl --user get-property org.freedesktop.secrets \
        /org/freedesktop/secrets/collection/login \
        org.freedesktop.Secret.Collection Locked 2>/dev/null)

    if [ "$_locked" != "b false" ]; then
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
        echo "" | gnome-keyring-daemon --unlock --components=secrets,pkcs11 -d 2>/dev/null
        sleep 1
    fi

    # Force-create the default collection so ReadAlias("default") resolves and the
    # broker can store credentials. A no-op if it already exists.
    if ! secret-tool lookup _keyring_init _keyring_init >/dev/null 2>&1; then
        echo "init" | secret-tool store --label="Keyring Init" _keyring_init _keyring_init 2>/dev/null
    fi

    touch "$_keyring_marker"

    # Restart the system device broker so it re-reads the now-initialized keyring.
    # The user-session identity broker is D-Bus-activated (not a systemd unit), so
    # it is NOT restarted here — it re-activates with a fresh process, and now
    # working environment, on the next call from Edge.
    sudo systemctl restart microsoft-identity-device-broker.service 2>/dev/null
fi

# Start the intune agent timer if it isn't already running.
if ! systemctl -q --user is-active intune-agent.timer 2>/dev/null; then
    systemctl --user start intune-agent.timer 2>/dev/null
fi
