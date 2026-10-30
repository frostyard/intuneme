package provision

import (
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// sessionSetupEnv runs the embedded session-setup script under bash with every
// external command it touches stubbed, so the test never reaches the host's
// systemd, D-Bus, or keyring. The stub busctl reports the login collection as
// unlocked only once the stub gnome-keyring-daemon has run, unless
// unlockSticks is false, in which case it stays locked.
type sessionSetupEnv struct {
	dir     string
	runtime string
	marker  string
	log     string
}

func newSessionSetupEnv(t *testing.T, withBus, unlockSticks bool) *sessionSetupEnv {
	t.Helper()
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("bash not available")
	}
	dir := t.TempDir()
	// Unix socket paths are limited to ~108 bytes; t.TempDir() can exceed it.
	runtime, err := os.MkdirTemp("", "iss")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(runtime) })
	e := &sessionSetupEnv{
		dir:     dir,
		runtime: runtime,
		marker:  filepath.Join(dir, "keyring-init-done"),
		log:     filepath.Join(dir, "calls.log"),
	}
	if withBus {
		l, err := net.Listen("unix", filepath.Join(runtime, "bus"))
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = l.Close() })
	}

	bin := filepath.Join(dir, "bin")
	if err := os.Mkdir(bin, 0755); err != nil {
		t.Fatal(err)
	}
	unlocked := filepath.Join(dir, "unlocked")
	daemon := `echo "gnome-keyring-daemon $*" >> "$STUB_LOG"`
	if unlockSticks {
		daemon += "\ntouch " + unlocked
	}
	stubs := map[string]string{
		"busctl":                             `[ -f ` + unlocked + ` ] && echo "b false" || echo "b true"`,
		"gnome-keyring-daemon":               daemon,
		"secret-tool":                        `exit 0`,
		"systemctl":                          `echo "systemctl $*" >> "$STUB_LOG"; exit 0`,
		"pkill":                              `exit 0`,
		"sleep":                              `exit 0`,
		"dbus-update-activation-environment": `exit 0`,
	}
	for name, body := range stubs {
		if err := os.WriteFile(filepath.Join(bin, name), []byte("#!/bin/sh\n"+body+"\n"), 0755); err != nil {
			t.Fatal(err)
		}
	}
	return e
}

func (e *sessionSetupEnv) run(t *testing.T) string {
	t.Helper()
	script := strings.Replace(string(intuneSessionSetupScript),
		`_keyring_marker="/tmp/.intuneme-keyring-init-done"`,
		`_keyring_marker="`+e.marker+`"`, 1)
	if script == string(intuneSessionSetupScript) {
		t.Fatal("marker path not found in session-setup script")
	}
	cmd := exec.Command("bash", "-c", script)
	cmd.Env = []string{
		"PATH=" + filepath.Join(e.dir, "bin") + ":/usr/bin:/bin",
		"HOME=" + filepath.Join(e.dir, "home"),
		"XDG_RUNTIME_DIR=" + e.runtime,
		"STUB_LOG=" + e.log,
	}
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("session-setup failed: %v\n%s", err, out)
	}
	calls, _ := os.ReadFile(e.log)
	return string(calls)
}

func (e *sessionSetupEnv) markerWritten() bool {
	_, err := os.Stat(e.marker)
	return err == nil
}

// A GNOME extension launch right after boot runs before the container user's
// systemd manager exists. The keyring cannot be unlocked then, and writing the
// marker would suppress the unlock for the rest of the boot.
func TestSessionSetupSkipsKeyringWithoutSessionBus(t *testing.T) {
	e := newSessionSetupEnv(t, false, true)
	calls := e.run(t)
	if e.markerWritten() {
		t.Error("marker written with no session bus")
	}
	if strings.Contains(calls, "gnome-keyring-daemon") {
		t.Errorf("keyring daemon started with no session bus:\n%s", calls)
	}
}

func TestSessionSetupNoMarkerWhenUnlockFails(t *testing.T) {
	e := newSessionSetupEnv(t, true, false)
	calls := e.run(t)
	if !strings.Contains(calls, "gnome-keyring-daemon --unlock") {
		t.Fatalf("unlock not attempted:\n%s", calls)
	}
	if e.markerWritten() {
		t.Error("marker written although the login collection is still locked")
	}
	if strings.Contains(calls, "restart microsoft-identity-device-broker") {
		t.Error("device broker restarted although the keyring is still locked")
	}
}

func TestSessionSetupMarksVerifiedUnlock(t *testing.T) {
	e := newSessionSetupEnv(t, true, true)
	calls := e.run(t)
	if !e.markerWritten() {
		t.Fatalf("marker not written after a verified unlock:\n%s", calls)
	}
	if !strings.Contains(calls, "restart microsoft-identity-device-broker.service") {
		t.Errorf("device broker not restarted after unlock:\n%s", calls)
	}

	// Once marked, later session entries leave the keyring alone.
	if err := os.Remove(e.log); err != nil {
		t.Fatal(err)
	}
	if calls := e.run(t); strings.Contains(calls, "gnome-keyring-daemon") {
		t.Errorf("keyring re-initialized after marker:\n%s", calls)
	}
}
