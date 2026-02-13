package session

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDiscoverFromEnv(t *testing.T) {
	t.Setenv("XDG_RUNTIME_DIR", "/run/user/1000")
	t.Setenv("DISPLAY", ":1")
	t.Setenv("WAYLAND_DISPLAY", "wayland-0")
	t.Setenv("DBUS_SESSION_BUS_ADDRESS", "unix:path=/run/user/1000/bus")
	t.Setenv("XAUTHORITY", "/run/user/1000/.Xauthority")

	s, err := Discover(1000)
	if err != nil {
		t.Fatalf("Discover() error: %v", err)
	}
	if s.Display != ":1" {
		t.Errorf("Display = %q, want %q", s.Display, ":1")
	}
	if s.WaylandDisplay != "wayland-0" {
		t.Errorf("WaylandDisplay = %q, want %q", s.WaylandDisplay, "wayland-0")
	}
	if s.XAuthority != "/run/user/1000/.Xauthority" {
		t.Errorf("XAuthority = %q, want %q", s.XAuthority, "/run/user/1000/.Xauthority")
	}
	if s.DBusAddress != "unix:path=/run/user/1000/bus" {
		t.Errorf("DBusAddress = %q, want %q", s.DBusAddress, "unix:path=/run/user/1000/bus")
	}
}

func TestDiscoverXAuthorityGlob(t *testing.T) {
	tmp := t.TempDir()
	// Simulate .mutter-Xwaylandauth.XXXXXX file
	authFile := filepath.Join(tmp, ".mutter-Xwaylandauth.abc123")
	os.WriteFile(authFile, []byte{}, 0600)

	t.Setenv("XDG_RUNTIME_DIR", tmp)
	t.Setenv("DISPLAY", ":0")
	t.Setenv("WAYLAND_DISPLAY", "")
	t.Setenv("DBUS_SESSION_BUS_ADDRESS", "")
	t.Setenv("XAUTHORITY", "") // force glob search

	s, err := Discover(1000)
	if err != nil {
		t.Fatalf("Discover() error: %v", err)
	}
	if s.XAuthority != authFile {
		t.Errorf("XAuthority = %q, want %q", s.XAuthority, authFile)
	}
}

func TestDiscoverNoDisplay(t *testing.T) {
	t.Setenv("XDG_RUNTIME_DIR", "/run/user/1000")
	t.Setenv("DISPLAY", "")
	t.Setenv("WAYLAND_DISPLAY", "")
	t.Setenv("DBUS_SESSION_BUS_ADDRESS", "")
	t.Setenv("XAUTHORITY", "")

	_, err := Discover(1000)
	if err == nil {
		t.Fatal("expected error for missing DISPLAY, got nil")
	}
}

func TestDiscoverNoXAuthority(t *testing.T) {
	tmp := t.TempDir() // empty dir, no xauth files

	t.Setenv("XDG_RUNTIME_DIR", tmp)
	t.Setenv("DISPLAY", ":0")
	t.Setenv("WAYLAND_DISPLAY", "")
	t.Setenv("DBUS_SESSION_BUS_ADDRESS", "")
	t.Setenv("XAUTHORITY", "")

	_, err := Discover(1000)
	if err == nil {
		t.Fatal("expected error for missing XAUTHORITY, got nil")
	}
}
