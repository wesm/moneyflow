# Monarch Money

Connect a Monarch household to review transactions and write back supported changes.
Moneyflow imports posted transactions; bank-pending rows do not enter the local profile.

## Connect

1. Run `moneyflow tui` or `moneyflow web` and choose **Add profile**, then **Monarch Money**.
2. Name the profile and confirm its three-letter currency and decimal scale.
3. Enter your Monarch email, password, and Base32 authenticator secret.
4. Create and confirm a Moneyflow account password for the local credential vault.
5. Wait for authentication and import to finish.

The authenticator secret is the setup secret held by your password manager, not a current
six-digit code. Moneyflow uses it to generate verification codes. Secret fields show masked
typing feedback. Keep the secret in your password manager; never include it in bug reports.

For command-line setup:

```bash
moneyflow provider connect monarch --currency USD --scale 2
moneyflow provider connect monarch --profile "Example Profile"
```

Use the selector to add a separate household. A bound profile cannot silently switch its
provider identity or stored money settings.

For a smaller first import, add `--mtd` to `provider connect monarch`. It loads transactions
from the first day of the current month and requires a pristine profile. Later refreshes
load the complete profile; `--mtd` is not an ongoing date filter. **Unreleased:** a
month-to-date import is not a full refresh, so the first
[scheduled refresh](#refresh-data) after it downloads complete history.

## Where are credentials stored?

The selected profile has a password-encrypted `providers/monarch/credentials.enc` vault using Argon2id and
AES-256-GCM. It stores the login credentials and authenticator secret. Session material is
stored separately in `providers/monarch/session.json` with owner-only permissions; that
file is not password-encrypted. Generated verification codes are not persisted.

A valid saved session allows reopening without entering the account password each time.
When it expires, reconnect through setup and unlock the saved credentials. The SQLite
database is not application-encrypted. See [storage](../config/caching.md).

## Refresh data

Press `r`, or **Refresh** in the web app, for a full refresh: Moneyflow downloads complete
history. Successful edits update the local cache directly; saving edits does not start a
refresh.

**Unreleased:** The TUI and the web app also refresh Monarch in the background, as the Python
app (v0.11.1) did:

- **Recent refresh:** when the last refresh is 6 or more hours old, Moneyflow downloads only
  the transactions dated in the last 90 days, ending today.
- **Full refresh:** when the last full refresh was 30 or more days ago, or Moneyflow has no
  record of one, it downloads complete history instead.

Moneyflow checks when you open a profile and once a minute while the TUI or web app stays
open. The status line in the TUI, and the provider status in the web sidebar, name the kind
while it runs. Background refresh does not run in these cases:

- The profile's first import has not finished.
- A failed attempt is waiting for its retry time.
- The failure needs action, such as reconnecting or reviewing removals. Press `r` after acting.

MCP never refreshes on a schedule; its `refresh_data` tool runs a full refresh.

A recent refresh replaces only the transactions dated inside its 90-day window:

- Older transactions, the pending edits that target them, and the accounts, merchants, and
  categories they use stay as they are.
- Removal review counts only transactions inside the window.
- A transaction whose date Monarch moves from inside the window to an earlier date looks
  deleted until the next full refresh restores it.

Moneyflow records the time of the last full refresh in `provider-refresh.json`, beside
`moneyflow.db` in the profile folder. The file is advisory scheduling state: it is not part of
the database or the JSONL export. If it is missing or unreadable, the next scheduled refresh is
a full refresh. If Moneyflow cannot write it after a full refresh, the refresh still saves, and a
warning names the file and the error. The warning appears in the TUI status line, beside
**Refresh** in the web app, and in the `warning` field of the MCP `get_refresh_status` result.

Cached browsing and pending edits remain available if authentication fails. Large or
destructive provider changes may require explicit review before installation.

Refresh and local editing use the same profile state. An active provider-write batch must
finish or be reconciled before another refresh can start.

## Write changes back

Merchant names, assignment to existing categories, report visibility, and transaction
deletions use the ordinary [review and commit workflow](editing.md). A commit prepares a
durable batch and records remote results item by item. Check the write status after a
failure; do not repeat edits blindly.

Manage Monarch categories and category groups in Monarch itself. Moneyflow does not create,
rename, move, merge, or delete that provider's taxonomy.

## Disconnect or recover

```bash
moneyflow provider disconnect monarch --profile "Example Profile"
```

Disconnect removes the local session, not imported transactions. Reconnect to resume provider
access. Do not delete the entire Moneyflow directory to reset credentials.
If an import or write needs attention, use the displayed recovery action and retain the
profile until you have reconciled it with Monarch.
