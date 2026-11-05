package cmd

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/frostyard/intuneme/internal/nspawn"
	"github.com/frostyard/intuneme/internal/problemrec"
	"github.com/frostyard/intuneme/internal/problemrec/problemrectest"
)

// The positive tests below each write 1 problem record (5 in all). std caps the records of 1
// process at 10 a minute, for all recorders, so this package must keep its
// records at 10 or fewer. Every other test keeps the nil recorder.

// recordVersion is the stamped release version that the tests use.
const recordVersion = "v0.21.0"

// checkProblem reads the next record and checks it is a valid error record
// with code, whose MESSAGE is the original error text.
func checkProblem(t *testing.T, s *problemrectest.Socket, code, message string) {
	t.Helper()
	f := s.Next()
	problemrectest.Check(t, f, "error", "0.21.0", code)
	if got := f["MESSAGE"]; got != message {
		t.Errorf("MESSAGE = %q, want %q", got, message)
	}
}

func startRoot(t *testing.T, config string) string {
	t.Helper()
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "rootfs"), 0755); err != nil {
		t.Fatal(err)
	}
	if config != "" {
		if err := os.WriteFile(filepath.Join(root, "config.toml"), []byte(config), 0644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

// User setup and authentication errors are never recorded. These tests use a
// real test socket, so they prove that nothing is written (a nil recorder
// would prove nothing). They write no record.
func TestStartUserErrorsAreNotRecorded(t *testing.T) {
	tests := []struct {
		name, config, fail, want string
	}{
		{"invalid host uid", "host_uid = -1\n", "", "invalid host uid"},
		{"sudo authentication", "", "sudo -v", "sudo authentication failed: injected failure"},
		{"untrusted helper source", "", "install-helpers", "security migration failed: install trusted helpers"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockLaunchInstall(t)
			s := problemrectest.Use(t, recordVersion)
			r := &startSecurityRunner{fail: tt.fail}
			err := runStart(r, startRoot(t, tt.config))
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("error = %v, want %q", err, tt.want)
			}
			s.ExpectNone(50 * time.Millisecond)
		})
	}
}

// bootFailRunner is a cold boot whose systemd-nspawn does not start.
type bootFailRunner struct{ coldBootRunner }

func (r *bootFailRunner) RunBackground(name string, args ...string) error {
	if strings.Contains(strings.Join(args, " "), "systemd-nspawn") {
		return errors.New("injected boot failure")
	}
	_, err := r.Run(name, args...)
	return err
}

func TestStartRecordsBootFailure(t *testing.T) {
	mockLaunchInstall(t)
	s := problemrectest.Use(t, recordVersion)
	err := runStart(&bootFailRunner{}, startRoot(t, ""))
	if err == nil || err.Error() != "failed to start container: injected boot failure" {
		t.Fatalf("error = %v", err)
	}
	checkProblem(t, s, problemrec.CodeBootFailed, "injected boot failure")
}

func lingerError(output string) string {
	return fmt.Sprintf("failed to enable linger for %s in intuneme: injected failure: %s", os.Getenv("USER"), output)
}

func TestStartRecordsLingerFailureWhenRunning(t *testing.T) {
	shortLingerRetries(t)
	mockLaunchInstall(t)
	s := problemrectest.Use(t, recordVersion)
	const output = "Could not enable linger: Access denied"
	r := &startSecurityRunner{fail: "enable-linger", failOutput: output}
	err := runStart(r, startRoot(t, ""))
	if err == nil || err.Error() != lingerError(output) {
		t.Fatalf("error = %v, want %s", err, lingerError(output))
	}
	checkProblem(t, s, problemrec.CodeLingerFailed, lingerError(output))
}

func TestStartRecordsLingerFailureOnColdBoot(t *testing.T) {
	shortLingerRetries(t)
	mockLaunchInstall(t)
	s := problemrectest.Use(t, recordVersion)
	const output = "Could not enable linger: Access denied"
	r := &coldBootRunner{failLinger: true, failOutput: output}
	err := runStart(r, startRoot(t, ""))
	if err == nil || err.Error() != lingerError(output) {
		t.Fatalf("error = %v, want %s", err, lingerError(output))
	}
	checkProblem(t, s, problemrec.CodeLingerFailed, lingerError(output))
}

// proxyFailRunner is a cold boot whose proxy launch (setsid) fails.
type proxyFailRunner struct{ coldBootRunner }

func (r *proxyFailRunner) RunBackground(name string, args ...string) error {
	if name == "setsid" {
		return errors.New("injected proxy failure")
	}
	_, err := r.Run(name, args...)
	return err
}

func TestStartRecordsProxyFailure(t *testing.T) {
	mockLaunchInstall(t)
	s := problemrectest.Use(t, recordVersion)
	root := startRoot(t, "broker_proxy = true\n")
	// The container session bus is up, so start does not wait for it.
	if err := os.MkdirAll(filepath.Join(root, "runtime"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "runtime", "bus"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	err := runStart(&proxyFailRunner{}, root)
	const want = "failed to start broker-proxy: injected proxy failure"
	if err == nil || err.Error() != want {
		t.Fatalf("error = %v, want %s", err, want)
	}
	checkProblem(t, s, problemrec.CodeProxyFailed, want)
}

// execOutputRunner fails the nsenter launch with output, like sudo or the
// session bus wait would.
type execOutputRunner struct {
	mcpMockRunner
	output string
}

func (r *execOutputRunner) Run(name string, args ...string) ([]byte, error) {
	if name == "sudo" && len(args) > 0 && args[0] == nspawn.NsenterHelperPath {
		return []byte(r.output), errors.New("exit status 1")
	}
	return r.mcpMockRunner.Run(name, args...)
}

func TestOpenRecordsSessionBusTimeout(t *testing.T) {
	s := problemrectest.Use(t, recordVersion)
	const output = "intuneme: the container user session bus (/run/user/1000/bus) is not ready after 30 s; run 'intuneme start' and try again"
	root, _ := initializedRoot(t, true)
	err := runOpen(&execOutputRunner{output: output + "\n"}, root, "microsoft-edge", nil)
	const want = "exec in container failed: exit status 1: " + output
	if err == nil || err.Error() != want {
		t.Fatalf("error = %v, want %s", err, want)
	}
	checkProblem(t, s, problemrec.CodeSessionBusTimeout, want)
}

// Any other launch failure, such as sudo authentication without the
// passwordless rule, is not recorded.
func TestOpenSudoFailureIsNotRecorded(t *testing.T) {
	s := problemrectest.Use(t, recordVersion)
	root, _ := initializedRoot(t, true)
	err := runOpen(&execOutputRunner{output: "sudo: a password is required\n"}, root, "microsoft-edge", nil)
	const want = "exec in container failed: exit status 1: sudo: a password is required"
	if err == nil || err.Error() != want {
		t.Fatalf("error = %v, want %s", err, want)
	}
	s.ExpectNone(50 * time.Millisecond)
}
