# Quick Start

Get up and running with moneyflow in 5 minutes.

---

## Demo Mode (No Account Required)

Try moneyflow instantly without connecting any accounts:

```bash
moneyflow --demo
```

This loads synthetic spending data so you can explore all features risk-free.

**What you'll see:**

- ~3,000 transactions across 3 years (2023-2025)
- Realistic spending patterns for dual-income household
- Multiple accounts (checking, savings, credit cards)
- All features enabled

Press ++g++ to cycle through views, ++slash++ to search, ++q++ to quit.

---

## With Your Finance Platform

Choose your platform:

- [Monarch Money Setup](#with-monarch-money)
- [YNAB Setup](#with-ynab)

---

## With Monarch Money

### Step 1: Get Your 2FA Secret

!!! warning "Important: Do this BEFORE running moneyflow"
    You'll need your 2FA/TOTP secret key. Here's how to get it:

    1. Log into [Monarch Money](https://app.monarchmoney.com/) on the web
    2. Go to **Settings** → **Security**
    3. **Disable** 2FA, then **re-enable** it
    4. When shown the QR code, click **"Can't scan?"** or **"Manual entry"**
    5. Copy the secret key (looks like: `JBSWY3DPEHPK3PXP`)

### Launch moneyflow

```bash
moneyflow
```

On first run, you'll be prompted for:

1. **Monarch Money email** - Your login email
2. **Monarch Money password** - Your account password
3. **2FA Secret** - The secret key from Step 1
4. **Encryption password** - Create a NEW password to encrypt your stored credentials

!!! tip "Encryption Password"
    This is a **new password** just for moneyflow, not your Monarch password.

    Choose something you'll remember - you'll need it each time you launch moneyflow.

### Wait for Initial Data Load

First run downloads all your transactions:

- **Small accounts** (<1k transactions): ~10 seconds
- **Medium accounts** (1k-10k): ~30 seconds
- **Large accounts** (10k+): ~1-2 minutes

!!! success "One-Time Download"
    After the first load, all operations are instant! moneyflow works offline with your data cached locally.

### Explore

You're in! Here's what to try:

- Press ++g++ to cycle views: Merchants, Categories, Groups, Accounts, Time
- In Time view: Press ++t++ to cycle through Year, Month, and Day granularities
- Press ++enter++ on any row to drill down
- Press ++escape++ to go back
- Press ++question++ for help

---

## Common First Commands

```bash
# Fetch only current year from API (faster for large Monarch/YNAB accounts)
moneyflow --year 2025

# Enable caching for even faster startup next time
moneyflow --cache

# Fetch recent data + enable cache
moneyflow --year 2025 --cache
```

!!! note
    By default, all fetched data is shown in the view. Use TIME view to analyze specific periods.

---

## Quick Edit Example

Let's rename a merchant:

1. Launch: `moneyflow`
2. Press ++g++ until you see "Merchants" view
3. Use arrow keys to find a merchant
4. Press ++m++ to edit merchant name
5. Type the new name, press ++enter++
6. Press ++w++ to review changes
7. Press ++enter++ to commit to your backend (Monarch/YNAB)

Done! The change is now saved.

---

---

## With YNAB

### Step 1: Get Your Personal Access Token

!!! warning "Important: Generate token BEFORE running moneyflow"
    You'll need a Personal Access Token from YNAB:

    1. Log into [YNAB](https://app.ynab.com/)
    2. Go to **Account Settings** → **Developer Settings**
    3. Click **"New Token"** under Personal Access Tokens
    4. Enter your YNAB password and click **"Generate"**
    5. **Copy the token immediately** - you won't see it again

### Launch moneyflow (YNAB)

```bash
moneyflow
```

On first run, you'll be prompted for:

1. **Backend selection** - Choose **YNAB**
2. **Personal Access Token** - Paste the token from Step 1
3. **Encryption password** - Create a NEW password to encrypt your stored credentials

!!! tip "Encryption Password"
    This is a **new password** just for moneyflow, not your YNAB password.

    Choose something you'll remember - you'll need it each time you launch moneyflow.

!!! info "Multiple Budgets"
    If you have multiple YNAB budgets, moneyflow will automatically use the first one. Multi-budget selection UI is
    not yet implemented.

### Wait for Initial Data Load (YNAB)

First run downloads all your transactions:

- **Small budgets** (<1k transactions): ~5 seconds
- **Medium budgets** (1k-10k): ~15 seconds
- **Large budgets** (10k+): ~30-60 seconds

!!! success "One-Time Download"
    After the first load, all operations are instant! moneyflow works offline with your data cached locally.

### Explore (YNAB)

You're in! Here's what to try:

- Press ++g++ to cycle views: Merchants, Categories, Groups, Accounts
- Press ++enter++ on any row to drill down
- Press ++escape++ to go back
- Press ++question++ for help

---

## Next Steps

- [Keyboard Shortcuts](../guide/keyboard-shortcuts.md) - Learn all the keybindings
- [Navigation & Search](../guide/navigation.md) - Understand the different views
- [Editing Transactions](../guide/editing.md) - Master bulk operations
- [Monarch Money Guide](../guide/monarch.md) - Detailed Monarch-specific documentation
- [YNAB Guide](../guide/ynab.md) - Detailed YNAB-specific documentation
- [Amazon Mode](../guide/amazon-mode.md) - Analyze Amazon purchase history

!!! info "Multiple Accounts"
    moneyflow supports multiple accounts! You can add Monarch, YNAB, and Amazon accounts and switch between them
    from the account selector on startup.

---

## Need Help?

- [FAQ](../reference/faq.md) - Common questions
- [Troubleshooting](../reference/troubleshooting.md) - Fix common issues
- [GitHub Issues](https://github.com/wesm/moneyflow/issues) - Report bugs
