package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/frostyard/intuneme/internal/config"
	"github.com/spf13/cobra"
)

func TestDBusServiceContentMakesRootAbsolute(t *testing.T) {
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	got, err := dbusServiceContent("/usr/bin/intuneme", "./x")
	if err != nil {
		t.Fatal(err)
	}
	want := "broker-proxy --root " + filepath.Join(wd, "x") + "\n"
	if !strings.Contains(got, want) {
		t.Errorf("content %q does not contain %q", got, want)
	}
}

func TestDBusServiceContentKeepsAbsoluteRoot(t *testing.T) {
	got, err := dbusServiceContent("/usr/bin/intuneme", "/srv/in")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "broker-proxy --root /srv/in\n") {
		t.Errorf("unexpected content %q", got)
	}
}

// runSubcommand runs "config <parent> <name>" against a temp root and returns
// the root and the reported messages.
func runSubcommand(t *testing.T, root string, parent *cobra.Command, name string) []string {
	t.Helper()
	rec := &messageRecorder{}
	origRep, origRoot := rep, rootDir
	rep, rootDir = rec, root
	t.Cleanup(func() { rep, rootDir = origRep, origRoot })

	sub, _, err := parent.Find([]string{name})
	if err != nil || sub == parent {
		t.Fatalf("no %s subcommand: %v", name, err)
	}
	if err := sub.RunE(sub, nil); err != nil {
		t.Fatalf("%s %s: %v", parent.Use, name, err)
	}
	return rec.messages
}

func TestScreencastProxyEnableDisable(t *testing.T) {
	root := t.TempDir()

	runSubcommand(t, root, screencastProxyConfigCmd, "enable")
	cfg, err := config.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.ScreencastProxy || cfg.NotifyProxy {
		t.Errorf("enable set screencast_proxy=%v notify_proxy=%v, want true false", cfg.ScreencastProxy, cfg.NotifyProxy)
	}

	runSubcommand(t, root, screencastProxyConfigCmd, "disable")
	cfg, err = config.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ScreencastProxy {
		t.Error("disable did not clear screencast_proxy")
	}
}

func TestNotifyProxyEnableDisable(t *testing.T) {
	root := t.TempDir()

	msgs := runSubcommand(t, root, notifyProxyConfigCmd, "enable")
	cfg, err := config.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.NotifyProxy {
		t.Error("enable did not set notify_proxy")
	}
	if joined := strings.Join(msgs, "\n"); !strings.Contains(joined, "'intuneme stop' and then 'intuneme start'") {
		t.Errorf("enable output does not explain the restart:\n%s", joined)
	}

	runSubcommand(t, root, notifyProxyConfigCmd, "disable")
	cfg, err = config.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.NotifyProxy {
		t.Error("disable did not clear notify_proxy")
	}
}
