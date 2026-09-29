# SimpleFIN Integration

Use moneyflow with account and transaction data provided by a SimpleFIN server.

## Experimental support

Live-bank testing is pending. Automated tests use synthetic servers; they do not establish
compatibility with every institution. Edits are saved only in Moneyflow, not sent to your bank.
See [moving to Go](../getting-started/transition.md) before replacing a Python profile.

### Connect and Confirm Money Settings

Build with `make build`, then open `moneyflow tui` or `moneyflow web`. Choose
**Add profile**, **SimpleFIN (experimental)**, and a profile name. Confirm the three-letter currency
and decimal places before pasting a setup token or HTTPS Access URL in the masked field.
For USD, choose **USD** and **2** decimal places: `12.34` becomes exactly `1234` minor units.
Moneyflow never rounds an unrepresentable amount or uses floating point. Mixed currencies,
custom currency identifiers, and partial-account errors stop the import.

The [Go CLI instructions](simplefin-cli.md) cover terminal setup and an optional
public-demo smoke check. Tokens are claimed once, and the returned Access URL and money settings
are saved before fetching transactions. Neither a Moneyflow encryption password nor Python's
`moneyflow simplefin` command is used in Go.

### Storage and Recovery

The selected profile stores its Access URL and money settings in `providers/simplefin/session.json`.
This file is owner-private, **not password-encrypted**. The SQLite database is not encrypted either.
Keep both on storage controlled by your account; do not share the session file or profile backup.

If saving a claimed connection fails, retry saving while setup remains open. Do not claim the token
again. If import fails after saving, reopen the same profile and retry import when eligible; the
saved connection is reused. Cancellation waits for an in-flight claim to finish and saves a returned
credential before closing. If the claim outcome is unknown or its credential could not be saved,
revoke the unused connection at SimpleFIN and obtain a new token.

A bound profile is tied to its original Access URL and money settings. Restore that same Access URL
if its local file is lost. Replacement credentials require a **new profile**; they cannot silently
rebind existing transaction identities. Cached data and local editing remain available when
credentials are absent, revoked, or temporarily unavailable.

### Import and Local Editing

Initial import requests up to 1,095 days in bounded 90-day windows. Later imports overlap the last
successful import by 14 days. Actual history depends on the institution and SimpleFIN; the requested
window is not a completeness guarantee. Pending transactions are excluded.

Refresh is additive: it inserts newly observed posted transactions and leaves existing rows
unchanged. Local merchant/category edits, hide flags, deletion tombstones, and undo/redo history
survive refresh and restart. Upstream corrections to already imported rows are **not applied**.
There is no Go hard-refresh replacement mode. New transactions start in Uncategorized; categories
and groups are managed locally, not imported from SimpleFIN.

Use the usual edit controls, then **Pending changes** and **Commit** (`w`, then Enter in the TUI).
Commit writes the local journal into SQLite; it does not create a bank-write batch. The same rule
applies to [MCP](mcp.md), where edit tools require `--allow-write` and explicit review/commit.

### Refresh Timing

While the TUI or web app is running, automatic refresh is due after 24 hours. A failed attempt parks
automatic refresh until an explicit retry; repeatedly reopening the application does not retry it.
Manual attempts have a one-hour minimum interval, including after a failed or canceled fetch.
Rate-limit responses may extend this through `Retry-After`. Wait until the indicated next eligible
time. MCP has no scheduler: its `refresh_data` tool starts only an explicitly requested attempt.

### Help Validate an Institution

Compare imported dates, exact amounts, and counts with your institution. Try local edits and a
local deletion, refresh when eligible, then close and reopen Moneyflow. Check that edits persist
and the deleted row does not return. Report the Moneyflow version, OS, sanitized error code, and
reproduction steps. **Do not post tokens, Access URLs, account identifiers, or raw financial
responses.** Confirmed bugs should become synthetic regression tests. Removing the experimental
label requires maintainer review of that evidence.
