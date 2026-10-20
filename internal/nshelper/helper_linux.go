//go:build linux

package nshelper

import (
	"bufio"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"syscall"

	"golang.org/x/sys/unix"
)

// helperPATH is deliberately fixed. The helper runs as root under a NOPASSWD
// rule, so it must never resolve a command from the caller's PATH. The NixOS
// path is first for users on NixOS; the standard paths cover everything else.
const helperPATH = "/run/current-system/sw/bin:/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin"

// containerPATH is the PATH in the launched session. It matches the PATH the
// container image and session setup expect.
const containerPATH = "/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin"

// The following indirections exist so the security checks can be tested
// without root or a running container.
var (
	findExecutable = findInPath
	commandOutput  = func(name string, args ...string) ([]byte, error) {
		return exec.Command(name, args...).Output()
	}
)

// IsHelperName reports whether argv[0] refers to the installed helper. The
// intuneme binary is copied to Path and re-executed by sudo; main() uses this
// to dispatch into the helper instead of the CLI.
func IsHelperName(argv0 string) bool {
	return filepath.Base(argv0) == "nsenter-exec"
}

// Main runs the helper using the config installed next to it. It returns a
// process exit code.
func Main(args []string) int {
	cfg, err := LoadConfig(ConfigPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "nsenter-exec: %v\n", err)
		return 1
	}
	if len(args) != 1 {
		fmt.Fprintln(os.Stderr, "usage: nsenter-exec <script>")
		return 1
	}
	return Run(cfg, args[0])
}

// Run resolves the configured machine, validates the target, enters its
// namespaces, drops to the configured numeric UID, and runs script.
func Run(cfg *Config, script string) int {
	if os.Geteuid() != 0 {
		fmt.Fprintln(os.Stderr, "nsenter-exec: must be run as root")
		return 1
	}
	if strings.TrimSpace(script) == "" {
		fmt.Fprintln(os.Stderr, "nsenter-exec: empty script")
		return 1
	}

	leader, err := resolveLeader(cfg.Machine)
	if err != nil {
		fmt.Fprintf(os.Stderr, "nsenter-exec: %v\n", err)
		return 1
	}
	if err := validateTarget("/proc", leader, cfg); err != nil {
		fmt.Fprintf(os.Stderr, "nsenter-exec: refusing to enter pid %d: %v\n", leader, err)
		return 1
	}
	cred, err := credentialsFromRootfs(cfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "nsenter-exec: %v\n", err)
		return 1
	}
	if err := enterNamespaces("/proc", leader); err != nil {
		fmt.Fprintf(os.Stderr, "nsenter-exec: enter container: %v\n", err)
		return 1
	}

	// Everything below this point runs after the target's mount namespace is
	// active. /bin/bash therefore comes from the container, but it is executed
	// only after the kernel has applied the numeric UID/GID below. No
	// container-controlled binary runs as root.
	attr := &syscall.ProcAttr{
		Dir:   cfg.Home,
		Env:   buildEnv(cfg),
		Files: []uintptr{os.Stdin.Fd(), os.Stdout.Fd(), os.Stderr.Fd()},
		Sys: &syscall.SysProcAttr{
			Credential: buildCredential(cfg, cred),
		},
	}
	pid, err := syscall.ForkExec("/bin/bash", []string{"bash", "-c", script}, attr)
	if err != nil {
		fmt.Fprintf(os.Stderr, "nsenter-exec: exec: %v\n", err)
		return 1
	}
	var ws syscall.WaitStatus
	if _, err := syscall.Wait4(pid, &ws, 0, nil); err != nil {
		fmt.Fprintf(os.Stderr, "nsenter-exec: wait: %v\n", err)
		return 1
	}
	switch {
	case ws.Exited():
		return ws.ExitStatus()
	case ws.Signaled():
		return 128 + int(ws.Signal())
	default:
		return 1
	}
}

// resolveLeader asks systemd-machined for the configured machine's init PID.
// The caller never supplies this value.
func resolveLeader(machine string) (int, error) {
	machinectl, err := findExecutable("machinectl")
	if err != nil {
		return 0, err
	}
	out, err := commandOutput(machinectl, "show", machine, "-p", "Leader", "--value")
	if err != nil {
		return 0, fmt.Errorf("resolve leader of %s: %w", machine, err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(out)))
	if err != nil || pid <= 0 {
		return 0, fmt.Errorf("resolve leader of %s: invalid pid %q", machine, strings.TrimSpace(string(out)))
	}
	return pid, nil
}

// validateTarget proves that pid is the configured container and not an
// attacker-controlled namespace. Two independent checks are used:
//
//   - the target must live in the host's initial user namespace (the container
//     runs without --private-users), so entering its mount namespace cannot
//     shed root privileges into a user namespace; and
//   - the target's root must be the configured rootfs.
func validateTarget(procRoot string, pid int, cfg *Config) error {
	return validateTargetPaths(procRoot, pid, cfg)
}

func validateTargetPaths(procRoot string, pid int, cfg *Config) error {
	pidPath := filepath.Join(procRoot, strconv.Itoa(pid))
	selfUser, err := os.Stat(filepath.Join(procRoot, "self", "ns", "user"))
	if err != nil {
		return fmt.Errorf("stat own user namespace: %w", err)
	}
	targetUser, err := os.Stat(filepath.Join(pidPath, "ns", "user"))
	if err != nil {
		return fmt.Errorf("stat target user namespace: %w", err)
	}
	if !sameFile(selfUser, targetUser) {
		return fmt.Errorf("target is in a different user namespace")
	}
	targetRoot, err := os.Stat(filepath.Join(pidPath, "root"))
	if err != nil {
		return fmt.Errorf("stat target root: %w", err)
	}
	wantRoot, err := os.Stat(cfg.Rootfs)
	if err != nil {
		return fmt.Errorf("stat configured rootfs: %w", err)
	}
	if !sameFile(targetRoot, wantRoot) {
		return fmt.Errorf("target root is not %s", cfg.Rootfs)
	}
	return nil
}

