package broker

import (
	"errors"
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
	addr := ContainerBusAddress("/tmp/intuneme")
	want := "unix:path=/tmp/intuneme/runtime/bus"
	if addr != want {
		t.Errorf("ContainerBusAddress() = %q, want %q", addr, want)
	}
}

func TestDBusServiceFileContent(t *testing.T) {
	content := DBusServiceFileContent("/usr/local/bin/intuneme", "/home/u/.local/share/intuneme")
	if !strings.Contains(content, BusName) {
		t.Errorf("missing bus name in service file content:\n%s", content)
	}
	want := "Exec=/usr/local/bin/intuneme broker-proxy --root /home/u/.local/share/intuneme\n"
	if !strings.Contains(content, want) {
		t.Errorf("missing exec line %q in service file content:\n%s", want, content)
	}
	if !strings.Contains(content, "[D-BUS Service]") {
		t.Errorf("missing [D-BUS Service] header in service file content:\n%s", content)
	}
}

func TestDBusServiceFileContentQuotesRoot(t *testing.T) {
	content := DBusServiceFileContent("/opt/my tools/intuneme", "/home/u/my root/it's")
	want := "Exec='/opt/my tools/intuneme' broker-proxy --root '/home/u/my root/it'\\''s'\n"
	if !strings.Contains(content, want) {
		t.Errorf("missing exec line %q in service file content:\n%s", want, content)
	}
}

func stubOwnerLookup(t *testing.T, pid int, owned bool, err error) {
	t.Helper()
	old := LookupNameOwner
	LookupNameOwner = func() (int, bool, error) { return pid, owned, err }
	t.Cleanup(func() { LookupNameOwner = old })
}

func fakeProc(t *testing.T, pid int, comm, cmdline string) {
	t.Helper()
	old := procRoot
	procRoot = t.TempDir()
	t.Cleanup(func() { procRoot = old })
	dir := filepath.Join(procRoot, strconv.Itoa(pid))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "comm"), []byte(comm+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "cmdline"), []byte(cmdline), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestAlreadyOwnedErrorNamesOwner(t *testing.T) {
	fakeProc(t, 12345, "microsoft-identity-broker", "x\x00")
	stubOwnerLookup(t, 12345, true, nil)
	msg := alreadyOwnedError().Error()
	want := "bus name com.microsoft.identity.broker1 already owned by PID 12345 (microsoft-identity-broker)"
	if !strings.Contains(msg, want) {
		t.Errorf("error %q does not contain %q", msg, want)
	}
	if !strings.Contains(msg, "another intuneme broker-proxy") {
		t.Errorf("error %q has no hint", msg)
	}
}

func TestAlreadyOwnedErrorWithoutProcName(t *testing.T) {
	fakeProc(t, 1, "init", "init\x00")
	stubOwnerLookup(t, 999, true, nil)
	msg := alreadyOwnedError().Error()
	if !strings.Contains(msg, "already owned by PID 999;") {
		t.Errorf("unexpected error %q", msg)
	}
}

func TestAlreadyOwnedErrorFallback(t *testing.T) {
	want := "bus name com.microsoft.identity.broker1 already owned"
	for name, tc := range map[string]struct {
		pid   int
		owned bool
		err   error
	}{
		"lookup error": {0, false, errors.New("no session bus")},
		"no owner":     {0, false, nil},
		"bad pid":      {0, true, nil},
	} {
		t.Run(name, func(t *testing.T) {
			stubOwnerLookup(t, tc.pid, tc.owned, tc.err)
			if got := alreadyOwnedError().Error(); got != want {
				t.Errorf("error = %q, want %q", got, want)
			}
		})
	}
}

func TestIsProxyProcess(t *testing.T) {
	fakeProc(t, 42, "intuneme", "/usr/bin/intuneme\x00broker-proxy\x00--root\x00/x\x00")
	if !IsProxyProcess(42) {
		t.Error("expected broker-proxy process")
	}
	if IsProxyProcess(43) {
		t.Error("missing process must not match")
	}
	fakeProc(t, 44, "microsoft-ident", "/opt/microsoft/identity-broker/bin/microsoft-identity-broker\x00")
	if IsProxyProcess(44) {
		t.Error("foreign broker must not match")
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
