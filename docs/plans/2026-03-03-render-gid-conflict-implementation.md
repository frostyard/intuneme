# Render Group GID Conflict Fix — Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Fix `intuneme init` so it handles the case where the host's render GID is already occupied by another group inside the container, by reassigning the conflicting group to a free system GID first.

**Architecture:** Two new pure-function helpers (`findGroupByGID`, `findFreeSystemGID`) read the container's `/etc/group` file. `EnsureRenderGroup` is extended to detect conflicts and call `groupmod` to relocate the conflicting group before setting the render GID. All mutations go through `systemd-nspawn` + standard `groupmod`/`groupadd` tooling.

**Tech Stack:** Go 1.25, stdlib only (no new dependencies)

---

### Task 1: Add `findGroupByGID` helper with tests

**Files:**
- Modify: `internal/provision/provision.go:216-234` (add new function after `findGroupGID`)
- Modify: `internal/provision/provision_test.go:170` (add new test after `TestFindGroupGID`)

**Step 1: Write the failing test**

Add to `internal/provision/provision_test.go` after the `TestFindGroupGID` function (line 170):

```go
func TestFindGroupByGID(t *testing.T) {
	cases := []struct {
		name    string
		content string
		gid     int
		want    string
	}{
		{
			name:    "found",
			content: "root:x:0:\nvideo:x:44:\nrender:x:991:\n",
			gid:     991,
			want:    "render",
		},
		{
			name:    "not found",
			content: "root:x:0:\nvideo:x:44:\n",
			gid:     991,
			want:    "",
		},
		{
			name:    "finds correct group among many",
			content: "root:x:0:\nsystemd-resolve:x:992:\nrender:x:991:\n",
			gid:     992,
			want:    "systemd-resolve",
		},
		{
			name:    "empty file",
			content: "",
			gid:     100,
			want:    "",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tmp := filepath.Join(t.TempDir(), "group")
			if err := os.WriteFile(tmp, []byte(tc.content), 0644); err != nil {
				t.Fatalf("write temp group file: %v", err)
			}
			got, err := findGroupByGID(tmp, tc.gid)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tc.want {
				t.Errorf("findGroupByGID(%d) = %q, want %q", tc.gid, got, tc.want)
			}
		})
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/provision/ -run TestFindGroupByGID -v`
Expected: FAIL — `findGroupByGID` is not defined

**Step 3: Write minimal implementation**

Add to `internal/provision/provision.go` after `findGroupGID` (after line 234):

```go
// findGroupByGID reads a group file and returns the group name that owns the given GID.
// Returns "" if no group has that GID.
func findGroupByGID(groupPath string, gid int) (string, error) {
	data, err := os.ReadFile(groupPath)
	if err != nil {
		return "", err
	}
	gidStr := fmt.Sprintf("%d", gid)
	for line := range strings.SplitSeq(string(data), "\n") {
		fields := strings.Split(line, ":")
		if len(fields) >= 3 && fields[2] == gidStr {
			return fields[0], nil
		}
	}
	return "", nil
}
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/provision/ -run TestFindGroupByGID -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/provision/provision.go internal/provision/provision_test.go
git commit -m "feat: add findGroupByGID helper for reverse GID lookup (#33)"
```

---

### Task 2: Add `findFreeSystemGID` helper with tests

**Files:**
- Modify: `internal/provision/provision.go` (add new function after `findGroupByGID`)
- Modify: `internal/provision/provision_test.go` (add new test after `TestFindGroupByGID`)

**Step 1: Write the failing test**

Add to `internal/provision/provision_test.go` after `TestFindGroupByGID`:

```go
func TestFindFreeSystemGID(t *testing.T) {
	cases := []struct {
		name    string
		content string
		want    int
	}{
		{
			name:    "sparse file picks 999",
			content: "root:x:0:\nvideo:x:44:\nrender:x:991:\n",
			want:    999,
		},
		{
			name:    "999 taken picks 998",
			content: "root:x:0:\nfoo:x:999:\n",
			want:    998,
		},
		{
			name:    "empty file picks 999",
			content: "",
			want:    999,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tmp := filepath.Join(t.TempDir(), "group")
			if err := os.WriteFile(tmp, []byte(tc.content), 0644); err != nil {
				t.Fatalf("write temp group file: %v", err)
			}
			got, err := findFreeSystemGID(tmp)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tc.want {
				t.Errorf("findFreeSystemGID() = %d, want %d", got, tc.want)
			}
		})
	}

	// Full range should return error
	t.Run("no free GID", func(t *testing.T) {
		var lines []string
		for i := 100; i <= 999; i++ {
			lines = append(lines, fmt.Sprintf("g%d:x:%d:", i, i))
		}
		tmp := filepath.Join(t.TempDir(), "group")
		if err := os.WriteFile(tmp, []byte(strings.Join(lines, "\n")+"\n"), 0644); err != nil {
			t.Fatalf("write temp group file: %v", err)
		}
		_, err := findFreeSystemGID(tmp)
		if err == nil {
			t.Error("expected error when no free GID available, got nil")
		}
	})
}
```

