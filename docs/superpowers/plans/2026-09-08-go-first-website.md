# Go-first website implementation plan

> **For agentic workers:** Use superpowers:executing-plans to implement this plan inline, task by task.

**Goal:** Build one Go-first static website with a pinned Python documentation archive.

**Architecture:** Bun stages explicitly selected Markdown, invokes a separate locked Zensical
environment for current and archived docs, and assembles static pages and redirects into `site/`.
Built-output checks validate the links and metadata users actually receive.

**Tech stack:** Static HTML/CSS, Bun tooling and tests, Zensical 0.0.50 in a docs-only uv project.
The existing pinned Zensical release is published on PyPI and supports the TOML project settings;
verify template behavior by rendering, not by assuming hooks from newer online documentation.

**Spec:** [Approved website design](../specs/2026-09-08-go-first-website-design.md).

## Global constraints

- Stay on `go-port`; no push, deployment, branch change, or production profile access.
- Keep generated `site/` and staging outputs ignored; no screenshots in Git.
- Pin archive provenance to `e656f3c9502e700865e9a0f03c3130cc0f5f212c` / Python 0.11.1.
- Current docs use `/docs/`; archive uses `/legacy/v1/`; root and `/guide/` are static pages.
- Public inputs are explicit; historical specs are linked on GitHub, not copied into the site.
- Run provider tests with live opt-ins disabled. Documentation builds need no Moneyflow Python imports.
- Commit verified checkpoints; existing automatic RoboRev jobs may be inspected, not duplicated.

## Checkpoint 1: assembly and archived source

**Files:** Create `docs/pyproject.toml`, `docs/uv.lock`, `docs/zensical.toml`,
`docs/zensical-legacy.toml`, `docs/site-manifest.json`, `docs/legacy/v1/manifest.json`,
selected archived Markdown, `docs/tools/site.ts`, `docs/tools/site.test.ts`, and template overrides.
Modify `.gitignore` and `Makefile`.

**Interfaces:** `stagePages(sourceRoot, outputRoot, paths)` copies only manifest paths;
`redirectHTML(destination)` emits a static HTML redirect with an ordinary fallback link;
`checkSite(root)` resolves internal links and fragments against built files.
`bun docs/tools/site.ts build|check|serve` owns the combined output, with preview on loopback.

- [x] Write Bun tests for selected-source staging, path containment, redirect escaping and
  preserving fragments, and broken-link/fragment reporting. Use `mkdtemp` fixtures, not the repo:

  ```ts
  await stagePages(source, output, ['index.md'])
  expect(await readdir(output)).toEqual(['index.md'])
  expect(redirectHTML('/legacy/v1/guide/ynab/')).toContain('/legacy/v1/guide/ynab/')
  ```

- [x] Run `bun test docs/tools/site.test.ts` and observe missing implementation failures.
- [x] Implement staging, two Zensical invocations and combined output assembly. Stage into
  `docs/.site-work/` only; publish generated output to repository `site/`. Rebuild from a clean
  owned output each time. Test the build through `make docs-build`.
- [x] Extract selected tag documentation once with `git show <commit>:<path>` through
  `apply_patch`. Record original paths and hashes in the manifest; inspect changes to installs,
  reset recipes, links, badges and missing-image notices. No tag checkout or old application run.
- [x] Use archive template overrides for banner and robots metadata; inspect generated HTML.
  If the pinned release cannot render a required hook, implement bounded HTML post-processing
  in the builder and test its output. Give the archive its own canonical prefix and search.
- [x] Generate explicit old-path redirect stubs. Keep `/guide/` itself for the new walkthrough;
  preserve old topic paths. Never overwrite an existing generated page with a redirect.
- [x] Run tests, build, Markdown and privacy checks; include in the integrated site commit.

## Checkpoint 2: current Go documentation

**Files:** Update `docs/index.md`, `docs/getting-started/go.md`, `docs/guide/` usage pages,
`docs/reference/cli.md`, troubleshooting, `docs/architecture/cutover.md`; create
`docs/getting-started/python-transition.md`, `docs/guide/web.md` and `docs/architecture/website.md`.
Extend the explicit manifest and current Zensical navigation as pages become current.

