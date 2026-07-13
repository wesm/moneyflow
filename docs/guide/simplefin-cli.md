<!-- markdownlint-disable-file MD024 -->
# SimpleFIN CLI Reference

Complete reference for all `moneyflow simplefin` commands and their options.

---

## Overview

The `moneyflow simplefin` command group launches the TUI or runs one of three
subcommands:

| Subcommand | Purpose |
|---|---|
| [`simplefin` (group)](#the-simplefin-group) | Launch the TUI with SimpleFIN backend |
| [`default`](#simplefin-default) | View or manage the default SimpleFIN profile |
| [`refresh`](#simplefin-refresh) | Fetch latest transactions from the API |
| [`status`](#simplefin-status) | Show database statistics |

All SimpleFIN operations are accessed through the `simplefin` subcommand group.
See [Quick Reference](#quick-reference) for common commands.

---

## The `simplefin` Group

```bash
moneyflow simplefin [OPTIONS] [COMMAND]
```

Launches the terminal UI with the SimpleFIN backend, skipping the backend
picker. When a subcommand (`default`, `refresh`, `status`) is given, that
subcommand runs instead.

### Options

| Option | Description |
|---|---|
| `--year YYYY` | Only load transactions from this year onwards (e.g., `--year 2025`) |
| `--since YYYY-MM-DD` | Only load transactions from this date onwards (overrides `--year`) |
| `--mtd` | Load month-to-date transactions (from 1st of current month) |
| `--config-dir PATH` | Root configuration directory (default: `~/.moneyflow`) |
| `--no-cache` | Ignored — SimpleFIN uses SQLite, not the cache system |
| `--profile PROFILE_ID` | Use the specified SimpleFIN profile instead of the default |

### Examples

```bash
# Launch the TUI with default profile
moneyflow simplefin

# Launch with a specific profile
moneyflow simplefin --profile my-business

# Limit to transactions from 2025 onward
moneyflow simplefin --year 2025

# Use a custom config directory
moneyflow simplefin --config-dir /path/to/config
```

---

## `simplefin default`

```bash
moneyflow simplefin default [OPTIONS]
```

View or manage the default SimpleFIN profile. With no arguments, lists all
available SimpleFIN profiles and marks the current default.

### Options

| Option | Description |
|---|---|
| `--set PROFILE_ID` | Set the named profile as the default |
| `--clear` | Clear the current default selection |
| `--config-dir PATH` | Root configuration directory (default: `~/.moneyflow`) |

### Examples

```bash
# List all profiles and show the current default
moneyflow simplefin default

# Set a profile as the default
moneyflow simplefin default --set personal-checking

# Clear the default (forces profile selection on next launch)
moneyflow simplefin default --clear
```

---

## `simplefin refresh`

```bash
moneyflow simplefin refresh [OPTIONS]
```

Fetch latest transactions from the SimpleFIN API. By default performs an
**additive merge** — new transactions are added, existing local edits are
preserved. Use `--force` to clear the local database and re-fetch everything
(discards merchant, category-assignment, and hide edits). Local deletion
tombstones remain in effect.

### Options

| Option | Description |
|---|---|
| `--force` | Replace transaction rows from the API; retain deletion tombstones |
| `--config-dir PATH` | Root configuration directory (default: `~/.moneyflow`) |
| `--profile PROFILE_ID` | Use the specified SimpleFIN profile instead of the default |

### Examples

```bash
# Additive refresh (preserves edits)
moneyflow simplefin refresh

# Hard refresh (retains local deletion tombstones)
moneyflow simplefin refresh --force

# Refresh a non-default profile
moneyflow simplefin refresh --profile my-business

# Refresh using a group-level profile flag
moneyflow simplefin --profile my-business refresh
```

!!! note
    When using `--profile` on the group level (`moneyflow simplefin --profile X refresh`),
    the group flag is inherited by the subcommand. You can also pass `--profile`
    directly on the subcommand: `moneyflow simplefin refresh --profile X`.

---

## `simplefin status`

```bash
moneyflow simplefin status [OPTIONS]
```

Show SimpleFIN database statistics: transaction count, date range, total
amount with its ISO 4217 currency code when known, and last refresh timestamp.

### Options

| Option | Description |
|---|---|
| `--config-dir PATH` | Root configuration directory (default: `~/.moneyflow`) |
| `--profile PROFILE_ID` | Use the specified SimpleFIN profile instead of the default |

### Examples

```bash
# Show default profile stats
moneyflow simplefin status

# Show stats for a specific profile
moneyflow simplefin status --profile my-business
```

---

## Quick Reference

The most common SimpleFIN operations use the `simplefin` subcommand group:

| Command | Description |
|---|---|
| `moneyflow simplefin refresh` | Additive refresh (adds new transactions) |
| `moneyflow simplefin refresh --force` | Replace local rows; retain deletion tombstones |
| `moneyflow simplefin status` | View database statistics |
| `moneyflow simplefin default` | Set or view default profile |

---

## Shared Options

### `--profile`

The `--profile` flag is available on the `simplefin` group, `refresh`, and
`status` commands. It overrides the default profile for a single invocation.

| Scenario | Behavior |
|---|---|
| No `--profile`, 0 profiles configured | Error: no SimpleFIN account configured |
| No `--profile`, 1 profile configured | Automatically uses that profile |
| No `--profile`, 2+ profiles with default | Uses the configured default |
| No `--profile`, 2+ profiles no default | Prompts to select and saves as default |
| `--profile X` given, X is a valid SimpleFIN profile | Uses X |
| `--profile X` given, X is invalid or wrong type | Error: profile not found or not SimpleFIN |

### `--config-dir`

All SimpleFIN commands accept `--config-dir` to specify a custom
configuration directory. The default is `~/.moneyflow`.

---

## Exit Codes

| Code | Meaning |
|---|---|
| 0 | Success |
| 1 | Error (invalid profile, no profiles, API failure, etc.) |

---

## See Also

- [SimpleFIN Integration Guide](simplefin.md) — Setup, TUI usage, editing
- [Data Refresh Details](simplefin.md#data-refresh) — How refresh works internally
