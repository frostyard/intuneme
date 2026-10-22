package busproxy

import (
	"testing"

	"github.com/frostyard/intuneme/internal/busproxy/busproxytest"
)

func TestDialReportsListenerPID(t *testing.T) {
	sock, daemonPID := busproxytest.StartBus(t)

	conn, pid, err := Dial(sock)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer func() { _ = conn.Close() }()

	if pid != daemonPID {
		t.Errorf("pid = %d, want dbus-daemon PID %d", pid, daemonPID)
	}
	if names := conn.Names(); len(names) == 0 {
		t.Error("connection has no unique name, Hello did not run")
	}
}
