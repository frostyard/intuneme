package cmd

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/frostyard/clix"
	"github.com/frostyard/intuneme/internal/config"
	"github.com/frostyard/intuneme/internal/nspawn"
	"github.com/frostyard/intuneme/internal/runner"
	"github.com/frostyard/intuneme/internal/sudoers"
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

		if clix.DryRun {
			if nspawn.IsRunning(r, cfg.MachineName) {
				rep.Message("[dry-run] Would stop container %s", cfg.MachineName)
			}
			rep.Message("[dry-run] Would remove %s", root)
			rep.Message("[dry-run] Would clean Intune state from ~/Intune")
			return nil
		}

		// Stop if running
		if nspawn.IsRunning(r, cfg.MachineName) {
			rep.Message("Stopping running container...")
			if err := nspawn.Stop(r, cfg.MachineName); err != nil {
				return fmt.Errorf("failed to stop container: %w", err)
			}
		}

		// Remove host-level rules installed by init.
		sudoers.Remove(r)

		// Remove rootfs with sudo (owned by root after nspawn use)
		rep.Message("Removing %s...", root)
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
			filepath.Join(intuneHome, ".local", "share", "keyrings"),
			filepath.Join(intuneHome, ".local", "state", "microsoft-identity-broker"),
			filepath.Join(intuneHome, ".cache", "intune-portal"),
		}
		for _, dir := range staleStateDirs {
			if _, err := os.Stat(dir); err == nil {
				if clix.Verbose {
					rep.Message("Cleaning %s...", dir)
				}
				_ = os.RemoveAll(dir)
			}
		}

		rep.Message("Destroyed.")
		return nil
	},
}

func init() {
	rootCmd.AddCommand(destroyCmd)
}
