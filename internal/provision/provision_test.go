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
	files    map[string]string
}

func (m *mockRunner) Run(name string, args ...string) ([]byte, error) {
	m.commands = append(m.commands, name+" "+strings.Join(args, " "))
	if name == "sudo" && len(args) >= 4 && args[0] == "install" && m.files != nil {
		source, dest := args[len(args)-2], args[len(args)-1]
		if data, err := os.ReadFile(source); err == nil {
			m.files[dest] = string(data)
		}
	}
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

func TestInstallHimmelblauPAMAllowlist(t *testing.T) {
	rootfs := t.TempDir()
	r := &mockRunner{files: map[string]string{}}
	if err := InstallHimmelblauPAMAllowlist(r, rootfs, "alice", false); err != nil {
		t.Fatalf("InstallHimmelblauPAMAllowlist() error: %v", err)
	}

	path := filepath.Join(rootfs, "etc", "intuneme-machine-shell-allow")
	if got, want := r.files[path], "root\nalice\n"; got != want {
		t.Fatalf("PAM allowlist = %q, want %q", got, want)
	}
	if err := InstallHimmelblauPAMAllowlist(r, rootfs, "alice", true); err != nil {
		t.Fatalf("InstallHimmelblauPAMAllowlist(mapped) error: %v", err)
	}
	if got, want := r.files[path], "root\n"; got != want {
		t.Fatalf("mapped PAM allowlist = %q, want %q", got, want)
	}
}

func TestVerifyHimmelblauMappedPAMAllowlist(t *testing.T) {
	tests := []struct {
		name    string
		content string
		wantErr bool
	}{
		{name: "only root allowed", content: "root\n"},
		{name: "local account still allowed", content: "root\nalice\n", wantErr: true},
		{name: "root missing", content: "alice\n", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rootfs := t.TempDir()
			path := filepath.Join(rootfs, himmelblauPAMAllowlistPath)
			if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, []byte(tt.content), 0644); err != nil {
				t.Fatal(err)
			}
			err := VerifyHimmelblauMappedPAMAllowlist(rootfs, "alice")
			if (err != nil) != tt.wantErr {
				t.Fatalf("VerifyHimmelblauMappedPAMAllowlist() error = %v, wantErr %t", err, tt.wantErr)
			}
		})
	}
}

func TestInitializeHimmelblauUserMap(t *testing.T) {
	rootfs := t.TempDir()
	r := &mockRunner{files: map[string]string{}}
	if err := InitializeHimmelblauUserMap(r, rootfs); err != nil {
		t.Fatalf("InitializeHimmelblauUserMap() error: %v", err)
	}

	path := filepath.Join(rootfs, "etc", "himmelblau", "user-map")
	if got := r.files[path]; got != "" {
		t.Fatalf("initial Himmelblau user map = %q, want empty", got)
	}
}

func TestEnsureHimmelblauUserMap(t *testing.T) {
	rootfs := t.TempDir()
	r := &mockRunner{files: map[string]string{}}
	if err := EnsureHimmelblauUserMap(r, rootfs); err != nil {
		t.Fatalf("EnsureHimmelblauUserMap() error: %v", err)
	}

	path := filepath.Join(rootfs, "etc", "himmelblau", "user-map")
	want := "sudo touch " + path
	if got := r.commands[len(r.commands)-1]; got != want {
		t.Fatalf("last command = %q, want %q", got, want)
	}
}

func TestInstallHimmelblauUserMap(t *testing.T) {
	rootfs := t.TempDir()
	r := &mockRunner{files: map[string]string{}}
	if err := InstallHimmelblauUserMap(r, rootfs, "alice", "alice@example.com"); err != nil {
		t.Fatalf("InstallHimmelblauUserMap() error: %v", err)
	}

	path := filepath.Join(rootfs, "etc", "himmelblau", "user-map")
	if got, want := r.files[path], "alice:alice@example.com\n"; got != want {
		t.Fatalf("Himmelblau user map = %q, want %q", got, want)
	}
}

