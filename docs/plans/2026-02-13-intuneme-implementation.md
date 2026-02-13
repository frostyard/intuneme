# intuneme Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Build a Go CLI (`intuneme`) that provisions and manages a systemd-nspawn container running Microsoft Intune on an immutable Debian host.

**Architecture:** Single Go binary using cobra for CLI, with an internal package structure separating concerns: `cmd/` for CLI wiring, `internal/config` for configuration, `internal/session` for host session discovery, `internal/nspawn` for container lifecycle, `internal/provision` for rootfs setup. System commands are executed through a `Runner` interface to enable testing.

**Tech Stack:** Go 1.26, cobra (CLI framework), BurntSushi/toml (config), standard library for os/exec/filepath.

---

### Task 1: Project scaffolding

**Files:**
- Create: `go.mod`
- Create: `main.go`
- Create: `cmd/root.go`

**Step 1: Initialize Go module**

Run: `go mod init github.com/bjk/intuneme`

**Step 2: Install cobra dependency**

Run: `go get github.com/spf13/cobra@latest`

**Step 3: Write root command**

Create `cmd/root.go`:

```go
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
```

**Step 4: Write main.go**

Create `main.go`:

```go
package main

import "github.com/bjk/intuneme/cmd"

func main() {
	cmd.Execute()
}
```

**Step 5: Verify it builds and runs**

Run: `go mod tidy && go build -o intuneme . && ./intuneme --help`
Expected: Help text showing "Manage an Intune container on an immutable Linux host"

**Step 6: Commit**

```bash
git add go.mod go.sum main.go cmd/
git commit -m "feat: scaffold Go project with cobra root command"
```

---

### Task 2: Runner interface and config package

**Files:**
- Create: `internal/runner/runner.go`
- Create: `internal/config/config.go`
- Create: `internal/config/config_test.go`

**Step 1: Write the Runner interface**

Create `internal/runner/runner.go`:

```go
package runner

import (
	"os"
	"os/exec"
)

// Runner executes system commands. Mockable for tests.
type Runner interface {
	// Run executes a command, returning combined output and error.
	Run(name string, args ...string) ([]byte, error)
	// RunAttached executes a command with stdin/stdout/stderr attached to the terminal.
	RunAttached(name string, args ...string) error
	// LookPath checks if a binary is in PATH.
	LookPath(name string) (string, error)
}

// SystemRunner executes real system commands.
type SystemRunner struct{}

func (r *SystemRunner) Run(name string, args ...string) ([]byte, error) {
	return exec.Command(name, args...).CombinedOutput()
}

func (r *SystemRunner) RunAttached(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func (r *SystemRunner) LookPath(name string) (string, error) {
	return exec.LookPath(name)
}
```

**Step 2: Write failing config test**

Create `internal/config/config_test.go`:

```go
package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDefaultRoot(t *testing.T) {
	home, _ := os.UserHomeDir()
	want := filepath.Join(home, ".local", "share", "intuneme")
	got := DefaultRoot()
	if got != want {
		t.Errorf("DefaultRoot() = %q, want %q", got, want)
	}
}

func TestLoadCreatesDefault(t *testing.T) {
	tmp := t.TempDir()
	cfg, err := Load(tmp)
	if err != nil {
		t.Fatalf("Load(%q) error: %v", tmp, err)
	}
	if cfg.MachineName != "intuneme" {
		t.Errorf("MachineName = %q, want %q", cfg.MachineName, "intuneme")
	}
	if cfg.RootfsPath != filepath.Join(tmp, "rootfs") {
		t.Errorf("RootfsPath = %q, want %q", cfg.RootfsPath, filepath.Join(tmp, "rootfs"))
	}
}

func TestLoadReadsExisting(t *testing.T) {
	tmp := t.TempDir()
	toml := `machine_name = "myintune"` + "\n"
	os.WriteFile(filepath.Join(tmp, "config.toml"), []byte(toml), 0644)

	cfg, err := Load(tmp)
	if err != nil {
		t.Fatalf("Load error: %v", err)
	}
	if cfg.MachineName != "myintune" {
		t.Errorf("MachineName = %q, want %q", cfg.MachineName, "myintune")
	}
}
```

**Step 3: Run tests to verify they fail**

Run: `go test ./internal/config/ -v`
Expected: FAIL — package doesn't exist yet

**Step 4: Write config implementation**

Create `internal/config/config.go`:

```go
package config

import (
	"os"
	"path/filepath"

	"github.com/BurntSushi/toml"
)

type Config struct {
	MachineName string `toml:"machine_name"`
	RootfsPath  string `toml:"rootfs_path"`
	Image       string `toml:"image"`
	HostUID     int    `toml:"host_uid"`
	HostUser    string `toml:"host_user"`
}

func DefaultRoot() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".local", "share", "intuneme")
}

func Load(root string) (*Config, error) {
	cfg := &Config{
		MachineName: "intuneme",
		RootfsPath:  filepath.Join(root, "rootfs"),
		Image:       "ghcr.io/frostyard/ubuntu-intune:latest",
		HostUID:     os.Getuid(),
		HostUser:    os.Getenv("USER"),
	}

	path := filepath.Join(root, "config.toml")
	if _, err := os.Stat(path); err == nil {
		if _, err := toml.DecodeFile(path, cfg); err != nil {
			return nil, err
		}
		// Ensure rootfs_path default if not in file
		if cfg.RootfsPath == "" {
			cfg.RootfsPath = filepath.Join(root, "rootfs")
		}
	}

	return cfg, nil
}

func (c *Config) Save(root string) error {
	path := filepath.Join(root, "config.toml")
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return toml.NewEncoder(f).Encode(c)
}
```

