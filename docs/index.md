# Moneyflow

Moneyflow is a local-first tool for reviewing personal finance transactions. One binary
serves three interfaces over the same saved profiles: a browser app, a terminal app (TUI),
and an MCP server for assistants. Group spending, drill into purchases, rename merchants and
recategorize in bulk, then review every pending change before you commit it.

Money is stored as exact signed integers in minor units, never floating point. Your data
stays in a SQLite profile on your machine. Monarch and YNAB edits reach the provider only
after you review and commit them. Amazon, bank CSV, and SimpleFIN edits stay local.

These pages describe the Go application at this revision. See
[installation](getting-started/installation.md) for the difference between source builds,
preview binaries, and the latest stable release.

## Pick an interface

| Interface | Start it | Guide |
| --- | --- | --- |
| Browser | `moneyflow web` | [Web interface](guide/web.md) |
| Terminal | `moneyflow tui` | [Terminal interface](getting-started/go.md) |
| MCP client | `moneyflow mcp --profile "Example Profile"` | [MCP server](guide/mcp.md) |

The browser and terminal share keys and workflows. Pending edits made in one interface show
up in the others, and they survive restart.

## What do you want to do?

- [Try the demo or connect an account](getting-started/quickstart.md).
- [Move from Python without deleting your data](getting-started/transition.md).
- [Import Amazon purchases and match bank charges](guide/amazon-mode.md).
- [Import Chase bank CSV files](guide/bank-csv.md).
- [Connect Monarch Money](guide/monarch.md), [YNAB](guide/ynab.md), or
  [experimental SimpleFIN](guide/simplefin.md).
- [Move through and search transactions](guide/navigation.md),
  [edit them](guide/editing.md), or [export them](guide/export.md).
- [Connect an assistant through MCP](guide/mcp.md).
- [Build and contribute](development/developing.md).

Each provider guide owns its limitations, credential storage, and refresh behavior.

## For agents

Coding and assistant agents should start with [Use Moneyflow with an agent](guide/agents.md).
It covers MCP setup, safe commands, and the rules an agent must follow.

Every page is also published as Markdown at its source path. The web guide
`https://moneyflow.dev/docs/guide/web/` is at `https://moneyflow.dev/docs/guide/web.md`.
Section index pages keep their file name: this page is `https://moneyflow.dev/docs/index.md`,
and `/docs/architecture/` is `/docs/architecture/index.md`. The index of all pages is
`https://moneyflow.dev/llms.txt`.
