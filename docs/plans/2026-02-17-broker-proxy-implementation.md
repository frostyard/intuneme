# Broker Proxy Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Add an opt-in D-Bus broker proxy that exposes the container's `com.microsoft.identity.broker1` on the host session bus, enabling host-side Edge/VS Code SSO.

**Architecture:** A new `internal/broker` package implements the D-Bus forwarding logic. A `broker-proxy` subcommand runs it in foreground. The `config` subcommand manages the opt-in flag and D-Bus activation file. `start`/`stop`/`status` commands gain broker-proxy lifecycle management when the flag is enabled.

**Tech Stack:** Go, `github.com/godbus/dbus/v5`, systemd D-Bus activation

---

### Task 1: Add BrokerProxy field to config

**Files:**
- Modify: `internal/config/config.go`
- Modify: `internal/config/config_test.go`

**Step 1: Write the failing test**

Add to `config_test.go`:

```go
func TestLoadBrokerProxy(t *testing.T) {
	tmp := t.TempDir()
	toml := "broker_proxy = true\n"
	if err := os.WriteFile(filepath.Join(tmp, "config.toml"), []byte(toml), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	cfg, err := Load(tmp)
	if err != nil {
		t.Fatalf("Load error: %v", err)
	}
	if !cfg.BrokerProxy {
		t.Error("BrokerProxy should be true")
	}
}

func TestLoadBrokerProxyDefault(t *testing.T) {
	tmp := t.TempDir()
	cfg, err := Load(tmp)
	if err != nil {
		t.Fatalf("Load error: %v", err)
	}
	if cfg.BrokerProxy {
		t.Error("BrokerProxy should default to false")
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test -v -run TestLoadBrokerProxy ./internal/config/`
Expected: FAIL — `cfg.BrokerProxy` field doesn't exist

**Step 3: Write minimal implementation**

Add to the `Config` struct in `internal/config/config.go`:

```go
type Config struct {
	MachineName string `toml:"machine_name"`
	RootfsPath  string `toml:"rootfs_path"`
	Image       string `toml:"image"`
	HostUID     int    `toml:"host_uid"`
	HostUser    string `toml:"host_user"`
	BrokerProxy bool   `toml:"broker_proxy"`
}
```

No default needed — Go zero value for `bool` is `false`.

**Step 4: Run test to verify it passes**

Run: `go test -v -run TestLoadBrokerProxy ./internal/config/`
Expected: PASS

**Step 5: Run full test suite**

Run: `make test`
Expected: All tests pass

**Step 6: Commit**

```bash
git add internal/config/config.go internal/config/config_test.go
git commit -m "feat: add BrokerProxy field to config"
```

---

### Task 2: Add godbus/dbus dependency

**Files:**
- Modify: `go.mod`
- Modify: `go.sum`

**Step 1: Add the dependency**

```bash
go get github.com/godbus/dbus/v5
```

**Step 2: Tidy**

```bash
go mod tidy
```

**Step 3: Verify it builds**

```bash
go build ./...
```

**Step 4: Commit**

```bash
git add go.mod go.sum
git commit -m "deps: add godbus/dbus/v5 for D-Bus broker proxy"
```

---

### Task 3: Implement internal/broker package — core proxy logic

**Files:**
- Create: `internal/broker/proxy.go`
- Create: `internal/broker/proxy_test.go`

**Step 1: Write the failing test**

The proxy needs two D-Bus connections — host bus and container bus. For unit testing, we test the method forwarding logic by defining the interface and verifying the proxy struct wires things correctly. Integration testing with real D-Bus buses is deferred to manual testing.

Create `internal/broker/proxy_test.go`:

