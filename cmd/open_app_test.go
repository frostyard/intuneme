package cmd

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/frostyard/clix"
	"github.com/frostyard/intuneme/internal/webapps"
)

// testAppID is a valid Edge web app ID for the tests.
const testAppID = "msedge-ahjdipcglkflohmjbcjmjpidpbbiocpk-Default"

// newIntuneHome returns a temporary Intune home with an applications directory.
func newIntuneHome(t *testing.T) string {
	t.Helper()
	home := filepath.Join(t.TempDir(), "Intune")
	if err := os.MkdirAll(filepath.Join(home, webapps.AppsDir), 0o755); err != nil {
		t.Fatal(err)
	}
	return home
}

// writeDesktop writes a desktop file for id into home.
func writeDesktop(t *testing.T, home, id, name string) {
	t.Helper()
	content := "[Desktop Entry]\nName=" + name + "\n"
	path := filepath.Join(home, webapps.AppsDir, id+".desktop")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestRunOpenApp_InvalidIDMakesNoRunnerCall(t *testing.T) {
	for _, id := range []string{
		"",
		"org.gnome.Nautilus",
		"../" + testAppID,
		testAppID + "; rm -rf ~",
		"msedge-" + strings.Repeat("a", 32) + "-Default\nexit 0",
	} {
		t.Run(id, func(t *testing.T) {
			r := &mcpMockRunner{}
			root, _ := initializedRoot(t, true)
			home := newIntuneHome(t)

			err := runOpenApp(r, root, home, id)
			if !errors.Is(err, webapps.ErrInvalidID) {
				t.Fatalf("runOpenApp(%q) = %v, want ErrInvalidID", id, err)
			}
			if len(r.runCalls) != 0 {
				t.Fatalf("runOpenApp(%q) ran commands: %v", id, r.runCalls)
			}
		})
	}
}

func TestRunOpenApp_MissingDesktopFileMakesNoRunnerCall(t *testing.T) {
	r := &mcpMockRunner{}
	root, _ := initializedRoot(t, true)
	home := newIntuneHome(t)

	err := runOpenApp(r, root, home, testAppID)
	if err == nil || errors.Is(err, webapps.ErrInvalidID) {
		t.Fatalf("runOpenApp() = %v, want a missing-file error", err)
	}
	if len(r.runCalls) != 0 {
		t.Fatalf("runOpenApp() ran commands: %v", r.runCalls)
	}
}

func TestRunOpenApp_NotInitialized(t *testing.T) {
	r := &mcpMockRunner{}
	home := newIntuneHome(t)
	writeDesktop(t, home, testAppID, "Outlook")

	err := runOpenApp(r, t.TempDir(), home, testAppID)
	if err == nil || !strings.Contains(err.Error(), "not initialized") {
		t.Fatalf("expected 'not initialized' error, got %v", err)
	}
	if len(r.runCalls) != 0 {
		t.Fatalf("runOpenApp() ran commands: %v", r.runCalls)
	}
}

func TestRunOpenApp_NotRunning(t *testing.T) {
	r := &mcpMockRunner{notRunning: true}
	root, _ := initializedRoot(t, true)
	home := newIntuneHome(t)
	writeDesktop(t, home, testAppID, "Outlook")

	err := runOpenApp(r, root, home, testAppID)
	if err == nil || !strings.Contains(err.Error(), "not running") {
		t.Fatalf("expected 'not running' error, got %v", err)
	}
}

// TestRunOpenApp_LaunchesThroughTheEdgeWrapper is the guard against a launch
// through gtk-launch or the desktop file's Exec line: either skips the
// container's microsoft-edge wrapper and its flags.
func TestRunOpenApp_LaunchesThroughTheEdgeWrapper(t *testing.T) {
	tests := []struct {
		name    string
		id      string
		appID   string
		profile string
	}{
		{
			name:    "default profile",
			id:      testAppID,
			appID:   "ahjdipcglkflohmjbcjmjpidpbbiocpk",
			profile: "Default",
		},
		{
			name:    "profile 3",
			id:      "msedge-" + strings.Repeat("a", 32) + "-Profile_3",
			appID:   strings.Repeat("a", 32),
			profile: "Profile 3",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := &mcpMockRunner{}
			root, _ := initializedRoot(t, true)
			home := newIntuneHome(t)
			writeDesktop(t, home, tt.id, "Outlook")

			if err := runOpenApp(r, root, home, tt.id); err != nil {
				t.Fatalf("runOpenApp() = %v, want nil", err)
			}
			script := lastExecScript(t, r)

			want := "nohup microsoft-edge '--profile-directory=" + tt.profile +
				"' '--app-id=" + tt.appID + "' >/dev/null 2>&1 &"
			if !strings.Contains(script, want) {
				t.Fatalf("expected %q in the launch script, got: %q", want, script)
			}
			// gtk-launch runs the desktop file's Exec line, which holds an
			// absolute Edge path and skips the wrapper and its container flags.
			for _, banned := range []string{"gtk-launch", "/usr/bin/microsoft-edge", "/opt/microsoft", ".desktop"} {
				if strings.Contains(script, banned) {
					t.Errorf("launch script must not contain %q: %q", banned, script)
				}
			}
		})
	}
}

