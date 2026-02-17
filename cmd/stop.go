package cmd

import (
	"fmt"
	"path/filepath"

	"github.com/frostyard/intuneme/internal/broker"
	"github.com/frostyard/intuneme/internal/config"
	"github.com/frostyard/intuneme/internal/nspawn"
	"github.com/frostyard/intuneme/internal/runner"
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

		// Stop broker proxy first so host apps get clean errors
		if cfg.BrokerProxy {
			pidPath := filepath.Join(root, "broker-proxy.pid")
			broker.StopByPIDFile(pidPath)
			fmt.Println("Broker proxy stopped.")
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
