package broker

import (
	"fmt"
	"path/filepath"
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

// LoginSessionArgs returns machinectl args to create a persistent login session
// inside the container. The login shell sources /etc/profile.d/intuneme.sh which
// handles DISPLAY import, gnome-keyring initialization, and broker restarts.
// The session stays alive via sleep infinity so the keyring daemon persists.
func LoginSessionArgs(machine, user string) []string {
	return []string{
		"shell", fmt.Sprintf("%s@%s", user, machine),
		"/bin/bash", "--login", "-c", "exec sleep infinity",
	}
}
