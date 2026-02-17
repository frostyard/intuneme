package broker

import (
	"fmt"
	"path/filepath"
)

// SessionBusSocketPath returns the filesystem path to the container's session bus socket.
func SessionBusSocketPath(rootfsPath string, uid int) string {
	return filepath.Join(rootfsPath, "run", fmt.Sprintf("user/%d", uid), "bus")
}

// EnableLingerArgs returns machinectl args to enable lingering for a user inside the container.
func EnableLingerArgs(machine, user string) []string {
	return []string{
		"shell", fmt.Sprintf("root@%s", machine),
		"/bin/loginctl", "enable-linger", user,
	}
}

// UnlockKeyringArgs returns machinectl args to create a login session and unlock the keyring.
// The returned command runs in the foreground — caller should run it in the background.
func UnlockKeyringArgs(machine, user, password string) []string {
	script := fmt.Sprintf(
		`echo '%s' | gnome-keyring-daemon --replace --unlock --components=secrets,pkcs11 && exec sleep infinity`,
		password,
	)
	return []string{
		"shell", fmt.Sprintf("%s@%s", user, machine),
		"/bin/bash", "--login", "-c", script,
	}
}

// ContainerPassword is the hardcoded password set during intuneme init.
const ContainerPassword = "Intuneme2024!"
