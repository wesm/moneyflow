"""
End-to-end TUI workflow tests.

These tests validate critical user workflows by running the actual Textual app
with mock backends. They catch issues like:
- CSS parse errors in screens
- Screen composition/mounting failures
- Workflow navigation bugs
- Data loading failures

All tests use tmp_path to avoid polluting production ~/.moneyflow directory.
"""

from unittest.mock import MagicMock

import pytest
from textual.containers import Container
from textual.widgets import Checkbox, DataTable

from moneyflow.app import MoneyflowApp
from moneyflow.screens.account_selector_screen import AccountSelectorScreen
from moneyflow.screens.batch_scope_screen import BatchScopeScreen
from moneyflow.screens.credential_screens import (
    BackendSelectionScreen,
    CachePromptScreen,
    CredentialSetupScreen,
    CredentialUnlockScreen,
    FilterScreen,
    QuitConfirmationScreen,
)
from moneyflow.screens.duplicates_screen import DuplicatesScreen
from moneyflow.screens.edit_screens import (
    EditMerchantScreen,
    SelectCategoryScreen,
)
from moneyflow.screens.review_screen import ReviewChangesScreen
from moneyflow.screens.search_screen import SearchScreen
from moneyflow.screens.transaction_detail_screen import TransactionDetailScreen

# ============================================================================
# CSS Validation Tests
# ============================================================================


class TestScreenCSSValidation:
    """
    Validate that all screen CSS parses without errors.

    These tests catch CSS syntax errors that would crash the app at runtime
    when a screen is pushed. This is critical because Textual CSS has different
    syntax rules than standard CSS.
    """

    def test_backend_selection_screen_css_parses(self):
        """BackendSelectionScreen CSS should parse without errors."""
        # Creating the screen will trigger CSS parsing
        screen = BackendSelectionScreen()
        assert screen is not None
        # If we get here, CSS parsed successfully

    def test_credential_setup_screen_css_parses(self, tmp_path):
        """CredentialSetupScreen CSS should parse without errors."""
        screen = CredentialSetupScreen(backend_type="monarch", profile_dir=tmp_path)
        assert screen is not None

    def test_credential_setup_screen_ynab_css_parses(self, tmp_path):
        """CredentialSetupScreen for YNAB should parse without errors."""
        screen = CredentialSetupScreen(backend_type="ynab", profile_dir=tmp_path)
        assert screen is not None

    def test_credential_unlock_screen_css_parses(self, tmp_path):
        """CredentialUnlockScreen CSS should parse without errors."""
        screen = CredentialUnlockScreen(profile_dir=tmp_path)
        assert screen is not None

    def test_quit_confirmation_screen_css_parses(self):
        """QuitConfirmationScreen CSS should parse without errors."""
        screen = QuitConfirmationScreen()
        assert screen is not None

    def test_filter_screen_css_parses(self):
        """FilterScreen CSS should parse without errors."""
        screen = FilterScreen()
        assert screen is not None

    def test_cache_prompt_screen_css_parses(self):
        """CachePromptScreen CSS should parse without errors."""
        screen = CachePromptScreen(
            age="2 hours",
            transaction_count=100,
            filter_desc="Last 30 days",
        )
        assert screen is not None

    def test_account_selector_screen_css_parses(self, tmp_path):
        """AccountSelectorScreen CSS should parse without errors."""
        screen = AccountSelectorScreen(config_dir=str(tmp_path))
        assert screen is not None

    def test_batch_scope_screen_css_parses(self):
        """BatchScopeScreen CSS should parse without errors."""
        screen = BatchScopeScreen(
            merchant_name="Test Merchant",
            selected_count=5,
            total_count=10,
        )
        assert screen is not None

    def test_duplicates_screen_css_parses(self):
        """DuplicatesScreen CSS should parse without errors."""
        import polars as pl

        empty_df = pl.DataFrame(
            {
                "id": [],
                "date": [],
                "merchant": [],
                "amount": [],
                "category": [],
                "account": [],
            }
        )
        mock_app = MagicMock()
        screen = DuplicatesScreen(
            duplicates_df=empty_df,
            groups=[],
            full_df=empty_df,
            main_app=mock_app,
        )
        assert screen is not None

    def test_select_category_screen_css_parses(self):
        """SelectCategoryScreen CSS should parse without errors."""
        screen = SelectCategoryScreen(
            categories={
                "cat1": {"name": "Category 1"},
                "cat2": {"name": "Category 2"},
            },
            current_category_id="cat1",
        )
        assert screen is not None

    def test_edit_merchant_screen_css_parses(self):
        """EditMerchantScreen CSS should parse without errors."""
        screen = EditMerchantScreen(
            current_merchant="Test Merchant",
            all_merchants=["Merchant 1", "Merchant 2"],
        )
        assert screen is not None

    def test_review_changes_screen_css_parses(self):
        """ReviewChangesScreen CSS should parse without errors."""
        screen = ReviewChangesScreen(pending_edits=[])
        assert screen is not None

    def test_search_screen_css_parses(self):
        """SearchScreen CSS should parse without errors."""
        screen = SearchScreen()
        assert screen is not None

    def test_transaction_detail_screen_css_parses(self):
        """TransactionDetailScreen CSS should parse without errors."""
        screen = TransactionDetailScreen(
            transaction_data={
                "id": "test",
                "date": "2024-01-01",
                "merchant": "Test",
                "amount": -10.00,
                "category": "Test",
                "account": "Test",
            }
        )
        assert screen is not None


