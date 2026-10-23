package cmd

import (
	"github.com/frostyard/intuneme/internal/config"
	"github.com/frostyard/intuneme/internal/screencast"
	"github.com/spf13/cobra"
)

var screencastProxyCmd = &cobra.Command{
	Use:   "screencast-proxy",
	Short: "Run the screen sharing proxy (foreground)",
	Long: "Takes over org.freedesktop.portal.Desktop on the container's session bus and forwards the " +
		"ScreenCast portal to the host portal, so apps in the container can share host screens and windows.",
	RunE: func(cmd *cobra.Command, args []string) error {
		return runBusProxy(cmd.Context(), "screencast-proxy",
			func(cfg *config.Config) bool { return cfg.ScreencastProxy }, screencast.Run)
	},
}

func init() {
	rootCmd.AddCommand(screencastProxyCmd)
}
