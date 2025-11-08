# Category Configuration

## Overview

moneyflow automatically uses your backend's category structure with **profile-local isolation**:

- **Monarch Money** - Fetches your actual Monarch categories and saves to your Monarch profile
- **YNAB** - Fetches your actual YNAB budget categories and saves to your YNAB profile
- **Amazon Mode** - Inherits categories from your primary backend (Monarch/YNAB) or uses built-in defaults
- **Demo Mode** - Uses built-in default categories

**No manual configuration needed!** Categories are automatically synced from your finance platform and isolated per account.

---

## How It Works

### Profile-Local Category Storage

Each account has its own category configuration:

```text
~/.moneyflow/
  ├── config.yaml                          # Global settings (optional)
  └── profiles/
      ├── monarch1/
      │   └── config.yaml                  # Monarch categories (auto-synced)
      ├── ynab1/
      │   └── config.yaml                  # YNAB categories (auto-synced)
      └── amazon/
          └── config.yaml                  # Amazon categories (optional, inherits by default)
```

**Benefits:**

- ✅ No category conflicts between accounts
- ✅ Different Monarch and YNAB category structures work independently
- ✅ Each account maintains its own category customizations

### For Monarch Money and YNAB Users

On every startup, moneyflow:

1. **Fetches your categories** from Monarch/YNAB API
2. **Saves them to `~/.moneyflow/profiles/{account_id}/config.yaml`**
3. **Uses them throughout the session** for grouping and filtering

Your categories are always up-to-date with your finance platform. If you add or rename categories in
Monarch or YNAB, they'll automatically appear in moneyflow on next launch.

**Example profile config.yaml (auto-generated):**

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

Amazon mode uses smart category inheritance:

**Priority order:**

1. **Profile-local config** - If `~/.moneyflow/profiles/amazon/config.yaml` exists, use it
2. **Explicit source** - If `amazon_categories_source` is set in global config, inherit from that profile
3. **Auto-inherit** - If only ONE Monarch/YNAB profile exists, inherit from it automatically
4. **Built-in defaults** - If multiple profiles exist or none configured

#### Example: Single Monarch Account + Amazon

```text
~/.moneyflow/profiles/
  ├── monarch1/config.yaml          # Has Food, Shopping, etc.
  └── amazon/                       # No config.yaml
      └── amazon.db
```

Result: Amazon automatically inherits Monarch's categories. Your Amazon purchases use the same category structure!

#### Example: Multiple Accounts + Amazon

```text
~/.moneyflow/profiles/
  ├── monarch1/config.yaml          # Has Monarch categories
  ├── ynab1/config.yaml             # Has different YNAB categories
  └── amazon/                       # No config.yaml
```

Result: Amazon uses built-in defaults (can't auto-pick between Monarch and YNAB).

#### Explicit Control

To specify which profile Amazon should inherit from, add to global `~/.moneyflow/config.yaml`:

```yaml
version: 1
amazon_categories_source: monarch1  # Use monarch1's categories for Amazon
```

### For Demo Mode Users

Demo mode always uses the built-in default categories (~60 categories in 15 groups). This provides a consistent
demo experience.

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

## Global Configuration

The global `~/.moneyflow/config.yaml` is for application-wide settings only:

```yaml
version: 1

# Optional: Specify which profile Amazon should inherit categories from
amazon_categories_source: monarch1

# Other global settings can go here
```

**What goes in global config:**

- `amazon_categories_source` - Explicit category inheritance for Amazon
- Application-wide preferences (future)
- Theme settings (future)

**What does NOT go in global config:**

- ❌ Categories (these are profile-local)
- ❌ Credentials (these are profile-local)
- ❌ Backend-specific settings (these are profile-local)

---

## Troubleshooting

### Categories don't match my Monarch/YNAB account

**Solution:** Restart moneyflow. Categories are fetched fresh on every startup.

### I want to use Monarch categories in Amazon mode

**Solution:** If you have only one Monarch profile, Amazon will automatically inherit its categories. If you have
multiple profiles, add this to `~/.moneyflow/config.yaml`:

```yaml
version: 1
amazon_categories_source: monarch1  # Replace with your profile ID
```

### I see "Using built-in default categories" in logs

This is normal for:

- First run before connecting to Monarch/YNAB
- Demo mode
- Amazon mode with multiple other profiles (and no explicit source configured)

To use your actual categories for Amazon, either have only one other profile or configure
`amazon_categories_source`.

### How do I reset a profile's categories to defaults?

Delete the profile's config file:

```bash
# Reset specific profile
rm ~/.moneyflow/profiles/monarch1/config.yaml

# Categories will be re-fetched from backend on next startup
```

### Categories are different between my accounts

This is expected! Each profile has its own category structure. Your Monarch categories won't interfere with your
YNAB categories.

---

## Technical Details

**Storage location:** `~/.moneyflow/profiles/{account_id}/config.yaml`

**Update frequency:** On every Monarch/YNAB startup (keeps categories in sync with backend)

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
