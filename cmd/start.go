package cmd

import (
	"fmt"
	"os"
	"time"

	"github.com/frostyard/clix"
	"github.com/frostyard/intuneme/internal/broker"
	"github.com/frostyard/intuneme/internal/config"
	"github.com/frostyard/intuneme/internal/nspawn"
	"github.com/frostyard/intuneme/internal/nvidia"
	"github.com/frostyard/intuneme/internal/runner"
	"github.com/frostyard/intuneme/internal/sudoers"
	"github.com/frostyard/intuneme/internal/udev"
	"github.com/spf13/cobra"
)

var startCmd = &cobra.Command{
	Use:   "start",
	Short: "Boot the Intune container",
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

		if _, err := os.Stat(cfg.RootfsPath); err != nil {
			return fmt.Errorf("not initialized — run 'intuneme init' first")
		}

		if nspawn.IsRunning(r, cfg.MachineName) {
			rep.Message("Container %s is already running.", cfg.MachineName)
			rep.Message("Use 'intuneme shell' to connect.")
			return nil
		}

		home, err := os.UserHomeDir()
		if err != nil {
			return fmt.Errorf("cannot determine home directory: %w", err)
		}
		intuneHome := home + "/Intune"
		containerHome := fmt.Sprintf("/home/%s", cfg.HostUser)
		sockets := nspawn.DetectHostSockets(cfg.HostUID)

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

		// Detect Nvidia GPU and prepare bind mounts.
		var nvidiaDevices []nspawn.BindMount
		var nvidiaLibs []nvidia.LibMapping
		nvidiaEnabled := nvidia.IsPresent()
		if nvidiaEnabled {
			nvidiaDevices = nvidia.DetectDevices()
			ldconfigOut, err := r.Run("ldconfig", "-p")
			if err != nil {
				rep.Message("Warning: Nvidia detected but ldconfig failed: %v", err)
				nvidiaEnabled = false
			} else {
				nvidiaLibs = nvidia.HostLibraries(ldconfigOut)
				if len(nvidiaLibs) == 0 {
					rep.Message("Warning: Nvidia detected but no driver libraries found.")
					nvidiaEnabled = false
				} else {
					sockets = append(sockets, nvidia.LibDirMounts(nvidiaLibs)...)
					sockets = append(sockets, nvidia.ICDMounts(nvidia.HostICDFiles())...)
				}
			}
		}

		if clix.DryRun {
			rep.Message("[dry-run] Would boot container %s", cfg.MachineName)
			if nvidiaEnabled {
				rep.Message("[dry-run] Would configure Nvidia GPU with %d libraries", len(nvidiaLibs))
			}
			if cfg.BrokerProxy {
				rep.Message("[dry-run] Would enable linger and start broker proxy")
			}
			return nil
		}

		rep.Message("Checking sudo credentials...")
		if err := nspawn.ValidateSudo(r); err != nil {
			return fmt.Errorf("sudo authentication failed: %w", err)
		}

		display := nspawn.HostDisplay()
		if err := nspawn.WriteDisplayMarker(r, cfg.RootfsPath, display); err != nil {
			return fmt.Errorf("write display marker: %w", err)
		}

		rep.Message("Booting container...")
		if err := nspawn.Boot(r, cfg.RootfsPath, cfg.MachineName, intuneHome, containerHome, sockets, nvidiaDevices); err != nil {
			return fmt.Errorf("failed to start container: %w", err)
		}

		rep.Message("Waiting for container to boot...")
		for range 30 {
			if nspawn.IsRunning(r, cfg.MachineName) {
				break
			}
			time.Sleep(1 * time.Second)
		}

		if !nspawn.IsRunning(r, cfg.MachineName) {
			return fmt.Errorf("container failed to start within 30 seconds")
		}

		// Clean stale Nvidia symlinks from previous boots (rootfs persists).
		if err := nvidia.CleanStaleLinks(r, cfg.MachineName); err != nil {
			rep.Message("Warning: failed to clean stale Nvidia links: %v", err)
		}

		// Create Nvidia library symlinks inside container.
		if nvidiaEnabled {
			if err := nvidia.Setup(r, cfg.MachineName, nvidiaLibs); err != nil {
				rep.Message("Warning: Nvidia library setup failed: %v", err)
			} else if clix.Verbose {
				rep.Message("Nvidia GPU libraries configured.")
			}
		}

		// Install udev rules for device hotplug (enables future hotplug events).
		if err := udev.Install(r, cfg.MachineName); err != nil {
			rep.Message("Warning: failed to install udev rules (hotplug won't work): %v", err)
		} else if clix.Verbose {
			rep.Message("Installed udev hotplug rules.")
		}

		// Ensure the sudoers rule for passwordless app launch exists.
		// Normally installed by init; reinstall here if missing (upgrade
		// from older version, or manual deletion).
		if !sudoers.IsInstalled() {
			if err := sudoers.Install(r, cfg.HostUser); err != nil {
				rep.Message("Warning: failed to install sudoers rule (open commands will need sudo prompt): %v", err)
			} else if clix.Verbose {
				rep.Message("Installed passwordless nsenter rule.")
			}
		}

		// Forward already-plugged YubiKeys.
		yubikeys := udev.DetectYubikeys()
		for _, yk := range yubikeys {
			name := yk.Name
			if name == "" {
				name = "Yubico device"
			}
			forwarded := false
			for _, devnode := range yk.Devices() {
				if err := udev.ForwardDevice(r, cfg.MachineName, devnode); err != nil {
					rep.Message("Warning: failed to forward %s: %v", devnode, err)
				} else {
					forwarded = true
					if clix.Verbose {
						rep.Message("Forwarded %s (%s)", devnode, name)
					}
				}
			}
			if forwarded {
				rep.Message("Forwarded YubiKey: %s", name)
			}
		}

		// Forward already-connected video devices.
		videoDevs := udev.DetectVideoDevices()
		videoForwarded := 0
		for _, vd := range videoDevs {
			if err := udev.ForwardDevice(r, cfg.MachineName, vd.DevNode); err != nil {
				rep.Message("Warning: failed to forward %s: %v", vd.DevNode, err)
			} else {
				videoForwarded++
				if clix.Verbose {
					name := vd.Name
					if name == "" {
						name = vd.DevNode
					}
					rep.Message("Forwarded video device: %s (%s)", vd.DevNode, name)
				}
			}
		}
		if videoForwarded > 0 {
			rep.Message("Forwarded %d video device(s).", videoForwarded)
		} else if clix.Verbose {
			rep.Message("No video devices detected.")
		}

		if cfg.BrokerProxy {
			rep.Message("Enabling linger for container user...")
			if _, err := r.Run("machinectl", broker.EnableLingerArgs(cfg.MachineName, cfg.HostUser)...); err != nil {
				return fmt.Errorf("failed to enable linger: %w", err)
			}

			if clix.Verbose {
				rep.Message("Creating login session...")
			}
			if err := r.RunBackground("machinectl", broker.LoginSessionArgs(cfg.MachineName, cfg.HostUser)...); err != nil {
				return fmt.Errorf("failed to create login session: %w", err)
			}

			rep.Message("Waiting for container session bus...")
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

			if clix.Verbose {
				rep.Message("Starting broker proxy...")
			}
			execPath, err := os.Executable()
			if err != nil {
				return fmt.Errorf("failed to determine executable path: %w", err)
			}
			// Use setsid so the broker proxy gets its own session and survives
			// terminal closure (e.g. when started from the GNOME extension).
			if err := r.RunBackground("setsid", execPath, "broker-proxy", "--root", root); err != nil {
				return fmt.Errorf("failed to start broker proxy: %w", err)
			}
			time.Sleep(2 * time.Second)

			rep.Message("Container and broker proxy running.")
			rep.Message("Host apps can now use SSO via com.microsoft.identity.broker1.")
		} else {
			rep.Message("Container is running. Use 'intuneme shell' to connect.")
		}

		return nil
	},
}

func init() {
	rootCmd.AddCommand(startCmd)
}