```go
package broker

import (
	"testing"
)

func TestBrokerMethods(t *testing.T) {
	// Verify all 8 broker methods are defined
	want := []string{
		"acquireTokenInteractively",
		"acquireTokenSilently",
		"getAccounts",
		"removeAccount",
		"acquirePrtSsoCookie",
		"generateSignedHttpRequest",
		"cancelInteractiveFlow",
		"getLinuxBrokerVersion",
	}
	got := BrokerMethods()
	if len(got) != len(want) {
		t.Fatalf("BrokerMethods() returned %d methods, want %d", len(got), len(want))
	}
	for i, m := range want {
		if got[i] != m {
			t.Errorf("BrokerMethods()[%d] = %q, want %q", i, got[i], m)
		}
	}
}

func TestContainerBusAddress(t *testing.T) {
	got := ContainerBusAddress("/tmp/rootfs", 1000)
	want := "unix:path=/tmp/rootfs/run/user/1000/bus"
	if got != want {
		t.Errorf("ContainerBusAddress() = %q, want %q", got, want)
	}
}

func TestDBusServiceFileContent(t *testing.T) {
	got := DBusServiceFileContent("/usr/local/bin/intuneme")
	if got == "" {
		t.Fatal("DBusServiceFileContent returned empty string")
	}
	// Must contain the well-known name
	if !contains(got, "com.microsoft.identity.broker1") {
		t.Error("missing bus name in service file")
	}
	if !contains(got, "/usr/local/bin/intuneme broker-proxy") {
		t.Error("missing Exec line in service file")
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsStr(s, substr))
}

func containsStr(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
```

**Step 2: Run test to verify it fails**

Run: `go test -v ./internal/broker/`
Expected: FAIL — package doesn't exist

**Step 3: Write minimal implementation**

Create `internal/broker/proxy.go`:

