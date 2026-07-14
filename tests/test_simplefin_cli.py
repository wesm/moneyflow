"""
Tests for SimpleFIN CLI subcommands.

Tests use CliRunner for Click command invocation and isolated
tmp_path config directories to avoid touching real profiles.
"""

import stat
from unittest.mock import AsyncMock, Mock, patch

import click
import pytest
from click.testing import CliRunner

from moneyflow.cli import _build_simplefin_backend, _resolve_simplefin_profile, simplefin
from moneyflow.data.account_manager import AccountManager
from moneyflow.data.credentials import CredentialManager


class TestBuildSimplefinBackend:
    def test_cli_factory_treats_profile_directory_as_managed(self, tmp_path):
        profile_dir = tmp_path / "simplefin-profile"
        profile_dir.mkdir(mode=0o755)
        profile_dir.chmod(0o755)

        backend = _build_simplefin_backend(profile_dir)
        backend._ensure_db_initialized()

        assert backend.profile_dir == profile_dir
        assert stat.S_IMODE(profile_dir.stat().st_mode) == 0o700


class TestResolveSimplefinProfile:
    """Tests for _resolve_simplefin_profile() helper."""

    def test_zero_profiles(self, tmp_path):
        """0 profiles -> error."""
        with pytest.raises(click.exceptions.Abort):
            _resolve_simplefin_profile(config_dir=str(tmp_path))

    def test_encrypted_legacy_credentials_use_decrypted_backend(self, tmp_path, monkeypatch):
        """SimpleFIN CLI migration must not classify opaque credentials as Monarch."""
        credentials = CredentialManager(config_dir=tmp_path)
        credentials.save_credentials(
            email="",
            password="https://example:secret@bridge.simplefin.org/simplefin",
            mfa_secret="",
            encryption_password="example-password",
            backend_type="simplefin",
        )
        monkeypatch.setattr("click.prompt", lambda *args, **kwargs: "example-password")

        account_id, profile_dir = _resolve_simplefin_profile(config_dir=str(tmp_path))

        account = AccountManager(config_dir=tmp_path).get_account(account_id)
        assert account is not None
        assert account.backend_type == "simplefin"
        assert (profile_dir / "credentials.enc").exists()

    def test_encrypted_legacy_monarch_credentials_are_not_reclassified(self, tmp_path, monkeypatch):
        """Entering SimpleFIN mode must preserve an encrypted Monarch profile's type."""
        credentials = CredentialManager(config_dir=tmp_path)
        credentials.save_credentials(
            email="example@example.com",
            password="example-password",
            mfa_secret="EXAMPLESECRET",
            encryption_password="legacy-password",
            backend_type="monarch",
        )
        monkeypatch.setattr("click.prompt", lambda *args, **kwargs: "legacy-password")

        with pytest.raises(click.exceptions.Abort):
            _resolve_simplefin_profile(config_dir=str(tmp_path))

        accounts = AccountManager(config_dir=tmp_path).list_accounts()
        assert len(accounts) == 1
        assert accounts[0].backend_type == "monarch"

    def test_single_profile(self, tmp_path):
        """1 profile -> silently resolves."""
        mgr = AccountManager(config_dir=tmp_path)
        mgr.create_account("My SimpleFIN", "simplefin")
        result = _resolve_simplefin_profile(config_dir=str(tmp_path))
        assert result is not None
        account_id, profile_dir = result
        assert account_id is not None
        assert profile_dir.exists()

    def test_multiple_with_default(self, tmp_path):
        """2+ profiles with default -> auto-resolves."""
        mgr = AccountManager(config_dir=tmp_path)
        acct1 = mgr.create_account("Personal", "simplefin", account_id="simplefin-personal")
        mgr.create_account("Business", "simplefin", account_id="simplefin-business")
        mgr.set_backend_default("simplefin", acct1.id)

        result = _resolve_simplefin_profile(config_dir=str(tmp_path))
        assert result is not None
        account_id, profile_dir = result
        assert account_id == "simplefin-personal"

    def test_multiple_no_default(self, tmp_path, monkeypatch):
        """2+ profiles with no default -> prompt user and set default."""
        mgr = AccountManager(config_dir=tmp_path)
        mgr.create_account("Personal", "simplefin", account_id="simplefin-personal")
        mgr.create_account("Business", "simplefin", account_id="simplefin-business")

        # Simulate user picking "1" at the prompt
        monkeypatch.setattr("click.prompt", lambda *a, **kw: "1")

        result = _resolve_simplefin_profile(config_dir=str(tmp_path))
        assert result is not None
        account_id, _ = result
        assert account_id == "simplefin-personal"
        assert mgr.get_backend_default("simplefin") == "simplefin-personal"

    def test_explicit_profile_valid(self, tmp_path):
        """Explicit valid profile_id -> resolves to that profile."""
        mgr = AccountManager(config_dir=tmp_path)
        mgr.create_account("Personal", "simplefin", account_id="simplefin-personal")
        mgr.create_account("Business", "simplefin", account_id="simplefin-business")

        account_id, profile_dir = _resolve_simplefin_profile(
            config_dir=str(tmp_path), profile_id="simplefin-business"
        )
        assert account_id == "simplefin-business"
        assert profile_dir.exists()

    def test_explicit_profile_invalid(self, tmp_path):
        """Nonexistent profile_id -> abort."""
        mgr = AccountManager(config_dir=tmp_path)
        mgr.create_account("Personal", "simplefin", account_id="simplefin-personal")

        with pytest.raises(click.exceptions.Abort):
            _resolve_simplefin_profile(config_dir=str(tmp_path), profile_id="nonexistent")

    def test_explicit_profile_wrong_type(self, tmp_path):
        """profile_id belonging to non-SimpleFIN account -> abort."""
        mgr = AccountManager(config_dir=tmp_path)
        mgr.create_account("My Amazon", "amazon", account_id="amazon-main")

        with pytest.raises(click.exceptions.Abort):
            _resolve_simplefin_profile(config_dir=str(tmp_path), profile_id="amazon-main")


