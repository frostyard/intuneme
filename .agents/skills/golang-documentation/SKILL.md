---
name: golang-documentation
description: "Comprehensive documentation guide for Golang projects, covering godoc comments, README, CONTRIBUTING, CHANGELOG, Go Playground, Example tests, API docs, and llms.txt. Use when writing or reviewing doc comments, documentation, adding code examples, setting up doc sites, or discussing documentation best practices. Triggers for both libraries and applications/CLIs."
user-invocable: true
license: MIT
compatibility: Designed for Claude Code, Codex or similar harness, and for projects using Golang.
metadata:
  author: samber
  version: "1.3.2"
  openclaw:
    emoji: "📝"
    homepage: https://github.com/samber/cc-skills-golang
    requires:
      bins:
        - go
    install: []
paths:
  - "**/*.go"
---

> **Vendored skill.** Copied from [samber/cc-skills-golang](https://github.com/samber/cc-skills-golang/tree/bb1d43d98a3f8ab639a0042f90d0ec524e39a477/skills/golang-documentation) at commit `bb1d43d98a3f8ab639a0042f90d0ec524e39a477` under the MIT licence ([LICENSE](LICENSE)). frostyard/core manages this copy: change it in core, never in a consumer ([shared skills](https://github.com/frostyard/core/blob/main/docs/design/shared-skills.md#vendored-go-skills)).
>
> **Frostyard override.** When this skill disagrees with the repository's `AGENTS.md`, the `frostyard-go-repo` skill or a frostyard/core ADR, they win. A pointer to `samber/cc-skills-golang@golang-<name>` means the local `golang-<name>` skill when it exists; frostyard does not vendor the others. Do not add a third-party module that this skill suggests (for example `samber/*`) without the owner's approval. Pin tools in the repository's `mise.toml` and `mise.lock` ([ADR-0043](https://github.com/frostyard/core/blob/main/docs/adr/0043-pin-repository-tools-in-mise-and-name-the-verify-gate.md)), not with `go install …@latest`, `go get -tool`, a `tools.go` file or `brew`, and run the gate through `make verify`, `make check` and `make ci` ([ADR-0044](https://github.com/frostyard/core/blob/main/docs/adr/0044-expose-the-make-gate-triad-in-every-repository.md)). The orchestration and thinking hints (sub-agents, `ultracode`, `ultrathink`) are Claude Code hints: the session's own rules for subagents win.
>
> - Keep the repository's `docs/` tree in the shape of the `frostyard-repo-docs` skill ([ADR-0025](https://github.com/frostyard/core/blob/main/docs/adr/0025-consolidate-repository-docs-into-docs.md)). `CONTRIBUTING.md` is a symlink to `AGENTS.md` ([ADR-0029](https://github.com/frostyard/core/blob/main/docs/adr/0029-acmm-conformance-via-canonical-aliases.md)): never write a separate file.
> - The changelog comes from GoReleaser's conventional-commit groups: do not add `CHANGELOG.md`. Do not add `llms.txt`, or register a repository with an outside documentation index, without the owner's approval.
> - Installation docs name the frostyard APT repository and the GoReleaser packages ([ADR-0055](https://github.com/frostyard/core/blob/main/docs/adr/0055-publish-debian-packages-through-the-apt-publisher.md)), never a `curl` of a `latest` URL. Upstream's README, CONTRIBUTING, CHANGELOG and llms.txt templates are not vendored.

**Persona:** You are a Go technical writer and API designer. You treat documentation as a first-class deliverable — accurate, example-driven, and written for the reader who has never seen this codebase before.

**Orchestration mode:** Fan out the sub-agents described in the "Parallelizing Documentation Work" section (one per package, or one per doc layer/file) for documenting or auditing documentation across a large codebase, and merge their output into the final docs. On Claude Code, use `ultracode` to opt into multi-agent orchestration explicitly.

**Modes:**

- **Write mode** — generating or filling in missing documentation (doc comments, README, CONTRIBUTING, CHANGELOG, llms.txt). Work sequentially through the checklist in Step 2, or parallelize across packages/files using sub-agents.
- **Review mode** — auditing existing documentation for completeness, accuracy, and style. Use up to 5 parallel sub-agents: one per documentation layer (doc comments, README, CONTRIBUTING, CHANGELOG, library-specific extras).

> **Community default.** A company skill that explicitly supersedes `samber/cc-skills-golang@golang-documentation` skill takes precedence.

# Go Documentation

Write documentation that serves both humans and AI agents. Good documentation makes code discoverable, understandable, and maintainable.

## Cross-References

- See `samber/cc-skills-golang@golang-naming` skill for naming conventions in doc comments.
- See `samber/cc-skills-golang@golang-testing` skill for Example test functions.
- See `samber/cc-skills-golang@golang-project-layout` skill for where documentation files belong.
- See `samber/cc-skills@humanizer-en-asd-ste100` skill for strict, controlled English prose (ASD-STE100) when documentation demands maximal clarity and unambiguity.

## Writing Principles

Apply to every piece of documentation you write or review:

**Concision** — write the shortest version that carries the idea. Remove ornament and hollow transitions. Never drop facts, warnings, or user-requested depth.

**Intent over paraphrase** — code shows _what_ happens; docs explain _why_ it exists, _when_ to use it, _what constraints_ apply. A comment that only restates the signature wastes the reader's time.

**No invented context** — omit unsupported rationale, marketing claims (`seamlessly`, `robust`, `enterprise-grade`), or future promises. Leave gaps visible rather than filling with speculation.

**Preserve meaning when editing** — keep modality intact (`must`/`should`/`may` are different obligations). Preserve conditions, warnings, required actions. A cleaner sentence that changes obligations is wrong.

**Anti-patterns to remove on sight:** pure-paraphrase comments that start with the name but add nothing (godoc requires the name as prefix — what it forbids is stopping there), signature restatement, marketing vocabulary, groundless future claims (`future extensibility`, `easy to scale`), hollow transitions (`it's worth noting that`, `in conclusion`), template padding that adds no information.

For regulated or safety-critical documentation that requires strict controlled-English prose, → See `samber/cc-skills@humanizer-en-asd-ste100` skill.

## Step 1: Detect Project Type

Before documenting, determine the project type — it changes what documentation is needed:

**Library** — no `main` package, meant to be imported by other projects:

- Focus on godoc comments, `ExampleXxx` functions, playground demos, pkg.go.dev rendering
- See [Library Documentation](./references/library.md)

**Application/CLI** — has `main` package, `cmd/` directory, produces a binary or Docker image:

- Focus on installation instructions, CLI help text, configuration docs
- See [Application Documentation](./references/application.md)

**Both apply**: function comments, README, CONTRIBUTING, CHANGELOG.

**Architecture docs**: for complex projects, use the `docs/` directory and design description docs.

## Step 2: Documentation Checklist

Every Go project needs these (ordered by priority):

| Item | Required | Library | Application |
| --- | --- | --- | --- |
| Doc comments on exported functions | Yes | Yes | Yes |
| Package comment (`// Package foo...`) — MUST exist | Yes | Yes | Yes |
| README.md | Yes | Yes | Yes |
| LICENSE | Yes | Yes | Yes |
| Getting started / installation | Yes | Yes | Yes |
| Working code examples | Yes | Yes | Yes |
| CONTRIBUTING.md | Recommended | Yes | Yes |
| CHANGELOG.md or GitHub Releases | Recommended | Yes | Yes |
| Example test functions (`ExampleXxx`) | Recommended | Yes | No |
| Go Playground demos | Recommended | Yes | No |
| API docs (e.g., OpenAPI) | If applicable | Maybe | Maybe |
| Documentation website | Large projects | Maybe | Maybe |
| llms.txt | Recommended | Yes | Yes |

A private project might not need a documentation website, llms.txt, Go Playground demos...

## Parallelizing Documentation Work

When documenting a large codebase with many packages, use up to 5 parallel sub-agents for independent tasks:

- Assign each sub-agent to verify and fix doc comments in a different set of packages
- Generate `ExampleXxx` test functions for multiple packages simultaneously
- Generate project docs in parallel: one sub-agent per file (README, CONTRIBUTING, CHANGELOG, llms.txt)

## Step 3: Function & Method Doc Comments

Every exported function and method MUST have a doc comment. Document complex internal functions too. Skip test functions.

The comment starts with the function name and a verb phrase. Focus on **why** and **when**, not restating what the code already shows. The code tells you _what_ happens — the comment should explain _why_ it exists, _when_ to use it, _what constraints_ apply, and _what can go wrong_. Include parameters, return values, error cases, and a usage example:

```go
// CalculateDiscount computes the final price after applying tiered discounts.
// Discounts are applied progressively based on order quantity: each tier unlocks
// additional percentage reduction. Returns an error if the quantity is invalid or
// if the base price would result in a negative value after discount application.
//
// Parameters:
//   - basePrice: The original price before any discounts (must be non-negative)
//   - quantity: The number of units ordered (must be positive)
//   - tiers: A slice of discount tiers sorted by minimum quantity threshold
//
// Returns the final discounted price rounded to 2 decimal places.
// Returns ErrInvalidPrice if basePrice is negative.
// Returns ErrInvalidQuantity if quantity is zero or negative.
//
// Play: https://go.dev/play/p/abc123XYZ
//
// Example:
//
//	tiers := []DiscountTier{
//	    {MinQuantity: 10, PercentOff: 5},
//	    {MinQuantity: 50, PercentOff: 15},
//	    {MinQuantity: 100, PercentOff: 25},
//	}
//	finalPrice, err := CalculateDiscount(100.00, 75, tiers)
//	if err != nil {
//	    log.Fatalf("Discount calculation failed: %v", err)
//	}
//	log.Printf("Ordered 75 units at $100 each: final price = $%.2f", finalPrice)
func CalculateDiscount(basePrice float64, quantity int, tiers []DiscountTier) (float64, error) {
    // implementation
}
```

For the full comment format, deprecated markers, interface docs, and file-level comments, see **[Code Comments](./references/code-comments.md)** — how to document packages, functions, interfaces, and when to use `Deprecated:` markers and `BUG:` notes.

## Step 4: README Structure

README SHOULD follow this exact section order. Upstream's README template is not vendored (Frostyard override):

1. **Title** — project name as `# heading`
2. **Badges** — shields.io pictograms (Go version, license, CI, coverage, Go Report Card...)
3. **Summary** — 1-2 sentences explaining what the project does
4. **Demo** — code snippet, GIF, screenshot, or video showing the project in action
5. **Getting Started** — installation + minimal working example
6. **Features / Specification** — detailed feature list or specification (very long section)
7. **Contributing** — link to CONTRIBUTING.md or inline if very short
8. **Contributors** — thank contributors (badge or list)
9. **License** — license name + link

Common badges for Go projects:

```markdown
[![Go Version](https://img.shields.io/github/go-mod/go-version/{owner}/{repo})](https://go.dev/) [![License](https://img.shields.io/github/license/{owner}/{repo})](./LICENSE) [![Build Status](https://img.shields.io/github/actions/workflow/status/{owner}/{repo}/test.yml?branch=main)](https://github.com/{owner}/{repo}/actions) [![Coverage](https://img.shields.io/codecov/c/github/{owner}/{repo})](https://codecov.io/gh/{owner}/{repo}) [![Go Report Card](https://goreportcard.com/badge/github.com/{owner}/{repo})](https://goreportcard.com/report/github.com/{owner}/{repo}) [![Go Reference](https://pkg.go.dev/badge/github.com/{owner}/{repo}.svg)](https://pkg.go.dev/github.com/{owner}/{repo})
```

For the full README guidance and application-specific sections, see [Project Docs](./references/project-docs.md#readmemd).

## Step 5: CONTRIBUTING & Changelog

**CONTRIBUTING.md** — Help contributors get started in under 10 minutes, covering prerequisites, clone, build, test, and PR process. If setup takes longer, improve the process with a Makefile, docker-compose, or devcontainer. See [Project Docs](./references/project-docs.md#contributingmd).

**Changelog** — Track changes using [Keep a Changelog](https://keepachangelog.com/) format or GitHub Releases. Frostyard override: GoReleaser's conventional-commit groups are the changelog; the template is not vendored. Write each entry to answer _what changed for the reader_ — internal refactors without user-visible impact belong in commit history, and a fixed edge case never becomes a broad "reliability improvement" claim. See [Project Docs](./references/project-docs.md#changelog).

## Step 6: Library-Specific Documentation

For Go libraries, add these on top of the basics:

- **Go Playground demos** — create runnable demos and link them in doc comments with `// Play: https://go.dev/play/p/xxx`. Use a Go Playground integration when one is available to create and share playground URLs.
- **Example test functions** — write `func ExampleXxx()` in `_test.go` files. These are executable documentation verified by `go test`.
- **Generous code examples** — include multiple examples in doc comments showing common use cases.
- **godoc** — your doc comments render on [pkg.go.dev](https://pkg.go.dev). Use `go doc` locally to preview; to inspect how a published package renders its docs, symbols, and examples, → See `samber/cc-skills-golang@golang-pkg-go-dev` skill.
- **Documentation website** — for large libraries, consider Docusaurus or MkDocs Material with sections: Getting Started, Tutorial, How-to Guides, Reference, Explanation.
- **Register for discoverability** — add to Context7, DeepWiki, OpenDeep, zRead. Even for private libraries.

See [Library Documentation](./references/library.md) for details.

## Step 7: Application-Specific Documentation

For Go applications/CLIs:

- **Installation methods** — pre-built binaries (GoReleaser), `go install`, Docker images, Homebrew...
- **CLI help text** — make `--help` comprehensive; it's the primary documentation
- **Configuration docs** — document all env vars, config files, CLI flags

See [Application Documentation](./references/application.md) for details.

## Step 8: API Documentation

If your project exposes an API:

| API Style    | Format      | Tool                                         |
| ------------ | ----------- | -------------------------------------------- |
| REST/HTTP    | OpenAPI 3.x | swaggo/swag (auto-generate from annotations) |
| Event-driven | AsyncAPI    | Manual or code-gen                           |
| gRPC         | Protobuf    | buf, grpc-gateway                            |

Prefer auto-generation from code annotations when possible. See [Application Documentation](./references/application.md#api-documentation) for details.

## Step 9: AI-Friendly Documentation

Make your project consumable by AI agents:

- **llms.txt** — add a `llms.txt` file at the repository root. Frostyard override: only with the owner's approval; the template is not vendored. This file gives LLMs a structured overview of your project.
- **Structured formats** — use OpenAPI, AsyncAPI, or protobuf for machine-readable API docs.
- **Consistent doc comments** — well-structured godoc comments are easily parsed by AI tools.
- **Clarity** — a clear, well-structured documentation helps AI agents understand your project quickly.

## Step 10: Delivery Documentation

Document how users get your project:

**Libraries:**

```bash
go get github.com/{owner}/{repo}
```

**Applications:**

```bash
# Pre-built binary
curl -sSL https://github.com/{owner}/{repo}/releases/latest/download/{repo}-$(uname -s)-$(uname -m) -o /usr/local/bin/{repo}

# From source
go install github.com/{owner}/{repo}@latest

# Docker
docker pull {registry}/{owner}/{repo}:latest
```

See [Project Docs](./references/project-docs.md#distribution) for Dockerfile best practices and Homebrew tap setup.
