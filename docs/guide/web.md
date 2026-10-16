# Web interface

`moneyflow web` serves the browser workspace: a sidebar, a command bar, charts above an exact
transaction table, and a status line. It opens the same profiles, pending edits, and review
flow as the [terminal app](../getting-started/go.md) and the [MCP server](mcp.md). The browser
and the TUI share profile data, and the server decides every view change for both. Keys match
the TUI, and most actions also have a pointer or touch control.

## Start the browser app

```bash
moneyflow web                              # profile selector
moneyflow web --demo                       # disposable synthetic profile
moneyflow web --profile "Example Profile"  # one saved profile
```

The command prints the address it serves, such as
`Moneyflow web: http://127.0.0.1:8080/`. A profile is served on `127.0.0.1:8080`, so bookmarks
and the browser's remembered settings keep working.

- If another server holds port 8080, Moneyflow says so and serves on a free `127.0.0.1` port.
  The browser remembers settings such as the theme per address, so that port starts with the
  defaults.
- `--demo` always picks a free port.
- With `--external-url`, Moneyflow keeps `127.0.0.1:8080` so the proxy has a fixed target, and
  fails if the port is busy.
- `--listen` sets the host and port in every case and never falls back.

The profile selector's **Demo** entry, or `d`, opens the same synthetic
household as `--demo` without restarting the server. Its data and edits stay separate from
saved profiles and last until the server stops, which deletes the demo.

## Work in the browser

The browser follows the [shared workflows](navigation.md). Text fields and overlays own their
keys while focused. Browser Back and Forward keep the analytical view, not the selection.

| Task | Key | Pointer or touch |
| --- | --- | --- |
| Change the grouping | `g`, `1`–`5` | A sidebar **Explore** entry |
| Group by Time, then cycle Year, Month, Day | `t` | **Time** in the sidebar |
| Open a group | Enter | Double-click the row; on a phone, tap it |
| Go back | Esc | Click an earlier breadcrumb |
| Break a group down | `g`, `t` | **Break down by** in the command bar |
| Choose a period | `p`, Left / Right, `a` | The period control's label, ‹ ›, and **Day**, **Month**, **Year**, **All** |
| Sort | `s`, `v` | Click a column header; click again to reverse |
| Search or filter | `/`, `f` | The command bar's search field and **Filters** menu |
| Select rows | Space, `Ctrl+A` | A row's box; the header box selects all |
| Edit merchant or category | `m`, `c` | A cell's pencil button, a double-click on the cell, or the selection bar |
| Hide or delete | `h`, `x` | The selection bar; on a phone, the row's ⋯ sheet |
| Open transaction information | `i`, or Enter on a transaction | Double-click another cell; on a phone, tap the row |
| Undo or redo | `u`, `U` | |
| Review and commit | `w` | **Review** in the sidebar's pending-edits card |
| Refresh provider data | `r`, `R` | **Refresh** in the sidebar |
| Export | `E` | **Export** in the sidebar |
| List the keys | `?` | |