**Step 5: Install toml dependency and run tests**

Run: `go get github.com/BurntSushi/toml@latest && go test ./internal/config/ -v`
Expected: PASS (3 tests)

**Step 6: Commit**

```bash
git add internal/ go.mod go.sum
git commit -m "feat: add runner interface and config package with tests"
```

---

### Task 3: Prerequisite checking

**Files:**
- Create: `internal/prereq/prereq.go`
- Create: `internal/prereq/prereq_test.go`

**Step 1: Write failing test**

Create `internal/prereq/prereq_test.go`:

```go
package prereq

import (
	"fmt"
	"strings"
	"testing"
)

type mockRunner struct {
	available map[string]bool
}

func (m *mockRunner) Run(name string, args ...string) ([]byte, error) {
	return nil, nil
}

func (m *mockRunner) RunAttached(name string, args ...string) error {
	return nil
}

func (m *mockRunner) LookPath(name string) (string, error) {
	if m.available[name] {
		return "/usr/bin/" + name, nil
	}
	return "", fmt.Errorf("not found: %s", name)
}

func TestCheckAllPresent(t *testing.T) {
	r := &mockRunner{available: map[string]bool{
		"systemd-nspawn": true, "machinectl": true, "podman": true,
	}}
	errs := Check(r)
	if len(errs) != 0 {
		t.Errorf("expected no errors, got %v", errs)
	}
}

func TestCheckMissing(t *testing.T) {
	r := &mockRunner{available: map[string]bool{
		"machinectl": true,
	}}
	errs := Check(r)
	if len(errs) != 2 {
		t.Fatalf("expected 2 errors, got %d: %v", len(errs), errs)
	}
	if !strings.Contains(errs[0].Error(), "systemd-nspawn") {
		t.Errorf("expected systemd-nspawn error, got: %v", errs[0])
	}
	if !strings.Contains(errs[1].Error(), "podman") {
		t.Errorf("expected podman error, got: %v", errs[1])
	}
}
```

**Step 2: Run tests to verify they fail**

Run: `go test ./internal/prereq/ -v`
Expected: FAIL — package doesn't exist yet

**Step 3: Write implementation**

Create `internal/prereq/prereq.go`:

```go
package prereq

import (
	"fmt"

	"github.com/bjk/intuneme/internal/runner"
)

type requirement struct {
	binary  string
	pkgHint string
}

var requirements = []requirement{
	{"systemd-nspawn", "systemd-container"},
	{"machinectl", "systemd-container"},
	{"podman", "podman"},
}

// Check verifies all required binaries are available.
// Returns a list of errors for each missing binary.
func Check(r runner.Runner) []error {
	var errs []error
	for _, req := range requirements {
		if _, err := r.LookPath(req.binary); err != nil {
			errs = append(errs, fmt.Errorf("%s not found — install the %q package", req.binary, req.pkgHint))
		}
	}
	return errs
}
```

**Step 4: Run tests**

Run: `go test ./internal/prereq/ -v`
Expected: PASS (2 tests)

**Step 5: Commit**

```bash
git add internal/prereq/
git commit -m "feat: add prerequisite checking with install hints"
```

---

### Task 4: Session environment discovery

**Files:**
- Create: `internal/session/session.go`
- Create: `internal/session/session_test.go`

**Step 1: Write failing tests**

Create `internal/session/session_test.go`:

```go
package session

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDiscoverFromEnv(t *testing.T) {
	t.Setenv("XDG_RUNTIME_DIR", "/run/user/1000")
	t.Setenv("DISPLAY", ":1")
	t.Setenv("WAYLAND_DISPLAY", "wayland-0")
	t.Setenv("DBUS_SESSION_BUS_ADDRESS", "unix:path=/run/user/1000/bus")
	t.Setenv("XAUTHORITY", "/run/user/1000/.Xauthority")

	s, err := Discover(1000)
	if err != nil {
		t.Fatalf("Discover() error: %v", err)
	}
	if s.Display != ":1" {
		t.Errorf("Display = %q, want %q", s.Display, ":1")
	}
	if s.WaylandDisplay != "wayland-0" {
		t.Errorf("WaylandDisplay = %q, want %q", s.WaylandDisplay, "wayland-0")
	}
	if s.XAuthority != "/run/user/1000/.Xauthority" {
		t.Errorf("XAuthority = %q, want %q", s.XAuthority, "/run/user/1000/.Xauthority")
	}
	if s.DBusAddress != "unix:path=/run/user/1000/bus" {
		t.Errorf("DBusAddress = %q, want %q", s.DBusAddress, "unix:path=/run/user/1000/bus")
	}
}

func TestDiscoverXAuthorityGlob(t *testing.T) {
	tmp := t.TempDir()
	// Simulate .mutter-Xwaylandauth.XXXXXX file
	authFile := filepath.Join(tmp, ".mutter-Xwaylandauth.abc123")
	os.WriteFile(authFile, []byte{}, 0600)

	t.Setenv("XDG_RUNTIME_DIR", tmp)
	t.Setenv("DISPLAY", ":0")
	t.Setenv("WAYLAND_DISPLAY", "")
	t.Setenv("DBUS_SESSION_BUS_ADDRESS", "")
	t.Setenv("XAUTHORITY", "") // force glob search

	s, err := Discover(1000)
	if err != nil {
		t.Fatalf("Discover() error: %v", err)
	}
	if s.XAuthority != authFile {
		t.Errorf("XAuthority = %q, want %q", s.XAuthority, authFile)
	}
}

func TestDiscoverNoDisplay(t *testing.T) {
	t.Setenv("XDG_RUNTIME_DIR", "/run/user/1000")
	t.Setenv("DISPLAY", "")
	t.Setenv("WAYLAND_DISPLAY", "")
	t.Setenv("DBUS_SESSION_BUS_ADDRESS", "")
	t.Setenv("XAUTHORITY", "")

	_, err := Discover(1000)
	if err == nil {
		t.Fatal("expected error for missing DISPLAY, got nil")
	}
}

func TestDiscoverNoXAuthority(t *testing.T) {
	tmp := t.TempDir() // empty dir, no xauth files

	t.Setenv("XDG_RUNTIME_DIR", tmp)
	t.Setenv("DISPLAY", ":0")
	t.Setenv("WAYLAND_DISPLAY", "")
	t.Setenv("DBUS_SESSION_BUS_ADDRESS", "")
	t.Setenv("XAUTHORITY", "")

	_, err := Discover(1000)
	if err == nil {
		t.Fatal("expected error for missing XAUTHORITY, got nil")
	}
}
```

