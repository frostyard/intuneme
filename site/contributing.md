# Contributing

Contributions are welcome. This page covers how to build and test the project locally, and how to submit changes.

## Building from source

Prerequisites: Go 1.26 or later, `make`.

```bash
git clone https://github.com/frostyard/intuneme.git
cd intuneme
make build
```

The binary is written to `./intuneme`.

## Running tests

```bash
make test
```

Tests use the Go standard library `testing` package. No external test framework is required.

## Linting and formatting

The project uses `golangci-lint`. Before committing, always run:

```bash
make fmt && make lint
```

Fix any reported issues before creating a commit. The CI pipeline runs the same checks and will fail if there are lint errors or unformatted code.

## Branch naming

Use a short, descriptive branch name with a `feat/`, `fix/`, or `chore/` prefix:

```
feat/webcam-hotplug
fix/broker-proxy-pid
chore/update-deps
```

## Pull request process

1. Fork the repository and create a branch from `main`.
2. Make your changes, following the patterns in the existing code.
3. Run `make fmt && make lint` and fix any issues.
4. Run `make test` and ensure all tests pass.
5. Open a pull request against `main`. Include a short description of what the change does and why.

For non-trivial changes, open an issue first to discuss the approach before investing time in an implementation.

## Reporting bugs

Open an issue on [GitHub Issues](https://github.com/frostyard/intuneme/issues). Include:

- Your host OS and version
- The output of `intuneme status`
- The exact command you ran and the error message
- Any relevant output from `journalctl -t intuneme-hotplug` or `journalctl -u systemd-nspawn@intuneme`
