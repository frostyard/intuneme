package broker

import (
	"strings"
	"testing"
)

func TestSessionBusSocketPath(t *testing.T) {
	got := SessionBusSocketPath("/tmp/rootfs", 1000)
	want := "/tmp/rootfs/run/user/1000/bus"
	if got != want {
		t.Errorf("SessionBusSocketPath() = %q, want %q", got, want)
	}
}

func TestEnableLingerArgs(t *testing.T) {
	args := EnableLingerArgs("intuneme", "testuser")
	joined := strings.Join(args, " ")
	if !strings.Contains(joined, "root@intuneme") {
		t.Errorf("missing root@machine in: %s", joined)
	}
	if !strings.Contains(joined, "enable-linger") {
		t.Errorf("missing enable-linger in: %s", joined)
	}
	if !strings.Contains(joined, "testuser") {
		t.Errorf("missing username in: %s", joined)
	}
}

func TestUnlockKeyringArgs(t *testing.T) {
	args := UnlockKeyringArgs("intuneme", "testuser", "testpass")
	joined := strings.Join(args, " ")
	if !strings.Contains(joined, "testuser@intuneme") {
		t.Errorf("missing user@machine in: %s", joined)
	}
	if !strings.Contains(joined, "gnome-keyring-daemon") {
		t.Errorf("missing gnome-keyring-daemon in: %s", joined)
	}
}
