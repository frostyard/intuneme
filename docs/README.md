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

## Index

### Decisions (ADRs)

*(none yet — org-wide decisions binding this repo are listed in
[org-adrs.md](org-adrs.md))*

### Design

- [Overview](design/overview.md) — purpose, architecture, key patterns
  (nsenter exec, bind mounts, hotplug, Nvidia, session setup), configuration,
  storage layout, host modifications (the entry-point doc)
- [Container Lifecycle](design/container-lifecycle.md) — how each command
  works: init, start, stop, destroy, recreate, status, shell, open, udev,
  extension, broker-proxy
- [Broker Proxy](design/broker-proxy.md) — D-Bus forwarding of
  `com.microsoft.identity.broker1` from container to host for host-side SSO

### Specs

- [Container Image](specs/container-image.md) — exact contents of the
  `ghcr.io/frostyard/ubuntu-intune` image: build stages, package set, static
  config, tag scheme

Historical (superpowers workflow, point-in-time):

- [MkDocs site design](superpowers/specs/2026-03-23-mkdocs-site-design.md)
- [Stop-wait design](superpowers/specs/2026-03-10-stop-wait-design.md)
- [Display marker fix design](superpowers/specs/2026-03-10-display-marker-fix-design.md)

### Plans

Historical point-in-time design/implementation plans (kept as-is; the living
docs above reflect current reality):

- intuneme CLI — [design](plans/2026-02-13-intuneme-cli-design.md), [implementation](plans/2026-02-13-intuneme-implementation.md)
- intuneme v2 — [design](plans/2026-02-13-intuneme-v2-design.md), [implementation](plans/2026-02-13-intuneme-v2-implementation.md)
- Audio support — [design](plans/2026-02-13-audio-support-design.md), [implementation](plans/2026-02-13-audio-support-implementation.md)
- Release pipeline — [design](plans/2026-02-13-release-pipeline-design.md), [implementation](plans/2026-02-13-release-pipeline-implementation.md)
- Broker proxy — [design](plans/2026-02-17-broker-proxy-design.md), [implementation](plans/2026-02-17-broker-proxy-implementation.md)
- Versioned container pulls — [design](plans/2026-02-17-versioned-container-pulls-design.md), [implementation](plans/2026-02-17-versioned-container-pulls-implementation.md)
- Webcam passthrough — [design](plans/2026-02-17-webcam-passthrough-design.md), [implementation](plans/2026-02-17-webcam-passthrough-implementation.md)
- Chromium container fix — [design](plans/2026-02-18-chromium-container-fix-design.md), [implementation](plans/2026-02-18-chromium-container-fix-implementation.md)
- GNOME extension — [design](plans/2026-02-18-gnome-extension-design.md), [implementation](plans/2026-02-18-gnome-extension-implementation.md)
- Multi-backend puller — [design](plans/2026-02-18-multi-backend-puller-design.md), [implementation](plans/2026-02-18-multi-backend-puller-implementation.md)
- Password prompt — [design](plans/2026-02-18-password-prompt-design.md), [implementation](plans/2026-02-18-password-prompt-implementation.md)
- Recreate container — [design](plans/2026-02-18-recreate-container-design.md), [implementation](plans/2026-02-18-recreate-container-implementation.md)
- Render GID conflict — [design](plans/2026-03-03-render-gid-conflict-design.md), [implementation](plans/2026-03-03-render-gid-conflict-implementation.md)
- clix integration — [design](plans/2026-03-04-clix-integration-design.md), [implementation](plans/2026-03-04-clix-integration-implementation.md)
- MkDocs site — [plan](superpowers/plans/2026-03-23-mkdocs-site.md)
- Stop-wait — [implementation](superpowers/plans/2026-03-10-stop-wait-implementation.md)
- Display marker fix — [plan](superpowers/plans/2026-03-10-display-marker-fix.md)

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
