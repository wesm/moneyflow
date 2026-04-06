"""
Centralized notification messages for consistent UI feedback.

This module provides a single source of truth for all user-facing notifications,
making them easier to test, maintain, and keep consistent across the application.

Each function/constant provides a tuple of (message, severity, timeout) that can be
unpacked and passed to the Textual notify() method.
"""

from moneyflow.tui.view_interface import NotificationSeverity

NotificationTuple = tuple[str, NotificationSeverity, int]

# ==================== Commit & Save ====================


def commit_starting(
    count: int, severity: NotificationSeverity = NotificationSeverity.INFO, timeout: int = 2
) -> NotificationTuple:
    """User pressed 'w' to commit changes."""
    return (f"Committing {count} change(s) to backend...", severity, timeout)


def commit_success(
    count: int, severity: NotificationSeverity = NotificationSeverity.INFO, timeout: int = 3
) -> NotificationTuple:
    """All changes committed successfully."""
    return (f"✅ Committed {count} change(s) successfully!", severity, timeout)


def commit_partial(
    success: int,
    failure: int,
    severity: NotificationSeverity = NotificationSeverity.WARNING,
    timeout: int = 8,
) -> NotificationTuple:
    """Some commits succeeded, some failed."""
    return (
        f"✅ Saved {success}, ❌ {failure} failed. Check terminal (run with --dev to see errors)",
        severity,
        timeout,
    )


def commit_error(
    error_msg: str, severity: NotificationSeverity = NotificationSeverity.ERROR, timeout: int = 5
) -> NotificationTuple:
    """Commit failed with an error."""
    return (f"❌ Error committing: {error_msg}", severity, timeout)


NO_PENDING_CHANGES: NotificationTuple = (
    "No pending changes to commit",
    NotificationSeverity.INFO,
    2,
)
COMMIT_CANCELLED: NotificationTuple = ("Commit cancelled", NotificationSeverity.INFO, 2)

# ==================== Session & Auth ====================

SESSION_EXPIRED: NotificationTuple = (
    "Session expired during commit. Refreshing...",
    NotificationSeverity.WARNING,
    2,
)
SESSION_REFRESHING: NotificationTuple = (
    "Session expired, re-authenticating...",
    NotificationSeverity.INFO,
    2,
)
SESSION_REFRESH_SUCCESS: NotificationTuple = (
    "Session refreshed successfully",
    NotificationSeverity.INFO,
    2,
)


def session_refresh_failed(
    error_msg: str, severity: NotificationSeverity = NotificationSeverity.ERROR, timeout: int = 5
) -> NotificationTuple:
    """Re-authentication failed."""
    return (f"Failed to refresh session: {error_msg}", severity, timeout)


# ==================== Retry Logic ====================


def retry_waiting(
    attempt: int,
    wait_seconds: float,
    max_retries: int = 5,
    severity: NotificationSeverity = NotificationSeverity.WARNING,
) -> NotificationTuple:
    """Waiting before retry attempt."""
    return (
        f"⚠ Retrying commit in {wait_seconds:.0f}s (attempt {attempt + 1}/{max_retries}). Press Ctrl-C to abort.",
        severity,
        int(wait_seconds),
    )


RETRY_CANCELLED: NotificationTuple = ("Commit cancelled by user", NotificationSeverity.WARNING, 3)

# ==================== Edit Operations ====================


def edit_queued(
    count: int, severity: NotificationSeverity = NotificationSeverity.INFO, timeout: int = 3
) -> NotificationTuple:
    """Edits queued for commit."""
    return (f"Queued {count} edits. Press w to review and commit.", severity, timeout)


MERCHANT_CHANGED: NotificationTuple = (
    "Merchant changed. Press w to review and commit.",
    NotificationSeverity.INFO,
    2,
)
CATEGORY_CHANGED: NotificationTuple = (
    "Category changed. Press w to review and commit.",
    NotificationSeverity.INFO,
    2,
)


def bulk_edit_category_queued(
    count: int,
    old_cat: str,
    new_cat: str,
    severity: NotificationSeverity = NotificationSeverity.INFO,
    timeout: int = 3,
) -> NotificationTuple:
    """Bulk recategorization queued."""
    return (
        f"Queued {count} transactions to edit_category: {old_cat} → {new_cat}. Press w to commit.",
        severity,
        timeout,
    )


