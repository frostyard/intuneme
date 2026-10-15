package provision

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/frostyard/intuneme/internal/runner"
	"github.com/frostyard/intuneme/internal/sudo"
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

type himmelblauStatePath struct {
	source  string
	restore string
}

var himmelblauStatePaths = []himmelblauStatePath{
	{source: "var/lib/himmelblaud", restore: "var/lib/private/himmelblaud"},
	{source: "var/cache/himmelblaud", restore: "var/cache/private/himmelblaud"},
	{source: "var/cache/himmelblau-policies", restore: "var/cache/himmelblau-policies"},
	{source: "var/cache/nss-himmelblau", restore: "var/cache/nss-himmelblau"},
}

const systemdCredentialKeyPath = "var/lib/systemd/credential.secret"
const himmelblauEncryptedPINPath = "var/lib/himmelblaud/hsm-pin-nopcr.enc"
const privateHimmelblauEncryptedPINPath = "var/lib/private/himmelblaud/hsm-pin-nopcr.enc"

// BackupHimmelblauState copies the daemon, policy, and NSS caches that contain
// Himmelblau device state. StateDirectory/CacheDirectory paths owned by
// DynamicUser services are copied from their public paths and restored under
// /var/lib/private or /var/cache/private so systemd can recreate its symlinks.
// It also preserves systemd's local credential key, which is required to
// decrypt Himmelblau's encrypted HSM PIN after recreate. Returns "" when no
// state has been created yet.
func BackupHimmelblauState(r runner.Runner, rootfs string) (backupDir string, retErr error) {
	if err := verifyHimmelblauCredentialKey(r, rootfs); err != nil {
		return "", err
	}
	tmpDir, err := os.MkdirTemp("", "intuneme-himmelblau-backup-*")
	if err != nil {
		return "", fmt.Errorf("create temp dir: %w", err)
	}

	keepBackup := false
	defer func() {
		if keepBackup {
			return
		}
		if err := RemoveHimmelblauStateBackup(r, tmpDir); err != nil {
			if retErr == nil {
				retErr = err
			} else {
				retErr = fmt.Errorf("%w; %v", retErr, err)
			}
		}
	}()

	backedUp := false
	for _, statePath := range himmelblauStatePaths {
		sourcePath := statePath.source
		src := filepath.Join(rootfs, sourcePath)
		info, err := os.Stat(src)
		if errors.Is(err, fs.ErrNotExist) && statePath.restore != statePath.source {
			sourcePath = statePath.restore
			src = filepath.Join(rootfs, sourcePath)
			info, err = os.Stat(src)
		}
		switch {
		case errors.Is(err, fs.ErrNotExist):
			continue
		case errors.Is(err, fs.ErrPermission):
			if _, err := r.Run("sudo", "test", "-d", src); err != nil {
				return "", fmt.Errorf("check Himmelblau state %s: %w", sourcePath, err)
			}
		case err != nil:
			return "", fmt.Errorf("stat Himmelblau state %s: %w", sourcePath, err)
		case !info.IsDir():
			return "", fmt.Errorf("himmelblau state path %s is not a directory", sourcePath)
		}

		dest := filepath.Join(tmpDir, statePath.source)
		if err := os.MkdirAll(dest, 0700); err != nil {
			return "", fmt.Errorf("create backup path %s: %w", statePath.source, err)
		}
		out, err := r.Run("sudo", "cp", "-a", src+string(filepath.Separator)+".", dest+string(filepath.Separator))
		if err != nil {
			return "", fmt.Errorf("backup Himmelblau state %s: %w\n%s", statePath.source, err, out)
		}
		backedUp = true
	}

	credentialKey := filepath.Join(rootfs, systemdCredentialKeyPath)
	keyInfo, keyErr := os.Stat(credentialKey)
	if keyErr != nil && !errors.Is(keyErr, fs.ErrNotExist) {
		return "", fmt.Errorf("stat systemd credential key: %w", keyErr)
	}
	if keyErr == nil {
		if !keyInfo.Mode().IsRegular() {
			return "", fmt.Errorf("systemd credential key path %s is not a regular file", systemdCredentialKeyPath)
		}
		dest := filepath.Join(tmpDir, systemdCredentialKeyPath)
		if err := os.MkdirAll(filepath.Dir(dest), 0700); err != nil {
			return "", fmt.Errorf("create systemd credential key backup path: %w", err)
		}
		out, err := r.Run("sudo", "cp", "-a", credentialKey, dest)
		if err != nil {
			return "", fmt.Errorf("backup systemd credential key: %w\n%s", err, out)
		}
		backupKeyInfo, err := os.Lstat(dest)
		if err != nil {
			return "", fmt.Errorf("verify systemd credential key backup: %w", err)
		}
		if !backupKeyInfo.Mode().IsRegular() {
			return "", fmt.Errorf("systemd credential key backup path %s is not a regular file", systemdCredentialKeyPath)
		}
		backedUp = true
	}

	if err := verifyHimmelblauBackupCredentialKey(tmpDir); err != nil {
		return "", err
	}

	if !backedUp {
		return "", nil
	}
	keepBackup = true
	return tmpDir, nil
}

