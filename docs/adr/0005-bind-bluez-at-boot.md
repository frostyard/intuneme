# 0005 — Bind the filtered BlueZ directory at boot

- **Status:** Accepted
- **Date:** 2026-11-11

## Context

Terminal-passkey enrollment needs a temporary filtered connection to host
BlueZ. The machine helper intentionally supports only the fixed MCP bind
destination; general runtime mounts and group-wide machine management were
removed by ADR-0002.

## Decision

For terminal-passkey images, `start` prepares the user-owned, mode-0700
`runtime/himmelblau-bluez` directory and binds it read-only at
`/run/intuneme/bluez-proxy` during authenticated container boot. It starts no
proxy. `enroll` checks the mount through the existing numeric-user exec
helper, starts `xdg-dbus-proxy` only for PAM, and removes its socket afterward.
The host system bus is never bound directly.

Enrollment refreshes the scoped security artifacts after host sudo
authentication. Reboot and service-readiness operations use that authenticated
sudo session, not a root shell grant. PAM still runs through the irreversible
privilege-drop helper and propagates its real exit status.

## Consequences

The MCP helper and its authorization remain unchanged. Existing running
containers need a stop/start to acquire the boot-time bind; enrollment reports
that requirement rather than falling back to broad machine management.
The empty private directory remains mounted for the container's lifetime.

## Alternatives considered

- **Extend the machine helper with arbitrary destinations:** restores authority
  deliberately removed by ADR-0002.
- **Add a second privileged runtime operation for BlueZ:** adds a new root
  interface when the existing authenticated boot can establish the mount.

## References

- Shapes: [Container Lifecycle](../design/container-lifecycle.md),
  [Container Images](../specs/container-image.md)
- Builds on: [ADR-0002](0002-pin-machine-operations-and-scope-shell-authorization.md)
