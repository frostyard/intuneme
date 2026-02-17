package broker

import (
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
)

func TestBrokerMethods(t *testing.T) {
	methods := BrokerMethods()
	if len(methods) != 8 {
		t.Fatalf("expected 8 methods, got %d", len(methods))
	}

	expected := []string{
		"acquireTokenInteractively",
		"acquireTokenSilently",
		"getAccounts",
		"removeAccount",
		"acquirePrtSsoCookie",
		"generateSignedHttpRequest",
		"cancelInteractiveFlow",
		"getLinuxBrokerVersion",
	}
	for _, e := range expected {
		if !slices.Contains(methods, e) {
			t.Errorf("missing method %q in BrokerMethods()", e)
		}
	}
}

func TestContainerBusAddress(t *testing.T) {
	addr := ContainerBusAddress("/var/lib/machines/intuneme", 1000)
	if addr != "unix:path=/var/lib/machines/intuneme/run/user/1000/bus" {
		t.Errorf("unexpected address: %s", addr)
	}
}

func TestContainerBusAddressDifferentUID(t *testing.T) {
	addr := ContainerBusAddress("/tmp/rootfs", 5001)
	if !strings.Contains(addr, "unix:path=") {
		t.Errorf("missing unix:path= prefix in: %s", addr)
	}
	if !strings.Contains(addr, "/tmp/rootfs/run/user/5001/bus") {
		t.Errorf("unexpected path in: %s", addr)
	}
}

func TestDBusServiceFileContent(t *testing.T) {
	content := DBusServiceFileContent("/usr/local/bin/intuneme")
	if !strings.Contains(content, BusName) {
		t.Errorf("missing bus name in service file content:\n%s", content)
	}
	if !strings.Contains(content, "Exec=/usr/local/bin/intuneme broker-proxy") {
		t.Errorf("missing exec line in service file content:\n%s", content)
	}
	if !strings.Contains(content, "[D-BUS Service]") {
		t.Errorf("missing [D-BUS Service] header in service file content:\n%s", content)
	}
}

func TestDBusServiceFilePath(t *testing.T) {
	path := DBusServiceFilePath()
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("could not get home dir: %v", err)
	}
	expected := filepath.Join(home, ".local", "share", "dbus-1", "services", "com.microsoft.identity.broker1.service")
	if path != expected {
		t.Errorf("expected %s, got %s", expected, path)
	}
}

func TestWriteAndReadPIDFile(t *testing.T) {
	tmp := t.TempDir()
	pidPath := filepath.Join(tmp, "test.pid")

	err := WritePIDFile(pidPath)
	if err != nil {
		t.Fatalf("WritePIDFile failed: %v", err)
	}

	pid, alive := IsRunningByPIDFile(pidPath)
	if pid != os.Getpid() {
		t.Errorf("expected PID %d, got %d", os.Getpid(), pid)
	}
	if !alive {
		t.Error("expected current process to be alive")
	}
}

func TestIsRunningByPIDFile_Missing(t *testing.T) {
	pid, alive := IsRunningByPIDFile("/nonexistent/path/test.pid")
	if pid != 0 || alive {
		t.Errorf("expected (0, false) for missing file, got (%d, %t)", pid, alive)
	}
}

func TestIsRunningByPIDFile_DeadProcess(t *testing.T) {
	tmp := t.TempDir()
	pidPath := filepath.Join(tmp, "dead.pid")
	// Write a PID that almost certainly doesn't exist
	if err := os.WriteFile(pidPath, []byte(strconv.Itoa(999999999)), 0o644); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}

	pid, alive := IsRunningByPIDFile(pidPath)
	if pid != 999999999 {
		t.Errorf("expected PID 999999999, got %d", pid)
	}
	if alive {
		t.Error("expected process to be dead")
	}
}

func TestStopByPIDFile_MissingFile(t *testing.T) {
	// Should not panic on missing file
	StopByPIDFile("/nonexistent/path/test.pid")
}
