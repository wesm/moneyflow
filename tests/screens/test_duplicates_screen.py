from unittest.mock import AsyncMock, MagicMock

import polars as pl
import pytest

from moneyflow.tui.screens.duplicates_screen import DuplicatesScreen


@pytest.fixture
def empty_df():
    return pl.DataFrame(
        {
            "id": [],
            "date": [],
            "merchant": [],
            "amount": [],
            "category": [],
            "account": [],
            "ids": pl.Series([], dtype=pl.List(pl.Utf8)),
        }
    )


@pytest.mark.asyncio
async def test_duplicates_screen_delete_action(empty_df):
    """Test that deleting from the duplicates screen correctly routes through the task runner."""
    mock_app = MagicMock()
    mock_app.task_runner = MagicMock()
    mock_app.task_runner.delete_with_retry = AsyncMock()

    screen = DuplicatesScreen(
        duplicates_df=empty_df,
        groups=[],
        full_df=empty_df,
        main_app=mock_app,
    )

    # Mock textual App behavior
    with pytest.MonkeyPatch.context() as m:
        mock_textual_app = MagicMock()
        mock_textual_app.push_screen = AsyncMock(
            return_value=True
        )  # Simulate user confirming deletion
        m.setattr(DuplicatesScreen, "app", mock_textual_app)
        m.setattr(screen, "rebuild_duplicates_table", MagicMock())
        m.setattr(screen, "refresh_table", MagicMock())
        m.setattr(screen, "update_status_line", MagicMock())

        # Add a mock transaction ID to selected_ids
        txn_id = "txn_123"
        screen.selected_ids.add(txn_id)

        # Trigger the delete worker
        await screen._delete_transaction_async()

        # Verify task_runner.delete_with_retry was called with correct txn_id
        mock_app.task_runner.delete_with_retry.assert_called_once_with(txn_id)
