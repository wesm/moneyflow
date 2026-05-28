# Quick Start

Get up and running with moneyflow in 5 minutes.

---

## Demo Mode (No Account Required)

Try moneyflow instantly without connecting any accounts:

```bash
moneyflow --demo
```

This loads synthetic spending data so you can explore all features risk-free.

![Merchants view](../assets/screenshots/cycle-1-merchants.svg)

**What you'll see:**

- ~3,000 transactions across 3 years (2023-2025)
- Realistic spending patterns for dual-income household
- Multiple accounts (checking, savings, credit cards)
- All features enabled

Press ++g++ to cycle through views, ++slash++ to search, ++q++ to quit.

---

## Explore the Views

Press ++g++ to cycle through different aggregation views:

<div class="screenshot-grid" markdown>

![Categories view](../assets/screenshots/cycle-2-categories.svg)
*Categories - See spending by category*

![Time view](../assets/screenshots/cycle-5-time-years.svg)
*Time - Analyze spending over time*

</div>

Press ++enter++ on any row to drill down into transaction details:

![Drill down to transactions](../assets/screenshots/drill-down-detail.svg)

---

## With Your Finance Platform

Choose your platform:

- [Monarch Money Setup](#with-monarch-money)
- [YNAB Setup](#with-ynab)

---

## With Monarch Money

!!! tip "New to Monarch Money?"
    Get **50% off your first year of Core Plan** with this [special offer link](https://monarchmoney.sjv.io/c/5108110/3777629/39024).

### Step 1: Use Direct Sign-In (Not Apple/Google)

Moneyflow authentication will not work with an account using Apple/Google Sign-In even
if you create a password and set up MFA for that account. You must use direct sign-in
(an email and password).

Follow the instructions [in this article.](https://help.monarch.com/hc/en-us/articles/4432419860244-Switch-to-Direct-Sign-In-or-Disconnect-Your-Apple-Google-Sign-In)

!!! warning "Do not lose access to your Monarch account!"
    If you want to use the same email that you have linked with Google/Apple, the best way to do so is to:

    1. Add another direct email address and password (credential #2)
    2. Test that you can login to Monarch with credentials #2
    3. After you have verified that the new credentials work, you can unlink credential #1 and/or remove it
    4. Add credentials #1 back without linking to Google/Apple
    5. Test credential #1 again and you can choose to keep or remove credential #2  

### Step 2: Get Your MFA Secret

!!! warning "Important: Do this BEFORE running moneyflow"
    You'll need your MFA (multi-factor authentication) secret key. Here's how to get it:

    1. Log into [Monarch Money](https://monarchmoney.sjv.io/c/5108110/3777629/39024) on the web
    2. Go to **Settings** → **Security**
    3. If multifactor authentication is enabled, **disable it first**, then **re-enable** it to be able to retrieve your MFA secret key
    4. When shown the QR code, click **"Can't scan?"** or **"Manual entry"**
    5. Copy the MFA secret key, it's a 32 character alphanumeric string (looks something like: `XXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX`)
    6. Finish the MFA setup by confirming the MFA code with your favorite authentication app

### Launch moneyflow

```bash
moneyflow
```

On first run, you'll be prompted for credentials:

![Monarch credentials](../assets/screenshots/monarch-credentials.svg)

1. **Monarch Money email** - Your login email
2. **Monarch Money password** - Your account password
3. **MFA Secret** - The secret key from Step 2
4. **Encryption password** - Create a NEW password to encrypt your stored credentials

!!! tip "Encryption Password"
    This is a **new password** just for moneyflow, not your Monarch password.

    Choose something you'll remember - you'll need it each time you launch moneyflow.

Transactions are cached locally after the initial download for instant startup.

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

![YNAB credentials](../assets/screenshots/ynab-credentials.svg)

1. **Backend selection** - Choose **YNAB**
2. **Personal Access Token** - Paste the token from Step 1
3. **Encryption password** - Create a NEW password to encrypt your stored credentials

!!! info "Multiple Budgets"
    If you have multiple YNAB budgets, moneyflow will automatically use the first one. Multi-budget selection UI is
    not yet implemented.

Transactions are cached locally after the initial download for instant startup.

---

## Common First Commands

```bash
# Fetch only current year from API (faster for large accounts)
moneyflow --year 2025

# Force refresh from API (ignore cache)
moneyflow --refresh
```

!!! note
    Caching is enabled by default. Your transactions are stored in an encrypted local cache for fast startup.
    Use `--refresh` to force a fresh download from your backend.

---

## Quick Edit Example

Let's rename a merchant:

1. Press ++g++ until you see "Merchants" view
2. Use arrow keys to find a merchant
3. Press ++m++ to edit merchant name
4. Type the new name, press ++enter++
5. Press ++w++ to review changes
6. Press ++enter++ to commit to your backend (Monarch/YNAB)

![Edit merchant](../assets/screenshots/drill-down-bulk-edit-merchant.svg)

Done! The change is now saved.

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
