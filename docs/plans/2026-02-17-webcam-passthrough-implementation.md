# Webcam Pass-Through Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Detect host webcams at container start and bind-mount `/dev/video*` and `/dev/media*` into the nspawn container so Teams video calls work in Edge.

**Architecture:** Add a `DetectVideoDevices()` function to the nspawn package that globs for V4L2 device nodes and reads their names from sysfs. The start command calls it, logs results, and merges the mounts into the existing bind-mount slice passed to `Boot()`.

**Tech Stack:** Go, systemd-nspawn, V4L2 (`/dev/video*`), sysfs (`/sys/class/video4linux/`)

---

### Task 1: Add DetectVideoDevices function

**Files:**
- Modify: `internal/nspawn/nspawn.go`

**Step 1: Write the failing test**

Add to `internal/nspawn/nspawn_test.go`:

```go
func TestDetectVideoDevices_ReturnsBindMounts(t *testing.T) {
	// DetectVideoDevices depends on real /dev nodes, so we test the
	// return type and that it doesn't error on this machine.
	// It may return empty if no cameras are present.
	devices := DetectVideoDevices()
	for _, d := range devices {
		if d.Mount.Host != d.Mount.Container {
			t.Errorf("video device mount should map to same path: host=%s container=%s", d.Mount.Host, d.Mount.Container)
		}
		if d.Mount.Host == "" {
			t.Error("empty host path in video device mount")
		}
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test -v -run TestDetectVideoDevices_ReturnsBindMounts ./internal/nspawn/`
Expected: FAIL — `DetectVideoDevices` not defined.

**Step 3: Write the implementation**

Add to `internal/nspawn/nspawn.go`:

```go
// VideoDevice represents a detected video device with its bind mount and display name.
type VideoDevice struct {
	Mount BindMount
	Name  string // human-readable name from sysfs, e.g. "Integrated Camera"
}

// DetectVideoDevices scans for V4L2 video and media controller devices.
// Returns an empty slice if no devices are found (cameras are optional).
func DetectVideoDevices() []VideoDevice {
	var devices []VideoDevice

	// Detect /dev/video* devices
	videoMatches, _ := filepath.Glob("/dev/video*")
	for _, dev := range videoMatches {
		name := readSysfsName(dev)
		devices = append(devices, VideoDevice{
			Mount: BindMount{dev, dev},
			Name:  name,
		})
	}

	// Detect /dev/media* devices (media controller nodes associated with cameras)
	mediaMatches, _ := filepath.Glob("/dev/media*")
	for _, dev := range mediaMatches {
		devices = append(devices, VideoDevice{
			Mount: BindMount{dev, dev},
			Name:  "",
		})
	}

	return devices
}

// readSysfsName reads the human-readable device name from sysfs.
// Returns the base device name if sysfs is unavailable.
func readSysfsName(devPath string) string {
	base := filepath.Base(devPath)
	data, err := os.ReadFile(filepath.Join("/sys/class/video4linux", base, "name"))
	if err != nil {
		return base
	}
	return strings.TrimSpace(string(data))
}
```

Add `"strings"` to the imports in `nspawn.go`.

**Step 4: Run test to verify it passes**

Run: `go test -v -run TestDetectVideoDevices_ReturnsBindMounts ./internal/nspawn/`
Expected: PASS

**Step 5: Run full test suite**

Run: `make test`
Expected: All tests pass.

**Step 6: Commit**

```bash
git add internal/nspawn/nspawn.go internal/nspawn/nspawn_test.go
git commit -m "feat: add video device detection for webcam pass-through"
```

---

### Task 2: Integrate detection into start command

**Files:**
- Modify: `cmd/start.go`

**Step 1: Write the integration code**

In `cmd/start.go`, after `sockets := nspawn.DetectHostSockets(cfg.HostUID)` (line 42), add:

```go
		videoDev := nspawn.DetectVideoDevices()
		if len(videoDev) > 0 {
			for _, d := range videoDev {
				if d.Name != "" {
					fmt.Printf("Detected webcam: %s (%s)\n", d.Mount.Host, d.Name)
				}
				sockets = append(sockets, d.Mount)
			}
		} else {
			fmt.Println("No webcams detected")
		}
```

This appends video device mounts to the existing `sockets` slice. No changes to `BuildBootArgs` or `Boot` needed — they already handle arbitrary `[]BindMount`.

**Step 2: Run the full test suite**

Run: `make test`
Expected: All tests pass.

**Step 3: Run fmt and lint**

Run: `make fmt && make lint`
Expected: No errors.

**Step 4: Commit**

```bash
git add cmd/start.go
git commit -m "feat: detect and bind-mount webcams on container start"
```

---

### Task 3: Manual verification

**Step 1: Build**

Run: `make build`
Expected: Clean build, no errors.

**Step 2: Check detection output (if a webcam is available)**

Run: `ls /dev/video*` to confirm device nodes exist on this machine.

If devices exist, the start command should print something like:
```
Detected webcam: /dev/video0 (Integrated Camera)
```

If no devices, it should print:
```
No webcams detected
```
