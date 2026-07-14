"""
Unit tests for moneyflow.backends.simplefin.SimpleFinBackend.

All SimpleFinClient calls are mocked — no real network access occurs.
Tests use an in-memory SQLite database to avoid filesystem side effects.
"""

import os
import sqlite3
import stat
from datetime import date, datetime, timedelta, timezone
from unittest.mock import AsyncMock, MagicMock

import pytest

from moneyflow.backends.simplefin import SimpleFinBackend

# ---------------------------------------------------------------------------
# Fixtures
# ---------------------------------------------------------------------------

VALID_ACCESS_URL = "https://testuser:testpass@bridge.simplefin.org/simplefin"

SAMPLE_TRANSACTIONS = [
    {
        "id": "acct-1:txn-1",
        "date": "2024-03-15",
        "amount": -42.50,
        "merchant": {"id": "Grocery Store", "name": "Grocery Store"},
        "category": {"id": "uncategorized", "name": "Uncategorized"},
        "account": {"id": "acct-1", "displayName": "Checking"},
        "currency": "USD",
        "notes": "",
        "hideFromReports": False,
        "pending": False,
        "isRecurring": False,
    },
    {
        "id": "acct-1:txn-2",
        "date": "2024-03-14",
        "amount": -12.00,
        "merchant": {"id": "Coffee Shop", "name": "Coffee Shop"},
        "category": {"id": "uncategorized", "name": "Uncategorized"},
        "account": {"id": "acct-1", "displayName": "Checking"},
        "currency": "USD",
        "notes": "",
        "hideFromReports": False,
        "pending": False,
        "isRecurring": False,
    },
    {
        "id": "acct-2:txn-3",
        "date": "2024-03-13",
        "amount": 2500.00,
        "merchant": {"id": "Payroll", "name": "Payroll"},
        "category": {"id": "uncategorized", "name": "Uncategorized"},
        "account": {"id": "acct-2", "displayName": "Savings"},
        "currency": "USD",
        "notes": "",
        "hideFromReports": False,
        "pending": False,
        "isRecurring": False,
    },
]

LEGACY_COLON_ID = "acct:1:txn"
ENCODED_COLON_ID = "v2:6:acct:1txn"
COLON_TRANSACTION = {
    "id": ENCODED_COLON_ID,
    "legacy_id": LEGACY_COLON_ID,
    "date": "2024-03-15",
    "amount": -42.50,
    "merchant": {"id": "Example Merchant", "name": "Example Merchant"},
    "category": {"id": "uncategorized", "name": "Uncategorized"},
    "account": {"id": "acct:1", "displayName": "Checking"},
    "currency": "USD",
    "notes": "",
    "hideFromReports": False,
    "pending": False,
    "isRecurring": False,
}
COLLIDING_COLON_TRANSACTION = {
    **COLON_TRANSACTION,
    "id": "v2:4:acct1:txn",
    "account": {"id": "acct", "displayName": "Savings"},
}


@pytest.fixture()
def backend(tmp_path):
    db_path = str(tmp_path / "test.db")
    return SimpleFinBackend(db_path=db_path)


@pytest.fixture()
def logged_in_backend(tmp_path):
    """Backend with a mocked client already injected."""
    db_path = str(tmp_path / "test.db")
    b = SimpleFinBackend(db_path=db_path)
    mock_client = MagicMock()
    mock_client.fetch_transactions = AsyncMock(return_value=SAMPLE_TRANSACTIONS)
    mock_client.currency_code = "USD"
    b._client = mock_client
    return b, mock_client


# ---------------------------------------------------------------------------
# Metadata / capabilities
# ---------------------------------------------------------------------------


class TestMetadata:
    def test_get_backend_type(self, backend):
        assert backend.get_backend_type() == "simplefin"

    def test_supports_category_sync_is_false(self, backend):
        assert backend.supports_category_sync is False

    def test_read_only_is_true(self, backend):
        assert backend.read_only is True

    def test_can_write_transactions_is_true(self, backend):
        assert backend.can_write_transactions is True


# ---------------------------------------------------------------------------
# login()
# ---------------------------------------------------------------------------


class TestLogin:
    @pytest.mark.asyncio
    async def test_login_with_valid_url_succeeds(self, backend):
        await backend.login(password=VALID_ACCESS_URL)
        assert backend._client is not None

    @pytest.mark.asyncio
    async def test_login_without_password_raises(self, backend):
        with pytest.raises(ValueError, match="requires an Access URL"):
            await backend.login()

    @pytest.mark.asyncio
    async def test_login_with_none_password_raises(self, backend):
        with pytest.raises(ValueError, match="requires an Access URL"):
            await backend.login(password=None)

    @pytest.mark.asyncio
    async def test_login_with_malformed_url_raises(self, backend):
        with pytest.raises(ValueError):
            await backend.login(password="not-a-valid-url")

    @pytest.mark.asyncio
    async def test_login_email_and_mfa_are_ignored(self, backend):
        await backend.login(
            email="anyone@example.com",
            password=VALID_ACCESS_URL,
            mfa_secret_key="TOTP_SECRET",
        )
        assert backend._client is not None


# ---------------------------------------------------------------------------
# get_transaction_categories() / get_transaction_category_groups()
# ---------------------------------------------------------------------------


class TestCategories:
    @pytest.mark.asyncio
    async def test_get_transaction_categories_returns_empty(self, backend):
        result = await backend.get_transaction_categories()
        assert result == {"categories": []}

    @pytest.mark.asyncio
    async def test_get_transaction_category_groups_returns_empty(self, backend):
        result = await backend.get_transaction_category_groups()
        assert result == {"categoryGroups": []}


# ---------------------------------------------------------------------------
# get_transactions()
# ---------------------------------------------------------------------------


