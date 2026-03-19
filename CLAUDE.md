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

A sudoers rule at `/etc/sudoers.d/intuneme-exec` (installed by `intuneme init`, persists across start/stop, removed by `intuneme destroy`) makes the nsenter command passwordless, so the GNOME extension can launch apps without a terminal for sudo prompts. The `start` command reinstalls it idempotently if missing (handles upgrades from older versions).

## Before committing

Always run `make fmt` and `make lint` before committing. Fix any lint errors before creating commits.

## Documentation

Update `README.md` when adding new commands, flags, or changing existing functionality. The README contains a commands table and feature sections that must stay in sync with the code.
