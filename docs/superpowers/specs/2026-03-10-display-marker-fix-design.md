# Fix WriteDisplayMarker permission denied

Issue: https://github.com/frostyard/intuneme/issues/60

## Problem

`nspawn.WriteDisplayMarker` writes to `<rootfs>/etc/intuneme-host-display` using
`os.WriteFile` as the unprivileged user. The rootfs `/etc/` is owned by root
(extracted from OCI image), so this fails with "permission denied".

## Solution

Change `WriteDisplayMarker` to accept a `runner.Runner` and use the temp-file +
`sudo install` pattern already established in `provision.sudoWriteFile`:

1. Write marker content to a temp file (user-writable location)
2. Use `sudo install -m 0644` to copy into rootfs
3. Clean up the temp file

## Changes

| File | Change |
|------|--------|
| `internal/nspawn/nspawn.go` | Add `runner.Runner` param to `WriteDisplayMarker`, implement sudo install |
| `cmd/start.go` | Pass `r` to `WriteDisplayMarker` |
| `internal/nspawn/nspawn_test.go` | Update test to pass runner |

## What stays the same

- `HostDisplay()` function
- `displayMarkerPath` constant
- Marker file content format (`DISPLAY=:N\n`)
