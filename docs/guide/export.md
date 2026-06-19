# Exporting Transactions

Export your transactions for backup, external analysis, or sharing. moneyflow
supports three formats with automatic metadata embedded in each.

## Opening the Export Modal

| Key | Action |
|-----|--------|
| ++E++ | Open export format and scope selection |

## Choosing a Format

| Format | Extension | Metadata | Best for |
|--------|-----------|----------|----------|
| Parquet | `.parquet` | Sidecar `.meta.json` file | Polars/Python analysis, compact binary |
| CSV | `.csv` | `#`-prefixed comment header | Spreadsheets, universal interchange |
| SQLite | `.db` | `metadata` table + `transactions` table | Database analysis, SQL queries |

Parquet is selected by default. Use arrow keys or click to switch formats.

## Choosing a Scope

- **Full dataset** — All loaded transactions
- **Current view** — Only what's currently visible on screen (respects filters, search, time period, and drill-down)

## Output Location

Files are written to `~/.moneyflow/exports/` with the naming pattern:

```text
<timestamp>-<scope>-export.<ext>
```

For example: `2026-06-19_143022-full-export.parquet`

All export files and directories are created with restrictive permissions (`0o600` for files, `0o700` for directories).

## Tips

!!! tip "CSV comment prefix"
    When reading exported CSV files back, pass `comment_prefix="#"` to Polars'
    `read_csv()` to skip the metadata header automatically:
    ```python
    pl.read_csv("export.csv", comment_prefix="#")
    ```

!!! tip "Metadata is app-level only"
    Export metadata includes the app version, timestamp, transaction count, date
    range, backend type, and category group names. No credentials, tokens, or
    personal data are included.

!!! tip "All formats preserve the same data"
    Each format exports the same set of columns. The only difference is how
    metadata is attached — sidecar file, inline header, or separate database
    table.
