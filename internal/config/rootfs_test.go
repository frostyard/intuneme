package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// rootfsWorld builds a directory tree with a home directory, a data root and
// symlinks, all inside a temp directory, so the checks can be exercised without
// touching a real home directory.
type rootfsWorld struct {
	base     string
	home     string
	dataRoot string
	// elsewhere is a second temp directory whose "custom" child does not exist.
	elsewhere string
}

func newRootfsWorld(t *testing.T) rootfsWorld {
	t.Helper()
	w := rootfsWorld{
		base:      t.TempDir(),
		elsewhere: t.TempDir(),
	}
	w.home = filepath.Join(w.base, "home", "alice")
	w.dataRoot = filepath.Join(w.base, "data", "intuneme")
	for _, dir := range []string{w.home, w.dataRoot, filepath.Join(w.base, "real")} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	// link 1: a symlink to the filesystem root.
	if err := os.Symlink("/", filepath.Join(w.base, "link-root")); err != nil {
		t.Fatal(err)
	}
	// link 2: a symlink to the parent of the home directory.
	if err := os.Symlink(filepath.Join(w.base, "home"), filepath.Join(w.base, "link-home")); err != nil {
		t.Fatal(err)
	}
	// link 3: a symlink to a plain directory, used as a rootfs.
	if err := os.Symlink(filepath.Join(w.base, "real"), filepath.Join(w.base, "link-real")); err != nil {
		t.Fatal(err)
	}
	return w
}

// TestValidateRootfsPath is the guard: a rootfs_path that a `sudo rm -rf` must
// never receive is refused, and a path a user can legitimately configure is
// accepted.
func TestValidateRootfsPath(t *testing.T) {
	w := newRootfsWorld(t)

	tests := []struct {
		name    string
		rootfs  string
		home    string
		noHome  bool
		wantErr string
	}{
		{name: "empty", rootfs: "", wantErr: "empty"},
		{name: "relative", rootfs: "rootfs", wantErr: "absolute"},
		{name: "relative with dot", rootfs: "./rootfs", wantErr: "absolute"},
		{name: "filesystem root", rootfs: "/", wantErr: "top-level"},
		{name: "top-level home", rootfs: "/home", wantErr: "top-level"},
		{name: "top-level usr", rootfs: "/usr", wantErr: "top-level"},
		{name: "parent element", rootfs: w.base + "/a/../b", wantErr: "clean"},
		{name: "trailing slash", rootfs: filepath.Join(w.dataRoot, "rootfs") + "/", wantErr: "clean"},
		{name: "double slash", rootfs: "//" + strings.TrimPrefix(filepath.Join(w.base, "x", "y"), "/"), wantErr: "clean"},
		{name: "home itself", rootfs: w.home, wantErr: "home"},
		{name: "ancestor of home", rootfs: filepath.Dir(w.home), wantErr: "home"},
		{name: "data root itself", rootfs: w.dataRoot, wantErr: "data root"},
		{name: "ancestor of data root", rootfs: filepath.Dir(w.dataRoot), wantErr: "data root"},
		{name: "empty home", rootfs: filepath.Join(w.dataRoot, "rootfs"), noHome: true, wantErr: "home"},
		{name: "relative home", rootfs: filepath.Join(w.dataRoot, "rootfs"), home: "home/alice", wantErr: "home"},
		{name: "symlinked parent to root", rootfs: filepath.Join(w.base, "link-root", "usr"), wantErr: "top-level"},
		{name: "symlinked parent to home parent", rootfs: filepath.Join(w.base, "link-home", "alice"), wantErr: "home"},
		{name: "default rootfs", rootfs: filepath.Join(w.dataRoot, "rootfs")},
		{name: "custom path outside the data root", rootfs: filepath.Join(w.elsewhere, "custom", "rootfs")},
		{name: "symlinked rootfs", rootfs: filepath.Join(w.base, "link-real")},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			home := tt.home
			if home == "" && !tt.noHome {
				home = w.home
			}
			err := ValidateRootfsPath(tt.rootfs, w.dataRoot, home)
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("ValidateRootfsPath(%q) = %v, want nil", tt.rootfs, err)
				}
				return
			}
			if err == nil {
				t.Fatalf("ValidateRootfsPath(%q) = nil, want an error", tt.rootfs)
			}
			if !strings.HasPrefix(err.Error(), "rootfs_path ") || !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("error %q does not start with %q or does not say %q", err, "rootfs_path ", tt.wantErr)
			}
			if tt.rootfs != "" && !strings.Contains(err.Error(), tt.rootfs) {
				t.Errorf("error %q does not name the bad value %q", err, tt.rootfs)
			}
		})
	}
}

