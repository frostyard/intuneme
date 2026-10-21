# 0002 — Pin machine operations and scope shell authorization

- **Status:** Accepted
- **Date:** 2026-10-20

## Context

A group-wide machine1.shell authorization includes root and unrelated machines.
A bind source checked by pathname can resolve to a different object at mount
time; lstat ownership of a symlink does not authorize its destination. Existing
running installations lack new helper artifacts and must be migratable without
recreate or an unauthenticated management grant.

## Decision

Authorize shell by caller UID, configured machine and configured non-root user.
Use a separate argv[0] of the trusted Go binary for fixed stop/bind operations.
Open bind sources with openat2 without symlinks, validate the descriptor owner,
and clone it with `open_tree`. Set read-only/nosuid/nodev with `mount_setattr`.
Pin the leader process, validate its root and user namespace, enter its mount
namespace after `unshare(CLONE_FS)`, and attach the clone with `move_mount` to
the fixed destination opened beneath the pinned root without symlinks. All
mount operations use descriptors. Sanitize the machinectl environment for stop.
Reconcile security artifacts before start's already-running return. Fail
explicitly on migration errors. Allow authenticated poweroff when the machine
helper is absent, never when an existing helper rejects an operation.

## Consequences

Requires Linux mount_setattr support (5.12+); unsupported kernels fail closed. Normal
host path aliases are resolved by the unprivileged CLI first. Broker enable-linger
needs authenticated sudo, while ordinary user shells remain passwordless.
Kernel tests exercise the actual native mount implementation in disposable
namespaces, checking source replacement, target-root mismatch, destination
symlinks and read-only/nosuid/nodev flags. Live enrolled GUI authentication
still needs a deployment smoke test.

## Alternatives considered

- **stat -L followed by machinectl bind:** still permits replacement races.
- **Descriptor pathname passed to machined:** older systemd chase implementations
  reconstruct a pathname, losing the pin before mounting.
- **Broad machine1.shell permission:** permits root, bypassing container sudo policy.
- **Silent migration warnings:** leaves vulnerable policies active while reporting success.

## References

- Shapes: [overview](../design/overview.md), [lifecycle](../design/container-lifecycle.md)
- Builds on: [ADR-0001](0001-make-helper-privilege-drop-irreversible.md)
