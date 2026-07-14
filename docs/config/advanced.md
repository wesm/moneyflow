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

Transaction data is cached locally by default for fast startup. The cache is encrypted with the same key as your
credentials.

**Cache behavior:**

- First run: Downloads all transactions from your backend
- Subsequent runs: Uses cached data instantly
- Cache auto-refreshes when you make edits that sync to the backend

**Options:**

```bash
moneyflow --refresh            # Force refresh from API (ignore cache)
moneyflow --no-cache           # Disable caching entirely for this session
```

**See:** [Caching Guide](caching.md) for details on cache location and management.

## Configuration Directory

All moneyflow configuration is stored in `~/.moneyflow/`:

```text
~/.moneyflow/
├── config.yaml        # Application configuration (categories, settings, etc.) - optional
├── credentials.enc    # Encrypted credentials (when encryption is enabled)
├── credentials.json   # Plaintext credentials (when encryption is disabled)
├── salt               # Encryption salt
├── merchants.json     # Merchant name cache
├── cache/             # Encrypted transaction cache (Monarch/YNAB)
├── profiles/          # Per-account profile directories
│   └── <profile-id>/
│       ├── credentials.enc    # Encrypted credentials for this account
│       ├── credentials.json   # Plaintext credentials (when encryption is disabled)
│       ├── salt               # Encryption salt
│       ├── config.yaml        # Profile-specific configuration
│       ├── merchants.json     # Per-profile merchant cache
│       ├── simplefin.db       # SimpleFIN local SQLite database
│       ├── last_update.json   # Last refresh timestamp
│       └── cache/             # Per-profile transaction cache (Monarch/YNAB)
└── moneyflow.log      # Application logs
```

**Security note:** `credentials.enc` is encrypted with AES-128 but still contains
sensitive material and should be kept private. `credentials.json` is plaintext —
restrict file permissions (0600 is set automatically) and avoid backing it up to
untrusted locations.