# ============================================================================
# App CSS Validation
# ============================================================================


class TestAppCSSValidation:
    """Validate that the main app CSS parses correctly."""

    def test_main_app_css_parses(self, tmp_path):
        """MoneyflowApp CSS should parse without errors."""
        # Create app in demo mode to avoid needing real credentials
        app = MoneyflowApp(demo_mode=True, config_dir=str(tmp_path))
        assert app is not None


# ============================================================================
# Demo Mode Workflow Tests
# ============================================================================


class TestDemoModeWorkflow:
    """Test the demo mode workflow end-to-end."""

    @pytest.mark.integration
    async def test_demo_mode_loads_data(self, tmp_path):
        """Demo mode should load and display sample data."""
        app = MoneyflowApp(demo_mode=True, config_dir=str(tmp_path))

        async with app.run_test() as pilot:
            # Wait for app to initialize
            for _ in range(40):
                if app.controller is not None and app.state.current_data is not None:
                    break
                await pilot.pause()

            # Verify data table has content
            table = app.query_one("#data-table", DataTable)
            assert table.row_count > 0, "Demo mode should populate data table"

    @pytest.mark.integration
    async def test_demo_mode_keyboard_navigation(self, tmp_path):
        """Demo mode should respond to keyboard navigation."""
        app = MoneyflowApp(demo_mode=True, config_dir=str(tmp_path))

        async with app.run_test() as pilot:
            # Wait for app to initialize
            for _ in range(40):
                if app.controller is not None and app.state.current_data is not None:
                    break
                await pilot.pause()

            # Press 'j' to move down
            await pilot.press("j")
            await pilot.pause()

            # Press 'k' to move up
            await pilot.press("k")
            await pilot.pause()

            # App should still be running
            assert app.is_running


# ============================================================================
# Account Selection Workflow Tests
# ============================================================================


