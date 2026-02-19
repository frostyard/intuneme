# Fix Chromium-based apps crashing in container

## Problem

Microsoft Edge and Azure VPN Client (both Chromium-based) crash immediately inside the nspawn container with `Trace/breakpoint trap`. The auth flow for the VPN client never opens because its embedded browser crashes before it can render.

**Root causes:**

1. **Missing `render` group** -- The container user is in `adm,sudo,video,audio` but not `render`. The GPU render node (`/dev/dri/renderD128`) is owned by group `render`, so the container user cannot access it for hardware-accelerated rendering.

2. **Chromium GPU sandbox fails** -- Chromium's GPU process sandbox tries to create user namespaces (`CLONE_NEWUSER`), which nspawn restricts. This causes a `SIGTRAP` crash before Chromium can fall back to software rendering.

## Approach

Fix DRI permissions and configure Chromium with container-safe flags. This enables hardware GPU acceleration while disabling only the GPU process sandbox (the renderer sandbox stays active).

## Changes

### 1. Provisioning: detect host `render` GID and add group

**File:** `internal/provision/provision.go`

- Detect the host's `render` group GID dynamically (read `/etc/group` or use `getent group render`).
- During `intuneme init`, ensure a `render` group with the matching GID exists inside the container (create it if missing, or adjust the existing group's GID).
- Add `render` to the `usermod --groups` list (three call sites: lines 159, 170, 178).

### 2. Edge wrapper: add `--disable-gpu-sandbox`

**File:** `ubuntu-intune/system_files/usr/local/bin/microsoft-edge`

Add `--disable-gpu-sandbox` to the flags prepended before calling the real binary. This flag disables only the GPU process sandbox, not the full Chromium sandbox.

### 3. Azure VPN Client wrapper

**New file:** `ubuntu-intune/system_files/usr/local/bin/microsoft-azurevpnclient`

Create a wrapper script mirroring the Edge wrapper pattern:
- Always prepend `--disable-gpu-sandbox`.
- Include Wayland/ozone flags (matching Edge for consistency).
- Call the real binary at `/opt/microsoft/microsoft-azurevpnclient/microsoft-azurevpnclient`.
- Pass through all user arguments.

### 4. Add VPN client to PATH

**File:** `ubuntu-intune/system_files/etc/profile.d/intuneme-profile.sh`

Add `/opt/microsoft/microsoft-azurevpnclient` to `PATH` so users can launch it without the full path.

## Architecture alignment

Per the project rule of thumb:
- **Go CLI** (host-dependent): render group GID detection and provisioning.
- **Container image** (static): Chromium flag wrappers and PATH configuration.

## Testing

1. Rebuild container image.
2. Run `intuneme recreate` (or `destroy` + `init`).
3. Verify `id` inside container shows `render` group.
4. Launch Edge -- should render pages without crashing.
5. Launch Azure VPN Client -- auth prompt should appear and VPN should connect.
