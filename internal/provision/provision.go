package provision

import (
	_ "embed"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/frostyard/intuneme/internal/runner"
)

//go:embed intuneme-profile.sh
var intuneProfileScript []byte

func PullImage(r runner.Runner, image string) error {
	out, err := r.Run("podman", "pull", image)
	if err != nil {
		return fmt.Errorf("podman pull failed: %w\n%s", err, out)
	}
	return nil
}

func ExtractRootfs(r runner.Runner, image string, rootfsPath string) error {
	// Remove any leftover extract container from a previous failed run
	_, _ = r.Run("podman", "rm", "intuneme-extract")

	// Create temporary container
	out, err := r.Run("podman", "create", "--name", "intuneme-extract", image)
	if err != nil {
		return fmt.Errorf("podman create failed: %w\n%s", err, out)
	}

	// Export the container filesystem to a tar, then extract with sudo to
	// preserve correct UIDs. Rootless podman cp remaps through the user
	// namespace, making ALL files (including /etc/sudo.conf, /usr/bin/sudo)
	// owned by the calling user. podman export preserves container-internal
	// UIDs where root-owned files stay uid 0.
	tmpTar := filepath.Join(os.TempDir(), "intuneme-rootfs.tar")
	out, err = r.Run("podman", "export", "-o", tmpTar, "intuneme-extract")
	if err != nil {
		_, _ = r.Run("podman", "rm", "intuneme-extract")
		return fmt.Errorf("podman export failed: %w\n%s", err, out)
	}
	defer func() { _ = os.Remove(tmpTar) }()

	if err := os.MkdirAll(rootfsPath, 0755); err != nil {
		_, _ = r.Run("podman", "rm", "intuneme-extract")
		return fmt.Errorf("mkdir rootfs: %w", err)
	}

	// RunAttached so sudo can prompt for password if needed (this is the
	// first sudo command in the init flow).
	if err := r.RunAttached("sudo", "tar", "-xf", tmpTar, "-C", rootfsPath); err != nil {
		_, _ = r.Run("podman", "rm", "intuneme-extract")
		return fmt.Errorf("extract rootfs failed: %w", err)
	}

	// Remove temporary container
	out, err = r.Run("podman", "rm", "intuneme-extract")
	if err != nil {
		return fmt.Errorf("podman rm failed: %w\n%s", err, out)
	}

	return nil
}

// sudoWriteFile writes data to path via a temp file + sudo install.
func sudoWriteFile(r runner.Runner, path string, data []byte, perm os.FileMode) error {
	tmp, err := os.CreateTemp("", "intuneme-fixup-*")
	if err != nil {
		return err
	}
	defer func() { _ = os.Remove(tmp.Name()) }()
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	_ = tmp.Close()
	_, err = r.Run("sudo", "install", "-m", fmt.Sprintf("%04o", perm), tmp.Name(), path)
	return err
}

// sudoMkdirAll creates directories with sudo.
func sudoMkdirAll(r runner.Runner, path string) error {
	_, err := r.Run("sudo", "mkdir", "-p", path)
	return err
}

// sudoSymlink creates a symlink with sudo, removing any existing link first.
func sudoSymlink(r runner.Runner, target, link string) error {
	_, err := r.Run("sudo", "ln", "-sf", target, link)
	return err
}

