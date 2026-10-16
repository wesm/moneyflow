# Export transactions

Save your committed transactions as a Parquet, CSV, or SQLite file to analyze in other tools.
Press `E` in the terminal application (TUI) or the browser, or choose **Export** in the browser.
Exports contain financial data and are not encrypted. An export is not a profile backup.

## Choose a format

| Format | Money and metadata |
| --- | --- |
| Parquet | Typed rows; metadata embedded in the file |
| CSV | Exact decimal text and integer minor units; metadata in `#` comment lines |
| SQLite | `transactions` table plus an `export_metadata` table |

Every format includes `amount`, `amount_minor`, `currency`, and `scale`. In CSV, a text value
that looks like a spreadsheet formula, including a provider transaction ID, starts with an
apostrophe. The apostrophe is export formatting; the saved data is unchanged. Money columns stay
plain decimal numbers.

## Choose what to include

**Full** exports every committed transaction. **Filtered** exports the committed transactions
that match the current search, period, filters, and drill-down. Pending edits and undone edits
are left out; the file's metadata counts them. If nothing matches, Moneyflow writes no file.

An export reads one revision of the profile, so an edit made at the same time cannot mix into
it. The metadata records the revision, time, scope, date range, provider kinds, and counts.
Check the metadata as well as the rows before you share a file.

In the TUI, Tab switches between format and scope, the arrow keys change the choice, Enter
exports, and Esc closes or cancels a running export. In the browser, pick a format and a scope,
then choose **Export**.

## Find the file

The TUI saves the file in the profile's `exports/` directory and never overwrites an existing
file. The browser downloads the file like any other download, and Moneyflow does not encrypt it.

Export does not change your transactions. To keep everything, including credentials and pending
edits, make a [profile backup](../config/caching.md#back-up-a-profile) instead.
