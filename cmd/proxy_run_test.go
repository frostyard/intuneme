package cmd

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/frostyard/intuneme/internal/config"
)

// enableBrokerProxy points rootDir at a temp root with broker_proxy enabled.
func enableBrokerProxy(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	cfg, err := config.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	cfg.BrokerProxy = true
	if err := cfg.Save(root); err != nil {
		t.Fatal(err)
	}
	origRoot := rootDir
	rootDir = root
	t.Cleanup(func() { rootDir = origRoot })
	return root
}

func brokerEnabled(cfg *config.Config) bool { return cfg.BrokerProxy }

// A second instance that fails before it owns the bus name must leave the
// running instance's PID file alone, so stop can still find it.
func TestRunBusProxyKeepsOtherPIDFile(t *testing.T) {
	root := enableBrokerProxy(t)
	pidPath := filepath.Join(root, "broker-proxy.pid")
	// PID 1 is not a broker-proxy, so the start check lets the run go ahead.
	if err := os.WriteFile(pidPath, []byte("1"), 0o644); err != nil {
		t.Fatal(err)
	}

	errOwned := errors.New("bus name already owned")
	err := runBusProxy(context.Background(), "broker-proxy", brokerEnabled,
		func(context.Context, string, func() error) error { return errOwned })
	if !errors.Is(err, errOwned) {
		t.Fatalf("runBusProxy() = %v, want %v", err, errOwned)
	}

	data, err := os.ReadFile(pidPath)
	if err != nil {
		t.Fatalf("PID file removed: %v", err)
	}
	if got := strings.TrimSpace(string(data)); got != "1" {
		t.Errorf("PID file = %q, want %q", got, "1")
	}
}

func TestRunBusProxyWritesPIDOnReady(t *testing.T) {
	root := enableBrokerProxy(t)
	pidPath := filepath.Join(root, "broker-proxy.pid")

	err := runBusProxy(context.Background(), "broker-proxy", brokerEnabled,
		func(_ context.Context, _ string, ready func() error) error {
			if _, err := os.Stat(pidPath); err == nil {
				t.Error("PID file written before ready")
			}
			if err := ready(); err != nil {
				return err
			}
			data, err := os.ReadFile(pidPath)
			if err != nil {
				t.Fatalf("read PID file: %v", err)
			}
			if got, want := strings.TrimSpace(string(data)), strconv.Itoa(os.Getpid()); got != want {
				t.Errorf("PID file = %q, want %q", got, want)
			}
			return nil
		})
	if err != nil {
		t.Fatalf("runBusProxy() = %v", err)
	}
	if _, err := os.Stat(pidPath); !os.IsNotExist(err) {
		t.Errorf("PID file not removed on exit: %v", err)
	}
}

func TestRunBusProxyDisabled(t *testing.T) {
	enableBrokerProxy(t)

	err := runBusProxy(context.Background(), "broker-proxy",
		func(*config.Config) bool { return false },
		func(context.Context, string, func() error) error {
			t.Error("run called for a disabled proxy")
			return nil
		})
	if err == nil || !strings.Contains(err.Error(), "intuneme config broker-proxy enable") {
		t.Errorf("runBusProxy() = %v, want a not enabled error", err)
	}
}