class TestGetTransactions:
    @pytest.mark.asyncio
    async def test_returns_all_transactions_wrapper_format(self, logged_in_backend):
        b, mock_client = logged_in_backend
        result = await b.get_transactions(limit=100, offset=0)

        assert "allTransactions" in result
        assert "results" in result["allTransactions"]
        assert "totalCount" in result["allTransactions"]
        assert result["allTransactions"]["totalCount"] == 3
        assert len(result["allTransactions"]["results"]) == 3

    @pytest.mark.asyncio
    async def test_hidden_from_reports_true_returns_empty(self, logged_in_backend):
        b, mock_client = logged_in_backend
        result = await b.get_transactions(hidden_from_reports=True)

        assert result["allTransactions"]["results"] == []
        assert result["allTransactions"]["totalCount"] == 0

    @pytest.mark.asyncio
    async def test_pagination_offset_slices_correctly(self, logged_in_backend):
        b, mock_client = logged_in_backend
        result = await b.get_transactions(limit=2, offset=0)
        assert len(result["allTransactions"]["results"]) == 2

        result2 = await b.get_transactions(limit=2, offset=2)
        assert len(result2["allTransactions"]["results"]) == 1

    @pytest.mark.asyncio
    async def test_same_data_served_on_multiple_calls(self, logged_in_backend):
        """SQLite store serves data without re-fetching from API."""
        b, mock_client = logged_in_backend
        id_sets = []

        for offset in range(0, 3, 1):
            result = await b.get_transactions(limit=1, offset=offset)
            ids = {t["id"] for t in result["allTransactions"]["results"]}
            id_sets.append(ids)

        # API should only be hit once (initial populate), not per pagination call
        mock_client.fetch_transactions.assert_called_once()

        # All three transactions should be returned across pagination calls
        all_ids = set().union(*id_sets)
        assert all_ids == {"acct-1:txn-1", "acct-1:txn-2", "acct-2:txn-3"}

    @pytest.mark.asyncio
    async def test_successful_empty_refresh_is_not_repeated(self, tmp_path):
        """A successful refresh with no posted transactions initializes the store."""
        b = SimpleFinBackend(db_path=str(tmp_path / "test.db"))
        mock_client = MagicMock()
        mock_client.fetch_transactions = AsyncMock(return_value=[])
        b._client = mock_client

        await b.get_transactions(hidden_from_reports=False)
        await b.get_transactions(hidden_from_reports=True)

        mock_client.fetch_transactions.assert_called_once()

    @pytest.mark.asyncio
    async def test_second_page_past_end_returns_empty_results(self, logged_in_backend):
        b, mock_client = logged_in_backend
        result = await b.get_transactions(limit=100, offset=1000)
        assert result["allTransactions"]["results"] == []
        assert result["allTransactions"]["totalCount"] == 3

    @pytest.mark.asyncio
    async def test_not_logged_in_raises_runtime_error(self, backend):
        with pytest.raises(RuntimeError, match="not logged in"):
            await backend.get_transactions()

    @pytest.mark.asyncio
    async def test_date_filters_work(self, logged_in_backend):
        b, _ = logged_in_backend
        result = await b.get_transactions(start_date="2024-03-14")
        ids = {t["id"] for t in result["allTransactions"]["results"]}
        assert ids == {"acct-1:txn-1", "acct-1:txn-2"}

    @pytest.mark.asyncio
    async def test_pending_transactions_are_filtered_from_store(self, tmp_path):
        """Pending transactions returned by the API are NOT stored in SQLite."""
        b = SimpleFinBackend(db_path=str(tmp_path / "test.db"))
        mock_client = MagicMock()
        transactions_with_pending = SAMPLE_TRANSACTIONS + [
            {
                "id": "txn-pending",
                "date": "2024-03-16",
                "amount": -99.00,
                "merchant": {"id": "Pending Charge", "name": "Pending Charge"},
                "category": {"id": "uncategorized", "name": "Uncategorized"},
                "account": {"id": "acct-1", "displayName": "Checking"},
                "notes": "",
                "hideFromReports": False,
                "pending": True,
                "isRecurring": False,
            },
        ]
        mock_client.fetch_transactions = AsyncMock(return_value=transactions_with_pending)
        b._client = mock_client

        await b.refresh()

        # Pending transaction should not be in the store
        result = await b.get_transactions(limit=100, offset=0)
        ids = {t["id"] for t in result["allTransactions"]["results"]}
        assert "txn-pending" not in ids
        assert len(result["allTransactions"]["results"]) == 3


# ---------------------------------------------------------------------------
# refresh()
# ---------------------------------------------------------------------------


