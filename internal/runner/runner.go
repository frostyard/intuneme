package runner

import (
	"os"
	"os/exec"
	"syscall"
)

// Runner executes system commands. Mockable for tests.
type Runner interface {
	// Run executes a command, returning combined output and error.
	Run(name string, args ...string) ([]byte, error)
	// RunAttached executes a command with stdin/stdout/stderr attached to the terminal.
	RunAttached(name string, args ...string) error
	// RunBackground starts a command detached from the terminal and returns immediately.
	// Stdin/stdout/stderr are connected to /dev/null.
	RunBackground(name string, args ...string) error
	// LookPath checks if a binary is in PATH.
	LookPath(name string) (string, error)
}

// SystemRunner executes real system commands.
type SystemRunner struct{}

func (r *SystemRunner) Run(name string, args ...string) ([]byte, error) {
	return exec.Command(name, args...).CombinedOutput()
}

func (r *SystemRunner) RunAttached(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func (r *SystemRunner) RunBackground(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	return cmd.Start()
}

func (r *SystemRunner) LookPath(name string) (string, error) {
	return exec.LookPath(name)
}
