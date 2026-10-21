# 0001 — Make the helper privilege drop irreversible

- **Status:** Accepted
- **Date:** 2026-10-20

## Context

Namespace entry does not import nspawn's capability restrictions. A numeric UID
drop alone permits setuid or file-capability executables to regain host
privileges. Go runtime threads share filesystem state, which prevents mount
namespace entry until `CLONE_FS` is unshared.

## Decision

Keep namespace transitions on a locked OS thread with private filesystem state.
Before ForkExec, set `no_new_privs` and clear the capability bounding, ambient,
and inheritable sets. ForkExec drops numeric UID/GID and supplementary groups
before executing container code. Reject UID 0 and invalid kernel ID sentinels.
Authorize the one required broker restart through a static container polkit rule
limited to its unit and restart verb, instead of escalating the session process.

## Consequences

Setuid sudo is intentionally unavailable in app/MCP sessions. Interactive
container administration uses machinectl sessions. Kernel regression tests need
unshare, subordinate UID/GID maps and uidmap; CI requires them. Node evaluates
the actual shipped authorization policy. GUI/tenant authentication still needs
a smoke test on an enrolled installation.

## Alternatives considered

- **Only narrow sudoers:** does not prevent other setuid or capability-bearing binaries.
- **Only LockOSThread:** does not separate the filesystem state shared by Go threads.

## References

- Shapes: [overview](../design/overview.md), [container image](../specs/container-image.md)
