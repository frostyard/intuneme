package broker

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/godbus/dbus/v5"
	"github.com/godbus/dbus/v5/introspect"
)

const (
	BusName       = "com.microsoft.identity.broker1"
	ObjectPath    = "/com/microsoft/identity/broker1"
	InterfaceName = "com.microsoft.identity.Broker1"
)

// introspectXML describes the Broker1 interface for D-Bus introspection.
const introspectXML = `<node>
  <interface name="` + InterfaceName + `">
    <method name="acquireTokenInteractively">
      <arg name="protocolVersion" type="s" direction="in"/>
      <arg name="correlationID" type="s" direction="in"/>
      <arg name="requestJSON" type="s" direction="in"/>
      <arg name="responseJSON" type="s" direction="out"/>
    </method>
    <method name="acquireTokenSilently">
      <arg name="protocolVersion" type="s" direction="in"/>
      <arg name="correlationID" type="s" direction="in"/>
      <arg name="requestJSON" type="s" direction="in"/>
      <arg name="responseJSON" type="s" direction="out"/>
    </method>
    <method name="getAccounts">
      <arg name="protocolVersion" type="s" direction="in"/>
      <arg name="correlationID" type="s" direction="in"/>
      <arg name="requestJSON" type="s" direction="in"/>
      <arg name="responseJSON" type="s" direction="out"/>
    </method>
    <method name="removeAccount">
      <arg name="protocolVersion" type="s" direction="in"/>
      <arg name="correlationID" type="s" direction="in"/>
      <arg name="requestJSON" type="s" direction="in"/>
      <arg name="responseJSON" type="s" direction="out"/>
    </method>
    <method name="acquirePrtSsoCookie">
      <arg name="protocolVersion" type="s" direction="in"/>
      <arg name="correlationID" type="s" direction="in"/>
      <arg name="requestJSON" type="s" direction="in"/>
      <arg name="responseJSON" type="s" direction="out"/>
    </method>
    <method name="generateSignedHttpRequest">
      <arg name="protocolVersion" type="s" direction="in"/>
      <arg name="correlationID" type="s" direction="in"/>
      <arg name="requestJSON" type="s" direction="in"/>
      <arg name="responseJSON" type="s" direction="out"/>
    </method>
    <method name="cancelInteractiveFlow">
      <arg name="protocolVersion" type="s" direction="in"/>
      <arg name="correlationID" type="s" direction="in"/>
      <arg name="requestJSON" type="s" direction="in"/>
      <arg name="responseJSON" type="s" direction="out"/>
    </method>
    <method name="getLinuxBrokerVersion">
      <arg name="protocolVersion" type="s" direction="in"/>
      <arg name="correlationID" type="s" direction="in"/>
      <arg name="requestJSON" type="s" direction="in"/>
      <arg name="responseJSON" type="s" direction="out"/>
    </method>
  </interface>
  ` + introspect.IntrospectDataString + `
</node>`

// BrokerMethods returns the 8 method names of the Broker1 D-Bus interface.
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

// ContainerBusAddress returns the D-Bus address for the container's session bus,
// accessible from the host via the bind-mounted runtime directory.
func ContainerBusAddress(root string) string {
	return "unix:path=" + SessionBusSocketPath(root)
}

// DBusServiceFileContent returns the content of a D-Bus service activation file
// that launches the broker-proxy subcommand.
func DBusServiceFileContent(execPath string) string {
	return fmt.Sprintf("[D-BUS Service]\nName=%s\nExec=%s broker-proxy\n", BusName, execPath)
}

// DBusServiceFilePath returns the path where the D-Bus service activation file
// should be installed for the current user.
func DBusServiceFilePath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		home = os.Getenv("HOME")
	}
	return filepath.Join(home, ".local", "share", "dbus-1", "services", BusName+".service")
}

// WritePIDFile writes the current process PID to the given path.
func WritePIDFile(pidPath string) error {
	return os.WriteFile(pidPath, []byte(strconv.Itoa(os.Getpid())), 0o644)
}

// IsRunningByPIDFile reads a PID from the file and checks if the process is alive
// via signal 0. Returns (0, false) if the file cannot be read or parsed.
func IsRunningByPIDFile(pidPath string) (int, bool) {
	data, err := os.ReadFile(pidPath)
	if err != nil {
		return 0, false
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		return 0, false
	}
	proc, err := os.FindProcess(pid)
	if err != nil {
		return pid, false
	}
	err = proc.Signal(syscall.Signal(0))
	return pid, err == nil
}

