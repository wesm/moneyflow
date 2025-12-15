"""
Amazon transaction linker service.

Links Monarch/YNAB transactions to Amazon orders by matching amount and date.
Searches Amazon profile databases for orders that match a given transaction.
"""

import sqlite3
from dataclasses import dataclass
from datetime import date, datetime, timedelta
from pathlib import Path
from typing import List, Optional

from .logging_config import get_logger

logger = get_logger(__name__)


@dataclass
class AmazonOrderMatch:
    """
    Represents a matched Amazon order.

    Attributes:
        order_id: Amazon order ID (e.g., "113-1234567-8901234")
        order_date: Date of the order (YYYY-MM-DD format)
        total_amount: Sum of all items in the order (negative for expenses)
        items: List of items in the order, each with:
            - name: Product name
            - amount: Item price (negative)
            - quantity: Number of items
            - asin: Amazon Standard Identification Number
        confidence: Match confidence ("high" for exact match, "medium" for close)
        source_profile: Name of the Amazon profile this match came from
    """

    order_id: str
    order_date: str
    total_amount: float
    items: List[dict]
    confidence: str
    source_profile: str


class AmazonLinker:
    """
    Links transactions to Amazon orders by matching amount and date.

    This service searches Amazon profile databases for orders that match
    a given transaction amount (within tolerance) and date (within date range).

    Usage:
        linker = AmazonLinker(config_dir=Path.home() / ".moneyflow")

        # Check if merchant looks like Amazon
        if linker.is_amazon_merchant("AMZN MKTP US"):
            matches = linker.find_matching_orders(
                amount=-37.98,
                transaction_date="2025-01-15",
            )
            for match in matches:
                print(f"Order {match.order_id}: {match.total_amount}")
    """

    # Patterns that identify Amazon merchants (case-insensitive)
    AMAZON_PATTERNS = ["amazon", "amzn"]

    # Default amount tolerance (allows for rounding differences)
    AMOUNT_TOLERANCE = 0.02

    def __init__(self, config_dir: Path):
        """
        Initialize the Amazon linker.

        Args:
            config_dir: Path to moneyflow config directory (e.g., ~/.moneyflow)
        """
        self.config_dir = Path(config_dir)
        self.profiles_dir = self.config_dir / "profiles"

    def is_amazon_merchant(self, merchant_name: str) -> bool:
        """
        Check if a merchant name looks like Amazon.

        Args:
            merchant_name: Merchant name from transaction

        Returns:
            True if merchant appears to be Amazon
        """
        if not merchant_name:
            return False

        merchant_lower = merchant_name.lower()
        return any(pattern in merchant_lower for pattern in self.AMAZON_PATTERNS)

    def find_amazon_databases(self) -> List[Path]:
        """
        Find all Amazon profile databases.

        Searches for amazon.db files in profiles named "amazon" or starting with "amazon-".

        Returns:
            List of paths to Amazon databases
        """
        databases = []

        if not self.profiles_dir.exists():
            return databases

        for profile_dir in self.profiles_dir.iterdir():
            if not profile_dir.is_dir():
                continue

            # Look in profiles named "amazon" or starting with "amazon-"
            profile_name = profile_dir.name
            if profile_name != "amazon" and not profile_name.startswith("amazon-"):
                continue

            db_path = profile_dir / "amazon.db"
            if db_path.exists():
                databases.append(db_path)

        return databases

    def find_matching_orders(
        self,
        amount: float,
        transaction_date: str,
        date_tolerance_days: int = 7,
        amount_tolerance: Optional[float] = None,
    ) -> List[AmazonOrderMatch]:
        """
        Find Amazon orders matching the given amount and date.

        Args:
            amount: Transaction amount to match (negative for expenses)
            transaction_date: Transaction date (YYYY-MM-DD format)
            date_tolerance_days: Days +/- to search for matching orders
            amount_tolerance: Amount tolerance for matching (default 0.02)

        Returns:
            List of matching orders, sorted by date proximity (closest first)
        """
        if amount_tolerance is None:
            amount_tolerance = self.AMOUNT_TOLERANCE

        databases = self.find_amazon_databases()
        if not databases:
            return []

        # Parse transaction date
        try:
            txn_date = datetime.strptime(transaction_date, "%Y-%m-%d").date()
        except ValueError:
            logger.warning(f"Invalid transaction date format: {transaction_date}")
            return []

        # Calculate date range
        start_date = txn_date - timedelta(days=date_tolerance_days)
        end_date = txn_date + timedelta(days=date_tolerance_days)

        all_matches: List[AmazonOrderMatch] = []

        for db_path in databases:
            try:
                matches = self._search_database(
                    db_path=db_path,
                    amount=amount,
                    amount_tolerance=amount_tolerance,
                    start_date=start_date,
                    end_date=end_date,
                    txn_date=txn_date,
                )
                all_matches.extend(matches)
            except Exception as e:
                logger.warning(f"Error searching database {db_path}: {e}")
                continue

        # Sort by date proximity (closest to transaction date first)
        all_matches.sort(key=lambda m: abs((self._parse_date(m.order_date) - txn_date).days))

        return all_matches

    def _parse_date(self, date_str: str) -> date:
        """Parse a date string to date object."""
        return datetime.strptime(date_str, "%Y-%m-%d").date()

    def _search_database(
        self,
        db_path: Path,
        amount: float,
        amount_tolerance: float,
        start_date: date,
        end_date: date,
        txn_date: date,
    ) -> List[AmazonOrderMatch]:
        """
        Search a single Amazon database for matching orders.

        Args:
            db_path: Path to Amazon database
            amount: Amount to match
            amount_tolerance: Amount tolerance
            start_date: Start of date range
            end_date: End of date range
            txn_date: Transaction date (for confidence calculation)

        Returns:
            List of matching orders from this database
        """
        matches = []
        profile_name = db_path.parent.name

        try:
            conn = sqlite3.connect(db_path)
            conn.row_factory = sqlite3.Row

            # Query to aggregate orders and filter by date range
            # We'll do amount filtering in Python for flexibility
            query = """
                SELECT
                    order_id,
                    MIN(date) as order_date,
                    SUM(amount) as total_amount,
                    GROUP_CONCAT(id) as item_ids
                FROM transactions
                WHERE date >= ? AND date <= ?
                GROUP BY order_id
            """

            cursor = conn.execute(
                query,
                (start_date.isoformat(), end_date.isoformat()),
            )

            for row in cursor:
                order_total = row["total_amount"]

                # Check amount match within tolerance
                if abs(order_total - amount) > amount_tolerance:
                    continue

                # Fetch item details for this order
                items = self._fetch_order_items(conn, row["order_id"])

                # Determine confidence
                order_date = self._parse_date(row["order_date"])
                days_diff = abs((order_date - txn_date).days)
                amount_diff = abs(order_total - amount)

                if amount_diff < 0.01 and days_diff <= 2:
                    confidence = "high"
                else:
                    confidence = "medium"

                match = AmazonOrderMatch(
                    order_id=row["order_id"],
                    order_date=row["order_date"],
                    total_amount=round(order_total, 2),
                    items=items,
                    confidence=confidence,
                    source_profile=profile_name,
                )
                matches.append(match)

            conn.close()

        except sqlite3.DatabaseError as e:
            logger.warning(f"Database error reading {db_path}: {e}")
            return []

        return matches

    def _fetch_order_items(self, conn: sqlite3.Connection, order_id: str) -> List[dict]:
        """
        Fetch all items for a given order.

        Args:
            conn: Database connection
            order_id: Order ID to fetch items for

        Returns:
            List of item dicts with name, amount, quantity, asin
        """
        cursor = conn.execute(
            """
            SELECT merchant as name, amount, quantity, asin
            FROM transactions
            WHERE order_id = ?
            ORDER BY merchant
            """,
            (order_id,),
        )

        items = []
        for row in cursor:
            items.append(
                {
                    "name": row["name"],
                    "amount": row["amount"],
                    "quantity": row["quantity"],
                    "asin": row["asin"],
                }
            )

        return items
