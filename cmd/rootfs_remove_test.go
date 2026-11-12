package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/frostyard/clix"
	"github.com/frostyard/intuneme/internal/runner"
)

// writeRootfsConfig writes a config.toml naming rootfsPath as the rootfs.
func writeRootfsConfig(t *testing.T, root, rootfsPath string) {
	t.Helper()
	content := "machine_name = \"intuneme\"\nrootfs_path = " + quoteConfigValue(rootfsPath) + "\n"
	if err := os.WriteFile(filepath.Join(root, "config.toml"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// quoteConfigValue quotes a path for a TOML string value.
func quoteConfigValue(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range s {
		if r == '"' || r == '\\' {
			b.WriteByte('\\')
		}
		b.WriteRune(r)
	}
	b.WriteByte('"')
	return b.String()
}

// TestRemoveRootfsRefusesUnsafePath is the guard: the delete runs no command for
// a path that rm -rf must never receive, and the error says why.
func TestRemoveRootfsRefusesUnsafePath(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	root := t.TempDir()

	for _, path := range []string{"", "rootfs", "/", home} {
		r := &startSecurityRunner{}
		out, err := removeRootfs(r, root, path)
		if err == nil {
			t.Fatalf("removeRootfs(%q) returned no error", path)
		}
		if !strings.Contains(err.Error(), "refusing to remove rootfs") {
			t.Errorf("error %q does not say the delete was refused", err)
		}
		if out != nil {
			t.Errorf("removeRootfs(%q) returned output %q", path, out)
		}
		if len(r.commands) != 0 {
			t.Fatalf("removeRootfs(%q) ran commands: %v", path, r.commands)
		}
	}
}

// TestRemoveRootfsRemovesSafePath proves a safe path still deletes the rootfs
// with exactly 1 sudo rm -rf.
func TestRemoveRootfsRemovesSafePath(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	root := t.TempDir()
	path := filepath.Join(root, "rootfs")
	r := &startSecurityRunner{}

	if _, err := removeRootfs(r, root, path); err != nil {
		t.Fatalf("removeRootfs(%q) error: %v", path, err)
	}
	want := "sudo rm -rf " + path
	if len(r.commands) != 1 || r.commands[0] != want {
		t.Fatalf("commands = %v, want exactly %q", r.commands, want)
	}
}

// TestDestroyRefusesRelativeRootfsPath proves destroy stops at config.Load, so a
// relative rootfs_path never reaches a delete.
func TestDestroyRefusesRelativeRootfsPath(t *testing.T) {
	root := t.TempDir()
	writeRootfsConfig(t, root, "rootfs")
	useRootDir(t, root)
	rec := useInitTestReporter(t)

	origDryRun, origAll := clix.DryRun, destroyAll
	clix.DryRun, destroyAll = true, false
	t.Cleanup(func() { clix.DryRun, destroyAll = origDryRun, origAll })

	err := destroyCmd.RunE(destroyCmd, nil)
	if err == nil {
		t.Fatal("destroy accepted a relative rootfs_path")
	}
	if !strings.Contains(err.Error(), "rootfs_path") {
		t.Errorf("error %q does not name rootfs_path", err)
	}
	if len(rec.messages) != 0 {
		t.Errorf("destroy reported steps before the config check: %v", rec.messages)
	}
}

// TestRecreateRefusesRelativeRootfsPath proves recreate stops at config.Load, so
// a relative rootfs_path runs no host command.
func TestRecreateRefusesRelativeRootfsPath(t *testing.T) {
	root := t.TempDir()
	writeRootfsConfig(t, root, "rootfs")
	useRootDir(t, root)
	useInitTestReporter(t)

	r := &recreateRunner{}
	origRunner := newRecreateRunner
	newRecreateRunner = func() runner.Runner { return r }
	t.Cleanup(func() { newRecreateRunner = origRunner })

	origDryRun, origSkip := clix.DryRun, skipSignatureRecreate
	clix.DryRun, skipSignatureRecreate = false, false
	t.Cleanup(func() { clix.DryRun, skipSignatureRecreate = origDryRun, origSkip })

	err := recreateCmd.RunE(recreateCmd, nil)
	if err == nil {
		t.Fatal("recreate accepted a relative rootfs_path")
	}
	if !strings.Contains(err.Error(), "rootfs_path") {
		t.Errorf("error %q does not name rootfs_path", err)
	}
	if len(r.commands) != 0 {
		t.Errorf("recreate ran host commands: %v", r.commands)
	}
}
