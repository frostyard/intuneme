package cmd

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/frostyard/clix"
	"github.com/frostyard/intuneme/internal/broker"
	"github.com/frostyard/intuneme/internal/busproxy"
)

func TestStatusJSON(t *testing.T) {
	// Set JSON mode directly (--json flag is registered by clix.App.Run,
	// which isn't called in tests).
	clix.JSONOutput = true
	defer func() { clix.JSONOutput = false }()

	// Capture stdout
	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	rootDir = t.TempDir()
	rootCmd.SetArgs([]string{"status"})
	_ = rootCmd.Execute()

	_ = w.Close()
	os.Stdout = old

	var buf bytes.Buffer
	_, _ = buf.ReadFrom(r)

	if buf.Len() == 0 {
		t.Fatal("expected JSON output, got nothing")
	}

	var result map[string]any
	if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
		t.Fatalf("expected valid JSON, got: %s", buf.String())
	}

	if _, ok := result["initialized"]; !ok {
		t.Fatal("expected 'initialized' key in JSON output")
	}
}

func TestBrokerProxyStatus(t *testing.T) {
	old := findNameOwner
	defer func() { findNameOwner = old }()

	dir := t.TempDir()
	tests := []struct {
		name  string
		owner broker.NameOwner
		ok    bool
		want  string
	}{
		{"no owner", broker.NameOwner{}, false, "not running"},
		{"foreign owner", broker.NameOwner{PID: 12345, Name: "microsoft-identity-broker"}, true,
			"not running (bus name owned by PID 12345 (microsoft-identity-broker))"},
		{"foreign owner no name", broker.NameOwner{PID: 7}, true, "not running (bus name owned by PID 7)"},
		{"activated proxy", broker.NameOwner{PID: 99, Name: "intuneme", IsProxy: true, RootResolved: true, ProxyRoot: dir}, true,
			"running (PID 99, D-Bus activated)"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			findNameOwner = func() (broker.NameOwner, bool) { return tt.owner, tt.ok }
			if got := brokerProxyStatus(dir); got != tt.want {
				t.Errorf("brokerProxyStatus() = %q, want %q", got, tt.want)
			}
		})
	}
}

// TestBrokerProxyStatusDifferentRoot is the regression test for a bus name
// owned by a broker-proxy that serves another root. That is a conflict, not
// this instance's proxy.
func TestBrokerProxyStatusDifferentRoot(t *testing.T) {
	old := findNameOwner
	defer func() { findNameOwner = old }()

	dir := t.TempDir()
	other := filepath.Join(dir, "other") + string(filepath.Separator)
	findNameOwner = func() (broker.NameOwner, bool) {
		return broker.NameOwner{PID: 99, Name: "intuneme", IsProxy: true, RootResolved: true, ProxyRoot: other}, true
	}
	want := fmt.Sprintf("not running (bus name owned by intuneme broker-proxy for root %s, PID 99)", filepath.Join(dir, "other"))
	if got := brokerProxyStatus(dir); got != want {
		t.Errorf("brokerProxyStatus() = %q, want %q", got, want)
	}
}

func TestBrokerProxyStatusSameRootCleaned(t *testing.T) {
	old := findNameOwner
	defer func() { findNameOwner = old }()

	dir := t.TempDir()
	findNameOwner = func() (broker.NameOwner, bool) {
		return broker.NameOwner{PID: 99, Name: "intuneme", IsProxy: true, RootResolved: true, ProxyRoot: filepath.Join(dir, ".")}, true
	}
	want := "running (PID 99, D-Bus activated)"
	if got := brokerProxyStatus(dir); got != want {
		t.Errorf("brokerProxyStatus() = %q, want %q", got, want)
	}
}

func TestBrokerProxyStatusPIDFileWins(t *testing.T) {
	old := findNameOwner
	defer func() { findNameOwner = old }()
	findNameOwner = func() (broker.NameOwner, bool) {
		t.Error("bus must not be queried when the PID file shows a running proxy")
		return broker.NameOwner{}, false
	}
	dir := t.TempDir()
	if err := busproxy.WritePID(filepath.Join(dir, "broker-proxy.pid")); err != nil {
		t.Fatal(err)
	}
	want := fmt.Sprintf("running (PID %d)", os.Getpid())
	if got := brokerProxyStatus(dir); got != want {
		t.Errorf("brokerProxyStatus() = %q, want %q", got, want)
	}
}

