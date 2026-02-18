package cmd

import (
	"embed"
	"fmt"
	"io/fs"
	"os"
	"os/user"
	"path/filepath"

	"github.com/frostyard/intuneme/internal/runner"
	"github.com/spf13/cobra"
)

//go:embed extension/*
var extensionFS embed.FS

const extensionUUID = "intuneme@frostyard.org"

var extensionCmd = &cobra.Command{
	Use:   "extension",
	Short: "Manage the GNOME Shell extension",
}

var extensionInstallCmd = &cobra.Command{
	Use:   "install",
	Short: "Install the GNOME Shell Quick Settings extension",
	RunE: func(cmd *cobra.Command, args []string) error {
		r := &runner.SystemRunner{}

		u, err := user.Current()
		if err != nil {
			return fmt.Errorf("get current user: %w", err)
		}

		// Install extension files to ~/.local/share/gnome-shell/extensions/<uuid>/
		extDir := filepath.Join(u.HomeDir, ".local", "share", "gnome-shell", "extensions", extensionUUID)
		if err := os.MkdirAll(extDir, 0755); err != nil {
			return fmt.Errorf("create extension dir: %w", err)
		}

		err = fs.WalkDir(extensionFS, "extension", func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}

			// Compute the relative path under extension/
			rel, _ := filepath.Rel("extension", path)
			dest := filepath.Join(extDir, rel)

			if d.IsDir() {
				return os.MkdirAll(dest, 0755)
			}

			data, err := extensionFS.ReadFile(path)
			if err != nil {
				return err
			}

			return os.WriteFile(dest, data, 0644)
		})
		if err != nil {
			return fmt.Errorf("install extension files: %w", err)
		}
		fmt.Printf("Extension files installed to %s\n", extDir)

		// Install polkit policy (needs sudo)
		policyData, err := extensionFS.ReadFile("extension/org.frostyard.intuneme.policy")
		if err != nil {
			return fmt.Errorf("read polkit policy: %w", err)
		}

		tmpFile, err := os.CreateTemp("", "intuneme-policy-*.xml")
		if err != nil {
			return fmt.Errorf("create temp file: %w", err)
		}
		defer func() { _ = os.Remove(tmpFile.Name()) }()

		if _, err := tmpFile.Write(policyData); err != nil {
			_ = tmpFile.Close()
			return fmt.Errorf("write temp file: %w", err)
		}
		_ = tmpFile.Close()

		policyDest := "/usr/share/polkit-1/actions/org.frostyard.intuneme.policy"
		if _, err := r.Run("sudo", "cp", tmpFile.Name(), policyDest); err != nil {
			return fmt.Errorf("install polkit policy (sudo cp): %w", err)
		}
		fmt.Printf("Polkit policy installed to %s\n", policyDest)

		// Enable the extension
		if _, err := r.Run("gnome-extensions", "enable", extensionUUID); err != nil {
			fmt.Fprintf(os.Stderr, "warning: could not enable extension: %v\n", err)
			fmt.Println("You may need to enable it manually via GNOME Extensions app.")
		} else {
			fmt.Println("Extension enabled.")
		}

		fmt.Println("\nLog out and back in to activate the extension.")
		return nil
	},
}

func init() {
	extensionCmd.AddCommand(extensionInstallCmd)
	rootCmd.AddCommand(extensionCmd)
}
