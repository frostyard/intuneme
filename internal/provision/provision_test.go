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

func TestPullImage(t *testing.T) {
	r := &mockRunner{}
	err := PullImage(r, "ghcr.io/frostyard/ubuntu-intune:latest")
	if err != nil {
		t.Fatalf("PullImage error: %v", err)
	}
	if len(r.commands) != 1 {
		t.Fatalf("expected 1 command, got %d", len(r.commands))
	}
	if !strings.Contains(r.commands[0], "podman pull") {
		t.Errorf("expected podman pull, got: %s", r.commands[0])
	}
}

func TestExtractRootfs(t *testing.T) {
	r := &mockRunner{}
	rootfs := filepath.Join(t.TempDir(), "rootfs")
	err := ExtractRootfs(r, "ghcr.io/frostyard/ubuntu-intune:latest", rootfs)
	if err != nil {
		t.Fatalf("ExtractRootfs error: %v", err)
	}
	// Should run: podman rm (cleanup), podman create, podman export, sudo tar, podman rm
	if len(r.commands) != 5 {
		t.Fatalf("expected 5 commands, got %d: %v", len(r.commands), r.commands)
	}
	if !strings.Contains(r.commands[2], "podman export") {
		t.Errorf("expected podman export, got: %s", r.commands[2])
	}
	if !strings.Contains(r.commands[3], "sudo tar") {
		t.Errorf("expected sudo tar, got: %s", r.commands[3])
	}
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
		"etc/environment",
		"pam-configs/pwquality",
		"pwquality.conf",
		"fix-home-ownership.service",
		"microsoft-edge",
		"intuneme.sh",
		"sudoers.d/intuneme",
		"display.conf",
	} {
		if !strings.Contains(allCmds, want) {
			t.Errorf("expected command referencing %q, not found in:\n%s", want, allCmds)
		}
	}

	// Verify symlinks were created
	for _, want := range []string{
		"intune-agent.timer",
		"fix-home-ownership.service",
		"microsoft-identity-device-broker.service",
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
