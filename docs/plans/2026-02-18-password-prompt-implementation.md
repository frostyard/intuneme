# Password Prompt Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Remove the hardcoded container password, prompt the user interactively with confirmation and host-side validation, and fix a shell injection bug in `SetContainerPassword`.

**Architecture:** Two files change — `internal/provision/provision.go` gets a safer `SetContainerPassword` using a temp file + `--bind-ro` instead of shell interpolation; `cmd/init.go` gets `validatePassword`, `readPassword`, and a `--password-file` flag. All new logic is unexported helpers with unit tests.

**Tech Stack:** Go stdlib (`os`, `fmt`, `unicode`, `strings`), `github.com/charmbracelet/x/term` (already an indirect dep — promoted to direct by this change).

---

### Task 1: Fix `SetContainerPassword` in `internal/provision/provision.go`

The current implementation interpolates user + password into a single-quoted shell string, which breaks if the password contains a single-quote. The fix writes `user:password\n` to a temp file and passes it to `chpasswd` via stdin inside the container using `--bind-ro`.

**Files:**
- Modify: `internal/provision/provision.go` (the `SetContainerPassword` function, currently lines 165–171)
- Modify: `internal/provision/provision_test.go` (add `TestSetContainerPassword`)

**Step 1: Write the failing test**

Add this test to `internal/provision/provision_test.go`:

```go
func TestSetContainerPassword(t *testing.T) {
	r := &mockRunner{}
	err := SetContainerPassword(r, "/rootfs", "alice", "H@rdPa$$w0rd!")
	if err != nil {
		t.Fatalf("SetContainerPassword error: %v", err)
	}
	if len(r.commands) != 1 {
		t.Fatalf("expected 1 command, got %d: %v", len(r.commands), r.commands)
	}
	cmd := r.commands[0]

	// The password must NOT appear in the command string (no shell interpolation).
	if strings.Contains(cmd, "H@rdPa$$w0rd!") {
		t.Errorf("password must not appear in command args, got: %s", cmd)
	}
	// Must use bind-ro to pass the file into the container.
	if !strings.Contains(cmd, "--bind-ro=") {
		t.Errorf("expected --bind-ro= in command, got: %s", cmd)
	}
	// Must redirect the file into chpasswd inside the container.
	if !strings.Contains(cmd, "chpasswd < /run/chpasswd-input") {
		t.Errorf("expected 'chpasswd < /run/chpasswd-input' in command, got: %s", cmd)
	}
}

func TestSetContainerPasswordSpecialChars(t *testing.T) {
	// A password with a single-quote would break the old shell interpolation approach.
	r := &mockRunner{}
	err := SetContainerPassword(r, "/rootfs", "alice", "It'sAGr8Pass!")
	if err != nil {
		t.Fatalf("SetContainerPassword error: %v", err)
	}
	if strings.Contains(r.commands[0], "It'sAGr8Pass!") {
		t.Errorf("password must not appear literally in command, got: %s", r.commands[0])
	}
}
```

**Step 2: Run test to verify it fails**

```bash
go test ./internal/provision/... -run TestSetContainerPassword -v
```

Expected: FAIL — current implementation puts the password directly in the command string.

**Step 3: Implement the fix**

Replace the `SetContainerPassword` function in `internal/provision/provision.go`:

```go
// SetContainerPassword sets the user's password inside the container via chpasswd.
// Without a password, the account is locked and machinectl shell/login won't work interactively.
// The password is passed via a temp file bound read-only into the container to avoid shell injection.
func SetContainerPassword(r runner.Runner, rootfsPath, user, password string) error {
	tmp, err := os.CreateTemp("", "intuneme-chpasswd-*")
	if err != nil {
		return fmt.Errorf("create chpasswd temp file: %w", err)
	}
	defer func() { _ = os.Remove(tmp.Name()) }()

	if _, err := fmt.Fprintf(tmp, "%s:%s\n", user, password); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write chpasswd input: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close chpasswd temp file: %w", err)
	}

	return r.RunAttached("sudo", "systemd-nspawn", "--console=pipe",
		"--bind-ro="+tmp.Name()+":/run/chpasswd-input",
		"-D", rootfsPath,
		"bash", "-c", "chpasswd < /run/chpasswd-input",
	)
}
```

Note: `os.CreateTemp` creates the file with mode 0600 — no `os.Chmod` call needed.

