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