// buildCredential always takes the UID from the baked config. The container's
// passwd file is only consulted for the primary GID and supplementary groups;
// a container-root user cannot promote themselves to uid 0 through it.
func buildCredential(cfg *Config, cred credential) *syscall.Credential {
	return &syscall.Credential{
		Uid:    cfg.UID,
		Gid:    cred.GID,
		Groups: cred.Groups,
	}
}

func sameFile(a, b os.FileInfo) bool {
	as, ok := a.Sys().(*syscall.Stat_t)
	if !ok {
		return false
	}
	bs, ok := b.Sys().(*syscall.Stat_t)
	if !ok {
		return false
	}
	return as.Dev == bs.Dev && as.Ino == bs.Ino
}

type credential struct {
	GID    uint32
	Groups []uint32
}

// credentialsFromRootfs reads the container's group database for supplementary
// groups, but never takes the UID from it. The UID is always cfg.UID, so a
// container-root user cannot change their passwd entry to uid 0 and have the
// helper execute the script as root.
func credentialsFromRootfs(cfg *Config) (credential, error) {
	gid := cfg.GID
	if passwd, err := os.Open(filepath.Join(cfg.Rootfs, "etc", "passwd")); err == nil {
		scanner := bufio.NewScanner(passwd)
		for scanner.Scan() {
			fields := strings.Split(scanner.Text(), ":")
			if len(fields) < 4 {
				continue
			}
			uid, err := strconv.ParseUint(fields[2], 10, 32)
			if err != nil || uint32(uid) != cfg.UID {
				continue
			}
			if parsed, err := strconv.ParseUint(fields[3], 10, 32); err == nil && parsed <= math.MaxUint32 {
				gid = uint32(parsed)
			}
			break
		}
		_ = passwd.Close()
	} else if !os.IsNotExist(err) {
		return credential{}, fmt.Errorf("read container passwd: %w", err)
	}

	primaryGID := gid
	groups := []uint32{primaryGID}
	if groupFile, err := os.Open(filepath.Join(cfg.Rootfs, "etc", "group")); err == nil {
		scanner := bufio.NewScanner(groupFile)
		for scanner.Scan() {
			fields := strings.Split(scanner.Text(), ":")
			if len(fields) < 4 {
				continue
			}
			gid, err := strconv.ParseUint(fields[2], 10, 32)
			if err != nil || gid > math.MaxUint32 {
				continue
			}
			groupGID := uint32(gid)
			for member := range strings.SplitSeq(fields[3], ",") {
				if strings.TrimSpace(member) == cfg.User {
					groups = append(groups, groupGID)
					break
				}
			}
		}
		_ = groupFile.Close()
	} else if !os.IsNotExist(err) {
		return credential{}, fmt.Errorf("read container group: %w", err)
	}

	slices.Sort(groups)
	groups = slices.Compact(groups)
	return credential{GID: primaryGID, Groups: groups}, nil
}

// enterNamespaces opens every namespace fd before switching (so the target's
// /proc is not needed after the first setns) and enters only the namespaces
// the helper needs.
func enterNamespaces(procRoot string, pid int) error {
	runtime.LockOSThread()

	namespaces := []struct {
		name string
		flag int
	}{
		{"mnt", unix.CLONE_NEWNS},
		{"uts", unix.CLONE_NEWUTS},
		{"ipc", unix.CLONE_NEWIPC},
		{"net", unix.CLONE_NEWNET},
		{"pid", unix.CLONE_NEWPID},
	}
	fds := make([]int, 0, len(namespaces))
	defer func() {
		for _, fd := range fds {
			_ = unix.Close(fd)
		}
	}()

	for _, ns := range namespaces {
		path := filepath.Join(procRoot, strconv.Itoa(pid), "ns", ns.name)
		fd, err := unix.Open(path, unix.O_RDONLY|unix.O_CLOEXEC, 0)
		if err != nil {
			return fmt.Errorf("open %s: %w", path, err)
		}
		fds = append(fds, fd)
	}
	for i, ns := range namespaces {
		if err := unix.Setns(fds[i], ns.flag); err != nil {
			return fmt.Errorf("setns %s: %w", ns.name, err)
		}
	}
	return nil
}

// buildEnv gives the launched process a deterministic environment. The
// session script sets DISPLAY/XAUTHORITY/D-Bus and the rest.
func buildEnv(cfg *Config) []string {
	env := []string{
		"PATH=" + containerPATH,
		"HOME=" + cfg.Home,
		"USER=" + cfg.User,
		"LOGNAME=" + cfg.User,
		"SHELL=/bin/bash",
	}
	for _, key := range []string{"TERM", "LANG", "LC_ALL", "LC_CTYPE"} {
		if value, ok := os.LookupEnv(key); ok {
			env = append(env, key+"="+value)
		}
	}
	return env
}

func findInPath(name string) (string, error) {
	for _, dir := range strings.Split(helperPATH, ":") {
		path := filepath.Join(dir, name)
		info, err := os.Stat(path)
		if err == nil && !info.IsDir() && info.Mode()&0o111 != 0 {
			return path, nil
		}
	}
	return "", fmt.Errorf("%s not found in %s", name, helperPATH)
}
