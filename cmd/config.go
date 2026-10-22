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

// dbusServiceContent builds the D-Bus service file content. The root is made
// absolute first: dbus-daemon resolves a relative path against its own working
// directory, not the one of the user who ran "config broker-proxy enable".
func dbusServiceContent(execPath, root string) (string, error) {
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return "", fmt.Errorf("resolve root path: %w", err)
	}
	return broker.DBusServiceFileContent(execPath, absRoot)
}

var brokerProxyEnableCmd = &cobra.Command{
	Use:   "enable",
	Short: "Enable the host-side broker proxy",
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
		content, err := dbusServiceContent(execPath, root)
		if err != nil {
			return err
		}
		if err := os.WriteFile(svcPath, []byte(content), 0644); err != nil {
			return fmt.Errorf("write dbus service file: %w", err)
		}

		rep.Message("Broker proxy enabled.")
		rep.Message("D-Bus activation file installed: %s", svcPath)
		rep.Message("The proxy will start automatically on next 'intuneme start',")
		rep.Message("or when a host app calls the broker.")
		return nil
	},
}

var brokerProxyDisableCmd = &cobra.Command{
	Use:   "disable",
	Short: "Disable the host-side broker proxy",
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

		rep.Message("Broker proxy disabled.")
		return nil
	},
}

// containerBusProxyConfigCmd builds "config <subcommand>" with enable and
// disable subcommands for a proxy that serves a name on the container's
// session bus. Such a proxy has no D-Bus activation file: "intuneme start"
// launches it, so enable only sets the config flag.
func containerBusProxyConfigCmd(subcommand, label, short, enableShort, disableShort string,
	set func(cfg *config.Config, enabled bool)) *cobra.Command {
	setEnabled := func(enabled bool) (string, error) {
		root := rootDir
		if root == "" {
			var err error
			root, err = config.DefaultRoot()
			if err != nil {
				return "", err
			}
		}
		cfg, err := config.Load(root)
		if err != nil {
			return "", err
		}
		set(cfg, enabled)
		if err := cfg.Save(root); err != nil {
			return "", fmt.Errorf("save config: %w", err)
		}
		return root, nil
	}

	parent := &cobra.Command{Use: subcommand, Short: short}
	parent.AddCommand(&cobra.Command{
		Use:   "enable",
		Short: enableShort,
		RunE: func(cmd *cobra.Command, args []string) error {
			if _, err := setEnabled(true); err != nil {
				return err
			}
			rep.Message("%s enabled.", label)
			rep.Message("The proxy starts on the next 'intuneme start'.")
			rep.Message("If the container is running, run 'intuneme stop' and then 'intuneme start'.")
			rep.Message("Apps opened before the proxy runs, such as Edge, must be restarted to use it.")
			return nil
		},
	})
	parent.AddCommand(&cobra.Command{
		Use:   "disable",
		Short: disableShort,
		RunE: func(cmd *cobra.Command, args []string) error {
			root, err := setEnabled(false)
			if err != nil {
				return err
			}
			broker.StopByPIDFile(filepath.Join(root, subcommand+".pid"))
			rep.Message("%s disabled.", label)
			return nil
		},
	})
	return parent
}

var notifyProxyConfigCmd = containerBusProxyConfigCmd("notify-proxy", "Notify proxy",
	"Manage desktop notification proxy configuration",
	"Forward container desktop notifications to the host",
	"Stop forwarding container desktop notifications",
	func(cfg *config.Config, enabled bool) { cfg.NotifyProxy = enabled })

func init() {
	configCmd.AddCommand(notifyProxyConfigCmd)
	brokerProxyConfigCmd.AddCommand(brokerProxyEnableCmd)
	brokerProxyConfigCmd.AddCommand(brokerProxyDisableCmd)
	configCmd.AddCommand(brokerProxyConfigCmd)
	rootCmd.AddCommand(configCmd)
}
