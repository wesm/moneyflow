"""Tests for CsvFinanceBackend."""

import asyncio
import os

import pytest

from moneyflow.backends.csv_backend import (
    CsvFinanceBackend,
    _validate_path_security,
)


@pytest.fixture
def tmp_profile_dir(tmp_path):
    profile = tmp_path / "profiles" / "csv_test"
    profile.mkdir(parents=True)
    return profile


@pytest.fixture
def tmp_config_dir(tmp_path):
    config = tmp_path / "config"
    config.mkdir()
    return str(config)


@pytest.fixture
def chase_backend(tmp_profile_dir, tmp_config_dir):
    return CsvFinanceBackend(
        profile_dir=tmp_profile_dir,
        config_dir=tmp_config_dir,
        institution_name="chase_credit",
    )


class TestCsvFinanceBackend:
    def test_get_backend_type(self, chase_backend):
        assert chase_backend.get_backend_type() == "csv_chase_credit"

    def test_db_path_derived_from_institution_name(self, chase_backend, tmp_profile_dir):
        expected = str(tmp_profile_dir / "chase_credit_transactions.db")
        assert chase_backend.db_path == expected

    def test_schema_is_created_on_first_connection(self, chase_backend):
        conn = chase_backend._get_connection()
        tables = conn.execute(
            "SELECT name FROM sqlite_master WHERE type='table' AND name NOT LIKE 'sqlite_%'"
        ).fetchall()
        conn.close()
        table_names = {row[0] for row in tables}
        assert "transactions" in table_names
        assert "import_history" in table_names

    def test_transactions_table_has_correct_columns(self, chase_backend):
        conn = chase_backend._get_connection()
        cols = conn.execute("PRAGMA table_info(transactions)").fetchall()
        conn.close()
        col_names = {row[1] for row in cols}
        for col in (
            "id",
            "date",
            "amount",
            "merchant",
            "category",
            "category_id",
            "account",
            "notes",
            "extras",
            "hideFromReports",
            "imported_at",
        ):
            assert col in col_names

    def test_insert_and_get_transactions(self, chase_backend):
        conn = chase_backend._get_connection()
        conn.execute(
            "INSERT INTO transactions (id, date, amount, merchant, extras) VALUES (?, ?, ?, ?, ?)",
            ("chase_001", "2026-07-12", -50.0, "EXAMPLE GIFT SHOP", '{"raw_category":"Gifts"}'),
        )
        conn.commit()
        conn.close()

        async def _run():
            result = await chase_backend.get_transactions(limit=10)
            assert result["totalCount"] == 1
            txn = result["results"][0]
            assert txn["id"] == "chase_001"
            assert txn["date"] == "2026-07-12"
            assert txn["amount"] == -50.0
            assert txn["merchant"]["name"] == "EXAMPLE GIFT SHOP"
            assert txn["raw_category"] == "Gifts"
            assert txn["hideFromReports"] is False
            assert txn["pending"] is False

        asyncio.run(_run())

    def test_update_transaction(self, chase_backend):
        conn = chase_backend._get_connection()
        conn.execute(
            "INSERT INTO transactions (id, date, amount, merchant) VALUES (?, ?, ?, ?)",
            ("chase_002", "2026-07-12", -20.0, "OLD MERCHANT"),
        )
        conn.commit()
        conn.close()

        async def _run():
            result = await chase_backend.update_transaction(
                "chase_002", merchant_name="NEW MERCHANT"
            )
            assert result["updateTransaction"]["transaction"]["merchant"]["name"] == "NEW MERCHANT"

        asyncio.run(_run())

        conn = chase_backend._get_connection()
        row = conn.execute(
            "SELECT merchant FROM transactions WHERE id = ?", ("chase_002",)
        ).fetchone()
        conn.close()
        assert row[0] == "NEW MERCHANT"

    def test_delete_transaction(self, chase_backend):
        conn = chase_backend._get_connection()
        conn.execute(
            "INSERT INTO transactions (id, date, amount, merchant) VALUES (?, ?, ?, ?)",
            ("chase_003", "2026-07-12", -10.0, "TO_DELETE"),
        )
        conn.commit()
        conn.close()

        async def _run():
            result = await chase_backend.delete_transaction("chase_003")
            assert result is True

        asyncio.run(_run())

        conn = chase_backend._get_connection()
        exists = conn.execute("SELECT 1 FROM transactions WHERE id = ?", ("chase_003",)).fetchone()
        conn.close()
        assert exists is None

    def test_get_all_merchants(self, chase_backend):
        conn = chase_backend._get_connection()
        conn.execute(
            "INSERT INTO transactions (id, date, amount, merchant) VALUES (?, ?, ?, ?)",
            ("m1", "2026-07-12", -1.0, "Merch A"),
        )
        conn.execute(
            "INSERT INTO transactions (id, date, amount, merchant) VALUES (?, ?, ?, ?)",
            ("m2", "2026-07-12", -2.0, "Merch A"),
        )
        conn.execute(
            "INSERT INTO transactions (id, date, amount, merchant) VALUES (?, ?, ?, ?)",
            ("m3", "2026-07-12", -3.0, "Merch B"),
        )
        conn.commit()
        conn.close()

        async def _run():
            merchants = await chase_backend.get_all_merchants()
            assert merchants == ["Merch A", "Merch B"]

        asyncio.run(_run())

    def test_get_import_history(self, chase_backend):
        conn = chase_backend._get_connection()
        conn.execute(
            "INSERT INTO import_history (filename, record_count, duplicate_count) VALUES (?, ?, ?)",
            ("test.csv", 100, 5),
        )
        conn.commit()
        conn.close()

        history = chase_backend.get_import_history()
        assert len(history) == 1
        assert history[0]["filename"] == "test.csv"
        assert history[0]["record_count"] == 100

    def test_get_database_stats(self, chase_backend):
        conn = chase_backend._get_connection()
        conn.execute(
            "INSERT INTO transactions (id, date, amount, merchant) VALUES (?, ?, ?, ?)",
            ("s1", "2026-01-01", -50.0, "M"),
        )
        conn.execute(
            "INSERT INTO transactions (id, date, amount, merchant) VALUES (?, ?, ?, ?)",
            ("s2", "2026-12-31", -30.0, "M"),
        )
        conn.commit()
        conn.close()

        stats = chase_backend.get_database_stats()
        assert stats["total_transactions"] == 2
        assert stats["total_amount"] == -80.0
        assert stats["earliest_date"] == "2026-01-01"
        assert stats["latest_date"] == "2026-12-31"

    def test_db_file_has_restrictive_permissions(self, chase_backend):
        chase_backend._get_connection().close()
        mode = os.stat(chase_backend.db_path).st_mode & 0o777
        assert mode == 0o600

    def test_rejects_symlinked_database_path(self, tmp_path, tmp_config_dir):
        profile = tmp_path / "symlink_profile"
        profile.mkdir()
        real_db = profile / "real_chase_credit_transactions.db"
        symlink_db = profile / "chase_credit_transactions.db"
        symlink_db.symlink_to(real_db)

        backend = CsvFinanceBackend(
            profile_dir=profile,
            config_dir=tmp_config_dir,
            institution_name="chase_credit",
        )
        with pytest.raises(OSError):
            _validate_path_security(backend.db_path)

    def test_rejects_group_writable_parent_directory(self, tmp_path, tmp_config_dir):
        profile = tmp_path / "insecure_profile"
        profile.mkdir()
        profile.chmod(0o777)

        backend = CsvFinanceBackend(
            profile_dir=profile,
            config_dir=tmp_config_dir,
            institution_name="chase_credit",
        )
        with pytest.raises(OSError):
            _validate_path_security(backend.db_path)