// TestLoadRefusesUnsafeRootfsPath proves every command that loads the config
// stops when config.toml holds a rootfs_path that must never be deleted.
func TestLoadRefusesUnsafeRootfsPath(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	tests := []struct {
		name   string
		rootfs string
	}{
		{name: "explicit empty", rootfs: ""},
		{name: "relative", rootfs: "rootfs"},
		{name: "filesystem root", rootfs: "/"},
		{name: "parent element", rootfs: t.TempDir() + "/a/../b"},
		{name: "home", rootfs: home},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := t.TempDir()
			content := "rootfs_path = " + quoteTOML(tt.rootfs) + "\n"
			if err := os.WriteFile(filepath.Join(root, "config.toml"), []byte(content), 0o644); err != nil {
				t.Fatal(err)
			}
			cfg, err := Load(root)
			if err == nil {
				t.Fatalf("Load accepted rootfs_path %q: %+v", tt.rootfs, cfg)
			}
			if !strings.Contains(err.Error(), "rootfs_path") {
				t.Errorf("error %q does not name the key", err)
			}
		})
	}
}

// TestLoadOmittedRootfsPathKeepsDefault proves that a config.toml without the
// rootfs_path key still gets <root>/rootfs, while an explicit empty value is
// refused (TestLoadRefusesUnsafeRootfsPath).
func TestLoadOmittedRootfsPathKeepsDefault(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "config.toml"), []byte("machine_name = \"intuneme\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(root)
	if err != nil {
		t.Fatalf("Load error: %v", err)
	}
	if want := filepath.Join(root, "rootfs"); cfg.RootfsPath != want {
		t.Errorf("RootfsPath = %q, want %q", cfg.RootfsPath, want)
	}
}

// quoteTOML quotes a path for a TOML string value.
func quoteTOML(s string) string {
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

// TestLoadAcceptsSafeRootfsPath proves an absolute rootfs_path outside the
// guarded directories still loads.
func TestLoadAcceptsSafeRootfsPath(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	root := t.TempDir()
	rootfs := filepath.Join(t.TempDir(), "custom", "rootfs")
	if err := os.WriteFile(filepath.Join(root, "config.toml"), []byte("rootfs_path = "+quoteTOML(rootfs)+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(root)
	if err != nil {
		t.Fatalf("Load error: %v", err)
	}
	if cfg.RootfsPath != rootfs {
		t.Errorf("RootfsPath = %q, want %q", cfg.RootfsPath, rootfs)
	}
}

// TestLoadDefaultRootfsNeedsNoConfigFile proves the default rootfs is checked
// and accepted when there is no config file at all.
func TestLoadDefaultRootfsNeedsNoConfigFile(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	root := t.TempDir()

	cfg, err := Load(root)
	if err != nil {
		t.Fatalf("Load error: %v", err)
	}
	if want := filepath.Join(root, "rootfs"); cfg.RootfsPath != want {
		t.Errorf("RootfsPath = %q, want %q", cfg.RootfsPath, want)
	}
}

// TestLoadRelativeDataRootResolvesDefault proves a relative --root still works:
// the default rootfs is made absolute before it is checked.
func TestLoadRelativeDataRootResolvesDefault(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	base := t.TempDir()
	t.Chdir(base)
	if err := os.Mkdir("data", 0o755); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load("data")
	if err != nil {
		t.Fatalf("Load error: %v", err)
	}
	if want := filepath.Join(base, "data", "rootfs"); cfg.RootfsPath != want {
		t.Errorf("RootfsPath = %q, want %q", cfg.RootfsPath, want)
	}
}
