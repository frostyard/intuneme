package cmd

import (
	"bufio"
	"bytes"
	"io"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"unicode"

	"github.com/frostyard/intuneme/internal/config"
	"github.com/frostyard/intuneme/internal/problemrec"
)

// maxPasswdSize bounds the read of the container's /etc/passwd.
const maxPasswdSize = 1 << 20

// identityCheck is identityConsistent; tests replace it to see whether
// recordInternal calls it.
var identityCheck = identityConsistent

// recordInternal records the internal failure err under code, but only when
// the config identity is consistent (identityConsistent). Otherwise the
// failure can come from user setup, and a lost record is better than a wrong
// one. With an inert recorder (a dev build, or none) it returns at once and
// reads no file. It never changes err or output.
func recordInternal(cfg *config.Config, code string, err error) {
	if problemrec.Current().Err() != nil {
		return
	}
	if identityCheck(cfg) {
		problemrec.Error(code, err)
	}
}

// identityConsistent reports whether the configured host identity matches the
// running user and an account in the container: HostUID is a valid non-root
// UID equal to os.Getuid(), HostUser is a plausible non-root user name, and
// <rootfs>/etc/passwd has exactly 1 line for that name, and that line is a
// strict 7-field entry with that UID (see passwdEntryMatches). The file is
// opened with O_NONBLOCK and O_NOFOLLOW and must be a regular file, so a FIFO
// or a symlink cannot block or redirect the read. Any read or parse error, a
// malformed entry or a duplicate entry for the user gives false (fail
// closed). It writes and prints nothing.
func identityConsistent(cfg *config.Config) bool {
	if cfg == nil {
		return false
	}
	uid := cfg.HostUID
	if uid <= 0 || uid != os.Getuid() || uint64(uid) >= math.MaxUint32 {
		return false
	}
	user := cfg.HostUser
	if user == "" || user == "root" || strings.Contains(user, ":") || strings.IndexFunc(user, unicode.IsSpace) >= 0 {
		return false
	}
	f, err := os.OpenFile(filepath.Join(cfg.RootfsPath, "etc", "passwd"),
		syscall.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK|syscall.O_CLOEXEC, 0)
	if err != nil {
		return false
	}
	defer func() { _ = f.Close() }()
	if fi, err := f.Stat(); err != nil || !fi.Mode().IsRegular() {
		return false
	}
	data, err := io.ReadAll(io.LimitReader(f, maxPasswdSize+1))
	if err != nil || len(data) > maxPasswdSize {
		return false
	}
	found := false
	sc := bufio.NewScanner(bytes.NewReader(data))
	sc.Buffer(make([]byte, 0, 64*1024), maxPasswdSize)
	for sc.Scan() {
		fields := strings.Split(sc.Text(), ":")
		if fields[0] != user {
			continue
		}
		if found || !passwdEntryMatches(fields, uid) {
			return false
		}
		found = true
	}
	return found && sc.Err() == nil
}

// passwdEntryMatches reports whether fields is a strict passwd entry with
// uid: exactly 7 fields, field 3 is the decimal uid (no sign, no leading
// zero), field 4 is a GID below MaxUint32, and field 6 (home) is absolute.
func passwdEntryMatches(fields []string, uid int) bool {
	if len(fields) != 7 || fields[2] != strconv.Itoa(uid) {
		return false
	}
	gid, err := strconv.ParseUint(fields[3], 10, 32)
	if err != nil || gid == math.MaxUint32 {
		return false
	}
	return filepath.IsAbs(fields[5])
}
