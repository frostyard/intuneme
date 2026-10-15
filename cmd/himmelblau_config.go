package cmd

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/x/term"
	"github.com/frostyard/clix"
	"github.com/frostyard/intuneme/internal/config"
	"github.com/frostyard/intuneme/internal/provision"
	"github.com/frostyard/intuneme/internal/runner"
	"github.com/spf13/cobra"
)

const himmelblauConfigFileName = "himmelblau.conf"

var editHimmelblau bool

var editCmd = &cobra.Command{
	Use:   "edit",
	Short: "Edit container configuration",
	Long: `Edit the commented Himmelblau configuration with the user's editor.
Requires an initialized Himmelblau image and --himmelblau. Stop and start the
container after editing to apply the new settings.`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		if !editHimmelblau {
			return fmt.Errorf("select a configuration to edit with --himmelblau")
		}

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
			return fmt.Errorf("load intuneme config: %w", err)
		}
		if !cfg.Himmelblau {
			return fmt.Errorf("Himmelblau mode is not enabled — run 'intuneme init --himmelblau' first")
		}
		if _, err := os.Stat(cfg.RootfsPath); err != nil {
			return fmt.Errorf("not initialized — run 'intuneme init --himmelblau' first")
		}
		cfg.HimmelblauDomain, err = config.NormalizeHimmelblauDomain(cfg.HimmelblauDomain)
		if err != nil {
			return fmt.Errorf("Himmelblau domain is not configured: %w", err)
		}
		if clix.DryRun {
			rep.Message("[dry-run] Would edit Himmelblau configuration")
			return nil
		}

		rep.Message("Editing Himmelblau configuration...")
		content, err := editHimmelblauConfig(root, cfg.RootfsPath, cfg.HimmelblauDomain)
		if err != nil {
			return err
		}
		if err := saveHimmelblauConfig(root, content); err != nil {
			return err
		}
		rep.Message("Saved Himmelblau configuration at %s", savedHimmelblauConfigPath(root))
		rep.Message("Stop and start the container for the new settings to take effect.")
		return nil
	},
}

func savedHimmelblauConfigPath(root string) string {
	return filepath.Join(root, himmelblauConfigFileName)
}

func prepareHimmelblauConfigForEdit(content []byte, domain string) ([]byte, error) {
	return provision.PrepareHimmelblauConfig(content, domain)
}

func editHimmelblauConfig(root, rootfsPath, domain string) ([]byte, error) {
	if !term.IsTerminal(os.Stdin.Fd()) {
		return nil, fmt.Errorf("'intuneme edit --himmelblau' requires an interactive terminal")
	}
	path := savedHimmelblauConfigPath(root)
	content, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		content, err = os.ReadFile(filepath.Join(rootfsPath, "etc", "himmelblau", "himmelblau.conf"))
	}
	if err != nil {
		return nil, fmt.Errorf("read initial Himmelblau configuration: %w", err)
	}
	prepared, err := prepareHimmelblauConfigForEdit(content, domain)
	if err != nil {
		rep.Warning("Could not prefill required Himmelblau settings before editing; correct them in the editor: %v", err)
	} else {
		content = prepared
	}
	tmp, err := os.CreateTemp(root, ".himmelblau-*.conf")
	if err != nil {
		return nil, fmt.Errorf("create editable Himmelblau configuration: %w", err)
	}
	tmpPath := tmp.Name()
	defer func() { _ = os.Remove(tmpPath) }()
	if _, err := tmp.Write(content); err != nil {
		_ = tmp.Close()
		return nil, fmt.Errorf("write editable Himmelblau configuration: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return nil, fmt.Errorf("close editable Himmelblau configuration: %w", err)
	}

	editor, desktopDefault, err := resolveHimmelblauEditor()
	if err != nil {
		return nil, err
	}
	if desktopDefault {
		if err := openWithDesktopDefaultEditor(tmpPath, editor[0], os.Stdin, os.Stderr); err != nil {
			return nil, err
		}
	} else if err := runHimmelblauEditor(tmpPath, editor); err != nil {
		return nil, err
	}

	edited, err := os.ReadFile(tmpPath)
	if err != nil {
		return nil, fmt.Errorf("read edited Himmelblau configuration: %w", err)
	}
	if err := provision.ValidateHimmelblauConfig(edited, domain); err != nil {
		return nil, fmt.Errorf("edited Himmelblau configuration is invalid: %w", err)
	}
	return edited, nil
}

