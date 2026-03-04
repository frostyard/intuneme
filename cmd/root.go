package cmd

import (
	"github.com/frostyard/clix"
	"github.com/frostyard/std/reporter"
	"github.com/spf13/cobra"
)

var rootDir string
var rep reporter.Reporter

var rootCmd = &cobra.Command{
	Use:   "intuneme",
	Short: "Manage an Intune container on an immutable Linux host",
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		rep = clix.NewReporter()
		return nil
	},
}

func RootCmd() *cobra.Command {
	return rootCmd
}

func init() {
	rootCmd.PersistentFlags().StringVar(&rootDir, "root", "", "root directory for intuneme data (default ~/.local/share/intuneme)")
}
