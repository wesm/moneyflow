"""Review pending changes before committing."""

from textual.app import ComposeResult
from textual.containers import Container
from textual.events import Key
from textual.screen import ModalScreen
from textual.widgets import Button, DataTable, Label, Static


class ReviewChangesScreen(ModalScreen):
    """Screen to review pending changes before committing to API."""

    CSS = """
    ReviewChangesScreen {
        background: $surface;
    }

    #review-container {
        height: 100%;
        padding: 1 2;
    }

    #review-header {
        height: 5;
        background: $panel;
        padding: 1;
        margin-bottom: 1;
    }

    #review-title {
        text-style: bold;
        color: $accent;
    }

    #review-help {
        color: $text-muted;
        margin-top: 1;
    }

    #changes-table {
        height: 1fr;
        border: solid $accent;
    }

    #review-footer {
        height: 5;
        background: $panel;
        padding: 1;
        dock: bottom;
    }

    .footer-instructions {
        color: $accent;
        text-align: center;
        text-style: bold;
        margin-bottom: 1;
    }

    #button-container {
        layout: horizontal;
        width: 100%;
        align: center middle;
    }

    #button-container Button {
        margin: 0 1;
        min-width: 20;
    }
    """

    def __init__(self, pending_edits: list, categories: dict = None):
        super().__init__()
        self.pending_edits = pending_edits
        self.categories = categories or {}

    def compose(self) -> ComposeResult:
        with Container(id="review-container"):
            with Container(id="review-header"):
                yield Label(
                    f"📝 Review {len(self.pending_edits)} Pending Change(s)", id="review-title"
                )
                yield Static("Review changes below", id="review-help")

            yield DataTable(id="changes-table", cursor_type="row", zebra_stripes=True)

            with Container(id="review-footer"):
                yield Static("Enter=Commit | Esc=Cancel", classes="footer-instructions")
                with Container(id="button-container"):
                    yield Button("Commit Changes (Enter)", variant="primary", id="commit-button")
                    yield Button("Cancel (Esc)", variant="default", id="cancel-button")

    async def on_mount(self) -> None:
        """Populate the changes table."""
        table = self.query_one("#changes-table", DataTable)

        # Add columns
        table.add_column("Type", key="type", width=12)
        table.add_column("Transaction", key="transaction", width=15)
        table.add_column("Field", key="field", width=15)
        table.add_column("Old Value", key="old", width=30)
        table.add_column("New Value", key="new", width=30)

        # Add rows for each pending edit
        for edit in self.pending_edits:
            edit_type = (
                "Merchant"
                if edit.field == "merchant"
                else "Category"
                if edit.field == "category"
                else "Hide"
            )
            txn_id_short = edit.transaction_id[:12] + "..."

            # For category changes, show category names not IDs
            if edit.field == "category":
                old_val = self.categories.get(edit.old_value, {}).get("name", edit.old_value)
                new_val = self.categories.get(edit.new_value, {}).get("name", edit.new_value)
            else:
                old_val = str(edit.old_value)
                new_val = str(edit.new_value)

            # Truncate if too long
            old_val = old_val[:29] if len(old_val) > 29 else old_val
            new_val = new_val[:29] if len(new_val) > 29 else new_val

            table.add_row(edit_type, txn_id_short, edit.field, old_val, new_val)

    async def on_button_pressed(self, event: Button.Pressed) -> None:
        """Handle button clicks."""
        if event.button.id == "commit-button":
            self.dismiss(True)  # Confirm commit
        elif event.button.id == "cancel-button":
            self.dismiss(False)  # Cancel

    def on_key(self, event: Key) -> None:
        """Handle keyboard shortcuts."""
        if event.key == "escape":
            event.stop()  # Prevent event from bubbling to parent
            self.dismiss(False)  # Cancel
        elif event.key == "enter":
            event.stop()  # Prevent event from bubbling
            self.dismiss(True)  # Commit
        else:
            # Stop all other keys from bubbling to parent app
            # This prevents accidental activation of app-level shortcuts (like 'c' for edit category)
            event.stop()