```go
package broker

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"

	"github.com/godbus/dbus/v5"
	"github.com/godbus/dbus/v5/introspect"
)

const (
	BusName       = "com.microsoft.identity.broker1"
	ObjectPath    = "/com/microsoft/identity/broker1"
	InterfaceName = "com.microsoft.identity.Broker1"
)

// BrokerMethods returns the list of methods on the Broker1 interface.
func BrokerMethods() []string {
	return []string{
		"acquireTokenInteractively",
		"acquireTokenSilently",
		"getAccounts",
		"removeAccount",
		"acquirePrtSsoCookie",
		"generateSignedHttpRequest",
		"cancelInteractiveFlow",
		"getLinuxBrokerVersion",
	}
}

// ContainerBusAddress returns the D-Bus address for the container's session bus.
func ContainerBusAddress(rootfsPath string, uid int) string {
	return fmt.Sprintf("unix:path=%s/run/user/%d/bus", rootfsPath, uid)
}

// DBusServiceFileContent returns the content for a D-Bus activation .service file.
func DBusServiceFileContent(execPath string) string {
	return fmt.Sprintf("[D-BUS Service]\nName=%s\nExec=%s broker-proxy\n", BusName, execPath)
}

// DBusServiceFilePath returns the path where the D-Bus activation file should be installed.
func DBusServiceFilePath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".local", "share", "dbus-1", "services", BusName+".service")
}

// Proxy forwards D-Bus method calls from the host session bus to the container's broker.
type Proxy struct {
	hostConn      *dbus.Conn
	containerConn *dbus.Conn
}

// forwarder is exported on the host bus and forwards calls to the container.
type forwarder struct {
	containerConn *dbus.Conn
}

// forward sends a method call to the container's broker and returns the response.
func (f *forwarder) forward(method string, protocolVersion, correlationID, requestJSON string) (string, *dbus.Error) {
	obj := f.containerConn.Object(BusName, dbus.ObjectPath(ObjectPath))
	call := obj.Call(InterfaceName+"."+method, 0, protocolVersion, correlationID, requestJSON)
	if call.Err != nil {
		return "", dbus.MakeFailedError(call.Err)
	}
	var response string
	if err := call.Store(&response); err != nil {
		return "", dbus.MakeFailedError(err)
	}
	return response, nil
}

// Each method on the Broker1 interface has the same signature: (s, s, s) -> (s).
// We export them individually so godbus can route by method name.

func (f *forwarder) AcquireTokenInteractively(protocolVersion, correlationID, requestJSON string) (string, *dbus.Error) {
	return f.forward("acquireTokenInteractively", protocolVersion, correlationID, requestJSON)
}

func (f *forwarder) AcquireTokenSilently(protocolVersion, correlationID, requestJSON string) (string, *dbus.Error) {
	return f.forward("acquireTokenSilently", protocolVersion, correlationID, requestJSON)
}

func (f *forwarder) GetAccounts(protocolVersion, correlationID, requestJSON string) (string, *dbus.Error) {
	return f.forward("getAccounts", protocolVersion, correlationID, requestJSON)
}

func (f *forwarder) RemoveAccount(protocolVersion, correlationID, requestJSON string) (string, *dbus.Error) {
	return f.forward("removeAccount", protocolVersion, correlationID, requestJSON)
}

func (f *forwarder) AcquirePrtSsoCookie(protocolVersion, correlationID, requestJSON string) (string, *dbus.Error) {
	return f.forward("acquirePrtSsoCookie", protocolVersion, correlationID, requestJSON)
}

func (f *forwarder) GenerateSignedHttpRequest(protocolVersion, correlationID, requestJSON string) (string, *dbus.Error) {
	return f.forward("generateSignedHttpRequest", protocolVersion, correlationID, requestJSON)
}

func (f *forwarder) CancelInteractiveFlow(protocolVersion, correlationID, requestJSON string) (string, *dbus.Error) {
	return f.forward("cancelInteractiveFlow", protocolVersion, correlationID, requestJSON)
}

func (f *forwarder) GetLinuxBrokerVersion(protocolVersion, correlationID, requestJSON string) (string, *dbus.Error) {
	return f.forward("getLinuxBrokerVersion", protocolVersion, correlationID, requestJSON)
}

// introspectXML describes the Broker1 interface for D-Bus introspection.
const introspectXML = `<node>
	<interface name="` + InterfaceName + `">
		<method name="acquireTokenInteractively">
			<arg direction="in" type="s" name="protocolVersion"/>
			<arg direction="in" type="s" name="correlationId"/>
			<arg direction="in" type="s" name="requestJson"/>
			<arg direction="out" type="s" name="responseJson"/>
		</method>
		<method name="acquireTokenSilently">
			<arg direction="in" type="s" name="protocolVersion"/>
			<arg direction="in" type="s" name="correlationId"/>
			<arg direction="in" type="s" name="requestJson"/>
			<arg direction="out" type="s" name="responseJson"/>
		</method>
		<method name="getAccounts">
			<arg direction="in" type="s" name="protocolVersion"/>
			<arg direction="in" type="s" name="correlationId"/>
			<arg direction="in" type="s" name="requestJson"/>
			<arg direction="out" type="s" name="responseJson"/>
		</method>
		<method name="removeAccount">
			<arg direction="in" type="s" name="protocolVersion"/>
			<arg direction="in" type="s" name="correlationId"/>
			<arg direction="in" type="s" name="requestJson"/>
			<arg direction="out" type="s" name="responseJson"/>
		</method>
		<method name="acquirePrtSsoCookie">
			<arg direction="in" type="s" name="protocolVersion"/>
			<arg direction="in" type="s" name="correlationId"/>
			<arg direction="in" type="s" name="requestJson"/>
			<arg direction="out" type="s" name="responseJson"/>
		</method>
		<method name="generateSignedHttpRequest">
			<arg direction="in" type="s" name="protocolVersion"/>
			<arg direction="in" type="s" name="correlationId"/>
			<arg direction="in" type="s" name="requestJson"/>
			<arg direction="out" type="s" name="responseJson"/>
		</method>
		<method name="cancelInteractiveFlow">
			<arg direction="in" type="s" name="protocolVersion"/>
			<arg direction="in" type="s" name="correlationId"/>
			<arg direction="in" type="s" name="requestJson"/>
			<arg direction="out" type="s" name="responseJson"/>
		</method>
		<method name="getLinuxBrokerVersion">
			<arg direction="in" type="s" name="protocolVersion"/>
			<arg direction="in" type="s" name="correlationId"/>
			<arg direction="in" type="s" name="requestJson"/>
			<arg direction="out" type="s" name="responseJson"/>
		</method>
	</interface>` + introspect.IntrospectDataString + `
