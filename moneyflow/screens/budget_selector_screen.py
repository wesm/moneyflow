"""Budget selection screen for YNAB."""

from typing import Dict, List, Optional

from textual.app import ComposeResult
from textual.binding import Binding
from textual.containers import Container
from textual.screen import ModalScreen
from textual.widgets import Button, DataTable, Label, Static


class BudgetSelectorScreen(ModalScreen):
    """
    Budget selection screen for YNAB setup.

    Keyboard shortcuts:
    - Up/Down: Navigate between budgets
    - Enter: Select highlighted budget
    - Esc: Exit/Cancel
    """

    BINDINGS = [
        Binding("escape", "exit_budget_selector", "Exit", show=False),
        Binding("enter", "select_current", "Select", show=False),
    ]

    CSS = """
    BudgetSelectorScreen {
        align: center middle;
    }

    #budget-container {
        width: 80;
        height: 50%;
        min-height: 20;
        border: solid $accent;
        background: $surface;
        padding: 2 4;
    }

    #budget-title {
        width: 100%;
        text-align: center;
        text-style: bold;
        color: $accent;
        margin-bottom: 1;
    }

    .budget-help {
        color: $text-muted;
        text-align: center;
        margin-bottom: 2;
    }

    #budget-table {
        width: 100%;
        height: 1fr;
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

    def __init__(self, budgets: List[Dict[str, str]]):
        """
        Initialize budget selector with available budgets.

        Args:
            budgets: List of budget dictionaries with 'id', 'name', and 'last_modified_on' fields
        """
        super().__init__()
        self.budgets = budgets
        self.selected_budget_id: Optional[str] = None

    def compose(self) -> ComposeResult:
        with Container(id="budget-container"):
            yield Label("💰 Select YNAB Budget", id="budget-title")

            yield Static(
                "Choose which budget to use for moneyflow.\n"
                "Keys: ↑/↓=Navigate | Enter=Select | Esc=Cancel",
                classes="budget-help",
            )

            # Create data table for budgets
            table = DataTable(id="budget-table")
            yield table

            with Container(id="button-container"):
                yield Button("Select", variant="primary", id="select-button")
                yield Button("Cancel", variant="default", id="cancel-button")

            yield Label("", id="error-label")

    def on_mount(self) -> None:
        """Set up the budget table when screen mounts."""
        table = self.query_one("#budget-table", DataTable)

        # Add columns
        table.add_column("Budget Name", width=40)
        table.add_column("Last Modified", width=25)

        # Add rows for each budget
        for budget in self.budgets:
            last_modified = budget.get("last_modified_on", "Unknown")
            if last_modified and last_modified != "Unknown":
                # Parse and format the date for better display
                try:
                    from datetime import datetime

                    dt = datetime.fromisoformat(last_modified.replace("Z", "+00:00"))
                    last_modified = dt.strftime("%Y-%m-%d %H:%M")
                except Exception:
                    pass

            table.add_row(budget["name"], last_modified, key=budget["id"])

        # Focus the table
        table.focus()

    async def on_button_pressed(self, event: Button.Pressed) -> None:
        if event.button.id == "cancel-button":
            self.dismiss(None)
            return

        if event.button.id == "select-button":
            await self.select_current_budget()

    def on_data_table_row_selected(self, event: DataTable.RowSelected) -> None:
        """Store the selected budget ID when a row is selected."""
        self.selected_budget_id = event.row_key.value

    def action_exit_budget_selector(self) -> None:
        """Exit budget selector (Esc key)."""
        self.dismiss(None)

    def action_select_current(self) -> None:
        """Select currently highlighted budget (Enter key)."""
        table = self.query_one("#budget-table", DataTable)
        if table.cursor_row is not None and table.cursor_row < len(self.budgets):
            # Get the budget ID from the row key
            row_key = table.get_row_at(table.cursor_row)[0]  # Get the key of the row
            self.selected_budget_id = row_key
            self.dismiss(self.selected_budget_id)

    async def select_current_budget(self) -> None:
        """Select the currently highlighted budget."""
        table = self.query_one("#budget-table", DataTable)
        if table.cursor_row is not None:
            # Get budget ID from cursor position
            budget = self.budgets[table.cursor_row]
            self.dismiss(budget["id"])
        else:
            error_label = self.query_one("#error-label", Label)
            error_label.update("❌ Please select a budget")