func TestRebootHimmelblauContainer(t *testing.T) {
	r := &mockRunner{outputs: [][]byte{
		[]byte("100\n"),
		nil,
		[]byte("200\n"),
		[]byte("INTUNEME_HIMMELBLAU_READY\n"),
	}}
	if err := RebootHimmelblauContainer(r, "intuneme"); err != nil {
		t.Fatalf("RebootHimmelblauContainer() error: %v", err)
	}

	want := []string{
		"machinectl show intuneme -p Leader --value",
		"machinectl reboot intuneme",
		"machinectl show intuneme -p Leader --value",
		"machinectl shell root@intuneme /bin/sh -c " +
			"attempt=0\nwhile [ \"$attempt\" -lt 30 ]; do\n" +
			"  if /bin/systemctl is-active --quiet himmelblaud.service &&\n" +
			"     /bin/systemctl is-active --quiet himmelblaud-tasks.service &&\n" +
			"     /bin/systemctl is-active --quiet himmelblaud.socket &&\n" +
			"     /bin/systemctl is-active --quiet himmelblaud-tasks.socket &&\n" +
			"     /bin/systemctl is-active --quiet himmelblaud-broker.socket; then\n" +
			"    echo INTUNEME_HIMMELBLAU_READY\n" +
			"    exit 0\n" +
			"  fi\n" +
			"  sleep 1\n" +
			"  attempt=$((attempt + 1))\n" +
			"done\necho \"Himmelblau services did not become active\" >&2\nexit 1",
	}
	if len(r.commands) != len(want) {
		t.Fatalf("commands = %q, want %q", r.commands, want)
	}
	for i := range want {
		if r.commands[i] != want[i] {
			t.Errorf("command %d = %q, want %q", i, r.commands[i], want[i])
		}
	}
}

func TestRebootHimmelblauContainerRequiresReadyMarker(t *testing.T) {
	r := &mockRunner{outputs: [][]byte{
		[]byte("100\n"),
		nil,
		[]byte("200\n"),
		[]byte("Connection to machine intuneme terminated.\n"),
	}}
	err := RebootHimmelblauContainer(r, "intuneme")
	if err == nil || !strings.Contains(err.Error(), "services") {
		t.Fatalf("RebootHimmelblauContainer() error = %v, want readiness error", err)
	}
}

func TestConfigureHimmelblauDomain(t *testing.T) {
	rootfs := t.TempDir()
	defaultPath := filepath.Join(rootfs, "usr", "lib", "himmelblau", "himmelblau.conf")
	if err := os.MkdirAll(filepath.Dir(defaultPath), 0755); err != nil {
		t.Fatal(err)
	}
	defaults := `# Packaged Himmelblau defaults and documentation.
[global]

# Primary Entra ID domain used for user sign-in.
# domain = example.com

# Map short usernames to UPNs in the primary domain.
# cn_name_mapping = true

# Local account map used by intuneme.
# user_map_file = /var/lib/himmelblau/user-map

# Enable passwordless Bluetooth QR-code sign-in.
# enable_passwordless_qr_bluetooth = false

[service]
# Other package documentation remains available.
`
	if err := os.WriteFile(defaultPath, []byte(defaults), 0644); err != nil {
		t.Fatal(err)
	}
	r := &mockRunner{files: map[string]string{}}
	if err := ConfigureHimmelblauDomain(r, rootfs, "Example.COM"); err != nil {
		t.Fatalf("ConfigureHimmelblauDomain() error: %v", err)
	}

	path := filepath.Join(rootfs, "etc", "himmelblau", "himmelblau.conf")
	got := r.files[path]
	for _, want := range []string{
		"# Packaged Himmelblau defaults and documentation.",
		"# Primary Entra ID domain used for user sign-in.",
		"domain = example.com",
		"apply_policy = true",
		"# Map short usernames to UPNs in the primary domain.",
		"cn_name_mapping = false",
		"user_map_file = /etc/himmelblau/user-map",
		"enable_passwordless_qr_bluetooth = true",
		"[service]",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("Himmelblau config missing %q:\n%s", want, got)
		}
	}
	if err := ValidateHimmelblauConfig([]byte(got), "example.com"); err != nil {
		t.Fatalf("ValidateHimmelblauConfig() error: %v", err)
	}
}

