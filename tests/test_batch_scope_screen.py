"""Tests for BatchScopeScreen."""

from typing import cast

import pytest
from textual.app import App
from textual.widgets import Button, Label, Static

from moneyflow.tui.screens.batch_scope_screen import BatchScopeScreen


class MinimalApp(App):
    """Minimal app for testing screens."""

    pass


@pytest.fixture
def test_app():
    """Create a minimal test app."""
    return MinimalApp()


@pytest.fixture
def capture():
    """Fixture to capture screen dismiss callbacks."""
    container = {"result": None}

    def callback(value):
        container["result"] = value

    return container, callback


class TestBatchScopeScreenDisplay:
    """Test BatchScopeScreen displays correctly."""

    async def test_screen_displays_merchant_name(self, test_app):
        """Test screen shows the merchant name being renamed."""
        async with test_app.run_test() as pilot:
            screen = BatchScopeScreen(
                merchant_name="Amazon.com/abc",
                selected_count=3,
                total_count=10,
            )
            test_app.push_screen(screen)
            await pilot.pause()

            # Find the specific label rendering the text
            title_label = cast(Label, screen.query_one("#batch-scope-title"))
            assert "Amazon.com/abc" in str(title_label.render())

    async def test_screen_displays_counts(self, test_app):
        """Test screen shows selected and total counts."""
        async with test_app.run_test() as pilot:
            screen = BatchScopeScreen(
                merchant_name="Test Merchant",
                selected_count=5,
                total_count=15,
            )
            test_app.push_screen(screen)
            await pilot.pause()

            message_static = cast(Static, screen.query_one("#batch-scope-message"))
            rendered_text = str(message_static.render())
            assert "You selected 5 transaction(s), but 15 exist with this payee." in rendered_text

    async def test_screen_has_three_buttons(self, test_app):
        """Test screen has all required buttons."""
        async with test_app.run_test() as pilot:
            screen = BatchScopeScreen(
                merchant_name="Test",
                selected_count=2,
                total_count=8,
            )
            test_app.push_screen(screen)
            await pilot.pause()

            all_button = cast(Button, screen.query_one("#all"))
            selected_button = cast(Button, screen.query_one("#selected"))
            cancel_button = cast(Button, screen.query_one("#cancel"))

            assert all_button is not None
            assert selected_button is not None
            assert cancel_button is not None


class TestBatchScopeScreenDismiss:
    """Test BatchScopeScreen dismiss returns correct values."""

    async def test_all_button_returns_all(self, test_app, capture):
        """Test clicking 'Rename all' returns 'all'."""
        result, callback = capture
        async with test_app.run_test() as pilot:
            screen = BatchScopeScreen(
                merchant_name="Test",
                selected_count=2,
                total_count=10,
            )
            test_app.push_screen(screen, callback=callback)
            await pilot.pause()

            await pilot.click("#all")
            await pilot.pause()

            assert result["result"] == "all"

    async def test_selected_button_returns_selected(self, test_app, capture):
        """Test clicking 'Rename selected only' returns 'selected'."""
        result, callback = capture
        async with test_app.run_test() as pilot:
            screen = BatchScopeScreen(
                merchant_name="Test",
                selected_count=2,
                total_count=10,
            )
            test_app.push_screen(screen, callback=callback)
            await pilot.pause()

            await pilot.click("#selected")
            await pilot.pause()

            assert result["result"] == "selected"

    async def test_cancel_button_returns_cancel(self, test_app, capture):
        """Test clicking 'Cancel' returns 'cancel'."""
        result, callback = capture
        async with test_app.run_test() as pilot:
            screen = BatchScopeScreen(
                merchant_name="Test",
                selected_count=2,
                total_count=10,
            )
            test_app.push_screen(screen, callback=callback)
            await pilot.pause()

            await pilot.click("#cancel")
            await pilot.pause()

            assert result["result"] == "cancel"

    async def test_escape_key_returns_cancel(self, test_app, capture):
        """Test pressing Escape returns 'cancel'."""
        result, callback = capture
        async with test_app.run_test() as pilot:
            screen = BatchScopeScreen(
                merchant_name="Test",
                selected_count=2,
                total_count=10,
            )
            test_app.push_screen(screen, callback=callback)
            await pilot.pause()

            await pilot.press("escape")
            await pilot.pause()

            assert result["result"] == "cancel"

    async def test_key_1_returns_all(self, test_app, capture):
        """Test pressing '1' returns 'all'."""
        result, callback = capture
        async with test_app.run_test() as pilot:
            screen = BatchScopeScreen(
                merchant_name="Test",
                selected_count=2,
                total_count=10,
            )
            test_app.push_screen(screen, callback=callback)
            await pilot.pause()

            await pilot.press("1")
            await pilot.pause()

            assert result["result"] == "all"

    async def test_key_2_returns_selected(self, test_app, capture):
        """Test pressing '2' returns 'selected'."""
        result, callback = capture
        async with test_app.run_test() as pilot:
            screen = BatchScopeScreen(
                merchant_name="Test",
                selected_count=2,
                total_count=10,
            )
            test_app.push_screen(screen, callback=callback)
            await pilot.pause()

            await pilot.press("2")
            await pilot.pause()

            assert result["result"] == "selected"


class TestBatchScopeScreenButtonLabels:
    """Test button labels display counts correctly."""

    async def test_all_button_shows_total_count(self, test_app):
        """Test 'Rename all' button shows total count."""
        async with test_app.run_test() as pilot:
            screen = BatchScopeScreen(
                merchant_name="Test",
                selected_count=3,
                total_count=25,
            )
            test_app.push_screen(screen)
            await pilot.pause()

            all_button = cast(Button, screen.query_one("#all"))
            label = str(all_button.label)
            assert "25" in label

    async def test_selected_button_shows_selected_count(self, test_app):
        """Test 'Rename selected only' button shows selected count."""
        async with test_app.run_test() as pilot:
            screen = BatchScopeScreen(
                merchant_name="Test",
                selected_count=7,
                total_count=50,
            )
            test_app.push_screen(screen)
            await pilot.pause()

            selected_button = cast(Button, screen.query_one("#selected"))
            label = str(selected_button.label)
            assert "7" in label


class TestBatchScopeScreenInit:
    """Test BatchScopeScreen initialization."""

    def test_init_stores_merchant_name(self):
        """Test constructor stores merchant name."""
        screen = BatchScopeScreen("Amazon", 3, 10)
        assert screen.merchant_name == "Amazon"

    def test_init_stores_selected_count(self):
        """Test constructor stores selected count."""
        screen = BatchScopeScreen("Amazon", 3, 10)
        assert screen.selected_count == 3

    def test_init_stores_total_count(self):
        """Test constructor stores total count."""
        screen = BatchScopeScreen("Amazon", 3, 10)
        assert screen.total_count == 10
