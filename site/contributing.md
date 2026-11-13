# Contributing

Contributions are welcome. This page covers how to build and test the project locally, and how to submit changes.

## Tools

The tool versions are pinned in `mise.toml`, with checksums in `mise.lock`: golangci-lint and Node.js (for the template gate). Go comes from the `go` line of `go.mod`. Install them with [mise](https://mise.jdx.dev/):

```bash
git clone https://github.com/frostyard/intuneme.git
cd intuneme
mise install
```

## Building from source

```bash
make build
```

The binary is written to `./intuneme`.

## The gate

```bash
make verify   # tidy diff, vet, gofmt check, pinned golangci-lint, template gate, race tests
make check    # make fmt, then make verify
make ci       # verify, the corrections check, govulncheck and the build: what CI runs
```

`make verify` changes no file. `make ci BASE=origin/main` also checks that no record in `.memory/corrections/` was removed, renamed or changed since `origin/main`, except a filled empty `promoted_to`. `make lint` fails when the installed golangci-lint is not the `mise.toml` pin; run `mise install` to fix it.

Tests use the Go standard library `testing` package. No external test framework is required. The kernel boundary test (`internal/nshelper`) needs `unshare` and subordinate UID/GID maps, and skips without them; CI sets `INTUNEME_REQUIRE_KERNEL_TESTS=1` so that it fails instead of skipping there.

## Linting and formatting

Before committing, always run:

```bash
make check
```

Fix any reported issues before creating a commit. CI runs the same checks and fails if there are lint errors or unformatted code.

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
3. Run `make check` and fix any issues.
4. Open a pull request against `main`. Fill in the pull request template, including the risk tier. Include a short description of what the change does and why.

For non-trivial changes, open an issue first to discuss the approach before investing time in an implementation.

## Reporting bugs

Open an issue on [GitHub Issues](https://github.com/frostyard/intuneme/issues). Include:

- Your host OS and version
- The output of `intuneme status`
- The exact command you ran and the error message
- Any relevant output from `journalctl -t intuneme-hotplug` or `journalctl -u systemd-nspawn@intuneme`

Report a security vulnerability privately, as [SECURITY.md](https://github.com/frostyard/intuneme/blob/main/SECURITY.md) says, never in a public issue.
