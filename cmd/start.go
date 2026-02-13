package cmd

import (
	"fmt"
	"os"
	"time"

	"github.com/bjk/intuneme/internal/config"
	"github.com/bjk/intuneme/internal/nspawn"
	"github.com/bjk/intuneme/internal/runner"
	"github.com/bjk/intuneme/internal/session"
	"github.com/spf13/cobra"
)

var startCmd = &cobra.Command{
	Use:   "start",
	Short: "Boot the container and launch intune-portal",
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

		// Check rootfs exists
		if _, err := os.Stat(cfg.RootfsPath); err != nil {
			return fmt.Errorf("not initialized — run 'intuneme init' first")
		}

		// Discover host session
		sess, err := session.Discover(cfg.HostUID)
		if err != nil {
			return fmt.Errorf("session discovery failed: %w", err)
		}

		// Check if already running
		if nspawn.IsRunning(r, cfg.MachineName) {
			fmt.Printf("Container %s is already running.\n", cfg.MachineName)
			fmt.Println("Launching intune-portal...")
			return nspawn.LaunchIntune(r, cfg.MachineName, cfg.HostUser, sess)
		}

		homeDir, _ := os.UserHomeDir()

		fmt.Println("Booting container...")
		go func() {
			nspawn.Boot(r, cfg.RootfsPath, cfg.MachineName, homeDir, sess)
		}()

		// Wait for container to be ready
		fmt.Println("Waiting for container to boot...")
		for range 30 {
			if nspawn.IsRunning(r, cfg.MachineName) {
				break
			}
			time.Sleep(1 * time.Second)
		}

		if !nspawn.IsRunning(r, cfg.MachineName) {
			return fmt.Errorf("container failed to start within 30 seconds")
		}

		fmt.Println("Launching intune-portal...")
		return nspawn.LaunchIntune(r, cfg.MachineName, cfg.HostUser, sess)
	},
}

func init() {
	rootCmd.AddCommand(startCmd)
}
