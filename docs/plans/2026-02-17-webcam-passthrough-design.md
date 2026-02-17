# Webcam Detection and Pass-Through

## Goal

Detect host webcams at container start and bind-mount them into the nspawn container so Teams video calls work in Edge.

## Approach

Direct V4L2 device bind-mount. Scan `/dev/video*` and `/dev/media*` at start time, bind each found device into the container at its same path. No hot-plug support — restart the container to pick up new devices.

## Why This Works

- Edge discovers V4L2 devices automatically when they exist at `/dev/video*`.
- The container user is already in the `video` group (set during `intuneme init`).
- No additional packages, environment variables, or config needed in the container image.

## Changes

### `internal/nspawn/nspawn.go`

Add `DetectVideoDevices()`:

1. Glob `/dev/video*` to find V4L2 video devices.
2. Glob `/dev/media*` to find associated media controller devices.
3. For each video device, read `/sys/class/video4linux/<name>/name` to get the human-readable camera name.
4. Return a struct containing `[]BindMount` and display names for logging.
5. Return empty results if no devices found (cameras are optional).

### `cmd/start.go`

After the existing `DetectHostSockets()` call:

1. Call `DetectVideoDevices()`.
2. Log each detected camera: `Detected webcam: /dev/video0 (Integrated Camera)`.
3. Log `No webcams detected` if none found.
4. Append camera mounts to the sockets slice before passing to `Boot()`.

### No Other Changes

- `BuildBootArgs` signature unchanged — camera mounts merge into the existing `[]BindMount` slice.
- Container image (`ubuntu-intune/`) unchanged — no new packages or config.
- `internal/provision/` unchanged — no new fixups.
- Profile.d scripts unchanged — no new environment variables.
