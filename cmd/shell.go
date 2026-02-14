package cmd

import (
	"fmt"
	"os"

	"github.com/frostyard/intuneme/internal/config"
	"github.com/frostyard/intuneme/internal/nspawn"
	"github.com/frostyard/intuneme/internal/runner"
	"github.com/spf13/cobra"
)

var shellCmd = &cobra.Command{
	Use:   "shell",
	Short: "Open a shell in the running container",
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

		if _, err := os.Stat(cfg.RootfsPath); err != nil {
			return fmt.Errorf("not initialized — run 'intuneme init' first")
		}

		if !nspawn.IsRunning(r, cfg.MachineName) {
			return fmt.Errorf("container is not running — run 'intuneme start' first")
		}

		return nspawn.Shell(r, cfg.MachineName, cfg.HostUser)
	},
}

func init() {
	rootCmd.AddCommand(shellCmd)
}
