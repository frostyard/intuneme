package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/frostyard/clix"
	"github.com/frostyard/intuneme/internal/bluezproxy"
	"github.com/frostyard/intuneme/internal/config"
	"github.com/frostyard/intuneme/internal/nspawn"
	"github.com/frostyard/intuneme/internal/problemrec/problemrectest"
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
	oldNow, oldSleep := lingerNow, lingerSleep
	now := time.Unix(0, 0)
	lingerNow = func() time.Time { return now }
	lingerSleep = func(time.Duration) { now = now.Add(20 * time.Second) }
	t.Cleanup(func() { lingerNow, lingerSleep = oldNow, oldSleep })
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

func TestHimmelblauStartBindsBlueZOnlyForTerminalPasskeys(t *testing.T) {
	mockLaunchInstall(t)
	for _, terminal := range []bool{false, true} {
		t.Run(fmt.Sprintf("terminal=%t", terminal), func(t *testing.T) {
			root, err := os.MkdirTemp("", "imb")
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = os.RemoveAll(root) })
			rootfs := filepath.Join(root, "rootfs")
			defaults := filepath.Join(rootfs, "usr/lib/himmelblau/himmelblau.conf")
			if err := os.MkdirAll(filepath.Dir(defaults), 0755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(defaults, []byte("[global]\n"), 0644); err != nil {
				t.Fatal(err)
			}
			cfg := &config.Config{
				MachineName: "intuneme", RootfsPath: rootfs, HostUID: os.Getuid(), HostUser: "alice",
				Himmelblau: true, HimmelblauDomain: "example.com", HimmelblauUPN: "alice@example.com",
				HimmelblauTerminalPasskey: terminal,
			}
			if terminal {
				cfg.HimmelblauTerminalPasskeySource = strings.Repeat("a", 40)
				writeTerminalPasskeyMarker(t, rootfs, "usr/share/intuneme/himmelblau-terminal-passkey-source", cfg.HimmelblauTerminalPasskeySource)
			}
			if err := cfg.Save(root); err != nil {
				t.Fatal(err)
			}
			r := &coldBootRunner{}
			if err := runStart(r, root); err != nil {
				t.Fatal(err)
			}
			all := strings.Join(r.commands, "\n")
			var boot string
			for _, c := range r.commands {
				if strings.HasPrefix(c, "sudo systemd-nspawn ") {
					boot = c
				}
			}
			wantBind := "--bind-ro=" + filepath.Join(root, "runtime/himmelblau-bluez") + ":" + bluezproxy.ContainerDir
			if strings.Contains(boot, wantBind) != terminal {
				t.Fatalf("terminal=%t, boot = %s", terminal, boot)
			}
			if strings.Contains(all, "machinectl bind") || strings.Contains(all, "xdg-dbus-proxy") {
				t.Fatalf("startup used a runtime bind or started a proxy: %s", all)
			}
			if strings.Index(all, "install-helpers") > strings.Index(all, "sudo systemd-nspawn ") {
				t.Fatalf("Himmelblau boot preceded security reconciliation: %s", all)
			}
			if !strings.Contains(all, "sudo systemd-run --machine=intuneme --wait --pipe") ||
				!strings.Contains(all, "sudo "+nspawn.NsenterHelperPath+" exec ") {
				t.Fatalf("Himmelblau startup did not initialize linger and its session: %s", all)
			}
		})
	}
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

func mockProductNameHost(t *testing.T) {
	t.Helper()
	file := filepath.Join(t.TempDir(), "host_product_name")
	if err := os.WriteFile(file, []byte("Host Model\n"), 0o444); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(nspawn.OverrideHostProductNamePath(file))
}

