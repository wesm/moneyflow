"""Tests for BatchScopeScreen."""

from typing import cast

import pytest
from textual.app import App, ComposeResult
from textual.widgets import Button

from moneyflow.screens.batch_scope_screen import BatchScopeScreen


class MinimalApp(App):
    """Minimal app for testing screens."""

    pass


@pytest.fixture
def test_app():
    """Create a minimal test app."""
    return MinimalApp()


class TestBatchScopeScreenDisplay:
    """Test BatchScopeScreen displays correctly."""

    async def test_screen_displays_merchant_name(self):
        """Test screen shows the merchant name being renamed."""

        class AppWithScreen(App):
            def compose(self) -> ComposeResult:
                return []

            async def on_mount(self):
                screen = BatchScopeScreen(
                    merchant_name="Amazon.com/abc",
                    selected_count=3,
                    total_count=10,
                )
                self.install_screen(screen, "test_screen")
                self.push_screen("test_screen")

        app = AppWithScreen()
        async with app.run_test() as pilot:
            await pilot.pause()
            await pilot.pause()

            # Get the screen from app's screen stack
            screen = cast(BatchScopeScreen, app.screen)
            # Check that merchant_name is displayed in the screen
            assert screen.merchant_name == "Amazon.com/abc"

    async def test_screen_displays_counts(self):
        """Test screen shows selected and total counts."""

        class AppWithScreen(App):
            async def on_mount(self):
                screen = BatchScopeScreen(
                    merchant_name="Test Merchant",
                    selected_count=5,
                    total_count=15,
                )
                self.install_screen(screen, "test_screen")
                self.push_screen("test_screen")

        app = AppWithScreen()
        async with app.run_test() as pilot:
            await pilot.pause()
            await pilot.pause()

            screen = cast(BatchScopeScreen, app.screen)
            # Check that counts are stored correctly
            assert screen.selected_count == 5
            assert screen.total_count == 15

    async def test_screen_has_three_buttons(self):
        """Test screen has all required buttons."""

        class AppWithScreen(App):
            async def on_mount(self):
                screen = BatchScopeScreen(
                    merchant_name="Test",
                    selected_count=2,
                    total_count=8,
                )
                self.install_screen(screen, "test_screen")
                self.push_screen("test_screen")

        app = AppWithScreen()
        async with app.run_test() as pilot:
            await pilot.pause()
            await pilot.pause()

            screen = cast(BatchScopeScreen, app.screen)
            all_button = cast(Button, screen.query_one("#all"))
            selected_button = cast(Button, screen.query_one("#selected"))
            cancel_button = cast(Button, screen.query_one("#cancel"))

            assert all_button is not None
            assert selected_button is not None
            assert cancel_button is not None


class TestBatchScopeScreenDismiss:
    """Test BatchScopeScreen dismiss returns correct values."""

    async def test_all_button_returns_all(self, test_app):
        """Test clicking 'Rename all' returns 'all'."""
        result = None

        def capture_result(value):
            nonlocal result
            result = value

        async with test_app.run_test() as pilot:
            screen = BatchScopeScreen(
                merchant_name="Test",
                selected_count=2,
                total_count=10,
            )
            test_app.push_screen(screen, callback=capture_result)
            await pilot.pause()

            await pilot.click("#all")
            await pilot.pause()

            assert result == "all"

    async def test_selected_button_returns_selected(self, test_app):
        """Test clicking 'Rename selected only' returns 'selected'."""
        result = None

        def capture_result(value):
            nonlocal result
            result = value

        async with test_app.run_test() as pilot:
            screen = BatchScopeScreen(
                merchant_name="Test",
                selected_count=2,
                total_count=10,
            )
            test_app.push_screen(screen, callback=capture_result)
            await pilot.pause()

            await pilot.click("#selected")
            await pilot.pause()

            assert result == "selected"

    async def test_cancel_button_returns_cancel(self, test_app):
        """Test clicking 'Cancel' returns 'cancel'."""
        result = None

        def capture_result(value):
            nonlocal result
            result = value

        async with test_app.run_test() as pilot:
            screen = BatchScopeScreen(
                merchant_name="Test",
                selected_count=2,
                total_count=10,
            )
            test_app.push_screen(screen, callback=capture_result)
            await pilot.pause()

            await pilot.click("#cancel")
            await pilot.pause()

            assert result == "cancel"

    async def test_escape_key_returns_cancel(self, test_app):
        """Test pressing Escape returns 'cancel'."""
        result = None

        def capture_result(value):
            nonlocal result
            result = value

        async with test_app.run_test() as pilot:
            screen = BatchScopeScreen(
                merchant_name="Test",
                selected_count=2,
                total_count=10,
            )
            test_app.push_screen(screen, callback=capture_result)
            await pilot.pause()

            await pilot.press("escape")
            await pilot.pause()

            assert result == "cancel"

    async def test_key_1_returns_all(self, test_app):
        """Test pressing '1' returns 'all'."""
        result = None

        def capture_result(value):
            nonlocal result
            result = value

        async with test_app.run_test() as pilot:
            screen = BatchScopeScreen(
                merchant_name="Test",
                selected_count=2,
                total_count=10,
            )
            test_app.push_screen(screen, callback=capture_result)
            await pilot.pause()

            await pilot.press("1")
            await pilot.pause()

            assert result == "all"

    async def test_key_2_returns_selected(self, test_app):
        """Test pressing '2' returns 'selected'."""
        result = None

        def capture_result(value):
            nonlocal result
            result = value

        async with test_app.run_test() as pilot:
            screen = BatchScopeScreen(
                merchant_name="Test",
                selected_count=2,
                total_count=10,
            )
            test_app.push_screen(screen, callback=capture_result)
            await pilot.pause()

            await pilot.press("2")
            await pilot.pause()

            assert result == "selected"


class TestBatchScopeScreenButtonLabels:
    """Test button labels display counts correctly."""

    async def test_all_button_shows_total_count(self):
        """Test 'Rename all' button shows total count."""

        class AppWithScreen(App):
            async def on_mount(self):
                screen = BatchScopeScreen(
                    merchant_name="Test",
                    selected_count=3,
                    total_count=25,
                )
                self.install_screen(screen, "test_screen")
                self.push_screen("test_screen")

        app = AppWithScreen()
        async with app.run_test() as pilot:
            await pilot.pause()
            await pilot.pause()

            screen = cast(BatchScopeScreen, app.screen)
            all_button = cast(Button, screen.query_one("#all"))
            label = str(all_button.label)
            assert "25" in label

    async def test_selected_button_shows_selected_count(self):
        """Test 'Rename selected only' button shows selected count."""

        class AppWithScreen(App):
            async def on_mount(self):
                screen = BatchScopeScreen(
                    merchant_name="Test",
                    selected_count=7,
                    total_count=50,
                )
                self.install_screen(screen, "test_screen")
                self.push_screen("test_screen")

        app = AppWithScreen()
        async with app.run_test() as pilot:
            await pilot.pause()
            await pilot.pause()

            screen = cast(BatchScopeScreen, app.screen)
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
