package cmd

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/frostyard/intuneme/internal/broker"
	"github.com/frostyard/intuneme/internal/config"
	"github.com/spf13/cobra"
)

var configCmd = &cobra.Command{
	Use:   "config",
	Short: "Manage intuneme configuration",
}

var brokerProxyConfigCmd = &cobra.Command{
	Use:   "broker-proxy",
	Short: "Manage broker proxy configuration",
}

var brokerProxyEnableCmd = &cobra.Command{
	Use:   "enable",
	Short: "Enable the host-side broker proxy",
	RunE: func(cmd *cobra.Command, args []string) error {
		root := rootDir
		if root == "" {
			root = config.DefaultRoot()
		}

		cfg, err := config.Load(root)
		if err != nil {
			return err
		}

		cfg.BrokerProxy = true
		if err := cfg.Save(root); err != nil {
			return fmt.Errorf("save config: %w", err)
		}

		execPath, err := os.Executable()
		if err != nil {
			return fmt.Errorf("resolve executable path: %w", err)
		}

		svcPath := broker.DBusServiceFilePath()
		if err := os.MkdirAll(filepath.Dir(svcPath), 0755); err != nil {
			return fmt.Errorf("create dbus services dir: %w", err)
		}
		content := broker.DBusServiceFileContent(execPath)
		if err := os.WriteFile(svcPath, []byte(content), 0644); err != nil {
			return fmt.Errorf("write dbus service file: %w", err)
		}

		fmt.Println("Broker proxy enabled.")
		fmt.Printf("D-Bus activation file installed: %s\n", svcPath)
		fmt.Println("The proxy will start automatically on next 'intuneme start',")
		fmt.Println("or when a host app calls the broker.")
		return nil
	},
}

var brokerProxyDisableCmd = &cobra.Command{
	Use:   "disable",
	Short: "Disable the host-side broker proxy",
	RunE: func(cmd *cobra.Command, args []string) error {
		root := rootDir
		if root == "" {
			root = config.DefaultRoot()
		}

		cfg, err := config.Load(root)
		if err != nil {
			return err
		}

		cfg.BrokerProxy = false
		if err := cfg.Save(root); err != nil {
			return fmt.Errorf("save config: %w", err)
		}

		svcPath := broker.DBusServiceFilePath()
		if err := os.Remove(svcPath); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return fmt.Errorf("remove dbus service file: %w", err)
		}

		pidPath := filepath.Join(root, "broker-proxy.pid")
		broker.StopByPIDFile(pidPath)

		fmt.Println("Broker proxy disabled.")
		return nil
	},
}

func init() {
	brokerProxyConfigCmd.AddCommand(brokerProxyEnableCmd)
	brokerProxyConfigCmd.AddCommand(brokerProxyDisableCmd)
	configCmd.AddCommand(brokerProxyConfigCmd)
	rootCmd.AddCommand(configCmd)
}
