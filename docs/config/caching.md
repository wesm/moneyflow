# Profiles, storage, and refresh

Find your data, check past edits, control refreshes, and back up a profile. Moneyflow works from
the data saved in each profile. It downloads from the provider only when you refresh or a
scheduled refresh is due. Pending edits are saved in the profile as an ordered list that undo and
redo move through.

## Where is my data?

Profiles live in `~/.moneyflow`. Set `MONEYFLOW_HOME` to use another directory. Moneyflow
refuses a directory that holds Python Moneyflow data; see
[moving to Go](../getting-started/transition.md).
Each profile lives under `profiles/<profile-id>/` and includes `moneyflow.db`.
Credential and session files are separate from that database.

The database is not application-encrypted. Use storage and backups appropriate for financial
data. The [provider guides](../index.md) explain which credentials are encrypted and which
session files are only owner-private.

## Where can I inspect past edits?

Each profile keeps `audit.jsonl` beside `moneyflow.db`. It records local
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
| `provider_noop_intent` / `provider_noop_committed` | A net-zero set of edits cleared without provider work, followed by confirmation it was saved |
| `provider_finalize_intent` / `provider_finalized` | The completed batch about to be saved in the local database, followed by confirmation it was saved |
| `provider_reconcile_intent` / `provider_reconciled` | Provider data used for recovery, followed by confirmation it was saved |
| `local_commit_intent` / `local_committed` | Before/requested local edits, followed by confirmation they were saved |

The log contains financial labels and identifiers, is not encrypted, and uses
owner-only file permissions. Keep it with your private backups; do not attach it to a public
issue without removing personal data. Credentials and raw provider response bodies are not
written to it.

For example, inspect a profile's log with `jq . /path/to/profile/audit.jsonl`.
Moneyflow stops before sending further writes if it cannot save their audit records.

## When does data refresh?

Press `r` in the TUI or the browser, or choose **Refresh** in the browser sidebar. Amazon asks for
files; in the browser, use **Import Amazon orders**. Network providers start a refresh. Bank CSV
profiles do not refresh; rerun `moneyflow provider import institution` instead.

For Monarch, `r` downloads the last 90 days unless complete history is due, and `R` downloads
complete history; see [Monarch refresh](../guide/monarch.md#refresh-data). YNAB and SimpleFIN
read complete data on every refresh. Saving edits does not trigger a refresh. Successful edits
update saved transactions without invalidating the rest of the cache.

The TUI and the browser schedule eligible Monarch, YNAB, and SimpleFIN refreshes while running.
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

Do not copy only a live database file. Do not delete data to fix an authentication problem. If
Moneyflow cannot open a profile's database format, see
[troubleshooting](../reference/troubleshooting.md#the-profile-schema-is-incompatible).
