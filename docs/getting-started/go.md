# Go application quick start

The Go application on `go-port` is the replacement for Moneyflow's Python application. It runs
the TUI, web UI, and MCP server from one binary, sharing the same SQLite profiles and pending
edits. Python removal and the Python package's proposed binary launcher are not released yet.
The current PyPI and Nix installation instructions still install Python.

## Build and try it

From a checkout of `go-port`, use the Go toolchain declared in `go.mod`, Bun, and Make:

```bash
make web-install
make build
./bin/moneyflow version
./bin/moneyflow tui --demo
```

`make build` builds and embeds the web application. The resulting binary does not need the Python
application or a separate frontend server at runtime. On Windows the binary is
`bin/moneyflow.exe`. Demo profiles are temporary; their edits disappear when the process exits.

Bare `moneyflow` prints command help. Use `moneyflow tui --demo`, not Python's `moneyflow --demo`.
Examples below use the local build explicitly so an installed Python command cannot take its place.

## Create a persistent profile

```bash
./bin/moneyflow tui
# Or use the browser selector and setup wizard:
./bin/moneyflow web
```

Choose **Add profile**, give it a name, and select Monarch, YNAB, or Amazon. YNAB setup asks for
the personal access token, a Moneyflow vault password, and a plan choice when needed. Monarch
setup supports saved credentials and generated TOTP verification codes. Amazon imports CSV files
and commits edits locally. SimpleFIN has no Go adapter yet.

After setup, select the profile directly by its name or ID:

```bash
./bin/moneyflow tui --profile "Example Profile" --year 2026
./bin/moneyflow web --profile "Example Profile" --open=false
```

Go's default catalog is `~/.moneyflow/v2`. `MONEYFLOW_HOME` overrides that catalog root.
This is separate from Python state: opening Go does not convert Python profiles. During the
preview, incompatible Go schemas require explicit recovery, not an automatic migration.
Recovery backs up the old database and creates a pristine one; it does not transfer local edits.
Keep your original data until a [cutover has been verified](../architecture/cutover.md).

## Use MCP as your daily interface

For offline analysis, point your MCP client's stdio configuration at the Go binary with
`mcp --profile "Example Profile"`. For YNAB refresh and commits, start the server in a terminal:

```bash
./bin/moneyflow mcp --profile "Example Profile" \
  --unlock --allow-write --transport streamable-http
```

Enter the **Moneyflow vault password**, not the YNAB personal access token. Connect the MCP client
to `http://127.0.0.1:8081/mcp/` with the profile's bearer token. The
[MCP guide](../guide/mcp.md#authenticated-http) covers token setup and private Caddy proxying.

`--unlock` activates the existing YNAB vault for this process. `--allow-write` separately enables
edit tools. Neither flag refreshes data or commits anything at startup. A GUI client without a
controlling terminal should connect to this already-unlocked HTTP process instead of trying to
supply a password through stdio or tool arguments.

A normal MCP session is:

1. Inspect profile/provider status and call `refresh_data` when you want fresh provider data.
   Poll `get_refresh_status` until that attempt finishes.
2. Search transactions and inspect categories. Use exact IDs returned by tools.
3. Preview a category/merchant edit, hide toggle, or deletion with `dry_run`, then stage it with
   the current revision.
4. Read `review_changes`. Commit only the revision you reviewed with `commit_changes`.
5. Poll `get_commit_status`; a background batch is not complete just because the tool returned.
   Follow any pause, reconnect, or reconciliation guidance before issuing more writes.

MCP targets the 2026-07-28 protocol. See the [MCP guide](../guide/mcp.md) for editing tools,
limits, and restart recovery. `preview_export` and `export_transactions` export the full committed
profile, or an explicitly filtered committed subset, to a file on the server; HTTP clients do not
receive a download. MCP also provides category/group management for local and Amazon profiles.
MCP does not run the TUI/web six-hour refresh scheduler.

## Keyboard workflows and provider limits

Use `g` to cycle views, Enter to drill, `/` to search, and `i` for transaction details. Select rows
with Space; `m` edits merchants, `c` changes categories, `h` toggles hidden state, and `x` stages
deletion where the provider allows it. `u` undoes and `U` redoes. `w`, then Enter starts the reviewed
commit. Pending edits survive restart; remote writes use resumable batches.

Provider restrictions are intentional: YNAB does not allow hide edits, transfer edits, or split
category edits; Monarch taxonomy administration remains in Monarch. Check the application's
availability message rather than assuming every provider supports every operation.

## Where to go next

- [MCP server](../guide/mcp.md): client setup, authorization, tools, and recovery.
- [Living architecture](../architecture/index.md): current behavior and ownership.
- [Python retirement checklist](../architecture/cutover.md): remaining release work, not shipped features.

The older Python guides remain labeled separately while Python still ships. They are not a
prerequisite for operating the Go application.
