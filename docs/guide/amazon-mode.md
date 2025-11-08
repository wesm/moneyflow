# Amazon Purchase Analysis Mode

moneyflow includes a dedicated mode for analyzing Amazon purchase history using Amazon's official "Your Orders" data
export. This allows you to import, categorize, and explore your Amazon purchases using the same powerful terminal UI.

## Overview

Amazon mode provides:

- Import from official Amazon "Your Orders" data export
- Automatic deduplication and category assignment
- SQLite storage (local, no cloud dependencies)
- Same powerful TUI with keyboard-driven navigation
- Track quantity, pricing, and order status

## Getting Started

### 1. Request Your Amazon Data

**IMPORTANT**: You need to request your purchase history from Amazon first.

!!! note "How to Request Your Amazon Data"
    1. Log into your Amazon account
    2. Go to **Account Settings** → **Privacy** → **Request My Data**
    3. Select **"Your Orders"** (you don't need all your data)
    4. Submit the request
    5. Wait 1-3 days for Amazon to prepare your data
    6. Download the **Your Orders.zip** file when ready
    7. Unzip it to get the "Your Orders" directory

The directory will contain files like:

- `Retail.OrderHistory.1/Retail.OrderHistory.1.csv`
- `Retail.OrderHistory.2/Retail.OrderHistory.2.csv`
- etc.

### 2. Import Your Purchase Data

```bash
# Import from the unzipped directory
moneyflow amazon import ~/Downloads/"Your Orders"
```

The import will:

- Scan for all Retail.OrderHistory CSV files
- Parse and validate order data
- Assign categories automatically using built-in category mappings
- Detect and skip duplicates
- Skip cancelled orders
- Store everything in SQLite

### 3. Check Import Status

```bash
# View database statistics
moneyflow amazon status
```

This shows:

- Total transactions imported
- Date range of purchases
- Total amount spent
- Number of unique items and categories
- Import history

### 4. Launch the UI

You can access Amazon mode in two ways:

#### Option 1: Account Selector (Recommended)

```bash
# Launch moneyflow normally
moneyflow
```

After importing, Amazon will appear in the account selector alongside your other accounts (Monarch Money, YNAB,
etc.). Select it with arrow keys or click.

#### Option 2: Direct CLI Command

```bash
# Open Amazon mode directly
moneyflow amazon
```

Both methods use the same keyboard-driven interface and features.

## CSV Format

moneyflow imports from the official Amazon "Your Orders" data export format.

### Expected Files

Files named: `Retail.OrderHistory.*.csv`

### Expected Columns

- **ASIN**: Amazon Standard Identification Number (ASIN) or product name hash if ASIN missing
- **Order ID**: Amazon order identifier
- **Order Date**: ISO timestamp (e.g., "2025-10-13T22:08:07Z")
- **Product Name**: Item description/title
- **Quantity**: Number of items ordered
- **Total Owed**: Final amount paid (after tax)
- **Unit Price**: Item price before tax
- **Order Status**: "Closed", "New", "Cancelled", etc.
- **Shipment Status**: "Shipped", "Delivered", etc.

### Category Assignment

Categories are automatically assigned using moneyflow's built-in category mappings. You can edit categories in the UI
after import.

## Features

### Automatic Deduplication

Transactions are deduplicated based on a unique ID generated from:

- ASIN (or product name hash if ASIN missing)
- Order ID

This means you can safely re-import the same directory multiple times - duplicates will be automatically
skipped.

```bash
# First import
moneyflow amazon import ~/Downloads/"Your Orders"
# Output: Imported 100 new transactions

# Re-import (safe!)
moneyflow amazon import ~/Downloads/"Your Orders"
# Output: Skipped 100 duplicates, Imported 0 new transactions
```

Cancelled orders are automatically skipped during import.

### Incremental Imports

Amazon mode supports incremental imports, preserving any manual edits you've made:

1. Import initial data export
2. Edit categories and item names in the UI
3. Request and import a fresh data export from Amazon (with new purchases)
4. Only new orders are added - your edits are preserved
5. Use `--force` flag to re-import and overwrite existing transactions if needed

### Database Location

**Default Location**:

Amazon data is stored in your profile directory:

```text
~/.moneyflow/profiles/amazon/amazon.db
```

This integrates Amazon with your other accounts and allows selection from the account picker.

**Custom Database Location**:

You can use a custom location with the `--db-path` flag:

```bash
# Use custom database
moneyflow amazon --db-path ~/Documents/amazon-purchases.db

# All commands support --db-path
moneyflow amazon --db-path ~/custom.db import ~/Downloads/"Your Orders"
moneyflow amazon --db-path ~/custom.db status
```

## UI Navigation

Amazon mode uses the same keyboard shortcuts as the main application.
See [Keyboard Shortcuts](keyboard-shortcuts.md) for the complete reference.

**View name mappings:**

In Amazon mode, views reflect Amazon purchase data:

- **Item** (instead of Merchant) - Product names
- **Category** - Product categories
- **Group** - Category groups
- **Order ID** (instead of Account) - Group by Amazon order

All navigation, editing, and search shortcuts work identically.

## Troubleshooting

### Import fails with "No Retail.OrderHistory CSV files found"

**Cause**: The directory doesn't contain Amazon export files.

**Solution**:

1. Make sure you've unzipped the "Your Orders.zip" file
2. Point to the unzipped directory (not individual CSV files)
3. The directory should contain folders like `Retail.OrderHistory.1/`

### "Amazon database is empty" when launching

**Cause**: No data has been imported yet.

**Solution**: Import your data first:

```bash
moneyflow amazon import ~/Downloads/"Your Orders"
```

### Import shows "0 new transactions"

**Cause**: All transactions already exist in the database.

**Solution**:

- This is expected if you're re-importing the same data
- Use `--force` flag to re-import: `moneyflow amazon import --force ~/Downloads/"Your Orders"`
- Or delete the database and start fresh: `rm ~/.moneyflow/profiles/amazon/amazon.db`

### Missing ASIN for some items

**Cause**: Some Amazon items don't have ASINs (e.g., digital content, gift cards).

**Solution**: moneyflow automatically generates a pseudo-ASIN from the product name hash. This is normal and doesn't
affect functionality.

## Tips

- **Check status often**: Use `moneyflow amazon status` to verify imports
- **Safe to experiment**: Edits are local only, delete the database to reset
- **Use custom paths**: Keep different analyses separate with `--db-path`
- **Re-import periodically**: Request fresh exports from Amazon to get new orders
- **Filter by status**: Use order status and shipment status to find specific orders

## Questions?

See the main [documentation](../index.md) or [open an issue](https://github.com/wesm/moneyflow/issues).
