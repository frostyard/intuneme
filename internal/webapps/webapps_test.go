package webapps

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"unicode/utf8"
)

// letters returns n a-p letters, the alphabet of a Chromium app ID.
func letters(n int) string {
	return strings.Repeat("a", n)
}

// appID returns a valid Edge web app ID with the given profile part.
func appID(profile string) string {
	return "msedge-" + letters(32) + "-" + profile
}

func TestValidID(t *testing.T) {
	tests := []struct {
		name string
		id   string
		want bool
	}{
		{"default profile", appID("Default"), true},
		{"profile 1", appID("Profile_1"), true},
		{"profile 12", appID("Profile_12"), true},
		{"profile 9999", appID("Profile_9999"), true},
		{"real edge id", "msedge-ahjdipcglkflohmjbcjmjpidpbbiocpk-Default", true},
		{"empty", "", false},
		{"uppercase id", "msedge-" + strings.Repeat("A", 32) + "-Default", false},
		{"letters q to z", "msedge-" + strings.Repeat("q", 32) + "-Default", false},
		{"31 letters", "msedge-" + letters(31) + "-Default", false},
		{"33 letters", "msedge-" + letters(33) + "-Default", false},
		{"profile with a space", "msedge-" + letters(32) + "-Profile 1", false},
		{"profile 0", appID("Profile_0"), false},
		{"trailing desktop suffix", appID("Default") + ".desktop", false},
		{"relative path", "../" + appID("Default"), false},
		{"slash inside", "msedge-" + letters(32) + "/Default", false},
		{"shell separator", appID("Default") + "; rm -rf ~", false},
		{"command substitution", appID("Default") + "$(id)", false},
		{"newline", appID("Default") + "\n", false},
		{"chrome prefix", "chrome-" + letters(32) + "-Default", false},
		{"leading space", " " + appID("Default"), false},
		{"trailing space", appID("Default") + " ", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ValidID(tt.id); got != tt.want {
				t.Fatalf("ValidID(%q) = %v, want %v", tt.id, got, tt.want)
			}
		})
	}
}

