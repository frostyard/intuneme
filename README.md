# intuneme
[![OpenSSF Scorecard](https://api.scorecard.dev/projects/github.com/frostyard/intuneme/badge)](https://scorecard.dev/viewer/?uri=github.com/frostyard/intuneme)

`intuneme` provisions and manages a [systemd-nspawn](https://www.freedesktop.org/software/systemd/man/systemd-nspawn.html) container running Microsoft Intune on an immutable Linux host. The container handles enrollment, compliance, and corporate resource access while making minimal changes to the host.

## Features

- Container lifecycle management (`init`, `start`, `stop`, `destroy`, `recreate`)
- Broker proxy for host-side single sign-on (Edge, VS Code, and other MSAL apps)
- Notify proxy for container desktop notifications on the host (Outlook mail and calendar in Edge): `intuneme config notify-proxy enable`, foreground debug run with `intuneme notify-proxy`
- Screencast proxy for screen and window sharing from the container's Edge (Teams meetings): `intuneme config screencast-proxy enable`, foreground debug run with `intuneme screencast-proxy`
- Run MCP servers inside the container, wired to host stdio (`mcp`) — for tools that must authenticate against the container's tenant
- Device hotplug — YubiKey and webcam passthrough via udev rules
- GNOME Quick Settings extension for start/stop and app launch without a terminal
- Desktop shortcuts for Microsoft Edge and Intune Portal

## Documentation

Full documentation is at **https://frostyard.github.io/intuneme/**

## Quick install

```bash
go install github.com/frostyard/intuneme@latest
```

`intuneme init` installs the root-owned helper from the binary you run. Later `start` runs will not replace that helper from a user-owned `~/go/bin` build; use `make install` or `sudo install` it to a root-owned path if you want automatic helper upgrades.

Alternatively, Arch Linux users can install `intuneme` or `intuneme-bin` from the Arch User Repository (AUR):

- [intuneme](https://aur.archlinux.org/packages/intuneme) (build from source)
- [intuneme-bin](https://aur.archlinux.org/packages/intuneme-bin) (pre-compiled binary)

## Problem records

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
validated and sudo authentication passed. User setup and authentication errors
(not initialized, not running, failed sudo authentication, a bad config, a
user-owned helper source, a missing sudoers rule) are never recorded.

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

## Contributing

See the [contributing guide](https://frostyard.github.io/intuneme/contributing/) on the docs site.

## License

MIT

### Helper security

App and MCP launches permanently disable setuid/file-capability elevation and
run as your configured numeric user. Container administration requiring sudo
must use an interactive `intuneme shell` session. The session restarts only the
Microsoft device broker through a scoped container D-Bus policy.

After upgrading, run `intuneme start` even if the container is already running.
It authenticates and refreshes the security policy without rebooting the
container. A failed policy update stops the command with an error.

Passwordless container shells are limited to your configured user and machine;
root shells require authentication. MCP mounts require Linux 5.12+ and accept a user-owned directory
and remain read-only. Install the CLI in a root-owned path before upgrading
with `intuneme start`; a user-owned development build cannot refresh privileged
helpers. Older running installations can still be stopped or recreated through
an authenticated fallback when the machine helper has not yet been installed.
