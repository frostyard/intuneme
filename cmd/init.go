package cmd

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/user"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/charmbracelet/x/term"
	"github.com/frostyard/clix"
	"github.com/frostyard/intuneme/internal/config"
	"github.com/frostyard/intuneme/internal/prereq"
	"github.com/frostyard/intuneme/internal/provision"
	"github.com/frostyard/intuneme/internal/puller"
	"github.com/frostyard/intuneme/internal/runner"
	"github.com/frostyard/intuneme/internal/sudoers"
	pkgversion "github.com/frostyard/intuneme/internal/version"
	"github.com/spf13/cobra"
)

var forceInit bool
var passwordFile string
var insidersInit bool
var himmelblauInit bool
var himmelblauTerminalPasskeyInit bool
var himmelblauDomainInit string
var himmelblauLocalImageInit string
var tmpDirInit string

var initUID = os.Getuid

var initCmd = &cobra.Command{
	Use:   "init",
	Short: "Provision an identity nspawn container",
	RunE: func(cmd *cobra.Command, args []string) error {
		if himmelblauInit && insidersInit {
			return fmt.Errorf("--insiders cannot be combined with --himmelblau; the Himmelblau image already tracks the community nightly packages")
		}
		if himmelblauTerminalPasskeyInit && !himmelblauInit {
			return fmt.Errorf("--himmelblau-terminal-passkey requires --himmelblau")
		}
		if !himmelblauInit && (cmd.Flags().Changed("himmelblau-domain") || cmd.Flags().Changed("himmelblau-local-image") || cmd.Flags().Changed("himmelblau-terminal-passkey")) {
			return fmt.Errorf("--himmelblau-domain, --himmelblau-local-image, and --himmelblau-terminal-passkey require --himmelblau")
		}

		r := &runner.SystemRunner{}
		root := rootDir
		if root == "" {
			var err error
			root, err = config.DefaultRoot()
			if err != nil {
				return err
			}
		}

		// Check prerequisites
		if errs := prereq.Check(r); len(errs) > 0 {
			for _, e := range errs {
				rep.Warning("  - %s", e)
			}
			return fmt.Errorf("missing prerequisites")
		}

		// Resolve host user early — needed for password validation.
		u, err := user.Current()
		if err != nil {
			return fmt.Errorf("get current user: %w", err)
		}

		// Acquire and validate password before doing any container work.
		password, err := readPassword(passwordFile)
		if err != nil {
			return err
		}

		// Load config for dry-run reporting.
		cfg, err := config.Load(root)
		if err != nil {
			return fmt.Errorf("load config: %w", err)
		}

		// Resolve the container host name before any container work, so a bad
		// hostname key stops init with nothing changed.
		hostHostname, _ := os.Hostname()
		containerHostname, err := cfg.ContainerHostname(hostHostname)
		if err != nil {
			return err
		}

		localImage := cfg.HimmelblauLocalImage
		if cmd.Flags().Changed("himmelblau-local-image") {
			localImage = himmelblauLocalImageInit
		}
		if err := requireLocalTerminalPasskeyImage(himmelblauTerminalPasskeyInit, localImage); err != nil {
			return err
		}

		if forceInit && (cfg.Himmelblau != himmelblauInit || cfg.HimmelblauTerminalPasskey != himmelblauTerminalPasskeyInit) {
			if _, err := os.Stat(cfg.RootfsPath); err == nil {
				return fmt.Errorf("cannot change image family or Himmelblau variant with 'init --force'; run 'intuneme destroy' before selecting a different image")
			}
		}

		if himmelblauInit {
			domain := cfg.HimmelblauDomain
			if cmd.Flags().Changed("himmelblau-domain") {
				domain = himmelblauDomainInit
			}
			if domain == "" && !clix.DryRun {
				domain, err = promptHimmelblauDomain()
				if err != nil {
					return err
				}
			}
			if domain != "" {
				domain, err = config.NormalizeHimmelblauDomain(domain)
				if err != nil {
					return err
				}
			}
			cfg.HimmelblauDomain = domain
			cfg.HimmelblauTerminalPasskey = himmelblauTerminalPasskeyInit
			cfg.HimmelblauTerminalPasskeySource = ""

			if cmd.Flags().Changed("himmelblau-local-image") {
				cfg.HimmelblauLocalImage = himmelblauLocalImageInit
			}
			if cfg.HimmelblauLocalImage != "" {
				if err := config.ValidateHimmelblauLocalImage(cfg.HimmelblauLocalImage); err != nil {
					return err
				}
			}
		} else {
			cfg.HimmelblauDomain = ""
			cfg.HimmelblauLocalImage = ""
			cfg.HimmelblauTerminalPasskey = false
			cfg.HimmelblauTerminalPasskeySource = ""
		}

		cfg.Insiders = insidersInit
		cfg.Himmelblau = himmelblauInit
		cfg.HimmelblauUPN = ""
		image := pkgversion.ImageRef(cfg.Insiders, cfg.Himmelblau)
		if cfg.HimmelblauLocalImage != "" {
			image = cfg.HimmelblauLocalImage
		}

		if clix.DryRun {
			rep.Message("[dry-run] Would pull OCI image %s and create container at %s", image, cfg.RootfsPath)
			if himmelblauInit {
				rep.Warning("Himmelblau is experimental: its mapped Entra identity inherits the local user's sudo and host authority")
				if cfg.HimmelblauTerminalPasskey {
					rep.Message("[dry-run] Would use the fork-based terminal passkey image variant")
				}
				if cfg.HimmelblauDomain == "" {
					rep.Message("[dry-run] Would prompt for the Entra sign-in domain (or accept --himmelblau-domain)")
				} else {
					rep.Message("[dry-run] Would configure Himmelblau domain %s", cfg.HimmelblauDomain)
				}
			}
			rep.Message("[dry-run] Would create container user %s", u.Username)
			rep.Message("[dry-run] Would set container hostname to %s", containerHostname)
			return nil
		}

		// Create ~/Intune directory
		home, err := os.UserHomeDir()
		if err != nil {
			return fmt.Errorf("cannot determine home directory: %w", err)
		}
		intuneHome := filepath.Join(home, "Intune")
		if err := os.MkdirAll(intuneHome, 0755); err != nil {
			return fmt.Errorf("create ~/Intune: %w", err)
		}

		// Check if already initialized
		if err := checkInitRootfs(root, cfg.RootfsPath, forceInit); err != nil {
			return err
		}

		if cfg.Himmelblau {
			rep.Warning("Himmelblau mode is experimental. Enrollment maps the host-matched local account to an Entra UPN; it inherits that account's sudo and host authority, and nspawn behavior is not upstream-validated.")
			if cfg.HimmelblauTerminalPasskey {
				rep.Warning("Terminal passkey mode uses an unmerged Himmelblau fork; enabling QR requires host xdg-dbus-proxy and an active BlueZ service.")
			}
		}

		p, err := puller.DetectForImage(r, image)
		if err != nil {
			return err
		}

		rep.Message("Pulling and extracting OCI image %s (via %s)...", image, p.Name())
		if err := prepareInitRootfs(r, p, image, cfg.RootfsPath, tmpDirInit, nil); err != nil {
			return err
		}
		if cfg.Himmelblau {
			if err := provision.VerifyHimmelblauPAMTester(cfg.RootfsPath); err != nil {
				return err
			}
			terminalPasskey, source, err := resolveTerminalPasskeyImage(
				cfg.RootfsPath,
				cfg.HimmelblauLocalImage,
				himmelblauTerminalPasskeyInit,
			)
			if err != nil {
				return err
			}
			cfg.HimmelblauTerminalPasskey = terminalPasskey
			cfg.HimmelblauTerminalPasskeySource = source
			if terminalPasskey && !himmelblauTerminalPasskeyInit {
				rep.Warning("Detected a terminal-passkey Himmelblau image built from %s; when QR is enabled, enrollment requires host xdg-dbus-proxy and an active BlueZ service.", source)
			}
			if err := provision.ConfigureHimmelblauDomain(r, cfg.RootfsPath, cfg.HimmelblauDomain); err != nil {
				return err
			}
			if err := installSavedHimmelblauConfig(r, root, cfg.RootfsPath, cfg.HimmelblauDomain); err != nil {
				return err
			}
			if err := provision.InitializeHimmelblauUserMap(r, cfg.RootfsPath); err != nil {
				return err
			}
		}

		if err := provision.ProvisionContainer(r, rep, cfg.RootfsPath, u.Username, os.Getuid(), os.Getgid(), containerHostname, cfg.MachineName); err != nil {
			return err
		}
		if cfg.Himmelblau {
			if err := provision.EnsureHimmelblauPrimaryGroupName(r, cfg.RootfsPath, u.Username); err != nil {
				return err
			}
			if err := provision.InstallHimmelblauPAMAllowlist(r, cfg.RootfsPath, u.Username, false); err != nil {
				return err
			}
		}

		rep.Message("Setting container user password...")
		if err := provision.SetContainerPassword(r, cfg.RootfsPath, u.Username, password); err != nil {
			return fmt.Errorf("set password failed: %w", err)
		}

		if clix.Verbose {
			rep.Message("Installing sudoers rule for passwordless app launch...")
		}
		if err := sudoers.Install(r, sudoers.Options{
			User:    u.Username,
			Machine: cfg.MachineName,
			UID:     os.Getuid(),
			GID:     os.Getgid(),
			Home:    fmt.Sprintf("/home/%s", u.Username),
			Rootfs:  cfg.RootfsPath,
			// init is the explicit, sudo-authenticated install. It may run
			// from a user-owned go-install binary; the root-owned copy is
			// what later starts use.
			AllowUntrustedSource: true,
		}); err != nil {
			rep.Warning("sudoers install failed: %v", err)
		}

		if err := finishInit(r, cfg, root, u.Username, os.Getuid(), initCompletionDeps{}); err != nil {
			return err
		}
		return nil
	},
}

