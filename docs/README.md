# Maintaining the documentation

Help readers use or maintain Moneyflow without reconstructing its development history.
Follow the [documentation rules in AGENTS.md](https://github.com/wesm/moneyflow/blob/main/AGENTS.md#documentation).

## Where does a fact belong?

| Reader question | Owning document |
| --- | --- |
| What is Moneyflow? | [Overview](index.md); README routes to guides |
| How do I install or update it? | [Installation](getting-started/installation.md) |
| What happens to my Python data? | [Moving to Go](getting-started/transition.md) |
| How do I import and match Amazon purchases? | [Amazon](guide/amazon-mode.md) |
| How does a provider connect, store credentials, or write changes? | Its guide under `guide/` |
| What does a command accept? | Command `--help`; [CLI reference](reference/cli.md) routes to it |
| How do components fit together now? | [Architecture](architecture/index.md) |
| How do we build and publish? | [Development](development/developing.md) and [releases](development/releases.md) |

Update the owning section when behavior changes. Link to it from related pages instead of
copying details. Keep exact commands, limits, failure behavior, and authorization requirements.

## What gets published?

`docs/site-manifest.json` selects published pages. `docs/zensical.toml` defines current
navigation. The combined site includes the homepage, walkthrough, current guides, and a
frozen Python v1 archive. See [website operation](architecture/website.md) for layout and
capture tools. README and command help must agree with the current guides.

The checked-out docs describe that revision. Mark unreleased behavior clearly; a change on
`main` or `go-port` is not proof that the latest release contains it. Stable Go releases
deploy docs from their tag after publication. Prereleases do not replace the public site.
A manual deployment requires an explicit stable release tag.

Keep current contracts and their rationale in the architecture and user guides. Use Git
history for implementation chronology. Describe unbuilt work explicitly as a limitation;
an approved proposal is not evidence that a feature exists.

## How do I check a change?

Use the Go and Bun versions and private temporary-directory setup in
[development](development/developing.md). The website build captures synthetic TUI and web
screens, so it also needs `tmux`, Freeze 0.2.2, and Playwright Chromium. Install Freeze into
a task-local directory and use the capture tool's explicit binary setting:

```bash
make web-install
GOBIN="$TMPDIR/site-tools" go install github.com/charmbracelet/freeze@v0.2.2
export FREEZE_BIN="$TMPDIR/site-tools/freeze"
cd web && bunx playwright install --with-deps chromium && cd ..
```

Install `tmux` with your operating system's package manager if it is missing. Then run:

```bash
uv sync --project docs --frozen
make docs-check
```

The check lints Markdown, checks arrow-list formatting, and builds into ignored `site/`.
`docs/tools/site.ts` stages only manifest-listed pages, leaves this README in the repository,
and checks links in the complete built site.

After building, preview the output:

```bash
make docs-serve
```

Open `http://127.0.0.1:8000/`. Rebuild after editing.

Use the existing synthetic TUI frames or browser fixtures when a screenshot materially
helps a reader. Inspect the rendered image. Do not capture private profiles or label a
Python screenshot as the Go application. Generated screenshots and embedded web assets stay
ignored; retained visual assets belong on a separately managed orphan-assets branch.

The documentation toolchain uses Python and uv. The application, its tests, release archives,
and end users do not require Python. Do not restore the Python application for docs builds.
