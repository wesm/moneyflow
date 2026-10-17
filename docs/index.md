# Moneyflow

Moneyflow shows where your money goes and lets you clean up merchants and categories in bulk.
Edits wait until you review and commit them. Use it in a browser, in a terminal, or from an AI
assistant through MCP. All three open the same profiles and share the same pending edits.

- Your data stays in a SQLite profile on your machine.
- Monarch and YNAB receive your edits only after you commit them. Amazon, bank CSV, and
  SimpleFIN edits stay local.
- Amounts are exact integers in each currency's smallest unit, so totals never pick up
  rounding errors.

## Get started

1. [Install Moneyflow](getting-started/installation.md).
2. [Try the demo, then add your data](getting-started/quickstart.md).
3. Coming from Python Moneyflow? Read [moving to Go](getting-started/transition.md) first.

## Pick an interface

| Interface | Start it | Guide |
| --- | --- | --- |
| Browser | `moneyflow web` | [Web interface](guide/web.md) |
| Terminal | `moneyflow tui` | [Terminal interface](getting-started/go.md) |
| AI assistant | `moneyflow mcp --profile "Example Profile"` | [MCP server](guide/mcp.md) |

## Find a task

- Connect [Monarch Money](guide/monarch.md), [YNAB](guide/ynab.md), or
  [SimpleFIN](guide/simplefin.md) (experimental).
- Import [Amazon orders](guide/amazon-mode.md) and match them to bank charges.
- Import [Chase bank CSV files](guide/bank-csv.md).
- [Find transactions](guide/navigation.md), [edit them](guide/editing.md), or
  [export them](guide/export.md).
- Look up a [command or flag](reference/cli.md).
- [Build Moneyflow and contribute](development/developing.md).

## For agents

Start with [Use Moneyflow with an agent](guide/agents.md). It covers MCP setup, safe commands,
and what an agent must not do.

Every page is also published as Markdown. Replace the trailing `/` with `.md`:
`https://moneyflow.dev/docs/guide/web/` becomes `https://moneyflow.dev/docs/guide/web.md`.
Section front pages keep their file name: this page is `https://moneyflow.dev/docs/index.md`,
and `/docs/architecture/` is `/docs/architecture/index.md`. `https://moneyflow.dev/llms.txt`
lists every page.
