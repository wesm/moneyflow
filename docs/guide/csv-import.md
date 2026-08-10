# CSV Import

moneyflow includes a generic CSV import engine that supports importing transaction histories from bank and
institution CSV exports. Each institution uses a column-mapping configuration, making it easy to add support
for new formats.

## Overview

CSV import mode provides:

- Import from standard bank/institution CSV exports
- Pluggable institution mappings (Chase, BofA, Citi, etc.)
- Automatic deduplication by date, amount, and merchant
- Per-institution SQLite storage (local, no cloud dependencies)
- Same powerful TUI with keyboard-driven navigation
- Same-day duplicate handling via sequence suffixes

## Supported Institutions

| Institution | Identifier | Description |
|-------------|-----------|-------------|
| Chase Credit Card | `chase_credit` | Chase credit card activity export |

More institutions will be added over time. To request support for a specific institution, [open an issue](https://github.com/wesm/moneyflow/issues).

## Getting Started

### 1. Export Your Data

Export your transaction history from your bank or credit card website:

- **Chase**: Download account activity as CSV
- **Other banks**: Look for "Export", "Download Transactions", or "Download CSV" options

### 2. Import Your Data

```bash
# Import Chase credit card transactions
moneyflow import institution chase_credit ~/Downloads/

# List available institution mappings
moneyflow import list

# Force re-import already-imported files
moneyflow import institution chase_credit ~/Downloads/ --force
```

!!! warning "Importing multiple cards of the same institution"

    If you have more than one card of the same type (e.g., two Chase credit
    cards), import each card with its own `--account` label and point the
    import at that card's CSV file (or a directory containing only that
    card's files):

    ```bash
    moneyflow import institution chase_credit ~/Downloads/Chase1234_Activity.csv --account personal_card
    moneyflow import institution chase_credit ~/Downloads/Chase5678_Activity.csv --account business_card
    ```

    Without `--account`, transactions from different cards that share a
    date, amount, and merchant are treated as duplicates and silently
    dropped. The label is stored on each transaction so the cards remain
    distinguishable in the UI.

The import will:

- Scan for files matching the institution's expected filename pattern
- Parse dates, amounts, and merchant names using the institution's column mapping
- Store extra columns (category, type, memo, etc.) as metadata
- Detect and skip duplicate transactions
- Record import history for each file

### 3. Launch the UI

```bash
# Open Chase credit card mode directly
moneyflow chase_credit
```

## How It Works

### Institution Mappings

Each institution is defined by an `InstitutionMapping` — a configuration that tells the import engine:

- Which CSV columns map to standard transaction fields (date, merchant, amount)
- What date format to expect (e.g., `%m/%d/%Y` for Chase, ISO 8601 for others)
- Whether amounts are already negative (expenses) or need sign flipping
- Which columns to preserve as extra metadata
- How to generate unique transaction IDs for deduplication

Mappings live in `moneyflow/importers/mappings/` — one Python file per institution.

### Deduplication

Transactions are deduplicated using a unique ID generated from date, amount, and merchant fields.
The ID is stable across re-imports, even if you edit the transaction in the TUI later.

```bash
# First import
moneyflow import institution chase_credit ~/Downloads/
# Output: Imported 150 new transactions

# Re-import (safe!)
moneyflow import institution chase_credit ~/Downloads/
# Output: Imported 0 new transactions (all previously imported)

# Force re-import
moneyflow import institution chase_credit ~/Downloads/ --force
```

If you make two identical purchases on the same day from the same merchant (e.g., two $4.50 coffees),
moneyflow automatically adds a sequence suffix to keep them distinct.

Deduplication is scoped per account label: imports with different `--account` values never
deduplicate against each other, which is why each card of the same institution must be
imported with its own label (see the warning above).

### Storage

Each institution's data is stored in a separate SQLite database:

```text
~/.moneyflow/profiles/csv_chase_credit/chase_credit_transactions.db
```

This keeps institutions isolated and makes it easy to manage data per-account.

## CSV Format

### Expected Files

Files matching the institution's file pattern (e.g., `Chase*.csv` for Chase). The engine uses
`encoding` and `skip_rows` settings from the mapping to handle institution-specific quirks.

### Standard Fields

Every institution mapping must produce at minimum:

- **date** — Transaction date
- **merchant** — Merchant/description
- **amount** — Transaction amount (negative for expenses)

Optional standard fields include notes, category, and account. Extra columns from the CSV
are preserved as JSON metadata and available in the TUI.

### Split Debit/Credit Columns

Some institutions use separate Debit and Credit columns instead of a signed Amount column.
The mapping supports this via `debit_column` and `credit_column` fields — the engine computes
`amount = credit - debit` automatically.

## Adding a New Institution

To add support for a new institution:

1. Create a mapping file: `moneyflow/importers/mappings/<institution>.py`
2. Register it in: `moneyflow/importers/mappings/registry.py`
3. Add a CLI command group in `moneyflow/cli.py` (optional, for `moneyflow <institution>`)

See the Chase credit card mapping at `moneyflow/importers/mappings/chase.py` for a complete example.

## Troubleshooting

### "No files matching" error

**Cause**: The directory doesn't contain files matching the institution's expected pattern.

**Solution**:

1. Check that your CSV files are in the directory
2. For Chase, files should match `Chase*.csv`
3. Verify the file extension is `.csv` (not `.xlsx` or `.qfx`)

### Import shows "0 new transactions"

**Cause**: All files have already been imported (tracked in import history).

**Solution**: Use `--force` to re-import:

```bash
moneyflow import institution chase_credit ~/Downloads/ --force
```

### Encoding errors or garbled text

**Cause**: The CSV uses a non-UTF-8 encoding.

**Solution**: Some banks export with Windows-1252 or ISO-8859-1 encoding. The institution
mapping specifies the encoding — if yours is wrong, file an issue or adjust the mapping.

### Missing columns / "ValueError: Missing required column_map targets"

**Cause**: The CSV format has changed or doesn't match the mapping.

**Solution**: Check that your CSV has the expected column headers. If your bank changed
its export format, the mapping may need updating.

## Tips

- **Check list often**: Use `moneyflow import list` to see available institutions
- **Safe to re-import**: Deduplication prevents double-counting
- **Edits stay local**: Category and merchant edits are stored in the local SQLite database
- **Separate profiles**: Each institution is independent — no cross-contamination
- **Build incrementally**: New mappings are small (~30 lines) and easy to contribute

## Questions?

See the main [documentation](../index.md) or [open an issue](https://github.com/wesm/moneyflow/issues).
