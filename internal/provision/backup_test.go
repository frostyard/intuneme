package provision

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/frostyard/intuneme/internal/runner"
)

type backupCopyRunner struct {
	mockRunner
	skipCopyFrom   string
	protectedPaths []string
}

type backupCleanupRunner struct {
	mockRunner
	protectedDir string
}

type restoreCredentialKeyRunner struct {
	mockRunner
	copyCredentialKey    bool
	corruptCredentialKey bool
}

const testMachineID = "0123456789abcdef0123456789abcdef\n"

func writeTestMachineID(t *testing.T, root string) {
	t.Helper()
	path := filepath.Join(root, "etc/machine-id")
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(testMachineID), 0444); err != nil {
		t.Fatal(err)
	}
}

func (r *restoreCredentialKeyRunner) Run(name string, args ...string) ([]byte, error) {
	out, err := r.mockRunner.Run(name, args...)
	if err != nil || name != "sudo" || len(args) == 0 {
		return out, err
	}
	if args[0] == "cp" && len(args) == 4 && filepath.Base(args[2]) == "machine-id" {
		if err := os.MkdirAll(filepath.Dir(args[3]), 0755); err != nil {
			return nil, err
		}
		return exec.Command("cp", args[1:]...).CombinedOutput()
	}
	if args[0] == "cp" && len(args) == 4 && args[1] == "-a" && filepath.Base(args[2]) == "credential.secret" {
		if !r.copyCredentialKey {
			return out, nil
		}
		if err := os.MkdirAll(filepath.Dir(args[3]), 0700); err != nil {
			return nil, err
		}
		if r.corruptCredentialKey {
			return nil, os.WriteFile(args[3], []byte("incorrect key"), 0600)
		}
		return exec.Command("cp", args[1:]...).CombinedOutput()
	}
	if args[0] == "cmp" && len(args) == 4 && args[1] == "-s" {
		return nil, exec.Command("cmp", args[2], args[3]).Run()
	}
	return out, nil
}

func (r *backupCopyRunner) Run(name string, args ...string) ([]byte, error) {
	if name == "sudo" && len(args) == 4 && args[0] == "cp" && args[1] == "-a" {
		r.commands = append(r.commands, name+" "+strings.Join(args, " "))
		if args[2] == r.skipCopyFrom {
			return nil, nil
		}
		for _, path := range r.protectedPaths {
			if err := os.Chmod(path, 0700); err != nil {
				return nil, err
			}
		}
		out, err := exec.Command("cp", args[1:]...).CombinedOutput()
		for _, path := range r.protectedPaths {
			if chmodErr := os.Chmod(path, 0000); chmodErr != nil {
				return out, chmodErr
			}
		}
		return out, err
	}
	if name == "sudo" && len(args) > 0 {
		switch args[0] {
		case "mkdir", "install", "ln", "cmp":
			r.commands = append(r.commands, name+" "+strings.Join(args, " "))
			return exec.Command(args[0], args[1:]...).CombinedOutput()
		}
	}
	return r.mockRunner.Run(name, args...)
}

