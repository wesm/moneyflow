# Web interface

Run `./bin/moneyflow web` to open the profile selector. Use `--demo` for a disposable synthetic
profile, or `--profile "Example Profile"` to select one explicitly. The browser and TUI share
profile data and server-authoritative transitions.

**Unreleased:** The command prints the address it serves, such as
`Moneyflow web: http://127.0.0.1:8080/`. A profile is served on `127.0.0.1:8080`, so bookmarks
and the browser's remembered settings keep working. `--demo` picks a free port on `127.0.0.1`
instead, so a demo starts even while another server holds port 8080. `--listen` sets the host and
port in either case.

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

An entry that cannot run now is dimmed and shows its reason when you point at it.

The bottom of the sidebar holds:

- The pending-edits card, such as `3 pending edits · 27 transactions`. Its **Review** button
  (`w`) appears while edits are pending, and **Write** shows an unfinished provider write's
  progress. See [review and commit](editing.md#review-and-commit).
- Provider status for Monarch, YNAB, and SimpleFIN profiles: when data was last refreshed, a
  refresh button (`r`), refresh progress, and **Reconnect** when the provider needs it.
- The theme toggle; see [Themes](#themes).

**Unreleased:** At 1280 px and wider, the **Collapse sidebar** button beside the Moneyflow name
switches the sidebar to the icon rail that narrower windows use. The **Expand sidebar** button at
the top of the rail brings the sidebar back. The browser remembers the choice. Narrower windows
show the rail or the phone drawer whatever you chose; see [Narrow screens](#narrow-screens).

The command bar shows where you are and what the view includes. It holds the
[breadcrumb and Break down by](navigation.md#use-the-breadcrumb-and-break-down-by-in-the-browser),
the [period control](navigation.md#choose-a-period-in-the-browser), the currency chip when the
profile has more than one currency, the [Filters](filters.md) menu, the
[search](navigation.md#search) field, and the **Charts** switch.

The status line at the bottom shows:

- Up to six keys for the current view, such as `↵ Open` and `/ Search`. A short notice, such as
  why a key did nothing, takes their place for four seconds or until your next action.
- The connection: **Connected**, **Updating…** for requests slower than 300 ms, or **Offline**
  with **Retry**.
- The profile revision, except at phone width.
- The pending-edits text, when the sidebar card is not shown.

## Charts

**Unreleased:** Above the table, the KPI strip sums the current period, and the chart row
shows how the money moved. All charts read committed values; pending edits change them only
after you commit. MCP clients can read the same data with
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
- A period still in progress, such as the current month, is drawn hollow. So is a period that a
  custom range covers only in part.

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
- A year: its months, ending at the current month while the year is in progress.
- A custom range: days up to 62 days, months up to 36 months, and years beyond.
- All time: months from your first to your newest transaction in the currency, or years when
  that is more than 60 months.
- Periods without transactions are drawn as empty columns.

In the Time grouping, the columns are the table's rows; see
[Navigate and search](navigation.md#choose-a-year-month-or-day) for their unit.

The **trend** ends at the current month while the scope is in progress, and at your newest
transaction's month for all time. A month in progress ends the line with a hollow dot. Months
before your first transaction in the currency are left out.

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

- **1280 px and wider:** the full sidebar, unless you collapse it to the icon rail.
- **641 to 1279 px:** an icon rail with tooltips replaces the sidebar. The status line shows the
  pending-edits text.
- **Below 900 px:** the panel moves under the cash-flow chart.
- **640 px and narrower:** the phone layout. The menu button opens the sidebar as a drawer, the
  command bar takes two rows, and the charts open from the **Charts** switch. The status line
  shows the pending-edits text instead of keys, and leaves out the profile revision.

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

## Reconnect and recovery

A profile may need reconnect, vault unlock or explicit schema recovery. Use the displayed workflow.
Closing a tab does not discard durable pending work. If a mutation sees a stale revision, inspect
the new state and invoke it again deliberately. Provider write batches have their own status and
recovery controls; see [editing](editing.md).

## Limitations

**Unreleased:** These limits apply to the browser workspace:

- No currency conversion. Each view shows one currency.
- Charts, the KPI strip, Trend, vs prior, and group totals use committed values until you
  commit. See [editing](editing.md).
- The In, Out, Share, Trend, and vs prior columns cannot be sorted.
- There is no command palette. `?` lists the actions and keys of the current view.
- The TUI does not draw charts.
