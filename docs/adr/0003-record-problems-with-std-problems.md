# 0003 — Record problems with std/problems

- **Status:** Accepted
- **Date:** 2026-11-04

## Context

A panic of the intuneme CLI, or an internal failure of `start` or `open`,
leaves only the stderr of 1 terminal. When the GNOME extension runs the
command, nobody sees even that. Nobody can count these problems, and
chairlift cannot show them.

Frostyard decided in [core ADR-0069](https://github.com/frostyard/core/blob/main/docs/adr/0069-report-problems-local-first-opt-in-with-one-sender-per-host.md)
that apps record panics and coded errors as journal entries with 1 fixed
`MESSAGE_ID` (`8915bdd215c84cbab0c31c05a1a33e7e`), schema
`frostyard.problem/1` of core's problem report spec, through the `problems`
package of `github.com/frostyard/std` (v0.4.0). Apps never send: chairlift and
`snosi-health` are the only senders, after the user or admin opts in. The
package writes 1 datagram to the local journal socket, drops what does not
fit, caps a process at 10 records a minute, and is inert when the version is
not a release semver. `org.frostyard.intuneme` is in the schema's app ID
allowlist.

The intuneme binary is also the privileged `nsenter-exec` and `machine-exec`
helper, dispatched by `argv[0]` and run as root under sudo.

## Decision

- `main()` makes 1 recorder (`internal/problemrec.Init`) with app ID
  `org.frostyard.intuneme`, component `cli` and the stamped `main.version`
  without 1 leading `v`. It does this only after the helper dispatch, so the
  root helpers never record. It defers `RecoverPanic` and turns on
  `CrashOutput`; a panic still prints its trace and exits 2.
- intuneme records exactly 4 coded errors, with the original error, where it
  returns them unchanged: `start.boot_failed`, `start.linger_failed`,
  `start.proxy_failed` and `open.session_bus_timeout`.
- A code is recorded only for an internal failure after config validation
  and sudo authentication passed. User setup and authentication errors are
  never recorded: the config load, the rootfs check, `sudo -v` and the
  security reconcile of `start` (which fails for a bad config identity or a
  user-owned helper source), and any `open` launch failure other than the
  session bus timeout (which can be a missing sudoers rule or a sudo
  password prompt). The 3 `start` codes run only after `sudo -v` and the
  security reconcile passed; dry run never reaches them.
- A code is also recorded only when the config identity matches the running
  user and the container account (`identityConsistent` in `cmd`): `host_uid`
  is the running non-root UID, `host_user` is a plausible non-root name, and
  the container's `/etc/passwd` has that name with that UID. Any read or
  parse error fails closed: a lost record is better than a wrong one.
- `open.session_bus_timeout` is recorded only when 1 output line is exactly
  the prologue's timeout diagnostic for the configured UID. A substring is
  not enough: sudo quotes the submitted script, which holds the `echo` of
  that message, in a policy-denial message.
- Codes are a stable contract: a new code is added, never renamed or reused.
- The error text goes only to the journal `MESSAGE` field, never to a
  `FROSTYARD_*` field.
- intuneme sends nothing over a network for problems, and adds no flag,
  setting or output. A dev, `git describe` or snapshot version records
  nothing.

## Consequences

- chairlift's Problems page can list intuneme's panics and internal errors,
  and a user who opts in can send their counts.
- The local journal keeps the error text, which can name the host user, the
  machine name or a path. Panic frames are function names only.
- Every release-build run creates a small crash file under
  `$XDG_STATE_HOME/frostyard-problems/org.frostyard.intuneme/`; the next run
  removes the files of finished processes.
- `make build` stamps `git describe` and records nothing; only GoReleaser
  release builds (and a `make build` exactly at a tag) record.
- Tests that write records must stay within the per-process cap of 10 a
  minute, which they cannot reset.

## Alternatives considered

- **Record every security reconcile or launch failure:** these fail for user
  setup and authentication too, so the records would not show defects of
  intuneme.
- **Record every returned error:** most are user mistakes or host setup; they
  say nothing about intuneme's code and would use up the rate cap.
- **Record in the root helpers too:** a root process would write to the
  system journal and the root user's state folder, and the helpers must stay
  minimal.
- **Send reports from intuneme:** core ADR-0069 allows 1 sender per host,
  after consent; intuneme is not it.

## References

- Shapes: [design/overview.md → Problem Records](../design/overview.md#problem-records)
- Builds on: [core ADR-0069](https://github.com/frostyard/core/blob/main/docs/adr/0069-report-problems-local-first-opt-in-with-one-sender-per-host.md), [core problem report spec](https://github.com/frostyard/core/blob/main/docs/specs/problem-report.md), [ADR-0001](0001-make-helper-privilege-drop-irreversible.md) (the helper dispatch)