// reusableInitRootfs accepts an absent or empty directory, including an empty
// directory left by an older failed init. A symlink is never reusable: init
// treats it as an existing installation (--force extracts through it), and
// the failure cleanup never removes it.
func reusableInitRootfs(path string) (bool, error) {
	_, reusable, err := inspectInitRootfs(path)
	return reusable, err
}

// inspectInitRootfs keeps the original metadata so preparation can distinguish
// a user-owned empty leftover from a directory supplied by an administrator.
func inspectInitRootfs(path string) (os.FileInfo, bool, error) {
	fi, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return nil, true, nil
	}
	if err != nil {
		return nil, false, err
	}
	if fi.Mode()&os.ModeSymlink != 0 {
		return fi, false, nil
	}
	if !fi.IsDir() {
		return fi, false, fmt.Errorf("rootfs must be a directory, not a symlink or other file: %s", path)
	}
	f, err := os.Open(path)
	if errors.Is(err, fs.ErrPermission) {
		return fi, false, nil
	}
	if err != nil {
		return fi, false, err
	}
	defer func() { _ = f.Close() }()
	_, err = f.Readdirnames(1)
	if errors.Is(err, io.EOF) {
		return fi, true, nil
	}
	if errors.Is(err, fs.ErrPermission) {
		return fi, false, nil
	}
	return fi, false, err
}

