# Use Moneyflow with an agent

An agent can read these docs as Markdown, work with a profile through the MCP server, and run
the commands that need no terminal. It must leave commits, credentials, and live provider
access to the person who owns the data. This page covers each of those boundaries and how to
experiment with synthetic data.

## Read these docs

- `https://moneyflow.dev/llms.txt` lists every page in
  the [llmstxt.org](https://llmstxt.org/) format.
- Every page has a Markdown twin: replace the trailing `/` of its URL with `.md`. This page is
  `https://moneyflow.dev/docs/guide/agents.md`; the overview is
  `https://moneyflow.dev/docs/index.md`.
- These pages describe the Go application on `main`. Command help belongs to the installed
  binary, so check `moneyflow <command> --help` when the two disagree. See
  [installation](../getting-started/installation.md) for release status.

## Connect through MCP

Point the MCP client at the binary and one profile. Use the profile's exact display name or
its ID:

```json
{
  "mcpServers": {
    "moneyflow": {
      "command": "/path/to/moneyflow",
      "args": ["mcp", "--profile", "PROFILE_NAME_OR_ID"]
    }
  }
}
```

One server serves one existing profile. It never creates a profile, runs provider onboarding,
or imports Amazon files; a person does that in the browser, the TUI, or the
[command line](#run-commands-without-a-terminal).

The server starts read-only: 17 tools and five `moneyflow://` resources. Add `--allow-write`
to the `args` to register the editing and commit tools, 33 tools in all. Each step of an edit
is a separate call:

| Step | Tools | What changes |
| --- | --- | --- |
| Read | `search_transactions`, `get_transactions`, `get_insights`, `get_categories`, `review_changes`, and the other read tools | Nothing |
| Preview | An editing tool with `dry_run: true` | Nothing; returns a bounded preview |
| Stage | The same tool without `dry_run`, plus `undo_changes` and `redo_changes` | The pending journal, revision-checked |
| Commit | `commit_changes` with the reviewed revision | Local data; for Monarch and YNAB, a provider write batch |

Two tools change the profile without `--allow-write`. `refresh_data` imports provider data,
and `confirm_refresh_deletions` removes local transactions the provider no longer lists.
`export_transactions` writes a file in the profile's `exports/` directory on the server.

The [MCP guide](mcp.md) owns the details: the [read and write policy](mcp.md#read-and-write-policy),
[chart insights](mcp.md#chart-insights), [server-side export](mcp.md#server-side-export),
[refresh](mcp.md#explicit-provider-refresh), and [operational limits](mcp.md#operational-limits).
A YNAB profile opens offline from saved data. To refresh or commit it, a person starts the
[authenticated HTTP](mcp.md#authenticated-http) server with `--unlock`, enters the vault
password at the terminal, and gives the client the endpoint and bearer token.

## Run commands without a terminal

These commands take no prompt, so an agent can run them from a script. Each prints errors on
standard error and exits non-zero on failure.

| Command | What it does |
| --- | --- |
| `moneyflow --help`, `moneyflow <command> --help` | Lists commands and flags |
| `moneyflow version` | Prints the version, commit, and build date |
| `moneyflow openapi --format json` | Prints the HTTP API contract; the default format is `yaml` |
| `moneyflow provider import list` | Lists bank CSV mappings, such as `chase_credit` |
| `moneyflow provider import institution chase_credit PATH --profile NAME` | Imports Chase CSV files into a CSV profile; an unused name creates one. See [bank CSV](bank-csv.md) |
| `moneyflow provider import amazon DIRECTORY --profile NAME --currency USD --scale 2` | Imports Amazon order-history files. See [Amazon](amazon-mode.md) |
| `moneyflow profile export --profile NAME_OR_ID --output FILE` | Writes the whole profile to a new JSONL file |
| `moneyflow profile import --input FILE --name NAME` | Creates a new profile from a JSONL file |
| `moneyflow profile rename --profile NAME_OR_ID --name NAME` | [Renames a profile](../getting-started/go.md#rename-a-profile) in place |
| `moneyflow mcp --profile NAME_OR_ID` | Serves MCP over standard input and output |
| `moneyflow web --demo --open=false` | Serves the synthetic demo and prints its address |

A new Amazon profile asks for its currency and scale unless you pass both `--currency` and
`--scale`. Passing only one of them is an error. An existing profile keeps its saved values.

No command only lists profiles. Pass a name that does not exist and the error lists them, for
example `no profile named "Unknown". Profiles: Example CSV, Example Copy`.

JSONL transfer is the only way to move a profile between catalogs or versions. The
[transfer guide](../getting-started/transition.md#transfer-a-go-profile-with-jsonl) owns its
preconditions, limits, and what it leaves out.

These need a person:

- `moneyflow tui` needs an interactive terminal.
- `moneyflow provider connect` asks for credentials at a masked prompt.
- `moneyflow mcp --unlock` asks for the YNAB vault password on the controlling terminal.
- `moneyflow mcp token reveal` and `moneyflow mcp token rotate` print a bearer token.

## Rules for agents

- **Commit only what the person reviewed.** Preview with `dry_run`, stage, show the
  `review_changes` result, and call `commit_changes` with that revision only after the person
  approves it. A Monarch or YNAB commit writes to the provider.
- **On `revision_conflict`, read again.** Never resend an edit or a hide toggle blindly.
- **Never handle credentials.** Do not run `provider connect`, pass passwords or tokens in
  arguments or tool calls, or copy a bearer token or token file into a transcript.
- **Never touch live providers on your own.** Do not call `refresh_data`, `commit_changes`,
  or the write-recovery tools against a Monarch, YNAB, or SimpleFIN profile unless the person
  asks. Experiment on synthetic data.
- **Never use the Python profile directory.** Do not point `MONEYFLOW_HOME` at
  `~/.moneyflow-python` or at a folder that holds Python app data, and never move, edit, or
  delete files in a profile directory. Go refuses a Python folder; see
  [moving to Go](../getting-started/transition.md#move-python-data-out-of-moneyflow).
- **Treat exports as private.** Profile JSONL and transaction exports hold unencrypted
  financial data. Never attach them to issues, commits, or chat.

## Experiment with synthetic data

The demo is a synthetic household of about 2,000 transactions. Its edits disappear when the
process exits.

```bash
moneyflow web --demo --open=false   # prints "Moneyflow web: http://127.0.0.1:PORT/p/PROFILE_ID/"
moneyflow tui --demo
```

`--demo` always serves on a free port. Stop the server when you are done.

MCP has no demo flag. For MCP experiments, make a throwaway catalog and import a synthetic CSV.
Moneyflow rejects profile paths under group- or world-writable folders such as `/tmp`, so
create it in your home directory with a private umask:

```bash
umask 077
export MONEYFLOW_HOME="$(mktemp -d "$HOME/moneyflow-agent.XXXXXX")"
mkdir "$MONEYFLOW_HOME/csv"
printf 'Transaction Date,Description,Amount,Category,Memo\n09/01/2026,EXAMPLE MERCHANT,-12.34,Groceries,\n' \
  > "$MONEYFLOW_HOME/csv/Chase_example.csv"
moneyflow provider import institution chase_credit "$MONEYFLOW_HOME/csv" --profile "Example CSV"
moneyflow mcp --profile "Example CSV" --allow-write
```

A CSV profile commits locally and never writes to a bank, so you can stage and commit freely.
Delete the temporary directory afterwards.

## Change Moneyflow itself

A coding agent working in the repository follows
[AGENTS.md](https://github.com/wesm/moneyflow/blob/main/AGENTS.md) and the
[development guide](../development/developing.md). Those cover the toolchain, the private
temporary directory that Unix tests and demos need, and the checks to run before each commit.
A Go program can also run Moneyflow in its own process through the root package
`github.com/wesm/moneyflow`; `go doc github.com/wesm/moneyflow` describes it.