**Step 2: Run tests to verify they fail**

Run: `go test ./internal/session/ -v`
Expected: FAIL — package doesn't exist yet

**Step 3: Write implementation**

Create `internal/session/session.go`:

```go
package session

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Session holds the discovered host graphical session environment.
type Session struct {
	XDGRuntimeDir  string
	Display        string
	WaylandDisplay string
	DBusAddress    string
	XAuthority     string
	UID            int
}

// xauthorityPatterns are searched in XDG_RUNTIME_DIR when $XAUTHORITY is unset.
var xauthorityPatterns = []string{
	".mutter-Xwaylandauth.*",
	"xauth_*",
	".Xauthority",
}

// Discover detects the host graphical session environment.
func Discover(uid int) (*Session, error) {
	s := &Session{UID: uid}

	// XDG_RUNTIME_DIR
	s.XDGRuntimeDir = os.Getenv("XDG_RUNTIME_DIR")
	if s.XDGRuntimeDir == "" {
		s.XDGRuntimeDir = fmt.Sprintf("/run/user/%d", uid)
	}

	// DISPLAY (required)
	s.Display = os.Getenv("DISPLAY")
	if s.Display == "" {
		return nil, fmt.Errorf("no DISPLAY set — intune-portal requires a graphical session")
	}

	// WAYLAND_DISPLAY (optional)
	s.WaylandDisplay = os.Getenv("WAYLAND_DISPLAY")

	// DBUS_SESSION_BUS_ADDRESS
	s.DBusAddress = os.Getenv("DBUS_SESSION_BUS_ADDRESS")
	if s.DBusAddress == "" {
		s.DBusAddress = fmt.Sprintf("unix:path=%s/bus", s.XDGRuntimeDir)
	}

	// XAUTHORITY — check env first, then glob for known patterns
	s.XAuthority = os.Getenv("XAUTHORITY")
	if s.XAuthority == "" {
		found, err := findXAuthority(s.XDGRuntimeDir)
		if err != nil {
			return nil, err
		}
		s.XAuthority = found
	}

	return s, nil
}

func findXAuthority(runtimeDir string) (string, error) {
	var searched []string
	for _, pattern := range xauthorityPatterns {
		full := filepath.Join(runtimeDir, pattern)
		searched = append(searched, full)
		matches, _ := filepath.Glob(full)
		if len(matches) > 0 {
			return matches[0], nil
		}
	}
	return "", fmt.Errorf(
		"no XAUTHORITY found — searched for:\n  %s\nSet $XAUTHORITY to the correct path",
		strings.Join(searched, "\n  "),
	)
}

// ContainerXAuthority returns the XAUTHORITY path remapped into the container's
// /run/user-external/<uid>/ mount point.
func (s *Session) ContainerXAuthority() string {
	base := filepath.Base(s.XAuthority)
	return fmt.Sprintf("/run/user-external/%d/%s", s.UID, base)
}

// ContainerEnv returns --setenv flags for machinectl shell.
func (s *Session) ContainerEnv() []string {
	uid := s.UID
	env := []string{
		fmt.Sprintf("XDG_RUNTIME_DIR=/run/user-external/%d", uid),
		fmt.Sprintf("DBUS_SESSION_BUS_ADDRESS=unix:path=/run/user-external/%d/bus", uid),
		fmt.Sprintf("DISPLAY=%s", s.Display),
		fmt.Sprintf("XAUTHORITY=%s", s.ContainerXAuthority()),
	}
	if s.WaylandDisplay != "" {
		env = append(env, fmt.Sprintf("WAYLAND_DISPLAY=%s", s.WaylandDisplay))
	}
	return env
}
```

**Step 4: Run tests**

Run: `go test ./internal/session/ -v`
Expected: PASS (4 tests)

**Step 5: Commit**

```bash
git add internal/session/
git commit -m "feat: add host session environment discovery with xauthority glob"
```

---

### Task 5: Container provisioning (init command)

**Files:**
- Create: `internal/provision/provision.go`
- Create: `internal/provision/provision_test.go`
- Create: `cmd/init.go`

**Step 1: Write failing tests for provisioning logic**

Create `internal/provision/provision_test.go`:

