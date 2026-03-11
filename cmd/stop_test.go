package cmd

import (
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/frostyard/clix"
)

func TestMain(m *testing.M) {
	rep = clix.NewReporter()
	os.Exit(m.Run())
}

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
	// Effective timeout = 1ms * 5 = 5ms; verify it appears in the error
	if !strings.Contains(err.Error(), "5ms") {
		t.Errorf("expected computed timeout in error message, got: %v", err)
	}
}
