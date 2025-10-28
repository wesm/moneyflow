"""Transaction detail view screen."""

from textual.app import ComposeResult
from textual.containers import Container, VerticalScroll
from textual.events import Key
from textual.screen import ModalScreen
from textual.widgets import Label, Static

from ..formatters import ViewPresenter


class TransactionDetailScreen(ModalScreen):
    """Modal showing all transaction fields from the API."""

    CSS = """
    TransactionDetailScreen {
        align: center middle;
    }

    #detail-dialog {
        width: 80;
        height: auto;
        max-height: 40;
        border: thick $primary;
        background: $surface;
        padding: 2 4;
    }

    #detail-title {
        width: 100%;
        text-align: center;
        text-style: bold;
        color: $accent;
        margin-bottom: 2;
    }

    #detail-content {
        height: 30;
        border: solid $panel;
        padding: 1;
    }

    .field-label {
        color: $accent;
        text-style: bold;
        margin-top: 1;
    }

    .field-value {
        color: $text;
        margin-left: 2;
        margin-bottom: 1;
    }

    #close-hint {
        text-align: center;
        color: $text-muted;
        margin-top: 1;
        text-style: italic;
    }
    """

    def __init__(self, transaction_data: dict):
        super().__init__()
        self.transaction_data = transaction_data

    def compose(self) -> ComposeResult:
        with Container(id="detail-dialog"):
            yield Label("📋 Transaction Details", id="detail-title")

            with VerticalScroll(id="detail-content"):
                # Core fields
                yield Label("ID:", classes="field-label")
                yield Static(str(self.transaction_data.get("id", "N/A")), classes="field-value")

                yield Label("Date:", classes="field-label")
                yield Static(str(self.transaction_data.get("date", "N/A")), classes="field-value")

                yield Label("Amount:", classes="field-label")
                amount = self.transaction_data.get("amount", 0)
                yield Static(ViewPresenter.format_amount(amount), classes="field-value")

                yield Label("Merchant:", classes="field-label")
                yield Static(
                    str(self.transaction_data.get("merchant", "N/A")), classes="field-value"
                )

                yield Label("Merchant ID:", classes="field-label")
                yield Static(
                    str(self.transaction_data.get("merchant_id", "N/A")), classes="field-value"
                )

                yield Label("Category:", classes="field-label")
                yield Static(
                    str(self.transaction_data.get("category", "N/A")), classes="field-value"
                )

                yield Label("Category ID:", classes="field-label")
                yield Static(
                    str(self.transaction_data.get("category_id", "N/A")), classes="field-value"
                )

                yield Label("Group:", classes="field-label")
                yield Static(str(self.transaction_data.get("group", "N/A")), classes="field-value")

                yield Label("Account:", classes="field-label")
                yield Static(
                    str(self.transaction_data.get("account", "N/A")), classes="field-value"
                )

                yield Label("Account ID:", classes="field-label")
                yield Static(
                    str(self.transaction_data.get("account_id", "N/A")), classes="field-value"
                )

                # Additional fields
                yield Label("Notes:", classes="field-label")
                notes = self.transaction_data.get("notes", "")
                yield Static(notes if notes else "(none)", classes="field-value")

                yield Label("Hidden from Reports:", classes="field-label")
                hidden = self.transaction_data.get("hideFromReports", False)
                yield Static("Yes" if hidden else "No", classes="field-value")

                yield Label("Pending:", classes="field-label")
                pending = self.transaction_data.get("pending", False)
                yield Static("Yes" if pending else "No", classes="field-value")

                yield Label("Recurring:", classes="field-label")
                recurring = self.transaction_data.get("isRecurring", False)
                yield Static("Yes" if recurring else "No", classes="field-value")

            yield Static("Esc/Enter=Close", id="close-hint")

    def on_key(self, event: Key) -> None:
        """Handle keyboard shortcuts."""
        if event.key in ("escape", "enter"):
            self.dismiss()
            event.stop()  # Prevent event from propagating to parent app
