package notify

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/godbus/dbus/v5"
	"golang.org/x/sys/unix"
)

// imagePathHints are the Notify hints that carry a file path. Chromium sets
// both spellings: "image-path" is the spec name, "image_path" the deprecated one.
var imagePathHints = []string{"image-path", "image_path"}

// maxIconSize is the largest icon file the proxy copies to the host.
const maxIconSize = 1 << 20

// iconStore copies icon files from the container's /tmp into a host directory
// that the proxy owns, and gives the host notification server those copies.
//
// The container's /tmp is a tmpfs in its own mount namespace, so the host
// reaches it through /proc/<pid>/root of a process in that namespace. The
// container controls everything in that tree, so a path is never handed to
// the host server directly: an absolute symlink there would resolve against
// the host root. Instead the file is opened with openat2 and RESOLVE_IN_ROOT,
// checked to be a small regular file, and copied.
type iconStore struct {
	// procRoot is "/proc/<pid>/root" for a process inside the container.
	procRoot string
	// dir holds the copies. Only the proxy writes to it.
	dir string
}

// iconDir returns the host directory for icon copies.
func iconDir() (string, error) {
	runtimeDir := os.Getenv("XDG_RUNTIME_DIR")
	if runtimeDir == "" {
		return "", errors.New("XDG_RUNTIME_DIR is not set")
	}
	return filepath.Join(runtimeDir, "intuneme-notify"), nil
}

// newIconStore empties dir and creates it with mode 0700.
func newIconStore(pid int, dir string) (*iconStore, error) {
	if err := os.RemoveAll(dir); err != nil {
		return nil, fmt.Errorf("clear icon directory: %w", err)
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("create icon directory: %w", err)
	}
	return &iconStore{procRoot: "/proc/" + strconv.Itoa(pid) + "/root", dir: dir}, nil
}

// close removes all copies.
func (s *iconStore) close() {
	_ = os.RemoveAll(s.dir)
}

// remove deletes copies made for a notification.
func (s *iconStore) remove(files []string) {
	for _, f := range files {
		_ = os.Remove(f)
	}
}

// copyIn copies the container file at p into dir and returns the host path of
// the copy. p must be a clean absolute path under /tmp.
func (s *iconStore) copyIn(p string) (string, error) {
	clean := filepath.Clean(p)
	if clean != p || !strings.HasPrefix(clean, "/tmp/") {
		return "", fmt.Errorf("icon path %q is not a clean path under /tmp", p)
	}

	root, err := unix.Open(s.procRoot, unix.O_PATH|unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
	if err != nil {
		return "", fmt.Errorf("open container root: %w", err)
	}
	defer func() { _ = unix.Close(root) }()

	// O_NONBLOCK keeps a FIFO from blocking the open. The fstat below then
	// rejects it.
	fd, err := unix.Openat2(root, strings.TrimPrefix(clean, "/"), &unix.OpenHow{
		Flags:   unix.O_RDONLY | unix.O_NONBLOCK | unix.O_NOCTTY | unix.O_CLOEXEC,
		Resolve: unix.RESOLVE_IN_ROOT | unix.RESOLVE_NO_MAGICLINKS,
	})
	if err != nil {
		return "", fmt.Errorf("open icon %s: %w", clean, err)
	}
	src := os.NewFile(uintptr(fd), clean)
	defer func() { _ = src.Close() }()

	info, err := src.Stat()
	if err != nil {
		return "", fmt.Errorf("stat icon %s: %w", clean, err)
	}
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("icon %s is not a regular file", clean)
	}
	if info.Size() > maxIconSize {
		return "", fmt.Errorf("icon %s is larger than %d bytes", clean, maxIconSize)
	}
	data, err := io.ReadAll(io.LimitReader(src, maxIconSize+1))
	if err != nil {
		return "", fmt.Errorf("read icon %s: %w", clean, err)
	}
	if len(data) > maxIconSize {
		return "", fmt.Errorf("icon %s is larger than %d bytes", clean, maxIconSize)
	}

	dst, err := os.CreateTemp(s.dir, "icon-*"+filepath.Ext(clean))
	if err != nil {
		return "", fmt.Errorf("create icon copy: %w", err)
	}
	if _, err := dst.Write(data); err != nil {
		_ = dst.Close()
		_ = os.Remove(dst.Name())
		return "", fmt.Errorf("write icon copy: %w", err)
	}
	if err := dst.Close(); err != nil {
		_ = os.Remove(dst.Name())
		return "", fmt.Errorf("write icon copy: %w", err)
	}
	return dst.Name(), nil
}

// iconCopies maps container icon paths to host copies for one Notify call.
// One file is copied once, even when several arguments name it.
type iconCopies struct {
	store  *iconStore
	byPath map[string]string
	files  []string
}

func (s *iconStore) begin() *iconCopies {
	return &iconCopies{store: s, byPath: map[string]string{}}
}

// mapPath maps a file:// URL or an absolute path to a host copy and keeps the
// URL form. ok is false when the file cannot be copied. Anything else, such as
// an icon theme name, is returned unchanged with ok true.
func (c *iconCopies) mapPath(s string) (string, bool) {
	p, isURL := strings.CutPrefix(s, "file://")
	if !strings.HasPrefix(p, "/") {
		return s, true
	}
	hostPath, ok := c.byPath[p]
	if !ok {
		copied, err := c.store.copyIn(p)
		if err != nil {
			return "", false
		}
		hostPath = copied
		c.byPath[p] = hostPath
		c.files = append(c.files, hostPath)
	}
	if isURL {
		return "file://" + hostPath, true
	}
	return hostPath, true
}

// appIcon rewrites the Notify app_icon argument. A path that cannot be copied
// becomes "", so the server shows no icon rather than a broken one.
func (c *iconCopies) appIcon(icon string) string {
	mapped, _ := c.mapPath(icon)
	return mapped
}

// hints returns a copy of hints with the image path hints mapped to host
// copies. A hint whose file cannot be copied is dropped.
func (c *iconCopies) hints(hints map[string]dbus.Variant) map[string]dbus.Variant {
	out := make(map[string]dbus.Variant, len(hints))
	for k, v := range hints {
		out[k] = v
	}
	for _, key := range imagePathHints {
		v, ok := out[key]
		if !ok {
			continue
		}
		p, ok := v.Value().(string)
		if !ok {
			delete(out, key)
			continue
		}
		mapped, ok := c.mapPath(p)
		if !ok {
			delete(out, key)
			continue
		}
		out[key] = dbus.MakeVariant(mapped)
	}
	return out
}
