package sudoers

import (
	"bytes"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"syscall"

	"github.com/BurntSushi/toml"
	"github.com/frostyard/intuneme/internal/nshelper"
	"github.com/frostyard/intuneme/internal/runner"
)

const filePath = "/etc/sudoers.d/intuneme-exec"

// executablePath is a variable so tests can supply a small fake helper binary
// instead of copying the test executable.
var (
	executablePath  = os.Executable
	checkExecutable = verifyExecutable
)

// ErrUntrustedSource means the running binary is not safe to install as the
// privileged helper (it is not root-owned, or its path is writable).
var ErrUntrustedSource = errors.New("privileged helper source is not trusted")

// Options are the values baked into the root-owned helper and its config.
type Options struct {
	User    string
	Machine string
	UID     int
	GID     int
	Home    string
	Rootfs  string
	// AllowUntrustedSource is set only by the explicit, sudo-authenticated
	// `init` command. `start` leaves it false so a user-owned binary cannot
	// refresh the root-owned helper without an explicit install.
	AllowUntrustedSource bool
}

func findVisudo(r runner.Runner) string {
	if path, err := r.LookPath("visudo"); err == nil {
		return path
	}
	return "/usr/sbin/visudo"
}

// Install writes the helper binary, its root-owned config, and the
// passwordless sudoers rule.
//
// The rule authorizes a single fixed helper path. sudo-rs (the default
// sudo/su on Ubuntu 25.10+) forbids wildcards in command arguments, so the
// helper must resolve the container itself rather than receiving a PID and
// script from the caller. See internal/nshelper.
func Install(r runner.Runner, opts Options) error {
	if opts.UID < 0 || uint64(opts.UID) > math.MaxUint32 {
		return fmt.Errorf("invalid uid %d", opts.UID)
	}
	if opts.GID < 0 || uint64(opts.GID) > math.MaxUint32 {
		return fmt.Errorf("invalid gid %d", opts.GID)
	}
	cfg := &nshelper.Config{
		Machine: opts.Machine,
		User:    opts.User,
		UID:     uint32(opts.UID),
		GID:     uint32(opts.GID),
		Home:    opts.Home,
		Rootfs:  opts.Rootfs,
	}
	if err := cfg.Validate(); err != nil {
		return err
	}
	if !opts.AllowUntrustedSource {
		exe, err := executablePath()
		if err != nil {
			return fmt.Errorf("locate current executable: %w", err)
		}
		if err := checkExecutable(exe); err != nil {
			return err
		}
	}

	// Install the helper first. It must be root-owned and not user-writable,
	// since it runs as root before dropping to the container user.
	if _, err := r.Run("sudo", "install", "-d", "-m", "0755", "-o", "root", "-g", "root", nshelper.Dir); err != nil {
		return fmt.Errorf("create helper dir: %w", err)
	}
	if err := installExecutable(r, opts); err != nil {
		return err
	}
	if err := installConfig(r, cfg); err != nil {
		return err
	}
	if err := installMachineHelper(r, opts); err != nil {
		return err
	}

	rule := fmt.Sprintf(
		"# Installed by intuneme: passwordless helpers for container app launch,\n"+
			"# machine stop, and the read-only MCP bind.\n"+
			"%s ALL=(root) NOPASSWD: %s\n"+
			"%s ALL=(root) NOPASSWD: %s\n",
		opts.User, nshelper.Path,
		opts.User, nshelper.MachineHelperPath,
	)

	tmp, err := os.CreateTemp("", "intuneme-sudoers-*")
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}
	defer func() { _ = os.Remove(tmp.Name()) }()

	if _, err := tmp.Write([]byte(rule)); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write temp file: %w", err)
	}
	_ = tmp.Close()

	// Validate before installing: a broken sudoers file can lock out sudo.
	if _, err := r.Run(findVisudo(r), "-c", "-f", tmp.Name()); err != nil {
		return fmt.Errorf("sudoers syntax check failed: %w", err)
	}

	if _, err := r.Run("sudo", "install", "-m", "0440", tmp.Name(), filePath); err != nil {
		return fmt.Errorf("install sudoers rule: %w", err)
	}
	return nil
}

// verifyExecutable refuses to install a privileged helper from a binary the
// user could have replaced. The binary and every directory on its path must be
// root-owned and not group/world-writable; otherwise a user could run `start`
// with a cached sudo timestamp and have their own executable copied to the
// root-owned helper path.
func verifyExecutable(path string) error {
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return fmt.Errorf("resolve executable: %w", err)
	}
	info, err := os.Stat(resolved)
	if err != nil {
		return fmt.Errorf("stat executable: %w", err)
	}
	if !info.Mode().IsRegular() || info.Mode().Perm()&0o111 == 0 {
		return fmt.Errorf("%w: %s is not an executable regular file", ErrUntrustedSource, resolved)
	}
	for current := resolved; ; current = filepath.Dir(current) {
		info, err := os.Stat(current)
		if err != nil {
			return fmt.Errorf("stat %s: %w", current, err)
		}
		st, ok := info.Sys().(*syscall.Stat_t)
		if !ok || st.Uid != 0 {
			return fmt.Errorf("%w: %s is not root-owned", ErrUntrustedSource, current)
		}
		if info.Mode().Perm()&0o022 != 0 {
			return fmt.Errorf("%w: %s is group/world-writable", ErrUntrustedSource, current)
		}
		if current == "/" {
			break
		}
	}
	return nil
}

