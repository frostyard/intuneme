package broker

import (
	"fmt"
	"path/filepath"
	"strings"
)

// RuntimeDir returns the host-side directory that is bind-mounted into the
// container as /run/user/<uid>. The container's session bus socket appears
// here as "bus".
func RuntimeDir(root string) string {
	return filepath.Join(root, "runtime")
}

// SessionBusSocketPath returns the host-side path to the container's session bus socket.
// This works because RuntimeDir is bind-mounted to /run/user/<uid> inside the container.
func SessionBusSocketPath(root string) string {
	return filepath.Join(RuntimeDir(root), "bus")
}

// RuntimeBindMount returns the nspawn bind mount that maps the host-side runtime
// directory to /run/user/<uid> inside the container, making the session bus socket
// accessible from the host.
func RuntimeBindMount(root string, uid int) (host, container string) {
	return RuntimeDir(root), fmt.Sprintf("/run/user/%d", uid)
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
	// Escape single quotes to prevent shell injection.
	escaped := strings.ReplaceAll(password, "'", `'\''`)
	script := fmt.Sprintf(
		`printf '%%s' '%s' | gnome-keyring-daemon --replace --unlock --components=secrets,pkcs11 && exec sleep infinity`,
		escaped,
	)
	return []string{
		"shell", fmt.Sprintf("%s@%s", user, machine),
		"/bin/bash", "--login", "-c", script,
	}
}

// ContainerPassword is the hardcoded password set during intuneme init.
const ContainerPassword = "Intuneme2024!"
