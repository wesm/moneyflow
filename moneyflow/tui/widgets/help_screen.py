"""Help screen modal showing all keyboard shortcuts."""

from textual.app import ComposeResult
from textual.containers import Container, VerticalScroll
from textual.events import Key
from textual.screen import ModalScreen
from textual.widgets import Button, Static

from ..keybindings import get_help_text


class HelpScreen(ModalScreen):
    """Modal screen showing keyboard shortcuts and help information."""

    CSS = """
    HelpScreen {
        align: center middle;
    }

    #help-dialog {
        width: 80;
        height: auto;
        max-height: 90%;
        border: thick $primary;
        background: $surface;
        padding: 1 2;
    }

    #help-title {
        text-align: center;
        text-style: bold;
        color: $accent;
        margin-bottom: 1;
    }

    #help-content {
        height: auto;
        max-height: 30;
        border: solid $panel;
        background: $panel;
        padding: 1;
        margin-bottom: 1;
    }

    #help-footer {
        text-align: center;
        color: $text-muted;
    }

    Button {
        width: 100%;
    }
    """

    def compose(self) -> ComposeResult:
        """Compose the help screen."""
        with Container(id="help-dialog"):
            yield Static("moneyflow - Help", id="help-title")
            with VerticalScroll(id="help-content"):
                yield Static(get_help_text())
            yield Static("Esc=Close", id="help-footer")
            yield Button("Close", variant="primary", id="close-button")

    def on_button_pressed(self, event: Button.Pressed) -> None:
        """Handle button press."""
        self.dismiss()

    def on_key(self, event: Key) -> None:
        """Handle keyboard input."""
        if event.key == "escape" or event.key == "q" or event.key == "question_mark":
            self.dismiss()
