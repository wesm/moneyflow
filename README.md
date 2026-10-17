# Moneyflow

Moneyflow shows where your money goes and lets you clean up merchants and categories in bulk.
Edits wait until you review and commit them. Use it in a browser, in a terminal, or from an AI
assistant through MCP. Your data stays in a local SQLite profile.

- **Docs:** [moneyflow.dev/docs](https://moneyflow.dev/docs/)
- **For agents:** [`moneyflow.dev/llms.txt`](https://moneyflow.dev/llms.txt) lists every page,
  and each page has a Markdown copy, such as `https://moneyflow.dev/docs/guide/web.md`.

## Install

On Linux or macOS:

```bash
curl -fsSL https://github.com/wesm/moneyflow/releases/latest/download/install.sh -o install.sh
less install.sh
sh install.sh
```

For Windows, manual downloads, and building from source, see
[installation](docs/getting-started/installation.md). Coming from Python Moneyflow? Read
[moving to Go](docs/getting-started/transition.md) first.

## Try it

```bash
moneyflow web --demo   # in your browser
moneyflow tui --demo   # in your terminal
```

The demo uses made-up data and needs no account. To use your own data, run `moneyflow web` or
`moneyflow tui` and choose **Add profile**.

## Connect your data

| Source | What you can do |
| --- | --- |
| [Monarch Money](docs/guide/monarch.md) | Import transactions and review edits before writing them back |
| [YNAB](docs/guide/ynab.md) | Connect one plan and write supported payee/category edits and deletions |
| [Amazon](docs/guide/amazon-mode.md) | Import order-history CSV files, categorize purchases locally, and match bank charges to products |
| [Bank CSV](docs/guide/bank-csv.md) | Import Chase credit-card exports, reconcile overlapping files, and preserve local edits |
| [SimpleFIN](docs/guide/simplefin.md) | Import bank data and edit locally; experimental, with live-bank testing pending |

Use the same profiles in the [browser](docs/guide/web.md), the
[terminal](docs/getting-started/go.md), and the [MCP server](docs/guide/mcp.md).
Pending edits and undo history survive a restart. Amounts are exact integers in each
currency's smallest unit, never floating point.

## Find your next step

- [Quick start](docs/getting-started/quickstart.md): open a profile and review a transaction.
- [Use Moneyflow with an agent](docs/guide/agents.md): MCP setup, safe commands, and rules.
- [Amazon guide](docs/guide/amazon-mode.md): import, reimport, and inspect matched products.
- [Keyboard shortcuts](docs/guide/keyboard-shortcuts.md): navigate and edit without a mouse.
- [Exports](docs/guide/export.md): save transactions for analysis.
- [Configuration](docs/config/advanced.md): choose data paths and configure web access.
- [Security](SECURITY.md): understand local storage and network boundaries.
- [Development](docs/development/developing.md): build, run with live reload, and verify changes.
- [Documentation maintenance](docs/README.md): writing rules, ownership, and publishing.

Moneyflow is MIT-licensed. The website, [moneyflow.dev](https://moneyflow.dev), is published
from each stable release, so it can lag behind `main`.
