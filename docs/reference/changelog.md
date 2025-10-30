# Changelog

## Unreleased

**New:**

- **YNAB support** - Full integration with You Need A Budget
- `--config-dir` option for custom configuration directory
- Nix flake for reproducible builds
- Green styling for credits/refunds
- Right-justified dollar amounts

**Fixed:**

- Crash when quitting during credential screen
- Empty account (0 transactions) load error
- Log path in error messages when using `--config-dir`

---

## v0.5.3 - October 2025

**New:**

- Duplicates screen deletes immediately from backend with real-time table updates
- Progress notifications for batch delete operations

**Fixed:**

- Cache now updates after deletions (prevents deleted transactions from reappearing)
- Multi-select 3x faster on large views (8,000+ transactions)
- Log files no longer expose transaction data

---

**Upgrade**: `pip install --upgrade moneyflow` or `uvx moneyflow@latest`

**Full history**: [CHANGELOG.md](https://github.com/wesm/moneyflow/blob/main/CHANGELOG.md)
