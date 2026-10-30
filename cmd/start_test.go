package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/frostyard/clix"
	"github.com/frostyard/intuneme/internal/runner"
	"github.com/frostyard/intuneme/internal/sudoers"
)

type startSecurityRunner struct {
	commands   []string
	fail       string
	failOutput string // returned as command output with an injected failure
	failCount  int    // how many times fail fires; 0 means every time
	failed     int
}

func (r *startSecurityRunner) Run(name string, args ...string) ([]byte, error) {
	command := name + " " + strings.Join(args, " ")
	r.commands = append(r.commands, command)
	if r.fail != "" && strings.Contains(command, r.fail) {
		if r.failCount > 0 && r.failed >= r.failCount {
			return nil, nil
		}
		r.failed++
		return []byte(r.failOutput), fmt.Errorf("injected failure")
	}
	return nil, nil
}
func (r *startSecurityRunner) RunAttached(name string, args ...string) error {
	_, err := r.Run(name, args...)
	return err
}
func (r *startSecurityRunner) RunBackground(name string, args ...string) error {
	_, err := r.Run(name, args...)
	return err
}
func (r *startSecurityRunner) LookPath(name string) (string, error) { return "/usr/bin/" + name, nil }

func TestStartMigratesRunningContainer(t *testing.T) {
	mockLaunchInstall(t)
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "rootfs"), 0755); err != nil {
		t.Fatal(err)
	}
	r := &startSecurityRunner{}
	if err := runStart(r, root); err != nil {
		t.Fatal(err)
	}
	all := strings.Join(r.commands, "\n")
	for _, want := range []string{"sudo -v", "50-intuneme.rules", "install-helpers", "49-intuneme-broker.rules", "sudoers.d/intuneme"} {
		if !strings.Contains(all, want) {
			t.Fatalf("missing %s: %s", want, all)
		}
	}
	if strings.Index(all, "sudoers.d/intuneme") > strings.Index(all, "machinectl show") {
		t.Fatal("migration followed early return")
	}
	if strings.Contains(all, "sudo systemd-nspawn") {
		t.Fatal("running container was rebooted")
	}
	// Without proxies, linger is still required: the GNOME extension's
	// nsenter launch path needs the user manager and session bus. systemd-run
	// --wait reports the inner loginctl exit status; machinectl shell does not.
	if !strings.Contains(all, "sudo systemd-run --machine=intuneme --wait --pipe") {
		t.Fatalf("already-running start did not enable linger: %s", all)
	}
}

func TestStartLingerFailureIsFatal(t *testing.T) {
	shortLingerRetries(t)
	mockLaunchInstall(t)
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "rootfs"), 0755); err != nil {
		t.Fatal(err)
	}
	r := &startSecurityRunner{fail: "enable-linger", failOutput: "Could not enable linger: Access denied"}
	err := runStart(r, root)
	if err == nil {
		t.Fatal("linger failure reported as success")
	}
	// The inner loginctl output must reach the error: systemd-run --wait makes
	// an inner failure a non-zero exit of the outer command.
	if !strings.Contains(err.Error(), "Could not enable linger: Access denied") {
		t.Fatalf("error lost the command output: %v", err)
	}
}

// Linger runs right after registration, when logind may not answer yet.
func TestStartLingerRetriesTransientFailure(t *testing.T) {
	shortLingerRetries(t)
	mockLaunchInstall(t)
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "rootfs"), 0755); err != nil {
		t.Fatal(err)
	}
	r := &startSecurityRunner{fail: "enable-linger", failOutput: "Bus not available", failCount: 1}
	if err := runStart(r, root); err != nil {
		t.Fatalf("transient linger failure was fatal: %v", err)
	}
	if got := r.failed; got != 1 {
		t.Fatalf("failures = %d, want 1", got)
	}
	var lingerCalls int
	for _, c := range r.commands {
		if strings.Contains(c, "enable-linger") {
			lingerCalls++
		}
	}
	if lingerCalls != 2 {
		t.Fatalf("linger attempts = %d, want 2: %v", lingerCalls, r.commands)
	}
}

func shortLingerRetries(t *testing.T) {
	t.Helper()
	oldAttempts, oldDelay := lingerAttempts, lingerRetryDelay
	lingerAttempts, lingerRetryDelay = 2, 0
	t.Cleanup(func() { lingerAttempts, lingerRetryDelay = oldAttempts, oldDelay })
}

func TestStartMigrationFailsClosed(t *testing.T) {
	mockLaunchInstall(t)
	for _, fail := range []string{"sudo -v", "50-intuneme.rules", "install-helpers", "49-intuneme-broker.rules", "sudoers.d/intuneme"} {
		t.Run(fail, func(t *testing.T) {
			root := t.TempDir()
			if err := os.Mkdir(filepath.Join(root, "rootfs"), 0755); err != nil {
				t.Fatal(err)
			}
			r := &startSecurityRunner{fail: fail}
			if err := runStart(r, root); err == nil {
				t.Fatal("migration failure reported as success")
			}
			if strings.Contains(strings.Join(r.commands, "\n"), "sudo systemd-nspawn") {
				t.Fatal("booted after failed migration")
			}
		})
	}
}

// coldBootRunner models a fresh boot: the machine is not registered until
// systemd-nspawn has run. It answers machinectl like a live container.
type coldBootRunner struct {
	startSecurityRunner
	booted     bool
	failLinger bool
	failOutput string
}

