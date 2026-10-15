package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestValidatePassword(t *testing.T) {
	tests := []struct {
		name     string
		password string
		wantErr  bool
		errMsg   string
	}{
		{
			name:     "strong password",
			password: "Correct3Horse!",
		},
		{
			name:     "short password is accepted",
			password: "x",
		},
		{
			name:    "empty password rejected",
			wantErr: true,
			errMsg:  "password must not be empty",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validatePassword(tt.password)
			if tt.wantErr {
				if err == nil || !contains(err.Error(), tt.errMsg) {
					t.Fatalf("expected error containing %q, got: %v", tt.errMsg, err)
				}
			} else if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

// contains is a helper to check substring membership.
func contains(s, substr string) bool {
	return strings.Contains(s, substr)
}

func TestReadPasswordFromFile(t *testing.T) {
	tests := []struct {
		name     string
		content  string
		wantPass string
		wantErr  bool
		errMsg   string
	}{
		{
			name:     "weak password from file is accepted",
			content:  "weak\n",
			wantPass: "weak",
		},
		{
			name:     "trims trailing newline",
			content:  "weak\n\n",
			wantPass: "weak",
		},
		{
			name:     "uses only first line",
			content:  "weak\nignored line",
			wantPass: "weak",
		},
		{
			name:    "empty password rejected",
			content: "\n",
			wantErr: true,
			errMsg:  "password must not be empty",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f, err := os.CreateTemp(t.TempDir(), "pwfile-*")
			if err != nil {
				t.Fatal(err)
			}
			if _, err := f.WriteString(tt.content); err != nil {
				t.Fatal(err)
			}
			if err := f.Close(); err != nil {
				t.Fatal(err)
			}

			got, err := readPassword(f.Name())
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error, got nil")
				}
				if tt.errMsg != "" && !contains(err.Error(), tt.errMsg) {
					t.Errorf("expected error containing %q, got: %v", tt.errMsg, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.wantPass {
				t.Errorf("got %q, want %q", got, tt.wantPass)
			}
		})
	}
}

func TestReadPasswordFileNotFound(t *testing.T) {
	_, err := readPassword("/nonexistent/path/to/pwfile")
	if err == nil {
		t.Fatal("expected error for missing file, got nil")
	}
}

func TestResolveTerminalPasskeyImage(t *testing.T) {
	const sourceCommit = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	markerPath := filepath.Join("usr", "share", "intuneme", "himmelblau-terminal-passkey-source")

	t.Run("standard image remains standard", func(t *testing.T) {
		got, source, err := resolveTerminalPasskeyImage(t.TempDir(), "", false)
		if err != nil || got || source != "" {
			t.Fatalf("resolveTerminalPasskeyImage() = (%v, %q, %v), want standard image", got, source, err)
		}
	})

	t.Run("marked local image is detected without the extra selector", func(t *testing.T) {
		rootfs := t.TempDir()
		writeTerminalPasskeyMarker(t, rootfs, markerPath, sourceCommit)
		got, resolvedSource, err := resolveTerminalPasskeyImage(rootfs, "localhost/custom-himmelblau:dev", false)
		if err != nil || !got || resolvedSource != sourceCommit {
			t.Fatalf("resolveTerminalPasskeyImage() = (%v, %q, %v), want detected local image", got, resolvedSource, err)
		}
	})

	t.Run("explicit terminal-passkey mode accepts a marked local image", func(t *testing.T) {
		rootfs := t.TempDir()
		writeTerminalPasskeyMarker(t, rootfs, markerPath, sourceCommit)
		got, resolvedSource, err := resolveTerminalPasskeyImage(rootfs, "localhost/custom-himmelblau:dev", true)
		if err != nil || !got || resolvedSource != sourceCommit {
			t.Fatalf("resolveTerminalPasskeyImage() = (%v, %q, %v), want marked local image", got, resolvedSource, err)
		}
	})

	t.Run("explicit terminal-passkey mode requires a local image", func(t *testing.T) {
		if _, _, err := resolveTerminalPasskeyImage(t.TempDir(), "", true); err == nil || !strings.Contains(err.Error(), "--himmelblau-local-image") {
			t.Fatalf("resolveTerminalPasskeyImage() error = %v, want local-image requirement", err)
		}
	})

	t.Run("missing marker is rejected when explicitly requested", func(t *testing.T) {
		if _, _, err := resolveTerminalPasskeyImage(t.TempDir(), "localhost/custom-himmelblau:dev", true); err == nil || !strings.Contains(err.Error(), "missing its fork source marker") {
			t.Fatalf("resolveTerminalPasskeyImage() error = %v, want missing marker error", err)
		}
	})
}

func TestRequireLocalTerminalPasskeyImage(t *testing.T) {
	tests := []struct {
		name    string
		enabled bool
		image   string
		wantErr bool
	}{
		{name: "standard image needs no local override"},
		{name: "terminal-passkey requires local image", enabled: true, wantErr: true},
		{name: "terminal-passkey accepts local image", enabled: true, image: "localhost/ubuntu-himmelblau-terminal-passkey:dev"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := requireLocalTerminalPasskeyImage(tt.enabled, tt.image)
			if (err != nil) != tt.wantErr {
				t.Fatalf("requireLocalTerminalPasskeyImage() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func writeTerminalPasskeyMarker(t *testing.T, rootfs, relativePath, source string) {
	t.Helper()
	path := filepath.Join(rootfs, relativePath)
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(source+"\n"), 0644); err != nil {
		t.Fatal(err)
	}
}

func FuzzValidatePassword(f *testing.F) {
	f.Add("Correct3Horse!")
	f.Add("short")
	f.Add("")

	f.Fuzz(func(t *testing.T, password string) {
		// validatePassword must never panic regardless of input.
		_ = validatePassword(password)
	})
}