def bulk_edit_category_from_group(
    count: int,
    group: str,
    new_cat: str,
    severity: NotificationSeverity = NotificationSeverity.INFO,
    timeout: int = 3,
) -> NotificationTuple:
    """Bulk recategorization from group queued."""
    return (
        f"Queued {count} transactions from {group} to edit_category to {new_cat}. Press w to commit.",
        severity,
        timeout,
    )


def hide_toggled(
    action: str, severity: NotificationSeverity = NotificationSeverity.INFO, timeout: int = 2
) -> NotificationTuple:
    """Transaction hidden/unhidden."""
    return (f"{action} from reports. Press w to commit.", severity, timeout)


def hide_toggled_bulk(
    count: int, severity: NotificationSeverity = NotificationSeverity.INFO, timeout: int = 3
) -> NotificationTuple:
    """Multiple transactions hidden/unhidden."""
    return (
        f"Toggled hide/unhide for {count} transactions. Press w to commit.",
        severity,
        timeout,
    )


# ==================== Navigation & Views ====================


def view_changed(
    view_name: str, severity: NotificationSeverity = NotificationSeverity.INFO, timeout: int = 1
) -> NotificationTuple:
    """View mode changed."""
    return (f"Viewing: {view_name}", severity, timeout)


def sort_changed(
    field_name: str, severity: NotificationSeverity = NotificationSeverity.INFO, timeout: int = 1
) -> NotificationTuple:
    """Sort field changed."""
    return (f"Sorting by: {field_name}", severity, timeout)


def sort_direction_changed(
    direction: str, severity: NotificationSeverity = NotificationSeverity.INFO, timeout: int = 1
) -> NotificationTuple:
    """Sort direction reversed."""
    return (f"Sort: {direction}", severity, timeout)


def time_period_changed(
    description: str, severity: NotificationSeverity = NotificationSeverity.INFO, timeout: int = 1
) -> NotificationTuple:
    """Time period changed."""
    return (f"Viewing: {description}", severity, timeout)


ALL_TRANSACTIONS_VIEW: NotificationTuple = (
    "All transactions (ungrouped)",
    NotificationSeverity.INFO,
    1,
)

# ==================== Selection & Multi-select ====================


def selected_count(
    count: int, severity: NotificationSeverity = NotificationSeverity.INFO, timeout: int = 1
) -> NotificationTuple:
    """Selection changed."""
    return (f"Selected: {count} transaction(s)", severity, timeout)


# ==================== Search & Filters ====================


def search_results(
    query: str,
    count: int,
    severity: NotificationSeverity = NotificationSeverity.INFO,
    timeout: int = 2,
) -> NotificationTuple:
    """Search executed with results."""
    return (f"Search: '{query}' - {count} results", severity, timeout)


SEARCH_CLEARED: NotificationTuple = ("Search cleared", NotificationSeverity.INFO, 1)


def filters_applied(
    status_list: list[str],
    severity: NotificationSeverity = NotificationSeverity.INFO,
    timeout: int = 3,
) -> NotificationTuple:
    """Filters applied."""
    return (f"Filters: {', '.join(status_list)}", severity, timeout)


# ==================== Duplicates ====================


def duplicates_found(
    count: int, severity: NotificationSeverity = NotificationSeverity.INFO, timeout: int = 3
) -> NotificationTuple:
    """Duplicates found."""
    return (f"Found {count} potential duplicates", severity, timeout)


NO_DUPLICATES: NotificationTuple = ("✅ No duplicates found!", NotificationSeverity.INFO, 3)
SCANNING_DUPLICATES: NotificationTuple = (
    "Scanning for duplicates...",
    NotificationSeverity.INFO,
    1,
)
NO_TRANSACTIONS_TO_CHECK: NotificationTuple = (
    "No transactions to check",
    NotificationSeverity.INFO,
    2,
)

# ==================== Errors & Warnings ====================


def operation_not_available(
    reason: str, severity: NotificationSeverity = NotificationSeverity.INFO, timeout: int = 2
) -> NotificationTuple:
    """Operation not available in current context."""
    return (reason, severity, timeout)


TRANSACTION_DELETED: NotificationTuple = ("Transaction deleted", NotificationSeverity.INFO, 2)


def delete_error(
    error_msg: str, severity: NotificationSeverity = NotificationSeverity.ERROR, timeout: int = 5
) -> NotificationTuple:
    """Error deleting transaction."""
    return (f"Error deleting: {error_msg}", severity, timeout)


REFRESH_NEEDED: NotificationTuple = (
    "Press Ctrl+L to refresh data from backend",
    NotificationSeverity.INFO,
    3,
)