func TestHimmelblauRecreatePreservesCredentialIdentityAndPublicLinks(t *testing.T) {
	for _, private := range []bool{false, true} {
		t.Run(map[bool]string{false: "legacy public directories", true: "DynamicUser private directories"}[private], func(t *testing.T) {
			oldRoot := t.TempDir()
			writeTestMachineID(t, oldRoot)
			for relPath, contents := range map[string]string{
				"var/lib/himmelblaud/hsm-pin-nopcr.enc": "encrypted PIN fixture",
				"var/cache/himmelblaud/cache.db":        "protected PRT fixture",
				"var/lib/systemd/credential.secret":     "credential key fixture",
			} {
				if private && strings.Contains(relPath, "/himmelblaud/") {
					relPath = strings.Replace(relPath, "/himmelblaud/", "/private/himmelblaud/", 1)
				}
				path := filepath.Join(oldRoot, relPath)
				if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, []byte(contents), 0600); err != nil {
					t.Fatal(err)
				}
			}
			if private {
				for _, parent := range []string{"var/lib", "var/cache"} {
					if err := os.Symlink("private/himmelblaud", filepath.Join(oldRoot, parent, "himmelblaud")); err != nil {
						t.Fatal(err)
					}
				}
			}
			r := &backupCopyRunner{}
			backup, err := BackupHimmelblauState(r, oldRoot)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				if err := RemoveHimmelblauStateBackup(r, backup); err != nil {
					t.Error(err)
				}
			})
			newRoot := t.TempDir()
			writeTestMachineID(t, newRoot)
			if err := os.Chmod(filepath.Join(newRoot, "etc/machine-id"), 0644); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(newRoot, "etc/machine-id"), []byte("fedcba9876543210fedcba9876543210\n"), 0444); err != nil {
				t.Fatal(err)
			}
			if err := RestoreHimmelblauState(r, newRoot, backup); err != nil {
				t.Fatal(err)
			}
			for relPath, want := range map[string]string{
				"etc/machine-id":                                testMachineID,
				"var/lib/systemd/credential.secret":             "credential key fixture",
				"var/lib/private/himmelblaud/hsm-pin-nopcr.enc": "encrypted PIN fixture",
				"var/cache/private/himmelblaud/cache.db":        "protected PRT fixture",
				"var/lib/himmelblaud/hsm-pin-nopcr.enc":         "encrypted PIN fixture",
			} {
				got, err := os.ReadFile(filepath.Join(newRoot, relPath))
				if err != nil || string(got) != want {
					t.Errorf("restored %s differs from original: %v", relPath, err)
				}
			}
			for _, parent := range []string{"var/lib", "var/cache"} {
				link, err := os.Readlink(filepath.Join(newRoot, parent, "himmelblaud"))
				if err != nil || link != "private/himmelblaud" {
					t.Errorf("%s public link = %q, %v; root initializer must reuse private state", parent, link, err)
				}
				info, err := os.Stat(filepath.Join(newRoot, parent, "private"))
				if err != nil || info.Mode().Perm() != 0700 {
					t.Errorf("%s private parent must be protected: %v", parent, err)
				}
			}
		})
	}
}