class TestRefresh:
    @pytest.mark.asyncio
    async def test_refresh_adds_new_transactions(self, tmp_path):
        b = SimpleFinBackend(db_path=str(tmp_path / "test.db"))
        mock_client = MagicMock()
        mock_client.fetch_transactions = AsyncMock(return_value=SAMPLE_TRANSACTIONS)
        b._client = mock_client

        added = await b.refresh()
        assert added == 3

        result = await b.get_transactions(limit=100, offset=0)
        assert result["allTransactions"]["totalCount"] == 3

    @pytest.mark.asyncio
    async def test_refresh_persists_currency_and_uses_iso_code_for_display(self, tmp_path):
        transactions = [{**transaction, "currency": "EUR"} for transaction in SAMPLE_TRANSACTIONS]
        backend = SimpleFinBackend(db_path=str(tmp_path / "test.db"))
        mock_client = MagicMock()
        mock_client.fetch_transactions = AsyncMock(return_value=transactions)
        mock_client.currency_code = "EUR"
        backend._client = mock_client

        await backend.refresh()

        result = await backend.get_transactions(limit=100, offset=0)
        assert {
            transaction["currency"] for transaction in result["allTransactions"]["results"]
        } == {"EUR"}
        assert backend.get_currency_symbol() == "EUR"

    @pytest.mark.asyncio
    async def test_refresh_backfills_currency_for_existing_rows(self, tmp_path):
        backend = SimpleFinBackend(db_path=str(tmp_path / "test.db"))
        backend._ensure_db_initialized()
        conn = sqlite3.connect(backend._db_path)
        conn.execute(
            "INSERT INTO transactions (id, date, merchant_name) VALUES (?, ?, ?)",
            ("legacy-transaction", "2024-01-01", "Example Merchant"),
        )
        conn.commit()
        conn.close()

        mock_client = MagicMock()
        mock_client.fetch_transactions = AsyncMock(return_value=[])
        mock_client.currency_code = "USD"
        backend._client = mock_client

        await backend.refresh()

        conn = sqlite3.connect(backend._db_path)
        currency = conn.execute(
            "SELECT currency FROM transactions WHERE id = ?", ("legacy-transaction",)
        ).fetchone()[0]
        conn.close()
        assert currency == "USD"

    @pytest.mark.asyncio
    async def test_refresh_idempotent(self, logged_in_backend):
        """Repeated refresh does not duplicate transactions."""
        b, mock_client = logged_in_backend

        await b.refresh()
        await b.refresh()

        result = await b.get_transactions(limit=100, offset=0)
        assert result["allTransactions"]["totalCount"] == 3

    @pytest.mark.asyncio
    async def test_refresh_migrates_legacy_colon_id_without_losing_local_edits(self, tmp_path):
        backend = SimpleFinBackend(db_path=str(tmp_path / "test.db"))
        backend._ensure_db_initialized()
        conn = sqlite3.connect(backend._db_path)
        conn.execute(
            """INSERT INTO transactions
               (id, date, merchant_name, account_id, currency)
               VALUES (?, ?, ?, ?, ?)""",
            (LEGACY_COLON_ID, "2024-03-15", "Locally Edited", "acct:1", "USD"),
        )
        conn.commit()
        conn.close()
        mock_client = MagicMock()
        mock_client.fetch_transactions = AsyncMock(return_value=[COLON_TRANSACTION])
        mock_client.currency_code = "USD"
        backend._client = mock_client

        assert await backend.refresh() == 0

        result = await backend.get_transactions(limit=100, offset=0)
        assert result["allTransactions"]["totalCount"] == 1
        transaction = result["allTransactions"]["results"][0]
        assert transaction["id"] == ENCODED_COLON_ID
        assert transaction["merchant"]["name"] == "Locally Edited"

    @pytest.mark.asyncio
    @pytest.mark.parametrize("method_name", ["refresh", "hard_refresh"])
    async def test_fetch_error_preserves_existing_data_and_metadata(
        self, logged_in_backend, method_name
    ):
        backend, mock_client = logged_in_backend
        await backend.refresh()
        before_stats = backend.get_database_stats()
        mock_client.fetch_transactions = AsyncMock(
            side_effect=RuntimeError("SimpleFIN response is missing required transaction ID")
        )

        with pytest.raises(RuntimeError, match="missing required transaction ID"):
            await getattr(backend, method_name)()

        assert backend.get_database_stats() == before_stats

    @pytest.mark.asyncio
    async def test_refresh_not_logged_in_raises(self, backend):
        with pytest.raises(RuntimeError, match="not logged in"):
            await backend.refresh()

    @pytest.mark.asyncio
    async def test_refresh_uses_two_week_lookback(self, tmp_path):
        """refresh fetches from 2 weeks before last_refresh_end_date."""
        b = SimpleFinBackend(db_path=str(tmp_path / "test.db"))
        mock_client = MagicMock()
        mock_client.fetch_transactions = AsyncMock(return_value=[])
        b._client = mock_client

        # Seed metadata with a known last_refresh_end_date
        b._ensure_db_initialized()
        conn = sqlite3.connect(b._db_path)
        conn.execute(
            "INSERT OR REPLACE INTO refresh_metadata (key, value) VALUES (?, ?)",
            ("last_refresh_end_date", "2024-03-15"),
        )
        conn.commit()
        conn.close()

        await b.refresh()

        expected_start = (date.fromisoformat("2024-03-15") - timedelta(days=14)).isoformat()
        mock_client.fetch_transactions.assert_called_once()
        call_kwargs = mock_client.fetch_transactions.call_args[1]
        assert call_kwargs["start_date"] == expected_start
        assert call_kwargs["end_date"] == (date.today() + timedelta(days=1)).isoformat()


# ---------------------------------------------------------------------------
# hard_refresh()
# ---------------------------------------------------------------------------


