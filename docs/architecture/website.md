# Website and publishing

Moneyflow builds one static site: a homepage at `/`, a walkthrough at `/guide/`, current Zensical
documentation at `/docs/`, and a frozen Python archive at `/legacy/v1/`. These are documentation
routes, not routes served by the financial web application.

## Build and inspect

From the repository root, with Go, Bun, uv, tmux and Freeze 0.2.2 installed:

```bash
make web-install
# Install the capture tool locally, without replacing a global binary.
GOBIN="$PWD/bin" go install github.com/charmbracelet/freeze@v0.2.2
export FREEZE_BIN="$PWD/bin/freeze"
cd web && bunx playwright install chromium && cd ..
make docs-test
make docs-build
make docs-check
make docs-browser-test
make docs-serve
```

Preview listens only on `http://127.0.0.1:8000/`. It serves the entire assembled site. Rebuild
after changing source; the preview does not watch files. `docs/pyproject.toml` and `docs/uv.lock`
pin a separate documentation environment. No Python Moneyflow, Textual or Polars is installed by
this build. The existing root application environment is not part of the website toolchain.

`docs/site-manifest.json` explicitly selects current pages and static assets. The builder stages
them in ignored `docs/.site-work/`, runs both Zensical configurations, and assembles ignored
`site/`. It removes only those owned output directories before rebuilding. Do not hand-edit them.
Historical design links are rewritten to GitHub source links; the public manifest does not include
the historical specs or plans. Current edit links point to `go-port`.

The built checker resolves same-site links, fragments, assets and canonical URLs. Archive pages
must carry `noindex`. Inspect representative pages in a browser too: a passing link checker does
not prove that a responsive layout or keyboard focus is usable.

## Real application captures

The homepage and walkthrough use real Go application captures, not illustrations. Every site build
runs `make docs-screenshots`: it builds the current binary, opens `moneyflow tui --demo` in an
isolated tmux server, and turns its ANSI terminal output into SVG using
[Freeze](https://github.com/charmbracelet/freeze). The captures retain the application's text,
colors and layout. Freeze embeds the terminal font. Browser images are high-resolution PNGs
captured with Playwright from `moneyflow web --demo`, using the existing isolated browser-test
server helper. Both renderers use the existing embedded synthetic fixture and temporary SQLite
profiles; no live profile or provider is involved.

The sequence shows merchant totals, a drill into transactions, selecting two rows, changing their
category, reviewing the pending change, and opening export. It never commits or writes an export.
The website lightbox supports keyboard activation, Escape, a Close button and focus restoration;
image links still open directly without JavaScript. The original SVG or PNG remains available for
full-resolution inspection.

Capture runs require macOS or Linux (tmux); CI uses Linux. Generated SVGs and PNGs live in ignored
`docs/.screenshots/` and are copied into `site/screenshots/`. They are rebuilt rather than committed
on `go-port`. If durable assets are retained later, they belong on a separately managed orphan
assets branch. There is no Python screenshot generator or parity-golden update in this pipeline.

## Archive provenance

`docs/legacy/v1/manifest.json` records the v0.11.1 source commit and Git blob for each selected
original Markdown file. The checked-in text includes reviewed archival edits: version-pinned
installation, banners, missing-image notices, old-source links and safer reset guidance.
The text is frozen; only archive maintenance belongs there. It does not receive new Go features.

The source tag did not contain generated screenshots. Missing images are omitted, not regenerated
from historical Python code. Source attribution and the repository's MIT license still apply.
Builds use the local archive; they do not fetch tags, switch branches or depend on Python source
remaining in the repository.

Legacy search is a separate index beneath its prefix. Existing Python topic URLs have explicit
static redirect pages, preserving bookmark query and fragment in the browser and providing a
fallback link without JavaScript. The new `/guide/` index remains the walkthrough. Add redirects
to the manifest only with a reviewed destination; do not blanket-redirect current docs paths.

## Publication is a separate action

Build CI checks the combined output and uploads it as an artifact. It does not publish. The deploy
workflow requires manual dispatch and names the `documentation-production` environment. An
environment name alone is not approval protection: repository settings must enforce that policy.

Before the first authorized deployment:

1. Review the built artifact, preview wording, legacy installation and transition warnings.
2. Configure and verify required reviewers for `documentation-production` in GitHub.
3. Disable the old automatic documentation deployment on `stable`. Changing a workflow on
   `go-port` does not change the copy on that branch.
4. Confirm that Pages still uses the intended `gh-pages` output and `moneyflow.dev` domain.
5. Dispatch deployment for the reviewed commit. Root, walkthrough, current docs and archive
   publish together; verify old deep links and both search indexes on the public site afterward.

Do not interpret a local build, commit or uploaded preview artifact as permission to publish,
change DNS or release the Go application. The [cutover checklist](cutover.md) remains authoritative
for release and data-transition gaps.