</node>`

// Run starts the proxy and blocks until ctx is cancelled.
func Run(ctx context.Context, rootfsPath string, uid int) error {
	containerAddr := ContainerBusAddress(rootfsPath, uid)

	// Connect to the container's session bus
	containerConn, err := dbus.Dial(containerAddr)
	if err != nil {
		return fmt.Errorf("connect to container bus at %s: %w", containerAddr, err)
	}
	if err := containerConn.Auth(nil); err != nil {
		containerConn.Close()
		return fmt.Errorf("auth on container bus: %w", err)
	}
	if err := containerConn.Hello(); err != nil {
		containerConn.Close()
		return fmt.Errorf("hello on container bus: %w", err)
	}
	defer containerConn.Close()

	// Connect to the host session bus
	hostConn, err := dbus.ConnectSessionBus()
	if err != nil {
		return fmt.Errorf("connect to host session bus: %w", err)
	}
	defer hostConn.Close()

	fwd := &forwarder{containerConn: containerConn}

	// Export the forwarder on the host bus
	if err := hostConn.Export(fwd, dbus.ObjectPath(ObjectPath), InterfaceName); err != nil {
		return fmt.Errorf("export broker interface: %w", err)
	}

	// Export introspection
	if err := hostConn.Export(
		introspect.Introspectable(introspectXML),
		dbus.ObjectPath(ObjectPath),
		"org.freedesktop.DBus.Introspectable",
	); err != nil {
		return fmt.Errorf("export introspection: %w", err)
	}

	// Claim the well-known name
	reply, err := hostConn.RequestName(BusName, dbus.NameFlagDoNotQueue)
	if err != nil {
		return fmt.Errorf("request bus name %s: %w", BusName, err)
	}
	if reply != dbus.RequestNameReplyPrimaryOwner {
		return fmt.Errorf("bus name %s already owned", BusName)
	}

	log.Printf("Broker proxy running: forwarding %s to %s", BusName, containerAddr)

	<-ctx.Done()
	log.Println("Broker proxy shutting down")
	return nil
}
```

**Step 4: Run tests to verify they pass**

Run: `go test -v ./internal/broker/`
Expected: PASS

**Step 5: Run full test suite and lint**

Run: `make test && make fmt && make lint`
Expected: All pass

**Step 6: Commit**

```bash
git add internal/broker/proxy.go internal/broker/proxy_test.go
git commit -m "feat: add internal/broker package with D-Bus forwarding proxy"
```

---

### Task 4: Add broker-proxy subcommand

**Files:**
- Create: `cmd/broker_proxy.go`

**Step 1: Write the subcommand**

Create `cmd/broker_proxy.go`:

```go
package cmd

import (
	"fmt"

	"github.com/frostyard/intuneme/internal/broker"
	"github.com/frostyard/intuneme/internal/config"
	"github.com/spf13/cobra"
)

var brokerProxyCmd = &cobra.Command{
	Use:   "broker-proxy",
	Short: "Run the D-Bus broker proxy (foreground)",
	Long:  "Forwards com.microsoft.identity.broker1 from the container's session bus to the host session bus.",
	RunE: func(cmd *cobra.Command, args []string) error {
		root := rootDir
		if root == "" {
			root = config.DefaultRoot()
		}

		cfg, err := config.Load(root)
		if err != nil {
			return err
		}

		if !cfg.BrokerProxy {
			return fmt.Errorf("broker proxy is not enabled — run 'intuneme config broker-proxy enable' first")
		}

		return broker.Run(cmd.Context(), cfg.RootfsPath, cfg.HostUID)
	},
}

func init() {
	rootCmd.AddCommand(brokerProxyCmd)
}
```

**Step 2: Verify it compiles**

Run: `go build ./...`
Expected: Success

**Step 3: Verify help text appears**

Run: `go run . broker-proxy --help`
Expected: Shows usage for broker-proxy subcommand

**Step 4: Commit**

```bash
git add cmd/broker_proxy.go
git commit -m "feat: add broker-proxy subcommand"
```

---

### Task 5: Add config broker-proxy enable/disable subcommands

**Files:**
- Create: `cmd/config.go`

**Step 1: Write the subcommands**

Create `cmd/config.go`:

