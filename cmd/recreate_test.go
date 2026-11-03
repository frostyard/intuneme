package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// warningRecorder captures Warning calls for test assertions.
type warningRecorder struct {
	messageRecorder
	warnings []string
}

func (r *warningRecorder) Warning(format string, args ...any) {
	r.warnings = append(r.warnings, fmt.Sprintf(format, args...))
}

// writeRootfsHostname creates <rootfs>/etc/hostname with the given content.
func writeRootfsHostname(t *testing.T, rootfs, content string) {
	t.Helper()
	dir := filepath.Join(rootfs, "etc")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "hostname"), []byte(content), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
}

func TestContainerHostnameFromRootfs(t *testing.T) {
	rootfs := t.TempDir()
	writeRootfsHostname(t, rootfs, "oldhostLXC\n")

	if got := containerHostnameFromRootfs(rootfs); got != "oldhostLXC" {
		t.Errorf("containerHostnameFromRootfs() = %q, want %q", got, "oldhostLXC")
	}
}

func TestContainerHostnameFromRootfsMissing(t *testing.T) {
	if got := containerHostnameFromRootfs(t.TempDir()); got != "" {
		t.Errorf("containerHostnameFromRootfs() = %q, want empty", got)
	}
}

func TestWarnIfHostnameChanges(t *testing.T) {
	tests := []struct {
		name     string
		old      string
		newName  string
		wantWarn bool
	}{
		{name: "changed", old: "oldhostLXC\n", newName: "intune-laptop", wantWarn: true},
		{name: "unchanged", old: "intune-laptop\n", newName: "intune-laptop"},
		{name: "old empty", old: "\n", newName: "intune-laptop"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rootfs := t.TempDir()
			writeRootfsHostname(t, rootfs, tt.old)

			rec := &warningRecorder{}
			warnIfHostnameChanges(rec, rootfs, tt.newName)

			if !tt.wantWarn {
				if len(rec.warnings) != 0 {
					t.Fatalf("unexpected warnings: %v", rec.warnings)
				}
				return
			}
			if len(rec.warnings) != 1 {
				t.Fatalf("warnings = %v, want exactly 1", rec.warnings)
			}
			want := "Container hostname changes from \"oldhostLXC\" to \"intune-laptop\". Intune can show this as a renamed or new device."
			if rec.warnings[0] != want {
				t.Errorf("warning = %q, want %q", rec.warnings[0], want)
			}
			if !strings.Contains(rec.warnings[0], tt.newName) {
				t.Errorf("warning %q does not name the new hostname", rec.warnings[0])
			}
		})
	}
}
