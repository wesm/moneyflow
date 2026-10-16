# Navigate and search

Find where your money went by grouping transactions, opening a group, choosing a period, and
searching. The terminal application (TUI) and the browser use the same keys. In the browser you
can also click or tap. [Keyboard shortcuts](keyboard-shortcuts.md) lists every key.

## Group and drill down

Group transactions to see totals by merchant, category, group, account, or time. Open a group to
see the transactions in it.

| Task | Key | In the browser |
| --- | --- | --- |
| Cycle Merchant, Category, Group, Account, Time | `g` | |
| Group by one dimension | `1`–`5` | Choose **Merchants**, **Categories**, **Groups**, **Accounts**, or **Time** in the sidebar |
| Group by account | `A` | Choose **Accounts** |
| Show all transactions | `d` | Choose **All transactions** |
| Open the selected group | Enter | Double-click or tap the row |
| Go back one step | Esc | Click an earlier breadcrumb to go back several steps |

- `1`–`5`, `A`, and the sidebar choices leave any drill-down.
- From all transactions, `g` returns to the grouped view.
- Inside a group, `g` breaks its transactions down by a dimension you have not used yet. In the
  browser, **Break down by** does the same, and **Transactions** removes the breakdown.

Each group shows **In** (the sum of positive amounts), **Out** (the sum of negative amounts), and
**Net** (both combined). Amount sorting and percentages use Net. Hidden transactions count
toward a group but add nothing to its amounts. In the browser, **Share** is a row's Net as a
percent of all rows with the same sign.

## Move through results

| Key | Action |
| --- | --- |
| Arrows or `j` / `k` | Move one row |
| Page Up / Page Down | Move one page |
| Home or `T` | First row |
| End or `B` | Last row |
| `s` | Change the sort field |
| `v` | Reverse the sort order |

In the browser, click a column header to sort by it, and click again to reverse. Click a row to
move the cursor to it. Double-click a group to open it, a Merchant or Category cell to
[edit it](editing.md#open-the-editor), or any other cell to see the transaction's details.

A new period, grouping, drill-down, search, or filter moves the cursor to the first row. After a
drill-down, Esc returns to the row you left. A sort moves the TUI cursor to the first row; the
browser cursor stays on its row.

The browser loads long results 200 rows at a time. When you scroll the cursor row off screen,
the cursor moves to the nearest row on screen, so the next `j` or `k` continues from there.

## Go back with Esc

Each Esc press undoes one step, in this order:

1. Close an open dialog or menu.
2. Clear the selection.
3. Clear a search applied at this level.
4. Remove the breakdown of the current group.
5. Return to the previous view.

At the top level with nothing to undo, Esc does nothing. Going back keeps the current period and
filters. It does not restore an earlier selection.

### Use the breadcrumb and Break down by in the browser

Click an earlier breadcrumb to return to that level in one step. It clears what repeated Esc
presses would clear on the way, and adds one entry to the browser's history.

Inside a drill-down, use **Break down by** to break the rows down by a dimension you have not
used yet. Choose **Transactions** to remove the breakdown.

## Choose a year, month, or day

Choose a period to see one calendar year, month, or day, or all time. These rules apply in both
interfaces:

- A period covers the whole calendar year, month, or day, even if it has no transactions.
- A new period replaces any date limits and time drill-downs. Your grouping, search, and other
  drill-downs stay.
- Left and Right move to the previous or next period of the same length.
- `a` returns to all time and removes any period drill-down.
- If you drilled into a period without choosing one, Left and Right select the period next to
  that drill-down.
- If a period drill-down was your only drill-down and you broke it down, a period change makes
  the breakdown the top-level grouping.

Press `p` to open a period chooser. The TUI and browser choosers differ; see below.

### Group by time

Press `t` to list periods as rows. At the top level it groups by time. Inside a drill-down it
breaks the rows down by time. In a time view, each press changes the unit: year, month, then day.
Inside a drill-down into one period, `t` tells you to press Esc first.

The browser also groups by time with `5` or **Time**, and picks the unit from the period:

| Period | Rows |
| --- | --- |
| A day or a month | Days |
| A year | Months |
| All time | Months, or years when your transactions span more than 60 months |
| Custom range | Days up to 62 days, months up to 36 months, years beyond |

A unit you choose with `t` stays until you change the period. The TUI keeps the unit you chose.

Days without transactions are left out. Months and years without transactions are listed, so
gaps show. The browser lists every month or year in the period; the TUI lists those between your
first and last transactions.

### Choose a period in the terminal

The TUI chooser starts at the current month on your computer's calendar.

| Keys | Result |
| --- | --- |
| `p`, Enter | This month |
| `p`, Down, Enter | Last month |
| `p`, Shift+Tab, Enter | This year |
| `p`, Shift+Tab, Down, Enter | Last year |
| `p`, Tab, Enter | Today |
| `p`, type `2025-09`, Enter | September 2025 |
| `p`, type `2025`, Enter | All of 2025 |
| `p`, type `2025-09-15`, Enter | September 15, 2025 |
| `p`, `a` | All time |

- Tab and Shift+Tab switch between year, month, and day. The date you were on stays: September
  2025 becomes 2025, then September 2025 again. A day past the end of a shorter month becomes
  that month's last day.
- Down picks the previous period and Up the next.
- Type `YYYY`, `YYYY-MM`, or `YYYY-MM-DD` to jump to a date. Moneyflow shows the exact date range
  before you press Enter. An invalid or incomplete date stays in the chooser for you to fix.
- Esc closes the chooser without changing the view.

### Choose a period in the browser

Use the period control in the command bar:

- **‹** and **›** move to the previous or next period. They do not apply to a custom range or to
  all time, unless you have drilled into a period.
- **Day**, **Month**, and **Year** choose a period of that length that contains the current
  period's last day, or your newest transaction if the period contains it. **All** shows all
  time.

Click the period, or press `p`, to open the chooser:

| Tab | Key | Choose |
| --- | --- | --- |
| Day | `d` | A day |
| Month | `m` | A month; ‹ and › change the year |
| Year | `y` | A year from your first transaction to your newest |
| Custom | `c` | A start day and an end day, then **Apply** |

- Arrow keys move between months and years. On a calendar, Left and Right move a day, and Up and
  Down move a week.
- Enter chooses. `a` or **All time** shows all time.
- Esc closes the chooser without changing the period.

The browser offers only dates in your transactions in the selected currency. It never uses your
computer's calendar, and the chooser offers no dates after your newest transaction.

If a period has no transactions, choose **Jump to latest** to go to the period of the same
length that contains your newest transaction. From a custom range, it chooses that month.

## Search

Search finds transactions by merchant or category name. Press `/` and type. The search is a
case-insensitive regular expression, and the results update as you type.

- Enter applies the search. Esc cancels it and restores the previous search.
- An invalid expression shows an error, and Enter does not apply it.
- In the browser, clicking outside the search field also applies it, and the browser's Back
  button cancels a search you have not applied.
- A search longer than 2,048 bytes is refused.
- If a browser search fails, for example because the connection is lost, the field says
  "Search failed." with the reason.

Product names from an [Amazon import](amazon-mode.md#match-bank-charges-to-products) also match
the bank charges they belong to. MCP search works differently: it matches literal text,
ignoring case, in merchant, category, and notes. See [MCP](mcp.md#read-and-write-policy).

To limit dates, hidden transactions, or transfers, use [filters](filters.md).

## See a transaction's details

Press `i` to see the cursor transaction's details, including any matching Amazon order. In the
browser, Enter on a transaction row does the same. Press Esc to close the details.
