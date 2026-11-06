# 0004 — Adopt the frostyard repository baseline

- **Status:** Accepted
- **Date:** 2026-11-06

## Context

frostyard/core holds every code repository to 1 checklist, rows B1 to B17 of
[core's repository baseline spec](https://github.com/frostyard/core/blob/main/docs/specs/repository-baseline.md)
([core ADR-0071](https://github.com/frostyard/core/blob/main/docs/adr/0071-hold-every-code-repository-to-one-baseline.md)).
A weekly sweep checks each row. Before this decision, intuneme met only some
rows:

- The gate was spread over the 5 jobs of `test.yml`, with no `make verify`,
  `make check` or `make ci`. `make lint` printed "skipping" and passed when
  golangci-lint was missing or failed, and CI ran golangci-lint `latest`.
- There was no `mise.toml`, no risk-path list, no `.github/copilot-instructions.md`
  alias, no `.claude/skills` link, and no risk-tier section in a PR template.
- `docs/superpowers/` (a framework tree that B16 forbids), 28 finished plans
  in `docs/plans/`, a root development log (`notes.md`) and a superseded
  `test_build.sh` were still in the tree.
- `README.md` did not follow core's README spec (B17): it held the full
  problem-records contract and the helper-security notes.

## Decision

- **Gate.** The `Makefile` has `verify` (tidy diff, vet, gofmt check, the
  pinned golangci-lint, the template gate, the race tests; it changes no
  file), `check` (`fmt`, then `verify`) and `ci` (`verify`, the corrections
  check against `BASE`, `govulncheck`, the build). `ci` has no recipe: its
  prerequisites are the parts. `.github/workflows/ci.yml` (formerly `test.yml`,
  workflow name `Tests`) runs each prerequisite as its own `make <target>`
  step ([core ADR-0073](https://github.com/frostyard/core/blob/main/docs/adr/0073-let-ci-run-the-make-ci-targets-as-separate-jobs.md)).
  The old `Lint` and `Test` jobs are now part of the `Verify` job.
- **Tool pins.** `mise.toml` and `mise.lock` pin golangci-lint 2.15.0 and
  Node 24.22.2. Go comes from `go.mod` only. `make lint` fails unless the
  installed golangci-lint is the pin.
- **Template gate.** `scripts/check-template.mjs`, `scripts/check-corrections.mjs`,
  `scripts/lib/risk-paths.mjs` and `test/check-template.test.mjs` are
  byte-identical copies from bketelsen/agentic-template. They check the agent
  aliases, the `docs/README.md` index, the risk-path list, the corrections
  file and the hash pins of the 2 files copied from core.
- **Risk paths.** `.github/risk-paths.json` lists the governance files, the
  release configuration, every file that runs `sudo`, the scripts, the polkit
  files and the container image, each with a category of core's risk-paths
  spec (`risk_paths_test.go` checks both); `.github/workflows/risk-paths.yml` calls
  core's labeller ([core ADR-0068](https://github.com/frostyard/core/blob/main/docs/adr/0068-label-pull-requests-that-touch-high-risk-paths.md)).
- **Leftovers.** `docs/superpowers/`, the 28 finished plans, `notes.md` and
  `test_build.sh` are deleted. Each plan's feature is in the code, and its
  facts that still hold are in `docs/design/` and `docs/specs/`. Git history
  keeps the rest.
- **README.** `README.md` is a `tool` README. The problem-records contract and
  the helper-security notes moved to the site (`site/reference/`).

## Consequences

- A contributor runs `mise install`, then `make check` or `make ci`. The same
  targets run in CI, so a local pass predicts the CI result.
- A golangci-lint bump is 1 edit of `mise.toml` and a `mise lock`, not a
  workflow change.
- A pull request that touches a listed risk path gets the `risk:high` label
  and needs its owner's review before it is ready.
- `.memory/corrections.jsonl` is append-only from this change on; `make ci
  BASE=<ref>` checks it.
- The docs site still uses MkDocs. The move to ProperDocs and MaterialX
  ([core ADR-0070](https://github.com/frostyard/core/blob/main/docs/adr/0070-build-tool-docs-sites-with-materialx-on-github-pages.md),
  row B9) and the move to Go 1.27 (row B11) are separate changes, because
  each can change behavior.

## Alternatives considered

- **Run `make ci` in 1 job:** rejected. The verify, vulncheck and build jobs
  run in parallel, and a failure names its part.
- **Keep the finished plans as history:** rejected. They describe the code as
  it was in February and March 2026, and git history keeps them.

## References

- Shapes: [design/overview.md](../design/overview.md),
  [specs/container-image.md](../specs/container-image.md)
- Builds on: [core ADR-0071](https://github.com/frostyard/core/blob/main/docs/adr/0071-hold-every-code-repository-to-one-baseline.md),
  [core ADR-0072](https://github.com/frostyard/core/blob/main/docs/adr/0072-write-every-readme-to-one-standard.md),
  [core ADR-0043](https://github.com/frostyard/core/blob/main/docs/adr/0043-pin-repository-tools-in-mise-and-name-the-verify-gate.md),
  [core ADR-0044](https://github.com/frostyard/core/blob/main/docs/adr/0044-expose-the-make-gate-triad-in-every-repository.md)
