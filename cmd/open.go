package cmd

import (
	"fmt"
	"os"

	"github.com/frostyard/intuneme/internal/config"
	"github.com/frostyard/intuneme/internal/nspawn"
	"github.com/frostyard/intuneme/internal/runner"
	"github.com/spf13/cobra"
)

var openCmd = &cobra.Command{
	Use:   "open",
	Short: "Launch an application inside the running container",
}

func makeOpenAppCmd(use, short, command string) *cobra.Command {
	return &cobra.Command{
		Use:   use,
		Short: short,
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

			return nspawn.Exec(r, cfg.MachineName, cfg.HostUser, cfg.HostUID, command)
		},
	}
}

func init() {
	openCmd.AddCommand(makeOpenAppCmd(
		"edge",
		"Launch Microsoft Edge inside the container",
		"microsoft-edge",
	))
	openCmd.AddCommand(makeOpenAppCmd(
		"portal",
		"Launch Intune Portal inside the container",
		"intune-portal",
	))
	rootCmd.AddCommand(openCmd)
}
