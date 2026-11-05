package cmd

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/frostyard/intuneme/internal/config"
	"github.com/frostyard/intuneme/internal/nspawn"
	"github.com/frostyard/intuneme/internal/problemrec"
	"github.com/frostyard/intuneme/internal/problemrec/problemrectest"
)

// The positive tests below each write 1 problem record (5 in all). std caps
// the records of 1 process at 10 a minute, for all recorders, so this package
// must keep its records at 10 or fewer. Every other test keeps the nil
// recorder, or writes no record.

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

// testUser is the host user of an identity root.
const testUser = "tester"

// identityRoot returns a start/open root whose config identity is consistent:
// host_user testUser, host_uid os.Getuid(), and a matching line in
// rootfs/etc/passwd. extra is appended to the config.
func identityRoot(t *testing.T, extra string) string {
	t.Helper()
	if os.Getuid() == 0 {
		t.Skip("a root test process has no consistent non-root identity")
	}
	root := startRoot(t, fmt.Sprintf("host_user = %q\nhost_uid = %d\n", testUser, os.Getuid())+extra)
	writePasswd(t, root, fmt.Sprintf("root:x:0:0:root:/root:/bin/bash\n%s:x:%d:%d::/home/%s:/bin/bash\n", testUser, os.Getuid(), os.Getgid(), testUser))
	return root
}

func writePasswd(t *testing.T, root, content string) {
	t.Helper()
	dir := filepath.Join(root, "rootfs", "etc")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "passwd"), []byte(content), 0644); err != nil {
		t.Fatal(err)
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
		identity                 bool
	}{
		{"invalid host uid", "host_uid = -1\n", "", "invalid host uid", false},
		{"sudo authentication", "", "sudo -v", "sudo authentication failed: injected failure", true},
		{"untrusted helper source", "", "install-helpers", "security migration failed: install trusted helpers", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockLaunchInstall(t)
			s := problemrectest.Use(t, recordVersion)
			r := &startSecurityRunner{fail: tt.fail}
			// A consistent identity, so only the exclusion under test
			// stops the record.
			root := startRoot(t, tt.config)
			if tt.identity {
				root = identityRoot(t, tt.config)
			}
			err := runStart(r, root)
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
	err := runStart(&bootFailRunner{}, identityRoot(t, ""))
	if err == nil || err.Error() != "failed to start container: injected boot failure" {
		t.Fatalf("error = %v", err)
	}
	checkProblem(t, s, problemrec.CodeBootFailed, "injected boot failure")
}

func lingerError(output string) string {
	return fmt.Sprintf("failed to enable linger for %s in intuneme: injected failure: %s", testUser, output)
}

func TestStartRecordsLingerFailureWhenRunning(t *testing.T) {
	shortLingerRetries(t)
	mockLaunchInstall(t)
	s := problemrectest.Use(t, recordVersion)
	const output = "Could not enable linger: Access denied"
	r := &startSecurityRunner{fail: "enable-linger", failOutput: output}
	err := runStart(r, identityRoot(t, ""))
	if err == nil || err.Error() != lingerError(output) {
		t.Fatalf("error = %v, want %s", err, lingerError(output))
	}
	checkProblem(t, s, problemrec.CodeLingerFailed, lingerError(output))
}

// A host_user with no matching container account passes the security
// reconcile, but linger then fails: that is user setup, not recorded.
func TestStartLingerFailureWithoutContainerAccountIsNotRecorded(t *testing.T) {
	tests := []struct{ name, passwd string }{
		{"no line for the user", "root:x:0:0:root:/root:/bin/bash\n"},
		{"line with another uid", fmt.Sprintf("root:x:0:0:root:/root:/bin/bash\n%s:x:%d:%d::/home/%s:/bin/bash\n", testUser, os.Getuid()+1, os.Getgid(), testUser)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			shortLingerRetries(t)
			mockLaunchInstall(t)
			s := problemrectest.Use(t, recordVersion)
			root := identityRoot(t, "")
			writePasswd(t, root, tt.passwd)
			const output = "Could not enable linger: No such user"
			r := &startSecurityRunner{fail: "enable-linger", failOutput: output}
			err := runStart(r, root)
			if err == nil || err.Error() != lingerError(output) {
				t.Fatalf("error = %v, want %s", err, lingerError(output))
			}
			s.ExpectNone(50 * time.Millisecond)
		})
	}
}

