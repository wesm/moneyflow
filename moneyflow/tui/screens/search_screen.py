"""Search screen with live filtering."""

from textual.app import ComposeResult
from textual.containers import Container
from textual.events import Key
from textual.screen import ModalScreen
from textual.widgets import Input, Label, Static


class SearchScreen(ModalScreen):
    """Modal for entering search query with live preview."""

    CSS = """
    SearchScreen {
        align: center middle;
    }

    #search-dialog {
        width: 70;
        height: auto;
        border: thick $primary;
        background: $surface;
        padding: 2 4;
    }

    #search-title {
        width: 100%;
        text-align: center;
        text-style: bold;
        color: $accent;
        margin-bottom: 1;
    }

    #search-help {
        color: $text-muted;
        text-align: center;
        margin-bottom: 2;
    }

    #search-input {
        margin: 1 0;
    }

    #search-stats {
        color: $text-muted;
        text-align: center;
        margin: 1 0;
    }
    """

    def __init__(self, current_query: str = ""):
        super().__init__()
        self.current_query = current_query

    def compose(self) -> ComposeResult:
        with Container(id="search-dialog"):
            yield Label("🔍 Search Transactions", id="search-title")

            yield Static(
                "Type to search merchant or category names\n"
                "Press Enter with empty search to clear filter",
                id="search-help",
            )

            yield Input(
                placeholder="Search... (Enter to apply, Esc to cancel)",
                value=self.current_query,
                id="search-input",
            )

            yield Static("", id="search-stats")

    async def on_mount(self) -> None:
        """Focus search input on load."""
        self.query_one("#search-input", Input).focus()

    async def on_input_submitted(self, event: Input.Submitted) -> None:
        """Handle Enter key - apply search."""
        query = event.value.strip()
        event.stop()  # Prevent propagation
        self.dismiss(query)  # Empty string clears the search

    def on_key(self, event: Key) -> None:
        """Handle keyboard shortcuts."""
        if event.key == "escape":
            event.stop()  # Prevent propagation
            # Cancel - return None to indicate no change
            self.dismiss(None)