class TestSimplefinCommand:
    """Tests for the top-level `moneyflow simplefin` command (not subcommands)."""

    def test_no_profiles_triggers_first_time_setup(self, tmp_path):
        """Without --profile and no profiles, launches with profile_dir=None (first-time setup)."""
        launch_mock = Mock()
        with patch("moneyflow.tui.app.launch_simplefin_mode", launch_mock):
            runner = CliRunner()
            result = runner.invoke(
                simplefin,
                ["--config-dir", str(tmp_path)],
            )
        assert result.exit_code == 0
        launch_mock.assert_called_once()
        _, _, kwargs = launch_mock.mock_calls[0]
        assert kwargs["profile_dir"] is None

    def test_single_profile_resolves(self, tmp_path):
        """Without --profile and 1 profile, launches with that profile."""
        mgr = AccountManager(config_dir=tmp_path)
        mgr.create_account("My SimpleFIN", "simplefin", account_id="simplefin-main")

        launch_mock = Mock()
        with patch("moneyflow.tui.app.launch_simplefin_mode", launch_mock):
            runner = CliRunner()
            result = runner.invoke(
                simplefin,
                ["--config-dir", str(tmp_path)],
            )
        assert result.exit_code == 0
        launch_mock.assert_called_once()
        _, _, kwargs = launch_mock.mock_calls[0]
        assert kwargs["profile_dir"] is not None
        assert (tmp_path / "profiles" / "simplefin-main").samefile(kwargs["profile_dir"])

    def test_multiple_with_default_resolves_correctly(self, tmp_path):
        """Without --profile and 2+ profiles with a default, uses the default."""
        mgr = AccountManager(config_dir=tmp_path)
        mgr.create_account("Personal", "simplefin", account_id="simplefin-personal")
        mgr.create_account("Business", "simplefin", account_id="simplefin-business")
        mgr.set_backend_default("simplefin", "simplefin-personal")

        launch_mock = Mock()
        with patch("moneyflow.tui.app.launch_simplefin_mode", launch_mock):
            runner = CliRunner()
            result = runner.invoke(
                simplefin,
                ["--config-dir", str(tmp_path)],
            )
        assert result.exit_code == 0
        launch_mock.assert_called_once()
        _, _, kwargs = launch_mock.mock_calls[0]
        assert (tmp_path / "profiles" / "simplefin-personal").samefile(kwargs["profile_dir"])

    def test_invalid_multiple_profile_selection_aborts_without_setup(self, tmp_path, monkeypatch):
        """A selection error must not be mistaken for the no-profile setup case."""
        mgr = AccountManager(config_dir=tmp_path)
        mgr.create_account("Personal", "simplefin", account_id="simplefin-personal")
        mgr.create_account("Business", "simplefin", account_id="simplefin-business")
        monkeypatch.setattr("click.prompt", lambda *args, **kwargs: "invalid-profile")

        launch_mock = Mock()
        with patch("moneyflow.tui.app.launch_simplefin_mode", launch_mock):
            result = CliRunner().invoke(simplefin, ["--config-dir", str(tmp_path)])

        assert result.exit_code != 0
        assert "Invalid selection" in result.output
        launch_mock.assert_not_called()


