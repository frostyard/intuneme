package session

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Session holds the discovered host graphical session environment.
type Session struct {
	XDGRuntimeDir  string
	Display        string
	WaylandDisplay string
	DBusAddress    string
	XAuthority     string
	UID            int
}

// xauthorityPatterns are searched in XDG_RUNTIME_DIR when $XAUTHORITY is unset.
var xauthorityPatterns = []string{
	".mutter-Xwaylandauth.*",
	"xauth_*",
	".Xauthority",
}

// Discover detects the host graphical session environment.
func Discover(uid int) (*Session, error) {
	s := &Session{UID: uid}

	// XDG_RUNTIME_DIR
	s.XDGRuntimeDir = os.Getenv("XDG_RUNTIME_DIR")
	if s.XDGRuntimeDir == "" {
		s.XDGRuntimeDir = fmt.Sprintf("/run/user/%d", uid)
	}

	// DISPLAY (required)
	s.Display = os.Getenv("DISPLAY")
	if s.Display == "" {
		return nil, fmt.Errorf("no DISPLAY set — intune-portal requires a graphical session")
	}

	// WAYLAND_DISPLAY (optional)
	s.WaylandDisplay = os.Getenv("WAYLAND_DISPLAY")

	// DBUS_SESSION_BUS_ADDRESS
	s.DBusAddress = os.Getenv("DBUS_SESSION_BUS_ADDRESS")
	if s.DBusAddress == "" {
		s.DBusAddress = fmt.Sprintf("unix:path=%s/bus", s.XDGRuntimeDir)
	}

	// XAUTHORITY — check env first, then glob for known patterns
	s.XAuthority = os.Getenv("XAUTHORITY")
	if s.XAuthority == "" {
		found, err := findXAuthority(s.XDGRuntimeDir)
		if err != nil {
			return nil, err
		}
		s.XAuthority = found
	}

	return s, nil
}

func findXAuthority(runtimeDir string) (string, error) {
	var searched []string
	for _, pattern := range xauthorityPatterns {
		full := filepath.Join(runtimeDir, pattern)
		searched = append(searched, full)
		matches, _ := filepath.Glob(full)
		if len(matches) > 0 {
			return matches[0], nil
		}
	}
	return "", fmt.Errorf(
		"no XAUTHORITY found — searched for:\n  %s\nSet $XAUTHORITY to the correct path",
		strings.Join(searched, "\n  "),
	)
}

// ContainerXAuthority returns the XAUTHORITY path remapped into the container's
// /run/user-external/<uid>/ mount point.
func (s *Session) ContainerXAuthority() string {
	base := filepath.Base(s.XAuthority)
	return fmt.Sprintf("/run/user-external/%d/%s", s.UID, base)
}

// ContainerEnv returns --setenv flags for machinectl shell.
func (s *Session) ContainerEnv() []string {
	uid := s.UID
	env := []string{
		fmt.Sprintf("XDG_RUNTIME_DIR=/run/user-external/%d", uid),
		fmt.Sprintf("DBUS_SESSION_BUS_ADDRESS=unix:path=/run/user-external/%d/bus", uid),
		fmt.Sprintf("DISPLAY=%s", s.Display),
		fmt.Sprintf("XAUTHORITY=%s", s.ContainerXAuthority()),
	}
	if s.WaylandDisplay != "" {
		env = append(env, fmt.Sprintf("WAYLAND_DISPLAY=%s", s.WaylandDisplay))
	}
	return env
}
