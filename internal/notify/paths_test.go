package notify

import (
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"github.com/godbus/dbus/v5"
)

// testStore returns an icon store whose container root is a temp directory
// with a tmp/ subdirectory. Files placed under root stand in for the
// container's files.
func testStore(t *testing.T) (*iconStore, string) {
	t.Helper()
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "tmp"), 0o755); err != nil {
		t.Fatal(err)
	}
	s := &iconStore{procRoot: root, dir: filepath.Join(t.TempDir(), "icons")}
	if err := os.Mkdir(s.dir, 0o700); err != nil {
		t.Fatal(err)
	}
	return s, root
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// assertCopy checks that hostPath is a copy in the store directory with the
// given content.
func assertCopy(t *testing.T, s *iconStore, hostPath, content string) {
	t.Helper()
	if filepath.Dir(hostPath) != s.dir {
		t.Fatalf("copy %q is not in %q", hostPath, s.dir)
	}
	data, err := os.ReadFile(hostPath)
	if err != nil {
		t.Fatalf("read copy: %v", err)
	}
	if string(data) != content {
		t.Errorf("copy content = %q, want %q", data, content)
	}
	info, err := os.Stat(hostPath)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("copy mode = %v, want 0600", info.Mode().Perm())
	}
}

func TestAppIconCopiesFile(t *testing.T) {
	s, root := testStore(t)
	writeFile(t, filepath.Join(root, "tmp/.org.chromium.Chromium.x/logo.png"), "png")

	c := s.begin()
	got := c.appIcon("file:///tmp/.org.chromium.Chromium.x/logo.png")
	hostPath, ok := strings.CutPrefix(got, "file://")
	if !ok {
		t.Fatalf("appIcon = %q, want a file:// URL", got)
	}
	assertCopy(t, s, hostPath, "png")

	got = c.appIcon("/tmp/.org.chromium.Chromium.x/logo.png")
	if got != hostPath {
		t.Errorf("plain path = %q, want the same copy %q", got, hostPath)
	}
	if len(c.files) != 1 {
		t.Errorf("files = %v, want one copy", c.files)
	}
}

func TestAppIconKeepsThemeName(t *testing.T) {
	s, _ := testStore(t)
	c := s.begin()
	for _, in := range []string{"microsoft-edge", ""} {
		if got := c.appIcon(in); got != in {
			t.Errorf("appIcon(%q) = %q, want unchanged", in, got)
		}
	}
}

func TestCopyInRejects(t *testing.T) {
	s, root := testStore(t)

	hostSecret := filepath.Join(t.TempDir(), "secret")
	writeFile(t, hostSecret, "host secret")
	if err := os.Symlink(hostSecret, filepath.Join(root, "tmp/abs-link")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("../../../../../../.."+hostSecret, filepath.Join(root, "tmp/rel-link")); err != nil {
		t.Fatal(err)
	}
	if err := syscall.Mkfifo(filepath.Join(root, "tmp/fifo"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(root, "tmp/dir"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(root, "tmp/big"), strings.Repeat("x", maxIconSize+1))
	writeFile(t, filepath.Join(root, "etc/passwd"), "container passwd")
	writeFile(t, filepath.Join(root, "tmp/ok"), "ok")

	tests := []struct{ name, path string }{
		{"absolute symlink to a host file", "/tmp/abs-link"},
		{"relative symlink out of the root", "/tmp/rel-link"},
		{"dot dot", "/tmp/../etc/passwd"},
		{"outside tmp", "/etc/passwd"},
		{"tmp itself", "/tmp"},
		{"fifo", "/tmp/fifo"},
		{"directory", "/tmp/dir"},
		{"over the size cap", "/tmp/big"},
		{"missing", "/tmp/gone"},
		{"unclean", "/tmp//ok"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got, err := s.copyIn(tt.path); err == nil {
				t.Errorf("copyIn(%q) = %q, want an error", tt.path, got)
			}
		})
	}

	entries, err := os.ReadDir(s.dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Errorf("rejected files left copies: %v", entries)
	}
}

func TestHints(t *testing.T) {
	s, root := testStore(t)
	writeFile(t, filepath.Join(root, "tmp/d/icon.png"), "icon")

	in := map[string]dbus.Variant{
		"image-path": dbus.MakeVariant("/tmp/d/icon.png"),
		"image_path": dbus.MakeVariant("/tmp/d/icon.png"),
		"urgency":    dbus.MakeVariant(byte(1)),
	}
	c := s.begin()
	got := c.hints(in)

	first, _ := got["image-path"].Value().(string)
	assertCopy(t, s, first, "icon")
	if v := got["image_path"].Value(); v != first {
		t.Errorf("image_path = %v, want the same copy %q", v, first)
	}
	if _, ok := got["urgency"]; !ok {
		t.Error("urgency should be kept")
	}
	if v := in["image-path"].Value(); v != "/tmp/d/icon.png" {
		t.Errorf("input map was modified: image-path = %v", v)
	}

	s.remove(c.files)
	if _, err := os.Stat(first); !os.IsNotExist(err) {
		t.Errorf("copy not removed: %v", err)
	}
}

func TestHintsDropsBadPath(t *testing.T) {
	s, _ := testStore(t)
	got := s.begin().hints(map[string]dbus.Variant{
		"image-path": dbus.MakeVariant("/tmp/gone/icon.png"),
		"image_path": dbus.MakeVariant(uint32(3)),
	})
	if len(got) != 0 {
		t.Errorf("hints = %v, want both image hints dropped", got)
	}
}

func TestHintsKeepsThemeName(t *testing.T) {
	s, _ := testStore(t)
	got := s.begin().hints(map[string]dbus.Variant{"image-path": dbus.MakeVariant("mail-unread")})
	if v := got["image-path"].Value(); v != "mail-unread" {
		t.Errorf("image-path = %v, want unchanged theme name", v)
	}
}
