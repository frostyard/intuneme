# intuneme v2 Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Rework the intuneme CLI to run Edge + Intune inside the container, using `~/Intune` as the container home, eliminating host session forwarding complexity.

**Architecture:** Booted nspawn container with its own systemd, D-Bus, and keyring. Host provides display (X11/Wayland), audio (PipeWire), and GPU via individual socket/device binds. A `profile.d` script inside the container handles environment setup on login. No more embedded startup script.

**Tech Stack:** Go, systemd-nspawn, machinectl, cobra

**Design doc:** `docs/plans/2026-02-13-intuneme-v2-design.md`

---

### Task 1: Validate machinectl shell (manual, hands-on)

This is a manual development cycle to confirm machinectl shell works with the simplified bind mount scheme before building the rest of the CLI around it.

**Prerequisite:** You need an existing rootfs from `intuneme init`. If the container is currently running, stop it first with `machinectl poweroff intuneme`.

**Step 1: Create ~/Intune directory**

```bash
mkdir -p ~/Intune
```

**Step 2: Boot container with new bind mounts**

```bash
ROOTFS=~/.local/share/intuneme/rootfs
USER=$(whoami)
UID_NUM=$(id -u)

sudo systemd-nspawn \
  -D "$ROOTFS" \
  --machine=intuneme-v2 \
  --bind="$HOME/Intune:/home/$USER" \
  --bind=/tmp/.X11-unix \
  --bind=/dev/dri \
  --console=pipe \
  -b &
```

**Step 3: Wait for boot and test machinectl shell**

```bash
# Wait ~10 seconds for boot
sleep 10
machinectl list

# Try machinectl shell
machinectl shell $USER@intuneme-v2
```

**Step 4: Evaluate results**

If machinectl shell gives you an interactive session:
- Run `echo $XDG_RUNTIME_DIR` — should be `/run/user/<uid>`
- Run `busctl --user list` — should show D-Bus services
- Run `id` — should show your user with correct UID
- **Result: proceed with machinectl shell as the `intuneme shell` implementation**

If machinectl shell produces no output or hangs:
- Try fallback: `systemd-run -M intuneme-v2 --uid=$USER -t /bin/bash -l`
- If fallback works, note it and we'll use that instead
- **Result: adjust Task 6 and Task 7 to use the fallback**

**Step 5: Test optional socket binds (if machinectl shell works)**

Stop the container and reboot with optional sockets:

```bash
machinectl poweroff intuneme-v2
sleep 3

# Build bind args conditionally
BINDS="--bind=$HOME/Intune:/home/$USER --bind=/tmp/.X11-unix --bind=/dev/dri"
[ -S /run/user/$UID_NUM/wayland-0 ] && BINDS="$BINDS --bind=/run/user/$UID_NUM/wayland-0:/run/host-wayland"
[ -S /run/user/$UID_NUM/pipewire-0 ] && BINDS="$BINDS --bind=/run/user/$UID_NUM/pipewire-0:/run/host-pipewire"

sudo systemd-nspawn -D "$ROOTFS" --machine=intuneme-v2 $BINDS --console=pipe -b &
sleep 10

machinectl shell $USER@intuneme-v2
# Inside: check if sockets are visible
ls -la /run/host-wayland 2>/dev/null
ls -la /run/host-pipewire 2>/dev/null
```

**Step 6: Clean up**

```bash
machinectl poweroff intuneme-v2
```

**Step 7: Record results in notes.md**

Document what worked and what didn't. This determines whether Tasks 6-7 use `machinectl shell` or the `systemd-run` fallback.

---

### Task 2: Delete internal/session package and start-intune.sh

**Files:**
- Delete: `internal/session/session.go`
- Delete: `internal/provision/start-intune.sh`

These are no longer needed. Compilation will break — that's fine, subsequent tasks fix the imports.

**Step 1: Delete files**

Delete `internal/session/session.go` and `internal/provision/start-intune.sh`.

**Step 2: Remove the go:embed directive from provision.go**

In `internal/provision/provision.go`, remove lines 3-4:

```go
// Remove these two lines:
//go:embed start-intune.sh
var startIntuneScript []byte
```

