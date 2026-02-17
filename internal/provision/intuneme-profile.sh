#!/bin/bash
# /etc/profile.d/intuneme.sh — runs on login in the intuneme container.
# Sets display/audio environment, imports into systemd user session,
# and initializes gnome-keyring on first login after boot.

# Display environment
export DISPLAY=:0
export NO_AT_BRIDGE=1
export GTK_A11Y=none
export PATH="/opt/microsoft/intune/bin:$PATH"

# X11 auth — bind-mounted from host at /run/host-xauthority
if [ -f /run/host-xauthority ]; then
    export XAUTHORITY=/run/host-xauthority
fi

# Import into systemd user session so user services (broker) see them
systemctl --user import-environment DISPLAY XAUTHORITY PATH NO_AT_BRIDGE GTK_A11Y 2>/dev/null

# Wayland socket (bind-mounted from host at /run/host-wayland)
if [ -S /run/host-wayland ]; then
    export WAYLAND_DISPLAY=/run/host-wayland
    systemctl --user import-environment WAYLAND_DISPLAY 2>/dev/null
fi

# PipeWire socket (bind-mounted from host at /run/host-pipewire)
if [ -S /run/host-pipewire ]; then
    export PIPEWIRE_REMOTE=/run/host-pipewire
    systemctl --user import-environment PIPEWIRE_REMOTE 2>/dev/null
fi

# PulseAudio socket (bind-mounted from host at /run/host-pulse)
if [ -S /run/host-pulse ]; then
    export PULSE_SERVER=unix:/run/host-pulse
    systemctl --user import-environment PULSE_SERVER 2>/dev/null
fi

# Initialize gnome-keyring once per boot.
# The keyring must be unlocked for microsoft-identity-broker to store credentials.
# Marker lives in /tmp (tmpfs) so it resets on every container boot — the keyring
# dir is on the persistent bind-mounted home and would survive reboots.
_keyring_dir="$HOME/.local/share/keyrings"
_keyring_init_marker="/tmp/.intuneme-keyring-init-done"

if [ ! -f "$_keyring_init_marker" ]; then
    mkdir -p "$_keyring_dir"
    if [ ! -f "$_keyring_dir/default" ]; then
        echo "login" > "$_keyring_dir/default"
    fi
    echo "" | gnome-keyring-daemon --replace --unlock --components=secrets,pkcs11 -d 2>/dev/null
    sleep 1
    # Store a test secret to force creation of the default collection.
    # Without this, ReadAlias("default") returns "/" and the broker can't store credentials.
    if ! secret-tool lookup _keyring_init _keyring_init >/dev/null 2>&1; then
        echo "init" | secret-tool store --label="Keyring Init" _keyring_init _keyring_init 2>/dev/null
    fi
    touch "$_keyring_init_marker"
    # Restart brokers so they pick up the now-initialized keyring.
    # They start before login and fail with storage_keyring_write_failure.
    systemctl --user restart microsoft-identity-broker.service 2>/dev/null
    sudo systemctl restart microsoft-identity-device-broker.service 2>/dev/null
fi

# Start intune agent timer if not running
if ! systemctl -q --user is-active intune-agent.timer 2>/dev/null; then
    systemctl --user start intune-agent.timer 2>/dev/null
fi
