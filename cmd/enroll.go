package cmd

import (
	"bufio"
	"fmt"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/charmbracelet/x/term"
	"github.com/frostyard/intuneme/internal/bluezproxy"
	"github.com/frostyard/intuneme/internal/broker"
	"github.com/frostyard/intuneme/internal/config"
	"github.com/frostyard/intuneme/internal/nspawn"
	"github.com/frostyard/intuneme/internal/provision"
	"github.com/frostyard/intuneme/internal/runner"
	"github.com/spf13/cobra"
)

var himmelblauUser string

var himmelblauUPNPattern = regexp.MustCompile(`^[A-Za-z0-9._%+-]+@[A-Za-z0-9][A-Za-z0-9.-]*[A-Za-z0-9]$`)

var enrollCmd = &cobra.Command{
	Use:   "enroll",
	Short: "Map the local account to an Entra UPN and start Himmelblau PAM enrollment",
	Long: `Write a Himmelblau user-map entry linking the host-matched local
container account to an Entra UPN. If the container is stopped, start it with
the new map; if it is already running, reboot it so Himmelblau reloads the map.
Then start an interactive PAM login as that local account. Himmelblau performs
first-login enrollment during PAM authentication; this command does not use a
separate join API.`,
	RunE: func(cmd *cobra.Command, args []string) error {
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
		if !cfg.Himmelblau {
			return fmt.Errorf("Himmelblau mode is not enabled — run 'intuneme init --himmelblau' first")
		}
		if _, err := os.Stat(cfg.RootfsPath); err != nil {
			return fmt.Errorf("not initialized — run 'intuneme init --himmelblau' first")
		}
		if err := provision.VerifyHimmelblauPAMTester(cfg.RootfsPath); err != nil {
			return err
		}
		if cfg.HimmelblauTerminalPasskey {
			if err := provision.VerifyHimmelblauTerminalPasskey(cfg.RootfsPath, cfg.HimmelblauTerminalPasskeySource); err != nil {
				return err
			}
			if !term.IsTerminal(os.Stdin.Fd()) || !term.IsTerminal(os.Stdout.Fd()) || !term.IsTerminal(os.Stderr.Fd()) {
				return fmt.Errorf("terminal passkey enrollment requires an interactive terminal on stdin, stdout, and stderr")
			}
		}

		r := &runner.SystemRunner{}
		containerRunning := nspawn.IsRunning(r, cfg.MachineName)

		upn := himmelblauUser
		if upn == "" {
			upn, err = promptHimmelblauUPN()
			if err != nil {
				return err
			}
		}
		if err := validateHimmelblauUPN(upn); err != nil {
			return err
		}
		upn = strings.ToLower(upn)
		if err := validateHimmelblauUPNDomain(upn, cfg.HimmelblauDomain); err != nil {
			return err
		}
		if cfg.HimmelblauUPN != "" && cfg.HimmelblauUPN != upn {
			return fmt.Errorf("Himmelblau is already configured for %q; use 'intuneme destroy' before enrolling a different account", cfg.HimmelblauUPN)
		}

		if err := provision.InstallHimmelblauUserMap(r, cfg.RootfsPath, cfg.HostUser, upn); err != nil {
			return err
		}
		mappedBefore := cfg.HimmelblauUPN != ""
		if err := prepareHimmelblauEnrollmentContainer(
			containerRunning,
			mappedBefore,
			func() error {
				rep.Message("Starting stopped container to load the Himmelblau user mapping...")
				return startCmd.RunE(startCmd, nil)
			},
			func() error {
				rep.Message("Rebooting container to load the Himmelblau user mapping...")
				return provision.RebootHimmelblauContainer(r, cfg.MachineName)
			},
			func() error {
				return provision.WaitForHimmelblauServices(r, cfg.MachineName)
			},
		); err != nil {
			return err
		}

		var proxy *bluezproxy.Proxy
		systemBusAddress := ""
		terminalQR := false
		if cfg.HimmelblauTerminalPasskey {
			terminalQR, err = provision.HimmelblauPasswordlessQRBluetoothEnabled(cfg.RootfsPath)
			if err != nil {
				return err
			}
		}
		if terminalQR {
			proxy, err = bluezproxy.Start(root)
			if err != nil {
				return err
			}
			if err := nspawn.EnsureBind(r, cfg.MachineName, cfg.HostUser, proxy.HostDir(), bluezproxy.ContainerDir, proxy.BindMarkerPath()); err != nil {
				if stopErr := proxy.Stop(); stopErr != nil {
					return fmt.Errorf("bind filtered host BlueZ proxy into container: %w; stop proxy: %v", err, stopErr)
				}
				return fmt.Errorf("bind filtered host BlueZ proxy into container: %w", err)
			}
			systemBusAddress = proxy.BusAddress()
			rep.Message("Host BlueZ is available to this PAM attempt through a filtered D-Bus proxy.")
		}

		if err := provision.InstallHimmelblauPAMAllowlist(r, cfg.RootfsPath, cfg.HostUser, true); err != nil {
			if proxy != nil {
				if stopErr := proxy.Stop(); stopErr != nil {
					return fmt.Errorf("%w; stop filtered host BlueZ proxy: %v", err, stopErr)
				}
			}
			return err
		}
		restoreAllowlist := func(cause error) error {
			if err := provision.InstallHimmelblauPAMAllowlist(r, cfg.RootfsPath, cfg.HostUser, mappedBefore); err != nil {
				return fmt.Errorf("%w; restore machine-shell PAM allowlist: %v", cause, err)
			}
			return cause
		}
		if err := provision.VerifyHimmelblauMappedPAMAllowlist(cfg.RootfsPath, cfg.HostUser); err != nil {
			if proxy != nil {
				if stopErr := proxy.Stop(); stopErr != nil {
					return fmt.Errorf("%w; stop filtered host BlueZ proxy: %v", restoreAllowlist(err), stopErr)
				}
			}
			return restoreAllowlist(err)
		}

		rep.Message("Starting explicit Himmelblau PAM authentication for local user %s mapped to %s...", cfg.HostUser, upn)
		authErr := nspawn.HimmelblauPAMAuthenticate(r, cfg.MachineName, cfg.HostUser, systemBusAddress)
		if proxy != nil {
			if stopErr := proxy.Stop(); stopErr != nil {
				if authErr == nil {
					authErr = fmt.Errorf("stop filtered host BlueZ proxy: %w", stopErr)
				} else {
					authErr = fmt.Errorf("%w; stop filtered host BlueZ proxy: %v", authErr, stopErr)
				}
			}
		}
		if authErr != nil {
			return restoreAllowlist(authErr)
		}
		rep.Message("Entra authentication succeeded; opening the container shell...")
		if err := nspawn.Shell(r, cfg.MachineName, cfg.HostUser); err != nil {
			return restoreAllowlist(fmt.Errorf("open container shell after Himmelblau PAM authentication: %w", err))
		}

		uid, err := nspawn.UserUID(r, cfg.MachineName, cfg.HostUser)
		if err != nil {
			return restoreAllowlist(err)
		}
		if uid != cfg.HostUID {
			return restoreAllowlist(fmt.Errorf("local container account %q resolved to UID %d, not host UID %d", cfg.HostUser, uid, cfg.HostUID))
		}

		cfg.HimmelblauUPN = upn
		if err := cfg.Save(root); err != nil {
			return fmt.Errorf("save Himmelblau enrollment: %w", err)
		}
		if _, err := r.Run("machinectl", broker.EnableLingerArgs(cfg.MachineName, cfg.HostUser)...); err != nil {
			return fmt.Errorf("enable lingering user session: %w", err)
		}
		busReady := false
		for range 30 {
			if nspawn.SessionBusReady(r, cfg.MachineName, cfg.HostUID) {
				busReady = true
				break
			}
			time.Sleep(time.Second)
		}
		if !busReady {
			return fmt.Errorf("container session bus not available after 30 seconds")
		}

		rep.Message("Himmelblau PAM enrollment completed for %s.", upn)
		return nil
	},
}

