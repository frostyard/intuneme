# Fix WriteDisplayMarker Permission Denied Implementation Plan

> **For agentic workers:** REQUIRED: Use superpowers:subagent-driven-development (if subagents available) or superpowers:executing-plans to implement this plan. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Fix `intuneme start` "permission denied" error when writing the display marker into the root-owned container rootfs (issue #60).

**Architecture:** Change `WriteDisplayMarker` to accept a `runner.Runner` and use a temp-file + `sudo install` pattern (matching the existing `provision.sudoWriteFile` approach). Update the call site in `cmd/start.go` and the test in `nspawn_test.go`.

**Tech Stack:** Go, stdlib `os`, `runner.Runner` interface

---

## Chunk 1: Implementation

### Task 1: Update WriteDisplayMarker to use sudo install

**Files:**
- Modify: `internal/nspawn/nspawn.go:77-83`
- Modify: `internal/nspawn/nspawn_test.go:101-119`

- [ ] **Step 1: Write the failing test**

Replace the existing `TestWriteDisplayMarker` in `internal/nspawn/nspawn_test.go` with a version that passes a `mockRunner` and verifies the `sudo install` command is called. Add the `mockRunner` type (same pattern used in `internal/puller/puller_test.go` and `internal/provision/provision_test.go`).

Add at the top of the test file (after the existing imports — note `"strings"` is already imported):

```go
type mockRunner struct {
	commands []string
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
	return "/usr/bin/" + name, nil
}
```

Replace `TestWriteDisplayMarker`:

```go
func TestWriteDisplayMarker(t *testing.T) {
	tmpDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(tmpDir, "etc"), 0755); err != nil {
		t.Fatalf("create etc dir: %v", err)
	}

	r := &mockRunner{}
	if err := WriteDisplayMarker(r, tmpDir, ":1"); err != nil {
		t.Fatalf("WriteDisplayMarker failed: %v", err)
	}

	// Verify sudo install was called targeting the correct path
	if len(r.commands) != 1 {
		t.Fatalf("expected 1 command, got %d: %v", len(r.commands), r.commands)
	}
	cmd := r.commands[0]
	wantSuffix := filepath.Join(tmpDir, "etc", "intuneme-host-display")
	if !strings.Contains(cmd, "sudo install -m 0644") {
		t.Errorf("expected sudo install command, got: %s", cmd)
	}
	if !strings.HasSuffix(cmd, wantSuffix) {
		t.Errorf("command should target %s, got: %s", wantSuffix, cmd)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/nspawn/ -run TestWriteDisplayMarker -v`
Expected: FAIL — `WriteDisplayMarker` still has the old 2-argument signature.

- [ ] **Step 3: Update WriteDisplayMarker implementation**

In `internal/nspawn/nspawn.go`, add `"github.com/frostyard/intuneme/internal/runner"` to the imports, then replace the `WriteDisplayMarker` function:

```go
// WriteDisplayMarker writes the host DISPLAY value into the container rootfs
// so that container scripts and services can read it.
// Uses sudo install because the rootfs /etc/ is owned by root.
func WriteDisplayMarker(r runner.Runner, rootfs, display string) error {
	tmp, err := os.CreateTemp("", "intuneme-display-*")
	if err != nil {
		return err
	}
	defer func() { _ = os.Remove(tmp.Name()) }()

	content := fmt.Sprintf("DISPLAY=%s\n", display)
	if _, err := tmp.Write([]byte(content)); err != nil {
		_ = tmp.Close()
		return err
	}
	_ = tmp.Close()

	path := filepath.Join(rootfs, displayMarkerPath)
	_, err = r.Run("sudo", "install", "-m", "0644", tmp.Name(), path)
	return err
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/nspawn/ -run TestWriteDisplayMarker -v`
Expected: PASS

- [ ] **Step 5: Run fmt and lint**

Run: `make fmt && make lint`
Expected: No errors.

- [ ] **Step 6: Commit**

```bash
git add internal/nspawn/nspawn.go internal/nspawn/nspawn_test.go
git commit -m "fix: use sudo install for display marker write (#60)

WriteDisplayMarker now uses a temp file + sudo install to write into
the root-owned rootfs /etc/, fixing permission denied on start."
```

### Task 2: Update call site in cmd/start.go

**Files:**
- Modify: `cmd/start.go:84`

- [ ] **Step 1: Update the call site**

In `cmd/start.go`, change line 84 from:

```go
if err := nspawn.WriteDisplayMarker(cfg.RootfsPath, display); err != nil {
```

to:

```go
if err := nspawn.WriteDisplayMarker(r, cfg.RootfsPath, display); err != nil {
```

- [ ] **Step 2: Verify build succeeds**

Run: `go build ./...`
Expected: Success, no errors.

- [ ] **Step 3: Run full test suite**

Run: `go test ./...`
Expected: All tests pass.

- [ ] **Step 4: Run fmt and lint**

Run: `make fmt && make lint`
Expected: No errors.

- [ ] **Step 5: Commit**

```bash
git add cmd/start.go
git commit -m "fix: pass runner to WriteDisplayMarker in start command"
```
