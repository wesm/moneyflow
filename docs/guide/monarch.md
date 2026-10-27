# Monarch Money

Connect a Monarch household to review its transactions in the browser, the terminal, or an MCP
client. Moneyflow imports posted transactions; bank-pending rows stay out of the local profile.
Reviewed edits to merchants, categories, report visibility, and deletions are written back to
Monarch when you commit them.

## What writes back to Monarch?

| Change | On commit |
| --- | --- |
| Merchant name | Written to Monarch |
| Category, chosen from existing Monarch categories | Written to Monarch |
| Hide or show in reports | Written to Monarch |
| Transaction deletion | Written to Monarch |
| Create, rename, move, merge, or delete a category or group | Not available; manage them in Monarch |

Edits use the ordinary [review and commit workflow](editing.md#review-and-commit): press `w`,
or select **Review** on the browser's pending-edits card, check the changes, then press Enter.
A commit prepares a durable batch and records each remote result as it arrives. After a
failure, check the write status before doing anything else; do not repeat edits blindly.
[Recover an interrupted provider write](editing.md#recover-an-interrupted-provider-write)
explains the recovery actions.

## Connect a profile

You need your Monarch email and password, and the authenticator setup key: the Base32 text
behind the QR code in Monarch Settings > Security. It is not a current six-digit code; Moneyflow
generates the verification codes from it. Your password manager may hold it. If you only
scanned the QR code, set up the authenticator again in Monarch and save the key it shows.
Moneyflow cannot connect a Monarch account that does not sign in with an authenticator app.

The browser and the terminal use the same steps:

1. Run `moneyflow web` or `moneyflow tui`.
2. Choose **Add profile** (`a`), then **Monarch Money** (`m`).
3. Enter a profile name.
4. Choose the three-letter currency, such as `USD`. Monarch does not report one, and you cannot
   change it after setup.
5. Enter your Monarch email, password, and authenticator setup key.
6. Create and confirm a Moneyflow account password. It encrypts the local credential vault.
7. Wait for authentication and the first import to finish.

Secret fields show masked typing feedback. Keep the authenticator secret in your password
manager, and never include it in a bug report.

To add another household, choose **Add profile** again. A connected profile stays bound to its
household and its money settings; it cannot silently switch to another provider identity.

### Connect from the command line

```bash
moneyflow provider connect monarch --currency USD
moneyflow provider connect monarch --profile "Example Profile"
```

The command prompts for credentials and the account password. In an empty data folder
(`~/.moneyflow`, or the folder `MONEYFLOW_HOME` names), it creates a profile named
**Monarch**. Otherwise `--profile` selects an existing profile by name or ID; it does not
create one. Without `--profile`, the command uses the only profile when exactly one exists.
Rename a profile at any time; its ID, data, and credentials stay:

```bash
moneyflow profile rename --profile Monarch --name "Example Household"
```

For a smaller first import, add `--mtd`. It loads transactions from the first day of the
current month and requires a pristine profile. It is not an ongoing date filter: because a
month-to-date import is not a full refresh, the first [scheduled refresh](#refresh-data)
after it downloads complete history.

## Where are credentials stored?

| File in the profile folder | Contents | Protection |
| --- | --- | --- |
| `providers/monarch/credentials.enc` | Login credentials and authenticator secret | Encrypted with your account password (Argon2id, AES-256-GCM) |
| `providers/monarch/session.json` | Session material | Owner-only permissions; not password-encrypted |

Generated verification codes are never saved. While the saved session is valid, Moneyflow
reopens the profile without asking for the account password. When the session expires,
reconnect through setup and unlock the saved credentials. The SQLite database is not
application-encrypted; see [storage](../config/caching.md).

## Refresh data

| Kind | What it downloads |
| --- | --- |
| Recent refresh | Transactions dated in the last 90 days, ending today |
| Full refresh | Complete history |

Press `r` in the browser or the terminal, or select **Refresh** in the browser's sidebar, to run
a refresh. It is a recent refresh, unless the profile has no earlier refresh, the last full
refresh was 30 or more days ago, or Moneyflow has no record of one. Then it runs a full refresh.
The background schedule uses the same rule.

To run a full refresh at any time:

- Press `R` (Shift+R) in the browser or the terminal. The browser's help (`?`) lists it, and
  the tooltip on the sidebar's refresh button names it.
- In the browser, select **Retry** after a failed full refresh.
- Through MCP, call `refresh_data` with `complete_history: true`.

The terminal status line and the browser's [refresh dialog](web.md#refresh-provider-data)
name the kind that is running. Saving edits does not start a refresh; successful edits update
the local copy directly.

### Background refresh

While `moneyflow tui` or `moneyflow web` stays open, Moneyflow refreshes Monarch on a
schedule, as the Python app (v0.11.1) did:

- A recent refresh when the last refresh is 6 or more hours old.
- A full refresh instead when the last full refresh was 30 or more days ago, or Moneyflow has
  no record of one.

Moneyflow checks when you open the profile and once a minute after that. The browser opens the
refresh dialog for a background refresh too. It shows a banner while the data predates a
refresh that did not finish.

Background refresh does not run when:

- The profile's first import has not finished.
- A failed attempt is waiting for its retry time. A refresh that stops early, because its
  request was canceled or reached the browser's 30-minute limit, counts as failed: provider
  status reports `provider_unavailable`, and background refresh waits five minutes.
- The failure needs action, such as reconnecting or reviewing removals. Press `r` after acting.

MCP never refreshes on a schedule. Its `refresh_data` tool reads the same data as `r`, or
complete history with `complete_history: true`.

### Progress

A refresh reads visible transactions, then hidden ones, and then reads both again to confirm
that nothing changed during the first read. The count runs across visible and hidden
transactions, and the percentage covers both reads and never moves backward. In the terminal
status line, a full refresh reads like this:

```text
Refreshing complete provider history: 31,808 transactions fetched (50%).
Refreshing complete provider history: verifying 12,000 of 31,808 transactions (69%).
```

The browser's [refresh dialog](web.md#refresh-provider-data) shows the same counts.

### What a recent refresh leaves alone

A recent refresh replaces only the transactions dated inside its 90-day window:

- Older transactions, the pending edits that target them, and the accounts, merchants, and
  categories they use stay as they are.
- Removal review counts only transactions inside the window.
- A recent refresh cannot see that Monarch deleted an older transaction, or changed one dated
  before the window. The next full refresh, which runs at least every 30 days, removes or
  updates it.
- A transaction whose date Monarch moves from inside the window to an earlier date looks
  deleted until the next full refresh restores it. The recent refresh discards pending edits
  on that transaction, and the full refresh restores the transaction but not those edits.

### The refresh record

Moneyflow records the time of the last full refresh in `provider-refresh.json`, beside
`moneyflow.db` in the profile folder. This advisory scheduling file is not part of the database
or the JSONL export. If it is missing or unreadable, the next scheduled refresh is a full
refresh. If Moneyflow cannot write it after a full refresh, the refresh still saves, and this
warning appears:

```text
Refresh saved, but Moneyflow could not record it as a full refresh in provider-refresh.json. The next check will download complete history again.
```

It appears in the terminal status line, beside **Refresh** in the browser, and in the
`warning` field of the MCP `get_refresh_status` result.

## Limitations

- Pending bank activity is not imported.
- Moneyflow does not manage Monarch's categories or category groups.
- An active provider-write batch must finish or be reconciled before another refresh starts.
  Refresh and local editing share the same profile state.
- Large or destructive provider changes can require explicit review before Moneyflow installs
  them.

## Troubleshooting

- **Authentication fails:** cached browsing and pending edits stay available. Reconnect through
  setup when you are ready.
- **An import or write needs attention:** use the recovery action Moneyflow shows, and keep the
  profile until you have reconciled it with Monarch.
- **Moneyflow refuses the data folder:** it holds Python app files. See
  [moving Python data](../getting-started/transition.md#move-python-data-out-of-moneyflow).

To remove the local session without touching imported transactions:

```bash
moneyflow provider disconnect monarch --profile "Example Profile"
```

Reconnect to resume provider access. Do not delete the whole Moneyflow folder to reset
credentials.