func prepareHimmelblauEnrollmentContainer(containerRunning, mappedBefore bool, startContainer, rebootContainer, waitServices func() error) error {
	if !containerRunning {
		if err := startContainer(); err != nil {
			return fmt.Errorf("start container for Himmelblau enrollment: %w", err)
		}
		if err := waitServices(); err != nil {
			return fmt.Errorf("wait for Himmelblau services after start: %w", err)
		}
		return nil
	}
	if !mappedBefore {
		return rebootContainer()
	}
	return waitServices()
}

func validateHimmelblauUPN(upn string) error {
	if !himmelblauUPNPattern.MatchString(upn) {
		return fmt.Errorf("invalid Entra UPN %q; expected a single user@domain address", upn)
	}
	return nil
}

func validateHimmelblauUPNDomain(upn, configuredDomain string) error {
	domain, err := config.NormalizeHimmelblauDomain(configuredDomain)
	if err != nil {
		return fmt.Errorf("Himmelblau domain is not configured; run 'intuneme init --force --himmelblau --himmelblau-domain <domain>': %w", err)
	}
	_, upnDomain, _ := strings.Cut(upn, "@")
	if upnDomain != domain {
		return fmt.Errorf("UPN domain %q does not match configured Entra sign-in domain %q", upnDomain, domain)
	}
	return nil
}

func promptHimmelblauUPN() (string, error) {
	if !term.IsTerminal(os.Stdin.Fd()) {
		return "", fmt.Errorf("an Entra UPN is required; pass --user when stdin is not a terminal")
	}
	fmt.Print("Entra UPN: ")
	line, err := bufio.NewReader(os.Stdin).ReadString('\n')
	if err != nil {
		return "", fmt.Errorf("read Entra UPN: %w", err)
	}
	return strings.TrimSpace(line), nil
}

func init() {
	enrollCmd.Flags().StringVar(&himmelblauUser, "user", "", "Entra UPN to authenticate as (prompted when omitted)")
	rootCmd.AddCommand(enrollCmd)
}
