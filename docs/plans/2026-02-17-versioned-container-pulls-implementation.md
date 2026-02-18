# Versioned Container Pulls Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Pull a version-pinned container image for release builds and `latest` for dev/local builds.

**Architecture:** New `internal/version` package derives the OCI image reference from the CLI version string (injected via ldflags). `cmd/init.go` calls `version.ImageRef()` instead of reading a config field. The `Image` field is removed from config.

**Tech Stack:** Go stdlib only (`regexp`), no new dependencies.

---

### Task 1: Create `internal/version` package with tests

**Files:**
- Create: `internal/version/version.go`
- Create: `internal/version/version_test.go`

**Step 1: Write the failing tests**

Create `internal/version/version_test.go`:

```go
package version

import "testing"

func TestImageRef(t *testing.T) {
	const registry = "ghcr.io/frostyard/ubuntu-intune"

	tests := []struct {
		version string
		want    string
	}{
		{"dev", registry + ":latest"},
		{"0.4.0", registry + ":v0.4.0"},
		{"v0.4.0", registry + ":v0.4.0"},
		{"1.0.0", registry + ":v1.0.0"},
		{"v1.0.0", registry + ":v1.0.0"},
		{"v0.4.0-2-g98e23e6", registry + ":latest"},
		{"v0.4.0-dirty", registry + ":latest"},
		{"none", registry + ":latest"},
		{"", registry + ":latest"},
		{"v0.4.0-rc1", registry + ":latest"},
		{"0.4.0-beta.1", registry + ":latest"},
	}

	for _, tt := range tests {
		t.Run(tt.version, func(t *testing.T) {
			Version = tt.version
			got := ImageRef()
			if got != tt.want {
				t.Errorf("ImageRef() = %q, want %q", got, tt.want)
			}
		})
	}
}
```

**Step 2: Run tests to verify they fail**

Run: `go test ./internal/version/ -v`
Expected: FAIL — package does not exist yet.

**Step 3: Write minimal implementation**

Create `internal/version/version.go`:

```go
package version

import "regexp"

// Version is set from main.go at startup via ldflags.
var Version = "dev"

const imageBase = "ghcr.io/frostyard/ubuntu-intune"

var semverRe = regexp.MustCompile(`^v?(\d+\.\d+\.\d+)$`)

// ImageRef returns the full OCI image reference for the container.
// Release versions (clean semver) get a pinned tag; everything else gets latest.
func ImageRef() string {
	m := semverRe.FindStringSubmatch(Version)
	if m == nil {
		return imageBase + ":latest"
	}
	return imageBase + ":v" + m[1]
}
```

**Step 4: Run tests to verify they pass**

Run: `go test ./internal/version/ -v`
Expected: PASS — all table cases green.

**Step 5: Commit**

```bash
git add internal/version/version.go internal/version/version_test.go
git commit -m "feat: add internal/version package with ImageRef()"
```

---

### Task 2: Wire version from `main.go`

**Files:**
- Modify: `main.go:2-5` (imports), `main.go:22-26` (main func)

**Step 1: Add import and set version**

In `main.go`, add `"github.com/frostyard/intuneme/internal/version"` to imports.

In `main()`, before `fang.Execute`, add:

```go
version.Version = version
```

Note: The local `version` var (line 13) shadows the package name only inside `main()` — the import is used via the assignment. This is idiomatic Go for ldflags patterns.

**Step 2: Verify it compiles**

Run: `go build -o /dev/null .`
Expected: clean build, no errors.

**Step 3: Verify version injection works**

Run: `go run -ldflags "-X main.version=1.2.3" . version`
Expected: version output contains `1.2.3`.

**Step 4: Commit**

```bash
git add main.go
git commit -m "feat: wire CLI version into internal/version package"
```

---

### Task 3: Use `ImageRef()` in init command

**Files:**
- Modify: `cmd/init.go:9-13` (imports), `cmd/init.go:49-58` (image usage)

**Step 1: Update imports**

Add `"github.com/frostyard/intuneme/internal/version"` to imports in `cmd/init.go`.

Remove `"github.com/frostyard/intuneme/internal/config"` from imports only if it's no longer used after all changes — but it IS still used for `config.DefaultRoot()` and `config.Load()`, so keep it.

**Step 2: Replace image references**

Replace lines 49-58 with:

```go
image := version.ImageRef()
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

**Step 3: Verify it compiles**

Run: `go build -o /dev/null .`
Expected: clean build, no errors.

**Step 4: Commit**

```bash
git add cmd/init.go
git commit -m "feat: use version-derived image ref in init command"
```

---

### Task 4: Remove `Image` from config

**Files:**
- Modify: `internal/config/config.go:10-13` (struct), `internal/config/config.go:25-31` (Load defaults)

**Step 1: Check for other references to `cfg.Image`**

Run: `grep -rn '\.Image' internal/ cmd/`
Expected: Only the references we already replaced in Task 3 and the struct definition. If there are others, update them too.

**Step 2: Remove `Image` field from struct**

In `internal/config/config.go`, change the struct to:

```go
type Config struct {
	MachineName string `toml:"machine_name"`
	RootfsPath  string `toml:"rootfs_path"`
	HostUID     int    `toml:"host_uid"`
	HostUser    string `toml:"host_user"`
	BrokerProxy bool   `toml:"broker_proxy"`
}
```

Remove the `Image` default from `Load()`:

```go
cfg := &Config{
    MachineName: "intuneme",
    RootfsPath:  filepath.Join(root, "rootfs"),
    HostUID:     os.Getuid(),
    HostUser:    os.Getenv("USER"),
}
```

**Step 3: Verify everything compiles and tests pass**

Run: `go build -o /dev/null . && go test ./...`
Expected: clean build, all tests pass.

**Step 4: Run lint**

Run: `make fmt && make lint`
Expected: no issues.

**Step 5: Commit**

```bash
git add internal/config/config.go
git commit -m "refactor: remove Image field from config"
```
