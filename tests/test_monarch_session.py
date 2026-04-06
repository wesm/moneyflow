"""
Tests for MonarchMoney session management.

Covers:
- Session file placement (profile_dir vs default)
- Session cleanup on login failure
- Session directory cleanup
"""

import os
import pickle
from unittest.mock import AsyncMock, patch

import pytest

from moneyflow.backends import MonarchBackend
from moneyflow.backends.monarch_client import (
    LoginFailedException,
    MonarchMoney,
    RequireMFAException,
)


@pytest.fixture
def profile_dir(tmp_path):
    return tmp_path / "test-profile"


@pytest.fixture
def mm(profile_dir):
    return MonarchMoney(profile_dir=str(profile_dir))


@pytest.fixture
def mm_with_session(mm):
    mm.set_token("test-token")
    mm.save_session()
    return mm


class TestSessionFilePlacement:
    """Test that session files are placed in the correct directory."""

    def test_default_session_file_location(self):
        """Test default session file location (.mm/mm_session.pickle)."""
        mm = MonarchMoney()
        assert mm._session_file == ".mm/mm_session.pickle"

    def test_explicit_session_file_parameter(self):
        """Test that explicit session_file parameter is respected."""
        custom_path = "/tmp/custom_session.pickle"
        mm = MonarchMoney(session_file=custom_path)
        assert mm._session_file == custom_path

    def test_profile_dir_session_location(self):
        """Test session file placed in profile_dir when provided."""
        profile_dir = "/home/user/.moneyflow/profiles/monarch-personal"
        mm = MonarchMoney(profile_dir=profile_dir)
        expected_path = os.path.join(profile_dir, ".mm", "mm_session.pickle")
        assert mm._session_file == expected_path

    def test_explicit_session_file_takes_precedence(self):
        """Test that explicit session_file parameter takes precedence over profile_dir."""
        custom_path = "/tmp/explicit.pickle"
        profile_dir = "/home/user/.moneyflow/profiles/test"
        mm = MonarchMoney(session_file=custom_path, profile_dir=profile_dir)
        assert mm._session_file == custom_path


class TestSessionSaveLoad:
    """Test session save/load with profile directories."""

    def test_save_session_creates_profile_directory(self, mm, profile_dir):
        """Test that saving session creates profile .mm directory."""
        # Set a token and save
        mm.set_token("test-token-123")
        mm.save_session()

        # Verify directory and file exist
        session_dir = profile_dir / ".mm"
        session_file = session_dir / "mm_session.pickle"

        assert session_dir.exists()
        assert session_file.exists()

        # Verify content
        with open(session_file, "rb") as f:
            data = pickle.load(f)
        assert data["token"] == "test-token-123"

    def test_load_session_from_profile_dir(self, mm, profile_dir):
        """Test loading session from profile directory."""
        session_dir = profile_dir / ".mm"
        session_dir.mkdir(parents=True)

        # Create a session file manually
        session_file = session_dir / "mm_session.pickle"
        with open(session_file, "wb") as f:
            pickle.dump({"token": "saved-token-456"}, f)

        # Load session
        mm.load_session()

        assert mm.token == "saved-token-456"
        assert mm._headers["Authorization"] == "Token saved-token-456"


class TestSessionCleanup:
    """Test session cleanup on errors."""

    def test_delete_session_removes_file(self, mm_with_session, profile_dir):
        """Test that delete_session removes the session file."""
        session_file = profile_dir / ".mm" / "mm_session.pickle"
        assert session_file.exists()

        # Delete session
        mm_with_session.delete_session()

        # Verify file removed
        assert not session_file.exists()

    def test_delete_session_removes_empty_directory(self, mm_with_session, profile_dir):
        """Test that delete_session removes empty .mm directory."""
        session_dir = profile_dir / ".mm"
        assert session_dir.exists()

        # Delete session
        mm_with_session.delete_session()

        # Verify directory removed (was empty after deleting session file)
        assert not session_dir.exists()

    def test_delete_session_keeps_nonempty_directory(self, mm_with_session, profile_dir):
        """Test that delete_session keeps .mm directory if it has other files."""
        session_dir = profile_dir / ".mm"

        # Add another file to the directory
        other_file = session_dir / "other.txt"
        other_file.write_text("keep this")

        # Delete session
        mm_with_session.delete_session()

        # Verify directory still exists (has other files)
        assert session_dir.exists()
        assert other_file.exists()
        assert not (session_dir / "mm_session.pickle").exists()

    def test_delete_session_handles_missing_file(self, mm):
        """Test that delete_session handles missing file gracefully."""
        # Try to delete non-existent session (should not raise)
        mm.delete_session()  # No error


