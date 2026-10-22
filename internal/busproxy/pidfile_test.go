package busproxy

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestCheckNotRunning(t *testing.T) {
	dir := t.TempDir()
	pidPath := filepath.Join(dir, "x.pid")

	if err := CheckNotRunning(pidPath, "notify-proxy"); err != nil {
		t.Errorf("missing file: %v", err)
	}

	// A live process with the subcommand in its command line.
	// "notify-proxy" becomes $0. The trailing ":" keeps sh from exec'ing sleep.
	cmd := exec.Command("sh", "-c", "sleep 30; :", "notify-proxy")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cmd.Process.Kill(); _ = cmd.Wait() })
	// Until the child has run exec, its cmdline is still the test binary's.
	cmdlinePath := "/proc/" + strconv.Itoa(cmd.Process.Pid) + "/cmdline"
	for range 100 {
		if data, _ := os.ReadFile(cmdlinePath); strings.Contains(string(data), "notify-proxy") {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if err := os.WriteFile(pidPath, []byte(strconv.Itoa(cmd.Process.Pid)), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := CheckNotRunning(pidPath, "notify-proxy"); err == nil {
		t.Error("live proxy not detected")
	}
	// The same live process is not another proxy's instance.
	if err := CheckNotRunning(pidPath, "screencast-proxy"); err != nil {
		t.Errorf("unrelated live process: %v", err)
	}

	// A dead process.
	_ = cmd.Process.Kill()
	_ = cmd.Wait()
	if err := CheckNotRunning(pidPath, "notify-proxy"); err != nil {
		t.Errorf("dead process: %v", err)
	}
}

func TestRemoveOwnPID(t *testing.T) {
	pidPath := filepath.Join(t.TempDir(), "x.pid")

	if err := os.WriteFile(pidPath, []byte("1"), 0o644); err != nil {
		t.Fatal(err)
	}
	RemoveOwnPID(pidPath)
	if _, err := os.Stat(pidPath); err != nil {
		t.Errorf("another process's PID file was removed: %v", err)
	}

	if err := WritePID(pidPath); err != nil {
		t.Fatal(err)
	}
	RemoveOwnPID(pidPath)
	if _, err := os.Stat(pidPath); !os.IsNotExist(err) {
		t.Errorf("own PID file not removed: %v", err)
	}
}
