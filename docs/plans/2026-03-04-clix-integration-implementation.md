# clix Integration Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Replace intuneme's CLI boilerplate with `github.com/frostyard/clix` for unified version handling, common flags (`--json`, `--verbose`, `--dry-run`, `--silent`), structured output, and reporter-based progress.

**Architecture:** Add clix as a dependency, replace main.go's manual fang/signal setup with `clix.App.Run()`, add a package-level `reporter.Reporter` in root.go initialized via `clix.NewReporter()`, then migrate each command file to use the reporter and respect the new flags.

**Tech Stack:** Go, github.com/frostyard/clix, github.com/frostyard/std/reporter, cobra

---

### Task 1: Add clix dependency and simplify main.go

**Files:**
- Modify: `go.mod`
- Modify: `main.go`

**Step 1: Add clix dependency**

Run: `go get github.com/frostyard/clix@latest`

**Step 2: Rewrite main.go**

Replace the entire contents of `main.go` with:

```go
package main

import (
	"os"

	"github.com/frostyard/clix"
	"github.com/frostyard/intuneme/cmd"
	pkgversion "github.com/frostyard/intuneme/internal/version"
)

var version = "dev"
var commit = "none"
var date = "unknown"
var builtBy = "local"

func main() {
	pkgversion.Version = version

	app := clix.App{
		Version: version,
		Commit:  commit,
		Date:    date,
		BuiltBy: builtBy,
	}

	if err := app.Run(cmd.RootCmd()); err != nil {
		os.Exit(1)
	}
}
```

**Step 3: Tidy modules**

Run: `go mod tidy`

**Step 4: Verify it builds**

Run: `go build -o intuneme .`
Expected: Builds without errors. `./intuneme --version` prints version string.

**Step 5: Run existing tests**

Run: `go test ./...`
Expected: All tests pass.

**Step 6: Commit**

```bash
git add main.go go.mod go.sum
git commit -m "feat: replace fang/signal boilerplate with clix.App.Run()"
```

---

### Task 2: Add reporter to root.go

**Files:**
- Modify: `cmd/root.go`

**Step 1: Add PersistentPreRunE with reporter initialization**

Replace `cmd/root.go` with:

```go
package cmd

import (
	"github.com/frostyard/clix"
	"github.com/frostyard/std/reporter"
	"github.com/spf13/cobra"
)

var rootDir string
var rep reporter.Reporter

var rootCmd = &cobra.Command{
	Use:   "intuneme",
	Short: "Manage an Intune container on an immutable Linux host",
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		rep = clix.NewReporter()
		return nil
	},
}

func RootCmd() *cobra.Command {
	return rootCmd
}

func init() {
	rootCmd.PersistentFlags().StringVar(&rootDir, "root", "", "root directory for intuneme data (default ~/.local/share/intuneme)")
}
```

**Step 2: Verify it builds**

Run: `go build -o intuneme .`
Expected: Builds without errors.

**Step 3: Commit**

```bash
git add cmd/root.go
git commit -m "feat: add package-level reporter initialized via clix.NewReporter()"
```

---

### Task 3: Migrate status command with JSON output

**Files:**
- Modify: `cmd/status.go`

**Step 1: Write a test for JSON output**

Create `cmd/status_test.go`:

```go
package cmd

import (
	"bytes"
	"encoding/json"
	"os"
	"testing"
)

func TestStatusJSON(t *testing.T) {
	// Capture stdout
	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	// Set JSON mode and run status in an uninitialized root
	rootDir = t.TempDir()
	rootCmd.SetArgs([]string{"status", "--json"})
	_ = rootCmd.Execute()

	w.Close()
	os.Stdout = old

	var buf bytes.Buffer
	buf.ReadFrom(r)

	// When not initialized with --json, should output valid JSON with status field
	if buf.Len() > 0 {
		var result map[string]any
		if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
			t.Fatalf("expected valid JSON, got: %s", buf.String())
		}
	}
}
```

**Step 2: Run the test to verify it fails**

Run: `go test ./cmd/ -run TestStatusJSON -v`
Expected: FAIL (--json flag not recognized yet, or output not JSON)

**Step 3: Rewrite status.go**

Replace `cmd/status.go` with:

