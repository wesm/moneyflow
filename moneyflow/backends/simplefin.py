"""
SimpleFIN backend implementation for moneyflow.

Connects to the SimpleFIN Bridge API (https://www.simplefin.org/protocol.html)
to pull read-only transaction data. Transaction edits are persisted in a local
SQLite store, enabling category/merchant/hide changes even though the API itself
is read-only.
"""

import logging
import os
import sqlite3
from datetime import date, datetime, timedelta, timezone
from pathlib import Path
from typing import Any, Dict, List, Optional

from .base import FinanceBackend
from .simplefin_client import SimpleFinClient, parse_access_url

logger = logging.getLogger(__name__)


class SimpleFinBackend(FinanceBackend):
    """
    SimpleFIN backend adapter with local SQLite persistence.

    Transaction data is fetched from the SimpleFIN API and stored in a local
    SQLite database. All edits (category, merchant, hide) are persisted locally.
    The API is never modified — this is a local-only persistence layer.

    Key behaviours:
    - Locally writable: can_write_transactions=True allows the commit pipeline to
      persist edits in SQLite, even though the SimpleFIN protocol is read-only.
    - API read-only: read_only=True gates category/group manager features.
    - SQLite-backed: all data and edits stored in a local file.
    - First-run: empty SQLite triggers a full API fetch (3-year lookback).
    - Refresh: fetches transactions since 2 weeks before last refresh date,
      skips pending, merges additively (INSERT OR IGNORE preserves local edits).
      The 2-week lookback ensures transactions that transition from pending to
      posted are re-fetched and captured, even if their original date predates
      the last refresh boundary.
    - Currency-safe: persists one ISO currency code per profile and rejects
      mixed-currency data rather than aggregating incompatible amounts.
    - Local deletion: tombstones keep deleted transaction IDs excluded from
      subsequent additive and hard refreshes.
    - No category hierarchy: get_transaction_categories() and
      get_transaction_category_groups() return empty collections.
    """

    DEFAULT_DB_PATH = str(Path.home() / ".moneyflow" / "simplefin.db")

    def __init__(
        self,
        db_path: Optional[str] = None,
        profile_dir: Optional[Path] = None,
    ) -> None:
        """
        Initialise the backend.

        Args:
            db_path: Path to the local SQLite database file. Defaults to
                     ~/.moneyflow/simplefin.db or profile_dir/simplefin.db.
            profile_dir: Profile directory for multi-account mode. If provided
                         and db_path is not set, db_path is derived from
                         profile_dir.
        """
        self.profile_dir = profile_dir
        self._managed_db_directory = db_path is None
        if db_path is None and profile_dir is not None:
            db_path = str(Path(profile_dir) / "simplefin.db")
        self._client: Optional[SimpleFinClient] = None
        if db_path:
            self._db_path = str(Path(db_path).expanduser())
        else:
            self._db_path = self.DEFAULT_DB_PATH
            logger.warning(
                "No db_path or profile_dir provided — using default: %s",
                self._db_path,
            )
        self._db_initialized = False

    # ------------------------------------------------------------------
    # SQLite store helpers
    # ------------------------------------------------------------------

    def _ensure_db_initialized(self) -> None:
        """Create the database directory and schema on first access."""
        if self._db_initialized:
            return

        if self._db_path != ":memory:":
            db_path = Path(self._db_path)
            db_path.parent.mkdir(mode=0o700, parents=True, exist_ok=True)
            if self._managed_db_directory:
                os.chmod(db_path.parent, 0o700)
            flags = os.O_RDWR | os.O_CREAT
            if hasattr(os, "O_CLOEXEC"):
                flags |= os.O_CLOEXEC
            fd = os.open(db_path, flags, 0o600)
            try:
                # os.open's mode only applies to new files, so also restrict
                # databases created by older versions.
                os.fchmod(fd, 0o600)
            finally:
                os.close(fd)

        conn = sqlite3.connect(self._db_path)
        conn.execute("""
            CREATE TABLE IF NOT EXISTS transactions (
                id TEXT PRIMARY KEY,
                date TEXT NOT NULL,
                amount REAL,
                merchant_name TEXT NOT NULL DEFAULT '',
                merchant_id TEXT NOT NULL DEFAULT '',
                category_id TEXT NOT NULL DEFAULT 'uncategorized',
                category_name TEXT NOT NULL DEFAULT 'Uncategorized',
                account_id TEXT NOT NULL DEFAULT '',
                account_name TEXT NOT NULL DEFAULT '',
                currency TEXT NOT NULL DEFAULT '',
                notes TEXT NOT NULL DEFAULT '',
                hideFromReports INTEGER NOT NULL DEFAULT 0,
                pending INTEGER NOT NULL DEFAULT 0,
                isRecurring INTEGER NOT NULL DEFAULT 0
            )
        """)
        conn.execute("""
            CREATE TABLE IF NOT EXISTS refresh_metadata (
                key TEXT PRIMARY KEY,
                value TEXT NOT NULL
            )
        """)
        conn.execute("""
            CREATE TABLE IF NOT EXISTS deleted_transactions (
                id TEXT PRIMARY KEY,
                deleted_at TEXT NOT NULL
            )
        """)

        transaction_columns = {
            row[1] for row in conn.execute("PRAGMA table_info(transactions)").fetchall()
        }
        if "currency" not in transaction_columns:
            conn.execute("ALTER TABLE transactions ADD COLUMN currency TEXT NOT NULL DEFAULT ''")

        conn.execute("CREATE INDEX IF NOT EXISTS idx_txn_date ON transactions(date)")
        conn.execute("CREATE INDEX IF NOT EXISTS idx_txn_pending ON transactions(pending)")
        conn.execute("CREATE INDEX IF NOT EXISTS idx_txn_hidden ON transactions(hideFromReports)")

        conn.commit()
        conn.close()
        self._db_initialized = True

    def _get_connection(self) -> sqlite3.Connection:
        """Get a SQLite connection, initialising the database if needed."""
        self._ensure_db_initialized()
        return sqlite3.connect(self._db_path)

    def _row_to_transaction_dict(self, row: sqlite3.Row) -> Dict[str, Any]:
        """Convert a SQLite row to the standard moneyflow transaction dict format."""
        return {
            "id": row["id"],
            "date": row["date"],
            "amount": row["amount"],
            "merchant": {
                "id": row["merchant_id"] or row["merchant_name"],
                "name": row["merchant_name"],
            },
            "category": {
                "id": row["category_id"],
                "name": row["category_name"],
            },
            "account": {
                "id": row["account_id"],
                "displayName": row["account_name"],
            },
            "currency": row["currency"],
            "notes": row["notes"],
            "hideFromReports": bool(row["hideFromReports"]),
            "pending": bool(row["pending"]),
            "isRecurring": bool(row["isRecurring"]),
        }

    def _get_refresh_metadata(self) -> Dict[str, str]:
        """Read all refresh_metadata rows as a dict."""
        conn = self._get_connection()
        cursor = conn.execute("SELECT key, value FROM refresh_metadata")
        result = dict(cursor.fetchall())
        conn.close()
        return result

    def _set_refresh_metadata(self, key: str, value: str) -> None:
        """Upsert a single refresh_metadata row."""
        conn = self._get_connection()
        conn.execute(
            "INSERT OR REPLACE INTO refresh_metadata (key, value) VALUES (?, ?)",
            (key, value),
        )
        conn.commit()
        conn.close()

    def _is_empty(self) -> bool:
        """Check whether the transactions table has any rows."""
        conn = self._get_connection()
        count = conn.execute("SELECT COUNT(*) FROM transactions").fetchone()[0]
        conn.close()
        return count == 0

    @staticmethod
    def _get_deleted_transaction_ids(conn: sqlite3.Connection) -> set[str]:
        """Return transaction IDs hidden by persistent local deletion tombstones."""
        rows = conn.execute("SELECT id FROM deleted_transactions").fetchall()
        return {row[0] for row in rows}

    def _migrate_legacy_transaction_ids(
        self,
        conn: sqlite3.Connection,
        transactions: List[Dict[str, Any]],
    ) -> set[str]:
        """Migrate ambiguous legacy IDs while preserving local edits and deletions."""
        deleted_ids = self._get_deleted_transaction_ids(conn)
        legacy_mappings: Dict[str, set[str]] = {}
        for transaction in transactions:
            legacy_id = transaction.get("legacy_id")
            if isinstance(legacy_id, str):
                legacy_mappings.setdefault(legacy_id, set()).add(str(transaction["id"]))

        if any(
            legacy_id in deleted_ids and len(new_ids) > 1
            for legacy_id, new_ids in legacy_mappings.items()
        ):
            raise RuntimeError(
                "SimpleFIN refresh found an ambiguous legacy deletion tombstone; "
                "data was not changed. Resolve the legacy tombstone before refreshing."
            )

        deleted_at = datetime.now(timezone.utc).isoformat()

        for transaction in transactions:
            new_id = str(transaction["id"])
            legacy_id = transaction.get("legacy_id")
            if not isinstance(legacy_id, str) or legacy_id == new_id:
                continue

            account_id = str(transaction.get("account", {}).get("id", ""))
            legacy_row = conn.execute(
                "SELECT account_id FROM transactions WHERE id = ?", (legacy_id,)
            ).fetchone()
            legacy_row_matches = legacy_row is not None and legacy_row[0] == account_id

            if legacy_id in deleted_ids or new_id in deleted_ids:
                conn.execute(
                    "INSERT OR IGNORE INTO deleted_transactions (id, deleted_at) VALUES (?, ?)",
                    (new_id, deleted_at),
                )
                deleted_ids.add(new_id)
                conn.execute("DELETE FROM transactions WHERE id = ?", (new_id,))
                if legacy_row_matches:
                    conn.execute("DELETE FROM transactions WHERE id = ?", (legacy_id,))
                continue

            if legacy_row_matches:
                # Prefer the legacy row because it can contain local merchant,
                # category, or visibility edits from before the ID upgrade.
                conn.execute("DELETE FROM transactions WHERE id = ?", (new_id,))
                conn.execute(
                    "UPDATE transactions SET id = ? WHERE id = ?",
                    (new_id, legacy_id),
                )

        return deleted_ids

    def _resolve_currency_code(
        self,
        transactions: List[Dict[str, Any]],
        *,
        include_existing: bool,
    ) -> Optional[str]:
        """Resolve one safe ISO currency code for storage and aggregation."""
        currencies = {
            currency
            for transaction in transactions
            if (currency := str(transaction.get("currency") or "").upper())
        }
        client_currency = getattr(self._client, "currency_code", None)
        if isinstance(client_currency, str) and client_currency:
            currencies.add(client_currency.upper())

        if include_existing:
            conn = self._get_connection()
            rows = conn.execute(
                "SELECT DISTINCT currency FROM transactions WHERE currency != ''"
            ).fetchall()
            conn.close()
            currencies.update(str(row[0]).upper() for row in rows)

        if len(currencies) > 1:
            raise RuntimeError(
                "SimpleFIN profile contains multiple currencies; "
                "moneyflow cannot aggregate them safely."
            )
        return next(iter(currencies), None)

    def _get_last_refresh_timestamp(self) -> Optional[datetime]:
        """Read the last successful refresh UTC timestamp, or None."""
        metadata = self._get_refresh_metadata()
        raw = metadata.get("last_refresh_timestamp")
        if not raw:
            return None
        try:
            dt = datetime.fromisoformat(raw)
            if dt.tzinfo is None:
                dt = dt.replace(tzinfo=timezone.utc)
            return dt
        except (ValueError, TypeError):
            return None

    def get_last_update_time(self) -> Optional[datetime]:
        """Return the last API refresh time as a naive local datetime.

        Reads the UTC ``last_refresh_timestamp`` from the SQLite metadata
        table and converts to the local timezone. Returns None if no
        refresh has ever occurred.
        """
        ts = self._get_last_refresh_timestamp()
        if ts is None:
            return None
        return ts.astimezone().replace(tzinfo=None)

    def is_refresh_stale(self, max_age_hours: int = 24) -> bool:
        """
        Check whether the last API refresh is older than *max_age_hours*.

        Returns True when the persisted ``last_refresh_timestamp`` is absent
        (never refreshed) or older than the given threshold — indicating the
        caller should schedule an API refresh.

        Args:
            max_age_hours: Staleness threshold in hours.

        Returns:
            True if a refresh should be attempted.
        """
        last_ts = self._get_last_refresh_timestamp()
        if last_ts is None:
            return True
        age = datetime.now(timezone.utc) - last_ts
        return age.total_seconds() > max_age_hours * 3600

    # ------------------------------------------------------------------
    # Public helpers
    # ------------------------------------------------------------------

    async def refresh(self) -> int:
        """
        Fetch transactions from the API and merge additively into SQLite.

        Fetches transactions since 2 weeks before the last refresh date (or
        3 years if no prior refresh). The 2-week lookback ensures that
        transactions which were pending during a previous refresh and have
        since transitioned to posted are re-fetched and captured, even if
        their original transaction date predates the last refresh boundary.
        Duplicates are ignored by INSERT OR IGNORE.

        Returns:
            Number of new transactions added.
        """
        if self._client is None:
            raise RuntimeError("SimpleFIN backend is not logged in. Call login() first.")

        metadata = self._get_refresh_metadata()
        last_refresh_end = metadata.get("last_refresh_end_date")

        end_date = (date.today() + timedelta(days=1)).isoformat()

        if last_refresh_end:
            last_refresh_date = date.fromisoformat(last_refresh_end)
            start_date = (last_refresh_date - timedelta(days=14)).isoformat()
        else:
            start_date = (date.today() - timedelta(days=365 * 3)).isoformat()

        transactions = await self._client.fetch_transactions(
            start_date=start_date,
            end_date=end_date,
        )
        currency_code = self._resolve_currency_code(transactions, include_existing=True)

        non_pending = [t for t in transactions if not t.get("pending", False)]

        conn = self._get_connection()
        deleted_ids = self._migrate_legacy_transaction_ids(conn, transactions)
        if currency_code:
            conn.execute(
                "UPDATE transactions SET currency = ? WHERE currency = ''",
                (currency_code,),
            )

        before = conn.execute("SELECT COUNT(*) FROM transactions").fetchone()[0]

        for txn in non_pending:
            if txn["id"] in deleted_ids:
                continue
            conn.execute(
                """INSERT OR IGNORE INTO transactions
                   (id, date, amount, merchant_name, merchant_id,
                    category_id, category_name, account_id, account_name,
                    currency, notes, hideFromReports, pending, isRecurring)
                   VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)""",
                (
                    txn["id"],
                    txn["date"],
                    txn.get("amount"),
                    txn.get("merchant", {}).get("name", ""),
                    txn.get("merchant", {}).get("id", ""),
                    txn.get("category", {}).get("id", "uncategorized"),
                    txn.get("category", {}).get("name", "Uncategorized"),
                    txn.get("account", {}).get("id", ""),
                    txn.get("account", {}).get("displayName", ""),
                    txn.get("currency") or currency_code or "",
                    txn.get("notes", ""),
                    1 if txn.get("hideFromReports") else 0,
                    1 if txn.get("pending") else 0,
                    1 if txn.get("isRecurring") else 0,
                ),
            )

        after = conn.execute("SELECT COUNT(*) FROM transactions").fetchone()[0]
        added = after - before

        conn.execute(
            "INSERT OR REPLACE INTO refresh_metadata (key, value) VALUES (?, ?)",
            ("last_refresh_end_date", date.today().isoformat()),
        )
        conn.execute(
            "INSERT OR REPLACE INTO refresh_metadata (key, value) VALUES (?, ?)",
            ("last_refresh_count", str(len(non_pending))),
        )
        conn.execute(
            "INSERT OR REPLACE INTO refresh_metadata (key, value) VALUES (?, ?)",
            ("last_refresh_timestamp", datetime.now(timezone.utc).isoformat()),
        )
        if currency_code:
            conn.execute(
                "INSERT OR REPLACE INTO refresh_metadata (key, value) VALUES (?, ?)",
                ("currency_code", currency_code),
            )
        conn.commit()
        conn.close()

        logger.info(
            "SimpleFIN refresh: %d new transactions (skipped %d pending)",
            added,
            len(transactions) - len(non_pending),
        )
        return added

    async def hard_refresh(self, lookback_days: int = 1095) -> int:
        """
        Replace local transaction rows with transactions from the API.

        This overwrites merchant, category-assignment, and hide edits. Local
        deletion tombstones remain in effect. Use refresh() for additive merge.

        Args:
            lookback_days: Number of days of history to fetch
                          (default 1095 = 3 years).

        Returns:
            Number of transactions inserted.
        """
        if self._client is None:
            raise RuntimeError("SimpleFIN backend is not logged in. Call login() first.")

        start_date = (date.today() - timedelta(days=lookback_days)).isoformat()
        end_date = (date.today() + timedelta(days=1)).isoformat()

        transactions = await self._client.fetch_transactions(
            start_date=start_date,
            end_date=end_date,
        )
        currency_code = self._resolve_currency_code(transactions, include_existing=False)

        non_pending = [t for t in transactions if not t.get("pending", False)]

        conn = self._get_connection()
        deleted_ids = self._migrate_legacy_transaction_ids(conn, transactions)
        conn.execute("DELETE FROM transactions")
        conn.execute("DELETE FROM refresh_metadata")
        inserted = 0

        for txn in non_pending:
            if txn["id"] in deleted_ids:
                continue
            conn.execute(
                """INSERT INTO transactions
                   (id, date, amount, merchant_name, merchant_id,
                    category_id, category_name, account_id, account_name,
                    currency, notes, hideFromReports, pending, isRecurring)
                   VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)""",
                (
                    txn["id"],
                    txn["date"],
                    txn.get("amount"),
                    txn.get("merchant", {}).get("name", ""),
                    txn.get("merchant", {}).get("id", ""),
                    txn.get("category", {}).get("id", "uncategorized"),
                    txn.get("category", {}).get("name", "Uncategorized"),
                    txn.get("account", {}).get("id", ""),
                    txn.get("account", {}).get("displayName", ""),
                    txn.get("currency") or currency_code or "",
                    txn.get("notes", ""),
                    1 if txn.get("hideFromReports") else 0,
                    1 if txn.get("pending") else 0,
                    1 if txn.get("isRecurring") else 0,
                ),
            )
            inserted += 1

        conn.execute(
            "INSERT OR REPLACE INTO refresh_metadata (key, value) VALUES (?, ?)",
            ("last_refresh_end_date", date.today().isoformat()),
        )
        conn.execute(
            "INSERT OR REPLACE INTO refresh_metadata (key, value) VALUES (?, ?)",
            ("last_refresh_count", str(inserted)),
        )
        conn.execute(
            "INSERT OR REPLACE INTO refresh_metadata (key, value) VALUES (?, ?)",
            ("last_refresh_timestamp", datetime.now(timezone.utc).isoformat()),
        )
        if currency_code:
            conn.execute(
                "INSERT OR REPLACE INTO refresh_metadata (key, value) VALUES (?, ?)",
                ("currency_code", currency_code),
            )
        conn.commit()
        conn.close()

        logger.info(
            "SimpleFIN hard refresh: %d transactions (lookback %d days, skipped %d pending)",
            inserted,
            lookback_days,
            len(transactions) - len(non_pending),
        )
        return inserted

    def get_database_stats(self) -> Dict[str, Any]:
        """
        Return statistics about the local SQLite store.

        Returns:
            Dict with keys: total_transactions, total_amount, earliest_date,
            latest_date, last_refresh_timestamp, last_refresh_count,
            currency_code.
        """
        conn = self._get_connection()

        total = conn.execute("SELECT COUNT(*) FROM transactions").fetchone()[0]

        row = conn.execute(
            "SELECT COALESCE(SUM(amount), 0.0) as total_amount, "
            "MIN(date) as earliest_date, MAX(date) as latest_date "
            "FROM transactions"
        ).fetchone()
        total_amount = row[0] if row[0] is not None else 0.0
        earliest_date = row[1]
        latest_date = row[2]

        metadata = self._get_refresh_metadata()
        last_refresh_timestamp = metadata.get("last_refresh_timestamp")
        last_refresh_count = metadata.get("last_refresh_count")
        currency_code = metadata.get("currency_code")

        conn.close()

        return {
            "total_transactions": total,
            "total_amount": total_amount,
            "earliest_date": earliest_date,
            "latest_date": latest_date,
            "last_refresh_timestamp": last_refresh_timestamp,
            "last_refresh_count": last_refresh_count,
            "currency_code": currency_code,
        }

    # ------------------------------------------------------------------
    # Authentication
    # ------------------------------------------------------------------

    async def login(
        self,
        email: Optional[str] = None,
        password: Optional[str] = None,
        use_saved_session: bool = True,
        save_session: bool = True,
        mfa_secret_key: Optional[str] = None,
    ) -> None:
        """
        Initialise the backend with the SimpleFIN Access URL.

        The Access URL must be supplied as the 'password' parameter.

        Args:
            email: Unused. SimpleFIN uses URL-embedded credentials.
            password: The SimpleFIN Access URL.
            use_saved_session: Unused.
            save_session: Unused.
            mfa_secret_key: Unused.

        Raises:
            ValueError: If password is absent or the Access URL is malformed.
        """
        if not password:
            raise ValueError(
                "SimpleFIN backend requires an Access URL. "
                "Store it in the password field via moneyflow-setup."
            )

        parse_access_url(password)
        self._client = SimpleFinClient(password)

    # ------------------------------------------------------------------
    # Transactions
    # ------------------------------------------------------------------

    async def get_transactions(
        self,
        limit: int = 100,
        offset: int = 0,
        start_date: Optional[str] = None,
        end_date: Optional[str] = None,
        **kwargs: Any,
    ) -> Dict[str, Any]:
        """
        Fetch transactions from the local SQLite store.

        If the store is empty (first run), this triggers an automatic refresh
        from the SimpleFIN API.

        DataManager calls this method twice — once with hidden_from_reports=False
        and once with hidden_from_reports=True. SimpleFIN returns all transactions
        for both passes; the second pass is handled by the SQLite filter.

        Args:
            limit: Page size.
            offset: Number of transactions to skip.
            start_date: ISO date filter (inclusive).
            end_date: ISO date filter (exclusive).
            **kwargs: 'hidden_from_reports' is handled.

        Returns:
            Dict in the standard format:
                {"allTransactions": {"results": [...], "totalCount": N}}
        """
        if self._client is None:
            raise RuntimeError("SimpleFIN backend is not logged in. Call login() first.")

        # Auto-refresh only before the first successful API refresh. A valid
        # refresh may store no rows when an account has no posted transactions.
        if self._get_last_refresh_timestamp() is None:
            await self.refresh()

        conn = self._get_connection()
        conn.row_factory = sqlite3.Row

        query = "SELECT * FROM transactions WHERE 1=1"
        params: List[Any] = []

        hidden_from_reports = kwargs.get("hidden_from_reports")
        if hidden_from_reports is True:
            query += " AND hideFromReports = 1"
        elif hidden_from_reports is False:
            query += " AND hideFromReports = 0"

        if start_date:
            query += " AND date >= ?"
            params.append(start_date)

        if end_date:
            query += " AND date < ?"
            params.append(end_date)

        count_query = query.replace("SELECT *", "SELECT COUNT(*)")
        total_count = conn.execute(count_query, params).fetchone()[0]

        query += " ORDER BY date DESC LIMIT ? OFFSET ?"
        params.extend([limit, offset])

        cursor = conn.execute(query, params)
        rows = cursor.fetchall()
        conn.close()

        transactions = [self._row_to_transaction_dict(r) for r in rows]

        return {
            "allTransactions": {
                "results": transactions,
                "totalCount": total_count,
            }
        }

    # ------------------------------------------------------------------
    # Categories (not supported by SimpleFIN)
    # ------------------------------------------------------------------

    async def get_transaction_categories(self) -> Dict[str, Any]:
        """Return an empty categories collection."""
        return {"categories": []}

    async def get_transaction_category_groups(self) -> Dict[str, Any]:
        """Return an empty category-groups collection."""
        return {"categoryGroups": []}

    # ------------------------------------------------------------------
    # Merchants
    # ------------------------------------------------------------------

    async def get_all_merchants(self) -> List[str]:
        """
        Return all unique merchant names sorted alphabetically.

        Reads from the local SQLite store. If the store is empty, triggers
        an automatic refresh.

        Returns:
            Sorted list of unique merchant name strings.
        """
        if self._client is None:
            raise RuntimeError("SimpleFIN backend is not logged in. Call login() first.")

        if self._get_last_refresh_timestamp() is None:
            await self.refresh()

        conn = self._get_connection()
        cursor = conn.execute(
            "SELECT DISTINCT merchant_name FROM transactions "
            "WHERE merchant_name != '' ORDER BY merchant_name"
        )
        merchants = [row[0] for row in cursor.fetchall()]
        conn.close()
        return merchants

    # ------------------------------------------------------------------
    # Write operations (local SQLite persistence)
    # ------------------------------------------------------------------

    async def update_transaction(
        self,
        transaction_id: str,
        merchant_name: Optional[str] = None,
        category_id: Optional[str] = None,
        hide_from_reports: Optional[bool] = None,
        category_name: Optional[str] = None,
        **kwargs: Any,
    ) -> Dict[str, Any]:
        """
        Update a transaction in the local SQLite store.

        Args:
            transaction_id: ID of the transaction to update.
            merchant_name: New merchant name (if changing).
            category_id: New category ID (if changing).
            hide_from_reports: New hidden status (if changing).
            category_name: New category display name (if changing category).
            **kwargs: Unused.

        Returns:
            Standard response dict.
        """
        conn = self._get_connection()
        updates: List[str] = []
        params: List[Any] = []

        if merchant_name is not None:
            updates.append("merchant_name = ?")
            params.append(merchant_name)
            updates.append("merchant_id = ?")
            params.append(merchant_name)

        if category_id is not None:
            updates.append("category_id = ?")
            params.append(category_id)

        if category_name is not None:
            updates.append("category_name = ?")
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
        Delete a transaction and persist a tombstone in the local SQLite store.

        Args:
            transaction_id: ID of the transaction to delete.

        Returns:
            True if a row was deleted.
        """
        conn = self._get_connection()
        cursor = conn.execute("DELETE FROM transactions WHERE id = ?", (transaction_id,))
        deleted = cursor.rowcount > 0
        if deleted:
            conn.execute(
                "INSERT OR REPLACE INTO deleted_transactions (id, deleted_at) VALUES (?, ?)",
                (transaction_id, datetime.now(timezone.utc).isoformat()),
            )
        conn.commit()
        conn.close()
        return deleted

    # ------------------------------------------------------------------
    # Auth lifecycle
    # ------------------------------------------------------------------

    def clear_auth(self) -> None:
        """Clear authentication state. The SQLite store is not affected."""
        self._client = None

    # ------------------------------------------------------------------
    # Metadata / capabilities
    # ------------------------------------------------------------------

    def get_backend_type(self) -> str:
        return "simplefin"

    def get_currency_symbol(self) -> str:
        """Return the profile's ISO currency code for unambiguous display."""
        return self._get_refresh_metadata().get("currency_code", "¤")

    @property
    def read_only(self) -> bool:
        """
        SimpleFIN API is read-only.

        This gates category/group manager features that do not apply to SimpleFIN.
        """
        return True

    @property
    def can_write_transactions(self) -> bool:
        """
        Edits can be persisted in the local SQLite store even though the API
        is read-only. The commit pipeline calls update_transaction() which
        writes to SQLite.
        """
        return True

    @property
    def supports_category_sync(self) -> bool:
        """SimpleFIN does not provide a category hierarchy."""
        return False
