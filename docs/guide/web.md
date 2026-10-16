# Web interface

`moneyflow web` runs Moneyflow in your browser: charts of where your money went, an exact
transaction table, and the same keys as the [terminal app](../getting-started/go.md). It opens
the same profiles and pending edits as the terminal and the [MCP server](mcp.md). Most actions
also work with a mouse or touch.

## Start it

```bash
moneyflow web                              # choose a profile
moneyflow web --demo                       # temporary made-up data
moneyflow web --profile "Example Profile"  # open one saved profile
```

The command prints its address, such as `Moneyflow web: http://127.0.0.1:8080/`, and opens it.

- Saved profiles use `127.0.0.1:8080`, so bookmarks and saved browser settings keep working.
  If another program holds port 8080, Moneyflow says so and picks a free port; settings such as
  the theme start at their defaults there.
- `--demo` always picks a free port.
- With `--external-url`, Moneyflow keeps `127.0.0.1:8080` so your proxy has a fixed target,
  and fails if the port is busy.
- `--listen` sets the host and port and never falls back.

To try the demo without restarting, choose **Demo** (or press `d`) in the profile selector.
Demo data stays separate from your profiles and is deleted when the server stops.

## Common tasks

Text fields and dialogs take the keys while they have focus. Browser Back and Forward return to
earlier views but don't restore a selection.

| Task | Key | Mouse or touch |
| --- | --- | --- |
| Change the grouping | `g`, `1`–`5` | An **Explore** entry in the sidebar |
| Group by Time; cycle year, month, day | `t` | **Time** in the sidebar |
| Open a group | Enter | Double-click the row; on a phone, tap it |
| Go back | Esc | Click an earlier breadcrumb |
| Break a group down by another grouping | `g`, `t` | **Break down by** |
| Choose a period | `p`, Left / Right, `a` | The period control, ‹ ›, or **Day**, **Month**, **Year**, **All** |
| Sort | `s`, `v` | Click a column header; click again to reverse |
| Search or filter | `/`, `f` | The search field or the **Filters** menu |
| Select rows | Space, `Ctrl+A` | A row's checkbox; the header checkbox selects all |
| Edit the merchant or category | `m`, `c` | Double-click the cell, its pencil button, or the selection bar |
| Hide or delete | `h`, `x` | The selection bar; on a phone, the row's ⋯ menu |
| Show a transaction's details | `i`, or Enter on a transaction | Double-click a cell; on a phone, tap the row |
| Undo or redo | `u`, `U` | |
| Review and commit | `w` | **Review** in the sidebar |
| Refresh provider data | `r`, `R` | **Refresh** in the sidebar |
| Export | `E` | **Export** in the sidebar |
| Find possible duplicates | `D` | **Duplicates** in the sidebar |
| Edit categories or groups | `C`, `G` | **Edit categories**, **Edit groups** |
| List the keys | `?` | |

When an action can't run, Moneyflow says why. When something fails, an error banner gives the
server's reason.