func WriteFixups(r runner.Runner, rootfsPath, user string, uid, gid int, hostname string) error {
	// /etc/hostname
	if err := sudoWriteFile(r,
		filepath.Join(rootfsPath, "etc", "hostname"),
		[]byte(hostname+"\n"), 0644,
	); err != nil {
		return fmt.Errorf("write hostname: %w", err)
	}

	// /etc/hosts
	hosts := fmt.Sprintf("127.0.0.1 %s localhost\n", hostname)
	if err := sudoWriteFile(r,
		filepath.Join(rootfsPath, "etc", "hosts"),
		[]byte(hosts), 0644,
	); err != nil {
		return fmt.Errorf("write hosts: %w", err)
	}

	// /etc/environment
	env := "DISPLAY=:0\nNO_AT_BRIDGE=1\nGTK_A11Y=none\n"
	if err := sudoWriteFile(r,
		filepath.Join(rootfsPath, "etc", "environment"),
		[]byte(env), 0644,
	); err != nil {
		return fmt.Errorf("write environment: %w", err)
	}

	// Password quality — two files needed (matching mkosi reference):
	// 1. /usr/share/pam-configs/pwquality — PAM profile with inline params so
	//    pam-auth-update generates the correct common-password line
	// 2. /etc/security/pwquality.conf — config file that pam_pwquality.so and
	//    the intune compliance agent both read for the actual policy values
	pwqualityProfile := `Name: Pwquality password strength checking
Default: yes
Priority: 1024
Conflicts: cracklib
Password-Type: Primary
Password:
	requisite			pam_pwquality.so retry=3 dcredit=-1 ocredit=-1 ucredit=-1 lcredit=-1 minlen=12
Password-Initial:
	requisite			pam_pwquality.so retry=3 dcredit=-1 ocredit=-1 ucredit=-1 lcredit=-1 minlen=12
`
	pamConfigsDir := filepath.Join(rootfsPath, "usr", "share", "pam-configs")
	if err := sudoMkdirAll(r, pamConfigsDir); err != nil {
		return fmt.Errorf("mkdir pam-configs: %w", err)
	}
	if err := sudoWriteFile(r, filepath.Join(pamConfigsDir, "pwquality"), []byte(pwqualityProfile), 0644); err != nil {
		return fmt.Errorf("write pam-configs/pwquality: %w", err)
	}

	pwqualityConf := `# Intune compliance password policy
minlen = 12
dcredit = -1
ucredit = -1
lcredit = -1
ocredit = -1
enforcing = 1
retry = 3
dictcheck = 1
usercheck = 1
`
	pwqDir := filepath.Join(rootfsPath, "etc", "security")
	if err := sudoMkdirAll(r, pwqDir); err != nil {
		return fmt.Errorf("mkdir security: %w", err)
	}
	if err := sudoWriteFile(r, filepath.Join(pwqDir, "pwquality.conf"), []byte(pwqualityConf), 0644); err != nil {
		return fmt.Errorf("write pwquality.conf: %w", err)
	}

	// Override intune-agent timer to activate on default.target instead of
	// graphical-session.target (which is never reached in a headless nspawn container).
	// Without this, the agent never runs and never reports compliance status.
	agentTimerOverride := filepath.Join(rootfsPath, "etc", "systemd", "user", "intune-agent.timer.d")
	if err := sudoMkdirAll(r, agentTimerOverride); err != nil {
		return fmt.Errorf("mkdir intune-agent timer override: %w", err)
	}
	if err := sudoWriteFile(r, filepath.Join(agentTimerOverride, "override.conf"), []byte(`[Unit]
PartOf=default.target
After=default.target

[Install]
WantedBy=default.target
`), 0644); err != nil {
		return fmt.Errorf("write intune-agent timer override: %w", err)
	}
	// Enable the timer for default.target
	userWantsDir := filepath.Join(rootfsPath, "etc", "systemd", "user", "default.target.wants")
	if err := sudoMkdirAll(r, userWantsDir); err != nil {
		return fmt.Errorf("mkdir user wants dir: %w", err)
	}
	if err := sudoSymlink(r, "/usr/lib/systemd/user/intune-agent.timer", filepath.Join(userWantsDir, "intune-agent.timer")); err != nil {
		return fmt.Errorf("symlink intune-agent.timer: %w", err)
	}

	// fix-home-ownership.service
	svc := fmt.Sprintf(`[Unit]
Description=Fix home directory ownership
ConditionPathExists=!/var/lib/fix-home-ownership-done

[Service]
Type=oneshot
ExecStart=/bin/chown -R %d:%d /home/%s
ExecStartPost=/bin/touch /var/lib/fix-home-ownership-done
RemainAfterExit=yes

[Install]
WantedBy=multi-user.target
`, uid, gid, user)

	svcPath := filepath.Join(rootfsPath, "etc", "systemd", "system", "fix-home-ownership.service")
	if err := sudoWriteFile(r, svcPath, []byte(svc), 0644); err != nil {
		return fmt.Errorf("write fix-home-ownership.service: %w", err)
	}

	// Enable the service (symlink)
	wantsDir := filepath.Join(rootfsPath, "etc", "systemd", "system", "multi-user.target.wants")
	if err := sudoMkdirAll(r, wantsDir); err != nil {
		return fmt.Errorf("mkdir multi-user wants dir: %w", err)
	}
	if err := sudoSymlink(r, svcPath, filepath.Join(wantsDir, "fix-home-ownership.service")); err != nil {
		return fmt.Errorf("symlink fix-home-ownership.service: %w", err)
	}

	// Enable device broker — unit is "static" so systemctl enable is a no-op;
	// create the symlink directly.
	if err := sudoSymlink(r,
		"/usr/lib/systemd/system/microsoft-identity-device-broker.service",
		filepath.Join(wantsDir, "microsoft-identity-device-broker.service"),
	); err != nil {
		return fmt.Errorf("symlink microsoft-identity-device-broker.service: %w", err)
	}

	// /usr/local/bin/microsoft-edge — wrapper that enables Wayland/Ozone on Wayland sessions
	edgeWrapper := `#!/bin/sh -e

if [ -n "$WAYLAND_DISPLAY" ]
then
    set -- \
        '--enable-features=UseOzonePlatform' \
        '--enable-features=WebRTCPipeWireCapturer' \
        '--ozone-platform=wayland' \
        "$@"
fi

/usr/bin/microsoft-edge "$@"
`
	edgePath := filepath.Join(rootfsPath, "usr", "local", "bin", "microsoft-edge")
	if err := sudoMkdirAll(r, filepath.Dir(edgePath)); err != nil {
		return fmt.Errorf("mkdir edge wrapper dir: %w", err)
	}
	if err := sudoWriteFile(r, edgePath, []byte(edgeWrapper), 0755); err != nil {
		return fmt.Errorf("write microsoft-edge wrapper: %w", err)
	}

	// Install profile.d/intuneme.sh — sets display/audio env on login
	profileDir := filepath.Join(rootfsPath, "etc", "profile.d")
	if err := sudoMkdirAll(r, profileDir); err != nil {
		return fmt.Errorf("mkdir profile.d: %w", err)
	}
	if err := sudoWriteFile(r, filepath.Join(profileDir, "intuneme.sh"), intuneProfileScript, 0755); err != nil {
		return fmt.Errorf("write profile.d/intuneme.sh: %w", err)
	}

	// Passwordless sudo for the container user
	sudoersDir := filepath.Join(rootfsPath, "etc", "sudoers.d")
	if err := sudoMkdirAll(r, sudoersDir); err != nil {
		return fmt.Errorf("mkdir sudoers.d: %w", err)
	}
	sudoersRule := fmt.Sprintf("%s ALL=(ALL) NOPASSWD: ALL\n", user)
	if err := sudoWriteFile(r, filepath.Join(sudoersDir, "intuneme"), []byte(sudoersRule), 0440); err != nil {
		return fmt.Errorf("write sudoers.d/intuneme: %w", err)
	}

	// Broker display override — broker starts before login, needs DISPLAY
	brokerOverrideDir := filepath.Join(rootfsPath, "usr", "lib", "systemd", "user",
		"microsoft-identity-broker.service.d")
	if err := sudoMkdirAll(r, brokerOverrideDir); err != nil {
		return fmt.Errorf("mkdir broker override dir: %w", err)
	}
	if err := sudoWriteFile(r, filepath.Join(brokerOverrideDir, "display.conf"),
		[]byte("[Service]\nEnvironment=\"DISPLAY=:0\"\n"), 0644); err != nil {
		return fmt.Errorf("write broker display override: %w", err)
	}

	return nil
}