// RemoveHimmelblauStateBackup removes a staged backup produced by
// BackupHimmelblauState, retrying with sudo when copied state is protected.
func RemoveHimmelblauStateBackup(r runner.Runner, backupDir string) error {
	if backupDir == "" {
		return nil
	}
	cleanPath := filepath.Clean(backupDir)
	if filepath.Dir(cleanPath) != filepath.Clean(os.TempDir()) ||
		!strings.HasPrefix(filepath.Base(cleanPath), "intuneme-himmelblau-backup-") {
		return fmt.Errorf("refusing to remove unexpected Himmelblau backup path %q", backupDir)
	}
	if err := os.RemoveAll(cleanPath); err == nil {
		return nil
	}
	out, err := r.Run("sudo", "rm", "-rf", "--", cleanPath)
	if err != nil {
		return fmt.Errorf("remove Himmelblau backup %s: %w\n%s", cleanPath, err, out)
	}
	if _, err := os.Lstat(cleanPath); err == nil {
		return fmt.Errorf("himmelblau backup still exists after privileged removal: %s", cleanPath)
	} else if !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("verify Himmelblau backup removal: %w", err)
	}
	return nil
}

func verifyHimmelblauCredentialKey(r runner.Runner, rootfs string) error {
	hsmPinFound := false
	for _, relPath := range []string{himmelblauEncryptedPINPath, privateHimmelblauEncryptedPINPath} {
		found, err := pathExistsWithPrivilege(r, filepath.Join(rootfs, relPath))
		if err != nil {
			return fmt.Errorf("inspect Himmelblau HSM PIN credential: %w", err)
		}
		if found {
			hsmPinFound = true
			break
		}
	}
	if !hsmPinFound {
		return nil
	}

	keyPath := filepath.Join(rootfs, systemdCredentialKeyPath)
	keyInfo, err := os.Stat(keyPath)
	if errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("himmelblau's encrypted HSM PIN exists but %s is missing; recreate cannot decrypt it safely. Restore the key from a backup or reinitialize an unenrolled container", filepath.Join("/", systemdCredentialKeyPath))
	}
	if err != nil {
		return fmt.Errorf("inspect systemd credential key: %w", err)
	}
	if !keyInfo.Mode().IsRegular() {
		return fmt.Errorf("systemd credential key %s is not a regular file", filepath.Join("/", systemdCredentialKeyPath))
	}
	return nil
}