func checkInitRootfs(root, path string, force bool) error {
	reusable, err := reusableInitRootfs(path)
	if err != nil {
		return err
	}
	if !reusable && !force {
		if _, err := os.Stat(filepath.Join(root, "config.toml")); os.IsNotExist(err) {
			return fmt.Errorf("incomplete installation at %s: %s exists but config.toml does not — use --force to reinitialize", root, path)
		}
		return fmt.Errorf("already initialized at %s — use --force to reinitialize", root)
	}
	return nil
}

// prepareInitRootfs owns only an absent target or a user-owned empty leftover.
// An empty directory of another owner is accepted but never chmodded or removed.
// A populated --force target must survive every error.
// A nil verify selects the production ownership check.
func prepareInitRootfs(r runner.Runner, p puller.Puller, image, path, tmpDir string, verify func(string) error) (err error) {
	fi, reusable, err := inspectInitRootfs(path)
	if err != nil {
		return err
	}
	cleanup := fi == nil
	if fi != nil && reusable {
		st, ok := fi.Sys().(*syscall.Stat_t)
		cleanup = ok && uint64(st.Uid) == uint64(initUID())
	}
	if err := os.MkdirAll(path, 0o755); err != nil {
		return fmt.Errorf("create rootfs dir: %w", err)
	}
	defer func() {
		if err == nil || !cleanup {
			return
		}
		// Empty failed pulls need no sudo. Partial extractions can contain
		// root-owned files, so use the same removal path as destroy.
		removeErr := os.Remove(path)
		if removeErr == nil || os.IsNotExist(removeErr) {
			return
		}
		if !errors.Is(removeErr, syscall.ENOTEMPTY) && !errors.Is(removeErr, syscall.EEXIST) {
			rep.Warning("remove failed init rootfs %s: %v", path, removeErr)
			return
		}
		if out, removeErr := r.Run("sudo", "rm", "-rf", path); removeErr != nil {
			rep.Warning("remove failed init rootfs %s: %v: %s", path, removeErr, out)
		}
	}()
	if cleanup {
		// MkdirAll's mode is filtered by the caller's umask.
		if err := os.Chmod(path, 0o755); err != nil {
			return fmt.Errorf("set initial rootfs mode: %w", err)
		}
	}
	if err := p.PullAndExtract(r, image, path, tmpDir); err != nil {
		return err
	}
	if verify == nil {
		verify = puller.VerifyRootOwned
	}
	if err := verify(path); err != nil {
		return err
	}
	return puller.ReconcileRootfs(r, path)
}

