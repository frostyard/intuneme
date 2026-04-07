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
	outputs     map[string]string
	errors      map[string]error
}

func (m *mockRunner) Run(name string, args ...string) ([]byte, error) {
	cmd := name + " " + strings.Join(args, " ")
	m.commands = append(m.commands, cmd)
	// Capture the temp file content before WriteDisplayMarker deletes it.
	// sudo install <flags> <src> <dst> — src is the second-to-last arg.
	if name == "sudo" && len(args) >= 4 && args[0] == "install" {
		src := args[len(args)-2]
		if data, err := os.ReadFile(src); err == nil {
			m.fileContent = string(data)
		}
	}
	for prefix, err := range m.errors {
		if strings.HasPrefix(cmd, prefix) {
			return nil, err
		}
	}
	for prefix, out := range m.outputs {
		if strings.HasPrefix(cmd, prefix) {
			return []byte(out), nil
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
		{Host: "/run/user/1000/wayland-0", Container: "/run/host-wayland"},
	}
	args := BuildBootArgs("/tmp/rootfs", "intuneme", "/home/testuser/Intune", "/home/testuser", sockets, nil)

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
	args := BuildBootArgs("/tmp/rootfs", "intuneme", "/home/testuser/Intune", "/home/testuser", nil, nil)

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

func TestLeaderPID(t *testing.T) {
	r := &mockRunner{
		outputs: map[string]string{
			"machinectl show intuneme -p Leader --value": "12345\n",
		},
	}

	pid, err := LeaderPID(r, "intuneme")
	if err != nil {
		t.Fatalf("LeaderPID failed: %v", err)
	}
	if pid != "12345" {
		t.Fatalf("LeaderPID() = %q, want %q", pid, "12345")
	}
}

func TestMachineUnit(t *testing.T) {
	r := &mockRunner{
		outputs: map[string]string{
			"machinectl show intuneme -p Unit --value": "intuneme.scope\n",
		},
	}

	unit, err := MachineUnit(r, "intuneme")
	if err != nil {
		t.Fatalf("MachineUnit failed: %v", err)
	}
	if unit != "intuneme.scope" {
		t.Fatalf("MachineUnit() = %q, want %q", unit, "intuneme.scope")
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
		{Host: "/run/user/1000/pulse/native", Container: "/run/host-pulse"},
	}
	args := BuildBootArgs("/tmp/rootfs", "intuneme", "/home/testuser/Intune", "/home/testuser", sockets, nil)

	joined := strings.Join(args, " ")
	if !strings.Contains(joined, "--bind=/run/user/1000/pulse/native:/run/host-pulse") {
		t.Errorf("missing pulse socket bind in: %s", joined)
	}
}

func TestBuildBootArgs_NvidiaDevices(t *testing.T) {
	nvidiaDevs := []BindMount{
		{Host: "/dev/nvidia0", Container: "/dev/nvidia0"},
		{Host: "/dev/nvidiactl", Container: "/dev/nvidiactl"},
	}
	args := BuildBootArgs("/tmp/rootfs", "intuneme", "/home/testuser/Intune", "/home/testuser", nil, nvidiaDevs)

	joined := strings.Join(args, " ")
	// Verify device binds.
	if !strings.Contains(joined, "--bind=/dev/nvidia0") {
		t.Errorf("missing nvidia0 bind in: %s", joined)
	}
	if !strings.Contains(joined, "--bind=/dev/nvidiactl") {
		t.Errorf("missing nvidiactl bind in: %s", joined)
	}
	// Verify DeviceAllow properties.
	if !strings.Contains(joined, "--property=DeviceAllow=/dev/nvidia0 rwm") {
		t.Errorf("missing DeviceAllow for nvidia0 in: %s", joined)
	}
	if !strings.Contains(joined, "--property=DeviceAllow=/dev/nvidiactl rwm") {
		t.Errorf("missing DeviceAllow for nvidiactl in: %s", joined)
	}
}

func TestBuildBootArgs_ReadOnlyBinds(t *testing.T) {
	sockets := []BindMount{
		{Host: "/usr/lib/x86_64-linux-gnu", Container: "/run/host-nvidia/0", ReadOnly: true},
		{Host: "/usr/share/vulkan/icd.d/nvidia_icd.json", Container: "/usr/share/vulkan/icd.d/nvidia_icd.json", ReadOnly: true},
		{Host: "/run/user/1000/wayland-0", Container: "/run/host-wayland"},
	}
	args := BuildBootArgs("/tmp/rootfs", "intuneme", "/home/testuser/Intune", "/home/testuser", sockets, nil)

	joined := strings.Join(args, " ")
	if !strings.Contains(joined, "--bind-ro=/usr/lib/x86_64-linux-gnu:/run/host-nvidia/0") {
		t.Errorf("missing read-only bind for nvidia lib dir in: %s", joined)
	}
	if !strings.Contains(joined, "--bind-ro=/usr/share/vulkan/icd.d/nvidia_icd.json:/usr/share/vulkan/icd.d/nvidia_icd.json") {
		t.Errorf("missing read-only bind for ICD file in: %s", joined)
	}
	if !strings.Contains(joined, "--bind=/run/user/1000/wayland-0:/run/host-wayland") {
		t.Errorf("missing writable bind for wayland socket in: %s", joined)
	}
}

func TestBuildBootArgs_NoNvidiaDevices(t *testing.T) {
	args := BuildBootArgs("/tmp/rootfs", "intuneme", "/home/testuser/Intune", "/home/testuser", nil, nil)

	joined := strings.Join(args, " ")
	if strings.Contains(joined, "DeviceAllow") {
		t.Errorf("unexpected DeviceAllow in args with no Nvidia devices: %s", joined)
	}
	if strings.Contains(joined, "nvidia") {
		t.Errorf("unexpected nvidia reference in args with no Nvidia devices: %s", joined)
	}
}
