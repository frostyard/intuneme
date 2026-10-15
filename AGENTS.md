# intuneme

Go CLI tool that provisions and manages a systemd-nspawn identity container on an immutable Linux host. The default image runs Microsoft's Intune stack; the opt-in experimental image uses Himmelblau.

This file (`AGENTS.md`) is the canonical agent instructions file — `CLAUDE.md`, `GEMINI.md`, and `.cursorrules` are symlinks to it (frostyard/core ADR-0018).

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
UID and save the UPN.

Container roots using Himmelblau `DynamicUser` store daemon state under
`/var/lib/private/himmelblaud` and `/var/cache/private/himmelblaud`; recreate
must restore to these private backing paths, not create conflicting public
directories. It must also preserve `/var/lib/systemd/credential.secret` for
the encrypted HSM PIN. If that key is missing while the encrypted PIN exists,
recreate must fail before deleting the rootfs; follow the recovery guidance in
`site/troubleshooting.md`. The mapping preserves the local account's UID, GID,
groups, and home, so do not describe or provision a second Unix identity for
the UPN. The mapped identity retains the host-matched account's passwordless
container sudo; because nspawn is not user-namespaced, container root is host
root. Never change image families during `recreate`. A marked local Himmelblau
image is an explicit image override; detect and record its terminal-passkey
variant and source commit during recreation, while continuing to preserve
Himmelblau state.

During Himmelblau `init` and `recreate`, intuneme renames the local account's primary group to the host username without changing its GID; a conflicting same-name group at another GID is an explicit provisioning error. This matches the local `user-map` name and avoids the common resolver UID/GID collision. The Microsoft image's group naming is unchanged.

Himmelblau `start` binds the host's `/run/udev/data` read-only when present so its compliance worker can read filesystem-type metadata. Both Himmelblau images install `binutils` and verify `strings` is available for Secure Boot database custom-compliance scripts. Do not loosen the task worker's `PrivateDevices` or device policy to solve these checks.

The Microsoft image remains based on Ubuntu 24.04. Both Himmelblau image variants use the pinned Ubuntu 26.04 base and matching Community Nightly packages; the terminal-passkey daemon and PAM packages are built for that release.

The terminal-passkey image builds the daemon and PAM module from pinned commit
`dae50ca7cb84e959fa76b4e3a4ee682431a48b57`, which includes the cached-PRT
client fallback. Intuneme applies no local source patches. The standard
Himmelblau image continues to use signed upstream packages. The fallback
retries Edge-client acquisition errors with the default client while
preserving consent/network handling and never turning retry failures into
authentication success; see `ubuntu-himmelblau-terminal-passkey/README.md`.

The terminal-passkey local-image helper builds the unchunked `builder` target and supports Docker Buildx with `--load`, Podman, or Buildah. Its Containerfile's chunkah/OCI-archive stages are not used by this helper; the terminal-passkey image is not published.

`recreate` backs up Himmelblau's protected `DynamicUser` state before deleting the rootfs. When host-side inspection of the encrypted HSM PIN returns permission denied, retry the existence check through `sudo`; never treat inaccessible PIN state as absent. If `credential.secret` exists, verify the staged backup contains a regular copy before allowing rootfs deletion; also reject any staged encrypted-PIN/key mismatch. After restore, verify the destination key is a regular file and byte-for-byte matches the backup before removing the backup.

During initial Himmelblau provisioning, set `apply_policy` and `enable_passwordless_qr_bluetooth` to `true`; when installing a saved config, default each to `true` if missing or commented, but preserve an explicit `false` opt-out. The terminal QR flow still requires the terminal-passkey image and host BlueZ proxy prerequisites.

`init` sets a password for the local container account so interactive `machinectl` login works. intuneme and both image families accept any non-empty password; they do not enforce password-quality rules. Recommend a strong password because it protects interactive access to the container.

## Executing commands inside the container

`nspawn.Exec()` uses `sudo nsenter` to enter the container's namespaces and run a command as the user via `su`. This is the only reliable approach for launching GUI apps (Edge, Portal) non-interactively:

- **nsenter + nohup &** — enters namespaces, runs in a login bash (gets correct PATH for wrappers at `/usr/local/bin/`), backgrounds the process. The orphaned process is reparented to PID 1 inside the container. Proven and reliable.
- **machinectl shell** — uses polkit (good), but puts processes in a session scope that systemd cleans up when the shell exits (kills GUI apps).
- **systemd-run --machine** — requires root (direct bus transport), bypasses polkit. Permission denied as a normal user.
- **machinectl shell + systemd-run --user** — the transient user service has a minimal PATH that skips `/usr/local/bin/` wrappers, and the sanitized environment breaks X11 auth.

