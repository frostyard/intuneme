<!-- readme-kind: tool -->
# intuneme

[![CI](https://github.com/frostyard/intuneme/actions/workflows/ci.yml/badge.svg)](https://github.com/frostyard/intuneme/actions/workflows/ci.yml) [![OpenSSF Scorecard](https://api.scorecard.dev/projects/github.com/frostyard/intuneme/badge)](https://scorecard.dev/viewer/?uri=github.com/frostyard/intuneme)

intuneme runs Microsoft Intune in a [systemd-nspawn](https://www.freedesktop.org/software/systemd/man/systemd-nspawn.html) container, for people who must enroll an immutable Linux host in their company's Intune. The container does the enrollment, the compliance checks and the corporate sign-in, and the host stays almost unchanged.

**Status:** pre-release. Releases are 0.x, so read the release notes before you upgrade.

**Documentation:** [frostyard.github.io/intuneme](https://frostyard.github.io/intuneme/)

## What it does

- **Container lifecycle.** `init`, `start`, `stop`, `destroy` and `recreate` manage the container; `recreate` keeps the enrollment. [Quick start](https://frostyard.github.io/intuneme/getting-started/quick-start/)
- **Experimental Himmelblau.** `init --himmelblau`, `edit --himmelblau` and `enroll --user` provide an alternative Entra/PAM stack with PIN-unlocked broker SSO, without Edge or Intune Portal, and with a local-build terminal-passkey variant that includes WorkIQ account discovery and broker redirect compatibility. [Setup and limitations](https://frostyard.github.io/intuneme/user-guide/himmelblau/)
- **Single sign-on on the host.** The broker proxy gives Edge, VS Code and other MSAL apps on the host the container's sign-in. [Broker proxy](https://frostyard.github.io/intuneme/user-guide/broker-proxy/)
- **Notifications and screen sharing.** The notify and screencast proxies show the container's Edge notifications on the host and let Teams share host screens. [Notifications](https://frostyard.github.io/intuneme/user-guide/notifications/), [Screen sharing](https://frostyard.github.io/intuneme/user-guide/screen-sharing/)
- **MCP servers in the tenant.** `intuneme mcp` runs a host MCP server inside the container, wired to host stdio, so it signs in to the container's tenant. [MCP servers](https://frostyard.github.io/intuneme/user-guide/mcp-servers/)
- **Devices.** YubiKeys and webcams pass through with udev hotplug. [Device hotplug](https://frostyard.github.io/intuneme/user-guide/device-hotplug/)
- **Desktop integration.** A GNOME Quick Settings extension starts and stops the container and opens apps, and desktop shortcuts open Edge and Intune Portal. [GNOME extension](https://frostyard.github.io/intuneme/user-guide/gnome-extension/)

## Install

```bash
go install github.com/frostyard/intuneme@latest
sudo install -m 0755 "$(go env GOPATH)/bin/intuneme" /usr/local/bin/intuneme
```

Install to a root-owned path, as the second line does: `intuneme start` refreshes its privileged helpers only from a root-owned binary. [Installation](https://frostyard.github.io/intuneme/getting-started/installation/) lists the `.deb` and `.rpm` packages, the AUR packages and the requirements.

## Use

```bash
intuneme init         # pull the image and provision the container
intuneme start        # boot it
intuneme open portal  # enroll with Intune Portal
```

The [command reference](https://frostyard.github.io/intuneme/reference/cli/intuneme/) lists every command and flag. [Helper security](https://frostyard.github.io/intuneme/reference/helper-security/) says what the privileged helpers allow.

## Problem records

A release build writes 1 record to the local systemd journal when it panics or when 1 of 4 internal errors happens, and it sends nothing over the network. Its structured fields hold the app ID, the version and a stable code, or for a panic the function names of the stack, and never file paths, line numbers or arguments. The original error text or panic value stays in the local journal `MESSAGE` field only. [Problem records](https://frostyard.github.io/intuneme/reference/problem-records/) lists the codes. To read them:

```bash
journalctl --user MESSAGE_ID=8915bdd215c84cbab0c31c05a1a33e7e
```

## Develop

```bash
mise install   # the pinned tools
make ci        # what CI runs
```

[Contributing](https://frostyard.github.io/intuneme/contributing/) explains each target.

## Where to go next

- [AGENTS.md](AGENTS.md): the rules for agents and contributors.
- [docs/README.md](docs/README.md): the decisions, designs, specs and plans.
- [SECURITY.md](SECURITY.md): report a vulnerability privately.
- [LICENSE](LICENSE): MIT.
