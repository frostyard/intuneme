# Recreate Container Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Add `intuneme recreate` command that upgrades the container image while preserving Intune enrollment state.

**Architecture:** New backup/restore functions in `internal/provision/` handle shadow hash and device broker DB preservation. A new `cmd/recreate.go` orchestrates: stop container, backup state, destroy rootfs, pull new image, re-provision, restore state, install polkit.

**Tech Stack:** Go stdlib (os, strings, path/filepath, fmt), existing runner/provision/nspawn/puller/config packages.

---

### Task 1: BackupShadowEntry — failing test

**Files:**
- Create: `internal/provision/backup_test.go`

**Step 1: Write the failing test**

```go
package provision

import (
	"os"
	"path/filepath"
	"testing"
)

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
			rootfs := t.TempDir()
			shadowDir := filepath.Join(rootfs, "etc")
			if err := os.MkdirAll(shadowDir, 0755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(shadowDir, "shadow"), []byte(tt.shadow), 0640); err != nil {
				t.Fatal(err)
			}

			got, err := BackupShadowEntry(rootfs, tt.username)
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
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/provision/ -run TestBackupShadowEntry -v`
Expected: FAIL — `BackupShadowEntry` not defined

---

### Task 2: BackupShadowEntry — implementation

**Files:**
- Create: `internal/provision/backup.go`

**Step 3: Write minimal implementation**

```go
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
		if len(fields) >= 1 && fields[0] == username {
			return line, nil
		}
	}
	return "", fmt.Errorf("user %q not found in shadow file", username)
}
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/provision/ -run TestBackupShadowEntry -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/provision/backup.go internal/provision/backup_test.go
git commit -m "feat: add BackupShadowEntry for recreate command"
```

---

### Task 3: RestoreShadowEntry — failing test

**Files:**
- Modify: `internal/provision/backup_test.go`

**Step 1: Write the failing test**

Add to `backup_test.go`:

```go
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
			rootfs := t.TempDir()
			shadowDir := filepath.Join(rootfs, "etc")
			if err := os.MkdirAll(shadowDir, 0755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(shadowDir, "shadow"), []byte(tt.shadow), 0640); err != nil {
				t.Fatal(err)
			}

			r := &mockRunner{}
			err := RestoreShadowEntry(r, rootfs, tt.shadowLine)
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
			if !strings.Contains(allCmds, filepath.Join(rootfs, "etc", "shadow")) {
				t.Errorf("expected command targeting shadow file, got:\n%s", allCmds)
			}
		})
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/provision/ -run TestRestoreShadowEntry -v`
Expected: FAIL — `RestoreShadowEntry` not defined

---

### Task 4: RestoreShadowEntry — implementation

**Files:**
- Modify: `internal/provision/backup.go`

**Step 3: Write minimal implementation**

Add to `backup.go`:

```go
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
		if len(fields) >= 1 && fields[0] == username {
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
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/provision/ -run TestRestoreShadowEntry -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/provision/backup.go internal/provision/backup_test.go
git commit -m "feat: add RestoreShadowEntry for recreate command"
```

---

### Task 5: BackupDeviceBrokerState / RestoreDeviceBrokerState — tests

**Files:**
- Modify: `internal/provision/backup_test.go`

**Step 1: Write the failing tests**

Add to `backup_test.go`:

```go
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
	defer os.RemoveAll(tmpDir)

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
```

**Step 2: Run test to verify they fail**

Run: `go test ./internal/provision/ -run "TestBackupDeviceBroker|TestRestoreDeviceBroker" -v`
Expected: FAIL — functions not defined

---

### Task 6: BackupDeviceBrokerState / RestoreDeviceBrokerState — implementation

**Files:**
- Modify: `internal/provision/backup.go`

**Step 3: Write minimal implementation**

Add to `backup.go`:

```go
const deviceBrokerRelPath = "var/lib/microsoft-identity-device-broker"

// BackupDeviceBrokerState copies the device broker state directory from the
// rootfs to a temporary directory. Returns the temp directory path, or ""
// if the broker directory doesn't exist (no enrollment to preserve).
// The caller is responsible for cleaning up the temp directory.
func BackupDeviceBrokerState(r runner.Runner, rootfs string) (string, error) {
	brokerDir := filepath.Join(rootfs, deviceBrokerRelPath)
	if _, err := os.Stat(brokerDir); os.IsNotExist(err) {
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
```

**Step 4: Run test to verify they pass**

Run: `go test ./internal/provision/ -run "TestBackupDeviceBroker|TestRestoreDeviceBroker" -v`
Expected: PASS

**Step 5: Run all provision tests**

Run: `go test ./internal/provision/ -v`
Expected: All PASS

**Step 6: Commit**

```bash
git add internal/provision/backup.go internal/provision/backup_test.go
git commit -m "feat: add device broker state backup/restore for recreate"
```

---

### Task 7: recreate command

**Files:**
- Create: `cmd/recreate.go`

**Step 1: Write the command**