**Step 4: Run test to verify it passes**

```bash
go test ./internal/provision/... -run TestSetContainerPassword -v
```

Expected: PASS for both `TestSetContainerPassword` and `TestSetContainerPasswordSpecialChars`.

**Step 5: Run all provision tests to check for regressions**

```bash
go test ./internal/provision/... -v
```

Expected: all tests PASS.

**Step 6: Commit**

```bash
git add internal/provision/provision.go internal/provision/provision_test.go
git commit -m "fix: avoid shell injection in SetContainerPassword

Use a temp file + --bind-ro instead of interpolating the password
into a single-quoted shell string, which breaks on passwords
containing single-quotes.

Co-Authored-By: Claude Sonnet 4.6 <noreply@anthropic.com>"
```

---

### Task 2: Add `validatePassword` to `cmd/init.go` with tests

`validatePassword` enforces the same rules as the container's `pam_pwquality.so` configuration: minlen=12, at least one digit/uppercase/lowercase/special character, and must not contain the username (case-insensitive).

**Files:**
- Modify: `cmd/init.go` (add `validatePassword` function and `unicode`/`strings` imports if not already present)
- Create: `cmd/init_test.go`

**Step 1: Create `cmd/init_test.go` with failing tests**

```go
package cmd

import (
	"testing"
)

func TestValidatePassword(t *testing.T) {
	tests := []struct {
		name     string
		username string
		password string
		wantErr  bool
		errMsg   string
	}{
		{
			name:     "valid password",
			username: "alice",
			password: "Correct3Horse!",
			wantErr:  false,
		},
		{
			name:     "too short",
			username: "alice",
			password: "Short1!A",
			wantErr:  true,
			errMsg:   "at least 12 characters",
		},
		{
			name:     "exactly 12 chars valid",
			username: "alice",
			password: "Aa1!Aa1!Aa1!",
			wantErr:  false,
		},
		{
			name:     "missing digit",
			username: "alice",
			password: "NoDigitsHere!A",
			wantErr:  true,
			errMsg:   "at least one digit",
		},
		{
			name:     "missing uppercase",
			username: "alice",
			password: "nouppercase1!aa",
			wantErr:  true,
			errMsg:   "at least one uppercase",
		},
		{
			name:     "missing lowercase",
			username: "alice",
			password: "NOLOWERCASE1!AA",
			wantErr:  true,
			errMsg:   "at least one lowercase",
		},
		{
			name:     "missing special character",
			username: "alice",
			password: "NoSpecialChar1A",
			wantErr:  true,
			errMsg:   "at least one special character",
		},
		{
			name:     "contains username (exact)",
			username: "alice",
			password: "alice-Passw0rd!",
			wantErr:  true,
			errMsg:   "must not contain your username",
		},
		{
			name:     "contains username (case insensitive)",
			username: "alice",
			password: "ALICE-Passw0rd!",
			wantErr:  true,
			errMsg:   "must not contain your username",
		},
		{
			name:     "multiple failures reported together",
			username: "alice",
			password: "short",
			wantErr:  true,
			errMsg:   "at least 12 characters",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validatePassword(tt.username, tt.password)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error, got nil")
				}
				if tt.errMsg != "" && !contains(err.Error(), tt.errMsg) {
					t.Errorf("expected error containing %q, got: %v", tt.errMsg, err)
				}
			} else {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
			}
		})
	}
}

// contains is a helper because strings.Contains is fine but avoids an import cycle.
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(substr) == 0 ||
		func() bool {
			for i := 0; i <= len(s)-len(substr); i++ {
				if s[i:i+len(substr)] == substr {
					return true
				}
			}
			return false
		}())
}
```

**Step 2: Run test to verify it fails**

```bash
go test ./cmd/... -run TestValidatePassword -v
```

Expected: FAIL — `validatePassword` is not defined yet.

**Step 3: Add `validatePassword` to `cmd/init.go`**

Add these imports to `cmd/init.go` (merge with existing import block):

```go
import (
    "fmt"
    "os"
    "os/user"
    "path/filepath"
    "strings"
    "unicode"

    "github.com/frostyard/intuneme/internal/config"
    "github.com/frostyard/intuneme/internal/prereq"
    "github.com/frostyard/intuneme/internal/provision"
    "github.com/frostyard/intuneme/internal/runner"
    pkgversion "github.com/frostyard/intuneme/internal/version"
    "github.com/spf13/cobra"
)
```

