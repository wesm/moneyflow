# Terminal interface

`moneyflow tui` runs Moneyflow in your terminal, driven entirely from the keyboard. It opens the
same profiles and pending edits as the [browser](../guide/web.md) and the
[MCP server](../guide/mcp.md). Use it locally, over SSH, or in tmux. For a first run, follow
the [quick start](quickstart.md).

## Start the TUI

```bash
moneyflow tui --demo
moneyflow tui
moneyflow tui --profile "Example Profile" --year 2026
```

| Flag | Effect |
| --- | --- |
| `--demo` | Opens a temporary profile of made-up data; edits disappear when you quit |
| `--profile NAME_OR_ID` | Opens one saved profile instead of the profile selector |
| `--year YYYY` | Shows transactions from January 1 of that year through today |
| `--since YYYY-MM-DD` | Shows transactions from that date through today |
| `--mtd` | Shows month-to-date transactions |
| `--theme NAME` | Chooses a [color theme](../config/themes.md) |

Running `moneyflow` with no subcommand prints help.

If `--profile` matches no profile, or several, the command stops and lists the profiles you can
choose. Profiles that share a name are listed with their IDs; pass an ID instead.

## Keys

| Key | Action |
| --- | --- |
| `g` | Cycle the grouping |
| `1`–`5` | Group by Merchant, Category, Group, Account, or Time |
| `t` | Group by Time; press again to switch between year, month, and day |
| `p` | Choose a period |
| Enter / Esc | Open the selected group / go back |
| `/` | Search |
| `f` | Filters |
| `i` | Transaction details |
| Space | Select a row |
| `m` / `c` | Edit the merchant / change the category |
| `h` | Hide or unhide |
| `x` | Delete, where the provider allows it |
| `u` / `U` | Undo / redo |
| `w`, then Enter | Review pending edits, then commit |
| `?` | Keys for the current view |
| `q` | Quit |

The [keyboard reference](../guide/keyboard-shortcuts.md) lists every key.
[Navigation](../guide/navigation.md) and [editing](../guide/editing.md) cover each workflow.
Charts are browser-only.

Some providers don't allow every edit. YNAB doesn't allow hide, transfer, or split-category
edits, and Monarch categories are managed in Monarch. When an action is unavailable, the TUI
says why.

## Create a profile

Run `moneyflow tui` (or `moneyflow web`), choose **Add profile**, and pick a source:

- [Monarch](../guide/monarch.md) supports saved credentials and generated two-factor codes.
- [YNAB](../guide/ynab.md) asks for a personal access token, a Moneyflow vault password, and a
  plan when you have several.
- [Amazon](../guide/amazon-mode.md) imports order-history CSV files. Edits stay local.
- [SimpleFIN](../guide/simplefin.md) (experimental) imports read-only bank data. Edits stay
  local.

[Chase bank CSV](../guide/bank-csv.md) profiles are created with an import command.

## Rename a profile

New profiles are named after their provider, such as "Monarch" or "YNAB". If that name is
taken, the next is "Monarch 2", "Monarch 3", and so on. To rename one, run:

```bash
moneyflow profile rename --profile Monarch --name "Example Budget"
```

The profile keeps its ID, data, and credentials. The new name must not belong to another
profile, but it may change the capitalization of the current one. Web, TUI, and MCP processes
that have the profile open keep working. While an import or export holds the profile, the command
fails as busy; run it again when that command finishes.

## Where profiles live

Profiles live in `~/.moneyflow`. Set `MONEYFLOW_HOME` to use another directory. Moneyflow
refuses a directory that holds Python Moneyflow data; [moving to Go](transition.md) explains
how to move it aside.

If a release can't open a profile, Moneyflow refuses it instead of migrating it. Recovery backs
up the old database and creates an empty one; it doesn't carry over local edits. See
[when a release can't open a profile](transition.md#when-a-release-cant-open-a-profile).

## Use an assistant alongside the TUI

Point an MCP client at `moneyflow mcp --profile "Example Profile"`. The assistant reads the same
profile and can stage edits for you to review in the TUI. YNAB refresh and commits need the
unlocked HTTP server, started from a terminal. See the [MCP guide](../guide/mcp.md) and
[Use Moneyflow with an agent](../guide/agents.md).
