package cmd

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"github.com/frostyard/intuneme/internal/runner"
)

type initPuller struct{ extract func(string) error }

func (p initPuller) Name() string                                            { return "fake" }
func (p initPuller) PullAndExtract(_ runner.Runner, _, path, _ string) error { return p.extract(path) }
func skipRootOwned(string) error                                             { return nil }

func passwdPuller(t *testing.T) initPuller {
	t.Helper()
	return initPuller{extract: func(path string) error {
		if err := os.MkdirAll(filepath.Join(path, "etc"), 0o755); err != nil {
			return err
		}
		return os.WriteFile(filepath.Join(path, "etc", "passwd"), []byte("root:x:0:0::/root:/bin/bash\n"), 0o644)
	}}
}

func TestPrepareInitRootfsUmask(t *testing.T) {
	old := syscall.Umask(0o077)
	t.Cleanup(func() { syscall.Umask(old) })
	path := filepath.Join(t.TempDir(), "rootfs")
	r := &startSecurityRunner{}
	p := passwdPuller(t)
	extract := p.extract
	p.extract = func(path string) error {
		fi, err := os.Stat(path)
		if err != nil {
			return err
		}
		if fi.Mode().Perm() != 0o755 {
			t.Errorf("mode before extraction = %o, want 755", fi.Mode().Perm())
		}
		return extract(path)
	}
	if err := prepareInitRootfs(r, p, "image", path, "", skipRootOwned); err != nil {
		t.Fatal(err)
	}
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm() != 0o755 {
		t.Fatalf("rootfs mode = %o, want 755", fi.Mode().Perm())
	}
	wantChown := fi.Sys().(*syscall.Stat_t).Uid != 0 || fi.Sys().(*syscall.Stat_t).Gid != 0
	all := strings.Join(r.commands, "\n")
	if strings.Contains(all, "sudo chown 0:0 "+path) != wantChown {
		t.Fatalf("ownership commands = %v", r.commands)
	}
	if strings.Contains(all, "sudo chmod") {
		t.Fatalf("unneeded chmod: %v", r.commands)
	}
}

func TestPrepareInitRootfsFailedPullRetry(t *testing.T) {
	path := filepath.Join(t.TempDir(), "rootfs")
	failure := errors.New("pull failed")
	r := &startSecurityRunner{}
	err := prepareInitRootfs(r, initPuller{func(string) error { return failure }}, "image", path, "", skipRootOwned)
	if err != failure {
		t.Fatalf("error = %v, want original failure", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("failed pull left rootfs: %v", err)
	}
	if len(r.commands) != 0 {
		t.Fatalf("empty cleanup used sudo: %v", r.commands)
	}
	if err := prepareInitRootfs(r, passwdPuller(t), "image", path, "", skipRootOwned); err != nil {
		t.Fatalf("retry: %v", err)
	}
}

func TestPrepareInitRootfsPartialFailure(t *testing.T) {
	for _, existing := range []bool{false, true} {
		t.Run(map[bool]string{false: "new", true: "force"}[existing], func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "rootfs")
			if existing {
				if err := os.Mkdir(path, 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(path, "keep"), nil, 0o644); err != nil {
					t.Fatal(err)
				}
				if err := checkInitRootfs("root", path, true); err != nil {
					t.Fatal(err)
				}
			}
			failure := errors.New("partial extraction failed")
			p := initPuller{func(path string) error {
				if err := os.WriteFile(filepath.Join(path, "partial"), nil, 0o644); err != nil {
					return err
				}
				return failure
			}}
			r := &startSecurityRunner{}
			if err := prepareInitRootfs(r, p, "image", path, "", skipRootOwned); err != failure {
				t.Fatalf("error = %v", err)
			}
			got := strings.Contains(strings.Join(r.commands, "\n"), "sudo rm -rf "+path)
			if got == existing {
				t.Fatalf("cleanup requested = %v for existing=%v: %v", got, existing, r.commands)
			}
			if existing {
				if _, err := os.Stat(filepath.Join(path, "keep")); err != nil {
					t.Fatal(err)
				}
			}
		})
	}
}

func TestInitEmptyLeftover(t *testing.T) {
	path := filepath.Join(t.TempDir(), "rootfs")
	if err := os.Mkdir(path, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := checkInitRootfs("root", path, false); err != nil {
		t.Fatalf("empty leftover blocked init: %v", err)
	}
	failure := errors.New("pull failed")
	if err := prepareInitRootfs(&startSecurityRunner{}, initPuller{func(string) error { return failure }}, "image", path, "", skipRootOwned); err != failure {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("empty leftover was not cleaned: %v", err)
	}
	if err := os.Mkdir(path, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(path, "keep"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := checkInitRootfs("root", path, false); err == nil || !strings.Contains(err.Error(), "use --force") {
		t.Fatalf("nonempty error = %v", err)
	}
}

func TestPrepareInitRootfsPostExtractionFailure(t *testing.T) {
	for _, step := range []string{"verify", "reconcile"} {
		t.Run(step, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "rootfs")
			r := &startSecurityRunner{}
			verify := skipRootOwned
			failure := errors.New("verify failed")
			if step == "verify" {
				verify = func(string) error { return failure }
			} else {
				r.fail = "sudo chmod"
			}
			// Extraction changes the mode so reconciliation also runs for a root test user.
			p := initPuller{func(path string) error { return os.Chmod(path, 0o700) }}
			err := prepareInitRootfs(r, p, "image", path, "", verify)
			if err == nil {
				t.Fatal("failure reported as success")
			}
			if step == "verify" && err != failure {
				t.Fatalf("error = %v", err)
			}
			if _, err := os.Stat(path); !os.IsNotExist(err) {
				t.Fatalf("failed preparation left directory: %v", err)
			}
		})
	}
}

func TestInitSymlinkRootfsIsNeverCleaned(t *testing.T) {
	target := filepath.Join(t.TempDir(), "real")
	if err := os.Mkdir(target, 0o755); err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	path := filepath.Join(root, "rootfs")
	if err := os.Symlink(target, path); err != nil {
		t.Fatal(err)
	}
	if err := checkInitRootfs(root, path, false); err == nil {
		t.Fatal("symlinked rootfs accepted without --force")
	}
	failure := errors.New("pull failed")
	r := &startSecurityRunner{}
	if err := prepareInitRootfs(r, initPuller{func(string) error { return failure }}, "image", path, "", skipRootOwned); err != failure {
		t.Fatalf("error = %v", err)
	}
	if fi, err := os.Lstat(path); err != nil || fi.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("symlink removed: %v", err)
	}
	if _, err := os.Stat(target); err != nil {
		t.Fatalf("symlink target removed: %v", err)
	}
	if len(r.commands) != 0 {
		t.Fatalf("cleanup ran commands: %v", r.commands)
	}
}

func TestInitIncompleteInstallMessage(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "rootfs")
	if err := os.MkdirAll(filepath.Join(path, "etc"), 0o755); err != nil {
		t.Fatal(err)
	}
	err := checkInitRootfs(root, path, false)
	if err == nil || !strings.Contains(err.Error(), "incomplete installation") {
		t.Fatalf("error without config.toml = %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "config.toml"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	err = checkInitRootfs(root, path, false)
	if err == nil || !strings.Contains(err.Error(), "already initialized") {
		t.Fatalf("error with config.toml = %v", err)
	}
}