func TestStartRecordsLingerFailureOnColdBoot(t *testing.T) {
	shortLingerRetries(t)
	mockLaunchInstall(t)
	s := problemrectest.Use(t, recordVersion)
	const output = "Could not enable linger: Access denied"
	r := &coldBootRunner{failLinger: true, failOutput: output}
	err := runStart(r, identityRoot(t, ""))
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
	root := identityRoot(t, "broker_proxy = true\n")
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

// busTimeoutLine is the prologue's session bus diagnostic for uid.
func busTimeoutLine(uid int) string {
	return fmt.Sprintf("intuneme: the container user session bus (/run/user/%d/bus) is not ready after 30 s; run 'intuneme start' and try again", uid)
}

func TestOpenRecordsSessionBusTimeout(t *testing.T) {
	s := problemrectest.Use(t, recordVersion)
	root := identityRoot(t, "")
	output := busTimeoutLine(os.Getuid())
	err := runOpen(&execOutputRunner{output: output + "\n"}, root, "microsoft-edge", nil)
	want := "exec in container failed: exit status 1: " + output
	if err == nil || err.Error() != want {
		t.Fatalf("error = %v, want %s", err, want)
	}
	checkProblem(t, s, problemrec.CodeSessionBusTimeout, want)
}

// With host_uid = -1 the prologue waits for /run/user/-1/bus and times out:
// a bad config, not recorded.
func TestOpenBadHostUIDTimeoutIsNotRecorded(t *testing.T) {
	s := problemrectest.Use(t, recordVersion)
	root := identityRoot(t, "")
	if err := os.WriteFile(filepath.Join(root, "config.toml"), []byte(fmt.Sprintf("host_user = %q\nhost_uid = -1\n", testUser)), 0644); err != nil {
		t.Fatal(err)
	}
	output := busTimeoutLine(-1)
	err := runOpen(&execOutputRunner{output: output + "\n"}, root, "microsoft-edge", nil)
	want := "exec in container failed: exit status 1: " + output
	if err == nil || err.Error() != want {
		t.Fatalf("error = %v, want %s", err, want)
	}
	s.ExpectNone(50 * time.Millisecond)
}

// Any other launch failure, such as sudo authentication without the
// passwordless rule, is not recorded, even with a consistent identity.
func TestOpenSudoFailureIsNotRecorded(t *testing.T) {
	s := problemrectest.Use(t, recordVersion)
	root := identityRoot(t, "")
	err := runOpen(&execOutputRunner{output: "sudo: a password is required\n"}, root, "microsoft-edge", nil)
	const want = "exec in container failed: exit status 1: sudo: a password is required"
	if err == nil || err.Error() != want {
		t.Fatalf("error = %v, want %s", err, want)
	}
	s.ExpectNone(50 * time.Millisecond)
}

func TestIdentityConsistent(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("a root test process has no consistent non-root identity")
	}
	uid := os.Getuid()
	good := fmt.Sprintf("root:x:0:0:root:/root:/bin/bash\n%s:x:%d:%d::/home/%s:/bin/bash\n", testUser, uid, uid, testUser)
	tests := []struct {
		name   string
		user   string
		uid    int
		passwd string // "" means no file; "symlink" links etc/passwd to a good file
		want   bool
	}{
		{"good", testUser, uid, good, true},
		{"missing passwd", testUser, uid, "", false},
		{"symlinked passwd", testUser, uid, "symlink", false},
		{"no line for the user", testUser, uid, "root:x:0:0:root:/root:/bin/bash\n", false},
		{"wrong uid in passwd", testUser, uid, fmt.Sprintf("%s:x:%d:%d::/home/x:/bin/sh\n", testUser, uid+1, uid), false},
		{"uid is not the running user", testUser, uid + 1, fmt.Sprintf("%s:x:%d:%d::/home/x:/bin/sh\n", testUser, uid+1, uid), false},
		{"negative uid", testUser, -1, "tester:x:-1:-1::/home/x:/bin/sh\n", false},
		{"root user", "root", uid, fmt.Sprintf("root:x:%d:0::/root:/bin/sh\n", uid), false},
		{"empty user", "", uid, fmt.Sprintf(":x:%d:0::/x:/bin/sh\n", uid), false},
		{"user with a colon", "a:b", uid, fmt.Sprintf("a:b:%d:0::/x:/bin/sh\n", uid), false},
		{"user with a space", "a b", uid, fmt.Sprintf("a b:x:%d:0::/x:/bin/sh\n", uid), false},
		{"passwd over 1 MiB", testUser, uid, strings.Repeat("#\n", maxPasswdSize/2+1) + good, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := t.TempDir()
			rootfs := filepath.Join(root, "rootfs")
			if err := os.MkdirAll(filepath.Join(rootfs, "etc"), 0755); err != nil {
				t.Fatal(err)
			}
			passwd := filepath.Join(rootfs, "etc", "passwd")
			switch tt.passwd {
			case "":
			case "symlink":
				target := filepath.Join(root, "passwd")
				if err := os.WriteFile(target, []byte(good), 0644); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(target, passwd); err != nil {
					t.Fatal(err)
				}
			default:
				if err := os.WriteFile(passwd, []byte(tt.passwd), 0644); err != nil {
					t.Fatal(err)
				}
			}
			cfg := &config.Config{HostUser: tt.user, HostUID: tt.uid, RootfsPath: rootfs, MachineName: "intuneme"}
			if got := identityConsistent(cfg); got != tt.want {
				t.Errorf("identityConsistent = %v, want %v", got, tt.want)
			}
		})
	}
	if identityConsistent(nil) {
		t.Error("identityConsistent(nil) = true")
	}
}
