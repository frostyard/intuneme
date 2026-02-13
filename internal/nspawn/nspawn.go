package nspawn

import (
	"fmt"

	"github.com/bjk/intuneme/internal/runner"
	"github.com/bjk/intuneme/internal/session"
)

// BuildBootArgs returns the systemd-nspawn arguments to boot the container.
func BuildBootArgs(rootfs, machine, homeDir string, s *session.Session) []string {
	args := []string{
		"-D", rootfs,
		fmt.Sprintf("--machine=%s", machine),
		fmt.Sprintf("--bind=%s", homeDir),
		"--bind=/tmp/.X11-unix",
		fmt.Sprintf("--bind=%s:/run/user-external/%d", s.XDGRuntimeDir, s.UID),
		"-b",
	}
	return args
}

// BuildShellArgs returns the machinectl shell arguments to launch intune-portal.
func BuildShellArgs(machine, user string, s *session.Session) []string {
	args := []string{"shell"}
	for _, env := range s.ContainerEnv() {
		args = append(args, fmt.Sprintf("--setenv=%s", env))
	}
	args = append(args, fmt.Sprintf("%s@%s", user, machine))
	args = append(args, "/usr/bin/intune-portal")
	return args
}

// Boot starts the nspawn container in the background using sudo.
func Boot(r runner.Runner, rootfs, machine, homeDir string, s *session.Session) error {
	args := append([]string{"systemd-nspawn"}, BuildBootArgs(rootfs, machine, homeDir, s)...)
	return r.RunAttached("sudo", args...)
}

// IsRunning checks if the machine is registered with machinectl.
func IsRunning(r runner.Runner, machine string) bool {
	_, err := r.Run("machinectl", "show", machine)
	return err == nil
}

// LaunchIntune runs intune-portal inside the container via machinectl shell.
func LaunchIntune(r runner.Runner, machine, user string, s *session.Session) error {
	args := append([]string{"shell"}, BuildShellArgs(machine, user, s)[1:]...)
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
