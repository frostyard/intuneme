package udev

import (
	"fmt"
	"strings"
	"testing"
)

type mockRunner struct {
	commands []string
	outputs  map[string]string // key: command prefix -> output
	errors   map[string]error  // key: command prefix -> error
}

func newMockRunner() *mockRunner {
	return &mockRunner{
		outputs: make(map[string]string),
		errors:  make(map[string]error),
	}
}

func (m *mockRunner) Run(name string, args ...string) ([]byte, error) {
	cmd := name + " " + strings.Join(args, " ")
	m.commands = append(m.commands, cmd)

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

func (m *mockRunner) hasCommand(prefix string) bool {
	for _, cmd := range m.commands {
		if strings.HasPrefix(cmd, prefix) {
			return true
		}
	}
	return false
}

func TestRulesContent(t *testing.T) {
	content := RulesContent()

	checks := []string{
		`ATTR{idVendor}=="1050"`,
		`ENV{ID_VENDOR_ID}=="1050"`,
		ScriptDir + "/" + ScriptName,
		`SUBSYSTEM=="usb"`,
		`SUBSYSTEM=="hidraw"`,
		`ACTION=="add"`,
		`ACTION=="remove"`,
	}
	for _, want := range checks {
		if !strings.Contains(content, want) {
			t.Errorf("rules content missing %q", want)
		}
	}
}

func TestScriptContent(t *testing.T) {
	content := ScriptContent("testmachine")

	if !strings.Contains(content, `MACHINE="testmachine"`) {
		t.Error("script content missing machine name")
	}
	if !strings.Contains(content, "#!/bin/bash") {
		t.Error("script content missing shebang")
	}
	if !strings.Contains(content, "nsenter") {
		t.Error("script content missing nsenter")
	}
	if !strings.Contains(content, "mknod") {
		t.Error("script content missing mknod")
	}
	if !strings.Contains(content, StateDir) {
		t.Errorf("script content missing state dir %s", StateDir)
	}
}

func TestScriptContentDifferentMachines(t *testing.T) {
	c1 := ScriptContent("machine-a")
	c2 := ScriptContent("machine-b")

	if !strings.Contains(c1, `MACHINE="machine-a"`) {
		t.Error("machine-a not templated")
	}
	if !strings.Contains(c2, `MACHINE="machine-b"`) {
		t.Error("machine-b not templated")
	}
	if c1 == c2 {
		t.Error("script content should differ for different machines")
	}
}

func TestInstall(t *testing.T) {
	r := newMockRunner()
	err := Install(r, "intuneme")
	if err != nil {
		t.Fatalf("Install failed: %v", err)
	}

	// Verify script dir creation.
	if !r.hasCommand("sudo mkdir -p " + ScriptDir) {
		t.Error("missing mkdir for script dir")
	}

	// Verify sudo install calls for script and rule.
	installCount := 0
	for _, cmd := range r.commands {
		if strings.HasPrefix(cmd, "sudo install") {
			installCount++
		}
	}
	if installCount != 2 {
		t.Errorf("expected 2 sudo install calls (script + rule), got %d", installCount)
	}

	// Verify udevadm reload.
	if !r.hasCommand("sudo udevadm control --reload-rules") {
		t.Error("missing udevadm reload")
	}
}

func TestInstallError(t *testing.T) {
	r := newMockRunner()
	r.errors["sudo mkdir"] = fmt.Errorf("permission denied")

	err := Install(r, "intuneme")
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "script dir") {
		t.Errorf("expected script dir error, got: %v", err)
	}
}

func TestRemoveGraceful(t *testing.T) {
	// Remove should succeed even when called multiple times or on a clean system.
	r := newMockRunner()
	err := Remove(r)
	if err != nil {
		t.Fatalf("Remove failed: %v", err)
	}

	// A second remove should also succeed (idempotent).
	r2 := newMockRunner()
	err = Remove(r2)
	if err != nil {
		t.Fatalf("second Remove failed: %v", err)
	}
}

func TestRulesPath(t *testing.T) {
	got := RulesPath()
	want := "/etc/udev/rules.d/70-intuneme-yubikey.rules"
	if got != want {
		t.Errorf("RulesPath() = %q, want %q", got, want)
	}
}

func TestScriptPath(t *testing.T) {
	got := ScriptPath()
	want := "/usr/local/lib/intuneme/usb-hotplug"
	if got != want {
		t.Errorf("ScriptPath() = %q, want %q", got, want)
	}
}

func TestForwardDevice(t *testing.T) {
	r := newMockRunner()
	r.outputs["machinectl show"] = "12345"
	r.outputs["stat -c"] = "0xbd 0x9"

	err := ForwardDevice(r, "intuneme", "/dev/bus/usb/003/009")
	if err != nil {
		t.Fatalf("ForwardDevice failed: %v", err)
	}

	// Should set cgroup for USB devices too (runtime DeviceAllow is additive).
	if !r.hasCommand("sudo systemctl set-property machine-intuneme.scope DevicePolicy=auto DeviceAllow=/dev/bus/usb/003/009 rwm") {
		t.Error("missing cgroup set-property for USB device")
	}

	// Should call mknod.
	if !r.hasCommand("sudo nsenter") {
		t.Error("missing nsenter call")
	}
}

func TestForwardDeviceHidraw(t *testing.T) {
	r := newMockRunner()
	r.outputs["machinectl show"] = "12345"
	r.outputs["stat -c"] = "0xa 0x3"

	err := ForwardDevice(r, "intuneme", "/dev/hidraw3")
	if err != nil {
		t.Fatalf("ForwardDevice failed: %v", err)
	}

	// Should set cgroup for hidraw device.
	if !r.hasCommand("sudo systemctl set-property machine-intuneme.scope DevicePolicy=auto DeviceAllow=/dev/hidraw3 rwm") {
		t.Error("missing cgroup set-property for hidraw")
	}
}

func TestForwardDeviceContainerNotRunning(t *testing.T) {
	r := newMockRunner()
	r.errors["machinectl show"] = fmt.Errorf("machine not found")

	err := ForwardDevice(r, "intuneme", "/dev/bus/usb/003/009")
	if err == nil {
		t.Fatal("expected error when container not running")
	}
}

func TestYubikeyDeviceDevices(t *testing.T) {
	dev := YubikeyDevice{
		USBDevice:     "/dev/bus/usb/003/009",
		HIDRawDevices: []string{"/dev/hidraw3", "/dev/hidraw4"},
	}
	got := dev.Devices()
	if len(got) != 3 {
		t.Fatalf("expected 3 devices, got %d", len(got))
	}
	if got[0] != "/dev/bus/usb/003/009" {
		t.Errorf("first device should be USB, got %s", got[0])
	}
}
