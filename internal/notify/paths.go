package notify

import (
	"os"
	"strconv"
	"strings"

	"github.com/godbus/dbus/v5"
)

// imagePathHints are the Notify hints that carry a file path. Chromium sets
// both spellings: "image-path" is the spec name, "image_path" the deprecated one.
var imagePathHints = []string{"image-path", "image_path"}

// pathRewriter maps file paths inside the container to paths the host can
// read. The container's /tmp is a tmpfs in its own mount namespace, so the
// host reaches it through /proc/<pid>/root of a process in that namespace.
type pathRewriter struct {
	// procRoot is "/proc/<pid>/root" for a process inside the container.
	procRoot string
	// exists reports whether a host path exists. Replaced in tests.
	exists func(string) bool
}

func newPathRewriter(pid int) *pathRewriter {
	return &pathRewriter{
		procRoot: "/proc/" + strconv.Itoa(pid) + "/root",
		exists: func(p string) bool {
			_, err := os.Stat(p)
			return err == nil
		},
	}
}

// mapPath maps a file:// URL or an absolute path into procRoot and keeps the
// URL form. ok is false when the host path does not exist. Anything else, such
// as an icon theme name, is returned unchanged with ok true.
func (r *pathRewriter) mapPath(s string) (string, bool) {
	p, isURL := strings.CutPrefix(s, "file://")
	if !strings.HasPrefix(p, "/") {
		return s, true
	}
	hostPath := r.procRoot + p
	if !r.exists(hostPath) {
		return "", false
	}
	if isURL {
		return "file://" + hostPath, true
	}
	return hostPath, true
}

// appIcon rewrites the Notify app_icon argument. A path that does not exist on
// the host becomes "", so the server shows no icon rather than a broken one.
func (r *pathRewriter) appIcon(icon string) string {
	mapped, _ := r.mapPath(icon)
	return mapped
}

// hints returns a copy of hints with the image path hints mapped into
// procRoot. A hint whose path does not exist on the host is dropped.
func (r *pathRewriter) hints(hints map[string]dbus.Variant) map[string]dbus.Variant {
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
			continue
		}
		mapped, ok := r.mapPath(p)
		if !ok {
			delete(out, key)
			continue
		}
		out[key] = dbus.MakeVariant(mapped)
	}
	return out
}
