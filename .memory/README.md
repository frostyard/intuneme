# `.memory/` — corrections inbox

The single sanctioned inbox for durable learned corrections.

Contract:

- `corrections.jsonl` is append-only. It has one JSON object per line. It has 5 fields, and every field is required:

  ```json
  {"date":"YYYY-MM-DD","scope":"…","correction":"…","evidence":"…","promoted_to":""}
  ```

- `promoted_to` starts empty. When the correction graduates into `AGENTS.md`, a document under `docs/`, or a skill, set it to that path. Promotion is the only sanctioned duplication.
- Never record credentials or non-public vulnerability details.
- `make verify` validates every JSON line and every required field.
- `make ci BASE=<ref>` checks the append-only rule in CI.
- A blocking review point adds 1 line in the PR that fixes it.
