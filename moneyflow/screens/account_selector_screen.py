"""Account selection screen for multi-account support."""

from datetime import datetime

from textual.app import ComposeResult
from textual.containers import Container, ScrollableContainer
from textual.screen import Screen
from textual.widgets import Button, Label, Static

from ..account_manager import Account, AccountManager


class AccountSelectorScreen(Screen):
    """
    Account selection screen shown on startup.

    Allows users to:
    - Select an existing account to use
    - Add a new account
    - Use demo mode (no account required)
    - Delete accounts (with confirmation)

    Returns selected account ID when dismissed, or special values:
    - "demo" for demo mode
    - "add_new" to trigger add account flow
    - None if user exits
    """

    CSS = """
    AccountSelectorScreen {
        align: center middle;
    }

    #selector-container {
        width: 80;
        height: auto;
        max-height: 90%;
        border: solid $accent;
        background: $surface;
        padding: 2 4;
    }

    #selector-title {
        width: 100%;
        text-align: center;
        text-style: bold;
        color: $accent;
        margin-bottom: 1;
    }

    .selector-help {
        color: $text-muted;
        text-align: center;
        margin-bottom: 2;
    }

    #accounts-scroll {
        width: 100%;
        height: auto;
        max-height: 20;
        border: solid $primary;
        margin-bottom: 2;
    }

    .account-item {
        width: 100%;
        height: auto;
        padding: 1 2;
        background: $boost;
        margin-bottom: 1;
    }

    .account-item:hover {
        background: $primary;
    }

    .account-name {
        text-style: bold;
        color: $text;
    }

    .account-meta {
        color: $text-muted;
        text-style: italic;
    }

    #button-container {
        layout: horizontal;
        width: 100%;
        height: auto;
        align: center middle;
        margin-top: 1;
    }

    #button-container Button {
        margin: 0 1;
    }

    .action-button {
        width: auto;
        min-width: 20;
    }

    #no-accounts-message {
        text-align: center;
        color: $warning;
        margin: 2 0;
    }
    """

    def __init__(self, config_dir: str = None):
        """
        Initialize account selector.

        Args:
            config_dir: Optional config directory (defaults to ~/.moneyflow)
        """
        super().__init__()
        self.config_dir = config_dir
        self.account_manager = AccountManager(config_dir=config_dir)
        # Load accounts immediately so they're available for compose()
        self.accounts = self.account_manager.list_accounts()

    def compose(self) -> ComposeResult:
        """Compose the account selector UI."""
        with Container(id="selector-container"):
            yield Label("💼 Select Account", id="selector-title")

            yield Static(
                "Choose an account to load, or add a new one:",
                classes="selector-help",
            )

            # Scrollable account list
            with ScrollableContainer(id="accounts-scroll"):
                yield from self._render_account_list()

            # Action buttons
            with Container(id="button-container"):
                yield Button(
                    "+ Add New Account", variant="success", id="add-button", classes="action-button"
                )
                yield Button(
                    "🎮 Demo Mode", variant="default", id="demo-button", classes="action-button"
                )
                yield Button("Exit", variant="default", id="exit-button", classes="action-button")


    def _render_account_list(self) -> list:
        """Render list of account items."""
        widgets = []

        if not self.accounts:
            # No accounts configured yet
            widgets.append(
                Static(
                    "No accounts configured. Click 'Add New Account' to get started.",
                    id="no-accounts-message",
                )
            )
            return widgets

        # Render each account
        for account in self.accounts:
            widgets.append(self._create_account_item(account))

        return widgets

    def _create_account_item(self, account: Account) -> Container:
        """
        Create a clickable account item widget.

        Args:
            account: Account to render

        Returns:
            Container with account info and select button
        """
        # Format backend type with icon
        backend_icons = {
            "monarch": "🏦",
            "ynab": "💰",
            "amazon": "📦",
            "demo": "🎮",
        }
        icon = backend_icons.get(account.backend_type, "📊")

        # Format last used date
        if account.last_used:
            try:
                last_used_dt = datetime.fromisoformat(account.last_used)
                last_used_str = f"Last used: {last_used_dt.strftime('%Y-%m-%d %H:%M')}"
            except (ValueError, TypeError):
                last_used_str = "Last used: Unknown"
        else:
            last_used_str = "Never used"

        # Simplified: Use a single button per account with icon, name, and metadata
        # The button includes formatted info and encodes account_id in its ID
        button_label = (
            f"{icon} {account.name}\n  {account.backend_type.capitalize()} • {last_used_str}"
        )

        return Button(
            button_label,
            variant="primary",
            id=f"select-{account.id}",
            classes="account-item",
        )

    async def on_button_pressed(self, event: Button.Pressed) -> None:
        """Handle button presses."""
        button_id = event.button.id

        if button_id == "exit-button":
            self.dismiss(None)  # User chose to exit
            return

        if button_id == "demo-button":
            self.dismiss("demo")  # Special value for demo mode
            return

        if button_id == "add-button":
            self.dismiss("add_new")  # Special value to trigger add account flow
            return

        # Check if it's an account select button
        if button_id and button_id.startswith("select-"):
            account_id = button_id.replace("select-", "")
            # Update last used timestamp
            self.account_manager.update_last_used(account_id)
            self.dismiss(account_id)
            return
