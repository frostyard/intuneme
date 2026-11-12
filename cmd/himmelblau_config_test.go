package cmd

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/frostyard/clix"
	"github.com/frostyard/intuneme/internal/provision"
)

func TestEditCommandRequiresHimmelblauSelection(t *testing.T) {
	original := editHimmelblau
	editHimmelblau = false
	defer func() { editHimmelblau = original }()

	err := editCmd.RunE(editCmd, nil)
	if err == nil || !strings.Contains(err.Error(), "--himmelblau") {
		t.Fatalf("edit command error = %v, want selection error mentioning --himmelblau", err)
	}
}

func TestInitNoLongerHasHimmelblauEditorFlag(t *testing.T) {
	if initCmd.Flags().Lookup("himmelblau-edit-conf") != nil {
		t.Fatal("init unexpectedly still exposes --himmelblau-edit-conf")
	}
	if editCmd.Flags().Lookup("himmelblau") == nil {
		t.Fatal("edit command is missing --himmelblau")
	}
}

func TestPrepareHimmelblauConfigForEditRepairsRequiredPathValue(t *testing.T) {
	original := []byte(`[global]
domain = "example.com"
user_map_file = /etc/himmelblau/user-map
cn_name_mapping = false
enable_passwordless_qr_bluetooth = true
`)

	prepared, err := prepareHimmelblauConfigForEdit(original, "example.com")
	if err != nil {
		t.Fatalf("prepareHimmelblauConfigForEdit() error: %v", err)
	}
	if !strings.Contains(string(prepared), "user_map_file = /etc/himmelblau/user-map") {
		t.Fatalf("prepared config has incorrect user_map_file: %s", prepared)
	}
	if err := provision.ValidateHimmelblauConfig(prepared, "example.com"); err != nil {
		t.Fatalf("prepared config is not valid: %v", err)
	}
	if !strings.Contains(string(prepared), "enable_passwordless_qr_bluetooth = true") {
		t.Fatal("preparation dropped the user's additional setting")
	}
	if !strings.Contains(string(prepared), "apply_policy = true") {
		t.Fatal("preparation did not enable Intune policy application by default")
	}
}

func TestPrepareHimmelblauConfigPreservesExplicitApplyPolicyOptOut(t *testing.T) {
	original := []byte(`[global]
domain = example.com
apply_policy = false
`)

	prepared, err := prepareHimmelblauConfigForEdit(original, "example.com")
	if err != nil {
		t.Fatalf("prepareHimmelblauConfigForEdit() error: %v", err)
	}
	if !strings.Contains(string(prepared), "apply_policy = false") {
		t.Fatalf("preparation did not preserve explicit apply_policy opt-out:\n%s", prepared)
	}
	if strings.Contains(string(prepared), "apply_policy = true") {
		t.Fatalf("preparation overwrote explicit apply_policy opt-out:\n%s", prepared)
	}
}

func TestPrepareHimmelblauConfigDefaultsPasswordlessQRBluetoothUnlessDisabled(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    string
	}{
		{name: "missing option defaults on", content: "[global]\ndomain = example.com\n", want: "enable_passwordless_qr_bluetooth = true"},
		{name: "explicit opt-out is preserved", content: "[global]\ndomain = example.com\nenable_passwordless_qr_bluetooth = false\n", want: "enable_passwordless_qr_bluetooth = false"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			prepared, err := prepareHimmelblauConfigForEdit([]byte(tt.content), "example.com")
			if err != nil {
				t.Fatalf("prepareHimmelblauConfigForEdit() error: %v", err)
			}
			if !strings.Contains(string(prepared), tt.want) {
				t.Fatalf("prepared config missing %q:\n%s", tt.want, prepared)
			}
			if tt.name == "explicit opt-out is preserved" && strings.Contains(string(prepared), "enable_passwordless_qr_bluetooth = true") {
				t.Fatalf("preparation overwrote explicit opt-out:\n%s", prepared)
			}
		})
	}
}

