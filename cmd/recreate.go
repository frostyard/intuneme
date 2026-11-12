package cmd

import (
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"strings"

	"github.com/frostyard/clix"
	"github.com/frostyard/intuneme/internal/broker"
	"github.com/frostyard/intuneme/internal/config"
	"github.com/frostyard/intuneme/internal/nspawn"
	"github.com/frostyard/intuneme/internal/provision"
	"github.com/frostyard/intuneme/internal/puller"
	"github.com/frostyard/intuneme/internal/runner"
	pkgversion "github.com/frostyard/intuneme/internal/version"
	"github.com/frostyard/std/reporter"
	"github.com/spf13/cobra"
)

var insidersRecreate bool
var tmpDirRecreate string
var skipSignatureRecreate bool

// newRecreateRunner is the runner recreate uses. A test replaces it to prove
// that a failed signature check runs no host command.
var newRecreateRunner = func() runner.Runner { return &runner.SystemRunner{} }

var recreateCmd = &cobra.Command{
	Use:   "recreate",
	Short: "Recreate the container with a fresh image, preserving enrollment state",
	RunE: func(cmd *cobra.Command, args []string) error {
		r := newRecreateRunner()
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

		// Resolve the container host name before the dry-run return, before
		// sudo, before stopping and before deleting the old rootfs, so a bad
		// hostname key can never destroy an existing container.
		hostHostname, _ := os.Hostname()
		containerHostname, err := cfg.ContainerHostname(hostHostname)
		if err != nil {
			return err
		}

		// Verify initialized
		if _, err := os.Stat(cfg.RootfsPath); err != nil {
			return fmt.Errorf("not initialized — run 'intuneme init' first")
		}

		// Warn, also in dry-run, when the new host name differs from the one
		// recorded in the current rootfs.
		warnIfHostnameChanges(rep, cfg.RootfsPath, containerHostname)

		u, err := user.Current()
		if err != nil {
			return fmt.Errorf("get current user: %w", err)
		}

		if clix.DryRun {
			rep.Message("[dry-run] Would check the image signature against ubuntu-intune/cosign.pub")
			rep.Message("[dry-run] Would stop container (if running)")
			rep.Message("[dry-run] Would backup shadow entry and broker state")
			rep.Message("[dry-run] Would remove old rootfs at %s", cfg.RootfsPath)
			rep.Message("[dry-run] Would pull new image and re-provision")
			rep.Message("[dry-run] Would set container hostname to %s", containerHostname)
			return nil
		}

		// Resolve the channel and check the image signature here, after the
		// dry-run return and before sudo, the stop, the backups and the delete.
		// An image that does not verify must leave the running container and its
		// enrollment untouched.
		insiders := cfg.Insiders
		if cmd.Flags().Changed("insiders") {
			insiders = insidersRecreate
		}
		image := pkgversion.ImageRef(insiders)
		pullRef, err := resolvePullRef(cmd.Context(), image, skipSignatureRecreate)
		if err != nil {
			return err
		}
		p, err := puller.Detect(r)
		if err != nil {
			return err
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
			if err := nspawn.StopAndWait(r, cfg.MachineName, nspawn.DefaultStopPoll, nspawn.DefaultStopAttempts); err != nil {
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
		// The backup is the only copy of the enrollment once the rootfs is
		// deleted below, so it is kept — and its location reported — on every
		// failure path. Only a successful restore may discard it.
		brokerRestored := false
		if brokerBackupDir != "" {
			defer func() {
				if brokerRestored {
					_ = os.RemoveAll(brokerBackupDir)
					return
				}
				rep.Warning("Device broker state was NOT restored. Enrollment backup kept at %s", brokerBackupDir)
			}()
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

		rep.Message("Pulling and extracting OCI image %s (via %s)...", pullRef, p.Name())
		if err := os.MkdirAll(cfg.RootfsPath, 0755); err != nil {
			return fmt.Errorf("create rootfs dir: %w", err)
		}
		if err := os.Chmod(cfg.RootfsPath, 0o755); err != nil {
			return fmt.Errorf("set initial rootfs mode: %w", err)
		}
		if err := p.PullAndExtract(r, pullRef, cfg.RootfsPath, tmpDirRecreate); err != nil {
			return err
		}
		if err := puller.VerifyRootOwned(cfg.RootfsPath); err != nil {
			return err
		}
		if err := puller.ReconcileRootfs(r, cfg.RootfsPath); err != nil {
			return err
		}

		// Re-provision
		if err := provision.ProvisionContainer(r, rep, cfg.RootfsPath, u.Username, os.Getuid(), os.Getgid(), containerHostname, cfg.MachineName); err != nil {
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
			} else {
				brokerRestored = true
			}
		}

		rep.Message("Container recreated. Run 'intuneme start' to boot.")
		return nil
	},
}

// containerHostnameFromRootfs returns the container host name recorded in
// <rootfs>/etc/hostname, trimmed of surrounding whitespace. It returns "" when
// the file cannot be read or holds nothing, because an unreadable or empty file
// must never block recreate.
func containerHostnameFromRootfs(rootfsPath string) string {
	data, err := os.ReadFile(filepath.Join(rootfsPath, "etc", "hostname"))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

// warnIfHostnameChanges warns when the rootfs about to be deleted carries a
// container host name other than newName. Intune shows the container host
// name as the device name, so such a change can surface in the tenant as a
// renamed or new device after the enrollment state is restored.
func warnIfHostnameChanges(rep reporter.Reporter, rootfsPath, newName string) {
	old := containerHostnameFromRootfs(rootfsPath)
	if old == "" || old == newName {
		return
	}
	rep.Warning("Container hostname changes from %q to %q. Intune can show this as a renamed or new device.", old, newName)
}

func init() {
	recreateCmd.Flags().BoolVar(&insidersRecreate, "insiders", false, "switch to the insiders channel container image")
	recreateCmd.Flags().StringVar(&tmpDirRecreate, "tmp-dir", "", "directory for temporary files during image extraction (default: system temp dir)")
	recreateCmd.Flags().BoolVar(&skipSignatureRecreate, "insecure-skip-signature-check", false, "skip the cosign signature check of the container image (not recommended)")
	rootCmd.AddCommand(recreateCmd)
}
