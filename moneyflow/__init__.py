"""
Personal Finance Power User TUI

A terminal-based interface for fast transaction management.
Supports multiple finance platforms including Monarch Money.
"""

from .version import get_version

__version__ = get_version()

from .backends import DemoBackend, FinanceBackend, MonarchBackend, get_backend
from .backends.monarch_client import MonarchMoney
from .data.data_manager import DataManager
from .data.duplicate_detector import DuplicateDetector
from .data.state import AppState, SortMode, TransactionEdit, ViewMode

__all__ = [
    "AppState",
    "DataManager",
    "DemoBackend",
    "DuplicateDetector",
    "FinanceBackend",
    "MonarchBackend",
    "MonarchMoney",
    "SortMode",
    "TransactionEdit",
    "ViewMode",
    "get_backend",
]
