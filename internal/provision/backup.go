package provision

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/frostyard/intuneme/internal/runner"
)

// BackupShadowEntry reads the shadow file from the rootfs and returns the
// full line for the given username. This preserves the password hash so it
// can be restored after re-provisioning.
func BackupShadowEntry(rootfs, username string) (string, error) {
	data, err := os.ReadFile(filepath.Join(rootfs, "etc", "shadow"))
	if err != nil {
		return "", fmt.Errorf("read shadow: %w", err)
	}
	for _, line := range strings.Split(string(data), "\n") {
		if line == "" {
			continue
		}
		fields := strings.SplitN(line, ":", 2)
		if fields[0] == username {
			return line, nil
		}
	}
	return "", fmt.Errorf("user %q not found in shadow file", username)
}

// RestoreShadowEntry reads the new rootfs shadow file, replaces the line
// for the user extracted from shadowLine, and writes it back via sudo.
func RestoreShadowEntry(r runner.Runner, rootfs, shadowLine string) error {
	username := strings.SplitN(shadowLine, ":", 2)[0]

	shadowPath := filepath.Join(rootfs, "etc", "shadow")
	data, err := os.ReadFile(shadowPath)
	if err != nil {
		return fmt.Errorf("read shadow: %w", err)
	}

	lines := strings.Split(string(data), "\n")
	found := false
	for i, line := range lines {
		fields := strings.SplitN(line, ":", 2)
		if fields[0] == username {
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
