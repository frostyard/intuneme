package provision

import (
	"path/filepath"
	"strings"
	"testing"
)

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

func TestWriteFixups(t *testing.T) {
	r := &mockRunner{}
	rootfs := "/tmp/test-rootfs"

	err := WriteFixups(r, rootfs, "testuser", 1000, 1000, "testhost")
	if err != nil {
		t.Fatalf("WriteFixups error: %v", err)
	}

	// Verify sudo commands were issued for key files
	allCmds := strings.Join(r.commands, "\n")

	for _, want := range []string{
		"etc/hostname",
		"etc/hosts",
		"fix-home-ownership.service",
		"intuneme.sh",
		"sudoers.d/intuneme",
	} {
		if !strings.Contains(allCmds, want) {
			t.Errorf("expected command referencing %q, not found in:\n%s", want, allCmds)
		}
	}

	// Verify symlinks were created
	for _, want := range []string{
		"fix-home-ownership.service",
	} {
		found := false
		for _, cmd := range r.commands {
			if strings.Contains(cmd, "ln -sf") && strings.Contains(cmd, want) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected symlink command for %q", want)
		}
	}
}

func TestWritePolkitRule(t *testing.T) {
	tmp := t.TempDir()
	rulesDir := filepath.Join(tmp, "etc", "polkit-1", "rules.d")

	r := &mockRunner{}
	err := InstallPolkitRule(r, rulesDir)
	if err != nil {
		t.Fatalf("InstallPolkitRule error: %v", err)
	}

	// Basic check — at least some sudo commands were issued
	if len(r.commands) == 0 {
		t.Errorf("expected sudo commands for polkit installation")
	}
}
