package nvidia

import (
	"bufio"
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/frostyard/intuneme/internal/nspawn"
)

// LibMapping maps a library basename to its absolute host path.
type LibMapping struct {
	Basename string // e.g. "libcuda.so.560.35.03"
	HostPath string // e.g. "/usr/lib/x86_64-linux-gnu/libcuda.so.560.35.03"
}

// IsPresent reports whether an Nvidia GPU is available on the host.
func IsPresent() bool {
	_, err := os.Stat("/dev/nvidiactl")
	return err == nil
}

// DetectDevices returns bind mounts for all Nvidia device nodes.
// Each device is bound to the same path inside the container.
func DetectDevices() []nspawn.BindMount {
	patterns := []string{
		"/dev/nvidia[0-9]*",
		"/dev/nvidiactl",
		"/dev/nvidia-modeset",
		"/dev/nvidia-uvm",
		"/dev/nvidia-uvm-tools",
		"/dev/nvidia-caps/*",
	}
	var mounts []nspawn.BindMount
	for _, pattern := range patterns {
		matches, _ := filepath.Glob(pattern)
		for _, dev := range matches {
			mounts = append(mounts, nspawn.BindMount{Host: dev, Container: dev})
		}
	}
	return mounts
}

// nvidiaLibPrefixes are the library name prefixes we look for in ldconfig output.
var nvidiaLibPrefixes = []string{
	"libnvidia-",
	"libcuda",
	"libEGL_nvidia",
	"libGLX_nvidia",
	"libGLESv2_nvidia",
	"libnvcuvid",
	"libnvoptix",
}

// HostLibraries parses ldconfig -p output and returns Nvidia library mappings.
// Only x86-64 libraries are included to avoid multilib collisions.
func HostLibraries(ldconfigOutput []byte) []LibMapping {
	var libs []LibMapping
	seen := make(map[string]bool)

	scanner := bufio.NewScanner(bytes.NewReader(ldconfigOutput))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		// ldconfig -p lines look like:
		//   libcuda.so.1 (libc6,x86-64) => /usr/lib/x86_64-linux-gnu/libcuda.so.1
		if !strings.Contains(line, "=>") {
			continue
		}
		// Filter to x86-64 only.
		if !strings.Contains(line, "x86-64") {
			continue
		}
		// Check if this is an Nvidia library.
		isNvidia := false
		for _, prefix := range nvidiaLibPrefixes {
			if strings.HasPrefix(line, prefix) {
				isNvidia = true
				break
			}
		}
		if !isNvidia {
			continue
		}
		// Extract the path after "=> ".
		parts := strings.SplitN(line, "=> ", 2)
		if len(parts) != 2 {
			continue
		}
		hostPath := strings.TrimSpace(parts[1])
		if hostPath == "" {
			continue
		}
		basename := filepath.Base(hostPath)
		// Deduplicate by basename — first match wins (ldconfig orders by priority).
		if seen[basename] {
			continue
		}
		seen[basename] = true
		libs = append(libs, LibMapping{Basename: basename, HostPath: hostPath})
	}
	return libs
}

// LibDirMounts returns bind mounts for the unique host directories containing
// Nvidia libraries. Each directory is mounted read-only at /run/host-nvidia/<index>/
// inside the container, using an index to avoid basename collisions.
func LibDirMounts(libs []LibMapping) []nspawn.BindMount {
	seen := make(map[string]int)
	var mounts []nspawn.BindMount
	idx := 0
	for _, lib := range libs {
		dir := filepath.Dir(lib.HostPath)
		if _, ok := seen[dir]; ok {
			continue
		}
		seen[dir] = idx
		containerPath := fmt.Sprintf("/run/host-nvidia/%d", idx)
		mounts = append(mounts, nspawn.BindMount{Host: dir, Container: containerPath, ReadOnly: true})
		idx++
	}
	return mounts
}

// HostICDFiles returns paths to Nvidia vendor JSON files that exist on the host.
// These are needed for Vulkan and EGL vendor dispatch.
func HostICDFiles() []string {
	candidates := []string{
		"/usr/share/vulkan/icd.d/nvidia_icd.json",
		"/usr/share/glvnd/egl_vendor.d/10_nvidia.json",
	}
	// Also glob for platform-specific EGL configs.
	eglPlatformGlob, _ := filepath.Glob("/usr/share/egl/egl_external_platform.d/*nvidia*.json")

	var found []string
	for _, f := range candidates {
		if _, err := os.Stat(f); err == nil {
			found = append(found, f)
		}
	}
	found = append(found, eglPlatformGlob...)
	return found
}

// ICDMounts returns bind mounts for ICD JSON files. Each file is mounted at
// the same path inside the container (Nvidia-specific filenames don't conflict
// with Mesa files).
func ICDMounts(files []string) []nspawn.BindMount {
	var mounts []nspawn.BindMount
	for _, f := range files {
		mounts = append(mounts, nspawn.BindMount{Host: f, Container: f, ReadOnly: true})
	}
	return mounts
}

// libDirIndex returns a map from host directory to its mount index,
// matching the layout produced by LibDirMounts.
func libDirIndex(libs []LibMapping) map[string]int {
	idx := 0
	m := make(map[string]int)
	for _, lib := range libs {
		dir := filepath.Dir(lib.HostPath)
		if _, ok := m[dir]; ok {
			continue
		}
		m[dir] = idx
		idx++
	}
	return m
}