Also remove the `_ "embed"` import.

**Step 3: Remove start-intune.sh installation from WriteFixups**

In `internal/provision/provision.go` `WriteFixups()`, remove the block at ~line 166-169 that installs start-intune.sh:

```go
// Remove this block:
scriptDir := filepath.Join(rootfsPath, "opt", "intuneme")
os.MkdirAll(scriptDir, 0755)
if err := os.WriteFile(filepath.Join(scriptDir, "start-intune.sh"), startIntuneScript, 0755); err != nil {
    return fmt.Errorf("write start-intune.sh: %w", err)
}
```

**Step 4: Remove keyring pre-creation from WriteFixups**

In `internal/provision/provision.go` `WriteFixups()`, remove the keyring directory block at ~line 136-138:

```go
// Remove this block:
keyringDir := filepath.Join(rootfsPath, "home", user, ".local", "share", "keyrings")
os.MkdirAll(keyringDir, 0755)
os.WriteFile(filepath.Join(keyringDir, "default"), []byte("login\n"), 0644)
```

**Step 5: Remove PAM login file rewrite from WriteFixups**

In `internal/provision/provision.go` `WriteFixups()`, remove the PAM login block at ~line 172-176:

```go
// Remove this block:
pamLogin := filepath.Join(rootfsPath, "etc", "pam.d", "login")
if err := os.WriteFile(pamLogin, []byte("@include common-auth\n@include common-account\n@include common-session\n"), 0644); err != nil {
    return fmt.Errorf("write pam.d/login: %w", err)
}
```

**Step 6: Commit**

```bash
git add -u
git commit -m "refactor: delete session package and start-intune.sh for v2"
```

---

### Task 3: Create profile.d/intuneme.sh (embedded resource)

**Files:**
- Create: `internal/provision/intuneme-profile.sh`
- Modify: `internal/provision/provision.go`

This replaces `start-intune.sh`. Instead of a manually-launched startup script, the container gets a `/etc/profile.d/` script that runs on every login shell. Based on the proven mkosi `extra.sh` pattern.

**Step 1: Create the profile script**

Create `internal/provision/intuneme-profile.sh`:

```bash
#!/bin/bash
# /etc/profile.d/intuneme.sh — runs on login in the intuneme container.
# Sets display/audio environment, imports into systemd user session,
# and initializes gnome-keyring on first login after boot.

# Display environment
export DISPLAY=:0
export NO_AT_BRIDGE=1
export GTK_A11Y=none
export PATH="/opt/microsoft/intune/bin:$PATH"

# Import into systemd user session so user services (broker) see them
systemctl --user import-environment DISPLAY PATH NO_AT_BRIDGE GTK_A11Y 2>/dev/null

# Wayland socket (bind-mounted from host at /run/host-wayland)
if [ -S /run/host-wayland ]; then
    export WAYLAND_DISPLAY=/run/host-wayland
    systemctl --user import-environment WAYLAND_DISPLAY 2>/dev/null
fi

# PipeWire socket (bind-mounted from host at /run/host-pipewire)
if [ -S /run/host-pipewire ]; then
    export PIPEWIRE_REMOTE=/run/host-pipewire
    systemctl --user import-environment PIPEWIRE_REMOTE 2>/dev/null
fi

# Initialize gnome-keyring once per boot.
# The keyring must be unlocked for microsoft-identity-broker to store credentials.
_keyring_dir="$HOME/.local/share/keyrings"
_keyring_init_marker="$_keyring_dir/.init_done"

if [ ! -f "$_keyring_init_marker" ]; then
    mkdir -p "$_keyring_dir"
    if [ ! -f "$_keyring_dir/default" ]; then
        echo "login" > "$_keyring_dir/default"
    fi
    printf '' | gnome-keyring-daemon --replace --unlock --components=secrets,pkcs11 -d 2>/dev/null
    sleep 1
    touch "$_keyring_init_marker"
fi

# Start intune agent timer if not running
if ! systemctl -q --user is-active intune-agent.timer 2>/dev/null; then
    systemctl --user start intune-agent.timer 2>/dev/null
fi
```

