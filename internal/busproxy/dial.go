// Package busproxy holds the connection code shared by the host-side proxies
// that serve names on the container's session bus.
package busproxy

import (
	"fmt"
	"net"
	"syscall"

	"github.com/godbus/dbus/v5"
)

// Dial connects to the bus socket at socketPath and returns the connection
// together with the PID of the process that listens on the socket. The kernel
// reports that PID in the caller's PID namespace, so for the container's
// session bus it is a host PID of a process inside the container.
func Dial(socketPath string, opts ...dbus.ConnOption) (*dbus.Conn, int, error) {
	uconn, err := net.DialUnix("unix", nil, &net.UnixAddr{Name: socketPath, Net: "unix"})
	if err != nil {
		return nil, 0, fmt.Errorf("dial %s: %w", socketPath, err)
	}

	pid, err := peerPID(uconn)
	if err != nil {
		_ = uconn.Close()
		return nil, 0, err
	}

	conn, err := dbus.ConnectUnix(uconn, opts...)
	if err != nil {
		return nil, 0, fmt.Errorf("connect bus at %s: %w", socketPath, err)
	}
	return conn, pid, nil
}

// peerPID reads SO_PEERCRED from a connected unix socket.
func peerPID(uconn *net.UnixConn) (int, error) {
	raw, err := uconn.SyscallConn()
	if err != nil {
		return 0, fmt.Errorf("raw socket: %w", err)
	}
	var cred *syscall.Ucred
	var credErr error
	if err := raw.Control(func(fd uintptr) {
		cred, credErr = syscall.GetsockoptUcred(int(fd), syscall.SOL_SOCKET, syscall.SO_PEERCRED)
	}); err != nil {
		return 0, fmt.Errorf("raw socket control: %w", err)
	}
	if credErr != nil {
		return 0, fmt.Errorf("read SO_PEERCRED: %w", credErr)
	}
	return int(cred.Pid), nil
}