func TestConfigureHimmelblauDomainOverridesPackagedDefaults(t *testing.T) {
	rootfs := t.TempDir()
	defaultPath := filepath.Join(rootfs, "usr", "lib", "himmelblau", "himmelblau.conf")
	if err := os.MkdirAll(filepath.Dir(defaultPath), 0755); err != nil {
		t.Fatal(err)
	}
	defaults := `[global]
# domain = example.com
apply_policy = false
enable_passwordless_qr_bluetooth = false
# cn_name_mapping = true
# user_map_file = /var/lib/himmelblau/user-map
`
	if err := os.WriteFile(defaultPath, []byte(defaults), 0644); err != nil {
		t.Fatal(err)
	}
	r := &mockRunner{files: map[string]string{}}
	if err := ConfigureHimmelblauDomain(r, rootfs, "example.com"); err != nil {
		t.Fatalf("ConfigureHimmelblauDomain() error: %v", err)
	}
	got := r.files[filepath.Join(rootfs, "etc", "himmelblau", "himmelblau.conf")]
	for _, want := range []string{"apply_policy = true", "enable_passwordless_qr_bluetooth = true"} {
		if !strings.Contains(got, want) {
			t.Errorf("configured defaults did not override upstream %q:\n%s", want, got)
		}
	}
}

func TestInstallHimmelblauConfigDefaultsApplyPolicyUnlessDisabled(t *testing.T) {
	const requiredSettings = "[global]\ndomain = example.com\ncn_name_mapping = false\nuser_map_file = /etc/himmelblau/user-map\n"
	tests := []struct {
		name    string
		content string
		want    string
	}{
		{name: "missing setting defaults on", content: requiredSettings, want: "apply_policy = true"},
		{name: "commented setting defaults on", content: requiredSettings + "# apply_policy = false\n", want: "apply_policy = true"},
		{name: "explicit opt-out is preserved", content: requiredSettings + "apply_policy = false\n", want: "apply_policy = false"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rootfs := t.TempDir()
			r := &mockRunner{files: map[string]string{}}
			if err := InstallHimmelblauConfig(r, rootfs, []byte(tt.content), "example.com"); err != nil {
				t.Fatalf("InstallHimmelblauConfig() error: %v", err)
			}
			path := filepath.Join(rootfs, "etc", "himmelblau", "himmelblau.conf")
			got := r.files[path]
			if !strings.Contains(got, tt.want) {
				t.Fatalf("installed config missing %q:\n%s", tt.want, got)
			}
			if tt.name == "explicit opt-out is preserved" && strings.Contains(got, "apply_policy = true") {
				t.Fatalf("installed config overwrote explicit opt-out:\n%s", got)
			}
		})
	}
}

