# Go-first website and Python documentation archive

Status: Information architecture approved; assembled design awaiting review.

Source baseline: `70f453e` on `go-port`.

## Goal and boundary

Make Moneyflow's public identity and operating documentation describe its Go application.
Go is the only forward-looking implementation. Keep a frozen Python documentation archive for
people who still need the old distribution, not a second maintained product site.

This work builds and verifies a deployable site. It does not publish it, release Go packages,
implement the Python launcher, migrate profiles, delete Python application code, or authorize
provider writes. Those release gates remain in the maintained cutover checklist.

## Information architecture

| Route | Purpose | Implementation |
| --- | --- | --- |
| `/` | Explain the product and direct readers to try it | Small static HTML/CSS homepage |
| `/guide/` | Walk through connect, explore, refine, review, and commit | Small static walkthrough |
| `/docs/` | Current installation, usage, reference, and architecture | Zensical |
| `/legacy/v1/` | Frozen Python documentation and pinned installation | Separate Zensical archive |

The root and walkthrough share typography, colors, navigation, and a small stylesheet. Their
navigation links to Docs, GitHub, and a discreet Python v1 archive entry. Zensical links back to
the homepage. This follows the lightweight website-plus-documentation structure used by the
reference projects; it does not add a frontend application framework or reuse the financial web
application's build.

A single Zensical homepage would require less assembly but would not provide the requested
distinct marketing/walkthrough tiers. A separate frontend framework would add a second application
toolchain. Static pages plus Zensical keep each tier small and produce one static deployment.

## Homepage and walkthrough

Lead with local-first transaction analysis and efficient refinement, with TUI, web, and MCP as
three ways to use the same application. Explain Monarch and YNAB connections and Amazon imports
without claiming provider support is identical. Link to the current limitations table.

The homepage has a concise introduction, interface entry points, the refinement workflow,
provider summary, and getting-started links. The walkthrough adds concrete keyboard and MCP
examples, staged-edit/commit semantics, and recovery guidance. It is not a second reference manual.

Use semantic HTML, keyboard-visible focus, responsive layouts, reduced-motion behavior, and
minimal JavaScript only where an interaction requires it. Do not invent testimonials, usage
numbers, benchmark claims, pricing tiers, or screenshots of unfinished functionality.

Until Go release artifacts are available, calls to action say "Try the Go preview" and link to
verified source-build instructions. Do not show unshipped Homebrew, installer-script, or PyPI
launcher commands. The product name is Moneyflow, not a permanent "Moneyflow Go" sub-brand.

Use synthetic examples only. Generated screenshots and distributions remain ignored outputs;
durable visual assets, if retained, must use the separately managed orphan-assets policy.
The first site can use text/code examples without waiting for screenshot production.

## Current documentation

Keep source pages in the repository's existing `docs/` tree. Add an explicit public-source
manifest so the build stages only current documentation, selected styles, and approved assets.
Do not publish the entire directory recursively: `superpowers/`, benchmark records, build tools,
lockfiles, and historical Python pages are not current public documentation.

Use `docs/zensical.toml` for the current site and a docs-only `docs/pyproject.toml` / `docs/uv.lock`.
The documentation project is not a Python package. Pin Zensical after verifying the installed
release used for implementation; do not inherit the application dependency graph. Documentation
must build without installing or importing `moneyflow`, Textual, or Polars.

Current navigation has Start, Workflows, Interfaces, Providers, Reference, and Architecture:

- Start: build/install status, quick start, Python transition and limitations.
- Workflows: navigation, selection, editing, review/commit/recovery, duplicates, and export.
- Interfaces: TUI, web deployment including reverse-proxy base paths, and MCP.
- Providers: Monarch, YNAB, Amazon, with explicit restrictions and reconnect/import behavior.
- Reference: actual Cobra commands, configuration, troubleshooting, and release notes.
- Architecture: the existing maintained `docs/architecture/` pages.

Rewrite existing Python-oriented pages against actual Go behavior; do not merely replace
`uv run moneyflow` with `moneyflow tui`. Cache tiers, profile paths, flags, taxonomy capabilities,
and commit behavior differ. Historical design links from architecture pages point to GitHub
source history rather than pulling excluded spec pages into the public build.

## Frozen Python archive

Use tag `v0.11.1`, resolved to commit `e656f3c9502e700865e9a0f03c3130cc0f5f212c`, as the
content source. The [published Python package][python-release] is version `0.11.1`, requires
Python 3.11 or newer, and is not yanked as checked for this design.

Do not copy the currently deployed site and call it v0.11.1: the inspected published `gh-pages`
commit `09780e8bd5d0ca2514b3a02f18249d25c7c9ff40` identifies deployment source
`3defee674faaf9749f40dafa1203ea0ff15e578b`, the v0.9.2 commit.

During implementation, make a one-time, reviewed text snapshot of the selected v0.11.1 docs under
`docs/legacy/v1/`. Record the source commit and original paths in an archive manifest. Subsequent
builds use that local text snapshot and the docs-only environment: they neither fetch a moving
branch nor run historical application code. This small documentation archive survives eventual
removal of the Python package. It is the only intentionally retained Python operating guide.

Permitted edits to the snapshot are archival labeling, pinned install commands, route/link fixes,
removal of obsolete badges, and corrections needed to prevent unsafe or misleading instructions.
No new Python feature documentation is added. Each page has a persistent "Legacy Python v1"
banner, the archived package version, and links to the Go site and transition guide.

