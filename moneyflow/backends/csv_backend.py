"""Generic CSV-backed FinanceBackend for imported transaction data."""

import json
import os
import sqlite3
import stat as stat_module
from pathlib import Path
from typing import Any

from moneyflow.backends.base import FinanceBackend
from moneyflow.data.file_utils import (
    ensure_restrictive_directory,
    open_restrictive_file,
    require_current_user_ownership,
)

STANDARD_FIELDS = frozenset(
    {
        "id",
        "date",
        "amount",
        "merchant",
        "category",
        "category_id",
        "account",
        "notes",
        "hideFromReports",
        "pending",
        "isRecurring",
        "imported_at",
    }
)


def _stable_category_id(name: str) -> str:
    """Generate a stable category_id from a category name."""
    slug = "".join(c if c.isalnum() else "_" for c in name).rstrip("_")
    return f"cat_{slug}" if slug else "cat_uncategorized"


def _requires_posix_mode_checks() -> bool:
    """Return whether Unix ownership and mode checks are meaningful."""
    return os.name == "posix"


def _validate_path_security(db_path_str: str) -> None:
    """Validate that the database path and parent directory are secure.

    Checks are performed on every database connection to defend against
    symlink/time-of-check-to-time-of-use races on the path.
    """
    db_path = Path(db_path_str)
    parent = db_path.parent

    _validate_path_components(parent)

    if not parent.exists():
        raise OSError(f"Parent directory does not exist: {parent}")

    st_parent = os.lstat(parent)
    if not stat_module.S_ISDIR(st_parent.st_mode):
        raise OSError(f"Parent is not a directory: {parent}")
    if _requires_posix_mode_checks() and st_parent.st_uid != os.getuid():
        raise OSError(f"Parent directory not owned by current user: {parent}")
    if _requires_posix_mode_checks() and stat_module.S_IMODE(st_parent.st_mode) & 0o077:
        raise OSError(f"Parent directory is group/other writable: {parent}")

    # When the database file has not been created yet (e.g. first call to
    # _secure_open_db), only the parent directory can be validated. File-level
    # checks will run after creation.
    if not db_path.exists() and not db_path.is_symlink():
        return

    st_db = os.lstat(db_path)
    if stat_module.S_ISLNK(st_db.st_mode):
        raise OSError(f"Database path is a symlink: {db_path}")
    if not stat_module.S_ISREG(st_db.st_mode):
        raise OSError(f"Database is not a regular file: {db_path}")
    if _requires_posix_mode_checks() and st_db.st_uid != os.getuid():
        raise OSError(f"Database not owned by current user: {db_path}")
    if _requires_posix_mode_checks() and stat_module.S_IMODE(st_db.st_mode) != 0o600:
        raise OSError(f"Database permissions are not 0o600: {db_path}")

    real_db = os.path.realpath(db_path)
    real_parent = os.path.realpath(parent)
    if not Path(real_db).is_relative_to(real_parent):
        raise OSError(f"Database path escapes parent directory: {db_path}")


def _validate_path_components(path: Path) -> None:
    """Reject symlinked or unsafe directory components before opening a database."""
    absolute_path = Path(os.path.abspath(path))
    current = Path(absolute_path.anchor)

    for component in absolute_path.parts[1:]:
        current /= component
        st_component = os.lstat(current)
        mode = st_component.st_mode
        if stat_module.S_ISLNK(mode):
            raise OSError(f"Database path contains a symlinked component: {current}")
        if not stat_module.S_ISDIR(mode):
            raise OSError(f"Database path component is not a directory: {current}")
        if (
            _requires_posix_mode_checks()
            and stat_module.S_IMODE(mode) & 0o022
            and not mode & stat_module.S_ISVTX
        ):
            raise OSError(f"Database path component is group/other writable: {current}")


