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

// recordInternal records the internal failure err under code, but only when
// the config identity is consistent (identityConsistent). Otherwise the
// failure can come from user setup, and a lost record is better than a wrong
// one. It never changes err or output.
func recordInternal(cfg *config.Config, code string, err error) {
	if identityConsistent(cfg) {
		problemrec.Error(code, err)
	}
}

// identityConsistent reports whether the configured host identity matches the
// running user and an account in the container: HostUID is a valid non-root
// UID equal to os.Getuid(), HostUser is a plausible non-root user name, and
// <rootfs>/etc/passwd has a line with that name and UID. Any read or parse
// error gives false (fail closed). It writes and prints nothing.
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
	f, err := os.OpenFile(filepath.Join(cfg.RootfsPath, "etc", "passwd"), os.O_RDONLY|syscall.O_NOFOLLOW, 0)
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
	want := strconv.Itoa(uid)
	sc := bufio.NewScanner(bytes.NewReader(data))
	sc.Buffer(make([]byte, 0, 64*1024), maxPasswdSize)
	for sc.Scan() {
		fields := strings.Split(sc.Text(), ":")
		if len(fields) >= 3 && fields[0] == user && fields[2] == want {
			return true
		}
	}
	return false
}