// initCompletionDeps are the SELinux dependencies of finishInit. A nil field
// selects the production implementation, so init passes an empty value and
// tests inject fakes.
type initCompletionDeps struct {
	SELinuxEnabled       func() bool
	InstallSELinuxPolicy func(runner.Runner, string) error
}

func (d initCompletionDeps) selinuxEnabled() bool {
	if d.SELinuxEnabled != nil {
		return d.SELinuxEnabled()
	}
	return provision.SELinuxEnabled()
}

func (d initCompletionDeps) installSELinuxPolicy(r runner.Runner, rootfsPath string) error {
	if d.InstallSELinuxPolicy != nil {
		return d.InstallSELinuxPolicy(r, rootfsPath)
	}
	return provision.InstallSELinuxPolicy(r, rootfsPath)
}

// finishInit runs the last steps of init: apply the SELinux policy when
// SELinux is enabled, save the config, and report success. A SELinux policy
// failure returns an error before the config is saved and before success is
// reported, because a mislabeled rootfs cannot run the container user session.
func finishInit(r runner.Runner, cfg *config.Config, root, hostUser string, hostUID int, deps initCompletionDeps) error {
	if deps.selinuxEnabled() {
		if clix.Verbose {
			rep.Message("Applying SELinux policy (required for machinectl shell on SELinux systems)...")
		}
		if err := deps.installSELinuxPolicy(r, cfg.RootfsPath); err != nil {
			return fmt.Errorf("SELinux policy setup failed: %w\n"+
				"Install policycoreutils-python-utils (which provides semanage) or repair the SELinux policy store, then run %q",
				err, "intuneme init --force")
		}
	}

	if clix.Verbose {
		rep.Message("Saving config...")
	}
	cfg.HostUID = hostUID
	cfg.HostUser = hostUser
	if err := cfg.Save(root); err != nil {
		return err
	}

	rep.Message("Initialized intuneme at %s", root)
	return nil
}

// validatePassword ensures a password is set for interactive container logins.
func validatePassword(password string) error {
	if password == "" {
		return fmt.Errorf("password must not be empty")
	}
	return nil
}

