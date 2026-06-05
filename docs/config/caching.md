# Caching

moneyflow caches your transaction data locally for fast startup. Caching is **enabled by default**.

## How It Works

1. **First run**: Downloads all transactions from your backend (Monarch Money, YNAB, etc.)
2. **Subsequent runs**: Loads instantly from encrypted local cache
3. **Auto-refresh**: Cache updates when you commit changes to your backend

## Cache Location

```text
~/.moneyflow/cache/
```

Or within your profile directory if using multiple accounts:

```text
~/.moneyflow/profiles/<profile-name>/cache/
```

## Security

The cache is **encrypted** using the same AES-128 encryption as your credentials. Your transaction data is never
stored in plain text.

## CLI Options

| Option | Description |
|--------|-------------|
| `--refresh` | Force download from API, ignoring cache |
| `--no-cache` | Disable caching for this session |

## Common Scenarios

### Force a fresh download

```bash
moneyflow --refresh
```

Use this when you've made changes directly in your finance platform and want to see them immediately.

### Troubleshoot cache issues

```bash
# Run without cache to see if issue is cache-related
moneyflow --no-cache

# Or delete the cache directory
rm -rf ~/.moneyflow/cache/
```

### Check cache status

The cache status is shown in the application logs at `~/.moneyflow/moneyflow.log`.

---

## Cache File Format

The cache uses a **two-tier** system to balance freshness with API call frequency.
Each tier stores the same Polars DataFrame schema (as Parquet) alongside JSON metadata.

### File Layout

```text
~/.moneyflow/cache/
  ├── cache_metadata.json          # Unencrypted, fast validation
  ├── hot_transactions.parquet     # Recent 90 days (or .parquet.enc if encrypted)
  ├── cold_transactions.parquet    # Historical data (or .parquet.enc if encrypted)
  └── categories.json              # Category hierarchy (or .json.enc if encrypted)
```

### Transaction DataFrame Schema

Both hot and cold tiers store identical columns in Parquet format:

| Column | Type | Description |
|--------|------|-------------|
| `id` | `str` | Unique transaction ID from backend |
| `date` | `pl.Date` | Transaction date |
| `amount` | `float` | Transaction amount |
| `merchant` | `str` | Merchant name (defaults to `"Unknown"`) |
| `merchant_id` | `str` | Backend merchant ID |
| `category` | `str` | Category name (defaults to `"Uncategorized"`) |
| `category_id` | `str` | Backend category ID |
| `account` | `str` | Account display name |
| `account_id` | `str` | Backend account ID |
| `notes` | `str` | Transaction notes |
| `hideFromReports` | `bool` | Hidden-from-reports flag |
| `pending` | `bool` | Pending transaction flag |
| `isRecurring` | `bool` | Recurring transaction flag |
| `*` | `str` | Backend-specific extra fields (e.g. Amazon: `quantity`, `asin`, `order_id`), coerced to strings |

The `group` column is **not stored** in the Parquet cache. It is derived dynamically from the `category` column
at load time so that changes to `config.yaml` category groupings take effect on cached data.

### Tiers

| Tier | Time Range | Max Age | File |
|------|------------|---------|------|
| **Hot** | Last 90 days (rolling) | 6 hours | `hot_transactions.parquet[.enc]` |
| **Cold** | All older transactions | 30 days | `cold_transactions.parquet[.enc]` |

The hot tier refreshes frequently (6h) to catch recent edits. The cold tier is
stable and only refreshes every 30 days. A 30-day overlap between tiers ensures
no gaps when the cold cache expires.

### Categories JSON

```json
{
  "categories": {
    "<category_id>": {
      "name": "<string>",
      "group": "<string|null>",
      "group_id": "<string|null>",
      "group_type": "<string|null>"
    }
  },
  "category_groups": {
    "<group_id>": {
      "name": "<string>",
      "type": "<string>"
    }
  }
}
```

Fetched fresh from the backend API and stored alongside the transaction tiers.

### Metadata (unencrypted)

`cache_metadata.json` is stored in plain text for fast validation without
decryption overhead:

```json
{
  "version": "3.0",
  "hot": {
    "fetch_timestamp": "<ISO datetime>",
    "transaction_count": <int>,
    "earliest_date": "<YYYY-MM-DD|null>",
    "latest_date": "<YYYY-MM-DD|null>",
    "boundary_date": "<YYYY-MM-DD>"
  },
  "cold": {
    "fetch_timestamp": "<ISO datetime>",
    "transaction_count": <int>,
    "earliest_date": "<YYYY-MM-DD|null>",
    "latest_date": "<YYYY-MM-DD|null>"
  },
  "year_filter": <int|null>,
  "since_filter": "<string|null>",
  "total_transactions": <int>,
  "encrypted": <bool>
}
```

The boundary date separates the hot and cold tiers. It is computed as
`today - 90 days` and stored at save time. This ensures that even if the
cache is loaded days later, the split boundary reflects the original
save date.
