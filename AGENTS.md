# intuneme

Go CLI tool that provisions and manages a systemd-nspawn identity container on an immutable Linux host. The default image runs Microsoft's Intune stack; the opt-in experimental image uses Himmelblau.

This file (`AGENTS.md`) is the canonical agent instructions file — `CLAUDE.md`, `GEMINI.md`, `.cursorrules` and `.github/copilot-instructions.md` are relative symlinks to it, and `.claude/skills` links to `.agents/skills` (frostyard/core ADR-0018, ADR-0029). Edit `AGENTS.md`, never an alias; `make verify` checks the links.

## Architecture

The repository has two main components:

- **Go CLI** (`cmd/`, `internal/`) — Responsible for container lifecycle: init, start, stop, destroy, shell. Handles host-specific setup (user creation, hostname, polkit rules) that varies per machine.
- **Container images** (`ubuntu-intune/`, `ubuntu-himmelblau/`, `ubuntu-himmelblau-terminal-passkey/`) — Separate Containerfiles, build scripts, and system files define the Microsoft Intune default and the experimental Himmelblau alternatives.

**Rule of thumb:** The Go CLI starts and stops things. The container image defines what's inside the container. If something is static and doesn't depend on the host (packages, service overrides, config files), it belongs in the matching image directory. If it depends on the host user/UID/hostname, it stays in `internal/provision/`.

The default image is `ghcr.io/frostyard/ubuntu-intune`. `intuneme init
--himmelblau` opts into `ghcr.io/frostyard/ubuntu-himmelblau`, an experimental
image containing Himmelblau's own PAM/NSS, broker, and compliance stack. The
standard image remains on signed upstream nightly packages. The terminal-
passkey variant is local-build-only: users build
`ubuntu-himmelblau-terminal-passkey` and pass it with
`--himmelblau-local-image`; `--himmelblau-terminal-passkey` requires that local
image. A marked local image is detected and its source commit is persisted for
`recreate`. Do not pull or publish a hosted terminal-passkey image. Neither
Himmelblau image contains Microsoft Edge, `intune-portal`, or
`microsoft-identity-broker`.

Init collects the Entra sign-in domain (or accepts `--himmelblau-domain`) and
never bakes a tenant into the image. It starts
`/etc/himmelblau/himmelblau.conf` from the package's commented
`/usr/lib/himmelblau/himmelblau.conf` defaults, then sets the tenant domain and
intuneme's required local-account mapping options. `intuneme edit --himmelblau`
opens that effective config in `$VISUAL`/`$EDITOR`, or in a graphical session
via `xdg-open` using the host's default application; the desktop path waits for
Enter confirmation after the user saves and closes the app. Headless fallback
is `sensible-editor`, `editor`, `micro`, `nano`, `nvim`, `vim`, then `vi`. The
validated result is stored in `<root>/himmelblau.conf` and must be reapplied by
`start` and `recreate`, not overwritten by the default writer.

`intuneme enroll --user <UPN>` accepts a stopped or running container. It
writes `/etc/himmelblau/user-map`; when stopped it runs the normal `start` flow
to boot with the map, and when running with a new map it reboots because
in-place daemon restart/reload leaves socket activation broken. It removes the
local user from the `machine-shell` PAM allowlist and runs `pamtester` through
the foreground nsenter helper as the mapped local user; this preserves PAM's
real exit status, including cancellation. When the terminal-passkey variant's
`enable_passwordless_qr_bluetooth` option is true, enrollment requires an
interactive terminal, host `xdg-dbus-proxy`, and an active BlueZ
service/adapter. For that PAM attempt it exposes only `org.bluez` through a
filtered D-Bus proxy; never bind the host system bus socket directly into the
container. The QR may follow an Entra password prompt, and
`enable_passwordless_qr_bluetooth` does not skip earlier authentication. Do
not use `machinectl shell` to run the PAM check: it reports success even when
the remote command fails. Only after PAM succeeds does enrollment open the
usual shell and, after it exits, verify the local account still has the host
UID and save the UPN. Enrollment authenticates host sudo and refreshes the
scoped helpers before changing the mapping. `start` pre-binds the private
BlueZ directory read-only only for terminal-passkey images; enrollment checks
that mount and starts the filtered proxy only for PAM. Keep the runtime
machine helper MCP-only. See [ADR-0005](docs/adr/0005-bind-bluez-at-boot.md).

