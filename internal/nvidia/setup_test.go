package nvidia

import (
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

type mockRunner struct {
	commands []string
	outputs  map[string][]byte
}

func newMockRunner() *mockRunner {
	return &mockRunner{outputs: make(map[string][]byte)}
}

func (m *mockRunner) Run(name string, args ...string) ([]byte, error) {
	cmd := name + " " + strings.Join(args, " ")
	m.commands = append(m.commands, cmd)
	if out, ok := m.outputs[cmd]; ok {
		return out, nil
	}
	// For machinectl show, return a valid leader PID.
	if name == "machinectl" && len(args) >= 1 && args[0] == "show" {
		return []byte("42\n"), nil
	}
	// For "test -f ... -a ! -L ..." (regular file check), return error
	// to indicate the file doesn't exist (so symlink creation proceeds).
	if name == "sudo" {
		for _, a := range args {
			if a == "test" {
				return nil, &mockError{}
			}
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

type mockError struct{}

func (e *mockError) Error() string { return "mock error" }

func TestCleanStaleLinks(t *testing.T) {
	r := newMockRunner()
	if err := CleanStaleLinks(r, "intuneme"); err != nil {
		t.Fatalf("CleanStaleLinks failed: %v", err)
	}

	// Verify find command with correct -lname pattern.
	found := false
	for _, cmd := range r.commands {
		if strings.Contains(cmd, "find") && strings.Contains(cmd, "-lname") && strings.Contains(cmd, "/run/host-nvidia/*") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected find command with -lname '/run/host-nvidia/*', got: %v", r.commands)
	}

	// Verify cleanup of /run/host-nvidia.
	foundRm := false
	for _, cmd := range r.commands {
		if strings.Contains(cmd, "rm -rf /run/host-nvidia") {
			foundRm = true
			break
		}
	}
	if !foundRm {
		t.Errorf("expected rm -rf /run/host-nvidia, got: %v", r.commands)
	}
}

func TestSetup(t *testing.T) {
	r := newMockRunner()
	libs := []LibMapping{
		{Basename: "libcuda.so.1", HostPath: "/usr/lib/x86_64-linux-gnu/libcuda.so.1"},
		{Basename: "libnvoptix.so.1", HostPath: "/usr/lib64/libnvoptix.so.1"},
	}

	if err := Setup(r, "intuneme", libs); err != nil {
		t.Fatalf("Setup failed: %v", err)
	}

	// Verify symlink creation for both libraries.
	foundCuda := false
	foundNvoptix := false
	for _, cmd := range r.commands {
		if strings.Contains(cmd, "ln -s /run/host-nvidia/0/libcuda.so.1") {
			foundCuda = true
		}
		if strings.Contains(cmd, "ln -s /run/host-nvidia/1/libnvoptix.so.1") {
			foundNvoptix = true
		}
	}
	if !foundCuda {
		t.Errorf("expected symlink for libcuda.so.1 -> /run/host-nvidia/0/libcuda.so.1")
	}
	if !foundNvoptix {
		t.Errorf("expected symlink for libnvoptix.so.1 -> /run/host-nvidia/1/libnvoptix.so.1")
	}

	// Verify ldconfig was called.
	foundLdconfig := false
	for _, cmd := range r.commands {
		if strings.Contains(cmd, "ldconfig") {
			foundLdconfig = true
			break
		}
	}
	if !foundLdconfig {
		t.Errorf("expected ldconfig call after symlink creation")
	}
}

func TestLibDirMountsAndSetupAgreeOnIndexMapping(t *testing.T) {
	libs := []LibMapping{
		{Basename: "libcuda.so.1", HostPath: "/opt/nvidia/0/libcuda.so.1"},
		{Basename: "libnvidia-glcore.so.560", HostPath: "/opt/nvidia/0/libnvidia-glcore.so.560"},
		{Basename: "libnvoptix.so.1", HostPath: "/opt/nvidia/1/libnvoptix.so.1"},
		{Basename: "libnvidia-ml.so.1", HostPath: "/opt/nvidia/2/libnvidia-ml.so.1"},
		{Basename: "libnvidia-egl.so.1", HostPath: "/opt/nvidia/3/libnvidia-egl.so.1"},
		{Basename: "libnvidia-fbc.so.1", HostPath: "/opt/nvidia/4/libnvidia-fbc.so.1"},
		{Basename: "libnvidia-encode.so.1", HostPath: "/opt/nvidia/5/libnvidia-encode.so.1"},
		{Basename: "libnvidia-decode.so.1", HostPath: "/opt/nvidia/6/libnvidia-decode.so.1"},
	}

	r := newMockRunner()
	if err := Setup(r, "intuneme", libs); err != nil {
		t.Fatalf("Setup failed: %v", err)
	}

	setupIndexByDir := make(map[string]int)
	for _, lib := range libs {
		idx, ok := setupSymlinkIndex(r.commands, lib.Basename)
		if !ok {
			t.Fatalf("Setup did not create a symlink for %s; commands: %v", lib.Basename, r.commands)
		}
		setupIndexByDir[filepath.Dir(lib.HostPath)] = idx
	}

	// Re-check several LibDirMounts calls. With the old append-while-ranging-a-map
	// bug, slice position depended on map iteration and would eventually disagree
	// with the index Setup used in its /run/host-nvidia/<idx> symlink targets.
	for attempt := 0; attempt < 25; attempt++ {
		mounts := LibDirMounts(libs)
		for dir, idx := range setupIndexByDir {
			if idx < 0 || idx >= len(mounts) {
				t.Fatalf("Setup index %d for %s is outside LibDirMounts length %d", idx, dir, len(mounts))
			}
			wantContainer := fmt.Sprintf("/run/host-nvidia/%d", idx)
			if mounts[idx].Container != wantContainer {
				t.Fatalf("LibDirMounts()[%d].Container = %q, want %q", idx, mounts[idx].Container, wantContainer)
			}
			if mounts[idx].Host != dir {
				t.Fatalf("LibDirMounts()[%d].Host = %q, but Setup uses index %d for %q", idx, mounts[idx].Host, idx, dir)
			}
		}
	}
}

func setupSymlinkIndex(commands []string, basename string) (int, bool) {
	for _, cmd := range commands {
		fields := strings.Fields(cmd)
		for i := 0; i < len(fields)-2; i++ {
			if fields[i] != "ln" || fields[i+1] != "-s" {
				continue
			}
			target := fields[i+2]
			if filepath.Base(target) != basename {
				continue
			}
			idx, err := strconv.Atoi(filepath.Base(filepath.Dir(target)))
			if err != nil {
				return 0, false
			}
			return idx, true
		}
	}
	return 0, false
}

func TestCleanStaleLinks_PropagatesErrors(t *testing.T) {
	r := &errorRunner{failOn: "find"}
	if err := CleanStaleLinks(r, "intuneme"); err == nil {
		t.Error("expected error from find command, got nil")
	}

	r = &errorRunner{failOn: "rm"}
	if err := CleanStaleLinks(r, "intuneme"); err == nil {
		t.Error("expected error from rm command, got nil")
	}
}

// errorRunner fails on commands containing the failOn substring.
type errorRunner struct {
	failOn string
}

func (e *errorRunner) Run(name string, args ...string) ([]byte, error) {
	cmd := name + " " + strings.Join(args, " ")
	if name == "machinectl" && len(args) >= 1 && args[0] == "show" {
		return []byte("42\n"), nil
	}
	if strings.Contains(cmd, e.failOn) {
		return nil, &mockError{}
	}
	return nil, nil
}

func (e *errorRunner) RunAttached(string, ...string) error   { return nil }
func (e *errorRunner) RunBackground(string, ...string) error { return nil }
func (e *errorRunner) LookPath(name string) (string, error)  { return "/usr/bin/" + name, nil }

func TestSetup_SkipsExistingRegularFile(t *testing.T) {
	r := newMockRunner()
	libs := []LibMapping{
		{Basename: "libcuda.so.1", HostPath: "/usr/lib/x86_64-linux-gnu/libcuda.so.1"},
	}

	// Make the regular-file check succeed (file exists and is not a symlink).
	testCmd := "sudo nsenter -t 42 -m -- test -f /usr/lib/x86_64-linux-gnu/libcuda.so.1 -a ! -L /usr/lib/x86_64-linux-gnu/libcuda.so.1"
	r.outputs[testCmd] = []byte("")

	if err := Setup(r, "intuneme", libs); err != nil {
		t.Fatalf("Setup failed: %v", err)
	}

	// Verify no symlink was created (the ln command should not appear).
	for _, cmd := range r.commands {
		if strings.Contains(cmd, "ln -s") {
			t.Errorf("should not create symlink when regular file exists, but got: %s", cmd)
		}
	}
}
