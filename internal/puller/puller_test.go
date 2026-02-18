package puller

import (
	"fmt"
	"strings"
	"testing"
)

type mockRunner struct {
	available map[string]bool
	commands  []string
}

func (m *mockRunner) Run(name string, args ...string) ([]byte, error) {
	m.commands = append(m.commands, name+" "+strings.Join(args, " "))
	return nil, nil
}

func (m *mockRunner) RunAttached(name string, args ...string) error {
	m.commands = append(m.commands, name+" "+strings.Join(args, " "))
	return nil
}

func (m *mockRunner) RunBackground(name string, args ...string) error {
	m.commands = append(m.commands, name+" "+strings.Join(args, " "))
	return nil
}

func (m *mockRunner) LookPath(name string) (string, error) {
	if m.available[name] {
		return "/usr/bin/" + name, nil
	}
	return "", fmt.Errorf("not found: %s", name)
}

func TestDetectPrefersPodman(t *testing.T) {
	r := &mockRunner{available: map[string]bool{
		"podman": true, "skopeo": true, "umoci": true, "docker": true,
	}}
	p, err := Detect(r)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if p.Name() != "podman" {
		t.Errorf("expected podman, got %s", p.Name())
	}
}

func TestDetectFallsBackToSkopeo(t *testing.T) {
	r := &mockRunner{available: map[string]bool{
		"skopeo": true, "umoci": true, "docker": true,
	}}
	p, err := Detect(r)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if p.Name() != "skopeo+umoci" {
		t.Errorf("expected skopeo+umoci, got %s", p.Name())
	}
}

func TestDetectSkipsSkopeoWithoutUmoci(t *testing.T) {
	r := &mockRunner{available: map[string]bool{
		"skopeo": true, "docker": true,
	}}
	p, err := Detect(r)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if p.Name() != "docker" {
		t.Errorf("expected docker, got %s", p.Name())
	}
}

func TestDetectFallsBackToDocker(t *testing.T) {
	r := &mockRunner{available: map[string]bool{
		"docker": true,
	}}
	p, err := Detect(r)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if p.Name() != "docker" {
		t.Errorf("expected docker, got %s", p.Name())
	}
}

func TestDetectErrorsWhenNoneAvailable(t *testing.T) {
	r := &mockRunner{available: map[string]bool{}}
	_, err := Detect(r)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "no container tool found") {
		t.Errorf("unexpected error message: %v", err)
	}
}
