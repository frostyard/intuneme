package provision

import (
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// sessionSetupEnv runs the embedded session-setup script under bash with every
// external command it touches stubbed, so the test never reaches the host's
// systemd, D-Bus, keyring, or syslog. The stub busctl reports the login
// collection as unlocked only once the stub gnome-keyring-daemon has run while
// the "stick" file exists, unless stick is never set, in which case it stays
// locked.
type sessionSetupEnv struct {
	dir      string
	runtime  string
	marker   string
	attempts string
	env      string
	log      string
	stick    string
	unlocked string
	lock     string
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
		dir:      dir,
		runtime:  runtime,
		marker:   filepath.Join(dir, "keyring-init-done"),
		attempts: filepath.Join(dir, "keyring-unlock-attempts"),
		env:      filepath.Join(runtime, "intuneme-session-env"),
		log:      filepath.Join(dir, "calls.log"),
		stick:    filepath.Join(dir, "unlock-sticks"),
		unlocked: filepath.Join(dir, "unlocked"),
		lock:     filepath.Join(runtime, "intuneme-keyring.lock"),
	}
	if withBus {
		l, err := net.Listen("unix", filepath.Join(runtime, "bus"))
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = l.Close() })
	}
	if unlockSticks {
		e.stickUnlock(t)
	}

	bin := filepath.Join(dir, "bin")
	if err := os.Mkdir(bin, 0755); err != nil {
		t.Fatal(err)
	}
	stubs := map[string]string{
		"busctl": `[ -f ` + e.unlocked + ` ] && echo "b false" || echo "b true"`,
		"gnome-keyring-daemon": `echo "gnome-keyring-daemon $*" >> "$STUB_LOG"
if [ -f ` + e.stick + ` ]; then touch ` + e.unlocked + `; fi`,
		"secret-tool":                        `exit 0`,
		"systemctl":                          `echo "systemctl $*" >> "$STUB_LOG"; exit 0`,
		"pkill":                              `exit 0`,
		"sleep":                              `exit 0`,
		"dbus-update-activation-environment": `exit 0`,
		// The real logger must never run: it would write to the host's syslog.
		"logger": `echo "logger $*" >> "$STUB_LOG"`,
	}
	for name, body := range stubs {
		if err := os.WriteFile(filepath.Join(bin, name), []byte("#!/bin/sh\n"+body+"\n"), 0755); err != nil {
			t.Fatal(err)
		}
	}
	return e
}

// stickUnlock makes the next keyring unlock attempt actually succeed.
func (e *sessionSetupEnv) stickUnlock(t *testing.T) {
	t.Helper()
	if err := os.WriteFile(e.stick, nil, 0644); err != nil {
		t.Fatal(err)
	}
}

func (e *sessionSetupEnv) run(t *testing.T) string {
	t.Helper()
	calls, err := e.runScript()
	if err != nil {
		t.Fatal(err)
	}
	return calls
}

// busySleep replaces the sleep stub with a short real sleep, so concurrent runs
// of the script show an overlap big enough for a race to happen.
func (e *sessionSetupEnv) busySleep(t *testing.T) {
	t.Helper()
	body := "#!/bin/sh\nexec /bin/sleep 0.05\n"
	if err := os.WriteFile(filepath.Join(e.dir, "bin", "sleep"), []byte(body), 0755); err != nil {
		t.Fatal(err)
	}
}

// runScript is run without a *testing.T so helper goroutines can call it.
func (e *sessionSetupEnv) runScript() (string, error) {
	original := string(intuneSessionSetupScript)
	script := strings.Replace(original,
		`_keyring_marker="/tmp/.intuneme-keyring-init-done"`,
		`_keyring_marker="`+e.marker+`"`, 1)
	if script == original {
		return "", errors.New("marker path not found in session-setup script")
	}
	replaced := strings.Replace(script,
		`_keyring_attempts_file="/tmp/.intuneme-keyring-unlock-attempts"`,
		`_keyring_attempts_file="`+e.attempts+`"`, 1)
	if replaced == script {
		return "", errors.New("attempts file path not found in session-setup script")
	}
	script = replaced
	cmd := exec.Command("bash", "-c", script)
	cmd.Env = []string{
		"PATH=" + filepath.Join(e.dir, "bin") + ":/usr/bin:/bin",
		"HOME=" + filepath.Join(e.dir, "home"),
		"XDG_RUNTIME_DIR=" + e.runtime,
		"STUB_LOG=" + e.log,
	}
	if out, err := cmd.CombinedOutput(); err != nil {
		return "", fmt.Errorf("session-setup failed: %w\n%s", err, out)
	}
	calls, _ := os.ReadFile(e.log)
	return string(calls), nil
}

func (e *sessionSetupEnv) readLog(t *testing.T) string {
	t.Helper()
	b, _ := os.ReadFile(e.log)
	return string(b)
}

func (e *sessionSetupEnv) markerWritten() bool {
	_, err := os.Stat(e.marker)
	return err == nil
}

func (e *sessionSetupEnv) attemptsFileExists() bool {
	_, err := os.Stat(e.attempts)
	return err == nil
}