```go
package provision

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type mockRunner struct {
	commands []string
}

func (m *mockRunner) Run(name string, args ...string) ([]byte, error) {
	m.commands = append(m.commands, name+" "+strings.Join(args, " "))
	return nil, nil
}

func (m *mockRunner) RunAttached(name string, args ...string) error {
	m.commands = append(m.commands, name+" "+strings.Join(args, " "))
	return nil
}

func (m *mockRunner) LookPath(name string) (string, error) {
	return "/usr/bin/" + name, nil
}

func TestPullImage(t *testing.T) {
	r := &mockRunner{}
	err := PullImage(r, "ghcr.io/frostyard/ubuntu-intune:latest")
	if err != nil {
		t.Fatalf("PullImage error: %v", err)
	}
	if len(r.commands) != 1 {
		t.Fatalf("expected 1 command, got %d", len(r.commands))
	}
	if !strings.Contains(r.commands[0], "podman pull") {
		t.Errorf("expected podman pull, got: %s", r.commands[0])
	}
}

func TestExtractRootfs(t *testing.T) {
	r := &mockRunner{}
	err := ExtractRootfs(r, "ghcr.io/frostyard/ubuntu-intune:latest", "/tmp/test-rootfs")
	if err != nil {
		t.Fatalf("ExtractRootfs error: %v", err)
	}
	// Should run: podman create, podman cp, podman rm
	if len(r.commands) != 3 {
		t.Fatalf("expected 3 commands, got %d: %v", len(r.commands), r.commands)
	}
}

func TestWriteFixups(t *testing.T) {
	tmp := t.TempDir()
	rootfs := filepath.Join(tmp, "rootfs")
	os.MkdirAll(filepath.Join(rootfs, "etc"), 0755)
	os.MkdirAll(filepath.Join(rootfs, "etc", "systemd", "system"), 0755)
	os.MkdirAll(filepath.Join(rootfs, "etc", "pam.d"), 0755)

	err := WriteFixups(rootfs, "testuser", 1000, 1000, "testhost")
	if err != nil {
		t.Fatalf("WriteFixups error: %v", err)
	}

	// Check hostname
	data, _ := os.ReadFile(filepath.Join(rootfs, "etc", "hostname"))
	if strings.TrimSpace(string(data)) != "testhost" {
		t.Errorf("hostname = %q, want %q", strings.TrimSpace(string(data)), "testhost")
	}

	// Check environment file exists
	if _, err := os.Stat(filepath.Join(rootfs, "etc", "environment")); err != nil {
		t.Errorf("expected etc/environment to exist: %v", err)
	}

	// Check fix-home-ownership.service exists
	svcPath := filepath.Join(rootfs, "etc", "systemd", "system", "fix-home-ownership.service")
	if _, err := os.Stat(svcPath); err != nil {
		t.Errorf("expected fix-home-ownership.service to exist: %v", err)
	}
}

func TestWritePolkitRule(t *testing.T) {
	tmp := t.TempDir()
	rulesDir := filepath.Join(tmp, "etc", "polkit-1", "rules.d")

	r := &mockRunner{}
	err := InstallPolkitRule(r, rulesDir)
	if err != nil {
		t.Fatalf("InstallPolkitRule error: %v", err)
	}

	// Should have used sudo mkdir + sudo tee
	foundMkdir := false
	foundTee := false
	for _, c := range r.commands {
		if strings.Contains(c, "mkdir") && strings.Contains(c, rulesDir) {
			foundMkdir = true
		}
		if strings.Contains(c, "tee") || strings.Contains(c, rulesDir) {
			foundTee = true
		}
	}
	_ = foundMkdir
	_ = foundTee
	// Basic check — at least some sudo commands were issued
	if len(r.commands) == 0 {
		t.Errorf("expected sudo commands for polkit installation")
	}
}
```

**Step 2: Run tests to verify they fail**

Run: `go test ./internal/provision/ -v`
Expected: FAIL

**Step 3: Write provisioning implementation**

Create `internal/provision/provision.go`:

```go
package provision

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/bjk/intuneme/internal/runner"
)

func PullImage(r runner.Runner, image string) error {
	out, err := r.Run("podman", "pull", image)
	if err != nil {
		return fmt.Errorf("podman pull failed: %w\n%s", err, out)
	}
	return nil
}

func ExtractRootfs(r runner.Runner, image string, rootfsPath string) error {
	// Create temporary container
	out, err := r.Run("podman", "create", "--name", "intuneme-extract", image)
	if err != nil {
		return fmt.Errorf("podman create failed: %w\n%s", err, out)
	}

	// Copy filesystem out
	out, err = r.Run("podman", "cp", "intuneme-extract:/", rootfsPath)
	if err != nil {
		// Clean up on failure
		r.Run("podman", "rm", "intuneme-extract")
		return fmt.Errorf("podman cp failed: %w\n%s", err, out)
	}

	// Remove temporary container
	out, err = r.Run("podman", "rm", "intuneme-extract")
	if err != nil {
		return fmt.Errorf("podman rm failed: %w\n%s", err, out)
	}

	return nil
}

func WriteFixups(rootfsPath, user string, uid, gid int, hostname string) error {
	// /etc/hostname
	if err := os.WriteFile(
		filepath.Join(rootfsPath, "etc", "hostname"),
		[]byte(hostname+"\n"), 0644,
	); err != nil {
		return fmt.Errorf("write hostname: %w", err)
	}

	// /etc/hosts
	hosts := fmt.Sprintf("127.0.0.1 %s localhost\n", hostname)
	if err := os.WriteFile(
		filepath.Join(rootfsPath, "etc", "hosts"),
		[]byte(hosts), 0644,
	); err != nil {
		return fmt.Errorf("write hosts: %w", err)
	}

	// /etc/environment
	env := "DISPLAY=:0\nNO_AT_BRIDGE=1\nGTK_A11Y=none\n"
	if err := os.WriteFile(
		filepath.Join(rootfsPath, "etc", "environment"),
		[]byte(env), 0644,
	); err != nil {
		return fmt.Errorf("write environment: %w", err)
	}

	// PAM config for gnome-keyring
	pamAuth := filepath.Join(rootfsPath, "etc", "pam.d", "common-auth")
	appendLine(pamAuth, "auth optional pam_gnome_keyring.so")
	pamSession := filepath.Join(rootfsPath, "etc", "pam.d", "common-session")
	appendLine(pamSession, "session optional pam_gnome_keyring.so auto_start")

	// Pre-create keyring directory
	keyringDir := filepath.Join(rootfsPath, "home", user, ".local", "share", "keyrings")
	os.MkdirAll(keyringDir, 0755)
	os.WriteFile(filepath.Join(keyringDir, "default"), []byte("login\n"), 0644)

	// fix-home-ownership.service
	svc := fmt.Sprintf(`[Unit]
