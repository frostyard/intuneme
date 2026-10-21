package cmd

import (
	"fmt"
	"os"
	"strings"

	"github.com/frostyard/intuneme/internal/config"
	"github.com/frostyard/intuneme/internal/nspawn"
	"github.com/frostyard/intuneme/internal/runner"
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

	return nspawn.Exec(r, cfg.HostUID, strings.Join(parts, " "))
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
			root := rootDir
			if root == "" {
				var err error
				root, err = config.DefaultRoot()
				if err != nil {
					return err
				}
			}
			return runOpen(r, root, command, args)
		},
	}
}

func init() {
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
