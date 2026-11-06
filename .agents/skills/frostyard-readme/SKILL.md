---
name: frostyard-readme
description: Write a new root README.md, or bring an existing one to the frostyard README standard, from the template for the repository's kind (tool, library, app, extension, website, image, packaging, service, infrastructure, org), moving manuals, internals and history out to the docs site or docs/. Use whenever asked to write, fix, shorten or review a README, when a sweep issue names baseline row B17, or when a new repository needs its first README.
---

# Write a README to the frostyard standard

A README is the front page of a repository. It answers 4 questions, in
order: what is it, can I use it, how do I start, and where is the rest.
The contract is core's README spec,
[docs/specs/readme.md](https://github.com/frostyard/core/blob/main/docs/specs/readme.md),
and the reason is
[ADR-0072](https://github.com/frostyard/core/blob/main/docs/adr/0072-write-every-readme-to-one-standard.md).
Read the spec before you start: it has the kinds, the sections of each kind
and the rules R1 to R12.

Done means: [scripts/check-readme.mjs](scripts/check-readme.mjs) prints
`ok`, every command, flag and path in the README is true at the commit you
read, and every paragraph you removed is either in its new place or named
as untrue in the pull request.

## Steps

1. **Pick the kind.** Go down the spec's kinds table (`org`, `image`,
   `packaging`, `app`, `extension`, `tool`, `website`, `service`, `library`,
   `infrastructure`) and take the first row that describes the main thing
   the repository delivers. A README that already has a
   `<!-- readme-kind: ... -->` marker keeps its kind unless the owner
   changes it.
2. **Run the checker** on the current README, from the repository root:

   ```sh
   node .agents/skills/frostyard-readme/scripts/check-readme.mjs .
   ```

   In core, or for another repository, give its path:
   `node .agents/skills/frostyard-readme/scripts/check-readme.mjs --ref origin/main <clone>`.
   Keep the output for the pull request.
3. **Sort the current content.** For each section or paragraph of the old
   README, decide 1 of:
   - **keep**: it answers 1 of the 4 questions; name the section it goes to;
   - **move**: it is a guide, a reference, internals, a check list, a
     runbook or history; name the place from the spec's "What stays out of
     a README" table (the docs site for a tool or app, `docs/` otherwise);
   - **drop**: it is untrue at the commit you read, or a copy of a page
     that exists. Say which.
4. **Move before you cut.** Put each moved part in its new place first: a
   site page in `site/content/` (frostyard-docs-site skill) or a page in
   `docs/`. A new `docs/` page starts from its category's `TEMPLATE.md`
   when it is an ADR, design, spec or plan; a runbook or development guide
   can be a plain page. Add every new page to `docs/README.md`
   (frostyard-repo-docs skill).
5. **Write the README from the template** for the kind:
   `templates/<kind>.md.tmpl`. Replace every `{placeholder}`.
   - The lead's first sentence says what the thing is and who it is for.
   - The status line takes 1 value: `pre-release`, `stable`,
     `in production`, `maintenance` or `deprecated`, and at most 1 more
     sentence. No history and no milestones.
   - The documentation line links the docs site for a user-facing tool or
     app (`https://frostyard.github.io/<repo>/`), pkg.go.dev for a library,
     the live site for a website, and `docs/README.md` otherwise.
   - Add an optional section only when it has content (`Requirements`,
     `How it works`, `Problem records`, `Privacy`, `Limits`, `Credits`, and
     the kind's own, from the spec). Keep `Develop` and `Where to go next`
     last.
   - `Where to go next` links each of `AGENTS.md`, `docs/README.md`,
     `SECURITY.md` and `LICENSE` that the repository has. Name the license
     (`MIT` or `GPL-3.0`).
6. **Check every fact.** Run each command in `Install`, `Use` and `Develop`
   where you safely can, or find it in the code, the Makefile, the workflow
   or the release assets. The sweep compares README promises with the code.
7. **Run the checks:**

   ```sh
   node .agents/skills/frostyard-readme/scripts/check-readme.mjs .
   make ci
   ```

   The checker must print `ok`. `make ci` must pass: in many repositories
   it checks the links in `README.md` and `docs/`.
8. **Open the pull request.** Its body has the checker output before and
   after, and a table of the old sections: kept (where), moved (to which
   page) or dropped (why). Risk tier: low, unless a moved page is on the
   repository's risk-path list.

## Best examples

These READMEs were the best of their kind in the survey of 2026-11-06. Read
one before you write; none has the marker, the status line and the
documentation line yet.

| Kind | Example | What to copy |
| --- | --- | --- |
| `tool` | frostyard/nsl | A tagline, a short lead, features with a link each, a short start, all detail on the site |
| `library` | frostyard/snowkit | `go get`, a package table, links to pkg.go.dev and the design |
| `app` | frostyard/igloo | The Flatpak install and remove commands |
| `website` | frostyard/frostyard-org (2026-11 rewrite) | What the site covers, with links to live pages |
| `image` | frostyard/snosi | The images table and the download links |
| `packaging` | frostyard/bootc-debian, frostyard/flatpak-index | What it publishes, and the consumer's install commands |
| `service` | frostyard/problem-intake | The endpoints table and links to the runbooks |
| `org` | frostyard/core | What is here, by path |

## Pitfalls

- **The status line and the documentation line on 2 adjacent lines**
  render as 1 line. Put a blank line between them.
- **A `###` heading** fails R8. Use a bold lead-in, a list or a table, or
  move the detail out.
- **A heading that the kind does not list** fails R7. "Features" becomes
  `What it does`; "Installation" matches `Install`, but "Usage" does not
  match `Use` (a heading must start with the name): rename it `Use`;
  "Contributing", "Building" and "Testing" become `Develop`; "License"
  goes into `Where to go next`; "Layout" becomes `What is here` (top level
  only) for `org` and `infrastructure`, and goes to `AGENTS.md` otherwise.
- **History in the lead or the status line** ("moved on ...", "since M3
  ...") fails R12 when it has more than 2 dates, and reads as noise. Put it
  in `docs/plans/` or the release notes.
- **A real address or host name** in a public README fails R12 or leaks a
  private detail. Use `192.0.2.1` and `example.org` in examples.
- **A long `Develop` section.** Keep 1 code block of the setup and the
  gate; link the build guide for the rest.
- **A README in a fork.** Forks follow their upstream (ADR-0071). Do not
  rewrite their README; add 1 line at the top that links the file that
  says how the fork differs.
