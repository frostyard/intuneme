package provision

import (
	"errors"
	"strings"
	"testing"
)

// semanageRunner fails the semanage call with a given output and records
// every command. All other commands succeed.
type semanageRunner struct {
	semanageOut []byte
	semanageErr error
	commands    []string
}

func (s *semanageRunner) Run(name string, args ...string) ([]byte, error) {
	s.commands = append(s.commands, name+" "+strings.Join(args, " "))
	if name == "sudo" && len(args) > 0 && args[0] == "semanage" {
		return s.semanageOut, s.semanageErr
	}
	return nil, nil
}

func (s *semanageRunner) RunAttached(name string, args ...string) error {
	s.commands = append(s.commands, name+" "+strings.Join(args, " "))
	return nil
}

func (s *semanageRunner) RunBackground(name string, args ...string) error { return nil }

func (s *semanageRunner) LookPath(name string) (string, error) { return "/usr/bin/" + name, nil }

func (s *semanageRunner) ran(prefix string) bool {
	for _, c := range s.commands {
		if strings.HasPrefix(c, prefix) {
			return true
		}
	}
	return false
}

func TestInstallSELinuxPolicySemanage(t *testing.T) {
	exitErr := errors.New("exit status 1")
	tests := []struct {
		name    string
		out     string
		err     error
		wantErr bool
	}{
		{name: "success", out: "", err: nil},
		{name: "already defined", out: "ValueError: File context for /x(/.*)? already defined\n", err: exitErr},
		{name: "already exists", out: "libsemanage: Already Exists\n", err: exitErr},
		{name: "command not found", out: "sudo: semanage: command not found\n", err: exitErr, wantErr: true},
		{name: "permission denied", out: "ValueError: SELinux policy is not managed or store cannot be accessed.\n", err: exitErr, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := &semanageRunner{semanageOut: []byte(tt.out), semanageErr: tt.err}
			err := InstallSELinuxPolicy(r, "/home/u/.local/share/intuneme/rootfs")
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected an error, got nil")
				}
				if !strings.Contains(err.Error(), strings.TrimSpace(tt.out)) {
					t.Errorf("error %q does not include the semanage output %q", err, tt.out)
				}
				if r.ran("sudo restorecon") {
					t.Error("restorecon ran after a real semanage failure")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !r.ran("sudo restorecon -RF") {
				t.Error("restorecon did not run")
			}
		})
	}
}
