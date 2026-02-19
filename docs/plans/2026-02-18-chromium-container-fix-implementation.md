# Chromium Container Fix Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Fix Chromium-based apps (Edge, Azure VPN Client) crashing in the nspawn container by adding the `render` group for GPU access and configuring Chromium flags for container compatibility.

**Architecture:** Two-pronged fix: (1) Go provisioning detects the host's `render` group GID, creates a matching group in the container, and adds the user to it. (2) Container image wrapper scripts add `--disable-gpu-sandbox` to Chromium apps. Changes split per project convention — host-dependent logic in Go CLI, static config in container image.

**Tech Stack:** Go, systemd-nspawn, shell scripts, `/etc/group` parsing

---

### Task 1: Add `findGroupGID` function and tests

**Files:**
- Modify: `internal/provision/provision.go`
- Modify: `internal/provision/provision_test.go`

**Step 1: Write the failing tests**

Add to `provision_test.go`:

```go
func TestFindGroupGID(t *testing.T) {
	cases := []struct {
		name    string
		content string
		group   string
		want    int
	}{
		{
			name:    "found",
			content: "root:x:0:\nvideo:x:44:\nrender:x:991:\n",
			group:   "render",
			want:    991,
		},
		{
			name:    "not found",
			content: "root:x:0:\nvideo:x:44:\n",
			group:   "render",
			want:    -1,
		},
		{
			name:    "empty file",
			content: "",
			group:   "render",
			want:    -1,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tmp := filepath.Join(t.TempDir(), "group")
			os.WriteFile(tmp, []byte(tc.content), 0644)
			got, err := findGroupGID(tmp, tc.group)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tc.want {
				t.Errorf("findGroupGID(%q) = %d, want %d", tc.group, got, tc.want)
			}
		})
	}
}
```

**Step 2: Run tests to verify they fail**

Run: `go test ./internal/provision/ -run TestFindGroupGID -v`
Expected: FAIL — `findGroupGID` is undefined.

**Step 3: Implement `findGroupGID`**

Add to `provision.go` (add `"strconv"` to imports):

```go
// findGroupGID reads a group file and returns the GID for a given group name.
// Returns -1 if the group is not found.
func findGroupGID(groupPath, name string) (int, error) {
	data, err := os.ReadFile(groupPath)
	if err != nil {
		return -1, err
	}
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Split(line, ":")
		if len(fields) >= 3 && fields[0] == name {
			gid, err := strconv.Atoi(fields[2])
			if err != nil {
				return -1, fmt.Errorf("parse GID for %s: %w", name, err)
			}
			return gid, nil
		}
	}
	return -1, nil
}
```

**Step 4: Run tests to verify they pass**

Run: `go test ./internal/provision/ -run TestFindGroupGID -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/provision/provision.go internal/provision/provision_test.go
git commit -m "feat: add findGroupGID to parse group files"
```

---

### Task 2: Add `EnsureRenderGroup` function and tests

**Files:**
- Modify: `internal/provision/provision.go`
- Modify: `internal/provision/provision_test.go`

**Step 1: Write the failing tests**

Add to `provision_test.go`:

