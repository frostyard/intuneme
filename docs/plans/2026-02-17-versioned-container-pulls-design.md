# Versioned Container Pulls

## Problem

The CLI always pulls `ghcr.io/frostyard/ubuntu-intune:latest` regardless of which version of the CLI is running. GitHub Actions now tags the container image with the git tag (e.g., `v0.4.0`) on tag events. The CLI should pull the matching container version when it's a release build.

## Decision

- Release builds (goreleaser) pull a version-pinned image tag
- Local/dev builds pull `latest`
- No config override — the image is always derived from the CLI version

## Design

### New package: `internal/version`

Single file `version.go` with:

- `Version` var (default `"dev"`, set from `main.go` at startup via the existing ldflags-injected value)
- `ImageRef() string` — returns the full image reference:
  - Clean semver (`^v?\d+\.\d+\.\d+$`) -> `ghcr.io/frostyard/ubuntu-intune:v<major>.<minor>.<patch>`
  - Anything else -> `ghcr.io/frostyard/ubuntu-intune:latest`
- Ensures `v` prefix on the tag since container images are tagged with the git tag (which includes `v`)
- Semver detection via stdlib `regexp`, no external dependency

### Changes to `main.go`

Set `version.Version = version` before `fang.Execute()`.

### Changes to `cmd/init.go`

Replace `cfg.Image` references with `version.ImageRef()`. Print the resolved image ref so the user sees what's being pulled.

### Changes to `internal/config/config.go`

Remove `Image` field from `Config` struct and its default in `Load()`. Existing config files with an `image` key are silently ignored by the TOML decoder.

### Testing

Table-driven tests in `internal/version/version_test.go`:

| Version input | Expected tag |
|---|---|
| `"dev"` | `latest` |
| `"0.4.0"` | `v0.4.0` |
| `"v0.4.0"` | `v0.4.0` |
| `"v0.4.0-2-g98e23e6"` | `latest` |
| `"v0.4.0-dirty"` | `latest` |
| `"none"` | `latest` |
| `""` | `latest` |

## Version flow

```
goreleaser: -X main.version=0.4.0 -> version.Version="0.4.0" -> ImageRef()="ghcr.io/.../ubuntu-intune:v0.4.0"
make build: -X main.version=v0.4.0-2-g98e23e6 -> version.Version="v0.4.0-2-g98e23e6" -> ImageRef()="ghcr.io/.../ubuntu-intune:latest"
no flags:   version="dev" -> version.Version="dev" -> ImageRef()="ghcr.io/.../ubuntu-intune:latest"
```