func pathExistsWithPrivilege(r runner.Runner, path string) (bool, error) {
	_, err := os.Stat(path)
	if err == nil {
		return true, nil
	}
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	if !errors.Is(err, fs.ErrPermission) {
		return false, err
	}

	const checkPath = `if [ -e "$1" ] || [ -L "$1" ]; then printf 'present\n'; elif [ -d "${1%/*}" ]; then printf 'missing\n'; else exit 1; fi`
	out, err := r.Run("sudo", "sh", "-c", checkPath, "intuneme-path-check", path)
	if err != nil {
		return false, fmt.Errorf("privileged path check: %w\n%s", err, out)
	}
	switch strings.TrimSpace(string(out)) {
	case "present":
		return true, nil
	case "missing":
		return false, nil
	default:
		return false, fmt.Errorf("privileged path check returned unexpected output %q", strings.TrimSpace(string(out)))
	}
}

// RestoreHimmelblauState restores the state copied by BackupHimmelblauState.
func RestoreHimmelblauState(r runner.Runner, rootfs, backupDir string) error {
	if err := verifyHimmelblauBackupCredentialKey(backupDir); err != nil {
		return err
	}

	for _, statePath := range himmelblauStatePaths {
		src := filepath.Join(backupDir, statePath.source)
		if _, err := os.Stat(src); errors.Is(err, fs.ErrNotExist) {
			continue
		} else if err != nil {
			return fmt.Errorf("stat Himmelblau backup %s: %w", statePath.source, err)
		}

		dest := filepath.Join(rootfs, statePath.restore)
		out, err := r.Run("sudo", "mkdir", "-p", dest)
		if err != nil {
			return fmt.Errorf("create Himmelblau state path %s: %w\n%s", statePath.restore, err, out)
		}
		out, err = r.Run("sudo", "cp", "-a", src+string(filepath.Separator)+".", dest+string(filepath.Separator))
		if err != nil {
			return fmt.Errorf("restore Himmelblau state %s: %w\n%s", statePath.restore, err, out)
		}
	}
	credentialKey := filepath.Join(backupDir, systemdCredentialKeyPath)
	if _, err := os.Stat(credentialKey); errors.Is(err, fs.ErrNotExist) {
		return nil
	} else if err != nil {
		return fmt.Errorf("stat systemd credential key backup: %w", err)
	}
	dest := filepath.Join(rootfs, systemdCredentialKeyPath)
	out, err := r.Run("sudo", "mkdir", "-p", filepath.Dir(dest))
	if err != nil {
		return fmt.Errorf("create systemd credential key directory: %w\n%s", err, out)
	}
	out, err = r.Run("sudo", "cp", "-a", credentialKey, dest)
	if err != nil {
		return fmt.Errorf("restore systemd credential key: %w\n%s", err, out)
	}
	keyInfo, err := os.Lstat(dest)
	if err != nil {
		return fmt.Errorf("verify restored systemd credential key: %w", err)
	}
	if !keyInfo.Mode().IsRegular() {
		return fmt.Errorf("restored systemd credential key %s is not a regular file", systemdCredentialKeyPath)
	}
	out, err = r.Run("sudo", "cmp", "-s", credentialKey, dest)
	if err != nil {
		return fmt.Errorf("verify restored systemd credential key contents: %w\n%s", err, out)
	}
	return nil
}

func verifyHimmelblauBackupCredentialKey(backupDir string) error {
	pinPath := filepath.Join(backupDir, himmelblauEncryptedPINPath)
	if _, err := os.Stat(pinPath); errors.Is(err, fs.ErrNotExist) {
		return nil
	} else if err != nil {
		return fmt.Errorf("inspect Himmelblau backup HSM PIN credential: %w", err)
	}

	keyPath := filepath.Join(backupDir, systemdCredentialKeyPath)
	keyInfo, err := os.Lstat(keyPath)
	if errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("himmelblau backup contains an encrypted HSM PIN but is missing %s; the PIN cannot be restored safely", filepath.Join("/", systemdCredentialKeyPath))
	}
	if err != nil {
		return fmt.Errorf("inspect systemd credential key backup: %w", err)
	}
	if !keyInfo.Mode().IsRegular() {
		return fmt.Errorf("systemd credential key backup %s is not a regular file", filepath.Join("/", systemdCredentialKeyPath))
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

	return sudo.WriteFile(r, shadowPath, []byte(strings.Join(lines, "\n")), 0640)
}
