package cmd

import (
	"fmt"
	"os"
	"os/user"
	"path/filepath"

	"github.com/frostyard/intuneme/internal/broker"
	"github.com/frostyard/intuneme/internal/config"
	"github.com/frostyard/intuneme/internal/nspawn"
	"github.com/frostyard/intuneme/internal/provision"
	"github.com/frostyard/intuneme/internal/puller"
	"github.com/frostyard/intuneme/internal/runner"
	pkgversion "github.com/frostyard/intuneme/internal/version"
	"github.com/spf13/cobra"
)

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

		// Validate sudo early
		fmt.Println("Checking sudo credentials...")
		if err := nspawn.ValidateSudo(r); err != nil {
			return fmt.Errorf("sudo authentication failed: %w", err)
		}

		// Stop container if running
		if nspawn.IsRunning(r, cfg.MachineName) {
			if cfg.BrokerProxy {
				pidPath := filepath.Join(root, "broker-proxy.pid")
				broker.StopByPIDFile(pidPath)
				fmt.Println("Broker proxy stopped.")
			}
			fmt.Println("Stopping container...")
			if err := nspawn.Stop(r, cfg.MachineName); err != nil {
				return fmt.Errorf("failed to stop container: %w", err)
			}
			fmt.Println("Container stopped.")
		}

		// Backup state
		fmt.Println("Backing up shadow entry...")
		shadowLine, err := provision.BackupShadowEntry(r, cfg.RootfsPath, u.Username)
		if err != nil {
			return fmt.Errorf("backup shadow entry: %w", err)
		}

		fmt.Println("Backing up device broker state...")
		brokerBackupDir, err := provision.BackupDeviceBrokerState(r, cfg.RootfsPath)
		if err != nil {
			return fmt.Errorf("backup device broker state: %w", err)
		}
		if brokerBackupDir != "" {
			defer func() { _ = os.RemoveAll(brokerBackupDir) }()
			fmt.Println("Device broker state backed up.")
		} else {
			fmt.Println("No device broker state found (skipping).")
		}

		// Remove old rootfs
		fmt.Printf("Removing old rootfs at %s...\n", cfg.RootfsPath)
		out, err := r.Run("sudo", "rm", "-rf", cfg.RootfsPath)
		if err != nil {
			return fmt.Errorf("rm rootfs failed: %w\n%s", err, out)
		}

		// Pull new image
		image := pkgversion.ImageRef()
		p, err := puller.Detect(r)
		if err != nil {
			return err
		}

		fmt.Printf("Pulling and extracting OCI image %s (via %s)...\n", image, p.Name())
		if err := os.MkdirAll(cfg.RootfsPath, 0755); err != nil {
			return fmt.Errorf("create rootfs dir: %w", err)
		}
		if err := p.PullAndExtract(r, image, cfg.RootfsPath); err != nil {
			return err
		}

		// Re-provision
		hostname, _ := os.Hostname()

		// Ensure container has a render group matching the host for GPU access
		if renderGID, renderErr := provision.FindHostRenderGID(); renderErr == nil && renderGID >= 0 {
			fmt.Println("Configuring GPU render group...")
			if err := provision.EnsureRenderGroup(r, cfg.RootfsPath, renderGID); err != nil {
				fmt.Fprintf(os.Stderr, "warning: render group setup failed: %v\n", err)
			}
		}

		fmt.Println("Creating container user...")
		if err := provision.CreateContainerUser(r, cfg.RootfsPath, u.Username, os.Getuid(), os.Getgid()); err != nil {
			return err
		}

		fmt.Println("Applying fixups...")
		if err := provision.WriteFixups(r, cfg.RootfsPath, u.Username, os.Getuid(), os.Getgid(), hostname+"LXC"); err != nil {
			return err
		}

		// Restore state
		fmt.Println("Restoring shadow entry...")
		if err := provision.RestoreShadowEntry(r, cfg.RootfsPath, shadowLine); err != nil {
			return fmt.Errorf("restore shadow entry: %w", err)
		}

		if brokerBackupDir != "" {
			fmt.Println("Restoring device broker state...")
			if err := provision.RestoreDeviceBrokerState(r, cfg.RootfsPath, brokerBackupDir); err != nil {
				fmt.Fprintf(os.Stderr, "warning: restore device broker state failed: %v\n", err)
			}
		}

		// Install polkit rules
		fmt.Println("Installing polkit rules...")
		if err := provision.InstallPolkitRule(r, "/etc/polkit-1/rules.d"); err != nil {
			fmt.Fprintf(os.Stderr, "warning: polkit install failed: %v\n", err)
		}

		fmt.Println("Container recreated. Run 'intuneme start' to boot.")
		return nil
	},
}

func init() {
	rootCmd.AddCommand(recreateCmd)
}
