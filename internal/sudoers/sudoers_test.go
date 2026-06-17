package sudoers

import (
	"os"
	"strings"
	"testing"

	"github.com/frostyard/intuneme/internal/nspawn"
)

// mockRunner records commands and captures the content installed to each
// destination path so tests can assert on generated files.
type mockRunner struct {
	commands  []string
	installed map[string]string // dest path -> file content
}

func newMockRunner() *mockRunner {
	return &mockRunner{installed: map[string]string{}}
}

func (m *mockRunner) Run(name string, args ...string) ([]byte, error) {
	m.commands = append(m.commands, name+" "+strings.Join(args, " "))
	// Capture installed file content: `sudo install [flags] <src> <dst>`.
	// The `-d` form (directory create) has no src/dst pair to capture.
	if name == "sudo" && len(args) >= 4 && args[0] == "install" && !contains(args, "-d") {
		src := args[len(args)-2]
		dst := args[len(args)-1]
		if data, err := os.ReadFile(src); err == nil {
			m.installed[dst] = string(data)
		}
	}
	return nil, nil
}

func (m *mockRunner) RunAttached(name string, args ...string) error { return nil }
func (m *mockRunner) RunBackground(name string, args ...string) error {
	return nil
}
func (m *mockRunner) LookPath(name string) (string, error) { return "/usr/bin/" + name, nil }

func contains(s []string, v string) bool {
	for _, x := range s {
		if x == v {
			return true
		}
	}
	return false
}

func TestInstall_RuleHasNoWildcards(t *testing.T) {
	r := newMockRunner()
	if err := Install(r, "testuser"); err != nil {
		t.Fatalf("Install failed: %v", err)
	}

	rule, ok := r.installed[filePath]
	if !ok {
		t.Fatalf("sudoers rule was not installed; commands: %v", r.commands)
	}
	if strings.Contains(rule, "*") {
		t.Errorf("sudoers rule must not contain wildcards (sudo-rs rejects them):\n%s", rule)
	}
	if !strings.Contains(rule, "testuser ALL=(root) NOPASSWD: "+nspawn.NsenterHelperPath) {
		t.Errorf("sudoers rule does not reference the helper path:\n%s", rule)
	}
}

func TestInstall_InstallsHelper(t *testing.T) {
	r := newMockRunner()
	if err := Install(r, "testuser"); err != nil {
		t.Fatalf("Install failed: %v", err)
	}

	helper, ok := r.installed[nspawn.NsenterHelperPath]
	if !ok {
		t.Fatalf("helper script was not installed; commands: %v", r.commands)
	}
	if helper != nspawn.NsenterHelperScript("testuser") {
		t.Errorf("installed helper does not match NsenterHelperScript output:\n%s", helper)
	}

	// Helper must be installed root-owned and executable (0755), and the
	// directory created with the same ownership.
	var sawHelperInstall, sawDirInstall bool
	for _, c := range r.commands {
		if strings.Contains(c, nspawn.NsenterHelperPath) && strings.Contains(c, "install -m 0755 -o root -g root") {
			sawHelperInstall = true
		}
		if strings.Contains(c, "install -d -m 0755 -o root -g root "+nspawn.NsenterHelperDir) {
			sawDirInstall = true
		}
	}
	if !sawDirInstall {
		t.Errorf("expected helper directory to be created root-owned; commands: %v", r.commands)
	}
	if !sawHelperInstall {
		t.Errorf("expected helper to be installed 0755 root:root; commands: %v", r.commands)
	}
}

func TestInstall_ValidatesBeforeInstallingRule(t *testing.T) {
	r := newMockRunner()
	if err := Install(r, "testuser"); err != nil {
		t.Fatalf("Install failed: %v", err)
	}

	var visudoIdx, ruleInstallIdx = -1, -1
	for i, c := range r.commands {
		if strings.HasPrefix(c, "/usr/sbin/visudo -c -f") {
			visudoIdx = i
		}
		if strings.Contains(c, "install -m 0440") && strings.Contains(c, filePath) {
			ruleInstallIdx = i
		}
	}
	if visudoIdx == -1 {
		t.Fatalf("visudo syntax check was not run; commands: %v", r.commands)
	}
	if ruleInstallIdx == -1 {
		t.Fatalf("rule was not installed; commands: %v", r.commands)
	}
	if visudoIdx > ruleInstallIdx {
		t.Errorf("visudo check (%d) must run before installing the rule (%d)", visudoIdx, ruleInstallIdx)
	}
}

func TestRemove_RemovesRuleAndHelper(t *testing.T) {
	r := newMockRunner()
	Remove(r)

	if len(r.commands) != 1 {
		t.Fatalf("expected a single rm command, got: %v", r.commands)
	}
	cmd := r.commands[0]
	if !strings.Contains(cmd, filePath) {
		t.Errorf("Remove must delete the sudoers rule, got: %s", cmd)
	}
	if !strings.Contains(cmd, nspawn.NsenterHelperPath) {
		t.Errorf("Remove must delete the nsenter helper, got: %s", cmd)
	}
}
