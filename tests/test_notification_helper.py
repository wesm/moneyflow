"""
Unit tests for notification_helper.

These tests verify that notification messages are consistent, well-formatted,
and return the correct severity/timeout values.
"""

import pytest

from moneyflow.tui import notification_helper


class TestCommitNotifications:
    """Test commit-related notifications."""

    def test_commit_starting(self):
        msg, severity, timeout = notification_helper.commit_starting(15)
        assert "15 change(s)" in msg
        assert "Committing" in msg
        assert severity == "information"
        assert timeout == 2

    def test_commit_success(self):
        msg, severity, timeout = notification_helper.commit_success(10)
        assert "10 change(s)" in msg
        assert "✅" in msg
        assert "successfully" in msg
        assert severity == "information"
        assert timeout == 3

    def test_commit_partial(self):
        msg, severity, timeout = notification_helper.commit_partial(success=8, failure=2)
        assert "8" in msg
        assert "2" in msg
        assert "failed" in msg
        assert severity == "warning"
        assert timeout == 8

    def test_commit_error(self):
        msg, severity, timeout = notification_helper.commit_error("Connection timeout")
        assert "Connection timeout" in msg
        assert "❌" in msg
        assert severity == "error"
        assert timeout == 5

    def test_no_pending_changes(self):
        msg, severity, timeout = notification_helper.NO_PENDING_CHANGES
        assert "No pending changes" in msg
        assert severity == "information"
        assert timeout == 2


class TestSessionNotifications:
    """Test session/auth notifications."""

    def test_session_expired(self):
        msg, severity, timeout = notification_helper.SESSION_EXPIRED
        assert "expired" in msg.lower()
        assert "Refreshing" in msg
        assert severity == "warning"
        assert timeout == 2

    def test_session_refreshing(self):
        msg, severity, _ = notification_helper.SESSION_REFRESHING
        assert "re-authenticating" in msg
        assert severity == "information"

    def test_session_refresh_success(self):
        msg, severity, _ = notification_helper.SESSION_REFRESH_SUCCESS
        assert "refreshed successfully" in msg
        assert severity == "information"

    def test_session_refresh_failed(self):
        msg, severity, timeout = notification_helper.session_refresh_failed("Invalid token")
        assert "Invalid token" in msg
        assert "Failed" in msg
        assert severity == "error"
        assert timeout == 5


class TestRetryNotifications:
    """Test retry logic notifications."""

    def test_retry_waiting(self):
        msg, severity, timeout = notification_helper.retry_waiting(
            attempt=1, wait_seconds=120.0, max_retries=5
        )
        assert "120s" in msg
        assert "attempt 2/5" in msg  # attempt is 0-indexed
        assert "Ctrl-C" in msg
        assert "abort" in msg
        assert severity == "warning"
        assert timeout == 120

    def test_retry_waiting_first_attempt(self):
        msg, _, _ = notification_helper.retry_waiting(0, 60.0)
        assert "attempt 1/5" in msg
        assert "60s" in msg

    def test_retry_cancelled(self):
        msg, severity, _ = notification_helper.RETRY_CANCELLED
        assert "cancelled" in msg
        assert "user" in msg
        assert severity == "warning"


class TestEditNotifications:
    """Test edit operation notifications."""

    def test_edit_queued(self):
        msg, severity, _ = notification_helper.edit_queued(25)
        assert "25 edits" in msg
        assert "Press w" in msg
        assert severity == "information"

    def test_merchant_changed(self):
        msg, severity, _ = notification_helper.MERCHANT_CHANGED
        assert "Merchant changed" in msg
        assert "Press w" in msg
        assert severity == "information"

    def test_category_changed(self):
        msg, _, _ = notification_helper.CATEGORY_CHANGED
        assert "Category changed" in msg
        assert "Press w" in msg

    def test_bulk_edit_category_queued(self):
        msg, _, _ = notification_helper.bulk_edit_category_queued(50, "Food & Dining", "Groceries")
        assert "50 transactions" in msg
        assert "Food & Dining" in msg
        assert "Groceries" in msg
        assert "→" in msg

    def test_hide_toggled(self):
        msg, _, _ = notification_helper.hide_toggled("Hidden")
        assert "Hidden from reports" in msg
        assert "Press w" in msg

    def test_hide_toggled_bulk(self):
        msg, _, _ = notification_helper.hide_toggled_bulk(10)
        assert "10 transactions" in msg
        assert "Toggled" in msg


class TestNavigationNotifications:
    """Test navigation and view change notifications."""

    def test_view_changed(self):
        msg, _, timeout = notification_helper.view_changed("Merchants")
        assert "Merchants" in msg
        assert "Viewing" in msg
        assert timeout == 1

    def test_sort_changed(self):
        msg, _, _ = notification_helper.sort_changed("Amount")
        assert "Amount" in msg
        assert "Sorting" in msg

    def test_sort_direction_changed(self):
        msg, _, _ = notification_helper.sort_direction_changed("Descending")
        assert "Descending" in msg
        assert "Sort" in msg

    def test_time_period_changed(self):
        msg, _, _ = notification_helper.time_period_changed("October 2025")
        assert "October 2025" in msg
        assert "Viewing" in msg

    def test_all_transactions_view(self):
        msg, severity, _ = notification_helper.ALL_TRANSACTIONS_VIEW
        assert "all transactions" in msg.lower()
        assert "ungrouped" in msg.lower()
        assert severity == "information"


