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

func TestPodmanPullAndExtract(t *testing.T) {
	r := &mockRunner{available: map[string]bool{"podman": true}}
	p := &PodmanPuller{}
	rootfs := t.TempDir()

	err := p.PullAndExtract(r, "ghcr.io/frostyard/ubuntu-intune:latest", rootfs)
	if err != nil {
		t.Fatalf("PullAndExtract error: %v", err)
	}

	// Expected commands:
	// 1. podman rm intuneme-extract (cleanup)
	// 2. podman pull <image>
	// 3. podman create --name intuneme-extract <image>
	// 4. podman export -o <tmp> intuneme-extract
	// 5. sudo tar -xf <tmp> -C <rootfs>
	// 6. podman rm intuneme-extract
	if len(r.commands) != 6 {
		t.Fatalf("expected 6 commands, got %d: %v", len(r.commands), r.commands)
	}
	if !strings.Contains(r.commands[1], "podman pull") {
		t.Errorf("cmd[1]: expected podman pull, got: %s", r.commands[1])
	}
	if !strings.Contains(r.commands[2], "podman create") {
		t.Errorf("cmd[2]: expected podman create, got: %s", r.commands[2])
	}
	if !strings.Contains(r.commands[3], "podman export") {
		t.Errorf("cmd[3]: expected podman export, got: %s", r.commands[3])
	}
	if !strings.Contains(r.commands[4], "sudo tar") {
		t.Errorf("cmd[4]: expected sudo tar, got: %s", r.commands[4])
	}
	if !strings.Contains(r.commands[5], "podman rm") {
		t.Errorf("cmd[5]: expected podman rm, got: %s", r.commands[5])
	}
}

func TestSkopeoPullAndExtract(t *testing.T) {
	r := &mockRunner{available: map[string]bool{"skopeo": true, "umoci": true}}
	p := &SkopeoPuller{}
	rootfs := t.TempDir()

	err := p.PullAndExtract(r, "ghcr.io/frostyard/ubuntu-intune:latest", rootfs)
	if err != nil {
		t.Fatalf("PullAndExtract error: %v", err)
	}

	// Expected commands:
	// 1. skopeo copy docker://<image> oci:<tmpDir>:latest
	// 2. sudo umoci raw unpack --image <tmpDir>:latest <rootfs>
	if len(r.commands) != 2 {
		t.Fatalf("expected 2 commands, got %d: %v", len(r.commands), r.commands)
	}
	if !strings.Contains(r.commands[0], "skopeo copy docker://ghcr.io/frostyard/ubuntu-intune:latest oci:") {
		t.Errorf("cmd[0]: expected skopeo copy, got: %s", r.commands[0])
	}
	if !strings.Contains(r.commands[1], "sudo umoci raw unpack --image") {
		t.Errorf("cmd[1]: expected sudo umoci raw unpack, got: %s", r.commands[1])
	}
	if !strings.Contains(r.commands[1], rootfs) {
		t.Errorf("cmd[1]: expected rootfs path %s, got: %s", rootfs, r.commands[1])
	}
}

func TestDockerPullAndExtract(t *testing.T) {
	r := &mockRunner{available: map[string]bool{"docker": true}}
	p := &DockerPuller{}
	rootfs := t.TempDir()

	err := p.PullAndExtract(r, "ghcr.io/frostyard/ubuntu-intune:latest", rootfs)
	if err != nil {
		t.Fatalf("PullAndExtract error: %v", err)
	}

	// Expected commands:
	// 1. docker rm intuneme-extract (cleanup)
	// 2. docker pull <image>
	// 3. docker create --name intuneme-extract <image>
	// 4. docker export -o <tmp> intuneme-extract
	// 5. sudo tar -xf <tmp> -C <rootfs>
	// 6. docker rm intuneme-extract
	if len(r.commands) != 6 {
		t.Fatalf("expected 6 commands, got %d: %v", len(r.commands), r.commands)
	}
	if !strings.Contains(r.commands[1], "docker pull") {
		t.Errorf("cmd[1]: expected docker pull, got: %s", r.commands[1])
	}
	if !strings.Contains(r.commands[2], "docker create") {
		t.Errorf("cmd[2]: expected docker create, got: %s", r.commands[2])
	}
	if !strings.Contains(r.commands[3], "docker export") {
		t.Errorf("cmd[3]: expected docker export, got: %s", r.commands[3])
	}
	if !strings.Contains(r.commands[4], "sudo tar") {
		t.Errorf("cmd[4]: expected sudo tar, got: %s", r.commands[4])
	}
	if !strings.Contains(r.commands[5], "docker rm") {
		t.Errorf("cmd[5]: expected docker rm, got: %s", r.commands[5])
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