class TestSimplefinRefresh:
    """Tests for `moneyflow simplefin refresh`."""

    def _setup_account_with_mocks(self, tmp_path, monkeypatch):
        """Helper: create an account + mock backend + credential loader."""
        mgr = AccountManager(config_dir=tmp_path)
        mgr.create_account("Test", "simplefin", account_id="simplefin-test")

        mock_backend = AsyncMock()
        mock_backend.refresh = AsyncMock(return_value=5)
        mock_backend.hard_refresh = AsyncMock(return_value=10)

        monkeypatch.setattr(
            "moneyflow.cli._build_simplefin_backend",
            lambda *a, **kw: mock_backend,
        )
        monkeypatch.setattr(
            "moneyflow.cli._load_simplefin_credentials",
            lambda *a, **kw: "https://user:pass@bridge.simplefin.org/simplefin",
        )

        return mock_backend

    def test_no_profile(self, tmp_path):
        """0 profiles -> error message."""
        runner = CliRunner()
        result = runner.invoke(
            simplefin,
            ["refresh"],
            env={"HOME": str(tmp_path)},
        )
        assert result.exit_code != 0
        assert "No SimpleFIN account configured" in result.output

    def test_with_profile_additive_refresh(self, tmp_path, monkeypatch):
        """1 profile -> calls backend.refresh() additively."""
        mock_backend = self._setup_account_with_mocks(tmp_path, monkeypatch)

        runner = CliRunner()
        result = runner.invoke(
            simplefin,
            ["refresh", "--config-dir", str(tmp_path)],
        )
        assert result.exit_code == 0, result.output
        mock_backend.refresh.assert_called_once()

    def test_with_profile_force_refresh(self, tmp_path, monkeypatch):
        """--force -> calls backend.hard_refresh()."""
        mock_backend = self._setup_account_with_mocks(tmp_path, monkeypatch)

        runner = CliRunner()
        result = runner.invoke(
            simplefin,
            ["refresh", "--force", "--config-dir", str(tmp_path)],
        )
        assert result.exit_code == 0, result.output
        mock_backend.hard_refresh.assert_called_once()
        mock_backend.refresh.assert_not_called()

    def test_refresh_with_explicit_profile(self, tmp_path, monkeypatch):
        """--profile X -> uses that profile (not default)."""
        mgr = AccountManager(config_dir=tmp_path)
        mgr.create_account("Personal", "simplefin", account_id="simplefin-personal")
        mgr.create_account("Business", "simplefin", account_id="simplefin-business")
        mgr.set_backend_default("simplefin", "simplefin-personal")

        mock_backend = AsyncMock()
        mock_backend.refresh = AsyncMock(return_value=5)
        monkeypatch.setattr(
            "moneyflow.cli._build_simplefin_backend",
            lambda *a, **kw: mock_backend,
        )
        monkeypatch.setattr(
            "moneyflow.cli._load_simplefin_credentials",
            lambda *a, **kw: "https://user:pass@bridge.simplefin.org/simplefin",
        )

        runner = CliRunner()
        result = runner.invoke(
            simplefin,
            ["--profile", "simplefin-business", "refresh", "--config-dir", str(tmp_path)],
        )
        assert result.exit_code == 0, result.output
        mock_backend.refresh.assert_called_once()

    def test_refresh_with_profile_after_subcommand(self, tmp_path, monkeypatch):
        """--profile after subcommand name works too."""
        mgr = AccountManager(config_dir=tmp_path)
        mgr.create_account("Personal", "simplefin", account_id="simplefin-personal")
        mgr.create_account("Business", "simplefin", account_id="simplefin-business")
        mgr.set_backend_default("simplefin", "simplefin-personal")

        mock_backend = AsyncMock()
        mock_backend.refresh = AsyncMock(return_value=5)
        monkeypatch.setattr(
            "moneyflow.cli._build_simplefin_backend",
            lambda *a, **kw: mock_backend,
        )
        monkeypatch.setattr(
            "moneyflow.cli._load_simplefin_credentials",
            lambda *a, **kw: "https://user:pass@bridge.simplefin.org/simplefin",
        )

        runner = CliRunner()
        result = runner.invoke(
            simplefin,
            ["refresh", "--profile", "simplefin-business", "--config-dir", str(tmp_path)],
        )
        assert result.exit_code == 0, result.output
        mock_backend.refresh.assert_called_once()


