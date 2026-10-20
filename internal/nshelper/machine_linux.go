//go:build linux

package nshelper

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
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
	if args[0] == "bind" {
		source, err := pinBindSource(args[1], cfg.UID)
		if err != nil {
			return err
		}
		defer func() { _ = source.Close() }()
		// machined resolves this helper-owned FD while we wait for its reply.
		// Never pass the caller's pathname after validating it: it may be renamed
		// or replaced at any point. The open descriptor pins the checked inode.
		pinned := fmt.Sprintf("/proc/%d/fd/%d", os.Getpid(), source.Fd())
		commandArgs = []string{"bind", "--read-only", "--mkdir", cfg.Machine, pinned, MCPMountDir}
	}
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