func TestEditCommandDryRun(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "rootfs"), 0755); err != nil {
		t.Fatal(err)
	}
	config := "himmelblau = true\nhimmelblau_domain = \"example.com\"\n"
	if err := os.WriteFile(filepath.Join(root, "config.toml"), []byte(config), 0644); err != nil {
		t.Fatal(err)
	}

	originalRoot, originalEdit, originalDryRun, originalReporter := rootDir, editHimmelblau, clix.DryRun, rep
	rootDir, editHimmelblau, clix.DryRun = root, true, true
	recorder := &messageRecorder{}
	rep = recorder
	defer func() {
		rootDir, editHimmelblau, clix.DryRun, rep = originalRoot, originalEdit, originalDryRun, originalReporter
	}()

	if err := editCmd.RunE(editCmd, nil); err != nil {
		t.Fatalf("edit command dry-run error: %v", err)
	}
	if !strings.Contains(strings.Join(recorder.messages, "\n"), "Would edit Himmelblau configuration") {
		t.Fatalf("dry-run messages = %v", recorder.messages)
	}
}

func TestRunHimmelblauEditor(t *testing.T) {
	dir := t.TempDir()
	editor := filepath.Join(dir, "editor")
	if err := os.WriteFile(editor, []byte("#!/bin/sh\nprintf '\\nenable_passwordless_qr_bluetooth = true\\n' >> \"$1\"\n"), 0755); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(dir, "himmelblau.conf")
	if err := os.WriteFile(configPath, []byte("[global]\n"), 0600); err != nil {
		t.Fatal(err)
	}

	if err := runHimmelblauEditor(configPath, []string{editor}); err != nil {
		t.Fatalf("runHimmelblauEditor() error: %v", err)
	}
	content, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if got := string(content); got != "[global]\n\nenable_passwordless_qr_bluetooth = true\n" {
		t.Fatalf("edited config = %q", got)
	}
}

func TestRunHimmelblauEditorReturnsEditorFailure(t *testing.T) {
	dir := t.TempDir()
	editor := filepath.Join(dir, "editor")
	if err := os.WriteFile(editor, []byte("#!/bin/sh\nexit 7\n"), 0755); err != nil {
		t.Fatal(err)
	}

	err := runHimmelblauEditor(filepath.Join(dir, "himmelblau.conf"), []string{editor})
	if err == nil {
		t.Fatal("expected editor failure")
	}
}

func TestResolveHimmelblauEditorPrefersConfiguredEditor(t *testing.T) {
	t.Setenv("VISUAL", "host-editor --wait")
	t.Setenv("EDITOR", "fallback-editor")
	t.Setenv("DISPLAY", ":1")

	editor, desktopDefault, err := resolveHimmelblauEditor()
	if err != nil {
		t.Fatalf("resolveHimmelblauEditor() error: %v", err)
	}
	if desktopDefault || strings.Join(editor, " ") != "host-editor --wait" {
		t.Fatalf("editor = %v, desktopDefault = %t", editor, desktopDefault)
	}
}

func TestResolveHimmelblauEditorUsesDesktopDefault(t *testing.T) {
	binDir := t.TempDir()
	opener := filepath.Join(binDir, "xdg-open")
	if err := os.WriteFile(opener, []byte("#!/bin/sh\nexit 0\n"), 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir)
	t.Setenv("VISUAL", "")
	t.Setenv("EDITOR", "")
	t.Setenv("DISPLAY", ":test")
	t.Setenv("WAYLAND_DISPLAY", "")

	editor, desktopDefault, err := resolveHimmelblauEditor()
	if err != nil {
		t.Fatalf("resolveHimmelblauEditor() error: %v", err)
	}
	if !desktopDefault || len(editor) != 1 || editor[0] != opener {
		t.Fatalf("editor = %v, desktopDefault = %t, want host xdg-open", editor, desktopDefault)
	}
}

func TestResolveHimmelblauEditorFallsBackToHostTerminalEditor(t *testing.T) {
	binDir := t.TempDir()
	micro := filepath.Join(binDir, "micro")
	if err := os.WriteFile(micro, []byte("#!/bin/sh\nexit 0\n"), 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir)
	t.Setenv("VISUAL", "")
	t.Setenv("EDITOR", "")
	t.Setenv("DISPLAY", "")
	t.Setenv("WAYLAND_DISPLAY", "")

	editor, desktopDefault, err := resolveHimmelblauEditor()
	if err != nil {
		t.Fatalf("resolveHimmelblauEditor() error: %v", err)
	}
	if desktopDefault || len(editor) != 1 || editor[0] != micro {
		t.Fatalf("editor = %v, desktopDefault = %t, want host micro", editor, desktopDefault)
	}
}

