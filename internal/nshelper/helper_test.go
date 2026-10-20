package nshelper

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}

func testConfig(t *testing.T) *Config {
	t.Helper()
	rootfs := t.TempDir()
	return &Config{
		Machine: "intuneme",
		User:    "testuser",
		UID:     1000,
		GID:     1000,
		Home:    "/home/testuser",
		Rootfs:  rootfs,
	}
}

func TestLoadConfig(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "helper.toml")
	writeFile(t, path, `
machine = "intuneme"
user = "testuser"
uid = 1000
gid = 1000
home = "/home/testuser"
rootfs = "/home/testuser/.local/share/intuneme/rootfs"
`)
	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig failed: %v", err)
	}
	if cfg.Machine != "intuneme" || cfg.User != "testuser" || cfg.UID != 1000 {
		t.Errorf("unexpected config: %+v", cfg)
	}
}

func TestConfigValidate(t *testing.T) {
	valid := testConfig(t)
	if err := valid.Validate(); err != nil {
		t.Fatalf("valid config rejected: %v", err)
	}
	tests := []struct {
		name string
		edit func(*Config)
	}{
		{"empty machine", func(c *Config) { c.Machine = "" }},
		{"host machine", func(c *Config) { c.Machine = ".host" }},
		{"shell in machine", func(c *Config) { c.Machine = "a b" }},
		{"empty user", func(c *Config) { c.User = "" }},
		{"colon in user", func(c *Config) { c.User = "a:b" }},
		{"negative uid", func(c *Config) { c.UID = -1 }},
		{"negative gid", func(c *Config) { c.GID = -1 }},
		{"relative rootfs", func(c *Config) { c.Rootfs = "rootfs" }},
		{"relative home", func(c *Config) { c.Home = "home" }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := *valid
			tt.edit(&cfg)
			if err := cfg.Validate(); err == nil {
				t.Errorf("expected config to be rejected")
			}
		})
	}
}

func TestResolveLeader(t *testing.T) {
	oldFind, oldOutput := findExecutable, commandOutput
	t.Cleanup(func() { findExecutable, commandOutput = oldFind, oldOutput })

	findExecutable = func(name string) (string, error) { return "/usr/bin/" + name, nil }
	commandOutput = func(name string, args ...string) ([]byte, error) {
		if name != "/usr/bin/machinectl" {
			t.Errorf("unexpected command %q", name)
		}
		if strings.Join(args, " ") != "show intuneme -p Leader --value" {
			t.Errorf("unexpected args: %v", args)
		}
		return []byte("4242\n"), nil
	}

	pid, err := resolveLeader("intuneme")
	if err != nil {
		t.Fatalf("resolveLeader failed: %v", err)
	}
	if pid != 4242 {
		t.Errorf("pid = %d, want 4242", pid)
	}
}

func TestResolveLeaderRejectsInvalidOutput(t *testing.T) {
	oldFind, oldOutput := findExecutable, commandOutput
	t.Cleanup(func() { findExecutable, commandOutput = oldFind, oldOutput })

	findExecutable = func(name string) (string, error) { return "/usr/bin/" + name, nil }
	commandOutput = func(string, ...string) ([]byte, error) { return []byte("-1\n"), nil }

	if _, err := resolveLeader("intuneme"); err == nil {
		t.Fatal("expected invalid leader output to be rejected")
	}
}

// fakeProc builds a /proc-like tree with self/ns/user and pid/{ns/user,root}.
// When sameUserNS is true the target points at the same user namespace file;
// otherwise it has its own.
func fakeProc(t *testing.T, targetRoot string, sameUserNS bool) string {
	t.Helper()
	proc := t.TempDir()
	selfUser := filepath.Join(proc, "self", "ns", "user")
	writeFile(t, selfUser, "user-ns")
	if err := os.MkdirAll(filepath.Join(proc, "42", "ns"), 0755); err != nil {
		t.Fatal(err)
	}
	targetUser := filepath.Join(proc, "42", "ns", "user")
	if sameUserNS {
		if err := os.Symlink(selfUser, targetUser); err != nil {
			t.Fatal(err)
		}
	} else {
		writeFile(t, targetUser, "other-user-ns")
	}
	if err := os.Symlink(targetRoot, filepath.Join(proc, "42", "root")); err != nil {
		t.Fatal(err)
	}
	return proc
}

