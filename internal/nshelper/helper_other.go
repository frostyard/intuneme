//go:build !linux

package nshelper

import (
	"fmt"
	"os"
	"path/filepath"
)

// IsHelperName reports whether argv[0] refers to the installed helper.
func IsHelperName(argv0 string) bool {
	return filepath.Base(argv0) == "nsenter-exec"
}

// Main is a stub for non-Linux builds. intuneme itself is Linux-only, but
// keeping the package buildable everywhere makes `go test ./...` and editor
// tooling work on other platforms.
func Main(args []string) int {
	fmt.Fprintln(os.Stderr, "nsenter-exec: only supported on Linux")
	return 1
}
