---
name: golang-dependency-management
description: "Dependency management for Golang projects — go.mod and go.sum, `go get` install and upgrade flows, Minimal Version Selection, conflict resolution with replace/exclude/retract, `govulncheck` scanning of the module tree, outdated dependency and binary size auditing, vendoring, `tool` directives, and go.work workspaces. Use when adding, removing, or upgrading Go dependencies, deciding whether to take on a package, resolving version conflicts, or auditing what a module pulls in. Covers choosing and upgrading dependency versions, not the surrounding tooling: do NOT use for fixing an exploitable vulnerability in code (→ See `golang-security` skill)."
user-invocable: true
license: MIT
compatibility: Designed for Claude Code, Codex or similar harness, and for projects using Golang.
metadata:
  author: samber
  version: "1.3.1"
  openclaw:
    emoji: "📦"
    homepage: https://github.com/samber/cc-skills-golang
    requires:
      bins:
        - go
        - govulncheck
    install:
      - kind: go
        package: golang.org/x/vuln/cmd/govulncheck@latest
        bins: [govulncheck]
---

> **Vendored skill.** Copied from [samber/cc-skills-golang](https://github.com/samber/cc-skills-golang/tree/bb1d43d98a3f8ab639a0042f90d0ec524e39a477/skills/golang-dependency-management) at commit `bb1d43d98a3f8ab639a0042f90d0ec524e39a477` under the MIT licence ([LICENSE](LICENSE)). frostyard/core manages this copy: change it in core, never in a consumer ([shared skills](https://github.com/frostyard/core/blob/main/docs/design/shared-skills.md#vendored-go-skills)).
>
> **Frostyard override.** When this skill disagrees with the repository's `AGENTS.md`, the `frostyard-go-repo` skill or a frostyard/core ADR, they win. A pointer to `samber/cc-skills-golang@golang-<name>` means the local `golang-<name>` skill when it exists; frostyard does not vendor the others. Do not add a third-party module that this skill suggests (for example `samber/*`) without the owner's approval. Pin tools in the repository's `mise.toml` and `mise.lock` ([ADR-0043](https://github.com/frostyard/core/blob/main/docs/adr/0043-pin-repository-tools-in-mise-and-name-the-verify-gate.md)), not with `go install …@latest`, `go get -tool`, a `tools.go` file or `brew`, and run the gate through `make verify`, `make check` and `make ci` ([ADR-0044](https://github.com/frostyard/core/blob/main/docs/adr/0044-expose-the-make-gate-triad-in-every-repository.md)). The orchestration and thinking hints (sub-agents, `ultracode`, `ultrathink`) are Claude Code hints: the session's own rules for subagents win.
>
> - Pin tools in `mise.toml` and `mise.lock`, not in `tool` directives or `tools.go` ([ADR-0043](https://github.com/frostyard/core/blob/main/docs/adr/0043-pin-repository-tools-in-mise-and-name-the-verify-gate.md)). The "Installing CLI Tools" section does not apply.
> - Nothing auto-merges in frostyard: a Dependabot or Renovate pull request passes the repository's review gate and merge queue ([ADR-0042](https://github.com/frostyard/core/blob/main/docs/adr/0042-adopt-a-merge-queue-on-the-default-branch.md)), so the auto-merge strategy in `references/automated-updates.md` does not apply.

**Persona:** You are a Go dependency steward. You treat every new dependency as a long-term maintenance commitment — you ask whether the standard library already solves the problem before reaching for an external package.

**Dependencies:**

- govulncheck: `go install golang.org/x/vuln/cmd/govulncheck@latest`

# Go Dependency Management

## AI Agent Rule: Ask Before Adding Dependencies

**Before running `go get` to add any new dependency, AI agents MUST ask the user for confirmation.** AI agents can suggest packages that are unmaintained, low-quality, or unnecessary when the standard library already provides equivalent functionality. Using `go get -u` to upgrade an existing dependency is safe.

Before proposing a dependency, evaluate:

- Does the standard library already cover the use case?
- Is the license compatible?
- Are there well-known alternatives?
- What it does and why it's needed?

The `samber/cc-skills-golang@golang-popular-libraries` skill contains a curated list of vetted, production-ready libraries. Prefer recommending packages from that list. When no vetted option exists, favor well-known packages from the Go team (`golang.org/x/...`) or established organizations over obscure alternatives.

## Key Rules

- `go.sum` MUST be committed — it records cryptographic checksums of every dependency version, letting `go mod verify` detect supply-chain tampering. Without it, a compromised proxy could silently substitute malicious code
- `govulncheck ./...` or `go tool govulncheck ./...` before every release — catches known CVEs in your dependency tree before they reach production
- Maintenance status, license compatibility, and stdlib alternatives are important considerations before adding a dependency — every dependency increases attack surface, maintenance burden, and binary size
- `go mod tidy` before every commit that changes dependencies — removes unused modules and adds missing ones, keeping go.mod honest

## go.mod & go.sum

### Essential Commands

| Command           | Purpose                                      |
| ----------------- | -------------------------------------------- |
| `go mod tidy`     | Add missing deps, remove unused ones         |
| `go mod download` | Download modules to local cache              |
| `go mod verify`   | Verify cached modules match go.sum checksums |
| `go mod vendor`   | Copy deps into `vendor/` directory           |
| `go mod edit`     | Edit go.mod programmatically (scripts, CI)   |
| `go mod graph`    | Print the module requirement graph           |
| `go mod why`      | Explain why a module or package is needed    |

### Vendoring

Use `go mod vendor` when you need hermetic builds (no network access), reproducibility guarantees beyond checksums, or when deploying to environments without module proxy access. CI pipelines and Docker builds sometimes benefit from vendoring. Run `go mod vendor` after any dependency change and commit the `vendor/` directory.

## Installing & Upgrading Dependencies

### Adding a Dependency

```bash
go get github.com/google/uuid          # Latest version
go get github.com/google/uuid@v1.6.0   # Specific version
go get github.com/google/uuid@latest   # Explicitly latest
go get github.com/google/uuid@<commit> # Specific commit (pseudo-version)
```

Before pinning a version, inspect the module's available versions, importers, and known vulnerabilities on pkg.go.dev → See `samber/cc-skills-golang@golang-pkg-go-dev` skill.

### Upgrading

```bash
go get -u ./...            # Upgrade ALL direct+indirect deps to latest minor/patch
go get -u=patch ./...      # Upgrade to latest patch only (safer)
go get github.com/pkg@v1.5 # Upgrade specific package
```

**Prefer `go get -u=patch`** for routine updates. Patch and minor updates are usually lower risk than major upgrades, but still require review. For dependency updates, run:

```bash
go get -u=patch ./...
go mod tidy
go test ./...
go vet ./...
govulncheck ./...   # or: go tool govulncheck ./...
```

Release notes and changelogs for libraries affecting persistence, serialization, networking, authentication, authorization, cryptography, or public APIs may contain important information about breaking changes.

### Removing a Dependency

```bash
go get github.com/google/uuid@none  # Mark for removal
go mod tidy                          # Clean up go.mod and go.sum
```

### Installing CLI Tools

For Go 1.24+ modules, pin executable tools in `go.mod` with `tool` directives. Do not create a new `tools.go` blank-import file unless the module must support Go <1.24.

```bash
# Add tools to the current module.
go get -tool github.com/golangci/golangci-lint/v2/cmd/golangci-lint@latest
go get -tool golang.org/x/vuln/cmd/govulncheck@latest
go get -tool golang.org/x/perf/cmd/benchstat@latest

# Run pinned tools reproducibly.
go tool golangci-lint run ./...
go tool govulncheck ./...
go tool benchstat old.txt new.txt

# Install all module-pinned tools into GOBIN/PATH when needed.
go install tool

# Update pinned tools deliberately, then review go.mod/go.sum.
go get -u tool
go mod tidy
```

`go.mod` shape for a module targeting Go 1.27 or newer. This is an example target, not a cap; keep the project's actual `go` directive and do not change it just to add tools.

```go.mod
module example.com/project

go 1.27

tool (
    github.com/golangci/golangci-lint/v2/cmd/golangci-lint
    golang.org/x/vuln/cmd/govulncheck
    golang.org/x/perf/cmd/benchstat
)
```

For `go 1.27` or newer, `go mod tidy` auto-merges duplicate `require` blocks and enforces a two-block layout (direct dependencies, then indirect), preserving existing comments — no manual cleanup needed after a merge that introduces a second `require` block.

For Go <1.24 only, use the legacy `tools.go` blank-import workaround:

```go
//go:build tools

package tools

import (
    _ "github.com/golangci/golangci-lint/v2/cmd/golangci-lint"
    _ "golang.org/x/vuln/cmd/govulncheck"
)
```

Rule: Go 1.24+ = `tool` directives. Go <1.24 = `tools.go` fallback.

### Module target note

When using a newer toolchain, `go mod init` may create a module with an older default `go` directive. If the project intentionally targets the newer toolchain's APIs, update the directive deliberately:

```bash
go mod edit -go=1.27
go mod tidy
```

For future Go versions, use the project's intended target version. Do not use APIs newer than the module's `go` directive until the project explicitly agrees to upgrade it.

## Deep Dives

- **[Versioning & MVS](./references/versioning.md)** — Semantic versioning rules (major.minor.patch), when to increment each number, pre-release versions, the Minimal Version Selection (MVS) algorithm (why you can't just pick "latest"), and major version suffix conventions (v0, v1, v2 suffixes for breaking changes).

- **[Auditing Dependencies](./references/auditing.md)** — Vulnerability scanning with `govulncheck`, tracking outdated dependencies, analyzing which dependencies make the binary large (`goweight`), and distinguishing test-only vs binary dependencies to keep `go.mod` clean.

- **[Dependency Conflicts & Resolution](./references/conflicts.md)** — Diagnosing version conflicts (what `go get` does when you request incompatible versions), resolution strategies (`replace` directives for local development, `exclude` for broken versions, `retract` for published versions that should be skipped), and workflows for conflicts across your dependency tree.

- **[Go Workspaces](./references/workspaces.md)** — `go.work` files for multi-module development (e.g., library + example application), when to use workspaces vs monorepos, and workspace best practices.

- **[Automated Dependency Updates](./references/automated-updates.md)** — Setting up Dependabot or Renovate for automatic dependency update PRs, auto-merge strategies (when to merge automatically vs require review), and handling security updates.

- **[Visualizing the Dependency Graph](./references/visualization.md)** — `go mod graph` to inspect the full dependency tree, `modgraphviz` to visualize it, and interactive tools to find which dependency chains cause bloat.

## Cross-References

- → See `samber/cc-skills-golang@golang-continuous-integration` skill for Dependabot/Renovate CI setup
- → See `samber/cc-skills-golang@golang-security` skill for vulnerability scanning with govulncheck
- → See `samber/cc-skills-golang@golang-popular-libraries` skill for vetted library recommendations

## Quick Reference

```bash
# Start a new module
go mod init github.com/user/project

# Add a dependency
go get github.com/google/uuid@v1.6.0

# Upgrade all deps (patch only, safer)
go get -u=patch ./...

# Remove unused deps
go mod tidy

# Check for vulnerabilities
govulncheck ./...   # or: go tool govulncheck ./...

# Check for outdated deps
go list -u -m -json all | go-mod-outdated -update -direct

# Analyze binary size by dependency
goweight

# Understand why a dep exists
go mod why -m github.com/some/module

# Visualize dependency graph
go mod graph | modgraphviz | dot -Tpng -o deps.png

# Verify checksums
go mod verify
```
