//go:build linux

package nshelper

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"golang.org/x/sys/unix"
)

func TestBindSourceRejectsSymlinksAndWrongOwner(t *testing.T) {
	dir := t.TempDir()
	for _, target := range []string{dir, "/etc"} {
		link := filepath.Join(t.TempDir(), "link")
		if err := os.Symlink(target, link); err != nil {
			t.Fatal(err)
		}
		for _, path := range []string{link, filepath.Join(link, "child")} {
			if file, err := pinBindSource(path, uint32(os.Getuid())); err == nil {
				_ = file.Close()
				t.Fatalf("accepted symlink %s", path)
			}
		}
	}
	if file, err := pinBindSource(dir, uint32(os.Getuid()+1)); err == nil {
		_ = file.Close()
		t.Fatal("accepted wrong owner")
	}
	for _, path := range []string{"relative", "/tmp/../etc"} {
		if file, err := pinBindSource(path, uint32(os.Getuid())); err == nil {
			_ = file.Close()
			t.Fatalf("accepted %s", path)
		}
	}
}

func TestBindSourcePinsDirectoryAcrossReplacement(t *testing.T) {
	parent := t.TempDir()
	source := filepath.Join(parent, "source")
	if err := os.Mkdir(source, 0700); err != nil {
		t.Fatal(err)
	}
	file, err := pinBindSource(source, uint32(os.Getuid()))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = file.Close() }()
	before, err := file.Stat()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(source, source+"-old"); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("/etc", source); err != nil {
		t.Fatal(err)
	}
	after, err := file.Stat()
	if err != nil {
		t.Fatal(err)
	}
	if !os.SameFile(before, after) {
		t.Fatal("descriptor followed replacement")
	}
	target, err := os.Stat(source)
	if err != nil {
		t.Fatal(err)
	}
	if os.SameFile(after, target) {
		t.Fatal("descriptor now references /etc")
	}
}

func TestMachineHelperRejectsUnscopedOperations(t *testing.T) {
	cfg := testConfig(t)
	for _, args := range [][]string{nil, {"exec", "id"}, {"stop", "other"}, {"bind"}, {"bind", "/tmp", "/etc"}} {
		if err := runMachine(cfg, args); err == nil {
			t.Fatalf("accepted %v", args)
		}
	}
}

// Exercise the native kernel mount operations on pinned descriptors. All
// mounts live in the disposable test namespace, never on the real host.
func runBindKernelBoundary(t *testing.T, dir string) {
	t.Helper()
	if err := unix.Mount("tmpfs", "/opt", "tmpfs", 0, "mode=0755"); err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(dir, "source")
	if err := os.Mkdir(source, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chown(source, 1000, 1000); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "marker"), []byte("original"), 0644); err != nil {
		t.Fatal(err)
	}
	findExecutable = func(string) (string, error) { return "/unused", nil }
	commandOutput = func(string, ...string) ([]byte, error) { return []byte(strconv.Itoa(os.Getpid())), nil }
	cfg := &Config{Machine: "test", User: "test", UID: 1000, GID: 1000, Home: "/", Rootfs: "/"}
	wrongRoot := *cfg
	wrongRoot.Rootfs = dir
	if err := runMachine(&wrongRoot, []string{"bind", source}); err == nil {
		t.Fatal("accepted mismatched target root")
	}
	if err := runMachine(cfg, []string{"bind", source}); err != nil {
		t.Fatal(err)
	}
	assertReadOnlyMCP(t)
	if err := unix.Unmount(MCPMountDir, 0); err != nil {
		t.Fatal(err)
	}
	// Force the replacement precisely after source validation, before mounting.
	pinned, err := pinBindSource(source, 1000)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = pinned.Close() }()
	if err := os.Rename(source, source+"-old"); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("/etc", source); err != nil {
		t.Fatal(err)
	}
	mount, err := cloneReadOnlyMount(pinned)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if mount >= 0 {
			_ = unix.Close(mount)
		}
	}()
	root, err := unix.Open("/", unix.O_PATH|unix.O_DIRECTORY, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = unix.Close(root) }()
	if err := attachMCPMount(mount, root); err != nil {
		t.Fatal(err)
	}
	assertReadOnlyMCP(t)
	if err := unix.Close(mount); err != nil {
		t.Fatal(err)
	}
	mount = -1
	if err := unix.Unmount(MCPMountDir, 0); err != nil {
		t.Fatalf("unmount pinned clone: %v", err)
	}
	if err := os.Remove(MCPMountDir); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("/etc", MCPMountDir); err != nil {
		t.Fatal(err)
	}
	mount, err = cloneReadOnlyMount(pinned)
	if err != nil {
		t.Fatal(err)
	}
	if err := attachMCPMount(mount, root); err == nil {
		t.Fatal("followed destination symlink")
	}
	fmt.Println("KERNEL_BOUNDARY_OK")
}

func assertReadOnlyMCP(t *testing.T) {
	t.Helper()
	var stat unix.Statfs_t
	if err := unix.Statfs(MCPMountDir, &stat); err != nil {
		t.Fatal(err)
	}
	want := int64(unix.ST_RDONLY | unix.ST_NOSUID | unix.ST_NODEV)
	if stat.Flags&want != want {
		t.Fatalf("unsafe mount flags: %#x", stat.Flags)
	}
	data, err := os.ReadFile(filepath.Join(MCPMountDir, "marker"))
	if err != nil || string(data) != "original" {
		t.Fatalf("mounted replacement: %s: %v", data, err)
	}
	if err := os.WriteFile(filepath.Join(MCPMountDir, "forbidden"), nil, 0644); !errors.Is(err, unix.EROFS) {
		t.Fatalf("mount not read-only: %v", err)
	}
}
