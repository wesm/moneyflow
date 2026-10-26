# SimpleFIN

Connect a SimpleFIN server to import posted bank and card transactions, then review and edit
them in the browser, the terminal, or an MCP client. Every edit commits locally; Moneyflow never
sends changes to your bank or to SimpleFIN. Support is **experimental**: automated tests use
synthetic servers, and live-bank testing is pending.

## What stays local?

All of it. Merchant names, categories, report visibility, deletions, and category and group
changes commit to the local profile. Categories and groups are managed in Moneyflow, not imported
from SimpleFIN. See [moving to Go](../getting-started/transition.md) before replacing a Python
profile.

## Connect a profile

You need a SimpleFIN setup token or an HTTPS Access URL. The browser and the terminal use the
same steps:

1. Run `moneyflow web` or `moneyflow tui`.
2. Choose **Add profile** (`a`), then **SimpleFIN (experimental)** (`s`).
3. Enter a profile name.
4. Choose the three-letter currency of the connected accounts, such as `USD`.
5. Paste the setup token or Access URL into the masked field.
6. Wait for the first import to finish.

Moneyflow claims a setup token once, then saves the returned Access URL and the money settings
before it fetches transactions. SimpleFIN in Go uses no encryption password, and Python's
`moneyflow simplefin` command does not exist. The [SimpleFIN CLI reference](simplefin-cli.md)
covers command-line setup and an optional public-demo check.

Moneyflow never rounds an unrepresentable amount or uses floating point. Mixed currencies,
custom currency identifiers, and partial-account errors stop the import.

## Storage and recovery

The profile keeps its Access URL and money settings in `providers/simplefin/session.json`. The
file is owner-private but **not password-encrypted**, and the SQLite database is not encrypted
either. Keep both on storage your account controls. Do not share the session file or a profile
backup.

A connected profile stays bound to its original Access URL and money settings. If the local file
is lost, restore that same Access URL. Setup then keeps the profile's currency and decimal places
and asks only for the Access URL. Replacement credentials need a **new profile**; they cannot
silently rebind existing transaction identities. Cached data and local editing stay
available when credentials are absent, revoked, or temporarily unavailable.

## Import and refresh

The first import requests up to 1,095 days of history in 90-day windows. Later imports overlap
the last successful import by 14 days. Actual history depends on the institution and SimpleFIN;
the requested window does not guarantee completeness. Pending transactions are excluded.

Refresh only adds. It inserts newly observed posted transactions and leaves existing rows alone:

- Local merchant and category edits, hide flags, deletion records, and undo/redo history
  survive refresh and restart.
- Upstream corrections to already imported rows are **not applied**. There is no replacement
  or hard-refresh mode.
- New transactions start in Uncategorized.

### Refresh timing

Press `r` in the browser or the terminal, or select **Refresh** in the browser's sidebar, to
start a manual refresh.

| Refresh | Timing |
| --- | --- |
| Automatic | Due 24 hours after the last success, while `moneyflow tui` or `moneyflow web` runs |
| Manual | At least one hour apart, including after a failed or canceled fetch |
| After a failed automatic attempt | Parked until you retry explicitly; reopening the app does not retry |
| MCP | Only when a client calls `refresh_data`; MCP has no schedule |

A rate-limit response can extend the wait through `Retry-After`. Wait until the next eligible
time Moneyflow shows.

## Edit and commit

Use the usual edit keys, which the browser and the terminal share, then press `w` to review and
Enter to commit. Commit writes the local journal into SQLite; it does not create a bank-write
batch. The same rule applies to [MCP](mcp.md), where edit tools require `--allow-write` and an
explicit review and commit.

## Troubleshooting

- **Saving a claimed connection fails:** retry saving while setup is still open. Do not claim
  the token again.
- **Import fails after the connection saved:** reopen the same profile and retry the import when
  it is eligible. The saved connection is reused.
- **You cancel setup during a claim:** Moneyflow waits for the claim to finish and saves a
  returned credential before it closes.
- **The claim outcome is unknown, or its credential could not be saved:** revoke the unused
  connection at SimpleFIN and get a new token.
- **Moneyflow refuses the data folder:** it holds Python app files. See
  [moving Python data](../getting-started/transition.md#move-python-data-out-of-moneyflow).

## Help validate an institution

1. Compare imported dates, exact amounts, and counts with your institution.
2. Make local edits and a local deletion.
3. Refresh when eligible, then close and reopen Moneyflow.
4. Check that the edits persist and the deleted row does not return.

Report the Moneyflow version, OS, sanitized error code, and reproduction steps. **Do not post
tokens, Access URLs, account identifiers, or raw financial responses.** Confirmed bugs become
synthetic regression tests. The experimental label comes off only after maintainers review that
evidence.
