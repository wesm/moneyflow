# Moneyflow

Review and organize personal finance transactions from a keyboard-driven terminal,
a browser, or an MCP client. Moneyflow runs as one Go binary with the web app embedded.

This checkout contains the Go replacement. It is not a claim that a stable Go release
has been published. Check [GitHub Releases](https://github.com/wesm/moneyflow/releases)
for released binaries; the older PyPI package is the retired Python application.

## Try it

Install [mise](https://mise.jdx.dev/getting-started.html) and Make, then use the pinned tools:

```bash
mise trust
mise install
mise exec -- make web-install build
```

Follow [development setup](docs/development/developing.md) for the private temporary directory
needed by Unix demos, then run `mise exec -- make tui-demo` or `mise exec -- make web-demo`.
Demo data is synthetic and temporary. To keep your own data, run `./bin/moneyflow tui` or
`./bin/moneyflow web` and choose **Add profile**.

For binaries, installers, updates, and Windows instructions, see
[installation](docs/getting-started/installation.md).
If you used Python Moneyflow, read [moving to Go](docs/getting-started/transition.md)
before connecting accounts. Local-only Python edits do not transfer automatically.

## Connect your data

| Source | What you can do |
| --- | --- |
| [Monarch Money](docs/guide/monarch.md) | Import transactions and review edits before writing them back |
| [YNAB](docs/guide/ynab.md) | Connect one plan and write supported payee/category edits and deletions |
| [Amazon](docs/guide/amazon-mode.md) | Import order-history CSV files, categorize purchases locally, and match bank charges to products |
| [Bank CSV](docs/guide/bank-csv.md) | Import Chase credit-card exports, reconcile overlapping files, and preserve local edits |
| [SimpleFIN](docs/guide/simplefin.md) | Import bank data and edit locally; experimental, with live-bank testing pending |

Use the same profiles in the terminal, browser, and [MCP server](docs/guide/mcp.md).
Edits and undo/redo history survive restart. Review changes before committing them.
Money is stored exactly as signed integer minor units, never floating point.

## Find your next step

- [Quick start](docs/getting-started/quickstart.md): open a profile and review a transaction.
- [Amazon guide](docs/guide/amazon-mode.md): import, reimport, and inspect matched products.
- [Keyboard shortcuts](docs/guide/keyboard-shortcuts.md): navigate and edit without a mouse.
- [Exports](docs/guide/export.md): save transactions for analysis.
- [Configuration](docs/config/advanced.md): choose data paths and configure web access.
- [Security](SECURITY.md): understand local storage and network boundaries.
- [Development](docs/development/developing.md): build and verify changes.
- [Documentation maintenance](docs/README.md): writing rules, ownership, and publishing.

Moneyflow is MIT-licensed. The website is [moneyflow.dev](https://moneyflow.dev);
it follows stable Go releases after cutover, not every change in this branch.
