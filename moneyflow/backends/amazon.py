"""
Amazon purchase data backend implementation.

Provides a read-only view of Amazon purchase history stored in SQLite.
This backend does not connect to any Amazon API - it works with locally
imported CSV files from Amazon's order history export.
"""

import sqlite3
from pathlib import Path
from typing import Any, Dict, List, Optional

from ..categories import get_effective_category_groups, get_profile_category_groups
from .base import FinanceBackend


class AmazonBackend(FinanceBackend):
    """
    Amazon purchase history backend.

    This backend stores Amazon purchase data in a local SQLite database
    and provides a read-only view compatible with moneyflow's interface.

    Unlike cloud-based backends, this doesn't connect to any API - data is
    imported from CSV files exported from Amazon.com.
    """

    def __init__(
        self,
        db_path: Optional[str] = None,
        config_dir: str = str(Path.home() / ".moneyflow"),
        profile_dir: Optional[Path] = None,
    ):
        """
        Initialize the Amazon backend.

        Args:
            db_path: Path to SQLite database. Defaults to ~/.moneyflow/amazon.db
            config_dir: Config directory for loading categories (required for category inheritance)
            profile_dir: Profile directory for profile-local categories

        Note: Database file is not created until first access (lazy initialization).
        """
        if db_path is None:
            db_path = str(Path.home() / ".moneyflow" / "amazon.db")

        self.db_path = Path(db_path).expanduser()
        self.config_dir = config_dir
        self.profile_dir = profile_dir
        self._db_initialized = False

    def _ensure_db_initialized(self) -> None:
        """Ensure database and schema are initialized on first access."""
        if self._db_initialized:
            return

        # Create directory if needed
        self.db_path.parent.mkdir(parents=True, exist_ok=True)

        # Initialize schema for Amazon Orders data
        conn = sqlite3.connect(self.db_path)
        conn.execute("""
            CREATE TABLE IF NOT EXISTS transactions (
                id TEXT PRIMARY KEY,
                date TEXT NOT NULL,
                merchant TEXT NOT NULL,
                category TEXT NOT NULL DEFAULT 'Uncategorized',
                category_id TEXT NOT NULL DEFAULT 'cat_uncategorized',
                amount REAL NOT NULL,
                quantity INTEGER NOT NULL,
                asin TEXT NOT NULL,
                order_id TEXT NOT NULL,
                account TEXT NOT NULL,
                order_status TEXT,
                shipment_status TEXT,
                notes TEXT,
                hideFromReports INTEGER DEFAULT 0,
                imported_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
            )
        """)

        # Note: Categories are NOT stored in database - they come from categories.py
        # This avoids data duplication and allows easy category updates via config

        conn.execute("""
            CREATE TABLE IF NOT EXISTS import_history (
                id INTEGER PRIMARY KEY AUTOINCREMENT,
                filename TEXT NOT NULL,
                record_count INTEGER,
                duplicate_count INTEGER,
                skipped_count INTEGER DEFAULT 0,
                import_date TIMESTAMP DEFAULT CURRENT_TIMESTAMP
            )
        """)

        # Create indexes for performance
        conn.execute("CREATE INDEX IF NOT EXISTS idx_date ON transactions(date)")
        conn.execute("CREATE INDEX IF NOT EXISTS idx_merchant ON transactions(merchant)")
        conn.execute("CREATE INDEX IF NOT EXISTS idx_category ON transactions(category)")
        conn.execute("CREATE INDEX IF NOT EXISTS idx_order_id ON transactions(order_id)")
        conn.execute("CREATE INDEX IF NOT EXISTS idx_asin ON transactions(asin)")

        conn.commit()
        conn.close()

        self._db_initialized = True

    def _get_connection(self) -> sqlite3.Connection:
        """
        Get a database connection, initializing the database if needed.

        Returns:
            SQLite connection object
        """
        self._ensure_db_initialized()
        return sqlite3.connect(self.db_path)

    @staticmethod
    def generate_transaction_id(asin: str, order_id: str) -> str:
        """
        Generate a deterministic transaction ID for deduplication.

        Uses ASIN + Order ID to ensure:
        - Same item in same order = same ID (deduplicate)
        - Same item in different orders = different IDs
        - Editing category/merchant doesn't change ID

        Args:
            asin: Amazon Standard Identification Number
            order_id: Amazon Order ID

        Returns:
            Transaction ID in format: amz_{ASIN}_{clean_order_id}
        """
        # Clean order_id to create filesystem-safe ID
        clean_order = order_id.replace("-", "").replace(" ", "")
        return f"amz_{asin}_{clean_order}"

    def get_display_labels(self) -> Dict[str, str]:
        """
        Get backend-specific display labels for UI elements.

        Returns:
            Dictionary mapping standard field names to display names:
            - merchant: How to display the merchant column
            - account: How to display the account column (singular)
            - accounts: How to display accounts in views/breadcrumbs (plural)
        """
        return {
            "merchant": "Item Name",
            "account": "Order",
            "accounts": "Orders",
        }

    def get_column_config(self) -> Dict[str, Any]:
        """
        Get column configuration for Amazon backend.

        Amazon product names are typically longer than merchant names,
        so we use 30% wider columns for better readability.

        Returns:
            Dictionary with column width percentages:
            - merchant_width_pct: 60 (wider for Item Names)
            - account_width_pct: 30 (Order IDs are small)
        """
        return {"merchant_width_pct": 60, "account_width_pct": 30}

    async def login(
        self,
        email: Optional[str] = None,
        password: Optional[str] = None,
        use_saved_session: bool = True,
        save_session: bool = True,
        mfa_secret_key: Optional[str] = None,
    ) -> None:
        """
        No-op login for Amazon backend.

        Amazon backend doesn't require authentication - it works with
        local data only.
        """
        # Amazon backend doesn't need login - data is local
        pass

    async def get_transactions(
        self,
        limit: int = 100,
        offset: int = 0,
        start_date: Optional[str] = None,
        end_date: Optional[str] = None,
        hidden_from_reports: Optional[bool] = None,
        **kwargs,
    ) -> Dict[str, Any]:
        """
        Fetch transactions from local SQLite database.

        Args:
            limit: Maximum number of transactions to return
            offset: Number of transactions to skip (for pagination)
            start_date: Filter transactions from this date (ISO format: YYYY-MM-DD)
            end_date: Filter transactions to this date (ISO format: YYYY-MM-DD)
            hidden_from_reports: Filter by hideFromReports status (True/False/None for all)

        Returns:
            Dictionary containing transaction data in standard format
        """
        conn = self._get_connection()
        conn.row_factory = sqlite3.Row

        # Simple query - group will be added by data_manager.apply_groups() using Polars
        query = "SELECT * FROM transactions WHERE 1=1"
        params = []

        if start_date:
            query += " AND date >= ?"
            params.append(start_date)

        if end_date:
            query += " AND date <= ?"
            params.append(end_date)

        if hidden_from_reports is not None:
            query += " AND hideFromReports = ?"
            params.append(1 if hidden_from_reports else 0)

        query += " ORDER BY date DESC LIMIT ? OFFSET ?"
        params.extend([limit, offset])

        cursor = conn.execute(query, params)
        rows = cursor.fetchall()

        # Get total count for pagination
        count_query = "SELECT COUNT(*) FROM transactions t WHERE 1=1"
        count_params = []
        if start_date:
            count_query += " AND t.date >= ?"
            count_params.append(start_date)
        if end_date:
            count_query += " AND t.date <= ?"
            count_params.append(end_date)
        if hidden_from_reports is not None:
            count_query += " AND t.hideFromReports = ?"
            count_params.append(1 if hidden_from_reports else 0)

        total_count = conn.execute(count_query, count_params).fetchone()[0]
        conn.close()

        # Convert to standard transaction format
        # Note: group will be added by data_manager.apply_groups() using Polars
        transactions = []
        for row in rows:
            # sqlite3.Row supports dict-style key access
            row_keys = row.keys()
            order_status = row["order_status"] if "order_status" in row_keys else None
            shipment_status = row["shipment_status"] if "shipment_status" in row_keys else None

            txn = {
                "id": row["id"],
                "date": row["date"],
                "amount": row["amount"],
                "merchant": {"id": row["merchant"], "name": row["merchant"]},
                "category": {
                    "id": row["category_id"] or "cat_uncategorized",
                    "name": row["category"] or "Uncategorized",
                },
                # Group will be added by data_manager.apply_groups() based on category
                "account": {"id": row["order_id"], "displayName": row["order_id"]},
                "notes": row["notes"] or "",
                "hideFromReports": bool(row["hideFromReports"]),
                "pending": False,  # Amazon purchases are never pending
                "isRecurring": False,  # We don't track this for Amazon
                # Amazon-specific fields
                "quantity": row["quantity"],
                "asin": row["asin"],
                "order_id": row["order_id"],
                "order_status": order_status,
                "shipment_status": shipment_status,
            }
            transactions.append(txn)

        return {
            "allTransactions": {
                "results": transactions,
                "totalCount": total_count,
            }
        }

    async def get_transaction_categories(self) -> Dict[str, Any]:
        """
        Fetch all categories for Amazon backend with smart inheritance.

        Priority order:
        1. Profile-local config.yaml (if exists)
        2. Inherit from amazon_categories_source (if configured)
        3. Auto-inherit from single other profile (if only one Monarch/YNAB exists)
        4. Built-in defaults

        Returns:
            Dictionary containing categories in standard format
        """
        categories = []
        cat_id_counter = 1

        # Load category groups (profile-aware with Amazon inheritance)
        if self.profile_dir:
            category_groups = get_profile_category_groups(
                profile_dir=self.profile_dir, config_dir=self.config_dir, backend_type="amazon"
            )
        else:
            # Legacy mode - use global config
            category_groups = get_effective_category_groups(self.config_dir)

        # Build categories from loaded category groups
        for group_name, category_names in category_groups.items():
            for cat_name in category_names:
                cat_id = f"cat_{cat_name.lower().replace(' ', '_').replace('&', 'and')}"
                categories.append(
                    {
                        "id": cat_id,
                        "name": cat_name,
                        "group": {"name": group_name, "id": group_name},
                    }
                )
                cat_id_counter += 1

        return {"categories": categories}

    async def get_transaction_category_groups(self) -> Dict[str, Any]:
        """
        Fetch category groups.

        Amazon backend doesn't support groups, returns empty list.
        """
        return {"categoryGroups": []}

    async def update_transaction(
        self,
        transaction_id: str,
        merchant_name: Optional[str] = None,
        category_id: Optional[str] = None,
        hide_from_reports: Optional[bool] = None,
        **kwargs,
    ) -> Dict[str, Any]:
        """
        Update a transaction in the local database.

        Args:
            transaction_id: Unique identifier of the transaction
            merchant_name: New merchant/item name (if changing)
            category_id: New category ID (if changing)
            hide_from_reports: New hidden status (if changing)

        Returns:
            Dictionary containing the updated transaction data
        """
        conn = self._get_connection()

        updates = []
        params = []

        if merchant_name is not None:
            updates.append("merchant = ?")
            params.append(merchant_name)

        if category_id is not None:
            updates.append("category_id = ?")
            params.append(category_id)
            # Also update category name from effective category groups
            # (group is derived from category by data_manager, not stored)
            # Build category_id → category_name lookup
            if self.profile_dir:
                category_groups = get_profile_category_groups(
                    profile_dir=self.profile_dir, config_dir=self.config_dir, backend_type="amazon"
                )
            else:
                category_groups = get_effective_category_groups(self.config_dir)
            category_name = None
            for group_name, category_names in category_groups.items():
                for cat_name in category_names:
                    cat_id = f"cat_{cat_name.lower().replace(' ', '_').replace('&', 'and')}"
                    if cat_id == category_id:
                        category_name = cat_name
                        break
                if category_name:
                    break
            if category_name:
                updates.append("category = ?")
                params.append(category_name)

        if hide_from_reports is not None:
            updates.append("hideFromReports = ?")
            params.append(1 if hide_from_reports else 0)

        if not updates:
            conn.close()
            return {"updateTransaction": {"transaction": {"id": transaction_id}}}

        params.append(transaction_id)
        query = f"UPDATE transactions SET {', '.join(updates)} WHERE id = ?"

        conn.execute(query, params)
        conn.commit()
        conn.close()

        return {"updateTransaction": {"transaction": {"id": transaction_id}}}

    async def delete_transaction(self, transaction_id: str) -> bool:
        """
        Delete a transaction from the database.

        Args:
            transaction_id: Unique identifier of the transaction

        Returns:
            True if deletion was successful
        """
        conn = self._get_connection()
        cursor = conn.execute("DELETE FROM transactions WHERE id = ?", (transaction_id,))
        deleted = cursor.rowcount > 0
        conn.commit()
        conn.close()
        return deleted

    async def get_all_merchants(self) -> List[str]:
        """
        Get all unique merchant/item names from the database.

        Returns:
            List of merchant names, sorted alphabetically
        """
        conn = self._get_connection()
        cursor = conn.execute("SELECT DISTINCT merchant FROM transactions ORDER BY merchant")
        merchants = [row[0] for row in cursor.fetchall()]
        conn.close()
        return merchants

    def get_import_history(self) -> List[Dict[str, Any]]:
        """
        Get history of CSV imports.

        Returns:
            List of import records with filename, counts, and timestamps
        """
        conn = self._get_connection()
        conn.row_factory = sqlite3.Row
        cursor = conn.execute("""
            SELECT filename, record_count, duplicate_count, skipped_count, import_date
            FROM import_history
            ORDER BY id DESC
        """)
        history = [dict(row) for row in cursor.fetchall()]
        conn.close()
        return history

    def get_database_stats(self) -> Dict[str, Any]:
        """
        Get statistics about the database.

        Returns:
            Dictionary with transaction count, date range, total amount, etc.
        """
        conn = self._get_connection()

        stats = {}

        # Total transactions
        stats["total_transactions"] = conn.execute("SELECT COUNT(*) FROM transactions").fetchone()[
            0
        ]

        # Date range
        date_range = conn.execute("""
            SELECT MIN(date) as min_date, MAX(date) as max_date
            FROM transactions
        """).fetchone()
        stats["earliest_date"] = date_range[0]
        stats["latest_date"] = date_range[1]

        # Total amount (remember, amounts are negative)
        total = conn.execute("SELECT SUM(amount) FROM transactions").fetchone()[0]
        stats["total_amount"] = total or 0.0

        # Category count
        stats["category_count"] = conn.execute(
            "SELECT COUNT(DISTINCT category) FROM transactions"
        ).fetchone()[0]

        # Item count
        stats["item_count"] = conn.execute(
            "SELECT COUNT(DISTINCT merchant) FROM transactions"
        ).fetchone()[0]

        conn.close()
        return stats