class TestAccountSelectionWorkflow:
    """Test the account selection workflow."""

    @pytest.mark.integration
    async def test_account_selector_screen_mounts(self, tmp_path):
        """Account selector should mount without errors."""
        app = MoneyflowApp(demo_mode=True, config_dir=str(tmp_path))

        async with app.run_test() as pilot:
            # Wait for app to fully initialize
            assert await _wait_for_app_ready(app, pilot), "App failed to initialize"

            # Push the account selector screen
            screen = AccountSelectorScreen(config_dir=str(tmp_path))
            app.push_screen(screen)
            await pilot.pause()

            # Screen should be displayed
            assert app.screen is screen

    @pytest.mark.integration
    async def test_account_selector_keyboard_navigation(self, tmp_path):
        """Account selector should support keyboard navigation."""
        app = MoneyflowApp(demo_mode=True, config_dir=str(tmp_path))

        async with app.run_test() as pilot:
            # Wait for app to fully initialize
            assert await _wait_for_app_ready(app, pilot), "App failed to initialize"

            screen = AccountSelectorScreen(config_dir=str(tmp_path))
            app.push_screen(screen)
            await pilot.pause()

            # Navigate with j/k
            await pilot.press("j")
            await pilot.pause()
            await pilot.press("k")
            await pilot.pause()

            # Screen should still be displayed
            assert app.screen is screen


# ============================================================================
# Credential Setup Workflow Tests
# ============================================================================


async def _wait_for_app_ready(app, pilot, attempts=40) -> bool:
    """Wait for app to initialize (theme CSS loaded).

    Returns True if app became ready, False on timeout.
    Callers should assert the return value to catch initialization failures.
    """
    for _ in range(attempts):
        if app.controller is not None and app.state.current_data is not None:
            return True
        await pilot.pause()
    return False


class TestCredentialSetupWorkflow:
    """Test the credential setup workflow."""

    @pytest.mark.integration
    async def test_credential_setup_screen_mounts(self, tmp_path):
        """Credential setup screen should mount with expected widgets."""
        app = MoneyflowApp(demo_mode=True, config_dir=str(tmp_path))

        async with app.run_test() as pilot:
            # Wait for app to fully initialize (loads theme CSS)
            assert await _wait_for_app_ready(app, pilot), "App failed to initialize"

            screen = CredentialSetupScreen(
                backend_type="monarch",
                profile_dir=tmp_path / "profiles" / "test",
            )
            app.push_screen(screen)
            await pilot.pause()

            # Screen should be displayed (pushed onto stack)
            assert isinstance(app.screen, CredentialSetupScreen)

            # Verify key widgets are present
            email_input = screen.query("#email-input")
            assert len(list(email_input)) == 1, "Email input should be present"

            password_input = screen.query("#password-input")
            assert len(list(password_input)) == 1, "Password input should be present"

            encryption_checkbox = screen.query("#encryption-checkbox")
            assert len(list(encryption_checkbox)) == 1, "Encryption checkbox should be present"

    @pytest.mark.integration
    async def test_credential_setup_encryption_toggle(self, tmp_path):
        """Encryption checkbox should toggle password fields visibility."""
        app = MoneyflowApp(demo_mode=True, config_dir=str(tmp_path))

        async with app.run_test() as pilot:
            # Wait for app to fully initialize (loads theme CSS)
            assert await _wait_for_app_ready(app, pilot), "App failed to initialize"

            screen = CredentialSetupScreen(
                backend_type="monarch",
                profile_dir=tmp_path / "profiles" / "test",
            )
            app.push_screen(screen)
            await pilot.pause()
            await pilot.pause()  # Extra pause for screen composition

            # Screen should be displayed
            assert isinstance(app.screen, CredentialSetupScreen)

            # Get encryption checkbox and fields
            checkbox = screen.query_one("#encryption-checkbox", Checkbox)
            encryption_fields = screen.query_one("#encryption-fields", Container)

            # By default, encryption is unchecked (for auto-login convenience)
            assert checkbox.value is False, "Encryption should be disabled by default"
            assert encryption_fields.display is False, "Encryption fields should be hidden"

            # Toggle checkbox on to enable encryption
            checkbox.toggle()
            await pilot.pause()

            # Fields should now be visible
            assert checkbox.value is True, "Encryption should be enabled after toggle"
            assert encryption_fields.display is True, "Encryption fields should be visible"

            # Toggle checkbox back off
            checkbox.toggle()
            await pilot.pause()

            # Fields should be hidden again
            assert checkbox.value is False, "Encryption should be disabled"
            assert encryption_fields.display is False, "Encryption fields should be hidden again"

    @pytest.mark.integration
    async def test_backend_selection_screen_mounts(self, tmp_path):
        """Backend selection screen should mount without errors."""
        app = MoneyflowApp(demo_mode=True, config_dir=str(tmp_path))

        async with app.run_test() as pilot:
            # Wait for app to fully initialize (loads theme CSS)
            assert await _wait_for_app_ready(app, pilot), "App failed to initialize"

            screen = BackendSelectionScreen()
            app.push_screen(screen)
            await pilot.pause()

            # Screen should be displayed
            assert app.screen is screen


