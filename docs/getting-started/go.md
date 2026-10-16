# Terminal interface

`moneyflow tui` is the keyboard-driven terminal app (TUI). It opens the same SQLite profiles,
pending edits, and review flow as the [browser](../guide/web.md) and the
[MCP server](../guide/mcp.md), all from one binary. Use it over SSH, in tmux, or wherever
you prefer a terminal. For a first run, follow the [quick start](quickstart.md).

## Start the TUI

```bash
moneyflow tui --demo
moneyflow tui
moneyflow tui --profile "Example Profile" --year 2026
```

| Flag | Effect |
| --- | --- |
| `--demo` | Opens a temporary synthetic profile; edits disappear when the process exits |
| `--profile NAME_OR_ID` | Opens one saved profile instead of the selector |
| `--year YYYY` | Shows transactions from January 1 of that year through today |
| `--since YYYY-MM-DD` | Shows transactions from that date through today |
| `--mtd` | Shows month-to-date transactions |
| `--theme NAME` | Chooses a [color theme](../config/themes.md) |

Bare `moneyflow` prints command help. Use `moneyflow tui --demo`, not Python's
`moneyflow --demo`. From a source checkout, run `./bin/moneyflow` so an installed Python
command cannot take its place. [Installation](installation.md) covers binaries and builds; this
repository no longer contains the Python application or Nix packaging.

## Work with the keyboard

- `g` cycles views, `1`–`5` jump to Merchant, Category, Group, Account, or Time, Enter drills
  in, and Esc goes back.
- `t` groups by Time and cycles year, month, and day. `p` opens the time chooser.
- `/` searches, `f` opens filters, and `i` shows transaction details.
- Space selects rows. `m` edits merchants, `c` changes categories, `h` toggles hidden state,
  and `x` stages a deletion where the provider allows it.
- `u` undoes and `U` redoes. `w`, then Enter, starts the reviewed commit.
- `?` lists the keys for the current view. `q` quits.

Pending edits survive restart; remote writes use resumable batches. The
[keyboard reference](../guide/keyboard-shortcuts.md), [navigation](../guide/navigation.md),
and [editing](../guide/editing.md) guides cover each workflow. The TUI does not draw the
browser's charts.

Provider restrictions are intentional. YNAB does not allow hide edits, transfer edits, or split
category edits. Monarch taxonomy administration stays in Monarch. Check the app's availability
message rather than assuming every provider supports every operation.

## Create a profile

Run `moneyflow tui` or `moneyflow web`, choose **Add profile**, select a source, and name the
profile. YNAB setup asks for the personal access token, a Moneyflow vault password, and a plan
choice when needed. Monarch setup supports saved credentials and generated TOTP verification
codes. Amazon imports CSV files and commits edits locally. [SimpleFIN](../guide/simplefin.md)
imports read-only bank data and keeps edits local; its Go support stays experimental until
live-bank validation.

**Unreleased:** When no profile has the name you pass to `--profile`, or several do, the
command stops and lists the profiles you can choose. Profiles that share a name are listed with
their IDs; pass one of those IDs to `--profile`.

## Rename a profile

**Unreleased:** A profile that a provider command or first run creates is named after its
provider, such as "Monarch" or "YNAB". When that name is taken, it becomes "Monarch 2",
"Monarch 3", and so on. To choose another name, close every TUI, web, and MCP process using
the profile, then run:

```bash
moneyflow profile rename --profile Monarch --name "Example Budget"
```

The profile keeps its ID, data, and credentials. The new name must not belong to another
profile, but it may change the capitalization of the current one. While another process holds
the profile, the command refuses and asks you to close that process.

## Where profiles live

Go's default catalog is `~/.moneyflow`. `MONEYFLOW_HOME` overrides that catalog root. Go
refuses a catalog folder that holds Python app data; [moving to Go](transition.md) explains how
to move that data aside. Opening Go does not convert Python profiles.

During the preview, incompatible Go schemas need explicit recovery, not an automatic migration.
Recovery backs up the old database and creates a pristine one; it does not transfer local
edits. Keep your original data until a [cutover has been verified](../architecture/cutover.md).

## Use MCP alongside the TUI

Point an MCP client at `moneyflow mcp --profile "Example Profile"` to let an assistant read
the same profile and stage edits for you to review. YNAB refresh and commits need the
unlocked HTTP server that you start in a terminal. The [MCP guide](../guide/mcp.md) covers
setup, tools, and recovery, and [agents](../guide/agents.md) covers the rules an assistant
must follow.

## Where to go next

- [MCP server](../guide/mcp.md): client setup, authorization, tools, and recovery.
- [Living architecture](../architecture/index.md): current behavior and ownership.
- [Python retirement checklist](../architecture/cutover.md): remaining release work, not
  shipped features.

The older Python guides stay labeled separately while Python still ships. You do not need them
to run the Go application.
