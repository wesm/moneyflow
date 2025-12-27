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
