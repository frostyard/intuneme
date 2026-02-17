package cmd

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/frostyard/intuneme/internal/broker"
	"github.com/frostyard/intuneme/internal/config"
	"github.com/spf13/cobra"
)

var brokerProxyCmd = &cobra.Command{
	Use:   "broker-proxy",
	Short: "Run the D-Bus broker proxy (foreground)",
	Long:  "Forwards com.microsoft.identity.broker1 from the container's session bus to the host session bus.",
	RunE: func(cmd *cobra.Command, args []string) error {
		root := rootDir
		if root == "" {
			root = config.DefaultRoot()
		}

		cfg, err := config.Load(root)
		if err != nil {
			return err
		}

		if !cfg.BrokerProxy {
			return fmt.Errorf("broker proxy is not enabled — run 'intuneme config broker-proxy enable' first")
		}

		pidPath := filepath.Join(root, "broker-proxy.pid")
		if err := broker.WritePIDFile(pidPath); err != nil {
			return fmt.Errorf("write pid file: %w", err)
		}
		defer func() { _ = os.Remove(pidPath) }()

		return broker.Run(cmd.Context(), root)
	},
}

func init() {
	rootCmd.AddCommand(brokerProxyCmd)
}
