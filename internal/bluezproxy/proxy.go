package bluezproxy

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/godbus/dbus/v5"
)

const (
	// ContainerDir is the runtime bind target inside the nspawn container.
	ContainerDir        = "/run/intuneme/bluez-proxy"
	containerSocketPath = ContainerDir + "/system-bus"
	containerBusAddress = "unix:path=" + containerSocketPath
	boundMarker         = ".intuneme-bound"
	maxProxyLogBytes    = 4096
)

// Proxy manages the host-side filtered D-Bus socket used for one PAM attempt.
type Proxy struct {
	hostDir   string
	socket    string
	lockFile  *os.File
	cmd       *exec.Cmd
	exited    chan struct{}
	waitErr   error
	stderr    *limitedBuffer
	stopOnce  sync.Once
	stopError error
}

type limitedBuffer struct {
	mu sync.Mutex
	bytes.Buffer
}

func (b *limitedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	n := len(p)
	remaining := maxProxyLogBytes - b.Buffer.Len()
	if remaining > 0 {
		if len(p) > remaining {
			p = p[:remaining]
		}
		_, _ = b.Buffer.Write(p)
	}
	return n, nil
}

func (b *limitedBuffer) Len() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.Buffer.Len()
}

func (b *limitedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.Buffer.String()
}

// Start starts an xdg-dbus-proxy process that exposes only org.bluez from the
// host system bus. Its socket lives in a stable, private directory already
// bind-mounted into the container for terminal-passkey PAM authentication.
func Start(root string) (_ *Proxy, retErr error) {
	hostDir, err := ensureHostDir(root)
	if err != nil {
		return nil, err
	}
	lockFile, err := os.OpenFile(filepath.Join(hostDir, ".lock"), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, fmt.Errorf("open host BlueZ proxy lock: %w", err)
	}
	if err := syscall.Flock(int(lockFile.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		if closeErr := lockFile.Close(); closeErr != nil {
			return nil, fmt.Errorf("acquire host BlueZ proxy lock: %w; close lock: %v", err, closeErr)
		}
		if errors.Is(err, syscall.EWOULDBLOCK) || errors.Is(err, syscall.EAGAIN) {
			return nil, fmt.Errorf("another terminal-passkey PAM attempt is using the host BlueZ proxy")
		}
		return nil, fmt.Errorf("acquire host BlueZ proxy lock: %w", err)
	}
	lockHeld := false
	defer func() {
		if !lockHeld {
			if closeErr := lockFile.Close(); closeErr != nil && retErr == nil {
				retErr = fmt.Errorf("close host BlueZ proxy lock: %w", closeErr)
			}
		}
	}()
	systemBusSocket := "/run/dbus/system_bus_socket"
	if _, err := os.Stat(systemBusSocket); err != nil {
		return nil, fmt.Errorf("host system D-Bus socket is unavailable: %w", err)
	}
	binary, err := exec.LookPath("xdg-dbus-proxy")
	if err != nil {
		return nil, fmt.Errorf("terminal passkey enrollment requires xdg-dbus-proxy on the host: %w", err)
	}

	socket := filepath.Join(hostDir, "system-bus")
	if err := removeStaleSocket(socket); err != nil {
		return nil, err
	}
	stderr := &limitedBuffer{}
	cmd := exec.Command(binary, proxyArgs(systemBusSocket, socket)...)
	cmd.Stdout = io.Discard
	cmd.Stderr = stderr
	cmd.SysProcAttr = &syscall.SysProcAttr{Pdeathsig: syscall.SIGTERM}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start filtered host BlueZ D-Bus proxy: %w", err)
	}

	proxy := &Proxy{
		hostDir:  hostDir,
		socket:   socket,
		lockFile: lockFile,
		cmd:      cmd,
		exited:   make(chan struct{}),
		stderr:   stderr,
	}
	lockHeld = true
	go func() {
		proxy.waitErr = cmd.Wait()
		close(proxy.exited)
	}()
	if err := proxy.waitReady(5 * time.Second); err != nil {
		return nil, stopAfterStartFailure(proxy, err)
	}
	if err := verifyBlueZ("unix:path=" + proxy.socket); err != nil {
		return nil, stopAfterStartFailure(proxy, err)
	}
	return proxy, nil
}

