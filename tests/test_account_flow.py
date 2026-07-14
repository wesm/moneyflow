"""Tests for account flow coordination - SimpleFIN setup flow."""

from unittest.mock import AsyncMock, MagicMock

import pytest

from moneyflow.tui.account_flow import AccountFlowCoordinator
from moneyflow.tui.screens.credential_screens import CredentialSetupScreen


class TestSimpleFINSetup:
    """Tests for SimpleFIN new account setup flow."""

    @pytest.fixture
    def mock_app(self, tmp_path):
        app = MagicMock()
        app.config_dir = str(tmp_path)
        app.push_screen = AsyncMock(
            return_value={
                "email": "",
                "password": "https://user:pass@simplefin.example.com",
                "mfa_secret": "",
                "backend_type": "simplefin",
            }
        )
        return app

    @pytest.fixture
    def coordinator(self, mock_app):
        return AccountFlowCoordinator(mock_app)

    async def test_handle_new_simplefin_setup_passes_profile_dir_to_credential_screen(
        self, coordinator, mock_app
    ):
        """Test that handle_new_simplefin_setup creates the account before
        showing the credential screen, so profile_dir is available and
        encryption settings are preserved."""
        result = await coordinator.handle_new_simplefin_setup()

        assert result is not None
        account_id, profile_dir, creds = result
        assert profile_dir is not None
        assert profile_dir.exists()

        screen = mock_app.push_screen.call_args[0][0]  # type: ignore
        assert isinstance(screen, CredentialSetupScreen)
        assert screen.profile_dir is not None
        assert screen.profile_dir == profile_dir

    async def test_cancelled_setup_restores_active_account_even_if_cleanup_fails(
        self, mock_app, tmp_path, monkeypatch
    ):
        from moneyflow.data.account_manager import AccountManager

        account_manager = AccountManager(config_dir=tmp_path)
        account_manager.create_account("First", "demo")
        active = account_manager.create_account("Active", "demo")
        mock_app.push_screen.return_value = None
        monkeypatch.setattr(
            "moneyflow.tui.account_flow.AccountManager", lambda config_dir=None: account_manager
        )
        monkeypatch.setattr(
            "moneyflow.data.account_manager.shutil.rmtree",
            lambda path: (_ for _ in ()).throw(OSError("simulated cleanup failure")),
        )

        result = await AccountFlowCoordinator(mock_app).handle_new_simplefin_setup()

        assert result is None
        assert account_manager.get_last_active_account() == active
