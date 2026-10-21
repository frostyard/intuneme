//go:build linux

package nshelper

import (
	"encoding/binary"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"golang.org/x/sys/unix"
)

// This test executes the real helper in disposable user/mount/PID/UTS/IPC/net
// namespaces. It never needs host root or an enrolled container. CI must set
// INTUNEME_REQUIRE_KERNEL_TESTS=1 so missing namespace support is a failure.
func TestHelperKernelBoundary(t *testing.T) {
	if os.Getenv("INTUNEME_KERNEL_INNER") == "1" {
		runKernelBoundary(t)
		return
	}
	unshare, err := exec.LookPath("unshare")
	if err != nil {
		kernelUnavailable(t, err)
		return
	}
	args := []string{"--user", "--map-auto", "--map-root-user", "-muinp", "-f", "--mount-proc"}
	if out, err := exec.Command(unshare, append(args, "true")...).CombinedOutput(); err != nil {
		kernelUnavailable(t, fmt.Errorf("unshare: %w: %s", err, out))
		return
	}
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"plain", "setuid", "filecap", "exit", "bind"} {
		t.Run(mode, func(t *testing.T) {
			cmd := exec.Command(unshare, append(args, exe, "-test.run=^TestHelperKernelBoundary$", "-test.v")...)
			cmd.Env = append(os.Environ(), "INTUNEME_KERNEL_INNER=1", "INTUNEME_KERNEL_MODE="+mode)
			out, err := cmd.CombinedOutput()
			if err != nil {
				t.Fatalf("kernel boundary failed: %v\n%s", err, out)
			}
			if !strings.Contains(string(out), "KERNEL_BOUNDARY_OK") {
				t.Fatalf("child did not attest credentials: %s", out)
			}
		})
	}
}

func kernelUnavailable(t *testing.T, err error) {
	t.Helper()
	if os.Getenv("INTUNEME_REQUIRE_KERNEL_TESTS") == "1" {
		t.Fatal(err)
	}
	t.Skipf("kernel integration requires unshare and subordinate UID/GID maps: %v", err)
}

func runKernelBoundary(t *testing.T) {
	t.Helper()
	dir, err := os.MkdirTemp("/tmp", "intuneme-kernel-")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Remove(dir) }()
	if err := unix.Mount("tmpfs", dir, "tmpfs", 0, "mode=0755"); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = unix.Unmount(dir, 0) }()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(exe)
	if err != nil {
		t.Fatal(err)
	}
	fixture := filepath.Join(dir, "fixture")
	if err := os.WriteFile(fixture, data, 0755); err != nil {
		t.Fatal(err)
	}
	if os.Getenv("INTUNEME_KERNEL_MODE") == "bind" {
		runBindKernelBoundary(t, dir)
		return
	}
	switch os.Getenv("INTUNEME_KERNEL_MODE") {
	case "setuid":
		if err := os.Chmod(fixture, 0755|os.ModeSetuid); err != nil {
			t.Fatal(err)
		}
	case "filecap":
		// A permitted CAP_SETUID file capability, without the effective bit, so
		// exec can proceed with a zero bounding set and we can inspect the result.
		capdata := make([]byte, 20)
		binary.LittleEndian.PutUint32(capdata, 0x02000000)
		binary.LittleEndian.PutUint32(capdata[4:], 1<<unix.CAP_SETUID)
		if err := unix.Setxattr(fixture, "security.capability", capdata, 0); err != nil {
			t.Fatal(err)
		}
	}
	// Capture the expected namespace identities before the transition.
	var expected []string
	for _, ns := range []string{"mnt", "uts", "ipc", "net", "pid"} {
		value, err := os.Readlink("/proc/self/ns/" + ns)
		if err != nil {
			t.Fatal(err)
		}
		expected = append(expected, value)
	}
	findExecutable = func(string) (string, error) { return "/unused", nil }
	commandOutput = func(string, ...string) ([]byte, error) { return []byte(strconv.Itoa(os.Getpid())), nil }
	cfg := &Config{Machine: "test", User: "test", UID: 1000, GID: 1000, Home: "/", Rootfs: "/"}
	script := fmt.Sprintf("INTUNEME_EXPECT_NS='%s' exec '%s' -test.run=^TestKernelChild$", strings.Join(expected, "|"), fixture)
	wantStatus := 0
	if os.Getenv("INTUNEME_KERNEL_MODE") == "exit" {
		script = strings.Replace(script, " exec ", " ", 1) + " || exit $?; exit 37"
		wantStatus = 37
	}
	if status := Run(cfg, script); status != wantStatus {
		t.Fatalf("helper exit %d", status)
	}
}

func TestKernelChild(t *testing.T) {
	expected := os.Getenv("INTUNEME_EXPECT_NS")
	if expected == "" {
		t.Skip("subprocess only")
	}
	if os.Getuid() != 1000 || os.Geteuid() != 1000 || os.Getgid() != 1000 || os.Getegid() != 1000 {
		t.Fatal("privilege regained")
	}
	groups, err := os.Getgroups()
	if err != nil || len(groups) != 1 || groups[0] != 1000 {
		t.Fatalf("groups %v: %v", groups, err)
	}
	status, err := os.ReadFile("/proc/self/status")
	if err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"CapInh:", "CapPrm:", "CapEff:", "CapBnd:", "CapAmb:"} {
		if !strings.Contains(string(status), field+"\t0000000000000000") {
			t.Fatalf("unexpected %s: %s", field, status)
		}
	}
	if !strings.Contains(string(status), "NoNewPrivs:\t1") {
		t.Fatal("no_new_privs missing")
	}
	var got []string
	for _, ns := range []string{"mnt", "uts", "ipc", "net", "pid"} {
		value, err := os.Readlink("/proc/self/ns/" + ns)
		if err != nil {
			t.Fatal(err)
		}
		got = append(got, value)
	}
	if strings.Join(got, "|") != expected {
		t.Fatalf("namespaces %v, expected %s", got, expected)
	}
	if cwd, err := os.Getwd(); err != nil || cwd != "/" {
		t.Fatalf("cwd %q: %v", cwd, err)
	}
	fmt.Println("KERNEL_BOUNDARY_OK")
}
