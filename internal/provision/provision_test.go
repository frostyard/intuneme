package provision

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/frostyard/std/reporter"
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

	// Malformed GID should return an error
	t.Run("malformed GID", func(t *testing.T) {
		tmp := filepath.Join(t.TempDir(), "group")
		if err := os.WriteFile(tmp, []byte("render:x:notanumber:\n"), 0644); err != nil {
			t.Fatalf("write temp group file: %v", err)
		}
		_, err := findGroupGID(tmp, "render")
		if err == nil {
			t.Error("expected error for malformed GID, got nil")
		}
	})
}

func TestFindGroupByGID(t *testing.T) {
	cases := []struct {
		name    string
		content string
		gid     int
		want    string
	}{
		{
			name:    "found",
			content: "root:x:0:\nvideo:x:44:\nrender:x:991:\n",
			gid:     991,
			want:    "render",
		},
		{
			name:    "not found",
			content: "root:x:0:\nvideo:x:44:\n",
			gid:     991,
			want:    "",
		},
		{
			name:    "finds correct group among many",
			content: "root:x:0:\nsystemd-resolve:x:992:\nrender:x:991:\n",
			gid:     992,
			want:    "systemd-resolve",
		},
		{
			name:    "empty file",
			content: "",
			gid:     100,
			want:    "",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tmp := filepath.Join(t.TempDir(), "group")
			if err := os.WriteFile(tmp, []byte(tc.content), 0644); err != nil {
				t.Fatalf("write temp group file: %v", err)
			}
			got, err := findGroupByGID(tmp, tc.gid)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tc.want {
				t.Errorf("findGroupByGID(%d) = %q, want %q", tc.gid, got, tc.want)
			}
		})
	}
}

func TestFindFreeSystemGID(t *testing.T) {
	cases := []struct {
		name    string
		content string
		want    int
	}{
		{
			name:    "sparse file picks 999",
			content: "root:x:0:\nvideo:x:44:\nrender:x:991:\n",
			want:    999,
		},
		{
			name:    "999 taken picks 998",
			content: "root:x:0:\nfoo:x:999:\n",
			want:    998,
		},
		{
			name:    "empty file picks 999",
			content: "",
			want:    999,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tmp := filepath.Join(t.TempDir(), "group")
			if err := os.WriteFile(tmp, []byte(tc.content), 0644); err != nil {
				t.Fatalf("write temp group file: %v", err)
			}
			got, err := findFreeSystemGID(tmp)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tc.want {
				t.Errorf("findFreeSystemGID() = %d, want %d", got, tc.want)
			}
		})
	}

	// Full range should return error
	t.Run("no free GID", func(t *testing.T) {
		var lines []string
		for i := 100; i <= 999; i++ {
			lines = append(lines, fmt.Sprintf("g%d:x:%d:", i, i))
		}
		tmp := filepath.Join(t.TempDir(), "group")
		if err := os.WriteFile(tmp, []byte(strings.Join(lines, "\n")+"\n"), 0644); err != nil {
			t.Fatalf("write temp group file: %v", err)
		}
		_, err := findFreeSystemGID(tmp)
		if err == nil {
			t.Error("expected error when no free GID available, got nil")
		}
	})
}

