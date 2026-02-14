# Release Pipeline Design: Fang, GoReleaser, Docs, Man, Completions

Date: 2026-02-13

## Overview

Integrate charmbracelet/fang into the intuneme CLI to get styled help, man pages,
shell completions, and version info out of the box. Wire up goreleaser scripts to
generate artifacts at build time. Clean up the goreleaser config inherited from igloo.

## 1. Fang Integration

Replace `cmd.Execute()` in `main.go` with `fang.Execute()`.

**main.go:**

```go
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

Fang provides:
- Styled help pages and error messages
- Hidden `man` command (mango-powered, single man page)
- Hidden `completion` command (bash/zsh/fish/powershell)
- Version info from build metadata

## 2. cmd/root.go Refactor

Export the root command via `RootCmd()` returning the existing package-level `rootCmd`.
Remove the internal `Execute()` function. Subcommand files stay untouched (they still
register via `init()` calling `rootCmd.AddCommand()`).

```go
func RootCmd() *cobra.Command {
    return rootCmd
}
```

## 3. Goreleaser Scripts

Shell scripts called by goreleaser's `before.hooks`, using `go run .` since they
execute before the binary is built.

**scripts/completions.sh:**
```sh
#!/bin/sh
set -e
mkdir -p completions
for sh in bash zsh fish; do
    go run . completion "$sh" > "completions/intuneme.$sh"
done
```

**scripts/manpages.sh:**
```sh
#!/bin/sh
set -e
mkdir -p manpages
go run . man > "manpages/intuneme.1"
gzip -f "manpages/intuneme.1"
```

## 4. Goreleaser Config Cleanup

- Remove `pkg/dracut/95etc-overlay/` references from nfpms (not applicable)
- Fix release footer URL (`frostyard/intuneme` not `bketelsen/`)
- Keep: nfpms (deb/rpm/apk with completions + man page), changelog, nightly, gomod proxy

## 5. Makefile Updates

- `docs` target: generate man pages via `go run . man`
- Add `completions` target for local dev
- Add `manpages` target for local dev
- Keep existing targets unchanged

## 6. .gitignore Updates

Add generated artifact directories:
```
completions/
manpages/
dist/
```
