package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/spf13/cobra/doc"
)

var gendocsCmd = &cobra.Command{
	Use:    "gendocs <output-dir>",
	Short:  "Generate markdown CLI reference for MkDocs",
	Hidden: true,
	Args:   cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		dir := args[0]
		if err := os.MkdirAll(dir, 0755); err != nil {
			return fmt.Errorf("create output directory: %w", err)
		}

		cmd.Root().DisableAutoGenTag = true
		return doc.GenMarkdownTree(cmd.Root(), dir)
	},
}

func init() {
	rootCmd.AddCommand(gendocsCmd)
}
