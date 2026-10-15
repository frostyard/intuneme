package bluezproxy

import (
	"net"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"testing"

	"github.com/godbus/dbus/v5"
)

func TestProxyArgsExposeOnlyBlueZ(t *testing.T) {
	got := proxyArgs("/run/dbus/system_bus_socket", "/home/testuser/intuneme/bluez/system-bus")
	want := []string{
		"unix:path=/run/dbus/system_bus_socket",
		"/home/testuser/intuneme/bluez/system-bus",
		"--filter",
		"--see=org.bluez",
		"--talk=org.bluez",
		"--call=org.freedesktop.DBus=org.freedesktop.DBus.*@/org/freedesktop/DBus",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("proxyArgs() = %q, want %q", got, want)
	}
}

func TestLimitedBufferCapsProxyLogs(t *testing.T) {
	buffer := &limitedBuffer{}
	input := make([]byte, maxProxyLogBytes+100)
	if n, err := buffer.Write(input); err != nil || n != len(input) {
		t.Fatalf("Write() = (%d, %v), want (%d, nil)", n, err, len(input))
	}
	if got := buffer.Len(); got != maxProxyLogBytes {
		t.Fatalf("buffer length = %d, want %d", got, maxProxyLogBytes)
	}
}

func TestEnsureHostDirCreatesPrivateStableMount(t *testing.T) {
	root := t.TempDir()
	first, err := ensureHostDir(root)
	if err != nil {
		t.Fatalf("ensureHostDir() error: %v", err)
	}
	second, err := ensureHostDir(root)
	if err != nil {
		t.Fatalf("ensureHostDir() second call error: %v", err)
	}
	if first != second {
		t.Fatalf("host dir changed across calls: %q != %q", first, second)
	}

	info, err := os.Stat(first)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0700 {
		t.Fatalf("host dir mode = %04o, want 0700", got)
	}
	if _, err := os.Stat(filepath.Join(first, boundMarker)); err != nil {
		t.Fatalf("bind marker missing: %v", err)
	}
}

func TestEnsureHostDirResolvesRelativeRoot(t *testing.T) {
	root, err := os.MkdirTemp("/tmp", "intuneme-")
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := os.RemoveAll(root); err != nil {
			t.Errorf("remove temp root: %v", err)
		}
	}()
	workingDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	relativeRoot, err := filepath.Rel(workingDir, root)
	if err != nil {
		t.Fatal(err)
	}
	got, err := ensureHostDir(relativeRoot)
	if err != nil {
		t.Fatalf("ensureHostDir() error: %v", err)
	}
	if !filepath.IsAbs(got) {
		t.Fatalf("ensureHostDir() = %q, want an absolute directory", got)
	}
}

func TestRemoveStaleSocket(t *testing.T) {
	dir := t.TempDir()
	socket := filepath.Join(dir, "socket")
	listener, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	if err := removeStaleSocket(socket); err != nil {
		t.Fatalf("removeStaleSocket() error: %v", err)
	}
	if _, err := os.Lstat(socket); !os.IsNotExist(err) {
		t.Fatalf("socket still exists, stat error = %v", err)
	}
	_ = listener.Close()
}

func TestRemoveStaleSocketRefusesRegularFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "not-a-socket")
	if err := os.WriteFile(path, []byte("keep"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := removeStaleSocket(path); err == nil {
		t.Fatal("removeStaleSocket() removed a regular file")
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("regular file was changed: %v", err)
	}
}

func TestHostBlueZProxyFiltersTheSystemBus(t *testing.T) {
	if os.Getenv("INTUNEME_TEST_HOST_BLUEZ_PROXY") != "1" {
		t.Skip("set INTUNEME_TEST_HOST_BLUEZ_PROXY=1 to test against the host BlueZ service")
	}
	proxy, err := Start(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := proxy.Stop(); err != nil {
			t.Errorf("stop proxy: %v", err)
		}
	}()

	conn, err := dbus.Connect("unix:path=" + proxy.socket)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = conn.Close() }()

	var names []string
	if err := conn.BusObject().Call("org.freedesktop.DBus.ListNames", 0).Store(&names); err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(names, "org.bluez") {
		t.Fatalf("filtered bus names %q do not expose org.bluez", names)
	}
	if slices.Contains(names, "org.freedesktop.systemd1") {
		t.Fatalf("filtered bus unexpectedly exposes host systemd: %q", names)
	}
}