For an enrolled Himmelblau container, every non-dry-run `start` initializes
the session environment after linger, including before the already-running
return. Use the foreground exec path and `systemctl --user try-restart
himmelblau-broker.service` so an active broker inherits the display variables
needed for its Hello PIN dialog. Do not retain the PIN or auto-unlock PRTs.
The host-bound runtime directory can retain a stale user-bus socket after a
container restart. `userBusWaitScript` must require a successful D-Bus
`GetId`, not only `test -S`, and bound the whole probe loop (including stalled
requests) to 30 seconds without writing protocol data to stdout.

Container roots using Himmelblau `DynamicUser` store daemon state under
`/var/lib/private/himmelblaud` and `/var/cache/private/himmelblaud`; recreate
must restore to these private backing paths and create the public relative
`private/himmelblaud` symlinks before boot: the root HSM initializer runs before
the DynamicUser daemon. Reject conflicting public/private directories.
Preserve `/var/lib/systemd/credential.secret` together with `/etc/machine-id`;
systemd deletes a credential key belonging to a different machine ID.
If that key is missing while the encrypted PIN exists,
recreate must fail before deleting the rootfs; follow the recovery guidance in
`site/troubleshooting.md`. The mapping preserves the local account's UID, GID,
groups, and home, so do not describe or provision a second Unix identity for
the UPN. The mapped identity retains the host-matched account's groups, but
has no passwordless container sudo. Helper-launched processes retain
`no_new_privs`; interactive administration requires the container password.
Because nspawn is not user-namespaced, container root is host root.
Never change image families during `recreate`. A marked local Himmelblau
image is an explicit image override; detect and record its terminal-passkey
variant and source commit during recreation, while continuing to preserve
Himmelblau state.

During Himmelblau `init` and `recreate`, intuneme renames the local account's primary group to the host username without changing its GID; a conflicting same-name group at another GID is an explicit provisioning error. This matches the local `user-map` name and avoids the common resolver UID/GID collision. The Microsoft image's group naming is unchanged.

Himmelblau `start` binds the host's `/run/udev/data` read-only when present so its compliance worker can read filesystem-type metadata. Both Himmelblau images install `binutils` and verify `strings` is available for Secure Boot database custom-compliance scripts. Do not loosen the task worker's `PrivateDevices` or device policy to solve these checks.

The Microsoft image remains based on Ubuntu 24.04. Both Himmelblau image variants use the pinned Ubuntu 26.04 base and matching Community Nightly packages; the terminal-passkey daemon and PAM packages are built for that release.

The terminal-passkey image builds the daemon and PAM module from the commit in
[`internal/version/himmelblau_terminal_passkey_source.txt`](internal/version/himmelblau_terminal_passkey_source.txt), rebased onto official upstream
and including the cached-PRT client fallback and durable decoupled-Hello PRT
renewal during online PAM/broker PIN unlock. Intuneme applies no local source
patches. The standard
Himmelblau image continues to use signed upstream packages. The fallback
retries Edge-client acquisition errors with the default client while
preserving consent/network handling and never turning retry failures into
authentication success; see `ubuntu-himmelblau-terminal-passkey/README.md`.
Renewed PRTs are resealed while the PIN has unlocked the Hello key; background
renewal does not retain the PIN or unlocked key. Entra reauthentication policies
still apply.

The terminal-passkey local-image helper builds the unchunked `builder` target and supports Docker Buildx with `--load`, Podman, or Buildah. Its Containerfile's chunkah/OCI-archive stages are not used by this helper; the terminal-passkey image is not published.

`recreate` backs up Himmelblau's protected `DynamicUser` state before deleting the rootfs. When host-side inspection of the encrypted HSM PIN returns permission denied, retry through `sudo`. With `credential.secret`, require a valid regular `/etc/machine-id`; verify both files are regular and byte-for-byte equal at backup and restore. Retain the sensitive recovery backup after successful restore and report its path, so a first-boot failure cannot discard the only usable credentials. The user removes it only after confirming normal PIN login.

