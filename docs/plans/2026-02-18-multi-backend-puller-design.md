# Multi-Backend Container Image Puller

## Problem

The `init` command hardcodes podman for pulling and extracting OCI images. Users without podman installed cannot use intuneme, even if they have docker or skopeo+umoci available.

## Decision

Support three pull backends with automatic detection in preference order:

1. **podman** (preferred — daemonless, widely available)
2. **skopeo + umoci** (lightweight pair, both in Debian repos)
3. **docker** (requires daemon, but common fallback)

Error if none are available.

## Design

### New package: `internal/puller`

```go
// Puller pulls a container image from a registry and extracts it to a rootfs directory.
type Puller interface {
    Name() string
    PullAndExtract(r runner.Runner, image string, rootfsPath string) error
}

// Detect returns the first available Puller in preference order.
func Detect(r runner.Runner) (Puller, error)
```

### Backend: PodmanPuller

Exactly the current logic, unified into one method:

1. `podman pull <image>`
2. `podman create --name intuneme-extract <image>`
3. `podman export -o <tmpTar> intuneme-extract`
4. `sudo tar -xf <tmpTar> -C <rootfsPath>` (RunAttached for sudo prompt)
5. `podman rm intuneme-extract`
6. Clean up temp tar

### Backend: SkopeoPuller

Requires both `skopeo` and `umoci` to be detected:

1. `skopeo copy docker://<image> oci:<tmpDir>:latest`
2. `sudo umoci raw unpack --image <tmpDir>:latest <rootfsPath>`
3. Clean up `<tmpDir>`

Uses `sudo` on umoci to preserve container-internal UIDs (same rationale as `sudo tar` in the podman path).

### Backend: DockerPuller

Same create+export pattern as podman:

1. `docker pull <image>`
2. `docker create --name intuneme-extract <image>`
3. `docker export -o <tmpTar> intuneme-extract`
4. `sudo tar -xf <tmpTar> -C <rootfsPath>` (RunAttached for sudo prompt)
5. `docker rm intuneme-extract`
6. Clean up temp tar

### Detection logic

```go
func Detect(r runner.Runner) (Puller, error) {
    if _, err := r.LookPath("podman"); err == nil {
        return &PodmanPuller{}, nil
    }
    if _, err := r.LookPath("skopeo"); err == nil {
        if _, err := r.LookPath("umoci"); err == nil {
            return &SkopeoPuller{}, nil
        }
    }
    if _, err := r.LookPath("docker"); err == nil {
        return &DockerPuller{}, nil
    }
    return nil, fmt.Errorf("no container tool found; install podman, skopeo+umoci, or docker")
}
```

## Changes to existing code

### `internal/prereq/prereq.go`

Remove `podman` from the hard requirements. Only `systemd-nspawn` and `machinectl` remain — these are needed by all commands (start, stop, shell), not just init. Pull tool availability is checked at init time by `puller.Detect()`.

### `internal/provision/provision.go`

Delete `PullImage()` and `ExtractRootfs()`. Their logic moves into the puller implementations.

### `cmd/init.go`

Replace the pull+extract sequence:

```go
// Detect pull backend
p, err := puller.Detect(r)
if err != nil {
    return err
}

image := pkgversion.ImageRef()
fmt.Printf("Pulling OCI image %s (via %s)...\n", image, p.Name())
if err := os.MkdirAll(cfg.RootfsPath, 0755); err != nil {
    return fmt.Errorf("create rootfs dir: %w", err)
}
if err := p.PullAndExtract(r, image, cfg.RootfsPath); err != nil {
    return err
}
```

## Testing

Each puller implementation is testable via the `runner.Runner` interface — mock the `Run`, `RunAttached`, and `LookPath` calls to verify the correct command sequences. `Detect` is testable by mocking `LookPath` to simulate different tool availability.

## Non-goals

- No config option to force a specific backend (automatic detection only)
- No Go library approach (previous experience showed incomplete whiteout handling)
- No support for buildah, ctr, nerdctl, or crane CLI
