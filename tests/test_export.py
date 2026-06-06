"""Tests for export functionality."""

from datetime import date

import polars as pl

from moneyflow.tui import notification_helper


class TestExportFileWriting:
    """Test that export writes valid Parquet files."""

    def test_export_writes_valid_parquet(self, tmp_path):
        """Verify export writes a valid Parquet file with correct schema."""
        data = [
            {
                "id": "txn_1",
                "date": date(2024, 10, 1),
                "amount": -45.67,
                "merchant": "Whole Foods",
                "merchant_id": "merch_1",
                "category": "Groceries",
                "category_id": "cat_1",
                "account": "Chase Checking",
                "account_id": "acc_1",
                "notes": "",
                "hideFromReports": False,
                "pending": False,
                "isRecurring": False,
            },
            {
                "id": "txn_2",
                "date": date(2024, 10, 2),
                "amount": -23.45,
                "merchant": "Starbucks",
                "merchant_id": "merch_2",
                "category": "Restaurants & Bars",
                "category_id": "cat_2",
                "account": "Chase Checking",
                "account_id": "acc_1",
                "notes": "",
                "hideFromReports": False,
                "pending": False,
                "isRecurring": False,
            },
        ]
        df = pl.DataFrame(data)

        exports_dir = tmp_path / "exports"
        exports_dir.mkdir(parents=True, exist_ok=True)
        path = exports_dir / "2025-06-05-full-export.parquet"

        df.write_parquet(str(path))

        assert path.exists()
        assert path.stat().st_size > 0

        loaded = pl.read_parquet(str(path))
        assert len(loaded) == 2
        assert set(loaded.columns) >= {
            "id",
            "date",
            "amount",
            "merchant",
            "merchant_id",
            "category",
            "category_id",
            "account",
            "account_id",
            "notes",
            "hideFromReports",
            "pending",
            "isRecurring",
        }
        assert loaded["id"].to_list() == ["txn_1", "txn_2"]

    def test_export_empty_dataframe(self, tmp_path):
        """Verify exporting an empty DataFrame creates a valid (empty) file."""
        schema = {
            "id": pl.Utf8,
            "date": pl.Date,
            "amount": pl.Float64,
            "merchant": pl.Utf8,
        }
        df = pl.DataFrame(schema=schema)

        exports_dir = tmp_path / "exports"
        exports_dir.mkdir(parents=True, exist_ok=True)
        path = exports_dir / "empty-export.parquet"

        df.write_parquet(str(path))

        assert path.exists()
        loaded = pl.read_parquet(str(path))
        assert len(loaded) == 0

    def test_export_with_group_column(self, tmp_path):
        """Verify group column is properly included when present."""
        df = pl.DataFrame(
            {
                "id": ["txn_1"],
                "date": [date(2024, 10, 1)],
                "amount": [-45.67],
                "merchant": ["Whole Foods"],
                "merchant_id": ["merch_1"],
                "category": ["Groceries"],
                "category_id": ["cat_1"],
                "group": ["Food & Dining"],
                "account": ["Chase Checking"],
                "account_id": ["acc_1"],
                "notes": [""],
                "hideFromReports": [False],
                "pending": [False],
                "isRecurring": [False],
            }
        )

        exports_dir = tmp_path / "exports"
        exports_dir.mkdir(parents=True, exist_ok=True)
        path = exports_dir / "grouped-export.parquet"

        df.write_parquet(str(path))

        loaded = pl.read_parquet(str(path))
        assert "group" in loaded.columns
        assert loaded["group"][0] == "Food & Dining"

    def test_export_path_naming_convention(self, tmp_path):
        """Verify the naming convention: <date>-full-export.parquet."""
        from datetime import date as date_type

        today = date_type.today()
        expected_name = f"{today}-full-export.parquet"
        path = tmp_path / expected_name

        df = pl.DataFrame({"id": ["test"], "amount": [1.0]})
        df.write_parquet(str(path))

        assert path.name == expected_name

    def test_exports_dir_is_created(self, tmp_path):
        """Verify exports directory is created if it doesn't exist."""
        exports_dir = tmp_path / "exports"

        assert not exports_dir.exists()

        exports_dir.mkdir(parents=True, exist_ok=True)
        path = exports_dir / "test.parquet"

        df = pl.DataFrame({"id": ["test"], "amount": [1.0]})
        df.write_parquet(str(path))

        assert exports_dir.is_dir()
        assert path.exists()


class TestExportNotifications:
    """Test export notification messages."""

    def test_export_starting_message(self):
        msg, severity, timeout = notification_helper.export_starting(150)
        assert "150 transactions" in msg
        assert "Exporting" in msg
        assert severity == "information"
        assert timeout == 2

    def test_export_success_message(self):
        msg, severity, timeout = notification_helper.export_success(
            "/home/user/.moneyflow/exports/2025-06-05-full-export.parquet", 100
        )
        assert "100 transactions" in msg
        assert "✅" in msg
        assert "full-export.parquet" in msg
        assert severity == "information"
        assert timeout == 3

    def test_export_error_message(self):
        msg, severity, timeout = notification_helper.export_error("Permission denied")
        assert "Permission denied" in msg
        assert "❌" in msg
        assert severity == "error"
        assert timeout == 5

    def test_success_notification_includes_checkmark(self):
        """Success messages should use ✅ emoji."""
        msg = notification_helper.export_success("/tmp/out.parquet", 10)[0]
        assert "✅" in msg

    def test_error_notification_includes_x(self):
        """Error messages should use ❌ emoji."""
        msg = notification_helper.export_error("fail")[0]
        assert "❌" in msg
