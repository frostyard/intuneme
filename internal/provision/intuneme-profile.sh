#!/bin/bash
# /etc/profile.d/intuneme.sh — runs on interactive login in the container.
#
# All session initialization (display/audio env, D-Bus activation environment,
# gnome-keyring unlock, intune agent timer) lives in the shared script below so
# that the login path and the non-login nsenter launch path (nspawn.Exec, used
# by the GNOME extension) initialize the session identically. See that script
# for the rationale.
#
# Sourced — not executed — so the exported env vars land in the login shell too.
if [ -r /usr/local/bin/intuneme-session-setup ]; then
    # shellcheck source=/dev/null
    . /usr/local/bin/intuneme-session-setup
fi