**Step 2: Add go:embed for the new script in provision.go**

In `internal/provision/provision.go`, add:

```go
import (
    _ "embed"
    // ... existing imports
)

//go:embed intuneme-profile.sh
var intuneProfileScript []byte
```

**Step 3: Add profile script installation to WriteFixups**

Add to the end of `WriteFixups()` (before `return nil`):

```go
// /etc/profile.d/intuneme.sh — login environment setup
profileDir := filepath.Join(rootfsPath, "etc", "profile.d")
os.MkdirAll(profileDir, 0755)
if err := os.WriteFile(filepath.Join(profileDir, "intuneme.sh"), intuneProfileScript, 0755); err != nil {
    return fmt.Errorf("write profile.d/intuneme.sh: %w", err)
}
```

**Step 4: Add broker display override to WriteFixups**

The identity broker is a systemd user service and needs `DISPLAY=:0` set explicitly since it starts before any login shell runs the profile script.

Add to `WriteFixups()`:

```go
// Broker display override — broker starts before any login shell
brokerOverrideDir := filepath.Join(rootfsPath, "usr", "lib", "systemd", "user",
    "microsoft-identity-broker.service.d")
os.MkdirAll(brokerOverrideDir, 0755)
if err := os.WriteFile(filepath.Join(brokerOverrideDir, "display.conf"),
    []byte("[Service]\nEnvironment=\"DISPLAY=:0\"\n"), 0644); err != nil {
    return fmt.Errorf("write broker display override: %w", err)
}
```

**Step 5: Verify it compiles**

Run: `go build ./...`
Expected: compiles (session import errors remain in cmd/start.go and nspawn — those are fixed in later tasks)

**Step 6: Commit**

```bash
git add internal/provision/intuneme-profile.sh internal/provision/provision.go
git commit -m "feat: add profile.d login script replacing start-intune.sh"
```

---

### Task 4: Rewrite internal/nspawn

**Files:**
- Modify: `internal/nspawn/nspawn.go`
- Modify: `internal/nspawn/nspawn_test.go`

**Step 1: Write the new test file**

Replace `internal/nspawn/nspawn_test.go` entirely:

```go
package nspawn

import (
	"strings"
	"testing"
)

func TestBuildBootArgs(t *testing.T) {
	sockets := []BindMount{
		{"/run/user/1000/wayland-0", "/run/host-wayland"},
	}
	args := BuildBootArgs(
		"/home/testuser/.local/share/intuneme/rootfs",
		"intuneme",
		"/home/testuser/Intune",
		"/home/testuser",
		sockets,
	)

	joined := strings.Join(args, " ")

	must := []string{
		"--machine=intuneme",
		"--bind=/home/testuser/Intune:/home/testuser",
		"--bind=/tmp/.X11-unix",
		"--bind=/dev/dri",
		"--bind=/run/user/1000/wayland-0:/run/host-wayland",
		"--console=pipe",
		"-b",
	}
	for _, s := range must {
		if !strings.Contains(joined, s) {
			t.Errorf("missing %q in: %s", s, joined)
		}
	}

	// Must NOT contain old-style XDG_RUNTIME_DIR bind
	if strings.Contains(joined, "run-user-external") {
		t.Errorf("should not contain run-user-external bind: %s", joined)
	}
}

func TestBuildBootArgsNoOptionalSockets(t *testing.T) {
	args := BuildBootArgs(
		"/tmp/rootfs",
		"intuneme",
		"/home/testuser/Intune",
		"/home/testuser",
		nil,
	)

	joined := strings.Join(args, " ")

	// Should have the required binds but no optional ones
	if !strings.Contains(joined, "--bind=/tmp/.X11-unix") {
		t.Errorf("missing X11 bind in: %s", joined)
	}
	if strings.Contains(joined, "host-wayland") {
		t.Errorf("should not contain wayland bind when no sockets: %s", joined)
	}
}

func TestBuildShellArgs(t *testing.T) {
	args := BuildShellArgs("intuneme", "testuser")

	joined := strings.Join(args, " ")
	if !strings.Contains(joined, "shell") {
		t.Errorf("missing shell subcommand in: %s", joined)
	}
	if !strings.Contains(joined, "testuser@intuneme") {
		t.Errorf("missing user@machine in: %s", joined)
	}
}

func TestDetectHostSockets(t *testing.T) {
	// DetectHostSockets checks for real files, so in test we just
	// verify it returns an empty list when nothing exists
	sockets := DetectHostSockets(99999) // nonexistent UID
	if len(sockets) != 0 {
		t.Errorf("expected no sockets for nonexistent UID, got %d", len(sockets))
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/nspawn/ -v`
Expected: FAIL — functions don't match new signatures yet

