package cmd

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/frostyard/intuneme/internal/broker"
	"github.com/frostyard/intuneme/internal/config"
	"github.com/frostyard/intuneme/internal/notify"
	"github.com/spf13/cobra"
)

var notifyProxyCmd = &cobra.Command{
	Use:   "notify-proxy",
	Short: "Run the desktop notification proxy (foreground)",
	Long:  "Serves org.freedesktop.Notifications on the container's session bus and forwards notifications to the host session bus.",
	RunE: func(cmd *cobra.Command, args []string) error {
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

		if !cfg.NotifyProxy {
			return fmt.Errorf("notify proxy is not enabled — run 'intuneme config notify-proxy enable' first")
		}

		pidPath := filepath.Join(root, "notify-proxy.pid")
		if err := broker.WritePIDFile(pidPath); err != nil {
			return fmt.Errorf("write pid file: %w", err)
		}
		defer func() { _ = os.Remove(pidPath) }()

		return notify.Run(cmd.Context(), root)
	},
}

func init() {
	rootCmd.AddCommand(notifyProxyCmd)
}
