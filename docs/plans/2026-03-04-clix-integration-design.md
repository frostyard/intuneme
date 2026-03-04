# clix Integration Design

## Goal

Integrate `github.com/frostyard/clix` into intuneme to unify common CLI functionality: version handling, common flags (`--json`, `--verbose`, `--dry-run`, `--silent`), structured JSON output, and a reporter system for consistent progress output.

## Scope

Full adoption:
- Replace main.go boilerplate with `clix.App.Run()`
- Migrate all `fmt.Println` progress output to `clix.NewReporter()`
- Add `--json` output for `status` command
- Implement `--dry-run` for all mutation commands (init, start, stop, destroy, recreate)
- Gate verbose output behind `--verbose`
- Adopt clix output convention: progress to stderr via Reporter, data to stdout

## Design

### main.go Simplification

Replace version string formatting, signal handling, and fang execution with `clix.App{Version, Commit, Date, BuiltBy}.Run(rootCmd)`. Removes direct `fang` and `signal` imports.

### Reporter Wiring (Approach: Package-level)

Add `var rep reporter.Reporter` in `root.go`. Initialize it in `rootCmd.PersistentPreRunE` via `clix.NewReporter()`. This matches the existing `rootDir` pattern — simple package-level state accessible from all commands.

`clix.NewReporter()` returns:
- `--silent` → NoopReporter (suppresses all output; takes priority)
- `--json` → JSONReporter (stdout, for piping)
- default → TextReporter (stderr, keeps stdout clean)

### Command Migration

Replace across all commands:

| Before | After |
|--------|-------|
| `fmt.Println("message")` | `rep.Info("message")` |
| `fmt.Printf("key: %s\n", val)` | `rep.Info(fmt.Sprintf("key: %s", val))` |
| `fmt.Fprintf(os.Stderr, "warning: ...")` | `rep.Warn("...")` |

### JSON Output (status command)

When `--json` is active, `status` outputs structured JSON to stdout and returns early:

```go
clix.OutputJSON(map[string]any{
    "root":         root,
    "rootfs":       cfg.RootfsPath,
    "machine":      cfg.MachineName,
    "container":    containerStatus,
    "broker_proxy": brokerStatus,
})
```

### Dry-run for Mutation Commands

Commands: init, start, stop, destroy, recreate. Each gets an early guard after config loading:

```go
if clix.DryRun {
    rep.Info("[dry-run] Would <describe action>")
    return nil
}
```

Lists what would happen, then returns without executing.

### Verbose Flag

Gate detailed output behind `clix.Verbose`. Example: webcam detection details in `start.go`, individual directory cleanup messages in `destroy.go`.

### Dependencies

- Add: `github.com/frostyard/clix` (brings `github.com/frostyard/std` transitively)
- Remove: direct `github.com/charmbracelet/fang` dependency (clix wraps it)

## Commands Affected

| Command | Reporter | JSON | Dry-run | Verbose |
|---------|----------|------|---------|---------|
| init | yes | no | yes | yes |
| start | yes | no | yes | yes (webcam, broker details) |
| stop | yes | no | yes | no |
| destroy | yes | no | yes | yes (dir cleanup) |
| recreate | yes | no | yes | yes |
| status | yes | yes | no | no |
| shell | yes | no | no | no |
| open | yes | no | no | no |
| config | yes | no | no | no |
| extension | yes | no | no | no |
| broker-proxy | yes | no | no | yes |