class TestSelectionNotifications:
    """Test selection notifications."""

    def test_selected_count_single(self):
        msg, _, _ = notification_helper.selected_count(1)
        assert "1 transaction(s)" in msg
        assert "Selected" in msg

    def test_selected_count_multiple(self):
        msg, _, _ = notification_helper.selected_count(15)
        assert "15 transaction(s)" in msg


class TestSearchAndFilterNotifications:
    """Test search and filter notifications."""

    def test_search_results(self):
        msg, _, _ = notification_helper.search_results("Amazon", 42)
        assert "Amazon" in msg
        assert "42 results" in msg

    def test_search_cleared(self):
        msg, _, _ = notification_helper.SEARCH_CLEARED
        assert "cleared" in msg

    def test_filters_applied(self):
        msg, _, _ = notification_helper.filters_applied(
            ["hidden items shown", "transfers excluded"]
        )
        assert "hidden items shown" in msg
        assert "transfers excluded" in msg


class TestDuplicateNotifications:
    """Test duplicate detection notifications."""

    def test_duplicates_found(self):
        msg, _, _ = notification_helper.duplicates_found(5)
        assert "5" in msg
        assert "duplicates" in msg

    def test_no_duplicates(self):
        msg, _, _ = notification_helper.NO_DUPLICATES
        assert "✅" in msg
        assert "No duplicates" in msg

    def test_scanning_duplicates(self):
        msg, _, _ = notification_helper.SCANNING_DUPLICATES
        assert "Scanning" in msg

    def test_no_transactions_to_check(self):
        msg, _, _ = notification_helper.NO_TRANSACTIONS_TO_CHECK
        assert "No transactions" in msg


class TestErrorNotifications:
    """Test error and warning notifications."""

    def test_operation_not_available(self):
        msg, severity, _ = notification_helper.operation_not_available(
            "Delete only works in transaction detail view"
        )
        assert "Delete only works" in msg
        assert severity == "information"

    def test_transaction_deleted(self):
        msg, _, _ = notification_helper.TRANSACTION_DELETED
        assert "deleted" in msg

    def test_delete_error(self):
        msg, severity, _ = notification_helper.delete_error("Not found")
        assert "Not found" in msg
        assert severity == "error"

    def test_refresh_needed(self):
        msg, _, _ = notification_helper.REFRESH_NEEDED
        assert "Ctrl+L" in msg


class TestTupleStructure:
    """Test that all notifications return proper tuple structure."""

    def test_all_methods_return_three_element_tuple(self):
        """Ensure all notification methods return (str, str, int)."""
        # Test a few representative ones
        test_cases = [
            (notification_helper.commit_success, (10,)),
            (notification_helper.commit_starting, (5,)),
            (notification_helper.retry_waiting, (1, 60.0)),
            (notification_helper.edit_queued, (5,)),
        ]

        for method, args in test_cases:
            result = method(*args)
            assert isinstance(result, tuple), f"{method.__name__} didn't return tuple"
            assert len(result) == 3, f"{method.__name__} didn't return 3-tuple"
            msg, severity, timeout = result
            assert isinstance(msg, str), f"{method.__name__} message not string"
            assert severity in ("information", "warning", "error"), (
                f"{method.__name__} invalid severity: {severity}"
            )
            assert isinstance(timeout, int), f"{method.__name__} timeout not int"
            assert timeout > 0, f"{method.__name__} timeout not positive"


class TestMessageQuality:
    """Test notification message quality and consistency."""

    @pytest.mark.parametrize(
        "msg",
        [
            notification_helper.commit_success(1)[0],
            notification_helper.NO_DUPLICATES[0],
        ],
    )
    def test_success_messages_use_checkmark(self, msg):
        """Success messages should use ✅ emoji."""
        assert "✅" in msg, f"Success message missing checkmark: {msg}"

    @pytest.mark.parametrize(
        "msg",
        [
            notification_helper.commit_error("test")[0],
            notification_helper.commit_partial(1, 1)[0],
        ],
    )
    def test_error_messages_use_x(self, msg):
        """Error messages should use ❌ emoji."""
        assert "❌" in msg, f"Error message missing X: {msg}"

    def test_warning_messages_use_warning_emoji(self):
        """Warning messages should use ⚠ emoji when appropriate."""
        msg = notification_helper.retry_waiting(1, 60.0)[0]
        assert "⚠" in msg

    @pytest.mark.parametrize(
        "msg",
        [
            notification_helper.MERCHANT_CHANGED[0],
            notification_helper.edit_queued(1)[0],
            notification_helper.REFRESH_NEEDED[0],
        ],
    )
    def test_action_prompts_mention_key(self, msg):
        """Messages prompting action should mention the key."""
        # Should mention a key or keyboard shortcut
        assert any(key in msg for key in ["Press", "w", "Ctrl"]), (
            f"Action message doesn't mention key: {msg}"
        )