func TestValidateTargetPaths(t *testing.T) {
	cfg := testConfig(t)
	otherRoot := t.TempDir()
	proc := fakeProc(t, cfg.Rootfs, true)

	if err := validateTargetPaths(proc, 42, cfg); err != nil {
		t.Fatalf("valid target rejected: %v", err)
	}

	cfg.Rootfs = otherRoot
	if err := validateTargetPaths(proc, 42, cfg); err == nil {
		t.Error("expected mismatched rootfs to be rejected")
	}
}

func TestValidateTargetPathsRejectsUserNamespace(t *testing.T) {
	cfg := testConfig(t)
	proc := fakeProc(t, cfg.Rootfs, false)

	if err := validateTargetPaths(proc, 42, cfg); err == nil {
		t.Error("expected different user namespace to be rejected")
	}
}

func TestCredentialsFromRootfsIgnoresPasswdUID(t *testing.T) {
	cfg := testConfig(t)
	// The attacker has changed their passwd entry to uid 0. The helper must
	// still drop to cfg.UID (1000) and only use the passwd file for the GID.
	writeFile(t, filepath.Join(cfg.Rootfs, "etc", "passwd"), ""+
		"root:x:0:0:root:/root:/bin/bash\n"+
		"testuser:x:0:0:testuser:/home/testuser:/bin/bash\n")
	writeFile(t, filepath.Join(cfg.Rootfs, "etc", "group"), ""+
		"root:x:0:\n"+
		"testuser:x:1000:\n"+
		"render:x:992:testuser\n"+
		"plugdev:x:46:testuser\n")

	cred, err := credentialsFromRootfs(cfg)
	if err != nil {
		t.Fatalf("credentialsFromRootfs failed: %v", err)
	}
	got := buildCredential(cfg, cred)
	if got.Uid != 1000 {
		t.Errorf("credential uid = %d, want 1000", got.Uid)
	}
	if got.Gid != 1000 {
		t.Errorf("credential gid = %d, want 1000 (fallback)", got.Gid)
	}
	wantGroups := []uint32{46, 992, 1000}
	if len(got.Groups) != len(wantGroups) {
		t.Fatalf("groups = %v, want %v", got.Groups, wantGroups)
	}
	for i := range wantGroups {
		if got.Groups[i] != wantGroups[i] {
			t.Errorf("groups = %v, want %v", got.Groups, wantGroups)
			break
		}
	}
}

func TestCredentialsFromRootfsUsesMatchingUIDEntry(t *testing.T) {
	cfg := testConfig(t)
	writeFile(t, filepath.Join(cfg.Rootfs, "etc", "passwd"), ""+
		"root:x:0:0:root:/root:/bin/bash\n"+
		"testuser:x:1000:1001:testuser:/home/testuser:/bin/bash\n")
	writeFile(t, filepath.Join(cfg.Rootfs, "etc", "group"), ""+
		"testuser:x:1000:\n"+
		"render:x:992:testuser\n")

	cred, err := credentialsFromRootfs(cfg)
	if err != nil {
		t.Fatalf("credentialsFromRootfs failed: %v", err)
	}
	if cred.GID != 1001 {
		t.Errorf("gid = %d, want 1001 from passwd", cred.GID)
	}
}

func TestBuildEnvDoesNotInheritDangerousVariables(t *testing.T) {
	t.Setenv("LD_PRELOAD", "/tmp/evil.so")
	t.Setenv("BASH_ENV", "/tmp/evil.sh")
	cfg := testConfig(t)
	env := strings.Join(buildEnv(cfg), "\n")
	if strings.Contains(env, "LD_PRELOAD") || strings.Contains(env, "BASH_ENV") {
		t.Errorf("environment leaked dangerous variables:\n%s", env)
	}
	if !strings.Contains(env, "HOME="+cfg.Home) || !strings.Contains(env, "PATH="+containerPATH) {
		t.Errorf("environment missing required values:\n%s", env)
	}
}

func TestIsHelperName(t *testing.T) {
	if !IsHelperName("/usr/local/libexec/intuneme/nsenter-exec") {
		t.Error("expected helper basename to match")
	}
	if IsHelperName("/usr/bin/intuneme") {
		t.Error("CLI basename must not match")
	}
}