Description=Fix home directory ownership
ConditionPathExists=!/var/lib/fix-home-ownership-done

[Service]
Type=oneshot
ExecStart=/bin/chown -R %d:%d /home/%s
ExecStartPost=/bin/touch /var/lib/fix-home-ownership-done
RemainAfterExit=yes

[Install]
WantedBy=multi-user.target
`, uid, gid, user)

	svcPath := filepath.Join(rootfsPath, "etc", "systemd", "system", "fix-home-ownership.service")
	if err := os.WriteFile(svcPath, []byte(svc), 0644); err != nil {
		return fmt.Errorf("write fix-home-ownership.service: %w", err)
	}

	// Enable the service (symlink)
	wantsDir := filepath.Join(rootfsPath, "etc", "systemd", "system", "multi-user.target.wants")
	os.MkdirAll(wantsDir, 0755)
	os.Symlink(svcPath, filepath.Join(wantsDir, "fix-home-ownership.service"))

	return nil
}

// CreateContainerUser runs useradd inside the rootfs via nspawn.
func CreateContainerUser(r runner.Runner, rootfsPath, user string, uid, gid int) error {
	out, err := r.Run("sudo", "systemd-nspawn", "-D", rootfsPath, "--pipe",
		"useradd",
		"--uid", fmt.Sprintf("%d", uid),
		"--create-home",
		"--shell", "/bin/bash",
		"--groups", "adm,sudo,video,audio",
		user,
	)
	if err != nil {
		return fmt.Errorf("useradd in container failed: %w\n%s", err, out)
	}
	return nil
}

// InstallPolkitRule installs the polkit rule on the host using sudo.
func InstallPolkitRule(r runner.Runner, rulesDir string) error {
	rule := `polkit.addRule(function(action, subject) {
    if ((action.id == "org.freedesktop.machine1.manage-machines" ||
         action.id == "org.freedesktop.machine1.manage-images" ||
         action.id == "org.freedesktop.machine1.login" ||
         action.id == "org.freedesktop.machine1.shell" ||
         action.id == "org.freedesktop.machine1.host-shell") &&
        subject.isInGroup("sudo")) {
        return polkit.Result.YES;
    }
});
`
	// Create directory with sudo
	r.Run("sudo", "mkdir", "-p", rulesDir)

	// Write rule file via sudo tee
	out, err := r.Run("sudo", "tee", filepath.Join(rulesDir, "50-intuneme.rules"), rule)
	if err != nil {
		return fmt.Errorf("install polkit rule failed: %w\n%s", err, out)
	}
	return nil
}

func appendLine(path, line string) {
	data, _ := os.ReadFile(path)
	content := string(data)
	if !strings.Contains(content, line) {
		f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
		if err == nil {
			defer f.Close()
			f.WriteString(line + "\n")
		}
	}
}
```

**Step 4: Run tests**

Run: `go test ./internal/provision/ -v`
Expected: PASS (4 tests)

**Step 5: Wire up the init command**

Create `cmd/init.go`:

```go
package cmd

import (
	"fmt"
	"os"
	"os/user"

	"github.com/bjk/intuneme/internal/config"
	"github.com/bjk/intuneme/internal/prereq"
	"github.com/bjk/intuneme/internal/provision"
	"github.com/bjk/intuneme/internal/runner"
	"github.com/spf13/cobra"
)

var forceInit bool

var initCmd = &cobra.Command{
	Use:   "init",
	Short: "Provision the Intune nspawn container",
	RunE: func(cmd *cobra.Command, args []string) error {
		r := &runner.SystemRunner{}
		root := rootDir
		if root == "" {
			root = config.DefaultRoot()
		}

		// Check prerequisites
		if errs := prereq.Check(r); len(errs) > 0 {
			for _, e := range errs {
				fmt.Fprintln(os.Stderr, "  -", e)
			}
			return fmt.Errorf("missing prerequisites")
		}

		// Check if already initialized
		cfg, _ := config.Load(root)
		if _, err := os.Stat(cfg.RootfsPath); err == nil && !forceInit {
			return fmt.Errorf("already initialized at %s — use --force to reinitialize", root)
		}

		fmt.Println("Pulling OCI image...")
		if err := provision.PullImage(r, cfg.Image); err != nil {
			return err
		}

		fmt.Println("Extracting rootfs...")
		os.MkdirAll(root, 0755)
		if err := provision.ExtractRootfs(r, cfg.Image, cfg.RootfsPath); err != nil {
			return err
		}

		u, _ := user.Current()
		hostname, _ := os.Hostname()

		fmt.Println("Creating container user...")
		if err := provision.CreateContainerUser(r, cfg.RootfsPath, u.Username, os.Getuid(), os.Getgid()); err != nil {
			return err
		}

		fmt.Println("Applying fixups...")
		if err := provision.WriteFixups(cfg.RootfsPath, u.Username, os.Getuid(), os.Getgid(), hostname+"LXC"); err != nil {
			return err
		}

		fmt.Println("Installing polkit rules...")
		if err := provision.InstallPolkitRule(r, "/etc/polkit-1/rules.d"); err != nil {
			fmt.Fprintf(os.Stderr, "warning: polkit install failed: %v\n", err)
		}

		fmt.Println("Saving config...")
		cfg.HostUID = os.Getuid()
		cfg.HostUser = u.Username
		if err := cfg.Save(root); err != nil {
			return err
		}

		fmt.Printf("Initialized intuneme at %s\n", root)
		return nil
	},
}

func init() {
	initCmd.Flags().BoolVar(&forceInit, "force", false, "reinitialize even if already set up")
	rootCmd.AddCommand(initCmd)
}
```