Add this function at the bottom of `cmd/init.go`, before the `init()` function:

```go
// validatePassword checks the password against the same rules enforced by the
// container's pam_pwquality.so configuration (minlen=12, dcredit/ucredit/lcredit/ocredit=-1,
// usercheck=1). All failures are collected and returned together.
func validatePassword(username, password string) error {
	var errs []string
	if len(password) < 12 {
		errs = append(errs, "must be at least 12 characters")
	}
	var hasDigit, hasUpper, hasLower, hasSpecial bool
	for _, r := range password {
		switch {
		case unicode.IsDigit(r):
			hasDigit = true
		case unicode.IsUpper(r):
			hasUpper = true
		case unicode.IsLower(r):
			hasLower = true
		case !unicode.IsLetter(r) && !unicode.IsDigit(r):
			hasSpecial = true
		}
	}
	if !hasDigit {
		errs = append(errs, "must contain at least one digit")
	}
	if !hasUpper {
		errs = append(errs, "must contain at least one uppercase letter")
	}
	if !hasLower {
		errs = append(errs, "must contain at least one lowercase letter")
	}
	if !hasSpecial {
		errs = append(errs, "must contain at least one special character")
	}
	if strings.Contains(strings.ToLower(password), strings.ToLower(username)) {
		errs = append(errs, "must not contain your username")
	}
	if len(errs) > 0 {
		return fmt.Errorf("password requirements not met:\n  - %s", strings.Join(errs, "\n  - "))
	}
	return nil
}
```

**Step 4: Run test to verify it passes**

```bash
go test ./cmd/... -run TestValidatePassword -v
```

Expected: all subtests PASS.

**Step 5: Commit**

```bash
git add cmd/init.go cmd/init_test.go
git commit -m "feat: add validatePassword for host-side pwquality enforcement

Validates minlen=12, character class requirements, and username
check — matching the container's pam_pwquality.so configuration.

Co-Authored-By: Claude Sonnet 4.6 <noreply@anthropic.com>"
```

---

### Task 3: Add `readPassword` and `--password-file` flag to `cmd/init.go`

`readPassword` is the acquisition layer: it either reads from a file (non-interactive) or prompts the user twice with hidden input (interactive), then calls `validatePassword`.

**Files:**
- Modify: `cmd/init.go` (add `readPassword`, `passwordFile` var, import `charmbracelet/x/term`)
- Modify: `cmd/init_test.go` (add `TestReadPasswordFromFile`)

**Step 1: Add a test for the file-based path**

Add to `cmd/init_test.go`:

```go
import (
    "os"
    "path/filepath"
    "testing"
)

func TestReadPasswordFromFile(t *testing.T) {
	tests := []struct {
		name     string
		content  string
		username string
		wantPass string
		wantErr  bool
		errMsg   string
	}{
		{
			name:     "valid password from file",
			content:  "Correct3Horse!\n",
			username: "alice",
			wantPass: "Correct3Horse!",
			wantErr:  false,
		},
		{
			name:     "trims trailing newline",
			content:  "Correct3Horse!\n\n",
			username: "alice",
			wantPass: "Correct3Horse!",
			wantErr:  false,
		},
		{
			name:     "uses only first line",
			content:  "Correct3Horse!\nignored line",
			username: "alice",
			wantPass: "Correct3Horse!",
			wantErr:  false,
		},
		{
			name:     "invalid password rejected",
			content:  "weak\n",
			username: "alice",
			wantErr:  true,
			errMsg:   "password requirements not met",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f, err := os.CreateTemp(t.TempDir(), "pwfile-*")
			if err != nil {
				t.Fatal(err)
			}
			if _, err := f.WriteString(tt.content); err != nil {
				t.Fatal(err)
			}
			f.Close()

			got, err := readPassword(tt.username, f.Name())
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error, got nil")
				}
				if tt.errMsg != "" && !contains(err.Error(), tt.errMsg) {
					t.Errorf("expected error containing %q, got: %v", tt.errMsg, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.wantPass {
				t.Errorf("got %q, want %q", got, tt.wantPass)
			}
		})
	}
}

func TestReadPasswordFileNotFound(t *testing.T) {
	_, err := readPassword("alice", "/nonexistent/path/to/pwfile")
	if err == nil {
		t.Fatal("expected error for missing file, got nil")
	}
}
```