class TestHardRefresh:
    @pytest.mark.asyncio
    async def test_hard_refresh_replaces_all_data(self, logged_in_backend):
        """Old data is removed, new data from API replaces it entirely."""
        b, mock_client = logged_in_backend
        await b.get_transactions(limit=100, offset=0)  # populate with SAMPLE_TRANSACTIONS (3 txns)

        new_txns = [
            {
                "id": "txn-5",
                "date": "2024-06-01",
                "amount": -50.00,
                "merchant": {"id": "New Store", "name": "New Store"},
                "category": {"id": "shopping", "name": "Shopping"},
                "account": {"id": "acct-1", "displayName": "Checking"},
                "notes": "",
                "hideFromReports": False,
                "pending": False,
                "isRecurring": False,
            },
            {
                "id": "txn-6",
                "date": "2024-06-02",
                "amount": -25.00,
                "merchant": {"id": "Pharmacy", "name": "Pharmacy"},
                "category": {"id": "health", "name": "Health"},
                "account": {"id": "acct-2", "displayName": "Savings"},
                "notes": "",
                "hideFromReports": False,
                "pending": False,
                "isRecurring": False,
            },
        ]
        mock_client.fetch_transactions = AsyncMock(return_value=new_txns)

        count = await b.hard_refresh(lookback_days=90)
        assert count == 2

        result = await b.get_transactions(limit=100, offset=0)
        assert result["allTransactions"]["totalCount"] == 2
        ids = {t["id"] for t in result["allTransactions"]["results"]}
        assert "acct-1:txn-1" not in ids
        assert "txn-5" in ids

    @pytest.mark.asyncio
    async def test_hard_refresh_not_logged_in_raises(self, backend):
        with pytest.raises(RuntimeError, match="not logged in"):
            await backend.hard_refresh()

    @pytest.mark.asyncio
    async def test_hard_refresh_respects_lookback_days(self, logged_in_backend):
        """fetch_transactions is called with the correct start_date."""
        b, mock_client = logged_in_backend
        await b.get_transactions(limit=100, offset=0)
        mock_client.fetch_transactions = AsyncMock(return_value=[])

        await b.hard_refresh(lookback_days=30)

        expected_start = (date.today() - timedelta(days=30)).isoformat()
        mock_client.fetch_transactions.assert_called_once()
        call_kwargs = mock_client.fetch_transactions.call_args[1]
        assert call_kwargs["start_date"] == expected_start
        assert call_kwargs["end_date"] == (date.today() + timedelta(days=1)).isoformat()

    @pytest.mark.asyncio
    @pytest.mark.parametrize("method_name", ["refresh", "hard_refresh"])
    async def test_refreshes_migrate_legacy_colon_tombstone(self, tmp_path, method_name):
        backend = SimpleFinBackend(db_path=str(tmp_path / "test.db"))
        backend._ensure_db_initialized()
        conn = sqlite3.connect(backend._db_path)
        conn.execute(
            "INSERT INTO deleted_transactions (id, deleted_at) VALUES (?, ?)",
            (LEGACY_COLON_ID, "2024-03-15T00:00:00+00:00"),
        )
        conn.commit()
        conn.close()
        mock_client = MagicMock()
        mock_client.fetch_transactions = AsyncMock(return_value=[COLON_TRANSACTION])
        mock_client.currency_code = "USD"
        backend._client = mock_client

        assert await getattr(backend, method_name)() == 0

        conn = sqlite3.connect(backend._db_path)
        transaction_count = conn.execute("SELECT COUNT(*) FROM transactions").fetchone()[0]
        tombstones = {
            row[0] for row in conn.execute("SELECT id FROM deleted_transactions").fetchall()
        }
        conn.close()
        assert transaction_count == 0
        assert tombstones == {LEGACY_COLON_ID, ENCODED_COLON_ID}

    @pytest.mark.asyncio
    @pytest.mark.parametrize("method_name", ["refresh", "hard_refresh"])
    async def test_refreshes_reject_ambiguous_legacy_tombstone(self, tmp_path, method_name):
        backend = SimpleFinBackend(db_path=str(tmp_path / "test.db"))
        backend._ensure_db_initialized()
        conn = sqlite3.connect(backend._db_path)
        conn.execute(
            "INSERT INTO deleted_transactions (id, deleted_at) VALUES (?, ?)",
            (LEGACY_COLON_ID, "2024-03-15T00:00:00+00:00"),
        )
        conn.commit()
        conn.close()
        mock_client = MagicMock()
        mock_client.fetch_transactions = AsyncMock(
            return_value=[COLON_TRANSACTION, COLLIDING_COLON_TRANSACTION]
        )
        mock_client.currency_code = "USD"
        backend._client = mock_client

        with pytest.raises(RuntimeError, match="ambiguous legacy deletion tombstone"):
            await getattr(backend, method_name)()

        assert backend.get_database_stats()["total_transactions"] == 0
        conn = sqlite3.connect(backend._db_path)
        tombstones = {
            row[0] for row in conn.execute("SELECT id FROM deleted_transactions").fetchall()
        }
        conn.close()
        assert tombstones == {LEGACY_COLON_ID}


# ---------------------------------------------------------------------------
# Refresh staleness (last_refresh_timestamp / is_refresh_stale)
# ---------------------------------------------------------------------------


