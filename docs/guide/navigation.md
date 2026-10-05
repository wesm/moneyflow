# Navigate and search

Use `g` to group by merchant, category, group, account, or time. Press Enter to inspect
the selected group and Esc to return. `d` opens individual transactions; `A` opens accounts.

In this checkout, grouped tables in the TUI and browser show **In**, **Out**, and **Net**.
In sums positive amounts; Out sums negative amounts; Net combines them. Amount sorting and
percentages use Net. Hidden transactions count toward the group but do not contribute amounts.
At narrow terminal widths, the table omits Top Category to keep these amounts readable.

## Move through results

Use arrows or `j`/`k` to move. In the TUI, Page Up/Page Down move by a visible page;
`T` and `B` jump to the top and bottom. Reversing sort with `v` returns the cursor to
the top. `s` changes the sort field.

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
resolution; `a` returns to all time.
Enter still opens a selected group and Esc returns to its summary.

To browse grouped periods instead, use `Ctrl+t` to jump to Time grouping. Press it again to
cycle year, month, and day. In the browser, `t` cycles those units within Time grouping.
While drilled into a period, left and right move to adjacent periods.

## Search

Press `/`, enter a search, and apply it. TUI and web search use case-insensitive regular
expressions over merchant and category labels. Invalid expressions show an error rather
than silently changing the query.

For Amazon-like merchants, [imported product names](amazon-mode.md#match-bank-charges-to-products)
also help find related charges. This does not require an Amazon network connection.

MCP search deliberately differs: it uses literal, case-insensitive substring matching across
merchant, category, and notes. See [MCP](mcp.md#read-and-write-policy).

## Inspect details

Press `i` to view transaction information, including available Amazon order details.
Use `f` for [filters](filters.md) and `?` for the shortcuts available in the current interface.
