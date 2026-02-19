package provision

import (
	"os"
	"path/filepath"
	"strings"
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