class TestRefreshStaleness:
    def test_get_last_refresh_timestamp_returns_none_when_empty(self, backend):
        """No refresh metadata → returns None (stale)."""
        assert backend._get_last_refresh_timestamp() is None

    def test_get_last_refresh_timestamp_returns_datetime_when_set(self, backend):
        """Valid UTC timestamp in metadata → returns aware datetime."""
        backend._ensure_db_initialized()
        conn = sqlite3.connect(backend._db_path)
        conn.execute(
            "INSERT OR REPLACE INTO refresh_metadata (key, value) VALUES (?, ?)",
            ("last_refresh_timestamp", "2026-06-01T12:00:00+00:00"),
        )
        conn.commit()
        conn.close()

        dt = backend._get_last_refresh_timestamp()
        assert dt is not None
        assert dt.tzinfo is not None
        assert dt.year == 2026
        assert dt.month == 6

    def test_get_last_refresh_timestamp_handles_naive_fallback(self, backend):
        """A naive datetime string is treated as UTC."""
        backend._ensure_db_initialized()
        conn = sqlite3.connect(backend._db_path)
        conn.execute(
            "INSERT OR REPLACE INTO refresh_metadata (key, value) VALUES (?, ?)",
            ("last_refresh_timestamp", "2026-06-01T12:00:00"),
        )
        conn.commit()
        conn.close()

        dt = backend._get_last_refresh_timestamp()
        assert dt is not None
        assert dt.tzinfo is not None
        assert dt.tzinfo.utcoffset(dt) == timezone.utc.utcoffset(dt)

    def test_get_last_refresh_timestamp_returns_none_on_garbage(self, backend):
        """Corrupt timestamp string does not crash — returns None."""
        backend._ensure_db_initialized()
        conn = sqlite3.connect(backend._db_path)
        conn.execute(
            "INSERT OR REPLACE INTO refresh_metadata (key, value) VALUES (?, ?)",
            ("last_refresh_timestamp", "not-a-timestamp"),
        )
        conn.commit()
        conn.close()

        assert backend._get_last_refresh_timestamp() is None

    def test_is_refresh_stale_returns_true_when_no_timestamp(self, backend):
        """Never refreshed → stale."""
        assert backend.is_refresh_stale() is True

    def test_is_refresh_stale_returns_false_when_recent(self, backend):
        """Refreshed 1 hour ago → not stale (default 24h threshold)."""
        backend._ensure_db_initialized()
        recent = (datetime.now(timezone.utc) - timedelta(hours=1)).isoformat()
        conn = sqlite3.connect(backend._db_path)
        conn.execute(
            "INSERT OR REPLACE INTO refresh_metadata (key, value) VALUES (?, ?)",
            ("last_refresh_timestamp", recent),
        )
        conn.commit()
        conn.close()

        assert backend.is_refresh_stale() is False

    def test_is_refresh_stale_returns_true_when_old(self, backend):
        """Refreshed 48 hours ago → stale with default 24h threshold."""
        backend._ensure_db_initialized()
        old = (datetime.now(timezone.utc) - timedelta(hours=48)).isoformat()
        conn = sqlite3.connect(backend._db_path)
        conn.execute(
            "INSERT OR REPLACE INTO refresh_metadata (key, value) VALUES (?, ?)",
            ("last_refresh_timestamp", old),
        )
        conn.commit()
        conn.close()

        assert backend.is_refresh_stale() is True

    def test_is_refresh_stale_respects_custom_threshold(self, backend):
        """Custom 72h threshold: 48h old is not stale."""
        backend._ensure_db_initialized()
        old = (datetime.now(timezone.utc) - timedelta(hours=48)).isoformat()
        conn = sqlite3.connect(backend._db_path)
        conn.execute(
            "INSERT OR REPLACE INTO refresh_metadata (key, value) VALUES (?, ?)",
            ("last_refresh_timestamp", old),
        )
        conn.commit()
        conn.close()

        assert backend.is_refresh_stale(max_age_hours=72) is False

    @pytest.mark.asyncio
    async def test_refresh_stores_timestamp(self, logged_in_backend):
        """refresh() writes a last_refresh_timestamp."""
        b, _ = logged_in_backend
        await b.refresh()

        ts = b._get_last_refresh_timestamp()
        assert ts is not None
        assert ts.tzinfo is not None

    @pytest.mark.asyncio
    async def test_hard_refresh_stores_timestamp(self, logged_in_backend):
        """hard_refresh() writes a last_refresh_timestamp."""
        b, mock_client = logged_in_backend
        await b.hard_refresh(lookback_days=90)

        ts = b._get_last_refresh_timestamp()
        assert ts is not None
        assert ts.tzinfo is not None

    @pytest.mark.asyncio
    async def test_timestamp_survives_new_instance(self, tmp_path):
        """Timestamp written by one backend can be read by another."""
        db_path = str(tmp_path / "test.db")
        b1 = SimpleFinBackend(db_path=db_path)
        mock_client = MagicMock()
        mock_client.fetch_transactions = AsyncMock(return_value=SAMPLE_TRANSACTIONS)
        b1._client = mock_client
        await b1.refresh()

        del b1

        b2 = SimpleFinBackend(db_path=db_path)
        ts = b2._get_last_refresh_timestamp()
        assert ts is not None
        assert ts.tzinfo is not None

    @pytest.mark.asyncio
    async def test_refresh_updates_timestamp(self, logged_in_backend):
        """A second refresh updates the timestamp."""
        b, mock_client = logged_in_backend

        await b.refresh()
        ts1 = b._get_last_refresh_timestamp()

        await b.refresh()
        ts2 = b._get_last_refresh_timestamp()

        assert ts2 is not None
        assert ts2 > ts1  # Second timestamp is newer

    @pytest.mark.asyncio
    async def test_hard_refresh_updates_timestamp(self, logged_in_backend):
        """A second hard_refresh updates the timestamp."""
        b, mock_client = logged_in_backend

        await b.hard_refresh(lookback_days=90)
        ts1 = b._get_last_refresh_timestamp()

        await b.hard_refresh(lookback_days=90)
        ts2 = b._get_last_refresh_timestamp()

        assert ts2 is not None
        assert ts2 > ts1  # Second timestamp is newer


class TestGetLastUpdateTime:
    """Tests for get_last_update_time() public method."""

    def test_get_last_update_time_returns_none_when_no_refresh(self, backend):
        """No refresh metadata — returns None."""
        assert backend.get_last_update_time() is None

    def test_get_last_update_time_returns_local_datetime(self, backend):
        """Returns a naive local datetime when timestamp is present."""
        backend._ensure_db_initialized()
        conn = sqlite3.connect(backend._db_path)
        conn.execute(
            "INSERT OR REPLACE INTO refresh_metadata (key, value) VALUES (?, ?)",
            ("last_refresh_timestamp", "2026-06-01T12:00:00+00:00"),
        )
        conn.commit()
        conn.close()

        dt = backend.get_last_update_time()
        assert dt is not None
        assert dt.tzinfo is None  # naive local time
        assert dt.year == 2026
        assert dt.month == 6

    @pytest.mark.asyncio
    async def test_get_last_update_time_after_refresh(self, logged_in_backend):
        """Returns a value after refresh() completes."""
        b, _ = logged_in_backend
        await b.refresh()

        dt = b.get_last_update_time()
        assert dt is not None
        assert dt.tzinfo is None  # naive local time

    @pytest.mark.asyncio
    async def test_get_last_update_time_after_hard_refresh(self, logged_in_backend):
        """Returns a value after hard_refresh() completes."""
        b, _ = logged_in_backend
        await b.hard_refresh(lookback_days=90)

        dt = b.get_last_update_time()
        assert dt is not None
        assert dt.tzinfo is None


