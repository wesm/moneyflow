# Command reference

Run Moneyflow from the command line: open the terminal application (TUI), serve the browser
application or an MCP server, and manage profiles and providers. This page lists every command
and flag. `moneyflow --help`, or `--help` after any command, shows the same for the binary you
have installed.

## Profiles and data location

Profiles live in `~/.moneyflow`. Set `MONEYFLOW_HOME` to use another directory. Moneyflow
refuses a directory that holds Python Moneyflow data; see
[moving to Go](../getting-started/transition.md). [Storage](../config/caching.md) describes the
directory layout.

Commands that take `--profile NAME_OR_ID` accept a profile's display name or its ID:

- A name that matches no profile stops the command with `no profile named "NAME".` and lists the
  profiles that exist.
- A name that several profiles share stops the command, lists them with their IDs, and asks you
  to pass an ID.
- Without `--profile`, `tui` and `web` open the profile selector. Other commands use the only
  profile in a one-profile catalog and stop when the catalog has several.

Amazon and bank CSV imports create a profile when the name is unused. Provider connect commands
do not create named profiles; add those in the TUI or the browser. In an empty catalog, a connect
command creates one profile named after the provider.

Never pass credentials in command arguments. Monarch and YNAB setup prompt on an interactive
terminal with masked input. SimpleFIN also reads its setup token or Access URL from standard
input when standard input is not a terminal.

## Commands

