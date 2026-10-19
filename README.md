# intuneme
[![OpenSSF Scorecard](https://api.scorecard.dev/projects/github.com/frostyard/intuneme/badge)](https://scorecard.dev/viewer/?uri=github.com/frostyard/intuneme)

`intuneme` provisions and manages a [systemd-nspawn](https://www.freedesktop.org/software/systemd/man/systemd-nspawn.html) container running Microsoft Intune on an immutable Linux host. The container handles enrollment, compliance, and corporate resource access while making minimal changes to the host.

## Features

- Container lifecycle management (`init`, `start`, `stop`, `destroy`, `recreate`)
- Broker proxy for host-side single sign-on (Edge, VS Code, and other MSAL apps)
- Notify proxy for container desktop notifications on the host (Outlook mail and calendar in Edge)
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