More detail: [finding transactions](navigation.md), [editing](editing.md),
[review and commit](editing.md#review-and-commit), [export](export.md), and the
[keyboard reference](keyboard-shortcuts.md).

## Charts

Charts show how money moved in the period you chose. They use committed values, so pending
edits show up after you commit. MCP clients get the same numbers from
[`get_insights`](mcp.md#chart-insights). Chart keys are in the
[keyboard reference](keyboard-shortcuts.md#charts).

**Totals.** Income, Spending, and Net for the period, each with its change from the previous
period. In a drill-down you also get the average per transaction; at the top level, the savings
rate (Net divided by Income, shown when Income is above zero). Hidden transactions are counted
but add no money.

**Cash flow.** One column per period, with money in above zero, money out below, and a line for
Net. Point at a column to see its numbers. A partial period, such as the month in progress, is
lighter and labeled, for example `Oct 2026 (to date)` or `Jan 2026 (Jan 15–31)`.

- Click a column to choose that period.
- Drag across columns to choose a custom range.
- Shift-click a column, or press Shift+Enter on it, to extend the current period to it.

The columns cover the periods around your choice:

| You chose | Columns |
| --- | --- |
| A day | The days of its month |
| A month | The 13 months ending with it |
| A year | Each year from your first transaction through the chosen year, at most 10 |
| A custom range | Days up to 62 days, months up to 36 months, then years |
| All time | Months from your first to your newest transaction, or years past 60 months |

**Where it went.** The seven rows with the largest share of the period, then the rest. Switch
between **Spending** and **Income**. Click a bar to move to its row; double-click to open it.
In the Time grouping, this panel splits the selected period by category group instead.

**Daily spending.** In transaction views, a calendar shades each day by how much went out and
marks days with income. Click a day to choose it.

The **Charts** switch hides or shows the charts; the totals stay. On a phone, **Charts** opens
them in a drawer.

## Compare with the previous period

The **vs prior** column and the change on each total compare the period with the one just
before it:

| You chose | Compared with | Label |
| --- | --- | --- |
| A day | The day before | `vs Sep 14` |
| A month | The month before | `vs Aug` |
| The current month | The month before, from day 1 to today's day number | `vs Sep 1–7` |
| A year | The year before | `vs 2025` |
| The current year | The year before, through the same date | `vs 2025 to date` |
| A custom range | The same number of days just before it | `vs Feb 7 – Mar 9` |
| All time | Nothing; the comparison is hidden | — |

"Today" is the server's date. When the earlier month is shorter, the day number stops at its
last day, and February 29 becomes February 28. Transactions dated after today still count in
the current period, so totals match the table.

Changes read:

- `▲ 20.0%` or `▼ 5.3%`: the amount grew or shrank. Spending that grew and income that shrank
  are red; other changes are green.
- `new`: the earlier value was zero.
- `—`: both values are zero. A change that rounds to `0.0%` is gray, with no arrow.
- A signed difference such as `+460.00`: Net crossed from spending to income or back.

The **Trend** column shows the 12 months ending with your period, or with the current month
while your period is in progress.

## More than one currency

Each view shows one currency; Moneyflow never converts between them. When a profile has
several, pick one from the currency menu in the toolbar (on a phone, under **View**). Switching
currency leaves any drill-down or breakdown and clears the selection.

## On a phone

The browser works on a phone. Tap a row to open it, use its ⋯ menu to edit, and use **View** for
the period, currency, filters, and breakdown. The table never scrolls sideways; columns drop out
as the screen narrows. See [finding transactions](navigation.md#move-through-results).

## Themes

The browser starts in the dark theme. The theme button in the sidebar cycles **Light**,
**Dark**, and **System**, which follows your operating system.

## Serve it behind a private proxy

The web app has no login. Keep it on a trusted private network, or put access control in your
proxy. Never expose it to the public internet.

For a proxy serving `https://finance.example.com/moneyflow/`:

```bash
moneyflow web --listen 127.0.0.1:8080 --open=false \
  --base-path /moneyflow --external-url https://finance.example.com/moneyflow
```

The external URL's path must match `--base-path`. Keep the prefix when proxying; don't use a
rule that strips it. With Caddy:

```caddyfile
redir /moneyflow /moneyflow/ 308
handle /moneyflow/* {
    reverse_proxy 127.0.0.1:8080
}
```

When `--external-url` is set, edits are accepted only through that URL, not directly on the
listener.

## Refresh provider data

For Monarch, YNAB, and SimpleFIN profiles, press `r` or choose **Refresh** to download new
data. For Monarch, `r` reads the last 90 days and `R` (Shift+R) reads your complete history;
see [Monarch refresh](monarch.md#refresh-data). If a refresh is due when you open a profile, it
runs before you start working.

A dialog shows progress until the refresh ends:

- **Success:** the dialog closes and reports the result, such as
  `Refresh complete: 40 imported, 0 removed.`
- **Esc:** stops the refresh. Data saved so far stays saved. Automatic refresh then waits
  5 minutes; `r` still runs at once.
- **Failure:** the dialog gives the reason. Choose **Retry**, or **Continue with data from** a
  date to keep working with what you have.
- **Removals:** if the refresh would remove posted transactions, it asks first. Enter confirms;
  Esc keeps your current data.
- **Reconnect needed:** choose **Reconnect** to sign in again.

If the terminal app, an MCP client, or another tab is already refreshing, the dialog follows
that refresh. Only the interface that started it can stop it. The browser stops a refresh that
runs longer than 30 minutes, without changing data; `moneyflow tui` has no limit.

### Stale data banner

When your data missed a refresh, a banner above the table says how old it is and why, for
example `Showing data from 10 days ago (Oct 3, 2026, 9:15 AM). Refresh stopped.` It stays until
a refresh succeeds anywhere. After a failure, it shows when the next try is allowed, such as
`Retry available at 10:40 AM`. Use its **Refresh** or **Reconnect** button. SimpleFIN limits
how often you can refresh; see [SimpleFIN refresh timing](simplefin.md#refresh-timing).

## Reconnect and recovery

When a profile needs a reconnect, a vault unlock, or schema recovery, the page offers the step.
Closing the tab keeps your pending edits. If someone else changed the profile while you were
editing, Moneyflow refuses the stale change; check the new state and make it again. Provider
writes have their own status and recovery; see [editing](editing.md).

If profile setup fails, the form says why and stays open with the name and Monarch email you
entered. Re-enter passwords, tokens, and Access URLs.

## Limitations

- No currency conversion.
- Charts, totals, Trend, and vs prior use committed values.
- The In, Out, Share, Trend, and vs prior columns can't be sorted.
- No command palette; `?` lists the actions for the current view.
