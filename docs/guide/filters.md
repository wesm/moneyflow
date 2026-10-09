# Filter transactions

Press `f` to choose filters. Filters narrow the current analytical view; they do not delete
transactions or change provider import history.

- **TUI:** the dialog sets the date range and whether to show hidden transactions or
  transfers. Choose **Apply** to use them.
- **Browser:** `f` opens the command bar's **Filters** menu, or click it. It has
  **Show hidden transactions** and **Show transfers**, and each click applies at once and keeps
  the period. Its badge counts the options that are on. Choose dates with the
  [period control](navigation.md#choose-a-period-in-the-browser). Esc or a click outside
  closes the menu.

Use grouping and drill-down to narrow results to an account, merchant, category, or group.

Drill-down and [search](navigation.md#search) also narrow results. Returning with Esc restores
the parent view. A [filtered export](export.md#choose-what-to-include) applies these analytical
constraints to committed transactions, not pending edits.

## Start the TUI with a date filter

```bash
moneyflow tui --year 2026
moneyflow tui --since 2026-06-01
moneyflow tui --mtd
```

When combined, `--mtd` takes precedence over `--since`, then `--year`.
These flags filter local data through today. They do not narrow ordinary provider refreshes.
