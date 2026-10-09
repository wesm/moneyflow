# Navigate and search

Use `g` to group by merchant, category, group, account, or time. Press Enter to inspect
the selected group and Esc to return. Inside a group, `g` breaks its transactions down by
another of those dimensions. `d` opens individual transactions; `A` opens accounts.

In this checkout, grouped tables in the TUI and browser show **In**, **Out**, and **Net**.
In sums positive amounts; Out sums negative amounts; Net combines them. Amount sorting and
percentages use Net. Hidden transactions count toward the group but do not contribute amounts.
At narrow terminal widths, the table omits Top Category to keep these amounts readable.

The browser's grouped tables also show **Share**, the row's Net as a percent of all rows with
the same sign of Net. Merchant tables add the top category and its percent; category tables
add the group. The first column header names the current grouping, including a breakdown
inside a group. When sorted by amount, a divider marks where Net changes between spending and
income. The Time grouping shows a Net bar and the savings rate instead of Share.

## Move through results

Use arrows or `j`/`k` to move. In the TUI, Page Up/Page Down move by a visible page;
`T` and `B` jump to the top and bottom. Reversing sort with `v` returns the cursor to
the top. `s` changes the sort field. In the browser, click a sortable column header to sort by
it, and click it again to reverse the order.

In the browser, a click on a row moves the cursor and a click on its box selects or clears it; a
double-click opens a group, edits a transaction's Merchant or Category, or opens the
transaction's information from any other cell.

## Go back with Esc

Each Esc press undoes one step, in this order:

1. Close an open dialog or popup.
2. Clear the selection.
3. Clear a search applied at this level.
4. Remove the breakdown of the current group.
5. Return to the previous view.

At the top level with nothing to undo, Esc does nothing. Returning does not restore an
earlier selection, and it keeps the current period and filters.

## Use the breadcrumb and Break down by in the browser

The browser's command bar starts with a breadcrumb trail, such as
`Merchants › Corner Grocery › by Category`. The first crumb names the view that the
drill-downs started from: the grouping, with time grouping named by its unit (**Years**,
**Months**, or **Days**), or **All transactions** for the `d` view. When the saved view does
not record the unit, the crumb reads **Time**. Each drill-down adds a crumb, and a breakdown
adds "by" and its dimension.

- Click an earlier crumb to return to that level in one step. It clears what pressing Esc
  repeatedly would clear on the way, and the browser's Back history stays one entry per view.
- Inside a drill-down, **Break down by** lists **Transactions** and each dimension the
  drill-down has not used. Choosing a dimension breaks the rows down like `g`;
  **Transactions** removes the breakdown.

On screens narrower than 640 px, the menu button opens the sidebar as a drawer. The
period arrows stay in the command bar; **View** opens a sheet with the Day, Month, Year,
and All choices, currency, filters, and Break down by. The search icon shows or hides the
search field, which stays open while a search applies.

## Choose a year, month, or day

In this checkout's TUI, `t` opens a time chooser from a grouped view or transaction list:

| Keys | Result |
| --- | --- |
| `t`, Enter | This calendar month |
| `t`, Down, Enter | Last calendar month |
| `t`, Shift+Tab, Enter | This calendar year |
| `t`, Shift+Tab, Down, Enter | Last calendar year |
| `t`, Tab, Enter | Today |
| `t`, type `2025-09`, Enter | September 2025 |
| `t`, type `2025`, Enter | The whole of 2025 |
| `t`, type `2025-09-15`, Enter | September 15, 2025 |
| `t`, `a` | All time |

The chooser opens at the current month using your computer's calendar. Tab cycles **Year,
Month, Day**; Shift+Tab cycles backward. Down moves to the previous period; Up moves to the
next. Changing resolution keeps the date you were browsing: September 2025 becomes 2025
in Year, then September again in Month. When moving to a shorter month or a non-leap year,
the day is limited to the last day of that month.

