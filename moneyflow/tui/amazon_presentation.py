import logging
import tempfile
from pathlib import Path
from typing import Optional

import polars as pl
from textual.widgets import DataTable

from ..data.amazon_linker import AmazonLinker

logger = logging.getLogger(__name__)


class AmazonPresentationManager:
    """Manages presentation logic and lazy-loading for Amazon match data."""

    def __init__(self, demo_mode: bool, config_dir: Optional[str]):
        self.demo_mode = demo_mode
        self.config_dir = config_dir

        self._cache: dict[str, Optional[str]] = {}
        self._rows_loaded: set[int] = set()
        self._column_visible: bool = False
        self._column_index: Optional[int] = None
        self._row_to_txn_id: dict[int, str] = {}

    def set_visibility(self, visible: bool, index: Optional[int] = None) -> None:
        self._column_visible = visible
        self._column_index = index

    def get_demo_config_dir(self) -> Path:
        """Get the config directory for demo mode (cross-platform temp dir)."""
        return Path(tempfile.gettempdir()) / "moneyflow_demo"

    def create_demo_amazon_db(self, transactions) -> None:
        """Create demo Amazon database with matching orders for demo transactions."""
        from ..data.demo_data_generator import create_demo_amazon_database

        if not self.demo_mode:
            return

        demo_config_dir = str(self.get_demo_config_dir())
        create_demo_amazon_database(demo_config_dir, transactions)

    def find_amazon_matches(self, transaction: dict) -> tuple[list, bool]:
        """
        Find matching Amazon orders for a transaction.

        Returns:
            Tuple of (matches, searched)
        """
        merchant = transaction.get("merchant", "")
        amount = transaction.get("amount", 0)
        txn_date = transaction.get("date", "")

        if hasattr(txn_date, "isoformat"):
            txn_date = txn_date.isoformat()
        else:
            txn_date = str(txn_date)

        if self.demo_mode:
            config_dir = self.get_demo_config_dir()
        else:
            config_dir = Path(self.config_dir) if self.config_dir else Path.home() / ".moneyflow"

        linker = AmazonLinker(config_dir)

        if not linker.is_amazon_merchant(merchant):
            return [], False

        try:
            matches = linker.find_matching_orders(
                amount=float(amount),
                transaction_date=txn_date,
                date_tolerance_days=7,
            )
            return matches, True
        except Exception as e:
            logger.warning(f"Error finding Amazon matches: {e}")
            return [], True

    def format_amazon_match_status(self, matches: list) -> str:
        """Format Amazon match status for display in table column."""
        if not matches:
            return ""

        best_match = None
        indicator = ""

        for m in matches:
            if m.confidence in ("high", "medium"):
                best_match = m
                indicator = "✓"
                break
            elif m.confidence == "likely" and best_match is None:
                best_match = m
                indicator = "~"

        if not best_match:
            return ""

        item_name = ""
        if best_match.items and len(best_match.items) > 0:
            item_name = best_match.items[0].get("name", "")

        if not item_name:
            return indicator

        max_name_len = 27
        if len(item_name) > max_name_len:
            item_name = item_name[: max_name_len - 1] + "…"

        return f"{indicator} {item_name}"

    def search_amazon_items_for_query(
        self, query: str, df: Optional[pl.DataFrame], start_date=None, end_date=None
    ) -> set[str]:
        """Search Amazon transactions for items matching a query string."""
        matching_ids: set[str] = set()
        query_lower = query.lower()

        if df is None or len(df) == 0:
            return matching_ids

        if start_date and end_date:
            df = df.filter((pl.col("date") >= start_date) & (pl.col("date") <= end_date))

        if self.demo_mode:
            config_dir = self.get_demo_config_dir()
        else:
            config_dir = Path(self.config_dir) if self.config_dir else Path.home() / ".moneyflow"

        linker = AmazonLinker(config_dir)

        if not linker.find_amazon_databases():
            return matching_ids

        for row in df.iter_rows(named=True):
            merchant = row.get("merchant", "")
            if not linker.is_amazon_merchant(merchant):
                continue

            txn_id = row.get("id", "")
            amount = row.get("amount", 0)
            txn_date = row.get("date", "")

            if hasattr(txn_date, "isoformat"):
                txn_date = txn_date.isoformat()
            else:
                txn_date = str(txn_date)

            try:
                matches = linker.find_matching_orders(
                    amount=float(amount),
                    transaction_date=txn_date,
                    date_tolerance_days=7,
                )

                for match in matches:
                    for item in match.items:
                        item_name = item.get("name", "").lower()
                        if query_lower in item_name:
                            matching_ids.add(txn_id)
                            break
                    if txn_id in matching_ids:
                        break

            except Exception as e:
                logger.warning(f"Error searching Amazon for txn {txn_id}: {e}")

        return matching_ids

    def get_amazon_match_status(
        self, txn_id: str, amount: float, date_str: str, merchant: str
    ) -> str:
        """Get Amazon match status for a transaction, using cache when available."""
        if txn_id in self._cache:
            cached = self._cache[txn_id]
            if cached is not None:
                return cached

        matches, _ = self.find_amazon_matches(
            {"merchant": merchant, "amount": amount, "date": date_str}
        )

        status = self.format_amazon_match_status(matches)
        self._cache[txn_id] = status
        return status

    def load_matches_for_rows(
        self, table: DataTable, current_data: Optional[pl.DataFrame], start_row: int, end_row: int
    ) -> None:
        """Load Amazon matches for a range of rows and update table cells."""
        logger.debug(
            f"load_matches_for_rows: start={start_row}, end={end_row}, "
            f"visible={self._column_visible}, col_idx={self._column_index}"
        )
        if not self._column_visible or self._column_index is None:
            return

        if current_data is None:
            return

        df = current_data

        for row_idx in range(start_row, min(end_row, len(df))):
            if row_idx in self._rows_loaded:
                continue

            if row_idx >= len(df):
                break

            row_data = df.row(row_idx, named=True)
            txn_id = row_data["id"]
            amount = row_data["amount"]
            date_val = row_data["date"]
            merchant = row_data["merchant"]

            if hasattr(date_val, "isoformat"):
                date_str = date_val.isoformat()
            else:
                date_str = str(date_val)

            status = self.get_amazon_match_status(txn_id, amount, date_str, merchant)

            try:
                table.update_cell_at((row_idx, self._column_index), status)
                logger.debug(
                    f"Updated cell ({row_idx}, {self._column_index}) with status: {status[:20] if status else '(empty)'}..."
                )
            except Exception as e:
                logger.debug(f"Failed to update cell ({row_idx}, {self._column_index}): {e}")

            self._rows_loaded.add(row_idx)

    def on_amazon_view_refresh(self, current_data: Optional[pl.DataFrame]) -> None:
        """Called when the view is refreshed with Amazon transactions."""
        logger.debug(
            f"on_amazon_view_refresh: visible={self._column_visible}, "
            f"col_idx={self._column_index}, data_len={len(current_data) if current_data is not None else 'None'}"
        )
        self._rows_loaded.clear()
        self._row_to_txn_id.clear()

        if current_data is not None:
            for idx, row_data in enumerate(current_data.iter_rows(named=True)):
                txn_id = row_data["id"]
                self._row_to_txn_id[idx] = txn_id
                if txn_id in self._cache:
                    self._rows_loaded.add(idx)

    def get_cache(self) -> dict[str, Optional[str]]:
        """Get the full Amazon match cache dictionary."""
        return self._cache

    def get_cached_status(self, txn_id: str) -> Optional[str]:
        return self._cache.get(txn_id)

    def set_cached_status(self, txn_id: str, status: Optional[str]) -> None:
        self._cache[txn_id] = status