The recommended invocation is explicit and does not replace the installed Go command:

```bash
uvx --from 'moneyflow==0.11.1' moneyflow
uvx --from 'moneyflow==0.11.1' moneyflow --demo
```

Explain Python requirements and dependency resolution separately from the package pin: a pinned
package is not a promise that future provider APIs or all future transitive dependencies will
remain compatible. Pin every legacy installation example; unqualified install/upgrade commands
must not resolve a future Python launcher by accident.

Keep Python data separate from Go's `v2/` state. Replace broad "delete the whole configuration
directory" reset recipes with stop-and-back-up guidance; they could remove both implementations'
data. The archive is not a profile migration or downgrade mechanism.

Legacy internal links and assets resolve beneath `/legacy/v1/`; links to the new site are explicit.
Do not silently reinterpret a Python CLI link as Go instructions. The v0.11.1 source tag does not
contain generated screenshot images. Omit unavailable images with an archival notice rather than
ship broken references or represent older screenshots as v0.11.1 captures. No screenshot generator
runs as part of the archive or current-site build.

The archive has its own search index and canonical URLs and is excluded from the current docs'
search. Mark archived pages `noindex` to favor current documentation while keeping them directly
accessible. Preserve original source attribution and applicable licenses.

## Gap reporting

Maintain a user-facing transition page and keep `docs/architecture/cutover.md` as the detailed
release checklist. The two have different audiences, not separate competing inventories.

Every reported gap needs its scope, source/test evidence, user impact, and next action. Distinguish:

1. Missing replacement functionality, such as the Go SimpleFIN adapter.
2. Deliberate behavior changes, such as staged MCP writes and posted-only Monarch imports.
3. Implemented behavior awaiting live or installed-release sign-off.
4. Release/data-transition blockers, including the launcher and Python-profile continuity.
5. Enhancements that neither implementation exposes as an ordinary workflow; these must not
   become invented Python-parity requirements.

For example, a Python storage helper named `delete_account` does not prove the Python selector
offers arbitrary profile deletion. Compare user-reachable workflows, not symbol counts. Likewise,
Go's YNAB hide refusal must not be called lost functionality merely because Python accepts an
argument it ignores.

The initial audit covers the actual Python keybinding/CLI/MCP entry points against Go commands,
actions, provider policies, and tests. Each section states its inspected source revision. "Tested"
means named automated evidence; it does not imply live-account approval. Keep the Go build/install
preview warning until release delivery is demonstrated.

## Build and publishing

Add canonical build/serve/check commands for the combined site. Build each documentation tier
into its own output directory, then assemble static root/walkthrough pages and both docs trees into
one ignored distribution. Use explicit source manifests and validate output paths before cleanup.
The local preview server serves the whole combined site, not only `/docs/`.

Validate internal links, fragment targets, asset paths, canonical URLs, and cross-tier navigation
against built HTML. Preserve useful existing public paths through an explicit redirect map: route
old Python documentation paths to their legacy equivalents unless a deliberately reviewed current
destination is truthful. In particular, old `/guide/<topic>/` pages may coexist with the new
`/guide/` walkthrough index. Do not add a blanket redirect that captures `/docs/` or assets.

Update `.github/workflows/docs-build-check.yml` to build the combined site on `go-port` changes
and relevant pull requests. Upload the complete site as a review artifact; do not publish previews
over production. Replace Python screenshot generation and application dependency synchronization
with the docs-only build.

Prepare `.github/workflows/docs.yml` to deploy the same verified artifact, but keep production
publication an explicitly invoked, protected workflow until cutover is authorized. Remove the old
automatic `stable` publication trigger in that workflow's new version so it cannot unexpectedly
replace the Go site after cutover. This cannot change workflow files on other branches: disabling
the old `stable` workflow before first publication is a separate, explicitly authorized release
step. It must be named in the deployment checklist.

Required deployment approvals must be configured and verified in GitHub before publication;
declaring an environment in workflow YAML alone does not establish that protection.

One complete deployment carries root, walkthrough, current docs, and legacy together. CNAME and
Pages configuration remain single-site. Building or committing this branch does not authorize a
push, Pages deployment, DNS change, or branch change.

## Verification and completion

- Build with the docs-only lockfile and no application install; build both docs tiers without
  importing Python Moneyflow or generating Python screenshots.
- Run Markdown and built-site link/asset checks, including old deep links and nested legacy URLs.
- Check current search cannot return legacy/spec content, and archive search stays under its prefix.
- Inspect root, walkthrough, representative current docs, and legacy install pages in the browser
  at desktop and narrow widths; check keyboard navigation, focus, and light/dark behavior if offered.
- Verify every displayed command against its actual implementation. Current Go examples use a
  temporary synthetic profile; the legacy pin is checked against package metadata. Do not open
  personal profiles or install over the user's binary for this work.
- Review the archive diff separately from current copy: origin, permitted edits, pinned installs,
  missing-image notices, and old/new data warnings must be visible.
- Confirm the generated distribution is ignored and the staged diff contains only intended sources.
- Update the living site/deployment instructions and gap report. Do not mark release or migration
  gates complete merely because the site builds.

Implement in three independently verified checkpoints: docs-only assembly and archive; Go-first
content and gap inventory; marketing/walkthrough styling and publishing checks. The final site
must pass as one artifact before any production deployment is proposed.

[python-release]: https://pypi.org/project/moneyflow/0.11.1/