# ---------------------------------------------------------------------------
# get_all_merchants()
# ---------------------------------------------------------------------------


class TestGetDatabaseStats:
    """Tests for get_database_stats()."""

    def test_empty_database(self, logged_in_backend):
        """Empty database returns zeros and None dates."""
        b, _ = logged_in_backend
        stats = b.get_database_stats()
        assert stats["total_transactions"] == 0
        assert stats["total_amount"] == 0.0
        assert stats["earliest_date"] is None
        assert stats["latest_date"] is None
        assert stats["last_refresh_timestamp"] is None
        assert stats["last_refresh_count"] is None

    def test_with_data(self, logged_in_backend):
        """Database with transactions returns correct stats."""
        b, _ = logged_in_backend
        b._ensure_db_initialized()
        conn = sqlite3.connect(b._db_path)
        for txn in SAMPLE_TRANSACTIONS:
            conn.execute(
                """INSERT INTO transactions
                   (id, date, amount, merchant_name, merchant_id,
                    category_id, category_name, account_id, account_name,
                    notes, hideFromReports, pending, isRecurring)
                   VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)""",
                (
                    txn["id"],
                    txn["date"],
                    txn["amount"],
                    txn["merchant"]["name"],
                    txn["merchant"]["id"],
                    txn["category"]["id"],
                    txn["category"]["name"],
                    txn["account"]["id"],
                    txn["account"]["displayName"],
                    txn.get("notes", ""),
                    1 if txn.get("hideFromReports") else 0,
                    1 if txn.get("pending") else 0,
                    1 if txn.get("isRecurring") else 0,
                ),
            )
        conn.commit()
        conn.close()

        stats = b.get_database_stats()
        assert stats["total_transactions"] == 3
        assert stats["total_amount"] == pytest.approx(-42.50 + -12.00 + 2500.00)
        assert stats["earliest_date"] == "2024-03-13"
        assert stats["latest_date"] == "2024-03-15"

    def test_with_refresh_metadata(self, logged_in_backend):
        """Refresh metadata fields are populated."""
        b, _ = logged_in_backend
        b._set_refresh_metadata("last_refresh_timestamp", "2024-06-01T12:00:00+00:00")
        b._set_refresh_metadata("last_refresh_count", "42")

        stats = b.get_database_stats()
        assert stats["last_refresh_timestamp"] == "2024-06-01T12:00:00+00:00"
        assert stats["last_refresh_count"] == "42"

    @pytest.mark.asyncio
    async def test_stats_include_profile_currency(self, logged_in_backend):
        backend, _ = logged_in_backend

        await backend.refresh()

        assert backend.get_database_stats()["currency_code"] == "USD"


class TestGetAllMerchants:
    @pytest.mark.asyncio
    async def test_returns_sorted_unique_descriptions(self, logged_in_backend):
        b, _ = logged_in_backend
        merchants = await b.get_all_merchants()
        assert merchants == sorted({"Coffee Shop", "Grocery Store", "Payroll"})

    @pytest.mark.asyncio
    async def test_successful_empty_refresh_is_not_repeated(self, tmp_path):
        """Merchant reads honor refresh metadata even when no rows were stored."""
        b = SimpleFinBackend(db_path=str(tmp_path / "test.db"))
        mock_client = MagicMock()
        mock_client.fetch_transactions = AsyncMock(return_value=[])
        b._client = mock_client

        assert await b.get_all_merchants() == []
        assert await b.get_all_merchants() == []

        mock_client.fetch_transactions.assert_called_once()

    @pytest.mark.asyncio
    async def test_not_logged_in_raises(self, backend):
        with pytest.raises(RuntimeError, match="not logged in"):
            await backend.get_all_merchants()


# ---------------------------------------------------------------------------
# update_transaction() / delete_transaction() (local SQLite persistence)
# ---------------------------------------------------------------------------


