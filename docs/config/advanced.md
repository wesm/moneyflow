# Advanced Configuration

## Category Customization

Customize the category hierarchy to match your finance platform or workflow preferences.

**📁 Configuration file:** `~/.moneyflow/config.yaml`

**Quick commands:**

```bash
moneyflow categories dump              # View current hierarchy (YAML format)
moneyflow categories dump --format=readable  # View with counts
```

**Features:**

- Add custom categories from your finance platform
- Rename groups or categories
- Reorganize categories into different groups
- Create custom groups

**Built-in defaults**: The included categories were chosen to ease integration with Monarch Money but work well for
most personal finance platforms.

**See:** [Category Configuration Guide](../categories.md) for complete documentation.

## Data Caching

Speed up startup by caching transaction data locally.

**Usage:**

```bash
moneyflow --cache              # Enable caching (uses ~/.moneyflow/cache/)
moneyflow --cache ~/my-cache   # Custom cache location
moneyflow --refresh            # Force refresh, skip cache
```

**See:** [Caching Guide](caching.md) for details.

## Configuration Directory

All moneyflow configuration is stored in `~/.moneyflow/`:

```text
~/.moneyflow/
├── config.yaml        # Application configuration (categories, settings, etc.) - optional
├── credentials.enc    # Encrypted credentials
├── salt               # Encryption salt
├── merchants.json     # Merchant name cache
├── cache/             # Transaction cache (if --cache enabled)
└── moneyflow.log      # Application logs
```

**Security note:** credentials.enc is encrypted with AES-128. Safe to backup but keep private.