class TestSimplefinStatus:
    """Tests for `moneyflow simplefin status`."""

    def _setup_account(self, tmp_path, monkeypatch, stats: dict):
        """Helper: create account + mock backend returning given stats."""
        mgr = AccountManager(config_dir=tmp_path)
        mgr.create_account("Test", "simplefin", account_id="simplefin-test")

        mock_backend = Mock()
        mock_backend.get_database_stats.return_value = stats

        monkeypatch.setattr(
            "moneyflow.cli._build_simplefin_backend",
            lambda *a, **kw: mock_backend,
        )

        return mock_backend

    def test_no_profile(self, tmp_path):
        """0 profiles -> error message."""
        runner = CliRunner()
        result = runner.invoke(
            simplefin,
            ["status"],
            env={"HOME": str(tmp_path)},
        )
        assert result.exit_code != 0
        assert "No SimpleFIN account configured" in result.output

    def test_shows_canonical_stats(self, tmp_path, monkeypatch):
        """Displays total_transactions, date range, total amount."""
        self._setup_account(
            tmp_path,
            monkeypatch,
            {
                "total_transactions": 42,
                "total_amount": -1234.56,
                "earliest_date": "2024-01-15",
                "latest_date": "2025-06-01",
                "last_refresh_timestamp": None,
                "last_refresh_count": None,
                "currency_code": "EUR",
            },
        )

        runner = CliRunner()
        result = runner.invoke(
            simplefin,
            ["status", "--config-dir", str(tmp_path)],
        )
        assert result.exit_code == 0, result.output
        assert "42" in result.output
        assert "2024-01-15" in result.output
        assert "2025-06-01" in result.output
        assert "-1,234.56" in result.output  # formatted amount
        assert "Total amount (EUR)" in result.output

    def test_status_with_explicit_profile(self, tmp_path, monkeypatch):
        """--profile X -> shows status for that profile."""
        mgr = AccountManager(config_dir=tmp_path)
        mgr.create_account("Personal", "simplefin", account_id="simplefin-personal")
        mgr.create_account("Business", "simplefin", account_id="simplefin-business")
        mgr.set_backend_default("simplefin", "simplefin-personal")

        mock_backend = Mock()
        mock_backend.get_database_stats.return_value = {
            "total_transactions": 99,
            "total_amount": -500.0,
            "earliest_date": "2024-01-01",
            "latest_date": "2025-06-01",
        }
        monkeypatch.setattr(
            "moneyflow.cli._build_simplefin_backend",
            lambda *a, **kw: mock_backend,
        )

        runner = CliRunner()
        result = runner.invoke(
            simplefin,
            ["--profile", "simplefin-business", "status", "--config-dir", str(tmp_path)],
        )
        assert result.exit_code == 0, result.output
        assert "99" in result.output

    def test_status_with_profile_after_subcommand(self, tmp_path, monkeypatch):
        """--profile after subcommand name works too."""
        mgr = AccountManager(config_dir=tmp_path)
        mgr.create_account("Personal", "simplefin", account_id="simplefin-personal")
        mgr.create_account("Business", "simplefin", account_id="simplefin-business")
        mgr.set_backend_default("simplefin", "simplefin-personal")

        mock_backend = Mock()
        mock_backend.get_database_stats.return_value = {
            "total_transactions": 77,
            "total_amount": -500.0,
            "earliest_date": "2024-01-01",
            "latest_date": "2025-06-01",
        }
        monkeypatch.setattr(
            "moneyflow.cli._build_simplefin_backend",
            lambda *a, **kw: mock_backend,
        )

        runner = CliRunner()
        result = runner.invoke(
            simplefin,
            ["status", "--profile", "simplefin-business", "--config-dir", str(tmp_path)],
        )
        assert result.exit_code == 0, result.output
        assert "77" in result.output

    def test_shows_refresh_info_when_available(self, tmp_path, monkeypatch):
        """Displays last_refresh_timestamp and count when present."""
        self._setup_account(
            tmp_path,
            monkeypatch,
            {
                "total_transactions": 100,
                "total_amount": -5000.0,
                "earliest_date": "2023-01-01",
                "latest_date": "2025-06-01",
                "last_refresh_timestamp": "2025-06-24T10:30:00+00:00",
                "last_refresh_count": "150",
            },
        )

        runner = CliRunner()
        result = runner.invoke(
            simplefin,
            ["status", "--config-dir", str(tmp_path)],
        )
        assert result.exit_code == 0, result.output
        assert "2025-06-24T10:30:00+00:00" in result.output
        assert "150" in result.output


