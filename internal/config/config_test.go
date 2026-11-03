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

func TestLoadNotifyProxy(t *testing.T) {
	tmp := t.TempDir()
	toml := "notify_proxy = true\n"
	if err := os.WriteFile(filepath.Join(tmp, "config.toml"), []byte(toml), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	cfg, err := Load(tmp)
	if err != nil {
		t.Fatalf("Load error: %v", err)
	}
	if !cfg.NotifyProxy {
		t.Error("NotifyProxy should be true")
	}
}

func TestLoadNotifyProxyDefault(t *testing.T) {
	tmp := t.TempDir()
	cfg, err := Load(tmp)
	if err != nil {
		t.Fatalf("Load error: %v", err)
	}
	if cfg.NotifyProxy {
		t.Error("NotifyProxy should default to false")
	}
}

func TestLoadScreencastProxy(t *testing.T) {
	tmp := t.TempDir()
	if err := os.WriteFile(filepath.Join(tmp, "config.toml"), []byte("screencast_proxy = true\n"), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	cfg, err := Load(tmp)
	if err != nil {
		t.Fatalf("Load error: %v", err)
	}
	if !cfg.ScreencastProxy {
		t.Error("ScreencastProxy should be true")
	}
}

func TestLoadScreencastProxyDefault(t *testing.T) {
	cfg, err := Load(t.TempDir())
	if err != nil {
		t.Fatalf("Load error: %v", err)
	}
	if cfg.ScreencastProxy {
		t.Error("ScreencastProxy should default to false")
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
	f.Add("notify_proxy = true\n")
	f.Add("screencast_proxy = true\n")
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

func TestValidateHostname(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr bool
	}{
		{name: "single letter", input: "a"},
		{name: "hyphenated", input: "my-laptop"},
		{name: "dotted", input: "host1.example.com"},
		{name: "63 character label", input: strings.Repeat("a", 63)},
		{name: "64 character total", input: strings.Repeat("a", 31) + "." + strings.Repeat("b", 32)},
		{name: "empty", input: "", wantErr: true},
		{name: "leading hyphen", input: "-a", wantErr: true},
		{name: "trailing hyphen", input: "a-", wantErr: true},
		{name: "underscore", input: "a_b", wantErr: true},
		{name: "space inside", input: "a b", wantErr: true},
		{name: "leading space", input: " a", wantErr: true},
		{name: "double dot", input: "a..b", wantErr: true},
		{name: "leading dot", input: ".a", wantErr: true},
		{name: "trailing dot", input: "a.", wantErr: true},
		{name: "64 character label", input: strings.Repeat("a", 64), wantErr: true},
		{name: "65 character total", input: strings.Repeat("a", 65), wantErr: true},
		{name: "non ASCII", input: "héllo", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateHostname(tt.input)
			if tt.wantErr && err == nil {
				t.Fatalf("ValidateHostname(%q) = nil, want error", tt.input)
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("ValidateHostname(%q) error: %v", tt.input, err)
			}
			if err != nil && !strings.Contains(err.Error(), tt.input[:min(len(tt.input), 8)]) {
				t.Errorf("error %q does not name the bad value", err)
			}
		})
	}
}

func TestContainerHostnameDefault(t *testing.T) {
	cfg := &Config{}
	got, err := cfg.ContainerHostname("myworkstation")
	if err != nil {
		t.Fatalf("ContainerHostname error: %v", err)
	}
	if want := "myworkstationLXC"; got != want {
		t.Errorf("ContainerHostname() = %q, want %q", got, want)
	}
}

func TestContainerHostnameOverride(t *testing.T) {
	cfg := &Config{Hostname: "intune-laptop"}
	got, err := cfg.ContainerHostname("myworkstation")
	if err != nil {
		t.Fatalf("ContainerHostname error: %v", err)
	}
	if got != "intune-laptop" {
		t.Errorf("ContainerHostname() = %q, want %q", got, "intune-laptop")
	}
}

func TestContainerHostnameInvalid(t *testing.T) {
	cfg := &Config{Hostname: "bad host"}
	got, err := cfg.ContainerHostname("myworkstation")
	if err == nil {
		t.Fatalf("ContainerHostname() = %q, want error", got)
	}
	if got != "" {
		t.Errorf("ContainerHostname() = %q, want empty on error", got)
	}
	if !strings.Contains(err.Error(), "invalid hostname in config.toml") {
		t.Errorf("error %q does not mention config.toml", err)
	}
	if !strings.Contains(err.Error(), "bad host") {
		t.Errorf("error %q does not name the bad value", err)
	}
}

func TestLoadHostname(t *testing.T) {
	tmp := t.TempDir()
	if err := os.WriteFile(filepath.Join(tmp, "config.toml"), []byte("hostname = \"x\"\n"), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	cfg, err := Load(tmp)
	if err != nil {
		t.Fatalf("Load error: %v", err)
	}
	if cfg.Hostname != "x" {
		t.Errorf("Hostname = %q, want %q", cfg.Hostname, "x")
	}
}

func TestLoadHostnameDoesNotValidate(t *testing.T) {
	tmp := t.TempDir()
	if err := os.WriteFile(filepath.Join(tmp, "config.toml"), []byte("hostname = \"bad host\"\n"), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	cfg, err := Load(tmp)
	if err != nil {
		t.Fatalf("Load must not validate the hostname, got error: %v", err)
	}
	if cfg.Hostname != "bad host" {
		t.Errorf("Hostname = %q, want %q", cfg.Hostname, "bad host")
	}
}

func TestSaveOmitsEmptyHostname(t *testing.T) {
	tmp := t.TempDir()
	cfg := &Config{
		MachineName: "intuneme",
		RootfsPath:  filepath.Join(tmp, "rootfs"),
	}
	if err := cfg.Save(tmp); err != nil {
		t.Fatalf("Save error: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(tmp, "config.toml"))
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if strings.Contains(string(data), "hostname") {
		t.Errorf("config.toml must not contain a hostname key, got:\n%s", data)
	}
}

func TestSaveRoundTripHostname(t *testing.T) {
	tmp := t.TempDir()
	cfg := &Config{
		MachineName: "intuneme",
		RootfsPath:  filepath.Join(tmp, "rootfs"),
		Hostname:    "intune-laptop",
	}
	if err := cfg.Save(tmp); err != nil {
		t.Fatalf("Save error: %v", err)
	}

	loaded, err := Load(tmp)
	if err != nil {
		t.Fatalf("Load error: %v", err)
	}
	if loaded.Hostname != "intune-laptop" {
		t.Errorf("Hostname = %q, want %q", loaded.Hostname, "intune-laptop")
	}
}