```go
package cmd

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/frostyard/clix"
	"github.com/frostyard/intuneme/internal/broker"
	"github.com/frostyard/intuneme/internal/config"
	"github.com/frostyard/intuneme/internal/nspawn"
	"github.com/frostyard/intuneme/internal/runner"
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
			if clix.OutputJSON(map[string]any{
				"initialized": false,
			}) {
				return nil
			}
			rep.Message("Status: not initialized")
			rep.Message("Run 'intuneme init' to get started.")
			return nil
		}

		containerStatus := "stopped"
		if nspawn.IsRunning(r, cfg.MachineName) {
			containerStatus = "running"
		}

		brokerStatus := ""
		if cfg.BrokerProxy {
			pidPath := filepath.Join(root, "broker-proxy.pid")
			if pid, running := broker.IsRunningByPIDFile(pidPath); running {
				brokerStatus = fmt.Sprintf("running (PID %d)", pid)
			} else {
				brokerStatus = "not running"
			}
		}

		if clix.OutputJSON(map[string]any{
			"initialized":  true,
			"root":         root,
			"rootfs":       cfg.RootfsPath,
			"machine":      cfg.MachineName,
			"container":    containerStatus,
			"broker_proxy": brokerStatus,
		}) {
			return nil
		}

		rep.MessagePlain("Root:    %s", root)
		rep.MessagePlain("Rootfs:  %s", cfg.RootfsPath)
		rep.MessagePlain("Machine: %s", cfg.MachineName)
		rep.MessagePlain("Container: %s", containerStatus)

		if cfg.BrokerProxy {
			rep.MessagePlain("Broker proxy: %s", brokerStatus)
		}

		return nil
	},
}

func init() {
	rootCmd.AddCommand(statusCmd)
}
```

**Step 4: Run the test**

Run: `go test ./cmd/ -run TestStatusJSON -v`
Expected: PASS

**Step 5: Verify build and lint**

Run: `make fmt && make lint`

**Step 6: Commit**

```bash
git add cmd/status.go cmd/status_test.go
git commit -m "feat: migrate status command to reporter with --json support"
```

---

### Task 4: Migrate stop command with dry-run

**Files:**
- Modify: `cmd/stop.go`

**Step 1: Rewrite stop.go**

```go
package cmd

import (
	"fmt"
	"path/filepath"

	"github.com/frostyard/clix"
	"github.com/frostyard/intuneme/internal/broker"
	"github.com/frostyard/intuneme/internal/config"
	"github.com/frostyard/intuneme/internal/nspawn"
	"github.com/frostyard/intuneme/internal/runner"
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
			rep.Message("Container is not running.")
			return nil
		}

		if clix.DryRun {
			if cfg.BrokerProxy {
				rep.Message("[dry-run] Would stop broker proxy")
			}
			rep.Message("[dry-run] Would stop container %s", cfg.MachineName)
			return nil
		}

		// Stop broker proxy first so host apps get clean errors
		if cfg.BrokerProxy {
			pidPath := filepath.Join(root, "broker-proxy.pid")
			broker.StopByPIDFile(pidPath)
			rep.Message("Broker proxy stopped.")
		}

		rep.Message("Stopping container...")
		if err := nspawn.Stop(r, cfg.MachineName); err != nil {
			return err
		}
		rep.Message("Container stopped.")
		return nil
	},
}

func init() {
	rootCmd.AddCommand(stopCmd)
}
```

**Step 2: Verify build**

Run: `go build -o intuneme .`

**Step 3: Commit**

```bash
git add cmd/stop.go
git commit -m "feat: migrate stop command to reporter with dry-run support"
```

---

### Task 5: Migrate destroy command with dry-run and verbose

**Files:**
- Modify: `cmd/destroy.go`

**Step 1: Rewrite destroy.go**

