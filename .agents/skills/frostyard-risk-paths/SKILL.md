---
name: frostyard-risk-paths
description: Write or extend a repository's `.github/risk-paths.json` so that every privileged file matches an L1 or L2 glob, with a `why` that names a rule B category and a coverage test that fails when a privileged file is left out. Use whenever a repository gets its first risk-path list (baseline row B6), when a PR adds a privileged code path, or when a review finds a file missing from the list.
---

# Cover every privileged file in the risk-path list

The risk-path list decides which pull requests get `risk:high` and a level
label, and so which ones a person reads line by line. The contract is
core's
[risk-paths spec](https://github.com/frostyard/core/blob/main/docs/specs/risk-paths.md);
the reasons are
[ADR-0068](https://github.com/frostyard/core/blob/main/docs/adr/0068-label-pull-requests-that-touch-high-risk-paths.md)
and
[ADR-0078](https://github.com/frostyard/core/blob/main/docs/adr/0078-sort-pull-requests-by-levels-l1-to-l4.md).
Read the spec's rules B (categories), C (required entries), G (floors) and
H (grading) before you start.

A first list that is picked by hand misses files: the callers of a delete,
the file that enforces a token scope, the script that decides whether
signing runs, a `sudo` entry point in another package, an installer. A
review then finds them 1 at a time, and a privileged change in a missed
file merges without `risk:high`.

Done means: every privileged file that the marker scan finds, and every
caller and gate of it, is at L1 or L2 by the list; every L1 or L2 `why`
starts with a rule B category (or a protected-boundary id); a coverage test
in the repository's own gate fails when 1 of those files is left out; and
`make ci` passes.

## Steps

1. **Read what the repository already has.** The current
   `.github/risk-paths.json`, its caller workflow (`pr-triage.yml` or the
   older `risk-paths.yml`), `policies/agent-governance.json` when it
   exists (its `protected_boundaries[].id` or `protectedBoundaries[]`
   replace the categories in step 4), the "Map" or layout section of
   `AGENTS.md`, and the vendored `scripts/lib/risk-paths.mjs` (or the
   repository's own port) and its existing risk-path test, if any.
2. **Scan for markers.** From the repository root, list the tracked files
   that hold a privileged operation. Adapt the patterns to the languages in
   the tree, and keep the command and its output for the pull request:

   ```sh
   git grep -l -I -E \
     'os\.Remove|RemoveAll|unlink|rmtree|rm -rf|DELETE FROM|DROP TABLE|TRUNCATE|wipefs|sgdisk|mkfs|prune' \
     -- . ':!*.md' ':!.memory/**'
   ```

   Run 1 search for each family below, and read the hits, not only the
   counts:

   | Family | Markers (examples) | Rule B category |
   | --- | --- | --- |
   | Delete and erase | `os.Remove`, `RemoveAll`, `rm -rf`, `DELETE FROM`, `DROP`, prune, sweep, wipe, disk partitioning | destructive operations |
   | Migrations | `migrate`, `migrations/`, schema version bumps | destructive operations |
   | File modes and ownership | `chmod`, `Chmod`, `0600`, `0700`, `chown`, `umask`, socket modes | secrets and credentials, or auth |
   | Root and sudo | `sudo`, `pkexec`, `setuid`, `systemctl`, root provisioning scripts, Ansible roles with `become` | the category of what it does as root (often destructive operations or update verification) |
   | Install and uninstall | installers, uninstall hooks, `install -m`, package scripts (`postinst`, `prerm`), Flatpak wrappers, Windows `.nsh` hooks | publication, or destructive operations |
   | Signing and verification | `sign`, `signature`, `gpg`, `cosign`, `minisign`, checksum and `Verify=` checks, keyrings | signing and keys, or update verification |
   | Tokens and auth | token scope checks (`sc.Write`), peer admission, sign-in, OAuth, session cookies, MCP or HTTP auth middleware | auth |
   | Secret entry | password prompts, keychain or keyring calls, files that write a credential | secrets and credentials |
   | Downloaded toolchains and sources | `curl \| sh`, pinned download URLs and checksums, Flatpak offline sources, `mise.toml`, `mise.lock`, `go.mod`, `go.sum` | supply chain, or update verification |
   | Release and deploy | release and deploy workflows, GoReleaser config, publish scripts | publication |

   An organization floor (rule G) already puts some paths at L1 by a path
   word or an extension (`signing`, `keys`, `.asc`). Do not rely on a
   floor for a file whose name has no floor word.
3. **Follow each marker to its callers and gates.** The file that
   implements the operation is not enough. For each hit, also list:
   - every caller of it in another package (a `cmd/` file that calls an
     install or remove function of `internal/`);
   - every file that decides whether it runs or what it acts on (the script
     that runs probes before it signs; the config parser that sets the
     `Verify=` default, the target path and the mode; the reference check
     that decides what a sweep deletes);
   - every file that the operation reads as trusted input (Flatpak sources,
     download manifests, checksum lists).

   When a whole tree is privileged (an installer that runs as root, Ansible
   roles that run with `sudo`), cover the whole tree with 1 glob instead of
   a hand-picked subset.
4. **Write the globs.** Give each privileged path a glob at `L1` or `L2`
   (rule H: L1 for boot chain, signing, update verification, auth,
   publication, secrets, rulesets and settings, destructive operations and
   supply chain; L2 for governance and CI workflows; when unsure, L1).
   - Each `why` of an L1 or L2 entry starts with 1 rule B category and a
     colon, for example `destructive operations: decides which blobs the
     sweep deletes`. The categories are exactly the names of
     `WHY_CATEGORIES`: `boot chain`, `signing and keys`, `update
     verification`, `auth`, `publication`, `secrets and credentials`,
     `rulesets and settings`, `destructive operations`, `supply chain`,
     `governance`. Names such as `security`, `deploy`, `packages`, `tool
     pins` or `privacy` are not categories. With a governance policy that
     has protected boundaries, the `why` starts with a boundary id instead.
   - Keep the 2 required entries of rule C at L1 or L2: the list itself and
     the caller workflow.
   - An L4 glob never covers `docs/adr/**`, `docs/specs/**`, `AGENTS.md`,
     `policies/**` or the whole `docs/**` (rule H).
   - Follow the glob syntax of rule A: a glob with no `/` matches only at
     the root, except `**` alone, which matches every path; use `**/name`
     for a name anywhere. No `[...]`, `!` or `a**`.
5. **Add or extend the coverage test.** Put it in the repository's own
   gate (`make ci`), in its usual test language. It reads the list with the
   vendored library and holds 3 checks, each with a count in its output:
   - **Required files.** An explicit list of the privileged files from
     steps 2 and 3. Each must exist in the tree and match a glob of the
     list at L1 or L2. A version 1 list has no `level` field, and every
     glob in it is L1, so the check reads both versions:

     ```js
     const strict = (e) =>
       list.schema_version === 1 || e.level === "L1" || e.level === "L2";
     const covered = list.paths.some((e) => strict(e) && matchGlob(e.glob, path));
     ```

     A version 2 glob that matches at L3 or L4 does not count: it gets no
     `risk:high`. A floor does not count either (see Pitfalls).
   - **Marker scan.** The marker patterns of step 2 run over the tracked
     files (`git ls-files`), and each hit must match an L1 or L2 glob, or
     be named in a short exemption list in the test with a reason for each
     entry. When a pattern gives too much noise to keep, drop it from the
     test and keep its files in the required list.
   - **Categories and rule C.** `checkWhyCategories(list, boundaries)` and
     `checkRequiredPaths(list, [".github/risk-paths.json", <caller>])`
     give no errors. With an older vendored library that lacks them,
     re-vendor it with a new `sha256` pin first (ADR-0078).

   Give the test a negative control: a fixture path outside every L1 or L2
   glob (or a copy of the list with 1 glob removed) must make the check
   fail. A check that passes on an empty file list proves nothing: assert
   that the count of checked files is above 0.
6. **Verify and write the pull request.**
   - Run `make ci` and keep the output.
   - A pull request that changes an existing list is at least L2 (rule G),
     and the workflow adds `risk:high`.
   - A pull request that adds the first list gets no label from the
     workflow: with no list at the base SHA, the job writes "no list on
     base", adds no label and passes (spec rule E). Treat it as L2 or
     stricter anyway: add `risk:high` and `level:L2` by hand (`level:L1`
     when the PR also changes a file that the new list puts at L1), and
     follow rule F as for any `risk:high` PR. Add only labels that exist
     in the repository (`gh label list`): adding a missing label creates
     it, and only the accountable owner creates labels (the 4 level
     labels with core's `scripts/apply-triage-settings.sh`). Never remove a stricter level
     label that the PR already has.
   - In the PR body, name each matched path and why the change is safe, as
     `risk:high` needs for an owner author (spec rule F). List the marker
     commands of step 2 and the count of files that each check covers.
   - When a review finds a file that the list missed, the fix adds the file
     to the test's required list (the guard) and 1 record file under
     `.memory/corrections/`; its `promoted_to` names the test.

## Pitfalls

- **Only the implementing file is listed.** A delete in `internal/store`
  is listed, but the file that picks what to delete is not. Follow the
  callers and the gates (step 3).
- **A `sudo` scan finds only the literal.** Entry points that call an
  install, remove or helper function of another package hold no `sudo`
  string. Match those calls too, and keep the explicit required list.
- **The coverage test accepts any glob.** In version 2 a glob can be L3 or
  L4, which gives no `risk:high`. Require L1 or L2.
- **A `why` copied from a template.** A template's prefix such as
  `supply chain:` is a category; `tool pins:` or `security:` is not. Core's
  `validateRiskPaths` checks only the length: `checkWhyCategories` checks
  the name.
- **A hand-picked subset of a privileged tree.** When every file in a tree
  runs as root or with `sudo`, 1 glob for the tree is shorter and misses
  nothing.
- **A floor taken as coverage.** A floor (rule G) comes from a word or an
  extension in the path name, so a rename can remove it. List privileged
  files by glob, even when a floor covers them today.