// readPassword acquires and validates the container user password.
// If passwordFile is non-empty, it reads the first line of that file.
// Otherwise it prompts the user interactively (without echo), asking twice
// for confirmation. Up to 3 mismatch attempts are allowed.
func readPassword(passwordFile string) (string, error) {
	if passwordFile != "" {
		data, err := os.ReadFile(passwordFile)
		if err != nil {
			return "", fmt.Errorf("read password file: %w", err)
		}
		// Use only the first line; trim surrounding whitespace.
		first, _, _ := strings.Cut(strings.TrimRight(string(data), "\r\n"), "\n")
		password := strings.TrimSpace(first)
		if err := validatePassword(password); err != nil {
			return "", err
		}
		return password, nil
	}

	fmt.Println("A strong password is recommended because it protects interactive access to the container.")
	for range 3 {
		fmt.Print("Enter container user password: ")
		p1, err := term.ReadPassword(os.Stdin.Fd())
		fmt.Println()
		if err != nil {
			return "", fmt.Errorf("read password: %w", err)
		}

		fmt.Print("Confirm password: ")
		p2, err := term.ReadPassword(os.Stdin.Fd())
		fmt.Println()
		if err != nil {
			return "", fmt.Errorf("read password: %w", err)
		}

		if string(p1) != string(p2) {
			rep.Warning("Passwords do not match, please try again.")
			continue
		}

		if err := validatePassword(string(p1)); err != nil {
			return "", err
		}
		return string(p1), nil
	}
	return "", fmt.Errorf("passwords did not match after 3 attempts")
}

func init() {
	initCmd.Flags().BoolVar(&forceInit, "force", false, "reinitialize even if already set up")
	initCmd.Flags().StringVar(&passwordFile, "password-file", "", "path to file containing the container user password (first line used)")
	initCmd.Flags().BoolVar(&insidersInit, "insiders", false, "use the Microsoft insiders image (incompatible with --himmelblau)")
	initCmd.Flags().BoolVar(&himmelblauInit, "himmelblau", false, "use the experimental Himmelblau-backed container image")
	initCmd.Flags().BoolVar(&himmelblauTerminalPasskeyInit, "himmelblau-terminal-passkey", false, "use the local terminal-passkey image (requires --himmelblau-local-image)")
	initCmd.Flags().StringVar(&himmelblauDomainInit, "himmelblau-domain", "", "Entra sign-in domain (prompted during Himmelblau setup when omitted)")
	initCmd.Flags().StringVar(&himmelblauLocalImageInit, "himmelblau-local-image", "", "use a locally built localhost/ image instead of the default registry image")
	initCmd.Flags().StringVar(&tmpDirInit, "tmp-dir", "", "directory for temporary files during image extraction (default: system temp dir)")
	rootCmd.AddCommand(initCmd)
}

func resolveTerminalPasskeyImage(rootfsPath, localImage string, requested bool) (bool, string, error) {
	if err := requireLocalTerminalPasskeyImage(requested, localImage); err != nil {
		return false, "", err
	}
	source, found, err := provision.ReadHimmelblauTerminalPasskeySource(rootfsPath)
	if err != nil {
		return false, "", err
	}
	if !found {
		if requested {
			return false, "", fmt.Errorf("selected terminal-passkey image is missing its fork source marker")
		}
		return false, "", nil
	}
	return true, source, nil
}

func requireLocalTerminalPasskeyImage(requested bool, localImage string) error {
	if requested && localImage == "" {
		return fmt.Errorf("terminal-passkey mode requires a locally built image; pass --himmelblau-local-image")
	}
	return nil
}

func promptHimmelblauDomain() (string, error) {
	if !term.IsTerminal(os.Stdin.Fd()) {
		return "", fmt.Errorf("an Entra sign-in domain is required; pass --himmelblau-domain when stdin is not a terminal")
	}
	fmt.Print("Entra sign-in domain (for example, example.com): ")
	line, err := bufio.NewReader(os.Stdin).ReadString('\n')
	if err != nil {
		return "", fmt.Errorf("read Entra sign-in domain: %w", err)
	}
	return config.NormalizeHimmelblauDomain(line)
}
