package provision

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type mockRunner struct {
	commands []string
	outputs  [][]byte // if non-empty, popped in order for Run calls
}

func (m *mockRunner) Run(name string, args ...string) ([]byte, error) {
	m.commands = append(m.commands, name+" "+strings.Join(args, " "))
	if len(m.outputs) > 0 {
		out := m.outputs[0]
		m.outputs = m.outputs[1:]
		return out, nil
	}
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
	return "/usr/bin/" + name, nil
}

func TestWriteFixups(t *testing.T) {
	r := &mockRunner{}
	rootfs := "/tmp/test-rootfs"

	err := WriteFixups(r, rootfs, "testuser", 1000, 1000, "testhost")
	if err != nil {
		t.Fatalf("WriteFixups error: %v", err)
	}

	// Verify sudo commands were issued for key files
	allCmds := strings.Join(r.commands, "\n")

	for _, want := range []string{
		"etc/hostname",
		"etc/hosts",
		"fix-home-ownership.service",
		"intuneme.sh",
		"sudoers.d/intuneme",
	} {
		if !strings.Contains(allCmds, want) {
			t.Errorf("expected command referencing %q, not found in:\n%s", want, allCmds)
		}
	}

	// Verify symlinks were created
	for _, want := range []string{
		"fix-home-ownership.service",
	} {
		found := false
		for _, cmd := range r.commands {
			if strings.Contains(cmd, "ln -sf") && strings.Contains(cmd, want) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected symlink command for %q", want)
		}
	}
}

func TestSetContainerPassword(t *testing.T) {
	r := &mockRunner{}
	err := SetContainerPassword(r, "/rootfs", "alice", "H@rdPa$$w0rd!")
	if err != nil {
		t.Fatalf("SetContainerPassword error: %v", err)
	}
	if len(r.commands) != 1 {
		t.Fatalf("expected 1 command, got %d: %v", len(r.commands), r.commands)
	}
	cmd := r.commands[0]

	// The password must NOT appear in the command string (no shell interpolation).
	if strings.Contains(cmd, "H@rdPa$$w0rd!") {
		t.Errorf("password must not appear in command args, got: %s", cmd)
	}
	// Must use bind-ro to pass the file into the container.
	if !strings.Contains(cmd, "--bind-ro=") {
		t.Errorf("expected --bind-ro= in command, got: %s", cmd)
	}
	// Must redirect the file into chpasswd inside the container.
	if !strings.Contains(cmd, "chpasswd < /run/chpasswd-input") {
		t.Errorf("expected 'chpasswd < /run/chpasswd-input' in command, got: %s", cmd)
	}
}

func TestSetContainerPasswordSpecialChars(t *testing.T) {
	// A password with a single-quote would break the old shell interpolation approach.
	r := &mockRunner{}
	err := SetContainerPassword(r, "/rootfs", "alice", "It'sAGr8Pass!")
	if err != nil {
		t.Fatalf("SetContainerPassword error: %v", err)
	}
	if strings.Contains(r.commands[0], "It'sAGr8Pass!") {
		t.Errorf("password must not appear literally in command, got: %s", r.commands[0])
	}
}

func TestFindGroupGID(t *testing.T) {
	cases := []struct {
		name    string
		content string
		group   string
		want    int
	}{
		{
			name:    "found",
			content: "root:x:0:\nvideo:x:44:\nrender:x:991:\n",
			group:   "render",
			want:    991,
		},
		{
			name:    "not found",
			content: "root:x:0:\nvideo:x:44:\n",
			group:   "render",
			want:    -1,
		},
		{
			name:    "empty file",
			content: "",
			group:   "render",
			want:    -1,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tmp := filepath.Join(t.TempDir(), "group")
			if err := os.WriteFile(tmp, []byte(tc.content), 0644); err != nil {
				t.Fatalf("write temp group file: %v", err)
			}
			got, err := findGroupGID(tmp, tc.group)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tc.want {
				t.Errorf("findGroupGID(%q) = %d, want %d", tc.group, got, tc.want)
			}
		})
	}
}

func TestWritePolkitRule(t *testing.T) {
	tmp := t.TempDir()
	rulesDir := filepath.Join(tmp, "etc", "polkit-1", "rules.d")

	r := &mockRunner{}
	err := InstallPolkitRule(r, rulesDir)
	if err != nil {
		t.Fatalf("InstallPolkitRule error: %v", err)
	}

	// Basic check — at least some sudo commands were issued
	if len(r.commands) == 0 {
		t.Errorf("expected sudo commands for polkit installation")
	}
}
