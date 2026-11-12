# Documentation

Docs are split by the question they answer (frostyard/core's four-category
shape, [core ADR-0025](https://github.com/frostyard/core/blob/main/docs/adr/0025-consolidate-repository-docs-into-docs.md)):

| Directory | Question | Contents |
| --- | --- | --- |
| [adr/](adr/) | **Why** did we choose this? | Repo-local Architecture Decision Records — immutable once accepted; superseded, never edited. Org-wide decisions live in frostyard/core — see [org-adrs.md](org-adrs.md) |
| [design/](design/) | **How** does it fit together? | Living documents describing the current architecture |
| [specs/](specs/) | **What exactly** is the contract? | Precise, testable interface definitions |
| [plans/](plans/) | **When/in what order** do we build? | Roadmaps and phase plans; updated as work lands |

This tree (formerly `yeti/`) is the engineering documentation, written to be
maximally useful in an agent's context window. The **user-facing** MkDocs site
is separate: its content lives in [`site/`](../site/) (`mkdocs.yml`
`docs_dir: site`), published to GitHub Pages by `.github/workflows/docs.yml`.
Its move to ProperDocs and MaterialX
([core ADR-0070](https://github.com/frostyard/core/blob/main/docs/adr/0070-build-tool-docs-sites-with-materialx-on-github-pages.md))
is a separate change.

Outside this tree: [`AGENTS.md`](../AGENTS.md) holds the rules for agents,
and [`.memory/corrections.jsonl`](../.memory/corrections.jsonl) is the
append-only corrections inbox ([`.memory/README.md`](../.memory/README.md)).

## Index

### Decisions (ADRs)

- [0001 — Make the helper privilege drop irreversible](adr/0001-make-helper-privilege-drop-irreversible.md)
- [0002 — Pin machine operations and scope shell authorization](adr/0002-pin-machine-operations-and-scope-shell-authorization.md)
- [0003 — Record problems with std/problems](adr/0003-record-problems-with-std-problems.md)
- [0004 — Adopt the frostyard repository baseline](adr/0004-adopt-the-repository-baseline.md)
- [0005 — Verify the container image signature before extraction](adr/0005-verify-the-container-image-signature-before-extraction.md)
- Org-wide decisions: [org-adrs.md](org-adrs.md)

### Design

- [Overview](design/overview.md) — purpose, architecture, key patterns
  (nsenter exec, bind mounts, hotplug, Nvidia, session setup), configuration,
  storage layout, host modifications, release and package publication (the
  entry-point doc)
- [Container Lifecycle](design/container-lifecycle.md) — how each command
  works: init, start, stop, destroy, recreate, status, shell, open, udev,
  extension, broker-proxy, notify-proxy, screencast-proxy
- [Broker Proxy](design/broker-proxy.md) — D-Bus forwarding of
  `com.microsoft.identity.broker1` from container to host for host-side SSO
- [Notify Proxy](design/notify-proxy.md) — forwarding of
  `org.freedesktop.Notifications` from container to host, so web
  notifications from the container's Edge show on the host desktop
- [Screencast Proxy](design/screencast-proxy.md) — forwarding of the
  ScreenCast portal from container to host, so the container's Edge can
  share host screens and windows

### Specs

- [Container Image](specs/container-image.md) — exact contents of the
  `ghcr.io/frostyard/ubuntu-intune` image: build stages, package set, static
  config, tag scheme

### Plans

No open plans. Finished plans were removed when their work landed
([ADR-0004](adr/0004-adopt-the-repository-baseline.md)); the designs and specs
above describe the code as it is, and git history keeps the old plans.

## Conventions

- **New docs start from their category's `TEMPLATE.md`** (in each directory).
- New decision → new ADR with the next number; if it reverses an old one, mark
  the old one `Superseded by NNNN` rather than editing it. Decisions that bind
  more than this repo become ADRs in
  [frostyard/core](https://github.com/frostyard/core/tree/main/docs/adr) plus
  a line in [org-adrs.md](org-adrs.md).
- Design docs are updated in place to always reflect reality.
- Specs change only alongside the code that implements them.
- Cross-links between categories are mandatory in both directions.
- Adding a doc means adding it to the index above.
