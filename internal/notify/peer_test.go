package notify

import "testing"

func TestDialBusReportsListenerPID(t *testing.T) {
	sock, daemonPID := startBus(t)

	conn, pid, err := dialBus(sock)
	if err != nil {
		t.Fatalf("dialBus: %v", err)
	}
	defer func() { _ = conn.Close() }()

	if pid != daemonPID {
		t.Errorf("pid = %d, want dbus-daemon PID %d", pid, daemonPID)
	}
	if names := conn.Names(); len(names) == 0 {
		t.Error("connection has no unique name, Hello did not run")
	}
}
