"""
Textual implementation of IViewPresenter.

This module wraps Textual-specific UI operations behind the IViewPresenter
interface, allowing the AppController to work with Textual without direct
dependencies.
"""

import logging
from typing import List

from textual.message import Message
from textual.widgets import DataTable, Static

from .view_interface import IViewPresenter, NotificationSeverity, TableColumn

logger = logging.getLogger(__name__)


class TableUpdated(Message):
    """Message sent when the data table is updated."""

    pass


class TextualViewPresenter(IViewPresenter):
    """
    Textual-specific implementation of IViewPresenter.

    Wraps a Textual app instance and provides IViewPresenter interface.
    """

    def __init__(self, app):
        """
        Initialize with Textual app instance.

        Args:
            app: MoneyflowTUI instance (or any Textual App)
        """
        self.app = app

    def update_table(
        self, columns: List[TableColumn], rows: List[tuple], force_rebuild: bool = True
    ) -> None:
        """Update the main data table."""
        table = self.app.query_one("#data-table", DataTable)

        needs_rebuild = force_rebuild
        if not needs_rebuild:
            expected_keys = [col["key"] for col in columns]
            current_keys = list(table.columns.keys())
            needs_rebuild = current_keys != expected_keys

        if needs_rebuild:
            # Full rebuild - clear columns and rows
            table.clear(columns=True)
            # Add columns
            logger.debug("Adding columns with widths:")
            for col in columns:
                logger.debug(f"  {col['key']}: width={col['width']}")
                table.add_column(col["label"], key=col["key"], width=col["width"])
        else:
            # Smooth update - preserve columns if they match, rebuild if they don't
            # Columns match - just clear rows (smooth, no flash)
            table.clear(columns=False)

        # Add rows with explicit keys to avoid RowKey(value=None) issues
        # For aggregate views, use first column (merchant/category/group/account name) + index
        # For transaction views, use row index as key
        for idx, row in enumerate(rows):
            # Generate unique key using row index to ensure uniqueness
            # (first column might not always be unique, e.g., duplicate merchants)
            row_key = f"row_{idx}"
            table.add_row(*row, key=row_key)

    def show_notification(
        self, message: str, severity: NotificationSeverity = "information", timeout: int = 3
    ) -> None:
        """Show a notification using Textual's notify system."""
        self.app.notify(message, severity=severity, timeout=timeout)

    def update_breadcrumb(self, text: str) -> None:
        """Update breadcrumb widget."""
        if not hasattr(self, "_breadcrumb"):
            self._breadcrumb = self.app.query_one("#breadcrumb", Static)
        self._breadcrumb.update(text)

    def update_stats(self, stats_text: str) -> None:
        """Update stats widget."""
        if not hasattr(self, "_stats_widget"):
            self._stats_widget = self.app.query_one("#stats", Static)
        self._stats_widget.update(stats_text)

    def update_hints(self, hints_text: str) -> None:
        """Update action hints widget."""
        if not hasattr(self, "_hints_widget"):
            self._hints_widget = self.app.query_one("#action-hints", Static)
        self._hints_widget.update(hints_text)

    def update_pending_changes(self, count: int) -> None:
        """Update pending changes widget."""
        if not hasattr(self, "_changes_widget"):
            self._changes_widget = self.app.query_one("#pending-changes", Static)

        if count > 0:
            self._changes_widget.update(f"⚠ {count} pending change(s)")
        else:
            self._changes_widget.update("")

    def on_table_updated(self) -> None:
        """Called after table update to refresh Amazon column if needed."""
        self.app.post_message(TableUpdated())