A sudoers rule at `/etc/sudoers.d/intuneme-exec` (installed by `intuneme init`, persists across start/stop, removed by `intuneme destroy`) makes the nsenter command passwordless, so the GNOME extension can launch apps without a terminal for sudo prompts. The rule does not authorize `nsenter` directly: it authorizes a single root-owned helper script `/usr/local/libexec/intuneme/nsenter-exec`, which hard-codes the `nsenter`/`su` shape and takes only the leader PID and the script as arguments. This indirection is required because **sudo-rs** (the default `sudo`/`su` on Ubuntu 25.10+) forbids wildcards inside command arguments. The old rule used `*` for the leader PID and script and was rejected, printing an error on *every* `sudo` call (issue #168). The wildcard-free helper rule keeps the same effective authorization as the old rule (the leader PID and script are still caller-controlled), and the helper is installed `0755` root:root so the user can't edit code that runs as root. The `start` command reinstalls the rule and helper idempotently if missing; `sudoers.IsInstalled()` requires both to exist, so upgrades from the old wildcard rule self-heal.

**Session setup runs on the nsenter path too.** The nsenter shell is *non-login*, so it never sources `/etc/profile.d`. `nspawn.Exec()` therefore runs `/usr/local/bin/intuneme-session-setup` (the shared script that `/etc/profile.d/intuneme.sh` also sources) before launching the app. That script pushes `DISPLAY`/`XAUTHORITY` into the **D-Bus activation environment** (`dbus-update-activation-environment`) and unlocks gnome-keyring. This is mandatory for Microsoft's GTK identity broker and also supplies the display environment to Himmelblau's D-Bus-activated broker. The script starts the matching compliance timer for the selected image family (`intune-agent.timer` or `himmelblau-compliance-check.timer`). The same activation environment carries `HOME`, `WEBKIT_DISABLE_DMABUF_RENDERER=1`, and `WEBKIT_DISABLE_COMPOSITING_MODE=1`; in Himmelblau mode `HOME` must be imported from the validated Entra user's shell before broker activation. Without the WebKit variables the Microsoft broker's WebKitGTK sign-in window renders blank white on a Wayland host. Anything the broker must see has to be named in the script's `_env_vars` propagation list — a bare `export` never reaches a D-Bus-activated process. `start` reinstalls the script on every boot, unconditionally, so CLI upgrades reach existing containers without a `recreate`. See `docs/design/overview.md` → "Session Setup".

## Running MCP servers in the container (`intuneme mcp`)

`intuneme mcp` runs a host-provided MCP server binary *inside* the container with its stdio wired to a host client (e.g. VS Code). Use it when a server must authenticate against the container's enrolled tenant and the broker proxy can't help — most notably a two-tenant setup (one tenant on the host, a second inside intuneme), since a D-Bus well-known name has one owner per bus and can't represent two brokers.

Key design points:

- **Foreground exec, no TTY.** It uses `nspawn.ExecForeground()` (not `Exec()`): same `nsenter` shape and sudoers rule, but `exec` with attached stdio instead of `nohup … &`. Backgrounding or allocating a TTY corrupts JSON-RPC framing.
- **Binary stays out of the rootfs.** The server binary lives on the host (`mcp_binary` config key or `--binary`). Its directory is bind-mounted read-only at `/opt/intuneme-mcp` via `machinectl bind` (authorized passwordless by the polkit `manage-machines` rule). The bind is runtime-only and re-established on demand, so it **survives `intuneme recreate`** — nothing is added to `ubuntu-intune/`.
- **Server-agnostic.** No assumption about a particular tool; any self-contained MCP server works. The server's own arguments come from the `mcp_args` config key (e.g. `["mcp"]` for `workiq mcp`), with trailing `intuneme mcp -- args...` overriding them — this keeps the VS Code config to just `["mcp"]`. A `DOTNET_BUNDLE_EXTRACT_BASE_DIR` env var is set (harmless for non-.NET) so single-file .NET servers extract to ephemeral `/tmp` and the mount can stay read-only.

## Release automation

`make bump` tags a release with svu and pushes the tag. `.github/workflows/release.yml` runs only for tag pushes: GoReleaser Pro creates the GitHub release (including `frostyard-intuneme_<version>_amd64.deb`), `actions/attest-build-provenance` attests the assets including `dist/*.deb`, and the last step requests APT publication. That request is a `repository_dispatch` of type `publish-deb` to `frostyard/apt-publisher` with `repo` and `tag`, authenticated by `APT_PUBLISH_TOKEN`. It has no ref guard (a tag run's ref is `refs/tags/<tag>`, never `refs/heads/<default>`) and is not `continue-on-error`.

apt-publisher is the only writer of Frostyard's Debian repository ([frostyard/core ADR-0055](https://github.com/frostyard/core/blob/main/docs/adr/0055-publish-debian-packages-through-the-apt-publisher.md)). It verifies each `.deb`'s provenance against the tag, publishes it to `https://repository.frostyard.org/debian/`, and then dispatches `build` to frostyard/snosi ([ADR-0056](https://github.com/frostyard/core/blob/main/docs/adr/0056-rebuild-images-after-apt-publication.md)). So this workflow never uses repogen or dispatches to snosi itself. Renaming the package, or adding another `.deb` to the release, needs a change to intuneme's registration in apt-publisher's `config/producers.tsv` first: every `.deb` asset is published or the whole request is refused. `snapshot.yml` (the rolling `dev` prerelease) must never request publication.

`release_workflow_contract_test.go` pins this contract; the workflows only run on tags and `main`, so the test is their pull-request gate. See `docs/design/overview.md` → "Release and Package Publication".

## Before committing

Always run `make fmt` and `make lint` before committing. Fix any lint errors before creating commits.

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
- Update `README.md` when adding new commands, flags, or changing existing
  functionality. The README contains a commands table and feature sections
  that must stay in sync with the code.
- User-facing docs are separate: the MkDocs site content lives in `site/`
  (`mkdocs.yml`, published to GitHub Pages).
