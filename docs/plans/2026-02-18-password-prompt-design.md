# Password Prompt Design

**Date:** 2026-02-18
**Status:** Approved

## Problem

The `init` command hardcodes `"Intuneme2024!"` as the container user's password. This is a security liability: every installation shares the same password, and it is visible in the source code.

Additionally, `provision.SetContainerPassword` has a shell injection vulnerability — passwords containing single-quotes break the shell command used to invoke `chpasswd`.

## Goal

- Remove the hardcoded password
- Prompt the user interactively during `init`, with confirmation
- Validate the password on the host against the same rules enforced by the container's `pam_pwquality.so`
- Support non-interactive use via `--password-file`
- Fix the shell injection in `SetContainerPassword`

## Design

### Password Acquisition (`cmd/init.go`)

A `readPassword(username, passwordFile string) (string, error)` helper is added to `cmd/init.go`.

**Non-interactive path** (`--password-file` flag set):
- Read first line of the file, trim whitespace
- Run `validatePassword` — fail with a clear error if it doesn't pass
- No retry (non-interactive)

**Interactive path** (no `--password-file`):
- Use `charmbracelet/x/term`'s `ReadPassword(int(os.Stdin.Fd()))` to prompt `"Enter container user password: "` without echo
- Prompt again `"Confirm password: "` the same way
- If passwords don't match, re-prompt up to 3 times before giving up with an error
- After a successful match, run `validatePassword`

`initCmd` registers a `--password-file` string flag. `user.Current()` is hoisted above the password prompt so the username is available for validation. Password acquisition happens early in `RunE`, before any provisioning work, so failures are cheap.

### Password Validation (`cmd/init.go`)

`validatePassword(username, password string) error` enforces the container's `pwquality.conf` rules:

| Rule | Check |
|------|-------|
| `minlen=12` | `len(password) >= 12` |
| `dcredit=-1` | at least one `unicode.IsDigit` character |
| `ucredit=-1` | at least one `unicode.IsUpper` character |
| `lcredit=-1` | at least one `unicode.IsLower` character |
| `ocredit=-1` | at least one character that is neither letter nor digit |
| `usercheck=1` | password does not contain username (case-insensitive) |

Dictionary check (`dictcheck=1`) is not replicated on the host. The container's PAM will still catch dictionary words via `chpasswd`.

All failing rules are collected and reported together so the user sees everything wrong at once.

### Shell Injection Fix (`internal/provision/provision.go`)

The current `SetContainerPassword` builds a shell command by interpolating user and password strings into a single-quoted shell expression. Any single-quote in the password breaks this.

The fix writes `user:password\n` to a host temp file (mode `0600`) and passes it to `chpasswd` via stdin inside `systemd-nspawn` using `--bind-ro`:

```go
func SetContainerPassword(r runner.Runner, rootfsPath, user, password string) error {
    tmp, err := os.CreateTemp("", "intuneme-chpasswd-*")
    // write "user:password\n", chmod 0600, defer remove
    return r.RunAttached("sudo", "systemd-nspawn", "--console=pipe",
        "--bind-ro="+tmp.Name()+":/run/chpasswd-input",
        "-D", rootfsPath,
        "bash", "-c", "chpasswd < /run/chpasswd-input")
}
```

This is safe for any password content.

## Files Changed

- `cmd/init.go` — add `--password-file` flag, `readPassword`, `validatePassword`, hoist `user.Current()`, pass password to `SetContainerPassword`
- `internal/provision/provision.go` — fix `SetContainerPassword` to use temp file + `--bind-ro`

## Non-Goals

- No `--password` flag (avoids password in process list/shell history)
- No CGo / libcrack binding for dictionary check on host
- No new packages or files beyond what already exists
