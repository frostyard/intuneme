package nspawn

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/frostyard/intuneme/internal/runner"
	"github.com/frostyard/intuneme/internal/sudo"
)

// validDisplay matches X11 display formats: ":N" or ":N.S" or "host:N" or "host:N.S"
var validDisplay = regexp.MustCompile(`^[a-zA-Z0-9._-]*:[0-9]+(\.[0-9]+)?$`)

// BindMount represents a host:container bind mount pair.
type BindMount struct {
	Host      string
	Container string
	ReadOnly  bool
}

// xauthorityPatterns are searched in XDG_RUNTIME_DIR when $XAUTHORITY is unset.
var xauthorityPatterns = []string{
	".mutter-Xwaylandauth.*",
	"xauth_*",
}

// findXAuthority locates the host's Xauthority file.
func findXAuthority(uid int) string {
	// Check env first
	if xa := os.Getenv("XAUTHORITY"); xa != "" {
		if _, err := os.Stat(xa); err == nil {
			return xa
		}
	}
	// Glob for known patterns in runtime dir
	runtimeDir := fmt.Sprintf("/run/user/%d", uid)
	for _, pattern := range xauthorityPatterns {
		matches, _ := filepath.Glob(filepath.Join(runtimeDir, pattern))
		if len(matches) > 0 {
			return matches[0]
		}
	}
	// Check classic location
	if home, err := os.UserHomeDir(); err == nil {
		xa := filepath.Join(home, ".Xauthority")
		if _, err := os.Stat(xa); err == nil {
			return xa
		}
	}
	return ""
}

// displayMarkerPath is the container-side path where the host DISPLAY value is stored.
// Written by the CLI before boot; read by intuneme-profile.sh and the broker service.
const displayMarkerPath = "etc/intuneme-host-display"

// HostDisplay returns the host's DISPLAY value, falling back to ":0".
// It validates that the corresponding X11 socket exists.
func HostDisplay() string {
	display := os.Getenv("DISPLAY")
	if display == "" {
		return ":0"
	}

	// Validate the X11 socket exists (display ":N" -> /tmp/.X11-unix/XN)
	if num, ok := strings.CutPrefix(display, ":"); ok {
		// Strip screen number if present (e.g., ":1.0" -> "1")
		if idx := strings.IndexByte(num, '.'); idx >= 0 {
			num = num[:idx]
		}
		socket := filepath.Join("/tmp/.X11-unix", "X"+num)
		if _, err := os.Stat(socket); err != nil {
			return ":0"
		}
	}

	return display
}

// WriteDisplayMarker writes the host DISPLAY value into the container rootfs
// so that container scripts and services can read it.
// Uses sudo install because the rootfs /etc/ is owned by root.
func WriteDisplayMarker(r runner.Runner, rootfs, display string) error {
	if !validDisplay.MatchString(display) {
		return fmt.Errorf("invalid display value: %q", display)
	}

	content := fmt.Sprintf("DISPLAY=%s\n", display)
	path := filepath.Join(rootfs, displayMarkerPath)
	return sudo.WriteFile(r, path, []byte(content), 0644)
}

// DetectHostSockets checks which optional host sockets/files exist and returns
// bind mount pairs for them.
func DetectHostSockets(uid int) []BindMount {
	runtimeDir := fmt.Sprintf("/run/user/%d", uid)
	checks := []struct {
		hostPath      string
		containerPath string
	}{
		{runtimeDir + "/wayland-0", "/run/host-wayland"},
		{runtimeDir + "/pipewire-0", "/run/host-pipewire"},
		{runtimeDir + "/pulse/native", "/run/host-pulse"},
	}

	var mounts []BindMount
	for _, c := range checks {
		if _, err := os.Stat(c.hostPath); err == nil {
			mounts = append(mounts, BindMount{Host: c.hostPath, Container: c.containerPath})
		}
	}

	// Xauthority — required for X11 display access
	if xa := findXAuthority(uid); xa != "" {
		mounts = append(mounts, BindMount{Host: xa, Container: "/run/host-xauthority"})
	}

	return mounts
}

// DetectDRIDevices scans for DRM card and render devices.
// Returns individual device bind mounts instead of a directory bind so that
// nspawn correctly adds each device to the cgroup DeviceAllow list.
func DetectDRIDevices() []BindMount {
	var mounts []BindMount
	for _, pattern := range []string{"/dev/dri/card*", "/dev/dri/renderD*"} {
		matches, _ := filepath.Glob(pattern)
		for _, dev := range matches {
			mounts = append(mounts, BindMount{Host: dev, Container: dev})
		}
	}
	return mounts
}

