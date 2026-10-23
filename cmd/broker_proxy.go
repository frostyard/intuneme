package cmd

import (
	"github.com/frostyard/intuneme/internal/broker"
	"github.com/frostyard/intuneme/internal/config"
	"github.com/spf13/cobra"
)

var brokerProxyCmd = &cobra.Command{
	Use:   "broker-proxy",
	Short: "Run the D-Bus broker proxy (foreground)",
	Long:  "Forwards com.microsoft.identity.broker1 from the container's session bus to the host session bus.",
	RunE: func(cmd *cobra.Command, args []string) error {
		return runBusProxy(cmd.Context(), "broker-proxy",
			func(cfg *config.Config) bool { return cfg.BrokerProxy }, broker.Run)
	},
}

func init() {
	rootCmd.AddCommand(brokerProxyCmd)
}
