# Multi-Backend Container Image Puller Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Support podman, skopeo+umoci, and docker as image pull backends with automatic detection.

**Architecture:** New `internal/puller` package with a `Puller` interface and three implementations. A `Detect()` function probes for available tools in preference order. Each backend handles the full pull-and-extract-to-rootfs flow. The existing `PullImage`/`ExtractRootfs` functions are deleted from `provision` and replaced by the puller abstraction.

**Tech Stack:** Go stdlib, `runner.Runner` interface for command execution.

**Design doc:** `docs/plans/2026-02-18-multi-backend-puller-design.md`

---

### Task 1: Create puller package with interface and Detect function

**Files:**
- Create: `internal/puller/puller.go`
- Create: `internal/puller/puller_test.go`

**Step 1: Write the failing tests for Detect**

Create `internal/puller/puller_test.go`:

```go
package puller

import (
	"fmt"
	"strings"
	"testing"
)

type mockRunner struct {
	available map[string]bool
	commands  []string
}

func (m *mockRunner) Run(name string, args ...string) ([]byte, error) {
	m.commands = append(m.commands, name+" "+strings.Join(args, " "))
	return nil, nil
}

func (m *mockRunner) RunAttached(name string, args ...string) error {
	m.commands = append(m.commands, name+" "+strings.Join(args, " "))
	return nil
}

func (m *mockRunner) RunBackground(name string, args ...string) error {
	m.commands = append(m.commands, name+" "+strings.Join(args, " "))
	return nil
}

func (m *mockRunner) LookPath(name string) (string, error) {
	if m.available[name] {
		return "/usr/bin/" + name, nil
	}
	return "", fmt.Errorf("not found: %s", name)
}

func TestDetectPrefersPodman(t *testing.T) {
	r := &mockRunner{available: map[string]bool{
		"podman": true, "skopeo": true, "umoci": true, "docker": true,
	}}
	p, err := Detect(r)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if p.Name() != "podman" {
		t.Errorf("expected podman, got %s", p.Name())
	}
}

func TestDetectFallsBackToSkopeo(t *testing.T) {
	r := &mockRunner{available: map[string]bool{
		"skopeo": true, "umoci": true, "docker": true,
	}}
	p, err := Detect(r)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if p.Name() != "skopeo+umoci" {
		t.Errorf("expected skopeo+umoci, got %s", p.Name())
	}
}

func TestDetectSkipsSkopeoWithoutUmoci(t *testing.T) {
	r := &mockRunner{available: map[string]bool{
		"skopeo": true, "docker": true,
	}}
	p, err := Detect(r)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if p.Name() != "docker" {
		t.Errorf("expected docker, got %s", p.Name())
	}
}

func TestDetectFallsBackToDocker(t *testing.T) {
	r := &mockRunner{available: map[string]bool{
		"docker": true,
	}}
	p, err := Detect(r)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if p.Name() != "docker" {
		t.Errorf("expected docker, got %s", p.Name())
	}
}

func TestDetectErrorsWhenNoneAvailable(t *testing.T) {
	r := &mockRunner{available: map[string]bool{}}
	_, err := Detect(r)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "no container tool found") {
		t.Errorf("unexpected error message: %v", err)
	}
}
```

**Step 2: Run tests to verify they fail**

Run: `go test ./internal/puller/ -v`
Expected: FAIL — package doesn't exist yet.

**Step 3: Write the interface and Detect function**

Create `internal/puller/puller.go`:

```go
package puller

import (
	"fmt"

	"github.com/frostyard/intuneme/internal/runner"
)

// Puller pulls a container image from a registry and extracts it to a rootfs directory.
type Puller interface {
	// Name returns a human-readable name for the backend (e.g. "podman").
	Name() string
	// PullAndExtract pulls image from a registry and extracts it to rootfsPath.
	PullAndExtract(r runner.Runner, image string, rootfsPath string) error
}

// Detect returns the first available Puller in preference order:
// podman, skopeo+umoci, docker. Returns an error if none are available.
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

This won't compile yet — the three puller types don't exist. Add stubs so Detect compiles:

```go
// PodmanPuller pulls and extracts using podman.
type PodmanPuller struct{}

func (p *PodmanPuller) Name() string { return "podman" }