Start typing to jump directly. The input format selects the resolution, and the preview
shows the exact date range before you apply it. Invalid or incomplete dates stay in the
chooser for correction. Esc cancels without changing your view.

Selections include the whole calendar period, even if it has no transactions.
They replace existing date limits and time
drill-downs while preserving your grouping, search, and merchant, category, group, or account
selection. The selected period appears above the table.

After choosing a period, left and right move to adjacent years, months, or days at that
resolution; `a` returns to all time and removes any period drill-down.
Enter still opens a selected group and Esc returns to its summary.

To browse grouped periods instead, use `Ctrl+t` in the TUI to jump to Time grouping. Press
it again to cycle year, month, and day. In the browser, press `5` or choose **Time** in the
sidebar. The browser picks the unit from the period: a day or month lists days, and a year
lists months. All time lists months, or years when your transactions span more than 60
months. A custom range lists days up to 62 days, months up to 36 months, and years beyond.

When you have drilled into a period and no single year, month, or day is selected, left and
right select the period next to that drill-down. Every period change, whether from these
keys, `a`, the chooser, or a date filter, removes period drill-downs and keeps the others.
If a period drill-down was the only one and you had broken it down, the breakdown becomes
the top-level grouping. Left, right, and `a` work the same way in the TUI and the browser.

## Choose a period in the browser

The command bar's period control shows the current period, such as `‹ Sep 2026 ›`.

- **‹** and **›** move to the previous or next day, month, or year. They are disabled for a
  custom range or all time unless you have drilled into a period.
- **Day**, **Month**, and **Year** select the period of that length around the current one:
  its last day, or the date of your newest transaction when the period contains it. From all
  time they select the period containing your newest transaction. **All** shows all time.
  The segments are disabled when the selected currency has no transactions.
- Inside the segments, Left and Right change the segment instead of moving the period.

Click the label or press `t` to open the period chooser. Its tabs choose how to pick:

| Tab | Pick |
| --- | --- |
| Day | A day on the calendar |
| Month | A month; ‹ and › change the year |
| Year | A year from your first transaction to your newest |
| Custom | A start day and an end day on the calendar, then **Apply** |

The chooser opens on the current period's tab; all time and custom ranges open on Month.
Arrow keys move between months and years, and Enter picks one. **All time** or `a` shows all
time. Esc closes the chooser without changing the period and returns to the label; moving
focus out of the chooser also closes it.

Dates come from your transactions in the selected currency, never from your computer's
calendar. The chooser offers no dates after your newest transaction, but › can still move
past it.

When the chosen period has no transactions in the selected currency, the table is replaced by
"No transactions in" and the period, with a **Jump to latest** button. It selects the period
of the same length that contains your newest transaction; from a custom range it selects that
month. A search with no matches, or an empty period inside a merchant, category, group, or
account, shows the table's own empty state instead.

## Search

Press `/`, enter a search, and apply it. TUI and web search use case-insensitive regular
expressions over merchant and category labels. Invalid expressions show an error rather
than silently changing the query.

In the browser, the search field sits in the command bar. `/` focuses it, or click it.

- The table previews the results as you type.
- Enter applies the search and returns to the table. Moving focus out of the field also
  applies it; an invalid expression is dropped instead. Switching to another window leaves
  the search open.
- The browser's Back button during a preview ends the search without applying it. Forward
  then works as it does on any page: it returns to the view you left, with the previewed
  search.
- Esc cancels the preview, restores the previous search, and returns to the table.
- The field's clear button (×) previews the view without a search.
- A search longer than 2,048 bytes is refused with a message.

For Amazon-like merchants, [imported product names](amazon-mode.md#match-bank-charges-to-products)
also help find related charges. This does not require an Amazon network connection.

MCP search deliberately differs: it uses literal, case-insensitive substring matching across
merchant, category, and notes. See [MCP](mcp.md#read-and-write-policy).

## Inspect details

Press `i` to view transaction information, including available Amazon order details.
Use `f` for [filters](filters.md) and `?` for the shortcuts available in the current interface.
