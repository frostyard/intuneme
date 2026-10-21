package sudoers

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"github.com/BurntSushi/toml"
	"github.com/frostyard/intuneme/internal/nshelper"
)

// mockRunner records commands and captures the content installed to each
// destination path so tests can assert on generated files.
type mockRunner struct {
	commands  []string
	installed map[string]string // dest path -> file content
}

func newMockRunner() *mockRunner {
	return &mockRunner{installed: map[string]string{}}
}

func (m *mockRunner) Run(name string, args ...string) ([]byte, error) {
	m.commands = append(m.commands, name+" "+strings.Join(args, " "))
	// Capture installed file content: `sudo install [flags] <src> <dst>`.
	// The `-d` form (directory create) has no src/dst pair to capture.
	if name == "sudo" && len(args) >= 4 && args[0] == "install" && !contains(args, "-d") {
		src := args[len(args)-2]
		dst := args[len(args)-1]
		if data, err := os.ReadFile(src); err == nil {
			m.installed[dst] = string(data)
		}
	}
	return nil, nil
}

func (m *mockRunner) RunAttached(name string, args ...string) error { return nil }
func (m *mockRunner) RunBackground(name string, args ...string) error {
	return nil
}
func (m *mockRunner) LookPath(name string) (string, error) { return "/usr/bin/" + name, nil }

func contains(s []string, v string) bool {
	for _, x := range s {
		if x == v {
			return true
		}
	}
	return false
}

func testOptions() Options {
	return Options{
		User:    "testuser",
		Machine: "intuneme",
		UID:     1000,
		GID:     1000,
		Home:    "/home/testuser",
		Rootfs:  "/home/testuser/.local/share/intuneme/rootfs",
		// Tests use a fake user-owned executable; the explicit-install mode
		// skips the root-owned source check.
		AllowUntrustedSource: true,
	}
}

// fakeExecutable points the installer at a small file instead of the test
// binary. It returns a cleanup function.
func fakeExecutable(t *testing.T) {
	t.Helper()
	path := t.TempDir() + "/intuneme"
	if err := os.WriteFile(path, []byte("fake-intuneme-binary"), 0755); err != nil {
		t.Fatal(err)
	}
	oldPath, oldCheck := executablePath, checkExecutable
	executablePath = func() (string, error) { return path, nil }
	checkExecutable = func(string) error { return nil }
	t.Cleanup(func() { executablePath, checkExecutable = oldPath, oldCheck })
}

func TestInstall_RuleHasNoWildcards(t *testing.T) {
	fakeExecutable(t)
	r := newMockRunner()
	if err := Install(r, testOptions()); err != nil {
		t.Fatalf("Install failed: %v", err)
	}

	rule, ok := r.installed[filePath]
	if !ok {
		t.Fatalf("sudoers rule was not installed; commands: %v", r.commands)
	}
	if strings.Contains(rule, "*") {
		t.Errorf("sudoers rule must not contain wildcards (sudo-rs rejects them):\n%s", rule)
	}
	if !strings.Contains(rule, "testuser ALL=(root) NOPASSWD: "+nshelper.Path) {
		t.Errorf("sudoers rule does not reference the helper path:\n%s", rule)
	}
}

func TestInstall_InstallsHelperBinaryAndConfig(t *testing.T) {
	fakeExecutable(t)
	r := newMockRunner()
	if err := Install(r, testOptions()); err != nil {
		t.Fatalf("Install failed: %v", err)
	}

	helper, ok := r.installed[nshelper.Path+".new"]
	if !ok {
		t.Fatalf("helper binary was not staged; commands: %v", r.commands)
	}
	if helper != "fake-intuneme-binary" {
		t.Errorf("installed helper = %q, want fake-intuneme-binary", helper)
	}

	raw, ok := r.installed[nshelper.ConfigPath+".new"]
	if !ok {
		t.Fatalf("helper config was not staged; commands: %v", r.commands)
	}
	var cfg nshelper.Config
	if _, err := toml.Decode(raw, &cfg); err != nil {
		t.Fatalf("helper config is not valid TOML: %v\n%s", err, raw)
	}
	if cfg.Machine != "intuneme" || cfg.User != "testuser" || cfg.UID != 1000 ||
		cfg.GID != 1000 || cfg.Home != "/home/testuser" ||
		cfg.Rootfs != "/home/testuser/.local/share/intuneme/rootfs" {
		t.Errorf("helper config = %+v", cfg)
	}

	var sawBinaryInstall, sawConfigInstall, sawDirInstall, sawBinaryRename, sawConfigRename bool
	for _, c := range r.commands {
		if strings.Contains(c, nshelper.Path+".new") && strings.Contains(c, "install -m 0755 -o root -g root") {
			sawBinaryInstall = true
		}
		if strings.Contains(c, nshelper.ConfigPath+".new") && strings.Contains(c, "install -m 0644 -o root -g root") {
			sawConfigInstall = true
		}
		if strings.Contains(c, "install -d -m 0755 -o root -g root "+nshelper.Dir) {
			sawDirInstall = true
		}
		if c == "sudo mv -f "+nshelper.Path+".new "+nshelper.Path {
			sawBinaryRename = true
		}
		if c == "sudo mv -f "+nshelper.ConfigPath+".new "+nshelper.ConfigPath {
			sawConfigRename = true
		}
	}
	if !sawDirInstall {
		t.Errorf("expected helper directory to be created root-owned; commands: %v", r.commands)
	}
	if !sawBinaryInstall {
		t.Errorf("expected helper binary staged 0755 root:root; commands: %v", r.commands)
	}
	if !sawConfigInstall {
		t.Errorf("expected helper config staged 0644 root:root; commands: %v", r.commands)
	}
	if !sawBinaryRename {
		t.Errorf("expected helper binary to be activated with mv; commands: %v", r.commands)
	}
	if !sawConfigRename {
		t.Errorf("expected helper config to be activated with mv; commands: %v", r.commands)
	}
}

