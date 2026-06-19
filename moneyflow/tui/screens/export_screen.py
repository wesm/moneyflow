"""Modal screen for choosing export format and scope."""

from textual.app import ComposeResult
from textual.containers import Container
from textual.events import Key
from textual.screen import ModalScreen
from textual.widgets import Button, Label

from ...data.exporter import ExportFormat, ExportScope


class ExportScreen(ModalScreen):
    """
    Modal screen for selecting export format and scope.

    Returns a tuple of (ExportFormat, ExportScope) on confirm,
    or None if the user cancels.
    """

    CSS = """
    ExportScreen {
        background: $surface;
        align: center middle;
    }

    #export-container {
        width: 50;
        height: auto;
        padding: 1 2;
        background: $panel;
        border: solid $accent;
    }

    #export-title {
        text-style: bold;
        color: $accent;
        margin-bottom: 1;
        content-align: center top;
    }

    .section-label {
        color: $text;
        margin-top: 1;
        margin-bottom: 0;
    }

    #button-container {
        layout: horizontal;
        width: 100%;
        margin-top: 2;
        align: center middle;
    }

    #button-container Button {
        margin: 0 1;
        min-width: 20;
    }
    """

    def compose(self) -> ComposeResult:
        with Container(id="export-container"):
            yield Label("Export Data", id="export-title")
            yield Label("Format:", classes="section-label")
            yield Label(ExportFormat.PARQUET.display_name, id="format-value")
            yield Label("Scope:", classes="section-label")
            yield Label(ExportScope.FULL.display_name, id="scope-value")
            with Container(id="button-container"):
                yield Button("Export", variant="primary", id="export")
                yield Button("Cancel", variant="error", id="cancel")

    def on_mount(self) -> None:
        """Focus the Export button on mount so Enter triggers default action."""
        self.query_one("#export", Button).focus()

    async def on_button_pressed(self, event: Button.Pressed) -> None:
        """Handle button clicks."""
        if event.button.id == "export":
            self.dismiss((ExportFormat.PARQUET, ExportScope.FULL))
        else:
            self.dismiss(None)

    def on_key(self, event: Key) -> None:
        """Handle keyboard shortcuts."""
        if event.key == "escape":
            event.stop()
            self.dismiss(None)