func (e *sessionSetupEnv) envFileExists() bool {
	_, err := os.Stat(e.env)
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

// An unlock that never succeeds must not cost a pkill plus 2 s of sleep on
// every app launch and every shell login for the whole boot: the attempt count
// is capped, and the give-up is logged.
func TestSessionSetupStopsRetryingUnlock(t *testing.T) {
	e := newSessionSetupEnv(t, true, false)
	for i := 1; i <= 4; i++ {
		e.run(t)
	}
	calls := e.readLog(t)
	if got := strings.Count(calls, "gnome-keyring-daemon --unlock"); got != 3 {
		t.Errorf("unlock attempts = %d, want 3 (the cap):\n%s", got, calls)
	}
	if got := strings.Count(calls, "attempt 3 of 3"); got != 1 {
		t.Errorf("give-up log line for attempt 3 of 3 appeared %d times, want 1:\n%s", got, calls)
	}
	if got := strings.Count(calls, "no more retries this boot"); got != 1 {
		t.Errorf("give-up logged %d times, want 1:\n%s", got, calls)
	}
	if !strings.Contains(calls, "logger -t intuneme-session-setup -- ") {
		t.Errorf("log line not sent to logger:\n%s", calls)
	}
	if !strings.Contains(calls, "/tmp/.intuneme-keyring-unlock-attempts; /usr/local/bin/intuneme-session-setup") {
		t.Errorf("give-up log line does not say how to recover:\n%s", calls)
	}
	if e.markerWritten() {
		t.Error("marker written although the login collection is still locked")
	}
}

// A failed attempt must not poison a later one: a run whose unlock sticks
// writes the marker and clears the attempt counter.
func TestSessionSetupRecoversAfterOneFailedAttempt(t *testing.T) {
	e := newSessionSetupEnv(t, true, false)
	if calls := e.run(t); !strings.Contains(calls, "attempt 1 of 3") {
		t.Fatalf("first run did not log attempt 1:\n%s", calls)
	}
	if !e.attemptsFileExists() {
		t.Fatal("attempt counter not written after a failed unlock")
	}
	if e.markerWritten() {
		t.Fatal("marker written after a failed unlock")
	}

	e.stickUnlock(t)
	calls := e.run(t)
	if got := strings.Count(calls, "gnome-keyring-daemon --unlock"); got != 2 {
		t.Errorf("unlock attempts = %d, want 2 (one failure, one success):\n%s", got, calls)
	}
	if !e.markerWritten() {
		t.Fatalf("marker not written after the unlock stuck:\n%s", calls)
	}
	if e.attemptsFileExists() {
		t.Error("attempt counter not removed after a verified unlock")
	}
}

// Session entries overlap in real use: an app launch, an MCP session, and a
// login shell can all enter the script at once. Without the flock, two of them
// could read the same counter and both attempt the unlock, so the cap of 3 would
// not hold. The sleep stub really sleeps here, so the runs do overlap.
func TestSessionSetupSerializesConcurrentRuns(t *testing.T) {
	e := newSessionSetupEnv(t, true, false)
	e.busySleep(t)
	const runs = 6
	var wg sync.WaitGroup
	errs := make([]error, runs)
	for i := 0; i < runs; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, err := e.runScript()
			errs[i] = err
		}(i)
	}
	wg.Wait()
	for i, err := range errs {
		if err != nil {
			t.Errorf("concurrent run %d failed: %v", i, err)
		}
	}
	calls := e.readLog(t)
	if got := strings.Count(calls, "gnome-keyring-daemon --unlock"); got != 3 {
		t.Errorf("unlock attempts = %d, want 3 (the cap, under a lock):\n%s", got, calls)
	}
	if e.markerWritten() {
		t.Error("marker written although the login collection is still locked")
	}
}

// The intune agent timer must not fire before the session-setup script has
// pushed the environment into the D-Bus activation environment; it gates on a
// file that the script touches only when a session bus is usable.
func TestSessionSetupTouchesAgentGateFile(t *testing.T) {
	withBus := newSessionSetupEnv(t, true, true)
	withBus.run(t)
	if !withBus.envFileExists() {
		t.Errorf("%s not created with a session bus", filepath.Base(withBus.env))
	}

	withoutBus := newSessionSetupEnv(t, false, true)
	withoutBus.run(t)
	if withoutBus.envFileExists() {
		t.Error("agent gate file created with no session bus")
	}
}

func TestIntuneAgentTimerWaitsForSessionSetup(t *testing.T) {
	unit := "../../ubuntu-intune/system_files/etc/systemd/user/intune-agent.timer.d/override.conf"
	body, err := os.ReadFile(unit)
	if err != nil {
		t.Fatalf("read %s: %v", unit, err)
	}
	if !strings.Contains(string(body), "ConditionPathExists=%t/intuneme-session-env") {
		t.Errorf("%s does not gate on %%t/intuneme-session-env:\n%s", unit, body)
	}
	if !strings.Contains(string(intuneSessionSetupScript), `$XDG_RUNTIME_DIR/intuneme-session-env`) {
		t.Error("session-setup script does not create the agent gate file")
	}
}