func TestRunOpenAppList_Empty(t *testing.T) {
	t.Run("json", func(t *testing.T) {
		root, _ := initializedRoot(t, true)
		var out bytes.Buffer
		if err := runOpenAppList(root, filepath.Join(t.TempDir(), "Intune"), &out, true); err != nil {
			t.Fatalf("runOpenAppList() = %v, want nil", err)
		}
		if got := out.String(); got != "[]\n" {
			t.Fatalf("output = %q, want %q", got, "[]\n")
		}
	})

	t.Run("text", func(t *testing.T) {
		root, _ := initializedRoot(t, true)
		var out bytes.Buffer
		if err := runOpenAppList(root, filepath.Join(t.TempDir(), "Intune"), &out, false); err != nil {
			t.Fatalf("runOpenAppList() = %v, want nil", err)
		}
		if got := out.String(); got != "No Edge web apps found.\n" {
			t.Fatalf("output = %q, want %q", got, "No Edge web apps found.\n")
		}
	})
}

func TestRunOpenAppList_OneApp(t *testing.T) {
	t.Run("json", func(t *testing.T) {
		root, _ := initializedRoot(t, true)
		home := newIntuneHome(t)
		writeDesktop(t, home, testAppID, "Outlook")

		var out bytes.Buffer
		if err := runOpenAppList(root, home, &out, true); err != nil {
			t.Fatalf("runOpenAppList() = %v, want nil", err)
		}
		var got []webapps.App
		if err := json.Unmarshal(out.Bytes(), &got); err != nil {
			t.Fatalf("output %q is not valid JSON: %v", out.String(), err)
		}
		want := []webapps.App{{ID: testAppID, Name: "Outlook", Profile: "Default"}}
		if len(got) != 1 || got[0] != want[0] {
			t.Fatalf("output = %+v, want %+v", got, want)
		}
		if !strings.HasSuffix(out.String(), "\n") {
			t.Fatalf("output must end with a newline: %q", out.String())
		}
	})

	t.Run("text", func(t *testing.T) {
		root, _ := initializedRoot(t, true)
		home := newIntuneHome(t)
		writeDesktop(t, home, testAppID, "Outlook")

		var out bytes.Buffer
		if err := runOpenAppList(root, home, &out, false); err != nil {
			t.Fatalf("runOpenAppList() = %v, want nil", err)
		}
		got := out.String()
		if !strings.Contains(got, "ID") || !strings.Contains(got, "NAME") {
			t.Fatalf("output = %q, want an ID and NAME header", got)
		}
		if !strings.Contains(got, testAppID) || !strings.Contains(got, "Outlook") {
			t.Fatalf("output = %q, want the ID and the name", got)
		}
	})
}

func TestRunOpenAppList_NeedsNoRunningContainer(t *testing.T) {
	// The list comes from the host filesystem, so a runner that reports a
	// stopped machine must not change the result.
	root, _ := initializedRoot(t, true)
	home := newIntuneHome(t)
	writeDesktop(t, home, testAppID, "Outlook")

	var out bytes.Buffer
	if err := runOpenAppList(root, home, &out, true); err != nil {
		t.Fatalf("runOpenAppList() = %v, want nil", err)
	}
	if !strings.Contains(out.String(), testAppID) {
		t.Fatalf("output = %q, want the installed app", out.String())
	}
}

// executeOpenWebAppCmd runs the app subcommand with args and returns its error.
func executeOpenWebAppCmd(args ...string) error {
	cmd := makeOpenWebAppCmd()
	cmd.SetArgs(args)
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true
	return cmd.Execute()
}

func TestOpenWebAppCmd_UsageErrors(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want string
	}{
		{"list with an argument", []string{"--list", testAppID}, "--list takes no arguments"},
		{"no argument", nil, "give exactly 1 web app ID"},
		{"2 arguments", []string{testAppID, testAppID}, "give exactly 1 web app ID"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := executeOpenWebAppCmd(tt.args...)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("Execute(%v) = %v, want %q", tt.args, err, tt.want)
			}
		})
	}
}

func TestOpenWebAppCmd_JSONNeedsList(t *testing.T) {
	old := clix.JSONOutput
	t.Cleanup(func() { clix.JSONOutput = old })
	clix.JSONOutput = true

	err := executeOpenWebAppCmd(testAppID)
	if err == nil || !strings.Contains(err.Error(), "--json needs --list") {
		t.Fatalf("Execute() = %v, want %q", err, "--json needs --list")
	}
}

func TestOpenWebAppCmd_Flags(t *testing.T) {
	cmd := makeOpenWebAppCmd()
	if cmd.Flags().Lookup("list") == nil {
		t.Error("missing --list flag")
	}
	// --json is a global persistent flag of the CLI: clix reserves the name
	// across the whole command tree and App.Run fails when a command declares
	// its own.
	if cmd.Flags().Lookup("json") != nil || cmd.PersistentFlags().Lookup("json") != nil {
		t.Error("open app must not define --json; clix reserves it on the root command")
	}
	if got := cmd.Use; got != "app [ID]" {
		t.Errorf("Use = %q, want %q", got, "app [ID]")
	}
}