// BuildBootArgs returns the systemd-nspawn arguments to boot the container.
// nvidiaDevices are Nvidia device nodes that need explicit DeviceAllow entries
// (unlike DRI devices, nspawn does not auto-allow /dev/nvidia* in cgroups).
func BuildBootArgs(rootfs, machine, intuneHome, containerHome string, sockets []BindMount, nvidiaDevices []BindMount) []string {
	args := []string{
		"-D", rootfs,
		fmt.Sprintf("--machine=%s", machine),
		fmt.Sprintf("--bind=%s:%s", intuneHome, containerHome),
		"--bind=/tmp/.X11-unix",
	}
	// Bind DRI devices individually so nspawn adds them to the cgroup allow list.
	// A directory bind (--bind=/dev/dri) does not register contained devices.
	for _, dri := range DetectDRIDevices() {
		args = append(args, fmt.Sprintf("--bind=%s", dri.Host))
	}
	// Bind Nvidia device nodes with explicit cgroup DeviceAllow.
	for _, dev := range nvidiaDevices {
		args = append(args, fmt.Sprintf("--bind=%s", dev.Host))
		args = append(args, fmt.Sprintf("--property=DeviceAllow=%s rwm", dev.Host))
	}
	for _, s := range sockets {
		if s.ReadOnly {
			args = append(args, fmt.Sprintf("--bind-ro=%s:%s", s.Host, s.Container))
		} else {
			args = append(args, fmt.Sprintf("--bind=%s:%s", s.Host, s.Container))
		}
	}
	args = append(args, "--console=pipe", "-b")
	return args
}

// BuildShellArgs returns the machinectl shell arguments for an interactive session.
// Uses a login shell so /etc/profile.d scripts run (sets XAUTHORITY, audio, etc).
func BuildShellArgs(machine, user string) []string {
	return []string{"shell", fmt.Sprintf("%s@%s", user, machine), "/bin/bash", "--login"}
}

// LeaderPID returns the PID of the container's init process (Leader) as reported
// by machinectl, which is used to enter the container's namespaces via nsenter.
func LeaderPID(r runner.Runner, machine string) (string, error) {
	out, err := r.Run("machinectl", "show", machine, "-p", "Leader", "--value")
	if err != nil {
		return "", fmt.Errorf("machinectl show failed: %w", err)
	}
	pid := strings.TrimSpace(string(out))
	if pid == "" {
		return "", fmt.Errorf("could not determine container leader PID")
	}
	return pid, nil
}

// Exec runs a command non-interactively inside the container as the given user.
// Uses nsenter to enter the container's namespaces and launch the command in the
// background. Requires passwordless sudo for nsenter (installed by intuneme start
// via /etc/sudoers.d/intuneme-exec).
func Exec(r runner.Runner, machine, user string, uid int, command string) error {
	leaderPID, err := LeaderPID(r, machine)
	if err != nil {
		return err
	}
	uidStr := fmt.Sprintf("%d", uid)
	display := HostDisplay()
	script := fmt.Sprintf(
		`export DISPLAY=%s
export XAUTHORITY=/run/host-xauthority
export WAYLAND_DISPLAY=/run/host-wayland
export PIPEWIRE_REMOTE=/run/host-pipewire
export PULSE_SERVER=unix:/run/host-pulse
export XDG_RUNTIME_DIR=/run/user/%s
export DBUS_SESSION_BUS_ADDRESS=unix:path=/run/user/%s/bus
if [ -d /run/host-nvidia ]; then
    export __NV_PRIME_RENDER_OFFLOAD=1
    export __GLX_VENDOR_LIBRARY_NAME=nvidia
fi
nohup %s >/dev/null 2>&1 &`,
		display, uidStr, uidStr, command,
	)
	nsenterArgs := []string{
		"nsenter",
		"-t", leaderPID,
		"-m", "-u", "-i", "-n", "-p",
		"--", "/bin/su", "-s", "/bin/bash", user, "-c", script,
	}
	if _, err := r.Run("sudo", nsenterArgs...); err != nil {
		return fmt.Errorf("exec in container failed: %w", err)
	}
	return nil
}

// Boot starts the nspawn container in the background using sudo.
func Boot(r runner.Runner, rootfs, machine, intuneHome, containerHome string, sockets, nvidiaDevices []BindMount) error {
	args := append([]string{"systemd-nspawn"}, BuildBootArgs(rootfs, machine, intuneHome, containerHome, sockets, nvidiaDevices)...)
	return r.RunBackground("sudo", args...)
}

// ValidateSudo prompts for the sudo password if needed.
func ValidateSudo(r runner.Runner) error {
	return r.RunAttached("sudo", "-v")
}

// IsRunning checks if the machine is registered with machinectl.
func IsRunning(r runner.Runner, machine string) bool {
	_, err := r.Run("machinectl", "show", machine)
	return err == nil
}

// Shell opens an interactive session in the container via machinectl shell.
func Shell(r runner.Runner, machine, user string) error {
	args := BuildShellArgs(machine, user)
	return r.RunAttached("machinectl", args...)
}

// Stop powers off the container.
func Stop(r runner.Runner, machine string) error {
	out, err := r.Run("machinectl", "poweroff", machine)
	if err != nil {
		return fmt.Errorf("machinectl poweroff failed: %w\n%s", err, out)
	}
	return nil
}
