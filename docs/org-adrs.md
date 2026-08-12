# Org-wide decisions (frostyard/core ADRs)

Conventions this repository follows that are decided at the org level are
recorded as ADRs in
[frostyard/core](https://github.com/frostyard/core/tree/main/docs/adr).
The ones that bind intuneme:

- [ADR-0009 — repository.frostyard.org is the single artifact origin](https://github.com/frostyard/core/blob/main/docs/adr/0009-single-artifact-origin-repository-frostyard-org.md) — release packages are published to it (bucket `frostyardrepo`, base URL `https://repository.frostyard.org`)
- [ADR-0010 — Publish packages through the shared repogen action](https://github.com/frostyard/core/blob/main/docs/adr/0010-publish-packages-via-repogen-to-r2.md) — `release.yml`'s `publish-to-r2` step (`package-type: deb`)
- [ADR-0011 — Distro packages are named frostyard-&lt;tool&gt;](https://github.com/frostyard/core/blob/main/docs/adr/0011-frostyard-prefixed-package-names.md) — `.goreleaser.yaml` nfpm `package_name: frostyard-intuneme` (deb/rpm/apk)
- [ADR-0012 — svu-derived versions, make bump, and the rolling dev prerelease](https://github.com/frostyard/core/blob/main/docs/adr/0012-svu-versioning-and-rolling-dev-prerelease.md) — `.svu.yaml` and `make bump` tag releases; `snapshot.yml` runs `goreleaser release --nightly` after Tests on main, publishing the rolling `dev` prerelease (`incmajor` version template)
- [ADR-0013 — Component releases trigger image rebuilds via repository_dispatch](https://github.com/frostyard/core/blob/main/docs/adr/0013-release-fanout-via-repository-dispatch.md) — `release.yml` dispatches `build` to frostyard/snosi (note: the step's `if` guard checks for a branch ref, so it is currently skipped on tag-triggered runs)
- [ADR-0018 — Org-wide agent instruction and knowledge surfaces](https://github.com/frostyard/core/blob/main/docs/adr/0018-org-wide-agent-instruction-and-knowledge-surfaces.md) — `AGENTS.md` is canonical; `CLAUDE.md`, `GEMINI.md`, and `.cursorrules` are symlinks to it
- [ADR-0021 — SHA-pinned actions and least-privilege CI workflows](https://github.com/frostyard/core/blob/main/docs/adr/0021-sha-pinned-actions-and-least-privilege-ci.md) — `test.yml`, `release.yml`, `snapshot.yml`, `scorecard.yml`, and `build-container.yml` are SHA-pinned with scoped permissions; `docs.yml` still uses tag pins (`@v6`/`@v5`) and is the remaining gap
- [ADR-0022 — make ci is the canonical gate; TestI* is reserved](https://github.com/frostyard/core/blob/main/docs/adr/0022-make-ci-gate-and-test-naming-filter.md) — there is no single `make ci`/`make check` target yet; the CI gate is `test.yml` (golangci-lint, `go test -race`, vet, gofmt, tidy check, build) and the local pre-commit rule is `make fmt && make lint && make test` per [AGENTS.md](../AGENTS.md); the `TestI` prefix stays reserved for environment-requiring integration tests
- [ADR-0025 — One docs/ tree per repository, in core's four-category shape](https://github.com/frostyard/core/blob/main/docs/adr/0025-consolidate-repository-docs-into-docs.md) — this `docs/` tree (formerly `yeti/`); indexed in [README.md](README.md)
- [ADR-0026 — Distribute core agent skills to repos via sync PRs from core](https://github.com/frostyard/core/blob/main/docs/adr/0026-distribute-core-skills-via-sync-prs.md) — intuneme receives `.agents/skills/` via sync PRs (listed in core's `.github/skills-sync.json`); edit skills in core, not here

Not applicable:
[ADR-0007](https://github.com/frostyard/core/blob/main/docs/adr/0007-frostyard-sysext-filename-pattern.md)
/ [ADR-0008](https://github.com/frostyard/core/blob/main/docs/adr/0008-sysext-distribution-and-update-contract.md)
(sysext filename pattern and distribution contract) — intuneme ships nfpm
deb/rpm/apk packages and a container image, no sysexts.

When changing behavior covered by one of these, update or supersede the ADR
in frostyard/core first, then change this repo in the same effort.
