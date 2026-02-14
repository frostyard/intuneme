package cmd

import "github.com/spf13/cobra"

var rootDir string

var rootCmd = &cobra.Command{
	Use:   "intuneme",
	Short: "Manage an Intune container on an immutable Linux host",
}

func RootCmd() *cobra.Command {
	return rootCmd
}

func init() {
	rootCmd.PersistentFlags().StringVar(&rootDir, "root", "", "root directory for intuneme data (default ~/.local/share/intuneme)")
}
