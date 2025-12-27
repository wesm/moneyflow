"""Transaction detail view screen."""

from typing import List, Optional

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
        max-height: 50;
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
        height: 40;
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

    .amazon-section-title {
        color: $warning;
        text-style: bold;
        margin-bottom: 1;
        padding: 0 0 1 0;
        border-bottom: solid $panel;
    }

    .amazon-no-match {
        color: $text-muted;
        text-style: italic;
        margin-bottom: 2;
    }

    .amazon-order-header {
        color: $accent;
        margin-top: 1;
    }

    .amazon-order-info {
        color: $text-muted;
        margin-left: 2;
    }

    .amazon-item {
        color: $text;
        margin-left: 4;
    }

    .amazon-total {
        color: $text;
        text-style: bold;
        margin-left: 2;
        margin-top: 1;
    }

    .amazon-fuzzy-info {
        color: $text-muted;
        text-style: italic;
        margin-left: 2;
    }
    """

    def __init__(
        self,
        transaction_data: dict,
        amazon_matches: Optional[List] = None,
        amazon_searched: bool = False,
    ):
        super().__init__()
        self.transaction_data = transaction_data
        self.amazon_matches = amazon_matches or []
        self.amazon_searched = amazon_searched

    def compose(self) -> ComposeResult:
        with Container(id="detail-dialog"):
            yield Label("Transaction Details", id="detail-title")

            with VerticalScroll(id="detail-content"):
                # Amazon matches section (at top if searched)
                if self.amazon_searched:
                    yield from self._compose_amazon_section()

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

    def _compose_amazon_section(self) -> ComposeResult:
        """Compose the Amazon matches section."""
        yield Label("Matching Amazon Orders", classes="amazon-section-title")

        if not self.amazon_matches:
            yield Static("No matching orders found", classes="amazon-no-match")
            return

        for match in self.amazon_matches:
            # Order header with confidence marker
            if match.confidence == "high":
                confidence_marker = "*"
            elif match.confidence == "likely":
                confidence_marker = "~"
            else:
                confidence_marker = ""

            yield Label(
                f"Order: {match.order_id}{confidence_marker}",
                classes="amazon-order-header",
            )
            yield Static(
                f"Date: {match.order_date} | From: {match.source_profile}",
                classes="amazon-order-info",
            )

            # Items
            for item in match.items:
                qty_str = f" (x{item['quantity']})" if item["quantity"] > 1 else ""
                amount_str = ViewPresenter.format_amount(item["amount"])
                yield Static(
                    f"  {item['name']}{qty_str}: {amount_str}",
                    classes="amazon-item",
                )

            # Total
            total_str = ViewPresenter.format_amount(match.total_amount)
            yield Static(f"Total: {total_str}", classes="amazon-total")

            # For fuzzy matches, show the gift card info
            if match.confidence == "likely" and match.amount_difference is not None:
                gift_card_str = ViewPresenter.format_amount(-match.amount_difference)
                yield Static(
                    f"(Likely match: ~{gift_card_str} gift card used)",
                    classes="amazon-fuzzy-info",
                )

    def on_key(self, event: Key) -> None:
        """Handle keyboard shortcuts."""
        if event.key in ("escape", "enter"):
            self.dismiss()
            event.stop()  # Prevent event from propagating to parent app