```go
package cmd

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/frostyard/intuneme/internal/broker"
	"github.com/frostyard/intuneme/internal/config"
	"github.com/spf13/cobra"
)

var configCmd = &cobra.Command{
	Use:   "config",
	Short: "Manage intuneme configuration",
}

var brokerProxyConfigCmd = &cobra.Command{
	Use:   "broker-proxy",
	Short: "Manage broker proxy configuration",
}

var brokerProxyEnableCmd = &cobra.Command{
	Use:   "enable",
	Short: "Enable the host-side broker proxy",
	RunE: func(cmd *cobra.Command, args []string) error {
		root := rootDir
		if root == "" {
			root = config.DefaultRoot()
		}

		cfg, err := config.Load(root)
		if err != nil {
			return err
		}

		cfg.BrokerProxy = true
		if err := cfg.Save(root); err != nil {
			return fmt.Errorf("save config: %w", err)
		}

		// Install D-Bus activation file
		execPath, err := os.Executable()
		if err != nil {
			return fmt.Errorf("resolve executable path: %w", err)
		}

		svcPath := broker.DBusServiceFilePath()
		if err := os.MkdirAll(filepath.Dir(svcPath), 0755); err != nil {
			return fmt.Errorf("create dbus services dir: %w", err)
		}
		content := broker.DBusServiceFileContent(execPath)
		if err := os.WriteFile(svcPath, []byte(content), 0644); err != nil {
			return fmt.Errorf("write dbus service file: %w", err)
		}

		fmt.Println("Broker proxy enabled.")
		fmt.Printf("D-Bus activation file installed: %s\n", svcPath)
		fmt.Println("The proxy will start automatically on next 'intuneme start',")
		fmt.Println("or when a host app calls the broker.")
		return nil
	},
}

var brokerProxyDisableCmd = &cobra.Command{
	Use:   "disable",
	Short: "Disable the host-side broker proxy",
	RunE: func(cmd *cobra.Command, args []string) error {
		root := rootDir
		if root == "" {
			root = config.DefaultRoot()
		}

		cfg, err := config.Load(root)
		if err != nil {
			return err
		}

		cfg.BrokerProxy = false
		if err := cfg.Save(root); err != nil {
			return fmt.Errorf("save config: %w", err)
		}

		// Remove D-Bus activation file
		svcPath := broker.DBusServiceFilePath()
		if err := os.Remove(svcPath); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("remove dbus service file: %w", err)
		}

		// Kill running proxy if any
		pidPath := filepath.Join(root, "broker-proxy.pid")
		broker.StopByPIDFile(pidPath)

		fmt.Println("Broker proxy disabled.")
		return nil
	},
}

func init() {
	brokerProxyConfigCmd.AddCommand(brokerProxyEnableCmd)
	brokerProxyConfigCmd.AddCommand(brokerProxyDisableCmd)
	configCmd.AddCommand(brokerProxyConfigCmd)
	rootCmd.AddCommand(configCmd)
}
```

This depends on a `StopByPIDFile` helper — add it to `internal/broker/proxy.go`:

```go
// StopByPIDFile reads a PID from a file, sends SIGTERM, and removes the file.
// Errors are logged but not returned — best-effort cleanup.
func StopByPIDFile(pidPath string) {
	data, err := os.ReadFile(pidPath)
	if err != nil {
		return
	}
	pid := 0
	fmt.Sscanf(string(data), "%d", &pid)
	if pid > 0 {
		if proc, err := os.FindProcess(pid); err == nil {
			_ = proc.Signal(os.Interrupt)
		}
	}
	_ = os.Remove(pidPath)
}

// WritePIDFile writes the current process ID to a file.
func WritePIDFile(pidPath string) error {
	return os.WriteFile(pidPath, []byte(fmt.Sprintf("%d", os.Getpid())), 0644)
}
```

**Step 2: Update broker-proxy subcommand to write PID file**

In `cmd/broker_proxy.go`, add PID file write before `broker.Run`:

