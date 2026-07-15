"""Generic CSV-backed FinanceBackend for imported transaction data."""
import json
import sqlite3
from pathlib import Path
from typing import Any, Optional

from moneyflow.backends.base import FinanceBackend


class CsvFinanceBackend(FinanceBackend):
    """FinanceBackend that stores transactions in per-institution SQLite databases."""

    def __init__(
        self,
        *,
        profile_dir: Path | None = None,
        config_dir: str | None = None,
        institution_name: str,
    ) -> None:
        if profile_dir is None:
            profile_dir = Path.home() / ".moneyflow"
        self.institution_name = institution_name
        self.config_dir = config_dir or str(Path.home() / ".moneyflow")
        self.db_path = str(profile_dir / f"{institution_name}_transactions.db")
        self._db_initialized = False

    @property
    def supports_category_sync(self) -> bool:
        return False

    def _ensure_db_initialized(self) -> None:
        if self._db_initialized:
            return

        db_path = Path(self.db_path)
        db_path.parent.mkdir(parents=True, exist_ok=True)

        conn = sqlite3.connect(self.db_path)
        conn.execute("""
            CREATE TABLE IF NOT EXISTS transactions (
                id TEXT PRIMARY KEY,
                date TEXT NOT NULL,
                amount REAL NOT NULL,
                merchant TEXT NOT NULL DEFAULT '',
                category TEXT NOT NULL DEFAULT 'Uncategorized',
                category_id TEXT NOT NULL DEFAULT 'cat_uncategorized',
                account TEXT NOT NULL DEFAULT '',
                notes TEXT NOT NULL DEFAULT '',
                extras TEXT NOT NULL DEFAULT '{}',
                hideFromReports INTEGER NOT NULL DEFAULT 0,
                imported_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
            )
        """)
        conn.execute("""
            CREATE TABLE IF NOT EXISTS import_history (
                id INTEGER PRIMARY KEY AUTOINCREMENT,
                filename TEXT NOT NULL,
                record_count INTEGER NOT NULL,
                duplicate_count INTEGER NOT NULL,
                skipped_count INTEGER NOT NULL DEFAULT 0,
                import_date TIMESTAMP DEFAULT CURRENT_TIMESTAMP
            )
        """)
        conn.execute("CREATE INDEX IF NOT EXISTS idx_csv_date ON transactions(date)")
        conn.execute("CREATE INDEX IF NOT EXISTS idx_csv_merchant ON transactions(merchant)")
        conn.execute("CREATE INDEX IF NOT EXISTS idx_csv_category ON transactions(category)")
        conn.commit()
        conn.close()
        self._db_initialized = True

    def _get_connection(self) -> sqlite3.Connection:
        self._ensure_db_initialized()
        return sqlite3.connect(self.db_path)

    def _row_to_transaction_dict(self, row: sqlite3.Row) -> dict[str, Any]:
        extras = json.loads(row["extras"] or "{}")
        return {
            "id": row["id"],
            "date": row["date"],
            "amount": row["amount"],
            "merchant": {"id": row["id"], "name": row["merchant"]},
            "category": {"id": row["category_id"], "name": row["category"]},
            "account": {"id": "", "displayName": row["account"]},
            "notes": row["notes"],
            "hideFromReports": bool(row["hideFromReports"]),
            "pending": False,
            "isRecurring": False,
            **extras,
        }

    def get_backend_type(self) -> str:
        return f"csv_{self.institution_name}"

    async def login(self, **kwargs: Any) -> None:
        pass

    async def get_transactions(
        self,
        limit: int = 100,
        offset: int = 0,
        start_date: str | None = None,
        end_date: str | None = None,
        **kwargs: Any,
    ) -> dict[str, Any]:
        conn = self._get_connection()
        conn.row_factory = sqlite3.Row

        conditions: list[str] = []
        params: list[Any] = []
        if start_date:
            conditions.append("date >= ?")
            params.append(start_date)
        if end_date:
            conditions.append("date <= ?")
            params.append(end_date)

        where_clause = " AND ".join(conditions) if conditions else "1=1"
        count_query = f"SELECT COUNT(*) FROM transactions WHERE {where_clause}"
        total = conn.execute(count_query, params).fetchone()[0]

        query = (
            f"SELECT * FROM transactions WHERE {where_clause} "
            "ORDER BY date DESC LIMIT ? OFFSET ?"
        )
        params.extend([limit, offset])
        rows = conn.execute(query, params).fetchall()

        results = [self._row_to_transaction_dict(row) for row in rows]
        conn.close()
        return {"results": results, "totalCount": total}

    async def update_transaction(
        self,
        transaction_id: str,
        merchant_name: str | None = None,
        category_id: str | None = None,
        hide_from_reports: bool | None = None,
        **kwargs: Any,
    ) -> dict[str, Any]:
        conn = self._get_connection()
        conn.row_factory = sqlite3.Row

        updates: list[str] = []
        params: list[Any] = []
        if merchant_name is not None:
            updates.append("merchant = ?")
            params.append(merchant_name)
        if category_id is not None:
            updates.append("category_id = ?")
            params.append(category_id)
        if hide_from_reports is not None:
            updates.append("hideFromReports = ?")
            params.append(int(hide_from_reports))

        if updates:
            params.append(transaction_id)
            conn.execute(
                f"UPDATE transactions SET {', '.join(updates)} WHERE id = ?",
                params,
            )
            conn.commit()

        row = conn.execute(
            "SELECT * FROM transactions WHERE id = ?", (transaction_id,)
        ).fetchone()
        conn.close()
        if row is None:
            return {"updateTransaction": {"transaction": {"id": transaction_id}}}
        return {"updateTransaction": {"transaction": self._row_to_transaction_dict(row)}}

    async def delete_transaction(self, transaction_id: str) -> bool:
        conn = self._get_connection()
        conn.execute("DELETE FROM transactions WHERE id = ?", (transaction_id,))
        deleted = conn.total_changes > 0
        conn.commit()
        conn.close()
        return deleted

    async def get_all_merchants(self) -> list[str]:
        conn = self._get_connection()
        rows = conn.execute(
            "SELECT DISTINCT merchant FROM transactions ORDER BY merchant"
        ).fetchall()
        conn.close()
        return [row[0] for row in rows]

    async def get_transaction_categories(self) -> dict[str, Any]:
        conn = self._get_connection()
        rows = conn.execute(
            "SELECT DISTINCT category_id, category FROM transactions"
        ).fetchall()
        conn.close()
        categories = [
            {"id": row[0], "name": row[1], "group": {"id": "", "type": "expense"}}
            for row in rows
        ]
        return {"categories": categories}

    async def get_transaction_category_groups(self) -> dict[str, Any]:
        return {"categoryGroups": []}

    def get_display_labels(self) -> dict[str, str]:
        return {
            "merchant": "Description",
            "account": "Account",
            "accounts": "Accounts",
        }

    def get_import_history(self) -> list[dict[str, Any]]:
        conn = self._get_connection()
        conn.row_factory = sqlite3.Row
        rows = conn.execute(
            "SELECT * FROM import_history ORDER BY import_date DESC"
        ).fetchall()
        conn.close()
        return [dict(row) for row in rows]

    def get_database_stats(self) -> dict[str, Any]:
        conn = self._get_connection()
        conn.row_factory = sqlite3.Row
        total = conn.execute("SELECT COUNT(*) FROM transactions").fetchone()[0]
        date_range = conn.execute(
            "SELECT MIN(date) AS earliest, MAX(date) AS latest FROM transactions"
        ).fetchone()
        total_amount = (
            conn.execute("SELECT COALESCE(SUM(amount), 0) FROM transactions").fetchone()[0]
            or 0.0
        )
        conn.close()
        return {
            "total_transactions": total,
            "total_amount": total_amount,
            "earliest_date": date_range["earliest"],
            "latest_date": date_range["latest"],
        }