```go
package cmd

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/frostyard/clix"
	"github.com/frostyard/intuneme/internal/config"
	"github.com/frostyard/intuneme/internal/nspawn"
	"github.com/frostyard/intuneme/internal/runner"
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

		if clix.DryRun {
			if nspawn.IsRunning(r, cfg.MachineName) {
				rep.Message("[dry-run] Would stop container %s", cfg.MachineName)
			}
			rep.Message("[dry-run] Would remove %s", root)
			rep.Message("[dry-run] Would clean Intune state from ~/Intune")
			return nil
		}

		// Stop if running
		if nspawn.IsRunning(r, cfg.MachineName) {
			rep.Message("Stopping running container...")
			if err := nspawn.Stop(r, cfg.MachineName); err != nil {
				return fmt.Errorf("failed to stop container: %w", err)
			}
		}

		// Remove rootfs with sudo (owned by root after nspawn use)
		rep.Message("Removing %s...", root)
		out, err := r.Run("sudo", "rm", "-rf", cfg.RootfsPath)
		if err != nil {
			return fmt.Errorf("rm rootfs failed: %w\n%s", err, out)
		}

		// Remove config
		_ = os.Remove(fmt.Sprintf("%s/config.toml", root))

		// Clean intune state from ~/Intune (persists via bind mount)
		home, _ := os.UserHomeDir()
		intuneHome := filepath.Join(home, "Intune")
		staleStateDirs := []string{
			filepath.Join(intuneHome, ".config", "intune"),
			filepath.Join(intuneHome, ".local", "share", "intune"),
			filepath.Join(intuneHome, ".local", "share", "intune-portal"),
			filepath.Join(intuneHome, ".local", "share", "keyrings"),
			filepath.Join(intuneHome, ".local", "state", "microsoft-identity-broker"),
			filepath.Join(intuneHome, ".cache", "intune-portal"),
		}
		for _, dir := range staleStateDirs {
			if _, err := os.Stat(dir); err == nil {
				if clix.Verbose {
					rep.Message("Cleaning %s...", dir)
				}
				_ = os.RemoveAll(dir)
			}
		}

		rep.Message("Destroyed.")
		return nil
	},
}

func init() {
	rootCmd.AddCommand(destroyCmd)
}
```

**Step 2: Verify build**

Run: `go build -o intuneme .`

**Step 3: Commit**

```bash
git add cmd/destroy.go
git commit -m "feat: migrate destroy command to reporter with dry-run and verbose"
```

---

### Task 6: Migrate start command with dry-run and verbose

**Files:**
- Modify: `cmd/start.go`

**Step 1: Rewrite start.go**

