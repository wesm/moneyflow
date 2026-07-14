# SimpleFIN Integration

Use moneyflow with account and transaction data provided by a SimpleFIN server.

!!! tip "Looking for CLI commands?"
    See the [SimpleFIN CLI Reference](simplefin-cli.md) for all command-line options
    and subcommands.

## Overview

- View and analyze posted transactions returned by the configured server
- Edit merchant names and local category assignments
- Hide/unhide transactions from reports
- Navigate by time, merchant, and account
- Bulk edit with multi-select
- Store all moneyflow edits locally; the SimpleFIN protocol is read-only

---

## Storage and Synchronization Behavior

- SimpleFIN access is read-only. moneyflow does not send edits to the configured
  SimpleFIN server or financial institution.
- Posted transactions are stored in a profile-local SQLite database. moneyflow
  does not encrypt this database.
- An additive refresh inserts rows with new transaction IDs without overwriting
  existing rows. This preserves local edits but does not apply upstream corrections
  to transactions already stored.
- Local transaction deletions create tombstones in SQLite so the same IDs are not
  reinserted by later additive or hard refreshes.
- A hard refresh replaces transaction rows with the posted transactions returned
  by the server. Merchant renames, category assignments, and hide flags are lost;
  deletion tombstones and category definitions remain.
- Category definitions and assignments are managed by moneyflow rather than
  synchronized through SimpleFIN.
- moneyflow currently supports profiles whose accounts share one ISO 4217
  currency code. A refresh stops if the response contains multiple currencies,
  a custom currency identifier, or partial-account errors.

---

## Unsupported Operations

moneyflow's SimpleFIN backend does not:

- Create or delete transactions on the SimpleFIN server
- Synchronize merchant or category edits to the server
- Import an institution-provided category hierarchy
- Create split transactions
- Read or write transaction attachments

---

## Prerequisites

You need either:

- An HTTPS Access URL from a SimpleFIN server, or
- A one-time Base64 setup token from
  [SimpleFIN Bridge](https://bridge.simplefin.org/simplefin/create)

---

## Getting Credentials

If your provider gives you an Access URL, copy that URL directly. If you use
SimpleFIN Bridge, create and copy a one-time setup token from its
[connection page](https://bridge.simplefin.org/simplefin/create). moneyflow
claims the token during setup and stores the resulting Access URL.

!!! info
    An Access URL contains credentials. A setup token can be exchanged for an
    Access URL once. Keep both private.

---

## Initial Setup

### 1. Launch moneyflow

```bash
moneyflow
```

On first run, you'll be prompted to select a backend.

Select **SimpleFIN**.

### 2. Enter SimpleFIN Access URL

You'll see the credential setup screen:

Enter:

- **Access URL or token**: The credential from your provider or SimpleFIN Bridge

### 3. Create Encryption Password

Create a NEW password to encrypt your stored credentials:

- Only for moneyflow (not your SimpleFIN access URL)
- Needed every time you launch
- Minimum 8 characters

!!! info
    With encryption enabled, the Access URL is stored at
    `~/.moneyflow/profiles/<profile-id>/credentials.enc`. moneyflow uses Fernet
    encryption with a key derived through PBKDF2 (100,000 iterations).

### 4. Initial Data Load

moneyflow will:

1. Validate your Access URL
2. Fetch your transactions from the SimpleFIN API (up to 3 years of history)
3. Store them in a local SQLite database
4. Build the initial view

---

## Subsequent Runs

After initial setup, launching moneyflow only requires your **encryption password**:

```bash
moneyflow
# Enter encryption password: ********
# Loading...
```

moneyflow will:

- Decrypt your stored Access URL
- Serve data from the local SQLite database
- Check whether the last API refresh is older than 24 hours and, if needed,
  refresh in the background after the UI loads

---

## Data Refresh

### Automatic

On initial setup, SimpleFIN fetches your transaction history from the API.

On subsequent launches, moneyflow automatically checks if the last API refresh
is older than 24 hours. If so, it starts a background refresh after the UI loads:

- The UI appears immediately with cached data from SQLite
- Rows with new transaction IDs are added in the background
- A notification confirms when the refresh completes

### Manual (Additive)

Fetch new transactions from the SimpleFIN API while preserving local edits:

```bash
# Refresh default profile
moneyflow simplefin refresh
```

Existing rows are not overwritten. Transactions whose IDs are not already in
the database are added.

### Hard Refresh (Overwrite)

Replace local transaction rows with fresh data from the API. **Merchant renames,
category assignments, and hide flags will be discarded.** Category definitions
and local deletion tombstones remain.

```bash
# Hard refresh default profile
moneyflow simplefin refresh --force
```

The API is queried with a 3-year lookback (the same range as the initial data load).

### How Refresh Works

When refreshing (additive), SimpleFIN fetches all transactions since
**2 weeks before** the last refresh date and merges them into SQLite using
`INSERT OR IGNORE`:

- **New transactions** are added
- **Existing transactions** are preserved exactly as-is (local edits never overwritten)
- **Locally deleted transaction IDs** are excluded using persistent tombstones
- **Pending transactions** are skipped — they will be picked up once posted

The 2-week lookback is deliberate: if a transaction was pending during the
prior refresh (and thus skipped), it will be re-fetched once it transitions
to posted. Even though its original transaction date may precede the last
refresh boundary, the lookback window ensures it is included in the next
refresh. Duplicates are ignored by `INSERT OR IGNORE`.

When hard-refreshing, local transaction rows and refresh metadata are cleared
before returned posted transactions are inserted again. Deletion tombstones are
retained and continue to exclude their transaction IDs.

If the server reports partial account errors, multiple account currencies, or
a custom currency identifier, the refresh fails without updating the
successful-refresh timestamp. SimpleFIN permits custom currency identifiers;
moneyflow does not currently support them.

!!! note
    With additive refresh, because local edits are never overwritten, corrected
    transaction data from your institution (e.g., an updated amount) will NOT
    be reflected locally after the initial import. This behavior preserves local
    edits. Use `moneyflow simplefin refresh --force` to replace local rows with
    the current API response.

---

## Editing Transactions

All edits write to the local SQLite database. See the [Editing Guide](editing.md)
for full details.

### Read-Only Warning

The first time you commit edits, moneyflow shows a one-time notification:

> **Edits are saved locally only and will not sync to your financial institution.**

This is expected — SimpleFIN is a read-only protocol. Your merchant renames,
category changes, and hides are persisted in the local SQLite store and will
be available the next time you launch moneyflow.

---

## Categories

moneyflow's SimpleFIN backend does not import a category hierarchy. Instead:

- moneyflow uses **built-in default categories** (~60 in 15 groups)
- You can assign categories to transactions using the standard **`c`** (edit category) workflow
- Category definitions are stored in the profile's `config.yaml`
- Transaction category assignments are stored in the profile's SQLite database
- Use the "Category Manager" modal (**`SHIFT-C`**) to create, rename, merge,
  delete, and move categories
- Use the "Group Manager" modal (**`SHIFT-G`**) to create, rename, merge, or
  delete category groups

---

## Profiles & Default

moneyflow supports **multiple SimpleFIN profiles**, each with its own credentials,
category configuration, and local database.

### Listing Profiles

```bash
moneyflow simplefin default
```

This shows all configured SimpleFIN profiles and marks the current default.

### Setting a Default

```bash
moneyflow simplefin default --set my-business
```

The default profile is used automatically when no `--profile` flag is given.

### Clearing the Default

```bash
moneyflow simplefin default --clear
```

After clearing, the next operation with multiple profiles will prompt you
to choose one.

### Overriding the Default Per-Invocation

Use `--profile` on any SimpleFIN command to temporarily use a different
profile without changing the default:

```bash
moneyflow simplefin --profile personal status
moneyflow simplefin refresh --profile business
```

See the [CLI Reference](simplefin-cli.md#shared-options) for the full
behavior table.

---

## Reset Credentials

If you forget your encryption password or want to reconfigure:

### Option 1: Reset from Unlock Screen

1. Launch `moneyflow`
2. Click **"Reset Credentials"** on the unlock screen
3. Re-enter your SimpleFIN Access URL

### Option 2: Delete One Profile's Credentials

Locate the profile ID with `moneyflow simplefin default`, then delete only that
profile's credential and salt files:

```bash
rm -f ~/.moneyflow/profiles/<profile-id>/credentials.enc
rm -f ~/.moneyflow/profiles/<profile-id>/credentials.json
rm -f ~/.moneyflow/profiles/<profile-id>/salt
moneyflow
```

Do not delete `simplefin.db` unless you also intend to discard local edits.

---

## Troubleshooting

### "Access URL required" during login

- Ensure you pasted the Access URL or Base64 token into the password field
- Verify the URL starts with `https://` or the token is a valid Base64 string
- Try copying the token from bridge.simplefin.org again

### No transactions found after setup

- Your financial institution may not have transactions in the last 3 years
- Run `moneyflow simplefin status` to inspect the local row count and refresh time
- If using SimpleFIN Bridge, verify the connection there

### Stale data

- moneyflow automatically refreshes stale data (>24 hours) in the background
  at startup — normally no action is needed
- Launch with `moneyflow simplefin refresh` to force an immediate additive refresh
  while preserving local edits
- Launch with `moneyflow simplefin refresh --force` to completely replace local data
  from the API (discards merchant, category-assignment, and hide edits)

### Edits not persisting

- Ensure you press **`w`** to open the review screen, then **`Enter`** to commit
- If the database is deleted (`simplefin.db`), local edits are lost

## Data Privacy & Security

- With encryption enabled, the Access URL is stored in the profile's
  `credentials.enc` file. If encryption is disabled, it is stored in plaintext
  as `credentials.json`.
- Posted transactions are stored in the profile's unencrypted `simplefin.db`
  SQLite database.
- moneyflow requests data from the server named by the Access URL. When using
  SimpleFIN Bridge, the Bridge is an intermediary between the financial
  institution and moneyflow.

See [Security Documentation](https://github.com/wesm/moneyflow/blob/main/SECURITY.md).

---

## Next Steps

- [Editing Guide](editing.md) - Learn bulk operations and workflow
- [Navigation & Search](navigation.md) - Master the interface
- [Keyboard Shortcuts](keyboard-shortcuts.md) - Essential keybindings
