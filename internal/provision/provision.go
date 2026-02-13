package provision

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/bjk/intuneme/internal/runner"
)

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

	// PAM config for gnome-keyring
	pamAuth := filepath.Join(rootfsPath, "etc", "pam.d", "common-auth")
	appendLine(pamAuth, "auth optional pam_gnome_keyring.so")
	pamSession := filepath.Join(rootfsPath, "etc", "pam.d", "common-session")
	appendLine(pamSession, "session optional pam_gnome_keyring.so auto_start")

	// Pre-create keyring directory
	keyringDir := filepath.Join(rootfsPath, "home", user, ".local", "share", "keyrings")
	os.MkdirAll(keyringDir, 0755)
	os.WriteFile(filepath.Join(keyringDir, "default"), []byte("login\n"), 0644)

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

	return nil
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

func appendLine(path, line string) {
	data, _ := os.ReadFile(path)
	content := string(data)
	if !strings.Contains(content, line) {
		f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
		if err == nil {
			defer f.Close()
			f.WriteString(line + "\n")
		}
	}
}