```go
	RunE: func(cmd *cobra.Command, args []string) error {
		root := rootDir
		if root == "" {
			root = config.DefaultRoot()
		}

		cfg, err := config.Load(root)
		if err != nil {
			return err
		}

		if !cfg.BrokerProxy {
			return fmt.Errorf("broker proxy is not enabled — run 'intuneme config broker-proxy enable' first")
		}

		pidPath := filepath.Join(root, "broker-proxy.pid")
		if err := broker.WritePIDFile(pidPath); err != nil {
			return fmt.Errorf("write pid file: %w", err)
		}
		defer os.Remove(pidPath)

		return broker.Run(cmd.Context(), cfg.RootfsPath, cfg.HostUID)
	},
```

Add `"os"` and `"path/filepath"` to the import block.

**Step 3: Verify it compiles**

Run: `go build ./...`
Expected: Success

**Step 4: Verify help text**

Run: `go run . config broker-proxy --help`
Expected: Shows enable/disable subcommands

**Step 5: Run tests and lint**

Run: `make test && make fmt && make lint`
Expected: All pass

**Step 6: Commit**

```bash
git add cmd/config.go cmd/broker_proxy.go internal/broker/proxy.go
git commit -m "feat: add config broker-proxy enable/disable subcommands"
```

---

### Task 6: Add session bootstrap helpers to internal/broker

**Files:**
- Modify: `internal/broker/proxy.go`
- Create: `internal/broker/session.go`
- Create: `internal/broker/session_test.go`

**Step 1: Write the failing test**

Create `internal/broker/session_test.go`:

```go
package broker

import (
	"testing"
)

func TestEnableLingerArgs(t *testing.T) {
	args := EnableLingerArgs("intuneme", "testuser")
	// Should be: machinectl shell root@intuneme /bin/loginctl enable-linger testuser
	if len(args) < 4 {
		t.Fatalf("EnableLingerArgs returned %d args, want >= 4", len(args))
	}
	joined := ""
	for _, a := range args {
		joined += a + " "
	}
	if !containsStr(joined, "root@intuneme") {
		t.Errorf("missing root@machine in: %s", joined)
	}
	if !containsStr(joined, "enable-linger") {
		t.Errorf("missing enable-linger in: %s", joined)
	}
	if !containsStr(joined, "testuser") {
		t.Errorf("missing username in: %s", joined)
	}
}

func TestUnlockKeyringArgs(t *testing.T) {
	args := UnlockKeyringArgs("intuneme", "testuser", "testpass")
	joined := ""
	for _, a := range args {
		joined += a + " "
	}
	if !containsStr(joined, "testuser@intuneme") {
		t.Errorf("missing user@machine in: %s", joined)
	}
	if !containsStr(joined, "gnome-keyring-daemon") {
		t.Errorf("missing gnome-keyring-daemon in: %s", joined)
	}
}

func TestSessionBusSocketPath(t *testing.T) {
	got := SessionBusSocketPath("/tmp/rootfs", 1000)
	want := "/tmp/rootfs/run/user/1000/bus"
	if got != want {
		t.Errorf("SessionBusSocketPath() = %q, want %q", got, want)
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test -v -run TestEnableLinger ./internal/broker/`
Expected: FAIL — functions don't exist

**Step 3: Write minimal implementation**

Create `internal/broker/session.go`:

```go
package broker

import (
	"fmt"
	"path/filepath"
)

// SessionBusSocketPath returns the filesystem path to the container's session bus socket.
func SessionBusSocketPath(rootfsPath string, uid int) string {
	return filepath.Join(rootfsPath, "run", fmt.Sprintf("user/%d", uid), "bus")
}

// EnableLingerArgs returns machinectl args to enable lingering for a user inside the container.
func EnableLingerArgs(machine, user string) []string {
	return []string{
		"shell", fmt.Sprintf("root@%s", machine),
		"/bin/loginctl", "enable-linger", user,
	}
}

// UnlockKeyringArgs returns machinectl args to create a login session and unlock the keyring.
// The returned command runs in the foreground — caller should run it in the background.
func UnlockKeyringArgs(machine, user, password string) []string {
	// Create a login session, pipe the password to gnome-keyring-daemon --unlock,
	// then sleep to keep the session alive.
	script := fmt.Sprintf(
		`echo '%s' | gnome-keyring-daemon --replace --unlock --components=secrets,pkcs11 && exec sleep infinity`,
		password,
	)
	return []string{
		"shell", fmt.Sprintf("%s@%s", user, machine),
		"/bin/bash", "--login", "-c", script,
	}
}

// ContainerPassword is the hardcoded password set during intuneme init.
const ContainerPassword = "Intuneme2024!"
```

