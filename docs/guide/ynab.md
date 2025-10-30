# YNAB Integration

Terminal interface for YNAB (You Need A Budget) with full editing and sync capabilities.

## Overview

- View and analyze budgeted transactions
- Edit payees, categories, and transaction fields
- Navigate by time, payee, category, and account
- Bulk edit with multi-select
- Real-time sync to YNAB

---

## Prerequisites

1. Active YNAB subscription
2. Personal Access Token (see below for setup)

---

## Getting Your Personal Access Token

1. Log into [YNAB](https://app.ynab.com/)
2. Go to **Account Settings** → **Developer Settings**
3. Click **"New Token"** under Personal Access Tokens
4. Enter your YNAB password and click **"Generate"**
5. **Copy the token immediately** (you can't view it again)
6. Save to password manager

!!! info
    moneyflow uses YNAB's official API. Personal Access Tokens provide secure authentication without requiring your
    YNAB password.

---

## Initial Setup

### 1. Launch moneyflow

```bash
moneyflow
```

On first run, you'll be prompted to select a backend:

![Backend selection](https://raw.githubusercontent.com/wesm/moneyflow-assets/main/backend-select.svg)

Select **YNAB**.

### 2. Enter YNAB Credentials

You'll see the credential setup screen:

![Credential setup screen](https://raw.githubusercontent.com/wesm/moneyflow-assets/main/ynab-credentials.svg)

Enter:

- **Personal Access Token**: The token you generated from YNAB Developer Settings

### 3. Create Encryption Password

Create a NEW password to encrypt your stored credentials:

- Only for moneyflow (not your YNAB password)
- Needed every time you launch
- Minimum 8 characters

!!! info
    Token encrypted with AES-128/PBKDF2 (100k iterations) at `~/.moneyflow/credentials.enc`

### 4. Initial Data Load

**Note:** If you have multiple YNAB budgets, moneyflow will automatically use the first one. Multi-budget selection
UI is not yet implemented.

moneyflow will:

1. Authenticate with YNAB API
2. Fetch your transactions
3. Download categories and account metadata
4. Build the initial view

This takes 5-15 seconds depending on transaction count.

---

## Subsequent Runs

After initial setup, launching moneyflow only requires your **encryption password**:

```bash
moneyflow
# Enter encryption password: ********
# Loading...
```

moneyflow will:

- Decrypt your stored credentials
- Authenticate with YNAB API
- Load your latest transaction data

---

## Editing Transactions

All edits sync back to YNAB immediately after commit. See the [Editing Guide](editing.md) for full details.

**YNAB-specific notes:**

- Payees (YNAB term) are called "merchants" in moneyflow UI
- Category changes respect YNAB's category structure
- Split transactions are not currently supported

---

## Reset Credentials

If you forget your encryption password or want to reconfigure:

### Option 1: Reset from Unlock Screen

1. Launch `moneyflow`
2. Click **"Reset Credentials"** on the unlock screen
3. Re-enter your YNAB Personal Access Token

### Option 2: Manual Reset

Delete the credentials file and restart:

```bash
rm -rf ~/.moneyflow/
moneyflow
```

---

## Troubleshooting

### "Incorrect password" when unlocking

- You're entering the **encryption password** (the one YOU created for moneyflow)
- **Not** your YNAB password or token
- If you forgot it, click "Reset Credentials"

### "Authentication failed" during login

- Check your Personal Access Token is correct
- Token may have expired - generate a new one from YNAB Developer Settings
- Make sure you copied the entire token with no spaces before/after
- Try logging into YNAB web app to ensure your account is active

### "No budgets found"

- Ensure you have at least one budget in your YNAB account
- Try refreshing YNAB web app to sync data

### Personal Access Token lost

YNAB only shows tokens once during generation. If you lose it:

1. Go to YNAB Account Settings → Developer Settings
2. **Revoke** the old token
3. Generate a **new token**
4. Update moneyflow: Click "Reset Credentials" or delete `~/.moneyflow/`

### Slow startup

Try filtering to recent data (`--year 2025`) or enable caching (`--cache`). See [CLI Options](../reference/cli.md)
and [Caching](../config/caching.md) for details.

---

## Data Privacy & Security

Credentials encrypted locally. Data only sent to YNAB via official API. See [Security Documentation](https://github.com/wesm/moneyflow/blob/main/SECURITY.md).

---

## Next Steps

- [Editing Guide](editing.md) - Learn bulk operations and workflow
- [Navigation & Search](navigation.md) - Master the interface
- [Keyboard Shortcuts](keyboard-shortcuts.md) - Essential keybindings

---

## Limitations

Current limitations with YNAB integration:

- **No transaction creation**: Can't create new transactions (edit existing only)
- **No account management**: Can't add/remove accounts
- **No category creation**: Can't create custom categories (use existing ones)
- **No split transactions**: Can't split a transaction into multiple categories
- **No budget operations**: Can't modify budget amounts or goals
- **No reconciliation**: Can't mark accounts as reconciled

These features may be added in future releases.
