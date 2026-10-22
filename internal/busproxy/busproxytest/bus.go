// Package busproxytest runs private dbus-daemons for proxy tests.
package busproxytest

import (
	"bufio"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// StartBus runs a private dbus-daemon listening on a socket in a temp dir and
// returns the socket path and the daemon's PID. The test is skipped when
// dbus-daemon is not installed.
func StartBus(t *testing.T) (string, int) {
	t.Helper()
	if _, err := exec.LookPath("dbus-daemon"); err != nil {
		t.Skip("dbus-daemon not installed")
	}
	sock := filepath.Join(t.TempDir(), "bus")
	cmd := exec.Command("dbus-daemon", "--session", "--nofork", "--nopidfile",
		"--address=unix:path="+sock, "--print-address=1")
	out, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatalf("stdout pipe: %v", err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatalf("start dbus-daemon: %v", err)
	}
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	})
	// The daemon prints its address once the socket is listening.
	line, err := bufio.NewReader(out).ReadString('\n')
	if err != nil || !strings.HasPrefix(line, "unix:path="+sock) {
		t.Fatalf("dbus-daemon address: %q, %v", line, err)
	}
	if _, err := os.Stat(sock); err != nil {
		t.Fatalf("bus socket: %v", err)
	}
	return sock, cmd.Process.Pid
}
