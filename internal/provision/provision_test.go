package provision

import (
	"os"
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
	err := ExtractRootfs(r, "ghcr.io/frostyard/ubuntu-intune:latest", "/tmp/test-rootfs")
	if err != nil {
		t.Fatalf("ExtractRootfs error: %v", err)
	}
	// Should run: podman rm (cleanup), podman create, podman cp, podman rm
	if len(r.commands) != 4 {
		t.Fatalf("expected 4 commands, got %d: %v", len(r.commands), r.commands)
	}
}

func TestWriteFixups(t *testing.T) {
	tmp := t.TempDir()
	rootfs := filepath.Join(tmp, "rootfs")
	os.MkdirAll(filepath.Join(rootfs, "etc"), 0755)
	os.MkdirAll(filepath.Join(rootfs, "etc", "systemd", "system"), 0755)
	os.MkdirAll(filepath.Join(rootfs, "etc", "pam.d"), 0755)

	err := WriteFixups(rootfs, "testuser", 1000, 1000, "testhost")
	if err != nil {
		t.Fatalf("WriteFixups error: %v", err)
	}

	// Check hostname
	data, _ := os.ReadFile(filepath.Join(rootfs, "etc", "hostname"))
	if strings.TrimSpace(string(data)) != "testhost" {
		t.Errorf("hostname = %q, want %q", strings.TrimSpace(string(data)), "testhost")
	}

	// Check environment file exists
	if _, err := os.Stat(filepath.Join(rootfs, "etc", "environment")); err != nil {
		t.Errorf("expected etc/environment to exist: %v", err)
	}

	// Check fix-home-ownership.service exists
	svcPath := filepath.Join(rootfs, "etc", "systemd", "system", "fix-home-ownership.service")
	if _, err := os.Stat(svcPath); err != nil {
		t.Errorf("expected fix-home-ownership.service to exist: %v", err)
	}

	// v2: Check profile.d/intuneme.sh exists
	profilePath := filepath.Join(rootfs, "etc", "profile.d", "intuneme.sh")
	if _, err := os.Stat(profilePath); err != nil {
		t.Errorf("expected profile.d/intuneme.sh to exist: %v", err)
	}

	// v2: Check broker display override exists
	brokerOverride := filepath.Join(rootfs, "usr", "lib", "systemd", "user",
		"microsoft-identity-broker.service.d", "display.conf")
	if _, err := os.Stat(brokerOverride); err != nil {
		t.Errorf("expected broker display override to exist: %v", err)
	}

	// v2: Keyring dir should NOT be pre-created (handled at runtime by profile script)
	keyringDir := filepath.Join(rootfs, "home", "testuser", ".local", "share", "keyrings")
	if _, err := os.Stat(keyringDir); err == nil {
		t.Errorf("keyring dir should not be pre-created in v2")
	}

	// v2: start-intune.sh should NOT exist
	if _, err := os.Stat(filepath.Join(rootfs, "opt", "intuneme", "start-intune.sh")); err == nil {
		t.Errorf("start-intune.sh should not exist in v2")
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
