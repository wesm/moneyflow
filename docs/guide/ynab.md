# YNAB

Connect one YNAB budget to review its transactions in the browser, the terminal, or an MCP
client. Reviewed payee and category changes and deletions are written back to YNAB when you
commit them. The token is stored in an encrypted local vault, and each Moneyflow process must
unlock it before it can refresh or write.

## What writes back to YNAB?

| Change | On commit |
| --- | --- |
| Payee (merchant) | Written to YNAB |
| Category, including clearing it | Written to YNAB |
| Transaction deletion | Written to YNAB |
| Payee change or deletion of a split parent | Written to YNAB |
| Category change on a split parent | Refused |
| Hide or show | Refused |
| Edits to transfers | Refused |
| Create, rename, move, merge, or delete a category or group | Refused |

Moneyflow refuses unsupported edits before they are staged. Clearing a category is distinct
from leaving it unchanged. Commits use the shared [review and commit workflow](editing.md#review-and-commit)
and its durable write batch.

## Connect a profile

You need a YNAB personal access token. The browser and the terminal use the same steps:

1. Run `moneyflow web` or `moneyflow tui`.
2. Choose **Add profile** (`a`), then **YNAB** (`y`).
3. Enter a profile name.
4. Enter the personal access token, then create and confirm a Moneyflow account password. The
   password encrypts the token in the local vault.
5. Choose the budget. YNAB's API calls it a plan.
6. Check the currency and decimal scale. Moneyflow derives them from the budget, so you cannot
   edit them.
7. Wait for the first import to finish.

The budget's currency and scale bind the profile. Reconnecting cannot silently point the same
profile at another budget.

From the command line, `moneyflow provider connect ynab` prompts for the token, the account
password and its confirmation, and a budget number:

```bash
moneyflow provider connect ynab
moneyflow provider connect ynab --profile "Example Profile"
```

In an empty data folder, the command creates a profile named **YNAB**. Otherwise `--profile`
selects an existing profile by name or ID. Rename a profile with
`moneyflow profile rename --profile YNAB --name "Example Budget"`.

## Token storage

The token stays encrypted with your account password in `providers/ynab/credentials.enc`
inside the profile folder. Moneyflow never saves it in plain text and never unlocks it
automatically after a restart. The SQLite database is not application-encrypted; see
[storage](../config/caching.md).

## Open and unlock

When you open a YNAB profile, choose one:

- **Unlock YNAB** in the browser, or Enter in the terminal's profile list: enter the account
  password to refresh and write.
- **Open Offline** in the browser, or `o` in the terminal: browse committed data without network
  access.

## Refresh data

Press `r` in the browser or the terminal, or select **Refresh** in the browser's sidebar. Every
refresh reads a complete budget snapshot. While an unlocked `moneyflow tui` or `moneyflow web`
stays open, Moneyflow also refreshes YNAB every six hours.

The adapter converts YNAB milliunits to the profile's scale exactly, or rejects the import.

- Transfers and off-budget activity are hidden in reports.
- Uncleared status is kept.
- Split details stay on the parent transaction. They are not separate editable rows.

## Use YNAB through MCP

An MCP server opens YNAB offline unless you unlock it:

```bash
./bin/moneyflow mcp --profile "Example Profile" --unlock --allow-write
```

`--unlock` reads the account password from the controlling terminal. It neither refreshes nor
commits at startup, and it does not imply `--allow-write`. GUI clients have no controlling
terminal, so use the [authenticated HTTP workflow](mcp.md#authenticated-http) instead. Never
send the account password as a tool argument.

## Limitations

- Synthetic tests are not live-write sign-off for your budget. Review targets carefully before
  committing.
- Split lines cannot be edited.
- Moneyflow does not manage YNAB's categories or category groups.

See [provider architecture](../architecture/providers.md#ynab) for the write contract.

## Troubleshooting

- **YNAB returns 429 (rate limited):** the write batch parks until its next eligible time.
  Repeated clicking does not bypass the limit. Follow the status and recovery guidance.
- **A write stops partway:** see
  [recover an interrupted provider write](editing.md#recover-an-interrupted-provider-write).
- **Moneyflow refuses the data folder:** it holds Python app files. See
  [moving Python data](../getting-started/transition.md#move-python-data-out-of-moneyflow).

To remove the local credential vault and keep the profile's data:

```bash
moneyflow provider disconnect ynab --profile "Example Profile"
```
