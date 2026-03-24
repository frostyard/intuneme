package cmd

import (
	"fmt"
	"os"
	"os/user"
	"path/filepath"

	"github.com/frostyard/clix"
	"github.com/frostyard/intuneme/internal/broker"
	"github.com/frostyard/intuneme/internal/config"
	"github.com/frostyard/intuneme/internal/nspawn"
	"github.com/frostyard/intuneme/internal/provision"
	"github.com/frostyard/intuneme/internal/puller"
	"github.com/frostyard/intuneme/internal/runner"
	pkgversion "github.com/frostyard/intuneme/internal/version"
	"github.com/spf13/cobra"
)

var insidersRecreate bool

var recreateCmd = &cobra.Command{
	Use:   "recreate",
	Short: "Recreate the container with a fresh image, preserving enrollment state",
	RunE: func(cmd *cobra.Command, args []string) error {
		r := &runner.SystemRunner{}
		root := rootDir
		if root == "" {
			root = config.DefaultRoot()
		}

		cfg, err := config.Load(root)
		if err != nil {
			return err
		}

		// Verify initialized
		if _, err := os.Stat(cfg.RootfsPath); err != nil {
			return fmt.Errorf("not initialized — run 'intuneme init' first")
		}

		u, err := user.Current()
		if err != nil {
			return fmt.Errorf("get current user: %w", err)
		}

		if clix.DryRun {
			rep.Message("[dry-run] Would stop container (if running)")
			rep.Message("[dry-run] Would backup shadow entry and broker state")
			rep.Message("[dry-run] Would remove old rootfs at %s", cfg.RootfsPath)
			rep.Message("[dry-run] Would pull new image and re-provision")
			return nil
		}

		// Validate sudo early
		rep.Message("Checking sudo credentials...")
		if err := nspawn.ValidateSudo(r); err != nil {
			return fmt.Errorf("sudo authentication failed: %w", err)
		}

		// Stop container if running
		if nspawn.IsRunning(r, cfg.MachineName) {
			if cfg.BrokerProxy {
				pidPath := filepath.Join(root, "broker-proxy.pid")
				broker.StopByPIDFile(pidPath)
				rep.Message("Broker proxy stopped.")
			}
			rep.Message("Stopping container...")
			if err := nspawn.Stop(r, cfg.MachineName); err != nil {
				return fmt.Errorf("failed to stop container: %w", err)
			}
			rep.Message("Container stopped.")
		}

		// Backup state
		if clix.Verbose {
			rep.Message("Backing up shadow entry...")
		}
		shadowLine, err := provision.BackupShadowEntry(r, cfg.RootfsPath, u.Username)
		if err != nil {
			return fmt.Errorf("backup shadow entry: %w", err)
		}

		if clix.Verbose {
			rep.Message("Backing up device broker state...")
		}
		brokerBackupDir, err := provision.BackupDeviceBrokerState(r, cfg.RootfsPath)
		if err != nil {
			return fmt.Errorf("backup device broker state: %w", err)
		}
		if brokerBackupDir != "" {
			defer func() { _ = os.RemoveAll(brokerBackupDir) }()
			if clix.Verbose {
				rep.Message("Device broker state backed up.")
			}
		} else {
			if clix.Verbose {
				rep.Message("No device broker state found (skipping).")
			}
		}

		// Remove old rootfs
		rep.Message("Removing old rootfs at %s...", cfg.RootfsPath)
		out, err := r.Run("sudo", "rm", "-rf", cfg.RootfsPath)
		if err != nil {
			return fmt.Errorf("rm rootfs failed: %w\n%s", err, out)
		}

		// Pull new image
		if cmd.Flags().Changed("insiders") {
			cfg.Insiders = insidersRecreate
			if err := cfg.Save(root); err != nil {
				return fmt.Errorf("save config: %w", err)
			}
		}
		image := pkgversion.ImageRef(cfg.Insiders)
		p, err := puller.Detect(r)
		if err != nil {
			return err
		}

		rep.Message("Pulling and extracting OCI image %s (via %s)...", image, p.Name())
		if err := os.MkdirAll(cfg.RootfsPath, 0755); err != nil {
			return fmt.Errorf("create rootfs dir: %w", err)
		}
		if err := p.PullAndExtract(r, image, cfg.RootfsPath); err != nil {
			return err
		}

		// Re-provision
		hostname, _ := os.Hostname()

		if err := provision.ProvisionContainer(r, rep, cfg.RootfsPath, u.Username, os.Getuid(), os.Getgid(), hostname); err != nil {
			return err
		}

		// Restore state
		if clix.Verbose {
			rep.Message("Restoring shadow entry...")
		}
		if err := provision.RestoreShadowEntry(r, cfg.RootfsPath, shadowLine); err != nil {
			return fmt.Errorf("restore shadow entry: %w", err)
		}

		if brokerBackupDir != "" {
			if clix.Verbose {
				rep.Message("Restoring device broker state...")
			}
			if err := provision.RestoreDeviceBrokerState(r, cfg.RootfsPath, brokerBackupDir); err != nil {
				rep.Warning("restore device broker state failed: %v", err)
			}
		}

		rep.Message("Container recreated. Run 'intuneme start' to boot.")
		return nil
	},
}

func init() {
	recreateCmd.Flags().BoolVar(&insidersRecreate, "insiders", false, "switch to the insiders channel container image")
	rootCmd.AddCommand(recreateCmd)
}