def _secure_open_db(db_path_str: str) -> None:
    """Create or verify the database file with symlink protection."""
    db_path = Path(db_path_str)
    # Create the parent with owner-only permissions using the platform-aware
    # helper (handles Windows DACLs in addition to POSIX mode bits).
    ensure_restrictive_directory(db_path.parent, parents=True)

    _validate_path_security(db_path_str)

    if _requires_posix_mode_checks():
        flags = os.O_RDWR | os.O_CREAT | os.O_NOFOLLOW
        if hasattr(os, "O_CLOEXEC"):
            flags |= os.O_CLOEXEC

        fd = os.open(db_path, flags, 0o600)
        try:
            st = os.fstat(fd)
            if not stat_module.S_ISREG(st.st_mode):
                raise OSError(f"Database path is not a regular file: {db_path}")
            os.fchmod(fd, 0o600)
        finally:
            os.close(fd)
    else:
        # Windows: os.O_NOFOLLOW / os.fchmod are unavailable or ineffective.
        # Use the platform-aware helper that sets an owner-only DACL at
        # creation time (and handles the case where the file already exists
        # by reopening it). Owner-only Windows permissions matter because
        # mode bits are not honored — DACLs are the only effective control.
        if db_path.exists() or db_path.is_symlink():
            fd = os.open(db_path, os.O_RDWR)
            try:
                require_current_user_ownership(db_path)
                st = os.fstat(fd)
                if not stat_module.S_ISREG(st.st_mode):
                    raise OSError(f"Database path is not a regular file: {db_path}")
            finally:
                os.close(fd)
        else:
            fd = open_restrictive_file(db_path, read_write=True)
            try:
                st = os.fstat(fd)
                if not stat_module.S_ISREG(st.st_mode):
                    raise OSError(f"Database path is not a regular file: {db_path}")
            finally:
                os.close(fd)

    # Re-validate after the secure create/verify to catch any race.
    _validate_path_security(db_path_str)


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

        _secure_open_db(self.db_path)

        _validate_path_security(self.db_path)
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
                file_size INTEGER NOT NULL DEFAULT 0,
                file_hash TEXT NOT NULL DEFAULT '',
                record_count INTEGER NOT NULL,
                duplicate_count INTEGER NOT NULL,
                skipped_count INTEGER NOT NULL DEFAULT 0,
                import_date TIMESTAMP DEFAULT CURRENT_TIMESTAMP
            )
        """)

        # Migrate existing import_history tables that lack file_size or file_hash
        cols = {row[1] for row in conn.execute("PRAGMA table_info(import_history)")}
        if "file_size" not in cols:
            conn.execute(
                "ALTER TABLE import_history ADD COLUMN file_size INTEGER NOT NULL DEFAULT 0"
            )
        if "file_hash" not in cols:
            conn.execute("ALTER TABLE import_history ADD COLUMN file_hash TEXT NOT NULL DEFAULT ''")

        conn.execute("CREATE INDEX IF NOT EXISTS idx_csv_date ON transactions(date)")
        conn.execute("CREATE INDEX IF NOT EXISTS idx_csv_merchant ON transactions(merchant)")
        conn.execute("CREATE INDEX IF NOT EXISTS idx_csv_category ON transactions(category)")
        conn.commit()
        conn.close()
        self._db_initialized = True

    def _get_connection(self) -> sqlite3.Connection:
        self._ensure_db_initialized()
        _validate_path_security(self.db_path)
        return sqlite3.connect(self.db_path)

    def _row_to_transaction_dict(self, row: sqlite3.Row) -> dict[str, Any]:
        extras = json.loads(row["extras"] or "{}")
        safe_extras = {k: v for k, v in extras.items() if k not in STANDARD_FIELDS}
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
            **safe_extras,
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

        hidden = kwargs.get("hidden_from_reports")
        if hidden is not None:
            conditions.append("hideFromReports = ?")
            params.append(1 if hidden else 0)

        where_clause = " AND ".join(conditions) if conditions else "1=1"
        count_query = f"SELECT COUNT(*) FROM transactions WHERE {where_clause}"
        total = conn.execute(count_query, params).fetchone()[0]

        query = (
            f"SELECT * FROM transactions WHERE {where_clause} ORDER BY date DESC LIMIT ? OFFSET ?"
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
        category_name: str | None = None,
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
        if category_name is not None or kwargs.get("category_name"):
            name = category_name or kwargs.get("category_name", "")
            updates.append("category = ?")
            params.append(name)
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

        row = conn.execute("SELECT * FROM transactions WHERE id = ?", (transaction_id,)).fetchone()
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
        rows = conn.execute("SELECT DISTINCT category_id, category FROM transactions").fetchall()
        conn.close()
        categories = [
            {"id": row[0], "name": row[1], "group": {"id": "", "type": "expense"}} for row in rows
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
        rows = conn.execute("SELECT * FROM import_history ORDER BY import_date DESC").fetchall()
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
            conn.execute("SELECT COALESCE(SUM(amount), 0) FROM transactions").fetchone()[0] or 0.0
        )
        conn.close()
        return {
            "total_transactions": total,
            "total_amount": total_amount,
            "earliest_date": date_range["earliest"],
            "latest_date": date_range["latest"],
        }
