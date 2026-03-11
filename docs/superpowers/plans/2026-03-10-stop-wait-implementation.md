# Stop Wait Implementation Plan

> **For agentic workers:** REQUIRED: Use superpowers:subagent-driven-development (if subagents available) or superpowers:executing-plans to implement this plan. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make `intuneme stop` wait for the container to fully stop before returning, fixing the `stop && start` race condition (#59).

**Architecture:** Add a poll loop in `cmd/stop.go` that checks `nspawn.IsRunning()` every 500ms for up to 30 seconds after `nspawn.Stop()` returns. The stop command only prints "Container stopped." and returns once the container is fully deregistered. Extract the stop logic into a `runStop()` function that accepts a `runner.Runner` to enable testing with a mock.

**Tech Stack:** Go stdlib (`time`, `fmt`), existing `nspawn.IsRunning()` function, `runner.Runner` interface for testability.

---

## Chunk 1: Implementation

### Task 1: Add poll loop to stop command

**Files:**
- Modify: `cmd/stop.go:1-61`
- Create: `cmd/stop_test.go`

- [ ] **Step 1: Write the failing test**

Create `cmd/stop_test.go`. Define a local `stopMockRunner` (following the pattern from `internal/prereq/prereq_test.go`) that tracks `machinectl show` calls and returns error after a configurable count. `config.Load()` works with an empty temp dir (returns defaults), so no config file setup is needed.

```go
package cmd

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

type stopMockRunner struct {
	showCallCount int
	showFailAfter int // machinectl show returns error after this many calls
	poweroffErr   error
}

func (m *stopMockRunner) Run(name string, args ...string) ([]byte, error) {
	if name == "machinectl" && len(args) > 0 {
		switch args[0] {
		case "poweroff":
			return nil, m.poweroffErr
		case "show":
			m.showCallCount++
			if m.showCallCount > m.showFailAfter {
				return nil, fmt.Errorf("machine not found")
			}
			return []byte("Name=intuneme\n"), nil
		}
	}
	return nil, nil
}

func (m *stopMockRunner) RunAttached(string, ...string) error   { return nil }
func (m *stopMockRunner) RunBackground(string, ...string) error { return nil }
func (m *stopMockRunner) LookPath(name string) (string, error) {
	return "/usr/bin/" + name, nil
}

func TestRunStop_WaitsForShutdown(t *testing.T) {
	// Container is "running" for first 3 show calls, then stops
	r := &stopMockRunner{showFailAfter: 3}

	err := runStop(r, t.TempDir(), 1*time.Millisecond, 100)
	if err != nil {
		t.Fatalf("runStop returned error: %v", err)
	}
	// showCallCount: 1 for initial IsRunning check + polls until deregistered
	if r.showCallCount < 2 {
		t.Errorf("expected multiple show calls (poll loop), got %d", r.showCallCount)
	}
}

func TestRunStop_NotRunning(t *testing.T) {
	// Container is not running (show fails immediately)
	r := &stopMockRunner{showFailAfter: 0}

	err := runStop(r, t.TempDir(), 1*time.Millisecond, 100)
	if err != nil {
		t.Fatalf("runStop returned error: %v", err)
	}
}

func TestRunStop_PoweroffError(t *testing.T) {
	r := &stopMockRunner{
		showFailAfter: 100, // always running
		poweroffErr:   fmt.Errorf("permission denied"),
	}

	err := runStop(r, t.TempDir(), 1*time.Millisecond, 100)
	if err == nil {
		t.Fatal("expected error from poweroff failure")
	}
	if !strings.Contains(err.Error(), "poweroff") {
		t.Errorf("expected poweroff error, got: %v", err)
	}
}

func TestRunStop_Timeout(t *testing.T) {
	// Container never stops — show always succeeds
	r := &stopMockRunner{showFailAfter: 1000}

	err := runStop(r, t.TempDir(), 1*time.Millisecond, 5)
	if err == nil {
		t.Fatal("expected timeout error")
	}
	if !strings.Contains(err.Error(), "did not stop") {
		t.Errorf("expected timeout message, got: %v", err)
	}
}
```

File: `cmd/stop_test.go`

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./cmd/ -run TestRunStop -v`
Expected: compilation error — `runStop` is not defined yet.

- [ ] **Step 3: Extract `runStop` function and add poll loop**

Modify `cmd/stop.go` to:
1. Add `"fmt"` and `"time"` to imports
2. Extract the stop logic into `func runStop(r runner.Runner, root string, pollInterval time.Duration, maxAttempts int) error`
3. Add the poll loop after `nspawn.Stop()` using the configurable parameters
4. Have the cobra `RunE` delegate to `runStop` with production values (500ms, 60)

The full updated `cmd/stop.go`:

```go
package cmd

import (
	"fmt"
	"path/filepath"
	"time"

	"github.com/frostyard/clix"
	"github.com/frostyard/intuneme/internal/broker"
	"github.com/frostyard/intuneme/internal/config"
	"github.com/frostyard/intuneme/internal/nspawn"
	"github.com/frostyard/intuneme/internal/runner"
	"github.com/spf13/cobra"
)

func runStop(r runner.Runner, root string, pollInterval time.Duration, maxAttempts int) error {
	cfg, err := config.Load(root)
	if err != nil {
		return err
	}

	if !nspawn.IsRunning(r, cfg.MachineName) {
		rep.Message("Container is not running.")
		return nil
	}

	if clix.DryRun {
		if cfg.BrokerProxy {
			rep.Message("[dry-run] Would stop broker proxy")
		}
		rep.Message("[dry-run] Would stop container %s", cfg.MachineName)
		return nil
	}

	// Stop broker proxy first so host apps get clean errors
	if cfg.BrokerProxy {
		pidPath := filepath.Join(root, "broker-proxy.pid")
		broker.StopByPIDFile(pidPath)
		rep.Message("Broker proxy stopped.")
	}

	rep.Message("Stopping container...")
	if err := nspawn.Stop(r, cfg.MachineName); err != nil {
		return err
	}

	// Wait for the container to fully deregister from systemd-machined.
	// machinectl poweroff returns before the machine is fully gone.
	for range maxAttempts {
		if !nspawn.IsRunning(r, cfg.MachineName) {
			rep.Message("Container stopped.")
			return nil
		}
		time.Sleep(pollInterval)
	}

	return fmt.Errorf("container did not stop within 30 seconds")
}

var stopCmd = &cobra.Command{
	Use:   "stop",
	Short: "Stop the container",
	RunE: func(cmd *cobra.Command, args []string) error {
		r := &runner.SystemRunner{}
		root := rootDir
		if root == "" {
			root = config.DefaultRoot()
		}
		return runStop(r, root, 500*time.Millisecond, 60)
	},
}

func init() {
	rootCmd.AddCommand(stopCmd)
}
```

**Design note:** `runStop` accepts `pollInterval` and `maxAttempts` parameters so tests can use tiny values (1ms, 5 attempts) instead of waiting 30 seconds. The cobra handler passes the production values (500ms interval, 60 attempts = 30 seconds). This is asymmetric with `cmd/start.go` which has inline constants — a future cleanup could extract `runStart` similarly, but that is out of scope.

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./cmd/ -run TestRunStop -v`
Expected: All 4 tests PASS.

- [ ] **Step 5: Run full test suite**

Run: `go test ./...`
Expected: All tests pass.

- [ ] **Step 6: Run linter**

Run: `make fmt && make lint`
Expected: No errors.

- [ ] **Step 7: Commit**

```bash
git add cmd/stop.go cmd/stop_test.go
git commit -m "fix: Wait for container to fully stop before returning (#59)"
```
