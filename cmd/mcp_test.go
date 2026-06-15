package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/frostyard/intuneme/internal/nspawn"
)

type mcpMockRunner struct {
	runCalls      [][]string
	attachedCalls [][]string
	probeErr      error // returned for the EnsureBind nsenter "test -e" probe
	bindErr       error
	notRunning    bool
}

func argsContain(args []string, want string) bool {
	for _, a := range args {
		if a == want {
			return true
		}
	}
	return false
}

func (m *mcpMockRunner) Run(name string, args ...string) ([]byte, error) {
	m.runCalls = append(m.runCalls, append([]string{name}, args...))
	switch {
	case name == "machinectl" && len(args) > 0 && args[0] == "show":
		if argsContain(args, "Leader") {
			return []byte("12345\n"), nil
		}
		if m.notRunning {
			return nil, fmt.Errorf("machine not found")
		}
		return []byte("Name=intuneme\n"), nil
	case name == "sudo" && len(args) > 0 && args[0] == "nsenter":
		// EnsureBind probe (test -e <bin>).
		return nil, m.probeErr
	case name == "machinectl" && len(args) > 0 && args[0] == "bind":
		return nil, m.bindErr
	}
	return nil, nil
}

func (m *mcpMockRunner) RunAttached(name string, args ...string) error {
	m.attachedCalls = append(m.attachedCalls, append([]string{name}, args...))
	return nil
}
func (m *mcpMockRunner) RunBackground(string, ...string) error { return nil }
func (m *mcpMockRunner) LookPath(name string) (string, error) {
	return "/usr/bin/" + name, nil
}

func (m *mcpMockRunner) bound() bool {
	for _, c := range m.runCalls {
		if len(c) > 1 && c[0] == "machinectl" && c[1] == "bind" {
			return true
		}
	}
	return false
}

func (m *mcpMockRunner) lastForegroundCommand(t *testing.T) string {
	t.Helper()
	if len(m.attachedCalls) == 0 {
		t.Fatal("expected a foreground (RunAttached) launch, got none")
	}
	// The script is the final argument to su -c.
	last := m.attachedCalls[len(m.attachedCalls)-1]
	return last[len(last)-1]
}

// initializedRoot returns a temp root that looks initialized (rootfs dir exists).
// When skipBinary is false it also creates a server binary and points the
// mcp_binary config key at it, returning that path.
func initializedRoot(t *testing.T, skipBinary bool) (root, binary string) {
	t.Helper()
	root = t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "rootfs"), 0755); err != nil {
		t.Fatal(err)
	}
	// Pin HostUser so the test doesn't depend on $USER.
	cfg := "host_user = \"tester\"\nhost_uid = 1000\n"
	if !skipBinary {
		mcpDir := filepath.Join(root, "mcp")
		if err := os.MkdirAll(mcpDir, 0755); err != nil {
			t.Fatal(err)
		}
		binary = filepath.Join(mcpDir, "server")
		if err := os.WriteFile(binary, []byte("#!/bin/sh\n"), 0755); err != nil {
			t.Fatal(err)
		}
		cfg += fmt.Sprintf("mcp_binary = %q\n", binary)
	}
	if err := os.WriteFile(filepath.Join(root, "config.toml"), []byte(cfg), 0644); err != nil {
		t.Fatal(err)
	}
	return root, binary
}

func TestRunMCP_NotInitialized(t *testing.T) {
	r := &mcpMockRunner{}
	err := runMCP(r, t.TempDir(), "", nil)
	if err == nil || !strings.Contains(err.Error(), "not initialized") {
		t.Fatalf("expected 'not initialized' error, got %v", err)
	}
}

func TestRunMCP_NotRunning(t *testing.T) {
	r := &mcpMockRunner{notRunning: true}
	root, _ := initializedRoot(t, false)
	err := runMCP(r, root, "", nil)
	if err == nil || !strings.Contains(err.Error(), "not running") {
		t.Fatalf("expected 'not running' error, got %v", err)
	}
}

func TestRunMCP_NoBinaryConfigured(t *testing.T) {
	r := &mcpMockRunner{}
	root, _ := initializedRoot(t, true)
	err := runMCP(r, root, "", nil)
	if err == nil || !strings.Contains(err.Error(), "no MCP server binary configured") {
		t.Fatalf("expected 'no MCP server binary configured' error, got %v", err)
	}
}

func TestRunMCP_BinaryNotFound(t *testing.T) {
	r := &mcpMockRunner{}
	root, _ := initializedRoot(t, true)
	missing := filepath.Join(t.TempDir(), "does-not-exist")
	err := runMCP(r, root, missing, nil)
	if err == nil || !strings.Contains(err.Error(), "MCP binary not found") {
		t.Fatalf("expected 'MCP binary not found' error, got %v", err)
	}
}

