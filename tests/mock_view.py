"""
Mock view presenter for testing AppController without UI.

This mock records all view operations so tests can verify that the
controller calls the right view methods with the right arguments.
"""

from typing import Any, Dict, List, Optional

from moneyflow.view_interface import IViewPresenter, NotificationSeverity


class MockViewPresenter(IViewPresenter):
    """
    Mock implementation of IViewPresenter for testing.

    Records all method calls so tests can verify controller behavior.
    """

    def __init__(self):
        """Initialize mock with empty call logs."""
        # Track all method calls
        self.table_updates = []  # List of (columns, rows, force_rebuild)
        self.notifications = []  # List of (message, severity, timeout)
        self.breadcrumbs = []  # List of breadcrumb texts
        self.stats = []  # List of stats texts
        self.hints = []  # List of hints texts
        self.pending_changes = []  # List of pending change counts

    def update_table(
        self, columns: List[Dict[str, Any]], rows: List[tuple], force_rebuild: bool = True
    ) -> None:
        """Record table update."""
        self.table_updates.append(
            {
                "columns": columns,
                "rows": rows,
                "force_rebuild": force_rebuild,
                "column_count": len(columns),
                "row_count": len(rows),
            }
        )

    def show_notification(
        self, message: str, severity: NotificationSeverity = "information", timeout: int = 3
    ) -> None:
        """Record notification."""
        self.notifications.append({"message": message, "severity": severity, "timeout": timeout})

    def update_breadcrumb(self, text: str) -> None:
        """Record breadcrumb update."""
        self.breadcrumbs.append(text)

    def update_stats(self, stats_text: str) -> None:
        """Record stats update."""
        self.stats.append(stats_text)

    def update_hints(self, hints_text: str) -> None:
        """Record hints update."""
        self.hints.append(hints_text)

    def update_pending_changes(self, count: int) -> None:
        """Record pending changes update."""
        self.pending_changes.append(count)

    # Helper methods for test assertions

    def get_last_table_update(self) -> Optional[Dict[str, Any]]:
        """Get the most recent table update."""
        return self.table_updates[-1] if self.table_updates else None

    def get_last_notification(self) -> Optional[Dict[str, Any]]:
        """Get the most recent notification."""
        return self.notifications[-1] if self.notifications else None

    def assert_table_updated(self, expected_columns: int = None, expected_rows: int = None):
        """Assert that table was updated."""
        assert len(self.table_updates) > 0, "Table was never updated"
        last = self.get_last_table_update()
        assert last is not None, "No table updates found"
        if expected_columns is not None:
            assert last["column_count"] == expected_columns, (
                f"Expected {expected_columns} columns, got {last['column_count']}"
            )
        if expected_rows is not None:
            assert last["row_count"] == expected_rows, (
                f"Expected {expected_rows} rows, got {last['row_count']}"
            )

    def assert_force_rebuild(self, expected: bool):
        """Assert force_rebuild was set correctly."""
        last = self.get_last_table_update()
        assert last is not None, "No table updates"
        assert last["force_rebuild"] == expected, (
            f"Expected force_rebuild={expected}, got {last['force_rebuild']}"
        )

    def reset(self):
        """Clear all recorded calls."""
        self.table_updates.clear()
        self.notifications.clear()
        self.breadcrumbs.clear()
        self.stats.clear()
        self.hints.clear()
        self.pending_changes.clear()
