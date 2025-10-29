# Installation

moneyflow can be installed in multiple ways depending on your preference.

## Quick Install

=== "pip"

    ```bash
    pip install moneyflow
    ```

    Then run:
    ```bash
    moneyflow
    ```

=== "uv"

    Run with `uvx`:

    ```bash
    uvx moneyflow
    ```

=== "pipx (Isolated)"

    Install in isolated environment:

    ```bash
    pipx install moneyflow
    ```

    Then run:
    ```bash
    moneyflow
    ```

=== "Nix"

    Run without installing:

    ```bash
    nix run github:wesm/moneyflow
    ```

    Or install to your profile:

    ```bash
    nix profile install github:wesm/moneyflow
    moneyflow
    ```

    Or from a local clone:

    ```bash
    git clone https://github.com/wesm/moneyflow.git
    cd moneyflow
    nix run .#
    ```

---

## From Source

For developers or contributors:

```bash
# Clone the repository
git clone https://github.com/wesm/moneyflow.git
cd moneyflow

# Install dependencies with uv
curl -LsSf https://astral.sh/uv/install.sh | sh
uv sync

# Run from source
uv run moneyflow
```

---

## Requirements

- **Python 3.11+** (automatically handled by pip/uvx/pipx)
- **Terminal**: Any modern terminal with Unicode support
- **Account**: Monarch Money, YNAB, or Amazon account (or use `--demo` mode)

---

## Verify Installation

```bash
# Check version
moneyflow --help

# Try demo mode (no account needed)
moneyflow --demo
```

If you see the demo data load successfully, you're all set!

---

## Next Steps

- [Quick Start Guide](quickstart.md) - Get up and running in 5 minutes
- [Monarch Money Setup](../guide/monarch.md) - Detailed guide for Monarch Money users
- [YNAB Setup](../guide/ynab.md) - Detailed guide for YNAB users
- [Amazon Mode](../guide/amazon-mode.md) - Import and analyze Amazon purchase history
- [Keyboard Shortcuts](../guide/keyboard-shortcuts.md) - Learn the keybindings

---

## Troubleshooting

Having issues? See the [Troubleshooting Guide](../reference/troubleshooting.md) for help.
