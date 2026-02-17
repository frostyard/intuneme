package cmd

import (
	"fmt"
	"os"
	"time"

	"github.com/frostyard/intuneme/internal/config"
	"github.com/frostyard/intuneme/internal/nspawn"
	"github.com/frostyard/intuneme/internal/runner"
	"github.com/spf13/cobra"
)

var startCmd = &cobra.Command{
	Use:   "start",
	Short: "Boot the Intune container",
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

		if _, err := os.Stat(cfg.RootfsPath); err != nil {
			return fmt.Errorf("not initialized — run 'intuneme init' first")
		}

		if nspawn.IsRunning(r, cfg.MachineName) {
			fmt.Printf("Container %s is already running.\n", cfg.MachineName)
			fmt.Println("Use 'intuneme shell' to connect.")
			return nil
		}

		home, _ := os.UserHomeDir()
		intuneHome := home + "/Intune"
		containerHome := fmt.Sprintf("/home/%s", cfg.HostUser)
		sockets := nspawn.DetectHostSockets(cfg.HostUID)

		videoDev := nspawn.DetectVideoDevices()
		if len(videoDev) > 0 {
			for _, d := range videoDev {
				if d.Name != "" {
					fmt.Printf("Detected webcam: %s (%s)\n", d.Mount.Host, d.Name)
				}
				sockets = append(sockets, d.Mount)
			}
		} else {
			fmt.Println("No webcams detected")
		}

		fmt.Println("Checking sudo credentials...")
		if err := nspawn.ValidateSudo(r); err != nil {
			return fmt.Errorf("sudo authentication failed: %w", err)
		}

		fmt.Println("Booting container...")
		if err := nspawn.Boot(r, cfg.RootfsPath, cfg.MachineName, intuneHome, containerHome, sockets); err != nil {
			return fmt.Errorf("failed to start container: %w", err)
		}

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

		fmt.Println("Container is running. Use 'intuneme shell' to connect.")
		return nil
	},
}

func init() {
	rootCmd.AddCommand(startCmd)
}
