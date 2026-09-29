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

Time grouping uses `t` to cycle year, month, and day. While drilled into a period, left and
right move to adjacent periods; `a` clears the time selection.

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