During initial Himmelblau provisioning, set `apply_policy` and `enable_passwordless_qr_bluetooth` to `true`; when installing a saved config, default each to `true` if missing or commented, but preserve an explicit `false` opt-out. The terminal QR flow still requires the terminal-passkey image and host BlueZ proxy prerequisites.

`init` sets a password for the local container account so interactive `machinectl` login works. intuneme and both image families accept any non-empty password; they do not enforce password-quality rules. Recommend a strong password because it protects interactive access to the container.

`init` and `recreate` explicitly set the new rootfs directory mode to `0755`, then reconcile its owner to `0:0` and mode to `0755` after extraction and ownership verification; never rely on the umask. `start` runs the same reconciliation after the security refresh and before its already-running check, except in dry run; failures are fatal and produce no problem record. Reconciliation resolves a symlinked rootfs path and changes only the resolved directory. Init initially chmods and cleans up only an absent rootfs or an empty rootfs directory owned by the running UID. Linger retries use a 30-second total time budget, not an attempt count; an in-flight attempt can finish after the deadline.

The optional `product_name` key is validated only at use in `start`, before sudo; never validate it in `config.Load`. Its bind source path must be absolute and must never contain `:` or `\` (nspawn splits at `:` and reads `\` as an escape), and the host DMI target must exist before sudo and boot.

## Executing commands inside the container

`nspawn.Exec()` uses the installed Go helper to enter the container namespaces and execute as the numeric configured user. Background GUI processes are reparented inside the container.

- **Helper + nohup &** — enters namespaces on a locked OS thread after unsharing `CLONE_FS`, sets `no_new_privs`, clears capability bounding/ambient/inheritable sets, drops UID/GID, then executes the container shell. Never restore setuid elevation on this path.

- **machinectl shell** — uses polkit (good), but puts processes in a session scope that systemd cleans up when the shell exits (kills GUI apps).
- **systemd-run --machine** — requires root (direct bus transport), bypasses polkit. Permission denied as a normal user.
- **machinectl shell + systemd-run --user** — the transient user service has a minimal PATH that skips `/usr/local/bin/` wrappers, and the sanitized environment breaks X11 auth.

A sudoers rule at `/etc/sudoers.d/intuneme-exec` (installed by `intuneme init` and reinstalled by every `intuneme start`, removed by `intuneme destroy`) makes app launch passwordless, so the GNOME extension does not need a terminal for sudo prompts. The rule authorizes root-owned `nsenter-exec` and `machine-exec` helpers under `/usr/local/libexec/intuneme/`. The helper is a copy of the intuneme binary dispatched by `argv[0]`; its machine, user, UID, home, and rootfs are baked into a root-owned TOML config at install time. The caller supplies only the script to run.

The helper resolves the container leader from the baked machine name itself, verifies the target is in the host's initial user namespace and that its root is the configured rootfs, enters the container's mount/UTS/IPC/net/PID namespaces, drops to the **numeric** configured UID/GID (with supplementary groups read from the container's `/etc/group`), and only then execs `/bin/bash -c <script>`. It does not use `su` or execute any container-controlled binary while privileged. This is deliberate: an earlier version accepted the leader PID from the caller and ran the container's `/bin/su` as root, so a user-created user+mount namespace with a fake `/bin/su` (or a modified `/etc/passwd`) yielded host root. `sudoers.Install` validates the config, installs the helper `0755` root:root and the config `0644` root:root, and validates the wildcard-free rule with `visudo -c`. `init` may install from a user-owned `go install` binary because it is the explicit, sudo-authenticated install; `start` fails and removes passwordless helper authorization if its source is not root-owned or its path is group/world-writable; it never silently keeps an older helper. Install intuneme to a root-owned path to get automatic helper refreshes. `start` reconciles all security artifacts before boot or returning for an already-running machine. See `internal/nshelper`.

**Session setup runs on the nsenter path too.** The nsenter shell is *non-login*, so it never sources `/etc/profile.d`. `nspawn.Exec()` therefore runs `/usr/local/bin/intuneme-session-setup` (the shared script that `/etc/profile.d/intuneme.sh` also sources) before launching the app. Broker restart uses a container polkit policy limited to `manage-units`, the device-broker unit, and `restart`; no session-side sudo is needed. The canonical policy is in `ubuntu-intune/system_files`, embedded for upgrade reconciliation. That script pushes `DISPLAY`/`XAUTHORITY` into the **D-Bus activation environment** (`dbus-update-activation-environment`) and unlocks gnome-keyring. This is mandatory: the Microsoft identity broker is a GTK app that the session D-Bus daemon activates on demand — without a display in the activation environment it dies on startup (`cannot open display`) and **Edge can't authenticate**. The same activation environment carries `WEBKIT_DISABLE_DMABUF_RENDERER=1` and `WEBKIT_DISABLE_COMPOSITING_MODE=1`; without them the broker's WebKitGTK sign-in window renders blank white on a Wayland host. Anything the broker must see has to be named in the script's `_env_vars` propagation list — a bare `export` never reaches a D-Bus-activated process. `start` reinstalls the script on every boot, unconditionally, so CLI upgrades reach existing containers without a `recreate`. The nsenter path creates no logind session, so `start` always enables linger for the container user (not just when a proxy is on); without it an app launched first from the GNOME extension finds no `user@<uid>`, no `/run/user/<uid>`, and no session bus. Linger is enabled right after the machine registers, with `systemd-run --machine=<machine> --wait --pipe` and retries, never `machinectl shell`, which hides the inner exit status; an inner `loginctl` failure is therefore fatal and carries the command output. The nsenter prologue in `buildSessionEnvScript` waits up to 30 seconds for the user bus (`$XDG_RUNTIME_DIR/bus`) before it runs the session script, and fails the launch with a stderr message if the bus does not appear; `Exec` returns that message in its error, so `intuneme open` shows it. The prologue writes nothing to stdout. The script's once-per-boot keyring marker is written only after the login collection reads back unlocked: never write it unconditionally, or a failed early unlock leaves the keyring locked for the whole boot. Retries are capped at 3 attempts per boot (counter in `/tmp/.intuneme-keyring-unlock-attempts`), because each attempt costs a `pkill` and about 2 s of sleep; the block holds an `flock` on `$XDG_RUNTIME_DIR/intuneme-keyring.lock` so concurrent session entries cannot exceed the cap, and each failure and the give-up are logged to stderr and to the journal as `intuneme-session-setup`. The script touches `$XDG_RUNTIME_DIR/intuneme-session-env` once the activation environment is set, and `intune-agent.timer` carries `ConditionPathExists=%t/intuneme-session-env`, so the agent can only start after session setup and never activates the broker without a display. See `docs/design/overview.md` → "Session Setup".

## Running MCP servers in the container (`intuneme mcp`)

`intuneme mcp` runs a host-provided MCP server binary *inside* the container with its stdio wired to a host client (e.g. VS Code). Use it when a server must authenticate against the container's enrolled tenant and the broker proxy can't help — most notably a two-tenant setup (one tenant on the host, a second inside intuneme), since a D-Bus well-known name has one owner per bus and can't represent two brokers.

Key design points:

- **Foreground exec, no TTY.** It uses `nspawn.ExecForeground()` (not `Exec()`): same `nsenter` shape and sudoers rule, but `exec` with attached stdio instead of `nohup … &`. Backgrounding or allocating a TTY corrupts JSON-RPC framing.
- **Binary stays out of the rootfs.** The server binary lives on the host (`mcp_binary` config key or `--binary`). Its directory is bind-mounted read-only at `/opt/intuneme-mcp` by a scoped root-owned machine helper (`machine-exec bind <dir>`), which validates that the directory is owned by the host user and forces `--read-only` and the fixed destination. The bind is runtime-only and re-established on demand, so it **survives `intuneme recreate`** — nothing is added to `ubuntu-intune/`.
- **Server-agnostic.** No assumption about a particular tool; any self-contained MCP server works. The server's own arguments come from the `mcp_args` config key (e.g. `["mcp"]` for `workiq mcp`), with trailing `intuneme mcp -- args...` overriding them — this keeps the VS Code config to just `["mcp"]`. A `DOTNET_BUNDLE_EXTRACT_BASE_DIR` env var is set (harmless for non-.NET) so single-file .NET servers extract to ephemeral `/tmp` and the mount can stay read-only.

## Problem records

intuneme writes panics and 4 coded internal errors to the local journal with `github.com/frostyard/std/problems` (`internal/problemrec`, ADR-0003, core ADR-0069). Rules:

- The recorder starts in `main()` only after the helper dispatch. Never make a recorder, call `problemrec.Error`, or defer `RecoverPanic` in `nshelper.Main`, `nshelper.MachineMain` or anything they call: they run as root.
- `defer rec.RecoverPanic()` stays a direct defer in `main()`, so a panic still exits 2.
- Codes (`problemrec.Code*`) are a stable contract: add a new one (with a test, the table in `site/reference/problem-records.md`, `docs/design/overview.md` and ADR-0003), never rename or reuse one. Record only internal failures, never user errors.
- A code is recorded only for an internal failure after config validation and sudo authentication passed; user setup and authentication errors are never recorded. Record a code only where the failure cannot come from user config or sudo authentication (so `refreshStartSecurity` and any `sudo` failure of `nspawn.Exec` other than `nspawn.ErrSessionBusTimeout` get no record), and prove each exclusion with a socket-backed negative test (`problemrectest.Use` then `ExpectNone`), never with a nil recorder.
- Record only through `recordInternal(cfg, code, err)` in `cmd`: it records only when `identityConsistent(cfg)` holds (the config `host_uid` is the running non-root UID, `host_user` is a plausible non-root name, and the container's `/etc/passwd` has exactly 1 strict entry for that name with that UID; any read or parse error fails closed). A config that passes validation can still name no container account. With an inert recorder `recordInternal` returns at once and reads no file.
- Open a path under the container rootfs with `O_NONBLOCK|O_NOFOLLOW` and reject a non-regular file before you read it; parse a passwd entry strictly (7 fields, numeric UID and GID, absolute home) and fail closed.
- Classify a failure by matching 1 full diagnostic line (for `open`, the exact session bus timeout line for the configured UID, `nspawn.ErrSessionBusTimeout`), never by a substring: sudo quotes the submitted script, which holds the `echo` of that message.
- Pass the original error to `problemrec.Error`; its text goes only to the journal `MESSAGE` field. Never put error text in another field, and never change a returned error or output to record it.
- A statement about what a record holds (README, site, docs) names the part it covers: the structured fields never hold file paths, line numbers or arguments, but the journal `MESSAGE` field holds the original error text or panic value, which can hold a path. Never write "the record never holds" for the whole record.
- No network: a record is 1 datagram to the local journal socket. Never add a sender, a flag or output for problems.
- Tests use `problemrectest.Use` (a `unixgram` socket in `t.TempDir()`). std caps a process at 10 records a minute and tests cannot reset it, so 1 test package writes at most 10 records.

## Release automation

`make bump` tags a release with svu and pushes the tag. `.github/workflows/release.yml` runs only for tag pushes: GoReleaser Pro creates the GitHub release (including `frostyard-intuneme_<version>_amd64.deb`), `actions/attest-build-provenance` attests the assets including `dist/*.deb`, and the last step requests APT publication. That request is a `repository_dispatch` of type `publish-deb` to `frostyard/apt-publisher` with `repo` and `tag`, authenticated by `APT_PUBLISH_TOKEN`. It has no ref guard (a tag run's ref is `refs/tags/<tag>`, never `refs/heads/<default>`) and is not `continue-on-error`.

apt-publisher is the only writer of Frostyard's Debian repository ([frostyard/core ADR-0055](https://github.com/frostyard/core/blob/main/docs/adr/0055-publish-debian-packages-through-the-apt-publisher.md)). It verifies each `.deb`'s provenance against the tag, publishes it to `https://repository.frostyard.org/debian/`, and then dispatches `build` to frostyard/snosi ([ADR-0056](https://github.com/frostyard/core/blob/main/docs/adr/0056-rebuild-images-after-apt-publication.md)). So this workflow never uses repogen or dispatches to snosi itself. Renaming the package, or adding another `.deb` to the release, needs a change to intuneme's registration in apt-publisher's `config/producers.tsv` first: every `.deb` asset is published or the whole request is refused. `snapshot.yml` (the rolling `dev` prerelease) must never request publication.

`release_workflow_contract_test.go` pins this contract; the workflows only run on tags and `main`, so the test is their pull-request gate. See `docs/design/overview.md` → "Release and Package Publication".

## Releases and dependencies

- **Tag format:** `v<major>.<minor>.<patch>` (latest: `v0.21.0`).
- **Release start:** see "Release automation" above.
- **Who tags:** Brian only. Agents never tag, push tags, or release. `make bump` pushes a tag, so it is for Brian only.
- **Version rule:** svu computes the next version (`svu next` in `make bump`), configured in `.svu.yml` (`tag.prefix: "v"`, `v0: true`).
- **Internal dependencies:** `github.com/frostyard/clix` and `github.com/frostyard/std` in `go.mod`. Other frostyard references: `frostyard/apt-publisher` (`repository_dispatch` in `.github/workflows/release.yml`) and the `ghcr.io/frostyard/ubuntu-intune` image (`ubuntu-intune/`, cosign verify note in `.goreleaser.yaml`). The workflows pin no other frostyard action or image.
- **Who uses this repository:** not stated.

## CI workflows

Every external action in `.github/workflows` is pinned to a full commit SHA with a `# vX.Y.Z` comment (frostyard/core ADR-0021). `workflow_pinning_test.go` enforces this. A call of a frostyard/core reusable workflow (core has no release tags) is SHA-pinned with a comment that names its source instead. Dependabot updates the SHA and the comment together.
`.github/dependabot.yml` ignores major updates of `redhat-actions/buildah-build`: v3 defaults `squash` to true and would flatten the image to 1 layer, so the build stays on v2.13 (#233).
The docker update for `/ubuntu-intune` ignores `library/ubuntu` major and minor updates: Intune supports only Ubuntu LTS releases, and Dependabot reads an interim release (24.04 to 24.10) as a minor update. Patch and digest updates continue. `dependabot_config_test.go` pins it.

## Gate

- `mise install` provisions the pinned tools (`mise.toml`, checksums in `mise.lock`): golangci-lint and Node. Go comes from `go.mod` only. Bump golangci-lint in `mise.toml` and run `mise lock`; never pin it in a workflow or the `Makefile`.
- `make verify` is the credential-free gate and changes no file: tidy diff, vet, gofmt check, the pinned golangci-lint, the template gate (`scripts/check-template.mjs`) and the race tests. `make check` runs `make fmt`, then `make verify`. `make ci BASE=origin/main` adds the corrections check against `BASE`, govulncheck and the build.
- `ci` has no recipe: `.github/workflows/ci.yml` runs each prerequisite as its own `make <target>` step (frostyard/core ADR-0073). A new prerequisite of `ci` needs its step in `ci.yml` in the same change. Keep the workflow name `Tests`: `snapshot.yml` follows it.
- `scripts/check-template.mjs`, `scripts/check-corrections.mjs`, `scripts/lib/risk-paths.mjs` and `test/check-template.test.mjs` are copies from bketelsen/agentic-template. Never edit them here; take a new copy.
- Every doc under `docs/` must be linked from `docs/README.md`, or `make verify` fails.

## Before committing

Always run `make check` before committing, and `make ci BASE=origin/main` before you open a pull request. Fix any lint errors before creating commits.

## Security and risk paths

- `.github/risk-paths.json` lists the high-risk paths: governance and CI files, the release configuration and hooks, every file that runs `sudo` or installs or removes host privilege files (the privileged helpers, `main.go` and the `cmd/` files that install or delete host files), the polkit files, the scripts in `scripts/` and `ubuntu-intune/`. Each entry has a `level`, and the `why` of an L1 or L2 entry starts with 1 category of core's `docs/specs/risk-paths.md` (section B) and a colon; an L3 entry takes free text. `risk_paths_test.go` checks the categories and the coverage of every non-test Go file that calls `"sudo"` or an `Install*`, `Remove*`, `Main` or `MachineMain` function of `sudoers`, `udev`, `nshelper` or `provision`, of its `privilegedRequired` entry points (`main.go` and the `cmd/` installers), and of every `scripts/*.sh`. A new privileged entry point that the scan cannot see goes in `privilegedRequired`. A pull request that touches one gets a level label (`level:L1` to `level:L4`) from the `.github/workflows/risk-paths.yml` caller of core's `pr-triage.yml`, and `risk:high` when the level is L1 or L2 (frostyard/core ADR-0068, ADR-0078). That workflow also removes `ready-for-human` when the pull request head moves after the label, or when the label comes while CI at the head is not green (ADR-0077). Never remove `risk:high` or a level label to make a pull request ready; the PR body names each matched path and why the change is safe. A new privileged helper, installer or root-run script gets an entry in the same change.
- Report vulnerabilities as `SECURITY.md` says (GitHub private vulnerability reporting). Never put vulnerability details in a public issue or in `.memory/corrections.jsonl`.

## Corrections

`.memory/corrections.jsonl` is append-only, 1 JSON object per line with exactly `date`, `scope`, `correction`, `evidence` and `promoted_to` (`.memory/README.md`). A blocking review point adds 1 line in the pull request that fixes it, with a guard (a test that fails without the fix, or an `AGENTS.md` rule). Set `promoted_to` to the file that now holds the rule; never edit an older line.

## Documentation rules

Engineering docs live in `docs/` in frostyard/core's four-category shape
(formerly the `yeti/` directory) — see the category table, index, and
conventions in [docs/README.md](docs/README.md). Read
[docs/design/overview.md](docs/design/overview.md) for codebase context
before performing tasks, and write `docs/` content to be maximally useful to
an AI agent understanding the codebase — detailed architecture, patterns, and
decision rationale rather than user-facing guides.

- Repo-local decisions get an ADR in `docs/adr/` (next free number, from the
  TEMPLATE); org-wide decisions go to
  [frostyard/core](https://github.com/frostyard/core/tree/main/docs/adr) plus
  a line in [docs/org-adrs.md](docs/org-adrs.md).
- **update documentation** After any change to source code, update relevant
  documentation in AGENTS.md, README.md, and `docs/`. A task is not complete
  without reviewing and updating relevant documentation.
- `README.md` is a `tool` README to frostyard/core's README spec (ADR-0072):
  a short top block, `What it does`, `Install`, `Use`, `Problem records`,
  `Develop` and `Where to go next`, at most 1000 words. Check it with
  `node .agents/skills/frostyard-readme/scripts/check-readme.mjs .` (the skill
  comes with core's skills sync). Guides, the command reference and long
  tables go to the site in `site/`, not to the README. Update the `What it
  does` list when a feature is added or removed.
- User-facing docs are separate: the MkDocs site content lives in `site/`
  (`mkdocs.yml`, published to GitHub Pages).

## Container privilege policy

The container-side `/etc/sudoers.d/intuneme` is now comment-only: no passwordless
sudo grants remain. Broker restart uses the scoped container polkit policy.
`start` authenticates and reconciles that policy, the session scripts, and the
sudoers file before boot or the already-running return. `start` also reinstalls the Edge managed policy that suppresses the `--disable-gpu-sandbox` banner (#62). A migration error is
fatal. App/MCP sessions cannot use sudo because the helper sets `no_new_privs`;
interactive `intuneme shell` administration still uses the container password.

## Host authorization and upgrade invariants

The generated host polkit rule matches `subject.uid`, the configured machine,
and the configured non-root username. Never restore a group-wide `shell` grant:
it also authorizes root and unrelated-container shells. Linger setup (`systemd-run --machine`)
uses the start command's authenticated sudo session; ordinary user sessions
still use the scoped passwordless shell grant.

`machine-exec` is a second copy of the trusted Go binary, dispatched by argv[0],
using the same root-owned TOML config. It validates the target, accepts only
`stop` or `bind <directory>`, and clears the command environment. For binds it
uses `openat2(RESOLVE_NO_SYMLINKS|RESOLVE_NO_MAGICLINKS)` and fstat, then
`open_tree`, `mount_setattr`, and `move_mount` on descriptors to attach a
read-only, nosuid, nodev mount inside the validated container mount namespace.
Destination components are also opened without symlinks. This needs Linux 5.12+
and fails closed on older kernels. Never pass a descriptor pathname through
machined: older systemd versions resolve it back to a replaceable pathname.
The CLI resolves legitimate host path aliases before the privileged open.

Missing machine helpers on upgrade permit an explicitly authenticated
`sudo machinectl poweroff` fallback. A present helper's rejection never falls
back. `start` refreshes host polkit, helpers, container broker policy and sudoers
before its already-running return; failure is fatal. See ADR-0002.
