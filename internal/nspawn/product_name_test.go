package nspawn

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func mockHostProductName(t *testing.T) {
	t.Helper()
	file := filepath.Join(t.TempDir(), "host_product_name")
	if err := os.WriteFile(file, []byte("Host Model\n"), 0o444); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(OverrideHostProductNamePath(file))
}

func TestBuildBootArgsProductName(t *testing.T) {
	mockHostProductName(t)
	mount := ProductNameMount("/x/dmi/product_name")
	if mount.Host != "/x/dmi/product_name" || mount.Container != ProductNamePath || !mount.ReadOnly {
		t.Fatalf("unexpected mount: %+v", mount)
	}
	args := BuildBootArgs("/rootfs", "intuneme", "/home/Intune", "/home/user", []BindMount{mount}, nil)
	want := "--bind-ro=/x/dmi/product_name:/sys/class/dmi/id/product_name"
	if !slices.Contains(args, want) {
		t.Fatalf("args lack %q: %v", want, args)
	}
	args = BuildBootArgs("/rootfs", "intuneme", "/home/Intune", "/home/user", nil, nil)
	for _, arg := range args {
		if strings.Contains(arg, ProductNamePath) {
			t.Fatalf("unset override adds DMI bind: %s", arg)
		}
	}
}

func TestWriteProductNameFile(t *testing.T) {
	mockHostProductName(t)
	root := t.TempDir()
	dir := filepath.Join(root, "dmi")
	// The explicit chmod must repair an existing directory too.
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	file, err := WriteProductNameFile(root, "Test Model")
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(dir, "product_name"); file != want {
		t.Fatalf("path = %q, want %q", file, want)
	}
	data, err := os.ReadFile(file)
	if err != nil || string(data) != "Test Model\n" {
		t.Fatalf("content = %q, error = %v", data, err)
	}
	for path, mode := range map[string]os.FileMode{file: 0o644, dir: 0o700} {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != mode {
			t.Errorf("%s mode = %o, want %o", path, info.Mode().Perm(), mode)
		}
	}
}

func TestWriteProductNameFileReplacesSymlink(t *testing.T) {
	mockHostProductName(t)
	root := t.TempDir()
	dir := filepath.Join(root, "dmi")
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(root, "unchanged")
	if err := os.WriteFile(target, []byte("original"), 0o600); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(dir, "product_name")
	if err := os.Symlink(target, file); err != nil {
		t.Fatal(err)
	}
	if _, err := WriteProductNameFile(root, "Test Model"); err != nil {
		t.Fatal(err)
	}
	info, err := os.Lstat(file)
	if err != nil || !info.Mode().IsRegular() {
		t.Fatalf("final file is not regular: %v, %v", info, err)
	}
	data, err := os.ReadFile(target)
	if err != nil || string(data) != "original" {
		t.Fatalf("symlink target changed: %q, %v", data, err)
	}
}

func TestWriteProductNameFileRejectsPaths(t *testing.T) {
	for _, name := range []string{"colon", "backslash", "relative", "missing DMI"} {
		t.Run(name, func(t *testing.T) {
			mockHostProductName(t)
			parent := t.TempDir()
			root := filepath.Join(parent, "state")
			switch name {
			case "colon":
				root += ":bad"
			case "backslash":
				root += `\bad`
			case "relative":
				t.Chdir(parent)
				root = "state"
			case "missing DMI":
				t.Cleanup(OverrideHostProductNamePath(filepath.Join(parent, "missing")))
			}
			_, err := WriteProductNameFile(root, "Test Model")
			if err == nil {
				t.Fatal("expected error")
			}
			if name == "missing DMI" && !strings.Contains(err.Error(), "remove product_name") {
				t.Fatalf("error lacks recovery instruction: %v", err)
			}
			entries, err := os.ReadDir(parent)
			if err != nil || len(entries) != 0 {
				t.Fatalf("refused path wrote files: %v, %v", entries, err)
			}
		})
	}
}

// MkdirAll and Chmod follow a symlink, so a symlinked dmi directory is refused.
func TestWriteProductNameFileRejectsSymlinkDir(t *testing.T) {
	mockHostProductName(t)
	root := t.TempDir()
	elsewhere := t.TempDir()
	if err := os.Chmod(elsewhere, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(elsewhere, filepath.Join(root, "dmi")); err != nil {
		t.Fatal(err)
	}
	if _, err := WriteProductNameFile(root, "Test Model"); err == nil || !strings.Contains(err.Error(), "not a directory") {
		t.Fatalf("error = %v", err)
	}
	info, err := os.Stat(elsewhere)
	if err != nil || info.Mode().Perm() != 0o755 {
		t.Fatalf("symlink target changed: %v, %v", info, err)
	}
	entries, err := os.ReadDir(elsewhere)
	if err != nil || len(entries) != 0 {
		t.Fatalf("symlink target got files: %v, %v", entries, err)
	}
}

func TestWriteProductNameFileCleansTempOnError(t *testing.T) {
	mockHostProductName(t)
	root := t.TempDir()
	final := filepath.Join(root, "dmi", "product_name")
	if err := os.MkdirAll(final, 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := WriteProductNameFile(root, "Test Model"); err == nil {
		t.Fatal("rename onto directory succeeded")
	}
	entries, err := os.ReadDir(filepath.Dir(final))
	if err != nil || len(entries) != 1 || entries[0].Name() != "product_name" {
		t.Fatalf("temp file left after error: %v, %v", entries, err)
	}
}