- [x] Ground commands in `cmd/moneyflow` registrations and flags; ground keyboard behavior in
  `internal/tui/keymap.go` and current action policies. Correct the stale quick-start MCP limitation.
- [x] Write the transition page as new content, not a link to a nonexistent destination.
  Link to the detailed cutover inventory without duplicating its full release checklist.
- [x] Rewrite navigation, editing, filters, provider and export instructions against Go.
  Document staged edits, provider restrictions, committed export scope, profile recovery and
  reverse-proxy base paths. Keep SimpleFIN explicitly legacy-only.
- [x] Set current edit links to `edit/go-port/docs/`; disable archive edit links. Rewrite
  excluded historical source links to GitHub at staging rather than publishing those directories.
- [x] Run `make docs-build docs-check`; inspect links and search indexes from both builds.
  Current search locations must stay under `/docs/`, archived ones under `/legacy/v1/`.
- [x] Run repository Markdown/Python checks and inspect the content diff; include in the integrated site commit.

## Checkpoint 3: landing pages and publishing contract

**Files:** Create `docs/website/index.html`, `docs/website/guide/index.html`,
`docs/website/styles/site.css`; modify `.github/workflows/docs-build-check.yml`,
`.github/workflows/docs.yml`, `AGENTS.md`, and the living website instructions.

- [x] Build responsive semantic static pages: introduction, three interface entry points,
  refinement sequence, provider summary, preview CTA and discreet legacy navigation.
  Use CSS and text/code examples, no fabricated screenshots, metrics or testimonials.
- [x] Share colors/navigation with Zensical using its theme override and stylesheet. Verify
  keyboard focus and contrast at desktop and narrow widths in the browser.
- [x] Update build CI to run the docs-only build/check and upload the complete `site/` artifact.
  Replace screenshot generation and app dependency installation. Include `go-port` push and PR paths.
- [x] Make deployment manual with a named production environment, building/checking the same
  artifact before publication. Document that environment protection and disabling the old stable
  workflow require separately authorized GitHub configuration; do not execute deployment.
- [x] Run the combined site from loopback. Inspect homepage, walkthrough, current quick start,
  transition page, legacy installation and old deep-link redirects. Verify canonical/robots
  tags, navigation, fragment redirects, fallback links and isolated search in built output.
- [x] Run full focused tooling checks, Markdown, Python regression/type/lint checks, and privacy
  scan. Commit sources only, inspect automatic review findings, and report built vs published.

## Completion evidence

The three checkpoints were integrated into one source commit so the public manifest never points
at not-yet-committed pages. Zensical requires its output beneath the config's project root, so the
archive config lives at `docs/zensical-legacy.toml`, not inside `docs/legacy/`.

- `make docs-test docs-build docs-check`: passed. Both Zensical builds reported no issues.
- Markdown, arrow-list check, Python type/format/lint checks: passed.
- Python regression/coverage: 1,839 passed, 5 skipped, 16 deselected; 85% aggregate coverage.
  Provider live opt-ins were disabled.
- `actionlint` on both documentation workflows: passed.
- Isolated docs environment: Zensical 0.0.50 present; Moneyflow, Textual and Polars absent.
- Chromium rendered desktop and 390px mobile homepage, walkthrough and documentation/archive
  pages. The homepage had no horizontal overflow; the skip link focused and navigated to main.
  Visual review corrected banner-link contrast. Screenshots remained outside the repository.
- Search navigation for `commit` landed within current docs; `SimpleFIN` within the archive.
  Old YNAB route preserved query and fragment. Archive canonical URLs and robots tags verified.
- In-app browser setup failed before obtaining a browser; isolated installed Playwright provided
  the render evidence instead. No personal browser profile was used.
- Automatic reviews of preceding commits were passing when inspected. No duplicate review started.

No deployment, push, live provider write or profile migration was performed. A site build does not
close release, live-provider or migration gates.
