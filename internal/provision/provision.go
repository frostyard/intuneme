package provision

import (
	_ "embed"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/bjk/intuneme/internal/runner"
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
	r.Run("podman", "rm", "intuneme-extract")

	// Create temporary container
	out, err := r.Run("podman", "create", "--name", "intuneme-extract", image)
	if err != nil {
		return fmt.Errorf("podman create failed: %w\n%s", err, out)
	}

	// Copy filesystem out
	out, err = r.Run("podman", "cp", "intuneme-extract:/", rootfsPath)
	if err != nil {
		// Clean up on failure
		r.Run("podman", "rm", "intuneme-extract")
		return fmt.Errorf("podman cp failed: %w\n%s", err, out)
	}

	// Remove temporary container
	out, err = r.Run("podman", "rm", "intuneme-extract")
	if err != nil {
		return fmt.Errorf("podman rm failed: %w\n%s", err, out)
	}

	return nil
}

func WriteFixups(rootfsPath, user string, uid, gid int, hostname string) error {
	// /etc/hostname
	if err := os.WriteFile(
		filepath.Join(rootfsPath, "etc", "hostname"),
		[]byte(hostname+"\n"), 0644,
	); err != nil {
		return fmt.Errorf("write hostname: %w", err)
	}

	// /etc/hosts
	hosts := fmt.Sprintf("127.0.0.1 %s localhost\n", hostname)
	if err := os.WriteFile(
		filepath.Join(rootfsPath, "etc", "hosts"),
		[]byte(hosts), 0644,
	); err != nil {
		return fmt.Errorf("write hosts: %w", err)
	}

	// /etc/environment
	env := "DISPLAY=:0\nNO_AT_BRIDGE=1\nGTK_A11Y=none\n"
	if err := os.WriteFile(
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
	os.MkdirAll(pamConfigsDir, 0755)
	if err := os.WriteFile(filepath.Join(pamConfigsDir, "pwquality"), []byte(pwqualityProfile), 0644); err != nil {
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
	os.MkdirAll(pwqDir, 0755)
	if err := os.WriteFile(filepath.Join(pwqDir, "pwquality.conf"), []byte(pwqualityConf), 0644); err != nil {
		return fmt.Errorf("write pwquality.conf: %w", err)
	}

	// Override intune-agent timer to activate on default.target instead of
	// graphical-session.target (which is never reached in a headless nspawn container).
	// Without this, the agent never runs and never reports compliance status.
	agentTimerOverride := filepath.Join(rootfsPath, "etc", "systemd", "user", "intune-agent.timer.d")
	os.MkdirAll(agentTimerOverride, 0755)
	if err := os.WriteFile(filepath.Join(agentTimerOverride, "override.conf"), []byte(`[Unit]
PartOf=default.target
After=default.target

[Install]
WantedBy=default.target
`), 0644); err != nil {
		return fmt.Errorf("write intune-agent timer override: %w", err)
	}
	// Enable the timer for default.target
	userWantsDir := filepath.Join(rootfsPath, "etc", "systemd", "user", "default.target.wants")
	os.MkdirAll(userWantsDir, 0755)
	os.Symlink("/usr/lib/systemd/user/intune-agent.timer", filepath.Join(userWantsDir, "intune-agent.timer"))

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
	if err := os.WriteFile(svcPath, []byte(svc), 0644); err != nil {
		return fmt.Errorf("write fix-home-ownership.service: %w", err)
	}

	// Enable the service (symlink)
	wantsDir := filepath.Join(rootfsPath, "etc", "systemd", "system", "multi-user.target.wants")
	os.MkdirAll(wantsDir, 0755)
	os.Symlink(svcPath, filepath.Join(wantsDir, "fix-home-ownership.service"))

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
	os.MkdirAll(filepath.Dir(edgePath), 0755)
	if err := os.WriteFile(edgePath, []byte(edgeWrapper), 0755); err != nil {
		return fmt.Errorf("write microsoft-edge wrapper: %w", err)
	}

	// Install profile.d/intuneme.sh — sets display/audio env on login
	profileDir := filepath.Join(rootfsPath, "etc", "profile.d")
	os.MkdirAll(profileDir, 0755)
	if err := os.WriteFile(filepath.Join(profileDir, "intuneme.sh"), intuneProfileScript, 0755); err != nil {
		return fmt.Errorf("write profile.d/intuneme.sh: %w", err)
	}

	// Broker display override — broker starts before login, needs DISPLAY
	brokerOverrideDir := filepath.Join(rootfsPath, "usr", "lib", "systemd", "user",
		"microsoft-identity-broker.service.d")
	os.MkdirAll(brokerOverrideDir, 0755)
	if err := os.WriteFile(filepath.Join(brokerOverrideDir, "display.conf"),
		[]byte("[Service]\nEnvironment=\"DISPLAY=:0\"\n"), 0644); err != nil {
		return fmt.Errorf("write broker display override: %w", err)
	}

	return nil
}

// InstallPackages installs additional packages inside the container rootfs.
// The frostyard OCI image includes intune-portal and the identity broker but
// not Microsoft Edge or libsecret-tools, which we need for SSO and keyring init.
func InstallPackages(r runner.Runner, rootfsPath string) error {
	// Add the Edge apt repo (uses same Microsoft GPG key already in the image)
	edgeRepo := "deb [arch=amd64 signed-by=/etc/apt/keyrings/microsoft-edge.gpg] https://packages.microsoft.com/repos/edge stable main\n"
	edgeListPath := filepath.Join(rootfsPath, "etc", "apt", "sources.list.d", "microsoft-edge.list")
	os.MkdirAll(filepath.Dir(edgeListPath), 0755)
	if err := os.WriteFile(edgeListPath, []byte(edgeRepo), 0644); err != nil {
		return fmt.Errorf("write edge repo: %w", err)
	}

	// Download the Edge GPG key on the host and install it into the rootfs.
	// The frostyard image doesn't have curl/gpg so we fetch from the host side.
	keyPath := filepath.Join(rootfsPath, "etc", "apt", "keyrings", "microsoft-edge.gpg")
	out, err := r.Run("bash", "-c",
		fmt.Sprintf("curl -fsSL https://packages.microsoft.com/keys/microsoft.asc | gpg --dearmor -o %s", keyPath))
	if err != nil {
		return fmt.Errorf("download edge GPG key: %w\n%s", err, out)
	}

	return r.RunAttached("sudo", "systemd-nspawn", "--console=pipe", "-D", rootfsPath,
		"bash", "-c", "apt-get update && apt-get install -y microsoft-edge-stable libsecret-tools sudo",
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
	defer os.Remove(tmpFile.Name())

	if _, err := tmpFile.WriteString(rule); err != nil {
		tmpFile.Close()
		return fmt.Errorf("write temp file: %w", err)
	}
	tmpFile.Close()

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

