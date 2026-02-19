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

	return nil
}

// SetContainerPassword sets the user's password inside the container via chpasswd.
// Without a password, the account is locked and machinectl shell/login won't work interactively.
// The password is passed via a temp file bound read-only into the container to avoid shell injection.
func SetContainerPassword(r runner.Runner, rootfsPath, user, password string) error {
	tmp, err := os.CreateTemp("", "intuneme-chpasswd-*")
	if err != nil {
		return fmt.Errorf("create chpasswd temp file: %w", err)
	}
	defer func() { _ = os.Remove(tmp.Name()) }()

	if _, err := fmt.Fprintf(tmp, "%s:%s\n", user, password); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write chpasswd input: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close chpasswd temp file: %w", err)
	}

	return r.RunAttached("sudo", "systemd-nspawn", "--console=pipe",
		"--bind-ro="+tmp.Name()+":/run/chpasswd-input",
		"-D", rootfsPath,
		"bash", "-c", "chpasswd < /run/chpasswd-input",
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

	// Install with correct permissions — polkitd runs as the polkitd user
	// and needs read access (644), but sudo cp inherits root's umask (often 077).
	dest := filepath.Join(rulesDir, "50-intuneme.rules")
	if err := r.RunAttached("sudo", "install", "-m", "0644", tmpFile.Name(), dest); err != nil {
		return fmt.Errorf("install polkit rule failed: %w", err)
	}
	return nil
}