func TestStartColdBootProductName(t *testing.T) {
	for _, name := range []string{"", "Test Model"} {
		t.Run(fmt.Sprintf("%q", name), func(t *testing.T) {
			mockLaunchInstall(t)
			mockProductNameHost(t)
			root := startRoot(t, fmt.Sprintf("product_name = %q\n", name))
			r := &coldBootRunner{}
			if err := runStart(r, root); err != nil {
				t.Fatal(err)
			}
			file := filepath.Join(root, "dmi", "product_name")
			var boot string
			for _, command := range r.commands {
				if strings.Contains(command, "systemd-nspawn") {
					boot = command
				}
			}
			if boot == "" {
				t.Fatal("container did not boot")
			}
			if name == "" {
				if strings.Contains(boot, nspawn.ProductNamePath) {
					t.Fatalf("unset key adds DMI mount: %s", boot)
				}
				if _, err := os.Stat(file); !os.IsNotExist(err) {
					t.Fatalf("unset key wrote product name file: %v", err)
				}
				return
			}
			want := "--bind-ro=" + file + ":" + nspawn.ProductNamePath
			if !strings.Contains(boot, want) {
				t.Fatalf("boot lacks %q: %s", want, boot)
			}
			data, err := os.ReadFile(file)
			if err != nil || string(data) != name+"\n" {
				t.Fatalf("product name = %q, %v", data, err)
			}
			info, err := os.Stat(file)
			if err != nil || info.Mode().Perm() != 0o644 {
				t.Fatalf("product name file mode: %v, %v", info, err)
			}
		})
	}
}

// --root can be relative, but systemd-nspawn needs an absolute bind source.
func TestStartProductNameRelativeRoot(t *testing.T) {
	mockLaunchInstall(t)
	mockProductNameHost(t)
	root := startRoot(t, "product_name = \"Test Model\"\n")
	t.Chdir(filepath.Dir(root))
	r := &coldBootRunner{}
	if err := runStart(r, filepath.Base(root)); err != nil {
		t.Fatal(err)
	}
	want := "--bind-ro=" + filepath.Join(root, "dmi", "product_name") + ":" + nspawn.ProductNamePath
	if !strings.Contains(strings.Join(r.commands, "\n"), want) {
		t.Fatalf("boot lacks %q: %v", want, r.commands)
	}
}

func TestStartInvalidProductNameBeforeSudo(t *testing.T) {
	for _, name := range []string{"bad\nname", strings.Repeat("x", 65)} {
		t.Run(fmt.Sprintf("%q", name), func(t *testing.T) {
			mockProductNameHost(t)
			s := problemrectest.Use(t, recordVersion)
			root := identityRoot(t, fmt.Sprintf("product_name = %q\n", name))
			r := &coldBootRunner{}
			err := runStart(r, root)
			if err == nil || !strings.Contains(err.Error(), "invalid product_name in config.toml") {
				t.Fatalf("error = %v", err)
			}
			if len(r.commands) != 0 {
				t.Fatalf("invalid value ran commands: %v", r.commands)
			}
			s.ExpectNone(50 * time.Millisecond)
		})
	}
}

func TestStartProductNameDryRun(t *testing.T) {
	mockProductNameHost(t)
	old := clix.DryRun
	clix.DryRun = true
	t.Cleanup(func() { clix.DryRun = old })
	root := startRoot(t, "product_name = \"Test Model\"\n")
	r := &coldBootRunner{}
	if err := runStart(r, root); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "dmi")); !os.IsNotExist(err) {
		t.Fatalf("dry run created DMI directory: %v", err)
	}
	for _, command := range r.commands {
		if strings.HasPrefix(command, "sudo ") || strings.Contains(command, "systemd-nspawn") {
			t.Fatalf("dry run changed container: %s", command)
		}
	}
}

func TestStartProductNamePreparationFailureIsNotRecorded(t *testing.T) {
	mockLaunchInstall(t)
	s := problemrectest.Use(t, recordVersion)
	root := identityRoot(t, "product_name = \"Test Model\"\n")
	t.Cleanup(nspawn.OverrideHostProductNamePath(filepath.Join(t.TempDir(), "missing")))
	r := &coldBootRunner{}
	err := runStart(r, root)
	if err == nil || !strings.Contains(err.Error(), "remove product_name") {
		t.Fatalf("error = %v", err)
	}
	// The host DMI check runs before sudo: no password prompt, no host change.
	if len(r.commands) != 0 {
		t.Fatalf("missing DMI target ran commands: %v", r.commands)
	}
	s.ExpectNone(50 * time.Millisecond)
}

