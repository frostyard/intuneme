package cmd

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/frostyard/intune/internal/config"
	"github.com/frostyard/intune/internal/nspawn"
	"github.com/frostyard/intune/internal/runner"
	"github.com/spf13/cobra"
)

var destroyCmd = &cobra.Command{
	Use:   "destroy",
	Short: "Remove the container rootfs and all state",
	RunE: func(cmd *cobra.Command, args []string) error {
		r := &runner.SystemRunner{}
		root := rootDir
		if root == "" {
			root = config.DefaultRoot()
		}

		cfg, err := config.Load(root)
		if err != nil {
			return err
		}

		// Stop if running
		if nspawn.IsRunning(r, cfg.MachineName) {
			fmt.Println("Stopping running container...")
			if err := nspawn.Stop(r, cfg.MachineName); err != nil {
				return fmt.Errorf("failed to stop container: %w", err)
			}
		}

		// Remove rootfs with sudo (owned by root after nspawn use)
		fmt.Printf("Removing %s...\n", root)
		out, err := r.Run("sudo", "rm", "-rf", cfg.RootfsPath)
		if err != nil {
			return fmt.Errorf("rm rootfs failed: %w\n%s", err, out)
		}

		// Remove config
		_ = os.Remove(fmt.Sprintf("%s/config.toml", root))

		// Clean intune state from ~/Intune (persists via bind mount)
		home, _ := os.UserHomeDir()
		intuneHome := filepath.Join(home, "Intune")
		staleStateDirs := []string{
			filepath.Join(intuneHome, ".config", "intune"),
			filepath.Join(intuneHome, ".local", "share", "intune"),
			filepath.Join(intuneHome, ".local", "share", "intune-portal"),
			filepath.Join(intuneHome, ".local", "share", "microsoft-identity-broker"),
			filepath.Join(intuneHome, ".local", "share", "keyrings"),
			filepath.Join(intuneHome, ".cache", "intune-portal"),
		}
		for _, dir := range staleStateDirs {
			if _, err := os.Stat(dir); err == nil {
				fmt.Printf("Cleaning %s...\n", dir)
				_ = os.RemoveAll(dir)
			}
		}

		fmt.Println("Destroyed.")
		return nil
	},
}

func init() {
	rootCmd.AddCommand(destroyCmd)
}
