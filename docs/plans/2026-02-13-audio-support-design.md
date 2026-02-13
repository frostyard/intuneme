# Audio Support Design

Date: 2026-02-13

## Goal

Enable full-duplex audio (playback + microphone) in the intuneme container for Microsoft Edge, primarily for Teams calls and media playback.

## Context

The host runs PipeWire, which exposes a PulseAudio-compatible socket at `/run/user/{uid}/pulse/native`. Edge (Chromium-based) uses the PulseAudio API via `libpulse` for audio I/O.

The project already forwards:
- PipeWire socket (`pipewire-0` -> `/run/host-pipewire`)
- Wayland socket (`wayland-0` -> `/run/host-wayland`)
- X11 socket + Xauthority

Audio is the missing piece.

## Approach

Forward the PulseAudio socket from the host into the container and install the PulseAudio client library. No server-side components needed inside the container.

Three changes across three files:

### 1. Socket Detection (`internal/nspawn/nspawn.go`)

Add PulseAudio socket to `DetectHostSockets()` checks:

```go
{runtimeDir + "/pulse/native", "/run/host-pulse"},
```

Same pattern as Wayland and PipeWire. No changes to `BuildBootArgs` — it already iterates all detected sockets.

### 2. Environment Setup (`internal/provision/intuneme-profile.sh`)

Add after the PipeWire block:

```bash
# PulseAudio socket (bind-mounted from host at /run/host-pulse)
if [ -S /run/host-pulse ]; then
    export PULSE_SERVER=unix:/run/host-pulse
    systemctl --user import-environment PULSE_SERVER 2>/dev/null
fi
```

### 3. Package Installation (`internal/provision/provision.go`)

Add `libpulse0` to the `apt-get install` line in `InstallPackages()`:

```
apt-get install -y microsoft-edge-stable libsecret-tools sudo libpulse0
```

## What We Decided Against

- **PipeWire client inside container**: Overkill. Edge uses PulseAudio API, not PipeWire directly.
- **Full PulseAudio server inside container**: Unnecessary. Socket forwarding is sufficient.
- **ALSA passthrough**: Too low-level. PulseAudio socket handles mixing and device abstraction.