func TestRunMCP_BindsAndLaunches(t *testing.T) {
	// probeErr set => binary not yet visible => bind must happen.
	r := &mcpMockRunner{probeErr: fmt.Errorf("not found")}
	root, _ := initializedRoot(t, false)
	if err := runMCP(r, root, "", nil); err != nil {
		t.Fatalf("runMCP returned error: %v", err)
	}
	if !r.bound() {
		t.Error("expected a 'machinectl bind' call when binary not yet visible")
	}
	cmd := r.lastForegroundCommand(t)
	if !strings.Contains(cmd, "exec env DOTNET_BUNDLE_EXTRACT_BASE_DIR=") {
		t.Errorf("foreground command missing dotnet extract env: %q", cmd)
	}
	if !strings.Contains(cmd, nspawn.MCPMountDir+"/server") {
		t.Errorf("foreground command missing container binary path: %q", cmd)
	}
}

func TestRunMCP_SkipsBindWhenPresent(t *testing.T) {
	// probeErr nil => already bound => no bind call, still launches.
	r := &mcpMockRunner{probeErr: nil}
	root, _ := initializedRoot(t, false)
	if err := runMCP(r, root, "", nil); err != nil {
		t.Fatalf("runMCP returned error: %v", err)
	}
	if r.bound() {
		t.Error("did not expect 'machinectl bind' when binary already visible")
	}
	if len(r.attachedCalls) == 0 {
		t.Error("expected a foreground launch")
	}
}

func TestRunMCP_PassesArgsVerbatim(t *testing.T) {
	r := &mcpMockRunner{probeErr: nil}
	root, _ := initializedRoot(t, false)
	if err := runMCP(r, root, "", []string{"foo", "bar"}); err != nil {
		t.Fatalf("runMCP returned error: %v", err)
	}
	cmd := r.lastForegroundCommand(t)
	if !strings.Contains(cmd, "'foo' 'bar'") {
		t.Errorf("expected args in command, got: %q", cmd)
	}
}

func TestRunMCP_UsesConfigArgsByDefault(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "rootfs"), 0755); err != nil {
		t.Fatal(err)
	}
	mcpDir := filepath.Join(root, "mcp")
	if err := os.MkdirAll(mcpDir, 0755); err != nil {
		t.Fatal(err)
	}
	binary := filepath.Join(mcpDir, "server")
	if err := os.WriteFile(binary, []byte("#!/bin/sh\n"), 0755); err != nil {
		t.Fatal(err)
	}
	cfg := fmt.Sprintf("host_user = \"tester\"\nhost_uid = 1000\nmcp_binary = %q\nmcp_args = [\"mcp\"]\n", binary)
	if err := os.WriteFile(filepath.Join(root, "config.toml"), []byte(cfg), 0644); err != nil {
		t.Fatal(err)
	}
	r := &mcpMockRunner{probeErr: nil}
	if err := runMCP(r, root, "", nil); err != nil {
		t.Fatalf("runMCP returned error: %v", err)
	}
	cmd := r.lastForegroundCommand(t)
	if !strings.HasSuffix(strings.TrimSpace(cmd), "'mcp'") {
		t.Errorf("expected configured mcp_args appended, got: %q", cmd)
	}
}

func TestRunMCP_TrailingArgsOverrideConfigArgs(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "rootfs"), 0755); err != nil {
		t.Fatal(err)
	}
	mcpDir := filepath.Join(root, "mcp")
	if err := os.MkdirAll(mcpDir, 0755); err != nil {
		t.Fatal(err)
	}
	binary := filepath.Join(mcpDir, "server")
	if err := os.WriteFile(binary, []byte("#!/bin/sh\n"), 0755); err != nil {
		t.Fatal(err)
	}
	cfg := fmt.Sprintf("host_user = \"tester\"\nhost_uid = 1000\nmcp_binary = %q\nmcp_args = [\"mcp\"]\n", binary)
	if err := os.WriteFile(filepath.Join(root, "config.toml"), []byte(cfg), 0644); err != nil {
		t.Fatal(err)
	}
	r := &mcpMockRunner{probeErr: nil}
	if err := runMCP(r, root, "", []string{"serve"}); err != nil {
		t.Fatalf("runMCP returned error: %v", err)
	}
	cmd := r.lastForegroundCommand(t)
	if strings.Contains(cmd, "'mcp'") {
		t.Errorf("did not expect configured mcp_args when overridden: %q", cmd)
	}
	if !strings.Contains(cmd, "'serve'") {
		t.Errorf("expected overriding arg, got: %q", cmd)
	}
}

func TestRunMCP_CustomBinaryFlagOverridesConfig(t *testing.T) {
	root, _ := initializedRoot(t, false) // config points at <root>/mcp/server
	custom := filepath.Join(t.TempDir(), "myserver")
	if err := os.WriteFile(custom, []byte("#!/bin/sh\n"), 0755); err != nil {
		t.Fatal(err)
	}
	r := &mcpMockRunner{probeErr: fmt.Errorf("not found")}
	if err := runMCP(r, root, custom, nil); err != nil {
		t.Fatalf("runMCP returned error: %v", err)
	}
	cmd := r.lastForegroundCommand(t)
	if !strings.Contains(cmd, nspawn.MCPMountDir+"/myserver") {
		t.Errorf("expected custom binary basename in command, got: %q", cmd)
	}
}
