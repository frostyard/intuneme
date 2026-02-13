package cmd

import (
	"fmt"
	"os"

	"github.com/frostyard/intune/internal/config"
	"github.com/frostyard/intune/internal/nspawn"
	"github.com/frostyard/intune/internal/runner"
	"github.com/spf13/cobra"
)

var statusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show container and intune-portal status",
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

		// Check initialized
		if _, err := os.Stat(cfg.RootfsPath); err != nil {
			fmt.Println("Status: not initialized")
			fmt.Println("Run 'intuneme init' to get started.")
			return nil
		}

		fmt.Printf("Root:    %s\n", root)
		fmt.Printf("Rootfs:  %s\n", cfg.RootfsPath)
		fmt.Printf("Machine: %s\n", cfg.MachineName)

		if nspawn.IsRunning(r, cfg.MachineName) {
			fmt.Println("Container: running")
		} else {
			fmt.Println("Container: stopped")
		}

		return nil
	},
}

func init() {
	rootCmd.AddCommand(statusCmd)
}
