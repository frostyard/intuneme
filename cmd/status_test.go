package cmd

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/frostyard/clix"
	"github.com/frostyard/intuneme/internal/broker"
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
		{"activated proxy", broker.NameOwner{PID: 99, Name: "intuneme", IsProxy: true, ProxyRoot: dir}, true,
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
		return broker.NameOwner{PID: 99, Name: "intuneme", IsProxy: true, ProxyRoot: other}, true
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
		return broker.NameOwner{PID: 99, Name: "intuneme", IsProxy: true, ProxyRoot: filepath.Join(dir, ".")}, true
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
	if err := broker.WritePIDFile(filepath.Join(dir, "broker-proxy.pid")); err != nil {
		t.Fatal(err)
	}
	want := fmt.Sprintf("running (PID %d)", os.Getpid())
	if got := brokerProxyStatus(dir); got != want {
		t.Errorf("brokerProxyStatus() = %q, want %q", got, want)
	}
}