// InstallPackages installs additional packages inside the container rootfs.
// The frostyard OCI image includes intune-portal and the identity broker but
// not Microsoft Edge or libsecret-tools, which we need for SSO and keyring init.
// Packages already present in the image are skipped.
func InstallPackages(r runner.Runner, rootfsPath string) error {
	packages := []string{"microsoft-edge-stable", "libsecret-tools", "sudo", "libpulse0"}

	// Check which packages are already installed
	var missing []string
	for _, pkg := range packages {
		_, err := r.Run("sudo", "systemd-nspawn", "--console=pipe", "-D", rootfsPath,
			"dpkg", "-s", pkg)
		if err != nil {
			missing = append(missing, pkg)
		}
	}

	if len(missing) == 0 {
		fmt.Println("  All packages already installed, skipping.")
		return nil
	}

	fmt.Printf("  Installing packages: %s\n", strings.Join(missing, ", "))

	// Add the Edge apt repo (uses same Microsoft GPG key already in the image)
	edgeRepo := "deb [arch=amd64 signed-by=/etc/apt/keyrings/microsoft-edge.gpg] https://packages.microsoft.com/repos/edge stable main\n"
	edgeListPath := filepath.Join(rootfsPath, "etc", "apt", "sources.list.d", "microsoft-edge.list")
	if err := sudoMkdirAll(r, filepath.Dir(edgeListPath)); err != nil {
		return fmt.Errorf("mkdir edge apt sources dir: %w", err)
	}
	if err := sudoWriteFile(r, edgeListPath, []byte(edgeRepo), 0644); err != nil {
		return fmt.Errorf("write edge repo: %w", err)
	}

	// Download the Edge GPG key on the host if not already present.
	// The frostyard image doesn't have curl/gpg so we fetch from the host side.
	keyPath := filepath.Join(rootfsPath, "etc", "apt", "keyrings", "microsoft-edge.gpg")
	if _, err := os.Stat(keyPath); os.IsNotExist(err) {
		if err := sudoMkdirAll(r, filepath.Dir(keyPath)); err != nil {
			return fmt.Errorf("mkdir keyrings dir: %w", err)
		}
		tmpKey, err := os.CreateTemp("", "intuneme-gpg-*")
		if err != nil {
			return fmt.Errorf("create temp gpg file: %w", err)
		}
		defer func() { _ = os.Remove(tmpKey.Name()) }()
		_ = tmpKey.Close()
		out, err := r.Run("bash", "-c",
			fmt.Sprintf("curl -fsSL https://packages.microsoft.com/keys/microsoft.asc | gpg --dearmor -o %s", tmpKey.Name()))
		if err != nil {
			return fmt.Errorf("download edge GPG key: %w\n%s", err, out)
		}
		if _, err := r.Run("sudo", "cp", tmpKey.Name(), keyPath); err != nil {
			return fmt.Errorf("install edge GPG key: %w", err)
		}
	}

	installCmd := "apt-get update && apt-get install -y " + strings.Join(missing, " ")
	return r.RunAttached("sudo", "systemd-nspawn", "--console=pipe", "-D", rootfsPath,
		"bash", "-c", installCmd,
	)
}