func TestEnsureRenderGroup(t *testing.T) {
	t.Run("group missing creates it", func(t *testing.T) {
		tmp := t.TempDir()
		groupFile := filepath.Join(tmp, "etc", "group")
		if err := os.MkdirAll(filepath.Dir(groupFile), 0755); err != nil {
			t.Fatalf("setup: %v", err)
		}
		if err := os.WriteFile(groupFile, []byte("root:x:0:\nvideo:x:44:\n"), 0644); err != nil {
			t.Fatalf("setup: %v", err)
		}

		r := &mockRunner{}
		err := EnsureRenderGroup(r, reporter.NoopReporter{}, tmp, 991)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(r.commands) != 1 {
			t.Fatalf("expected 1 command, got %d: %v", len(r.commands), r.commands)
		}
		cmd := r.commands[0]
		if !strings.Contains(cmd, "groupadd") || !strings.Contains(cmd, "991") {
			t.Errorf("expected groupadd with GID 991, got: %s", cmd)
		}
	})

	t.Run("group exists with correct GID is noop", func(t *testing.T) {
		tmp := t.TempDir()
		groupFile := filepath.Join(tmp, "etc", "group")
		if err := os.MkdirAll(filepath.Dir(groupFile), 0755); err != nil {
			t.Fatalf("setup: %v", err)
		}
		if err := os.WriteFile(groupFile, []byte("root:x:0:\nrender:x:991:\n"), 0644); err != nil {
			t.Fatalf("setup: %v", err)
		}

		r := &mockRunner{}
		err := EnsureRenderGroup(r, reporter.NoopReporter{}, tmp, 991)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(r.commands) != 0 {
			t.Errorf("expected no commands for matching GID, got: %v", r.commands)
		}
	})

	t.Run("group exists with wrong GID modifies it", func(t *testing.T) {
		tmp := t.TempDir()
		groupFile := filepath.Join(tmp, "etc", "group")
		if err := os.MkdirAll(filepath.Dir(groupFile), 0755); err != nil {
			t.Fatalf("setup: %v", err)
		}
		if err := os.WriteFile(groupFile, []byte("root:x:0:\nrender:x:500:\n"), 0644); err != nil {
			t.Fatalf("setup: %v", err)
		}

		r := &mockRunner{}
		err := EnsureRenderGroup(r, reporter.NoopReporter{}, tmp, 991)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(r.commands) != 1 {
			t.Fatalf("expected 1 command, got %d: %v", len(r.commands), r.commands)
		}
		cmd := r.commands[0]
		if !strings.Contains(cmd, "groupmod") || !strings.Contains(cmd, "991") {
			t.Errorf("expected groupmod with GID 991, got: %s", cmd)
		}
	})

	t.Run("GID conflict reassigns conflicting group first", func(t *testing.T) {
		tmp := t.TempDir()
		groupFile := filepath.Join(tmp, "etc", "group")
		if err := os.MkdirAll(filepath.Dir(groupFile), 0755); err != nil {
			t.Fatalf("setup: %v", err)
		}
		// render exists at GID 500, but target GID 992 is taken by systemd-resolve
		if err := os.WriteFile(groupFile, []byte("root:x:0:\nrender:x:500:\nsystemd-resolve:x:992:\n"), 0644); err != nil {
			t.Fatalf("setup: %v", err)
		}

		r := &mockRunner{}
		err := EnsureRenderGroup(r, reporter.NoopReporter{}, tmp, 992)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(r.commands) != 2 {
			t.Fatalf("expected 2 commands, got %d: %v", len(r.commands), r.commands)
		}
		// First command: reassign systemd-resolve to a free GID
		if !strings.Contains(r.commands[0], "groupmod") || !strings.Contains(r.commands[0], "systemd-resolve") {
			t.Errorf("expected first command to reassign systemd-resolve, got: %s", r.commands[0])
		}
		// Second command: set render to target GID
		if !strings.Contains(r.commands[1], "groupmod") || !strings.Contains(r.commands[1], "992") || !strings.Contains(r.commands[1], "render") {
			t.Errorf("expected second command to set render GID to 992, got: %s", r.commands[1])
		}
	})

	t.Run("no conflict when render group is missing and GID is free", func(t *testing.T) {
		tmp := t.TempDir()
		groupFile := filepath.Join(tmp, "etc", "group")
		if err := os.MkdirAll(filepath.Dir(groupFile), 0755); err != nil {
			t.Fatalf("setup: %v", err)
		}
		// render doesn't exist, target GID 992 is free
		if err := os.WriteFile(groupFile, []byte("root:x:0:\nvideo:x:44:\n"), 0644); err != nil {
			t.Fatalf("setup: %v", err)
		}

		r := &mockRunner{}
		err := EnsureRenderGroup(r, reporter.NoopReporter{}, tmp, 992)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(r.commands) != 1 {
			t.Fatalf("expected 1 command (groupadd only), got %d: %v", len(r.commands), r.commands)
		}
		if !strings.Contains(r.commands[0], "groupadd") {
			t.Errorf("expected groupadd, got: %s", r.commands[0])
		}
	})

	t.Run("GID conflict when render group is missing", func(t *testing.T) {
		tmp := t.TempDir()
		groupFile := filepath.Join(tmp, "etc", "group")
		if err := os.MkdirAll(filepath.Dir(groupFile), 0755); err != nil {
			t.Fatalf("setup: %v", err)
		}
		// render doesn't exist, but target GID 992 is taken by systemd-resolve
		if err := os.WriteFile(groupFile, []byte("root:x:0:\nsystemd-resolve:x:992:\n"), 0644); err != nil {
			t.Fatalf("setup: %v", err)
		}

		r := &mockRunner{}
		err := EnsureRenderGroup(r, reporter.NoopReporter{}, tmp, 992)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(r.commands) != 2 {
			t.Fatalf("expected 2 commands, got %d: %v", len(r.commands), r.commands)
		}
		// First: reassign conflicting group
		if !strings.Contains(r.commands[0], "groupmod") || !strings.Contains(r.commands[0], "systemd-resolve") {
			t.Errorf("expected first command to reassign systemd-resolve, got: %s", r.commands[0])
		}
		// Second: groupadd render
		if !strings.Contains(r.commands[1], "groupadd") || !strings.Contains(r.commands[1], "render") {
			t.Errorf("expected second command to groupadd render, got: %s", r.commands[1])
		}
	})
}

