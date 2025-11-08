# Changelog

## v0.7.0 - November 2025

**New:**

- **Time as first-class grouping** - Group by Year/Month/Day alongside Merchant/Category/Account
- **Day granularity** - New granularity level for daily spending analysis
- **Time toggle with 't' key** - Cycle through Year/Month/Day granularities
- **Multi-account support** - Manage multiple Monarch, YNAB, and Amazon accounts from single interface
- **Account selector** - Choose accounts on startup with keyboard navigation (↑/↓, Enter, j/k)
- **Profile-local categories** - Each account maintains its own category structure (no conflicts)
- **Amazon category inheritance** - Auto-inherits categories from Monarch/YNAB if only one profile exists
- **YNAB:** Batch payee updates - Rename merchant once instead of updating each transaction (100x faster)
- **YNAB:** Support for non-USD currencies - Display currency symbol from YNAB budget settings
- Currency symbol in column header (no longer in each amount)
- Amazon mode integrates with account selector (appears alongside Monarch/YNAB)

**Fixed:**

- 'g' key now returns to aggregate view from top-level detail view
- Breadcrumb dimensions now display in drill-down order
- Sort by amount (descending) when drilling into time periods
- Arrow key bindings now work correctly in account/backend selectors (priority over scroll)
- Test suite no longer clobbers production config.yaml

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
