!!! warning "Legacy Python 0.11.1"
    Frozen documentation, not the current application. Provider APIs may have changed.
    Use the [current documentation](/docs/) for Go and read the
    [transition guidance](/docs/getting-started/python-transition/) before changing profiles.

# Installation

This page describes Python 0.11.1. Prefer the isolated invocation below so an existing Go command
is not replaced:

```bash
uvx --from 'moneyflow==0.11.1' moneyflow
```

The alternative package installs below can place a `moneyflow` command on your PATH. Keep that
separate from your Go binary. Version pinning selects this application release but does not
freeze provider APIs or guarantee future compatibility of every transitive dependency.

## Quick Install

=== "pip"

    ```bash
    pip install 'moneyflow==0.11.1'
    ```

    Then run:
    ```bash
    uvx --from 'moneyflow==0.11.1' moneyflow
    ```

=== "uv"

    Run with `uvx`:

    ```bash
    uvx --from 'moneyflow==0.11.1' moneyflow
    ```

=== "pipx (Isolated)"

    Install in isolated environment:

    ```bash
    pipx install 'moneyflow==0.11.1'
    ```

    Then run:
    ```bash
    uvx --from 'moneyflow==0.11.1' moneyflow
    ```

=== "Nix"

    Run without installing:

    ```bash
    nix run github:wesm/moneyflow/v0.11.1
    ```

    Or install to your profile:

    ```bash
    nix profile install github:wesm/moneyflow/v0.11.1
    uvx --from 'moneyflow==0.11.1' moneyflow
    ```

    Or from a local clone:

    ```bash
    git clone --branch v0.11.1 --depth 1 https://github.com/wesm/moneyflow.git
    cd moneyflow
    nix run .#
    ```

---

## From Source

For developers or contributors:

```bash
# Clone the repository
git clone --branch v0.11.1 --depth 1 https://github.com/wesm/moneyflow.git
cd moneyflow

# Install dependencies with uv
curl -LsSf https://astral.sh/uv/install.sh | sh
uv sync

# Run from source
uv run --project . moneyflow
```

---

## Requirements

- **Python 3.11+** (automatically handled by pip/uvx/pipx)
- **Terminal**: Any modern terminal with Unicode support
- **Account**: [Monarch Money](https://monarchmoney.sjv.io/c/5108110/3777629/39024),
  YNAB, or Amazon account (or use `--demo` mode)

---

## Verify Installation

```bash
# Check version
uvx --from 'moneyflow==0.11.1' moneyflow --help

# Try demo mode (no account needed)
uvx --from 'moneyflow==0.11.1' moneyflow --demo
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
