package sudoers

import (
	"fmt"
	"os"

	"github.com/frostyard/intuneme/internal/runner"
)

const filePath = "/etc/sudoers.d/intuneme-exec"

// Install writes a sudoers rule that allows the given user to run nsenter
// into the intuneme container without a password prompt. This enables the
// GNOME extension to launch container apps (Edge, Portal) directly without
// needing a terminal for sudo authentication.
func Install(r runner.Runner, user string) error {
	rule := fmt.Sprintf(
		"# Installed by intuneme — passwordless nsenter for container app launch.\n"+
			"%s ALL=(root) NOPASSWD: /usr/bin/nsenter -t * -m -u -i -n -p -- /bin/su -s /bin/bash %s -c *\n",
		user, user,
	)

	tmp, err := os.CreateTemp("", "intuneme-sudoers-*")
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}
	defer func() { _ = os.Remove(tmp.Name()) }()

	if _, err := tmp.Write([]byte(rule)); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write temp file: %w", err)
	}
	_ = tmp.Close()

	// Validate before installing — a broken sudoers file can lock out sudo.
	if _, err := r.Run("/usr/sbin/visudo", "-c", "-f", tmp.Name()); err != nil {
		return fmt.Errorf("sudoers syntax check failed: %w", err)
	}

	if _, err := r.Run("sudo", "install", "-m", "0440", tmp.Name(), filePath); err != nil {
		return fmt.Errorf("install sudoers rule: %w", err)
	}
	return nil
}

// Remove deletes the sudoers rule file. Intentionally graceful: missing
// files and failed removals are not errors.
func Remove(r runner.Runner) {
	if !IsInstalled() {
		return
	}
	_, _ = r.Run("sudo", "rm", "-f", filePath)
}

// IsInstalled reports whether the sudoers rule file exists.
func IsInstalled() bool {
	_, err := os.Stat(filePath)
	return err == nil
}
