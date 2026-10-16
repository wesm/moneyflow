# Website and publishing

Moneyflow builds one static site for moneyflow.dev. It has a homepage at `/`, a tour at
`/guide/`, the current Zensical documentation at `/docs/`, an agent index at `/llms.txt`, and
a frozen Python archive at `/legacy/v1/`. Every current docs page also has a Markdown copy
for agents. These are documentation routes, not routes served by the financial web
application.

## Build and inspect

Follow [documentation setup](https://github.com/wesm/moneyflow/blob/main/docs/README.md#how-do-i-check-a-change)
for mise tools, tmux, Playwright, and the private temporary directory. From the repository root,
with mise activated (or each command prefixed by `mise exec --`):

```bash
make web-install
cd web && bunx playwright install chromium && cd ..
make docs-test
make docs-check
make docs-browser-test
make docs-serve
```

| Command | What it does |
| --- | --- |
| `make docs-test` | Unit tests for staging, redirects, Markdown copies, `llms.txt` and the checker |
| `make docs-check` | Lints Markdown, builds the site, then runs the built-site checker |
| `make docs-browser-test` | Opens the built site in Chromium: lightbox keyboard behavior, the Markdown link, and phone width |
| `make docs-serve` | Builds, checks, then serves the site |

Preview listens only on `http://127.0.0.1:8000/`. It serves the entire assembled site. It sends
`.md` files as `text/markdown; charset=utf-8` and `.txt` files as `text/plain; charset=utf-8`,
so a browser shows them instead of downloading them. Rebuild after changing source; the
preview does not watch files. `docs/pyproject.toml` and `docs/uv.lock` pin a separate
documentation environment. No Python Moneyflow, Textual or Polars is installed by this build.
There is no Python application environment in this checkout.

`docs/site-manifest.json` selects the published files. Its `current` list names the docs pages
and their stylesheet. Its `static` list names the homepage, tour, and their assets under
`docs/website/`. The builder stages the selected files in ignored `docs/.site-work/`, runs both
Zensical configurations, and assembles ignored `site/`. It removes only those owned output
directories before rebuilding. Do not hand-edit them. Edit links come from `edit_uri` in
`docs/zensical.toml`.

## Pages for agents

Agents read Markdown more reliably than a themed HTML page, so the build publishes both.

Each current page's Markdown is copied unchanged to its source path under `/docs/`:

| HTML page | Markdown copy |
| --- | --- |
| `/docs/` | `/docs/index.md` |
| `/docs/guide/web/` | `/docs/guide/web.md` |
| `/docs/architecture/` | `/docs/architecture/index.md` |

A section index keeps its `index.md` name. That way, relative `.md` links inside the copy
resolve to the other copies, exactly as they do in the repository.

Every current docs page links to its copy twice. `docs/overrides/main.html` adds
`<link rel="alternate" type="text/markdown">` to the page head. `docs/overrides/partials/actions.html`
adds a visible **View as Markdown** link beside the page title. Both templates derive the
source path from the page's edit URL.

The build generates `/llms.txt` in the [llmstxt.org](https://llmstxt.org/) format:

- an H1, `Moneyflow`;
- a blockquote with `site_description` from `docs/zensical.toml`;
- a paragraph that explains the Markdown copies;
- one H2 per top-level nav section, listing each page as
  `- [Title](https://moneyflow.dev/docs/<path>.md): description`;
- an `## Optional` section with one link to the Python archive.

The title is the page's H1, and the description is its first paragraph with links reduced to
their text. The build reads both from the staged page and the nav from `docs/zensical.toml`,
so the index cannot drift from the docs. The build fails if a nav page is not published or has
no H1 and opening paragraph. Nested nav sections are listed under their top-level section.

## What the checker enforces

`bun docs/tools/site.ts check` reads the built `site/` and fails on any of these:

- A same-site link, asset, or fragment does not resolve.
- A page other than a 404 or a redirect lacks the canonical URL for its route.
- The archive's `legacy/v1/index.html` is missing, or an archive page lacks `noindex`.
- A docs page other than `docs/404.html` has no Markdown copy, no alternate link, or no
  visible Markdown link, or either link points somewhere other than its copy.
- `llms.txt` is missing, or one of its links does not resolve to a built file.
- A Markdown page in the manifest's `current` list is missing from the nav in
  `docs/zensical.toml`.

Inspect representative pages in a browser too. A passing link checker does not prove that a
responsive layout or keyboard focus is usable.

## Homepage and tour

The homepage and tour are hand-written HTML in `docs/website/`, styled by
`docs/website/styles/site.css`. They follow the reader's light or dark system setting. The
Zensical docs use the scheme set in `docs/zensical.toml`.

The homepage is for experienced users. It has, in order:

1. the hero, with the browser capture and the terminal capture side by side;
2. the review keys, which work in both interfaces;
3. the three interfaces (browser, terminal, MCP), each with its command and guide, plus links to
   `/llms.txt` and the agent guide;
4. what Moneyflow does;
5. the sources, and where each one's committed edits go;
6. the demo commands;
7. the release status.

The release status must match [installation](../getting-started/installation.md) and
[moving to Go](../getting-started/transition.md). Update it when either changes.

The tour at `/guide/` walks through five steps: start with the demo, explore, select and edit,
review and commit, and export. Each step pairs a browser capture with a terminal capture where
both exist, and names the browser's pointer actions beside the shared keys.

Both pages share one nav: Docs, Tour, and GitHub. Keep the skip link, landmarks, alt text,
focus outlines, and lightbox keyboard behavior when editing them.

## Real application captures

The homepage and tour use real Go application captures, not illustrations. Every site build
runs `make docs-screenshots`. It builds the current binary, opens `moneyflow tui --demo` in an
isolated tmux server, and turns its ANSI terminal output into SVG using
[Freeze](https://github.com/charmbracelet/freeze). The captures keep the application's text,
colors and layout. Freeze embeds the terminal font. Browser images are high-resolution PNGs
captured with Playwright from `moneyflow web --demo`, using the existing isolated browser-test
server helper. Both renderers use generated synthetic transactions and temporary SQLite
profiles; no live profile or provider is involved.

The sequence shows merchant totals, a drill into transactions, selecting two rows, changing their
category, reviewing the pending change, and opening export. The browser run also captures a
390 by 844 phone layout in the dark scheme and the anchored editor with "Books" typed in
the Category box, before the change is staged. It never commits or writes an export.
The lightbox supports keyboard activation, Escape, a Close button and focus restoration.
Image links still open directly without JavaScript. The original SVG or PNG stays available for
full-resolution inspection.

Capture runs require macOS or Linux (tmux); CI uses Linux. Generated SVGs and PNGs live in ignored
`docs/.screenshots/` and are copied into `site/screenshots/`. They are rebuilt, not committed.
If durable assets are kept later, they belong on a separately managed orphan assets branch.

## Archive provenance

`docs/legacy/v1/manifest.json` records the v0.11.1 source commit and Git blob for each selected
original Markdown file. The checked-in text includes reviewed archival edits: version-pinned
installation, banners, missing-image notices, old-source links and safer reset guidance.
The text is frozen; only archive maintenance belongs there. It does not receive new Go features.
The archive has no Markdown copies and appears in `llms.txt` only under `## Optional`.

The source tag did not contain generated screenshots. Missing images are omitted, not regenerated
from historical Python code. Source attribution and the repository's MIT license still apply.
Builds use the local archive; they do not fetch tags, switch branches or depend on Python source
remaining in the repository.

Legacy search is a separate index beneath its prefix. Existing Python topic URLs have explicit
static redirect pages. They keep a bookmark's query and fragment in the browser and provide a
fallback link without JavaScript. `/guide/` remains the tour. Add redirects to the manifest only
with a reviewed destination; do not blanket-redirect current docs paths.

## Publication is a separate action

Build CI checks the combined output and uploads it as an artifact. It does not publish. The deploy
workflow runs after a stable release or by manual dispatch with a stable release tag. It names
the `documentation-production` environment. An
environment name alone is not approval protection: repository settings must enforce that policy.

Before the first authorized deployment:

1. Review the built artifact, release status wording, legacy installation and transition warnings.
2. Configure and verify required reviewers for `documentation-production` in GitHub.
3. Disable the old automatic documentation deployment on `stable`. Changing a workflow on
   `main` does not change the copy on that branch.
4. Confirm that Pages still uses the intended `gh-pages` output and `moneyflow.dev` domain.
5. Dispatch deployment for a published stable Go release tag. The workflow verifies that the
   tag is annotated and belongs to `main`, and rejects drafts and prereleases.
   Root, tour, current docs, `llms.txt` and archive publish together. Afterward, verify old deep
   links, both search indexes, and a few Markdown copies on the public site.

Do not interpret a local build, commit or uploaded preview artifact as permission to publish,
change DNS or release the Go application. The [cutover checklist](cutover.md) remains authoritative
for release and data-transition gaps.
