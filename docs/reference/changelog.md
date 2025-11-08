# Changelog

## v0.7.0 - November 2025

**New:**

- **Multi-account support** - Manage multiple Monarch, YNAB, and Amazon accounts from single interface
- **Account selector** - Choose accounts on startup with keyboard navigation (↑/↓, Enter, j/k)
- **Profile-local categories** - Each account maintains its own category structure (no conflicts)
- **Amazon category inheritance** - Auto-inherits categories from Monarch/YNAB if only one profile exists
- **YNAB:** Batch payee updates - Rename merchant once instead of updating each transaction individually
- Keyboard navigation for backend and account selection screens
- Amazon mode integrates with account selector (appears alongside Monarch/YNAB)

**Fixed:**

- **YNAB:** Arrow key bindings now work in account selector (priority over scroll)

---

## v0.6.0 - October 2025

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
