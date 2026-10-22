package cmd

import (
	"github.com/frostyard/intuneme/internal/config"
	"github.com/frostyard/intuneme/internal/notify"
	"github.com/spf13/cobra"
)

var notifyProxyCmd = &cobra.Command{
	Use:   "notify-proxy",
	Short: "Run the desktop notification proxy (foreground)",
	Long:  "Serves org.freedesktop.Notifications on the container's session bus and forwards notifications to the host session bus.",
	RunE: func(cmd *cobra.Command, args []string) error {
		return runContainerBusProxy(cmd.Context(), "notify-proxy",
			func(cfg *config.Config) bool { return cfg.NotifyProxy }, notify.Run)
	},
}

func init() {
	rootCmd.AddCommand(notifyProxyCmd)
}
