# YNAB Integration

**This guide is specifically for YNAB (You Need A Budget) users.**

---

## Overview

moneyflow provides a powerful terminal interface for YNAB, allowing you to:

- View and analyze all your budgeted transactions
- Edit merchant names (payees), categories, and other fields
- Navigate by time periods, payees, categories, and accounts
- Bulk edit transactions with multi-select
- Commit changes back to YNAB in real-time

All changes sync bidirectionally with your YNAB account.

---

## Prerequisites

Before setting up moneyflow with YNAB, you'll need:

1. **YNAB subscription** - Active You Need A Budget subscription required
2. **Personal Access Token** - For API authentication (see below)

---

## Getting Your Personal Access Token

!!! warning "Generate token BEFORE running moneyflow"

    1. Log into [YNAB](https://app.ynab.com/)
    2. Go to **Account Settings** → **Developer Settings**
    3. Under **Personal Access Tokens**, click **"New Token"**
    4. Enter your YNAB password
    5. Click **"Generate"**
    6. **Copy the token immediately** - you won't be able to see it again
    7. Save this somewhere secure (password manager recommended)

!!! info "Why do I need this?"
    moneyflow uses YNAB's official API to access your budget data. Personal Access Tokens provide secure, long-lived authentication without requiring your YNAB password.

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

moneyflow will ask you to create a **NEW password** to encrypt your stored credentials:

- This password is **only for moneyflow**, not for YNAB
- Choose something memorable - you'll need it every time you launch
- Minimum 8 characters recommended

!!! info "How Credentials Are Stored"
    Your YNAB Personal Access Token is encrypted with AES-128 using PBKDF2 key derivation (100,000 iterations) and stored at:

    ```
    ~/.moneyflow/credentials.enc
    ```

    Only you can decrypt it with your encryption password.

### 4. Initial Data Load

**Note:** If you have multiple YNAB budgets, moneyflow will automatically use the first one. Multi-budget selection UI is not yet implemented.

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

Try filtering to recent data (`--year 2025`) or enable caching (`--cache`). See [CLI Options](../reference/cli.md) and [Caching](../config/caching.md) for details.

---

## Data Privacy & Security

Your YNAB credentials are encrypted locally. moneyflow uses YNAB's official REST API and doesn't send data anywhere except YNAB. See [Security Documentation](https://github.com/wesm/moneyflow/blob/main/SECURITY.md) for full details.

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

---

## YNAB API Rate Limits

YNAB's API has rate limits:

- **200 requests per hour** per access token
- moneyflow batches operations to minimize API calls
- Initial load: ~5-10 API calls
- Commit operations: 1 API call per batch

If you hit rate limits, wait an hour before making more requests.

---

## Differences from YNAB Web/Mobile App

moneyflow is optimized for **bulk operations** and **keyboard-driven workflows**:

| Feature | YNAB App | moneyflow |
|---------|----------|-----------|
| Navigation | Mouse/touch | Keyboard |
| Bulk edit | Limited | Unlimited multi-select |
| Search | Basic | Type-to-filter everywhere |
| Views | Budget-focused | Transaction-focused |
| Speed | Web UI delays | Instant local data |
| Editing | One at a time | Bulk with review |

Use moneyflow when you need to **clean up** lots of transactions quickly. Use YNAB's official apps for **budgeting** and **planning**.

---

## YNAB Terminology in moneyflow

moneyflow uses generic terminology that maps to YNAB concepts:

| moneyflow Term | YNAB Term |
|----------------|-----------|
| Merchant | Payee |
| Category | Category |
| Group | Category Group |
| Account | Account |
| Amount | Amount (outflow negative, inflow positive) |

The UI displays "Merchant" but the underlying concept is YNAB's "Payee."
