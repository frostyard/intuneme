// Package nshelper implements the privileged helper installed at
// /usr/local/libexec/intuneme/nsenter-exec.
//
// The helper is the only thing the intuneme-exec sudoers rule authorizes. It
// must therefore treat every argument as untrusted: it resolves the container
// leader itself, verifies that the target is the configured container, enters
// only that container's namespaces, and drops to the configured numeric UID
// before executing anything from the container's filesystem.
package nshelper

import (
	"fmt"
	"math"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/BurntSushi/toml"
)

const (
	// Dir is the root-owned directory holding the helper and its config.
	Dir = "/usr/local/libexec/intuneme"
	// Path is the fixed helper path authorized by the sudoers rule.
	Path = Dir + "/nsenter-exec"
	// ConfigPath holds the values baked in at install time.
	ConfigPath = Path + ".toml"
)

// machineRe matches systemd machine names. The special ".host" name is
// rejected separately: resolving it would target the host, not the container.
var machineRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]*$`)

// Config is written root-owned by `intuneme init`/`start` and read by the
// helper. The caller cannot influence any of these values.
type Config struct {
	Machine string `toml:"machine"`
	User    string `toml:"user"`
	UID     uint32 `toml:"uid"`
	GID     uint32 `toml:"gid"`
	Home    string `toml:"home"`
	Rootfs  string `toml:"rootfs"`
}

// LoadConfig reads and validates a helper config file.
func LoadConfig(path string) (*Config, error) {
	var cfg Config
	if _, err := toml.DecodeFile(path, &cfg); err != nil {
		return nil, fmt.Errorf("read helper config %s: %w", path, err)
	}
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return &cfg, nil
}

// Validate rejects configurations that cannot safely identify one container
// and one unprivileged user.
func (c *Config) Validate() error {
	if c.Machine == "" || c.Machine == ".host" {
		return fmt.Errorf("helper config: invalid machine name %q", c.Machine)
	}
	if !machineRe.MatchString(c.Machine) {
		return fmt.Errorf("helper config: invalid machine name %q", c.Machine)
	}
	if c.UID == 0 || c.UID == math.MaxUint32 || c.GID == math.MaxUint32 {
		return fmt.Errorf("helper config: invalid unprivileged uid/gid")
	}
	if c.User == "" || strings.ContainsAny(c.User, ":\n\r\t") {
		return fmt.Errorf("helper config: invalid user %q", c.User)
	}
	if !filepath.IsAbs(c.Home) || !filepath.IsAbs(c.Rootfs) {
		return fmt.Errorf("helper config: home and rootfs must be absolute paths")
	}
	return nil
}
