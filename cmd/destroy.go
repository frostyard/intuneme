package cmd

import (
	"fmt"
	"os"

	"github.com/bjk/intuneme/internal/config"
	"github.com/bjk/intuneme/internal/nspawn"
	"github.com/bjk/intuneme/internal/runner"
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
		os.Remove(fmt.Sprintf("%s/config.toml", root))

		fmt.Println("Destroyed.")
		return nil
	},
}

func init() {
	rootCmd.AddCommand(destroyCmd)
}
