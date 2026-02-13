package cmd

import (
	"fmt"
	"os"
	"os/user"
	"path/filepath"

	"github.com/frostyard/intune/internal/config"
	"github.com/frostyard/intune/internal/prereq"
	"github.com/frostyard/intune/internal/provision"
	"github.com/frostyard/intune/internal/runner"
	"github.com/spf13/cobra"
)

var forceInit bool

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

		fmt.Println("Pulling OCI image...")
		if err := provision.PullImage(r, cfg.Image); err != nil {
			return err
		}

		fmt.Println("Extracting rootfs...")
		os.MkdirAll(root, 0755)
		if err := provision.ExtractRootfs(r, cfg.Image, cfg.RootfsPath); err != nil {
			return err
		}

		u, _ := user.Current()
		hostname, _ := os.Hostname()

		fmt.Println("Creating container user...")
		if err := provision.CreateContainerUser(r, cfg.RootfsPath, u.Username, os.Getuid(), os.Getgid()); err != nil {
			return err
		}

		fmt.Println("Setting container user password...")
		if err := provision.SetContainerPassword(r, cfg.RootfsPath, u.Username, "Intuneme2024!"); err != nil {
			return fmt.Errorf("set password failed: %w", err)
		}

		fmt.Println("Installing additional packages (Edge, libsecret-tools)...")
		if err := provision.InstallPackages(r, cfg.RootfsPath); err != nil {
			return fmt.Errorf("install packages failed: %w", err)
		}

		fmt.Println("Enabling services...")
		if err := provision.EnableServices(r, cfg.RootfsPath); err != nil {
			return fmt.Errorf("enable services failed: %w", err)
		}

		fmt.Println("Applying fixups...")
		if err := provision.WriteFixups(cfg.RootfsPath, u.Username, os.Getuid(), os.Getgid(), hostname+"LXC"); err != nil {
			return err
		}

		fmt.Println("Configuring PAM modules...")
		if err := provision.ConfigurePAM(r, cfg.RootfsPath); err != nil {
			return fmt.Errorf("configure PAM failed: %w", err)
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

func init() {
	initCmd.Flags().BoolVar(&forceInit, "force", false, "reinitialize even if already set up")
	rootCmd.AddCommand(initCmd)
}