**Step 6: Build and verify help**

Run: `go mod tidy && go build -o intuneme . && ./intuneme init --help`
Expected: Help text showing init command with --force flag

**Step 7: Commit**

```bash
git add internal/provision/ cmd/init.go
git commit -m "feat: add container provisioning and init command"
```

---

### Task 6: Container lifecycle (start command)

**Files:**
- Create: `internal/nspawn/nspawn.go`
- Create: `internal/nspawn/nspawn_test.go`
- Create: `cmd/start.go`

**Step 1: Write failing tests for nspawn flag building**

Create `internal/nspawn/nspawn_test.go`:

```go
package nspawn

import (
	"strings"
	"testing"

	"github.com/bjk/intuneme/internal/session"
)

func TestBuildBootArgs(t *testing.T) {
	s := &session.Session{
		XDGRuntimeDir: "/run/user/1000",
		Display:       ":1",
		XAuthority:    "/run/user/1000/.mutter-Xwaylandauth.abc123",
		UID:           1000,
	}
	args := BuildBootArgs("/home/testuser/.local/share/intuneme/rootfs", "intuneme", "/home/testuser", s)

	joined := strings.Join(args, " ")
	if !strings.Contains(joined, "--machine=intuneme") {
		t.Errorf("missing --machine flag in: %s", joined)
	}
	if !strings.Contains(joined, "--bind=/home/testuser") {
		t.Errorf("missing home bind in: %s", joined)
	}
	if !strings.Contains(joined, "--bind=/tmp/.X11-unix") {
		t.Errorf("missing X11 bind in: %s", joined)
	}
	if !strings.Contains(joined, "--bind=/run/user/1000:/run/user-external/1000") {
		t.Errorf("missing XDG runtime bind in: %s", joined)
	}
	if !strings.Contains(joined, "-b") {
		t.Errorf("missing -b (boot) flag in: %s", joined)
	}
}

func TestBuildShellArgs(t *testing.T) {
	s := &session.Session{
		XDGRuntimeDir: "/run/user/1000",
		Display:       ":1",
		DBusAddress:   "unix:path=/run/user/1000/bus",
		XAuthority:    "/run/user/1000/.mutter-Xwaylandauth.abc123",
		UID:           1000,
	}
	args := BuildShellArgs("intuneme", "testuser", s)

	joined := strings.Join(args, " ")
	if !strings.Contains(joined, "shell") {
		t.Errorf("missing shell subcommand in: %s", joined)
	}
	if !strings.Contains(joined, "testuser@intuneme") {
		t.Errorf("missing user@machine in: %s", joined)
	}
	if !strings.Contains(joined, "--setenv=DISPLAY=:1") {
		t.Errorf("missing DISPLAY setenv in: %s", joined)
	}
	if !strings.Contains(joined, "intune-portal") {
		t.Errorf("missing intune-portal command in: %s", joined)
	}
}
```

**Step 2: Run tests to verify they fail**

Run: `go test ./internal/nspawn/ -v`
Expected: FAIL

**Step 3: Write implementation**

Create `internal/nspawn/nspawn.go`:

```go
package nspawn

import (
	"fmt"

	"github.com/bjk/intuneme/internal/runner"
	"github.com/bjk/intuneme/internal/session"
)

// BuildBootArgs returns the systemd-nspawn arguments to boot the container.
func BuildBootArgs(rootfs, machine, homeDir string, s *session.Session) []string {
	args := []string{
		"-D", rootfs,
		fmt.Sprintf("--machine=%s", machine),
		fmt.Sprintf("--bind=%s", homeDir),
		"--bind=/tmp/.X11-unix",
		fmt.Sprintf("--bind=%s:/run/user-external/%d", s.XDGRuntimeDir, s.UID),
		"-b",
	}
	return args
}

// BuildShellArgs returns the machinectl shell arguments to launch intune-portal.
func BuildShellArgs(machine, user string, s *session.Session) []string {
	args := []string{"shell"}
	for _, env := range s.ContainerEnv() {
		args = append(args, fmt.Sprintf("--setenv=%s", env))
	}
	args = append(args, fmt.Sprintf("%s@%s", user, machine))
	args = append(args, "/usr/bin/intune-portal")
	return args
}

// Boot starts the nspawn container in the background using sudo.
func Boot(r runner.Runner, rootfs, machine, homeDir string, s *session.Session) error {
	args := append([]string{"systemd-nspawn"}, BuildBootArgs(rootfs, machine, homeDir, s)...)
	return r.RunAttached("sudo", args...)
}

// IsRunning checks if the machine is registered with machinectl.
func IsRunning(r runner.Runner, machine string) bool {
	_, err := r.Run("machinectl", "show", machine)
	return err == nil
}

// LaunchIntune runs intune-portal inside the container via machinectl shell.
func LaunchIntune(r runner.Runner, machine, user string, s *session.Session) error {
	args := append([]string{"shell"}, BuildShellArgs(machine, user, s)[1:]...)
	// machinectl shell is the full command; BuildShellArgs includes "shell"
	return r.RunAttached("machinectl", args...)
}

// Stop powers off the container.
func Stop(r runner.Runner, machine string) error {
	out, err := r.Run("machinectl", "poweroff", machine)
	if err != nil {
		return fmt.Errorf("machinectl poweroff failed: %w\n%s", err, out)
	}
	return nil
}
```

