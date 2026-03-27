package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDefaultRoot(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("os.UserHomeDir() error: %v", err)
	}
	want := filepath.Join(home, ".local", "share", "intuneme")
	got, err := DefaultRoot()
	if err != nil {
		t.Fatalf("DefaultRoot() error: %v", err)
	}
	if got != want {
		t.Errorf("DefaultRoot() = %q, want %q", got, want)
	}
}

func TestLoadCreatesDefault(t *testing.T) {
	tmp := t.TempDir()
	cfg, err := Load(tmp)
	if err != nil {
		t.Fatalf("Load(%q) error: %v", tmp, err)
	}
	if cfg.MachineName != "intuneme" {
		t.Errorf("MachineName = %q, want %q", cfg.MachineName, "intuneme")
	}
	if cfg.RootfsPath != filepath.Join(tmp, "rootfs") {
		t.Errorf("RootfsPath = %q, want %q", cfg.RootfsPath, filepath.Join(tmp, "rootfs"))
	}
}

func TestLoadReadsExisting(t *testing.T) {
	tmp := t.TempDir()
	toml := `machine_name = "myintune"` + "\n"
	if err := os.WriteFile(filepath.Join(tmp, "config.toml"), []byte(toml), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	cfg, err := Load(tmp)
	if err != nil {
		t.Fatalf("Load error: %v", err)
	}
	if cfg.MachineName != "myintune" {
		t.Errorf("MachineName = %q, want %q", cfg.MachineName, "myintune")
	}
}

func TestLoadBrokerProxy(t *testing.T) {
	tmp := t.TempDir()
	toml := "broker_proxy = true\n"
	if err := os.WriteFile(filepath.Join(tmp, "config.toml"), []byte(toml), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	cfg, err := Load(tmp)
	if err != nil {
		t.Fatalf("Load error: %v", err)
	}
	if !cfg.BrokerProxy {
		t.Error("BrokerProxy should be true")
	}
}

func TestLoadBrokerProxyDefault(t *testing.T) {
	tmp := t.TempDir()
	cfg, err := Load(tmp)
	if err != nil {
		t.Fatalf("Load error: %v", err)
	}
	if cfg.BrokerProxy {
		t.Error("BrokerProxy should default to false")
	}
}

func TestLoadInsiders(t *testing.T) {
	tmp := t.TempDir()
	toml := "insiders = true\n"
	if err := os.WriteFile(filepath.Join(tmp, "config.toml"), []byte(toml), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	cfg, err := Load(tmp)
	if err != nil {
		t.Fatalf("Load error: %v", err)
	}
	if !cfg.Insiders {
		t.Error("Insiders should be true")
	}
}

func TestLoadInsidersDefault(t *testing.T) {
	tmp := t.TempDir()
	cfg, err := Load(tmp)
	if err != nil {
		t.Fatalf("Load error: %v", err)
	}
	if cfg.Insiders {
		t.Error("Insiders should default to false")
	}
}

func TestSaveRoundTripInsiders(t *testing.T) {
	tmp := t.TempDir()
	cfg := &Config{
		MachineName: "intuneme",
		RootfsPath:  filepath.Join(tmp, "rootfs"),
		Insiders:    true,
	}
	if err := cfg.Save(tmp); err != nil {
		t.Fatalf("Save error: %v", err)
	}

	loaded, err := Load(tmp)
	if err != nil {
		t.Fatalf("Load error: %v", err)
	}
	if !loaded.Insiders {
		t.Error("Insiders should survive Save/Load round-trip")
	}
}

func FuzzLoad(f *testing.F) {
	f.Add(`machine_name = "intuneme"` + "\n")
	f.Add("broker_proxy = true\n")
	f.Add("insiders = true\n")
	f.Add(`machine_name = "test"` + "\n" + `rootfs_path = "/tmp/rootfs"` + "\n")
	f.Add("")
	f.Add("invalid toml {{{\n")
	f.Add(`host_uid = 99999` + "\n")

	f.Fuzz(func(t *testing.T, content string) {
		tmp := t.TempDir()
		if err := os.WriteFile(filepath.Join(tmp, "config.toml"), []byte(content), 0644); err != nil {
			t.Fatalf("write config: %v", err)
		}
		// Must never panic regardless of TOML content.
		_, _ = Load(tmp)
	})
}
