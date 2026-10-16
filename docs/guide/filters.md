# Filter transactions

Filters narrow the current view by date and by whether hidden transactions and transfers appear.
They never delete transactions or change provider import history. Press `f` in the terminal
application (TUI) or the browser. By default, hidden transactions are shown and transfers are not.

| Interface | How filters work |
| --- | --- |
| TUI | `f` opens a dialog with a start date, an end date, **Show hidden**, and **Show transfers**. Choose **Apply** to use them |
| Browser | `f` or a click opens the command bar's **Filters** menu with **Show hidden transactions** and **Show transfers**. Each click applies at once and keeps the period. Esc or a click outside closes the menu |

In the TUI dialog, enter both dates as `YYYY-MM-DD`, or clear both. Moneyflow refuses a single
date (`Enter both dates or clear both dates`), another format (`Dates must use YYYY-MM-DD`), and
a start after the end (`Start date must not be after end date`). The
[dialog keys](keyboard-shortcuts.md#dialogs) are in the shortcut reference.

The browser chooses dates with the [period control](navigation.md#choose-a-period-in-the-browser)
instead. The **Filters** badge counts the options changed from the default. Below 640 px wide,
filters are in the **View** sheet.

Grouping, drill-down, and [search](navigation.md#search) also narrow results. Esc restores the
parent view. A [filtered export](export.md#choose-what-to-include) applies these constraints to
committed transactions, not pending edits.

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
| `--mtd` | The first day of the current month through today |

When combined, `--mtd` takes precedence over `--since`, then `--year`. These flags filter local
data. They do not narrow ordinary provider refreshes.
