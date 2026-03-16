package nspawn

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type mockRunner struct {
	commands    []string
	fileContent string // captured from the source file in the last "install" command
}

func (m *mockRunner) Run(name string, args ...string) ([]byte, error) {
	m.commands = append(m.commands, name+" "+strings.Join(args, " "))
	// Capture the temp file content before WriteDisplayMarker deletes it.
	// sudo install <flags> <src> <dst> — src is the second-to-last arg.
	if name == "sudo" && len(args) >= 4 && args[0] == "install" {
		src := args[len(args)-2]
		if data, err := os.ReadFile(src); err == nil {
			m.fileContent = string(data)
		}
	}
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

func TestBuildBootArgs(t *testing.T) {
	sockets := []BindMount{
		{"/run/user/1000/wayland-0", "/run/host-wayland"},
	}
	args := BuildBootArgs("/tmp/rootfs", "intuneme", "/home/testuser/Intune", "/home/testuser", sockets)

	joined := strings.Join(args, " ")
	if !strings.Contains(joined, "--machine=intuneme") {
		t.Errorf("missing --machine flag in: %s", joined)
	}
	if !strings.Contains(joined, "--bind=/home/testuser/Intune:/home/testuser") {
		t.Errorf("missing home bind in: %s", joined)
	}
	if !strings.Contains(joined, "--bind=/tmp/.X11-unix") {
		t.Errorf("missing X11 bind in: %s", joined)
	}
	// DRI devices are bound individually (not as a directory) so that nspawn
	// adds them to the cgroup allow list. The exact devices depend on the
	// host, so just verify at least one DRI bind is present if the host has DRI.
	if _, err := os.Stat("/dev/dri/renderD128"); err == nil {
		if !strings.Contains(joined, "--bind=/dev/dri/renderD128") {
			t.Errorf("missing DRI renderD128 bind in: %s", joined)
		}
	}
	if !strings.Contains(joined, "--bind=/run/user/1000/wayland-0:/run/host-wayland") {
		t.Errorf("missing wayland socket bind in: %s", joined)
	}
	if !strings.Contains(joined, "-b") {
		t.Errorf("missing -b (boot) flag in: %s", joined)
	}
}

func TestBuildBootArgsNoSockets(t *testing.T) {
	args := BuildBootArgs("/tmp/rootfs", "intuneme", "/home/testuser/Intune", "/home/testuser", nil)

	joined := strings.Join(args, " ")
	if !strings.Contains(joined, "--machine=intuneme") {
		t.Errorf("missing --machine flag in: %s", joined)
	}
	if strings.Contains(joined, "host-wayland") {
		t.Errorf("unexpected wayland bind in: %s", joined)
	}
	if strings.Contains(joined, "host-pipewire") {
		t.Errorf("unexpected pipewire bind in: %s", joined)
	}
	if strings.Contains(joined, "host-pulse") {
		t.Errorf("unexpected pulse bind in: %s", joined)
	}
}

func TestBuildShellArgs(t *testing.T) {
	args := BuildShellArgs("intuneme", "testuser")

	joined := strings.Join(args, " ")
	if !strings.Contains(joined, "shell") {
		t.Errorf("missing shell subcommand in: %s", joined)
	}
	if !strings.Contains(joined, "testuser@intuneme") {
		t.Errorf("missing user@machine in: %s", joined)
	}
	if !strings.Contains(joined, "/bin/bash --login") {
		t.Errorf("missing login shell in: %s", joined)
	}
}

func TestDetectVideoDevices_ReturnsBindMounts(t *testing.T) {
	// DetectVideoDevices depends on real /dev nodes, so we test the
	// return type and that it doesn't error on this machine.
	// It may return empty if no cameras are present.
	devices := DetectVideoDevices()
	for _, d := range devices {
		if d.Mount.Host != d.Mount.Container {
			t.Errorf("video device mount should map to same path: host=%s container=%s", d.Mount.Host, d.Mount.Container)
		}
		if d.Mount.Host == "" {
			t.Error("empty host path in video device mount")
		}
	}
}

func TestHostDisplay_UsesEnv(t *testing.T) {
	// Test: unset DISPLAY falls back to :0
	t.Setenv("DISPLAY", "")
	if got := HostDisplay(); got != ":0" {
		t.Errorf("HostDisplay() with empty DISPLAY = %q, want %q", got, ":0")
	}
}

func TestHostDisplay_FallbackWhenSocketMissing(t *testing.T) {
	// Set DISPLAY to a value whose socket doesn't exist
	t.Setenv("DISPLAY", ":99")
	if got := HostDisplay(); got != ":0" {
		t.Errorf("HostDisplay() with missing socket = %q, want %q", got, ":0")
	}
}

func TestWriteDisplayMarker(t *testing.T) {
	tmpDir := t.TempDir()

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

	// Verify the temp file contained the correct marker content
	wantContent := "DISPLAY=:1\n"
	if r.fileContent != wantContent {
		t.Errorf("marker content = %q, want %q", r.fileContent, wantContent)
	}
}

func TestWriteDisplayMarker_InvalidDisplay(t *testing.T) {
	r := &mockRunner{}
	tests := []string{
		"; rm -rf /",
		"$(whoami)",
		":1\nMALICIOUS=true",
		"`id`",
		"",
	}
	for _, display := range tests {
		if err := WriteDisplayMarker(r, t.TempDir(), display); err == nil {
			t.Errorf("expected error for display %q, got nil", display)
		}
	}
}

func TestDetectHostSockets_PulseAudio(t *testing.T) {
	sockets := []BindMount{
		{"/run/user/1000/pulse/native", "/run/host-pulse"},
	}
	args := BuildBootArgs("/tmp/rootfs", "intuneme", "/home/testuser/Intune", "/home/testuser", sockets)

	joined := strings.Join(args, " ")
	if !strings.Contains(joined, "--bind=/run/user/1000/pulse/native:/run/host-pulse") {
		t.Errorf("missing pulse socket bind in: %s", joined)
	}
}