class TestLocalPersistence:
    async def _populate(self, b):
        """Ensure the SQLite store is populated with sample transactions."""
        await b.get_transactions(limit=100, offset=0)

    @pytest.mark.asyncio
    async def test_update_merchant_name(self, logged_in_backend):
        b, _ = logged_in_backend
        await self._populate(b)
        await b.update_transaction("acct-1:txn-1", merchant_name="Whole Foods")

        result = await b.get_transactions(limit=100, offset=0)
        txn = next(t for t in result["allTransactions"]["results"] if t["id"] == "acct-1:txn-1")
        assert txn["merchant"]["name"] == "Whole Foods"

    @pytest.mark.asyncio
    async def test_update_merchant_does_not_affect_other_transactions(self, logged_in_backend):
        b, _ = logged_in_backend
        await self._populate(b)
        await b.update_transaction("acct-1:txn-1", merchant_name="Whole Foods")

        result = await b.get_transactions(limit=100, offset=0)
        txn2 = next(t for t in result["allTransactions"]["results"] if t["id"] == "acct-1:txn-2")
        assert txn2["merchant"]["name"] == "Coffee Shop"

    @pytest.mark.asyncio
    async def test_update_category(self, logged_in_backend):
        b, _ = logged_in_backend
        await self._populate(b)
        await b.update_transaction("acct-1:txn-1", category_id="cat_groceries")

        result = await b.get_transactions(limit=100, offset=0)
        txn = next(t for t in result["allTransactions"]["results"] if t["id"] == "acct-1:txn-1")
        assert txn["category"]["id"] == "cat_groceries"

    @pytest.mark.asyncio
    async def test_update_category_persists_name(self, logged_in_backend):
        """category_name is persisted in SQLite when passed alongside category_id."""
        b, _ = logged_in_backend
        await self._populate(b)
        await b.update_transaction(
            "acct-1:txn-1",
            category_id="cat_groceries",
            category_name="Groceries",
        )

        result = await b.get_transactions(limit=100, offset=0)
        txn = next(t for t in result["allTransactions"]["results"] if t["id"] == "acct-1:txn-1")
        assert txn["category"]["id"] == "cat_groceries"
        assert txn["category"]["name"] == "Groceries"

    @pytest.mark.asyncio
    async def test_update_category_name_survives_new_connection(self, tmp_path):
        """Simulates an app restart: category name survives when a new
        backend instance reads the same SQLite database."""
        db_path = str(tmp_path / "test.db")
        b1 = SimpleFinBackend(db_path=db_path)
        mock_client = MagicMock()
        mock_client.fetch_transactions = AsyncMock(return_value=SAMPLE_TRANSACTIONS)
        b1._client = mock_client
        await self._populate(b1)

        await b1.update_transaction(
            "acct-1:txn-1",
            category_id="groceries",
            category_name="Groceries",
        )

        del b1

        b2 = SimpleFinBackend(db_path=db_path)
        mock_client2 = MagicMock()
        mock_client2.fetch_transactions = AsyncMock(return_value=[])
        b2._client = mock_client2

        result = await b2.get_transactions(limit=100, offset=0)
        txn = next(t for t in result["allTransactions"]["results"] if t["id"] == "acct-1:txn-1")
        assert txn["category"]["id"] == "groceries"
        assert txn["category"]["name"] == "Groceries"

    @pytest.mark.asyncio
    async def test_update_hide_from_reports(self, logged_in_backend):
        b, _ = logged_in_backend
        await self._populate(b)
        await b.update_transaction("acct-1:txn-1", hide_from_reports=True)

        result = await b.get_transactions(limit=100, offset=0)
        txn = next(t for t in result["allTransactions"]["results"] if t["id"] == "acct-1:txn-1")
        assert txn["hideFromReports"] is True

    @pytest.mark.asyncio
    async def test_update_with_no_changes(self, logged_in_backend):
        b, _ = logged_in_backend
        await self._populate(b)
        result = await b.update_transaction("acct-1:txn-1")
        assert result == {"updateTransaction": {"transaction": {"id": "acct-1:txn-1"}}}

    @pytest.mark.asyncio
    async def test_update_preserved_across_refresh(self, logged_in_backend):
        """Local edits survive a subsequent refresh (INSERT OR IGNORE)."""
        b, mock_client = logged_in_backend
        await self._populate(b)

        await b.update_transaction("acct-1:txn-1", merchant_name="Whole Foods")

        new_txn = {
            "id": "txn-4",
            "date": "2024-03-20",
            "amount": -15.00,
            "merchant": {"id": "New Store", "name": "New Store"},
            "category": {"id": "uncategorized", "name": "Uncategorized"},
            "account": {"id": "acct-1", "displayName": "Checking"},
            "notes": "",
            "hideFromReports": False,
            "pending": False,
            "isRecurring": False,
        }
        mock_client.fetch_transactions = AsyncMock(return_value=[new_txn])

        await b.refresh()

        result = await b.get_transactions(limit=100, offset=0)
        txn1 = next(t for t in result["allTransactions"]["results"] if t["id"] == "acct-1:txn-1")
        assert txn1["merchant"]["name"] == "Whole Foods"
        assert result["allTransactions"]["totalCount"] == 4

    @pytest.mark.asyncio
    async def test_delete_transaction(self, logged_in_backend):
        b, _ = logged_in_backend
        await self._populate(b)

        deleted = await b.delete_transaction("acct-1:txn-1")
        assert deleted is True

        result = await b.get_transactions(limit=100, offset=0)
        assert result["allTransactions"]["totalCount"] == 2
        ids = {t["id"] for t in result["allTransactions"]["results"]}
        assert "acct-1:txn-1" not in ids

    @pytest.mark.asyncio
    async def test_deleted_transaction_stays_deleted_after_refresh(self, logged_in_backend):
        b, _ = logged_in_backend
        await self._populate(b)

        assert await b.delete_transaction("acct-1:txn-1") is True
        await b.refresh()

        result = await b.get_transactions(limit=100, offset=0)
        ids = {transaction["id"] for transaction in result["allTransactions"]["results"]}
        assert "acct-1:txn-1" not in ids

    @pytest.mark.asyncio
    async def test_delete_nonexistent_transaction(self, logged_in_backend):
        b, _ = logged_in_backend
        await self._populate(b)
        deleted = await b.delete_transaction("does-not-exist")
        assert deleted is False


# ---------------------------------------------------------------------------
# Profile directory integration
# ---------------------------------------------------------------------------


