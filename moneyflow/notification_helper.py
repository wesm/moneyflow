"""
Centralized notification messages for consistent UI feedback.

This module provides a single source of truth for all user-facing notifications,
making them easier to test, maintain, and keep consistent across the application.

Each method returns a tuple of (message, severity, timeout) that can be
unpacked and passed to the Textual notify() method.
"""

from typing import Literal

NotificationSeverity = Literal["information", "warning", "error"]
NotificationTuple = tuple[str, NotificationSeverity, int]


class NotificationHelper:
    """
    Helper class for consistent notification messages.

    All methods are static and return tuples of (message, severity, timeout)
    that can be directly unpacked into self.notify() calls.

    Example:
        msg, severity, timeout = NotificationHelper.commit_success(15)
        self.notify(msg, severity=severity, timeout=timeout)
    """

    # ==================== Commit & Save ====================

    @staticmethod
    def commit_starting(count: int) -> NotificationTuple:
        """User pressed 'w' to commit changes."""
        return (f"Committing {count} change(s) to backend...", "information", 2)

    @staticmethod
    def commit_success(count: int) -> NotificationTuple:
        """All changes committed successfully."""
        return (f"✅ Committed {count} change(s) successfully!", "information", 3)

    @staticmethod
    def commit_partial(success: int, failure: int) -> NotificationTuple:
        """Some commits succeeded, some failed."""
        return (
            f"✅ Saved {success}, ❌ {failure} failed. Check terminal (run with --dev to see errors)",
            "warning",
            8,
        )

    @staticmethod
    def commit_error(error_msg: str) -> NotificationTuple:
        """Commit failed with an error."""
        return (f"❌ Error committing: {error_msg}", "error", 5)

    @staticmethod
    def no_pending_changes() -> NotificationTuple:
        """User tried to commit but no changes pending."""
        return ("No pending changes to commit", "information", 2)

    @staticmethod
    def commit_cancelled() -> NotificationTuple:
        """User cancelled the commit from batch scope prompt."""
        return ("Commit cancelled", "information", 2)

    # ==================== Session & Auth ====================

    @staticmethod
    def session_expired() -> NotificationTuple:
        """Session expired during an operation."""
        return ("Session expired during commit. Refreshing...", "warning", 2)

    @staticmethod
    def session_refreshing() -> NotificationTuple:
        """Attempting to re-authenticate."""
        return ("Session expired, re-authenticating...", "information", 2)

    @staticmethod
    def session_refresh_success() -> NotificationTuple:
        """Re-authentication succeeded."""
        return ("Session refreshed successfully", "information", 2)

    @staticmethod
    def session_refresh_failed(error_msg: str) -> NotificationTuple:
        """Re-authentication failed."""
        return (f"Failed to refresh session: {error_msg}", "error", 5)

    # ==================== Retry Logic ====================

    @staticmethod
    def retry_waiting(attempt: int, wait_seconds: float, max_retries: int = 5) -> NotificationTuple:
        """Waiting before retry attempt."""
        return (
            f"⚠ Retrying commit in {wait_seconds:.0f}s (attempt {attempt + 1}/{max_retries}). Press Ctrl-C to abort.",
            "warning",
            int(wait_seconds),
        )

    @staticmethod
    def retry_cancelled() -> NotificationTuple:
        """User pressed Ctrl-C to cancel retry."""
        return ("Commit cancelled by user", "warning", 3)

    # ==================== Edit Operations ====================

    @staticmethod
    def edit_queued(count: int) -> NotificationTuple:
        """Edits queued for commit."""
        return (f"Queued {count} edits. Press w to review and commit.", "information", 3)

    @staticmethod
    def merchant_changed() -> NotificationTuple:
        """Single merchant changed."""
        return ("Merchant changed. Press w to review and commit.", "information", 2)

    @staticmethod
    def category_changed() -> NotificationTuple:
        """Single category changed."""
        return ("Category changed. Press w to review and commit.", "information", 2)

    @staticmethod
    def bulk_edit_category_queued(count: int, old_cat: str, new_cat: str) -> NotificationTuple:
        """Bulk recategorization queued."""
        return (
            f"Queued {count} transactions to edit_category: {old_cat} → {new_cat}. Press w to commit.",
            "information",
            3,
        )

    @staticmethod
    def bulk_edit_category_from_group(count: int, group: str, new_cat: str) -> NotificationTuple:
        """Bulk recategorization from group queued."""
        return (
            f"Queued {count} transactions from {group} to edit_category to {new_cat}. Press w to commit.",
            "information",
            3,
        )

    @staticmethod
    def hide_toggled(action: str) -> NotificationTuple:
        """Transaction hidden/unhidden."""
        return (f"{action} from reports. Press w to commit.", "information", 2)

    @staticmethod
    def hide_toggled_bulk(count: int) -> NotificationTuple:
        """Multiple transactions hidden/unhidden."""
        return (
            f"Toggled hide/unhide for {count} transactions. Press w to commit.",
            "information",
            3,
        )

    # ==================== Navigation & Views ====================

    @staticmethod
    def view_changed(view_name: str) -> NotificationTuple:
        """View mode changed."""
        return (f"Viewing: {view_name}", "information", 1)

    @staticmethod
    def sort_changed(field_name: str) -> NotificationTuple:
        """Sort field changed."""
        return (f"Sorting by: {field_name}", "information", 1)

    @staticmethod
    def sort_direction_changed(direction: str) -> NotificationTuple:
        """Sort direction reversed."""
        return (f"Sort: {direction}", "information", 1)

    @staticmethod
    def time_period_changed(description: str) -> NotificationTuple:
        """Time period changed."""
        return (f"Viewing: {description}", "information", 1)

    @staticmethod
    def all_transactions_view() -> NotificationTuple:
        """Switched to ungrouped view."""
        return ("All transactions (ungrouped)", "information", 1)

    # ==================== Selection & Multi-select ====================

    @staticmethod
    def selected_count(count: int) -> NotificationTuple:
        """Selection changed."""
        return (f"Selected: {count} transaction(s)", "information", 1)

    # ==================== Search & Filters ====================

    @staticmethod
    def search_results(query: str, count: int) -> NotificationTuple:
        """Search executed with results."""
        return (f"Search: '{query}' - {count} results", "information", 2)

    @staticmethod
    def search_cleared() -> NotificationTuple:
        """Search cleared."""
        return ("Search cleared", "information", 1)

    @staticmethod
    def filters_applied(status_list: list[str]) -> NotificationTuple:
        """Filters applied."""
        return (f"Filters: {', '.join(status_list)}", "information", 3)

    # ==================== Duplicates ====================

    @staticmethod
    def duplicates_found(count: int) -> NotificationTuple:
        """Duplicates found."""
        return (f"Found {count} potential duplicates", "information", 3)

    @staticmethod
    def no_duplicates() -> NotificationTuple:
        """No duplicates found."""
        return ("✅ No duplicates found!", "information", 3)

    @staticmethod
    def scanning_duplicates() -> NotificationTuple:
        """Scanning for duplicates."""
        return ("Scanning for duplicates...", "information", 1)

    @staticmethod
    def no_transactions_to_check() -> NotificationTuple:
        """No transactions to check for duplicates."""
        return ("No transactions to check", "information", 2)

    # ==================== Errors & Warnings ====================

    @staticmethod
    def operation_not_available(reason: str) -> NotificationTuple:
        """Operation not available in current context."""
        return (reason, "information", 2)

    @staticmethod
    def transaction_deleted() -> NotificationTuple:
        """Transaction deleted successfully."""
        return ("Transaction deleted", "information", 2)

    @staticmethod
    def delete_error(error_msg: str) -> NotificationTuple:
        """Error deleting transaction."""
        return (f"Error deleting: {error_msg}", "error", 5)

    @staticmethod
    def refresh_needed() -> NotificationTuple:
        """Data refresh needed after operation."""
        return ("Press Ctrl+L to refresh data from backend", "information", 3)
