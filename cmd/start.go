package cmd

import (
	"fmt"
	"os"
	"time"

	"github.com/frostyard/intuneme/internal/broker"
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

		// When broker proxy is enabled, bind-mount a host directory to
		// /run/user/<uid> inside the container so the session bus socket
		// is accessible from the host.
		if cfg.BrokerProxy {
			runtimeDir := broker.RuntimeDir(root)
			if err := os.MkdirAll(runtimeDir, 0700); err != nil {
				return fmt.Errorf("create runtime dir: %w", err)
			}
			hostDir, containerDir := broker.RuntimeBindMount(root, cfg.HostUID)
			sockets = append(sockets, nspawn.BindMount{Host: hostDir, Container: containerDir})
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

		if cfg.BrokerProxy {
			fmt.Println("Enabling linger for container user...")
			if _, err := r.Run("machinectl", broker.EnableLingerArgs(cfg.MachineName, cfg.HostUser)...); err != nil {
				return fmt.Errorf("failed to enable linger: %w", err)
			}

			fmt.Println("Creating login session...")
			if err := r.RunBackground("machinectl", broker.LoginSessionArgs(cfg.MachineName, cfg.HostUser)...); err != nil {
				return fmt.Errorf("failed to create login session: %w", err)
			}

			fmt.Println("Waiting for container session bus...")
			busPath := broker.SessionBusSocketPath(root)
			busReady := false
			for range 30 {
				if _, err := os.Stat(busPath); err == nil {
					busReady = true
					break
				}
				time.Sleep(1 * time.Second)
			}
			if !busReady {
				return fmt.Errorf("container session bus not available after 30 seconds")
			}

			fmt.Println("Starting broker proxy...")
			execPath, err := os.Executable()
			if err != nil {
				return fmt.Errorf("failed to determine executable path: %w", err)
			}
			if err := r.RunBackground(execPath, "broker-proxy", "--root", root); err != nil {
				return fmt.Errorf("failed to start broker proxy: %w", err)
			}
			time.Sleep(2 * time.Second)
			fmt.Println("Broker proxy started.")

			fmt.Println("Container and broker proxy running.")
			fmt.Println("Host apps can now use SSO via com.microsoft.identity.broker1.")
		} else {
			fmt.Println("Container is running. Use 'intuneme shell' to connect.")
		}

		return nil
	},
}

func init() {
	rootCmd.AddCommand(startCmd)
}