func TestCreateContainerUserIncludesRender(t *testing.T) {
	tmp := t.TempDir()
	etcDir := filepath.Join(tmp, "etc")
	if err := os.MkdirAll(etcDir, 0755); err != nil {
		t.Fatalf("setup: %v", err)
	}
	if err := os.WriteFile(filepath.Join(etcDir, "passwd"), []byte("root:x:0:0:root:/root:/bin/bash\n"), 0644); err != nil {
		t.Fatalf("setup: %v", err)
	}
	if err := os.WriteFile(filepath.Join(etcDir, "group"), []byte("root:x:0:\nrender:x:991:\n"), 0644); err != nil {
		t.Fatalf("setup: %v", err)
	}

	r := &mockRunner{}
	err := CreateContainerUser(r, reporter.NoopReporter{}, tmp, "alice", 1000, 1000)
	if err != nil {
		t.Fatalf("CreateContainerUser error: %v", err)
	}

	allCmds := strings.Join(r.commands, "\n")
	if !strings.Contains(allCmds, "render") {
		t.Errorf("expected 'render' in group list, commands:\n%s", allCmds)
	}
}

func TestCreateContainerUserNoRenderGroupSkipsIt(t *testing.T) {
	tmp := t.TempDir()
	etcDir := filepath.Join(tmp, "etc")
	if err := os.MkdirAll(etcDir, 0755); err != nil {
		t.Fatalf("setup: %v", err)
	}
	if err := os.WriteFile(filepath.Join(etcDir, "passwd"), []byte("root:x:0:0:root:/root:/bin/bash\n"), 0644); err != nil {
		t.Fatalf("setup: %v", err)
	}
	if err := os.WriteFile(filepath.Join(etcDir, "group"), []byte("root:x:0:\nvideo:x:44:\n"), 0644); err != nil {
		t.Fatalf("setup: %v", err)
	}

	r := &mockRunner{}
	err := CreateContainerUser(r, reporter.NoopReporter{}, tmp, "alice", 1000, 1000)
	if err != nil {
		t.Fatalf("CreateContainerUser error: %v", err)
	}

	allCmds := strings.Join(r.commands, "\n")
	if strings.Contains(allCmds, "render") {
		t.Errorf("expected no 'render' when group absent, commands:\n%s", allCmds)
	}
}

func FuzzFindGroupGID(f *testing.F) {
	f.Add("root:x:0:\nvideo:x:44:\nrender:x:991:\n", "render")
	f.Add("root:x:0:\n", "render")
	f.Add("", "render")
	f.Add("render:x:notanumber:\n", "render")
	f.Add(":::\n", "root")
	f.Add("a:b:1:c:d:e:f\n", "a")

	f.Fuzz(func(t *testing.T, content, group string) {
		tmp := filepath.Join(t.TempDir(), "group")
		if err := os.WriteFile(tmp, []byte(content), 0644); err != nil {
			t.Fatalf("write temp file: %v", err)
		}
		gid, err := findGroupGID(tmp, group)
		if err != nil {
			return // parse errors are expected for malformed input
		}
		if gid < -1 {
			t.Errorf("findGroupGID returned unexpected GID %d", gid)
		}
	})
}

func FuzzFindUserByUID(f *testing.F) {
	f.Add("root:x:0:0:root:/root:/bin/bash\n", 0)
	f.Add("nobody:x:65534:65534::/nonexistent:/usr/sbin/nologin\n", 1000)
	f.Add("", 0)
	f.Add(":::\n", 0)
	f.Add("user:x:abc:1000::/home/user:/bin/bash\n", 1000)

	f.Fuzz(func(t *testing.T, content string, uid int) {
		tmp := filepath.Join(t.TempDir(), "passwd")
		if err := os.WriteFile(tmp, []byte(content), 0644); err != nil {
			t.Fatalf("write temp file: %v", err)
		}
		// Must never panic.
		_, err := findUserByUID(tmp, uid)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})
}

func FuzzFindFreeSystemGID(f *testing.F) {
	f.Add("root:x:0:\nvideo:x:44:\nrender:x:991:\n")
	f.Add("")
	f.Add("foo:x:999:\n")
	f.Add(":::\n")
	f.Add("a:b:notanumber:\n")

	f.Fuzz(func(t *testing.T, content string) {
		tmp := filepath.Join(t.TempDir(), "group")
		if err := os.WriteFile(tmp, []byte(content), 0644); err != nil {
			t.Fatalf("write temp file: %v", err)
		}
		// Must never panic.
		gid, err := findFreeSystemGID(tmp)
		if err != nil {
			return // expected when all GIDs are taken
		}
		if gid < 100 || gid > 999 {
			t.Errorf("findFreeSystemGID returned GID %d outside range 100-999", gid)
		}
	})
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