// EnableServices enables systemd services inside the container rootfs.
// The system device broker must be enabled so it starts on boot.
func EnableServices(r runner.Runner, rootfsPath string) error {
	return r.RunAttached("sudo", "systemd-nspawn", "--console=pipe", "-D", rootfsPath,
		"systemctl", "enable", "microsoft-identity-device-broker",
	)
}

// ConfigurePAM runs pam-auth-update inside the container to properly configure PAM
// modules for password quality, gnome-keyring, and intune compliance.
// This must run inside the container (via systemd-nspawn) because pam-auth-update
// regenerates /etc/pam.d/common-* files from profiles in /usr/share/pam-configs/.
func ConfigurePAM(r runner.Runner, rootfsPath string) error {
	return r.RunAttached("sudo", "systemd-nspawn", "--console=pipe", "-D", rootfsPath,
		"pam-auth-update", "--enable", "pwquality", "mkhomedir", "gnome-keyring", "intune", "unix", "--force",
	)
}

// SetContainerPassword sets the user's password inside the container via chpasswd.
// Without a password, the account is locked and machinectl shell/login won't work interactively.
func SetContainerPassword(r runner.Runner, rootfsPath, user, password string) error {
	return r.RunAttached("sudo", "systemd-nspawn", "--console=pipe", "-D", rootfsPath,
		"bash", "-c", fmt.Sprintf("echo '%s:%s' | chpasswd", user, password),
	)
}

