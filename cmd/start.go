package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/frostyard/clix"
	"github.com/frostyard/intuneme/internal/broker"
	"github.com/frostyard/intuneme/internal/config"
	"github.com/frostyard/intuneme/internal/nspawn"
	"github.com/frostyard/intuneme/internal/nvidia"
	"github.com/frostyard/intuneme/internal/provision"
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

		return runStart(r, root)
	},
}

func runStart(r runner.Runner, root string) error {
	cfg, err := config.Load(root)
	if err != nil {
		return err
	}

	if _, err := os.Stat(cfg.RootfsPath); err != nil {
		return fmt.Errorf("not initialized — run 'intuneme init' first")
	}

	if !clix.DryRun {
		if err := nspawn.ValidateSudo(r); err != nil {
			return fmt.Errorf("sudo authentication failed: %w", err)
		}
		if err := refreshStartSecurity(r, cfg); err != nil {
			return fmt.Errorf("security migration failed: %w", err)
		}
	}

	if nspawn.IsRunning(r, cfg.MachineName) {
		if !clix.DryRun {
			// Heals containers booted before linger was unconditional.
			if err := enableUserLinger(r, cfg); err != nil {
				return err
			}
		}
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

	// The proxies need the container's session bus from the host. When
	// one is enabled, bind-mount a host directory to /run/user/<uid>
	// inside the container so the session bus socket is accessible.
	sessionBus := cfg.BrokerProxy || cfg.NotifyProxy || cfg.ScreencastProxy
	if sessionBus {
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
		rep.Message("[dry-run] Would enable linger for the container user")
		if sessionBus {
			rep.Message("[dry-run] Would open a container login session")
		}
		if cfg.BrokerProxy {
			rep.Message("[dry-run] Would start broker proxy")
		}
		if cfg.NotifyProxy {
			rep.Message("[dry-run] Would start notify proxy")
		}
		if cfg.ScreencastProxy {
			rep.Message("[dry-run] Would start screencast proxy")
		}
		return nil
	}

	display := nspawn.HostDisplay()
	if err := nspawn.WriteDisplayMarker(r, cfg.RootfsPath, display); err != nil {
		return fmt.Errorf("write display marker: %w", err)
	}

	// Drop the container's /tmp/.X11-unix tmpfiles directive before boot.
	// Left in place it empties that directory during container startup,
	// and the directory is bind-mounted from the host, so the host loses
	// its Xwayland socket. Refuse to boot rather than break the host
	// session.
	if err := provision.OverrideX11Tmpfiles(r, cfg.RootfsPath); err != nil {
		return fmt.Errorf("override container x11 tmpfiles rule: %w", err)
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

	// Ensure the container user is in groups that depend on packages
	// installed inside the image (currently plugdev for pcscd access).
	// Self-heals containers provisioned before plugdev was in baseGroups.
	// EnsureUserGroups returns the list of added groups even on error
	// (partial-success contract), so log added groups regardless of err.
	added, err := provision.EnsureUserGroups(r, cfg.MachineName, cfg.HostUser)
	for _, g := range added {
		rep.Message("Added %s to %s group", cfg.HostUser, g)
	}
	if err != nil {
		rep.Message("Warning: failed to reconcile user groups (smartcards may not work): %v", err)
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

	rep.Message("Enabling linger for container user...")
	if err := enableUserLinger(r, cfg); err != nil {
		return err
	}

	if !sessionBus {
		rep.Message("Container is running. Use 'intuneme shell' to connect.")
		return nil
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

	if cfg.BrokerProxy {
		if err := startProxy(r, root, "broker-proxy"); err != nil {
			return err
		}
		rep.Message("Broker proxy running.")
		rep.Message("Host apps can now use SSO via com.microsoft.identity.broker1.")
	}
	if cfg.NotifyProxy {
		if err := startProxy(r, root, "notify-proxy"); err != nil {
			return err
		}
		rep.Message("Notify proxy running.")
		rep.Message("Container desktop notifications now show on the host.")
	}
	if cfg.ScreencastProxy {
		if err := startProxy(r, root, "screencast-proxy"); err != nil {
			return err
		}
		rep.Message("Screencast proxy running.")
		rep.Message("Apps in the container can now share host screens and windows.")
	}
	rep.Message("Container is running. Use 'intuneme shell' to connect.")

	return nil
}

// startProxy launches the proxy subcommand in the background and waits for
// its PID file. The PID file is <root>/<subcommand>.pid.
// enableUserLinger makes the container start the user's systemd manager (and
// so /run/user/<uid> and the session bus) without a login. The nsenter launch
// path used by `intuneme open` and the GNOME extension never creates a logind
// session, so without linger an app launched first after boot finds no session
// bus: the D-Bus activation environment can't be set and the keyring can't be
// unlocked. Idempotent; linger persists in the rootfs.
func enableUserLinger(r runner.Runner, cfg *config.Config) error {
	if _, err := r.Run("sudo", append([]string{"machinectl"}, broker.EnableLingerArgs(cfg.MachineName, cfg.HostUser)...)...); err != nil {
		return fmt.Errorf("failed to enable linger: %w", err)
	}
	return nil
}

func startProxy(r runner.Runner, root, subcommand string) error {
	if clix.Verbose {
		rep.Message("Starting %s...", subcommand)
	}
	execPath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("failed to determine executable path: %w", err)
	}
	// Pass an absolute root: "intuneme status" reads it back from the
	// proxy's command line, and a relative root would depend on the
	// proxy's working directory.
	proxyRoot, err := filepath.Abs(root)
	if err != nil {
		return fmt.Errorf("resolve root path: %w", err)
	}
	// Use setsid so the proxy gets its own session and survives terminal
	// closure (e.g. when started from the GNOME extension).
	if err := r.RunBackground("setsid", execPath, subcommand, "--root", proxyRoot); err != nil {
		return fmt.Errorf("failed to start %s: %w", subcommand, err)
	}
	pidPath := filepath.Join(root, subcommand+".pid")
	for range 10 {
		if _, alive := broker.IsRunningByPIDFile(pidPath); alive {
			return nil
		}
		time.Sleep(500 * time.Millisecond)
	}
	return fmt.Errorf("%s failed to start within 5 seconds", subcommand)
}

func init() {
	rootCmd.AddCommand(startCmd)
}

// Run before boot and before the already-running return. A failed security
// migration must never be reported as a successful start.
var installLaunchHelpers = sudoers.Install

func refreshStartSecurity(r runner.Runner, cfg *config.Config) error {
	// Revoke the old broad host grant before installing or replacing helpers.
	if err := provision.InstallPolkitRule(r, "/etc/polkit-1/rules.d", cfg.MachineName, cfg.HostUser, cfg.HostUID); err != nil {
		return err
	}
	if err := installLaunchHelpers(r, sudoers.Options{
		User: cfg.HostUser, Machine: cfg.MachineName, UID: cfg.HostUID, GID: os.Getgid(),
		Home: fmt.Sprintf("/home/%s", cfg.HostUser), Rootfs: cfg.RootfsPath,
	}); err != nil {
		sudoers.Remove(r)
		return fmt.Errorf("install trusted helpers (install intuneme in a root-owned path): %w", err)
	}

	if err := provision.InstallSessionScripts(r, cfg.RootfsPath); err != nil {
		return err
	}
	return provision.EnsureContainerSudoers(r, cfg.RootfsPath, cfg.HostUser)
}
