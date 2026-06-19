"""Modal screen for choosing export format and scope."""

from textual.app import ComposeResult
from textual.containers import Container
from textual.events import Key
from textual.screen import ModalScreen
from textual.widgets import Button, Label, RadioButton, RadioSet

from ...data.exporter import ExportFormat, ExportScope


class ExportScreen(ModalScreen):
    """
    Modal screen for selecting export format and scope.

    Returns a tuple of (ExportFormat, ExportScope) on confirm,
    or None if the user cancels.
    """

    DEFAULT_FOCUS = "#format-select"

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

    RadioSet {
        margin: 0 0 0 0;
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
            with RadioSet(id="format-select"):
                for fmt in ExportFormat:
                    btn = RadioButton(fmt.display_name, value=(fmt == ExportFormat.PARQUET))
                    btn.export_format = fmt
                    yield btn
            yield Label("Scope:", classes="section-label")
            with RadioSet(id="scope-select"):
                for scope in ExportScope:
                    btn = RadioButton(scope.display_name, value=(scope == ExportScope.FULL))
                    btn.export_scope = scope
                    yield btn
            with Container(id="button-container"):
                yield Button("Export", variant="primary", id="export")
                yield Button("Cancel", variant="error", id="cancel")

    def on_mount(self) -> None:
        """Focus the format selector on mount."""
        self.query_one("#format-select", RadioSet).focus()

    async def on_button_pressed(self, event: Button.Pressed) -> None:
        """Handle button clicks."""
        if event.button.id == "export":
            format_set = self.query_one("#format-select", RadioSet)
            scope_set = self.query_one("#scope-select", RadioSet)
            selected_btn = format_set.pressed_button
            export_format = selected_btn.export_format if selected_btn else ExportFormat.PARQUET
            selected_scope_btn = scope_set.pressed_button
            export_scope = (
                selected_scope_btn.export_scope if selected_scope_btn else ExportScope.FULL
            )
            self.dismiss((export_format, export_scope))
        else:
            self.dismiss(None)

    def on_key(self, event: Key) -> None:
        """Handle keyboard shortcuts."""
        if event.key == "escape":
            event.stop()
            self.dismiss(None)
