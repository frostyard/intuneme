package nspawn

import (
	"strings"
	"testing"

	"github.com/bjk/intuneme/internal/session"
)

func TestBuildBootArgs(t *testing.T) {
	s := &session.Session{
		XDGRuntimeDir: "/run/user/1000",
		Display:       ":1",
		XAuthority:    "/run/user/1000/.mutter-Xwaylandauth.abc123",
		UID:           1000,
	}
	args := BuildBootArgs("/home/testuser/.local/share/intuneme/rootfs", "intuneme", "/home/testuser", s)

	joined := strings.Join(args, " ")
	if !strings.Contains(joined, "--machine=intuneme") {
		t.Errorf("missing --machine flag in: %s", joined)
	}
	if !strings.Contains(joined, "--bind=/home/testuser") {
		t.Errorf("missing home bind in: %s", joined)
	}
	if !strings.Contains(joined, "--bind=/tmp/.X11-unix") {
		t.Errorf("missing X11 bind in: %s", joined)
	}
	if !strings.Contains(joined, "--bind=/run/user/1000:/run/user-external/1000") {
		t.Errorf("missing XDG runtime bind in: %s", joined)
	}
	if !strings.Contains(joined, "-b") {
		t.Errorf("missing -b (boot) flag in: %s", joined)
	}
}

func TestBuildShellArgs(t *testing.T) {
	s := &session.Session{
		XDGRuntimeDir: "/run/user/1000",
		Display:       ":1",
		DBusAddress:   "unix:path=/run/user/1000/bus",
		XAuthority:    "/run/user/1000/.mutter-Xwaylandauth.abc123",
		UID:           1000,
	}
	args := BuildShellArgs("intuneme", "testuser", s)

	joined := strings.Join(args, " ")
	if !strings.Contains(joined, "shell") {
		t.Errorf("missing shell subcommand in: %s", joined)
	}
	if !strings.Contains(joined, "testuser@intuneme") {
		t.Errorf("missing user@machine in: %s", joined)
	}
	if !strings.Contains(joined, "--setenv=DISPLAY=:1") {
		t.Errorf("missing DISPLAY setenv in: %s", joined)
	}
	if !strings.Contains(joined, "intune-portal") {
		t.Errorf("missing intune-portal command in: %s", joined)
	}
}
