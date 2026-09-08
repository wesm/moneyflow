# Command reference

Bare `moneyflow` prints Cobra help. The Go application uses explicit subcommands:

| Command | Purpose |
| --- | --- |
| `moneyflow tui` | Profile selector and terminal interface |
| `moneyflow web` | Browser interface and API |
| `moneyflow mcp` | MCP server; read-only by default |
| `moneyflow provider` | Connection, import and provider management commands |
| `moneyflow version` | Build version |
| `moneyflow openapi` | Web API schema tooling |

Use each command's `--help` for the current flags. TUI flags are not global flags:
`moneyflow --demo` is rejected; use `moneyflow tui --demo` or `moneyflow web --demo`.
`--theme` belongs to the TUI, not the web command.

## Profiles and previews

`--profile` resolves an opaque ID first, then a unique normalized display name. Do not guess
when a name is ambiguous. TUI/web without a profile show selection; demo mode bypasses the catalog.
Developer `--fixture` also uses a temporary profile, not persistent financial state.

The default Go catalog is `~/.moneyflow/v2`. `MONEYFLOW_HOME` selects a different catalog root.
Do not use a Python profile directory as a Go database. Schema compatibility is checked before
opening the application. Preview recovery backs up and recreates; it is not migration.

## Interface-specific setup

- [Build and run](../getting-started/go.md).
- [Web mount paths and proxying](../guide/web.md).
- [MCP transport, token setup and tools](../guide/mcp.md).
- [Monarch](../guide/monarch.md), [YNAB](../guide/ynab.md), [Amazon](../guide/amazon-mode.md).

Python's YAML configuration and its `categories dump` / `categories audit` commands are not
the Go configuration model. The [legacy archive](/legacy/v1/reference/cli/) documents those commands.
