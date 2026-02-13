package nspawn

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/bjk/intuneme/internal/runner"
)

// BindMount represents a host:container bind mount pair.
type BindMount struct {
	Host      string
	Container string
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
			mounts = append(mounts, BindMount{c.hostPath, c.containerPath})
		}
	}

	// Xauthority — required for X11 display access
	if xa := findXAuthority(uid); xa != "" {
		mounts = append(mounts, BindMount{xa, "/run/host-xauthority"})
	}

	return mounts
}

// BuildBootArgs returns the systemd-nspawn arguments to boot the container.
func BuildBootArgs(rootfs, machine, intuneHome, containerHome string, sockets []BindMount) []string {
	args := []string{
		"-D", rootfs,
		fmt.Sprintf("--machine=%s", machine),
		fmt.Sprintf("--bind=%s:%s", intuneHome, containerHome),
		"--bind=/tmp/.X11-unix",
		"--bind=/dev/dri",
	}
	for _, s := range sockets {
		args = append(args, fmt.Sprintf("--bind=%s:%s", s.Host, s.Container))
	}
	args = append(args, "--console=pipe", "-b")
	return args
}

// BuildShellArgs returns the machinectl shell arguments for an interactive session.
func BuildShellArgs(machine, user string) []string {
	return []string{"shell", fmt.Sprintf("%s@%s", user, machine)}
}

// Boot starts the nspawn container in the background using sudo.
func Boot(r runner.Runner, rootfs, machine, intuneHome, containerHome string, sockets []BindMount) error {
	args := append([]string{"systemd-nspawn"}, BuildBootArgs(rootfs, machine, intuneHome, containerHome, sockets)...)
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
