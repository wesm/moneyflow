"""
Textual implementation of IViewPresenter.

This module wraps Textual-specific UI operations behind the IViewPresenter
interface, allowing the AppController to work with Textual without direct
dependencies.
"""

from typing import Any, Dict, List

from textual.widgets import DataTable, Static

from .view_interface import IViewPresenter, NotificationSeverity


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
        self, columns: List[Dict[str, Any]], rows: List[tuple], force_rebuild: bool = True
    ) -> None:
        """Update the main data table."""
        import logging

        logger = logging.getLogger(__name__)

        table = self.app.query_one("#data-table", DataTable)

        if force_rebuild:
            # Full rebuild - clear columns and rows
            table.clear(columns=True)
            # Add columns
            logger.debug("Adding columns with widths:")
            for col in columns:
                logger.debug(f"  {col['key']}: width={col['width']}")
                table.add_column(col["label"], key=col["key"], width=col["width"])
        else:
            # Smooth update - preserve columns if they match, rebuild if they don't
            expected_keys = [col["key"] for col in columns]
            current_keys = list(table.columns.keys())

            if current_keys != expected_keys:
                # Column mismatch - need full rebuild
                table.clear(columns=True)
                for col in columns:
                    table.add_column(col["label"], key=col["key"], width=col["width"])
            else:
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
        breadcrumb = self.app.query_one("#breadcrumb", Static)
        breadcrumb.update(text)

    def update_stats(self, stats_text: str) -> None:
        """Update stats widget."""
        stats_widget = self.app.query_one("#stats", Static)
        stats_widget.update(stats_text)

    def update_hints(self, hints_text: str) -> None:
        """Update action hints widget."""
        hints_widget = self.app.query_one("#action-hints", Static)
        hints_widget.update(hints_text)

    def update_pending_changes(self, count: int) -> None:
        """Update pending changes widget."""
        changes_widget = self.app.query_one("#pending-changes", Static)
        if count > 0:
            changes_widget.update(f"⚠ {count} pending change(s)")
        else:
            changes_widget.update("")

    def on_table_updated(self) -> None:
        """Called after table update to refresh Amazon column if needed."""
        self.app.handle_amazon_column_refresh()

    def get_amazon_cache(self) -> dict[str, str | None] | None:
        """Get the Amazon match cache from the app."""
        return self.app._amazon_match_cache