func (r *coldBootRunner) Run(name string, args ...string) ([]byte, error) {
	command := name + " " + strings.Join(args, " ")
	r.commands = append(r.commands, command)
	if strings.Contains(command, "systemd-nspawn") {
		r.booted = true
		return nil, nil
	}
	if r.failLinger && strings.Contains(command, "enable-linger") {
		return []byte(r.failOutput), fmt.Errorf("injected failure")
	}
	if strings.HasPrefix(command, "machinectl show") {
		if !r.booted {
			return nil, fmt.Errorf("machine not found")
		}
		if strings.Contains(command, "-p Leader") {
			return []byte("4242\n"), nil
		}
		return []byte("intuneme\n"), nil
	}
	return nil, nil
}

func (r *coldBootRunner) RunAttached(name string, args ...string) error {
	_, err := r.Run(name, args...)
	return err
}

func (r *coldBootRunner) RunBackground(name string, args ...string) error {
	_, err := r.Run(name, args...)
	return err
}

// On a cold boot the GNOME extension can launch an app as soon as the machine
// registers, so linger must be enabled right after registration: before the
// udev, group, YubiKey and video steps. Without a proxy there is no login
// session, and none is needed.
func TestStartColdBootWithoutProxyEnablesLingerFirst(t *testing.T) {
	mockLaunchInstall(t)
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "rootfs"), 0755); err != nil {
		t.Fatal(err)
	}
	r := &coldBootRunner{}
	if err := runStart(r, root); err != nil {
		t.Fatalf("runStart: %v", err)
	}

	boot, linger, udevInstall := -1, -1, -1
	for i, c := range r.commands {
		switch {
		case strings.Contains(c, "systemd-nspawn"):
			boot = i
		case strings.Contains(c, "systemd-run --machine=intuneme --wait --pipe") && strings.Contains(c, "enable-linger"):
			if linger < 0 {
				linger = i
			}
		case strings.Contains(c, "udevadm control --reload-rules"):
			if udevInstall < 0 {
				udevInstall = i
			}
		}
	}
	if boot < 0 {
		t.Fatalf("container was not booted: %v", r.commands)
	}
	if linger < 0 {
		t.Fatalf("linger was not enabled: %v", r.commands)
	}
	if udevInstall < 0 {
		t.Fatalf("udev rules were not installed: %v", r.commands)
	}
	if linger < boot {
		t.Errorf("linger ran before boot: %v", r.commands)
	}
	if linger > udevInstall {
		t.Errorf("linger ran after the udev install; an extension launch can arrive first: %v", r.commands)
	}
	for _, c := range r.commands {
		if strings.Contains(c, "@intuneme") {
			t.Errorf("no proxy is enabled, but a login session was created: %s", c)
		}
	}
}

// Linger comes before the udev, group, YubiKey and video steps, so a linger
// failure stops the boot there: nothing after it runs, and the error says why.
func TestStartColdBootLingerFailureIsFatal(t *testing.T) {
	shortLingerRetries(t)
	mockLaunchInstall(t)
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "rootfs"), 0755); err != nil {
		t.Fatal(err)
	}
	const message = "Could not enable linger: Access denied"
	r := &coldBootRunner{failLinger: true, failOutput: message}
	err := runStart(r, root)
	if err == nil {
		t.Fatal("linger failure reported as success")
	}
	if !strings.Contains(err.Error(), message) {
		t.Fatalf("error lost the command output: %v", err)
	}
	for _, c := range r.commands {
		if strings.Contains(c, "udevadm control --reload-rules") {
			t.Fatalf("boot went on after a linger failure: %v", r.commands)
		}
	}
}

func TestStartMigrationDryRun(t *testing.T) {
	old := clix.DryRun
	clix.DryRun = true
	t.Cleanup(func() { clix.DryRun = old })
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "rootfs"), 0755); err != nil {
		t.Fatal(err)
	}
	r := &startSecurityRunner{}
	if err := runStart(r, root); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(strings.Join(r.commands, "\n"), "sudo ") {
		t.Fatal("dry run changed security configuration")
	}
}

func mockLaunchInstall(t *testing.T) {
	t.Helper()
	old := installLaunchHelpers
	installLaunchHelpers = func(r runner.Runner, opts sudoers.Options) error {
		_, err := r.Run("sudo", "install-helpers")
		return err
	}
	t.Cleanup(func() { installLaunchHelpers = old })
}

func TestStartProxyPassesAbsoluteRoot(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	if err := os.Mkdir("root", 0o755); err != nil {
		t.Fatal(err)
	}
	// A live PID in the PID file makes startProxy return at once.
	if err := os.WriteFile(filepath.Join("root", "notify-proxy.pid"), []byte(fmt.Sprint(os.Getpid())), 0o644); err != nil {
		t.Fatal(err)
	}

	r := &startSecurityRunner{}
	if err := startProxy(r, "root", "notify-proxy"); err != nil {
		t.Fatalf("startProxy: %v", err)
	}
	if len(r.commands) != 1 {
		t.Fatalf("commands = %v, want one launch", r.commands)
	}
	want := " notify-proxy --root " + filepath.Join(dir, "root")
	if got := r.commands[0]; !strings.HasPrefix(got, "setsid ") || !strings.HasSuffix(got, want) {
		t.Errorf("launch = %q, want setsid ...%s", got, want)
	}
}

func TestStartProxyFailsWithoutPIDFile(t *testing.T) {
	if testing.Short() {
		t.Skip("waits 5 seconds")
	}
	r := &startSecurityRunner{}
	if err := startProxy(r, t.TempDir(), "notify-proxy"); err == nil {
		t.Fatal("startProxy succeeded without a PID file")
	}
}
