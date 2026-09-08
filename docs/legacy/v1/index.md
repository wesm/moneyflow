!!! warning "Legacy Python 0.11.1"
    Frozen documentation, not the current application. Provider APIs may have changed.
    Use the [current documentation](/docs/) for Go and read the
    [transition guidance](/docs/getting-started/python-transition/) before changing profiles.

# moneyflow

## Terminal UI for personal finance power users

[Screenshot omitted: this release's source archive does not include generated images.]

```bash
# Install and run
pip install 'moneyflow==0.11.1'
uvx --from 'moneyflow==0.11.1' moneyflow

# Or run directly with uvx (no install needed)
uvx --from 'moneyflow==0.11.1' moneyflow
uvx --from 'moneyflow==0.11.1' moneyflow --demo  # Try with demo data
```

Track spending, bulk edit transactions, and navigate your financial
data at lightning speed. Supports personal finance platforms like
[Monarch Money](https://monarchmoney.sjv.io/c/5108110/3777629/39024), YNAB, or even
analyzing your Amazon purchase history.

<div class="quick-links" markdown>
[Get Started](getting-started/installation.md){ .md-button .md-button--primary }
[Try Demo](getting-started/quickstart.md){ .md-button }
[View on GitHub](https://github.com/wesm/moneyflow){ .md-button }
</div>

---

## Who Is This For?

moneyflow is perfect if you:

- **Live in the terminal** - Prefer keyboard-driven workflows over clicking through web UIs
- **Have lots of transactions to clean up** - Need to rename dozens of merchants or recategorize hundreds of
  transactions
- **Want to analyze spending patterns** - Quickly drill down by merchant, category, or time period
- **Track Amazon purchases** - Want insights into your Amazon spending habits
- **Value privacy** - Prefer local data processing over cloud-only platforms

---

## Features

<div class="feature-grid" markdown>

<div class="feature-card" markdown>
### Keyboard-Driven
Navigate, filter, and edit without touching the mouse. Vim-inspired shortcuts make common operations instant.
</div>

<div class="feature-card" markdown>
### Fast Local Operations
Download transactions once. All filtering, searching, and aggregation happens locally using Polars—no API latency.
</div>

<div class="feature-card" markdown>
### Rapid Data Refinement
Select multiple transactions. Rename merchants or recategorize hundreds of transactions with a few keystrokes.
</div>

<div class="feature-card" markdown>
### Smart Views & Drill-Down
Aggregate by merchant, category, group, or account. Drill down and sub-group within any view—see your Amazon
purchases by category, or your restaurant spending grouped by merchant or credit card.
</div>

<div class="feature-card" markdown>
### Secure Credentials
Local credential storage with AES-128 encryption. Your finance credentials stay on your machine.
</div>

<div class="feature-card" markdown>
### Review Before Commit
See exactly what changes you're making before saving. All edits are queued and reviewed together.
</div>

<div class="feature-card" markdown>
### Export Anywhere
Export your data to Parquet, CSV, or SQLite with export metadata. Full
dataset or filtered transactions — press ++E++ to export.
</div>

<div class="feature-card" markdown>
### Multi-Account Support
Manage multiple accounts (Monarch, YNAB, SimpleFIN, Amazon) and switch between
them from the account selector.
</div>

</div>

---

## Platform Support

**Currently supported:**

- **[Monarch Money](https://monarchmoney.sjv.io/c/5108110/3777629/39024)** - Full-featured integration with real-time sync
- **[YNAB (You Need A Budget)](https://www.ynab.com/)** - Full-featured integration with real-time sync
- **[Amazon Purchase History](guide/amazon-mode.md)** - Import and analyze your Amazon order history from official
  data exports
- **Demo Mode** - Synthetic data for testing features
- **[SimpleFIN](guide/simplefin.md)** - Import read-only account and transaction
  data from a SimpleFIN server; moneyflow edits are stored locally

**Future:**

- Lunch Money
- Actual Budget
- Generic CSV import for any platform

The backend system is pluggable—adding new platforms is straightforward.
See the archived [contribution guide](https://github.com/wesm/moneyflow/blob/v0.11.1/docs/development/contributing.md)
for the historical development workflow. Current development targets Go.

---

## Installation

```bash
# Quick install
pip install 'moneyflow==0.11.1'

# Or use uvx (no installation needed!)
uvx --from 'moneyflow==0.11.1' moneyflow --demo
```

**Requirements:** Python 3.11+

**Next steps:**

1. [Full installation guide](getting-started/installation.md) - Detailed setup instructions
2. [Quick start guide](getting-started/quickstart.md) - Get up and running in 2 minutes
3. [Keyboard shortcuts](guide/keyboard-shortcuts.md) - Master the interface
4. [Exporting transactions](guide/export.md) - Export to Parquet, CSV, or SQLite
5. [SimpleFIN Guide](guide/simplefin.md) - Configure a SimpleFIN connection

---

## Independent Open Source Project

!!! info ""
    moneyflow is an independent open-source project. It is not affiliated with, endorsed by, or officially connected
    to Monarch Money, Inc., YNAB LLC, or any other finance platform.

---

## License

MIT License - see [LICENSE](https://github.com/wesm/moneyflow/blob/v0.11.1/LICENSE) for details.
