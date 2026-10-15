package config

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/BurntSushi/toml"
)

type Config struct {
	MachineName                     string `toml:"machine_name"`
	RootfsPath                      string `toml:"rootfs_path"`
	HostUID                         int    `toml:"host_uid"`
	HostUser                        string `toml:"host_user"`
	BrokerProxy                     bool   `toml:"broker_proxy"`
	Insiders                        bool   `toml:"insiders"`
	Himmelblau                      bool   `toml:"himmelblau"`
	HimmelblauTerminalPasskey       bool   `toml:"himmelblau_terminal_passkey"`
	HimmelblauTerminalPasskeySource string `toml:"himmelblau_terminal_passkey_source"`
	HimmelblauDomain                string `toml:"himmelblau_domain"`
	HimmelblauLocalImage            string `toml:"himmelblau_local_image"`
	HimmelblauUPN                   string `toml:"himmelblau_upn"`
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

var dnsLabelPattern = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?$`)
var gitCommitPattern = regexp.MustCompile(`^[0-9a-f]{40}$`)
var localImagePattern = regexp.MustCompile(`^localhost/[a-z0-9]+(?:[._/-][a-z0-9]+)*(?::[A-Za-z0-9_][A-Za-z0-9_.-]{0,127})?$`)

// NormalizeHimmelblauDomain validates and normalizes a DNS-form Entra sign-in
// domain, such as example.com or example.onmicrosoft.com.
func NormalizeHimmelblauDomain(domain string) (string, error) {
	domain = strings.ToLower(strings.TrimSpace(domain))
	if len(domain) > 253 || strings.Contains(domain, "..") || strings.HasPrefix(domain, ".") || strings.HasSuffix(domain, ".") {
		return "", fmt.Errorf("invalid Entra sign-in domain %q", domain)
	}
	labels := strings.Split(domain, ".")
	if len(labels) < 2 {
		return "", fmt.Errorf("invalid Entra sign-in domain %q: enter a fully qualified domain such as example.com", domain)
	}
	for _, label := range labels {
		if len(label) > 63 || !dnsLabelPattern.MatchString(label) {
			return "", fmt.Errorf("invalid Entra sign-in domain %q", domain)
		}
	}
	return domain, nil
}

// ValidateHimmelblauLocalImage restricts development overrides to localhost
// tags so they cannot accidentally bypass the published image reference.
func ValidateHimmelblauLocalImage(image string) error {
	if !localImagePattern.MatchString(image) {
		return fmt.Errorf("invalid local Himmelblau image reference %q: use localhost/<name>[:tag]", image)
	}
	return nil
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
	if cfg.HimmelblauTerminalPasskey && !cfg.Himmelblau {
		return nil, fmt.Errorf("himmelblau_terminal_passkey requires himmelblau = true")
	}
	if cfg.HimmelblauTerminalPasskey && !gitCommitPattern.MatchString(cfg.HimmelblauTerminalPasskeySource) {
		return nil, fmt.Errorf("himmelblau_terminal_passkey requires a valid himmelblau_terminal_passkey_source commit")
	}
	if cfg.HimmelblauTerminalPasskeySource != "" && !cfg.HimmelblauTerminalPasskey {
		return nil, fmt.Errorf("himmelblau_terminal_passkey_source requires himmelblau_terminal_passkey = true")
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
