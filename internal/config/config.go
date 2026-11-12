package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"
)

type Config struct {
	MachineName string `toml:"machine_name"`
	// RootfsPath is the absolute path to the container root filesystem
	// directory. Load checks it with ValidateRootfsPath, so a relative, unclean,
	// top-level or home-containing value stops every command before anything is
	// deleted as root.
	RootfsPath  string `toml:"rootfs_path"`
	HostUID     int    `toml:"host_uid"`
	HostUser    string `toml:"host_user"`
	BrokerProxy bool   `toml:"broker_proxy"`
	NotifyProxy bool   `toml:"notify_proxy"`
	// ScreencastProxy forwards the ScreenCast portal from the container's
	// session bus to the host portal, for screen sharing from Edge.
	ScreencastProxy bool `toml:"screencast_proxy"`
	Insiders        bool `toml:"insiders"`
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
	// Hostname is the container host name to write to /etc/hostname. Empty
	// means the default `<host hostname>LXC`. `intuneme init` and
	// `intuneme recreate` write it to the container /etc/hostname, and it
	// becomes the device name in Intune. It must be a valid RFC 1123 host
	// name; the value is validated only by ContainerHostname, never by Load,
	// so unrelated commands keep working with a bad value.
	Hostname string `toml:"hostname,omitempty"`
	// ProductName is the PC model presented through the container's DMI sysfs
	// file to Intune. Empty leaves the host model unchanged. Only
	// ContainerProductName validates it, never Load, so unrelated commands
	// keep working with a bad value.
	ProductName string `toml:"product_name,omitempty"`
}

// hostnameMaxLen is the Linux HOST_NAME_MAX (64 bytes, without the trailing
// NUL). RFC 1123 limits a single label to 63 characters.
const (
	hostnameMaxLen      = 64
	hostnameLabelMaxLen = 63
)

// ValidateHostname reports whether name is a valid RFC 1123 host name within
// the Linux HOST_NAME_MAX. The name must be 1 to 64 bytes long and made of
// dot-separated labels; each label must be 1 to 63 characters of ASCII letters,
// digits or '-', must not start or end with '-', and must not be empty. A name
// with leading, trailing or doubled dots therefore fails, and surrounding
// whitespace is rejected rather than trimmed.
func ValidateHostname(name string) error {
	if name == "" {
		return fmt.Errorf("hostname %q is empty; it must be 1 to %d characters", name, hostnameMaxLen)
	}
	if len(name) > hostnameMaxLen {
		return fmt.Errorf("hostname %q is %d bytes long; the maximum is %d", name, len(name), hostnameMaxLen)
	}
	for _, label := range strings.Split(name, ".") {
		if label == "" {
			return fmt.Errorf("hostname %q has an empty label; dots must separate non-empty labels", name)
		}
		if len(label) > hostnameLabelMaxLen {
			return fmt.Errorf("hostname %q has a label of %d characters; the maximum is %d", name, len(label), hostnameLabelMaxLen)
		}
		if strings.HasPrefix(label, "-") || strings.HasSuffix(label, "-") {
			return fmt.Errorf("hostname %q has the label %q that starts or ends with '-'", name, label)
		}
		for _, c := range label {
			switch {
			case c >= 'a' && c <= 'z':
			case c >= 'A' && c <= 'Z':
			case c >= '0' && c <= '9':
			case c == '-':
			default:
				return fmt.Errorf("hostname %q has the character %q; only ASCII letters, digits and '-' are allowed", name, c)
			}
		}
	}
	return nil
}

// ContainerHostname returns the host name to write into the container. When the
// hostname key is set it is validated and returned; a bad value is an error so
// the caller can stop before changing anything. When the key is empty the
// pre-existing default `<hostHostname>LXC` is returned unvalidated, so existing
// installs keep their current host name.
func (c *Config) ContainerHostname(hostHostname string) (string, error) {
	if c.Hostname == "" {
		return hostHostname + "LXC", nil
	}
	if err := ValidateHostname(c.Hostname); err != nil {
		return "", fmt.Errorf("invalid hostname in config.toml: %w", err)
	}
	return c.Hostname, nil
}

const productNameMaxLen = 64

// ValidateProductName accepts 1 to 64 bytes of printable ASCII without leading
// or trailing spaces. The kernel strips trailing spaces from DMI strings.
func ValidateProductName(name string) error {
	if len(name) == 0 || len(name) > productNameMaxLen {
		return fmt.Errorf("product name %q must be 1 to %d bytes long", name, productNameMaxLen)
	}
	for i := range len(name) {
		if name[i] < 0x20 || name[i] > 0x7e {
			return fmt.Errorf("product name %q must contain only printable ASCII (0x20..0x7E)", name)
		}
	}
	if strings.HasPrefix(name, " ") || strings.HasSuffix(name, " ") {
		return fmt.Errorf("product name %q must not have leading or trailing spaces", name)
	}
	return nil
}

// ContainerProductName returns the validated override, or empty when unset.
func (c *Config) ContainerProductName() (string, error) {
	if c.ProductName == "" {
		return "", nil
	}
	if err := ValidateProductName(c.ProductName); err != nil {
		return "", fmt.Errorf("invalid product_name in config.toml: %w", err)
	}
	return c.ProductName, nil
}

func DefaultRoot() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("cannot determine home directory: %w", err)
	}
	return filepath.Join(home, ".local", "share", "intuneme"), nil
}

// defaultRootfsPath returns <root>/rootfs as an absolute path. --root may be a
// relative path, but rm and systemd-nspawn need an absolute one, and Load
// refuses a relative rootfs_path.
func defaultRootfsPath(root string) (string, error) {
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return "", fmt.Errorf("cannot resolve data root %q: %w", root, err)
	}
	return filepath.Join(absRoot, "rootfs"), nil
}

// Load reads <root>/config.toml and fills in the defaults for the keys the file
// does not set. It checks the resulting rootfs_path, so a value that a
// destructive command would delete as root stops every command here.
func Load(root string) (*Config, error) {
	defaultRootfs, err := defaultRootfsPath(root)
	if err != nil {
		return nil, err
	}
	cfg := &Config{
		MachineName: "intuneme",
		RootfsPath:  defaultRootfs,
		HostUID:     os.Getuid(),
		HostUser:    os.Getenv("USER"),
	}

	path := filepath.Join(root, "config.toml")
	if _, err := os.Stat(path); err == nil {
		// A key the file omits keeps the default set above. An explicit
		// rootfs_path = "" is not replaced: ValidateRootfsPath refuses it.
		if _, err := toml.DecodeFile(path, cfg); err != nil {
			return nil, err
		}
	}

	// The default is checked too, so a bad data root cannot name a rootfs that
	// the destructive commands would then delete.
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("cannot determine home directory to check rootfs_path: %w", err)
	}
	if err := ValidateRootfsPath(cfg.RootfsPath, root, home); err != nil {
		return nil, fmt.Errorf("invalid rootfs_path in config.toml: %w", err)
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
