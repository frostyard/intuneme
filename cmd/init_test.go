package cmd

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/frostyard/intuneme/internal/config"
	"github.com/frostyard/intuneme/internal/runner"
)

func TestValidatePassword(t *testing.T) {
	tests := []struct {
		name     string
		username string
		password string
		wantErr  bool
		errMsgs  []string
	}{
		{
			name:     "valid password",
			username: "alice",
			password: "Correct3Horse!",
			wantErr:  false,
		},
		{
			name:     "too short",
			username: "alice",
			password: "Short1!A",
			wantErr:  true,
			errMsgs:  []string{"at least 12 characters"},
		},
		{
			name:     "exactly 12 chars valid",
			username: "alice",
			password: "Aa1!Aa1!Aa1!",
			wantErr:  false,
		},
		{
			name:     "missing digit",
			username: "alice",
			password: "NoDigitsHere!A",
			wantErr:  true,
			errMsgs:  []string{"at least one digit"},
		},
		{
			name:     "missing uppercase",
			username: "alice",
			password: "nouppercase1!aa",
			wantErr:  true,
			errMsgs:  []string{"at least one uppercase"},
		},
		{
			name:     "missing lowercase",
			username: "alice",
			password: "NOLOWERCASE1!AA",
			wantErr:  true,
			errMsgs:  []string{"at least one lowercase"},
		},
		{
			name:     "missing special character",
			username: "alice",
			password: "NoSpecialChar1A",
			wantErr:  true,
			errMsgs:  []string{"at least one special character"},
		},
		{
			name:     "contains username (exact)",
			username: "alice",
			password: "alice-Passw0rd!",
			wantErr:  true,
			errMsgs:  []string{"must not contain your username"},
		},
		{
			name:     "contains username (case insensitive)",
			username: "alice",
			password: "ALICE-Passw0rd!",
			wantErr:  true,
			errMsgs:  []string{"must not contain your username"},
		},
		{
			name:     "multiple failures reported together",
			username: "alice",
			password: "short",
			wantErr:  true,
			errMsgs:  []string{"at least 12 characters", "at least one digit", "at least one uppercase"},
		},
		{
			name:     "empty username skips usercheck",
			username: "",
			password: "Correct3Horse!",
			wantErr:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validatePassword(tt.username, tt.password)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error, got nil")
				}
				for _, msg := range tt.errMsgs {
					if !contains(err.Error(), msg) {
						t.Errorf("expected error containing %q, got: %v", msg, err)
					}
				}
			} else {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
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
		username string
		wantPass string
		wantErr  bool
		errMsg   string
	}{
		{
			name:     "valid password from file",
			content:  "Correct3Horse!\n",
			username: "alice",
			wantPass: "Correct3Horse!",
			wantErr:  false,
		},
		{
			name:     "trims trailing newline",
			content:  "Correct3Horse!\n\n",
			username: "alice",
			wantPass: "Correct3Horse!",
			wantErr:  false,
		},
		{
			name:     "uses only first line",
			content:  "Correct3Horse!\nignored line",
			username: "alice",
			wantPass: "Correct3Horse!",
			wantErr:  false,
		},
		{
			name:     "invalid password rejected",
			content:  "weak\n",
			username: "alice",
			wantErr:  true,
			errMsg:   "password requirements not met",
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

			got, err := readPassword(tt.username, f.Name())
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
	_, err := readPassword("alice", "/nonexistent/path/to/pwfile")
	if err == nil {
		t.Fatal("expected error for missing file, got nil")
	}
}

func FuzzValidatePassword(f *testing.F) {
	f.Add("alice", "Correct3Horse!")
	f.Add("alice", "short")
	f.Add("", "Correct3Horse!")
	f.Add("alice", "alice-Passw0rd!")
	f.Add("alice", "ALICE-Passw0rd!")
	f.Add("alice", "Aa1!Aa1!Aa1!")
	f.Add("alice", "")
	f.Add("", "")

	f.Fuzz(func(t *testing.T, username, password string) {
		// validatePassword must never panic regardless of input.
		_ = validatePassword(username, password)
	})
}

// initTestReporter captures Message and Warning calls so a test can prove that
// a failed init never reports success.
type initTestReporter struct {
	messages []string
	warnings []string
}

func (r *initTestReporter) Step(int, int, string)       {}
func (r *initTestReporter) Progress(int, string)        {}
func (r *initTestReporter) MessagePlain(string, ...any) {}
func (r *initTestReporter) Error(error, string)         {}
func (r *initTestReporter) Complete(string, any)        {}
func (r *initTestReporter) IsJSON() bool                { return false }
func (r *initTestReporter) Message(format string, args ...any) {
	r.messages = append(r.messages, fmt.Sprintf(format, args...))
}
func (r *initTestReporter) Warning(format string, args ...any) {
	r.warnings = append(r.warnings, fmt.Sprintf(format, args...))
}

// useInitTestReporter swaps the package reporter for a recorder.
func useInitTestReporter(t *testing.T) *initTestReporter {
	t.Helper()
	rec := &initTestReporter{}
	origRep := rep
	rep = rec
	t.Cleanup(func() { rep = origRep })
	return rec
}

func TestFinishInitSELinuxFailureStopsBeforeSave(t *testing.T) {
	rec := useInitTestReporter(t)

	root := t.TempDir()
	cfg := &config.Config{MachineName: "intuneme", RootfsPath: filepath.Join(root, "rootfs")}
	installErr := errors.New("exit status 1: sudo: semanage: command not found")
	deps := initCompletionDeps{
		SELinuxEnabled: func() bool { return true },
		InstallSELinuxPolicy: func(runner.Runner, string) error {
			return installErr
		},
	}

	err := finishInit(&runner.SystemRunner{}, cfg, root, "alice", 1000, deps)
	if err == nil {
		t.Fatal("finishInit returned nil after a SELinux policy failure")
	}
	if !errors.Is(err, installErr) {
		t.Errorf("error %v does not wrap the installer error", err)
	}
	for _, want := range []string{"policycoreutils-python-utils", "intuneme init --force"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not mention %q", err, want)
		}
	}

	if _, statErr := os.Stat(filepath.Join(root, "config.toml")); !os.IsNotExist(statErr) {
		t.Errorf("config.toml exists after a SELinux failure (stat error: %v)", statErr)
	}
	for _, msg := range rec.messages {
		if strings.Contains(msg, "Initialized intuneme") {
			t.Errorf("success message reported after a SELinux failure: %q", msg)
		}
	}
}

func TestFinishInitSuccessWritesConfig(t *testing.T) {
	rec := useInitTestReporter(t)

	root := t.TempDir()
	cfg := &config.Config{MachineName: "intuneme", RootfsPath: filepath.Join(root, "rootfs")}
	deps := initCompletionDeps{
		SELinuxEnabled:       func() bool { return true },
		InstallSELinuxPolicy: func(runner.Runner, string) error { return nil },
	}

	if err := finishInit(&runner.SystemRunner{}, cfg, root, "alice", 1000, deps); err != nil {
		t.Fatalf("finishInit returned an error: %v", err)
	}

	if _, err := os.Stat(filepath.Join(root, "config.toml")); err != nil {
		t.Errorf("config.toml was not written: %v", err)
	}
	if cfg.HostUser != "alice" || cfg.HostUID != 1000 {
		t.Errorf("config host fields = %q/%d, want alice/1000", cfg.HostUser, cfg.HostUID)
	}
	found := false
	for _, msg := range rec.messages {
		if strings.Contains(msg, "Initialized intuneme at "+root) {
			found = true
		}
	}
	if !found {
		t.Errorf("success message missing; messages: %q", rec.messages)
	}
}
