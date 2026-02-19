# VPN Plumbing Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Enable the azure-vpnclient in the container to create VPN tunnels through the host network stack.

**Architecture:** Add `--capability=CAP_NET_ADMIN` and `--bind=/dev/net/tun` to the static nspawn boot args. Always-on, no config gating.

**Tech Stack:** Go, systemd-nspawn, stdlib testing

---

### Task 1: Add VPN capability and TUN device to nspawn boot args

**Files:**
- Modify: `internal/nspawn/nspawn.go:123-136` (BuildBootArgs function)
- Test: `internal/nspawn/nspawn_test.go`

**Step 1: Add failing test assertions**

In `internal/nspawn/nspawn_test.go`, add two assertions to `TestBuildBootArgs` after the existing `/dev/dri` check (after line 26):

```go
	if !strings.Contains(joined, "--capability=CAP_NET_ADMIN") {
		t.Errorf("missing CAP_NET_ADMIN capability in: %s", joined)
	}
	if !strings.Contains(joined, "--bind=/dev/net/tun") {
		t.Errorf("missing /dev/net/tun bind in: %s", joined)
	}
```

**Step 2: Run tests to verify they fail**

Run: `go test ./internal/nspawn/ -run TestBuildBootArgs -v`
Expected: FAIL with "missing CAP_NET_ADMIN capability" and "missing /dev/net/tun bind"

**Step 3: Add the two flags to BuildBootArgs**

In `internal/nspawn/nspawn.go`, add two lines to the static args slice in `BuildBootArgs` (after the `--bind=/dev/dri` line):

```go
		"--capability=CAP_NET_ADMIN",
		"--bind=/dev/net/tun",
```

**Step 4: Run tests to verify they pass**

Run: `go test ./internal/nspawn/ -run TestBuildBootArgs -v`
Expected: PASS

**Step 5: Run full test suite and lint**

Run: `go test ./... && make fmt && make lint`
Expected: All pass, no lint errors

**Step 6: Commit**

```bash
git add internal/nspawn/nspawn.go internal/nspawn/nspawn_test.go
git commit -m "feat: add CAP_NET_ADMIN and /dev/net/tun for VPN support"
```
