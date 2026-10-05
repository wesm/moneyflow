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

## Where can I inspect past edits?

**Unreleased:** Each profile keeps `audit.jsonl` beside `moneyflow.db`. It records local
commits and provider writes, including transaction IDs, dates, before values, requested
changes, and provider responses. Each line is a JSON object. The file remains after pending
changes and completed write batches are cleared.

Records distinguish a planned change or attempted request from a provider acknowledgment
and a completed local commit. An attempt without an acknowledgment has an unknown outcome;
it is not evidence that the provider rejected the edit. Missing completion records can also
mean the process stopped before it recorded local completion. Do not use either case as a
reason to resend a write blindly.

| Event | Meaning |
| --- | --- |
| `provider_planned` / `provider_attempt` | Before values and requested changes, saved before dispatch |
| `provider_response` | Returned fields or an error classification; the response may still fail validation |
| `provider_acknowledged` | A response accepted by Moneyflow |
| `provider_read_observed` / `provider_read_confirmed` | A targeted recovery read found the requested values, followed by acceptance |
| `provider_read_retry_authorized` | A targeted recovery read found the previous values, permitting a retry |
| `provider_read_unresolved` | A targeted recovery read did not resolve the uncertain update |
| `provider_finalized` | The completed batch was saved in the local database |
| `provider_reconcile_intent` / `provider_reconciled` | Provider data used for recovery, followed by confirmation it was saved |
| `local_commit_intent` / `local_committed` | Before/requested local edits, followed by confirmation they were saved |

The log starts with edits made by versions that include this feature. It cannot reconstruct
earlier edits. It contains financial labels and identifiers, is not encrypted, and uses
owner-only file permissions. Keep it with your private backups; do not attach it to a public
issue without removing personal data. Credentials and raw provider response bodies are not
written to it.

For example, inspect a profile's log with `jq . /path/to/profile/audit.jsonl`.
Moneyflow stops before sending further writes if it cannot save their audit records.

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
