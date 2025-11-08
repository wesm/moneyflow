# Changelog

All notable changes to moneyflow will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/).

## [0.7.0] - 2025-11-08

### Added

- **Multi-account support** - Manage multiple Monarch, YNAB, and Amazon accounts from single interface
- **Account selector** - Choose accounts on startup with keyboard navigation (↑/↓, Enter, j/k)
- **Profile-local categories** - Each account maintains its own category structure (no conflicts)
- **Amazon category inheritance** - Auto-inherits categories from Monarch/YNAB if only one profile exists
- **YNAB:** Batch payee updates - Rename merchant once instead of updating each transaction (100x faster)
- Keyboard navigation for backend and account selection screens

### Fixed

- **YNAB:** Arrow key bindings now work in account selector (priority over scroll)

## [0.6.0] - 2025-10-29

### Added
- **YNAB support** - Full integration with You Need A Budget (editing, sync, categories)
- `--config-dir` option to specify custom configuration directory
- Nix flake for reproducible builds and installation
- Categories from backend now stored in `config.yaml` for local reference
- Green styling for positive amounts (credits) for visual distinction
- Right-justified dollar amounts with improved +/- sign placement

### Fixed
- Crash when quitting during credential screen
- Empty YNAB account (0 transactions) crash on load
- Error messages now show correct log file path when using `--config-dir`

## [0.5.3] - 2025-10-26

### Added
- Duplicates screen deletes immediately from backend with real-time table updates
- Progress notifications for batch delete operations (e.g., "Deleting... 10/51 complete")

### Fixed
- Cache now updates after deletions (prevents deleted transactions from reappearing)
- Multi-select operations 3x faster on large views (8,000+ transactions)
- Log files no longer expose transaction data
- Duplicates screen delete workflow (NoActiveWorker error, consistent keybindings)

---

For detailed commit history, see [GitHub commits](https://github.com/wesm/moneyflow/commits/main).