func TestInstallHimmelblauConfigDefaultsPasswordlessQRBluetoothUnlessDisabled(t *testing.T) {
	const requiredSettings = "[global]\ndomain = example.com\ncn_name_mapping = false\nuser_map_file = /etc/himmelblau/user-map\n"
	tests := []struct {
		name    string
		content string
		want    string
	}{
		{name: "missing setting defaults on", content: requiredSettings, want: "enable_passwordless_qr_bluetooth = true"},
		{name: "commented setting defaults on", content: requiredSettings + "# enable_passwordless_qr_bluetooth = false\n", want: "enable_passwordless_qr_bluetooth = true"},
		{name: "explicit opt-out is preserved", content: requiredSettings + "enable_passwordless_qr_bluetooth = false\n", want: "enable_passwordless_qr_bluetooth = false"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rootfs := t.TempDir()
			r := &mockRunner{files: map[string]string{}}
			if err := InstallHimmelblauConfig(r, rootfs, []byte(tt.content), "example.com"); err != nil {
				t.Fatalf("InstallHimmelblauConfig() error: %v", err)
			}
			path := filepath.Join(rootfs, "etc", "himmelblau", "himmelblau.conf")
			got := r.files[path]
			if !strings.Contains(got, tt.want) {
				t.Fatalf("installed config missing %q:\n%s", tt.want, got)
			}
			if tt.name == "explicit opt-out is preserved" && strings.Contains(got, "enable_passwordless_qr_bluetooth = true") {
				t.Fatalf("installed config overwrote explicit opt-out:\n%s", got)
			}
		})
	}
}

func TestConfigureHimmelblauDomainAcceptsPackagedBareStringDefaults(t *testing.T) {
	rootfs := t.TempDir()
	defaultPath := filepath.Join(rootfs, "usr", "lib", "himmelblau", "himmelblau.conf")
	if err := os.MkdirAll(filepath.Dir(defaultPath), 0755); err != nil {
		t.Fatal(err)
	}
	defaults := `[global]
# Primary Entra ID domain used for user sign-in.
# domain = example.com
local_groups = users
home_attr = CN
home_alias = CN
use_etc_skel = true
# cn_name_mapping = true
# user_map_file = /var/lib/himmelblau/user-map
`
	if err := os.WriteFile(defaultPath, []byte(defaults), 0644); err != nil {
		t.Fatal(err)
	}
	r := &mockRunner{files: map[string]string{}}

	if err := ConfigureHimmelblauDomain(r, rootfs, "example.com"); err != nil {
		t.Fatalf("ConfigureHimmelblauDomain() rejected packaged config syntax: %v", err)
	}
	got := r.files[filepath.Join(rootfs, "etc", "himmelblau", "himmelblau.conf")]
	for _, want := range []string{
		"local_groups = users",
		"home_attr = CN",
		"home_alias = CN",
		"use_etc_skel = true",
		"domain = example.com",
		"user_map_file = /etc/himmelblau/user-map",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("configured Himmelblau file missing %q:\n%s", want, got)
		}
	}
}

func TestVerifyHimmelblauPAMTesterRequiresExecutable(t *testing.T) {
	rootfs := t.TempDir()
	if err := os.MkdirAll(filepath.Join(rootfs, "usr", "bin"), 0755); err != nil {
		t.Fatal(err)
	}

	if err := VerifyHimmelblauPAMTester(rootfs); err == nil || !strings.Contains(err.Error(), "pamtester") {
		t.Fatalf("VerifyHimmelblauPAMTester() error = %v, want missing helper error", err)
	}

	path := filepath.Join(rootfs, "usr", "bin", "pamtester")
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := VerifyHimmelblauPAMTester(rootfs); err != nil {
		t.Fatalf("VerifyHimmelblauPAMTester() rejected executable: %v", err)
	}

	if err := os.Chmod(path, 0644); err != nil {
		t.Fatal(err)
	}
	if err := VerifyHimmelblauPAMTester(rootfs); err == nil || !strings.Contains(err.Error(), "executable") {
		t.Fatalf("VerifyHimmelblauPAMTester() error = %v, want non-executable helper error", err)
	}
}