// installExecutable copies the currently running intuneme binary to the fixed
// helper path. The binary dispatches to internal/nshelper based on argv[0], so
// the same code can be the CLI and the root-owned helper. The installed copy is
// root-owned; later changes to the user's binary do not affect it.
func installExecutable(r runner.Runner, opts Options) error {
	exe, err := executablePath()
	if err != nil {
		return fmt.Errorf("locate current executable: %w", err)
	}
	if !opts.AllowUntrustedSource {
		if err := checkExecutable(exe); err != nil {
			return err
		}
	}
	// Install beside the destination and rename, so a running helper (for
	// example a long-lived MCP server) cannot make the update fail with
	// ETXTBSY and never observes a partially written binary.
	staged := nshelper.Path + ".new"
	if _, err := r.Run("sudo", "install", "-m", "0755", "-o", "root", "-g", "root", exe, staged); err != nil {
		return fmt.Errorf("install helper binary: %w", err)
	}
	if _, err := r.Run("sudo", "mv", "-f", staged, nshelper.Path); err != nil {
		return fmt.Errorf("activate helper binary: %w", err)
	}
	return nil
}

// installMachineHelper writes the fixed stop/bind helper. It is a script, not
// the binary: the operations are two fixed machinectl invocations, and the
// script validates the caller-controlled bind source before using it.
func installMachineHelper(r runner.Runner, opts Options) error {
	tmp, err := os.CreateTemp("", "intuneme-machine-helper-*")
	if err != nil {
		return fmt.Errorf("create machine helper: %w", err)
	}
	defer func() { _ = os.Remove(tmp.Name()) }()
	if _, err := tmp.Write([]byte(nshelper.MachineHelperScript(opts.Machine, opts.UID))); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write machine helper: %w", err)
	}
	_ = tmp.Close()

	staged := nshelper.MachineHelperPath + ".new"
	if _, err := r.Run("sudo", "install", "-m", "0755", "-o", "root", "-g", "root", tmp.Name(), staged); err != nil {
		return fmt.Errorf("install machine helper: %w", err)
	}
	if _, err := r.Run("sudo", "mv", "-f", staged, nshelper.MachineHelperPath); err != nil {
		return fmt.Errorf("activate machine helper: %w", err)
	}
	return nil
}

// installConfig writes the baked machine/user/rootfs values root-owned next to
// the helper. These values are never taken from the caller.
func installConfig(r runner.Runner, cfg *nshelper.Config) error {
	var buf bytes.Buffer
	if err := toml.NewEncoder(&buf).Encode(cfg); err != nil {
		return fmt.Errorf("encode helper config: %w", err)
	}

	tmp, err := os.CreateTemp("", "intuneme-helper-config-*")
	if err != nil {
		return fmt.Errorf("create temp config: %w", err)
	}
	defer func() { _ = os.Remove(tmp.Name()) }()

	if _, err := tmp.Write(buf.Bytes()); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write temp config: %w", err)
	}
	_ = tmp.Close()

	staged := nshelper.ConfigPath + ".new"
	if _, err := r.Run("sudo", "install", "-m", "0644", "-o", "root", "-g", "root", tmp.Name(), staged); err != nil {
		return fmt.Errorf("install helper config: %w", err)
	}
	if _, err := r.Run("sudo", "mv", "-f", staged, nshelper.ConfigPath); err != nil {
		return fmt.Errorf("activate helper config: %w", err)
	}
	return nil
}

// Remove deletes the sudoers rule, the helper, and its config. Intentionally
// graceful: missing files and failed removals are not errors.
func Remove(r runner.Runner) {
	_, _ = r.Run("sudo", "rm", "-f",
		filePath,
		nshelper.Path, nshelper.Path+".new",
		nshelper.ConfigPath, nshelper.ConfigPath+".new",
		nshelper.MachineHelperPath, nshelper.MachineHelperPath+".new",
	)
}

// IsInstalled reports whether the sudoers rule, helper, and helper config all
// exist. Requiring the config means an upgrade from the old shell-script helper
// counts as not installed and is replaced by start.
func IsInstalled(r runner.Runner) bool {
	for _, path := range []string{nshelper.Path, nshelper.ConfigPath, nshelper.MachineHelperPath, filePath} {
		if _, err := os.Stat(path); err != nil {
			return false
		}
	}
	return true
}