**Step 2: Run test to verify it fails**

```bash
go test ./cmd/... -run TestReadPassword -v
```

Expected: FAIL — `readPassword` not defined yet.

**Step 3: Add `readPassword` and `passwordFile` var to `cmd/init.go`**

First, add the import for `charmbracelet/x/term`. Update the import block:

```go
import (
    "fmt"
    "os"
    "os/user"
    "path/filepath"
    "strings"
    "unicode"

    "github.com/charmbracelet/x/term"
    "github.com/frostyard/intuneme/internal/config"
    "github.com/frostyard/intuneme/internal/prereq"
    "github.com/frostyard/intuneme/internal/provision"
    "github.com/frostyard/intuneme/internal/runner"
    pkgversion "github.com/frostyard/intuneme/internal/version"
    "github.com/spf13/cobra"
)
```

Add the package-level var (alongside `forceInit`):

```go
var passwordFile string
```

Add `readPassword` to `cmd/init.go`:

```go
// readPassword acquires and validates the container user password.
// If passwordFile is non-empty, it reads the first line of that file.
// Otherwise it prompts the user interactively (without echo), asking twice
// for confirmation. Up to 3 mismatch attempts are allowed.
func readPassword(username, passwordFile string) (string, error) {
	if passwordFile != "" {
		data, err := os.ReadFile(passwordFile)
		if err != nil {
			return "", fmt.Errorf("read password file: %w", err)
		}
		// Use only the first line; trim surrounding whitespace.
		first := strings.SplitN(strings.TrimRight(string(data), "\r\n"), "\n", 2)[0]
		password := strings.TrimSpace(first)
		if err := validatePassword(username, password); err != nil {
			return "", err
		}
		return password, nil
	}

	for range 3 {
		fmt.Print("Enter container user password: ")
		p1, err := term.ReadPassword(int(os.Stdin.Fd()))
		fmt.Println()
		if err != nil {
			return "", fmt.Errorf("read password: %w", err)
		}

		fmt.Print("Confirm password: ")
		p2, err := term.ReadPassword(int(os.Stdin.Fd()))
		fmt.Println()
		if err != nil {
			return "", fmt.Errorf("read password: %w", err)
		}

		if string(p1) != string(p2) {
			fmt.Fprintln(os.Stderr, "Passwords do not match, please try again.")
			continue
		}

		if err := validatePassword(username, string(p1)); err != nil {
			return "", err
		}
		return string(p1), nil
	}
	return "", fmt.Errorf("passwords did not match after 3 attempts")
}
```

**Step 4: Run `go mod tidy` to promote the dependency**

`charmbracelet/x/term` is currently an indirect dep. Importing it directly promotes it.

```bash
cd /home/bjk/projects/frostyard/intuneme && go mod tidy
```

Expected: `go.mod` updated (removes `// indirect` comment from `charmbracelet/x/term` line).

**Step 5: Build to verify imports compile**

```bash
go build ./...
```

Expected: no errors.

**Step 6: Run tests**

```bash
go test ./cmd/... -run TestReadPassword -v
```

Expected: all subtests PASS.

**Step 7: Commit**

```bash
git add cmd/init.go cmd/init_test.go go.mod go.sum
git commit -m "feat: add readPassword with --password-file support

Prompts interactively (hidden input, confirm twice) or reads from
a file when --password-file is set. Validates via validatePassword
before returning.

Co-Authored-By: Claude Sonnet 4.6 <noreply@anthropic.com>"
```

---

### Task 4: Wire up in `initCmd.RunE` and remove the hardcoded password

Hoist `user.Current()`, call `readPassword` early (fail fast before any container work), pass the result to `SetContainerPassword`, and remove the hardcoded string.

**Files:**
- Modify: `cmd/init.go` (the `RunE` function body and `init()` function)

**Step 1: Update `initCmd` in `cmd/init.go`**

Replace the existing `RunE` body and `init()` at the bottom with:

