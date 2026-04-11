"""
View interface for decoupling business logic from UI implementation.

This module defines the abstract interface that all UI implementations
(Textual, Web, GUI, etc.) must implement. The AppController uses this
interface to perform all UI operations without knowing the implementation.

This enables:
- Testing business logic with MockViewPresenter
- Swapping UI implementations (Textual → Web → GUI)
- Clear separation between "what to show" and "how to render"
"""

from abc import ABC, abstractmethod
from enum import Enum
from typing import List, TypedDict


class NotificationSeverity(str, Enum):
    INFO = "information"
    WARNING = "warning"
    ERROR = "error"

    def __str__(self) -> str:
        return self.value


class TableColumn(TypedDict):
    label: str
    key: str
    width: int


class IViewPresenter(ABC):
    """
    Abstract interface for UI presentation.

    Any UI implementation (Textual, Web, GUI) must implement these methods.
    The AppController calls these methods to update the UI without knowing
    the implementation details.
    """

    @abstractmethod
    def update_table(
        self, columns: List[TableColumn], rows: List[tuple], force_rebuild: bool = True
    ) -> None:
        """
        Update the main data table.

        Args:
            columns: List of column definitions [{"label": str, "key": str, "width": int}, ...]
            rows: List of row tuples matching column order
            force_rebuild: If True, rebuild columns. If False, only update rows (smooth update).

        Example:
            columns = [{"label": "Date", "key": "date", "width": 12}, ...]
            rows = [("2025-10-14", "Amazon", ...), ...]
            view.update_table(columns, rows, force_rebuild=False)
        """
        pass

    @abstractmethod
    def show_notification(
        self,
        message: str,
        severity: NotificationSeverity = NotificationSeverity.INFO,
        timeout: int = 3,
    ) -> None:
        """
        Show a notification to the user.

        Args:
            message: Notification text
            severity: "information", "warning", or "error"
            timeout: How long to show (seconds)
        """
        pass

    @abstractmethod
    def update_breadcrumb(self, text: str) -> None:
        """Update navigation breadcrumb (e.g., 'Merchants > October 2025')."""
        pass

    @abstractmethod
    def update_stats(self, stats_text: str) -> None:
        """Update statistics display (e.g., '1,234 txns | Income: $X | ...')."""
        pass

    @abstractmethod
    def update_hints(self, hints_text: str) -> None:
        """Update action hints bar (e.g., 'Enter=Drill down | m=Edit merchant...')."""
        pass

    @abstractmethod
    def update_pending_changes(self, count: int) -> None:
        """Update pending changes indicator (e.g., '⚠ 5 pending change(s)')."""
        pass

    @abstractmethod
    def on_table_updated(self) -> None:
        """
        Called after the table has been updated.

        This hook allows the UI to perform post-update actions like
        refreshing lazy-loaded content.
        """
        pass
