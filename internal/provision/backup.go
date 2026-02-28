package provision

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/frostyard/intuneme/internal/runner"
)

const deviceBrokerRelPath = "var/lib/microsoft-identity-device-broker"

// BackupDeviceBrokerState copies the device broker state directory from the
// rootfs to a temporary directory. Returns the temp directory path, or ""
// if the broker directory doesn't exist (no enrollment to preserve).
// The caller is responsible for cleaning up the temp directory.
func BackupDeviceBrokerState(r runner.Runner, rootfs string) (string, error) {
	brokerDir := filepath.Join(rootfs, deviceBrokerRelPath)
	if _, err := os.Stat(brokerDir); errors.Is(err, fs.ErrNotExist) {
		return "", nil
	}

	tmpDir, err := os.MkdirTemp("", "intuneme-broker-backup-*")
	if err != nil {
		return "", fmt.Errorf("create temp dir: %w", err)
	}

	dest := filepath.Join(tmpDir, "microsoft-identity-device-broker")
	if _, err := r.Run("sudo", "cp", "-a", brokerDir, dest); err != nil {
		_ = os.RemoveAll(tmpDir)
		return "", fmt.Errorf("backup device broker state: %w", err)
	}
	return tmpDir, nil
}

// RestoreDeviceBrokerState copies the backed-up device broker state back
// into the new rootfs. The backupDir should be the path returned by
// BackupDeviceBrokerState.
func RestoreDeviceBrokerState(r runner.Runner, rootfs, backupDir string) error {
	src := filepath.Join(backupDir, "microsoft-identity-device-broker")
	dest := filepath.Join(rootfs, deviceBrokerRelPath)
	if _, err := r.Run("sudo", "cp", "-a", src, dest); err != nil {
		return fmt.Errorf("restore device broker state: %w", err)
	}
	return nil
}

// BackupShadowEntry reads the shadow file from the rootfs and returns the
// full line for the given username. This preserves the password hash so it
// can be restored after re-provisioning. Uses sudo because the rootfs shadow
// file is owned by root after nspawn use.
func BackupShadowEntry(r runner.Runner, rootfs, username string) (string, error) {
	data, err := r.Run("sudo", "cat", filepath.Join(rootfs, "etc", "shadow"))
	if err != nil {
		return "", fmt.Errorf("read shadow: %w", err)
	}
	for line := range strings.SplitSeq(strings.TrimRight(string(data), "\n"), "\n") {
		if line == "" {
			continue
		}
		name, _, _ := strings.Cut(line, ":")
		if name == username {
			return line, nil
		}
	}
	return "", fmt.Errorf("user %q not found in shadow file", username)
}

// RestoreShadowEntry reads the new rootfs shadow file, replaces the line
// for the user extracted from shadowLine, and writes it back via sudo.
func RestoreShadowEntry(r runner.Runner, rootfs, shadowLine string) error {
	username, _, _ := strings.Cut(shadowLine, ":")

	shadowPath := filepath.Join(rootfs, "etc", "shadow")
	data, err := r.Run("sudo", "cat", shadowPath)
	if err != nil {
		return fmt.Errorf("read shadow: %w", err)
	}

	lines := strings.Split(string(data), "\n")
	found := false
	for i, line := range lines {
		name, _, _ := strings.Cut(line, ":")
		if name == username {
			lines[i] = shadowLine
			found = true
			break
		}
	}
	if !found {
		return fmt.Errorf("user %q not found in new shadow file", username)
	}

	return sudoWriteFile(r, shadowPath, []byte(strings.Join(lines, "\n")), 0640)
}
