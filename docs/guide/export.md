# Export transactions

Press `E` in the terminal or browser to save committed transactions for analysis.
Exports contain financial data and are not encrypted. They are not profile backups.

## Choose a format

| Format | Money and metadata |
| --- | --- |
| Parquet | Typed rows; metadata embedded in the file |
| CSV | Exact decimal text and integer minor units; metadata in `#` comment lines |
| SQLite | `transactions` table plus an `export_metadata` table |

Every format includes `amount`, `amount_minor`, `currency`, and `scale`.
Free-text CSV values that look like spreadsheet formulas are prefixed to prevent formula
interpretation. Treat those prefixes as export formatting, not merchant-name changes.

## Choose what to include

**Full** exports every committed transaction. **Filtered** applies the current search,
date, visibility, and drill-down filters to committed data. Pending edits and inactive redo
operations are excluded; their counts appear in metadata. An empty selected scope does not
produce a file.

Export captures one revision so a concurrent edit cannot mix rows from different states.
Metadata includes the source revision, time, scope, date range, provider kinds, and counts.
Review metadata as well as transaction rows before sharing a file.

## Find the file

The TUI writes beneath the selected profile's `exports/` directory. Existing files are
not overwritten. The browser downloads a file through its normal download controls.
Browser downloads inherit the browser and operating system's handling; they are not encrypted
by Moneyflow.

Export runs without changing committed transactions. Cancel from the export interface if
needed. To preserve the full profile, including credentials and pending changes, follow
[profile backups](../config/caching.md#back-up-a-profile) instead.