```go
func TestEnsureRenderGroup(t *testing.T) {
	t.Run("group missing creates it", func(t *testing.T) {
		tmp := t.TempDir()
		groupFile := filepath.Join(tmp, "etc", "group")
		os.MkdirAll(filepath.Dir(groupFile), 0755)
		os.WriteFile(groupFile, []byte("root:x:0:\nvideo:x:44:\n"), 0644)

		r := &mockRunner{}
		err := EnsureRenderGroup(r, tmp, 991)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(r.commands) != 1 {
			t.Fatalf("expected 1 command, got %d: %v", len(r.commands), r.commands)
		}
		cmd := r.commands[0]
		if !strings.Contains(cmd, "groupadd") || !strings.Contains(cmd, "991") {
			t.Errorf("expected groupadd with GID 991, got: %s", cmd)
		}
	})

	t.Run("group exists with correct GID is noop", func(t *testing.T) {
		tmp := t.TempDir()
		groupFile := filepath.Join(tmp, "etc", "group")
		os.MkdirAll(filepath.Dir(groupFile), 0755)
		os.WriteFile(groupFile, []byte("root:x:0:\nrender:x:991:\n"), 0644)

		r := &mockRunner{}
		err := EnsureRenderGroup(r, tmp, 991)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(r.commands) != 0 {
			t.Errorf("expected no commands for matching GID, got: %v", r.commands)
		}
	})

	t.Run("group exists with wrong GID modifies it", func(t *testing.T) {
		tmp := t.TempDir()
		groupFile := filepath.Join(tmp, "etc", "group")
		os.MkdirAll(filepath.Dir(groupFile), 0755)
		os.WriteFile(groupFile, []byte("root:x:0:\nrender:x:500:\n"), 0644)

		r := &mockRunner{}
		err := EnsureRenderGroup(r, tmp, 991)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(r.commands) != 1 {
			t.Fatalf("expected 1 command, got %d: %v", len(r.commands), r.commands)
		}
		cmd := r.commands[0]
		if !strings.Contains(cmd, "groupmod") || !strings.Contains(cmd, "991") {
			t.Errorf("expected groupmod with GID 991, got: %s", cmd)
		}
	})
}
```

**Step 2: Run tests to verify they fail**

Run: `go test ./internal/provision/ -run TestEnsureRenderGroup -v`
Expected: FAIL — `EnsureRenderGroup` is undefined.

**Step 3: Implement `EnsureRenderGroup`**

Add to `provision.go`:

```go
// EnsureRenderGroup ensures a "render" group with the given GID exists in the container.
// If the group is missing it is created; if it exists with a different GID it is modified.
func EnsureRenderGroup(r runner.Runner, rootfsPath string, gid int) error {
	containerGroupPath := filepath.Join(rootfsPath, "etc", "group")
	existingGID, err := findGroupGID(containerGroupPath, "render")
	if err != nil {
		return fmt.Errorf("check container render group: %w", err)
	}

	if existingGID == gid {
		return nil
	}

	gidStr := fmt.Sprintf("%d", gid)
	if existingGID >= 0 {
		return r.RunAttached("sudo", "systemd-nspawn", "--console=pipe", "-D", rootfsPath,
			"groupmod", "--gid", gidStr, "render")
	}
	return r.RunAttached("sudo", "systemd-nspawn", "--console=pipe", "-D", rootfsPath,
		"groupadd", "--gid", gidStr, "render")
}
```

**Step 4: Run tests to verify they pass**

Run: `go test ./internal/provision/ -run TestEnsureRenderGroup -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/provision/provision.go internal/provision/provision_test.go
git commit -m "feat: add EnsureRenderGroup for GPU access in container"
```

---

### Task 3: Add `render` to user groups dynamically

**Files:**
- Modify: `internal/provision/provision.go`
- Modify: `internal/provision/provision_test.go`

**Step 1: Write the failing test**

Add to `provision_test.go`:

```go
func TestCreateContainerUserIncludesRender(t *testing.T) {
	tmp := t.TempDir()
	// Set up minimal rootfs with passwd and group files
	etcDir := filepath.Join(tmp, "etc")
	os.MkdirAll(etcDir, 0755)
	os.WriteFile(filepath.Join(etcDir, "passwd"), []byte("root:x:0:0:root:/root:/bin/bash\n"), 0644)
	os.WriteFile(filepath.Join(etcDir, "group"), []byte("root:x:0:\nrender:x:991:\n"), 0644)

	r := &mockRunner{}
	err := CreateContainerUser(r, tmp, "alice", 1000, 1000)
	if err != nil {
		t.Fatalf("CreateContainerUser error: %v", err)
	}

	allCmds := strings.Join(r.commands, "\n")
	if !strings.Contains(allCmds, "render") {
		t.Errorf("expected 'render' in group list, commands:\n%s", allCmds)
	}
}

func TestCreateContainerUserNoRenderGroupSkipsIt(t *testing.T) {
	tmp := t.TempDir()
	etcDir := filepath.Join(tmp, "etc")
	os.MkdirAll(etcDir, 0755)
	os.WriteFile(filepath.Join(etcDir, "passwd"), []byte("root:x:0:0:root:/root:/bin/bash\n"), 0644)
	os.WriteFile(filepath.Join(etcDir, "group"), []byte("root:x:0:\nvideo:x:44:\n"), 0644)

	r := &mockRunner{}
	err := CreateContainerUser(r, tmp, "alice", 1000, 1000)
	if err != nil {
		t.Fatalf("CreateContainerUser error: %v", err)
	}

	allCmds := strings.Join(r.commands, "\n")
	if strings.Contains(allCmds, "render") {
		t.Errorf("expected no 'render' when group absent, commands:\n%s", allCmds)
	}
}
```

