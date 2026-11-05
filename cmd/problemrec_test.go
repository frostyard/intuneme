package cmd

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/frostyard/intuneme/internal/problemrec"
	"github.com/frostyard/intuneme/internal/problemrec/problemrectest"
)

// The tests below each write 1 problem record. std caps the records of 1
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

func TestStartRecordsSecurityReconcileFailure(t *testing.T) {
	mockLaunchInstall(t)
	s := problemrectest.Use(t, recordVersion)
	r := &startSecurityRunner{fail: "install-helpers"}
	err := runStart(r, startRoot(t, ""))
	const inner = "install trusted helpers (install intuneme in a root-owned path): injected failure"
	if err == nil || err.Error() != "security migration failed: "+inner {
		t.Fatalf("error = %v", err)
	}
	checkProblem(t, s, problemrec.CodeSecurityReconcile, inner)
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

func TestOpenRecordsLaunchFailure(t *testing.T) {
	s := problemrectest.Use(t, recordVersion)
	r := &mcpMockRunner{probeErr: errors.New("injected launch failure")}
	root, _ := initializedRoot(t, true)
	err := runOpen(r, root, "microsoft-edge", nil)
	const want = "exec in container failed: injected launch failure"
	if err == nil || err.Error() != want {
		t.Fatalf("error = %v, want %s", err, want)
	}
	checkProblem(t, s, problemrec.CodeLaunchFailed, want)
}