class TestLoginErrorHandling:
    """Test login error handling and session cleanup."""

    @pytest.mark.asyncio
    async def test_login_deletes_session_on_failure(self, mm_with_session, profile_dir):
        """Test that failed login deletes the session file."""
        session_file = profile_dir / ".mm" / "mm_session.pickle"
        assert session_file.exists()

        # Mock _login_user to fail
        with patch.object(mm_with_session, "_login_user", new_callable=AsyncMock) as mock_login:
            mock_login.side_effect = LoginFailedException("Invalid credentials")

            # Attempt login (should fail and delete session)
            with pytest.raises(LoginFailedException):
                await mm_with_session.login(
                    email="test@example.com",
                    password="wrong",
                    use_saved_session=False,
                )

            # Verify session file deleted
            assert not session_file.exists()

    @pytest.mark.asyncio
    async def test_login_deletes_session_on_mfa_required(self, mm_with_session, profile_dir):
        """Test that MFA requirement deletes the session file."""
        session_file = profile_dir / ".mm" / "mm_session.pickle"
        assert session_file.exists()

        # Mock _login_user to require MFA
        with patch.object(mm_with_session, "_login_user", new_callable=AsyncMock) as mock_login:
            mock_login.side_effect = RequireMFAException("MFA required")

            # Attempt login (should fail and delete session)
            with pytest.raises(RequireMFAException):
                await mm_with_session.login(
                    email="test@example.com",
                    password="password",
                    use_saved_session=False,
                )

            # Verify session file deleted
            assert not session_file.exists()

    @pytest.mark.asyncio
    async def test_login_retries_on_corrupt_session(self, mm, profile_dir):
        """Test that corrupt session is deleted and login retries."""
        # Create corrupt session file
        session_dir = profile_dir / ".mm"
        session_dir.mkdir(parents=True)
        session_file = session_dir / "mm_session.pickle"
        session_file.write_text("corrupt data not pickle")

        assert session_file.exists()

        # Mock _login_user to succeed
        with patch.object(mm, "_login_user", new_callable=AsyncMock) as mock_login:
            mock_login.return_value = None  # Success

            async def fake_login(*args, **kwargs):
                mm.set_token("new-token")
                return None

            mock_login.side_effect = fake_login

            # Attempt login with use_saved_session=True
            # Should detect corrupt session, delete it, and retry
            await mm.login(
                email="test@example.com",
                password="password",
                use_saved_session=True,
            )

            # Verify it attempted fresh login after corrupt session
            mock_login.assert_called_once()

            # Verify the corrupt session file was recreated and contains the new token
            assert session_file.exists()
            with open(session_file, "rb") as f:
                data = pickle.load(f)
            assert data.get("token") == "new-token"

    @pytest.mark.asyncio
    async def test_login_validates_session_with_api_call(self, mm_with_session, profile_dir):
        """Test that saved session is validated by making an API call."""
        session_file = profile_dir / ".mm" / "mm_session.pickle"
        assert session_file.exists()

        # Mock get_subscription_details to succeed (session is valid)
        with patch.object(
            mm_with_session, "get_subscription_details", new_callable=AsyncMock
        ) as mock_api:
            mock_api.return_value = {"subscription": {}}

            # Login should use saved session and validate it
            await mm_with_session.login(use_saved_session=True)

            # Verify API call was made to validate session
            mock_api.assert_called_once()

    @pytest.mark.asyncio
    async def test_login_deletes_stale_session_on_api_failure(self, mm_with_session, profile_dir):
        """Test that stale session (API rejects) is deleted and login retries."""
        session_file = profile_dir / ".mm" / "mm_session.pickle"
        assert session_file.exists()

        # Mock get_subscription_details to fail (session expired)
        # Mock _login_user to succeed on fresh login
        with (
            patch.object(
                mm_with_session, "get_subscription_details", new_callable=AsyncMock
            ) as mock_api,
            patch.object(mm_with_session, "_login_user", new_callable=AsyncMock) as mock_login,
        ):
            mock_api.side_effect = Exception("401 Unauthorized")
            mock_login.return_value = None

            async def fake_login(*args, **kwargs):
                mm_with_session.set_token("new-fresh-token")
                return None

            mock_login.side_effect = fake_login

            # Should detect stale session via API call, delete it, and retry
            await mm_with_session.login(
                email="test@example.com",
                password="password",
                use_saved_session=True,
            )

            # Verify API validation was attempted
            mock_api.assert_called_once()
            # Verify fresh login was performed
            mock_login.assert_called_once()

            # Verify the stale session file was replaced with the new token
            assert session_file.exists()
            with open(session_file, "rb") as f:
                data = pickle.load(f)
            assert data.get("token") == "new-fresh-token"


class TestBackendIntegration:
    """Test MonarchBackend integration with profile_dir."""

    def test_monarch_backend_with_profile_dir(self):
        """Test that MonarchBackend passes profile_dir to MonarchMoney."""
        profile_dir = "/home/user/.moneyflow/profiles/test"
        backend = MonarchBackend(profile_dir=profile_dir)

        expected_path = os.path.join(profile_dir, ".mm", "mm_session.pickle")
        assert backend.client._session_file == expected_path

    def test_monarch_backend_without_profile_dir(self):
        """Test MonarchBackend without profile_dir uses default location."""
        backend = MonarchBackend()

        # Should use default location
        assert backend.client._session_file == ".mm/mm_session.pickle"
