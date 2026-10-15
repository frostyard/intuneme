package config

import (
	"os"
	"path/filepath"
	"strings"
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

func TestLoadHimmelblau(t *testing.T) {
	tmp := t.TempDir()
	toml := "himmelblau = true\nhimmelblau_terminal_passkey = true\nhimmelblau_terminal_passkey_source = \"70002a71347d8b35a911e71a68facf2d2209370c\"\nhimmelblau_domain = \"example.com\"\nhimmelblau_local_image = \"localhost/ubuntu-himmelblau:dev\"\nhimmelblau_upn = \"alice@example.com\"\n"
	if err := os.WriteFile(filepath.Join(tmp, "config.toml"), []byte(toml), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	cfg, err := Load(tmp)
	if err != nil {
		t.Fatalf("Load error: %v", err)
	}
	if !cfg.Himmelblau {
		t.Error("Himmelblau should be true")
	}
	if !cfg.HimmelblauTerminalPasskey {
		t.Error("HimmelblauTerminalPasskey should be true")
	}
	if cfg.HimmelblauTerminalPasskeySource != "70002a71347d8b35a911e71a68facf2d2209370c" {
		t.Errorf("HimmelblauTerminalPasskeySource = %q, want pinned commit", cfg.HimmelblauTerminalPasskeySource)
	}
	if cfg.HimmelblauDomain != "example.com" {
		t.Errorf("HimmelblauDomain = %q, want example.com", cfg.HimmelblauDomain)
	}
	if cfg.HimmelblauLocalImage != "localhost/ubuntu-himmelblau:dev" {
		t.Errorf("HimmelblauLocalImage = %q, want localhost/ubuntu-himmelblau:dev", cfg.HimmelblauLocalImage)
	}
	if cfg.HimmelblauUPN != "alice@example.com" {
		t.Errorf("HimmelblauUPN = %q, want %q", cfg.HimmelblauUPN, "alice@example.com")
	}
}

func TestLoadRejectsTerminalPasskeyWithoutHimmelblau(t *testing.T) {
	tmp := t.TempDir()
	if err := os.WriteFile(filepath.Join(tmp, "config.toml"), []byte("himmelblau_terminal_passkey = true\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(tmp); err == nil || !strings.Contains(err.Error(), "requires himmelblau = true") {
		t.Fatalf("Load() error = %v, want terminal-passkey dependency error", err)
	}
}

func TestLoadRejectsInvalidTerminalPasskeySource(t *testing.T) {
	tmp := t.TempDir()
	content := "himmelblau = true\nhimmelblau_terminal_passkey = true\nhimmelblau_terminal_passkey_source = \"not-a-commit\"\n"
	if err := os.WriteFile(filepath.Join(tmp, "config.toml"), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(tmp); err == nil || !strings.Contains(err.Error(), "valid himmelblau_terminal_passkey_source") {
		t.Fatalf("Load() error = %v, want invalid source error", err)
	}
}

func TestLoadRejectsTerminalPasskeyWithoutSource(t *testing.T) {
	tmp := t.TempDir()
	content := "himmelblau = true\nhimmelblau_terminal_passkey = true\n"
	if err := os.WriteFile(filepath.Join(tmp, "config.toml"), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(tmp); err == nil || !strings.Contains(err.Error(), "valid himmelblau_terminal_passkey_source") {
		t.Fatalf("Load() error = %v, want missing source error", err)
	}
}

func TestSaveRoundTripHimmelblau(t *testing.T) {
	tmp := t.TempDir()
	cfg := &Config{
		MachineName:                     "intuneme",
		RootfsPath:                      filepath.Join(tmp, "rootfs"),
		Himmelblau:                      true,
		HimmelblauTerminalPasskey:       true,
		HimmelblauTerminalPasskeySource: "70002a71347d8b35a911e71a68facf2d2209370c",
		HimmelblauDomain:                "example.com",
		HimmelblauLocalImage:            "localhost/ubuntu-himmelblau:dev",
		HimmelblauUPN:                   "alice@example.com",
	}
	if err := cfg.Save(tmp); err != nil {
		t.Fatalf("Save error: %v", err)
	}

	loaded, err := Load(tmp)
	if err != nil {
		t.Fatalf("Load error: %v", err)
	}
	if !loaded.Himmelblau || !loaded.HimmelblauTerminalPasskey || loaded.HimmelblauDomain != cfg.HimmelblauDomain ||
		loaded.HimmelblauTerminalPasskeySource != cfg.HimmelblauTerminalPasskeySource ||
		loaded.HimmelblauLocalImage != cfg.HimmelblauLocalImage ||
		loaded.HimmelblauUPN != cfg.HimmelblauUPN {
		t.Errorf("Himmelblau settings = (%v, %q, %q, %q), want (%v, %q, %q, %q)",
			loaded.Himmelblau, loaded.HimmelblauDomain, loaded.HimmelblauLocalImage, loaded.HimmelblauUPN,
			cfg.Himmelblau, cfg.HimmelblauDomain, cfg.HimmelblauLocalImage, cfg.HimmelblauUPN)
	}
}

func TestNormalizeHimmelblauDomain(t *testing.T) {
	tests := []struct {
		input string
		want  string
		bad   bool
	}{
		{input: "Example.COM", want: "example.com"},
		{input: "example.onmicrosoft.com", want: "example.onmicrosoft.com"},
		{input: " example.com ", want: "example.com"},
		{input: "example", bad: true},
		{input: "example..com", bad: true},
		{input: "-example.com", bad: true},
		{input: "example-.com", bad: true},
		{input: "user@example.com", bad: true},
	}
	for _, tt := range tests {
		got, err := NormalizeHimmelblauDomain(tt.input)
		if tt.bad {
			if err == nil {
				t.Errorf("NormalizeHimmelblauDomain(%q) unexpectedly succeeded", tt.input)
			}
			continue
		}
		if err != nil {
			t.Errorf("NormalizeHimmelblauDomain(%q): %v", tt.input, err)
		} else if got != tt.want {
			t.Errorf("NormalizeHimmelblauDomain(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestValidateHimmelblauLocalImage(t *testing.T) {
	for _, image := range []string{
		"localhost/ubuntu-himmelblau:dev",
		"localhost/team/image",
	} {
		if err := ValidateHimmelblauLocalImage(image); err != nil {
			t.Errorf("ValidateHimmelblauLocalImage(%q): %v", image, err)
		}
	}
	for _, image := range []string{
		"ghcr.io/frostyard/ubuntu-himmelblau:latest",
		"localhost/",
		"localhost/../image",
		"localhost/image with spaces",
	} {
		if err := ValidateHimmelblauLocalImage(image); err == nil {
			t.Errorf("ValidateHimmelblauLocalImage(%q) unexpectedly succeeded", image)
		}
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
