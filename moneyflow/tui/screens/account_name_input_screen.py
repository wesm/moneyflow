"""Account name input screen for adding new accounts."""

from textual.app import ComposeResult
from textual.binding import Binding
from textual.containers import Container
from textual.screen import ModalScreen
from textual.widgets import Button, Input, Label, Static


class AccountNameInputScreen(ModalScreen):
    """
    Screen to prompt user for account name when adding a new account.

    Shows backend type and asks for a friendly display name.
    Returns the account name when dismissed, or None if canceled.

    Keyboard shortcuts:
    - Enter: Continue with entered name (also from input field)
    - Esc: Cancel
    """

    BINDINGS = [
        Binding("escape", "cancel", "Cancel", show=False),
    ]

    CSS = """
    AccountNameInputScreen {
        align: center middle;
    }

    #name-container {
        width: 60;
        height: auto;
        border: solid $accent;
        background: $surface;
        padding: 2 4;
    }

    #name-title {
        width: 100%;
        text-align: center;
        text-style: bold;
        color: $accent;
        margin-bottom: 1;
    }

    .name-label {
        margin-top: 1;
        color: $text;
    }

    .name-input {
        margin-bottom: 1;
    }

    .name-help {
        color: $text-muted;
        text-style: italic;
        margin-bottom: 2;
    }

    #button-container {
        layout: horizontal;
        width: 100%;
        height: auto;
        align: center middle;
        margin-top: 2;
    }

    #button-container Button {
        margin: 0 1;
    }

    #error-label {
        color: $error;
        text-align: center;
        margin-top: 1;
    }
    """

    def __init__(self, backend_type: str):
        """
        Initialize account name input screen.

        Args:
            backend_type: Backend type being added (monarch, ynab, etc.)
        """
        super().__init__()
        self.backend_type = backend_type

    def compose(self) -> ComposeResult:
        """Compose the account name input UI."""
        # Backend-specific prompts
        backend_labels = {
            "monarch": "Monarch Money",
            "ynab": "YNAB",
            "amazon": "Amazon Purchases",
        }
        backend_label = backend_labels.get(self.backend_type, self.backend_type.capitalize())

        # Example names based on backend
        examples = {
            "monarch": "Personal, Business, Joint",
            "ynab": "Main Budget, 2025 Budget",
            "amazon": "Purchases, Orders",
        }
        example = examples.get(self.backend_type, "My Account")

        with Container(id="name-container"):
            yield Label(f"📝 Name Your {backend_label} Account", id="name-title")

            yield Static(
                "Enter a friendly name to identify this account.\n"
                "Keys: Enter=Continue | Esc=Cancel",
                classes="name-help",
            )

            yield Label("Account Name:", classes="name-label")
            yield Input(
                placeholder=f"e.g., {example}",
                id="name-input",
                classes="name-input",
            )

            yield Static(
                "This name will appear in the account selector.",
                classes="name-help",
            )

            yield Static("", id="error-label")

            with Container(id="button-container"):
                yield Button("Continue", variant="primary", id="continue-button")
                yield Button("Cancel", variant="default", id="cancel-button")

    def on_mount(self) -> None:
        """Focus the input field when screen loads."""
        self.query_one("#name-input", Input).focus()

    async def on_button_pressed(self, event: Button.Pressed) -> None:
        """Handle button presses."""
        if event.button.id == "cancel-button":
            self.dismiss(None)
            return

        if event.button.id == "continue-button":
            await self._validate_and_submit()
            return

    async def on_input_submitted(self, event: Input.Submitted) -> None:
        """Handle Enter key in input field."""
        if event.input.id == "name-input":
            await self._validate_and_submit()

    async def _validate_and_submit(self) -> None:
        """Validate account name and submit if valid."""
        name_input = self.query_one("#name-input", Input)
        error_label = self.query_one("#error-label", Static)

        account_name = name_input.value.strip()

        # Validate name
        if not account_name:
            error_label.update("⚠ Account name cannot be empty")
            return

        if len(account_name) < 2:
            error_label.update("⚠ Account name must be at least 2 characters")
            return

        if len(account_name) > 50:
            error_label.update("⚠ Account name must be 50 characters or less")
            return

        # Valid - dismiss with account name
        self.dismiss(account_name)

    def action_cancel(self) -> None:
        """Cancel account name input (Esc key)."""
        self.dismiss(None)