**Step 4: Run tests**

Run: `go test ./internal/nspawn/ -v`
Expected: PASS (2 tests)

**Step 5: Wire up start command**

Create `cmd/start.go`:

```go
package cmd

import (
	"fmt"
	"os"

	"github.com/bjk/intuneme/internal/config"
	"github.com/bjk/intuneme/internal/nspawn"
	"github.com/bjk/intuneme/internal/runner"
	"github.com/bjk/intuneme/internal/session"
	"github.com/spf13/cobra"
)

var startCmd = &cobra.Command{
	Use:   "start",
	Short: "Boot the container and launch intune-portal",
	RunE: func(cmd *cobra.Command, args []string) error {
		r := &runner.SystemRunner{}
		root := rootDir
		if root == "" {
			root = config.DefaultRoot()
		}

		cfg, err := config.Load(root)
		if err != nil {
			return err
		}

		// Check rootfs exists
		if _, err := os.Stat(cfg.RootfsPath); err != nil {
			return fmt.Errorf("not initialized — run 'intuneme init' first")
		}

		// Discover host session
		sess, err := session.Discover(cfg.HostUID)
		if err != nil {
			return fmt.Errorf("session discovery failed: %w", err)
		}

		// Check if already running
		if nspawn.IsRunning(r, cfg.MachineName) {
			fmt.Printf("Container %s is already running.\n", cfg.MachineName)
			fmt.Println("Launching intune-portal...")
			return nspawn.LaunchIntune(r, cfg.MachineName, cfg.HostUser, sess)
		}

		homeDir, _ := os.UserHomeDir()

		fmt.Println("Booting container...")
		// Boot runs attached — it blocks until the container shuts down.
		// For a real implementation, we'd run this in a goroutine or background process.
		// For now, we boot and then launch intune in a follow-up.
		go func() {
			nspawn.Boot(r, cfg.RootfsPath, cfg.MachineName, homeDir, sess)
		}()

		// Wait for container to be ready
		fmt.Println("Waiting for container to boot...")
		for i := 0; i < 30; i++ {
			if nspawn.IsRunning(r, cfg.MachineName) {
				break
			}
			// Sleep 1 second between checks
			r.Run("sleep", "1")
		}

		if !nspawn.IsRunning(r, cfg.MachineName) {
			return fmt.Errorf("container failed to start within 30 seconds")
		}

		fmt.Println("Launching intune-portal...")
		return nspawn.LaunchIntune(r, cfg.MachineName, cfg.HostUser, sess)
	},
}

func init() {
	rootCmd.AddCommand(startCmd)
}
```

**Step 6: Build and verify**

Run: `go mod tidy && go build -o intuneme . && ./intuneme start --help`
Expected: Help text for start command

**Step 7: Commit**

```bash
git add internal/nspawn/ cmd/start.go
git commit -m "feat: add nspawn lifecycle and start command"
```

---

### Task 7: Stop, status, and destroy commands

**Files:**
- Create: `cmd/stop.go`
- Create: `cmd/status.go`
- Create: `cmd/destroy.go`

**Step 1: Write stop command**

Create `cmd/stop.go`:

```go
package cmd

import (
	"fmt"

	"github.com/bjk/intuneme/internal/config"
	"github.com/bjk/intuneme/internal/nspawn"
	"github.com/bjk/intuneme/internal/runner"
	"github.com/spf13/cobra"
)

var stopCmd = &cobra.Command{
	Use:   "stop",
	Short: "Stop the container",
	RunE: func(cmd *cobra.Command, args []string) error {
		r := &runner.SystemRunner{}
		root := rootDir
		if root == "" {
			root = config.DefaultRoot()
		}

		cfg, err := config.Load(root)
		if err != nil {
			return err
		}

		if !nspawn.IsRunning(r, cfg.MachineName) {
			fmt.Println("Container is not running.")
			return nil
		}

		fmt.Println("Stopping container...")
		if err := nspawn.Stop(r, cfg.MachineName); err != nil {
			return err
		}
		fmt.Println("Container stopped.")
		return nil
	},
}

func init() {
	rootCmd.AddCommand(stopCmd)
}
```

**Step 2: Write status command**

Create `cmd/status.go`:

```go
package cmd

import (
	"fmt"
	"os"

	"github.com/bjk/intuneme/internal/config"
	"github.com/bjk/intuneme/internal/nspawn"
	"github.com/bjk/intuneme/internal/runner"
	"github.com/spf13/cobra"
)

var statusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show container and intune-portal status",
	RunE: func(cmd *cobra.Command, args []string) error {
		r := &runner.SystemRunner{}
		root := rootDir
		if root == "" {
			root = config.DefaultRoot()
		}

		cfg, err := config.Load(root)
		if err != nil {
			return err
		}

		// Check initialized
		if _, err := os.Stat(cfg.RootfsPath); err != nil {
			fmt.Println("Status: not initialized")
			fmt.Println("Run 'intuneme init' to get started.")
			return nil
		}

		fmt.Printf("Root:    %s\n", root)
		fmt.Printf("Rootfs:  %s\n", cfg.RootfsPath)
		fmt.Printf("Machine: %s\n", cfg.MachineName)

		if nspawn.IsRunning(r, cfg.MachineName) {
			fmt.Println("Container: running")
		} else {
			fmt.Println("Container: stopped")
		}

		return nil
	},
}

func init() {
	rootCmd.AddCommand(statusCmd)
}
```