**Step 3: Rewrite nspawn.go**

Replace `internal/nspawn/nspawn.go` entirely:

```go
package nspawn

import (
	"fmt"
	"os"

	"github.com/frostyard/intune/internal/runner"
)

// BindMount represents a host:container bind mount pair.
type BindMount struct {
	Host      string
	Container string
}

// DetectHostSockets checks which optional host sockets exist and returns
// bind mount pairs for them. These are individual sockets, not the whole
// XDG_RUNTIME_DIR.
func DetectHostSockets(uid int) []BindMount {
	runtimeDir := fmt.Sprintf("/run/user/%d", uid)
	checks := []struct {
		hostPath      string
		containerPath string
	}{
		{runtimeDir + "/wayland-0", "/run/host-wayland"},
		{runtimeDir + "/pipewire-0", "/run/host-pipewire"},
	}

	var mounts []BindMount
	for _, c := range checks {
		if _, err := os.Stat(c.hostPath); err == nil {
			mounts = append(mounts, BindMount{c.hostPath, c.containerPath})
		}
	}
	return mounts
}

// BuildBootArgs returns the systemd-nspawn arguments to boot the container.
func BuildBootArgs(rootfs, machine, intuneHome, containerHome string, sockets []BindMount) []string {
	args := []string{
		"-D", rootfs,
		fmt.Sprintf("--machine=%s", machine),
		fmt.Sprintf("--bind=%s:%s", intuneHome, containerHome),
		"--bind=/tmp/.X11-unix",
		"--bind=/dev/dri",
	}
	for _, s := range sockets {
		args = append(args, fmt.Sprintf("--bind=%s:%s", s.Host, s.Container))
	}
	args = append(args, "--console=pipe", "-b")
	return args
}

// BuildShellArgs returns the machinectl shell arguments for an interactive session.
func BuildShellArgs(machine, user string) []string {
	return []string{"shell", fmt.Sprintf("%s@%s", user, machine)}
}

// Boot starts the nspawn container in the background using sudo.
func Boot(r runner.Runner, rootfs, machine, intuneHome, containerHome string, sockets []BindMount) error {
	args := append([]string{"systemd-nspawn"}, BuildBootArgs(rootfs, machine, intuneHome, containerHome, sockets)...)
	return r.RunBackground("sudo", args...)
}

// ValidateSudo prompts for the sudo password if needed.
func ValidateSudo(r runner.Runner) error {
	return r.RunAttached("sudo", "-v")
}

// IsRunning checks if the machine is registered with machinectl.
func IsRunning(r runner.Runner, machine string) bool {
	_, err := r.Run("machinectl", "show", machine)
	return err == nil
}

// Shell opens an interactive session in the container via machinectl shell.
func Shell(r runner.Runner, machine, user string) error {
	args := BuildShellArgs(machine, user)
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
Expected: PASS

**Step 5: Commit**

```bash
git add internal/nspawn/
git commit -m "refactor: rewrite nspawn package for v2 bind mount scheme"
```

---

### Task 5: Update cmd/start.go

**Files:**
- Modify: `cmd/start.go`

**Step 1: Rewrite start.go**

Replace `cmd/start.go` entirely:

```go
package cmd

import (
	"fmt"
	"os"
	"time"

	"github.com/frostyard/intune/internal/config"
	"github.com/frostyard/intune/internal/nspawn"
	"github.com/frostyard/intune/internal/runner"
	"github.com/spf13/cobra"
)

