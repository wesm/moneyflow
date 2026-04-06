"""Tests for MoneyflowApp."""

from datetime import datetime

import polars as pl


class TestCacheDataFiltering:
    """Test filtering cached data by date range (for --mtd, --since flags)."""

    def test_filter_by_start_date_basic(self):
        """Test basic filtering of cached data by start date."""
        from moneyflow.tui.app import MoneyflowApp

        df = pl.DataFrame(
            {
                "id": ["tx1", "tx2", "tx3", "tx4", "tx5"],
                "date": [
                    datetime(2025, 1, 15),
                    datetime(2025, 2, 10),
                    datetime(2025, 3, 5),
                    datetime(2025, 12, 1),
                    datetime(2025, 12, 15),
                ],
                "merchant": ["Store A", "Store B", "Store C", "Store D", "Store E"],
                "amount": [-10.0, -20.0, -30.0, -40.0, -50.0],
            }
        )

        filtered = MoneyflowApp._filter_df_by_start_date(df, "2025-12-01")

        assert len(filtered) == 2
        assert filtered["id"].to_list() == ["tx4", "tx5"]

    def test_filter_by_start_date_includes_boundary(self):
        """Test that filtering includes transactions on the start date."""
        from moneyflow.tui.app import MoneyflowApp

        df = pl.DataFrame(
            {
                "id": ["tx1", "tx2", "tx3"],
                "date": [
                    datetime(2025, 12, 1),
                    datetime(2025, 12, 1),
                    datetime(2025, 12, 2),
                ],
                "merchant": ["Store A", "Store B", "Store C"],
                "amount": [-10.0, -20.0, -30.0],
            }
        )

        filtered = MoneyflowApp._filter_df_by_start_date(df, "2025-12-01")

        assert len(filtered) == 3

    def test_filter_by_start_date_empty_result(self):
        """Test filtering when all transactions are before start date."""
        from moneyflow.tui.app import MoneyflowApp

        df = pl.DataFrame(
            {
                "id": ["tx1", "tx2"],
                "date": [datetime(2025, 1, 1), datetime(2025, 6, 15)],
                "merchant": ["Store A", "Store B"],
                "amount": [-10.0, -20.0],
            }
        )

        filtered = MoneyflowApp._filter_df_by_start_date(df, "2025-12-01")

        assert len(filtered) == 0

    def test_filter_mtd_scenario(self):
        """Test realistic MTD filtering scenario with full year of data."""
        from moneyflow.tui.app import MoneyflowApp

        dates = []
        ids = []
        for month in range(1, 13):
            for day in [1, 15]:
                dates.append(datetime(2025, month, day))
                ids.append(f"tx_{month}_{day}")

        df = pl.DataFrame(
            {
                "id": ids,
                "date": dates,
                "merchant": [f"Store {i}" for i in range(len(ids))],
                "amount": [-10.0] * len(ids),
            }
        )

        filtered = MoneyflowApp._filter_df_by_start_date(df, "2025-12-01")

        assert len(filtered) == 2
        assert all(d.month == 12 for d in filtered["date"].to_list())
