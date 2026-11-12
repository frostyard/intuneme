package cmd

import (
	"fmt"
	"os"

	"github.com/frostyard/intuneme/internal/config"
	"github.com/frostyard/intuneme/internal/runner"
)

// removeRootfs deletes the container rootfs with sudo, after checking the path
// once more with config.ValidateRootfsPath. config.Load already checked the
// value it read, but destroy, recreate and the failed-init cleanup all delete a
// directory as root, so each of them checks again right before the delete. A
// refused path runs no command at all.
func removeRootfs(r runner.Runner, root, path string) ([]byte, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("refusing to remove rootfs: cannot determine home directory: %w", err)
	}
	if err := config.ValidateRootfsPath(path, root, home); err != nil {
		return nil, fmt.Errorf("refusing to remove rootfs: %w", err)
	}
	return r.Run("sudo", "rm", "-rf", path)
}