func stopAfterStartFailure(proxy *Proxy, cause error) error {
	if err := proxy.Stop(); err != nil {
		return fmt.Errorf("%w; stop filtered BlueZ proxy: %v", cause, err)
	}
	return cause
}

func proxyArgs(systemBusSocket, socket string) []string {
	return []string{
		"unix:path=" + systemBusSocket,
		socket,
		"--filter",
		"--see=org.bluez",
		"--talk=org.bluez",
		"--call=org.freedesktop.DBus=org.freedesktop.DBus.*@/org/freedesktop/DBus",
	}
}

func (p *Proxy) waitReady(timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		select {
		case <-p.exited:
			return fmt.Errorf("filtered BlueZ D-Bus proxy exited before becoming ready: %v%s", p.waitErr, p.logSuffix())
		default:
		}
		info, err := os.Lstat(p.socket)
		if err == nil && info.Mode()&os.ModeSocket != 0 {
			conn, dialErr := net.DialTimeout("unix", p.socket, 250*time.Millisecond)
			if dialErr == nil {
				_ = conn.Close()
				return nil
			}
		}
		time.Sleep(25 * time.Millisecond)
	}
	return fmt.Errorf("timed out waiting for filtered BlueZ D-Bus proxy socket%s", p.logSuffix())
}

func (p *Proxy) logSuffix() string {
	if p.stderr.Len() == 0 {
		return ""
	}
	return ": " + strings.TrimSpace(p.stderr.String())
}

func verifyBlueZ(address string) error {
	conn, err := dbus.Connect(address)
	if err != nil {
		return fmt.Errorf("connect to filtered host D-Bus proxy: %w", err)
	}
	defer func() { _ = conn.Close() }()

	var hasOwner bool
	if err := conn.BusObject().Call("org.freedesktop.DBus.NameHasOwner", 0, "org.bluez").Store(&hasOwner); err != nil {
		return fmt.Errorf("check host BlueZ service through filtered proxy: %w", err)
	}
	if !hasOwner {
		return fmt.Errorf("host BlueZ service is not active; start bluetooth.service and retry")
	}
	var objects map[dbus.ObjectPath]map[string]map[string]dbus.Variant
	if err := conn.Object("org.bluez", "/").Call("org.freedesktop.DBus.ObjectManager.GetManagedObjects", 0).Store(&objects); err != nil {
		return fmt.Errorf("inspect host Bluetooth adapters through filtered proxy: %w", err)
	}
	for _, interfaces := range objects {
		if _, ok := interfaces["org.bluez.Adapter1"]; ok {
			return nil
		}
	}
	return fmt.Errorf("host BlueZ has no visible Bluetooth adapter")
}

func removeStaleSocket(path string) error {
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("inspect filtered BlueZ proxy socket: %w", err)
	}
	if info.Mode()&os.ModeSocket == 0 {
		return fmt.Errorf("refusing to replace non-socket filtered BlueZ proxy path %q", path)
	}
	if err := os.Remove(path); err != nil {
		return fmt.Errorf("remove stale filtered BlueZ proxy socket: %w", err)
	}
	return nil
}

