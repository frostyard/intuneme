# Org-wide decisions (frostyard/core ADRs)

Conventions this repository follows that are decided at the org level are
recorded as ADRs in
[frostyard/core](https://github.com/frostyard/core/tree/main/docs/adr).
The ones that bind intuneme:

- [ADR-0009 — repository.frostyard.org is the single artifact origin](https://github.com/frostyard/core/blob/main/docs/adr/0009-single-artifact-origin-repository-frostyard-org.md) — the `frostyard-intuneme` package is served from it; its Debian namespace is superseded by ADR-0055 (`https://repository.frostyard.org/debian/`)
- [ADR-0011 — Distro packages are named frostyard-&lt;tool&gt;](https://github.com/frostyard/core/blob/main/docs/adr/0011-frostyard-prefixed-package-names.md) — `.goreleaser.yaml` nfpm `package_name: frostyard-intuneme` (deb/rpm/apk)
- [ADR-0012 — svu-derived versions, make bump, and the rolling dev prerelease](https://github.com/frostyard/core/blob/main/docs/adr/0012-svu-versioning-and-rolling-dev-prerelease.md) — `.svu.yml` and `make bump` tag releases; `snapshot.yml` runs `goreleaser release --nightly` after Tests on main, publishing the rolling `dev` prerelease (`incmajor` version template)
- [ADR-0018 — Org-wide agent instruction and knowledge surfaces](https://github.com/frostyard/core/blob/main/docs/adr/0018-org-wide-agent-instruction-and-knowledge-surfaces.md) — `AGENTS.md` is canonical; `CLAUDE.md`, `GEMINI.md`, `.cursorrules` and `.github/copilot-instructions.md` are symlinks to it, and `.memory/corrections.jsonl` is the append-only corrections inbox
- [ADR-0019 — Governance as code and risk tiers](https://github.com/frostyard/core/blob/main/docs/adr/0019-governance-as-code-and-risk-tiers.md) — `.github/pull_request_template.md` has a `Risk tier` section
- [ADR-0021 — SHA-pinned actions and least-privilege CI workflows](https://github.com/frostyard/core/blob/main/docs/adr/0021-sha-pinned-actions-and-least-privilege-ci.md) — every workflow is SHA-pinned with top-level `permissions: {}` (or `read-all` in `scorecard.yml`) and job-level grants, and every checkout sets `persist-credentials: false`; `workflow_pinning_test.go` enforces the pins
- [ADR-0025 — One docs/ tree per repository, in core's four-category shape](https://github.com/frostyard/core/blob/main/docs/adr/0025-consolidate-repository-docs-into-docs.md) — this `docs/` tree (formerly `yeti/`); indexed in [README.md](README.md)
- [ADR-0026 — Distribute core agent skills to repos via sync PRs from core](https://github.com/frostyard/core/blob/main/docs/adr/0026-distribute-core-skills-via-sync-prs.md) — intuneme receives `.agents/skills/` via sync PRs (listed in core's `.github/skills-sync.json`); edit skills in core, not here
- [ADR-0029 — ACMM conformance via canonical aliases](https://github.com/frostyard/core/blob/main/docs/adr/0029-acmm-conformance-via-canonical-aliases.md) — the alias symlinks above and `.claude/skills` → `.agents/skills`
- [ADR-0038 — Scope the test-name filter to chairlift](https://github.com/frostyard/core/blob/main/docs/adr/0038-scope-the-test-name-filter-to-chairlift.md) — supersedes ADR-0022's `TestI*` filter; intuneme's gate runs every test
- [ADR-0040 — Publish the repository settings contract](https://github.com/frostyard/core/blob/main/docs/adr/0040-publish-the-repository-settings-contract.md) — `SECURITY.md` names GitHub private vulnerability reporting
- [ADR-0043 — Pin repository tools in mise and name the verify gate](https://github.com/frostyard/core/blob/main/docs/adr/0043-pin-repository-tools-in-mise-and-name-the-verify-gate.md) — `mise.toml` and `mise.lock` pin golangci-lint and Node; `make verify` changes no file
- [ADR-0044 — Expose the make gate triad in every repository](https://github.com/frostyard/core/blob/main/docs/adr/0044-expose-the-make-gate-triad-in-every-repository.md) — `make verify`, `make check` and `make ci`; `ci` runs `verify`
- [ADR-0053 — Name the svu config .svu.yml](https://github.com/frostyard/core/blob/main/docs/adr/0053-name-the-svu-config-svu-yml.md) — `.svu.yml`
- [ADR-0055 — Publish Debian packages through the apt-publisher single writer](https://github.com/frostyard/core/blob/main/docs/adr/0055-publish-debian-packages-through-the-apt-publisher.md) — `release.yml`'s `publish-deb` request to frostyard/apt-publisher, which publishes the attested `.deb` to `trixie` and `forky` (replaces ADR-0010's repogen `publish-to-r2` step, via ADR-0048)
- [ADR-0056 — The APT publisher triggers image rebuilds after packages are live](https://github.com/frostyard/core/blob/main/docs/adr/0056-rebuild-images-after-apt-publication.md) — apt-publisher, not `release.yml`, dispatches `build` to frostyard/snosi (supersedes ADR-0013, whose default-branch-guarded step never fired on tag runs); pinned by `release_workflow_contract_test.go`
- [ADR-0068 — Label pull requests that touch high-risk paths](https://github.com/frostyard/core/blob/main/docs/adr/0068-label-pull-requests-that-touch-high-risk-paths.md) — `.github/risk-paths.json` and the `risk-paths.yml` caller
- [ADR-0069 — Report problems local-first, opt-in, with one sender per host](https://github.com/frostyard/core/blob/main/docs/adr/0069-report-problems-local-first-opt-in-with-one-sender-per-host.md) — `internal/problemrec` writes panics and 4 coded errors to the local journal with `std/problems` and never sends; see [ADR-0003](adr/0003-record-problems-with-std-problems.md)
- [ADR-0070 — Build tool docs sites with MaterialX on GitHub Pages](https://github.com/frostyard/core/blob/main/docs/adr/0070-build-tool-docs-sites-with-materialx-on-github-pages.md) — the MkDocs site in `site/` still has to move to ProperDocs and MaterialX (a separate change)
- [ADR-0071 — Hold every code repository to one baseline](https://github.com/frostyard/core/blob/main/docs/adr/0071-hold-every-code-repository-to-one-baseline.md) — rows B1 to B17; see [ADR-0004](adr/0004-adopt-the-repository-baseline.md)
- [ADR-0072 — Write every README to one standard](https://github.com/frostyard/core/blob/main/docs/adr/0072-write-every-readme-to-one-standard.md) — `README.md` is a `tool` README
- [ADR-0073 — Let CI run the make ci targets as separate jobs](https://github.com/frostyard/core/blob/main/docs/adr/0073-let-ci-run-the-make-ci-targets-as-separate-jobs.md) — `ci.yml` runs each prerequisite of `make ci` as its own step

Not applicable:
[ADR-0007](https://github.com/frostyard/core/blob/main/docs/adr/0007-frostyard-sysext-filename-pattern.md)
/ [ADR-0008](https://github.com/frostyard/core/blob/main/docs/adr/0008-sysext-distribution-and-update-contract.md)
(sysext filename pattern and distribution contract) — intuneme ships nfpm
deb/rpm/apk packages and a container image, no sysexts.

When changing behavior covered by one of these, update or supersede the ADR
in frostyard/core first, then change this repo in the same effort.
