package cmd

import (
	"fmt"
	"path/filepath"
	"time"

	"github.com/frostyard/clix"
	"github.com/frostyard/intuneme/internal/broker"
	"github.com/frostyard/intuneme/internal/config"
	"github.com/frostyard/intuneme/internal/nspawn"
	"github.com/frostyard/intuneme/internal/runner"
	"github.com/frostyard/intuneme/internal/udev"
	"github.com/spf13/cobra"
)

func runStop(r runner.Runner, root string, pollInterval time.Duration, maxAttempts int) error {
	cfg, err := config.Load(root)
	if err != nil {
		return err
	}

	if !nspawn.IsRunning(r, cfg.MachineName) {
		rep.Message("Container is not running.")
		return nil
	}

	if clix.DryRun {
		if cfg.BrokerProxy {
			rep.Message("[dry-run] Would stop broker proxy")
		}
		rep.Message("[dry-run] Would stop container %s", cfg.MachineName)
		return nil
	}

	// Stop broker proxy first so host apps get clean errors
	if cfg.BrokerProxy {
		pidPath := filepath.Join(root, "broker-proxy.pid")
		broker.StopByPIDFile(pidPath)
		rep.Message("Broker proxy stopped.")
	}

	// Remove udev rules for YubiKey forwarding.
	if udev.IsInstalled() {
		if err := udev.Remove(r); err != nil {
			rep.Message("Warning: failed to remove udev rules: %v", err)
		} else if clix.Verbose {
			rep.Message("Removed YubiKey udev rules.")
		}
	}

	rep.Message("Stopping container...")
	if err := nspawn.Stop(r, cfg.MachineName); err != nil {
		return err
	}

	// Wait for the container to fully deregister from systemd-machined.
	// machinectl poweroff returns before the machine is fully gone.
	for range maxAttempts {
		if !nspawn.IsRunning(r, cfg.MachineName) {
			rep.Message("Container stopped.")
			return nil
		}
		time.Sleep(pollInterval)
	}

	timeout := pollInterval * time.Duration(maxAttempts)
	return fmt.Errorf("container %s did not stop within %s", cfg.MachineName, timeout)
}

var stopCmd = &cobra.Command{
	Use:   "stop",
	Short: "Stop the container",
	RunE: func(cmd *cobra.Command, args []string) error {
		r := &runner.SystemRunner{}
		root := rootDir
		if root == "" {
			root = config.DefaultRoot()
		}
		return runStop(r, root, 500*time.Millisecond, 60)
	},
}

func init() {
	rootCmd.AddCommand(stopCmd)
}