func TestBackupHimmelblauStateRejectsInvalidMachineID(t *testing.T) {
	for _, id := range []string{"missing", "", "uninitialized\n", strings.Repeat("0", 32), strings.Repeat("x", 32)} {
		t.Run(id, func(t *testing.T) {
			rootfs := t.TempDir()
			key := filepath.Join(rootfs, systemdCredentialKeyPath)
			if err := os.MkdirAll(filepath.Dir(key), 0755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(key, []byte("fixture key"), 0400); err != nil {
				t.Fatal(err)
			}
			if id != "missing" {
				if err := os.MkdirAll(filepath.Join(rootfs, "etc"), 0755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(rootfs, machineIDPath), []byte(id), 0444); err != nil {
					t.Fatal(err)
				}
			}
			r := &mockRunner{}
			if _, err := BackupHimmelblauState(r, rootfs); err == nil || !strings.Contains(err.Error(), "machine ID") {
				t.Fatalf("want machine-ID preflight error, got %v", err)
			}
			if len(r.commands) != 0 {
				t.Fatalf("invalid identity must fail before copying state: %v", r.commands)
			}
		})
	}
}

func TestBackupHimmelblauStateRejectsMissingMachineIDCopy(t *testing.T) {
	rootfs := t.TempDir()
	writeTestMachineID(t, rootfs)
	key := filepath.Join(rootfs, systemdCredentialKeyPath)
	if err := os.MkdirAll(filepath.Dir(key), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(key, []byte("fixture key"), 0400); err != nil {
		t.Fatal(err)
	}
	r := &backupCopyRunner{skipCopyFrom: filepath.Join(rootfs, machineIDPath)}
	backup, err := BackupHimmelblauState(r, rootfs)
	if backup != "" {
		t.Cleanup(func() { _ = os.RemoveAll(backup) })
	}
	if err == nil || !strings.Contains(err.Error(), "verify backup machine ID") {
		t.Fatalf("want missing machine-ID backup error, got %v", err)
	}
}

func TestRestoreHimmelblauStateRejectsBackupWithoutMachineID(t *testing.T) {
	backup := t.TempDir()
	key := filepath.Join(backup, systemdCredentialKeyPath)
	if err := os.MkdirAll(filepath.Dir(key), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(key, []byte("fixture key"), 0400); err != nil {
		t.Fatal(err)
	}
	r := &mockRunner{}
	err := RestoreHimmelblauState(r, t.TempDir(), backup)
	if err == nil || !strings.Contains(err.Error(), "machine ID") {
		t.Fatalf("want incomplete credential-identity error, got %v", err)
	}
	if len(r.commands) != 0 {
		t.Fatalf("restore must fail before copying: %v", r.commands)
	}
}

func TestRestoreHimmelblauStateRejectsMismatchedMachineIDCopy(t *testing.T) {
	backup := t.TempDir()
	writeTestMachineID(t, backup)
	key := filepath.Join(backup, systemdCredentialKeyPath)
	if err := os.MkdirAll(filepath.Dir(key), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(key, []byte("fixture key"), 0400); err != nil {
		t.Fatal(err)
	}
	rootfs := t.TempDir()
	if err := os.MkdirAll(filepath.Join(rootfs, "etc"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(rootfs, machineIDPath), []byte("fedcba9876543210fedcba9876543210\n"), 0444); err != nil {
		t.Fatal(err)
	}
	r := &backupCopyRunner{skipCopyFrom: filepath.Join(backup, machineIDPath)}
	err := RestoreHimmelblauState(r, rootfs, backup)
	if err == nil || !strings.Contains(err.Error(), "verify restored machine ID contents") {
		t.Fatalf("want mismatched restored machine-ID error, got %v", err)
	}
}

func TestBackupHimmelblauStateRejectsConflictingLayouts(t *testing.T) {
	for _, parent := range []string{"var/lib", "var/cache"} {
		t.Run(parent, func(t *testing.T) {
			rootfs := t.TempDir()
			for _, rel := range []string{"himmelblaud", "private/himmelblaud"} {
				if err := os.MkdirAll(filepath.Join(rootfs, parent, rel), 0700); err != nil {
					t.Fatal(err)
				}
			}
			r := &mockRunner{}
			_, err := BackupHimmelblauState(r, rootfs)
			if err == nil || !strings.Contains(err.Error(), "conflicting public and private") {
				t.Fatalf("want ambiguous state-layout error, got %v", err)
			}
			if len(r.commands) != 0 {
				t.Fatalf("ambiguous state must fail before backup: %v", r.commands)
			}
		})
	}
}

func TestRestoreHimmelblauStateRejectsConflictingPublicPath(t *testing.T) {
	for _, symlink := range []bool{false, true} {
		t.Run(map[bool]string{false: "directory", true: "unexpected link"}[symlink], func(t *testing.T) {
			rootfs := t.TempDir()
			parent := filepath.Join(rootfs, "var/lib")
			if err := os.MkdirAll(parent, 0755); err != nil {
				t.Fatal(err)
			}
			public := filepath.Join(parent, "himmelblaud")
			if symlink {
				if err := os.Symlink("wrong-state", public); err != nil {
					t.Fatal(err)
				}
			} else if err := os.Mkdir(public, 0700); err != nil {
				t.Fatal(err)
			}
			r := &mockRunner{}
			err := RestoreHimmelblauState(r, rootfs, t.TempDir())
			if err == nil || !strings.Contains(err.Error(), "conflicting Himmelblau public state") {
				t.Fatalf("want public-path conflict error, got %v", err)
			}
			if len(r.commands) != 0 {
				t.Fatalf("conflicting destination must fail before restore: %v", r.commands)
			}
		})
	}
}

func (r *backupCleanupRunner) Run(name string, args ...string) ([]byte, error) {
	if name == "sudo" && len(args) == 4 && args[0] == "rm" && args[1] == "-rf" && args[2] == "--" {
		r.commands = append(r.commands, name+" "+strings.Join(args, " "))
		if err := os.Chmod(r.protectedDir, 0700); err != nil {
			return nil, err
		}
		return nil, os.RemoveAll(args[3])
	}
	return r.mockRunner.Run(name, args...)
}

func TestBackupShadowEntry(t *testing.T) {
	tests := []struct {
		name     string
		shadow   string
		username string
		want     string
		wantErr  bool
	}{
		{
			name:     "extracts user line",
			shadow:   "root:*:20466:0:99999:7:::\ndaemon:*:20466:0:99999:7:::\nbjk:$y$j9T$hash:20501:0:99999:7:::\n",
			username: "bjk",
			want:     "bjk:$y$j9T$hash:20501:0:99999:7:::",
			wantErr:  false,
		},
		{
			name:     "user not found",
			shadow:   "root:*:20466:0:99999:7:::\n",
			username: "nobody",
			want:     "",
			wantErr:  true,
		},
		{
			name:     "handles trailing newline",
			shadow:   "alice:$6$salt$hash:20000:0:99999:7:::\n\n",
			username: "alice",
			want:     "alice:$6$salt$hash:20000:0:99999:7:::",
			wantErr:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := &mockRunner{outputs: [][]byte{[]byte(tt.shadow)}}
			got, err := BackupShadowEntry(r, "/fake/rootfs", tt.username)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestRestoreShadowEntry(t *testing.T) {
	tests := []struct {
		name       string
		shadow     string
		shadowLine string
		username   string
		want       string
		wantErr    bool
	}{
		{
			name:       "replaces existing user line",
			shadow:     "root:*:20466:0:99999:7:::\nbjk:$new$hash:20501:0:99999:7:::\n",
			shadowLine: "bjk:$old$hash:20000:0:99999:7:::",
			username:   "bjk",
			want:       "root:*:20466:0:99999:7:::\nbjk:$old$hash:20000:0:99999:7:::\n",
			wantErr:    false,
		},
		{
			name:       "user not found in new shadow",
			shadow:     "root:*:20466:0:99999:7:::\n",
			shadowLine: "bjk:$old$hash:20000:0:99999:7:::",
			username:   "bjk",
			want:       "",
			wantErr:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := &mockRunner{outputs: [][]byte{[]byte(tt.shadow)}}
			err := RestoreShadowEntry(r, "/fake/rootfs", tt.shadowLine)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			// Verify a sudo install command was issued targeting the shadow file
			if len(r.commands) == 0 {
				t.Fatal("expected sudo commands, got none")
			}
			allCmds := strings.Join(r.commands, "\n")
			if !strings.Contains(allCmds, "/fake/rootfs/etc/shadow") {
				t.Errorf("expected command targeting shadow file, got:\n%s", allCmds)
			}
		})
	}
}

func TestBackupDeviceBrokerState(t *testing.T) {
	rootfs := t.TempDir()
	brokerDir := filepath.Join(rootfs, "var", "lib", "microsoft-identity-device-broker")
	if err := os.MkdirAll(brokerDir, 0700); err != nil {
		t.Fatal(err)
	}

	r := &mockRunner{}
	tmpDir, err := BackupDeviceBrokerState(r, rootfs)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	if len(r.commands) != 1 {
		t.Fatalf("expected 1 command, got %d: %v", len(r.commands), r.commands)
	}
	cmd := r.commands[0]
	if !strings.Contains(cmd, "cp -a") {
		t.Errorf("expected 'cp -a' in command, got: %s", cmd)
	}
	if !strings.Contains(cmd, "microsoft-identity-device-broker") {
		t.Errorf("expected broker path in command, got: %s", cmd)
	}
}

func TestBackupDeviceBrokerStateNoBrokerDir(t *testing.T) {
	rootfs := t.TempDir()
	// No broker dir exists

	r := &mockRunner{}
	tmpDir, err := BackupDeviceBrokerState(r, rootfs)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tmpDir != "" {
		t.Errorf("expected empty tmpDir when broker dir missing, got %q", tmpDir)
	}
	if len(r.commands) != 0 {
		t.Errorf("expected no commands when broker dir missing, got: %v", r.commands)
	}
}

func TestBackupHimmelblauState(t *testing.T) {
	rootfs := t.TempDir()
	for _, relPath := range []string{"var/lib/himmelblaud", "var/cache/himmelblaud"} {
		if err := os.MkdirAll(filepath.Join(rootfs, relPath), 0700); err != nil {
			t.Fatal(err)
		}
	}

	r := &mockRunner{}
	backupDir, err := BackupHimmelblauState(r, rootfs)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer func() { _ = os.RemoveAll(backupDir) }()

	if len(r.commands) != 2 {
		t.Fatalf("expected two state-copy commands, got %d: %v", len(r.commands), r.commands)
	}
	commands := strings.Join(r.commands, "\n")
	for _, relPath := range []string{"var/lib/himmelblaud", "var/cache/himmelblaud"} {
		if !strings.Contains(commands, filepath.Join(rootfs, relPath)) {
			t.Errorf("backup commands missing %s: %s", relPath, commands)
		}
	}
}

func TestBackupHimmelblauStateUsesPrivatePathWhenPublicLinkIsAbsent(t *testing.T) {
	rootfs := t.TempDir()
	state := filepath.Join(rootfs, "var", "lib", "private", "himmelblaud")
	if err := os.MkdirAll(state, 0700); err != nil {
		t.Fatal(err)
	}

	r := &mockRunner{}
	backupDir, err := BackupHimmelblauState(r, rootfs)
	if err != nil {
		t.Fatalf("BackupHimmelblauState() error: %v", err)
	}
	defer func() { _ = os.RemoveAll(backupDir) }()
	if backupDir == "" {
		t.Fatal("expected private Himmelblau state to be backed up")
	}
	commands := strings.Join(r.commands, "\n")
	if !strings.Contains(commands, state+string(filepath.Separator)+".") {
		t.Fatalf("backup commands do not source the private state path: %s", commands)
	}
}

func TestBackupHimmelblauStateNoState(t *testing.T) {
	r := &mockRunner{}
	backupDir, err := BackupHimmelblauState(r, t.TempDir())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if backupDir != "" {
		t.Errorf("expected no backup dir, got %q", backupDir)
	}
	if len(r.commands) != 0 {
		t.Fatalf("expected no commands, got %v", r.commands)
	}
}

func TestBackupHimmelblauStateIncludesSystemdCredentialKey(t *testing.T) {
	rootfs := t.TempDir()
	writeTestMachineID(t, rootfs)
	hsmDir := filepath.Join(rootfs, "var", "lib", "himmelblaud")
	if err := os.MkdirAll(hsmDir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(hsmDir, "hsm-pin-nopcr.enc"), []byte("encrypted"), 0600); err != nil {
		t.Fatal(err)
	}
	key := filepath.Join(rootfs, "var", "lib", "systemd", "credential.secret")
	if err := os.MkdirAll(filepath.Dir(key), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(key, []byte("test key"), 0600); err != nil {
		t.Fatal(err)
	}

	r := &backupCopyRunner{}
	backupDir, err := BackupHimmelblauState(r, rootfs)
	if err != nil {
		t.Fatalf("BackupHimmelblauState() error: %v", err)
	}
	defer func() { _ = os.RemoveAll(backupDir) }()
	if backupDir == "" {
		t.Fatal("expected Himmelblau credential state to be backed up")
	}
	commands := strings.Join(r.commands, "\n")
	for _, relPath := range []string{"var/lib/himmelblaud", "var/lib/systemd/credential.secret", "etc/machine-id"} {
		if !strings.Contains(commands, filepath.Join(rootfs, relPath)) || !strings.Contains(commands, filepath.Join(backupDir, relPath)) {
			t.Fatalf("backup commands do not preserve %s: %v", relPath, r.commands)
		}
	}
	backupKey := filepath.Join(backupDir, "var", "lib", "systemd", "credential.secret")
	if _, err := os.Stat(backupKey); err != nil {
		t.Fatalf("backup is missing systemd credential key: %v", err)
	}
}

func TestBackupHimmelblauStateRejectsMissingCredentialKeyCopy(t *testing.T) {
	rootfs := t.TempDir()
	writeTestMachineID(t, rootfs)
	key := filepath.Join(rootfs, "var", "lib", "systemd", "credential.secret")
	if err := os.MkdirAll(filepath.Dir(key), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(key, []byte("fixture key"), 0400); err != nil {
		t.Fatal(err)
	}

	r := &backupCopyRunner{skipCopyFrom: key}
	backupDir, err := BackupHimmelblauState(r, rootfs)
	if backupDir != "" {
		_ = os.RemoveAll(backupDir)
	}
	if err == nil || !strings.Contains(err.Error(), "verify backup systemd credential key") {
		t.Fatalf("BackupHimmelblauState() error = %v, want missing credential key backup error", err)
	}
}

func TestBackupHimmelblauStatePrivilegedProbe(t *testing.T) {
	rootfs := os.Getenv("INTUNEME_BACKUP_PROBE_ROOTFS")
	if rootfs == "" {
		t.Skip("set INTUNEME_BACKUP_PROBE_ROOTFS to run the privileged backup probe")
	}
	rootfs, err := filepath.Abs(rootfs)
	if err != nil {
		t.Fatalf("resolve rootfs path: %v", err)
	}
	info, err := os.Stat(rootfs)
	if err != nil {
		t.Fatalf("inspect rootfs path: %v", err)
	}
	if !info.IsDir() {
		t.Fatalf("rootfs path is not a directory: %s", rootfs)
	}

	r := &runner.SystemRunner{}
	backupDir, err := BackupHimmelblauState(r, rootfs)
	if err != nil {
		t.Fatalf("BackupHimmelblauState() error: %v", err)
	}
	if backupDir == "" {
		t.Fatal("BackupHimmelblauState() found no state to back up")
	}
	defer func() {
		if err := RemoveHimmelblauStateBackup(r, backupDir); err != nil {
			t.Errorf("clean temporary backup: %v", err)
		}
	}()

	keyPath := filepath.Join(backupDir, systemdCredentialKeyPath)
	keyInfo, err := os.Lstat(keyPath)
	if err != nil {
		t.Fatalf("staged backup does not contain credential.secret: %v", err)
	}
	if !keyInfo.Mode().IsRegular() {
		t.Fatal("staged credential.secret is not a regular file")
	}
	t.Log("staged credential.secret is present; contents were not read")
}

func TestRemoveHimmelblauStateBackupUsesPrivilegeForProtectedState(t *testing.T) {
	backupDir, err := os.MkdirTemp("", "intuneme-himmelblau-backup-test-")
	if err != nil {
		t.Fatal(err)
	}
	protectedDir := filepath.Join(backupDir, "var", "cache", "policies")
	if err := os.MkdirAll(protectedDir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(protectedDir, "state"), []byte("cached state"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(protectedDir, 0600); err != nil {
		t.Fatal(err)
	}
	defer func() {
		_ = os.Chmod(protectedDir, 0700)
		_ = os.RemoveAll(backupDir)
	}()

	r := &backupCleanupRunner{protectedDir: protectedDir}
	if err := RemoveHimmelblauStateBackup(r, backupDir); err != nil {
		t.Fatalf("RemoveHimmelblauStateBackup() error: %v", err)
	}
	if len(r.commands) != 1 || r.commands[0] != "sudo rm -rf -- "+backupDir {
		t.Fatalf("cleanup commands = %v, want privileged removal of the validated backup directory", r.commands)
	}
	if _, err := os.Lstat(backupDir); !os.IsNotExist(err) {
		t.Fatalf("backup directory still exists after cleanup: err=%v", err)
	}
}

func TestRemoveHimmelblauStateBackupRejectsUnexpectedPath(t *testing.T) {
	path := t.TempDir()
	r := &mockRunner{}
	if err := RemoveHimmelblauStateBackup(r, path); err == nil {
		t.Fatal("RemoveHimmelblauStateBackup() accepted an arbitrary path")
	}
	if len(r.commands) != 0 {
		t.Fatalf("cleanup commands = %v, want no command for an unexpected path", r.commands)
	}
}

func TestBackupHimmelblauStateWithProtectedDynamicUserState(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("requires an unprivileged process to reproduce protected DynamicUser state")
	}

	rootfs := t.TempDir()
	writeTestMachineID(t, rootfs)
	privateDir := filepath.Join(rootfs, "var", "lib", "private", "himmelblaud")
	if err := os.MkdirAll(privateDir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(privateDir, "hsm-pin-nopcr.enc"), []byte("encrypted"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(privateDir, 0000); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := os.Chmod(privateDir, 0700); err != nil {
			t.Errorf("restore private state directory permissions: %v", err)
		}
	}()
	publicDir := filepath.Join(rootfs, "var", "lib", "himmelblaud")
	if err := os.Symlink("private/himmelblaud", publicDir); err != nil {
		t.Fatal(err)
	}
	key := filepath.Join(rootfs, systemdCredentialKeyPath)
	if err := os.MkdirAll(filepath.Dir(key), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(key, []byte("test key"), 0600); err != nil {
		t.Fatal(err)
	}

	r := &backupCopyRunner{
		mockRunner:     mockRunner{outputs: [][]byte{[]byte("present\n")}},
		protectedPaths: []string{privateDir},
	}
	backupDir, err := BackupHimmelblauState(r, rootfs)
	if err != nil {
		t.Fatalf("BackupHimmelblauState() error: %v", err)
	}
	defer func() { _ = os.RemoveAll(backupDir) }()
	if backupDir == "" {
		t.Fatal("expected Himmelblau credential state to be backed up")
	}
	if len(r.commands) == 0 || !strings.Contains(r.commands[0], "sudo sh -c") ||
		!strings.Contains(r.commands[0], filepath.Join(rootfs, himmelblauEncryptedPINPath)) {
		t.Fatalf("backup commands did not inspect the protected PIN with privilege: %v", r.commands)
	}
}

func TestBackupHimmelblauStateRejectsProtectedPINWithoutCredentialKey(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("requires an unprivileged process to reproduce protected DynamicUser state")
	}

	rootfs := t.TempDir()
	privateDir := filepath.Join(rootfs, "var", "lib", "private", "himmelblaud")
	if err := os.MkdirAll(privateDir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(privateDir, "hsm-pin-nopcr.enc"), []byte("encrypted"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(privateDir, 0000); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := os.Chmod(privateDir, 0700); err != nil {
			t.Errorf("restore private state directory permissions: %v", err)
		}
	}()

	r := &mockRunner{outputs: [][]byte{[]byte("present\n")}}
	backupDir, err := BackupHimmelblauState(r, rootfs)
	if backupDir != "" {
		_ = os.RemoveAll(backupDir)
	}
	if err == nil || !strings.Contains(err.Error(), "credential.secret") {
		t.Fatalf("BackupHimmelblauState() error = %v, want missing systemd credential key error", err)
	}
	if len(r.commands) != 1 || !strings.Contains(r.commands[0], "sudo sh -c") {
		t.Fatalf("backup commands = %v, want only the privileged PIN-existence check", r.commands)
	}
}

func TestBackupHimmelblauStateRejectsEncryptedPINWithoutCredentialKey(t *testing.T) {
	rootfs := t.TempDir()
	hsmDir := filepath.Join(rootfs, "var", "lib", "himmelblaud")
	if err := os.MkdirAll(hsmDir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(hsmDir, "hsm-pin-nopcr.enc"), []byte("encrypted"), 0600); err != nil {
		t.Fatal(err)
	}
	r := &mockRunner{}

	backupDir, err := BackupHimmelblauState(r, rootfs)
	if backupDir != "" {
		_ = os.RemoveAll(backupDir)
	}
	if err == nil || !strings.Contains(err.Error(), "credential.secret") {
		t.Fatalf("BackupHimmelblauState() error = %v, want missing systemd credential key error", err)
	}
	if len(r.commands) != 0 {
		t.Fatalf("backup commands = %v, want preflight to fail before copying state", r.commands)
	}
}

func TestBackupHimmelblauStateRejectsPrivateEncryptedPINWithoutCredentialKey(t *testing.T) {
	rootfs := t.TempDir()
	hsmDir := filepath.Join(rootfs, "var", "lib", "private", "himmelblaud")
	if err := os.MkdirAll(hsmDir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(hsmDir, "hsm-pin-nopcr.enc"), []byte("encrypted"), 0600); err != nil {
		t.Fatal(err)
	}

	backupDir, err := BackupHimmelblauState(&mockRunner{}, rootfs)
	if backupDir != "" {
		_ = os.RemoveAll(backupDir)
	}
	if err == nil || !strings.Contains(err.Error(), "credential.secret") {
		t.Fatalf("BackupHimmelblauState() error = %v, want missing systemd credential key error", err)
	}
}

func TestRestoreHimmelblauState(t *testing.T) {
	rootfs := t.TempDir()
	backupDir := t.TempDir()
	writeTestMachineID(t, backupDir)
	for _, relPath := range []string{"var/lib/himmelblaud", "var/cache/himmelblaud"} {
		if err := os.MkdirAll(filepath.Join(backupDir, relPath), 0700); err != nil {
			t.Fatal(err)
		}
	}
	credentialKey := filepath.Join(backupDir, "var", "lib", "systemd", "credential.secret")
	if err := os.MkdirAll(filepath.Dir(credentialKey), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(credentialKey, []byte("test key"), 0600); err != nil {
		t.Fatal(err)
	}

	r := &restoreCredentialKeyRunner{copyCredentialKey: true}
	if err := RestoreHimmelblauState(r, rootfs, backupDir); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	commands := strings.Join(r.commands, "\n")
	for _, paths := range []struct {
		backup  string
		restore string
	}{
		{backup: "var/lib/himmelblaud", restore: "var/lib/private/himmelblaud"},
		{backup: "var/cache/himmelblaud", restore: "var/cache/private/himmelblaud"},
	} {
		if !strings.Contains(commands, filepath.Join(rootfs, paths.restore)) {
			t.Errorf("restore commands missing systemd private state dir %s: %s", filepath.Join(rootfs, paths.restore), commands)
		}
		if !strings.Contains(commands, filepath.Join(backupDir, paths.backup)) {
			t.Errorf("restore commands missing backup %s: %s", filepath.Join(backupDir, paths.backup), commands)
		}
	}
	if !strings.Contains(commands, filepath.Join(rootfs, "var/lib/systemd/credential.secret")) ||
		!strings.Contains(commands, credentialKey) {
		t.Errorf("restore commands missing systemd credential key: %s", commands)
	}
	if !strings.Contains(commands, "cmp -s") {
		t.Errorf("restore commands missing credential key content verification: %s", commands)
	}
	restoredKey := filepath.Join(rootfs, systemdCredentialKeyPath)
	got, err := os.ReadFile(restoredKey)
	if err != nil || string(got) != "test key" {
		t.Fatalf("restored credential key = %q, %v, want exact backup contents", got, err)
	}
}

func TestRestoreHimmelblauStateRejectsMissingRestoredCredentialKey(t *testing.T) {
	rootfs := t.TempDir()
	backupDir := t.TempDir()
	writeTestMachineID(t, backupDir)
	credentialKey := filepath.Join(backupDir, systemdCredentialKeyPath)
	if err := os.MkdirAll(filepath.Dir(credentialKey), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(credentialKey, []byte("test key"), 0600); err != nil {
		t.Fatal(err)
	}

	err := RestoreHimmelblauState(&restoreCredentialKeyRunner{}, rootfs, backupDir)
	if err == nil || !strings.Contains(err.Error(), "verify restored systemd credential key") {
		t.Fatalf("RestoreHimmelblauState() error = %v, want missing destination-key verification error", err)
	}
}

func TestRestoreHimmelblauStateRejectsMismatchedRestoredCredentialKey(t *testing.T) {
	rootfs := t.TempDir()
	backupDir := t.TempDir()
	writeTestMachineID(t, backupDir)
	credentialKey := filepath.Join(backupDir, systemdCredentialKeyPath)
	if err := os.MkdirAll(filepath.Dir(credentialKey), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(credentialKey, []byte("test key"), 0600); err != nil {
		t.Fatal(err)
	}

	err := RestoreHimmelblauState(&restoreCredentialKeyRunner{
		copyCredentialKey:    true,
		corruptCredentialKey: true,
	}, rootfs, backupDir)
	if err == nil || !strings.Contains(err.Error(), "verify restored systemd credential key contents") {
		t.Fatalf("RestoreHimmelblauState() error = %v, want key-content verification error", err)
	}
}

func TestRestoreHimmelblauStateRejectsPINWithoutCredentialKey(t *testing.T) {
	rootfs := t.TempDir()
	backupDir := t.TempDir()
	pin := filepath.Join(backupDir, himmelblauEncryptedPINPath)
	if err := os.MkdirAll(filepath.Dir(pin), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(pin, []byte("encrypted"), 0600); err != nil {
		t.Fatal(err)
	}

	r := &mockRunner{}
	err := RestoreHimmelblauState(r, rootfs, backupDir)
	if err == nil || !strings.Contains(err.Error(), "credential.secret") {
		t.Fatalf("RestoreHimmelblauState() error = %v, want missing systemd credential key error", err)
	}
	if len(r.commands) != 0 {
		t.Fatalf("restore commands = %v, want validation to fail before restoring any state", r.commands)
	}
}

func TestRestoreDeviceBrokerState(t *testing.T) {
	rootfs := t.TempDir()
	backupDir := t.TempDir()

	r := &mockRunner{}
	err := RestoreDeviceBrokerState(r, rootfs, backupDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(r.commands) != 1 {
		t.Fatalf("expected 1 command, got %d: %v", len(r.commands), r.commands)
	}
	cmd := r.commands[0]
	if !strings.Contains(cmd, "cp -a") {
		t.Errorf("expected 'cp -a' in command, got: %s", cmd)
	}
	if !strings.Contains(cmd, "microsoft-identity-device-broker") {
		t.Errorf("expected broker dest path in command, got: %s", cmd)
	}
}
