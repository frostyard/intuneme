package broker

import (
	"context"
	"errors"
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

	"github.com/frostyard/intuneme/internal/config"
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
// that launches the broker-proxy subcommand. The root is passed with --root so
// a D-Bus activated instance uses the same root (and PID file) as
// "intuneme start".
//
// dbus-daemon parses an Exec value in two layers. First the service-file
// string is unescaped (only \s, \t, \n, \r and \\ are valid escapes), then
// the result is split with shell-like rules. The arguments are quoted for the
// shell layer and the whole value is escaped for the service-file layer.
//
// It returns an error when an argument contains a NUL byte, because a
// service-file value cannot carry one.
func DBusServiceFileContent(execPath, root string) (string, error) {
	exec, err := execLineValue([]string{execPath, "broker-proxy", "--root", root})
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("[D-BUS Service]\nName=%s\nExec=%s\n", BusName, exec), nil
}

// execLineValue builds the Exec value for an argv: each argument gets
// layer-2 (shell-like) quoting, then the joined command line gets layer-1
// (service-file) escaping.
func execLineValue(argv []string) (string, error) {
	quoted := make([]string, len(argv))
	for i, arg := range argv {
		if strings.ContainsRune(arg, 0) {
			return "", fmt.Errorf("exec argument %d contains a NUL byte", i)
		}
		quoted[i] = quoteExecArg(arg)
	}
	return escapeServiceFileValue(strings.Join(quoted, " ")), nil
}

// escapeServiceFileValue escapes a command line for a service-file value.
// dbus-daemon unescapes \s, \t, \n, \r and \\, and rejects every other
// escape, so a literal backslash must be doubled and the control characters
// that have an escape must use it.
func escapeServiceFileValue(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '\\':
			b.WriteString(`\\`)
		case '\n':
			b.WriteString(`\n`)
		case '\r':
			b.WriteString(`\r`)
		case '\t':
			b.WriteString(`\t`)
		default:
			b.WriteByte(s[i])
		}
	}
	return b.String()
}

// quoteExecArg quotes one argument of a D-Bus service file Exec line.
// dbus-daemon splits Exec with shell-like rules, so an argument that has
// characters outside a safe set is wrapped in single quotes (a single quote
// inside closes the quotes, is backslash-escaped, and reopens them). The
// result must still pass through
// escapeServiceFileValue before it goes into the file.
func quoteExecArg(arg string) string {
	if arg != "" && !strings.ContainsFunc(arg, needsExecQuote) {
		return arg
	}
	return "'" + strings.ReplaceAll(arg, "'", `'\''`) + "'"
}

func needsExecQuote(r rune) bool {
	switch {
	case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
		return false
	}
	return !strings.ContainsRune("/._-+:,@=%", r)
}

// NameOwnerLookup asks a message bus which process owns BusName. It returns
// owned=false when nobody owns the name.
type NameOwnerLookup func() (pid int, owned bool, err error)

// LookupNameOwner is the lookup used by Run and by "intuneme status". It asks
// the host session bus. Tests replace it so they do not need a session bus.
var LookupNameOwner NameOwnerLookup = sessionBusNameOwner

// procRoot is the proc filesystem mount. Tests replace it.
var procRoot = "/proc"

// sessionBusNameOwner connects to the host session bus and resolves the owner
// of BusName with org.freedesktop.DBus.GetNameOwner and
// org.freedesktop.DBus.GetConnectionUnixProcessID.
func sessionBusNameOwner() (int, bool, error) {
	conn, err := dbus.ConnectSessionBus()
	if err != nil {
		return 0, false, fmt.Errorf("connect host session bus: %w", err)
	}
	defer func() { _ = conn.Close() }()
	return nameOwnerOnBus(conn.BusObject())
}

func nameOwnerOnBus(bus dbus.BusObject) (int, bool, error) {
	var owner string
	if err := bus.Call("org.freedesktop.DBus.GetNameOwner", 0, BusName).Store(&owner); err != nil {
		var derr dbus.Error
		if errors.As(err, &derr) && derr.Name == "org.freedesktop.DBus.Error.NameHasNoOwner" {
			return 0, false, nil
		}
		return 0, false, fmt.Errorf("get owner of %s: %w", BusName, err)
	}
	var pid uint32
	if err := bus.Call("org.freedesktop.DBus.GetConnectionUnixProcessID", 0, owner).Store(&pid); err != nil {
		return 0, true, fmt.Errorf("get PID of %s: %w", owner, err)
	}
	return int(pid), true, nil
}

