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
			}); printed || err != nil {
				return err
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
			brokerStatus = brokerProxyStatus(root)
		}

		notifyStatus := ""
		if cfg.NotifyProxy {
			notifyStatus = pidProxyStatus(root, "notify-proxy")
		}
		screencastStatus := ""
		if cfg.ScreencastProxy {
			screencastStatus = pidProxyStatus(root, "screencast-proxy")
		}

		channel := "stable"
		if cfg.Insiders {
			channel = "insiders"
		}

		if printed, err := clix.OutputJSON(map[string]any{
			"initialized":      true,
			"root":             root,
			"rootfs":           cfg.RootfsPath,
			"machine":          cfg.MachineName,
			"container":        containerStatus,
			"channel":          channel,
			"broker_proxy":     brokerStatus,
			"notify_proxy":     notifyStatus,
			"screencast_proxy": screencastStatus,
		}); printed || err != nil {
			return err
		}

		rep.MessagePlain("Root:    %s", root)
		rep.MessagePlain("Rootfs:  %s", cfg.RootfsPath)
		rep.MessagePlain("Machine: %s", cfg.MachineName)
		rep.MessagePlain("Container: %s", containerStatus)
		rep.MessagePlain("Channel: %s", channel)

		if cfg.BrokerProxy {
			rep.MessagePlain("Broker proxy: %s", brokerStatus)
		}
		if cfg.NotifyProxy {
			rep.MessagePlain("Notify proxy: %s", notifyStatus)
		}
		if cfg.ScreencastProxy {
			rep.MessagePlain("Screencast proxy: %s", screencastStatus)
		}

		return nil
	},
}

// findNameOwner is replaced in tests so they need no session bus.
var findNameOwner = broker.FindNameOwner

// brokerProxyStatus describes the broker proxy. It checks the PID file first.
// When the PID file shows no running proxy, it asks the host session bus who
// owns the broker bus name, so a D-Bus activated proxy or a foreign broker
// shows in the status. A proxy that serves another root is a conflicting
// instance, not this root's proxy, so it does not count as running.
// Callers must check cfg.BrokerProxy first: this function connects to the
// session bus.
// pidProxyStatus reports a proxy that "intuneme start" launches, from its
// PID file.
func pidProxyStatus(root, subcommand string) string {
	if pid, running := broker.IsRunningByPIDFile(filepath.Join(root, subcommand+".pid")); running {
		return fmt.Sprintf("running (PID %d)", pid)
	}
	return "not running"
}

func brokerProxyStatus(root string) string {
	pidPath := filepath.Join(root, "broker-proxy.pid")
	if pid, running := broker.IsRunningByPIDFile(pidPath); running {
		return fmt.Sprintf("running (PID %d)", pid)
	}
	owner, ok := findNameOwner()
	if !ok {
		return "not running"
	}
	if owner.IsProxy {
		if !owner.RootResolved {
			return fmt.Sprintf("not running (bus name owned by intuneme broker-proxy with unresolved root %q, PID %d)", owner.ProxyRoot, owner.PID)
		}
		ownerRoot := filepath.Clean(owner.ProxyRoot)
		if ownerRoot == resolveStatusPath(root) {
			return fmt.Sprintf("running (PID %d, D-Bus activated)", owner.PID)
		}
		return fmt.Sprintf("not running (bus name owned by intuneme broker-proxy for root %s, PID %d)", ownerRoot, owner.PID)
	}
	return fmt.Sprintf("not running (bus name owned by %s)", owner)
}

// resolveStatusPath makes a root path absolute and clean so two spellings of
// the same directory compare equal. It does not resolve symlinks.
func resolveStatusPath(path string) string {
	abs, err := filepath.Abs(path)
	if err != nil {
		return filepath.Clean(path)
	}
	return filepath.Clean(abs)
}

func init() {
	rootCmd.AddCommand(statusCmd)
}
