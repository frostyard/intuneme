# Release Pipeline Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Integrate charmbracelet/fang for styled help, man pages, shell completions, and version info; wire up goreleaser scripts; clean up config files.

**Architecture:** Replace cobra's Execute with fang.Execute in main.go, export RootCmd() from cmd package. Shell scripts generate completions and man pages for goreleaser packaging.

**Tech Stack:** Go, charmbracelet/fang, spf13/cobra, goreleaser-pro, svu

---

### Task 1: Add fang dependency

**Files:**
- Modify: `go.mod`

**Step 1: Add fang**

Run: `go get github.com/charmbracelet/fang@latest`

**Step 2: Tidy**

Run: `go mod tidy`

**Step 3: Verify**

Run: `grep fang go.mod`
Expected: line containing `github.com/charmbracelet/fang`

**Step 4: Commit**

```bash
git add go.mod go.sum
git commit -m "feat: add charmbracelet/fang dependency"
```

---

### Task 2: Refactor cmd/root.go — export RootCmd, remove Execute

**Files:**
- Modify: `cmd/root.go`

**Step 1: Replace cmd/root.go**

Remove the `Execute()` function and its `fmt`/`os` imports. Add `RootCmd()` that returns the package-level `rootCmd`. The file should become:

```go
package cmd

import "github.com/spf13/cobra"

var rootDir string

var rootCmd = &cobra.Command{
	Use:   "intuneme",
	Short: "Manage an Intune container on an immutable Linux host",
}

func RootCmd() *cobra.Command {
	return rootCmd
}

func init() {
	rootCmd.PersistentFlags().StringVar(&rootDir, "root", "", "root directory for intuneme data (default ~/.local/share/intuneme)")
}
```

**Step 2: Verify it compiles**

