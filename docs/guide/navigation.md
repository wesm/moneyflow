# Navigate and search

Moneyflow groups your transactions by merchant, category, group, account, or time. Open a group
to see its transactions, break it down by another dimension, choose a period, and search. The
terminal application (TUI) and the browser use the same keys. The browser adds pointer, touch,
and on-screen controls for each key. This page covers both and says where they differ. Every key
is listed in [keyboard shortcuts](keyboard-shortcuts.md).

## Group and drill down

| Task | Key | Browser control |
| --- | --- | --- |
| Cycle Merchant, Category, Group, Account, Time | `g` | |
| Group by one dimension | `1`–`5` | **Merchants**, **Categories**, **Groups**, **Accounts**, or **Time** in the sidebar |
| Group by account | `A` | **Accounts** in the sidebar |
| Show every transaction | `d` | **All transactions** in the sidebar |
| Open the selected group | Enter | Double-click the row; on a phone, tap it |
| Go back one step | Esc | Click an earlier breadcrumb to go back several steps |

`1`–`5`, `A`, and the sidebar entries leave any drill-down and show the top level. From
**All transactions**, `g` returns to the grouped view. Inside a group, `g` breaks its
transactions down by a dimension the drill-down has not used yet. In the browser, the
[Break down by](#use-the-breadcrumb-and-break-down-by-in-the-browser) control does the same.

Grouped tables in both interfaces show **In**, **Out**, and **Net**. In sums positive amounts,
Out sums negative amounts, and Net combines them. Amount sorting and percentages use Net. Hidden
transactions count toward the group but do not contribute amounts. At narrow terminal widths,
the TUI table omits Top Category to keep these amounts readable.

The browser's grouped tables also show:

- **Share**, the row's Net as a percent of all rows with the same sign of Net.
- The top category and its percent in merchant tables, and the group in category tables.
- A first column header that names the current grouping, including a breakdown inside a group.
- A divider, when sorted by amount, where Net changes between spending and income.
- A Net bar and the savings rate instead of Share in the Time grouping.

## Move through results

| Key | Action |
| --- | --- |
| Arrows or `j` / `k` | Move one row |
| Page Up / Page Down | Move one page. In the browser, a page is the rows fully on screen |
| Home or `T` | First row |
| End or `B` | Last row |
| `s` | Change the sort field |
| `v` | Reverse the sort order |

A new period, grouping, drill-down, search, or filter puts the cursor on the first row in both
interfaces. After a drill-down, Esc returns to the row you left. A sort returns the TUI cursor to
the first row. The browser cursor stays on its row.

With a pointer in the browser:

- Click a sortable column header to sort by it. Click it again to reverse the order.
- Click a row to move the cursor there. Click its box to select or clear it.
- Double-click a group to open it. Double-click a transaction's Merchant or Category cell to
  [edit it](editing.md#open-the-editor). Double-click any other cell to open the transaction's
  information.

### Phones and narrow windows

On screens 640 px wide or narrower, each browser row takes two lines. A group shows its name and
Net, then its count, In, and Out. A transaction shows its merchant and amount, then its date,
category, and account. Touch works this way:

- Tap the box at the start of a row to select it or clear it.
- Tap a group, or its › button, to open it. The cursor moves there first, so going back
  returns to that group.
- Tap a transaction to open its information. Its ⋯ button opens **Merchant**, **Category**,
  **Hide**, **Delete**, and **Info** for that transaction.
- The header keeps the select-all box and the current sort field. Tap the field to reverse the
  order.

The keys work the same at every width. The browser table never scrolls sideways. On wider
screens, each text column, such as Merchant, Category, Account, or Top category, fits the
longest label the view has shown, up to 20 rem. The name column (Merchant for transactions, the
group's name in grouped tables) takes the room the other columns leave. After a drill shows one
merchant, category, or account in every row, the next text column takes that room. When the
room runs short, the widest text column narrows first, so a column cuts off its labels only
when it is at least as wide as the others. Widths stay the same while you scroll. In the Time
table, the Net bar takes the room.

When the columns still do not fit, the browser leaves out the least important ones until they
do. The boxes, the name column, and Net or Amount always stay. Columns leave in this order:

| Table | Order |
| --- | --- |
| Grouped | Top category or Group, then Trend and vs prior, Share, Count, and In and Out |
| Time | Net bar, then Savings rate, Count, and In and Out |
| Transactions | Account, then Flags, Date, and Category |

Below 760 px, every grouped table leaves out Trend and vs prior, along with Top category or
Group where the table has one.

### Long results in the browser

The browser table scrolls through every row of the result. It loads rows 200 at a time, and
rows still loading show as blank bars.

The cursor follows the scroll. When the mouse wheel, scrollbar, or touch carries the cursor row
off screen, the cursor moves to the nearest row fully on screen: the first after scrolling
down, or the last after scrolling up. The next `j` or `k` moves on from there. A row that has
not loaded yet gets the cursor once it loads, and scrolling back to the cursor before then
leaves the cursor where it was. While you drag across rows to select text, the cursor stays put.

## Go back with Esc

Each Esc press undoes one step, in this order:

1. Close an open dialog or popup.
2. Clear the selection.
3. Clear a search applied at this level.
4. Remove the breakdown of the current group.
5. Return to the previous view.

At the top level with nothing to undo, Esc does nothing. Going back does not restore an earlier
selection, and it keeps the current period and filters.

The TUI shows where you are in its top line. Each drill-down appears as a prefix and label, such
as `M: Corner Grocery > (by Category)`. `M`, `C`, `G`, and `A` mark merchant, category, group,
and account drill-downs; `T:` marks a period. A selected period comes first and an applied
search comes last, as `Search: '…'`.

### Use the breadcrumb and Break down by in the browser

The browser's command bar starts with a breadcrumb trail, such as
`Merchants › Corner Grocery › by Category`. The first crumb names the view the drill-downs
started from. It is the grouping, with time grouping named by its unit (**Years**, **Months**,
or **Days**), or **All transactions** for the `d` view. When the saved view does not record the
unit, the crumb reads **Time**. Each drill-down adds a crumb, and a breakdown adds "by" and its
dimension. A Time breakdown names its unit instead, such as **by Month**.

- Click an earlier crumb to return to that level in one step. It clears what pressing Esc
  repeatedly would clear on the way, and the browser's Back history stays one entry per view.
- Inside a drill-down, **Break down by** lists **Transactions** and each dimension the
  drill-down has not used. Choosing a dimension breaks the rows down like `g`;
  **Transactions** removes the breakdown. A chosen Time breakdown shows its unit, such as
  **Time · Day**.

At 900 px and narrower, the menu button in the top bar opens the sidebar as a drawer. On
screens 640 px wide or narrower, the period arrows stay in the command bar, and **View** opens a
sheet with the Day, Month, Year, and All choices, currency, filters, and Break down by. The
search icon shows or hides the search field, which stays open while a search applies.

## Choose a year, month, or day

These rules apply in both interfaces:

- A period covers the whole calendar year, month, or day, even if it has no transactions.
- Choosing a period replaces existing date limits and time drill-downs. Your grouping, search,
  and merchant, category, group, or account drill-downs stay.
- After you choose a period, Left and Right move to the adjacent year, month, or day.
- `a` returns to all time and removes any period drill-down.
- When you have drilled into a period and no single year, month, or day is selected, Left and
  Right select the period next to that drill-down.
- Every period change, whether from Left, Right, `a`, a chooser, or a date filter, removes period
  drill-downs and keeps the others. If a period drill-down was the only one and you had broken
  it down, the breakdown becomes the top-level grouping.
- Enter still opens a selected group and Esc returns to its summary.

`p` opens a period chooser. The TUI chooser and the browser chooser work differently; both are
described below.

### Group by time

`t` lists periods as rows. At the top level it groups by Time. Inside a drill-down or breakdown it
breaks the rows down by Time. In a Time view, each press cycles the unit through year, month, and
day. A drill-down into a period cannot break down by Time again, so there `t` tells you to press
Esc to go back to the time rows. The key hint names the unit the next press shows: `t=By Month`
in the TUI footer, `t Month` in the browser status line.

In the browser, `5` or **Time** in the sidebar also groups by Time. The browser picks the unit
from the period:

- A day or month lists days, and a year lists months.
- All time lists months, or years when your transactions span more than 60 months.
- A custom range lists days up to 62 days, months up to 36 months, and years beyond.
- A unit you choose with `t` lists the same dates in that unit and stays until you change the
  period.

The TUI keeps the unit you chose.

Days without transactions are left out, in the TUI and in the browser's table and cash-flow
chart. Months and years without transactions stay listed so gaps are visible. The browser lists
every one in the period, and the TUI lists those between your first and last transactions.

### Choose a period in the terminal

In the TUI, `p` opens the time chooser from a grouped view or transaction list:

| Keys | Result |
| --- | --- |
| `p`, Enter | This calendar month |
| `p`, Down, Enter | Last calendar month |
| `p`, Shift+Tab, Enter | This calendar year |
| `p`, Shift+Tab, Down, Enter | Last calendar year |
| `p`, Tab, Enter | Today |
| `p`, type `2025-09`, Enter | September 2025 |
| `p`, type `2025`, Enter | The whole of 2025 |
| `p`, type `2025-09-15`, Enter | September 15, 2025 |
| `p`, `a` | All time |

The chooser opens at the current month using your computer's calendar. Tab cycles **Year,
Month, Day**; Shift+Tab cycles backward. Down moves to the previous period and Up to the next.
Changing resolution keeps the date you were browsing: September 2025 becomes 2025 in Year,
then September again in Month. When moving to a shorter month or a non-leap year, the day is
limited to the last day of that month.

Type `YYYY`, `YYYY-MM`, or `YYYY-MM-DD` to jump directly. The format selects the resolution, and
the preview shows the exact date range before you apply it. Invalid or incomplete dates stay in
the chooser for correction. Esc cancels without changing your view. The selected period appears
above the table.

### Choose a period in the browser

The command bar's period control shows the current period, such as `‹ Sep 2026 ›`.

- **‹** and **›** move to the previous or next day, month, or year. They are disabled for a
  custom range or all time unless you have drilled into a period.
- **Day**, **Month**, and **Year** select the period of that length around the current one:
  its last day, or the date of your newest transaction when the period contains it. From all
  time they select the period containing your newest transaction. **All** shows all time.
  The segments are disabled when the selected currency has no transactions.
- Inside the segments, Left and Right change the segment instead of moving the period.

Click the label, or press `p` as shown beside it, to open the period chooser. Its tabs choose how
to pick:

| Tab | Key | Pick |
| --- | --- | --- |
| Day | `d` | A day on the calendar |
| Month | `m` | A month; ‹ and › change the year |
| Year | `y` | A year from your first transaction to your newest |
| Custom | `c` | A start day and an end day on the calendar, then **Apply** |

The chooser opens on the current period's tab; all time and custom ranges open on Month.
Focus starts inside the tab, on the selected month, year, or day. Without one, it starts at
the period's last day, or at your newest transaction for all time or a period that contains it.
A period outside your transactions starts at your first or newest transaction, whichever is
nearer.

- Arrow keys move between months and years.
- On a calendar, Left and Right move a day and Up and Down move a week. Moving past the
  shown month turns the page. Arrows stop at your first and newest transactions.
- Enter picks the focused month, year, or day.
- `d`, `m`, `y`, and `c` switch tabs and move focus into the new tab.
- On the tabs, Left and Right switch tabs, and Down or Enter moves into the tab.
- **All time** or `a` shows all time.
- Esc closes the chooser without changing the period and returns to the label. Moving focus
  out of the chooser also closes it.

While the chooser is open, these letters act on it instead of the main shortcuts.

The browser takes dates from your transactions in the selected currency, never from your
computer's calendar. The chooser offers no dates after your newest transaction, but › can still
move past it.

When the chosen period has no transactions in the selected currency, the table is replaced by
"No transactions in" and the period, with a **Jump to latest** button. It selects the period
of the same length that contains your newest transaction; from a custom range it selects that
month. A search with no matches, or an empty period inside a merchant, category, group, or
account, shows the table's own empty state instead.

## Search

Press `/` and type. The TUI and the browser search merchant and category labels with
case-insensitive regular expressions, and the table previews the results as you type. Enter
applies the search. Esc cancels it and restores the previous search. An invalid expression shows
an error, and Enter does not apply it.

In the browser, the search field sits in the command bar. Press `/` or click the field.

- Enter applies the search and returns to the table. Moving focus out of the field also
  applies it; an invalid expression is dropped instead. Switching to another window leaves
  the search open.
- The browser's Back button during a preview ends the search without applying it. Forward
  then works as it does on any page: it returns to the view you left, with the previewed
  search.
- Esc cancels the preview, restores the previous search, and returns focus to the table.
- The field's clear button (×) previews the view without a search.
- A search longer than 2,048 bytes is refused with a message.
- If the preview fails for another reason, such as a lost connection or a busy profile, the
  field says "Search failed." with the reason, and Enter does not apply it.

For Amazon-like merchants, [imported product names](amazon-mode.md#match-bank-charges-to-products)
also help find related charges. This does not require an Amazon network connection.

MCP search differs on purpose. It uses literal, case-insensitive substring matching across
merchant, category, and notes. See [MCP](mcp.md#read-and-write-policy).

To narrow by date range, hidden transactions, or transfers, use [filters](filters.md).

## Inspect a transaction

Press `i` to open the cursor transaction's information, including available Amazon order details.
In the TUI, Up/Down or `j`/`k` and Page Up/Page Down scroll it, and Esc, Enter, or `i` closes it.
In the browser, Enter on a transaction row also opens it, as does a double-click on a cell other
than Merchant or Category, or **Info** in a phone row's ⋯ menu.
