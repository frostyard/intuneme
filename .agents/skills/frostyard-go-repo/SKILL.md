---
name: frostyard-go-repo
description: Structure, build, test, and release a frostyard Go repository the way updex does — SDK-first layout, clix CLI entry, Makefile check gate, golangci-lint v2, GoReleaser Pro with svu-tagged releases and a nightly dev snapshot. Use whenever creating a new frostyard Go repo, adding CLI/release tooling to one, making an existing one match org conventions, or preparing, verifying or patching a release.
---

# Build a frostyard Go repository

Produce a Go repo that matches frostyard's house pattern, extracted from
[frostyard/updex](https://github.com/frostyard/updex) — the canonical
exemplar; when this document and updex disagree, updex wins, except for the
Fleet toolchain table below, which wins over every repository. "Done" is:
`make check` passes, CI is green on the CI workflow, and a tag would produce
a GoReleaser release.

## Fleet toolchain

Every frostyard Go repository uses one Go minor and one golangci-lint
version
([core ADR-0071](https://github.com/frostyard/core/blob/main/docs/adr/0071-hold-every-code-repository-to-one-baseline.md),
baseline row B11):

| Tool | Version | Where the repository pins it |
| --- | --- | --- |
| Go | 1.27 | `go.mod`: `go 1.27.<patch>`, a full version, the only Go pin ([ADR-0043](https://github.com/frostyard/core/blob/main/docs/adr/0043-pin-repository-tools-in-mise-and-name-the-verify-gate.md)) |
| golangci-lint | 2.15.0 | `mise.toml`: `golangci-lint = "2.15.0"`; no workflow or Makefile pins another release |

The pair moves together. A golangci-lint release built with Go N comes
before Go N. The repository-hygiene line proposes a bump as one core pull
request that changes this table; then each repository follows in its own
pull request, and its CI is the compatibility proof. The `@go` skills-sync
group carries this table to every Go repository.

## Layout: SDK-first

All logic lives in a public SDK package; the CLI is a thin shell over it.
For a project named `<name>`:

```
go.mod                    module github.com/frostyard/<name>, the fleet Go minor (see Fleet toolchain)
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
   - `.svu.yml` — svu config (`tag.prefix: "v"`, `v0: true`). svu reads
     only `.svu.yml`; a `.svu.yaml` is silently ignored
     ([core ADR-0053](https://github.com/frostyard/core/blob/main/docs/adr/0053-name-the-svu-config-svu-yml.md)).
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
   - `test.yml` (name it `ci.yml` in a new repository, ADR-0071 row B12;
     its jobs run `make ci`) — jobs: golangci-lint, govulncheck, unit tests with Codecov
     OIDC upload, e2e tests, `-race`, verify (`go mod tidy` diff, `go vet`,
     `gofmt -l`), and a linux amd64/arm64 build matrix. Pin actions by SHA.
   - `release.yml` — on tag push, three steps:
     1. GoReleaser Pro (`distribution: goreleaser-pro`, needs
        `secrets.GORELEASER_KEY`).
     2. `actions/attest-build-provenance` over `dist/checksums.txt` and the
        packages; the job grants `id-token: write` and
        `attestations: write`.
     3. A `publish-deb` `repository_dispatch` to `frostyard/apt-publisher`
        with `repo` and `tag`, using `secrets.APT_PUBLISH_TOKEN`. Don't
        guard it, and don't mark it `continue-on-error`.

     apt-publisher publishes the `.deb` files to
     `https://repository.frostyard.org/debian/`, then dispatches `build`
     to the image repos
     ([core ADR-0055](https://github.com/frostyard/core/blob/main/docs/adr/0055-publish-debian-packages-through-the-apt-publisher.md),
     [ADR-0056](https://github.com/frostyard/core/blob/main/docs/adr/0056-rebuild-images-after-apt-publication.md)).
     So the repo holds no signing or R2 credentials, and doesn't dispatch
     `build` itself. Before the first release:
     - register the repo in apt-publisher's `config/producers.tsv`;
     - share the org `APT_PUBLISH_TOKEN` secret with it.
   - `snapshot.yml` — nightly GoReleaser `release --nightly --clean` under
     the `dev` tag after green `main` tests. Use this exact top-level block:

     ```yaml
     concurrency:
       group: goreleaser-nightly
       cancel-in-progress: true
     ```

     Concurrency groups are repository-scoped, so do not substitute the
     project name. Cancelling a stale run selects the newest tested `main`
     commit and prevents overlapping uploads to the singleton release.
4. **Write the repo's agent surface** (CLAUDE.md/AGENTS.md per the repo's
   instruction-surface convention): build commands, architecture map,
   code-pattern rules — mirror updex's CLAUDE.md sections.
5. **Verify**: `make check` passes locally; push and confirm the test
   workflow is green. The first release is a maintainer's step, not an
   agent's: a maintainer runs `make bump` (build/test/fmt/lint, a clean
   tree, then tags `svu next` and pushes the tag), and the tag push produces
   the GoReleaser release.

## Releasing day-to-day

- Use conventional commits (`feat:`, `fix:`, `docs:`, …) — the changelog
  groups by them and svu derives the next version from them.
- `make bump` is the release command; never hand-craft versions. A
  maintainer runs it: agents never tag, push a tag, or publish a release.
  An agent that finds a release due prepares it (below), proposes the
  version (`svu next` is read-only) and asks the maintainer to tag.
- The `dev` pre-release is the rolling snapshot; real releases are svu tags.

### Before the tag

1. **Check the guards.** `mise.toml` pins `svu` (updex and std pin
   `svu = "3.4.1"`): an svu older than v3 does not read the `.svu.yml`
   format and gives no error. `make bump` runs `svu-version-check` first,
   which refuses any svu other than the pin
   ([std#90](https://github.com/frostyard/std/pull/90),
   [updex#449](https://github.com/frostyard/updex/pull/449)), and
   `bump-main-check`, which refuses a `HEAD` that is not `main` or not
   equal to `origin/main`
   ([updex#510](https://github.com/frostyard/updex/pull/510)). A repository
   without these targets gets them from updex's `Makefile` in a pull request
   before the release.
2. **Name the release commit.** It is the `origin/main` SHA on the day of
   the tag. Check that the CI workflow is green at that SHA
   (`gh run list -R OWNER/REPO --commit SHA`) and run `make ci` there. When
   `main` moves after a draft, name the new SHA and do steps 2 to 4 again: a
   merge after the draft changes the release commit.
3. **Compare the versions.** Run `svu next`, and `svu patch` or
   `svu minor` for the alternatives. When `svu next` is not the version the
   maintainer approved, say why. Two causes seen in the fleet:
   - Dependabot commits with a `feat(deps)` prefix make `svu next` a minor
     bump for dependency updates only.
   - A behaviour change merged as `fix:` stays a patch for svu, but before
     1.0 it needs a minor bump.

   `make bump` can tag only `svu next`. For another version, give the
   maintainer the steps that `make bump` does, with its guards, and the
   version and SHA written out, in this order:
   1. `git switch main && git pull --ff-only`, then `make svu-version-check`.
   2. `make bump-main-check`, and a check that `git rev-parse HEAD` prints
      the release commit. Stop when it does not.
   3. The gate that the repository's `bump` target runs (updex:
      `make build test fmt lint`; std: `make check`), then a clean tree
      (`test -z "$(git status --porcelain)"`).
   4. `make bump-main-check` again, just before the tag.
   5. `git tag -a vX.Y.Z -m "Version vX.Y.Z" SHA` and
      `git push origin vX.Y.Z`.

   In a repository without `bump-main-check`, use
   `git fetch origin main` and a check that `git rev-parse HEAD` equals
   `git rev-parse origin/main` in its place; propose the target in a pull
   request ("Check the guards" above).
4. **Release notes.** GoReleaser writes the release body from the commits
   and the `changelog` groups of `.goreleaser.yaml`. With
   `release.mode: replace`, it replaces any body that exists when the
   release run publishes, and a re-run replaces it again. So put
   hand-written notes (behaviour changes first) in the release ask, and
   apply them only after the release run ends:
   `gh release edit vX.Y.Z -R OWNER/REPO --notes-file NOTES.md`.

### After the tag

Record each check with its command and its result. A check that you did
not run is "not checked", not a pass.

1. **Release run.** `gh run list -R OWNER/REPO -w release.yml -L 3`: the
   run for the tag has conclusion `success`, and its commit is the release
   commit.
2. **Assets.**
   `gh release view vX.Y.Z -R OWNER/REPO --json assets,isDraft,isPrerelease`:
   every archive and package the `.goreleaser.yaml` builds, and
   `checksums.txt`; not a draft, not a pre-release.
3. **Checksums.** `gh release download vX.Y.Z -R OWNER/REPO -D DIR`, then
   `sha256sum -c checksums.txt` in `DIR`.
4. **Provenance.** For each asset and `checksums.txt`:
   `gh attestation verify FILE --repo OWNER/REPO --source-ref refs/tags/vX.Y.Z --source-digest SHA`,
   where `SHA` is the release commit. Record the signer workflow
   (`release.yml@refs/tags/vX.Y.Z`) and the commit.
5. **Delivery paths.** Check each path the repository has, and record a
   path it does not have as "not a path", with the reason (for example, no
   `homebrew_casks` section in `.goreleaser.yaml`):
   - APT: the apt-publisher `publish` run for the tag and the canary, as in
     step 5 of
     [frostyard-apt-producer](https://github.com/frostyard/core/blob/main/.agents/skills/frostyard-apt-producer/SKILL.md).
   - Homebrew: the cask change reaches the tap's default branch. When the
     `homebrew_casks` entry opens a pull request
     (`repository.pull_request.enabled: true`, as in nsl), that pull
     request merged; an open one is "pending", not a pass. Then
     `Casks/NAME.rb` on the tap's `main` names the version and the sha256
     values that `checksums.txt` gives for the archives, and the tap CI run
     on that commit is green.
   - GHCR: the image for the tag exists. When the repository attests its
     images,
     `gh attestation verify oci://ghcr.io/frostyard/NAME:TAG --repo OWNER/REPO --source-digest SHA`
     passes (add `--source-ref refs/tags/vX.Y.Z` when the image workflow
     runs on the tag). Record the image digest, the signer workflow and the
     source commit.
   - A downstream build (for example a snosi image build after an APT
     publication) belongs to its owner: record its run, not its result.

### Go security patch

When `govulncheck` (CI or a nightly run) reports a standard-library
vulnerability that a Go patch release fixes:

1. **Reachability.** Run `govulncheck ./...` with the release the
   repository's CI installs (no `@latest`). Record the vulnerability ID,
   the exit code, and whether the output shows call traces to the
   vulnerable symbol or lists it only as not called.
2. **Move the pin.** Change the `go 1.27.<patch>` line in `go.mod`, the
   only Go pin of the Fleet toolchain table. Add no `toolchain` line, and
   keep the Go minor.
3. **`mise.lock`.** When it holds a `[[tools.go]]` entry, run `mise lock go`
   after the `go.mod` change. Check that `mise.toml` did not change, and
   that each URL and checksum matches
   `https://go.dev/dl/?mode=json&include=all`. Where `mise.lock` is a risk
   path, the pull request body names it and why the change is safe.
4. **Prove it.** Run `govulncheck ./...` again on the branch (the ID is
   gone) and `make ci`. Move the docs that name the Go version
   (`AGENTS.md`, `README.md`) in the same pull request.
5. **Release.** Open the pull request
   (`build(go): move Go to 1.27.<patch> for GO-YYYY-NNNN`). After it merges
   and CI on `main` is green, propose the patch release by "Before the
   tag". A fleet-wide patch is 1 pull request in each repository, with the
   same title and the same checks, and 1 release ask that lists every
   repository with its fix pull request.

## Pitfalls

- GoReleaser config is **Pro** (`pro: true`); running it with the OSS
  distribution fails. CI needs `GORELEASER_KEY`.
- The nfpm package name is `frostyard-<name>`, not `<name>` — keep that
  prefix so org packages sort together in the APT repository.
- `make lint` silently skips if golangci-lint isn't installed — CI is the
  backstop, so don't treat a quiet local `make check` as proof lint ran.
- Don't disable the snapshot workflow's `cancel-in-progress` — see step 3.
- Don't trust `svu next` without a look at the commits: a `feat(deps)`
  Dependabot prefix makes it a minor bump — see "Before the tag".
- Don't edit a release body before the release run ends:
  `release.mode: replace` overwrites it.