**Step 2: Run tests to verify they fail**

Run: `go test ./internal/provision/ -run TestCreateContainerUser -v`
Expected: FAIL — groups don't include `render` yet.

**Step 3: Implement dynamic group list**

Replace the three hardcoded group strings in `CreateContainerUser` with a helper:

```go
const baseGroups = "adm,sudo,video,audio"

// userGroups returns the group list for the container user.
// Includes "render" if a render group exists in the container.
func userGroups(rootfsPath string) string {
	containerGroupPath := filepath.Join(rootfsPath, "etc", "group")
	gid, _ := findGroupGID(containerGroupPath, "render")
	if gid >= 0 {
		return baseGroups + ",render"
	}
	return baseGroups
}
```

Then in `CreateContainerUser`, replace all three occurrences of `"adm,sudo,video,audio"` with `userGroups(rootfsPath)`. The three sites are:
- Line 159: `"usermod", "--groups", userGroups(rootfsPath), "--append", user,`
- Line 170: `"--groups", userGroups(rootfsPath),`
- Line 178: `"usermod", "--groups", userGroups(rootfsPath), "--append", user,`

**Step 4: Run tests to verify they pass**

Run: `go test ./internal/provision/ -run TestCreateContainerUser -v`
Expected: PASS

**Step 5: Run all provision tests**

Run: `go test ./internal/provision/ -v`
Expected: All PASS (existing tests still pass)

**Step 6: Commit**

```bash
git add internal/provision/provision.go internal/provision/provision_test.go
git commit -m "feat: dynamically include render group in container user setup"
```

---

### Task 4: Call `EnsureRenderGroup` from init and recreate commands

**Files:**
- Modify: `cmd/init.go`
- Modify: `cmd/recreate.go`

**Step 1: Add render group setup to `cmd/init.go`**

Add this block just before the `"Creating container user..."` line (before line 84):

```go
		// Ensure container has a render group matching the host for GPU access
		renderGID, err := provision.FindHostRenderGID()
		if err == nil && renderGID >= 0 {
			fmt.Println("Configuring GPU render group...")
			if err := provision.EnsureRenderGroup(r, cfg.RootfsPath, renderGID); err != nil {
				fmt.Fprintf(os.Stderr, "warning: render group setup failed: %v\n", err)
			}
		}
```

**Step 2: Add exported `FindHostRenderGID` to `provision.go`**

```go
// FindHostRenderGID returns the GID of the host's "render" group, or -1 if not found.
func FindHostRenderGID() (int, error) {
	return findGroupGID("/etc/group", "render")
}
```

**Step 3: Add render group setup to `cmd/recreate.go`**

Add the same block before `CreateContainerUser` in `recreate.go` (before line 109). The exact same code:

```go
		renderGID, err := provision.FindHostRenderGID()
		if err == nil && renderGID >= 0 {
			fmt.Println("Configuring GPU render group...")
			if err := provision.EnsureRenderGroup(r, cfg.RootfsPath, renderGID); err != nil {
				fmt.Fprintf(os.Stderr, "warning: render group setup failed: %v\n", err)
			}
		}
```

Note: in `recreate.go`, the `err` variable is already used (check the exact variable scoping — may need `:=` vs `=` or a fresh scope).

**Step 4: Verify it compiles**

Run: `go build ./...`
Expected: Clean build, no errors.