Note: this test uses `fmt` and `strings` which are already imported in the test file. If `fmt` is not imported, add it to the imports.

**Step 2: Run test to verify it fails**

Run: `go test ./internal/provision/ -run TestFindFreeSystemGID -v`
Expected: FAIL — `findFreeSystemGID` is not defined

**Step 3: Write minimal implementation**

Add to `internal/provision/provision.go` after `findGroupByGID`:

```go
// findFreeSystemGID scans a group file and returns the first available GID
// in the system range (999 down to 100).
func findFreeSystemGID(groupPath string) (int, error) {
	data, err := os.ReadFile(groupPath)
	if err != nil {
		return -1, err
	}
	used := make(map[int]bool)
	for line := range strings.SplitSeq(string(data), "\n") {
		fields := strings.Split(line, ":")
		if len(fields) >= 3 {
			if gid, err := strconv.Atoi(fields[2]); err == nil {
				used[gid] = true
			}
		}
	}
	for gid := 999; gid >= 100; gid-- {
		if !used[gid] {
			return gid, nil
		}
	}
	return -1, fmt.Errorf("no free system GID in range 100-999")
}
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/provision/ -run TestFindFreeSystemGID -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/provision/provision.go internal/provision/provision_test.go
git commit -m "feat: add findFreeSystemGID helper to find available system GIDs (#33)"
```

---

### Task 3: Extend `EnsureRenderGroup` to handle GID conflicts

**Files:**
- Modify: `internal/provision/provision.go:243-261` (the `EnsureRenderGroup` function)
- Modify: `internal/provision/provision_test.go` (add new test case in `TestEnsureRenderGroup`)

**Step 1: Write the failing test**

Add a new subtest inside `TestEnsureRenderGroup` (after the "group exists with wrong GID modifies it" subtest, around line 239):

```go
	t.Run("GID conflict reassigns conflicting group first", func(t *testing.T) {
		tmp := t.TempDir()
		groupFile := filepath.Join(tmp, "etc", "group")
		if err := os.MkdirAll(filepath.Dir(groupFile), 0755); err != nil {
			t.Fatalf("setup: %v", err)
		}
		// render exists at GID 500, but target GID 992 is taken by systemd-resolve
		if err := os.WriteFile(groupFile, []byte("root:x:0:\nrender:x:500:\nsystemd-resolve:x:992:\n"), 0644); err != nil {
			t.Fatalf("setup: %v", err)
		}

		r := &mockRunner{}
		err := EnsureRenderGroup(r, tmp, 992)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(r.commands) != 2 {
			t.Fatalf("expected 2 commands, got %d: %v", len(r.commands), r.commands)
		}
		// First command: reassign systemd-resolve to a free GID
		if !strings.Contains(r.commands[0], "groupmod") || !strings.Contains(r.commands[0], "systemd-resolve") {
			t.Errorf("expected first command to reassign systemd-resolve, got: %s", r.commands[0])
		}
		// Second command: set render to target GID
		if !strings.Contains(r.commands[1], "groupmod") || !strings.Contains(r.commands[1], "992") || !strings.Contains(r.commands[1], "render") {
			t.Errorf("expected second command to set render GID to 992, got: %s", r.commands[1])
		}
	})

	t.Run("no conflict when render group is missing and GID is free", func(t *testing.T) {
		tmp := t.TempDir()
		groupFile := filepath.Join(tmp, "etc", "group")
		if err := os.MkdirAll(filepath.Dir(groupFile), 0755); err != nil {
			t.Fatalf("setup: %v", err)
		}
		// render doesn't exist, target GID 992 is free
		if err := os.WriteFile(groupFile, []byte("root:x:0:\nvideo:x:44:\n"), 0644); err != nil {
			t.Fatalf("setup: %v", err)
		}

		r := &mockRunner{}
		err := EnsureRenderGroup(r, tmp, 992)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(r.commands) != 1 {
			t.Fatalf("expected 1 command (groupadd only), got %d: %v", len(r.commands), r.commands)
		}
		if !strings.Contains(r.commands[0], "groupadd") {
			t.Errorf("expected groupadd, got: %s", r.commands[0])
		}
	})

	t.Run("GID conflict when render group is missing", func(t *testing.T) {
		tmp := t.TempDir()
		groupFile := filepath.Join(tmp, "etc", "group")
		if err := os.MkdirAll(filepath.Dir(groupFile), 0755); err != nil {
			t.Fatalf("setup: %v", err)
		}
		// render doesn't exist, but target GID 992 is taken by systemd-resolve
		if err := os.WriteFile(groupFile, []byte("root:x:0:\nsystemd-resolve:x:992:\n"), 0644); err != nil {
			t.Fatalf("setup: %v", err)
		}

		r := &mockRunner{}
		err := EnsureRenderGroup(r, tmp, 992)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(r.commands) != 2 {
			t.Fatalf("expected 2 commands, got %d: %v", len(r.commands), r.commands)
		}
		// First: reassign conflicting group
		if !strings.Contains(r.commands[0], "groupmod") || !strings.Contains(r.commands[0], "systemd-resolve") {
			t.Errorf("expected first command to reassign systemd-resolve, got: %s", r.commands[0])
		}
		// Second: groupadd render
		if !strings.Contains(r.commands[1], "groupadd") || !strings.Contains(r.commands[1], "render") {
			t.Errorf("expected second command to groupadd render, got: %s", r.commands[1])
		}
	})
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/provision/ -run "TestEnsureRenderGroup/GID_conflict" -v`
Expected: FAIL — the current `EnsureRenderGroup` doesn't handle GID conflicts (emits 1 command, test expects 2)