func resolveHimmelblauEditor() ([]string, bool, error) {
	if editor := strings.Fields(os.Getenv("VISUAL")); len(editor) > 0 {
		return editor, false, nil
	}
	if editor := strings.Fields(os.Getenv("EDITOR")); len(editor) > 0 {
		return editor, false, nil
	}
	if os.Getenv("DISPLAY") != "" || os.Getenv("WAYLAND_DISPLAY") != "" {
		if opener, err := exec.LookPath("xdg-open"); err == nil {
			return []string{opener}, true, nil
		}
	}
	for _, name := range []string{"sensible-editor", "editor", "micro", "nano", "nvim", "vim", "vi"} {
		if editor, err := exec.LookPath(name); err == nil {
			return []string{editor}, false, nil
		}
	}
	return nil, false, fmt.Errorf("no host editor found; set $VISUAL or $EDITOR")
}

func runHimmelblauEditor(path string, editor []string) error {
	if len(editor) == 0 || editor[0] == "" {
		return fmt.Errorf("no editor command configured")
	}
	args := append(editor[1:], path)
	edit := exec.Command(editor[0], args...)
	edit.Stdin = os.Stdin
	edit.Stdout = os.Stdout
	edit.Stderr = os.Stderr
	if err := edit.Run(); err != nil {
		return fmt.Errorf("edit Himmelblau configuration with %q: %w", editor[0], err)
	}
	return nil
}

func openWithDesktopDefaultEditor(path, opener string, stdin io.Reader, stderr io.Writer) error {
	open := exec.Command(opener, path)
	open.Stdout = stderr
	open.Stderr = stderr
	if err := open.Run(); err != nil {
		return fmt.Errorf("open Himmelblau configuration with the host's default editor: %w", err)
	}
	if _, err := fmt.Fprintln(stderr, "The configuration was opened in the host's default editor. Save and close it, then press Enter here to continue."); err != nil {
		return fmt.Errorf("prompt to finish editing Himmelblau configuration: %w", err)
	}
	if _, err := bufio.NewReader(stdin).ReadString('\n'); err != nil {
		return fmt.Errorf("wait for host editor confirmation: %w", err)
	}
	return nil
}

func saveHimmelblauConfig(root string, content []byte) error {
	if err := os.MkdirAll(root, 0755); err != nil {
		return fmt.Errorf("create intuneme data directory: %w", err)
	}
	tmp, err := os.CreateTemp(root, ".himmelblau.conf-*")
	if err != nil {
		return fmt.Errorf("create saved Himmelblau configuration: %w", err)
	}
	tmpPath := tmp.Name()
	defer func() { _ = os.Remove(tmpPath) }()
	if err := tmp.Chmod(0600); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("secure saved Himmelblau configuration: %w", err)
	}
	if _, err := tmp.Write(content); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write saved Himmelblau configuration: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("sync saved Himmelblau configuration: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close saved Himmelblau configuration: %w", err)
	}
	if err := os.Rename(tmpPath, savedHimmelblauConfigPath(root)); err != nil {
		return fmt.Errorf("save Himmelblau configuration: %w", err)
	}
	return nil
}

func installSavedHimmelblauConfig(r runner.Runner, root, rootfsPath, domain string) error {
	content, err := os.ReadFile(savedHimmelblauConfigPath(root))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read saved Himmelblau configuration: %w", err)
	}
	if err := provision.InstallHimmelblauConfig(r, rootfsPath, content, domain); err != nil {
		return fmt.Errorf("install saved Himmelblau configuration: %w", err)
	}
	return nil
}

func init() {
	editCmd.Flags().BoolVar(&editHimmelblau, "himmelblau", false, "edit Himmelblau's commented configuration")
	rootCmd.AddCommand(editCmd)
}
