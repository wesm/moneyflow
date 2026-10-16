# Filter transactions

Filters limit what you see by date, and choose whether hidden transactions and transfers appear.
They never delete transactions. By default, hidden transactions appear and transfers do not.

## Filter in the terminal

1. Press `f`.
2. Enter a start date and an end date as `YYYY-MM-DD`, or leave both empty.
3. Choose **Show hidden** and **Show transfers** with Space.
4. Choose **Apply**.

Moneyflow refuses the filter if you enter only one date, use another date format, or enter a
start date after the end date. [Keyboard shortcuts](keyboard-shortcuts.md#dialogs) lists the
dialog keys.

## Filter in the browser

Press `f` or click **Filters**, then choose **Show hidden transactions** or **Show transfers**.
Each choice applies at once. To choose dates, use the
[period control](navigation.md#choose-a-period-in-the-browser).

## Start the TUI with a date filter

```bash
moneyflow tui --year 2026
moneyflow tui --since 2026-06-01
moneyflow tui --mtd
```

| Flag | Shows transactions from |
| --- | --- |
| `--year YYYY` | January 1 of that year through today |
| `--since YYYY-MM-DD` | That date through today |
| `--mtd` | The first day of this month through today |

If you combine them, `--mtd` wins over `--since`, and `--since` wins over `--year`. These flags
filter what you see. They do not limit what a provider refresh downloads.

## Other ways to narrow results

Grouping, drill-down, and [search](navigation.md#search) also narrow results, and Esc goes back.
A [filtered export](export.md#choose-what-to-include) uses the same filters on committed
transactions.
