"""
Personal Finance Power User TUI

A terminal-based interface for fast transaction management.
Supports multiple finance platforms including Monarch Money.
"""

from .version import get_version

__version__ = get_version()

from .backends import DemoBackend, FinanceBackend, MonarchBackend, get_backend
from .data_manager import DataManager
from .duplicate_detector import DuplicateDetector
from .monarchmoney import MonarchMoney
from .state import AppState, SortMode, TransactionEdit, ViewMode

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
