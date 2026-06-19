"""Tests for ExportScreen modal."""

from typing import cast

import pytest
from textual.app import App
from textual.widgets import Button, Label, RadioSet

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

    async def test_format_selector_shows_parquet(self, test_app):
        """Test format RadioSet contains Parquet option."""
        async with test_app.run_test() as pilot:
            screen = ExportScreen()
            test_app.push_screen(screen)
            await pilot.pause()

            format_set = screen.query_one("#format-select", RadioSet)
            button_labels = [btn.label for btn in format_set.query("RadioButton")]
            assert ExportFormat.PARQUET.display_name in button_labels

    async def test_format_selector_shows_all_formats(self, test_app):
        """Test format selector shows Parquet, CSV, and SQLite."""
        async with test_app.run_test() as pilot:
            screen = ExportScreen()
            test_app.push_screen(screen)
            await pilot.pause()

            format_set = screen.query_one("#format-select", RadioSet)
            button_labels = [btn.label for btn in format_set.query("RadioButton")]
            assert "Parquet" in button_labels
            assert "CSV" in button_labels
            assert "SQLite" in button_labels

    async def test_parquet_is_default_selection(self, test_app):
        """Test Parquet is the default selected format."""
        async with test_app.run_test() as pilot:
            screen = ExportScreen()
            test_app.push_screen(screen)
            await pilot.pause()

            format_set = screen.query_one("#format-select", RadioSet)
            # First button (index 0) should be Parquet and selected
            assert format_set.pressed_index == 0

    async def test_scope_selector_shows_full(self, test_app):
        """Test scope RadioSet shows Full dataset."""
        async with test_app.run_test() as pilot:
            screen = ExportScreen()
            test_app.push_screen(screen)
            await pilot.pause()

            scope_set = screen.query_one("#scope-select", RadioSet)
            button_labels = [btn.label for btn in scope_set.query("RadioButton")]
            assert ExportScope.FULL.display_name in button_labels

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

    async def test_format_selector_focused_on_mount(self, test_app):
        """Test format selector receives initial focus."""
        async with test_app.run_test() as pilot:
            screen = ExportScreen()
            test_app.push_screen(screen)
            await pilot.pause()

            focused = screen.focused
            assert focused is not None
            assert focused.id == "format-select"


class TestExportScreenDismiss:
    """Test ExportScreen dismiss returns correct values."""

    async def test_export_with_parquet_default_returns_parquet(self, test_app, capture):
        """Test Export with default Parquet selection returns (PARQUET, FULL)."""
        result, callback = capture
        async with test_app.run_test() as pilot:
            screen = ExportScreen()
            test_app.push_screen(screen, callback=callback)
            await pilot.pause()

            await pilot.click("#export")
            await pilot.pause()

            assert result["result"] == (ExportFormat.PARQUET, ExportScope.FULL)

    async def test_export_with_csv_selected_returns_csv(self, test_app, capture):
        """Test selecting CSV then Export returns (CSV, FULL)."""
        result, callback = capture
        async with test_app.run_test() as pilot:
            screen = ExportScreen()
            test_app.push_screen(screen, callback=callback)
            await pilot.pause()

            # Select CSV (second RadioButton in format-select)
            csv_button = screen.query("RadioButton")[1]
            await pilot.click(csv_button)
            await pilot.pause()

            await pilot.click("#export")
            await pilot.pause()

            assert result["result"] == (ExportFormat.CSV, ExportScope.FULL)

    async def test_export_with_sqlite_selected_returns_sqlite(self, test_app, capture):
        """Test selecting SQLite then Export returns (SQLITE, FULL)."""
        result, callback = capture
        async with test_app.run_test() as pilot:
            screen = ExportScreen()
            test_app.push_screen(screen, callback=callback)
            await pilot.pause()

            sqlite_button = screen.query("RadioButton")[2]
            await pilot.click(sqlite_button)
            await pilot.pause()

            await pilot.click("#export")
            await pilot.pause()

            assert result["result"] == (ExportFormat.SQLITE, ExportScope.FULL)

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