class TestProfileIntegration:
    """Tests for profile_dir-driven database isolation in SimpleFinBackend."""

    def test_database_directory_and_file_use_private_permissions(self, tmp_path):
        db_path = tmp_path / "profile" / "simplefin.db"
        backend = SimpleFinBackend(db_path=str(db_path))

        original_umask = os.umask(0o022)
        try:
            backend._ensure_db_initialized()
        finally:
            os.umask(original_umask)

        assert stat.S_IMODE(db_path.parent.stat().st_mode) == 0o700
        assert stat.S_IMODE(db_path.stat().st_mode) == 0o600

    def test_existing_database_permissions_are_restricted(self, tmp_path):
        db_path = tmp_path / "simplefin.db"
        db_path.touch(mode=0o644)
        db_path.chmod(0o644)
        backend = SimpleFinBackend(db_path=str(db_path))

        backend._ensure_db_initialized()

        assert stat.S_IMODE(db_path.stat().st_mode) == 0o600

    def test_existing_managed_profile_directory_permissions_are_restricted(self, tmp_path):
        profile_dir = tmp_path / "simplefin-profile"
        profile_dir.mkdir(mode=0o755)
        profile_dir.chmod(0o755)
        backend = SimpleFinBackend(profile_dir=profile_dir)

        backend._ensure_db_initialized()

        assert stat.S_IMODE(profile_dir.stat().st_mode) == 0o700

    def test_existing_custom_database_directory_permissions_are_preserved(self, tmp_path):
        custom_dir = tmp_path / "shared-data"
        custom_dir.mkdir(mode=0o755)
        custom_dir.chmod(0o755)
        backend = SimpleFinBackend(db_path=str(custom_dir / "simplefin.db"))

        backend._ensure_db_initialized()

        assert stat.S_IMODE(custom_dir.stat().st_mode) == 0o755

    def test_existing_database_adds_currency_column(self, tmp_path):
        db_path = tmp_path / "legacy.db"
        conn = sqlite3.connect(db_path)
        conn.execute("""
            CREATE TABLE transactions (
                id TEXT PRIMARY KEY,
                date TEXT NOT NULL,
                amount REAL,
                merchant_name TEXT NOT NULL DEFAULT '',
                merchant_id TEXT NOT NULL DEFAULT '',
                category_id TEXT NOT NULL DEFAULT 'uncategorized',
                category_name TEXT NOT NULL DEFAULT 'Uncategorized',
                account_id TEXT NOT NULL DEFAULT '',
                account_name TEXT NOT NULL DEFAULT '',
                notes TEXT NOT NULL DEFAULT '',
                hideFromReports INTEGER NOT NULL DEFAULT 0,
                pending INTEGER NOT NULL DEFAULT 0,
                isRecurring INTEGER NOT NULL DEFAULT 0
            )
        """)
        conn.commit()
        conn.close()

        backend = SimpleFinBackend(db_path=str(db_path))
        backend._ensure_db_initialized()

        conn = sqlite3.connect(db_path)
        columns = {row[1] for row in conn.execute("PRAGMA table_info(transactions)")}
        conn.close()
        assert "currency" in columns

    def test_profile_dir_derives_db_path(self, tmp_path):
        """Given profile_dir, backend should derive db_path under that directory."""
        profile_dir = tmp_path / "profiles" / "simplefin-personal"
        profile_dir.mkdir(parents=True)
        b = SimpleFinBackend(profile_dir=profile_dir)
        expected = str(profile_dir / "simplefin.db")
        assert b._db_path == expected

    def test_profile_dir_creates_db_path_not_default(self, tmp_path):
        """profile_dir should override the DEFAULT_DB_PATH fallback."""
        profile_dir = tmp_path / "profiles" / "simplefin-personal"
        profile_dir.mkdir(parents=True)
        b = SimpleFinBackend(profile_dir=profile_dir)
        assert b._db_path != str(SimpleFinBackend.DEFAULT_DB_PATH)
        assert not b._db_path.endswith("/.moneyflow/simplefin.db")

    def test_two_profiles_get_separate_databases(self, tmp_path):
        """Two different profile_dirs should yield completely separate db paths."""
        p1 = tmp_path / "profiles" / "alpha"
        p2 = tmp_path / "profiles" / "beta"
        p1.mkdir(parents=True)
        p2.mkdir(parents=True)

        b1 = SimpleFinBackend(profile_dir=p1)
        b2 = SimpleFinBackend(profile_dir=p2)

        assert b1._db_path != b2._db_path
        assert b1._db_path == str(p1 / "simplefin.db")
        assert b2._db_path == str(p2 / "simplefin.db")

    def test_explicit_db_path_takes_precedence_over_profile_dir(self, tmp_path):
        """When both db_path and profile_dir are given, db_path wins."""
        profile_dir = tmp_path / "profiles" / "some-profile"
        profile_dir.mkdir(parents=True)
        custom_db = str(tmp_path / "custom" / "my.db")
        b = SimpleFinBackend(db_path=custom_db, profile_dir=profile_dir)
        assert b._db_path == custom_db

    def test_profile_dir_with_string_path(self, tmp_path):
        """profile_dir as a string (how app.py passes it) must work too."""
        profile_dir = tmp_path / "profiles" / "string-test"
        profile_dir.mkdir(parents=True)
        b = SimpleFinBackend(profile_dir=str(profile_dir))
        expected = str(profile_dir / "simplefin.db")
        assert b._db_path == expected


# ---------------------------------------------------------------------------
# clear_auth()
# ---------------------------------------------------------------------------


class TestClearAuth:
    @pytest.mark.asyncio
    async def test_clear_auth_resets_client(self, backend):
        await backend.login(password=VALID_ACCESS_URL)
        assert backend._client is not None
        backend.clear_auth()
        assert backend._client is None

    @pytest.mark.asyncio
    async def test_clear_auth_does_not_clear_sqlite_data(self, logged_in_backend):
        """SQLite data survives clear_auth()."""
        b, _ = logged_in_backend

        # Populate SQLite via get_transactions
        await b.get_transactions(limit=100, offset=0)

        # clear_auth should not drop the database
        b.clear_auth()

        # Re-initialise to validate data survives
        b._client = MagicMock()
        b._client.fetch_transactions = AsyncMock(return_value=[])
        result = await b.get_transactions(limit=100, offset=0)
        assert result["allTransactions"]["totalCount"] == 3