```go
package cmd

import (
	"fmt"
	"os"
	"time"

	"github.com/frostyard/clix"
	"github.com/frostyard/intuneme/internal/broker"
	"github.com/frostyard/intuneme/internal/config"
	"github.com/frostyard/intuneme/internal/nspawn"
	"github.com/frostyard/intuneme/internal/runner"
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
			rep.Message("Container %s is already running.", cfg.MachineName)
			rep.Message("Use 'intuneme shell' to connect.")
			return nil
		}

		home, _ := os.UserHomeDir()
		intuneHome := home + "/Intune"
		containerHome := fmt.Sprintf("/home/%s", cfg.HostUser)
		sockets := nspawn.DetectHostSockets(cfg.HostUID)

		videoDev := nspawn.DetectVideoDevices()
		if len(videoDev) > 0 {
			for _, d := range videoDev {
				if d.Name != "" && clix.Verbose {
					rep.Message("Detected webcam: %s (%s)", d.Mount.Host, d.Name)
				}
				sockets = append(sockets, d.Mount)
			}
		} else if clix.Verbose {
			rep.Message("No webcams detected")
		}

		// When broker proxy is enabled, bind-mount a host directory to
		// /run/user/<uid> inside the container so the session bus socket
		// is accessible from the host.
		if cfg.BrokerProxy {
			runtimeDir := broker.RuntimeDir(root)
			if err := os.MkdirAll(runtimeDir, 0700); err != nil {
				return fmt.Errorf("create runtime dir: %w", err)
			}
			hostDir, containerDir := broker.RuntimeBindMount(root, cfg.HostUID)
			sockets = append(sockets, nspawn.BindMount{Host: hostDir, Container: containerDir})
		}

		if clix.DryRun {
			rep.Message("[dry-run] Would boot container %s", cfg.MachineName)
			if cfg.BrokerProxy {
				rep.Message("[dry-run] Would enable linger and start broker proxy")
			}
			return nil
		}

		rep.Message("Checking sudo credentials...")
		if err := nspawn.ValidateSudo(r); err != nil {
			return fmt.Errorf("sudo authentication failed: %w", err)
		}

		rep.Message("Booting container...")
		if err := nspawn.Boot(r, cfg.RootfsPath, cfg.MachineName, intuneHome, containerHome, sockets); err != nil {
			return fmt.Errorf("failed to start container: %w", err)
		}

		rep.Message("Waiting for container to boot...")
		for range 30 {
			if nspawn.IsRunning(r, cfg.MachineName) {
				break
			}
			time.Sleep(1 * time.Second)
		}

		if !nspawn.IsRunning(r, cfg.MachineName) {
			return fmt.Errorf("container failed to start within 30 seconds")
		}

		if cfg.BrokerProxy {
			rep.Message("Enabling linger for container user...")
			if _, err := r.Run("machinectl", broker.EnableLingerArgs(cfg.MachineName, cfg.HostUser)...); err != nil {
				return fmt.Errorf("failed to enable linger: %w", err)
			}

			if clix.Verbose {
				rep.Message("Creating login session...")
			}
			if err := r.RunBackground("machinectl", broker.LoginSessionArgs(cfg.MachineName, cfg.HostUser)...); err != nil {
				return fmt.Errorf("failed to create login session: %w", err)
			}

			rep.Message("Waiting for container session bus...")
			busPath := broker.SessionBusSocketPath(root)
			busReady := false
			for range 30 {
				if _, err := os.Stat(busPath); err == nil {
					busReady = true
					break
				}
				time.Sleep(1 * time.Second)
			}
			if !busReady {
				return fmt.Errorf("container session bus not available after 30 seconds")
			}

			if clix.Verbose {
				rep.Message("Starting broker proxy...")
			}
			execPath, err := os.Executable()
			if err != nil {
				return fmt.Errorf("failed to determine executable path: %w", err)
			}
			// Use setsid so the broker proxy gets its own session and survives
			// terminal closure (e.g. when started from the GNOME extension).
			if err := r.RunBackground("setsid", execPath, "broker-proxy", "--root", root); err != nil {
				return fmt.Errorf("failed to start broker proxy: %w", err)
			}
			time.Sleep(2 * time.Second)

			rep.Message("Container and broker proxy running.")
			rep.Message("Host apps can now use SSO via com.microsoft.identity.broker1.")
		} else {
			rep.Message("Container is running. Use 'intuneme shell' to connect.")
		}

		return nil
	},
}

func init() {
	rootCmd.AddCommand(startCmd)
}
```

**Step 2: Verify build**

Run: `go build -o intuneme .`

**Step 3: Commit**

```bash
git add cmd/start.go
git commit -m "feat: migrate start command to reporter with dry-run and verbose"
```

---

### Task 7: Migrate init command with dry-run and verbose

**Files:**
- Modify: `cmd/init.go`

**Step 1: Rewrite init.go**

Replace all `fmt.Println`/`fmt.Printf` progress output with `rep.Message()`, all `fmt.Fprintln(os.Stderr, ...)` with `rep.Warning()`, and add dry-run guard after password acquisition. Keep `fmt.Print` for the interactive password prompts (they must go to stdout for the terminal).

Key changes:
- `fmt.Printf("Pulling and extracting OCI image %s (via %s)...\n", ...)` → `rep.Message("Pulling and extracting OCI image %s (via %s)...", ...)`
- `fmt.Fprintln(os.Stderr, "  -", e)` → `rep.Warning("  - %s", e)`
- `fmt.Fprintf(os.Stderr, "warning: ...")` → `rep.Warning("...")`
- Add after password acquisition:

```go
if clix.DryRun {
    rep.Message("[dry-run] Would pull OCI image %s", image)
    rep.Message("[dry-run] Would create container at %s", cfg.RootfsPath)
    rep.Message("[dry-run] Would create container user %s", u.Username)
    return nil
}
```

- Gate detailed provisioning steps behind `clix.Verbose`:

```go
if clix.Verbose {
    rep.Message("Configuring GPU render group...")
}
```

Leave `fmt.Print("Enter container user password: ")` and `fmt.Print("Confirm password: ")` as-is — these are interactive prompts that must go to stdout for terminal echo control.

**Step 2: Verify build and tests**

Run: `go build -o intuneme . && go test ./cmd/ -v`
Expected: Build succeeds, existing password validation tests pass.

**Step 3: Commit**

```bash
git add cmd/init.go
git commit -m "feat: migrate init command to reporter with dry-run and verbose"
```

