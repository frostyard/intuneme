# Design: intuneme stop should wait for container to fully stop

**Issue:** [#59](https://github.com/frostyard/intuneme/issues/59)
**Date:** 2026-03-10

## Problem

`intuneme stop` returns before the container is fully deregistered from systemd-machined. `machinectl poweroff` signals shutdown but the machine remains visible to `machinectl show` for a brief window afterward. This causes `intuneme stop && intuneme start` to fail because `start` sees the container as still running.

## Solution

Add a poll loop in `cmd/stop.go` after `nspawn.Stop()` returns. Poll `nspawn.IsRunning()` every 500ms for up to 30 seconds. Only print "Container stopped." once `IsRunning()` returns false. Return an error if the container doesn't stop within the timeout.

This mirrors the existing pattern in `cmd/start.go` where a poll loop waits for the container to come up after `nspawn.Boot()`.

## Design decisions

- **Poll loop lives in `cmd/stop.go`, not in `nspawn.Stop()`** — keeps `nspawn.Stop()` as a thin wrapper around `machinectl poweroff`, consistent with how `nspawn.Boot()` and the start-side poll loop are separated.
- **30-second timeout** — matches the start-side timeout.
- **500ms poll interval, 60 iterations** — responsive without being wasteful. Note: the start-side loop uses 1s intervals with 30 iterations; 500ms is more appropriate for shutdown which is typically faster than boot.
- **Broker proxy stop is already handled** — `broker.StopByPIDFile()` already polls for process exit for up to 5 seconds.

## Updated stop command flow

1. Check `IsRunning()` — if not running, print message and exit
2. Stop broker proxy (if enabled) — already waits up to 5s for process exit
3. Print "Stopping container..."
4. Call `nspawn.Stop()` — sends poweroff signal
5. Poll `IsRunning()` every 500ms for up to 30s
6. If still running after 30s, return error: `"container did not stop within 30 seconds"`
7. Print "Container stopped."

## Files changed

- `cmd/stop.go` — add poll loop between `nspawn.Stop()` and success message
