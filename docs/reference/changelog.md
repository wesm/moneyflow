# Changelog

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
