package config

import (
	"os"
	"path/filepath"

	"github.com/BurntSushi/toml"
)

type Config struct {
	MachineName string `toml:"machine_name"`
	RootfsPath  string `toml:"rootfs_path"`
	Image       string `toml:"image"`
	HostUID     int    `toml:"host_uid"`
	HostUser    string `toml:"host_user"`
}

func DefaultRoot() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".local", "share", "intuneme")
}

func Load(root string) (*Config, error) {
	cfg := &Config{
		MachineName: "intuneme",
		RootfsPath:  filepath.Join(root, "rootfs"),
		Image:       "ghcr.io/frostyard/ubuntu-intune:latest",
		HostUID:     os.Getuid(),
		HostUser:    os.Getenv("USER"),
	}

	path := filepath.Join(root, "config.toml")
	if _, err := os.Stat(path); err == nil {
		if _, err := toml.DecodeFile(path, cfg); err != nil {
			return nil, err
		}
		// Ensure rootfs_path default if not in file
		if cfg.RootfsPath == "" {
			cfg.RootfsPath = filepath.Join(root, "rootfs")
		}
	}

	return cfg, nil
}

func (c *Config) Save(root string) error {
	path := filepath.Join(root, "config.toml")
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return toml.NewEncoder(f).Encode(c)
}
