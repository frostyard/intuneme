package provision

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
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
