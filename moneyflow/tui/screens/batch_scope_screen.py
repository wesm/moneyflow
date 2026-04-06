"""Prompt user to choose batch scope for merchant rename."""

from textual.app import ComposeResult
from textual.containers import Container
from textual.events import Key
from textual.screen import ModalScreen
from textual.widgets import Button, Label, Static


class BatchScopeScreen(ModalScreen):
    """
    Prompt user to choose batch scope for merchant rename.

    When YNAB batch update would affect more transactions than selected
    in the queue, this screen lets the user choose:

    - "Rename all N" - Use batch payee update (affects all transactions)
    - "Rename selected N only" - Use individual transaction updates

    Attributes:
        merchant_name: The old merchant name being renamed
        selected_count: Number of transactions in the queue with this rename
        total_count: Total transactions on backend with this merchant
    """

    CSS = """
    BatchScopeScreen {
        background: $surface;
        align: center middle;
    }

    #batch-scope-container {
        width: 60;
        height: auto;
        padding: 1 2;
        background: $panel;
        border: solid $accent;
    }

    #batch-scope-title {
        text-style: bold;
        color: $accent;
        margin-bottom: 1;
    }

    #batch-scope-message {
        color: $text;
        margin-bottom: 1;
    }

    #batch-scope-help {
        color: $text-muted;
        margin-bottom: 1;
    }

    #button-container {
        layout: vertical;
        width: 100%;
        align: center middle;
    }

    #button-container Button {
        margin: 0 0 1 0;
        min-width: 40;
    }
    """

    def __init__(
        self,
        merchant_name: str,
        selected_count: int,
        total_count: int,
    ):
        """
        Initialize batch scope screen.

        Args:
            merchant_name: The old merchant name being renamed
            selected_count: Number of transactions in queue with this rename
            total_count: Total transactions on backend with this merchant
        """
        super().__init__()
        self.merchant_name = merchant_name
        self.selected_count = selected_count
        self.total_count = total_count

    def compose(self) -> ComposeResult:
        with Container(id="batch-scope-container"):
            yield Label(f"Rename '{self.merchant_name}'", id="batch-scope-title")
            yield Static(
                f"You selected {self.selected_count} transaction(s), "
                f"but {self.total_count} exist with this payee.",
                id="batch-scope-message",
            )
            yield Static(
                "Choose how to apply this rename:",
                id="batch-scope-help",
            )
            with Container(id="button-container"):
                yield Button(
                    f"Rename all {self.total_count}",
                    variant="primary",
                    id="all",
                )
                yield Button(
                    f"Rename selected {self.selected_count} only",
                    variant="default",
                    id="selected",
                )
                yield Button(
                    "Cancel",
                    variant="error",
                    id="cancel",
                )

    async def on_button_pressed(self, event: Button.Pressed) -> None:
        """Handle button clicks - return the button ID as result."""
        self.dismiss(event.button.id)  # "all", "selected", or "cancel"

    def on_key(self, event: Key) -> None:
        """Handle keyboard shortcuts."""
        if event.key == "escape":
            event.stop()
            self.dismiss("cancel")
        elif event.key == "1":
            event.stop()
            self.dismiss("all")
        elif event.key == "2":
            event.stop()
            self.dismiss("selected")
        # Let Tab, Arrow keys, Enter, etc. pass through for button navigation
