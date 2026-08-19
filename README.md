# moneyflow

[![PyPI version](https://img.shields.io/pypi/v/moneyflow?color=blue)](https://pypi.org/project/moneyflow/)
[![Python 3.11+](https://img.shields.io/badge/python-3.11+-blue.svg)](https://www.python.org/downloads/)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](https://opensource.org/licenses/MIT)
[![GitHub stars](https://img.shields.io/github/stars/wesm/moneyflow?style=social)](https://github.com/wesm/moneyflow)

**Track your moneyflow from the terminal.**

A keyboard-driven terminal UI for managing personal finance transactions. Built for users who prefer efficiency and
direct control over their financial data.

![moneyflow main screen](https://moneyflow.dev/assets/screenshots/home-screen.svg)

**Supported Platforms:**

- ✅ **[Monarch Money](https://monarchmoney.sjv.io/c/5108110/3777629/39024)** - Full integration with editing and sync
- ✅ **YNAB** - Full integration with editing and sync
- ✅ **Amazon Purchases** - Import and analyze purchase history
- ✅ **SimpleFIN** - Import read-only account and transaction data; edits remain local
- ✅ **Demo Mode** - Try it without an account

**Documentation:** [moneyflow.dev](https://moneyflow.dev)

---

## Installation

```bash
# Install with pip
pip install moneyflow

# Or run without installing (recommended)
uvx moneyflow

# Or use pipx
pipx install moneyflow
```

---

## Quick Start

```bash
# Try demo mode first (no account needed)
moneyflow --demo

# Connect to Monarch Money or YNAB
moneyflow

# Analyze Amazon purchase history
moneyflow amazon import ~/Downloads/"Your Orders"
moneyflow amazon

# Fetch only recent data from API (Monarch/YNAB only - for faster loading)
moneyflow --year 2025  # Fetch from 2025-01-01 onwards
moneyflow --since 2024-06-01  # Fetch from specific date
```

**First-time Monarch Money setup:** You'll need your 2FA secret key. See the [Monarch Money setup guide](https://moneyflow.dev/guide/monarch).

**First-time YNAB setup:** You'll need a Personal Access Token from your YNAB account settings. If you have multiple
budgets, you'll be prompted to select one. See the [YNAB setup guide](https://moneyflow.dev/guide/ynab).

---

## Go v2 SQLite Preview

The `go-port` branch contains the in-progress full Go replacement. Both its TUI and embedded web
application use the same pure-Go SQLite profile and preserve the keyboard-driven refinement and
editing workflow. Moneyflow discovers persistent profiles beneath `~/.moneyflow/v2`; set
`MONEYFLOW_HOME` to choose a different v2 catalog root.

```bash
make build

# Fresh temporary SQLite demos; edits disappear when each process exits
make tui-demo
make web-demo

# Select, add, reconnect, or recover a persistent TUI profile
./bin/moneyflow tui

# Advanced direct selection by exact profile name or ID
./bin/moneyflow tui --profile "Example Profile"

# Open with an initial local date filter (Python-compatible precedence: mtd > since > year)
./bin/moneyflow tui --year 2026
./bin/moneyflow tui --since 2026-06-01
./bin/moneyflow tui --mtd

# Persistent web profile on loopback, with automatic browser launch
./bin/moneyflow web

# Loopback without launching a browser
./bin/moneyflow web --open=false

# Bind to one explicit private/tailnet address
./bin/moneyflow web --open=false --listen 100.64.0.10:8080
```

Both persistent commands open the same profile catalog. The web selector lives at the configured
base path, and each selected profile uses its stable `/p/<profile-id>/` URL. You can add, connect,
reconnect, or recover a profile without leaving either application. Use `--profile` with an exact
profile name or ID only when you want to bypass the selector.

The TUI date flags filter the opened SQLite profile through today. They do not narrow provider
refreshes: Go v2 continues to reconcile the complete posted Monarch snapshot for correctness.

Pending edits, undo history, and redo history survive process restarts. Press `u` to undo, `U` to
redo, `C` to manage categories, `G` to manage category groups, and `w` to review and atomically
commit local changes. A TUI and web process may share one profile; revision checks reject stale
mutations instead of silently overwriting another process.

Press `D` in either interface to review likely duplicate transactions from the complete filtered
result. Moneyflow matches exact dates, amounts, accounts, and Unicode-lowercased merchant labels;
it does not use fuzzy dates or choose a winner automatically. Use Space to select rows and `x` to
stage deletion. A deletion remains undoable with `u`/`U` until `w`, then Enter commits it. Provider
refresh may restore a deleted transaction when the provider still reports it.

### Monarch read and refresh preview

The Go v2 preview can bind one pristine profile to one Monarch household and import posted
transactions. Choose **Add profile** in `moneyflow tui` or `moneyflow web` for the shared setup
wizard. The provider command remains available as an advanced terminal workflow:

```bash
./bin/moneyflow provider connect monarch --currency USD --scale 2
./bin/moneyflow provider connect monarch --profile "Example Profile"
./bin/moneyflow tui

# Remove only the local session; imported data remains available offline
./bin/moneyflow provider disconnect monarch
```

The wizard defaults to USD with scale 2 and lets you confirm another three-letter currency and
decimal scale. Those exact money settings are stored with the hardened Monarch session and reused
by later refreshes. First-time setup also asks for the Monarch email, password, Base32 TOTP secret,
and a Moneyflow account password. Secret entry displays masked feedback. Credentials are stored only
in a password-encrypted vault, and Moneyflow generates Monarch verification codes automatically. A
reconnect reuses the stored money settings and asks only for the Moneyflow account password when the
saved Monarch session has expired.

There is no destructive `--replace` option. Add a separate profile from the TUI when an existing
profile contains local state. The selector can recreate an incompatible preview schema only after
an explicit two-step confirmation and preserves the old database in a dated recovery directory.

Press `r` in the TUI or web UI to run a complete refresh. A long-lived TUI or web server also
checks every six hours and lets one process fetch at a time. If the session expires, browsing and
pending edits remain available offline; the TUI enters its reconnect wizard without losing the
current view, cursor, or scroll position. The CLI reports authentication and import stages plus
bounded transaction counts, and a retained valid session after a failed import skips credential
prompts entirely.

This slice is read/import/refresh only. Edits are durable local intent, survive refresh and
restart, and may be reviewed or undone, but `w` cannot commit them until the separate Monarch
write-back slice lands. Moneyflow imports posted transactions only; pending provider rows are used
for snapshot-integrity checks and do not enter the local profile.

For a Caddy mount, preserve the request path and make `--external-url` use exactly the configured
base path:

```bash
./bin/moneyflow web --open=false \
  --listen 127.0.0.1:8080 \
  --base-path /moneyflow/ \
  --external-url https://moneyflow.example.invalid/moneyflow/
```

```caddyfile
moneyflow.example.invalid {
    handle /moneyflow/* {
        reverse_proxy 127.0.0.1:8080
    }
}
```

Replace the reserved example host and address with private values for your network. Non-loopback
HTTP has no built-in authentication or transport encryption; restrict it to a trusted private
network or put authentication and TLS at the proxy. Configure proxy access logs to omit URL query
strings because durable view state can contain financial refinements. When `--external-url` is
set, the direct listener remains readable for diagnostics but mutations are accepted only through
the canonical origin.

The v2 database is not application-encrypted. Use full-disk encryption and protect backups as you
would other financial files. Moneyflow creates the profile with private platform permissions and
uses exact integer minor units, `synchronous=FULL`, and revision-checked atomic writes. Monarch
session material is stored separately inside each profile directory with owner-only platform
permissions. Monarch credentials live in that profile's separate owner-only `credentials.enc`
vault protected by an account password using Argon2id and AES-256-GCM; generated verification codes
are never persisted. The current preview intentionally has no provider write-back, built-in web
authentication, export workflow, Python-state import, or schema migrations. If the install-only
schema is incompatible, use the TUI recovery flow or move the complete profile directory aside;
automatic migration begins only after the v2 format stabilizes.

---

## Key Features

- **Keyboard-driven** - Navigate with `g` to cycle views, `Enter` to drill down, `Escape` to go back
- **Multi-select bulk editing** - Select with `Space`, edit with `m`/`c`/`h`, commit with `w`
- **Multiple aggregation dimensions** - Merchants, Categories, Groups, Accounts, Time (by year/month)
- **Drill-down and sub-grouping** - Analyze spending from multiple angles, combine dimensions
- **Type-to-search** - Filter transactions as you type with `/`
- **Review before commit** - Preview all changes before syncing to backend
- **Encrypted credentials** - AES-128 with PBKDF2 (100,000 iterations)

Full keyboard shortcuts and tutorials: [moneyflow.dev](https://moneyflow.dev)

---

## Common Workflows

**Clean up merchant names:**

1. Press `g` until Merchant view
2. Press `m` on a merchant to rename all transactions
3. Press `w` to review and commit

**Recategorize transactions:**

1. Press `d` for detail view
2. Press `Space` to multi-select transactions
3. Press `c` to change category
4. Press `w` to review and commit

**Analyze spending:**

1. Press `g` to cycle views (Merchants → Categories → Groups → Accounts → Time)
2. In Time view: Press `t` to cycle granularity (Year → Month → Day), `Enter` to drill into a period
3. In any aggregate view: Press `Enter` to drill down
4. Press `g` to cycle sub-groupings (including by Time)
5. Press `a` to clear time drill-down, `Escape` to go back

Learn more: [Navigation & Search Guide](https://moneyflow.dev/guide/navigation)

---

## Amazon Mode

Import and analyze your Amazon purchase history:

1. Request "Your Orders" export from Amazon (Account Settings → Privacy)
2. Download and unzip "Your Orders.zip"
3. Import: `moneyflow amazon import ~/Downloads/"Your Orders"`
4. Launch: `moneyflow amazon`

See [Amazon Mode Guide](https://moneyflow.dev/guide/amazon-mode) for details.

---

## Troubleshooting

### Login fails with "Incorrect password"

- Enter your **encryption password** (for moneyflow), not your backend password
- If forgotten: Click "Reset Credentials" or delete `~/.moneyflow/`

### Monarch Money - 2FA not working

- Copy the BASE32 secret (long string), not the QR code
- Get fresh secret: Disable and re-enable 2FA in Monarch Money

### YNAB - Connection fails

- Verify your Personal Access Token is correct
- Token may have expired - generate a new one from YNAB Developer Settings
- Make sure you copied the entire token (no spaces before/after)
- Token is only shown once - if lost, generate a new one

### Terminal displays weird characters

- Use a modern terminal with Unicode support (iTerm2, GNOME Terminal, Windows Terminal)

### Complete reset

```bash
rm -rf ~/.moneyflow/
pip install --upgrade --force-reinstall moneyflow
moneyflow
```

More help: [Troubleshooting Guide](https://moneyflow.dev/reference/troubleshooting)

---

## Themes

moneyflow includes multiple color themes for different aesthetic preferences:

- **default** - Original moneyflow dark theme
- **berg** - Orange on black (inspired by Bloomberg Terminal) - nostalgic 1980s financial terminal aesthetic
- **nord** - Nord (arctic blue tones) - popular among developers for eye-friendly cool colors
- **gruvbox** - Gruvbox (retro warm colors) - vintage aesthetic beloved by vim users
- **dracula** - Dracula (modern purple) - vibrant high-contrast dark theme
- **solarized-dark** - Solarized Dark (precision colors) - scientifically designed for reduced eye strain
- **monokai** - Monokai (Sublime Text classic) - the iconic editor theme

### Configuring Themes

Set your preferred theme in `~/.moneyflow/config.yaml`:

```yaml
version: 1

settings:
  theme: berg  # or nord, gruvbox, dracula, solarized-dark, monokai
```

Restart moneyflow for the theme to take effect.

---

## Documentation

**Full documentation available at [moneyflow.dev](https://moneyflow.dev)**

- [Installation](https://moneyflow.dev/getting-started/installation)
- [Quick Start Tutorial](https://moneyflow.dev/getting-started/quickstart)
- [Navigation & Search](https://moneyflow.dev/guide/navigation)
- [Editing Transactions](https://moneyflow.dev/guide/editing)
- [Keyboard Shortcuts](https://moneyflow.dev/guide/keyboard-shortcuts)
- [Monarch Money Setup](https://moneyflow.dev/guide/monarch)
- [YNAB Setup](https://moneyflow.dev/guide/ynab)
- [SimpleFIN Setup](https://moneyflow.dev/guide/simplefin)
- [Amazon Mode](https://moneyflow.dev/guide/amazon-mode)

---

## Security

- Credentials encrypted with AES-128 using PBKDF2 key derivation (100,000 iterations)
- Encryption password never leaves your machine
- Stored in `~/.moneyflow/credentials.enc` with 600 permissions
- See [SECURITY.md](SECURITY.md) for full details

---

## Contributing

Contributions welcome! See [Contributing Guide](https://moneyflow.dev/development/contributing).

**Development setup:**

```bash
git clone https://github.com/wesm/moneyflow.git
cd moneyflow
uv sync
uv run pytest -v
```

**Code quality checks:**

```bash
uv run pytest -v                          # Tests
uv run pyright moneyflow/                 # Type checking
uv run ruff format moneyflow/ tests/      # Formatting
uv run ruff check moneyflow/ tests/       # Linting
```

See [Developing moneyflow](https://moneyflow.dev/development/developing) for details.

---

## Acknowledgments

### Monarch Money Integration

This project's Monarch Money backend uses code derived from the [monarchmoney](https://github.com/hammem/monarchmoney)
Python client library by hammem, used under the MIT License.
See [licenses/monarchmoney-LICENSE](licenses/monarchmoney-LICENSE) for details.

Monarch Money® is a trademark of Monarch Money, Inc. This project is independent and not affiliated with, endorsed by,
or officially connected to Monarch Money, Inc.

---

## License

MIT License - see [LICENSE](LICENSE) file for details.

**Disclaimer:** Independent open-source project. Not affiliated with or endorsed by Monarch Money, Inc. or YNAB LLC.
