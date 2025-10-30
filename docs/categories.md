# Category Configuration

## Overview

moneyflow automatically uses your backend's category structure:

- **Monarch Money** - Fetches your actual Monarch categories on every startup
- **YNAB** - Fetches your actual YNAB budget categories on every startup
- **Amazon Mode** - Uses categories from your Monarch/YNAB account (if previously configured), otherwise uses built-in defaults
- **Demo Mode** - Uses built-in default categories

**No manual configuration needed!** Categories are automatically synced from your finance platform.

---

## How It Works

### For Monarch Money and YNAB Users

On every startup, moneyflow:

1. **Fetches your categories** from Monarch/YNAB API
2. **Saves them to `~/.moneyflow/config.yaml`** under `fetched_categories`
3. **Uses them throughout the session** for grouping and filtering

Your categories are always up-to-date with your finance platform. If you add or rename categories in
Monarch or YNAB, they'll automatically appear in moneyflow on next launch.

**Example config.yaml (auto-generated):**

```yaml
version: 1
fetched_categories:
  Food & Dining:
    - Groceries
    - Restaurants & Bars
    - Coffee Shops
  Shopping:
    - Clothing
    - Electronics
  Auto & Transport:
    - Gas
    - Auto Payment
    - Parking
```

### For Amazon Mode Users

When you launch Amazon mode, moneyflow:

1. **Checks for `fetched_categories` in config.yaml**
2. If found (from a previous Monarch/YNAB setup), uses those categories
3. If not found, uses built-in default categories

This means Amazon purchases are categorized using the same category structure as your main finance platform.

### For Demo Mode Users

Demo mode always uses the built-in default categories (~60 categories in 15 groups). This provides a consistent demo experience.

---

## Built-in Default Categories

If no `fetched_categories` exist in config.yaml, moneyflow uses built-in defaults:

- **15 groups**: Income, Food & Dining, Shopping, Auto & Transport, Housing, Bills & Utilities,
  Travel & Lifestyle, Health & Wellness, Children, Education, Gifts & Donations, Financial,
  Business, Uncategorized, Transfers
- **~60 categories**: Groceries, Restaurants, Gas, Shopping, Medical, etc.

These defaults are based on Monarch Money's category structure and work well for most personal finance scenarios.

---

## Viewing Your Categories

**View your current category structure:**

```bash
moneyflow categories dump              # YAML format
moneyflow categories dump --format=readable  # Human-readable with counts
```

This shows the actual categories being used (fetched from backend or defaults).

---

## Advanced: Custom Category Overrides

!!! info "Only for advanced users"
    Most users don't need this. Monarch/YNAB categories are automatically fetched and synced.

You can customize how categories are organized by editing `~/.moneyflow/config.yaml`:

```yaml
version: 1

# Backend categories (auto-populated by Monarch/YNAB)
fetched_categories:
  Food & Dining:
    - Groceries
    - Restaurants

# Optional: Custom overrides (applied on top of fetched_categories)
categories:
  rename_groups:
    "Food & Dining": "Food"
  add_to_groups:
    Food:
      - Fast Food
```

**Available customizations:**

- `rename_groups` - Rename category groups
- `rename_categories` - Rename individual categories
- `add_to_groups` - Add categories to existing groups
- `custom_groups` - Create entirely new groups
- `move_categories` - Move categories between groups

**Which backends write to config.yaml:**

- Monarch/YNAB: Write `fetched_categories` on every startup
- Amazon/Demo: Only read, never write

---

## Troubleshooting

### Categories don't match my Monarch/YNAB account

**Solution:** Restart moneyflow. Categories are fetched fresh on every startup.

### I want to use Monarch categories in Amazon mode

**Solution:** Run moneyflow with Monarch at least once. The categories will be saved to
`config.yaml` and automatically used by Amazon mode.

### I see "Using built-in default categories" in logs

This is normal for:

- First run before connecting to Monarch/YNAB
- Demo mode
- Amazon mode without previous Monarch/YNAB setup

To get your actual categories, connect to Monarch or YNAB.

### How do I reset to defaults?

Delete the fetched categories from config.yaml:

```bash
# Remove fetched_categories section
# Edit ~/.moneyflow/config.yaml and delete the 'fetched_categories:' section

# Or delete entire config
rm ~/.moneyflow/config.yaml
```

---

## Technical Details

**Storage location:** `~/.moneyflow/config.yaml`

**Update frequency:** On every Monarch/YNAB startup (keeps categories in sync)

**Format:**

```yaml
version: 1
fetched_categories:
  Group Name:
    - Category 1
    - Category 2
```

**Category resolution (two-step process):**

1. **Base categories** (one or the other, NOT merged):
   - `fetched_categories` from config.yaml (if present)
   - OR built-in `DEFAULT_CATEGORY_GROUPS` from `categories.py`

2. **Custom overrides** (merged on top of base):
   - `categories` section from config.yaml (if present)
   - Applied via rename_groups, add_to_groups, etc.
