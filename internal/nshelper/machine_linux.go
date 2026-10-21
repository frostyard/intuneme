//go:build linux

package nshelper

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"

	"golang.org/x/sys/unix"
)

func MachineMain(args []string) int {
	cfg, err := LoadConfig(ConfigPath)
	if err == nil && os.Geteuid() != 0 {
		err = fmt.Errorf("must be run as root")
	}
	if err == nil {
		err = runMachine(cfg, args)
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "machine-exec: %v\n", err)
		return 1
	}
	return 0
}

func runMachine(cfg *Config, args []string) error {
	if err := cfg.Validate(); err != nil {
		return err
	}
	if len(args) == 0 || (args[0] != "stop" && args[0] != "bind") ||
		(args[0] == "stop" && len(args) != 1) || (args[0] == "bind" && len(args) != 2) {
		return fmt.Errorf("usage: machine-exec stop | bind <directory>")
	}
	if args[0] == "bind" {
		return bindDirectory(cfg, args[1])
	}
	leader, err := resolveLeader(cfg.Machine)
	if err != nil {
		return err
	}
	if err := validateTarget("/proc", leader, cfg); err != nil {
		return err
	}
	machinectl, err := findExecutable("machinectl")
	if err != nil {
		return err
	}
	commandArgs := []string{"poweroff", cfg.Machine}
	command := exec.Command(machinectl, commandArgs...)
	command.Env = []string{"PATH=" + helperPATH, "LC_ALL=C"}
	command.Stdin = os.Stdin
	command.Stdout = os.Stdout
	command.Stderr = os.Stderr
	return command.Run()
}

func pinBindSource(path string, uid uint32) (*os.File, error) {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return nil, fmt.Errorf("bind source must be absolute and normalized")
	}
	fd, err := unix.Openat2(unix.AT_FDCWD, path, &unix.OpenHow{
		Flags:   unix.O_PATH | unix.O_DIRECTORY | unix.O_CLOEXEC,
		Resolve: unix.RESOLVE_NO_SYMLINKS | unix.RESOLVE_NO_MAGICLINKS,
	})
	if err != nil {
		return nil, fmt.Errorf("open bind source without symlinks: %w", err)
	}
	file := os.NewFile(uintptr(fd), path)
	var stat unix.Stat_t
	if err := unix.Fstat(fd, &stat); err != nil {
		_ = file.Close()
		return nil, err
	}
	if stat.Uid != uid {
		_ = file.Close()
		return nil, fmt.Errorf("bind source must be owned by uid %s", strconv.FormatUint(uint64(uid), 10))
	}
	return file, nil
}

// bindDirectory uses only descriptors after validation. In particular, do not
// send /proc/self/fd paths to machinectl: older machined chase() implementations
// turn them back into replaceable pathnames before mounting.
func bindDirectory(cfg *Config, path string) error {
	source, err := pinBindSource(path, cfg.UID)
	if err != nil {
		return err
	}
	defer func() { _ = source.Close() }()
	leader, err := resolveLeader(cfg.Machine)
	if err != nil {
		return err
	}
	proc, err := os.Open(fmt.Sprintf("/proc/%d", leader))
	if err != nil {
		return err
	}
	defer func() { _ = proc.Close() }()
	// A proc directory FD pins the process identity across PID reuse.
	openTarget := func(name string) (*os.File, error) {
		fd, err := unix.Openat(int(proc.Fd()), name, unix.O_RDONLY|unix.O_CLOEXEC, 0)
		if err != nil {
			return nil, err
		}
		return os.NewFile(uintptr(fd), name), nil
	}
	user, err := openTarget("ns/user")
	if err != nil {
		return err
	}
	defer func() { _ = user.Close() }()
	root, err := openTarget("root")
	if err != nil {
		return err
	}
	defer func() { _ = root.Close() }()
	mnt, err := openTarget("ns/mnt")
	if err != nil {
		return err
	}
	defer func() { _ = mnt.Close() }()
	selfUser, err := os.Stat("/proc/self/ns/user")
	if err != nil {
		return err
	}
	targetUser, err := user.Stat()
	if err != nil {
		return err
	}
	targetRoot, err := root.Stat()
	if err != nil {
		return err
	}
	wantRoot, err := os.Stat(cfg.Rootfs)
	if err != nil {
		return err
	}
	if !sameFile(selfUser, targetUser) || !sameFile(targetRoot, wantRoot) {
		return fmt.Errorf("bind target does not match configured container")
	}
	mount, err := cloneReadOnlyMount(source)
	if err != nil {
		return err
	}
	defer func() { _ = unix.Close(mount) }()
	runtime.LockOSThread()
	if err := unix.Unshare(unix.CLONE_FS); err != nil {
		return err
	}
	if err := unix.Setns(int(mnt.Fd()), unix.CLONE_NEWNS); err != nil {
		return err
	}
	return attachMCPMount(mount, int(root.Fd()))
}

func cloneReadOnlyMount(source *os.File) (int, error) {
	mount, err := unix.OpenTree(int(source.Fd()), "", unix.OPEN_TREE_CLONE|unix.OPEN_TREE_CLOEXEC|unix.AT_EMPTY_PATH)
	if err != nil {
		return -1, fmt.Errorf("clone pinned bind source: %w", err)
	}
	if err := unix.MountSetattr(mount, "", unix.AT_EMPTY_PATH, &unix.MountAttr{Attr_set: unix.MOUNT_ATTR_RDONLY | unix.MOUNT_ATTR_NOSUID | unix.MOUNT_ATTR_NODEV}); err != nil {
		_ = unix.Close(mount)
		return -1, fmt.Errorf("make MCP mount read-only: %w", err)
	}
	return mount, nil
}

func attachMCPMount(mount, root int) error {
	if err := unix.Mkdirat(root, "opt", 0755); err != nil && err != unix.EEXIST {
		return err
	}
	opt, err := unix.Openat2(root, "opt", &unix.OpenHow{Flags: unix.O_PATH | unix.O_DIRECTORY | unix.O_CLOEXEC, Resolve: unix.RESOLVE_BENEATH | unix.RESOLVE_NO_SYMLINKS})
	if err != nil {
		return err
	}
	defer func() { _ = unix.Close(opt) }()
	if err := unix.Mkdirat(opt, "intuneme-mcp", 0755); err != nil && err != unix.EEXIST {
		return err
	}
	dest, err := unix.Openat2(opt, "intuneme-mcp", &unix.OpenHow{Flags: unix.O_PATH | unix.O_DIRECTORY | unix.O_CLOEXEC, Resolve: unix.RESOLVE_BENEATH | unix.RESOLVE_NO_SYMLINKS})
	if err != nil {
		return err
	}
	defer func() { _ = unix.Close(dest) }()
	return unix.MoveMount(mount, "", dest, "", unix.MOVE_MOUNT_F_EMPTY_PATH|unix.MOVE_MOUNT_T_EMPTY_PATH)
}