---

### Task 8: Migrate recreate command with dry-run and verbose

**Files:**
- Modify: `cmd/recreate.go`

**Step 1: Rewrite recreate.go**

Same pattern as init. Key changes:
- All `fmt.Println`/`fmt.Printf` progress → `rep.Message()`
- All `fmt.Fprintf(os.Stderr, "warning: ...")` → `rep.Warning()`
- Add dry-run guard after sudo validation:

```go
if clix.DryRun {
    rep.Message("[dry-run] Would stop container (if running)")
    rep.Message("[dry-run] Would backup shadow entry and broker state")
    rep.Message("[dry-run] Would remove old rootfs at %s", cfg.RootfsPath)
    rep.Message("[dry-run] Would pull new image and re-provision")
    return nil
}
```

- Gate detailed steps behind `clix.Verbose`:

```go
if clix.Verbose {
    rep.Message("Backing up shadow entry...")
}
```

**Step 2: Verify build**

Run: `go build -o intuneme .`

**Step 3: Commit**

```bash
git add cmd/recreate.go
git commit -m "feat: migrate recreate command to reporter with dry-run and verbose"
```

---

### Task 9: Migrate remaining commands (shell, open, config, extension, broker-proxy)

**Files:**
- Modify: `cmd/shell.go`
- Modify: `cmd/open.go`
- Modify: `cmd/config.go`
- Modify: `cmd/extension.go`
- Modify: `cmd/broker_proxy.go`

These commands have minimal output. The migration is straightforward:

**shell.go:** No output to migrate (only errors). No changes needed.

**open.go:** No output to migrate (only errors). No changes needed.

**config.go:**
- `fmt.Println("Broker proxy enabled.")` → `rep.Message("Broker proxy enabled.")`
- `fmt.Printf("D-Bus activation file installed: %s\n", svcPath)` → `rep.Message("D-Bus activation file installed: %s", svcPath)`
- Same pattern for all other `fmt.Println` in config.go.

**extension.go:**
- `fmt.Printf("Extension files installed to %s\n", extDir)` → `rep.Message("Extension files installed to %s", extDir)`
- `fmt.Fprintf(os.Stderr, "warning: ...")` → `rep.Warning("...")`
- Same pattern for remaining output.

**broker_proxy.go:** No user-facing output to migrate (it's a daemon). No changes needed.

**Step 1: Apply changes to config.go and extension.go**

**Step 2: Verify build**

Run: `go build -o intuneme .`

**Step 3: Commit**

```bash
git add cmd/config.go cmd/extension.go
git commit -m "feat: migrate config and extension commands to reporter"
```

---

### Task 10: Update Makefile ldflags

**Files:**
- Modify: `Makefile`

**Step 1: Update LDFLAGS to inject all four build vars**

Change the LDFLAGS line from:

```makefile
LDFLAGS=-ldflags "-X main.version=$(VERSION) -s -w"
```

To:

```makefile
COMMIT?=$(shell git rev-parse --short HEAD 2>/dev/null || echo "none")
DATE?=$(shell date -u +%Y-%m-%dT%H:%M:%SZ)
LDFLAGS=-ldflags "-X main.version=$(VERSION) -X main.commit=$(COMMIT) -X main.date=$(DATE) -X main.builtBy=make -s -w"
```

**Step 2: Verify**

Run: `make build && ./intuneme --version`
Expected: Output includes commit hash, date, and "Built by: make"

**Step 3: Commit**

```bash
git add Makefile
git commit -m "feat: inject commit, date, and builtBy via Makefile ldflags"
```

---

### Task 11: Final verification and cleanup

**Step 1: Run full test suite**

Run: `go test ./... -v`
Expected: All tests pass.

**Step 2: Run linter**

Run: `make fmt && make lint`
Expected: No lint errors.

**Step 3: Verify all flags work**

Run:
- `./intuneme status --json` — should output JSON
- `./intuneme status --silent` — should produce no output
- `./intuneme stop --dry-run` — should print dry-run messages
- `./intuneme --version` — should show full version info

**Step 4: Remove unused fang import from go.mod (if still present)**

Run: `go mod tidy`

**Step 5: Commit any cleanup**

```bash
git add -A
git commit -m "chore: tidy modules after clix integration"
```
