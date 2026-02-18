package prereq

import (
	"fmt"
	"strings"
	"testing"
)

type mockRunner struct {
	available map[string]bool
}

func (m *mockRunner) Run(name string, args ...string) ([]byte, error) {
	return nil, nil
}

func (m *mockRunner) RunAttached(name string, args ...string) error {
	return nil
}

func (m *mockRunner) RunBackground(name string, args ...string) error {
	return nil
}

func (m *mockRunner) LookPath(name string) (string, error) {
	if m.available[name] {
		return "/usr/bin/" + name, nil
	}
	return "", fmt.Errorf("not found: %s", name)
}

func TestCheckAllPresent(t *testing.T) {
	r := &mockRunner{available: map[string]bool{
		"systemd-nspawn": true, "machinectl": true,
	}}
	errs := Check(r)
	if len(errs) != 0 {
		t.Errorf("expected no errors, got %v", errs)
	}
}

func TestCheckMissing(t *testing.T) {
	r := &mockRunner{available: map[string]bool{
		"machinectl": true,
	}}
	errs := Check(r)
	if len(errs) != 1 {
		t.Fatalf("expected 1 error, got %d: %v", len(errs), errs)
	}
	if !strings.Contains(errs[0].Error(), "systemd-nspawn") {
		t.Errorf("expected systemd-nspawn error, got: %v", errs[0])
	}
}