var startCmd = &cobra.Command{
	Use:   "start",
	Short: "Boot the Intune container",
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

		if _, err := os.Stat(cfg.RootfsPath); err != nil {
			return fmt.Errorf("not initialized — run 'intuneme init' first")
		}

		if nspawn.IsRunning(r, cfg.MachineName) {
			fmt.Printf("Container %s is already running.\n", cfg.MachineName)
			fmt.Println("Use 'intuneme shell' to connect.")
			return nil
		}

		home, _ := os.UserHomeDir()
		intuneHome := home + "/Intune"
		containerHome := fmt.Sprintf("/home/%s", cfg.HostUser)
		sockets := nspawn.DetectHostSockets(cfg.HostUID)

		fmt.Println("Checking sudo credentials...")
		if err := nspawn.ValidateSudo(r); err != nil {
			return fmt.Errorf("sudo authentication failed: %w", err)
		}

		fmt.Println("Booting container...")
		if err := nspawn.Boot(r, cfg.RootfsPath, cfg.MachineName, intuneHome, containerHome, sockets); err != nil {
			return fmt.Errorf("failed to start container: %w", err)
		}

		fmt.Println("Waiting for container to boot...")
		for range 30 {
			if nspawn.IsRunning(r, cfg.MachineName) {
				break
			}
			time.Sleep(1 * time.Second)
		}

		if !nspawn.IsRunning(r, cfg.MachineName) {
			return fmt.Errorf("container failed to start within 30 seconds")
		}

		fmt.Println("Container is running. Use 'intuneme shell' to connect.")
		return nil
	},
}

func init() {
	rootCmd.AddCommand(startCmd)
}
```

**Step 2: Verify it compiles**

Run: `go build ./...`
Expected: compiles

**Step 3: Commit**

```bash
git add cmd/start.go
git commit -m "refactor: simplify start command — drop session discovery and auto-launch"
```

---

### Task 6: Add cmd/shell.go

**Files:**
- Create: `cmd/shell.go`

**Step 1: Create shell.go**

```go
package cmd

import (
	"fmt"
	"os"

	"github.com/frostyard/intune/internal/config"
	"github.com/frostyard/intune/internal/nspawn"
	"github.com/frostyard/intune/internal/runner"
	"github.com/spf13/cobra"
)

var shellCmd = &cobra.Command{
	Use:   "shell",
	Short: "Open a shell in the running container",
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

		if _, err := os.Stat(cfg.RootfsPath); err != nil {
			return fmt.Errorf("not initialized — run 'intuneme init' first")
		}

		if !nspawn.IsRunning(r, cfg.MachineName) {
			return fmt.Errorf("container is not running — run 'intuneme start' first")
		}

		return nspawn.Shell(r, cfg.MachineName, cfg.HostUser)
	},
}

