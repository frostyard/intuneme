package config

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/BurntSushi/toml"
)

type Config struct {
	MachineName string `toml:"machine_name"`
	RootfsPath  string `toml:"rootfs_path"`
	HostUID     int    `toml:"host_uid"`
	HostUser    string `toml:"host_user"`
	BrokerProxy bool   `toml:"broker_proxy"`
	Insiders    bool   `toml:"insiders"`
	// MCPBinary is the host path to a self-contained MCP server binary that
	// `intuneme mcp` runs inside the container. Empty means none is configured
	// (must be supplied via --binary). The directory holding it is bind-mounted
	// into the container at runtime, so it lives outside the rootfs and survives
	// recreate.
	MCPBinary string `toml:"mcp_binary"`
	// MCPArgs are the default arguments passed to the MCP server binary when
	// `intuneme mcp` is invoked without trailing `-- args...`. For example
	// ["mcp"] makes `intuneme mcp` run `<binary> mcp`. Keeps the VS Code config
	// minimal (just ["mcp"]) by moving the server's own subcommand here.
	MCPArgs []string `toml:"mcp_args"`
}

func DefaultRoot() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("cannot determine home directory: %w", err)
	}
	return filepath.Join(home, ".local", "share", "intuneme"), nil
}

func Load(root string) (*Config, error) {
	cfg := &Config{
		MachineName: "intuneme",
		RootfsPath:  filepath.Join(root, "rootfs"),
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
	defer func() { _ = f.Close() }()
	return toml.NewEncoder(f).Encode(c)
}