// A proxy started in /srv/a with --root enrolled must not count as running
// for "intuneme --root enrolled status" run from another directory.
func TestBrokerProxyStatusRelativeRootDifferentCwd(t *testing.T) {
	old := findNameOwner
	defer func() { findNameOwner = old }()

	ownerDir := t.TempDir()
	callerDir := t.TempDir()
	ownerRoot := filepath.Join(ownerDir, "enrolled")
	findNameOwner = func() (broker.NameOwner, bool) {
		return broker.NameOwner{PID: 77, Name: "intuneme", IsProxy: true, RootResolved: true, ProxyRoot: ownerRoot}, true
	}
	t.Chdir(callerDir)
	want := fmt.Sprintf("not running (bus name owned by intuneme broker-proxy for root %s, PID 77)", ownerRoot)
	if got := brokerProxyStatus("enrolled"); got != want {
		t.Errorf("brokerProxyStatus() = %q, want %q", got, want)
	}
}

// When the owner's working directory cannot be read, a relative owner root
// must not be assumed to match the caller's root.
func TestBrokerProxyStatusUnresolvedOwnerRoot(t *testing.T) {
	old := findNameOwner
	defer func() { findNameOwner = old }()

	findNameOwner = func() (broker.NameOwner, bool) {
		return broker.NameOwner{PID: 78, Name: "intuneme", IsProxy: true, ProxyRoot: "enrolled"}, true
	}
	t.Chdir(t.TempDir())
	want := `not running (bus name owned by intuneme broker-proxy with unresolved root "enrolled", PID 78)`
	if got := brokerProxyStatus("enrolled"); got != want {
		t.Errorf("brokerProxyStatus() = %q, want %q", got, want)
	}
}

func TestPIDProxyStatus(t *testing.T) {
	root := t.TempDir()
	if got := pidProxyStatus(root, "screencast-proxy"); got != "not running" {
		t.Errorf("without PID file = %q", got)
	}
	pid := os.Getpid()
	if err := os.WriteFile(filepath.Join(root, "screencast-proxy.pid"), []byte(strconv.Itoa(pid)), 0o644); err != nil {
		t.Fatal(err)
	}
	if got, want := pidProxyStatus(root, "screencast-proxy"), fmt.Sprintf("running (PID %d)", pid); got != want {
		t.Errorf("with live PID = %q, want %q", got, want)
	}
}

// statusRecorder records every reporter record, plain or not, so tests can
// prove the status command wrote nothing after a failed JSON write.
type statusRecorder struct{ records []string }

func (r *statusRecorder) Step(int, int, string) {}
func (r *statusRecorder) Progress(int, string)  {}
func (r *statusRecorder) Warning(f string, a ...any) {
	r.records = append(r.records, fmt.Sprintf(f, a...))
}
func (r *statusRecorder) Error(error, string)  {}
func (r *statusRecorder) Complete(string, any) {}
func (r *statusRecorder) IsJSON() bool         { return false }
func (r *statusRecorder) Message(f string, a ...any) {
	r.records = append(r.records, fmt.Sprintf(f, a...))
}
func (r *statusRecorder) MessagePlain(f string, a ...any) {
	r.records = append(r.records, fmt.Sprintf(f, a...))
}

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) { return 0, errors.New("broken pipe") }

type shortWriter struct{}

func (shortWriter) Write(p []byte) (int, error) { return len(p) / 2, nil }

func TestStatusJSONWriteFailure(t *testing.T) {
	writers := map[string]io.Writer{"failed": failingWriter{}, "short": shortWriter{}}
	for _, initialized := range []bool{false, true} {
		for name, w := range writers {
			t.Run(fmt.Sprintf("%s/initialized=%v", name, initialized), func(t *testing.T) {
				root := t.TempDir()
				if initialized {
					if err := os.MkdirAll(filepath.Join(root, "rootfs"), 0o755); err != nil {
						t.Fatal(err)
					}
				}
				origRoot, origRep := rootDir, rep
				rec := &statusRecorder{}
				rootDir, rep = root, rec
				clix.JSONOutput, clix.Stdout = true, w
				t.Cleanup(func() {
					rootDir, rep = origRoot, origRep
					clix.JSONOutput, clix.Stdout = false, nil
				})

				err := statusCmd.RunE(statusCmd, nil)
				if err == nil {
					t.Fatal("expected an error when the JSON write fails")
				}
				if len(rec.records) != 0 {
					t.Fatalf("expected no reporter records after a failed write, got %q", rec.records)
				}
			})
		}
	}
}