[Navigate and search](navigation.md) covers the cursor, breadcrumb, period control, and Time
grouping. [Edit in the browser](editing.md#edit-in-the-browser) covers the editor anchored to the
cursor row and the selection bar, and [review and commit](editing.md#review-and-commit) covers
the review drawer. The [keyboard reference](keyboard-shortcuts.md) lists every key.

## What you see

The sidebar starts with the Moneyflow name and the profile's provider. Its
**Explore** entries choose the view, as their keys do: **Merchants** (`1`), **Categories**
(`2`), **Groups** (`3`), **Accounts** (`4`), **Time** (`5`), and **All transactions** (`d`).
Its **Maintain** entries open tools:

- **Duplicates** (`D`) finds [possible duplicates](editing.md#review-possible-duplicates).
- **Export** (`E`) opens [export](export.md).
- **Edit categories** (`C`) and **Edit groups** (`G`) open the
  [managers](editing.md#manage-categories-and-groups).
- Amazon profiles add **Import Amazon orders** (`r`); see [Amazon](amazon-mode.md).

An entry that cannot run now says why in the status line when you choose it. The bottom of
the sidebar holds:

- The pending-edits card, such as `3 pending edits · 27 transactions`. Its **Review** button
  (`w`) appears while edits are pending, and **Write** shows an unfinished provider write's
  progress. With nothing pending, **Last write** (`w`) reopens the status of
  the last finished write until the profile changes again, such as by an edit. See
  [review and commit](editing.md#review-and-commit).
- Provider status for Monarch, YNAB, and SimpleFIN profiles: when data was last refreshed, a
  refresh button (`r`), and **Reconnect** when the provider needs it. Below the button, the
  sidebar shows why refresh is unavailable, such as a provider write in progress or another
  process refreshing. The refresh dialog, and then the
  [stale data banner](#stale-data-banner), say why a refresh failed. See
  [Refresh provider data](#refresh-provider-data).
- The theme toggle; see [Themes](#themes).

The command bar holds the
[breadcrumb and Break down by](navigation.md#use-the-breadcrumb-and-break-down-by-in-the-browser),
the [period control](navigation.md#choose-a-period-in-the-browser), the currency chip when the
profile has more than one currency, the [Filters](filters.md) menu, the
[search](navigation.md#search) field, and the **Charts** switch.

The status line at the bottom shows:

- Up to six keys for the current view, such as `↵ Open` and `/ Search`.
- The connection: **Connected**, **Updating…** for requests slower than 300 ms, **Offline**
  with **Retry**, or **Not updated** with **Retry** after the server refused the last view
  request or the browser could not read its answer.
- The profile revision, except at phone width.
- The pending-edits text, when the sidebar card is not shown.

Three messages take the keys' place: `Loading…` while the profile's first view loads, a
running provider write's progress such as `Writing to Monarch: 3 of 6…`, and notices. A notice
reports a finished operation, such as `Refresh complete: 40 imported, 0 removed.` or
`Saved 6 changes to Monarch.` It also reports a change you did not ask for (a cleared
selection, a view that returned to its parent), an export with no data, or why a key did
nothing. A notice stays until a newer one replaces it or your next key, cursor move, or view
change clears it. A provider write that starts also clears it. Screen readers hear each notice.

When an action or a background check fails, an error banner below the command bar gives the
server's reason. Examples: a refused view change, a recheck when the window regains focus, an
undo or hide from a key, transaction information, the export preview, and a provider write
that ends early. Banners stay for 12 seconds, or until you close them. Screen readers hear them.

While a profile opens, the workspace appears at once with empty regions and `Loading…`, so
nothing moves when the data arrives. If the profile does not open, the page reads
`Couldn't load` with its name, such as `Couldn't load Household`, gives the reason, and offers
**Retry**. When Moneyflow cannot look up the name, it reads `Couldn't load this profile`. A missing profile, a profile
busy with recovery or another Moneyflow command, and a profile saved in a format this version
cannot open each have their own reason. Only an unreachable server suggests checking the
connection.

## Charts

The KPI strip above the table sums the current period, and the chart row shows how the money
moved. Both stay put while the table scrolls under its column header. In a window too short for
both, the charts scroll away when the cursor moves, leaving room for several rows. Charts read
committed values, so pending edits change them only after you commit. MCP clients can read the
same data with [`get_insights`](mcp.md#chart-insights). Chart keys are in the
[keyboard reference](keyboard-shortcuts.md#charts).

The **KPI strip** shows these tiles:

- **Income**, **Spending**, and **Net**, each with its change against the comparison range.
  The caption under the strip names that range, such as `vs Aug`.
- **Savings rate**: Net divided by Income, possibly negative. It shows at the top level,
  without a drill-down or search, when Income is above zero.
- **Avg per transaction**, inside a drill-down: Net divided by the visible transactions,
  rounded to the currency's smallest unit, or `—` when none are visible.
- **Transactions**: the count, with a line such as `2 hidden · 1 bank-pending` for hidden
  transactions and transactions the bank still lists as pending.

Income and Spending show `—` for zero, and Net shows `0.00`. Hidden transactions are counted
but add no money.

The **cash-flow chart** draws one column per period of the
[chart context](#ranges-and-comparisons). In rises above zero in green, Out falls below it,
and a line with dots traces Net. A band marks the periods in the current scope.

- Point at a column to see its In, Out, and Net.
- A partial period is drawn lighter, and its tooltip and screen-reader name say what it
  covers: `Oct 2026 (to date)` for the period in progress, `Jan 2026 (Jan 15–31)` or
  `May 2026 (May 1–10)` for a custom range's clipped end, and `Oct 2026 (Oct 10 – to date)`
  when that end holds today.
- In a grouped view, the cursor row's part of each column is solid, and the tooltip adds that
  row's In and Out. In the Time grouping, the cursor period's column is solid.
- Click a column to choose its period. Drag across columns to choose a custom range from the
  first to the last. Shift-click a column, or press Shift+Enter on it, to stretch the current
  period to it.

The panel beside the cash-flow chart depends on the view:

- **Where it went**, in grouped views, lists the seven rows with the largest share of the
  period, then `N others`. Its **Spending** and **Income** switch picks rows with negative or
  positive Net. Each bar shows the row's signed Net, such as `-360.00` (zero reads `0.00`), and
  its Share, such as `6.2%`. Click a bar to move the cursor to its row; double-click to drill
  in. The cursor row's bar is solid.
- **{period} by group**, in the Time grouping, such as `Sep 2, 2026 by group`, splits the
  cursor period's money across category groups, with the same switch. It follows the cursor
  without waiting for the server.
- **Daily spending**, in transaction views and for a single day, shades days darker for more
  Out and dots days with In. A day or month shows that month's calendar, and a year its weeks
  as columns. A custom range shows its months' calendars up to 62 days, otherwise the 53 weeks
  ending on its last day. All time shows the 53 weeks ending on your newest transaction. Days
  outside the period are faded, future days are blank, and the cursor transaction's day is
  outlined. Click a day to choose it.

The **Charts** switch shows or hides the chart row; the KPI strip stays, and the browser
remembers the choice. On a phone, the switch opens the strip and charts in a
**Visualizations** drawer instead of above the table.

While charts load, gray shapes hold their place. While a new view loads, the previous charts
stay dimmed and inert. A period with no transactions keeps the strip and charts above the
[empty-period message](navigation.md#choose-a-period-in-the-browser). An empty side of the panel
reads **No spending** or **No income**. If the charts cannot load, `Couldn't load charts` and
**Retry** replace them, and the table keeps working.

## Ranges and comparisons

Each view uses four date ranges:

| Range | What it covers | Used by |
| --- | --- | --- |
| Scope | The period you chose: a day, month, year, custom range, or all time | Table rows, KPI strip, Share, Where it went |
| Chart context | The cash-flow columns; it can reach past the scope | Cash-flow chart |
| Trend | Twelve months ending at the scope's last month | Trend column |
| Comparison | The earlier range the changes compare against | vs prior column, KPI changes |

"Today" is the server's date. A period is in progress when it contains today and today is not
its last day.

The **chart context** depends on the scope:

- A day: the days of its month.
- A month: thirteen months ending with it.
- A year: one column per year, from the year of your first transaction in the currency
  through the chosen year, at most ten years.
- A custom range: days up to 62 days, months up to 36 months, and years beyond.
- All time: months from your first to your newest transaction in the currency, or years when
  that is more than 60 months.

Periods without transactions are empty columns, except that the Time grouping by day leaves
those days out, as the table does. In the Time grouping, the columns are the table's rows; see
[Navigate and search](navigation.md#choose-a-year-month-or-day) for their unit.

The **trend** ends at the current month while the scope is in progress, and at your newest
transaction's month for all time. It leaves out months before your first transaction in the
currency. A month in progress ends the line with a lighter dot, and the cell's tooltip and
screen-reader name call it, for example, `Oct 2026 (to date)`.

The **comparison** is the range just before the scope:

| Scope | Comparison | Label |
| --- | --- | --- |
| Day | The day before | `vs Sep 14` |
| Month | The month before | `vs Aug` |
| Month in progress | The month before, from day 1 to today's day number | `vs Sep 1–7` |
| Year | The year before | `vs 2025` |
| Year in progress | The year before, from January 1 to the same date as today | `vs 2025 to date` |
| Custom range | The same number of days, ending the day before the range starts | `vs Feb 7 – Mar 9` |
| All time | None; the vs prior column and KPI changes are hidden | — |

The day number is clamped to the earlier month's last day, and February 29 becomes
February 28.

In a period in progress, such as 2026 to date, transactions dated after today still count in
the current amounts and KPIs, so they match the table. The comparison still stops at today's
date in the earlier period, such as the same day last year.

The **vs prior** column compares each row's Net with the comparison range. The KPI strip
compares Income, the size of Spending, and Net the same way. Changes show:

- `—` in gray when both values are zero.
- `new` when the earlier value is zero. It is green for income and red for spending.
- A percent change in size, such as `▲ 20.0%` or `▼ 5.3%`. ▲ means the amount grew and ▼
  means it shrank. Spending that grew and income that shrank are red; other changes are
  green. A Net that falls to zero shows `▼ 100.0%`.
- A change that rounds to `0.0%` in gray, with no arrow.
- The signed difference, such as `+460.00`, when Net changed between spending and income. It
  is green when positive and red when negative.

For example, a merchant with Net `-300.00` in August and `-360.00` in September shows a red
`▲ 20.0%` for September.

## Currencies

The browser shows one currency at a time and never converts between
currencies. When a profile has more than one, the command bar's currency chip lists each
currency with its transaction count. On a phone, the chip is in the **View** sheet.

Choosing another currency leaves any drill-down and breakdown, because they belong to the
previous currency. It also clears the selection. Esc does not undo a currency change.

## Narrow screens

The workspace adapts to the window width:

| Width | Layout |
| --- | --- |
| 1440 px and wider | The full sidebar always shows |
| 901 to 1439 px | An icon rail, unless you expand it; the status line shows the pending-edits text while the rail shows |
| 900 px and narrower | A top bar with the Moneyflow name and a menu button that opens the sidebar as a drawer; the status line shows the pending-edits text |
| Below 900 px | The panel moves under the cash-flow chart |
| 640 px and narrower | The phone layout |

In the icon rail, point at or focus an icon to see its name. **Expand sidebar** at the bottom
shows the full sidebar and **Collapse sidebar** returns to the rail; the browser remembers the
choice, and touch screens start expanded until you choose. The **Refresh** icon's tooltip shows
when data was last refreshed, then why refresh is unavailable or the last refresh's warning.
Pressing it while refresh is unavailable shows why in the status line.

In the phone layout:

- The command bar takes two rows. **View** opens a sheet with the period choices, currency,
  filters, and Break down by.
- Rows take two lines. Tap a row's box to select it, a group to open it, or a transaction to
  open its information. A transaction's ⋯ button opens **Merchant**, **Category**, **Hide**,
  **Delete**, and **Info**; see [choose the transactions](editing.md#choose-the-transactions).
- The selection bar spans the bottom of the screen, and the editor opens as a bottom sheet.
- **Charts** opens the KPI strip and charts in a **Visualizations** drawer.
- The status line shows the pending-edits text instead of keys, without the profile revision.

The table never scrolls sideways. [Navigate and search](navigation.md#move-through-results)
explains the phone rows and the order in which columns leave a narrow table.

## Themes

The browser opens in the dark Graphite theme. The theme toggle in the sidebar
cycles **Light**, **Dark**, and **System**, which follows your operating system. The browser
remembers the choice.

## Private reverse proxy

For a private proxy serving `https://finance.example.com/moneyflow/`:

```bash
moneyflow web --listen 127.0.0.1:8080 --open=false \
  --base-path /moneyflow --external-url https://finance.example.com/moneyflow
```

Preserve the prefix when proxying to the listener; do not use a proxy rule that strips it.
For example, the Caddy site routes can include:

```caddyfile
redir /moneyflow /moneyflow/ 308
handle /moneyflow/* {
    reverse_proxy 127.0.0.1:8080
}
```

The external URL's path must match the base path. Canonical-origin mutation checks mean the
direct listener is not an alternate write origin when an external URL is configured.

The financial web app has no built-in user authentication. Keep it on a trusted private
network, or put access control in the proxy. A mutation token does not make publicly readable
financial data private. Do not expose the app to the public internet merely because the
documentation site is public.

## Refresh provider data

Every refresh of a Monarch, YNAB, or SimpleFIN profile opens the refresh dialog:
`r`, `R`, **Refresh**, or a refresh due in the background. For Monarch, `r` and **Refresh**
read the last 90 days unless complete history is due, and `R` (Shift+R) reads complete history;
see [Monarch refresh](monarch.md#refresh-data). Pointing at the sidebar's refresh button shows
`Complete history · R`. If a refresh is due when you open a profile, the dialog opens before
you work with older data. While it is open, everything behind it is inert and Esc goes to it.

The dialog shows:

- The provider, such as **Refreshing from Monarch**. For Monarch it also names the kind:
  **Recent data (last 90 days)** or **Complete history**.
- Progress, such as `Fetched 7,000 transactions`. Monarch reads visible, then hidden
  transactions into the same count, then reads everything again to confirm nothing changed:
  `Verifying 12,000 of 31,808 transactions`. The bar covers both reads, so the first fills
  half of it. Monarch reports the hidden total only with their first page, so the bar can
  pause there; it never moves backward. Until the provider reports a total, the bar moves
  without filling.
- The elapsed time, and when data was last updated, such as
  `Last updated 10 days ago (Oct 3, 2026, 9:15 AM)`.

Screen readers hear the progress at most every 10 seconds.

What happens next:

- **Success:** the dialog closes and a notice reports the result, such as
  `Refresh complete: 40 imported, 0 removed.` A warning, such as an unrecorded full refresh,
  follows in the same notice and beside **Refresh** in the sidebar, or in the rail icon's
  tooltip.
- **Cancel (Esc):** the dialog shows **Stopping…** until the server confirms the stop, then
  closes. The view loads the latest saved data and reports `Refresh stopped.` Data saved before
  the cancel stays saved. Automatic refresh then waits 5 minutes; **Refresh** or `r` still runs
  at once.
- **Failure:** the dialog stays open with the reason. **Retry** tries again at once, with
  complete history if you asked for it. **Continue with data from** a date closes the dialog
  and keeps the older data.
- **Removals to review:** the dialog shows how many posted transactions the refresh would
  remove. **Confirm refresh** (Enter) applies the refresh; **Cancel** (Esc) keeps the current
  data.
- **Reconnect needed:** the dialog explains that the provider needs you to sign in again.
  **Reconnect** opens the reconnect form.

When the TUI, an MCP client, or another tab is refreshing the profile, the dialog says so, for
example `The terminal app (moneyflow tui) is refreshing this profile. Cancel it there.` It
follows that refresh and closes when it ends. It has no **Cancel** button: only the interface
that started a refresh can stop it.

If Moneyflow cannot check the refresh status while the dialog shows **Stopping…** or another
interface's refresh, the dialog shows the error. **Continue with data from** a date closes it,
and the stale data banner offers **Retry**.

The web app stops a refresh that runs longer than 30 minutes, with no data changed;
`moneyflow tui` has no such limit. When the server cannot say why a request failed, the dialog
says so and the terminal running `moneyflow web` logs the cause. A request the browser
abandons (you canceled, reloaded, or closed the tab) is not a server failure: the server
answers it with status 499 and code `request_canceled` and does not log it.

### Stale data banner

When the data you see missed a refresh, a banner above the table says so, for
example `Showing data from 10 days ago (Oct 3, 2026, 9:15 AM). Refresh stopped.` It stays until
a refresh succeeds, in this tab or in another Moneyflow interface. The banner appears when no
refresh is running and either:

- The last refresh did not finish: it failed, was canceled, was lost when the page reloaded,
  needs you to reconnect, or would remove transactions that you did not confirm.
- A refresh is due but cannot run, for example while a YNAB profile's credentials are locked.

After a failure, the banner shows when the retry delay ends, such as
`Retry available at 10:40 AM`. Automatic refresh waits at least until then, and longer when no
refresh is due yet. The banner's **Refresh** button, `r`, and **Retry** in the dialog try at
once, unless the provider limits manual attempts, as [SimpleFIN](simplefin.md#refresh-timing)
does. For SimpleFIN, **Refresh** waits and shows when it becomes available, such as
`Available at 10:40 AM`.

- When the provider needs you to reconnect, the banner offers **Reconnect** instead.
- If Moneyflow cannot check for newer provider data, the banner says so, keeps the last known
  data age, and offers **Retry**.
- When a refresh in this tab or elsewhere saved newer data than the view shows, the banner says
  `Newer provider data is loading.` until the view loads it.
- If loading fails, the banner says `Refreshed provider data could not be loaded.` with the
  reason. Its **Retry** loads the view again, each status check also retries, and the banner
  stays until the view shows the newer data.

See [Monarch refresh](monarch.md#refresh-data) for when background refresh runs.

## Reconnect and recovery

A profile may need a reconnect, a vault unlock, or explicit schema recovery. Use the workflow
the page shows. Closing a tab does not discard durable pending work. If a change meets a stale
revision, inspect the new state and make the change again deliberately. Provider write batches
have their own status and recovery controls; see [editing](editing.md).

When creating or recovering a profile, or a setup step, fails, the form says why
and stays open. Moneyflow keeps the profile name and the Monarch email you entered. It clears
passwords, secrets, tokens, and Access URLs, so enter those again.

## Limitations

These limits apply to the browser workspace:

- No currency conversion. Each view shows one currency.
- Charts, the KPI strip, Trend, vs prior, and group totals use committed values until you
  commit. See [editing](editing.md).
- The In, Out, Share, Trend, and vs prior columns cannot be sorted.
- There is no command palette. `?` lists the actions and keys of the current view.
- The TUI does not draw charts.
