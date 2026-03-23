package nvidia

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/frostyard/intuneme/internal/runner"
)

// containerLibDir is the Ubuntu library path where symlinks are created.
const containerLibDir = "/usr/lib/x86_64-linux-gnu"

// CleanStaleLinks removes any symlinks in the container's library directory
// that point into /run/host-nvidia/. This must be called on every start
// (even non-Nvidia boots) because the rootfs persists across boots and
// stale symlinks from a previous Nvidia session could break the loader.
func CleanStaleLinks(r runner.Runner, machine string) error {
	leaderPID, err := leaderPID(r, machine)
	if err != nil {
		return err
	}

	// Remove symlinks targeting /run/host-nvidia/*.
	_, _ = r.Run("sudo", "nsenter", "-t", leaderPID, "-m", "--",
		"find", containerLibDir, "-maxdepth", "1",
		"-lname", "/run/host-nvidia/*", "-delete")

	// Remove the mount point directory itself if it exists.
	_, _ = r.Run("sudo", "nsenter", "-t", leaderPID, "-m", "--",
		"rm", "-rf", "/run/host-nvidia")

	return nil
}

// Setup creates symlinks inside the container for each Nvidia library,
// pointing from the container's lib dir into the bind-mounted host directories.
// It also runs ldconfig to update the dynamic linker cache.
func Setup(r runner.Runner, machine string, libs []LibMapping) error {
	leaderPID, err := leaderPID(r, machine)
	if err != nil {
		return err
	}

	dirIdx := libDirIndex(libs)

	for _, lib := range libs {
		idx := dirIdx[filepath.Dir(lib.HostPath)]
		target := fmt.Sprintf("/run/host-nvidia/%d/%s", idx, lib.Basename)
		linkPath := filepath.Join(containerLibDir, lib.Basename)

		// Skip if a regular file (not a symlink) already exists — don't
		// clobber package-owned files.
		out, err := r.Run("sudo", "nsenter", "-t", leaderPID, "-m", "--",
			"test", "-f", linkPath, "-a", "!", "-L", linkPath)
		if err == nil && len(strings.TrimSpace(string(out))) == 0 {
			// Regular file exists, skip.
			continue
		}

		// Remove any existing file/symlink and create the new symlink.
		_, _ = r.Run("sudo", "nsenter", "-t", leaderPID, "-m", "--",
			"rm", "-f", linkPath)
		if _, err := r.Run("sudo", "nsenter", "-t", leaderPID, "-m", "--",
			"ln", "-s", target, linkPath); err != nil {
			return fmt.Errorf("symlink %s -> %s: %w", linkPath, target, err)
		}
	}

	// Update the dynamic linker cache.
	if _, err := r.Run("sudo", "nsenter", "-t", leaderPID, "-m", "--",
		"ldconfig"); err != nil {
		return fmt.Errorf("ldconfig failed: %w", err)
	}

	return nil
}

// leaderPID returns the PID of the container's init process.
func leaderPID(r runner.Runner, machine string) (string, error) {
	out, err := r.Run("machinectl", "show", machine, "-p", "Leader", "--value")
	if err != nil {
		return "", fmt.Errorf("machinectl show failed: %w", err)
	}
	pid := strings.TrimSpace(string(out))
	if pid == "" || pid == "0" {
		return "", fmt.Errorf("container %s is not running", machine)
	}
	return pid, nil
}
