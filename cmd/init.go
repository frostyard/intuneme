package cmd

import (
	"bufio"
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"strings"

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
		if _, err := os.Stat(cfg.RootfsPath); err == nil && !forceInit {
			return fmt.Errorf("already initialized at %s — use --force to reinitialize", root)
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
		if err := os.MkdirAll(cfg.RootfsPath, 0755); err != nil {
			return fmt.Errorf("create rootfs dir: %w", err)
		}
		if err := p.PullAndExtract(r, image, cfg.RootfsPath, tmpDirInit); err != nil {
			return err
		}
		if err := puller.VerifyRootOwned(cfg.RootfsPath); err != nil {
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

		hostname, _ := os.Hostname()

		if err := provision.ProvisionContainer(r, rep, cfg.RootfsPath, u.Username, os.Getuid(), os.Getgid(), hostname); err != nil {
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
		if err := sudoers.Install(r, u.Username); err != nil {
			rep.Warning("sudoers install failed: %v", err)
		}

		if provision.SELinuxEnabled() {
			if clix.Verbose {
				rep.Message("Applying SELinux policy (required for machinectl shell on SELinux systems)...")
			}
			if err := provision.InstallSELinuxPolicy(r, cfg.RootfsPath); err != nil {
				rep.Warning("SELinux policy setup failed: %v", err)
			}
		}

		if clix.Verbose {
			rep.Message("Saving config...")
		}
		cfg.HostUID = os.Getuid()
		cfg.HostUser = u.Username
		if err := cfg.Save(root); err != nil {
			return err
		}

		rep.Message("Initialized intuneme at %s", root)
		return nil
	},
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