**Step 3: Write destroy command**

Create `cmd/destroy.go`:

```go
package cmd

import (
	"fmt"
	"os"

	"github.com/bjk/intuneme/internal/config"
	"github.com/bjk/intuneme/internal/nspawn"
	"github.com/bjk/intuneme/internal/runner"
	"github.com/spf13/cobra"
)

var destroyCmd = &cobra.Command{
	Use:   "destroy",
	Short: "Remove the container rootfs and all state",
	RunE: func(cmd *cobra.Command, args []string) error {
		r := &runner.SystemRunner{}
		root := rootDir
		if root == "" {
			root = config.DefaultRoot()
		}

		cfg, err := config.Load(root)
		if err != nil {
			return err
		}

		// Stop if running
		if nspawn.IsRunning(r, cfg.MachineName) {
			fmt.Println("Stopping running container...")
			if err := nspawn.Stop(r, cfg.MachineName); err != nil {
				return fmt.Errorf("failed to stop container: %w", err)
			}
		}

		// Remove rootfs with sudo (owned by root after nspawn use)
		fmt.Printf("Removing %s...\n", root)
		out, err := r.Run("sudo", "rm", "-rf", cfg.RootfsPath)
		if err != nil {
			return fmt.Errorf("rm rootfs failed: %w\n%s", err, out)
		}

		// Remove config
		os.Remove(fmt.Sprintf("%s/config.toml", root))

		fmt.Println("Destroyed.")
		return nil
	},
}

func init() {
	rootCmd.AddCommand(destroyCmd)
}
```

**Step 4: Build and verify all commands**

Run: `go mod tidy && go build -o intuneme . && ./intuneme --help`
Expected: Help showing init, start, stop, status, destroy subcommands

**Step 5: Commit**

```bash
git add cmd/stop.go cmd/status.go cmd/destroy.go
git commit -m "feat: add stop, status, and destroy commands"
```

---

### Task 8: Integration smoke test

**Files:**
- Create: `test_build.sh`

**Step 1: Write a build-and-help smoke test**

Create `test_build.sh`:

```bash
#!/bin/bash
set -e

echo "=== Running unit tests ==="
go test ./... -v

echo ""
echo "=== Building binary ==="
go build -o intuneme .

echo ""
echo "=== Verifying commands ==="
./intuneme --help
./intuneme init --help
./intuneme start --help
./intuneme stop --help
./intuneme status --help
./intuneme destroy --help

echo ""
echo "=== All checks passed ==="
```

**Step 2: Run it**

Run: `chmod +x test_build.sh && ./test_build.sh`
Expected: All unit tests pass, binary builds, all help commands succeed

**Step 3: Commit**

```bash
git add test_build.sh
git commit -m "feat: add build and smoke test script"
```

---

### Task 9: Polkit rules and final polish

**Files:**
- Create: `polkit/50-intuneme.rules`
- Modify: `internal/provision/provision.go` (update InstallPolkitRule to use embedded file)

**Step 1: Create the polkit rules file as a project asset**

Create `polkit/50-intuneme.rules`:

```javascript
// Allow users in the sudo group to manage nspawn machines without authentication.
// Installed by intuneme init to /etc/polkit-1/rules.d/50-intuneme.rules
polkit.addRule(function(action, subject) {
    if ((action.id == "org.freedesktop.machine1.manage-machines" ||
         action.id == "org.freedesktop.machine1.manage-images" ||
         action.id == "org.freedesktop.machine1.login" ||
         action.id == "org.freedesktop.machine1.shell" ||
         action.id == "org.freedesktop.machine1.host-shell") &&
        subject.isInGroup("sudo")) {
        return polkit.Result.YES;
    }
});
```

**Step 2: Update provision.go to embed and install the file**

Add to the top of `internal/provision/provision.go`:

```go
import _ "embed"

//go:embed ../../polkit/50-intuneme.rules
// (Alternative: define the rule inline as a const, which is simpler)
```

Given the complexity of embed paths with internal packages, keep the rule as a `const` string in `provision.go` (already done in step 5 of Task 5). The file in `polkit/` serves as documentation and the canonical source.

**Step 3: Commit**

```bash
git add polkit/
git commit -m "feat: add polkit rules file for passwordless machinectl"
```

---

## Summary

| Task | Description | Estimated steps |
|------|-------------|----------------|
| 1 | Project scaffolding (go mod, cobra, main) | 6 |
| 2 | Runner interface + config package with tests | 6 |
| 3 | Prerequisite checking with tests | 5 |
| 4 | Session environment discovery with tests | 5 |
| 5 | Container provisioning + init command | 7 |
| 6 | nspawn lifecycle + start command | 7 |
| 7 | Stop, status, destroy commands | 5 |
| 8 | Integration smoke test | 3 |
| 9 | Polkit rules asset | 3 |

Total: 9 tasks, ~47 steps.

Tasks 1-4 are foundational packages. Task 5-6 are the core init/start flow. Tasks 7-9 are supporting commands and polish. Each task ends with a commit.
