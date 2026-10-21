package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/frostyard/clix"
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
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "rootfs"), 0755); err != nil {
		t.Fatal(err)
	}
	r := &startSecurityRunner{}
	if err := runStart(r, root); err != nil {
		t.Fatal(err)
	}
	all := strings.Join(r.commands, "\n")
	for _, want := range []string{"sudo -v", "49-intuneme-broker.rules", "sudoers.d/intuneme"} {
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
}

func TestStartMigrationFailsClosed(t *testing.T) {
	for _, fail := range []string{"sudo -v", "49-intuneme-broker.rules", "sudoers.d/intuneme"} {
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
