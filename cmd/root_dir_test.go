package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/frostyard/intuneme/internal/config"
)

// setDefaultRootEnv points HOME at a fresh temp dir and returns it, so a
// subcommand with no --root resolves config.DefaultRoot() inside it. The
// default root directory is created because config.Save does not create it.
func setDefaultRootEnv(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	if err := os.MkdirAll(filepath.Join(home, ".local", "share", "intuneme"), 0755); err != nil {
		t.Fatal(err)
	}
	return home
}

// useRootDir sets rootDir for one test and restores it afterwards.
func useRootDir(t *testing.T, root string) {
	t.Helper()
	orig := rootDir
	rootDir = root
	t.Cleanup(func() { rootDir = orig })
}

func TestScreencastProxyEnableUsesDefaultRoot(t *testing.T) {
	home := setDefaultRootEnv(t)
	useRootDir(t, "")
	rec := &messageRecorder{}
	origRep := rep
	rep = rec
	t.Cleanup(func() { rep = origRep })

	sub, _, err := screencastProxyConfigCmd.Find([]string{"enable"})
	if err != nil || sub == screencastProxyConfigCmd {
		t.Fatalf("no enable subcommand: %v", err)
	}
	if err := sub.RunE(sub, nil); err != nil {
		t.Fatalf("enable: %v", err)
	}

	cfg, err := config.Load(filepath.Join(home, ".local", "share", "intuneme"))
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.ScreencastProxy {
		t.Error("enable under the default root did not set screencast_proxy")
	}
}

func TestBrokerProxyEnableUsesDefaultRoot(t *testing.T) {
	home := setDefaultRootEnv(t)
	useRootDir(t, "")
	rec := &messageRecorder{}
	origRep := rep
	rep = rec
	t.Cleanup(func() { rep = origRep })

	if err := brokerProxyEnableCmd.RunE(brokerProxyEnableCmd, nil); err != nil {
		t.Fatalf("enable: %v", err)
	}

	cfg, err := config.Load(filepath.Join(home, ".local", "share", "intuneme"))
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.BrokerProxy {
		t.Error("enable under the default root did not set broker_proxy")
	}

	svcPath := filepath.Join(home, ".local", "share", "dbus-1", "services", "com.microsoft.identity.broker1.service")
	data, err := os.ReadFile(svcPath)
	if err != nil {
		t.Fatalf("read dbus service file: %v", err)
	}
	want := "--root " + filepath.Join(home, ".local", "share", "intuneme")
	if !strings.Contains(string(data), want) {
		t.Errorf("dbus service file %q does not contain %q", data, want)
	}
}

func TestNotifyProxyEnableDefaultRootError(t *testing.T) {
	t.Setenv("HOME", "")
	useRootDir(t, "")
	rec := &messageRecorder{}
	origRep := rep
	rep = rec
	t.Cleanup(func() { rep = origRep })

	sub, _, err := notifyProxyConfigCmd.Find([]string{"enable"})
	if err != nil || sub == notifyProxyConfigCmd {
		t.Fatalf("no enable subcommand: %v", err)
	}
	err = sub.RunE(sub, nil)
	if err == nil {
		t.Fatal("enable without a home directory returned no error")
	}
	if !strings.Contains(err.Error(), "cannot determine home directory") {
		t.Errorf("error %q does not contain %q", err, "cannot determine home directory")
	}
}