func init() {
	rootCmd.AddCommand(shellCmd)
}
```

**Step 2: Verify it compiles**

Run: `go build ./...`
Expected: compiles

**Step 3: Commit**

```bash
git add cmd/shell.go
git commit -m "feat: add shell command for interactive container access"
```

---

### Task 7: Update cmd/init.go

**Files:**
- Modify: `cmd/init.go`

**Step 1: Add ~/Intune creation to init**

In `cmd/init.go`, add after the prerequisites check (after line 33) and before the "already initialized" check:

```go
// Create ~/Intune directory
home, _ := os.UserHomeDir()
intuneHome := filepath.Join(home, "Intune")
if err := os.MkdirAll(intuneHome, 0755); err != nil {
    return fmt.Errorf("create ~/Intune: %w", err)
}
```

Add `"path/filepath"` to imports.

**Step 2: Verify it compiles**

Run: `go build ./...`
Expected: compiles

**Step 3: Commit**

```bash
git add cmd/init.go
git commit -m "feat: create ~/Intune directory during init"
```

---

### Task 8: Update cmd/destroy.go

**Files:**
- Modify: `cmd/destroy.go`

**Step 1: Update destroy to clean ~/Intune state**

In `cmd/destroy.go`, replace the stale state cleanup block (lines 48-61). The intune state now lives under `~/Intune/` instead of the host `$HOME`:

```go
// Clean intune state from ~/Intune (persists via bind mount)
home, _ := os.UserHomeDir()
intuneHome := filepath.Join(home, "Intune")
staleStateDirs := []string{
    filepath.Join(intuneHome, ".config", "intune"),
    filepath.Join(intuneHome, ".local", "share", "intune"),
    filepath.Join(intuneHome, ".local", "share", "intune-portal"),
    filepath.Join(intuneHome, ".local", "share", "microsoft-identity-broker"),
    filepath.Join(intuneHome, ".local", "share", "keyrings"),
    filepath.Join(intuneHome, ".cache", "intune-portal"),
}
for _, dir := range staleStateDirs {
    if _, err := os.Stat(dir); err == nil {
        fmt.Printf("Cleaning %s...\n", dir)
        os.RemoveAll(dir)
    }
}
```

**Step 2: Verify it compiles**

Run: `go build ./...`
Expected: compiles

**Step 3: Commit**

```bash
git add cmd/destroy.go
git commit -m "refactor: destroy cleans ~/Intune state instead of host home"
```

---

### Task 9: Update tests and verify full build

**Files:**
- Modify: `internal/provision/provision_test.go`
- Modify: `internal/prereq/prereq_test.go` (if it references session)

**Step 1: Update provision_test.go**

The `TestWriteFixups` test needs to be updated — it no longer checks for keyring directory or start-intune.sh, and should check for the profile.d script and broker override instead.

Replace `TestWriteFixups` in `internal/provision/provision_test.go`:

```go
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

	// Check profile.d/intuneme.sh exists
	profilePath := filepath.Join(rootfs, "etc", "profile.d", "intuneme.sh")
	if _, err := os.Stat(profilePath); err != nil {
		t.Errorf("expected profile.d/intuneme.sh to exist: %v", err)
	}

	// Check broker display override exists
	brokerOverride := filepath.Join(rootfs, "usr", "lib", "systemd", "user",
		"microsoft-identity-broker.service.d", "display.conf")
	if _, err := os.Stat(brokerOverride); err != nil {
		t.Errorf("expected broker display override to exist: %v", err)
	}

	// Check keyring dir does NOT exist (no longer pre-created)
	keyringDir := filepath.Join(rootfs, "home", "testuser", ".local", "share", "keyrings")
	if _, err := os.Stat(keyringDir); err == nil {
		t.Errorf("keyring dir should not be pre-created in v2")
	}
}
```

**Step 2: Run all tests**

Run: `go test ./... -v`
Expected: all PASS

**Step 3: Build the binary**

Run: `go build -o intuneme .`
Expected: binary compiles

**Step 4: Commit**

```bash
git add internal/provision/provision_test.go
git commit -m "test: update tests for v2 architecture"
```

---

### Task 10: Integration smoke test

This is a manual test of the full flow.

**Step 1: Destroy any existing container**

```bash
./intuneme destroy
```

**Step 2: Init**

```bash
./intuneme init
```

Expected: pulls OCI image, extracts rootfs, creates ~/Intune, creates user, applies fixups, configures PAM, installs polkit rules, saves config.

**Step 3: Start**

```bash
./intuneme start
```

Expected: boots container, reports "Container is running. Use 'intuneme shell' to connect."

**Step 4: Shell**

```bash
./intuneme shell
```

Expected: drops into an interactive shell inside the container.

Inside the container, verify:
```bash
echo $DISPLAY          # should be :0
echo $XDG_RUNTIME_DIR  # should be /run/user/<uid>
busctl --user list     # should show D-Bus services
ls /run/host-wayland   # should exist if host has Wayland
intune-portal          # should launch the enrollment GUI on host display
```

**Step 5: Stop**

```bash
./intuneme stop
```

**Step 6: Record results in notes.md**

---

### Task 11: Update notes.md with v2 status

**Files:**
- Modify: `notes.md`

Add a new section documenting the v2 rework, what changed, and what the current status is.

**Step 1: Commit**

```bash
git add notes.md
git commit -m "docs: update dev log with v2 rework status"
```
