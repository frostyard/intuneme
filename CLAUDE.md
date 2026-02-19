# intuneme

Go CLI tool that provisions and manages a systemd-nspawn container running Microsoft Intune on an immutable Linux host.

## Architecture

The repository has two main components:

- **Go CLI** (`cmd/`, `internal/`) — Responsible for container lifecycle: init, start, stop, destroy, shell. Handles host-specific setup (user creation, hostname, polkit rules) that varies per machine.
- **Container image** (`ubuntu-intune/`) — Containerfile + build scripts + system files that define the container contents. Packages, systemd unit overrides, PAM config, static config files, and the Edge wrapper all live here.

**Rule of thumb:** The Go CLI starts and stops things. The container image defines what's inside the container. If something is static and doesn't depend on the host (packages, service overrides, config files), it belongs in `ubuntu-intune/`. If it depends on the host user/UID/hostname, it stays in `internal/provision/`.

## Before committing

Always run `make fmt` and `make lint` before committing. Fix any lint errors before creating commits.

## Documentation

Update `README.md` when adding new commands, flags, or changing existing functionality. The README contains a commands table and feature sections that must stay in sync with the code.
