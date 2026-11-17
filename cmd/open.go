package cmd

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"text/tabwriter"

	"github.com/frostyard/clix"
	"github.com/frostyard/intuneme/internal/config"
	"github.com/frostyard/intuneme/internal/nspawn"
	"github.com/frostyard/intuneme/internal/problemrec"
	"github.com/frostyard/intuneme/internal/runner"
	"github.com/frostyard/intuneme/internal/webapps"
	"github.com/spf13/cobra"
)

var openCmd = &cobra.Command{
	Use:   "open",
	Short: "Launch an application inside the running container",
}

// runOpen launches command inside the container in the background. Trailing
// args are shell-quoted one by one, so a URL or flag reaches the app as a
// single argument (e.g. `intuneme open edge -- --app=https://...`).
func runOpen(r runner.Runner, root, command string, args []string) error {
	cfg, err := config.Load(root)
	if err != nil {
		return err
	}

	if _, err := os.Stat(cfg.RootfsPath); err != nil {
		return fmt.Errorf("not initialized — run 'intuneme init' first")
	}

	if !nspawn.IsRunning(r, cfg.MachineName) {
		return fmt.Errorf("container is not running — run 'intuneme start' first")
	}

	parts := []string{command}
	for _, a := range args {
		parts = append(parts, nspawn.ShellQuote(a))
	}

	if err := nspawn.Exec(r, cfg.HostUID, strings.Join(parts, " ")); err != nil {
		// Only the session bus timeout is an internal failure. Any other
		// Exec error can be a missing sudoers rule or failed sudo
		// authentication, which is never recorded. A bad config identity
		// (host_uid = -1) also times out, so recordInternal checks it.
		if errors.Is(err, nspawn.ErrSessionBusTimeout) {
			recordInternal(cfg, problemrec.CodeSessionBusTimeout, err)
		}
		return err
	}
	return nil
}

func makeOpenAppCmd(use, short, command string) *cobra.Command {
	return &cobra.Command{
		Use:   use + " [-- args...]",
		Short: short,
		Long: short + `.

Arguments after '--' are passed to the application unchanged. For example, to
open a site as a standalone Edge app window:

  intuneme open edge -- --app=https://outlook.office.com`,
		RunE: func(cmd *cobra.Command, args []string) error {
			r := &runner.SystemRunner{}
			root, err := resolveRoot()
			if err != nil {
				return err
			}
			return runOpen(r, root, command, args)
		},
	}
}

// intuneHomeDir returns the host directory that is bound to the container
// user's home; it is where Edge writes the desktop files of its web apps.
func intuneHomeDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("cannot determine home directory: %w", err)
	}
	return filepath.Join(home, "Intune"), nil
}

// runOpenAppList writes the installed Edge web apps to out, as a table or as 1
// JSON array. It needs the config but not a running container, and it runs no
// command.
func runOpenAppList(root, intuneHome string, out io.Writer, asJSON bool) error {
	if _, err := config.Load(root); err != nil {
		return err
	}

	apps, err := webapps.List(intuneHome)
	if err != nil {
		return err
	}

	if asJSON {
		return json.NewEncoder(out).Encode(apps)
	}

	if len(apps) == 0 {
		_, err := fmt.Fprintln(out, "No Edge web apps found.")
		return err
	}

	tw := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	if _, err := fmt.Fprintln(tw, "ID\tNAME"); err != nil {
		return err
	}
	for _, app := range apps {
		if _, err := fmt.Fprintf(tw, "%s\t%s\n", app.ID, app.Name); err != nil {
			return err
		}
	}
	return tw.Flush()
}

// runOpenApp launches 1 Edge web app with gtk-launch inside the container. The
// ID is checked against the desktop file in the Intune home before any runner
// call, so a refused ID never reaches sudo; runOpen then quotes it with
// nspawn.ShellQuote and loads the config again.
func runOpenApp(r runner.Runner, root, intuneHome, id string) error {
	cfg, err := config.Load(root)
	if err != nil {
		return err
	}

	if _, err := os.Stat(cfg.RootfsPath); err != nil {
		return fmt.Errorf("not initialized — run 'intuneme init' first")
	}

	if err := webapps.Check(intuneHome, id); err != nil {
		return err
	}

	return runOpen(r, root, "gtk-launch", []string{id})
}

func makeOpenWebAppCmd() *cobra.Command {
	var list bool

	cmd := &cobra.Command{
		Use:   "app [ID]",
		Short: "List or launch an Edge web app inside the container",
		Long: `List or launch the Microsoft 365 web apps (PWAs) that Edge installed in the
container.

Edge writes 1 desktop file per installed web app to the container user's
~/.local/share/applications/, and that home directory is a bind mount of
~/Intune on the host. The file name holds the ID, for example
msedge-ahjdipcglkflohmjbcjmjpidpbbiocpk-Default.desktop. Install an app in Edge
with "Apps > Install this site as an app" and it appears here.

The ID must match msedge-<32 letters a-p>-<Default|Profile_<n> and must name a
regular file in that directory; anything else is refused before any command
runs. The app is launched with gtk-launch as the container user.

Examples:

  intuneme open app --list
  intuneme open app --list --json
  intuneme open app msedge-ahjdipcglkflohmjbcjmjpidpbbiocpk-Default`,
		Args: func(_ *cobra.Command, args []string) error {
			// --json is the global flag of the CLI (clix reserves it), so it is
			// read here rather than declared as a local flag.
			if clix.JSONOutput && !list {
				return errors.New("--json needs --list")
			}
			if list {
				if len(args) > 0 {
					return fmt.Errorf("--list takes no arguments, got %d", len(args))
				}
				return nil
			}
			if len(args) != 1 {
				return errors.New("give exactly 1 web app ID, or --list")
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			root, err := resolveRoot()
			if err != nil {
				return err
			}
			intuneHome, err := intuneHomeDir()
			if err != nil {
				return err
			}

			if list {
				return runOpenAppList(root, intuneHome, cmd.OutOrStdout(), clix.JSONOutput)
			}

			r := &runner.SystemRunner{}
			return runOpenApp(r, root, intuneHome, args[0])
		},
	}

	cmd.Flags().BoolVar(&list, "list", false, "list the installed Edge web apps")
	return cmd
}

func init() {
	openCmd.AddCommand(makeOpenWebAppCmd())
	openCmd.AddCommand(makeOpenAppCmd(
		"edge",
		"Launch Microsoft Edge inside the container",
		"microsoft-edge",
	))
	openCmd.AddCommand(makeOpenAppCmd(
		"portal",
		"Launch Intune Portal inside the container",
		"intune-portal",
	))
	rootCmd.AddCommand(openCmd)
}
