package busproxy

import (
	"bytes"
	"fmt"
	"os"
	"strconv"
	"strings"
	"syscall"
)

// ReadPID returns the PID stored in pidPath. The bool is false when the file
// cannot be read or ParsePID rejects its content.
func ReadPID(pidPath string) (int, bool) {
	data, err := os.ReadFile(pidPath)
	if err != nil {
		return 0, false
	}
	return ParsePID(data)
}

// ParsePID parses the content of a PID file. The bool is false when the
// content is not an integer in 1..2147483647. kill(2) reads 0 as the caller's
// process group and a negative value as a process group (or, for -1, every
// process the user can signal), so such a value never names 1 process and a
// caller must treat it as no process. pid_t is a signed 32-bit integer, so a
// larger value is truncated at the syscall boundary (4294967296 becomes 0,
// 4294967295 becomes -1) and is rejected too.
func ParsePID(data []byte) (int, bool) {
	pid, err := strconv.ParseInt(strings.TrimSpace(string(data)), 10, 32)
	if err != nil || pid <= 0 {
		return 0, false
	}
	return int(pid), true
}

// CheckNotRunning returns an error when pidPath names a live process whose
// command line contains subcommand. A stale file, or one whose PID was reused
// by another program, does not count.
func CheckNotRunning(pidPath, subcommand string) error {
	pid, ok := ReadPID(pidPath)
	if !ok || syscall.Kill(pid, 0) != nil {
		return nil
	}
	cmdline, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/cmdline")
	if err != nil {
		return nil
	}
	for _, arg := range bytes.Split(cmdline, []byte{0}) {
		if string(arg) == subcommand {
			return fmt.Errorf("%s is already running (PID %d, %s)", subcommand, pid, pidPath)
		}
	}
	return nil
}

// WritePID writes the current process's PID to pidPath.
func WritePID(pidPath string) error {
	return os.WriteFile(pidPath, []byte(strconv.Itoa(os.Getpid())), 0o644)
}

// RemoveOwnPID removes pidPath only when it holds the current process's PID,
// so an exiting second instance does not remove the file of a running one.
func RemoveOwnPID(pidPath string) {
	if pid, ok := ReadPID(pidPath); ok && pid == os.Getpid() {
		_ = os.Remove(pidPath)
	}
}
