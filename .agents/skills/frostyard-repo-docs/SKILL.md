---
name: frostyard-repo-docs
description: Give a frostyard repo core's four-category docs/ shape (adr/, design/, specs/, plans/ + indexed README), migrate any legacy yeti/ or cairn/ AI-docs tree into it, and keep the design/spec docs current with .memory/ learnings folded in. Use whenever asked to update a frostyard repo's architecture docs, after landing a significant change, when docs have drifted from the code, to retire a yeti/ or cairn/ directory, or to scaffold docs/ in a repo that lacks the shape.
---

# frostyard-repo-docs — core's docs shape, in every repo

Every frostyard repo keeps one documentation tree, `docs/`, in the same
shape as frostyard/core's
([core ADR-0025](https://github.com/frostyard/core/blob/main/docs/adr/0025-consolidate-repository-docs-into-docs.md)):

| Directory | Question | Contents |
| --- | --- | --- |
| `docs/adr/` | **Why** did we choose this? | Repo-local ADRs — immutable once accepted; org-wide decisions go to core instead |
| `docs/design/` | **How** does it fit together? | Living architecture docs; `overview.md` is the entry point |
| `docs/specs/` | **What exactly** is the contract? | Precise interface/format definitions, changed only alongside code |
| `docs/plans/` | **When/in what order** do we build? | Phased plans with "Done when" outcomes |

`docs/README.md` carries this table plus an index of every doc.
`docs/org-adrs.md` lists the core ADRs binding the repo, linked from the
index. There is no separate "AI docs" directory — *all* docs are written to
be maximally useful in an agent's context window (style rules below). This
skill descends from the retired yeti service's doc-maintainer job
([prompt](https://github.com/frostyard/yeti/blob/main/src/policies/doc-maintainer.md)).

## Step 0 — Scaffold and migrate (once per repo)

If the repo lacks the shape, or still has a `yeti/`/`cairn/` tree:

1. Create the four category directories and seed each with core's
   canonical template (do not invent your own):
   `https://raw.githubusercontent.com/frostyard/core/main/docs/<category>/TEMPLATE.md`
   for `adr`, `design`, `specs`, `plans`.
2. Create `docs/README.md` modeled on
   `https://raw.githubusercontent.com/frostyard/core/main/docs/README.md`:
   the category table, an index section per category, and the conventions
   block (new docs start from the category TEMPLATE; new decision → new
   ADR with the next number; adding a doc means indexing it). Index every
   pre-existing `docs/*.md` file where it stands — do **not** force-move
   uncategorized process docs; categorize them opportunistically later.
3. Migrate the legacy tree with `git mv`:
   - `OVERVIEW.md` → `docs/design/overview.md`
   - contract/reference docs (APIs, file formats, config references) →
     `docs/specs/<name>.md`; other subsystem docs → `docs/design/<name>.md`
   - `learnings/*` → fold each into the right doc now and delete it; a
     learning not yet foldable moves to the `.memory/` inbox.

   The old directory must end up gone.
4. Update **every** reference to the old paths — they are load-bearing.
   Search broadly (`grep -rIn 'yeti\|cairn' . --exclude-dir=.git`) and
   expect hits in: `AGENTS.md`/`CLAUDE.md`/`.github/copilot-instructions.md`,
   workflow `paths-ignore` lists (rename, don't drop — though `docs/**` is
   often already listed, making the old entry deletable), coverage ignores
   (`codecov.yml`), `.mill.toml` context docs, `.github/prompts/*.md`,
   `.knowledge/README.md`, and doc-consistency tests that pin literal old
   paths (e.g. chairlift's `internal/installcheck`).
5. Add (or update) a short "Documentation rules" section in `AGENTS.md`
   pointing at `docs/README.md`'s table and conventions, and stating that
   repo-local decisions get an ADR in `docs/adr/` while org-wide ones go
   to frostyard/core.
6. Run the repo's gate (`make ci` or equivalent). Fix what the move broke;
   weaken nothing.
7. Commit the scaffold+migration separately from content rewrites:
   `docs: adopt core docs shape; fold yeti/ into docs/ (frostyard/core ADR-0025)`.

## Keeping the docs current

1. Read the codebase first; if updating, also read `docs/design/overview.md`
   **and every doc it links to**. Preserve accurate content, fix anything
   outdated.
2. `docs/design/overview.md` is the entry point and must cover:
   - **Purpose** — what this repo does and its role (2–3 sentences)
   - **Architecture** — key directories, modules, how they fit together
   - **Key Patterns** — important conventions, data flow, design decisions
   - **Configuration** — key config values and environment variables

   Keep it 200–500 lines; link out rather than inline detail.
3. Complex subsystems get dedicated docs — `docs/design/` for how-it-works,
   `docs/specs/` for exact contracts — one subject each, linked from
   overview.md and indexed in `docs/README.md`.
4. **Record decisions where you find them.** A "why is it done this way?"
   whose answer is "someone decided" deserves an ADR in `docs/adr/` (next
   free number, from the TEMPLATE) — or, if it binds more than this repo,
   an ADR in frostyard/core plus a line in `docs/org-adrs.md`.
5. **Drain the inbox.** `.memory/` entries (corrections, learnings) are
   seeds, not archives: fold each into the right doc — or into `AGENTS.md`
   when it is a rule of engagement — then delete the entry. The inbox must
   trend toward empty.
6. Mine merged PRs and closed issues since the last docs commit for
   undocumented decisions. Only document what the current code reflects.
7. Keep links bidirectional (ADR ↔ design ↔ spec ↔ plan) and the index
   complete. Docs only — no code changes in the same commit. Commit as:
   `docs: update architecture docs`.

## Capturing learnings between passes

While doing *any* work in a frostyard repo, when you hit a workaround for an
upstream bug (link the issue), a non-obvious pattern required for
correctness, a non-obvious convention, or a hard-won trial-and-error
discovery — fold it into the right `docs/` page immediately if small, or
drop it in `.memory/` to be folded by the next pass. Never write one-off
task notes, obvious knowledge, changelogs, or "append here" sections.

## Writing style (applies to all of docs/)

Optimize for a reader with a context window: dense, factual, structured;
state invariants and the *why* behind them; name exact paths, commands, and
constants; skip marketing prose and content duplicated from the README. If
a fact is enforced by a test or guard, say which one.
