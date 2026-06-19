"""Tests for ExportScreen modal."""

from typing import cast

import pytest
from textual.app import App
from textual.widgets import Button, Label

from moneyflow.data.exporter import ExportFormat, ExportScope
from moneyflow.tui.screens.export_screen import ExportScreen


class ExportTestApp(App):
    """Minimal app for testing ExportScreen."""

    pass


@pytest.fixture
def test_app():
    """Create a minimal test app."""
    return ExportTestApp()


@pytest.fixture
def capture():
    """Fixture to capture screen dismiss callbacks."""
    container = {"result": None}

    def callback(value):
        container["result"] = value

    return container, callback


class TestExportScreenDisplay:
    """Test ExportScreen displays correctly."""

    async def test_title_renders(self, test_app):
        """Test screen shows the title."""
        async with test_app.run_test() as pilot:
            screen = ExportScreen()
            test_app.push_screen(screen)
            await pilot.pause()

            title_label = cast(Label, screen.query_one("#export-title"))
            assert "Export Data" in str(title_label.render())

    async def test_format_label_shows_parquet(self, test_app):
        """Test format section shows Parquet."""
        async with test_app.run_test() as pilot:
            screen = ExportScreen()
            test_app.push_screen(screen)
            await pilot.pause()

            format_label = cast(Label, screen.query_one("#format-value"))
            rendered = str(format_label.render())
            assert ExportFormat.PARQUET.display_name in rendered

    async def test_scope_label_shows_full(self, test_app):
        """Test scope section shows Full dataset."""
        async with test_app.run_test() as pilot:
            screen = ExportScreen()
            test_app.push_screen(screen)
            await pilot.pause()

            scope_label = cast(Label, screen.query_one("#scope-value"))
            rendered = str(scope_label.render())
            assert ExportScope.FULL.display_name in rendered

    async def test_has_export_and_cancel_buttons(self, test_app):
        """Test both buttons are present."""
        async with test_app.run_test() as pilot:
            screen = ExportScreen()
            test_app.push_screen(screen)
            await pilot.pause()

            export_button = cast(Button, screen.query_one("#export"))
            cancel_button = cast(Button, screen.query_one("#cancel"))
            assert export_button is not None
            assert cancel_button is not None

    async def test_export_button_focused_on_mount(self, test_app):
        """Test Export button receives initial focus."""
        async with test_app.run_test() as pilot:
            screen = ExportScreen()
            test_app.push_screen(screen)
            await pilot.pause()

            focused = screen.focused
            assert focused is not None
            assert focused.id == "export"


class TestExportScreenDismiss:
    """Test ExportScreen dismiss returns correct values."""

    async def test_export_button_returns_tuple(self, test_app, capture):
        """Test clicking Export returns (ExportFormat.PARQUET, ExportScope.FULL)."""
        result, callback = capture
        async with test_app.run_test() as pilot:
            screen = ExportScreen()
            test_app.push_screen(screen, callback=callback)
            await pilot.pause()

            await pilot.click("#export")
            await pilot.pause()

            assert result["result"] == (ExportFormat.PARQUET, ExportScope.FULL)

    async def test_cancel_button_returns_none(self, test_app, capture):
        """Test clicking Cancel returns None."""
        result, callback = capture
        async with test_app.run_test() as pilot:
            screen = ExportScreen()
            test_app.push_screen(screen, callback=callback)
            await pilot.pause()

            await pilot.click("#cancel")
            await pilot.pause()

            assert result["result"] is None

    async def test_escape_key_returns_none(self, test_app, capture):
        """Test pressing Escape returns None."""
        result, callback = capture
        async with test_app.run_test() as pilot:
            screen = ExportScreen()
            test_app.push_screen(screen, callback=callback)
            await pilot.pause()

            await pilot.press("escape")
            await pilot.pause()

            assert result["result"] is None


class TestExportScreenInit:
    """Test ExportScreen initialization."""

    def test_init_no_params(self):
        """Test constructor requires no arguments."""
        screen = ExportScreen()
        assert screen is not None
