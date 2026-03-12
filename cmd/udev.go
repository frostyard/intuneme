package cmd

import (
	"fmt"

	"github.com/frostyard/clix"
	"github.com/frostyard/intuneme/internal/config"
	"github.com/frostyard/intuneme/internal/nspawn"
	"github.com/frostyard/intuneme/internal/runner"
	"github.com/frostyard/intuneme/internal/udev"
	"github.com/spf13/cobra"
)

var udevCmd = &cobra.Command{
	Use:   "udev",
	Short: "Manage udev rules for USB security key forwarding",
}

var udevInstallCmd = &cobra.Command{
	Use:   "install",
	Short: "Install udev rules for YubiKey forwarding",
	Long: `Install udev rules and a helper script that automatically forward
Yubico USB security keys into the container when plugged in.

This is normally called automatically by 'intuneme start', but can be
run manually to set up rules without starting the container.`,
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

		if clix.DryRun {
			rep.Message("[dry-run] Would install udev rules to %s", udev.RulesPath())
			rep.Message("[dry-run] Would install helper script to %s", udev.ScriptPath())
			return nil
		}

		rep.Message("Checking sudo credentials...")
		if err := nspawn.ValidateSudo(r); err != nil {
			return fmt.Errorf("sudo authentication failed: %w", err)
		}

		if err := udev.Install(r, cfg.MachineName); err != nil {
			return fmt.Errorf("install udev rules: %w", err)
		}

		rep.Message("Installed udev rules for YubiKey forwarding.")
		if clix.Verbose {
			rep.Message("  Rules: %s", udev.RulesPath())
			rep.Message("  Script: %s", udev.ScriptPath())
			rep.Message("  Machine: %s", cfg.MachineName)
		}

		return nil
	},
}

var udevRemoveCmd = &cobra.Command{
	Use:   "remove",
	Short: "Remove udev rules for YubiKey forwarding",
	Long: `Remove udev rules and the helper script installed by 'intuneme udev install'
or 'intuneme start'. This is safe to run even if the rules are not installed
or the container is not running — it will clean up whatever it finds.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		r := &runner.SystemRunner{}

		if clix.DryRun {
			rep.Message("[dry-run] Would remove udev rules from %s", udev.RulesPath())
			rep.Message("[dry-run] Would remove helper script from %s", udev.ScriptPath())
			return nil
		}

		if !udev.IsInstalled() {
			rep.Message("No udev rules installed — nothing to remove.")
			return nil
		}

		rep.Message("Checking sudo credentials...")
		if err := nspawn.ValidateSudo(r); err != nil {
			return fmt.Errorf("sudo authentication failed: %w", err)
		}

		if err := udev.Remove(r); err != nil {
			return fmt.Errorf("remove udev rules: %w", err)
		}

		rep.Message("Removed udev rules for YubiKey forwarding.")

		return nil
	},
}

func init() {
	udevCmd.AddCommand(udevInstallCmd)
	udevCmd.AddCommand(udevRemoveCmd)
	rootCmd.AddCommand(udevCmd)
}