// StopByPIDFile reads a PID from the file, sends SIGINT, waits briefly for exit,
// and removes the file. Best-effort: errors are silently ignored.
func StopByPIDFile(pidPath string) {
	data, err := os.ReadFile(pidPath)
	if err != nil {
		return
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		return
	}
	proc, err := os.FindProcess(pid)
	if err == nil {
		_ = proc.Signal(syscall.SIGINT)
		// Wait up to 5 seconds for the process to exit.
		for range 50 {
			if err := proc.Signal(syscall.Signal(0)); err != nil {
				break
			}
			time.Sleep(100 * time.Millisecond)
		}
	}
	_ = os.Remove(pidPath)
}

// forwarder proxies D-Bus method calls from the host session bus to the
// container's microsoft-identity-broker instance.
type forwarder struct {
	containerConn *dbus.Conn
}

func (f *forwarder) forward(method, protocolVersion, correlationID, requestJSON string) (string, *dbus.Error) {
	call := f.containerConn.Object(BusName, dbus.ObjectPath(ObjectPath)).Call(
		InterfaceName+"."+method, 0,
		protocolVersion, correlationID, requestJSON,
	)
	if call.Err != nil {
		return "", dbus.MakeFailedError(call.Err)
	}
	var response string
	if err := call.Store(&response); err != nil {
		return "", dbus.MakeFailedError(err)
	}
	return response, nil
}

// The following 8 exported methods match the Broker1 D-Bus interface signature.
// godbus dispatches incoming calls to these methods by name.

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

// Run connects to the container's D-Bus and the host session bus, exports the
// forwarder, claims the broker bus name, and blocks until ctx is cancelled.
func Run(ctx context.Context, root string) error {
	addr := ContainerBusAddress(root)
	containerConn, err := dbus.Dial(addr)
	if err != nil {
		return fmt.Errorf("dial container bus at %s: %w", addr, err)
	}
	defer func() { _ = containerConn.Close() }()

	if err := containerConn.Auth(nil); err != nil {
		return fmt.Errorf("auth on container bus: %w", err)
	}
	if err := containerConn.Hello(); err != nil {
		return fmt.Errorf("hello on container bus: %w", err)
	}

	hostConn, err := dbus.ConnectSessionBus()
	if err != nil {
		return fmt.Errorf("connect host session bus: %w", err)
	}
	defer func() { _ = hostConn.Close() }()

	fwd := &forwarder{containerConn: containerConn}

	// godbus dispatches by exact method name. The Broker1 D-Bus interface uses
	// camelCase (e.g. "getLinuxBrokerVersion") but Go requires exported methods
	// to be PascalCase. ExportWithMap bridges the two.
	methodMap := map[string]string{
		"AcquireTokenInteractively": "acquireTokenInteractively",
		"AcquireTokenSilently":      "acquireTokenSilently",
		"GetAccounts":               "getAccounts",
		"RemoveAccount":             "removeAccount",
		"AcquirePrtSsoCookie":       "acquirePrtSsoCookie",
		"GenerateSignedHttpRequest": "generateSignedHttpRequest",
		"CancelInteractiveFlow":     "cancelInteractiveFlow",
		"GetLinuxBrokerVersion":     "getLinuxBrokerVersion",
	}
	if err := hostConn.ExportWithMap(fwd, methodMap, dbus.ObjectPath(ObjectPath), InterfaceName); err != nil {
		return fmt.Errorf("export forwarder: %w", err)
	}
	if err := hostConn.Export(introspect.Introspectable(introspectXML), dbus.ObjectPath(ObjectPath), "org.freedesktop.DBus.Introspectable"); err != nil {
		return fmt.Errorf("export introspectable: %w", err)
	}

	reply, err := hostConn.RequestName(BusName, dbus.NameFlagDoNotQueue)
	if err != nil {
		return fmt.Errorf("request bus name %s: %w", BusName, err)
	}
	if reply != dbus.RequestNameReplyPrimaryOwner {
		return fmt.Errorf("bus name %s already owned", BusName)
	}

	log.Printf("Broker proxy running: forwarding %s to %s", BusName, addr)

	<-ctx.Done()
	log.Println("Broker proxy shutting down")
	return nil
}