func ensureHostDir(root string) (string, error) {
	absoluteRoot, err := filepath.Abs(root)
	if err != nil {
		return "", fmt.Errorf("resolve intuneme root for BlueZ proxy: %w", err)
	}
	dir := filepath.Join(absoluteRoot, "runtime", "himmelblau-bluez")
	if err := os.MkdirAll(dir, 0700); err != nil {
		return "", fmt.Errorf("create host BlueZ proxy directory: %w", err)
	}
	info, err := os.Lstat(dir)
	if err != nil {
		return "", fmt.Errorf("inspect host BlueZ proxy directory: %w", err)
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return "", fmt.Errorf("host BlueZ proxy path %q is not a real directory", dir)
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || int(stat.Uid) != os.Getuid() {
		return "", fmt.Errorf("host BlueZ proxy directory %q is not owned by the current user", dir)
	}
	if err := os.Chmod(dir, 0700); err != nil {
		return "", fmt.Errorf("secure host BlueZ proxy directory: %w", err)
	}
	if len(filepath.Join(dir, "system-bus")) > 100 {
		return "", fmt.Errorf("host BlueZ proxy socket path is too long")
	}
	marker := filepath.Join(dir, boundMarker)
	markerInfo, err := os.Lstat(marker)
	if err == nil && (!markerInfo.Mode().IsRegular() || markerInfo.Mode()&os.ModeSymlink != 0) {
		return "", fmt.Errorf("host BlueZ proxy bind marker %q is not a regular file", marker)
	}
	if err != nil && !os.IsNotExist(err) {
		return "", fmt.Errorf("inspect BlueZ proxy bind marker: %w", err)
	}
	if os.IsNotExist(err) {
		file, err := os.OpenFile(marker, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if err != nil {
			return "", fmt.Errorf("create BlueZ proxy bind marker: %w", err)
		}
		if _, err := file.WriteString("intuneme filtered BlueZ proxy mount\n"); err != nil {
			if closeErr := file.Close(); closeErr != nil {
				return "", fmt.Errorf("write BlueZ proxy bind marker: %w; close marker: %v", err, closeErr)
			}
			return "", fmt.Errorf("write BlueZ proxy bind marker: %w", err)
		}
		if err := file.Close(); err != nil {
			return "", fmt.Errorf("close BlueZ proxy bind marker: %w", err)
		}
	}
	return dir, nil
}

// HostDir returns the private host directory containing the proxy socket.
func (p *Proxy) HostDir() string {
	return p.hostDir
}

// BindMarkerPath returns the container-side path used to detect an existing
// runtime bind mount.
func (p *Proxy) BindMarkerPath() string {
	return filepath.Join(ContainerDir, boundMarker)
}

// BusAddress returns the container-side system bus address for this proxy.
func (p *Proxy) BusAddress() string {
	return containerBusAddress
}

// Stop stops the proxy and removes its socket. A stopped proxy leaves the
// stable host directory bind in place, so later enroll attempts can reuse it.
func (p *Proxy) Stop() error {
	p.stopOnce.Do(func() {
		select {
		case <-p.exited:
		default:
			if err := p.cmd.Process.Signal(syscall.SIGTERM); err != nil && !errors.Is(err, os.ErrProcessDone) {
				p.stopError = fmt.Errorf("signal filtered BlueZ proxy to stop: %w", err)
			}
			select {
			case <-p.exited:
			case <-time.After(2 * time.Second):
				if err := p.cmd.Process.Kill(); err != nil && !errors.Is(err, os.ErrProcessDone) {
					if p.stopError == nil {
						p.stopError = fmt.Errorf("kill filtered BlueZ proxy after timeout: %w", err)
					}
				}
				<-p.exited
			}
		}
		if err := os.Remove(p.socket); err != nil && !os.IsNotExist(err) {
			if p.stopError == nil {
				p.stopError = fmt.Errorf("remove filtered BlueZ proxy socket: %w", err)
			}
		}
		if err := syscall.Flock(int(p.lockFile.Fd()), syscall.LOCK_UN); err != nil && p.stopError == nil {
			p.stopError = fmt.Errorf("release host BlueZ proxy lock: %w", err)
		}
		if err := p.lockFile.Close(); err != nil && p.stopError == nil {
			p.stopError = fmt.Errorf("close host BlueZ proxy lock: %w", err)
		}
	})
	return p.stopError
}
