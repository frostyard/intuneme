package config

import (
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"strings"
)

// ValidateRootfsPath reports whether rootfs is a path that intuneme may delete
// as root. The rootfs directory is removed with `sudo rm -rf` by destroy,
// recreate and the failed-init cleanup, so a wrong value deletes the wrong
// directory as root. root is the intuneme data root (the directory holding
// config.toml) and home is the host user's home directory; both are used as
// "never delete this" anchors.
//
// The path must be:
//
//   - present and absolute. A relative path would be resolved against the
//     caller's working directory, which differs per command.
//   - clean: no "." or ".." elements, no doubled or trailing slash. `rm` would
//     resolve those itself, so the value that was checked must be the value
//     that is deleted.
//   - not "/" and not a top-level directory such as /home, /usr, /etc or /var.
//     Deleting one of those destroys the host.
//   - neither equal to nor an ancestor of the home directory or of the data
//     root. The default rootfs lives inside the data root, and the enrollment
//     state lives in ~/Intune, so a path above them deletes user data.
//
// Symlinks are only resolved in the parent path, never in the last element:
// `sudo rm -rf P` follows the parent's symlinks and removes the last element of
// P inside the resolved parent, while a rootfs that is itself a symlink is a
// supported setup (rm removes only the link). So the parent is resolved and the
// rules above are applied again to `<resolved parent>/<base of rootfs>`, which
// must stay outside the home directory and the data root even after a symlink
// points elsewhere. A parent that does not exist leaves nothing to delete and
// skips those checks. Any other resolution failure is an error, never a pass: a
// path that cannot be checked must not be deleted.
func ValidateRootfsPath(rootfs, root, home string) error {
	if rootfs == "" {
		return fmt.Errorf("rootfs_path %q is empty; it must be an absolute path to the container root filesystem", rootfs)
	}
	if !filepath.IsAbs(rootfs) {
		return fmt.Errorf("rootfs_path %q must be an absolute path", rootfs)
	}
	if filepath.Clean(rootfs) != rootfs {
		return fmt.Errorf("rootfs_path %q must be a clean path without '.' or '..' elements or extra slashes", rootfs)
	}
	if isTopLevel(rootfs) {
		return fmt.Errorf("rootfs_path %q must not be the filesystem root or a top-level directory", rootfs)
	}

	// The anchors must be usable, or the checks below cannot run at all.
	if home == "" || !filepath.IsAbs(home) {
		return fmt.Errorf("rootfs_path %q cannot be checked: the home directory %q is not an absolute path", rootfs, home)
	}
	home = filepath.Clean(home)
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return fmt.Errorf("rootfs_path %q cannot be checked: data root %q: %w", rootfs, root, err)
	}
	absRoot = filepath.Clean(absRoot)

	if contains(rootfs, home) {
		return fmt.Errorf("rootfs_path %q must not be the home directory or contain it (%s)", rootfs, home)
	}
	if contains(rootfs, absRoot) {
		return fmt.Errorf("rootfs_path %q must not be the data root or contain it (%s)", rootfs, absRoot)
	}

	// Resolve the parent path only. Report every value that had to be resolved so
	// a resolution failure is an error rather than a silent pass.
	resolved, ok, err := resolveRootfsParent(rootfs)
	if err != nil {
		return fmt.Errorf("rootfs_path %q cannot be checked: %w", rootfs, err)
	}
	if !ok {
		// The parent does not exist, so there is nothing to delete.
		return nil
	}
	if isTopLevel(resolved) {
		return fmt.Errorf("rootfs_path %q resolves to %q, the filesystem root or a top-level directory", rootfs, resolved)
	}

	homeValues := []string{home}
	if value, ok, err := resolveExisting(home); err != nil {
		return fmt.Errorf("rootfs_path %q cannot be checked: home directory %q: %w", rootfs, home, err)
	} else if ok {
		homeValues = append(homeValues, value)
	}
	rootValues := []string{absRoot}
	if value, ok, err := resolveExisting(absRoot); err != nil {
		return fmt.Errorf("rootfs_path %q cannot be checked: data root %q: %w", rootfs, absRoot, err)
	} else if ok {
		rootValues = append(rootValues, value)
	}

	for _, value := range homeValues {
		if contains(resolved, value) {
			return fmt.Errorf("rootfs_path %q resolves to %q, which is the home directory or contains it (%s)", rootfs, resolved, value)
		}
	}
	for _, value := range rootValues {
		if contains(resolved, value) {
			return fmt.Errorf("rootfs_path %q resolves to %q, which is the data root or contains it (%s)", rootfs, resolved, value)
		}
	}
	return nil
}

// isTopLevel reports whether path is the filesystem root or a directory
// directly below it, such as /home, /usr or /var.
func isTopLevel(path string) bool {
	return filepath.Dir(path) == "/"
}

// contains reports whether a is b or an ancestor of b. Both values must be
// clean absolute paths, and a must not be "/": a+"/" is "//", which matches no
// path, so the top-level check must catch "/" before this function runs.
func contains(a, b string) bool {
	return a == b || strings.HasPrefix(b, a+"/")
}

// resolveRootfsParent returns the path `sudo rm -rf` would act on: the base of
// rootfs joined to the resolved parent path. ok is false when the parent does
// not exist, because then there is nothing to delete. The last element of
// rootfs is never resolved: removing a symlink removes only the link.
func resolveRootfsParent(rootfs string) (resolved string, ok bool, err error) {
	parent, ok, err := resolveExisting(filepath.Dir(rootfs))
	if err != nil || !ok {
		return "", false, err
	}
	return filepath.Join(parent, filepath.Base(rootfs)), true, nil
}

// resolveExisting returns the symlink-free value of an absolute path. ok is
// false when the path does not exist; every other error is returned so the
// caller fails closed.
func resolveExisting(path string) (resolved string, ok bool, err error) {
	resolved, err = filepath.EvalSymlinks(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return "", false, nil
		}
		return "", false, err
	}
	return resolved, true, nil
}
