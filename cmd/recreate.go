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
				return fmt.Errorf("Himmelblau domain is not configured; run 'intuneme init --force --himmelblau --himmelblau-domain <domain>': %w", err)
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
		// failure path. Only a successful restore may discard it.
		brokerRestored := false
		backupCleaned := false
		if brokerBackupDir != "" {
			defer func() {
				if backupCleaned {
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
		if err := p.PullAndExtract(r, image, cfg.RootfsPath, tmpDirRecreate); err != nil {
			return err
		}
		if err := puller.VerifyRootOwned(cfg.RootfsPath); err != nil {
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
						return fmt.Errorf("Himmelblau image is missing its terminal-passkey fork marker")
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
		hostname, _ := os.Hostname()

		if err := provision.ProvisionContainer(r, rep, cfg.RootfsPath, u.Username, os.Getuid(), os.Getgid(), hostname); err != nil {
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
				if err := provision.RemoveHimmelblauStateBackup(r, brokerBackupDir); err != nil {
					return fmt.Errorf("remove restored Himmelblau backup: %w", err)
				}
			} else {
				_ = os.RemoveAll(brokerBackupDir)
			}
			backupCleaned = true
		}

		rep.Message("Container recreated. Run 'intuneme start' to boot.")
		return nil
	},
}

func init() {
	recreateCmd.Flags().BoolVar(&insidersRecreate, "insiders", false, "switch the Microsoft image to its insiders channel (unavailable for Himmelblau)")
	recreateCmd.Flags().StringVar(&himmelblauLocalImageRecreate, "himmelblau-local-image", "", "use a locally built Himmelblau image for recreation")
	recreateCmd.Flags().StringVar(&tmpDirRecreate, "tmp-dir", "", "directory for temporary files during image extraction (default: system temp dir)")
	rootCmd.AddCommand(recreateCmd)
}
