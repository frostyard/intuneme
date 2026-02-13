package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var rootDir string

var rootCmd = &cobra.Command{
	Use:   "intuneme",
	Short: "Manage an Intune container on an immutable Linux host",
}

func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func init() {
	rootCmd.PersistentFlags().StringVar(&rootDir, "root", "", "root directory for intuneme data (default ~/.local/share/intuneme)")
}
