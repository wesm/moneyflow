# Profiles, storage, and refresh

Moneyflow opens saved profile data for browsing. Provider refresh is a separate operation.
Local edits are stored as a journal: an ordered list of pending changes with an undo/redo cursor.

## Where is my data?

The default catalog is `~/.moneyflow/v2`. Set `MONEYFLOW_HOME` to choose another catalog.
Each named profile lives under `profiles/<profile-id>/` and includes `moneyflow.db`.
Credential and session files are separate from that database.

The database is not application-encrypted. Use storage and backups appropriate for financial
data. The [provider guides](../index.md) explain which credentials are encrypted and which
session files are only owner-private.

## When does data refresh?

Press `r` in TUI or web. Amazon asks for files; network providers start a refresh.
Monarch refresh downloads complete history and only starts when requested. Opening a
profile, browsing, reconnecting, and saving edits do not trigger a Monarch refresh.
Successful edits update saved transactions without invalidating the rest of the cache.

TUI and web schedule eligible YNAB and SimpleFIN refreshes while running.
MCP refresh is always explicit. Recovering an interrupted write with **Stop and reconcile**
also reloads provider data.

See [Monarch](../guide/monarch.md#refresh-data),
[YNAB](../guide/ynab.md), and
[SimpleFIN](../guide/simplefin.md#refresh-timing) for provider-specific behavior.
Do not treat a local date filter as a restriction on remote fetches.

## Back up a profile

1. Stop all Moneyflow processes using the profile.
2. Copy the complete profile directory to a protected backup location.
3. Keep any `moneyflow.db-wal` and `moneyflow.db-shm` files with the database if present.
4. Protect the backup as credentials and financial data, not just a transaction export.

Do not copy only a live database file. Do not delete data to resolve an authentication problem.
For incompatible preview formats, use the selector's explicit recovery flow; see
[moving to Go](../getting-started/transition.md#what-about-older-go-previews).