// ProcessName returns the name of a process from /proc/<pid>/comm, or "" if
// it cannot be read.
func ProcessName(pid int) string {
	data, err := os.ReadFile(filepath.Join(procRoot, strconv.Itoa(pid), "comm"))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

// IsProxyProcess reports whether the process is an "intuneme broker-proxy"
// instance, judged from /proc/<pid>/cmdline.
func IsProxyProcess(pid int) bool {
	_, isProxy := ProxyRoot(pid)
	return isProxy
}

// ProxyRoot returns the --root value from an "intuneme broker-proxy" command
// line. isProxy is false when the process is not an intuneme broker-proxy.
// root is empty when the proxy was started without --root; callers must then
// use the default root (see FindNameOwner).
func ProxyRoot(pid int) (root string, isProxy bool) {
	data, err := os.ReadFile(filepath.Join(procRoot, strconv.Itoa(pid), "cmdline"))
	if err != nil {
		return "", false
	}
	args := strings.Split(strings.TrimRight(string(data), "\x00"), "\x00")
	if len(args) < 2 || !strings.HasPrefix(filepath.Base(args[0]), "intuneme") {
		return "", false
	}
	for i := 1; i < len(args); i++ {
		switch {
		case args[i] == "broker-proxy":
			isProxy = true
		case args[i] == "--root" && i+1 < len(args):
			i++
			root = args[i]
		case strings.HasPrefix(args[i], "--root="):
			root = strings.TrimPrefix(args[i], "--root=")
		}
	}
	if !isProxy {
		return "", false
	}
	return root, true
}

// NameOwner describes the process that owns BusName.
type NameOwner struct {
	PID       int
	Name      string // from /proc/<pid>/comm, may be empty
	IsProxy   bool   // an "intuneme broker-proxy" instance
	ProxyRoot string // root the proxy serves; default root when --root was absent
	// RootResolved is true when ProxyRoot is absolute. A relative --root is
	// resolved against the owner's working directory (/proc/<pid>/cwd); when
	// that cannot be read, ProxyRoot keeps the raw value and RootResolved is
	// false, so callers must not assume it matches their own root.
	RootResolved bool
}

// String returns "PID 12345 (microsoft-identity-broker)", or "PID 12345" when
// the process name is unknown.
func (o NameOwner) String() string {
	if o.Name == "" {
		return fmt.Sprintf("PID %d", o.PID)
	}
	return fmt.Sprintf("PID %d (%s)", o.PID, o.Name)
}

// FindNameOwner returns the owner of BusName on the host session bus. It
// returns ok=false when the name has no owner or the lookup fails. For an
// intuneme broker-proxy it also reports the root the proxy serves; a proxy
// started without --root uses the default root.
func FindNameOwner() (owner NameOwner, ok bool) {
	pid, owned, err := LookupNameOwner()
	if err != nil || !owned || pid <= 0 {
		return NameOwner{}, false
	}
	proxyRoot, isProxy := ProxyRoot(pid)
	resolved := false
	if isProxy {
		proxyRoot, resolved = resolveProxyRoot(pid, proxyRoot)
	}
	return NameOwner{
		PID:          pid,
		Name:         ProcessName(pid),
		IsProxy:      isProxy,
		ProxyRoot:    proxyRoot,
		RootResolved: resolved,
	}, true
}

// resolveProxyRoot makes the root of a proxy process absolute. An empty root
// means the default root. A relative root is joined to the owner's working
// directory, read from /proc/<pid>/cwd, not to the caller's. ok is false when
// the root cannot be made absolute; root is then the raw value.
func resolveProxyRoot(pid int, root string) (string, bool) {
	if root == "" {
		def, err := config.DefaultRoot()
		if err != nil {
			return "", false
		}
		return filepath.Clean(def), true
	}
	if filepath.IsAbs(root) {
		return filepath.Clean(root), true
	}
	cwd, err := os.Readlink(filepath.Join(procRoot, strconv.Itoa(pid), "cwd"))
	if err != nil || !filepath.IsAbs(cwd) {
		return root, false
	}
	return filepath.Join(cwd, root), true
}

// alreadyOwnedError builds the error for a failed bus name claim. It names the
// owner when the lookup works and keeps the plain message when it does not.
func alreadyOwnedError() error {
	owner, ok := FindNameOwner()
	if !ok {
		return fmt.Errorf("bus name %s already owned", BusName)
	}
	return fmt.Errorf("bus name %s already owned by %s; another identity broker or another intuneme broker-proxy is probably running on the host",
		BusName, owner)
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
// It calls ready once it owns the bus name; an error from ready stops it.
func Run(ctx context.Context, root string, ready func() error) error {
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
		return alreadyOwnedError()
	}
	if err := ready(); err != nil {
		return err
	}

	log.Printf("Broker proxy running: forwarding %s to %s", BusName, addr)

	<-ctx.Done()
	log.Println("Broker proxy shutting down")
	return nil
}