func TestSplitID(t *testing.T) {
	tests := []struct {
		name       string
		id         string
		wantAppID  string
		wantDirect string
		wantErr    bool
	}{
		{
			name:       "default profile",
			id:         "msedge-ahjdipcglkflohmjbcjmjpidpbbiocpk-Default",
			wantAppID:  "ahjdipcglkflohmjbcjmjpidpbbiocpk",
			wantDirect: "Default",
		},
		{
			name:       "profile 1",
			id:         appID("Profile_1"),
			wantAppID:  letters(32),
			wantDirect: "Profile 1",
		},
		{
			name:       "profile 12",
			id:         appID("Profile_12"),
			wantAppID:  letters(32),
			wantDirect: "Profile 12",
		},
		{
			name:    "invalid id",
			id:      "../" + appID("Default"),
			wantErr: true,
		},
		{
			name:    "empty id",
			id:      "",
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotAppID, gotDir, err := SplitID(tt.id)
			if tt.wantErr {
				if !errors.Is(err, ErrInvalidID) {
					t.Fatalf("SplitID(%q) error = %v, want ErrInvalidID", tt.id, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("SplitID(%q) error = %v, want nil", tt.id, err)
			}
			if gotAppID != tt.wantAppID || gotDir != tt.wantDirect {
				t.Fatalf("SplitID(%q) = (%q, %q), want (%q, %q)", tt.id, gotAppID, gotDir, tt.wantAppID, tt.wantDirect)
			}
		})
	}
}

// newHome returns a temporary Intune home with an applications directory.
func newHome(t *testing.T) string {
	t.Helper()
	home := filepath.Join(t.TempDir(), "Intune")
	if err := os.MkdirAll(filepath.Join(home, AppsDir), 0o755); err != nil {
		t.Fatal(err)
	}
	return home
}

// writeApp writes a desktop file for id into home and returns its path.
func writeApp(t *testing.T, home, id, content string) string {
	t.Helper()
	path := filepath.Join(home, AppsDir, id+".desktop")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func desktopFile(name string) string {
	return "[Desktop Entry]\nName=" + name + "\nExec=/usr/bin/microsoft-edge --app=https://example.com\n"
}

func TestCheck(t *testing.T) {
	t.Run("regular file", func(t *testing.T) {
		home := newHome(t)
		writeApp(t, home, appID("Default"), desktopFile("Outlook"))
		if err := Check(home, appID("Default")); err != nil {
			t.Fatalf("Check() = %v, want nil", err)
		}
	})

	t.Run("invalid id", func(t *testing.T) {
		home := newHome(t)
		err := Check(home, "../"+appID("Default"))
		if !errors.Is(err, ErrInvalidID) {
			t.Fatalf("Check() = %v, want ErrInvalidID", err)
		}
		if strings.Contains(err.Error(), "\n") {
			t.Fatalf("error must not carry a raw line break: %q", err.Error())
		}
	})

	t.Run("invalid id with control characters is quoted", func(t *testing.T) {
		home := newHome(t)
		err := Check(home, appID("Default")+"\x1b[31m")
		if !errors.Is(err, ErrInvalidID) {
			t.Fatalf("Check() = %v, want ErrInvalidID", err)
		}
		if strings.Contains(err.Error(), "\x1b") {
			t.Fatalf("error must not carry raw control characters: %q", err.Error())
		}
	})

	t.Run("missing file", func(t *testing.T) {
		home := newHome(t)
		err := Check(home, appID("Default"))
		if err == nil || errors.Is(err, ErrInvalidID) {
			t.Fatalf("Check() = %v, want a missing-file error", err)
		}
	})

	t.Run("missing Intune home", func(t *testing.T) {
		err := Check(filepath.Join(t.TempDir(), "Intune"), appID("Default"))
		if err == nil {
			t.Fatal("Check() = nil, want an error")
		}
	})

	t.Run("symlink inside the directory", func(t *testing.T) {
		home := newHome(t)
		target := writeApp(t, home, appID("Profile_1"), desktopFile("Teams"))
		if err := os.Symlink(target, filepath.Join(home, AppsDir, appID("Default")+".desktop")); err != nil {
			t.Fatal(err)
		}
		if err := Check(home, appID("Default")); err == nil {
			t.Fatal("Check() = nil, want a refusal for a symlink")
		}
	})

	t.Run("symlink outside the tree", func(t *testing.T) {
		home := newHome(t)
		outside := filepath.Join(t.TempDir(), "secret.desktop")
		if err := os.WriteFile(outside, []byte(desktopFile("Secret")), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(outside, filepath.Join(home, AppsDir, appID("Default")+".desktop")); err != nil {
			t.Fatal(err)
		}
		if err := Check(home, appID("Default")); err == nil {
			t.Fatal("Check() = nil, want a refusal for an escaping symlink")
		}
	})

	t.Run("directory", func(t *testing.T) {
		home := newHome(t)
		if err := os.Mkdir(filepath.Join(home, AppsDir, appID("Default")+".desktop"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := Check(home, appID("Default")); err == nil {
			t.Fatal("Check() = nil, want a refusal for a directory")
		}
	})

	t.Run("fifo", func(t *testing.T) {
		home := newHome(t)
		if err := syscall.Mkfifo(filepath.Join(home, AppsDir, appID("Default")+".desktop"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := Check(home, appID("Default")); err == nil {
			t.Fatal("Check() = nil, want a refusal for a FIFO")
		}
	})

	t.Run("applications directory is an escaping symlink", func(t *testing.T) {
		outside := t.TempDir()
		if err := os.WriteFile(filepath.Join(outside, appID("Default")+".desktop"), []byte(desktopFile("Outside")), 0o644); err != nil {
			t.Fatal(err)
		}
		home := filepath.Join(t.TempDir(), "Intune")
		if err := os.MkdirAll(filepath.Dir(filepath.Join(home, AppsDir)), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(outside, filepath.Join(home, AppsDir)); err != nil {
			t.Fatal(err)
		}
		if err := Check(home, appID("Default")); err == nil {
			t.Fatal("Check() = nil, want a refusal for an escaping applications directory")
		}
	})
}

func TestList_NoDirectory(t *testing.T) {
	apps, err := List(filepath.Join(t.TempDir(), "Intune"))
	if err != nil {
		t.Fatalf("List() = %v, want nil error", err)
	}
	if apps == nil {
		t.Fatal("List() returned a nil slice, want an empty non-nil slice")
	}
	if len(apps) != 0 {
		t.Fatalf("List() returned %d apps, want 0", len(apps))
	}
}

func TestList_NoApplicationsDirectory(t *testing.T) {
	home := filepath.Join(t.TempDir(), "Intune")
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatal(err)
	}
	apps, err := List(home)
	if err != nil {
		t.Fatalf("List() = %v, want nil error", err)
	}
	if apps == nil || len(apps) != 0 {
		t.Fatalf("List() = %v, want an empty non-nil slice", apps)
	}
}

func TestList_SortsByNameThenID(t *testing.T) {
	home := newHome(t)
	writeApp(t, home, appID("Default"), desktopFile("Zulu"))
	writeApp(t, home, appID("Profile_1"), desktopFile("Alpha"))
	writeApp(t, home, appID("Profile_2"), desktopFile("Alpha"))

	apps, err := List(home)
	if err != nil {
		t.Fatalf("List() = %v, want nil error", err)
	}
	want := []App{
		{ID: appID("Profile_1"), Name: "Alpha", Profile: "Profile_1"},
		{ID: appID("Profile_2"), Name: "Alpha", Profile: "Profile_2"},
		{ID: appID("Default"), Name: "Zulu", Profile: "Default"},
	}
	if len(apps) != len(want) {
		t.Fatalf("List() returned %d apps, want %d: %v", len(apps), len(want), apps)
	}
	for i := range want {
		if apps[i] != want[i] {
			t.Errorf("apps[%d] = %+v, want %+v", i, apps[i], want[i])
		}
	}
}

func TestList_SkipsUnusableEntries(t *testing.T) {
	home := newHome(t)
	dir := filepath.Join(home, AppsDir)
	writeApp(t, home, appID("Default"), desktopFile("Good"))

	// A desktop file that is not an Edge web app.
	if err := os.WriteFile(filepath.Join(dir, "org.gnome.Nautilus.desktop"), []byte(desktopFile("Files")), 0o644); err != nil {
		t.Fatal(err)
	}
	// A valid ID without the .desktop suffix.
	if err := os.WriteFile(filepath.Join(dir, appID("Profile_1")), []byte(desktopFile("No suffix")), 0o644); err != nil {
		t.Fatal(err)
	}
	// A symlink, a directory, a FIFO and an oversized file, each with a valid ID.
	target := writeApp(t, home, appID("Profile_2"), desktopFile("Link target"))
	if err := os.Symlink(target, filepath.Join(dir, appID("Profile_3")+".desktop")); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(dir, appID("Profile_4")+".desktop"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := syscall.Mkfifo(filepath.Join(dir, appID("Profile_5")+".desktop"), 0o644); err != nil {
		t.Fatal(err)
	}
	big := desktopFile("Big") + strings.Repeat("#", maxDesktopSize)
	if err := os.WriteFile(filepath.Join(dir, appID("Profile_6")+".desktop"), []byte(big), 0o644); err != nil {
		t.Fatal(err)
	}

	apps, err := List(home)
	if err != nil {
		t.Fatalf("List() = %v, want nil error", err)
	}
	// The link target itself is a regular file, so it is listed.
	want := map[string]string{
		appID("Default"):   "Good",
		appID("Profile_2"): "Link target",
	}
	if len(apps) != len(want) {
		t.Fatalf("List() returned %d apps, want %d: %v", len(apps), len(want), apps)
	}
	for _, app := range apps {
		name, ok := want[app.ID]
		if !ok {
			t.Errorf("unexpected app %+v", app)
			continue
		}
		if app.Name != name {
			t.Errorf("app %s has name %q, want %q", app.ID, app.Name, name)
		}
	}
}

func TestList_NameParsing(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    string
	}{
		{
			name:    "plain key",
			content: "[Desktop Entry]\nName=Outlook\n",
			want:    "Outlook",
		},
		{
			name:    "localized key ignored",
			content: "[Desktop Entry]\nName[de]=Ausblick\nName=Outlook\n",
			want:    "Outlook",
		},
		{
			name:    "other group ignored",
			content: "[Other]\nName=Wrong\n[Desktop Entry]\nName=Right\n",
			want:    "Right",
		},
		{
			name:    "key before any group ignored",
			content: "Name=Before\n[Desktop Entry]\nName=After\n",
			want:    "After",
		},
		{
			name:    "only other group gives the id",
			content: "[Other]\nName=Wrong\n",
			want:    "",
		},
		{
			name:    "spaces trimmed",
			content: "[Desktop Entry]\nName=   Outlook Web   \n",
			want:    "Outlook Web",
		},
		{
			name:    "control characters removed",
			content: "[Desktop Entry]\nName=Out\x07look\x1b[0m\n",
			want:    "Outlook[0m",
		},
		{
			name:    "empty name gives the id",
			content: "[Desktop Entry]\nName=\n",
			want:    "",
		},
		{
			name:    "long name capped",
			content: "[Desktop Entry]\nName=" + strings.Repeat("x", maxNameRunes+50) + "\n",
			want:    strings.Repeat("x", maxNameRunes),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			home := newHome(t)
			writeApp(t, home, appID("Default"), tt.content)
			apps, err := List(home)
			if err != nil {
				t.Fatalf("List() = %v, want nil error", err)
			}
			want := tt.want
			if want == "" {
				want = appID("Default")
			}
			if len(apps) != 1 {
				t.Fatalf("List() returned %d apps, want 1", len(apps))
			}
			if apps[0].Name != want {
				t.Fatalf("name = %q, want %q", apps[0].Name, want)
			}
			if n := utf8.RuneCountInString(apps[0].Name); n > maxNameRunes {
				t.Fatalf("name holds %d runes, want at most %d", n, maxNameRunes)
			}
		})
	}
}

func TestList_EscapingApplicationsDirectory(t *testing.T) {
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, appID("Default")+".desktop"), []byte(desktopFile("Outside")), 0o644); err != nil {
		t.Fatal(err)
	}
	home := filepath.Join(t.TempDir(), "Intune")
	if err := os.MkdirAll(filepath.Dir(filepath.Join(home, AppsDir)), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(home, AppsDir)); err != nil {
		t.Fatal(err)
	}
	if _, err := List(home); err == nil {
		t.Fatal("List() = nil error, want a refusal for an escaping applications directory")
	}
}
