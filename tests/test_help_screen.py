"""Behavioral tests for the keyboard-help modal."""

import pytest
from textual.app import App
from textual.containers import VerticalScroll
from textual.widgets import Button, Static

from moneyflow.tui.widgets.help_screen import HelpScreen


class HelpHost(App[None]):
    """Minimal Textual host for the modal screen."""

    def on_mount(self) -> None:
        self.push_screen(HelpScreen())


@pytest.mark.asyncio
async def test_help_footer_scroll_and_close_keys() -> None:
    app = HelpHost()
    async with app.run_test(size=(80, 24)) as pilot:
        await pilot.pause()
        screen = app.screen
        assert isinstance(screen, HelpScreen)
        assert str(screen.query_one("#help-footer", Static).render()) == (
            "j/k=Scroll | Esc/Enter=Close"
        )
        assert str(screen.query_one("#close-button", Button).label) == "Close (Enter)"

        content = screen.query_one("#help-content", VerticalScroll)
        for _ in range(5):
            await pilot.press("j")
        await pilot.pause()
        assert content.scroll_y > 0

        await pilot.press("q")
        assert isinstance(app.screen, HelpScreen)
        await pilot.press("enter")
        assert not isinstance(app.screen, HelpScreen)
