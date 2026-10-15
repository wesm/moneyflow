# Web interface

Run `./bin/moneyflow web` to open the profile selector. Use `--demo` for a disposable synthetic
profile, or `--profile "Example Profile"` to select one explicitly. The browser and TUI share
profile data and server-authoritative transitions.

**Unreleased:** The command prints the address it serves, such as
`Moneyflow web: http://127.0.0.1:8080/`. A profile is served on `127.0.0.1:8080`, so bookmarks
and the browser's remembered settings keep working. If another server already holds port 8080,
Moneyflow says so and serves on a free `127.0.0.1` port instead. The browser remembers settings
such as the theme per address, so that port starts with the defaults. `--demo` always picks a
free port. With `--external-url`, Moneyflow keeps `127.0.0.1:8080` so the proxy has a fixed
target, and fails if the port is busy. `--listen` sets the host and port in every case and never
falls back.

**Unreleased:** The profile selector's **Demo** entry, or `d`, opens the same synthetic household
as `--demo` without restarting the server. Its data and edits stay separate from saved profiles.
Edits last until the server stops, which deletes the demo.

Keyboard-driven refinement remains central: grouping, drill/back, search, selection, edits and
review use the [shared workflows](navigation.md). Text fields and overlays own their keys while
focused. Browser navigation retains analytical view state, not transient selection. The
[keyboard reference](keyboard-shortcuts.md) lists every key, and `?` lists the keys available in
the current view. To change transactions, see [Edit in the browser](editing.md#edit-in-the-browser).

## What you see

**Unreleased:** The browser workspace has a sidebar, a command bar, charts above the table, and
a status line.

The sidebar starts with the Moneyflow name and then the profile's provider. Its **Explore**
entries choose the view, as their keys do:

| Entry | Key |
| --- | --- |
| Merchants | `1` |
| Categories | `2` |
| Groups | `3` |
| Accounts | `4` |
| Time | `5` |
| All transactions | `d` |

Its **Maintain** entries open tools:

- **Duplicates** (`D`) finds [possible duplicates](editing.md#review-possible-duplicates).
- **Export** (`E`) opens [export](export.md).
- **Edit categories** (`C`) and **Edit groups** (`G`) open the
  [managers](editing.md#manage-categories-and-groups).
- Amazon profiles add **Import Amazon orders** (`r`); see [Amazon](amazon-mode.md).

An entry that cannot run now explains why in the status line when you choose it.

The bottom of the sidebar holds:

- The pending-edits card, such as `3 pending edits · 27 transactions`. Its **Review** button
  (`w`) appears while edits are pending, and **Write** shows an unfinished provider write's
  progress. **Unreleased:** With nothing pending, **Last write** (`w`) reopens the status of the
  last finished write until the profile changes again, such as by an edit. See
  [review and commit](editing.md#review-and-commit).
- Provider status for Monarch, YNAB, and SimpleFIN profiles: when data was last refreshed, a
  refresh button (`r`), and **Reconnect** when the provider needs it. **Unreleased:** Below the
  button, the sidebar shows why refresh is unavailable, such as a provider write in progress or
  another process refreshing. In the icon rail, the **Refresh** icon's tooltip shows when data
  was last refreshed, then why refresh is unavailable or a warning from the last refresh.
  Pressing the icon while refresh is unavailable shows why in the status line. The refresh
  dialog, and then the [stale data banner](#stale-data-banner), say why a refresh failed. See
  [Refresh provider data](#refresh-provider-data).
- The theme toggle; see [Themes](#themes).

**Unreleased:** From 901 to 1439 px wide, the sidebar shows as an icon rail; point at an entry's
icon or move focus to it to see its name. **Expand sidebar** at the bottom of the rail shows the
full sidebar, and **Collapse sidebar** returns to the rail. The browser remembers the choice.
Until you choose, touch screens start with the full sidebar. At 1440 px and wider the full
sidebar always shows; see [Narrow screens](#narrow-screens).

The command bar shows where you are and what the view includes. It holds the
[breadcrumb and Break down by](navigation.md#use-the-breadcrumb-and-break-down-by-in-the-browser),
the [period control](navigation.md#choose-a-period-in-the-browser), the currency chip when the
profile has more than one currency, the [Filters](filters.md) menu, the
[search](navigation.md#search) field, and the **Charts** switch.

The status line at the bottom shows:

- Up to six keys for the current view, such as `↵ Open` and `/ Search`.
- The connection: **Connected**, **Updating…** for requests slower than 300 ms, **Offline**
  with **Retry**, or **Unreleased:** **Not updated** with **Retry** after the server refused the
  last view request or the browser could not read its answer.
- The profile revision, except at phone width.
- The pending-edits text, when the sidebar card is not shown.

**Unreleased:** These messages take the keys' place in the status line:

- `Loading…` while the profile's first view loads.
- A running provider write's progress, such as `Writing to Monarch: 3 of 6…`.
- A notice. It reports a finished operation, such as `Refresh complete: 40 imported, 0 removed.`
  or `Saved 6 changes to Monarch.`, a change you did not ask for, such as a cleared selection or
  a view that returned to its parent, an export with no data, or why a key did nothing.

A notice stays until a newer notice replaces it or your next action clears it: a key, a cursor
move, or a view change. A provider write that starts also clears it, so the write's progress
shows. Screen readers hear each notice.

**Unreleased:** When an action or a background check fails, an error banner below the command
bar says what failed and gives the server's reason. Examples are a view change the server
refuses, a recheck when the window regains focus, an undo or a hide from a key, transaction
information, the export preview, and a provider write that ends before it finishes. Error
banners stay for 12 seconds; the close button removes one sooner. Screen readers hear the same
messages.

**Unreleased:** While a profile opens, the workspace appears at once with its regions empty and
`Loading…` in the status line, so nothing moves when the data arrives. If the profile does not
open, the page reads `Couldn't load` with the profile's name, such as `Couldn't load Household`,
gives the reason, and offers **Retry**. When Moneyflow cannot look up the name, the page reads
`Couldn't load this profile`. A missing profile, a profile busy with recovery or another
Moneyflow command, and a profile saved in a format this version cannot open each have their own
reason. The reason suggests checking the
connection only when the browser could not reach the server.

## Charts

**Unreleased:** Above the table, the KPI strip sums the current period, and the chart row
shows how the money moved. They stay in place while the table scrolls below them under its
column header. A window too short for both keeps room for several table rows, and the charts
scroll up out of the way when the cursor moves. All charts read committed values; pending
edits change them only after you commit. MCP clients can read the same data with
[`get_insights`](mcp.md#chart-insights). Chart keys are in the
[keyboard reference](keyboard-shortcuts.md#charts).

The **KPI strip** shows these tiles:

- **Income**, **Spending**, and **Net**, each with its change against the comparison range.
  The caption under the strip names that range, such as `vs Aug`.
- **Savings rate**, which is Net divided by Income. It shows at the top level, without a
  drill-down or search, when Income is above zero. It can be negative.
- **Avg per transaction** in place of the savings rate inside a drill-down. It is Net divided by
  the visible transactions, rounded to the currency's smallest unit, or `—` when none are
  visible.
- **Transactions**, the count. A line under it counts hidden transactions and transactions the
  bank still lists as pending, such as `2 hidden · 1 bank-pending`.

Income and Spending show `—` for zero. Net shows `0.00` for zero. Hidden transactions are
counted but add no money.

The **cash-flow chart** draws one column per period of the
[chart context](#ranges-and-comparisons). In rises above zero in green, Out falls below it, and
a line with dots traces Net. A band marks the periods in the current scope.

- Point at a column to see its In, Out, and Net.
- In a grouped view, the cursor row's part of each column is solid, and the tooltip adds that
  row's In and Out. In the Time grouping, the cursor period's column is solid.
- Click a column to choose its period.
- Drag across columns to choose a custom range from the first to the last.
- Shift-click a column, or press Shift+Enter on it, to stretch the current period to it.

The panel beside the cash-flow chart depends on the view:

- **Where it went** in grouped views. It lists the seven rows with the largest share of the
  period, then `N others` for the rest. The **Spending** and **Income** switch chooses rows with
  negative or positive Net. Each bar shows the row's signed Net, such as `-360.00`, and its
  Share, such as `6.2%`. A zero Net reads `0.00`. Click a bar to move the table cursor to its row,
  or double-click it to drill into the row. The cursor row's bar is solid.
- **{period} by group** in the Time grouping, such as `Sep 2, 2026 by group`. It shows how the
  cursor period's money splits across category groups, with the same switch. It follows the
  cursor without waiting for the server.
- **Daily spending** in transaction views and for a single day. Darker days have more Out, and a
  dot marks days with In. A day or month shows that month's calendar. A year shows that year's
  weeks as columns. A custom range shows its months' calendars up to 62 days, otherwise the 53
  weeks ending on its last day. All time shows the 53 weeks ending on your newest transaction.
  Days outside the period are faded, and future days are blank. The cursor transaction's day is
  outlined. Click a day to choose it.

The **Charts** switch shows or hides the chart row. The KPI strip stays, and the browser
remembers the choice. On a phone, the KPI strip and charts are not shown above the table; the
switch opens them in a **Visualizations** drawer instead.

The charts show these states:

- While charts load, gray shapes hold their place, so nothing moves when they arrive.
- While a new view loads, the previous charts stay dimmed and do not respond.
- A period with no transactions keeps the strip and charts above the
  [empty-period message](navigation.md#choose-a-period-in-the-browser).
- An empty side of the panel reads **No spending** or **No income**.
- If the charts cannot load, `Couldn't load charts` and **Retry** take their place. The table
  keeps working.

## Ranges and comparisons

**Unreleased:** Each view uses four date ranges:

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
- Periods without transactions are drawn as empty columns.

In the Time grouping, the columns are the table's rows; see
[Navigate and search](navigation.md#choose-a-year-month-or-day) for their unit.

The **trend** ends at the current month while the scope is in progress, and at your newest
transaction's month for all time. A month in progress ends the line with the same solid dot
as a full month. Months before your first transaction in the currency are left out.

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

**Unreleased:** In a period in progress, such as 2026 to date, transactions dated after today
still count in the current amounts and KPIs, so they match the table. The comparison still stops
at today's date in the earlier period, such as the same day last year.

The **vs prior** column compares each row's Net with its Net in the comparison range. The KPI
strip compares Income, the size of Spending, and Net the same way. Changes show:

- `—` in gray when both values are zero.
- `new` when the earlier value is zero. It is green for income and red for spending.
- A percent change in size, such as `▲ 20.0%` or `▼ 5.3%`. ▲ means the amount grew and ▼ means
  it shrank. Spending that grew and income that shrank are red; other changes are green. A Net
  that falls to zero shows `▼ 100.0%`.
- A change that rounds to `0.0%` in gray, with no arrow.
- The signed difference, such as `+460.00`, when Net changed between spending and income. It is
  green when positive and red when negative.

For example, a merchant with Net `-300.00` in August and `-360.00` in September shows a red
`▲ 20.0%` for September.

## Currencies

**Unreleased:** The browser shows one currency at a time and never converts between currencies.
When a profile has more than one, the command bar's currency chip lists each currency with its
transaction count. On a phone, the chip is in the **View** sheet.

Choosing another currency leaves any drill-down and breakdown, because they belong to the
previous currency. It also clears the selection. Esc does not undo a currency change.

## Narrow screens

**Unreleased:** The workspace adapts to the window width:

- **1440 px and wider:** the full sidebar.
- **901 to 1439 px:** an icon rail, unless you expand it to the full sidebar. Touch screens start
  with the full sidebar. While the rail shows, the status line shows the pending-edits text.
- **900 px and narrower:** a top bar with the Moneyflow name and a menu button, which opens the
  sidebar as a drawer. The status line shows the pending-edits text.
- **Below 900 px:** the panel moves under the cash-flow chart.
- **640 px and narrower:** the phone layout. The command bar takes two rows, and the charts open
  from the **Charts** switch. The status line shows the pending-edits text instead of keys, and
  leaves out the profile revision.

The table never scrolls sideways. [Navigate and search](navigation.md#move-through-results)
explains the phone rows and the order in which columns leave a narrow table.

## Themes

**Unreleased:** The browser opens in the dark Graphite theme. The theme toggle in the sidebar
cycles **Light**, **Dark**, and **System**, which follows your operating system. The browser
remembers the choice.

## Private reverse proxy

For a private proxy serving `https://finance.example.com/moneyflow/`:

```bash
./bin/moneyflow web --listen 127.0.0.1:8080 --open=false \
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

The external URL's path must match the base path. Canonical-origin mutation checks mean direct
listener access is not an alternate write origin when an external URL is configured.

The financial web app has no built-in user authentication. Keep it on a trusted private network,
or put access control in the proxy. A mutation token does not make publicly readable financial
data private. Do not expose it to the public internet merely because the documentation site is public.

## Refresh provider data

**Unreleased:** Every refresh of a Monarch, YNAB, or SimpleFIN profile opens the refresh dialog,
whether you pressed `r`, `R`, or **Refresh**, or the refresh is due in the background. For
Monarch, `r` and **Refresh** read the last 90 days unless complete history is due, and `R`
(Shift+R) reads complete history; see [Monarch refresh](monarch.md#refresh-data). Pointing at
the sidebar's refresh button shows `Complete history · R`. When you open a
profile and a refresh is due, the dialog opens at once, before you work with older data. While it
is open, the workspace and any dialog or drawer behind it do not respond, and Esc goes to the
refresh dialog.

The dialog shows:

- The provider, such as **Refreshing from Monarch**. For Monarch it also names the kind:
  **Recent data (last 90 days)** or **Complete history**.
- Progress, such as `Fetched 7,000 transactions`. The count continues across the provider's
  partitions: Monarch reads visible transactions and then hidden ones, and the hidden ones add
  to the same count. Monarch then reads everything again to confirm nothing changed, shown as
  `Verifying 12,000 of 31,808 transactions`. The bar covers both reads, so the first read fills
  half of it. Monarch reports the hidden transactions' total only with their first page, so the
  bar can pause there until the count catches up; it never moves backward. Until the provider
  reports a total, the bar moves without filling.
- The elapsed time, and when data was last updated, such as
  `Last updated 10 days ago (Oct 3, 2026, 9:15 AM)`.

Screen readers hear the progress at most every 10 seconds.

What happens next:

- **Success:** the dialog closes and a status line notice reports the result, such as
  `Refresh complete: 40 imported, 0 removed.` A warning, such as an unrecorded full refresh,
  follows the result in the same notice and appears beside **Refresh** in the sidebar, or in the
  **Refresh** icon's tooltip in the icon rail.
- **Cancel (Esc):** Moneyflow stops the refresh. The dialog shows **Stopping…** until the server
  confirms that it stopped, then closes. The view loads the latest saved data and reports
  `Refresh stopped.` Data saved before the cancel arrived remains saved.
  Automatic refresh then waits 5 minutes; **Refresh** or `r` still runs at once.
- **Failure:** the dialog stays open with the reason. **Retry** tries again at once, with
  complete history if you asked for it. **Continue
  with data from** a date closes the dialog and keeps the older data.
- **Removals to review:** the dialog shows how many posted transactions the refresh would remove.
  **Confirm refresh** (Enter) applies the refresh; **Cancel** (Esc) keeps the current data.
- **Reconnect needed:** the dialog explains that the provider needs you to sign in again.
  **Reconnect** opens the reconnect form.

When the TUI, an MCP client, or another browser tab is refreshing the profile, the dialog says
so, for example `The terminal app (moneyflow tui) is refreshing this profile. Cancel it there.`
It follows that refresh and closes when it ends. It has no **Cancel** button, because only the
interface that started the refresh can stop it.

If Moneyflow cannot check the refresh status while the dialog shows **Stopping…** or another
interface's refresh, the dialog shows the error. **Continue with data from** a date closes it, and
the stale data banner offers **Retry**.

The web app stops a refresh that runs longer than 30 minutes; no data changes, and
`moneyflow tui` can run it without that limit. When the server cannot say why a request failed,
the dialog says so and the terminal running `moneyflow web` logs the cause. A request the browser
abandons, because you canceled it, reloaded the page, or closed the tab, is not a server failure:
the server answers it with status 499 and code `request_canceled` and does not log it.

### Stale data banner

**Unreleased:** When the data you see missed a refresh, a banner above the table says so, for
example `Showing data from 10 days ago (Oct 3, 2026, 9:15 AM). Refresh stopped.` It stays until a
refresh succeeds, in this tab or in another Moneyflow interface.
The banner appears when no refresh is running
and either:

- The last refresh did not finish: it failed, was canceled, was lost when the page reloaded, needs
  you to reconnect, or would remove transactions that you did not confirm.
- A refresh is due but cannot run, for example while a YNAB profile's credentials are locked.

After a failure, the banner shows when the retry delay ends, such as
`Retry available at 10:40 AM`. Automatic refresh waits at least until then, and longer when no
refresh is due yet. The banner's **Refresh** button, `r`, and **Retry** in the dialog try at
once, unless the provider limits manual attempts, as
[SimpleFIN](simplefin.md#refresh-timing) does. For SimpleFIN, **Refresh** waits and shows when it
becomes available, such as `Available at 10:40 AM`. When the provider needs you to reconnect, the
banner offers **Reconnect** instead. If Moneyflow cannot check for newer provider data, the banner
says so, keeps the last known data age, and offers **Retry**. When a refresh in this tab or elsewhere
saved newer data than the view shows, the banner says `Newer provider data is loading.` until the
view loads it. If loading fails, the banner says `Refreshed provider data could not be loaded.`
with the reason. Its **Retry** loads the view again, each status check also retries, and the
banner stays until the view shows the newer data.

See [Monarch refresh](monarch.md#refresh-data) for when background refresh runs.

## Reconnect and recovery

A profile may need reconnect, vault unlock or explicit schema recovery. Use the displayed workflow.
Closing a tab does not discard durable pending work. If a mutation sees a stale revision, inspect
the new state and invoke it again deliberately. Provider write batches have their own status and
recovery controls; see [editing](editing.md).

**Unreleased:** When creating or recovering a profile, or a setup step, fails, the form says why
and stays open. Moneyflow keeps the profile name and the Monarch email you entered. It clears
passwords, secrets, tokens, and Access URLs, so enter those again.

## Limitations

**Unreleased:** These limits apply to the browser workspace:

- No currency conversion. Each view shows one currency.
- Charts, the KPI strip, Trend, vs prior, and group totals use committed values until you
  commit. See [editing](editing.md).
- The In, Out, Share, Trend, and vs prior columns cannot be sorted.
- There is no command palette. `?` lists the actions and keys of the current view.
- The TUI does not draw charts.
