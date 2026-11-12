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
var himmelblauLocalImageRecreate string

var recreateCmd = &cobra.Command{
	Use:   "recreate",
	Short: "Recreate the container with a fresh image, preserving enrollment state",
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

		// Resolve the container host name before the dry-run return, before
		// sudo, before stopping and before deleting the old rootfs, so a bad
		// hostname key can never destroy an existing container.
		hostHostname, _ := os.Hostname()
		containerHostname, err := cfg.ContainerHostname(hostHostname)
		if err != nil {
			return err
		}

		if cmd.Flags().Changed("himmelblau-local-image") {
			if !cfg.Himmelblau {
				return fmt.Errorf("--himmelblau-local-image requires Himmelblau mode")
			}
			if err := config.ValidateHimmelblauLocalImage(himmelblauLocalImageRecreate); err != nil {
				return err
			}
			cfg.HimmelblauLocalImage = himmelblauLocalImageRecreate
			if err := cfg.Save(root); err != nil {
				return fmt.Errorf("save local Himmelblau image selection: %w", err)
			}
		}
		if err := requireLocalTerminalPasskeyImage(cfg.HimmelblauTerminalPasskey, cfg.HimmelblauLocalImage); err != nil {
			return err
		}

		if cfg.Himmelblau && cmd.Flags().Changed("insiders") {
			return fmt.Errorf("--insiders is not available for the experimental Himmelblau image")
		}
		if cfg.Himmelblau {
			cfg.HimmelblauDomain, err = config.NormalizeHimmelblauDomain(cfg.HimmelblauDomain)
			if err != nil {
				return fmt.Errorf("himmelblau domain is not configured; run 'intuneme init --force --himmelblau --himmelblau-domain <domain>': %w", err)
			}
			if cfg.HimmelblauUPN != "" {
				if err := validateHimmelblauUPNDomain(cfg.HimmelblauUPN, cfg.HimmelblauDomain); err != nil {
					return fmt.Errorf("configured Himmelblau UPN does not match the sign-in domain: %w", err)
				}
			}
			if cfg.HimmelblauLocalImage != "" {
				if err := config.ValidateHimmelblauLocalImage(cfg.HimmelblauLocalImage); err != nil {
					return err
				}
			}
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
			rep.Message("[dry-run] Would stop container (if running)")
			stateName := "device broker"
			if cfg.Himmelblau {
				stateName = "Himmelblau"
			}
			rep.Message("[dry-run] Would backup shadow entry and %s state", stateName)
			rep.Message("[dry-run] Would remove old rootfs at %s", cfg.RootfsPath)
			rep.Message("[dry-run] Would pull new image and re-provision")
			rep.Message("[dry-run] Would set container hostname to %s", containerHostname)
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

		stateName := "device broker"
		backupState := provision.BackupDeviceBrokerState
		if cfg.Himmelblau {
			stateName = "Himmelblau"
			backupState = provision.BackupHimmelblauState
		}
		if clix.Verbose {
			rep.Message("Backing up %s state...", stateName)
		}
		brokerBackupDir, err := backupState(r, cfg.RootfsPath)
		if err != nil {
			return fmt.Errorf("backup %s state: %w", stateName, err)
		}
		// The backup is the only copy of the enrollment once the rootfs is
		// deleted below, so it is kept — and its location reported — on every
		// failure path. Himmelblau keeps it through the first boot as well.
		brokerRestored := false
		backupHandled := false
		if brokerBackupDir != "" {
			defer func() {
				if backupHandled {
					return
				}
				if brokerRestored {
					rep.Warning("%s state was restored, but its backup could not be cleaned. Backup kept at %s", stateName, brokerBackupDir)
					return
				}
				rep.Warning("%s state was NOT restored. Enrollment backup kept at %s", stateName, brokerBackupDir)
			}()
			if clix.Verbose {
				rep.Message("%s state backed up.", stateName)
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
		image := pkgversion.ImageRef(cfg.Insiders, cfg.Himmelblau)
		if cfg.HimmelblauLocalImage != "" {
			image = cfg.HimmelblauLocalImage
		}
		p, err := puller.DetectForImage(r, image)
		if err != nil {
			return err
		}

		rep.Message("Pulling and extracting OCI image %s (via %s)...", image, p.Name())
		if err := os.MkdirAll(cfg.RootfsPath, 0755); err != nil {
			return fmt.Errorf("create rootfs dir: %w", err)
		}
		if err := os.Chmod(cfg.RootfsPath, 0o755); err != nil {
			return fmt.Errorf("set initial rootfs mode: %w", err)
		}
		if err := p.PullAndExtract(r, image, cfg.RootfsPath, tmpDirRecreate); err != nil {
			return err
		}
		if err := puller.VerifyRootOwned(cfg.RootfsPath); err != nil {
			return err
		}
		if err := puller.ReconcileRootfs(r, cfg.RootfsPath); err != nil {
			return err
		}
		if cfg.Himmelblau {
			if err := provision.VerifyHimmelblauPAMTester(cfg.RootfsPath); err != nil {
				return err
			}
			if cfg.HimmelblauLocalImage != "" || cfg.HimmelblauTerminalPasskey {
				source, found, err := provision.ReadHimmelblauTerminalPasskeySource(cfg.RootfsPath)
				if err != nil {
					return err
				}
				if !found {
					if cfg.HimmelblauTerminalPasskey {
						return fmt.Errorf("himmelblau image is missing its terminal-passkey fork marker")
					}
				}
				if found {
					changed := !cfg.HimmelblauTerminalPasskey || cfg.HimmelblauTerminalPasskeySource != source
					cfg.HimmelblauTerminalPasskey = true
					cfg.HimmelblauTerminalPasskeySource = source
					if changed {
						if err := cfg.Save(root); err != nil {
							return fmt.Errorf("save terminal-passkey source commit: %w", err)
						}
					}
				} else {
					cfg.HimmelblauTerminalPasskey = false
					cfg.HimmelblauTerminalPasskeySource = ""
				}
			}
			if err := provision.ConfigureHimmelblauDomain(r, cfg.RootfsPath, cfg.HimmelblauDomain); err != nil {
				return err
			}
			if err := installSavedHimmelblauConfig(r, root, cfg.RootfsPath, cfg.HimmelblauDomain); err != nil {
				return err
			}
		}

		// Re-provision
		if err := provision.ProvisionContainer(r, rep, cfg.RootfsPath, u.Username, os.Getuid(), os.Getgid(), containerHostname, cfg.MachineName); err != nil {
			return err
		}
		if cfg.Himmelblau {
			if err := provision.EnsureHimmelblauPrimaryGroupName(r, cfg.RootfsPath, u.Username); err != nil {
				return err
			}
			if cfg.HimmelblauUPN != "" {
				if err := provision.InstallHimmelblauUserMap(r, cfg.RootfsPath, u.Username, cfg.HimmelblauUPN); err != nil {
					return err
				}
			}
			if err := provision.InstallHimmelblauPAMAllowlist(r, cfg.RootfsPath, u.Username, cfg.HimmelblauUPN != ""); err != nil {
				return err
			}
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
				rep.Message("Restoring %s state...", stateName)
			}
			restoreState := provision.RestoreDeviceBrokerState
			if cfg.Himmelblau {
				restoreState = provision.RestoreHimmelblauState
			}
			if err := restoreState(r, cfg.RootfsPath, brokerBackupDir); err != nil {
				rep.Warning("restore %s state failed: %v", stateName, err)
				if cfg.Himmelblau {
					return fmt.Errorf("restore Himmelblau state: %w", err)
				}
			} else {
				brokerRestored = true
			}
		}

		if brokerBackupDir != "" && brokerRestored {
			if cfg.Himmelblau {
				rep.Message("Himmelblau recovery backup kept at %s. Keep it until normal PIN login works after boot; it contains sensitive credentials.", brokerBackupDir)
			} else {
				_ = os.RemoveAll(brokerBackupDir)
			}
			backupHandled = true
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
	recreateCmd.Flags().BoolVar(&insidersRecreate, "insiders", false, "switch the Microsoft image to its insiders channel (unavailable for Himmelblau)")
	recreateCmd.Flags().StringVar(&himmelblauLocalImageRecreate, "himmelblau-local-image", "", "use a locally built Himmelblau image for recreation")
	recreateCmd.Flags().StringVar(&tmpDirRecreate, "tmp-dir", "", "directory for temporary files during image extraction (default: system temp dir)")
	rootCmd.AddCommand(recreateCmd)
}