**Step 3: Update the implementation**

Replace the `EnsureRenderGroup` function in `internal/provision/provision.go` (lines 241-261) with:

```go
// EnsureRenderGroup ensures a "render" group with the given GID exists in the container.
// If the group is missing it is created; if it exists with a different GID it is modified.
// If the target GID is already occupied by another group, that group is reassigned to a
// free system GID first.
func EnsureRenderGroup(r runner.Runner, rootfsPath string, gid int) error {
	containerGroupPath := filepath.Join(rootfsPath, "etc", "group")
	existingGID, err := findGroupGID(containerGroupPath, "render")
	if err != nil {
		return fmt.Errorf("check container render group: %w", err)
	}

	if existingGID == gid {
		return nil
	}

	// Check if the target GID is occupied by another group.
	conflicting, err := findGroupByGID(containerGroupPath, gid)
	if err != nil {
		return fmt.Errorf("check GID conflict: %w", err)
	}
	if conflicting != "" && conflicting != "render" {
		freeGID, err := findFreeSystemGID(containerGroupPath)
		if err != nil {
			return fmt.Errorf("find free GID for %s: %w", conflicting, err)
		}
		fmt.Printf("  Reassigning group %q from GID %d to %d...\n", conflicting, gid, freeGID)
		if err := r.RunAttached("sudo", "systemd-nspawn", "--console=pipe", "-D", rootfsPath,
			"groupmod", "--gid", fmt.Sprintf("%d", freeGID), conflicting); err != nil {
			return fmt.Errorf("reassign group %s: %w", conflicting, err)
		}
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

**Step 4: Run all tests to verify everything passes**

Run: `go test ./internal/provision/ -v`
Expected: All tests PASS (existing tests still pass, new conflict tests pass)

**Step 5: Run lint**

Run: `make lint`
Expected: No lint errors

**Step 6: Commit**

```bash
git add internal/provision/provision.go internal/provision/provision_test.go
git commit -m "fix: handle render group GID conflicts in container (#33)

When the host's render GID is already occupied by another group
inside the container (e.g. systemd-resolve), reassign the
conflicting group to a free system GID before setting render."
```

---

### Task 4: Verify full build and existing tests

**Step 1: Run full test suite**

Run: `go test ./...`
Expected: All tests PASS

**Step 2: Run fmt and lint**

Run: `make fmt && make lint`
Expected: No issues

**Step 3: Final commit (if fmt changed anything)**

Only if `make fmt` modified files:
```bash
git add -A
git commit -m "style: apply gofmt"
```