**Step 4: Run tests to verify they pass**

Run: `go test -v ./internal/broker/`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/broker/session.go internal/broker/session_test.go
git commit -m "feat: add session bootstrap helpers for container linger and keyring"
```

---

### Task 7: Integrate broker proxy into start command

**Files:**
- Modify: `cmd/start.go`

**Step 1: Add broker proxy startup after container boot**

Add the following block after the "Container is running" success path (after line 76 in current `cmd/start.go`), but before the final print:

```go
		if cfg.BrokerProxy {
			fmt.Println("Enabling linger for container user...")
			lingerArgs := broker.EnableLingerArgs(cfg.MachineName, cfg.HostUser)
			if _, err := r.Run("machinectl", lingerArgs...); err != nil {
				return fmt.Errorf("enable linger: %w", err)
			}

			fmt.Println("Creating login session and unlocking keyring...")
			keyringArgs := broker.UnlockKeyringArgs(cfg.MachineName, cfg.HostUser, broker.ContainerPassword)
			if err := r.RunBackground("machinectl", keyringArgs...); err != nil {
				return fmt.Errorf("create login session: %w", err)
			}

			fmt.Println("Waiting for container session bus...")
			busSocket := broker.SessionBusSocketPath(cfg.RootfsPath, cfg.HostUID)
			busReady := false
			for range 30 {
				if _, err := os.Stat(busSocket); err == nil {
					busReady = true
					break
				}
				time.Sleep(1 * time.Second)
			}
			if !busReady {
				return fmt.Errorf("container session bus not ready at %s within 30 seconds", busSocket)
			}

			fmt.Println("Starting broker proxy...")
			execPath, err := os.Executable()
			if err != nil {
				return fmt.Errorf("resolve executable: %w", err)
			}
			if err := r.RunBackground(execPath, "broker-proxy", "--root", root); err != nil {
				return fmt.Errorf("start broker proxy: %w", err)
			}

			// Wait briefly for proxy to claim the bus name
			time.Sleep(2 * time.Second)
			fmt.Println("Broker proxy started.")
		}
```

Add imports for `"github.com/frostyard/intuneme/internal/broker"`.

Change the final print to differentiate:

```go
		if cfg.BrokerProxy {
			fmt.Println("Container and broker proxy running.")
			fmt.Println("Host apps can now use SSO via com.microsoft.identity.broker1.")
		} else {
			fmt.Println("Container is running. Use 'intuneme shell' to connect.")
		}
```

**Step 2: Verify it compiles**

Run: `go build ./...`
Expected: Success

**Step 3: Run tests and lint**

Run: `make test && make fmt && make lint`
Expected: All pass

**Step 4: Commit**

```bash
git add cmd/start.go
git commit -m "feat: start broker proxy on container boot when enabled"
```

---

### Task 8: Integrate broker proxy into stop command

**Files:**
- Modify: `cmd/stop.go`

**Step 1: Add proxy shutdown before container stop**

Modify `cmd/stop.go` to kill the proxy before stopping the container:

```go
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

		// Stop broker proxy first (if enabled) so host apps get clean errors
		if cfg.BrokerProxy {
			pidPath := filepath.Join(root, "broker-proxy.pid")
			broker.StopByPIDFile(pidPath)
			fmt.Println("Broker proxy stopped.")
		}

		fmt.Println("Stopping container...")
		if err := nspawn.Stop(r, cfg.MachineName); err != nil {
			return err
		}
		fmt.Println("Container stopped.")
		return nil
	},
}
```

Add imports for `"path/filepath"` and `"github.com/frostyard/intuneme/internal/broker"`.

**Step 2: Verify it compiles**

Run: `go build ./...`
Expected: Success

**Step 3: Run tests and lint**

Run: `make test && make fmt && make lint`
Expected: All pass

**Step 4: Commit**

```bash
git add cmd/stop.go
git commit -m "feat: stop broker proxy before container shutdown"
```

---

### Task 9: Add broker proxy status to status command

**Files:**
- Modify: `cmd/status.go`

**Step 1: Add proxy status line**

After the container running/stopped output, add:

```go
		if cfg.BrokerProxy {
			pidPath := filepath.Join(root, "broker-proxy.pid")
			if pid, running := broker.IsRunningByPIDFile(pidPath); running {
				fmt.Printf("Broker proxy: running (PID %d)\n", pid)
			} else {
				fmt.Println("Broker proxy: not running")
			}
		}
