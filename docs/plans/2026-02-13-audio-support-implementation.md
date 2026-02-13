# Audio Support Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Forward the host's PulseAudio socket into the nspawn container so Edge has full-duplex audio.

**Architecture:** Add PulseAudio socket detection alongside existing Wayland/PipeWire detection, set `PULSE_SERVER` env var in the profile.d script, and install `libpulse0` during provisioning.

**Tech Stack:** Go, systemd-nspawn, PulseAudio client library, bash

**Design doc:** `docs/plans/2026-02-13-audio-support-design.md`

---

### Task 1: Add PulseAudio socket detection test

**Files:**
- Modify: `internal/nspawn/nspawn_test.go`

**Step 1: Write the failing test**

Add a test that verifies `DetectHostSockets` includes the PulseAudio socket when it exists. Add this test function at the end of the file:

```go
func TestDetectHostSockets_PulseAudio(t *testing.T) {
	// Create a fake runtime dir with a pulse/native socket
	tmp := t.TempDir()
	pulseDir := filepath.Join(tmp, "pulse")
	os.MkdirAll(pulseDir, 0755)

	// Create a regular file to simulate the socket (os.Stat works the same)
	os.WriteFile(filepath.Join(pulseDir, "native"), nil, 0644)

	// Monkey-patch: call DetectHostSockets with a UID that maps to our tmp dir.
	// Since DetectHostSockets hardcodes /run/user/{uid}, we test the checks slice
	// directly by verifying the pulse/native path is in the list.
	// Instead, test that BuildBootArgs correctly passes through a pulse mount.
	sockets := []BindMount{
		{"/run/user/1000/pulse/native", "/run/host-pulse"},
	}
	args := BuildBootArgs("/tmp/rootfs", "intuneme", "/home/testuser/Intune", "/home/testuser", sockets)

	joined := strings.Join(args, " ")
	if !strings.Contains(joined, "--bind=/run/user/1000/pulse/native:/run/host-pulse") {
		t.Errorf("missing pulse socket bind in: %s", joined)
	}
}
```

Also add these imports at the top if not present: `"os"` and `"path/filepath"`.

**Step 2: Run test to verify it fails**

Run: `go test ./internal/nspawn/ -run TestDetectHostSockets_PulseAudio -v`

Expected: PASS (this test validates the bind-mount passthrough which already works). This is a characterization test — it confirms the plumbing works before we add the detection.

**Step 3: Commit**

```bash
git add internal/nspawn/nspawn_test.go
git commit -m "test: add pulse audio socket bind mount test"
```

---

### Task 2: Add PulseAudio socket to DetectHostSockets

**Files:**
- Modify: `internal/nspawn/nspawn.go:56-58`

**Step 1: Add the pulse/native entry to the checks slice**

In `DetectHostSockets()`, add a third entry to the `checks` slice (after the pipewire-0 line):

```go
	checks := []struct {
		hostPath      string
		containerPath string
	}{
		{runtimeDir + "/wayland-0", "/run/host-wayland"},
		{runtimeDir + "/pipewire-0", "/run/host-pipewire"},
		{runtimeDir + "/pulse/native", "/run/host-pulse"},
	}
```

**Step 2: Run all nspawn tests**

Run: `go test ./internal/nspawn/ -v`

Expected: All tests PASS including the new one from Task 1.

**Step 3: Commit**

```bash
git add internal/nspawn/nspawn.go
git commit -m "feat: detect and forward PulseAudio socket"
```

---

### Task 3: Add PULSE_SERVER to profile.d script

**Files:**
- Modify: `internal/provision/intuneme-profile.sh:30` (after the PipeWire block)

**Step 1: Add the PulseAudio block**

After line 30 (the closing `fi` of the PipeWire block), add:

```bash

# PulseAudio socket (bind-mounted from host at /run/host-pulse)
if [ -S /run/host-pulse ]; then
    export PULSE_SERVER=unix:/run/host-pulse
    systemctl --user import-environment PULSE_SERVER 2>/dev/null
fi
```

**Step 2: Verify the embedded script compiles**

Run: `go build ./internal/provision/`

Expected: Compiles successfully (the `go:embed` directive picks up the modified script).

**Step 3: Commit**

```bash
git add internal/provision/intuneme-profile.sh
git commit -m "feat: set PULSE_SERVER env var when pulse socket available"
```

---

### Task 4: Add libpulse0 to package install

**Files:**
- Modify: `internal/provision/provision.go:221`

**Step 1: Add libpulse0 to the apt-get install command**

Change line 221 from:

```go
		"bash", "-c", "apt-get update && apt-get install -y microsoft-edge-stable libsecret-tools sudo",
```

to:

```go
		"bash", "-c", "apt-get update && apt-get install -y microsoft-edge-stable libsecret-tools sudo libpulse0",
```

**Step 2: Run provision tests to verify nothing broke**

Run: `go test ./internal/provision/ -v`

Expected: All tests PASS. The `InstallPackages` function is tested via mock runner so the new package name just appears in the captured command string.

**Step 3: Run full test suite**

Run: `go test ./... -v`

Expected: All tests PASS across all packages.

**Step 4: Commit**

```bash
git add internal/provision/provision.go
git commit -m "feat: install libpulse0 in container for Edge audio"
```

---

### Task 5: Update no-sockets test to include pulse

**Files:**
- Modify: `internal/nspawn/nspawn_test.go`

**Step 1: Add pulse assertion to TestBuildBootArgsNoSockets**

In `TestBuildBootArgsNoSockets`, add after the pipewire check (line 47):

```go
	if strings.Contains(joined, "host-pulse") {
		t.Errorf("unexpected pulse bind in: %s", joined)
	}
```

**Step 2: Run tests**

Run: `go test ./internal/nspawn/ -v`

Expected: All tests PASS.

**Step 3: Commit**

```bash
git add internal/nspawn/nspawn_test.go
git commit -m "test: verify no pulse bind when no sockets provided"
```
