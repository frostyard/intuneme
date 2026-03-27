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
	"github.com/frostyard/intuneme/internal/sudoers"
	"github.com/frostyard/intuneme/internal/udev"
	"github.com/spf13/cobra"
)

var destroyAll bool

var destroyCmd = &cobra.Command{
	Use:   "destroy",
	Short: "Remove the container rootfs and all state",
	Long: `Remove the container rootfs and all state.

By default, removes the rootfs, udev rules, polkit rule, sudoers rule, and
Intune enrollment state from ~/Intune. Other files in ~/Intune (Downloads,
Edge profile) are preserved.

With --all, additionally removes the GNOME extension, polkit policy action,
D-Bus broker service file, the ~/Intune directory, and the intuneme data root
directory (default ~/.local/share/intuneme) entirely — a full uninstall of all
intuneme artifacts.`,
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

		if clix.DryRun {
			if cfg.BrokerProxy {
				rep.Message("[dry-run] Would stop broker proxy")
			}
			if nspawn.IsRunning(r, cfg.MachineName) {
				rep.Message("[dry-run] Would stop container %s", cfg.MachineName)
			}
			rep.Message("[dry-run] Would remove udev rules")
			rep.Message("[dry-run] Would remove polkit rule")
			rep.Message("[dry-run] Would remove rootfs at %s", cfg.RootfsPath)
			if destroyAll {
				rep.Message("[dry-run] Would disable and remove GNOME extension")
				rep.Message("[dry-run] Would remove polkit policy action")
				rep.Message("[dry-run] Would remove D-Bus broker service file")
				rep.Message("[dry-run] Would remove ~/Intune entirely")
				rep.Message("[dry-run] Would remove %s entirely", root)
			} else {
				rep.Message("[dry-run] Would clean Intune state from ~/Intune")
			}
			return nil
		}

		// Stop broker proxy first so host apps get clean errors.
		if cfg.BrokerProxy {
			pidPath := filepath.Join(root, "broker-proxy.pid")
			broker.StopByPIDFile(pidPath)
		}

		// Stop if running
		if nspawn.IsRunning(r, cfg.MachineName) {
			rep.Message("Stopping running container...")
			if err := nspawn.Stop(r, cfg.MachineName); err != nil {
				return fmt.Errorf("failed to stop container: %w", err)
			}
		}

		// Remove udev rules and hotplug artifacts.
		if err := udev.Remove(r); err != nil {
			rep.Message("Warning: failed to remove udev rules: %v", err)
		}

		// Remove polkit rule installed by init.
		if _, err := r.Run("sudo", "rm", "-f", "/etc/polkit-1/rules.d/50-intuneme.rules"); err != nil {
			rep.Message("Warning: failed to remove polkit rule: %v", err)
		}

		// Remove sudoers rule installed by init.
		sudoers.Remove(r)

		// Remove rootfs with sudo (owned by root after nspawn use)
		rep.Message("Removing %s...", root)
		out, err := r.Run("sudo", "rm", "-rf", cfg.RootfsPath)
		if err != nil {
			return fmt.Errorf("rm rootfs failed: %w\n%s", err, out)
		}

		// Remove config
		_ = os.Remove(fmt.Sprintf("%s/config.toml", root))

		// Clean intune state from ~/Intune (persists via bind mount)
		home, err := os.UserHomeDir()
		if err != nil {
			return fmt.Errorf("cannot determine home directory: %w", err)
		}
		if !filepath.IsAbs(home) {
			return fmt.Errorf("home directory is not an absolute path: %q", home)
		}
		intuneHome := filepath.Join(home, "Intune")

		if destroyAll {
			// --all: remove all intuneme artifacts from the host.

			// Disable and remove GNOME extension (best-effort: GNOME may not be running).
			_ = r.RunAttached("gnome-extensions", "disable", extensionUUID)
			extDir := filepath.Join(home, ".local", "share", "gnome-shell", "extensions", extensionUUID)
			if err := os.RemoveAll(extDir); err != nil {
				rep.Message("Warning: failed to remove GNOME extension: %v", err)
			}

			// Remove polkit policy action installed by extension install.
			if _, err := r.Run("sudo", "rm", "-f", "/etc/polkit-1/actions/org.frostyard.intuneme.policy"); err != nil {
				rep.Message("Warning: failed to remove polkit policy action: %v", err)
			}

			// Remove D-Bus service activation file.
			dbusPath := broker.DBusServiceFilePath()
			if err := os.Remove(dbusPath); err != nil && !os.IsNotExist(err) {
				rep.Message("Warning: failed to remove D-Bus service file: %v", err)
			}

			// Remove ~/Intune entirely.
			if err := os.RemoveAll(intuneHome); err != nil {
				rep.Message("Warning: failed to remove %s: %v", intuneHome, err)
			}

			// Remove data root directory entirely (rootfs already removed with sudo above).
			if err := os.RemoveAll(root); err != nil {
				rep.Message("Warning: failed to remove %s: %v", root, err)
			}
		} else {
			staleStateDirs := []string{
				filepath.Join(intuneHome, ".config", "intune"),
				filepath.Join(intuneHome, ".local", "share", "intune"),
				filepath.Join(intuneHome, ".local", "share", "intune-portal"),
				filepath.Join(intuneHome, ".local", "share", "keyrings"),
				filepath.Join(intuneHome, ".local", "state", "microsoft-identity-broker"),
				filepath.Join(intuneHome, ".cache", "intune-portal"),
			}
			for _, dir := range staleStateDirs {
				if _, err := os.Stat(dir); err == nil {
					if clix.Verbose {
						rep.Message("Cleaning %s...", dir)
					}
					_ = os.RemoveAll(dir)
				}
			}
		}

		rep.Message("Destroyed.")
		return nil
	},
}

func init() {
	destroyCmd.Flags().BoolVar(&destroyAll, "all", false, "remove all intuneme artifacts including GNOME extension, D-Bus service, and ~/Intune")
	rootCmd.AddCommand(destroyCmd)
}
