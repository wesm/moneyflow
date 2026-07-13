"""Tests for MoneyflowApp."""

from datetime import datetime

import polars as pl


class RefreshingSimpleFinBackend:
    """SimpleFIN backend stub that reports one new transaction."""

    async def refresh(self) -> int:
        return 1

    def get_last_update_time(self) -> datetime:
        return datetime(2025, 2, 2, 12, 0)


class FetchingDataManager:
    """Data manager stub for background refresh tests."""

    def __init__(self, df: pl.DataFrame):
        self._df = df
        self.df = None
        self.categories = {}
        self.category_groups = []

    async def fetch_all_data(self):
        return self._df, {"cat": {"name": "Category"}}, [{"name": "Group"}]

    def _populate_categories_from_config(self) -> None:
        return None


class RefreshController:
    """Controller stub that records refresh calls."""

    def __init__(self):
        self.refresh_calls = []

    def refresh_view(self, force_rebuild: bool = True) -> None:
        self.refresh_calls.append(force_rebuild)


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


class TestSubtitleFormatting:
    def test_last_update_time_uses_portable_hour_directive(self):
        from moneyflow.tui.app import MoneyflowApp

        class BackendStub:
            @staticmethod
            def get_backend_type():
                return "simplefin"

        class WindowsStrictTime:
            @staticmethod
            def strftime(format_string):
                if format_string != "%I:%M %p":
                    raise ValueError(f"Unsupported format: {format_string}")
                return "09:05 AM"

        app = MoneyflowApp(backend=BackendStub())
        app._last_update_time = WindowsStrictTime()

        app._refresh_subtitle()

        assert app.sub_title == "Simplefin | Last update: 9:05 AM"


class TestSimpleFinCredentialClaim:
    async def test_token_claim_runs_in_worker_thread(self, monkeypatch):
        from moneyflow.tui.screens import credential_screens

        calls = []

        def fake_claim(token):
            calls.append(("claim", token))
            return "https://example:secret@bridge.simplefin.org/simplefin"

        async def fake_to_thread(function, *args):
            calls.append(("to_thread", function, args))
            return function(*args)

        monkeypatch.setattr(credential_screens, "claim_token", fake_claim)
        monkeypatch.setattr(credential_screens.asyncio, "to_thread", fake_to_thread)

        result = await credential_screens._claim_simplefin_token("setup-token")

        assert result == "https://example:secret@bridge.simplefin.org/simplefin"
        assert calls[0][0] == "to_thread"
        assert calls[1] == ("claim", "setup-token")


class TestSimpleFinBackgroundRefresh:
    """Regression tests for SimpleFIN background refresh behavior."""

    async def test_background_refresh_preserves_display_start_date_filter(self, monkeypatch):
        from moneyflow.tui.app import MoneyflowApp

        df = pl.DataFrame(
            {
                "id": ["old", "new"],
                "date": [datetime(2025, 1, 15), datetime(2025, 2, 1)],
                "merchant": ["Old Store", "New Store"],
                "amount": [-10.0, -20.0],
            }
        )
        controller = RefreshController()
        app = MoneyflowApp(backend=RefreshingSimpleFinBackend())
        app.data_manager = FetchingDataManager(df)
        app.controller = controller
        app.display_start_date = "2025-02-01"

        monkeypatch.setattr(app, "notify", lambda *args, **kwargs: None)
        monkeypatch.setattr(app, "_save_last_update_time", lambda: None)
        monkeypatch.setattr(app, "_refresh_subtitle", lambda: None)
        monkeypatch.setattr(app, "_save_table_position", lambda: {"cursor": 0})
        monkeypatch.setattr(app, "_restore_table_position", lambda saved: None)

        await app._simplefin_background_refresh()

        assert app.state.transactions_df["id"].to_list() == ["new"]
        assert controller.refresh_calls == [False]


class TestCategoryManagerSource:
    async def test_manager_uses_complete_unfiltered_transactions(self, monkeypatch):
        from moneyflow.tui.app import MoneyflowApp

        filtered_df = pl.DataFrame({"id": ["visible"], "category_id": ["source-category"]})
        complete_df = pl.DataFrame(
            {
                "id": ["visible", "older"],
                "category_id": ["source-category", "source-category"],
            }
        )

        class DataManagerStub:
            categories = {
                "source-category": {"name": "Source", "group": "Group"},
                "target-category": {"name": "Target", "group": "Group"},
            }
            pending_edits = []

            async def fetch_unfiltered_transactions(self):
                return complete_df

        class ControllerStub:
            source_ids = []

            def queue_category_reassignment(self, source_df, source_id, target_id):
                self.source_ids = source_df.filter(pl.col("category_id") == source_id)[
                    "id"
                ].to_list()

        app = MoneyflowApp()
        app.data_manager = DataManagerStub()
        app.controller = ControllerStub()
        app.state.transactions_df = filtered_df

        async def exercise_reassignment(screen, **kwargs):
            assert screen.transaction_counts["source-category"] == 2
            screen._queue_reassign("source-category", "target-category")
            return False

        monkeypatch.setattr(app, "push_screen", exercise_reassignment)

        await app._manage_categories()

        assert app.controller.source_ids == ["visible", "older"]