func TestVerifyHimmelblauTerminalPasskeyRequiresPinnedImage(t *testing.T) {
	rootfs := t.TempDir()
	const source = "70002a71347d8b35a911e71a68facf2d2209370c"
	if err := VerifyHimmelblauTerminalPasskey(rootfs, source); err == nil || !strings.Contains(err.Error(), "fork marker") {
		t.Fatalf("VerifyHimmelblauTerminalPasskey() error = %v, want missing marker error", err)
	}

	path := filepath.Join(rootfs, "usr", "share", "intuneme", "himmelblau-terminal-passkey-source")
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(source+"\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := VerifyHimmelblauTerminalPasskey(rootfs, source); err != nil {
		t.Fatalf("VerifyHimmelblauTerminalPasskey() rejected pinned image: %v", err)
	}

	if err := os.WriteFile(path, []byte("1111111111111111111111111111111111111111\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := VerifyHimmelblauTerminalPasskey(rootfs, source); err == nil || !strings.Contains(err.Error(), "expected") {
		t.Fatalf("VerifyHimmelblauTerminalPasskey() error = %v, want source mismatch error", err)
	}
}

func TestReadHimmelblauTerminalPasskeySource(t *testing.T) {
	rootfs := t.TempDir()
	if source, found, err := ReadHimmelblauTerminalPasskeySource(rootfs); err != nil || found || source != "" {
		t.Fatalf("missing marker result = (%q, %v, %v), want empty/not-found/nil", source, found, err)
	}

	path := filepath.Join(rootfs, "usr", "share", "intuneme", "himmelblau-terminal-passkey-source")
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("70002a71347d8b35a911e71a68facf2d2209370c\n"), 0644); err != nil {
		t.Fatal(err)
	}
	source, found, err := ReadHimmelblauTerminalPasskeySource(rootfs)
	if err != nil || !found || source != "70002a71347d8b35a911e71a68facf2d2209370c" {
		t.Fatalf("valid marker result = (%q, %v, %v)", source, found, err)
	}

	if err := os.WriteFile(path, []byte("not-a-commit\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if _, _, err := ReadHimmelblauTerminalPasskeySource(rootfs); err == nil {
		t.Fatal("ReadHimmelblauTerminalPasskeySource() accepted an invalid marker")
	}
}

func TestHimmelblauPasswordlessQRBluetoothEnabled(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    bool
		wantErr bool
	}{
		{name: "missing option defaults on", content: "[global]\ndomain = example.com\n", want: true},
		{name: "commented option defaults on", content: "[global]\n# enable_passwordless_qr_bluetooth = false\n", want: true},
		{name: "explicitly disabled", content: "[global]\nenable_passwordless_qr_bluetooth = false\n"},
		{name: "enabled", content: "[global]\nenable_passwordless_qr_bluetooth = true\n", want: true},
		{name: "inline comment", content: "[global]\nenable_passwordless_qr_bluetooth = true # use phone\n", want: true},
		{name: "option outside global section defaults on", content: "[pam]\nenable_passwordless_qr_bluetooth = false\n[global]\ndomain = example.com\n", want: true},
		{name: "invalid value", content: "[global]\nenable_passwordless_qr_bluetooth = maybe\n", wantErr: true},
		{name: "missing global section", content: "[pam]\nenable_passwordless_qr_bluetooth = true\n", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rootfs := t.TempDir()
			path := filepath.Join(rootfs, "etc", "himmelblau", "himmelblau.conf")
			if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, []byte(tt.content), 0644); err != nil {
				t.Fatal(err)
			}
			got, err := HimmelblauPasswordlessQRBluetoothEnabled(rootfs)
			if (err != nil) != tt.wantErr {
				t.Fatalf("HimmelblauPasswordlessQRBluetoothEnabled() error = %v, wantErr %v", err, tt.wantErr)
			}
			if err == nil && got != tt.want {
				t.Fatalf("HimmelblauPasswordlessQRBluetoothEnabled() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestValidateHimmelblauConfigRequiresIntunemeSettings(t *testing.T) {
	tests := []struct {
		name    string
		content string
		wantErr bool
	}{
		{
			name:    "valid config with extra settings",
			content: "[global]\ndomain = example.com\ncn_name_mapping = false\nuser_map_file = /etc/himmelblau/user-map\nlocal_groups = users\nhome_attr = CN\nenable_passwordless_qr_bluetooth = true\n",
		},
		{
			name:    "wrong domain",
			content: "[global]\ndomain = other.example\ncn_name_mapping = false\nuser_map_file = /etc/himmelblau/user-map\n",
			wantErr: true,
		},
		{
			name:    "mapping mode enabled",
			content: "[global]\ndomain = example.com\ncn_name_mapping = true\nuser_map_file = /etc/himmelblau/user-map\n",
			wantErr: true,
		},
		{
			name:    "wrong user map",
			content: "[global]\ndomain = example.com\ncn_name_mapping = false\nuser_map_file = /tmp/user-map\n",
			wantErr: true,
		},
		{
			name:    "missing global section",
			content: "domain = example.com\ncn_name_mapping = false\nuser_map_file = /etc/himmelblau/user-map\n",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateHimmelblauConfig([]byte(tt.content), "example.com")
			if tt.wantErr && err == nil {
				t.Fatal("expected Himmelblau config validation error")
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("unexpected Himmelblau config validation error: %v", err)
			}
		})
	}
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

func TestInstallSessionScripts(t *testing.T) {
	r := &mockRunner{}
	if err := InstallSessionScripts(r, "/tmp/test-rootfs"); err != nil {
		t.Fatalf("InstallSessionScripts error: %v", err)
	}
	allCmds := strings.Join(r.commands, "\n")

	// Both the shared script (in usr/local/bin) and the profile.d entry point
	// must be written.
	for _, want := range []string{
		"usr/local/bin/intuneme-session-setup",
		"etc/profile.d/intuneme.sh",
	} {
		if !strings.Contains(allCmds, want) {
			t.Errorf("expected command referencing %q, not found in:\n%s", want, allCmds)
		}
	}
}

// TestSessionSetupScriptContent guards the two fixes that make the broker work
// on the non-login launch path: pushing env into the D-Bus activation
// environment, and unlocking the keyring with a real empty password.
func TestSessionSetupScriptContent(t *testing.T) {
	script := string(intuneSessionSetupScript)
	for _, want := range []string{
		"dbus-update-activation-environment", // broker inherits DISPLAY/XAUTHORITY
		"import-environment",
		`echo "" | gnome-keyring-daemon --unlock`, // empty pw via newline, not EOF
		"pkill -u", // reliable single-daemon unlock
	} {
		if !strings.Contains(script, want) {
			t.Errorf("session-setup script missing required snippet %q", want)
		}
	}
	// The broken restart of the (D-Bus-activated, not a unit) user broker must
	// not be reintroduced.
	if strings.Contains(script, "restart microsoft-identity-broker.service") {
		t.Error("session-setup must not restart the user identity broker (it is D-Bus-activated, not a systemd unit)")
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

func TestEnsureHimmelblauPrimaryGroupName(t *testing.T) {
	tests := []struct {
		name       string
		passwd     string
		group      string
		wantCmd    string
		wantErr    bool
		wantNoCmds bool
	}{
		{
			name:    "rename the group owning the user's primary GID",
			passwd:  "alice:x:1000:1000::/home/alice:/bin/bash\n",
			group:   "ubuntu:x:1000:\n",
			wantCmd: "groupmod --new-name alice ubuntu",
		},
		{
			name:    "preserve a primary GID different from the UID",
			passwd:  "alice:x:1000:1001::/home/alice:/bin/bash\n",
			group:   "ubuntu:x:1001:\n",
			wantCmd: "groupmod --new-name alice ubuntu",
		},
		{
			name:       "already matches",
			passwd:     "alice:x:1000:1000::/home/alice:/bin/bash\n",
			group:      "alice:x:1000:\n",
			wantNoCmds: true,
		},
		{
			name:    "create the missing primary group",
			passwd:  "alice:x:1000:1000::/home/alice:/bin/bash\n",
			group:   "ubuntu:x:1001:\n",
			wantCmd: "groupadd --gid 1000 alice",
		},
		{
			name:       "reject a conflicting group name",
			passwd:     "alice:x:1000:1000::/home/alice:/bin/bash\n",
			group:      "ubuntu:x:1000:\nalice:x:1001:\n",
			wantErr:    true,
			wantNoCmds: true,
		},
		{
			name:       "reject malformed primary GID",
			passwd:     "alice:x:1000:bad::/home/alice:/bin/bash\n",
			group:      "ubuntu:x:1000:\n",
			wantErr:    true,
			wantNoCmds: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rootfs := t.TempDir()
			etcDir := filepath.Join(rootfs, "etc")
			if err := os.MkdirAll(etcDir, 0755); err != nil {
				t.Fatalf("setup: %v", err)
			}
			if err := os.WriteFile(filepath.Join(etcDir, "passwd"), []byte(tt.passwd), 0644); err != nil {
				t.Fatalf("write passwd: %v", err)
			}
			if err := os.WriteFile(filepath.Join(etcDir, "group"), []byte(tt.group), 0644); err != nil {
				t.Fatalf("write group: %v", err)
			}

			r := &mockRunner{}
			err := EnsureHimmelblauPrimaryGroupName(r, rootfs, "alice")
			if (err != nil) != tt.wantErr {
				t.Fatalf("EnsureHimmelblauPrimaryGroupName() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantNoCmds {
				if len(r.commands) != 0 {
					t.Fatalf("unexpected commands: %v", r.commands)
				}
				return
			}
			if len(r.commands) != 1 || !strings.Contains(r.commands[0], tt.wantCmd) {
				t.Fatalf("commands = %v, want one command containing %q", r.commands, tt.wantCmd)
			}
		})
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

func TestRequiredRuntimeGroups_IncludesPlugdev(t *testing.T) {
	groups := requiredRuntimeGroups()
	found := false
	for _, g := range groups {
		if g == "plugdev" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("requiredRuntimeGroups() = %v, want it to include \"plugdev\"", groups)
	}
}

func TestEnsureUserGroups_SkipsWhenPresent(t *testing.T) {
	r := &mockRunner{
		outputs: [][]byte{
			[]byte("4242\n"),                       // machinectl show ... Leader (PID)
			[]byte("alice adm sudo video plugdev"), // id -nG alice
		},
	}

	added, err := EnsureUserGroups(r, "intuneme", "alice")
	if err != nil {
		t.Fatalf("EnsureUserGroups returned error: %v", err)
	}
	if len(added) != 0 {
		t.Errorf("added = %v, want empty slice", added)
	}
	for _, cmd := range r.commands {
		if strings.Contains(cmd, "usermod") {
			t.Errorf("usermod was invoked when not needed: %q", cmd)
		}
	}
}

func TestEnsureUserGroups_AddsWhenMissing(t *testing.T) {
	r := &mockRunner{
		outputs: [][]byte{
			[]byte("4242\n"),         // machinectl show ... Leader (PID)
			[]byte("alice adm sudo"), // id -nG alice (no plugdev)
		},
	}

	added, err := EnsureUserGroups(r, "intuneme", "alice")
	if err != nil {
		t.Fatalf("EnsureUserGroups returned error: %v", err)
	}
	if len(added) != 1 || added[0] != "plugdev" {
		t.Errorf("added = %v, want [plugdev]", added)
	}
	usermodCount := 0
	var usermodCmd string
	for _, cmd := range r.commands {
		if strings.Contains(cmd, "usermod") {
			usermodCount++
			usermodCmd = cmd
		}
	}
	if usermodCount != 1 {
		t.Errorf("usermod was invoked %d times, want exactly 1", usermodCount)
	}
	if !strings.Contains(usermodCmd, "usermod -aG plugdev alice") {
		t.Errorf("usermod cmd = %q, want it to include \"usermod -aG plugdev alice\"", usermodCmd)
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

func TestBaseGroupsContainsPlugdev(t *testing.T) {
	if !strings.Contains(baseGroups, "plugdev") {
		t.Errorf("baseGroups = %q, must contain \"plugdev\" so the container user can talk to pcscd (issue #146)", baseGroups)
	}
}

// TestSessionSetupPropagatesWebKitWorkaround guards the broker's activation
// environment. D-Bus-activated brokers need the same home and display values
// as the invoking identity, and WebKit's Wayland workaround must be imported
// explicitly rather than merely exported.
func TestSessionSetupPropagatesWebKitWorkaround(t *testing.T) {
	script := string(intuneSessionSetupScript)

	start := strings.Index(script, `_env_vars="`)
	if start < 0 {
		t.Fatal("session-setup script has no _env_vars assignment")
	}
	rest := script[start+len(`_env_vars="`):]
	end := strings.Index(rest, `"`)
	if end < 0 {
		t.Fatal("unterminated _env_vars assignment")
	}
	propagated := rest[:end]
	if !strings.Contains(propagated, "HOME") {
		t.Fatal("HOME must be propagated for the enrolled Himmelblau user's broker")
	}

	for _, v := range []string{"WEBKIT_DISABLE_DMABUF_RENDERER", "WEBKIT_DISABLE_COMPOSITING_MODE"} {
		if !strings.Contains(script, "export "+v+"=1") {
			t.Errorf("session-setup must export %s", v)
		}
		if !strings.Contains(propagated, v) {
			t.Errorf("%s is exported but not in _env_vars, so the D-Bus-activated broker never sees it", v)
		}
	}
}

func TestOverrideX11Tmpfiles(t *testing.T) {
	r := &mockRunner{}
	if err := OverrideX11Tmpfiles(r, "/tmp/test-rootfs"); err != nil {
		t.Fatalf("OverrideX11Tmpfiles error: %v", err)
	}
	allCmds := strings.Join(r.commands, "\n")
	if !strings.Contains(allCmds, "etc/tmpfiles.d/x11.conf") {
		t.Errorf("expected the override to be written, commands were:\n%s", allCmds)
	}
}

// TestX11TmpfilesOverrideOmitsX11Unix guards the whole point of the file: it
// replaces the base image's x11.conf and must not name /tmp/.X11-unix. A
// directive on that path would re-create the rule that wipes the host's X11
// socket, because the container shares that directory with the host.
func TestX11TmpfilesOverrideOmitsX11Unix(t *testing.T) {
	for _, line := range directives(string(x11TmpfilesOverride)) {
		if fields := strings.Fields(line); len(fields) > 1 && fields[1] == "/tmp/.X11-unix" {
			t.Errorf("override must not carry a /tmp/.X11-unix directive, found: %q", line)
		}
	}
}

// TestX11TmpfilesOverrideKeepsVendorDirectives checks the override is a copy of
// the vendor file rather than a mask of it. Those paths are container-private,
// so dropping them would break X11 session and input-method clients inside the
// container for no gain.
func TestX11TmpfilesOverrideKeepsVendorDirectives(t *testing.T) {
	got := directives(string(x11TmpfilesOverride))
	for _, want := range []string{"/tmp/.ICE-unix", "/tmp/.XIM-unix", "/tmp/.font-unix", "/tmp/.X[0-9]*-lock"} {
		found := false
		for _, line := range got {
			if fields := strings.Fields(line); len(fields) > 1 && fields[1] == want {
				found = true
			}
		}
		if !found {
			t.Errorf("override drops the vendor directive for %s", want)
		}
	}
}

// directives returns the active (non-blank, non-comment) lines of a
// tmpfiles.d fragment.
func directives(conf string) []string {
	var out []string
	for _, line := range strings.Split(conf, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		out = append(out, line)
	}
	return out
}