```go
var initCmd = &cobra.Command{
	Use:   "init",
	Short: "Provision the Intune nspawn container",
	RunE: func(cmd *cobra.Command, args []string) error {
		r := &runner.SystemRunner{}
		root := rootDir
		if root == "" {
			root = config.DefaultRoot()
		}

		// Check prerequisites
		if errs := prereq.Check(r); len(errs) > 0 {
			for _, e := range errs {
				fmt.Fprintln(os.Stderr, "  -", e)
			}
			return fmt.Errorf("missing prerequisites")
		}

		// Resolve host user early — needed for password validation.
		u, _ := user.Current()

		// Acquire and validate password before doing any container work.
		password, err := readPassword(u.Username, passwordFile)
		if err != nil {
			return err
		}

		// Create ~/Intune directory
		home, _ := os.UserHomeDir()
		intuneHome := filepath.Join(home, "Intune")
		if err := os.MkdirAll(intuneHome, 0755); err != nil {
			return fmt.Errorf("create ~/Intune: %w", err)
		}

		// Check if already initialized
		cfg, _ := config.Load(root)
		if _, err := os.Stat(cfg.RootfsPath); err == nil && !forceInit {
			return fmt.Errorf("already initialized at %s — use --force to reinitialize", root)
		}

		image := pkgversion.ImageRef()
		fmt.Printf("Pulling OCI image %s...\n", image)
		if err := provision.PullImage(r, image); err != nil {
			return err
		}

		fmt.Println("Extracting rootfs...")
		if err := os.MkdirAll(root, 0755); err != nil {
			return fmt.Errorf("create root dir: %w", err)
		}
		if err := provision.ExtractRootfs(r, image, cfg.RootfsPath); err != nil {
			return err
		}

		hostname, _ := os.Hostname()

		fmt.Println("Creating container user...")
		if err := provision.CreateContainerUser(r, cfg.RootfsPath, u.Username, os.Getuid(), os.Getgid()); err != nil {
			return err
		}

		fmt.Println("Setting container user password...")
		if err := provision.SetContainerPassword(r, cfg.RootfsPath, u.Username, password); err != nil {
			return fmt.Errorf("set password failed: %w", err)
		}

		fmt.Println("Applying fixups...")
		if err := provision.WriteFixups(r, cfg.RootfsPath, u.Username, os.Getuid(), os.Getgid(), hostname+"LXC"); err != nil {
			return err
		}

		fmt.Println("Installing polkit rules...")
		if err := provision.InstallPolkitRule(r, "/etc/polkit-1/rules.d"); err != nil {
			fmt.Fprintf(os.Stderr, "warning: polkit install failed: %v\n", err)
		}

		fmt.Println("Saving config...")
		cfg.HostUID = os.Getuid()
		cfg.HostUser = u.Username
		if err := cfg.Save(root); err != nil {
			return err
		}

		fmt.Printf("Initialized intuneme at %s\n", root)
		return nil
	},
}

func init() {
	initCmd.Flags().BoolVar(&forceInit, "force", false, "reinitialize even if already set up")
	initCmd.Flags().StringVar(&passwordFile, "password-file", "", "path to file containing the container user password (first line used)")
	rootCmd.AddCommand(initCmd)
}
```

**Step 2: Build to verify no compilation errors**

```bash
go build ./...
```

Expected: clean build, no errors.

**Step 3: Run all tests**

```bash
go test ./...
```

Expected: all tests PASS. No references to `"Intuneme2024!"` remain.

**Step 4: Verify the hardcoded password is gone**

```bash
grep -r "Intuneme2024" .
```

Expected: no output (zero matches).

**Step 5: Format and lint**

```bash
make fmt && make lint
```

Expected: no formatting changes needed, no lint errors.

**Step 6: Commit**

```bash
git add cmd/init.go
git commit -m "feat: prompt for container password during init

- Remove hardcoded 'Intuneme2024!' password
- Prompt interactively with confirmation (hidden input, 3 attempts)
- Support --password-file flag for non-interactive use
- Validate against pwquality rules before provisioning

Co-Authored-By: Claude Sonnet 4.6 <noreply@anthropic.com>"
```

---

## Verification Checklist

After all tasks complete:

- [ ] `grep -r "Intuneme2024" .` returns no matches
- [ ] `go test ./...` passes
- [ ] `go build ./...` succeeds
- [ ] `make fmt && make lint` clean
- [ ] `intuneme init` prompts for password (manual smoke test)
- [ ] `intuneme init --password-file /path/to/file` works without prompting (manual smoke test)
- [ ] A password with a single-quote (e.g., `It'sAGr8Pass!`) is accepted and set correctly
