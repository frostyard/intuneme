package cmd

import (
	"context"
	"fmt"
	"path/filepath"

	"github.com/frostyard/intuneme/internal/busproxy"
	"github.com/frostyard/intuneme/internal/config"
)

// runBusProxy runs a bus proxy (broker, notify or screencast) in the
// foreground. It refuses to start when its PID file names a running instance,
// writes the PID file only once the proxy owns its bus name, and removes the
// file on exit only if it still holds this PID.
func runBusProxy(ctx context.Context, subcommand string, enabled func(*config.Config) bool,
	run func(ctx context.Context, root string, ready func() error) error) error {
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
	if !enabled(cfg) {
		return fmt.Errorf("%s is not enabled — run 'intuneme config %s enable' first", subcommand, subcommand)
	}

	pidPath := filepath.Join(root, subcommand+".pid")
	if err := busproxy.CheckNotRunning(pidPath, subcommand); err != nil {
		return err
	}
	defer busproxy.RemoveOwnPID(pidPath)

	return run(ctx, root, func() error {
		if err := busproxy.WritePID(pidPath); err != nil {
			return fmt.Errorf("write pid file: %w", err)
		}
		return nil
	})
}
