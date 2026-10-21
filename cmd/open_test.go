package cmd

import (
	"strings"
	"testing"

	"github.com/frostyard/intuneme/internal/nspawn"
)

// lastExecScript returns the script passed to the nsenter helper by the most
// recent background launch.
func lastExecScript(t *testing.T, m *mcpMockRunner) string {
	t.Helper()
	for i := len(m.runCalls) - 1; i >= 0; i-- {
		c := m.runCalls[i]
		if len(c) > 1 && c[0] == "sudo" && c[1] == nspawn.NsenterHelperPath {
			return c[len(c)-1]
		}
	}
	t.Fatal("expected a background launch through the nsenter helper, got none")
	return ""
}

func TestRunOpen_NotInitialized(t *testing.T) {
	r := &mcpMockRunner{}
	err := runOpen(r, t.TempDir(), "microsoft-edge", nil)
	if err == nil || !strings.Contains(err.Error(), "not initialized") {
		t.Fatalf("expected 'not initialized' error, got %v", err)
	}
}

func TestRunOpen_NotRunning(t *testing.T) {
	r := &mcpMockRunner{notRunning: true}
	root, _ := initializedRoot(t, true)
	err := runOpen(r, root, "microsoft-edge", nil)
	if err == nil || !strings.Contains(err.Error(), "not running") {
		t.Fatalf("expected 'not running' error, got %v", err)
	}
}

func TestRunOpen_NoArgs(t *testing.T) {
	r := &mcpMockRunner{}
	root, _ := initializedRoot(t, true)
	if err := runOpen(r, root, "microsoft-edge", nil); err != nil {
		t.Fatalf("runOpen returned error: %v", err)
	}
	script := lastExecScript(t, r)
	if !strings.Contains(script, "\nnohup microsoft-edge >/dev/null 2>&1 &") {
		t.Errorf("expected a bare launch, got: %q", script)
	}
}

func TestRunOpen_QuotesArgs(t *testing.T) {
	r := &mcpMockRunner{}
	root, _ := initializedRoot(t, true)
	args := []string{"--app=https://outlook.office.com", "it's; rm -rf ~"}
	if err := runOpen(r, root, "microsoft-edge", args); err != nil {
		t.Fatalf("runOpen returned error: %v", err)
	}
	script := lastExecScript(t, r)
	want := `nohup microsoft-edge '--app=https://outlook.office.com' 'it'\''s; rm -rf ~' >/dev/null 2>&1 &`
	if !strings.Contains(script, want) {
		t.Errorf("expected quoted args %q in script, got: %q", want, script)
	}
}