class TestSimplefinDefault:
    """Tests for `moneyflow simplefin default`."""

    def test_no_profiles(self, tmp_path):
        """0 profiles -> error message."""
        runner = CliRunner()
        result = runner.invoke(
            simplefin,
            ["default"],
            env={"HOME": str(tmp_path)},
        )
        assert result.exit_code != 0
        assert "No SimpleFIN accounts configured" in result.output

    def test_show_current_default(self, tmp_path):
        """Default is set -> lists all profiles with default marked."""
        mgr = AccountManager(config_dir=tmp_path)
        mgr.create_account("Personal", "simplefin", account_id="simplefin-personal")
        acct = mgr.create_account("Business", "simplefin", account_id="simplefin-business")
        mgr.set_backend_default("simplefin", acct.id)

        runner = CliRunner()
        result = runner.invoke(
            simplefin,
            ["default", "--config-dir", str(tmp_path)],
        )
        assert result.exit_code == 0, result.output
        assert "simplefin-personal" in result.output
        assert "simplefin-business" in result.output
        assert "current default" in result.output
        assert "Set or change the default" in result.output

    def test_show_no_default_lists_profiles(self, tmp_path):
        """No default set with multiple profiles -> lists options."""
        mgr = AccountManager(config_dir=tmp_path)
        mgr.create_account("Personal", "simplefin", account_id="simplefin-personal")
        mgr.create_account("Business", "simplefin", account_id="simplefin-business")

        runner = CliRunner()
        result = runner.invoke(
            simplefin,
            ["default", "--config-dir", str(tmp_path)],
        )
        assert result.exit_code == 0, result.output
        assert "simplefin-personal" in result.output
        assert "simplefin-business" in result.output

    def test_set_default(self, tmp_path):
        """--set <id> -> sets the default."""
        mgr = AccountManager(config_dir=tmp_path)
        mgr.create_account("Personal", "simplefin", account_id="simplefin-personal")

        runner = CliRunner()
        result = runner.invoke(
            simplefin,
            ["default", "--set", "simplefin-personal", "--config-dir", str(tmp_path)],
        )
        assert result.exit_code == 0, result.output
        assert mgr.get_backend_default("simplefin") == "simplefin-personal"

    def test_clear_default(self, tmp_path):
        """--clear -> removes the default."""
        mgr = AccountManager(config_dir=tmp_path)
        acct = mgr.create_account("Personal", "simplefin", account_id="simplefin-personal")
        mgr.set_backend_default("simplefin", acct.id)

        runner = CliRunner()
        result = runner.invoke(
            simplefin,
            ["default", "--clear", "--config-dir", str(tmp_path)],
        )
        assert result.exit_code == 0, result.output
        assert mgr.get_backend_default("simplefin") is None

    def test_set_invalid_id(self, tmp_path):
        """--set with nonexistent ID -> error."""
        runner = CliRunner()
        result = runner.invoke(
            simplefin,
            ["default", "--set", "nonexistent", "--config-dir", str(tmp_path)],
        )
        assert result.exit_code != 0