```

Add a helper to `internal/broker/proxy.go`:

```go
// IsRunningByPIDFile checks if the process in the PID file is still running.
// Returns the PID and whether it's alive.
func IsRunningByPIDFile(pidPath string) (int, bool) {
	data, err := os.ReadFile(pidPath)
	if err != nil {
		return 0, false
	}
	pid := 0
	fmt.Sscanf(strings.TrimSpace(string(data)), "%d", &pid)
	if pid <= 0 {
		return 0, false
	}
	proc, err := os.FindProcess(pid)
	if err != nil {
		return pid, false
	}
	// On Unix, FindProcess always succeeds. Send signal 0 to check if alive.
	err = proc.Signal(syscall.Signal(0))
	return pid, err == nil
}
```

Add `"strings"`, `"syscall"` to the import block in `proxy.go`.

Add `"path/filepath"` and `"github.com/frostyard/intuneme/internal/broker"` to `cmd/status.go` imports.

**Step 2: Verify it compiles**

Run: `go build ./...`
Expected: Success

**Step 3: Run tests and lint**

Run: `make test && make fmt && make lint`
Expected: All pass

**Step 4: Commit**

```bash
git add cmd/status.go internal/broker/proxy.go
git commit -m "feat: show broker proxy status in intuneme status"
```

---

### Task 10: Manual integration test

This task is manual verification — no automated tests.

**Step 1: Build**

```bash
make build
```

**Step 2: Enable broker proxy**

```bash
./intuneme config broker-proxy enable
```

Verify:
- Config file at `~/.local/share/intuneme/config.toml` has `broker_proxy = true`
- D-Bus activation file exists at `~/.local/share/dbus-1/services/com.microsoft.identity.broker1.service`

**Step 3: Start container with proxy**

```bash
./intuneme start
```

Verify output includes:
- "Enabling linger for container user..."
- "Creating login session and unlocking keyring..."
- "Waiting for container session bus..."
- "Starting broker proxy..."
- "Broker proxy started."
- "Container and broker proxy running."

**Step 4: Check status**

```bash
./intuneme status
```

Verify output includes:
- "Container: running"
- "Broker proxy: running (PID XXXXX)"

**Step 5: Test D-Bus name is claimed**

```bash
dbus-send --session --print-reply --dest=com.microsoft.identity.broker1 \
  /com/microsoft/identity/broker1 \
  org.freedesktop.DBus.Introspectable.Introspect
```

Verify: Returns XML introspection data showing the Broker1 interface.

**Step 6: Test method forwarding**

```bash
dbus-send --session --print-reply --dest=com.microsoft.identity.broker1 \
  /com/microsoft/identity/broker1 \
  com.microsoft.identity.Broker1.getLinuxBrokerVersion \
  string:"1.0" string:"test-correlation-id" string:"{}"
```

Verify: Returns a string response from the container's broker (or a meaningful error if the broker isn't enrolled yet).

**Step 7: Stop and verify cleanup**

```bash
./intuneme stop
./intuneme status
```

Verify:
- "Broker proxy stopped."
- "Container: stopped"
- "Broker proxy: not running"
- No stale PID file at `~/.local/share/intuneme/broker-proxy.pid`

**Step 8: Disable and verify**

```bash
./intuneme config broker-proxy disable
```

Verify:
- Config file has `broker_proxy = false`
- D-Bus activation file removed
- `./intuneme start` boots without any proxy output

**Step 9: Verify default behavior unchanged**

With `broker_proxy = false`:
- `intuneme start` should behave exactly as before (no linger, no session bootstrap, no proxy)
- `intuneme stop` should behave exactly as before
- `intuneme status` should not show broker proxy line
