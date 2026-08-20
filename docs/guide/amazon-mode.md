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
moneyflow provider import amazon ~/Downloads/"Your Orders" --profile "Amazon Orders"
```

The import will:

- Scan for all Retail.OrderHistory CSV files
- Parse and validate the complete candidate before changing the profile
- Reconcile repeated and corrected rows under stable local identities
- Preserve existing user categories and assign new purchases to Uncategorized
- Treat cancelled rows as authoritative observations that can retire earlier items
- Store the item ledger and ordinary Moneyflow transactions in one Go v2 SQLite profile

When creating a profile, omit `--currency USD --scale 2` to confirm those defaults interactively.
For a different currency, provide both flags. A profile's currency and scale are immutable after
its first successful import.

Use `--clone-taxonomy-from NAME_OR_ID` only on the first import to copy another profile's committed
taxonomy. The copy is point-in-time; later taxonomy changes do not synchronize across profiles.

### 3. Open the Profile

```bash
# Open by exact profile name or opaque profile ID
moneyflow tui --profile "Amazon Orders"
```

You can also run `moneyflow tui` or `moneyflow web`, choose **Add profile**, and complete the Amazon
source chooser. Existing Amazon profiles appear in the shared profile selector.

Press ++r++ inside an Amazon profile to choose another export directory. Amazon imports are always
user-initiated: there is no background refresh, reconnect state, or Amazon credential storage.

### 4. Edit and Commit Locally

Amazon profiles use Moneyflow's ordinary local journal. Merchant/category edits, hide toggles,
deletions, category and group management, undo/redo, and ++w++ commit never call Amazon. Pressing
++r++ is the only import action.

## CSV Format

moneyflow imports from the official Amazon "Your Orders" data export format.

### Expected Files

Files named: `Retail.OrderHistory.*.csv`

### Expected Columns

- **ASIN**: Amazon Standard Identification Number (ASIN) or product name hash if ASIN missing
- **Order ID**: Amazon order identifier
- **Order Date**: ISO timestamp (e.g., "2025-10-13T22:08:07Z")
- **Product Name**: Item description/title
- **Quantity**: Number of items ordered; a blank value is treated as 1 for older exports
- **Total Owed**: Final amount paid (after tax)
- **Unit Price**: Optional item price before tax
- **Currency**: Optional currency code, validated against the profile binding when present
- **Order Status**: "Closed", "New", "Cancelled", etc.
- **Shipment Status**: "Shipped", "Delivered", etc.

### Category Assignment

New purchases start in Uncategorized. You can edit them locally after import, or clone another
profile's committed taxonomy during Amazon profile creation.

One Amazon profile supports one currency. A candidate containing conflicting currency codes is
rejected atomically; use separate profiles for multi-currency histories.

## Features

### Automatic Deduplication

Moneyflow reconciles order-local multisets using order ID, ASIN or an ASIN-less key, date, product,
quantity, exact minor-unit amount, and currency. Stable local IDs preserve user edits across status
changes and unambiguous corrections.

```bash
# First import
moneyflow provider import amazon ~/Downloads/"Your Orders" --profile "Amazon Orders"

# Re-import (safe!)
moneyflow provider import amazon ~/Downloads/"Your Orders" --profile "Amazon Orders"
```

### Transaction Linking

When you use Amazon mode alongside a primary financial backend, moneyflow can
automatically link Amazon orders to transactions in your bank accounts.

#### Amazon Column in Transaction View

When viewing transactions where ALL merchants are Amazon-like (e.g., after searching for "Amazon" or drilling into
Amazon), an **Amazon** column appears showing matched products:

![Amazon matching column](../assets/screenshots/amazon-matching-column.svg)

The column shows:

- **✓ Product Name** - Exact match found (amount matches within $0.02)
- **~ Product Name** - Likely match found (fuzzy matching for gift card scenarios)
- **...** - Still loading (matches are loaded lazily as you scroll)
- *(blank)* - No matching order found

#### Three-Pass Matching

moneyflow uses intelligent matching to find the right Amazon order:

1. **Exact Order Matching** - Transaction amount matches order total (within $0.02)
2. **Fuzzy Matching** - For gift card scenarios where transaction < order total (within max($15, 10% of order))
3. **Item-Level Matching** - When Amazon charges items separately, matches individual item amounts

This handles common scenarios like:

- Using a gift card for part of a purchase (shows as `~`)
- Split charges where Amazon bills items separately
- Multiple items in a single order

#### Transaction Details View

Press ++i++ on any Amazon transaction to see full order details:

```text
Matching Amazon Orders
───────────────────────────────────────
Order: 113-1234567-8901234*
Date: 2025-01-10 | From: amazon
  USB-C Cable (x2): -$12.99
  Wireless Mouse: -$24.99
  Total: -$37.98
───────────────────────────────────────
```

The `*` indicates a high-confidence match (exact amount and close date).

#### Searching by Product Name

The text search (++slash++) also searches Amazon product names! Search for "kindle" to find all
transactions where you purchased Kindle-related items, even if the merchant shows as "AMZN MKTP US".

**Requirements:**

- Import your Amazon purchase history first (`moneyflow provider import amazon`)
- Transaction must have "amazon" or "amzn" in the merchant name
- Amount and date must be within tolerance (7 days)

This feature helps you identify exactly what items were in each Amazon charge, making categorization easier.

### Incremental Imports

Amazon mode supports incremental imports, preserving any manual edits you've made:

1. Import initial data export
2. Edit categories and item names in the UI
3. Request and import a fresh data export from Amazon (with new purchases)
4. Overlapping rows update provider facts while stable local IDs preserve your edits
5. Orders present in the new export reconcile authoritatively; orders outside its period remain

### Database Location

**Default Location**:

Amazon data is stored in your profile directory:

```text
~/.moneyflow/v2/profiles/<profile-id>/moneyflow.db
```

This integrates Amazon with your other accounts and allows selection from the account picker.

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

### Amazon profile is empty when launching

**Cause**: No data has been imported yet.

**Solution**: Import your data first:

```bash
moneyflow provider import amazon ~/Downloads/"Your Orders" --profile "Amazon Orders"
```

### Import reports no inserted or updated transactions

**Cause**: All transactions already exist in the database.

**Solution**:

- This is expected if you're re-importing the same data
- No-op imports append counts-only history without changing the profile revision
- Use the profile selector's explicit recovery flow only for an incompatible preview schema

### Missing ASIN for some items

**Cause**: Some Amazon items don't have ASINs (e.g., digital content, gift cards).

**Solution**: moneyflow automatically generates a pseudo-ASIN from the product name hash. This is normal and doesn't
affect functionality.

## Tips

- **Review CLI counts**: Each import reports inserted, updated, restored, and retired rows
- **Safe to edit**: Amazon edits and commits are local only
- **Use separate profiles**: Keep different currencies or analyses in distinct catalog profiles
- **Re-import periodically**: Request fresh exports from Amazon to get new orders
- **Filter by status**: Use order status and shipment status to find specific orders

## Questions?

See the main [documentation](../index.md) or [open an issue](https://github.com/wesm/moneyflow/issues).
