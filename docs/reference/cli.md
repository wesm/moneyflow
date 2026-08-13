# CLI Reference

Complete reference for all command-line options.

## Global Commands

| Command | Description |
|---------|-------------|
| `moneyflow` | Launch TUI with default backend (Monarch Money) |
| `moneyflow --demo` | Launch with demo data |
| `moneyflow --year <YYYY>` | Load from specific year onwards |
| `moneyflow --since <YYYY-MM-DD>` | Load from specific date |
| `moneyflow --mtd` | Load month-to-date only |
| `moneyflow --no-cache` | Disable transaction caching |
| `moneyflow --cache [PATH]` | Enable caching (optional path) |
| `moneyflow --refresh` | Force API refresh, skip cache |

## Import Commands

Import transactions from CSV exports.

### `moneyflow import list`

List all available institution mappings.

```bash
moneyflow import list
```

Output:

```text
Available institution mappings:

  chase_credit         Chase Credit Card
```

### `moneyflow import institution`

Import CSV files for a specific institution.

```bash
moneyflow import institution <institution> <path> [--account <label>] [--force]
```

**Arguments:**

| Argument | Description |
|----------|-------------|
| `<institution>` | Institution identifier (e.g., `chase_credit`) |
| `<path>` | CSV file, or directory containing CSV files |

**Options:**

| Option | Description |
|--------|-------------|
| `--account <label>` | Account/card label stored on each transaction. Required when importing multiple cards of the same institution — without it, transactions from different cards sharing a date, amount, and merchant are silently deduplicated. Import each card from its own file or card-specific directory. |
| `--force` | Re-import already-imported files |
| `--config-dir <path>` | Custom config directory (default: `~/.moneyflow`) |

**Example:**

```bash
moneyflow import institution chase_credit ~/Downloads/
moneyflow import institution chase_credit ~/Downloads/ --force

# Two cards of the same institution: one import per card, each with its own label
moneyflow import institution chase_credit ~/Downloads/Chase1234_Activity.csv --account personal_card
moneyflow import institution chase_credit ~/Downloads/Chase5678_Activity.csv --account business_card
```

## Backend-Specific Commands

### Amazon (`moneyflow amazon`)

| Subcommand | Description |
|------------|-------------|
| `moneyflow amazon` | Launch Amazon mode TUI |
| `moneyflow amazon import <path>` | Import Amazon orders from CSV |
| `moneyflow amazon status` | Show database statistics |

**Options:**

| Option | Description |
|--------|-------------|
| `--db-path <path>` | Custom SQLite database path |
| `--config-dir <path>` | Custom config directory |

### CSV Institutions

Each registered CSV institution is available as a top-level command:

```bash
moneyflow chase_credit
```

Launches the TUI for that institution's imported data.

### SimpleFIN (`moneyflow simplefin`)

| Subcommand | Description |
|------------|-------------|
| `moneyflow simplefin` | Launch SimpleFIN mode TUI |
| `moneyflow simplefin login` | Set up or update SimpleFIN connection |
| `moneyflow simplefin default` | Manage default SimpleFIN profile |
| `moneyflow simplefin default --set <id>` | Set default profile |
| `moneyflow simplefin refresh` | Refresh data from SimpleFIN API |

**Options:**

| Option | Description |
|--------|-------------|
| `--profile <id>` | Use specific SimpleFIN profile |
| `--config-dir <path>` | Custom config directory |

## Global Options

Flags available on any command:

| Option | Description |
|--------|-------------|
| `--help` | Show help for the current command |

## See Also

- [Amazon Mode](../guide/amazon-mode.md) — Amazon purchase analysis
- [CSV Import](../guide/csv-import.md) — Generic CSV import engine
- [SimpleFIN](../guide/simplefin.md) — SimpleFIN open banking
- [SimpleFIN CLI Reference](../guide/simplefin-cli.md) — Detailed SimpleFIN CLI docs
