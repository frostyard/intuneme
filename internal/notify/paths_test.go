package notify

import (
	"testing"

	"github.com/godbus/dbus/v5"
)

func testRewriter(existing ...string) *pathRewriter {
	set := map[string]bool{}
	for _, p := range existing {
		set[p] = true
	}
	return &pathRewriter{
		procRoot: "/proc/42/root",
		exists:   func(p string) bool { return set[p] },
	}
}

func TestAppIcon(t *testing.T) {
	r := testRewriter("/proc/42/root/tmp/.org.chromium.Chromium.x/logo.png")
	tests := []struct {
		name, in, want string
	}{
		{"file URL", "file:///tmp/.org.chromium.Chromium.x/logo.png", "file:///proc/42/root/tmp/.org.chromium.Chromium.x/logo.png"},
		{"absolute path", "/tmp/.org.chromium.Chromium.x/logo.png", "/proc/42/root/tmp/.org.chromium.Chromium.x/logo.png"},
		{"theme name", "microsoft-edge", "microsoft-edge"},
		{"empty", "", ""},
		{"missing file", "file:///tmp/gone/logo.png", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := r.appIcon(tt.in); got != tt.want {
				t.Errorf("appIcon(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestHints(t *testing.T) {
	r := testRewriter("/proc/42/root/tmp/d/icon.png")
	in := map[string]dbus.Variant{
		"image-path":    dbus.MakeVariant("/tmp/d/icon.png"),
		"image_path":    dbus.MakeVariant("/tmp/d/icon.png"),
		"desktop-entry": dbus.MakeVariant("microsoft-edge"),
	}
	got := r.hints(in)

	for _, key := range []string{"image-path", "image_path"} {
		if v := got[key].Value(); v != "/proc/42/root/tmp/d/icon.png" {
			t.Errorf("%s = %v, want rewritten path", key, v)
		}
	}
	if v := got["desktop-entry"].Value(); v != "microsoft-edge" {
		t.Errorf("desktop-entry = %v, want unchanged", v)
	}
	if v := in["image-path"].Value(); v != "/tmp/d/icon.png" {
		t.Errorf("input map was modified: image-path = %v", v)
	}
}

func TestHintsDropsMissingPath(t *testing.T) {
	r := testRewriter()
	got := r.hints(map[string]dbus.Variant{
		"image-path": dbus.MakeVariant("/tmp/gone/icon.png"),
		"urgency":    dbus.MakeVariant(byte(1)),
	})
	if _, ok := got["image-path"]; ok {
		t.Error("image-path should be dropped when the file is missing")
	}
	if _, ok := got["urgency"]; !ok {
		t.Error("urgency should be kept")
	}
}

func TestHintsKeepsThemeName(t *testing.T) {
	r := testRewriter()
	got := r.hints(map[string]dbus.Variant{"image-path": dbus.MakeVariant("mail-unread")})
	if v := got["image-path"].Value(); v != "mail-unread" {
		t.Errorf("image-path = %v, want unchanged theme name", v)
	}
}
