# Problem records

A release build of intuneme writes 1 record to the local systemd journal
(`MESSAGE_ID=8915bdd215c84cbab0c31c05a1a33e7e`, schema `frostyard.problem/1`)
when it panics, or when 1 of 4 internal errors happens:

| Code | Meaning |
| --- | --- |
| `start.boot_failed` | `systemd-nspawn` did not start, or the machine did not register within 30 seconds |
| `start.linger_failed` | `start` could not enable linger for the container user |
| `start.proxy_failed` | `start` could not start the broker, notify or screencast proxy |
| `open.session_bus_timeout` | `open` (or the GNOME extension) could not launch the app because the container user session bus did not appear within 30 seconds |

A code is recorded only for an internal failure after the config was
validated and sudo authentication passed, and only when the configured user
and UID match the user who runs intuneme and an account in the container.
`open.session_bus_timeout` is recorded only for the exact timeout message of
the configured UID. User setup and authentication errors (not initialized, not
running, failed sudo authentication, a bad config, a user-owned helper source,
a missing sudoers rule) are never recorded.

The structured fields hold the app ID (`org.frostyard.intuneme`), the version,
the runtime (`go`), the component (`cli`), the class (`panic` or `error`), and
the code or, for a panic, the function names of the stack (no file paths, no
line numbers, no arguments). The error text or panic value stays in the local
journal `MESSAGE` field only. A panic in another goroutine or a fatal runtime
error goes to a crash file under
`$XDG_STATE_HOME/frostyard-problems/org.frostyard.intuneme/` (default
`~/.local/state/...`), and the next intuneme run turns it into a record.

Only release builds record: a `dev`, `git describe` (`v0.21.0-2-gabc1234`,
`-dirty`) or snapshot version records nothing. intuneme sends nothing, opens no
network connection, and adds no flag or output for this. chairlift's Problems
page can show the records. To read them:

```bash
journalctl --user MESSAGE_ID=8915bdd215c84cbab0c31c05a1a33e7e
```

`--user` needs a persistent journal (`Storage=persistent` or `auto` with
`/var/log/journal`). On a volatile journal, use
`journalctl MESSAGE_ID=8915bdd215c84cbab0c31c05a1a33e7e`.
