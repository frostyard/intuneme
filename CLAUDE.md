# intuneme

Go CLI tool that provisions and manages a systemd-nspawn container running Microsoft Intune on an immutable Linux host.

## Architecture

The repository has two main components:

- **Go CLI** (`cmd/`, `internal/`) — Responsible for container lifecycle: init, start, stop, destroy, shell. Handles host-specific setup (user creation, hostname, polkit rules) that varies per machine.
- **Container image** (`ubuntu-intune/`) — Containerfile + build scripts + system files that define the container contents. Packages, systemd unit overrides, PAM config, static config files, and the Edge wrapper all live here.

**Rule of thumb:** The Go CLI starts and stops things. The container image defines what's inside the container. If something is static and doesn't depend on the host (packages, service overrides, config files), it belongs in `ubuntu-intune/`. If it depends on the host user/UID/hostname, it stays in `internal/provision/`.

## Executing commands inside the container

`nspawn.Exec()` uses `sudo nsenter` to enter the container's namespaces and run a command as the user via `su`. This is the only reliable approach for launching GUI apps (Edge, Portal) non-interactively:

- **nsenter + nohup &** — enters namespaces, runs in a login bash (gets correct PATH for wrappers at `/usr/local/bin/`), backgrounds the process. The orphaned process is reparented to PID 1 inside the container. Proven and reliable.
- **machinectl shell** — uses polkit (good), but puts processes in a session scope that systemd cleans up when the shell exits (kills GUI apps).
- **systemd-run --machine** — requires root (direct bus transport), bypasses polkit. Permission denied as a normal user.
- **machinectl shell + systemd-run --user** — the transient user service has a minimal PATH that skips `/usr/local/bin/` wrappers, and the sanitized environment breaks X11 auth.

A sudoers rule at `/etc/sudoers.d/intuneme-exec` (installed by `intuneme init`, persists across start/stop, removed by `intuneme destroy`) makes the nsenter command passwordless, so the GNOME extension can launch apps without a terminal for sudo prompts. The rule does not authorize `nsenter` directly: it authorizes a single root-owned helper script `/usr/local/libexec/intuneme/nsenter-exec`, which hard-codes the `nsenter`/`su` shape and takes only the leader PID and the script as arguments. This indirection is required because **sudo-rs** (the default `sudo`/`su` on Ubuntu 25.10+) forbids wildcards inside command arguments. The old rule used `*` for the leader PID and script and was rejected, printing an error on *every* `sudo` call (issue #168). The wildcard-free helper rule keeps the same effective authorization as the old rule (the leader PID and script are still caller-controlled), and the helper is installed `0755` root:root so the user can't edit code that runs as root. The `start` command reinstalls the rule and helper idempotently if missing; `sudoers.IsInstalled()` requires both to exist, so upgrades from the old wildcard rule self-heal.

**Session setup runs on the nsenter path too.** The nsenter shell is *non-login*, so it never sources `/etc/profile.d`. `nspawn.Exec()` therefore runs `/usr/local/bin/intuneme-session-setup` (the shared script that `/etc/profile.d/intuneme.sh` also sources) before launching the app. That script pushes `DISPLAY`/`XAUTHORITY` into the **D-Bus activation environment** (`dbus-update-activation-environment`) and unlocks gnome-keyring. This is mandatory: the Microsoft identity broker is a GTK app that the session D-Bus daemon activates on demand — without a display in the activation environment it dies on startup (`cannot open display`) and **Edge can't authenticate**. `start` reinstalls the script if missing. See `yeti/OVERVIEW.md` → "Session Setup".

## Running MCP servers in the container (`intuneme mcp`)

`intuneme mcp` runs a host-provided MCP server binary *inside* the container with its stdio wired to a host client (e.g. VS Code). Use it when a server must authenticate against the container's enrolled tenant and the broker proxy can't help — most notably a two-tenant setup (one tenant on the host, a second inside intuneme), since a D-Bus well-known name has one owner per bus and can't represent two brokers.

Key design points:

- **Foreground exec, no TTY.** It uses `nspawn.ExecForeground()` (not `Exec()`): same `nsenter` shape and sudoers rule, but `exec` with attached stdio instead of `nohup … &`. Backgrounding or allocating a TTY corrupts JSON-RPC framing.
- **Binary stays out of the rootfs.** The server binary lives on the host (`mcp_binary` config key or `--binary`). Its directory is bind-mounted read-only at `/opt/intuneme-mcp` via `machinectl bind` (authorized passwordless by the polkit `manage-machines` rule). The bind is runtime-only and re-established on demand, so it **survives `intuneme recreate`** — nothing is added to `ubuntu-intune/`.
- **Server-agnostic.** No assumption about a particular tool; any self-contained MCP server works. The server's own arguments come from the `mcp_args` config key (e.g. `["mcp"]` for `workiq mcp`), with trailing `intuneme mcp -- args...` overriding them — this keeps the VS Code config to just `["mcp"]`. A `DOTNET_BUNDLE_EXTRACT_BASE_DIR` env var is set (harmless for non-.NET) so single-file .NET servers extract to ephemeral `/tmp` and the mount can stay read-only.

## Before committing

Always run `make fmt` and `make lint` before committing. Fix any lint errors before creating commits.

## Documentation

Update `README.md` when adding new commands, flags, or changing existing functionality. The README contains a commands table and feature sections that must stay in sync with the code.

**update documentation** After any change to source code, update relevant documentation in CLAUDE.md, README.md and the yeti/ folder. A task is not complete without reviewing and updating relevant documentation.

**yeti/ directory** The `yeti/` directory contains documentation written for AI consumption and context enhancement, not primarily for humans. Jobs like `doc-maintainer` and `issue-worker` instruct the AI to read `yeti/OVERVIEW.md` and related files for codebase context before performing tasks. Write content in this directory to be maximally useful to an AI agent understanding the codebase — detailed architecture, patterns, and decision rationale rather than user-facing guides.
