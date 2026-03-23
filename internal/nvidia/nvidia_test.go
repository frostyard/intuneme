package nvidia

import (
	"testing"

	"github.com/frostyard/intuneme/internal/nspawn"
)

func TestHostLibraries(t *testing.T) {
	ldconfigOutput := []byte(`	linux-vdso.so.1 (LINUX_VDSO) => linux-vdso.so.1
	libcuda.so.1 (libc6,x86-64) => /usr/lib/x86_64-linux-gnu/libcuda.so.1
	libcuda.so.1 (libc6) => /usr/lib/i386-linux-gnu/libcuda.so.1
	libnvidia-glcore.so.560.35.03 (libc6,x86-64) => /usr/lib/x86_64-linux-gnu/libnvidia-glcore.so.560.35.03
	libEGL_nvidia.so.0 (libc6,x86-64) => /usr/lib/x86_64-linux-gnu/libEGL_nvidia.so.0
	libGLX_nvidia.so.0 (libc6,x86-64) => /usr/lib/x86_64-linux-gnu/libGLX_nvidia.so.0
	libGLESv2_nvidia.so.2 (libc6,x86-64) => /usr/lib/x86_64-linux-gnu/libGLESv2_nvidia.so.2
	libnvcuvid.so.1 (libc6,x86-64) => /usr/lib/x86_64-linux-gnu/libnvcuvid.so.1
	libnvoptix.so.1 (libc6,x86-64) => /usr/lib/x86_64-linux-gnu/libnvoptix.so.1
	libpthread.so.0 (libc6,x86-64) => /lib/x86_64-linux-gnu/libpthread.so.0
`)

	libs := HostLibraries(ldconfigOutput)

	// Should have 7 Nvidia libs (the 32-bit libcuda should be filtered out).
	if got := len(libs); got != 7 {
		t.Fatalf("HostLibraries() returned %d libs, want 7", got)
	}

	// Verify first entry.
	if libs[0].Basename != "libcuda.so.1" {
		t.Errorf("libs[0].Basename = %q, want %q", libs[0].Basename, "libcuda.so.1")
	}
	if libs[0].HostPath != "/usr/lib/x86_64-linux-gnu/libcuda.so.1" {
		t.Errorf("libs[0].HostPath = %q, want %q", libs[0].HostPath, "/usr/lib/x86_64-linux-gnu/libcuda.so.1")
	}

	// Verify no 32-bit libs.
	for _, lib := range libs {
		if lib.HostPath == "/usr/lib/i386-linux-gnu/libcuda.so.1" {
			t.Errorf("32-bit libcuda should have been filtered out")
		}
	}

	// Verify non-Nvidia libs are excluded.
	for _, lib := range libs {
		if lib.Basename == "libpthread.so.0" {
			t.Errorf("non-Nvidia library libpthread should have been excluded")
		}
	}
}

func TestHostLibraries_Empty(t *testing.T) {
	libs := HostLibraries([]byte(""))
	if len(libs) != 0 {
		t.Errorf("HostLibraries(empty) returned %d libs, want 0", len(libs))
	}

	libs = HostLibraries([]byte("some garbage output\nno libraries here"))
	if len(libs) != 0 {
		t.Errorf("HostLibraries(garbage) returned %d libs, want 0", len(libs))
	}
}

func TestHostLibraries_DeduplicatesByBasename(t *testing.T) {
	ldconfigOutput := []byte(`	libcuda.so.1 (libc6,x86-64) => /usr/lib/x86_64-linux-gnu/libcuda.so.1
	libcuda.so.1 (libc6,x86-64) => /usr/lib64/libcuda.so.1
`)
	libs := HostLibraries(ldconfigOutput)
	if got := len(libs); got != 1 {
		t.Fatalf("HostLibraries() returned %d libs, want 1 (deduped)", got)
	}
	// First match wins.
	if libs[0].HostPath != "/usr/lib/x86_64-linux-gnu/libcuda.so.1" {
		t.Errorf("expected first match to win, got %q", libs[0].HostPath)
	}
}