func TestResolveHimmelblauEditorReturnsHelpfulErrorWhenUnavailable(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	t.Setenv("VISUAL", "")
	t.Setenv("EDITOR", "")
	t.Setenv("DISPLAY", "")
	t.Setenv("WAYLAND_DISPLAY", "")

	_, _, err := resolveHimmelblauEditor()
	if err == nil || !strings.Contains(err.Error(), "set $VISUAL or $EDITOR") {
		t.Fatalf("resolveHimmelblauEditor() error = %v, want helpful missing-editor error", err)
	}
}

func TestOpenWithDesktopDefaultEditorWaitsForUser(t *testing.T) {
	dir := t.TempDir()
	opener := filepath.Join(dir, "xdg-open")
	if err := os.WriteFile(opener, []byte("#!/bin/sh\nprintf '\\nenable_passwordless_qr_bluetooth = true\\n' >> \"$1\"\n"), 0755); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(dir, "himmelblau.conf")
	if err := os.WriteFile(configPath, []byte("[global]\n"), 0600); err != nil {
		t.Fatal(err)
	}
	var stderr bytes.Buffer

	if err := openWithDesktopDefaultEditor(configPath, opener, strings.NewReader("\n"), &stderr); err != nil {
		t.Fatalf("openWithDesktopDefaultEditor() error: %v", err)
	}
	content, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(content), "enable_passwordless_qr_bluetooth = true") {
		t.Fatalf("desktop editor did not change config: %q", content)
	}
	if !strings.Contains(stderr.String(), "press Enter here") {
		t.Fatalf("missing desktop editor completion prompt: %q", stderr.String())
	}
}

func TestSaveHimmelblauConfigUsesPrivateMode(t *testing.T) {
	root := t.TempDir()
	content := []byte("[global]\ndomain = \"example.com\"\n")

	if err := saveHimmelblauConfig(root, content); err != nil {
		t.Fatalf("saveHimmelblauConfig() error: %v", err)
	}
	path := savedHimmelblauConfigPath(root)
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(content) {
		t.Fatalf("saved config = %q, want %q", got, content)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0600 {
		t.Fatalf("saved config permissions = %04o, want 0600", info.Mode().Perm())
	}
}

func TestInstallSavedHimmelblauConfig(t *testing.T) {
	root := t.TempDir()
	rootfs := t.TempDir()
	content := []byte("[global]\ndomain = example.com\ncn_name_mapping = false\nuser_map_file = /etc/himmelblau/user-map\n")
	if err := saveHimmelblauConfig(root, content); err != nil {
		t.Fatal(err)
	}
	r := &mcpMockRunner{}

	if err := installSavedHimmelblauConfig(r, root, rootfs, "example.com"); err != nil {
		t.Fatalf("installSavedHimmelblauConfig() error: %v", err)
	}
	if len(r.runCalls) != 2 {
		t.Fatalf("install calls = %v, want mkdir and sudo install", r.runCalls)
	}
	if got := r.runCalls[0]; len(got) < 3 || got[0] != "sudo" || got[1] != "mkdir" || got[2] != "-p" {
		t.Fatalf("config directory command = %v, want sudo mkdir -p", got)
	}
	call := r.runCalls[1]
	if len(call) < 5 || call[0] != "sudo" || call[1] != "install" || call[len(call)-1] != filepath.Join(rootfs, "etc", "himmelblau", "himmelblau.conf") {
		t.Fatalf("install command = %v", call)
	}
}

func TestInstallSavedHimmelblauConfigNoOverride(t *testing.T) {
	r := &mcpMockRunner{}
	if err := installSavedHimmelblauConfig(r, t.TempDir(), t.TempDir(), "example.com"); err != nil {
		t.Fatalf("installSavedHimmelblauConfig() error: %v", err)
	}
	if len(r.runCalls) != 0 {
		t.Fatalf("commands = %v, want none", r.runCalls)
	}
}
