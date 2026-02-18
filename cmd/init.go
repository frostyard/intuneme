package cmd

import (
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"strings"
	"unicode"

	"github.com/charmbracelet/x/term"
	"github.com/frostyard/intuneme/internal/config"
	"github.com/frostyard/intuneme/internal/prereq"
	"github.com/frostyard/intuneme/internal/provision"
	"github.com/frostyard/intuneme/internal/puller"
	"github.com/frostyard/intuneme/internal/runner"
	pkgversion "github.com/frostyard/intuneme/internal/version"
	"github.com/spf13/cobra"
)

var forceInit bool
var passwordFile string

var initCmd = &cobra.Command{
	Use:   "init",
	Short: "Provision the Intune nspawn container",
	RunE: func(cmd *cobra.Command, args []string) error {
		r := &runner.SystemRunner{}
		root := rootDir
		if root == "" {
			root = config.DefaultRoot()
		}

		// Check prerequisites
		if errs := prereq.Check(r); len(errs) > 0 {
			for _, e := range errs {
				fmt.Fprintln(os.Stderr, "  -", e)
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

		// Create ~/Intune directory
		home, _ := os.UserHomeDir()
		intuneHome := filepath.Join(home, "Intune")
		if err := os.MkdirAll(intuneHome, 0755); err != nil {
			return fmt.Errorf("create ~/Intune: %w", err)
		}

		// Check if already initialized
		cfg, _ := config.Load(root)
		if _, err := os.Stat(cfg.RootfsPath); err == nil && !forceInit {
			return fmt.Errorf("already initialized at %s — use --force to reinitialize", root)
		}

		image := pkgversion.ImageRef()

		p, err := puller.Detect(r)
		if err != nil {
			return err
		}

		fmt.Printf("Pulling and extracting OCI image %s (via %s)...\n", image, p.Name())
		if err := os.MkdirAll(cfg.RootfsPath, 0755); err != nil {
			return fmt.Errorf("create rootfs dir: %w", err)
		}
		if err := p.PullAndExtract(r, image, cfg.RootfsPath); err != nil {
			return err
		}

		hostname, _ := os.Hostname()

		fmt.Println("Creating container user...")
		if err := provision.CreateContainerUser(r, cfg.RootfsPath, u.Username, os.Getuid(), os.Getgid()); err != nil {
			return err
		}

		fmt.Println("Setting container user password...")
		if err := provision.SetContainerPassword(r, cfg.RootfsPath, u.Username, password); err != nil {
			return fmt.Errorf("set password failed: %w", err)
		}

		fmt.Println("Applying fixups...")
		if err := provision.WriteFixups(r, cfg.RootfsPath, u.Username, os.Getuid(), os.Getgid(), hostname+"LXC"); err != nil {
			return err
		}

		fmt.Println("Installing polkit rules...")
		if err := provision.InstallPolkitRule(r, "/etc/polkit-1/rules.d"); err != nil {
			fmt.Fprintf(os.Stderr, "warning: polkit install failed: %v\n", err)
		}

		fmt.Println("Saving config...")
		cfg.HostUID = os.Getuid()
		cfg.HostUser = u.Username
		if err := cfg.Save(root); err != nil {
			return err
		}

		fmt.Printf("Initialized intuneme at %s\n", root)
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
		first := strings.SplitN(strings.TrimRight(string(data), "\r\n"), "\n", 2)[0]
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
			fmt.Fprintln(os.Stderr, "Passwords do not match, please try again.")
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
	rootCmd.AddCommand(initCmd)
}