# ============================================================================
# Search Workflow Tests
# ============================================================================


class TestSearchWorkflow:
    """Test the search workflow."""

    @pytest.mark.integration
    async def test_search_screen_opens(self, tmp_path):
        """Search screen should open with / key."""
        app = MoneyflowApp(demo_mode=True, config_dir=str(tmp_path))

        async with app.run_test() as pilot:
            # Wait for app to initialize
            for _ in range(40):
                if app.controller is not None and app.state.current_data is not None:
                    break
                await pilot.pause()

            # Press / to open search
            await pilot.press("/")
            await pilot.pause()

            # Search screen should be open
            # (it's a modal, so it overlays the main screen)
            search_input = app.query("SearchScreen Input")
            assert len(list(search_input)) > 0 or isinstance(app.screen, SearchScreen)


# ============================================================================
# Edit Workflow Tests
# ============================================================================


class TestEditWorkflow:
    """Test the edit workflows."""

    @pytest.mark.integration
    async def test_select_category_screen_mounts(self, tmp_path):
        """Select category screen should mount without errors."""
        app = MoneyflowApp(demo_mode=True, config_dir=str(tmp_path))

        async with app.run_test() as pilot:
            # Wait for app to fully initialize
            assert await _wait_for_app_ready(app, pilot), "App failed to initialize"

            screen = SelectCategoryScreen(
                categories={
                    "cat1": {"name": "Groceries"},
                    "cat2": {"name": "Shopping"},
                },
                current_category_id="cat1",
            )
            app.push_screen(screen)
            await pilot.pause()

            assert app.screen is screen

    @pytest.mark.integration
    async def test_edit_merchant_screen_mounts(self, tmp_path):
        """Edit merchant screen should mount without errors."""
        app = MoneyflowApp(demo_mode=True, config_dir=str(tmp_path))

        async with app.run_test() as pilot:
            # Wait for app to fully initialize
            assert await _wait_for_app_ready(app, pilot), "App failed to initialize"

            screen = EditMerchantScreen(
                current_merchant="Test Store",
                all_merchants=["Store A", "Store B", "Test Store"],
            )
            app.push_screen(screen)
            await pilot.pause()

            assert app.screen is screen


# ============================================================================
# Mock Backend Integration Tests
# ============================================================================


class TestDemoModeDataLoading:
    """Test app data loading in demo mode."""

    @pytest.mark.integration
    async def test_demo_mode_populates_state_data(self, tmp_path):
        """Demo mode should populate app state with sample data."""
        app = MoneyflowApp(demo_mode=True, config_dir=str(tmp_path))

        async with app.run_test() as pilot:
            # Wait for demo mode to initialize
            ready = False
            for _ in range(40):
                if app.controller is not None and app.state.current_data is not None:
                    ready = True
                    break
                await pilot.pause()

            assert ready, "App failed to initialize in demo mode"
            # App should have data
            assert app.state.current_data is not None
            assert len(app.state.current_data) > 0


# ============================================================================
# Error Handling Tests
# ============================================================================


class TestErrorHandling:
    """Test error handling in TUI workflows."""

    @pytest.mark.integration
    async def test_app_handles_missing_config_gracefully(self, tmp_path):
        """App should handle missing config directory gracefully."""
        # Use a fresh tmp_path with no existing config
        app = MoneyflowApp(demo_mode=True, config_dir=str(tmp_path / "nonexistent"))

        async with app.run_test() as pilot:
            # App should start without crashing
            await pilot.pause()
            assert app.is_running
