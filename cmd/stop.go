package cmd

import (
	"fmt"

	"github.com/bjk/intuneme/internal/config"
	"github.com/bjk/intuneme/internal/nspawn"
	"github.com/bjk/intuneme/internal/runner"
	"github.com/spf13/cobra"
)

var stopCmd = &cobra.Command{
	Use:   "stop",
	Short: "Stop the container",
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

		if !nspawn.IsRunning(r, cfg.MachineName) {
			fmt.Println("Container is not running.")
			return nil
		}

		fmt.Println("Stopping container...")
		if err := nspawn.Stop(r, cfg.MachineName); err != nil {
			return err
		}
		fmt.Println("Container stopped.")
		return nil
	},
}

func init() {
	rootCmd.AddCommand(stopCmd)
}
