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
	commands []string
	fail     string
}

func (r *startSecurityRunner) Run(name string, args ...string) ([]byte, error) {
	command := name + " " + strings.Join(args, " ")
	r.commands = append(r.commands, command)
	if r.fail != "" && strings.Contains(command, r.fail) {
		return nil, fmt.Errorf("injected failure")
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
	// nsenter launch path needs the user manager and session bus.
	if !strings.Contains(all, "sudo machinectl shell root@intuneme /bin/loginctl enable-linger") {
		t.Fatalf("already-running start did not enable linger: %s", all)
	}
}

func TestStartLingerFailureIsFatal(t *testing.T) {
	mockLaunchInstall(t)
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "rootfs"), 0755); err != nil {
		t.Fatal(err)
	}
	r := &startSecurityRunner{fail: "enable-linger"}
	if err := runStart(r, root); err == nil {
		t.Fatal("linger failure reported as success")
	}
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
