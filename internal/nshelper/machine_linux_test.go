//go:build linux

package nshelper

import (
	"errors"
	"fmt"
	"golang.org/x/sys/unix"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
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

// runBindKernelBoundary substitutes only the machined transport: a separate
// process resolves the helper's pinned FD, performs the real kernel bind, and
// verifies it is read-only even after replacing the source pathname.
func runBindKernelBoundary(t *testing.T, dir, fixture string) {
	t.Helper()
	if err := unix.Mount("tmpfs", "/opt", "tmpfs", 0, "mode=0755"); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = unix.Unmount("/opt", 0) }()
	if err := os.Mkdir(MCPMountDir, 0755); err != nil {
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
	transport := filepath.Join(dir, "machinectl")
	script := fmt.Sprintf("#!/bin/sh\nINTUNEME_MOUNT_SOURCE='%s' exec '%s' -test.run=^TestMachineMountChild$ -- \"$@\"\n", source, fixture)
	if err := os.WriteFile(transport, []byte(script), 0755); err != nil {
		t.Fatal(err)
	}
	findExecutable = func(string) (string, error) { return transport, nil }
	commandOutput = func(string, ...string) ([]byte, error) { return []byte(strconv.Itoa(os.Getpid())), nil }
	cfg := &Config{Machine: "test", User: "test", UID: 1000, GID: 1000, Home: "/", Rootfs: "/"}
	if err := runMachine(cfg, []string{"bind", source}); err != nil {
		t.Fatal(err)
	}
	fmt.Println("KERNEL_BOUNDARY_OK")
}

func TestMachineMountChild(t *testing.T) {
	source := os.Getenv("INTUNEME_MOUNT_SOURCE")
	if source == "" {
		t.Skip("subprocess only")
	}
	args := os.Args[len(os.Args)-6:]
	if args[0] != "bind" || args[1] != "--read-only" || args[2] != "--mkdir" || args[3] != "test" || args[5] != MCPMountDir {
		t.Fatalf("unsafe bind arguments: %v", args)
	}
	if !strings.HasPrefix(args[4], "/proc/") || !strings.Contains(args[4], "/fd/") {
		t.Fatalf("source was not pinned: %s", args[4])
	}
	if err := os.Rename(source, source+"-old"); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("/etc", source); err != nil {
		t.Fatal(err)
	}
	if err := unix.Mount(args[4], MCPMountDir, "", unix.MS_BIND, ""); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = unix.Unmount(MCPMountDir, 0) }()
	if err := unix.Mount("", MCPMountDir, "", unix.MS_BIND|unix.MS_REMOUNT|unix.MS_RDONLY, ""); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(MCPMountDir, "marker"))
	if err != nil || string(data) != "original" {
		t.Fatalf("mounted replacement: %s: %v", data, err)
	}
	if err := os.WriteFile(filepath.Join(MCPMountDir, "forbidden"), nil, 0644); !errors.Is(err, unix.EROFS) {
		t.Fatalf("mount not read-only: %v", err)
	}
}
