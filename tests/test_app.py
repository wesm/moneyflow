"""Tests for MoneyflowApp."""

from datetime import datetime

import polars as pl
import pytest

from moneyflow.data.state import TransactionEdit


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
            category_groups_config = {"Group": ["Source", "Target"]}
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

    async def test_manager_counts_pending_category_assignments(self, monkeypatch):
        from moneyflow.tui.app import MoneyflowApp

        complete_df = pl.DataFrame(
            {
                "id": ["transaction-1"],
                "category_id": ["source-category"],
            }
        )

        class DataManagerStub:
            categories = {
                "source-category": {"name": "Source", "group": "Group"},
                "target-category": {"name": "Target", "group": "Group"},
            }
            category_groups_config = {"Group": ["Source", "Target"]}
            pending_edits = [
                TransactionEdit(
                    transaction_id="transaction-1",
                    field="category",
                    old_value="source-category",
                    new_value="target-category",
                )
            ]

            async def fetch_unfiltered_transactions(self):
                return complete_df

        app = MoneyflowApp()
        app.data_manager = DataManagerStub()

        async def inspect_counts(screen, **kwargs):
            assert screen.transaction_counts.get("source-category", 0) == 0
            assert screen.transaction_counts["target-category"] == 1
            return False

        monkeypatch.setattr(app, "push_screen", inspect_counts)

        await app._manage_categories()

    async def test_manager_tracks_only_edits_that_own_deferred_config(self, monkeypatch):
        from moneyflow.tui.app import MoneyflowApp

        unrelated_edit = TransactionEdit(
            transaction_id="transaction-0",
            field="category",
            old_value="another-source",
            new_value="another-target",
            timestamp=datetime(2026, 1, 1),
        )
        dependent_timestamp = datetime(2026, 1, 2)
        complete_df = pl.DataFrame({"id": ["transaction-1"], "category_id": ["source-category"]})

        class DataManagerStub:
            categories = {
                "source-category": {"name": "Source", "group": "Group"},
                "target-category": {"name": "Target", "group": "Group"},
            }
            pending_edits = [unrelated_edit]
            pending_category_groups = None
            pending_category_group_edit_timestamps = set()
            pending_category_groups_previous = None
            category_groups_config = {"Group": ["Source", "Target"]}
            category_to_group = {"Source": "Group", "Target": "Group"}
            profile_dir = object()

            async def fetch_unfiltered_transactions(self):
                return complete_df

        class ControllerStub:
            def queue_category_reassignment(self, source_df, source_id, target_id):
                app.data_manager.pending_edits.append(
                    TransactionEdit(
                        transaction_id="transaction-1",
                        field="category",
                        old_value=source_id,
                        new_value=target_id,
                        timestamp=dependent_timestamp,
                    )
                )

        app = MoneyflowApp()
        app.data_manager = DataManagerStub()
        app.controller = ControllerStub()

        async def merge_source(screen, **kwargs):
            screen._queue_reassign("source-category", "target-category")
            app.data_manager.categories.pop("source-category")
            return True

        monkeypatch.setattr(app, "push_screen", merge_source)
        monkeypatch.setattr(app, "refresh_view", lambda *args, **kwargs: None)
        monkeypatch.setattr(app, "_restore_table_position", lambda *args: None)
        monkeypatch.setattr(app, "notify", lambda *args, **kwargs: None)

        await app._manage_categories()

        assert app.data_manager.pending_category_groups == {"Group": ["Target"]}
        assert app.data_manager.pending_category_group_edit_timestamps == {dependent_timestamp}
        assert app.data_manager.pending_category_groups_previous == {"Group": ["Source", "Target"]}