func TestInstall_ValidatesBeforeInstallingRule(t *testing.T) {
	fakeExecutable(t)
	r := newMockRunner()
	if err := Install(r, testOptions()); err != nil {
		t.Fatalf("Install failed: %v", err)
	}

	var visudoIdx, ruleInstallIdx = -1, -1
	for i, c := range r.commands {
		if strings.Contains(c, "visudo -c -f") {
			visudoIdx = i
		}
		if strings.Contains(c, "install -m 0440") && strings.Contains(c, filePath) {
			ruleInstallIdx = i
		}
	}
	if visudoIdx == -1 {
		t.Fatalf("visudo syntax check was not run; commands: %v", r.commands)
	}
	if ruleInstallIdx == -1 {
		t.Fatalf("rule was not installed; commands: %v", r.commands)
	}
	if visudoIdx > ruleInstallIdx {
		t.Errorf("visudo check (%d) must run before installing the rule (%d)", visudoIdx, ruleInstallIdx)
	}
}

func TestVerifyExecutableRejectsUserOwnedPath(t *testing.T) {
	path := filepath.Join(t.TempDir(), "intuneme")
	if err := os.WriteFile(path, []byte("binary"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := verifyExecutable(path); err == nil {
		t.Error("expected a user-owned executable path to be rejected")
	}
}

func TestVerifyExecutableAcceptsRootOwnedBinary(t *testing.T) {
	info, err := os.Stat("/bin/sh")
	if err != nil {
		t.Skip("/bin/sh not available")
	}
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok || st.Uid != 0 {
		t.Skip("/bin/sh is not root-owned in this environment")
	}
	if err := verifyExecutable("/bin/sh"); err != nil {
		t.Errorf("root-owned executable rejected: %v", err)
	}
}

func TestInstall_RejectsUntrustedSource(t *testing.T) {
	fakeExecutable(t)
	checkExecutable = verifyExecutable
	r := newMockRunner()
	opts := testOptions()
	opts.AllowUntrustedSource = false
	err := Install(r, opts)
	if !errors.Is(err, ErrUntrustedSource) {
		t.Fatalf("expected ErrUntrustedSource, got %v", err)
	}
	if len(r.commands) != 0 {
		t.Errorf("no commands should run for an untrusted source, got: %v", r.commands)
	}
}

func TestInstall_AllowsUntrustedSourceWhenExplicit(t *testing.T) {
	fakeExecutable(t)
	checkExecutable = verifyExecutable
	r := newMockRunner()
	opts := testOptions()
	opts.AllowUntrustedSource = true
	if err := Install(r, opts); err != nil {
		t.Fatalf("explicit install from a user-owned binary failed: %v", err)
	}
}

func TestInstall_RejectsUnsafeConfig(t *testing.T) {
	fakeExecutable(t)
	r := newMockRunner()
	opts := testOptions()
	opts.Machine = ".host"
	if err := Install(r, opts); err == nil {
		t.Fatal("expected .host machine to be rejected")
	}
	if len(r.commands) != 0 {
		t.Errorf("no commands should run for invalid config, got: %v", r.commands)
	}
}

func TestRemove_RemovesRuleHelperAndConfig(t *testing.T) {
	r := newMockRunner()
	Remove(r)

	if len(r.commands) != 1 {
		t.Fatalf("expected a single rm command, got: %v", r.commands)
	}
	cmd := r.commands[0]
	for _, path := range []string{filePath, nshelper.Path, nshelper.Path + ".new", nshelper.ConfigPath, nshelper.ConfigPath + ".new"} {
		if !strings.Contains(cmd, path) {
			t.Errorf("Remove must delete %s, got: %s", path, cmd)
		}
	}
}