func (p *PodmanPuller) PullAndExtract(r runner.Runner, image string, rootfsPath string) error {
	return fmt.Errorf("not implemented")
}

// SkopeoPuller pulls and extracts using skopeo + umoci.
type SkopeoPuller struct{}

func (p *SkopeoPuller) Name() string { return "skopeo+umoci" }

func (p *SkopeoPuller) PullAndExtract(r runner.Runner, image string, rootfsPath string) error {
	return fmt.Errorf("not implemented")
}

// DockerPuller pulls and extracts using docker.
type DockerPuller struct{}

func (p *DockerPuller) Name() string { return "docker" }

func (p *DockerPuller) PullAndExtract(r runner.Runner, image string, rootfsPath string) error {
	return fmt.Errorf("not implemented")
}
```

Put the stubs in the same `puller.go` file for now — they'll be fleshed out in subsequent tasks.

**Step 4: Run tests to verify they pass**

Run: `go test ./internal/puller/ -v`
Expected: All 5 Detect tests PASS.

**Step 5: Commit**

```
git add internal/puller/puller.go internal/puller/puller_test.go
git commit -m "feat: add puller package with Detect and interface stubs"
```

---

### Task 2: Implement PodmanPuller

**Files:**
- Modify: `internal/puller/puller.go` (replace PodmanPuller stub)
- Modify: `internal/puller/puller_test.go` (add PodmanPuller tests)

**Step 1: Write the failing test**

Add to `internal/puller/puller_test.go`:

```go
func TestPodmanPullAndExtract(t *testing.T) {
	r := &mockRunner{available: map[string]bool{"podman": true}}
	p := &PodmanPuller{}
	rootfs := t.TempDir()

	err := p.PullAndExtract(r, "ghcr.io/frostyard/ubuntu-intune:latest", rootfs)
	if err != nil {
		t.Fatalf("PullAndExtract error: %v", err)
	}

	// Expected commands:
	// 1. podman rm intuneme-extract (cleanup)
	// 2. podman pull <image>
	// 3. podman create --name intuneme-extract <image>
	// 4. podman export -o <tmp> intuneme-extract
	// 5. sudo tar -xf <tmp> -C <rootfs>
	// 6. podman rm intuneme-extract
	if len(r.commands) != 6 {
		t.Fatalf("expected 6 commands, got %d: %v", len(r.commands), r.commands)
	}
	if !strings.Contains(r.commands[1], "podman pull") {
		t.Errorf("cmd[1]: expected podman pull, got: %s", r.commands[1])
	}
	if !strings.Contains(r.commands[2], "podman create") {
		t.Errorf("cmd[2]: expected podman create, got: %s", r.commands[2])
	}
	if !strings.Contains(r.commands[3], "podman export") {
		t.Errorf("cmd[3]: expected podman export, got: %s", r.commands[3])
	}
	if !strings.Contains(r.commands[4], "sudo tar") {
		t.Errorf("cmd[4]: expected sudo tar, got: %s", r.commands[4])
	}
	if !strings.Contains(r.commands[5], "podman rm") {
		t.Errorf("cmd[5]: expected podman rm, got: %s", r.commands[5])
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/puller/ -run TestPodmanPullAndExtract -v`
Expected: FAIL — PullAndExtract returns "not implemented".

**Step 3: Implement PodmanPuller.PullAndExtract**

Replace the stub in `internal/puller/puller.go`. The logic comes directly from the existing `provision.PullImage` + `provision.ExtractRootfs`:

```go
func (p *PodmanPuller) PullAndExtract(r runner.Runner, image string, rootfsPath string) error {
	// Clean up any leftover extract container from a previous failed run
	_, _ = r.Run("podman", "rm", "intuneme-extract")

	// Pull the image
	out, err := r.Run("podman", "pull", image)
	if err != nil {
		return fmt.Errorf("podman pull failed: %w\n%s", err, out)
	}

	// Create a temporary container to export
	out, err = r.Run("podman", "create", "--name", "intuneme-extract", image)
	if err != nil {
		return fmt.Errorf("podman create failed: %w\n%s", err, out)
	}

	// Export to tar, then extract with sudo to preserve container-internal UIDs
	tmpTar := filepath.Join(os.TempDir(), "intuneme-rootfs.tar")
	out, err = r.Run("podman", "export", "-o", tmpTar, "intuneme-extract")
	if err != nil {
		_, _ = r.Run("podman", "rm", "intuneme-extract")
		return fmt.Errorf("podman export failed: %w\n%s", err, out)
	}
	defer func() { _ = os.Remove(tmpTar) }()

	// RunAttached so sudo can prompt for password
	if err := r.RunAttached("sudo", "tar", "-xf", tmpTar, "-C", rootfsPath); err != nil {
		_, _ = r.Run("podman", "rm", "intuneme-extract")
		return fmt.Errorf("extract rootfs failed: %w", err)
	}

	// Remove temporary container
	out, err = r.Run("podman", "rm", "intuneme-extract")
	if err != nil {
		return fmt.Errorf("podman rm failed: %w\n%s", err, out)
	}
	return nil
}
```

Add `"os"` and `"path/filepath"` to the import block.

**Step 4: Run test to verify it passes**

Run: `go test ./internal/puller/ -run TestPodmanPullAndExtract -v`
Expected: PASS.

**Step 5: Commit**

```
git add internal/puller/puller.go internal/puller/puller_test.go
git commit -m "feat: implement PodmanPuller"
```

---

### Task 3: Implement SkopeoPuller

**Files:**
- Modify: `internal/puller/puller.go` (replace SkopeoPuller stub)
- Modify: `internal/puller/puller_test.go` (add SkopeoPuller tests)

**Step 1: Write the failing test**

Add to `internal/puller/puller_test.go`:

```go
func TestSkopeoPullAndExtract(t *testing.T) {
	r := &mockRunner{available: map[string]bool{"skopeo": true, "umoci": true}}
	p := &SkopeoPuller{}
	rootfs := t.TempDir()

	err := p.PullAndExtract(r, "ghcr.io/frostyard/ubuntu-intune:latest", rootfs)
	if err != nil {
		t.Fatalf("PullAndExtract error: %v", err)
	}

	// Expected commands:
	// 1. skopeo copy docker://<image> oci:<tmpDir>:latest
	// 2. sudo umoci raw unpack --image <tmpDir>:latest <rootfs>
	if len(r.commands) != 2 {
		t.Fatalf("expected 2 commands, got %d: %v", len(r.commands), r.commands)
	}
	if !strings.Contains(r.commands[0], "skopeo copy docker://ghcr.io/frostyard/ubuntu-intune:latest oci:") {
		t.Errorf("cmd[0]: expected skopeo copy, got: %s", r.commands[0])
	}
	if !strings.Contains(r.commands[1], "sudo umoci raw unpack --image") {
		t.Errorf("cmd[1]: expected sudo umoci raw unpack, got: %s", r.commands[1])
	}
	if !strings.Contains(r.commands[1], rootfs) {
		t.Errorf("cmd[1]: expected rootfs path %s, got: %s", rootfs, r.commands[1])
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/puller/ -run TestSkopeoPullAndExtract -v`
Expected: FAIL — PullAndExtract returns "not implemented".

**Step 3: Implement SkopeoPuller.PullAndExtract**

Replace the stub in `internal/puller/puller.go`:

```go
func (p *SkopeoPuller) PullAndExtract(r runner.Runner, image string, rootfsPath string) error {
	// Create a temp directory for the OCI layout
	tmpDir, err := os.MkdirTemp("", "intuneme-oci-*")
	if err != nil {
		return fmt.Errorf("create temp dir: %w", err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	ociDest := tmpDir + ":latest"

	// Pull image to OCI layout
	out, err := r.Run("skopeo", "copy", "docker://"+image, "oci:"+ociDest)
	if err != nil {
		return fmt.Errorf("skopeo copy failed: %w\n%s", err, out)
	}

	// Unpack OCI layout to rootfs with sudo to preserve UIDs
	if err := r.RunAttached("sudo", "umoci", "raw", "unpack", "--image", ociDest, rootfsPath); err != nil {
		return fmt.Errorf("umoci unpack failed: %w", err)
	}

	return nil
}
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/puller/ -run TestSkopeoPullAndExtract -v`
Expected: PASS.

**Step 5: Commit**

```
git add internal/puller/puller.go internal/puller/puller_test.go
git commit -m "feat: implement SkopeoPuller"
```

---

### Task 4: Implement DockerPuller

**Files:**
- Modify: `internal/puller/puller.go` (replace DockerPuller stub)
- Modify: `internal/puller/puller_test.go` (add DockerPuller tests)

**Step 1: Write the failing test**

Add to `internal/puller/puller_test.go`:

```go
func TestDockerPullAndExtract(t *testing.T) {
	r := &mockRunner{available: map[string]bool{"docker": true}}
	p := &DockerPuller{}
	rootfs := t.TempDir()

	err := p.PullAndExtract(r, "ghcr.io/frostyard/ubuntu-intune:latest", rootfs)
	if err != nil {
		t.Fatalf("PullAndExtract error: %v", err)
	}

	// Expected commands:
	// 1. docker rm intuneme-extract (cleanup)
	// 2. docker pull <image>
	// 3. docker create --name intuneme-extract <image>
	// 4. docker export -o <tmp> intuneme-extract
	// 5. sudo tar -xf <tmp> -C <rootfs>
	// 6. docker rm intuneme-extract
	if len(r.commands) != 6 {
		t.Fatalf("expected 6 commands, got %d: %v", len(r.commands), r.commands)
	}
	if !strings.Contains(r.commands[1], "docker pull") {
		t.Errorf("cmd[1]: expected docker pull, got: %s", r.commands[1])
	}
	if !strings.Contains(r.commands[2], "docker create") {
		t.Errorf("cmd[2]: expected docker create, got: %s", r.commands[2])
	}
	if !strings.Contains(r.commands[3], "docker export") {
		t.Errorf("cmd[3]: expected docker export, got: %s", r.commands[3])
	}
	if !strings.Contains(r.commands[4], "sudo tar") {
		t.Errorf("cmd[4]: expected sudo tar, got: %s", r.commands[4])
	}
	if !strings.Contains(r.commands[5], "docker rm") {
		t.Errorf("cmd[5]: expected docker rm, got: %s", r.commands[5])
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/puller/ -run TestDockerPullAndExtract -v`
Expected: FAIL — PullAndExtract returns "not implemented".

**Step 3: Implement DockerPuller.PullAndExtract**

Replace the stub in `internal/puller/puller.go`:

```go
func (p *DockerPuller) PullAndExtract(r runner.Runner, image string, rootfsPath string) error {
	// Clean up any leftover extract container from a previous failed run
	_, _ = r.Run("docker", "rm", "intuneme-extract")

	// Pull the image
	out, err := r.Run("docker", "pull", image)
	if err != nil {
		return fmt.Errorf("docker pull failed: %w\n%s", err, out)
	}

	// Create a temporary container to export
	out, err = r.Run("docker", "create", "--name", "intuneme-extract", image)
	if err != nil {
		return fmt.Errorf("docker create failed: %w\n%s", err, out)
	}

	// Export to tar, then extract with sudo to preserve container-internal UIDs
	tmpTar := filepath.Join(os.TempDir(), "intuneme-rootfs.tar")
	out, err = r.Run("docker", "export", "-o", tmpTar, "intuneme-extract")
	if err != nil {
		_, _ = r.Run("docker", "rm", "intuneme-extract")
		return fmt.Errorf("docker export failed: %w\n%s", err, out)
	}
	defer func() { _ = os.Remove(tmpTar) }()

	// RunAttached so sudo can prompt for password
	if err := r.RunAttached("sudo", "tar", "-xf", tmpTar, "-C", rootfsPath); err != nil {
		_, _ = r.Run("docker", "rm", "intuneme-extract")
		return fmt.Errorf("extract rootfs failed: %w", err)
	}

	// Remove temporary container
	out, err = r.Run("docker", "rm", "intuneme-extract")
	if err != nil {
		return fmt.Errorf("docker rm failed: %w\n%s", err, out)
	}
	return nil
}
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/puller/ -run TestDockerPullAndExtract -v`
Expected: PASS.

**Step 5: Commit**

```
git add internal/puller/puller.go internal/puller/puller_test.go
git commit -m "feat: implement DockerPuller"
```

---

### Task 5: Remove podman from prereq and delete old provision pull/extract functions

**Files:**
- Modify: `internal/prereq/prereq.go` — remove podman requirement
- Modify: `internal/prereq/prereq_test.go` — update tests
- Modify: `internal/provision/provision.go` — delete `PullImage` and `ExtractRootfs`
- Modify: `internal/provision/provision_test.go` — delete `TestPullImage` and `TestExtractRootfs`

**Step 1: Update prereq**

In `internal/prereq/prereq.go`, change the `requirements` slice to remove the podman entry:

```go
var requirements = []requirement{
	{"systemd-nspawn", "systemd-container"},
	{"machinectl", "systemd-container"},
}
```

**Step 2: Update prereq tests**

In `internal/prereq/prereq_test.go`:

- `TestCheckAllPresent`: Remove `"podman": true` from the mock. Keep `systemd-nspawn` and `machinectl`.
- `TestCheckMissing`: The mock only has `machinectl: true`. Now only `systemd-nspawn` is missing, so expect 1 error (not 2). Remove the check for the podman error.

Updated tests:

```go
func TestCheckAllPresent(t *testing.T) {
	r := &mockRunner{available: map[string]bool{
		"systemd-nspawn": true, "machinectl": true,
	}}
	errs := Check(r)
	if len(errs) != 0 {
		t.Errorf("expected no errors, got %v", errs)
	}
}

func TestCheckMissing(t *testing.T) {
	r := &mockRunner{available: map[string]bool{
		"machinectl": true,
	}}
	errs := Check(r)
	if len(errs) != 1 {
		t.Fatalf("expected 1 error, got %d: %v", len(errs), errs)
	}
	if !strings.Contains(errs[0].Error(), "systemd-nspawn") {
		t.Errorf("expected systemd-nspawn error, got: %v", errs[0])
	}
}
```

**Step 3: Delete old provision functions**

In `internal/provision/provision.go`, delete:
- `PullImage` function (lines 16-22)
- `ExtractRootfs` function (lines 24-66)

In `internal/provision/provision_test.go`, delete:
- `TestPullImage` function
- `TestExtractRootfs` function

**Step 4: Run all tests**

Run: `go test ./internal/prereq/ ./internal/provision/ ./internal/puller/ -v`
Expected: All PASS.

**Step 5: Run lint**

Run: `make fmt && make lint`
Expected: Clean.

**Step 6: Commit**

```
git add internal/prereq/prereq.go internal/prereq/prereq_test.go internal/provision/provision.go internal/provision/provision_test.go
git commit -m "refactor: remove podman from prereqs and delete old pull/extract functions"
```

---

### Task 6: Wire puller into cmd/init.go

**Files:**
- Modify: `cmd/init.go`

**Step 1: Update imports**

In `cmd/init.go`, add the puller import and remove the provision import if it's no longer needed (but it IS still needed for `CreateContainerUser`, `SetContainerPassword`, `WriteFixups`, `InstallPolkitRule`):

```go
import (
	"fmt"
	"os"
	"os/user"
	"path/filepath"

	"github.com/frostyard/intuneme/internal/config"
	"github.com/frostyard/intuneme/internal/prereq"
	"github.com/frostyard/intuneme/internal/provision"
	"github.com/frostyard/intuneme/internal/puller"
	"github.com/frostyard/intuneme/internal/runner"
	pkgversion "github.com/frostyard/intuneme/internal/version"
	"github.com/spf13/cobra"
)
```

**Step 2: Replace the pull+extract block**

In `cmd/init.go`, replace lines 50-62 (the image pull and extract block):

```go
		// Old code:
		image := pkgversion.ImageRef()
		fmt.Printf("Pulling OCI image %s...\n", image)
		if err := provision.PullImage(r, image); err != nil {
			return err
		}

		fmt.Println("Extracting rootfs...")
		if err := os.MkdirAll(root, 0755); err != nil {
			return fmt.Errorf("create root dir: %w", err)
		}
		if err := provision.ExtractRootfs(r, image, cfg.RootfsPath); err != nil {
			return err
		}
```

With:

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

Note: also remove the separate `os.MkdirAll(root, 0755)` call — the rootfs dir is created above, and the root dir is already created by `config.Load`.

**Step 3: Build to verify compilation**

Run: `go build ./...`
Expected: Compiles with no errors.

**Step 4: Run all tests**

Run: `go test ./... -v`
Expected: All PASS.

**Step 5: Run lint**

Run: `make fmt && make lint`
Expected: Clean.

**Step 6: Commit**

```
git add cmd/init.go
git commit -m "feat: wire puller.Detect into init command"
```

---
