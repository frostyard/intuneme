package cmd

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/frostyard/clix"
	"github.com/frostyard/intuneme/internal/broker"
	"github.com/frostyard/intuneme/internal/config"
	"github.com/frostyard/intuneme/internal/nspawn"
	"github.com/frostyard/intuneme/internal/runner"
	"github.com/spf13/cobra"
)

var statusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show container and intune-portal status",
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

		// Check initialized
		if _, err := os.Stat(cfg.RootfsPath); err != nil {
			if printed, err := clix.OutputJSON(map[string]any{
				"initialized": false,
			}); err != nil {
				return err
			} else if printed {
				return nil
			}
			rep.Message("Status: not initialized")
			rep.Message("Run 'intuneme init' to get started.")
			return nil
		}

		containerStatus := "stopped"
		if nspawn.IsRunning(r, cfg.MachineName) {
			containerStatus = "running"
		}

		brokerStatus := ""
		if cfg.BrokerProxy {
			pidPath := filepath.Join(root, "broker-proxy.pid")
			if pid, running := broker.IsRunningByPIDFile(pidPath); running {
				brokerStatus = fmt.Sprintf("running (PID %d)", pid)
			} else {
				brokerStatus = "not running"
			}
		}

		channel := "stable"
		if cfg.Insiders {
			channel = "insiders"
		}

		if printed, err := clix.OutputJSON(map[string]any{
			"initialized":  true,
			"root":         root,
			"rootfs":       cfg.RootfsPath,
			"machine":      cfg.MachineName,
			"container":    containerStatus,
			"channel":      channel,
			"broker_proxy": brokerStatus,
		}); err != nil {
			return err
		} else if printed {
			return nil
		}

		rep.MessagePlain("Root:    %s", root)
		rep.MessagePlain("Rootfs:  %s", cfg.RootfsPath)
		rep.MessagePlain("Machine: %s", cfg.MachineName)
		rep.MessagePlain("Container: %s", containerStatus)
		rep.MessagePlain("Channel: %s", channel)

		if cfg.BrokerProxy {
			rep.MessagePlain("Broker proxy: %s", brokerStatus)
		}

		return nil
	},
}

func init() {
	rootCmd.AddCommand(statusCmd)
}
