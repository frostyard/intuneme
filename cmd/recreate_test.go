package cmd

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/frostyard/clix"
	"github.com/frostyard/intuneme/internal/imagesig"
	"github.com/frostyard/intuneme/internal/runner"
)

// warningRecorder captures Warning calls for test assertions.
type warningRecorder struct {
	messageRecorder
	warnings []string
}

func (r *warningRecorder) Warning(format string, args ...any) {
	r.warnings = append(r.warnings, fmt.Sprintf(format, args...))
}

// writeRootfsHostname creates <rootfs>/etc/hostname with the given content.
func writeRootfsHostname(t *testing.T, rootfs, content string) {
	t.Helper()
	dir := filepath.Join(rootfs, "etc")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "hostname"), []byte(content), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
}

func TestContainerHostnameFromRootfs(t *testing.T) {
	rootfs := t.TempDir()
	writeRootfsHostname(t, rootfs, "oldhostLXC\n")

	if got := containerHostnameFromRootfs(rootfs); got != "oldhostLXC" {
		t.Errorf("containerHostnameFromRootfs() = %q, want %q", got, "oldhostLXC")
	}
}

func TestContainerHostnameFromRootfsMissing(t *testing.T) {
	if got := containerHostnameFromRootfs(t.TempDir()); got != "" {
		t.Errorf("containerHostnameFromRootfs() = %q, want empty", got)
	}
}

func TestWarnIfHostnameChanges(t *testing.T) {
	tests := []struct {
		name     string
		old      string
		newName  string
		wantWarn bool
	}{
		{name: "changed", old: "oldhostLXC\n", newName: "intune-laptop", wantWarn: true},
		{name: "unchanged", old: "intune-laptop\n", newName: "intune-laptop"},
		{name: "old empty", old: "\n", newName: "intune-laptop"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rootfs := t.TempDir()
			writeRootfsHostname(t, rootfs, tt.old)

			rec := &warningRecorder{}
			warnIfHostnameChanges(rec, rootfs, tt.newName)

			if !tt.wantWarn {
				if len(rec.warnings) != 0 {
					t.Fatalf("unexpected warnings: %v", rec.warnings)
				}
				return
			}
			if len(rec.warnings) != 1 {
				t.Fatalf("warnings = %v, want exactly 1", rec.warnings)
			}
			want := "Container hostname changes from \"oldhostLXC\" to \"intune-laptop\". Intune can show this as a renamed or new device."
			if rec.warnings[0] != want {
				t.Errorf("warning = %q, want %q", rec.warnings[0], want)
			}
			if !strings.Contains(rec.warnings[0], tt.newName) {
				t.Errorf("warning %q does not name the new hostname", rec.warnings[0])
			}
		})
	}
}

// recreateRunner records every command. recreate must run none of them when the
// image signature does not verify. LookPath answers podman because
// puller.Detect calls it to pick a backend; that is not a host change.
type recreateRunner struct {
	commands []string
}

func (r *recreateRunner) Run(name string, args ...string) ([]byte, error) {
	r.commands = append(r.commands, name+" "+strings.Join(args, " "))
	return nil, errors.New("injected failure")
}

func (r *recreateRunner) RunAttached(name string, args ...string) error {
	_, err := r.Run(name, args...)
	return err
}

func (r *recreateRunner) RunBackground(name string, args ...string) error {
	_, err := r.Run(name, args...)
	return err
}

func (r *recreateRunner) LookPath(name string) (string, error) {
	if name == "podman" {
		return "/usr/bin/podman", nil
	}
	return "", errors.New("not found")
}

// TestRecreateBadSignatureChangesNothing is the guard: recreate must check the
// image signature before sudo, before the stop, before the backups and before
// it deletes the old rootfs, so a rejected image leaves the container and its
// enrollment untouched.
func TestRecreateBadSignatureChangesNothing(t *testing.T) {
	for _, tc := range []struct {
		name       string
		himmelblau bool
		localImage string
		dryRun     bool
	}{
		{name: "Microsoft"},
		{name: "hosted Himmelblau", himmelblau: true},
		{name: "local Himmelblau override", himmelblau: true, localImage: "localhost/ubuntu-himmelblau-terminal-passkey:dev"},
		{name: "dry-run local override", himmelblau: true, localImage: "localhost/ubuntu-himmelblau-terminal-passkey:dev", dryRun: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			rootfs := filepath.Join(root, "rootfs")
			if err := os.Mkdir(rootfs, 0o755); err != nil {
				t.Fatal(err)
			}
			sentinel := filepath.Join(rootfs, "sentinel")
			if err := os.WriteFile(sentinel, []byte("keep"), 0o644); err != nil {
				t.Fatal(err)
			}
			config := "machine_name = \"intuneme\"\nrootfs_path = " + strconv.Quote(rootfs) + "\n"
			if tc.himmelblau {
				config += "himmelblau = true\nhimmelblau_domain = \"example.com\"\n"
			}
			if err := os.WriteFile(filepath.Join(root, "config.toml"), []byte(config), 0o644); err != nil {
				t.Fatal(err)
			}

			useRootDir(t, root)
			useInitTestReporter(t)
			checked := false
			useSignatureCheck(t, func(_ context.Context, image string) (string, error) {
				checked = true
				if tc.himmelblau && image != "ghcr.io/frostyard/ubuntu-himmelblau:latest" {
					t.Errorf("checking %q instead of the selected Himmelblau image", image)
				}
				return "", imagesig.ErrInvalidSignature
			})
			r := &recreateRunner{}
			origRunner := newRecreateRunner
			newRecreateRunner = func() runner.Runner { return r }
			t.Cleanup(func() { newRecreateRunner = origRunner })

			origDryRun, origSkip := clix.DryRun, skipSignatureRecreate
			clix.DryRun, skipSignatureRecreate = tc.dryRun, false
			t.Cleanup(func() { clix.DryRun, skipSignatureRecreate = origDryRun, origSkip })
			flag := recreateCmd.Flags().Lookup("himmelblau-local-image")
			origChanged, origImage := flag.Changed, himmelblauLocalImageRecreate
			flag.Changed, himmelblauLocalImageRecreate = tc.localImage != "", tc.localImage
			t.Cleanup(func() { flag.Changed, himmelblauLocalImageRecreate = origChanged, origImage })

			err := recreateCmd.RunE(recreateCmd, nil)
			if tc.dryRun {
				if err != nil {
					t.Fatal(err)
				}
			} else if tc.localImage != "" {
				if !errors.Is(err, imagesig.ErrUnsigned) {
					t.Fatalf("error = %v, want explicit local trust requirement", err)
				}
			} else if !errors.Is(err, imagesig.ErrInvalidSignature) {
				t.Fatalf("error = %v, want the signature error", err)
			}
			if checked != (!tc.dryRun && tc.localImage == "") {
				t.Errorf("registry verification ran = %v for dry-run=%v, local=%q", checked, tc.dryRun, tc.localImage)
			}
			if len(r.commands) != 0 {
				t.Errorf("recreate ran host commands after a failed check: %v", r.commands)
			}
			if _, statErr := os.Stat(sentinel); statErr != nil {
				t.Errorf("the old rootfs was changed after a failed check: %v", statErr)
			}
			saved, readErr := os.ReadFile(filepath.Join(root, "config.toml"))
			if readErr != nil || string(saved) != config {
				t.Errorf("saved config changed before successful trust validation: %q, %v", saved, readErr)
			}
		})
	}
}
