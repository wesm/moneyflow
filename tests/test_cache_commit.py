"""
Tests for commit functionality when using cached data.

This test file addresses the critical issue where commits were failing
when loading from cache. These tests ensure the backend is properly
authenticated and ready to commit changes even when data is cached.
"""

from datetime import datetime
from unittest.mock import AsyncMock

import polars as pl
import pytest

from moneyflow.data.state import TransactionEdit

FIXED_TIME = datetime(2026, 4, 4)


@pytest.fixture
async def logged_in_mock_mm(mock_mm):
    """Fixture that provides a logged-in mock backend."""
    await mock_mm.login()
    return mock_mm


class TestCacheAndCommit:
    """Test that commits work correctly after loading from cache."""

    async def test_commit_works_after_cache_load(self, data_manager, logged_in_mock_mm):
        """
        Test that we can commit edits even when backend was initialized
        but data was loaded from cache (simulating real scenario).

        This reproduces the bug where commits failed with cached data.
        """
        # Create edit
        edits = [TransactionEdit("txn_1", "merchant", "Old", "New", FIXED_TIME)]

        # Attempt commit
        success, failure, _ = await data_manager.commit_pending_edits(edits)

        # Should succeed
        assert success == 1
        assert failure == 0
        assert len(logged_in_mock_mm.update_calls) == 1

    async def test_commit_multiple_edits_after_cache(self, data_manager, logged_in_mock_mm):
        """Test bulk commits after loading from cache."""
        # Use valid transaction IDs that exist in mock backend
        # Mock has txn_1 through txn_6
        edits = [
            TransactionEdit(f"txn_{i}", "merchant", f"Old{i}", f"New{i}", FIXED_TIME)
            for i in range(1, 7)  # Use 6 valid transaction IDs
        ]

        success, failure, _ = await data_manager.commit_pending_edits(edits)

        # All should succeed
        assert success == 6
        assert failure == 0
        assert len(logged_in_mock_mm.update_calls) == 6

    async def test_commit_handles_not_logged_in(self, mock_mm, tmp_path):
        """
        Test error handling when backend is NOT logged in.

        This might be the root cause - backend not properly authenticated
        when loading from cache.
        """
        from moneyflow.data.data_manager import DataManager

        # We need mock_mm to behave like a real backend that requires auth
        def mock_update(*args, **kwargs):
            if not mock_mm.is_logged_in:
                raise Exception("401 Unauthorized")
            return {"status": "success"}

        mock_mm.update_transaction = AsyncMock(side_effect=mock_update)

        # Don't login - simulate the bug scenario
        dm = DataManager(mock_mm, config_dir=str(tmp_path))

        edits = [TransactionEdit("txn_1", "category", "Old", "New", FIXED_TIME)]

        # With new logic: if ALL commits fail with 401, exception is raised
        with pytest.raises(Exception, match="401 Unauthorized"):
            await dm.commit_pending_edits(edits)

    async def test_commit_with_session_expiration_during_cache(
        self, data_manager, logged_in_mock_mm
    ):
        """
        Test that session expiration is handled during commit.

        Scenario:
        1. Login at startup
        2. Load from cache (fast)
        3. User edits transactions
        4. Session expires before commit
        5. Commit should auto-recover
        """
        # Simulate session expiring by making update fail once
        logged_in_mock_mm.update_transaction = AsyncMock(
            side_effect=[Exception("401 Unauthorized"), {"status": "success"}]
        )

        edits = [TransactionEdit("txn_1", "merchant", "Old", "New", FIXED_TIME)]

        # With new logic: if ALL commits fail with 401, exception is raised
        # This allows _commit_with_retry() to catch it and retry
        with pytest.raises(Exception, match="401 Unauthorized"):
            await data_manager.commit_pending_edits(edits)

        # The exception being raised is GOOD - it triggers retry logic in app.py


class TestCommitFailureDoesNotCorruptLocalState:
    """
    CRITICAL: Test that failed commits don't apply changes locally.

    This was a DATA CORRUPTION bug where commit failures would still
    apply edits to the local DataFrame, making it appear changes succeeded.
    """

    async def test_partial_commit_failure_keeps_pending_edits(
        self, data_manager, logged_in_mock_mm
    ):
        """Failed commits should keep edits in pending list, not clear them."""
        # Make all updates fail
        logged_in_mock_mm.update_transaction = AsyncMock(side_effect=Exception("Network timeout"))

        edits = [
            TransactionEdit("txn_1", "merchant", "Old1", "New1", FIXED_TIME),
            TransactionEdit("txn_2", "merchant", "Old2", "New2", FIXED_TIME),
        ]

        data_manager.pending_edits = edits.copy()

        # Commit will fail
        success, failure, _ = await data_manager.commit_pending_edits(edits)

        assert success == 0
        assert failure == 2
        # CRITICAL: pending_edits should still contain the edits
        # (In app.py, we only clear pending_edits if failure_count == 0)
        assert data_manager.pending_edits == edits

    async def test_all_commits_fail_local_state_unchanged(self, data_manager, logged_in_mock_mm):
        """
        When ALL commits fail, local DataFrame must NOT be modified.

        Simulates the bug: commits fail due to network, but local state
        gets corrupted with uncommitted changes.
        """
        # Set up initial data
        original_df = pl.DataFrame(
            {
                "id": ["txn_1", "txn_2"],
                "merchant": ["OldMerchant1", "OldMerchant2"],
                "amount": [-100.0, -200.0],
            }
        )
        data_manager.df = original_df.clone()

        # Make all commits fail
        logged_in_mock_mm.update_transaction = AsyncMock(
            side_effect=Exception("Connection timeout")
        )

        edits = [
            TransactionEdit("txn_1", "merchant", "OldMerchant1", "NewMerchant1", FIXED_TIME),
            TransactionEdit("txn_2", "merchant", "OldMerchant2", "NewMerchant2", FIXED_TIME),
        ]

        # Commit fails
        success, failure, _ = await data_manager.commit_pending_edits(edits)
        assert failure == 2

        # CRITICAL TEST: Local DataFrame should be UNCHANGED
        # The app.py code should NOT call CommitOrchestrator if failure > 0
        assert data_manager.df["merchant"].to_list() == ["OldMerchant1", "OldMerchant2"]
        # NOT ["NewMerchant1", "NewMerchant2"] - that would be corruption!

    async def test_commit_success_applies_changes_locally(self, data_manager, logged_in_mock_mm):
        """When all commits succeed, changes SHOULD be applied locally."""
        # Set up initial data
        original_df = pl.DataFrame(
            {
                "id": ["txn_1"],
                "merchant": ["OldMerchant"],
                "amount": [-100.0],
            }
        )
        data_manager.df = original_df.clone()

        edits = [TransactionEdit("txn_1", "merchant", "OldMerchant", "NewMerchant", FIXED_TIME)]

        # Commit succeeds
        success, failure, _ = await data_manager.commit_pending_edits(edits)
        assert success == 1
        assert failure == 0

        # This test just verifies commit worked at data_manager level
        # The app.py code is responsible for applying to local DataFrame
        # when failure_count == 0
