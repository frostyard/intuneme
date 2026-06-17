package sudoers

import (
	"fmt"
	"os"

	"github.com/frostyard/intuneme/internal/nspawn"
	"github.com/frostyard/intuneme/internal/runner"
)

const filePath = "/etc/sudoers.d/intuneme-exec"

// Install writes a sudoers rule granting the user passwordless sudo for the
// intuneme nsenter helper, so the GNOME extension can launch container apps
// (Edge, Portal) without a terminal prompt.
//
// The rule authorizes a single fixed helper path, not a raw nsenter command
// line. sudo-rs (the default sudo/su on Ubuntu 25.10+) forbids wildcards in
// command arguments, so the old rule (which used "*" for the leader PID and
// script) was rejected and broke every sudo call. The helper keeps the rule
// wildcard-free; what the user can invoke is unchanged.
func Install(r runner.Runner, user string) error {
	// Install the helper first. It must be root-owned and not user-writable,
	// since it runs as root before dropping to the user via su.
	if _, err := r.Run("sudo", "install", "-d", "-m", "0755", "-o", "root", "-g", "root", nspawn.NsenterHelperDir); err != nil {
		return fmt.Errorf("create helper dir: %w", err)
	}
	if err := installHelper(r, nspawn.NsenterHelperScript(user)); err != nil {
		return err
	}

	rule := fmt.Sprintf(
		"# Installed by intuneme: passwordless nsenter helper for container app launch.\n"+
			"%s ALL=(root) NOPASSWD: %s\n",
		user, nspawn.NsenterHelperPath,
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

	// Validate before installing: a broken sudoers file can lock out sudo.
	if _, err := r.Run("/usr/sbin/visudo", "-c", "-f", tmp.Name()); err != nil {
		return fmt.Errorf("sudoers syntax check failed: %w", err)
	}

	if _, err := r.Run("sudo", "install", "-m", "0440", tmp.Name(), filePath); err != nil {
		return fmt.Errorf("install sudoers rule: %w", err)
	}
	return nil
}

// installHelper writes the helper script to NsenterHelperPath, root-owned and
// executable (0755) so the user cannot modify the code that runs as root.
func installHelper(r runner.Runner, script string) error {
	tmp, err := os.CreateTemp("", "intuneme-nsenter-helper-*")
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}
	defer func() { _ = os.Remove(tmp.Name()) }()

	if _, err := tmp.Write([]byte(script)); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write temp file: %w", err)
	}
	_ = tmp.Close()

	if _, err := r.Run("sudo", "install", "-m", "0755", "-o", "root", "-g", "root", tmp.Name(), nspawn.NsenterHelperPath); err != nil {
		return fmt.Errorf("install nsenter helper: %w", err)
	}
	return nil
}

// Remove deletes the sudoers rule file and the privileged helper. Intentionally
// graceful: missing files and failed removals are not errors.
func Remove(r runner.Runner) {
	_, _ = r.Run("sudo", "rm", "-f", filePath, nspawn.NsenterHelperPath)
}

// IsInstalled reports whether both the sudoers rule and the nsenter helper
// exist. Requiring both means an upgrade from the old wildcard-only rule counts
// as not installed, so start self-heals by reinstalling the rule and helper.
func IsInstalled() bool {
	if _, err := os.Stat(filePath); err != nil {
		return false
	}
	if _, err := os.Stat(nspawn.NsenterHelperPath); err != nil {
		return false
	}
	return true
}