```go
package cmd

import (
	"fmt"
	"os"
	"os/user"
	"path/filepath"

	"github.com/frostyard/intuneme/internal/broker"
	"github.com/frostyard/intuneme/internal/config"
	"github.com/frostyard/intuneme/internal/nspawn"
	"github.com/frostyard/intuneme/internal/provision"
	"github.com/frostyard/intuneme/internal/puller"
	"github.com/frostyard/intuneme/internal/runner"
	pkgversion "github.com/frostyard/intuneme/internal/version"
	"github.com/spf13/cobra"
)

var recreateCmd = &cobra.Command{
	Use:   "recreate",
	Short: "Recreate the container with a fresh image, preserving enrollment state",
	RunE: func(cmd *cobra.Command, args []string) error {
		r := &runner.SystemRunner{}
		root := rootDir
		if root == "" {
			root = config.DefaultRoot()
		}

		cfg, err := config.Load(root)
		if err != nil {
			return err
		}

		// Verify initialized
		if _, err := os.Stat(cfg.RootfsPath); err != nil {
			return fmt.Errorf("not initialized — run 'intuneme init' first")
		}

		u, err := user.Current()
		if err != nil {
			return fmt.Errorf("get current user: %w", err)
		}

		// Validate sudo early
		fmt.Println("Checking sudo credentials...")
		if err := nspawn.ValidateSudo(r); err != nil {
			return fmt.Errorf("sudo authentication failed: %w", err)
		}

		// Stop container if running
		if nspawn.IsRunning(r, cfg.MachineName) {
			if cfg.BrokerProxy {
				pidPath := filepath.Join(root, "broker-proxy.pid")
				broker.StopByPIDFile(pidPath)
				fmt.Println("Broker proxy stopped.")
			}
			fmt.Println("Stopping container...")
			if err := nspawn.Stop(r, cfg.MachineName); err != nil {
				return fmt.Errorf("failed to stop container: %w", err)
			}
			fmt.Println("Container stopped.")
		}

		// Backup state
		fmt.Println("Backing up shadow entry...")
		shadowLine, err := provision.BackupShadowEntry(cfg.RootfsPath, u.Username)
		if err != nil {
			return fmt.Errorf("backup shadow entry: %w", err)
		}

		fmt.Println("Backing up device broker state...")
		brokerBackupDir, err := provision.BackupDeviceBrokerState(r, cfg.RootfsPath)
		if err != nil {
			return fmt.Errorf("backup device broker state: %w", err)
		}
		if brokerBackupDir != "" {
			defer os.RemoveAll(brokerBackupDir)
			fmt.Println("Device broker state backed up.")
		} else {
			fmt.Println("No device broker state found (skipping).")
		}

		// Remove old rootfs
		fmt.Printf("Removing old rootfs at %s...\n", cfg.RootfsPath)
		out, err := r.Run("sudo", "rm", "-rf", cfg.RootfsPath)
		if err != nil {
			return fmt.Errorf("rm rootfs failed: %w\n%s", err, out)
		}

		// Pull new image
		image := pkgversion.ImageRef()
		p, err := puller.Detect(r)
		if err != nil {
			return err
		}

		fmt.Printf("Pulling and extracting OCI image %s (via %s)...\n", image, p.Name())
		if err := os.MkdirAll(cfg.RootfsPath, 0755); err != nil {
			return fmt.Errorf("create rootfs dir: %w", err)
		}
		if err := p.PullAndExtract(r, image, cfg.RootfsPath); err != nil {
			return err
		}

		// Re-provision
		hostname, _ := os.Hostname()

		fmt.Println("Creating container user...")
		if err := provision.CreateContainerUser(r, cfg.RootfsPath, u.Username, os.Getuid(), os.Getgid()); err != nil {
			return err
		}

		fmt.Println("Applying fixups...")
		if err := provision.WriteFixups(r, cfg.RootfsPath, u.Username, os.Getuid(), os.Getgid(), hostname+"LXC"); err != nil {
			return err
		}

		// Restore state
		fmt.Println("Restoring shadow entry...")
		if err := provision.RestoreShadowEntry(r, cfg.RootfsPath, shadowLine); err != nil {
			return fmt.Errorf("restore shadow entry: %w", err)
		}

		if brokerBackupDir != "" {
			fmt.Println("Restoring device broker state...")
			if err := provision.RestoreDeviceBrokerState(r, cfg.RootfsPath, brokerBackupDir); err != nil {
				fmt.Fprintf(os.Stderr, "warning: restore device broker state failed: %v\n", err)
			}
		}

		// Install polkit rules
		fmt.Println("Installing polkit rules...")
		if err := provision.InstallPolkitRule(r, "/etc/polkit-1/rules.d"); err != nil {
			fmt.Fprintf(os.Stderr, "warning: polkit install failed: %v\n", err)
		}

		fmt.Println("Container recreated. Run 'intuneme start' to boot.")
		return nil
	},
}

func init() {
	rootCmd.AddCommand(recreateCmd)
}
```

**Step 2: Build to verify compilation**

Run: `go build ./...`
Expected: No errors

**Step 3: Commit**

```bash
git add cmd/recreate.go
git commit -m "feat: add intuneme recreate command"
```

---

### Task 8: Lint and final verification

**Step 1: Run formatter and linter**

Run: `make fmt && make lint`
Expected: No errors

**Step 2: Run all tests**

Run: `go test ./...`
Expected: All PASS

**Step 3: Fix any issues found, then commit if needed**

If lint or tests fail, fix and commit with:
```bash
git commit -m "fix: address lint findings in recreate"
```
