package cmd

import (
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"strings"
	"unicode"

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
var tmpDirInit string

var initCmd = &cobra.Command{
	Use:   "init",
	Short: "Provision the Intune nspawn container",
	RunE: func(cmd *cobra.Command, args []string) error {
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
		password, err := readPassword(u.Username, passwordFile)
		if err != nil {
			return err
		}

		// Load config for dry-run reporting.
		cfg, err := config.Load(root)
		if err != nil {
			return fmt.Errorf("load config: %w", err)
		}

		if clix.DryRun {
			rep.Message("[dry-run] Would pull OCI image and create container at %s", cfg.RootfsPath)
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

		cfg.Insiders = insidersInit
		image := pkgversion.ImageRef(cfg.Insiders)

		p, err := puller.Detect(r)
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

		hostname, _ := os.Hostname()

		if err := provision.ProvisionContainer(r, rep, cfg.RootfsPath, u.Username, os.Getuid(), os.Getgid(), hostname); err != nil {
			return err
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

// validatePassword checks the password against the same rules enforced by the
// container's pam_pwquality.so configuration (minlen=12, dcredit/ucredit/lcredit/ocredit=-1,
// usercheck=1). All failures are collected and returned together.
func validatePassword(username, password string) error {
	var errs []string
	if len([]rune(password)) < 12 {
		errs = append(errs, "must be at least 12 characters")
	}
	var hasDigit, hasUpper, hasLower, hasSpecial bool
	for _, r := range password {
		switch {
		case unicode.IsDigit(r):
			hasDigit = true
		case unicode.IsUpper(r):
			hasUpper = true
		case unicode.IsLower(r):
			hasLower = true
		case !unicode.IsLetter(r) && !unicode.IsDigit(r):
			hasSpecial = true
		}
	}
	if !hasDigit {
		errs = append(errs, "must contain at least one digit")
	}
	if !hasUpper {
		errs = append(errs, "must contain at least one uppercase letter")
	}
	if !hasLower {
		errs = append(errs, "must contain at least one lowercase letter")
	}
	if !hasSpecial {
		errs = append(errs, "must contain at least one special character")
	}
	if username != "" && strings.Contains(strings.ToLower(password), strings.ToLower(username)) {
		errs = append(errs, "must not contain your username")
	}
	if len(errs) > 0 {
		return fmt.Errorf("password requirements not met:\n  - %s", strings.Join(errs, "\n  - "))
	}
	return nil
}

// readPassword acquires and validates the container user password.
// If passwordFile is non-empty, it reads the first line of that file.
// Otherwise it prompts the user interactively (without echo), asking twice
// for confirmation. Up to 3 mismatch attempts are allowed.
func readPassword(username, passwordFile string) (string, error) {
	if passwordFile != "" {
		data, err := os.ReadFile(passwordFile)
		if err != nil {
			return "", fmt.Errorf("read password file: %w", err)
		}
		// Use only the first line; trim surrounding whitespace.
		first, _, _ := strings.Cut(strings.TrimRight(string(data), "\r\n"), "\n")
		password := strings.TrimSpace(first)
		if err := validatePassword(username, password); err != nil {
			return "", err
		}
		return password, nil
	}

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

		if err := validatePassword(username, string(p1)); err != nil {
			return "", err
		}
		return string(p1), nil
	}
	return "", fmt.Errorf("passwords did not match after 3 attempts")
}

func init() {
	initCmd.Flags().BoolVar(&forceInit, "force", false, "reinitialize even if already set up")
	initCmd.Flags().StringVar(&passwordFile, "password-file", "", "path to file containing the container user password (first line used)")
	initCmd.Flags().BoolVar(&insidersInit, "insiders", false, "use the insiders channel container image")
	initCmd.Flags().StringVar(&tmpDirInit, "tmp-dir", "", "directory for temporary files during image extraction (default: system temp dir)")
	rootCmd.AddCommand(initCmd)
}