// A running container keeps its boot mounts: start only prints a note.
func TestStartProductNameRunningContainer(t *testing.T) {
	mockLaunchInstall(t)
	mockProductNameHost(t)
	root := startRoot(t, "product_name = \"Test Model\"\n")
	r := &startSecurityRunner{}
	if err := runStart(r, root); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "dmi")); !os.IsNotExist(err) {
		t.Fatalf("running container wrote the DMI directory: %v", err)
	}
	for _, command := range r.commands {
		if strings.Contains(command, "systemd-nspawn") {
			t.Fatalf("running container was rebooted: %s", command)
		}
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

type timedLingerRunner struct {
	startSecurityRunner
	now *time.Time
}

func (r *timedLingerRunner) Run(name string, args ...string) ([]byte, error) {
	*r.now = r.now.Add(20 * time.Second)
	return r.startSecurityRunner.Run(name, args...)
}

func TestLingerTotalBudget(t *testing.T) {
	oldNow, oldSleep := lingerNow, lingerSleep
	now := time.Unix(0, 0)
	lingerNow = func() time.Time { return now }
	lingerSleep = func(d time.Duration) { now = now.Add(d) }
	t.Cleanup(func() { lingerNow, lingerSleep = oldNow, oldSleep })
	r := &timedLingerRunner{startSecurityRunner: startSecurityRunner{fail: "enable-linger", failOutput: "last D-Bus output"}, now: &now}
	err := enableUserLinger(r, &config.Config{HostUser: "user", MachineName: "intuneme"})
	if len(r.commands) != 2 {
		t.Fatalf("attempts = %d, want 2", len(r.commands))
	}
	if err == nil || !strings.Contains(err.Error(), "last D-Bus output") {
		t.Fatalf("error lost last output: %v", err)
	}
}

func TestStartReconcilesRootfs(t *testing.T) {
	for _, cold := range []bool{false, true} {
		t.Run(fmt.Sprint(cold), func(t *testing.T) {
			mockLaunchInstall(t)
			root := t.TempDir()
			path := filepath.Join(root, "rootfs")
			if err := os.Mkdir(path, 0o700); err != nil {
				t.Fatal(err)
			}
			r := &coldBootRunner{}
			r.booted = !cold
			if err := runStart(r, root); err != nil {
				t.Fatal(err)
			}
			all := strings.Join(r.commands, "\n")
			want := "sudo chmod 0755 -- " + path
			if !strings.Contains(all, want) {
				t.Fatalf("missing reconciliation: %v", r.commands)
			}
			if strings.Index(all, want) > strings.Index(all, "machinectl show") {
				t.Fatal("reconciliation followed IsRunning")
			}
			if strings.Index(all, want) < strings.Index(all, "sudoers.d/intuneme") {
				t.Fatal("reconciliation preceded security refresh")
			}
		})
	}
}

func TestStartRootfsReconcileFailureNotRecorded(t *testing.T) {
	mockLaunchInstall(t)
	s := problemrectest.Use(t, recordVersion)
	root := identityRoot(t, "")
	if err := os.Chmod(filepath.Join(root, "rootfs"), 0o700); err != nil {
		t.Fatal(err)
	}
	r := &startSecurityRunner{fail: "sudo chmod 0755 --", failOutput: "permission denied"}
	err := runStart(r, root)
	if err == nil || !strings.Contains(err.Error(), "rootfs reconciliation failed") {
		t.Fatalf("error = %v", err)
	}
	if strings.Contains(strings.Join(r.commands, "\n"), "machinectl show") {
		t.Fatal("continued after failed reconciliation")
	}
	s.ExpectNone(50 * time.Millisecond)
}
