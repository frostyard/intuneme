package broker

import (
	"strings"
	"testing"
)

func TestRuntimeDir(t *testing.T) {
	got := RuntimeDir("/tmp/intuneme")
	want := "/tmp/intuneme/runtime"
	if got != want {
		t.Errorf("RuntimeDir() = %q, want %q", got, want)
	}
}

func TestSessionBusSocketPath(t *testing.T) {
	got := SessionBusSocketPath("/tmp/intuneme")
	want := "/tmp/intuneme/runtime/bus"
	if got != want {
		t.Errorf("SessionBusSocketPath() = %q, want %q", got, want)
	}
}

func TestRuntimeBindMount(t *testing.T) {
	host, container := RuntimeBindMount("/tmp/intuneme", 1000)
	if host != "/tmp/intuneme/runtime" {
		t.Errorf("host = %q, want /tmp/intuneme/runtime", host)
	}
	if container != "/run/user/1000" {
		t.Errorf("container = %q, want /run/user/1000", container)
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