**Step 5: Commit**

```bash
git add cmd/init.go cmd/recreate.go internal/provision/provision.go
git commit -m "feat: configure render group during init and recreate"
```

---

### Task 5: Add `--disable-gpu-sandbox` to Edge wrapper

**Files:**
- Modify: `ubuntu-intune/system_files/usr/local/bin/microsoft-edge`

**Step 1: Update the wrapper script**

Replace the contents of the Edge wrapper with:

```sh
#!/bin/sh -e

# Container-safe Chromium flags: disable the GPU process sandbox which
# fails inside nspawn (cannot create nested user namespaces).
set -- '--disable-gpu-sandbox' "$@"

if [ -n "$WAYLAND_DISPLAY" ]
then
    unset -v DISPLAY
    set -- \
        '--enable-features=UseOzonePlatform' \
        '--enable-features=WebRTCPipeWireCapturer' \
        '--ozone-platform=wayland' \
        "$@"
fi

/usr/bin/microsoft-edge "$@"
```

**Step 2: Commit**

```bash
git add ubuntu-intune/system_files/usr/local/bin/microsoft-edge
git commit -m "feat: add --disable-gpu-sandbox to Edge wrapper for container compatibility"
```

---

### Task 6: Create Azure VPN Client wrapper

**Files:**
- Create: `ubuntu-intune/system_files/usr/local/bin/microsoft-azurevpnclient`

**Step 1: Create the wrapper script**

```sh
#!/bin/sh -e

# Container-safe Chromium flags: disable the GPU process sandbox which
# fails inside nspawn (cannot create nested user namespaces).
set -- '--disable-gpu-sandbox' "$@"

if [ -n "$WAYLAND_DISPLAY" ]
then
    unset -v DISPLAY
    set -- \
        '--enable-features=UseOzonePlatform' \
        '--ozone-platform=wayland' \
        "$@"
fi

/opt/microsoft/microsoft-azurevpnclient/microsoft-azurevpnclient "$@"
```

Note: No `WebRTCPipeWireCapturer` — the VPN client doesn't do screen capture.

**Step 2: Make it executable**

Run: `chmod +x ubuntu-intune/system_files/usr/local/bin/microsoft-azurevpnclient`

**Step 3: Commit**

```bash
git add ubuntu-intune/system_files/usr/local/bin/microsoft-azurevpnclient
git commit -m "feat: add Azure VPN Client wrapper with container-safe flags"
```

---

### Task 7: Add VPN client to PATH

**Files:**
- Modify: `internal/provision/intuneme-profile.sh`

**Step 1: Update the PATH export**

Change line 10 from:
```bash
export PATH="/opt/microsoft/intune/bin:$PATH"
```
to:
```bash
export PATH="/opt/microsoft/intune/bin:/opt/microsoft/microsoft-azurevpnclient:$PATH"
```

**Step 2: Also update the `systemctl --user import-environment` line**

No change needed — `PATH` is already imported on line 18.

**Step 3: Verify build**

Run: `go build ./...`
Expected: Clean build (the embedded file is recompiled).

**Step 4: Commit**

```bash
git add internal/provision/intuneme-profile.sh
git commit -m "feat: add Azure VPN Client to container PATH"
```

---

### Task 8: Lint and final verification

**Step 1: Run formatter and linter**

Run: `make fmt && make lint`
Expected: Clean output, no warnings.

**Step 2: Run all tests**

Run: `go test ./...`
Expected: All PASS.

**Step 3: Commit any lint fixes if needed**

---

### Manual Testing

After implementing all tasks:

1. Rebuild the container image: rebuild `ubuntu-intune/` per your build process.
2. Recreate the container: `./intuneme recreate`
3. Shell in: `./intuneme shell`
4. Verify render group: `id` — should show `render` in group list.
5. Verify DRI access: `ls -la /dev/dri/` — `renderD128` should be accessible.
6. Launch Edge: `microsoft-edge` — should render pages without crashing.
7. Launch VPN client: `microsoft-azurevpnclient` — auth prompt should appear and VPN should connect.
