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
	if len(args) == 0 || args[0] != "systemd-run" {
		t.Fatalf("args do not start with systemd-run: %v", args)
	}
	joined := strings.Join(args, " ")
	// `machinectl shell` hides the exit status of the command it runs, so it
	// cannot report a loginctl failure. `systemd-run --wait` does.
	for _, want := range []string{"--machine=intuneme", "--wait", "--pipe", "enable-linger", "testuser"} {
		if !strings.Contains(joined, want) {
			t.Errorf("missing %s in: %s", want, joined)
		}
	}
	for _, unwanted := range []string{"shell", "root@"} {
		if strings.Contains(joined, unwanted) {
			t.Errorf("unexpected %s in: %s", unwanted, joined)
		}
	}
}

func TestLoginSessionArgs(t *testing.T) {
	args := LoginSessionArgs("intuneme", "testuser")
	joined := strings.Join(args, " ")
	if !strings.Contains(joined, "testuser@intuneme") {
		t.Errorf("missing user@machine in: %s", joined)
	}
	if !strings.Contains(joined, "--login") {
		t.Errorf("missing --login in: %s", joined)
	}
	if !strings.Contains(joined, "sleep infinity") {
		t.Errorf("missing sleep infinity in: %s", joined)
	}
}
