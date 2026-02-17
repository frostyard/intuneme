package nspawn

import (
	"strings"
	"testing"
)

func TestBuildBootArgs(t *testing.T) {
	sockets := []BindMount{
		{"/run/user/1000/wayland-0", "/run/host-wayland"},
	}
	args := BuildBootArgs("/tmp/rootfs", "intuneme", "/home/testuser/Intune", "/home/testuser", sockets)

	joined := strings.Join(args, " ")
	if !strings.Contains(joined, "--machine=intuneme") {
		t.Errorf("missing --machine flag in: %s", joined)
	}
	if !strings.Contains(joined, "--bind=/home/testuser/Intune:/home/testuser") {
		t.Errorf("missing home bind in: %s", joined)
	}
	if !strings.Contains(joined, "--bind=/tmp/.X11-unix") {
		t.Errorf("missing X11 bind in: %s", joined)
	}
	if !strings.Contains(joined, "--bind=/dev/dri") {
		t.Errorf("missing /dev/dri bind in: %s", joined)
	}
	if !strings.Contains(joined, "--bind=/run/user/1000/wayland-0:/run/host-wayland") {
		t.Errorf("missing wayland socket bind in: %s", joined)
	}
	if !strings.Contains(joined, "-b") {
		t.Errorf("missing -b (boot) flag in: %s", joined)
	}
}

func TestBuildBootArgsNoSockets(t *testing.T) {
	args := BuildBootArgs("/tmp/rootfs", "intuneme", "/home/testuser/Intune", "/home/testuser", nil)

	joined := strings.Join(args, " ")
	if !strings.Contains(joined, "--machine=intuneme") {
		t.Errorf("missing --machine flag in: %s", joined)
	}
	if strings.Contains(joined, "host-wayland") {
		t.Errorf("unexpected wayland bind in: %s", joined)
	}
	if strings.Contains(joined, "host-pipewire") {
		t.Errorf("unexpected pipewire bind in: %s", joined)
	}
	if strings.Contains(joined, "host-pulse") {
		t.Errorf("unexpected pulse bind in: %s", joined)
	}
}

func TestBuildShellArgs(t *testing.T) {
	args := BuildShellArgs("intuneme", "testuser")

	joined := strings.Join(args, " ")
	if !strings.Contains(joined, "shell") {
		t.Errorf("missing shell subcommand in: %s", joined)
	}
	if !strings.Contains(joined, "testuser@intuneme") {
		t.Errorf("missing user@machine in: %s", joined)
	}
	if !strings.Contains(joined, "/bin/bash --login") {
		t.Errorf("missing login shell in: %s", joined)
	}
}

func TestDetectHostSockets_PulseAudio(t *testing.T) {
	sockets := []BindMount{
		{"/run/user/1000/pulse/native", "/run/host-pulse"},
	}
	args := BuildBootArgs("/tmp/rootfs", "intuneme", "/home/testuser/Intune", "/home/testuser", sockets)

	joined := strings.Join(args, " ")
	if !strings.Contains(joined, "--bind=/run/user/1000/pulse/native:/run/host-pulse") {
		t.Errorf("missing pulse socket bind in: %s", joined)
	}
}