func TestLibDirMounts(t *testing.T) {
	libs := []LibMapping{
		{Basename: "libcuda.so.1", HostPath: "/usr/lib/x86_64-linux-gnu/libcuda.so.1"},
		{Basename: "libnvidia-glcore.so.560", HostPath: "/usr/lib/x86_64-linux-gnu/libnvidia-glcore.so.560"},
		{Basename: "libnvoptix.so.1", HostPath: "/usr/lib64/libnvoptix.so.1"},
	}

	mounts := LibDirMounts(libs)

	// Two unique directories.
	if got := len(mounts); got != 2 {
		t.Fatalf("LibDirMounts() returned %d mounts, want 2", got)
	}

	// First directory.
	if mounts[0].Host != "/usr/lib/x86_64-linux-gnu" {
		t.Errorf("mounts[0].Host = %q, want /usr/lib/x86_64-linux-gnu", mounts[0].Host)
	}
	if mounts[0].Container != "/run/host-nvidia/0" {
		t.Errorf("mounts[0].Container = %q, want /run/host-nvidia/0", mounts[0].Container)
	}

	// Second directory.
	if mounts[1].Host != "/usr/lib64" {
		t.Errorf("mounts[1].Host = %q, want /usr/lib64", mounts[1].Host)
	}
	if mounts[1].Container != "/run/host-nvidia/1" {
		t.Errorf("mounts[1].Container = %q, want /run/host-nvidia/1", mounts[1].Container)
	}
}

func TestLibDirMounts_SingleDirectory(t *testing.T) {
	libs := []LibMapping{
		{Basename: "libcuda.so.1", HostPath: "/usr/lib64/libcuda.so.1"},
		{Basename: "libnvidia-glcore.so", HostPath: "/usr/lib64/libnvidia-glcore.so"},
	}

	mounts := LibDirMounts(libs)
	if got := len(mounts); got != 1 {
		t.Fatalf("LibDirMounts() returned %d mounts, want 1", got)
	}
}

func TestICDMounts(t *testing.T) {
	files := []string{
		"/usr/share/vulkan/icd.d/nvidia_icd.json",
		"/usr/share/glvnd/egl_vendor.d/10_nvidia.json",
	}

	mounts := ICDMounts(files)
	if got := len(mounts); got != 2 {
		t.Fatalf("ICDMounts() returned %d mounts, want 2", got)
	}
	// ICD files bind to the same path.
	for _, m := range mounts {
		if m.Host != m.Container {
			t.Errorf("ICDMount host %q != container %q, expected same path", m.Host, m.Container)
		}
	}
}

func TestICDMounts_Empty(t *testing.T) {
	mounts := ICDMounts(nil)
	if len(mounts) != 0 {
		t.Errorf("ICDMounts(nil) returned %d mounts, want 0", len(mounts))
	}
}

func TestLibDirIndex(t *testing.T) {
	libs := []LibMapping{
		{Basename: "libcuda.so.1", HostPath: "/usr/lib/x86_64-linux-gnu/libcuda.so.1"},
		{Basename: "libnvoptix.so.1", HostPath: "/usr/lib64/libnvoptix.so.1"},
		{Basename: "libnvidia-glcore.so", HostPath: "/usr/lib/x86_64-linux-gnu/libnvidia-glcore.so"},
	}

	idx := libDirIndex(libs)
	if got := len(idx); got != 2 {
		t.Fatalf("libDirIndex() returned %d entries, want 2", got)
	}
	if idx["/usr/lib/x86_64-linux-gnu"] != 0 {
		t.Errorf("expected /usr/lib/x86_64-linux-gnu at index 0, got %d", idx["/usr/lib/x86_64-linux-gnu"])
	}
	if idx["/usr/lib64"] != 1 {
		t.Errorf("expected /usr/lib64 at index 1, got %d", idx["/usr/lib64"])
	}
}

func TestDetectDevices_ReturnsBindMounts(t *testing.T) {
	// DetectDevices globs real /dev paths; we can't easily mock the filesystem.
	// Just verify it returns the right type and doesn't panic.
	devs := DetectDevices()
	for _, d := range devs {
		if d.Host != d.Container {
			t.Errorf("Nvidia device mount host %q != container %q, expected same path", d.Host, d.Container)
		}
	}
	_ = []nspawn.BindMount(devs) // Verify type compatibility.
}
