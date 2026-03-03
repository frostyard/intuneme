# Fix Render Group GID Conflict

**Date:** 2026-03-03
**Issue:** [#33](https://github.com/frostyard/intuneme/issues/33)

## Problem

`intuneme init` calls `groupmod --gid <hostGID> render` inside the container to sync the render group's GID with the host. This fails with exit status 4 when another container group (e.g. `systemd-resolve`) already occupies that GID.

The result is broken GPU/3D acceleration inside the container because the container user cannot access `/dev/dri/renderD128`.

## Solution

Extend `EnsureRenderGroup` to detect and resolve GID conflicts before setting the render group's GID. When the target GID is occupied by another group, reassign that group to a free system GID first.

## Approach

Use the existing pattern: read group files in Go, mutate via `groupmod`/`groupadd` inside `systemd-nspawn`.

### New helpers in `internal/provision/`

**`findGroupByGID(groupPath string, gid int) (string, error)`** — Reverse lookup: given a GID, return the group name that owns it, or `""` if no group has that GID.

**`findFreeSystemGID(groupPath string) (int, error)`** — Scan the container's `/etc/group`, collect all used GIDs, return the first available GID in the system range (scanning 999 down to 100).

### Modified `EnsureRenderGroup` flow

1. Read container `/etc/group` for render group's current GID
2. If render GID already matches host GID → done
3. Check if any other group holds the target host GID (`findGroupByGID`)
4. If conflict exists:
   a. Find a free system GID (`findFreeSystemGID`)
   b. `groupmod --gid <freeGID> <conflictingGroup>` to move it out of the way
   c. Print informational message about the reassignment
5. `groupmod` or `groupadd` render to the host GID (existing logic)

### Tests

- `TestFindGroupByGID` — correct group name, missing GID returns "", malformed entries
- `TestFindFreeSystemGID` — finds free GID in sparse file, handles nearly-full range
- `TestEnsureRenderGroup` — new case for GID conflict (mock runner verifies two `groupmod` calls in correct order)

## What doesn't change

- `FindHostRenderGID` — unchanged
- `findGroupGID` — unchanged
- Call site in `cmd/init.go` — unchanged (already treats failures as warnings)
- `userGroups` function — unchanged