// CreateContainerUser ensures a user with the matching UID exists inside the rootfs.
// If a user with the target UID already exists (e.g., "ubuntu" from the OCI image),
// it is renamed and reconfigured. Otherwise a new user is created.
func CreateContainerUser(r runner.Runner, rootfsPath, user string, uid, gid int) error {
	// Check if a user with this UID already exists in the rootfs passwd
	passwdPath := filepath.Join(rootfsPath, "etc", "passwd")
	existingUser, err := findUserByUID(passwdPath, uid)
	if err != nil {
		return fmt.Errorf("check existing users: %w", err)
	}

	if existingUser != "" && existingUser != user {
		// Rename the existing user and fix up their home directory
		fmt.Printf("  Renaming existing user %q to %q...\n", existingUser, user)
		if err := r.RunAttached("sudo", "systemd-nspawn", "--console=pipe", "-D", rootfsPath,
			"usermod", "--login", user, "--home", fmt.Sprintf("/home/%s", user), "--move-home", existingUser,
		); err != nil {
			return fmt.Errorf("usermod (rename) failed: %w", err)
		}
		// Ensure correct groups
		if err := r.RunAttached("sudo", "systemd-nspawn", "--console=pipe", "-D", rootfsPath,
			"usermod", "--groups", "adm,sudo,video,audio", "--append", user,
		); err != nil {
			return fmt.Errorf("usermod (groups) failed: %w", err)
		}
	} else if existingUser == "" {
		// No user with this UID — create one
		if err := r.RunAttached("sudo", "systemd-nspawn", "--console=pipe", "-D", rootfsPath,
			"useradd",
			"--uid", fmt.Sprintf("%d", uid),
			"--create-home",
			"--shell", "/bin/bash",
			"--groups", "adm,sudo,video,audio",
			user,
		); err != nil {
			return fmt.Errorf("useradd in container failed: %w", err)
		}
	} else {
		// User already exists with the right name — just ensure groups
		if err := r.RunAttached("sudo", "systemd-nspawn", "--console=pipe", "-D", rootfsPath,
			"usermod", "--groups", "adm,sudo,video,audio", "--append", user,
		); err != nil {
			return fmt.Errorf("usermod (groups) failed: %w", err)
		}
	}
	return nil
}

// findUserByUID reads a passwd file and returns the username for a given UID, or "" if not found.
func findUserByUID(passwdPath string, uid int) (string, error) {
	data, err := os.ReadFile(passwdPath)
	if err != nil {
		return "", err
	}
	uidStr := fmt.Sprintf("%d", uid)
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Split(line, ":")
		if len(fields) >= 3 && fields[2] == uidStr {
			return fields[0], nil
		}
	}
	return "", nil
}

// InstallPolkitRule installs the polkit rule on the host using sudo.
func InstallPolkitRule(r runner.Runner, rulesDir string) error {
	rule := `polkit.addRule(function(action, subject) {
    if ((action.id == "org.freedesktop.machine1.manage-machines" ||
         action.id == "org.freedesktop.machine1.manage-images" ||
         action.id == "org.freedesktop.machine1.login" ||
         action.id == "org.freedesktop.machine1.shell" ||
         action.id == "org.freedesktop.machine1.host-shell") &&
        subject.isInGroup("sudo")) {
        return polkit.Result.YES;
    }
});
`
	// Write rule to a temp file, then sudo cp it into place
	tmpFile, err := os.CreateTemp("", "intuneme-polkit-*.rules")
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}
	defer func() { _ = os.Remove(tmpFile.Name()) }()

	if _, err := tmpFile.WriteString(rule); err != nil {
		_ = tmpFile.Close()
		return fmt.Errorf("write temp file: %w", err)
	}
	if err := tmpFile.Close(); err != nil {
		return fmt.Errorf("close temp file: %w", err)
	}

	// Create directory with sudo
	if err := r.RunAttached("sudo", "mkdir", "-p", rulesDir); err != nil {
		return fmt.Errorf("create polkit rules dir: %w", err)
	}

	// Copy temp file into place with sudo
	dest := filepath.Join(rulesDir, "50-intuneme.rules")
	if err := r.RunAttached("sudo", "cp", tmpFile.Name(), dest); err != nil {
		return fmt.Errorf("install polkit rule failed: %w", err)
	}
	return nil
}
