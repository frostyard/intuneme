---
name: frostyard-go-repo
description: Structure, build, test, and release a frostyard Go repository the way updex does — SDK-first layout, clix CLI entry, Makefile check gate, golangci-lint v2, GoReleaser Pro with svu-tagged releases and a nightly dev snapshot. Use whenever creating a new frostyard Go repo, adding CLI/release tooling to one, or making an existing one match org conventions.
---

# Build a frostyard Go repository

Produce a Go repo that matches frostyard's house pattern, extracted from
[frostyard/updex](https://github.com/frostyard/updex) — the canonical
exemplar; when this document and updex disagree, updex wins. "Done" is: `make
check` passes, CI is green on the test workflow, and a tag would produce a
GoReleaser release.

## Layout: SDK-first

All logic lives in a public SDK package; the CLI is a thin shell over it.
For a project named `<name>`:

```
go.mod                    module github.com/frostyard/<name>, current Go (updex: 1.26)
<name>/                   public SDK: a Client struct + methods; the real API
cmd/<name>/               Cobra command handlers: parse flags, call SDK, format output
cmd/<name>-cli/main.go    entry point: clix.App{Version, Commit, Date, BuiltBy}.Run(NewRootCmd())
internal/                 unexported helpers (retry, testutil, …)
tests/e2e/                black-box tests: build the real binary, run it as a subprocess
scripts/                  completions.sh, manpages.sh (GoReleaser before-hooks)
```

Rules:

- **SDK code never imports CLI packages.** SDK methods take a
  `context.Context` and an options struct, return result structs + error.
  Never add package-level flag state to the SDK — extend the options struct.
- Use the org commons: [`github.com/frostyard/clix`](https://github.com/frostyard/clix)
  (fang/cobra wrapper: version strings, common flags, JSON output, reporter
  factory) and [`github.com/frostyard/std`](https://github.com/frostyard/std).
  `main.go` sets `version/commit/date/builtBy` vars for ldflags injection —
  copy `updex/cmd/updex-cli/main.go`.
- CLI output: JSON behind a `--json` flag, text tables otherwise.
- Error messages: lowercase, no trailing punctuation, wrap with
  `fmt.Errorf("context: %w", err)`.
- Modern Go idioms: `any`, `slices`/`maps`/`cmp`, `t.Context()`,
  `wg.Go()`, `omitzero` JSON tags.
- Tests: `t.TempDir()` for filesystem work; mockable `Runner` interfaces for
  anything that shells out (see `updex/sysext/mock_runner.go`); e2e tests
  stay read-only so they need no root.

## Steps

1. **Scaffold** the layout above. Start `go.mod` with
   `go mod init github.com/frostyard/<name>`.
2. **Copy the toolchain files from updex** and substitute the project name:
   - `Makefile` — targets `build`, `install`, `clean`, `fmt`, `lint`,
     `test`, `test-cover`, `tidy`, `check` (= fmt + lint + test), `bump`,
     `help`. `build` injects `-X main.version/commit/date/builtBy` ldflags.
   - `.golangci.yml` — v2 config, `default: standard` linters, gofmt
     formatter, errcheck excluded in `_test.go`.
   - `.svu.yaml` — svu config (`tag.prefix: "v"`, `v0: true`).
   - `.goreleaser.yaml` — `version: 2`, `pro: true`; CGO disabled,
     linux amd64+arm64, `-trimpath`, ldflags as above; before-hooks run
     `scripts/completions.sh` and `scripts/manpages.sh`; nfpm deb/rpm/apk
     packages named `frostyard-<name>` shipping completions + manpage;
     conventional-commit changelog groups; `nightly:` publishing a single
     `dev` tag pre-release (`keep_single_release: true`).
   - `scripts/completions.sh` / `scripts/manpages.sh` — build the binary,
     emit `completions/<name>.{bash,zsh,fish}` and `manpages/<name>.1.gz`
     (clix/fang provides the `completion` and `man` subcommands).
3. **Copy the workflows** from `updex/.github/workflows/`:
   - `test.yml` — jobs: golangci-lint, govulncheck, unit tests with Codecov
     OIDC upload, e2e tests, `-race`, verify (`go mod tidy` diff, `go vet`,
     `gofmt -l`), and a linux amd64/arm64 build matrix. Pin actions by SHA.
   - `release.yml` — on tag push: GoReleaser Pro (`distribution:
     goreleaser-pro`, needs `secrets.GORELEASER_KEY`) then publish packages
     to the org apt/rpm repo via `frostyard/repogen`'s `publish-to-r2`
     action (needs the `R2_*` secrets).
   - `snapshot.yml` — nightly GoReleaser `release --nightly --clean` under
     the `dev` tag after green `main` tests. Keep its concurrency group
     `cancel-in-progress: true`: overlapping runs recreate the same release
     and collide uploading identically named assets.
4. **Write the repo's agent surface** (CLAUDE.md/AGENTS.md per the repo's
   instruction-surface convention): build commands, architecture map,
   code-pattern rules — mirror updex's CLAUDE.md sections.
5. **Verify**: `make check` passes locally; push and confirm the test
   workflow is green; `git tag v0.1.0 && git push origin v0.1.0` (or `make
   bump`, which runs build/test/fmt/lint, requires a clean tree, then tags
   `svu next` and pushes the tag) produces a GoReleaser release.

## Releasing day-to-day

- Use conventional commits (`feat:`, `fix:`, `docs:`, …) — the changelog
  groups by them and svu derives the next version from them.
- `make bump` is the release command; never hand-craft versions.
- The `dev` pre-release is the rolling snapshot; real releases are svu tags.

## Pitfalls

- GoReleaser config is **Pro** (`pro: true`); running it with the OSS
  distribution fails. CI needs `GORELEASER_KEY`.
- The nfpm package name is `frostyard-<name>`, not `<name>` — keep that
  prefix so org packages sort together in the apt/rpm repo.
- `make lint` silently skips if golangci-lint isn't installed — CI is the
  backstop, so don't treat a quiet local `make check` as proof lint ran.
- Don't disable the snapshot workflow's `cancel-in-progress` — see step 3.
