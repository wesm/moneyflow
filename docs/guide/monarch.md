# Monarch Money Integration

**This guide is specifically for Monarch Money users.**

---

## Overview

moneyflow provides a powerful terminal interface for Monarch Money, allowing you to:

- View and analyze all your synced transactions
- Edit merchant names, categories, and other fields
- Hide/unhide transactions from reports
- Navigate by time periods, merchants, categories, and accounts
- Bulk edit transactions with multi-select
- Commit changes back to Monarch Money in real-time

All changes sync bidirectionally with your Monarch Money account.

---

## Prerequisites

Before setting up moneyflow with Monarch Money, you'll need:

1. **Monarch Money account** - Active subscription required
2. **2FA secret key** - For automatic login (see below)

---

## Getting Your 2FA Secret

!!! warning "Do this BEFORE running moneyflow"

    1. Log into [Monarch Money](https://app.monarchmoney.com/)
    2. Go to **Settings** → **Security**
    3. **Disable** your existing 2FA
    4. **Re-enable** 2FA
    5. When shown the QR code, click **"Can't scan?"**
    6. Copy the **BASE32 secret** (e.g., `JBSWY3DPEHPK3PXP`)
    7. Save this somewhere secure (password manager recommended)

!!! info "Why do I need this?"
    moneyflow requires your 2FA secret to automatically generate 6-digit codes for login.
    This allows unattended operation and avoids manual code entry on every startup.

---

## Initial Setup

### 1. Launch moneyflow

```bash
moneyflow
```

On first run, you'll be prompted to select a backend:

![Backend selection](https://raw.githubusercontent.com/wesm/moneyflow-assets/main/backend-select.svg)

Select **Monarch Money**.

### 2. Enter Monarch Money Credentials

You'll see the credential setup screen:

![Credential setup screen](https://raw.githubusercontent.com/wesm/moneyflow-assets/main/monarch-credentials.svg)

Enter:

- **Email**: Your Monarch Money login email
- **Password**: Your Monarch Money password
- **2FA Secret**: The BASE32 secret key from above

### 3. Create Encryption Password

moneyflow will ask you to create a **NEW password** to encrypt your stored credentials:

- This password is **only for moneyflow**, not for Monarch Money
- Choose something memorable - you'll need it every time you launch
- Minimum 8 characters recommended

!!! info "How Credentials Are Stored"
    Your Monarch Money credentials are encrypted with AES-128 using PBKDF2 key derivation (100,000 iterations)
    and stored at:

    ```
    ~/.moneyflow/credentials.enc
    ```

    Only you can decrypt them with your encryption password.

### 4. Initial Data Load

moneyflow will:

1. Authenticate with Monarch Money
2. Fetch your transactions (batches of 1000)
3. Download categories and account metadata
4. Build the initial view

This takes 10-30 seconds depending on transaction count.

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
- Authenticate with Monarch Money
- Load your latest transaction data

---

## Editing Transactions

All edits sync back to Monarch Money immediately after commit. See the [Editing Guide](editing.md) for full details.

---

## Reset Credentials

If you forget your encryption password or want to reconfigure:

### Option 1: Reset from Unlock Screen

1. Launch `moneyflow`
2. Click **"Reset Credentials"** on the unlock screen
3. Re-enter your Monarch Money credentials

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
- **Not** your Monarch Money password
- If you forgot it, click "Reset Credentials"

### "Authentication failed" during login

- Check your Monarch Money email and password
- Verify your 2FA secret is correct
- Try logging into Monarch Money web UI to ensure your account is active

### "Session expired" errors

moneyflow maintains a session with Monarch Money that expires after
~24 hours. moneyflow should recreate the session automatically, but if
you still see session errors:

1. Restart moneyflow - it will automatically re-authenticate
2. If that doesn't work, try resetting credentials

If you see session errors repeatedly, please [open a GitHub
issue](https://github.com/wesm/moneyflow/issues).

### Slow startup

Try filtering to recent data (`--year 2025`) or enable caching (`--cache`).
See [CLI Options](../reference/cli.md) and [Caching](../config/caching.md) for details.

---

## Data Privacy & Security

Your Monarch Money credentials are encrypted locally. moneyflow doesn't send data anywhere except Monarch Money.
See [Security Documentation](https://github.com/wesm/moneyflow/blob/main/SECURITY.md) for full details.

---

## Next Steps

- [Editing Guide](editing.md) - Learn bulk operations and workflow
- [Navigation & Search](navigation.md) - Master the interface
- [Keyboard Shortcuts](keyboard-shortcuts.md) - Essential keybindings

---

## Limitations

Current limitations with Monarch Money integration:

- **No transaction creation**: Can't create new transactions (edit existing only)
- **No account management**: Can't add/remove accounts
- **No category creation**: Can't create custom categories (use existing ones)
- **No split transactions**: Can't split a transaction into multiple categories
- **No attachments**: Can't view or add transaction attachments

These features may be added in future releases.
