---
name: frostyard-acmm-conformance
description: Bring a frostyard repo to Hive ACMM conformance the way core and repogen did — one canonical file per criterion, committed relative symlinks at the ACMM-checked paths, real git trees for directory criteria, new artifacts only where nothing canonical exists, all registered in a repo-local ADR and guarded by the docs-integrity gate. Use whenever a repo has open Hive ACMM evaluation issues (label `acmm`, "Opened by Hive ACMM Evaluation"), is flagged at ACMM L0/L2/L3, or is asked to satisfy agentic-fleet-management prerequisites.
---

# frostyard-acmm-conformance — satisfy ACMM criteria without duplicating content

The Hive ACMM evaluation grades repositories by checking that fixed paths
exist. Each generated issue lists acceptable paths and states "the content
can follow your project's conventions" — satisfying **any one** listed path
satisfies the criterion, and only existence is checked. The frostyard
answer ([core ADR-0029](https://github.com/frostyard/core/blob/main/docs/adr/0029-acmm-conformance-via-canonical-aliases.md))
is: never create a second copy of anything. Done looks like: every `acmm`
issue closable by one PR, `scripts/check-docs.mjs` green, zero duplicated
content, and the alias table registered in a repo-local ADR.

Precedents to imitate: core's own conformance (core ADR-0029, core#22–#40)
and repogen's ([repogen ADR-0012](https://github.com/frostyard/repogen/blob/main/docs/adr/0012-acmm-conformance-via-canonical-aliases.md),
repogen#37–#55).

## Steps

1. **Inventory the issues.** `gh issue list --repo frostyard/<repo>
   --label acmm --state open --json number,title,body`. Build a table:
   issue → criterion ID (`acmm:*`) → accepted paths. Set aside anything in
   the range that is *not* a file-existence criterion (substantive
   engineering issues, `hold` labels, already-merged PRs) — those are
   separate work items; never close them from the conformance PR.

2. **Classify each criterion** with three questions, in order:
   - Does canonical content already exist? → **relative symlink** to it.
   - Is the accepted path a directory? → **real git tree** (an evaluator
     reading the tree API sees a symlink as a blob, not a tree; put a
     `README.md` inside so the tree exists, and symlink any payload
     *inside* it if the payload lives elsewhere).
   - Nothing canonical? → **new artifact that does real work** — never a
     content-free stub.

3. **Unify the instruction surface first** (usually criteria `agents-md`,
   `contrib-guide`, `cursor-rules`, `simple-skills`). Merge every existing
   instruction file (`CLAUDE.md`, `.github/copilot-instructions.md`, …)
   into one canonical `AGENTS.md` — keep all content, deduplicate overlap,
   keep it tool-agnostic — then replace the old files with symlinks:
   `CLAUDE.md`, `GEMINI.md`, `CONTRIBUTING.md`, `.cursorrules` →
   `AGENTS.md`; `.github/copilot-instructions.md` → `../AGENTS.md`;
   `.claude/skills` → `../.agents/skills`. Targets are always relative,
   computed from the alias's own directory. `AGENTS.md` opens with a
   header naming its alias set, and gains the rule "conformance alias
   symlinks are listed in ADR-NNNN — edit their canonical targets, never
   the aliases."

4. **Apply the standard mapping** for the rest (adapt paths to what the
   issue accepts):

   | Criterion | Solution |
   | --- | --- |
   | E2E tests | Real tree (`test/e2e/` or `tests/e2e/`) whose README names the repo's actual e2e suite and how to run it; symlink the payload inside if it lives elsewhere |
   | PR template | `.github/pull_request_template.md` mirroring the repo's real gates + docs housekeeping + "aliases untouched" row |
   | Issue templates | `.github/ISSUE_TEMPLATE/` with `config.yml` (`blank_issues_enabled: true` — evaluators file free-form issues) + repo-appropriate templates |
   | Code style config | The config the repo's linter actually runs: `.golangci.yml` (Go, v2 schema), `.prettierrc.json` (docs/JS), etc. — pin what `make lint` does so local and CI agree |
   | Coverage gate | `.coverage-thresholds.json` (schema_version, the three docs-gate rates at 1.0, `never_relax: true`) consumed by `scripts/check-docs.mjs` |
   | Prompts catalog | `.github/prompts/README.md` + `review.prompt.md` runbook naming the repo's real gate commands |
   | EditorConfig | `.editorconfig` for the repo's languages |
   | Correction capture | `.memory/README.md` + empty append-only `corrections.jsonl` (five-field schema from core ADR-0018) |
   | PR acceptance metric | Real `docs/specs/pr-acceptance-metric.md` (must contain `## Definition` and `## Rules` — the gate pins them) + alias `docs/metrics.md` → `specs/pr-acceptance-metric.md` |
   | PR review rubric | Real `docs/specs/pr-review-rubric.md` (rows = the repo's actual verifiable gates) + alias `docs/review-rubric.md` → `specs/pr-review-rubric.md` |
   | Quality dashboard | Real `docs/design/quality-loop.md` (declare→review→gate→learn→observe, wired to the repo's real CI) + alias `docs/quality.md` → `design/quality-loop.md` |
   | Layered safety / mechanical enforcement / structural gates | One `.claude/settings.json` satisfies all of these: deny `gh pr merge`, `gh pr review --approve`, `gh release`, `git push origin main`, force-push, `.env`/secrets reads; ask on `git push` and `gh workflow run` (copy core's) |
   | Session summary | `.claude/session-summary.md` (current state / last landed / next) |

5. **Wire the gate.** Copy core's
   [`scripts/check-docs.mjs`](https://github.com/frostyard/core/blob/main/scripts/check-docs.mjs) and adapt
   only: the `SKIP_DIRS` set (add the repo's build-output dirs) and the
   link-checked file list. It fails CI on any broken or repo-escaping
   symlink, unindexed doc, or dead relative link. Add a `docs-gate` job
   (`node scripts/check-docs.mjs`) to the repo's existing CI workflow,
   matching its existing style.

6. **Record the decision.** New repo-local ADR from `docs/adr/TEMPLATE.md`
   (next free number): the full alias table, the directory-criteria rule,
   "aliases are not docs" (no index entries, no cross-link obligations —
   the canonical target carries those), and the contingency clause: if the
   evaluator ever rejects a symlink for a file criterion, that one alias
   becomes a real stub pointing at the canonical doc — one commit, decision
   stands. Index the ADR and the new real docs (never the aliases) in
   `docs/README.md`.

7. **Verify like the evaluator, then like CI.**
   - `node scripts/check-docs.mjs` — all rates 1.0.
   - `git ls-files -s | awk '$1=="120000"'` — exactly the alias set.
   - `gh api "repos/frostyard/<repo>/contents/<path>?ref=<branch>" --jq
     .type` for every criterion path: file-symlinks return `file`
     (indistinguishable from real files — why this works); directory
     criteria list their tree.
   - The repo's own build gate (`make ci`, or build+fmt+lint+test).
   - One PR: criterion→path table in the body, `Closes #N` per ACMM issue
     only, and the Windows note (checkouts need `core.symlinks=true` or
     WSL; GitHub's web renderer shows a symlinked `.md` as its target
     path).

## Pitfalls

- **A symlinked directory criterion.** The trees API shows mode 120000 as
  a blob; the criterion may not count it. Real tree with a README, payload
  symlinked inside.
- **Pinning the linter config surfaces pre-existing failures.** That means
  the gate was already red (verify: run with `--no-config` and compare) —
  fix the findings in the same PR; never loosen the config to hide them.
- **Three governance issues, one file.** Layered safety, mechanical
  enforcement, and structural gates all accept `.claude/settings.json` —
  don't invent three artifacts.
- **Editing an alias.** Any content change lands on the canonical target;
  the ADR's table is the registry. The gate catches broken links but not
  misdirected edits — GitHub's renderer showing target-path-instead-of-
  content is the tell.
- **Closing too much.** ACMM ranges can interleave real engineering issues
  and already-merged PRs; the conformance PR closes file-existence criteria
  only.
- **Forked repos:** every `gh` command needs an explicit
  `--repo frostyard/<repo>` or it may target the upstream parent.