Run: `go build ./cmd/...`
Expected: success (main.go will break — that's expected, we fix it in Task 3)

**Step 3: Commit**

```bash
git add cmd/root.go
git commit -m "refactor: export RootCmd, remove Execute from cmd package"
```

---

### Task 3: Rewrite main.go to use fang.Execute

**Files:**
- Modify: `main.go`

**Step 1: Replace main.go**

```go
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"

	"github.com/charmbracelet/fang"
	"github.com/frostyard/intune/cmd"
)

var version = "dev"
var commit = "none"
var date = "unknown"
var builtBy = "local"

func makeVersionString() string {
	return fmt.Sprintf("%s (Commit: %s) (Date: %s) (Built by: %s)", version, commit, date, builtBy)
}

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	if err := fang.Execute(ctx, cmd.RootCmd(),
		fang.WithVersion(makeVersionString()),
		fang.WithNotifySignal(os.Interrupt),
	); err != nil {
		os.Exit(1)
	}
}
```

**Step 2: Build the binary**

Run: `go build -o intuneme .`
Expected: success, produces `./intuneme` binary

**Step 3: Verify fang features work**

Run these and confirm output:
- `./intuneme --help` — should show styled help with all subcommands
- `./intuneme --version` — should show `dev (Commit: none) (Date: unknown) (Built by: local)`
- `./intuneme completion bash` — should output bash completion script
- `./intuneme man` — should output roff-formatted man page

**Step 4: Commit**

```bash
git add main.go
git commit -m "feat: use fang.Execute for styled help, version, man, completions"
```

---

### Task 4: Create goreleaser scripts

**Files:**
- Create: `scripts/completions.sh`
- Create: `scripts/manpages.sh`

**Step 1: Create scripts/completions.sh**

```sh
#!/bin/sh
set -e
mkdir -p completions
for sh in bash zsh fish; do
    go run . completion "$sh" > "completions/intuneme.$sh"
done
```

**Step 2: Create scripts/manpages.sh**

```sh
#!/bin/sh
set -e
mkdir -p manpages
go run . man > "manpages/intuneme.1"
gzip -f "manpages/intuneme.1"
```

**Step 3: Make executable**

Run: `chmod +x scripts/completions.sh scripts/manpages.sh`

**Step 4: Test scripts locally**

Run: `./scripts/completions.sh && ls completions/`
Expected: `intuneme.bash  intuneme.fish  intuneme.zsh`

Run: `./scripts/manpages.sh && ls manpages/`
Expected: `intuneme.1.gz`

**Step 5: Clean up generated files**

Run: `rm -rf completions/ manpages/`

**Step 6: Commit**

```bash
git add scripts/completions.sh scripts/manpages.sh
git commit -m "feat: add goreleaser scripts for completions and man pages"
```

---

### Task 5: Clean up .goreleaser.yaml

**Files:**
- Modify: `.goreleaser.yaml`

**Step 1: Remove dracut references from nfpms contents**

Remove these lines from the `contents:` list under `nfpms:`:

```yaml
      # Dracut module for /etc overlay persistence
      - src: ./pkg/dracut/95etc-overlay/module-setup.sh
        dst: /usr/lib/dracut/modules.d/95etc-overlay/module-setup.sh
        file_info:
          mode: 0755
      - src: ./pkg/dracut/95etc-overlay/etc-overlay-mount.sh
        dst: /usr/lib/dracut/modules.d/95etc-overlay/etc-overlay-mount.sh
        file_info:
          mode: 0755
```

**Step 2: Fix release footer URL**

Change `bketelsen/{{ .ProjectName }}` to `frostyard/{{ .ProjectName }}` in the release footer.

**Step 3: Verify YAML is valid**

Run: `python3 -c "import yaml; yaml.safe_load(open('.goreleaser.yaml'))"`
Expected: no error (or use `goreleaser check` if installed)

**Step 4: Commit**

```bash
git add .goreleaser.yaml
git commit -m "fix: clean up goreleaser config — remove dracut, fix footer URL"
```

---

### Task 6: Update Makefile

**Files:**
- Modify: `Makefile`

**Step 1: Replace the `docs` target and add new targets**

Replace the existing `docs` target:

```makefile
completions: build ## Generate shell completions
	@echo "Generating completions..."
	@mkdir -p completions
	@for sh in bash zsh fish; do \
		./$(BINARY_NAME) completion $$sh > completions/$(BINARY_NAME).$$sh; \
	done

manpages: build ## Generate man pages
	@echo "Generating man pages..."
	@mkdir -p manpages
	@./$(BINARY_NAME) man > manpages/$(BINARY_NAME).1

docs: completions manpages ## Generate all documentation
```

Also update the `.PHONY` line to include the new targets:

```makefile
.PHONY: build clean install test run help completions manpages docs
```

**Step 2: Verify targets work**

Run: `make docs`
Expected: generates files in `completions/` and `manpages/`

Run: `make clean && rm -rf completions/ manpages/`

**Step 3: Commit**

```bash
git add Makefile
git commit -m "feat: update Makefile with completions, manpages, docs targets"
```

---

### Task 7: Update .gitignore

**Files:**
- Modify: `.gitignore`

**Step 1: Add generated directories**

Append to `.gitignore`:

```
/completions
/manpages
/dist
```

**Step 2: Commit**

```bash
git add .gitignore
git commit -m "chore: ignore generated completions, manpages, dist directories"
```

---

### Task 8: Smoke test the full pipeline

**Step 1: Build and test all fang commands**

Run:
```bash
go build -o intuneme . && \
./intuneme --version && \
./intuneme --help && \
./intuneme completion bash > /dev/null && \
./intuneme completion zsh > /dev/null && \
./intuneme completion fish > /dev/null && \
./intuneme man > /dev/null
```
Expected: all succeed with no errors

**Step 2: Test goreleaser scripts**

Run:
```bash
./scripts/completions.sh && \
./scripts/manpages.sh && \
ls completions/ manpages/
```
Expected: `intuneme.bash intuneme.fish intuneme.zsh` and `intuneme.1.gz`

**Step 3: Test make targets**

Run: `rm -rf completions/ manpages/ && make docs && ls completions/ manpages/`
Expected: same files regenerated

**Step 4: Clean up**

Run: `rm -rf completions/ manpages/ intuneme`

**Step 5: Run existing tests**

Run: `go test ./...`
Expected: all pass (or no tests — either way no failures)
