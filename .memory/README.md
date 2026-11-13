# `.memory/` — corrections inbox

The single sanctioned inbox for durable learned corrections.

## Record files

Each correction is 1 file in `.memory/corrections/`. 2 pull requests that
each add a record add 2 different files, so they do not conflict.

- **Name:** `<date>-<slug>.json`, matching
  `^(\d{4}-\d{2}-\d{2})-([a-z0-9]+(?:-[a-z0-9]+)*)\.json$`. The date part
  equals the record's `date`; the slug has at most 60 characters.
  - A record from a pull request: slug `pr-<N>-<words>`, for example
    `2026-11-13-pr-412-cache-key.json`. 2 records in 1 pull request use
    different words.
  - A record with no pull request: 2 to 5 words and 4 random hexadecimal
    characters, for example `2026-11-13-agent-scope-rule-3f9a.json`.
  - The slug prefix `legacy-` is reserved for the move from the old
    corrections file.
- **Content:** 1 JSON object with exactly 5 string fields, in this order:
  `date` (a real `YYYY-MM-DD` date), `scope`, `correction`, `evidence` (not
  empty), and `promoted_to` (`""`, or the path the record was promoted to).
- **Canonical form:** the file text is exactly
  `JSON.stringify(record, null, 2) + "\n"`, with the keys in the order
  above. Formatters do not rewrite these files.

  ```json
  {
    "date": "2026-11-13",
    "scope": "ci",
    "correction": "Pin the cache key to mise.lock.",
    "evidence": "frostyard/pocket#412, cross-review of head 1a2b3c4",
    "promoted_to": "test/cache-key.test.mjs"
  }
  ```

- **Permanent:** a record file is never removed or renamed. Its text never
  changes, except that an empty `promoted_to` is filled once.
- The folder holds only record files, and an optional empty `.gitkeep` for a
  repository with no records. No subfolder, no symbolic link.
- `promoted_to` starts empty; when a correction graduates into
  [AGENTS.md](../AGENTS.md), a document under [docs/](../docs/README.md), or a
  skill, set it to that path. Promotion is the only sanctioned duplication.
- Never record credentials or non-public vulnerability details.
- A blocking review point adds a record in the PR that fixes it (core
  [quality loop](https://github.com/frostyard/core/blob/main/docs/design/quality-loop.md),
  Learn station).

This repository has 1 form: the folder. The old single file does not come
back. Its records moved here as `legacy-` record files.

## Tools

- `make verify` runs this repository's `scripts/check-corrections.mjs`, which
  checks the folder form. `make ci BASE=<ref>` adds `--base <ref>`: it also
  checks that no record is removed, renamed or changed, except a filled empty
  `promoted_to`.
- Core's `new-correction.mjs` is an optional helper that writes 1 record file
  in canonical form
  ([source](https://github.com/frostyard/core/blob/main/scripts/new-correction.mjs)).
  Run it from a copy outside this repository; it is not vendored here. A
  file written by hand is checked the same way.
