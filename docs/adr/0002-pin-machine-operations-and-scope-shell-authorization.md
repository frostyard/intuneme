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
and retain it while machined resolves `/proc/<helper-pid>/fd/<fd>`. Sanitize the
machinectl environment and force the fixed destination and read-only flag.
Reconcile security artifacts before start's already-running return. Fail
explicitly on migration errors. Allow authenticated poweroff when the machine
helper is absent, never when an existing helper rejects an operation.

## Consequences

Requires Linux openat2 support (5.6+); unsupported kernels fail closed. Normal
host path aliases are resolved by the unprivileged CLI first. Broker enable-linger
needs authenticated sudo, while ordinary user shells remain passwordless.
Kernel tests use disposable namespaces and substitute the machined transport
with a separate process performing actual read-only mounts; live machined and
enrolled GUI authentication need deployment smoke tests.

## Alternatives considered

- **stat -L followed by machinectl bind:** still permits replacement races.
- **Broad machine1.shell permission:** permits root, bypassing container sudo policy.
- **Silent migration warnings:** leaves vulnerable policies active while reporting success.

## References

- Shapes: [overview](../design/overview.md), [lifecycle](../design/container-lifecycle.md)
- Builds on: [ADR-0001](0001-make-helper-privilege-drop-irreversible.md)