| Command | Purpose | Guide |
| --- | --- | --- |
| `moneyflow tui` | Open the terminal application | [Quick start](../getting-started/go.md) |
| `moneyflow web` | Serve the browser application | [Web](../guide/web.md), [paths and web access](../config/advanced.md) |
| `moneyflow mcp` | Serve one profile through the Model Context Protocol | [MCP](../guide/mcp.md) |
| `moneyflow mcp token reveal` | Reveal the profile's streamable-HTTP bearer token | [Authenticated HTTP](../guide/mcp.md#authenticated-http) |
| `moneyflow mcp token rotate` | Rotate and reveal that token | [Authenticated HTTP](../guide/mcp.md#authenticated-http) |
| `moneyflow profile export` | Export an existing profile to an unencrypted JSONL file | [JSONL transfer](../getting-started/transition.md#transfer-a-go-profile-with-jsonl) |
| `moneyflow profile import` | Import JSONL into a new profile without credentials | [JSONL transfer](../getting-started/transition.md#transfer-a-go-profile-with-jsonl) |
| `moneyflow profile rename` | Rename a profile in place; its data, credentials, and ID stay | [Rename a profile](../getting-started/go.md#rename-a-profile) |
| `moneyflow provider connect monarch` | Connect and import one Monarch profile | [Monarch](../guide/monarch.md) |
| `moneyflow provider connect ynab` | Connect and import one YNAB profile | [YNAB](../guide/ynab.md) |
| `moneyflow provider connect simplefin` | Connect experimental SimpleFIN; edits stay in Moneyflow | [SimpleFIN CLI](../guide/simplefin-cli.md) |
| `moneyflow provider disconnect monarch` | Remove the local Monarch session | [Monarch](../guide/monarch.md#troubleshooting) |
| `moneyflow provider disconnect ynab` | Remove the local YNAB credential vault | [YNAB](../guide/ynab.md) |
| `moneyflow provider import amazon DIRECTORY` | Import Amazon order-history CSV files | [Amazon](../guide/amazon-mode.md) |
| `moneyflow provider import list` | List bank CSV mappings | [Bank CSV](../guide/bank-csv.md) |
| `moneyflow provider import institution MAPPING FILE_OR_DIRECTORY` | Import bank CSV exports into a separate local profile | [Bank CSV](../guide/bank-csv.md) |
| `moneyflow openapi` | Write the HTTP API contract | |
| `moneyflow version` | Print version, commit, and build time | |
| `moneyflow completion SHELL` | Generate a completion script for `bash`, `fish`, `powershell`, or `zsh` | |

## Interfaces

### `moneyflow tui`

| Flag | Meaning |
| --- | --- |
| `--profile NAME_OR_ID` | Open this profile instead of showing the selector |
| `--demo` | Open a temporary profile seeded with synthetic data |
| `--year YYYY` | Show transactions from January 1 of that year through today |
| `--since YYYY-MM-DD` | Show transactions from that date through today |
| `--mtd` | Show month-to-date transactions |
| `--theme NAME` | Color theme; default `default`. See [themes](../config/themes.md) |

`--mtd` takes precedence over `--since`, then `--year`. These flags filter local data only; see
[filters](../guide/filters.md#start-the-tui-with-a-date-filter). An unknown theme stops with
`unknown theme "NAME"`.

### `moneyflow web`

| Flag | Meaning |
| --- | --- |
| `--profile NAME_OR_ID` | Serve this profile instead of showing the selector |
| `--demo` | Serve a temporary profile seeded with synthetic data |
| `--listen HOST:PORT` | Listener address; default `127.0.0.1:8080` |
| `--open` | Open the application in a browser; default `true`. Use `--open=false` to skip |
| `--base-path PATH` | URL mount path; default `/` |
| `--external-url URL` | Canonical browser URL through a trusted proxy |

Without `--listen`, `--demo` uses a free `127.0.0.1` port, and so does a normal start when port
8080 is busy; Moneyflow prints that it switched. A non-loopback `--listen` address starts with
`Warning: serving unauthenticated financial data on a non-loopback address.` The web application
has no user authentication. See [paths and web access](../config/advanced.md) before serving it
to another machine.

### `moneyflow mcp`

| Flag | Meaning |
| --- | --- |
| `--profile NAME_OR_ID` | Serve this profile |
| `--transport stdio` or `streamable-http` | Protocol transport; default `stdio` |
| `--allow-write` | Register the staged editing tools |
| `--unlock` | Unlock the YNAB vault through the controlling terminal. Does not imply `--allow-write` |
| `--listen HOST:PORT` | Loopback listener for HTTP; default `127.0.0.1:8081` |
| `--base-path PATH` | Exact HTTP endpoint path; default `/mcp/` |
| `--external-url URL` | Canonical URL through a trusted proxy |

`--listen`, `--base-path`, and `--external-url` require `--transport streamable-http`; with
`stdio` the command stops with `HTTP flags require --transport streamable-http`. The HTTP
listener must be a loopback address.

`moneyflow mcp token reveal` and `moneyflow mcp token rotate` each take `--profile NAME_OR_ID`.
Rotation takes effect on the running server's next request.

## Profile commands

| Command | Required flags |
| --- | --- |
| `moneyflow profile export` | `--profile NAME_OR_ID` (an existing profile), `--output FILE` (a new JSONL file in an existing directory) |
| `moneyflow profile import` | `--input FILE` (a Moneyflow profile JSONL file), `--name NAME` (the new profile's display name) |
| `moneyflow profile rename` | `--profile NAME_OR_ID` (an existing profile), `--name NAME` (the new display name) |

For example:

```bash
moneyflow profile rename --profile Monarch --name "Example Budget"
```

Export and rename refuse while another TUI, web, or MCP process holds the profile, and ask you
to close it. Export also refuses with active edits (`commit or undo active edits first`), an
unfinished provider-write batch (`finish or reconcile the provider write batch first`), or a
running provider operation. Rename keeps the profile's ID, data, and credentials.

## Provider commands

| Command | Flags |
| --- | --- |
| `provider connect monarch` | `--profile NAME_OR_ID`, `--currency CODE` (three-letter import currency), `--scale N` (decimal places, 0–9), `--mtd` (load month-to-date transactions, from the 1st of the current month) |
| `provider connect ynab` | `--profile NAME_OR_ID` |
| `provider connect simplefin` | `--profile NAME_OR_ID`, `--currency CODE`, `--scale N` (decimal places, 0–9) |
| `provider disconnect monarch` | `--profile NAME_OR_ID` |
| `provider disconnect ynab` | `--profile NAME_OR_ID` |
| `provider import amazon DIRECTORY` | `--profile NAME_OR_ID` (created when absent), `--currency CODE`, `--scale N` (decimal places, 0–9), `--clone-taxonomy-from NAME_OR_ID` (copy categories and groups from another profile when this one is created) |
| `provider import list` | None |
| `provider import institution MAPPING FILE_OR_DIRECTORY` | `--profile NAME_OR_ID` (required; an unused name creates a CSV profile), `--account LABEL` (card label, default `Chase Credit Card`; reuse it for repeat imports), `--force` (reparse unchanged files without overwriting local edits) |

`provider import list` prints each mapping's name and description, such as
`chase_credit  Chase credit-card exports (USD, 2 decimal places)`. Pass that name as `MAPPING`.

Without `--scale`, amounts use the currency's ISO 4217 decimal places, such as 2 for USD and 0
for JPY. `--scale` needs `--currency`.

When standard input is not a terminal, `provider connect simplefin` needs `--currency`. Without
it the command stops with `provide --currency with standard input`.

## Other commands

| Command | Flags |
| --- | --- |
| `moneyflow openapi` | `--format yaml` or `json`; default `yaml` |
| `moneyflow version` | None. Prints `moneyflow VERSION (commit COMMIT, built TIME)` |
| `moneyflow completion bash`, `fish`, `powershell`, `zsh` | Run `moneyflow completion SHELL --help` for install steps |

Every command exits with status 1 and prints the reason on standard error when it fails.

Python Moneyflow command names do not work with this binary.
