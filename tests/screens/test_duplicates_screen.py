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


@pytest.mark.asyncio
async def test_duplicates_screen_rejects_refresh_during_delete_confirmation(empty_df):
    """A refresh while confirmation is open invalidates the selected transaction IDs."""
    main_app = MagicMock()
    main_app._simplefin_refresh_generation = 0
    main_app.can_edit_transaction_snapshot.return_value = False
    main_app.task_runner.delete_with_retry = AsyncMock()
    screen = DuplicatesScreen(empty_df, [], empty_df, main_app)
    screen.selected_ids.add("legacy-account:transaction")
    notifications = []
    screen.notify = lambda message, **kwargs: notifications.append(message)

    with pytest.MonkeyPatch.context() as monkeypatch:
        textual_app = MagicMock()
        textual_app.push_screen = AsyncMock(return_value=True)
        monkeypatch.setattr(DuplicatesScreen, "app", textual_app)

        await screen._delete_transaction_async()

    main_app.can_edit_transaction_snapshot.assert_called_once_with(0)
    main_app.task_runner.delete_with_retry.assert_not_called()
    assert notifications == [
        "Transactions refreshed; close and reopen duplicate review before deleting."
    ]


def test_duplicates_screen_rejects_stale_snapshot_hide():
    """A refresh invalidates duplicate rows before they can queue legacy IDs."""
    transactions = pl.DataFrame(
        {
            "id": ["legacy-account:transaction"],
            "date": ["2026-01-01"],
            "merchant": ["Example Merchant"],
            "amount": [-10.0],
            "category": ["Uncategorized"],
            "account": ["Example Account"],
            "hideFromReports": [False],
        }
    )
    main_app = MagicMock()
    main_app._simplefin_refresh_generation = 1
    main_app.can_edit_transaction_snapshot.return_value = False
    screen = DuplicatesScreen(
        transactions, [["legacy-account:transaction"]], transactions, main_app
    )
    screen.selected_ids.add("legacy-account:transaction")
    notifications = []
    screen.notify = lambda message, **kwargs: notifications.append(message)

    screen.action_toggle_hide()

    main_app.controller.queue_hide_toggle_edits.assert_not_called()
    assert notifications == [
        "Transactions refreshed; close and reopen duplicate review before editing."
    ]
