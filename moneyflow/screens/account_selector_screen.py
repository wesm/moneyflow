"""Account selection screen for multi-account support."""

from datetime import datetime

from textual.app import ComposeResult
from textual.binding import Binding
from textual.containers import Container, ScrollableContainer
from textual.screen import Screen
from textual.widgets import Button, Label, Static

from ..account_manager import Account, AccountManager


class AccountSelectorScreen(Screen):
    """
    Account selection screen shown on startup.

    Allows users to:
    - Navigate accounts with Up/Down arrows
    - Select account with Enter
    - Add new account (a/n key or button)
    - Use demo mode (d key or button)
    - Exit (Esc/q or button)

    Returns selected account ID when dismissed, or special values:
    - "demo" for demo mode
    - "add_new" to trigger add account flow
    - None if user exits

    Keyboard shortcuts:
    - Up/Down: Navigate accounts
    - Enter: Select highlighted account
    - a/n: Add new account
    - d: Demo mode
    - Esc/q: Exit
    - Tab/Shift+Tab: Navigate buttons
    """

    BINDINGS = [
        Binding("escape,q", "exit_selector", "Exit", show=False),
        Binding("a,n", "add_account", "Add New", show=False),
        Binding("d", "demo_mode", "Demo", show=False),
        Binding("up", "cursor_up", "Up", show=False, priority=True),
        Binding("down", "cursor_down", "Down", show=False, priority=True),
        Binding("enter", "select_current", "Select", show=False),
        Binding("j", "cursor_down", "Down (j)", show=False),
        Binding("k", "cursor_up", "Up (k)", show=False),
    ]

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
        padding: 0 1;
        background: $boost;
        margin-bottom: 1;
        border: solid transparent;
    }

    .account-item:hover {
        background: $primary;
    }

    .account-item:focus {
        background: $accent;
        border: thick $accent;
        text-style: bold;
        color: $text;
    }

    Button.account-item:focus {
        background: $accent;
        border: thick $accent;
        text-style: bold;
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
        min-width: 15;
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
        # Track currently highlighted account index (for arrow key navigation)
        self.current_index = 0

    def compose(self) -> ComposeResult:
        """Compose the account selector UI."""
        with Container(id="selector-container"):
            yield Label("💼 Select Account", id="selector-title")

            yield Static(
                "Choose an account to load, or add a new one.\n"
                "Keys: ↑/↓ or j/k=Navigate | Enter=Select | a=Add | d=Demo | Esc/q=Exit",
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

    def on_mount(self) -> None:
        """Set focus to first account button when screen loads."""
        if self.accounts:
            # Focus the first account button
            first_button_id = f"select-{self.accounts[0].id}"
            try:
                button = self.query_one(f"#{first_button_id}", Button)
                button.focus()
            except Exception:
                pass  # Button might not exist yet

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
        # Visual highlighting is handled by CSS :focus pseudo-class
        button_label = (
            f"{icon} {account.name}\n  {account.backend_type.capitalize()} • {last_used_str}"
        )

        return Button(
            button_label,
            variant="default",
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

    def action_exit_selector(self) -> None:
        """Exit the account selector (Esc/q key)."""
        self.dismiss(None)

    def action_add_account(self) -> None:
        """Trigger add new account flow (a/n key)."""
        self.dismiss("add_new")

    def action_demo_mode(self) -> None:
        """Launch demo mode (d key)."""
        self.dismiss("demo")

    def action_cursor_up(self) -> None:
        """Move selection up (↑ key)."""
        if not self.accounts:
            return

        # Move to previous account (wrap around)
        self.current_index = (self.current_index - 1) % len(self.accounts)
        self._focus_current_account()

    def action_cursor_down(self) -> None:
        """Move selection down (↓ key)."""
        if not self.accounts:
            return

        # Move to next account (wrap around)
        self.current_index = (self.current_index + 1) % len(self.accounts)
        self._focus_current_account()

    def action_select_current(self) -> None:
        """Select currently highlighted account (Enter key)."""
        if not self.accounts or self.current_index >= len(self.accounts):
            return

        account = self.accounts[self.current_index]
        # Update last used timestamp
        self.account_manager.update_last_used(account.id)
        self.dismiss(account.id)

    def _focus_current_account(self) -> None:
        """Focus the button for the currently selected account."""
        if self.current_index < len(self.accounts):
            account = self.accounts[self.current_index]
            button_id = f"select-{account.id}"
            try:
                button = self.query_one(f"#{button_id}", Button)
                button.focus()
            except Exception:
                pass  # Button might not exist yet