class TestGroupManagerPersistence:
    @pytest.mark.parametrize("pending_field", ["category", "merchant", "hide_from_reports"])
    async def test_group_changes_save_with_pending_transaction_edits(
        self, monkeypatch, pending_field
    ):
        from moneyflow.tui import app as app_module
        from moneyflow.tui.app import MoneyflowApp

        profile_dir = object()

        class DataManagerStub:
            categories = {
                "source-category": {"name": "Source", "group": "Renamed Group"},
                "target-category": {"name": "Target", "group": "Renamed Group"},
            }
            pending_edits = [
                TransactionEdit(
                    transaction_id="transaction-1",
                    field=pending_field,
                    old_value="old-value",
                    new_value="new-value",
                )
            ]
            pending_category_groups = None
            pending_category_group_edit_timestamps = set()
            pending_category_groups_previous = None
            category_groups_config = {}
            category_to_group = {}
            profile_dir = None

        app = MoneyflowApp()
        app.data_manager = DataManagerStub()
        app.data_manager.profile_dir = profile_dir

        async def finish_group_edit(screen, **kwargs):
            return True

        saved_groups = []
        monkeypatch.setattr(app, "push_screen", finish_group_edit)
        monkeypatch.setattr(app, "refresh_view", lambda *args, **kwargs: None)
        monkeypatch.setattr(app, "_restore_table_position", lambda *args: None)
        monkeypatch.setattr(app, "notify", lambda *args, **kwargs: None)
        monkeypatch.setattr(
            app_module,
            "save_categories_to_profile",
            lambda groups, profile_dir: saved_groups.append(groups),
        )

        await app._manage_groups()

        assert saved_groups == [{"Renamed Group": ["Source", "Target"]}]
        assert app.data_manager.pending_category_groups is None

    async def test_immediate_group_save_clears_stale_deferred_config(self, monkeypatch):
        from moneyflow.tui import app as app_module
        from moneyflow.tui.app import MoneyflowApp

        class DataManagerStub:
            categories = {"category": {"name": "Category", "group": "Current Group"}}
            pending_edits = []
            pending_category_groups = {"Stale Group": ["Category"]}
            pending_category_group_edit_timestamps = set()
            pending_category_groups_previous = {"Original Group": ["Category"]}
            category_groups_config = {}
            category_to_group = {}
            profile_dir = object()

        app = MoneyflowApp()
        app.data_manager = DataManagerStub()
        saved_groups = []

        async def finish_group_edit(screen, **kwargs):
            return True

        monkeypatch.setattr(app, "push_screen", finish_group_edit)
        monkeypatch.setattr(app, "refresh_view", lambda *args, **kwargs: None)
        monkeypatch.setattr(app, "_restore_table_position", lambda *args: None)
        monkeypatch.setattr(app, "notify", lambda *args, **kwargs: None)
        monkeypatch.setattr(
            app_module,
            "save_categories_to_profile",
            lambda groups, profile_dir: saved_groups.append(groups),
        )

        await app._manage_groups()

        assert saved_groups == [{"Current Group": ["Category"]}]
        assert app.data_manager.pending_category_groups is None
        assert app.data_manager.pending_category_groups_previous is None

    async def test_group_changes_survive_dependent_category_config_rollback(self, monkeypatch):
        from moneyflow.tui import app as app_module
        from moneyflow.tui.app import MoneyflowApp

        dependent_timestamp = datetime(2026, 1, 2)

        class DataManagerStub:
            categories = {"target-category": {"name": "Target", "group": "Renamed Group"}}
            pending_edits = []
            pending_category_groups = {"Renamed Group": ["Target"]}
            pending_category_group_edit_timestamps = {dependent_timestamp}
            pending_category_groups_previous = {"Original Group": ["Source", "Target"]}
            category_groups_config = {"Renamed Group": ["Target"]}
            category_to_group = {"Target": "Renamed Group"}
            profile_dir = object()

        app = MoneyflowApp()
        app.data_manager = DataManagerStub()
        saved_groups = []

        async def rename_group(screen, **kwargs):
            app.data_manager.categories["target-category"]["group"] = "Final Group"
            return True

        monkeypatch.setattr(app, "push_screen", rename_group)
        monkeypatch.setattr(app, "refresh_view", lambda *args, **kwargs: None)
        monkeypatch.setattr(app, "_restore_table_position", lambda *args: None)
        monkeypatch.setattr(app, "notify", lambda *args, **kwargs: None)
        monkeypatch.setattr(
            app_module,
            "save_categories_to_profile",
            lambda groups, profile_dir: saved_groups.append(groups),
        )

        await app._manage_groups()

        assert saved_groups == [{"Final Group": ["Source", "Target"]}]
        assert app.data_manager.pending_category_groups == {"Final Group": ["Target"]}
        assert app.data_manager.pending_category_groups_previous == {
            "Final Group": ["Source", "Target"]
        }

    def test_undo_last_category_edit_discards_dependent_config(self, monkeypatch):
        from moneyflow.tui import app as app_module
        from moneyflow.tui.app import MoneyflowApp

        deferred_groups = {"Renamed Group": ["Source", "Target"]}
        persisted_groups = {"Original Group": ["Source", "Target"]}
        ordinary_category_edit = TransactionEdit(
            transaction_id="transaction-0",
            field="category",
            old_value="another-source",
            new_value="another-target",
            timestamp=datetime(2026, 1, 1),
        )
        dependent_category_edit = TransactionEdit(
            transaction_id="transaction-1",
            field="category",
            old_value="source-category",
            new_value="target-category",
            timestamp=datetime(2026, 1, 2),
        )

        class DataManagerStub:
            pending_edits = [ordinary_category_edit, dependent_category_edit]
            pending_category_groups = deferred_groups
            pending_category_group_edit_timestamps = {dependent_category_edit.timestamp}
            pending_category_groups_previous = persisted_groups
            profile_dir = object()
            categories = {
                "target-category": {"name": "Target", "group": "Renamed Group"},
            }
            category_groups_config = deferred_groups
            category_to_group = {"Target": "Renamed Group"}

            def undo_last_batch(self):
                self.pending_edits.pop()
                return [dependent_category_edit]

            def _populate_categories_from_config(self):
                self.categories = {
                    "source-category": {"name": "Source", "group": "Original Group"},
                    "target-category": {"name": "Target", "group": "Original Group"},
                }

        app = MoneyflowApp()
        app.data_manager = DataManagerStub()
        saved_groups = []
        monkeypatch.setattr(app, "_save_table_position", lambda: None)
        monkeypatch.setattr(app, "_restore_table_position", lambda *args: None)
        monkeypatch.setattr(app, "refresh_view", lambda *args, **kwargs: None)
        monkeypatch.setattr(app, "notify", lambda *args, **kwargs: None)
        monkeypatch.setattr(
            app_module,
            "save_categories_to_profile",
            lambda groups, profile_dir: saved_groups.append(groups),
        )
        monkeypatch.setattr(
            app_module,
            "load_categories_from_profile",
            lambda profile_dir: persisted_groups,
            raising=False,
        )

        app.action_undo_pending_edits()

        assert saved_groups == []
        assert app.data_manager.pending_category_groups is None
        assert app.data_manager.pending_category_group_edit_timestamps == set()
        assert app.data_manager.pending_category_groups_previous is None
        assert app.data_manager.category_groups_config == persisted_groups
        assert "source-category" in app.data_manager.categories
        assert app.data_manager.pending_edits == [ordinary_category_edit]
