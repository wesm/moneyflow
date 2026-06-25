# Exporting Transactions

Export your loaded transactions for backup or external analysis. Exported files
contain financial data and are not encrypted, so treat them like account
statements before sharing or moving them to cloud storage.

## Opening the Export Modal

| Key | Action |
|-----|--------|
| ++E++ | Open export format and scope selection |

## Choosing a Format

| Format | Extension | Data fidelity | Metadata | Best for |
|--------|-----------|---------------|----------|----------|
| Parquet | `.parquet` | Type-preserving | Sidecar `.meta.json` file | Polars/Python analysis, compact binary |
| CSV | `.csv` | Spreadsheet-safe text; formula-like strings are prefixed | `#`-prefixed comment header | Spreadsheets, universal interchange |
| SQLite | `.db` | Query-friendly text columns | `metadata` table + `transactions` table | Database analysis, SQL queries |

Parquet is selected by default. Use arrow keys or click to switch formats.

## Choosing a Scope

- **Full dataset** — All loaded transactions.
- **Filtered transactions** — The same transaction set returned by moneyflow's
  active filters and navigation state. This includes search text, time
  selection, hide-from-reports and transfer visibility, and active drill-down or
  detail-view constraints.

If the selected scope contains no rows, the app shows "No data to export" and
does not create an empty file. The underlying exporter API can still write
schema-only files for tests and automation.

Exports use loaded transaction data. Pending edits that have not been committed
to the backend are not included.

## Output Location

Files are written to `~/.moneyflow/exports/` with the naming pattern:

```text
<timestamp>-<scope>-export.<ext>
```

For example: `2026-06-19_143022_865809-full-export.parquet`

All export files and directories are created with restrictive permissions (`0o600` for files, `0o700` for directories).

## Tips

!!! tip "CSV comment prefix"
    When reading exported CSV files back, pass `comment_prefix="#"` to Polars'
    `read_csv()` to skip the metadata header automatically:
    ```python
    pl.read_csv("export.csv", comment_prefix="#")
    ```

!!! tip "Metadata contents"
    Export metadata includes the app version, timestamp, transaction count, date
    range, backend type, and category group names. It does not include
    credentials, tokens, or encrypted blobs. Category group names can come from
    user configuration, so review metadata before sharing export files.

!!! tip "Large exports"
    Exports run in the background, but there is no cancellation UI yet. CSV and
    SQLite exports materialize data during serialization, so very large exports
    may take noticeable time.
