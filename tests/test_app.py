"""Tests for MoneyflowApp."""

import asyncio
from datetime import datetime
from unittest.mock import AsyncMock

import polars as pl
import pytest

from moneyflow.data.data_manager import DeferredCategoryChange
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
        self.edits_enabled = True

    def set_edits_enabled(self, enabled: bool) -> None:
        self.edits_enabled = enabled

    def refresh_view(self, force_rebuild: bool = True) -> None:
        self.refresh_calls.append(force_rebuild)


class TestQuitConfirmation:
    """Quit confirmation includes retryable category configuration changes."""

    @pytest.mark.asyncio
    async def test_pending_category_groups_are_reported_as_unsaved(self, monkeypatch):
        from moneyflow.tui.app import MoneyflowApp

        class DataManagerStub:
            pending_category_groups = {"Example Group": ["Example Category"]}

            @staticmethod
            def get_stats():
                return {"pending_changes": 0}

        app = MoneyflowApp()
        app.data_manager = DataManagerStub()
        confirmations = []

        async def inspect_confirmation(screen, *, wait_for_dismiss):
            confirmations.append((screen.has_unsaved_changes, wait_for_dismiss))
            return False

        monkeypatch.setattr(app, "push_screen", inspect_confirmation)

        await app._confirm_and_quit()

        assert confirmations == [(True, True)]


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

    async def test_background_refresh_preserves_pending_category_structure(self, monkeypatch):
        from moneyflow.tui.app import MoneyflowApp

        class PendingCategoryDataManager(FetchingDataManager):
            def __init__(self):
                super().__init__(pl.DataFrame({"id": ["new"]}))
                self.category_groups_config = {"Persisted Group": ["Old Category"]}
                self.pending_category_groups = {"Pending Group": ["New Category"]}
                self.pending_category_changes = [object()]

            async def fetch_all_data(self):
                return self._df, {}, {}

            def _populate_categories_from_config(self) -> None:
                group_name, category_names = next(iter(self.category_groups_config.items()))
                self.categories = {
                    "new_category": {
                        "name": category_names[0],
                        "group": group_name,
                    }
                }

        app = MoneyflowApp(backend=RefreshingSimpleFinBackend())
        app.data_manager = PendingCategoryDataManager()
        app.controller = RefreshController()

        monkeypatch.setattr(app, "notify", lambda *args, **kwargs: None)
        monkeypatch.setattr(app, "_save_last_update_time", lambda: None)
        monkeypatch.setattr(app, "_refresh_subtitle", lambda: None)
        monkeypatch.setattr(app, "_save_table_position", lambda: None)
        monkeypatch.setattr(app, "_restore_table_position", lambda saved: None)

        await app._simplefin_background_refresh()

        assert app.data_manager.category_groups_config == {"Pending Group": ["New Category"]}
        assert app.data_manager.categories["new_category"] == {
            "name": "New Category",
            "group": "Pending Group",
        }

    async def test_background_refresh_reloads_after_zero_addition_migration(self, monkeypatch):
        """An ID-only SQLite migration must still replace stale in-memory rows."""
        from moneyflow.tui.app import MoneyflowApp

        class MigratingSimpleFinBackend(RefreshingSimpleFinBackend):
            async def refresh(self) -> int:
                return 0

        migrated_df = pl.DataFrame({"id": ["encoded-account:encoded-transaction"]})
        app = MoneyflowApp(backend=MigratingSimpleFinBackend())
        app.data_manager = FetchingDataManager(migrated_df)
        app.controller = RefreshController()
        app.state.transactions_df = pl.DataFrame({"id": ["legacy-account:transaction"]})
        app.state.selected_ids.add("legacy-account:transaction")
        app.state.selected_group_keys.add("Example Merchant")

        monkeypatch.setattr(app, "notify", lambda *args, **kwargs: None)
        monkeypatch.setattr(app, "_save_last_update_time", lambda: None)
        monkeypatch.setattr(app, "_refresh_subtitle", lambda: None)
        monkeypatch.setattr(app, "_save_table_position", lambda: None)
        monkeypatch.setattr(app, "_restore_table_position", lambda saved: None)

        await app._simplefin_background_refresh()

        assert app.state.transactions_df["id"].to_list() == ["encoded-account:encoded-transaction"]
        assert app.state.selected_ids == set()
        assert app.state.selected_group_keys == set()
        assert app.controller.refresh_calls == [False]

    async def test_delete_tracks_exact_successful_ids(self, monkeypatch):
        """A failed deletion must not remove another transaction from local state."""
        from moneyflow.tui.app import MoneyflowApp

        app = MoneyflowApp()
        app.controller = RefreshController()
        app.state.transactions_df = pl.DataFrame({"id": ["first", "second"]})
        app.state.current_data = app.state.transactions_df
        app.state.selected_ids.update({"first", "second"})
        app.data_manager = type(
            "DataManager",
            (),
            {"df": app.state.transactions_df, "categories": {}, "category_groups": []},
        )()
        app.cache_manager = None

        class TaskRunner:
            @staticmethod
            async def delete_with_retry(transaction_id):
                return transaction_id == "second"

        app.task_runner = TaskRunner()
        notifications = []
        monkeypatch.setattr(
            app, "query_one", lambda *args, **kwargs: type("Table", (), {"cursor_row": 0})()
        )
        monkeypatch.setattr(app, "push_screen", AsyncMock(return_value=True))
        monkeypatch.setattr(app, "_save_table_position", lambda: None)
        monkeypatch.setattr(app, "_restore_table_position", lambda saved: None)
        monkeypatch.setattr(app, "refresh_view", lambda: None)
        monkeypatch.setattr(
            app,
            "notify",
            lambda message, **kwargs: notifications.append((message, kwargs.get("severity"))),
        )

        await app._delete_transaction()

        assert app.data_manager.df["id"].to_list() == ["first"]
        assert ("Deleted 1, failed 1", "warning") in notifications

    async def test_background_refresh_blocks_edits_queued_while_migrating(self, monkeypatch):
        """An edit started during refresh must not retain a pre-migration ID."""
        from moneyflow.tui.app import MoneyflowApp

        refresh_started = asyncio.Event()
        release_refresh = asyncio.Event()

        class BlockingMigrationBackend(RefreshingSimpleFinBackend):
            async def refresh(self) -> int:
                refresh_started.set()
                await release_refresh.wait()
                return 0

        class EditingRefreshController(RefreshController):
            def __init__(self):
                super().__init__()
                self.edits_enabled = True
                self.queued_ids = []

            def set_edits_enabled(self, enabled: bool) -> None:
                self.edits_enabled = enabled

            def queue_edit(self, transaction_id: str) -> int:
                if not self.edits_enabled:
                    return 0
                self.queued_ids.append(transaction_id)
                return 1

        app = MoneyflowApp(backend=BlockingMigrationBackend())
        app.data_manager = FetchingDataManager(
            pl.DataFrame({"id": ["encoded-account:encoded-transaction"]})
        )
        app.controller = EditingRefreshController()

        monkeypatch.setattr(app, "notify", lambda *args, **kwargs: None)
        monkeypatch.setattr(app, "_save_last_update_time", lambda: None)
        monkeypatch.setattr(app, "_refresh_subtitle", lambda: None)
        monkeypatch.setattr(app, "_save_table_position", lambda: None)
        monkeypatch.setattr(app, "_restore_table_position", lambda saved: None)

        refresh_task = asyncio.create_task(app._simplefin_background_refresh())
        await refresh_started.wait()
        assert app.can_edit_transaction_snapshot(0) is False
        queued = app.controller.queue_edit("legacy-account:transaction")
        release_refresh.set()
        await refresh_task

        assert queued == 0
        assert app.controller.queued_ids == []
        assert app.controller.edits_enabled is True
        assert app.can_edit_transaction_snapshot(0) is False

    async def test_reload_failure_after_refresh_keeps_stale_edits_disabled(self, monkeypatch):
        """Persisted ID changes invalidate the view even when reloading it fails."""
        from moneyflow.tui.app import MoneyflowApp

        class FailingReloadDataManager(FetchingDataManager):
            async def fetch_all_data(self):
                raise RuntimeError("reload failed")

        app = MoneyflowApp(backend=RefreshingSimpleFinBackend())
        app.data_manager = FailingReloadDataManager(pl.DataFrame({"id": ["new-id"]}))
        app.controller = RefreshController()
        notifications = []
        monkeypatch.setattr(app, "notify", lambda message, **kwargs: notifications.append(message))

        await app._simplefin_background_refresh()

        assert app._simplefin_refresh_generation == 1
        assert app.controller.edits_enabled is False
        assert app.can_edit_transaction_snapshot(0) is False
        assert any("reload failed" in message for message in notifications)


class TestBackendTaskRunnerDeletion:
    async def test_false_backend_result_is_propagated(self):
        from moneyflow.tui.backend_task_runner import BackendTaskRunner

        class Backend:
            @staticmethod
            async def delete_transaction(transaction_id):
                return False

        app = type("App", (), {"backend": Backend()})()

        assert await BackendTaskRunner(app).delete_with_retry("missing") is False


class TestSimpleFinStaleDialogs:
    async def test_merchant_dialog_rejects_refreshed_snapshot(self, monkeypatch):
        from moneyflow.tui.app import MoneyflowApp

        context = type(
            "EditContext",
            (),
            {
                "transactions": pl.DataFrame({"id": ["legacy-id"], "merchant": ["Old"]}),
                "current_value": "Old",
                "transaction_count": 1,
                "is_multi_select": False,
            },
        )()

        class Controller:
            edits_enabled = True
            edit_calls = []

            @staticmethod
            def determine_edit_context(field, cursor_row):
                return context

            @staticmethod
            def get_merchant_suggestions():
                return []

            def edit_merchant_current_selection(self, *args, **kwargs):
                self.edit_calls.append((args, kwargs))
                return 1

        app = MoneyflowApp()
        app.controller = Controller()
        app.data_manager = object()
        notifications = []
        monkeypatch.setattr(
            app, "query_one", lambda *args, **kwargs: type("Table", (), {"cursor_row": 0})()
        )

        async def refresh_while_open(screen, **kwargs):
            app._simplefin_refresh_generation += 1
            return "New"

        monkeypatch.setattr(app, "push_screen", refresh_while_open)
        monkeypatch.setattr(
            app,
            "notify",
            lambda message, **kwargs: notifications.append((message, kwargs.get("severity"))),
        )

        await app._edit_merchant()

        assert app.controller.edit_calls == []
        assert notifications == [
            ("Transactions refreshed; reopen the editor before making changes.", "warning")
        ]

    async def test_category_dialog_rejects_refreshed_snapshot(self, monkeypatch):
        from moneyflow.tui.app import MoneyflowApp

        context = type(
            "EditContext",
            (),
            {
                "transactions": pl.DataFrame({"id": ["legacy-id"]}),
                "current_value": "source",
                "transaction_count": 1,
                "is_multi_select": False,
            },
        )()

        class Controller:
            edits_enabled = True
            edit_calls = []

            @staticmethod
            def determine_edit_context(field, cursor_row):
                return context

            def edit_category_current_selection(self, *args, **kwargs):
                self.edit_calls.append((args, kwargs))
                return 1

        app = MoneyflowApp()
        app.controller = Controller()
        app.backend = type("Backend", (), {"supports_category_sync": True})()
        app.data_manager = type("DataManager", (), {"categories": {}})()
        notifications = []
        monkeypatch.setattr(
            app, "query_one", lambda *args, **kwargs: type("Table", (), {"cursor_row": 0})()
        )

        async def refresh_while_open(screen, **kwargs):
            app._simplefin_refresh_generation += 1
            return "target"

        monkeypatch.setattr(app, "push_screen", refresh_while_open)
        monkeypatch.setattr(
            app,
            "notify",
            lambda message, **kwargs: notifications.append((message, kwargs.get("severity"))),
        )

        await app._edit_category()

        assert app.controller.edit_calls == []
        assert notifications == [
            ("Transactions refreshed; reopen the editor before making changes.", "warning")
        ]

    async def test_group_dialog_rejects_refreshed_categories(self, monkeypatch):
        from moneyflow.tui import app as app_module
        from moneyflow.tui.app import MoneyflowApp

        class DataManager:
            categories = {"old": {"name": "Old", "group": "Old Group"}}
            category_groups_config = {"Old Group": ["Old"]}
            category_to_group = {"Old": "Old Group"}
            pending_category_changes = []
            pending_edits = []
            pending_category_groups = None
            profile_dir = object()

        app = MoneyflowApp()
        app.data_manager = DataManager()
        notifications = []
        saved_groups = []

        async def refresh_while_open(screen, **kwargs):
            app.data_manager.categories = {"new": {"name": "New", "group": "Current Group"}}
            app._simplefin_refresh_generation += 1
            return True

        monkeypatch.setattr(app, "push_screen", refresh_while_open)
        monkeypatch.setattr(
            app_module,
            "save_categories_to_profile",
            lambda groups, profile_dir: saved_groups.append(groups) or True,
        )
        monkeypatch.setattr(
            app,
            "notify",
            lambda message, **kwargs: notifications.append((message, kwargs.get("severity"))),
        )

        await app._manage_groups()

        assert saved_groups == []
        assert app.data_manager.category_groups_config == {"Old Group": ["Old"]}
        assert notifications == [
            ("Categories refreshed; reopen the group manager before making changes.", "warning")
        ]

    async def test_nested_group_picker_rejects_refreshed_snapshot(self, monkeypatch):
        from moneyflow.tui.app import MoneyflowApp

        context = type(
            "EditContext",
            (),
            {
                "transactions": pl.DataFrame({"id": ["legacy-id"]}),
                "current_value": "source",
                "transaction_count": 1,
                "is_multi_select": False,
            },
        )()

        class Controller:
            edits_enabled = True
            edit_calls = []

            @staticmethod
            def determine_edit_context(field, cursor_row):
                return context

            def edit_category_current_selection(self, *args, **kwargs):
                self.edit_calls.append((args, kwargs))
                return 1

        app = MoneyflowApp()
        app.controller = Controller()
        app.backend = type("Backend", (), {"supports_category_sync": False})()
        app.data_manager = type(
            "DataManager",
            (),
            {
                "categories": {"source": {"name": "Source", "group": "Example Group"}},
                "profile_dir": object(),
            },
        )()
        choices = iter(["__new__:New Category", "Example Group"])
        notifications = []
        monkeypatch.setattr(
            app, "query_one", lambda *args, **kwargs: type("Table", (), {"cursor_row": 0})()
        )

        async def choose(screen, **kwargs):
            choice = next(choices)
            if choice == "Example Group":
                app._simplefin_refresh_generation += 1
            return choice

        monkeypatch.setattr(app, "push_screen", choose)
        monkeypatch.setattr(
            app,
            "notify",
            lambda message, **kwargs: notifications.append((message, kwargs.get("severity"))),
        )

        await app._edit_category()

        assert "new_category" not in app.data_manager.categories
        assert app.controller.edit_calls == []
        assert notifications == [
            ("Transactions refreshed; reopen the editor before making changes.", "warning")
        ]

    async def test_delete_dialog_rejects_refreshed_snapshot(self, monkeypatch):
        from moneyflow.tui.app import MoneyflowApp

        app = MoneyflowApp()
        app.controller = RefreshController()
        app.state.transactions_df = pl.DataFrame({"id": ["legacy-id"]})
        app.state.current_data = app.state.transactions_df
        app.data_manager = type("DataManager", (), {"df": app.state.transactions_df})()
        delete_calls = []

        class TaskRunner:
            @staticmethod
            async def delete_with_retry(transaction_id):
                delete_calls.append(transaction_id)
                return True

        app.task_runner = TaskRunner()
        notifications = []
        monkeypatch.setattr(
            app, "query_one", lambda *args, **kwargs: type("Table", (), {"cursor_row": 0})()
        )

        async def refresh_while_open(screen, **kwargs):
            app._simplefin_refresh_generation += 1
            return True

        monkeypatch.setattr(app, "push_screen", refresh_while_open)
        monkeypatch.setattr(
            app,
            "notify",
            lambda message, **kwargs: notifications.append((message, kwargs.get("severity"))),
        )

        await app._delete_transaction()

        assert delete_calls == []
        assert notifications == [
            ("Transactions refreshed; reopen the view before deleting.", "warning")
        ]


class TestLocalCategoryCreation:
    async def test_creation_rebases_pending_structural_changes(self, monkeypatch):
        from moneyflow.tui import app as app_module
        from moneyflow.tui.app import MoneyflowApp

        dependent_edit = TransactionEdit(
            transaction_id="transaction-1",
            field="category",
            old_value="source-category",
            new_value="target-category",
            timestamp=datetime(2026, 1, 2),
        )

        class ControllerStub:
            @staticmethod
            def determine_edit_context(field, cursor_row):
                return type(
                    "EditContext",
                    (),
                    {
                        "transactions": pl.DataFrame({"id": ["transaction-2"]}),
                        "transaction_count": 1,
                        "is_multi_select": False,
                    },
                )()

            @staticmethod
            def edit_category_current_selection(*args, **kwargs):
                return 1

        class DataManagerStub:
            categories = {"target-category": {"name": "Target", "group": "Group"}}
            pending_edits = [dependent_edit]
            pending_category_groups = {"Group": ["Target"]}
            pending_category_changes = [
                DeferredCategoryChange(
                    before_groups={"Group": ["Source", "Target"]},
                    after_groups={"Group": ["Target"]},
                    before_edits=[],
                    dependent_timestamps={dependent_edit.timestamp},
                )
            ]
            category_groups_config = {"Group": ["Target"]}
            category_to_group = {"Target": "Group"}
            profile_dir = object()

        app = MoneyflowApp()
        app.controller = ControllerStub()
        app.backend = type("Backend", (), {"supports_category_sync": False})()
        app.data_manager = DataManagerStub()
        choices = iter(["__new__:New Category", "Group"])
        saved_groups = []
        monkeypatch.setattr(
            app, "query_one", lambda *args, **kwargs: type("Table", (), {"cursor_row": 0})()
        )

        async def choose(screen, **kwargs):
            return next(choices)

        monkeypatch.setattr(app, "push_screen", choose)
        monkeypatch.setattr(app, "_save_table_position", lambda: None)
        monkeypatch.setattr(app, "_restore_table_position", lambda *args: None)
        monkeypatch.setattr(app, "refresh_view", lambda *args, **kwargs: None)
        monkeypatch.setattr(app, "notify", lambda *args, **kwargs: None)
        monkeypatch.setattr(
            app_module,
            "save_categories_to_profile",
            lambda groups, profile_dir: saved_groups.append(groups) or True,
        )

        await app._edit_category()

        expected_base = {"Group": ["Source", "Target", "New Category"]}
        assert saved_groups == [expected_base]
        assert app.data_manager.pending_category_changes[0].before_groups == expected_base
        assert app.data_manager.pending_category_groups == {"Group": ["Target", "New Category"]}

    async def test_creation_direct_save_clears_stale_deferred_state(self, monkeypatch):
        """A successful direct save (no pending structural changes) must
        flush retained aliases and clear a config snapshot retained from an
        earlier failed save — otherwise the next commit re-applies the stale
        snapshot and drops the category just created."""
        from moneyflow.tui import app as app_module
        from moneyflow.tui.app import MoneyflowApp

        class ControllerStub:
            @staticmethod
            def determine_edit_context(field, cursor_row):
                return type(
                    "EditContext",
                    (),
                    {
                        "transactions": pl.DataFrame({"id": ["transaction-2"]}),
                        "transaction_count": 1,
                        "is_multi_select": False,
                    },
                )()

            @staticmethod
            def edit_category_current_selection(*args, **kwargs):
                return 1

        class DataManagerStub:
            categories = {"target-category": {"name": "Target", "group": "Group"}}
            pending_edits = []
            pending_category_groups = {"Group": ["Stale"]}
            pending_category_aliases = [("cat_old", "cat_new", "New")]
            pending_category_changes = []
            category_groups_config = {"Group": ["Target"]}
            category_to_group = {"Target": "Group"}
            profile_dir = object()

        recorded = []
        app = MoneyflowApp()
        app.controller = ControllerStub()
        app.backend = type(
            "Backend",
            (),
            {
                "supports_category_sync": False,
                "record_category_alias": staticmethod(lambda *args: recorded.append(args)),
            },
        )()
        app.data_manager = DataManagerStub()
        choices = iter(["__new__:New Category", "Group"])
        saved_groups = []
        monkeypatch.setattr(
            app, "query_one", lambda *args, **kwargs: type("Table", (), {"cursor_row": 0})()
        )

        async def choose(screen, **kwargs):
            return next(choices)

        monkeypatch.setattr(app, "push_screen", choose)
        monkeypatch.setattr(app, "_save_table_position", lambda: None)
        monkeypatch.setattr(app, "_restore_table_position", lambda *args: None)
        monkeypatch.setattr(app, "refresh_view", lambda *args, **kwargs: None)
        monkeypatch.setattr(app, "notify", lambda *args, **kwargs: None)
        monkeypatch.setattr(
            app_module,
            "save_categories_to_profile",
            lambda groups, profile_dir: saved_groups.append(groups) or True,
        )

        await app._edit_category()

        assert saved_groups == [{"Group": ["Target", "New Category"]}]
        # The retained alias is flushed, followed by a self-referencing reset
        # cancelling any alias left on the recreated id.
        assert recorded == [
            ("cat_old", "cat_new", "New"),
            ("new_category", "new_category", ""),
        ]
        assert app.data_manager.pending_category_aliases == []
        assert app.data_manager.pending_category_groups is None

    async def test_rejects_normalized_id_collision(self, monkeypatch):
        from moneyflow.tui.app import MoneyflowApp

        categories = {
            "food_dining": {
                "name": "Food & Dining",
                "group": "Expenses",
                "group_id": "expenses",
                "group_type": "",
            }
        }

        class ControllerStub:
            @staticmethod
            def determine_edit_context(field, cursor_row):
                return type(
                    "EditContext",
                    (),
                    {
                        "transactions": pl.DataFrame({"id": ["transaction-1"]}),
                        "transaction_count": 1,
                        "is_multi_select": False,
                    },
                )()

        app = MoneyflowApp()
        app.controller = ControllerStub()
        app.backend = type("Backend", (), {"supports_category_sync": False})()
        app.data_manager = type("DataManager", (), {"categories": categories})()
        notifications = []

        monkeypatch.setattr(
            app,
            "query_one",
            lambda *args, **kwargs: type("Table", (), {"cursor_row": 0})(),
        )

        async def choose_colliding_category(screen, **kwargs):
            return "__new__:Food Dining"

        monkeypatch.setattr(app, "push_screen", choose_colliding_category)
        monkeypatch.setattr(
            app,
            "notify",
            lambda message, **kwargs: notifications.append((message, kwargs.get("severity"))),
        )

        await app._edit_category()

        assert set(categories) == {"food_dining"}
        assert notifications == [("A category with an equivalent name already exists.", "error")]

    async def test_rejects_name_collision_with_csv_import_style_id(self, monkeypatch):
        """CSV-imported categories carry "cat_"-prefixed ids. Creating a
        category with an equivalent name must still be rejected even though
        the generated id differs from the stored one."""
        from moneyflow.tui.app import MoneyflowApp

        categories = {
            "cat_food_dining": {
                "name": "Food Dining",
                "group": "Expenses",
                "group_id": "expenses",
                "group_type": "",
            }
        }

        class ControllerStub:
            @staticmethod
            def determine_edit_context(field, cursor_row):
                return type(
                    "EditContext",
                    (),
                    {
                        "transactions": pl.DataFrame({"id": ["transaction-1"]}),
                        "transaction_count": 1,
                        "is_multi_select": False,
                    },
                )()

        app = MoneyflowApp()
        app.controller = ControllerStub()
        app.backend = type("Backend", (), {"supports_category_sync": False})()
        app.data_manager = type("DataManager", (), {"categories": categories})()
        notifications = []

        monkeypatch.setattr(
            app,
            "query_one",
            lambda *args, **kwargs: type("Table", (), {"cursor_row": 0})(),
        )

        async def choose_colliding_category(screen, **kwargs):
            return "__new__:Food & Dining"

        monkeypatch.setattr(app, "push_screen", choose_colliding_category)
        monkeypatch.setattr(
            app,
            "notify",
            lambda message, **kwargs: notifications.append((message, kwargs.get("severity"))),
        )

        await app._edit_category()

        assert set(categories) == {"cat_food_dining"}
        assert notifications == [("A category with an equivalent name already exists.", "error")]

    async def test_aborts_creation_when_category_config_cannot_be_saved(self, monkeypatch):
        from moneyflow.tui import app as app_module
        from moneyflow.tui.app import MoneyflowApp

        categories = {}

        class ControllerStub:
            edit_calls = []

            @staticmethod
            def determine_edit_context(field, cursor_row):
                return type(
                    "EditContext",
                    (),
                    {
                        "transactions": pl.DataFrame({"id": ["transaction-1"]}),
                        "transaction_count": 1,
                        "is_multi_select": False,
                    },
                )()

            def edit_category_current_selection(self, category_id, cursor_row):
                self.edit_calls.append(category_id)
                return 1

        app = MoneyflowApp()
        app.controller = ControllerStub()
        app.backend = type("Backend", (), {"supports_category_sync": False})()
        app.data_manager = type(
            "DataManager",
            (),
            {
                "categories": categories,
                "profile_dir": object(),
                "category_groups_config": {},
                "category_to_group": {},
            },
        )()
        notifications = []
        choices = iter(["__new__:New Category", "Group"])
        monkeypatch.setattr(
            app, "query_one", lambda *args, **kwargs: type("Table", (), {"cursor_row": 0})()
        )

        async def choose(screen, **kwargs):
            return next(choices)

        monkeypatch.setattr(app, "push_screen", choose)
        monkeypatch.setattr(app_module, "save_categories_to_profile", lambda *args, **kwargs: False)
        monkeypatch.setattr(
            app,
            "notify",
            lambda message, **kwargs: notifications.append((message, kwargs.get("severity"))),
        )

        await app._edit_category()

        assert categories == {}
        assert app.controller.edit_calls == []
        assert notifications[-1][1] == "error"


class TestCategoryManagerSource:
    def test_failed_rebase_save_keeps_rollback_snapshots_unchanged(self, monkeypatch):
        from moneyflow.tui import app as app_module
        from moneyflow.tui.app import MoneyflowApp

        change = DeferredCategoryChange(
            before_groups={"Group": ["Source", "Target"]},
            after_groups={"Group": ["Target"]},
            before_edits=[],
            dependent_timestamps={datetime(2026, 1, 2)},
        )

        class DataManagerStub:
            pending_category_changes = [change]
            pending_category_groups = {"Group": ["Target"]}
            category_groups_config = {"Group": ["Target", "New Category"]}
            category_to_group = {"Target": "Group", "New Category": "Group"}
            categories = {
                "target": {"name": "Target", "group": "Group"},
                "new_category": {"name": "New Category", "group": "Group"},
            }
            profile_dir = object()

            def _populate_categories_from_config(self):
                self.categories = {
                    "target": {"name": "Target", "group": "Group"},
                }

        app = MoneyflowApp()
        app.data_manager = DataManagerStub()
        monkeypatch.setattr(app_module, "save_categories_to_profile", lambda *args, **kwargs: False)
        monkeypatch.setattr(app, "notify", lambda *args, **kwargs: None)
        monkeypatch.setattr(app, "refresh_view", lambda *args, **kwargs: None)

        saved = app._rebase_pending_category_changes(
            {"Group": ["Target"]},
            {"Group": ["Target", "New Category"]},
            propagate_group_moves=False,
        )

        assert saved is False
        assert change.before_groups == {"Group": ["Source", "Target"]}
        assert change.after_groups == {"Group": ["Target"]}
        assert app.data_manager.pending_category_groups == {"Group": ["Target"]}
        assert app.data_manager.category_groups_config == {"Group": ["Target"]}
        assert "new_category" not in app.data_manager.categories

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
            pending_category_changes = []
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
        assert len(app.data_manager.pending_category_changes) == 1
        change = app.data_manager.pending_category_changes[0]
        assert change.dependent_timestamps == {dependent_timestamp}
        assert change.before_groups == {"Group": ["Source", "Target"]}
        assert change.after_groups == {"Group": ["Target"]}

    async def test_independent_category_creation_rebases_pending_history(self, monkeypatch):
        from moneyflow.tui import app as app_module
        from moneyflow.tui.app import MoneyflowApp

        dependent_edit = TransactionEdit(
            transaction_id="transaction-1",
            field="category",
            old_value="source-category",
            new_value="target-category",
            timestamp=datetime(2026, 1, 2),
        )
        complete_df = pl.DataFrame({"id": ["transaction-1"], "category_id": ["source-category"]})

        class DataManagerStub:
            categories = {"target-category": {"name": "Target", "group": "Group"}}
            pending_edits = [dependent_edit]
            pending_category_groups = {"Group": ["Target"]}
            pending_category_changes = [
                DeferredCategoryChange(
                    before_groups={"Group": ["Source", "Target"]},
                    after_groups={"Group": ["Target"]},
                    before_edits=[],
                    dependent_timestamps={dependent_edit.timestamp},
                )
            ]
            category_groups_config = {"Group": ["Target"]}
            category_to_group = {"Target": "Group"}
            profile_dir = object()

            async def fetch_unfiltered_transactions(self):
                return complete_df

        app = MoneyflowApp()
        app.data_manager = DataManagerStub()
        saved_groups = []

        async def create_category(screen, **kwargs):
            app.data_manager.categories["new-category"] = {
                "name": "New Category",
                "group": "Group",
            }
            return True

        monkeypatch.setattr(app, "push_screen", create_category)
        monkeypatch.setattr(app, "refresh_view", lambda *args, **kwargs: None)
        monkeypatch.setattr(app, "_restore_table_position", lambda *args: None)
        monkeypatch.setattr(app, "notify", lambda *args, **kwargs: None)
        monkeypatch.setattr(
            app_module,
            "save_categories_to_profile",
            lambda groups, profile_dir: saved_groups.append(groups) or True,
        )

        await app._manage_categories()

        expected_base = {"Group": ["Source", "Target", "New Category"]}
        assert saved_groups == [expected_base]
        assert app.data_manager.pending_category_changes[0].before_groups == expected_base
        assert app.data_manager.pending_category_groups == {"Group": ["Target", "New Category"]}

    async def test_undo_second_merge_restores_first_merge_boundary(self, monkeypatch):
        from moneyflow.tui.app import MoneyflowApp

        timestamps = [datetime(2026, 1, 1), datetime(2026, 1, 2)]
        complete_df = pl.DataFrame(
            {
                "id": ["transaction-a", "transaction-b"],
                "category_id": ["category-a", "category-b"],
            }
        )

        class DataManagerStub:
            categories = {
                "category-a": {"name": "A", "group": "Group"},
                "category-b": {"name": "B", "group": "Group"},
                "category-c": {"name": "C", "group": "Group"},
            }
            pending_edits = []
            pending_category_groups = None
            pending_category_changes = []
            category_groups_config = {"Group": ["A", "B", "C"]}
            category_to_group = {"A": "Group", "B": "Group", "C": "Group"}
            profile_dir = object()

            async def fetch_unfiltered_transactions(self):
                return complete_df

            def undo_last_batch(self):
                last_timestamp = self.pending_edits[-1].timestamp
                undone = [edit for edit in self.pending_edits if edit.timestamp == last_timestamp]
                self.pending_edits = [
                    edit for edit in self.pending_edits if edit.timestamp != last_timestamp
                ]
                return undone

            def _populate_categories_from_config(self):
                self.categories = {
                    name.lower(): {"name": name, "group": group}
                    for group, names in self.category_groups_config.items()
                    for name in names
                }

        class ControllerStub:
            call_index = 0

            def queue_category_reassignment(self, source_df, source_id, target_id):
                for edit in app.data_manager.pending_edits:
                    if edit.field == "category" and edit.new_value == source_id:
                        edit.new_value = target_id
                matching = source_df.filter(pl.col("category_id") == source_id)
                timestamp = timestamps[self.call_index]
                self.call_index += 1
                for transaction_id in matching["id"].to_list():
                    app.data_manager.pending_edits.append(
                        TransactionEdit(
                            transaction_id=transaction_id,
                            field="category",
                            old_value=source_id,
                            new_value=target_id,
                            timestamp=timestamp,
                        )
                    )

        app = MoneyflowApp()
        app.data_manager = DataManagerStub()
        app.controller = ControllerStub()
        operations = [
            ("category-a", "category-b"),
            ("category-b", "category-c"),
        ]

        async def merge_category(screen, **kwargs):
            source_id, target_id = operations.pop(0)
            screen._queue_reassign(source_id, target_id)
            app.data_manager.categories.pop(source_id)
            return True

        monkeypatch.setattr(app, "push_screen", merge_category)
        monkeypatch.setattr(app, "refresh_view", lambda *args, **kwargs: None)
        monkeypatch.setattr(app, "_save_table_position", lambda: None)
        monkeypatch.setattr(app, "_restore_table_position", lambda *args: None)
        monkeypatch.setattr(app, "notify", lambda *args, **kwargs: None)

        await app._manage_categories()
        await app._manage_categories()
        app.action_undo_pending_edits()

        assert len(app.data_manager.pending_category_changes) == 1
        assert app.data_manager.pending_category_groups == {"Group": ["B", "C"]}
        assert len(app.data_manager.pending_edits) == 1
        assert app.data_manager.pending_edits[0].new_value == "category-b"

    async def test_retarget_only_merge_undo_precedes_unrelated_edit(self, monkeypatch):
        from moneyflow.tui.app import MoneyflowApp

        category_edit = TransactionEdit(
            transaction_id="transaction-a",
            field="category",
            old_value="category-a",
            new_value="category-b",
            timestamp=datetime(2026, 1, 1),
        )
        unrelated_edit = TransactionEdit(
            transaction_id="transaction-a",
            field="merchant",
            old_value="Old Merchant",
            new_value="New Merchant",
            timestamp=datetime(2026, 1, 2),
        )
        complete_df = pl.DataFrame({"id": ["transaction-a"], "category_id": ["category-a"]})

        class DataManagerStub:
            categories = {
                "category-b": {"name": "B", "group": "Group"},
                "category-c": {"name": "C", "group": "Group"},
            }
            pending_edits = [category_edit, unrelated_edit]
            pending_category_groups = None
            pending_category_changes = []
            category_groups_config = {"Group": ["B", "C"]}
            category_to_group = {"B": "Group", "C": "Group"}
            profile_dir = object()

            async def fetch_unfiltered_transactions(self):
                return complete_df

            def undo_last_batch(self):
                edit = self.pending_edits.pop()
                return [edit]

            def _populate_categories_from_config(self):
                self.categories = {
                    name.lower(): {"name": name, "group": group}
                    for group, names in self.category_groups_config.items()
                    for name in names
                }

        class ControllerStub:
            def queue_category_reassignment(self, source_df, source_id, target_id):
                for edit in app.data_manager.pending_edits:
                    if edit.field == "category" and edit.new_value == source_id:
                        edit.new_value = target_id

        app = MoneyflowApp()
        app.data_manager = DataManagerStub()
        app.controller = ControllerStub()

        async def merge_empty_category(screen, **kwargs):
            screen._queue_reassign("category-b", "category-c")
            app.data_manager.categories.pop("category-b")
            return True

        monkeypatch.setattr(app, "push_screen", merge_empty_category)
        monkeypatch.setattr(app, "refresh_view", lambda *args, **kwargs: None)
        monkeypatch.setattr(app, "_save_table_position", lambda: None)
        monkeypatch.setattr(app, "_restore_table_position", lambda *args: None)
        monkeypatch.setattr(app, "notify", lambda *args, **kwargs: None)

        await app._manage_categories()
        app.action_undo_pending_edits()

        assert app.data_manager.pending_category_changes == []
        assert len(app.data_manager.pending_edits) == 2
        assert app.data_manager.pending_edits[0].new_value == "category-b"
        assert app.data_manager.pending_edits[1] == unrelated_edit

    async def test_independent_category_move_does_not_move_hidden_source(self, monkeypatch):
        from moneyflow.tui import app as app_module
        from moneyflow.tui.app import MoneyflowApp

        dependent_edit = TransactionEdit(
            transaction_id="transaction-a",
            field="category",
            old_value="category-a",
            new_value="category-b",
            timestamp=datetime(2026, 1, 1),
        )
        complete_df = pl.DataFrame({"id": ["transaction-a"], "category_id": ["category-a"]})

        class DataManagerStub:
            categories = {
                "category-b": {"name": "B", "group": "Group G"},
                "category-c": {"name": "C", "group": "Group H"},
            }
            pending_edits = [dependent_edit]
            pending_category_groups = {"Group G": ["B"], "Group H": ["C"]}
            pending_category_changes = [
                DeferredCategoryChange(
                    before_groups={"Group G": ["A", "B"], "Group H": ["C"]},
                    after_groups={"Group G": ["B"], "Group H": ["C"]},
                    before_edits=[],
                    dependent_timestamps={dependent_edit.timestamp},
                )
            ]
            category_groups_config = {"Group G": ["B"], "Group H": ["C"]}
            category_to_group = {"B": "Group G", "C": "Group H"}
            profile_dir = object()

            async def fetch_unfiltered_transactions(self):
                return complete_df

        app = MoneyflowApp()
        app.data_manager = DataManagerStub()
        saved_groups = []

        async def move_category(screen, **kwargs):
            app.data_manager.categories["category-b"]["group"] = "Group H"
            return True

        monkeypatch.setattr(app, "push_screen", move_category)
        monkeypatch.setattr(app, "refresh_view", lambda *args, **kwargs: None)
        monkeypatch.setattr(app, "_restore_table_position", lambda *args: None)
        monkeypatch.setattr(app, "notify", lambda *args, **kwargs: None)
        monkeypatch.setattr(
            app_module,
            "save_categories_to_profile",
            lambda groups, profile_dir: saved_groups.append(groups) or True,
        )

        await app._manage_categories()

        expected_base = {"Group G": ["A"], "Group H": ["B", "C"]}
        assert saved_groups == [expected_base]
        assert app.data_manager.pending_category_changes[0].before_groups == expected_base


class TestGroupManagerPersistence:
    def test_write_retries_pending_category_config(self, monkeypatch):
        from moneyflow.tui import app as app_module
        from moneyflow.tui.app import MoneyflowApp

        groups = {"Group": ["Category"]}

        class DataManagerStub:
            pending_category_groups = groups
            pending_category_changes = [object()]
            pending_category_aliases = []
            profile_dir = object()

            @staticmethod
            def get_stats():
                return {"pending_changes": 0}

        app = MoneyflowApp()
        app.data_manager = DataManagerStub()
        notifications = []
        monkeypatch.setattr(app_module, "save_categories_to_profile", lambda *args, **kwargs: True)
        monkeypatch.setattr(
            app,
            "notify",
            lambda message, **kwargs: notifications.append((message, kwargs.get("severity"))),
        )

        app.action_review_and_commit()

        assert app.data_manager.pending_category_groups is None
        assert app.data_manager.pending_category_changes == []
        assert notifications == [("Category configuration saved.", None)]

    def test_write_retry_flushes_retained_category_aliases(self, monkeypatch):
        """A no-edit retry that persists pending_category_groups must also
        persist the aliases retained from the failed save — otherwise the
        cleared retry trigger silently drops them and later imports
        resurrect the renamed/merged/deleted categories."""
        from moneyflow.tui import app as app_module
        from moneyflow.tui.app import MoneyflowApp

        class DataManagerStub:
            pending_category_groups = {"Group": ["Fun Purchases"]}
            pending_category_changes = []
            pending_category_aliases = [("cat_shopping", "cat_fun_purchases", "Fun Purchases")]
            profile_dir = object()

            @staticmethod
            def get_stats():
                return {"pending_changes": 0}

        recorded = []
        app = MoneyflowApp()
        app.data_manager = DataManagerStub()
        app.backend = type(
            "Backend",
            (),
            {"record_category_alias": staticmethod(lambda *args: recorded.append(args))},
        )()
        monkeypatch.setattr(app_module, "save_categories_to_profile", lambda *args, **kwargs: True)
        monkeypatch.setattr(app, "notify", lambda *args, **kwargs: None)

        app.action_review_and_commit()

        assert recorded == [("cat_shopping", "cat_fun_purchases", "Fun Purchases")]
        assert app.data_manager.pending_category_aliases == []
        assert app.data_manager.pending_category_groups is None

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
            pending_category_changes = []
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
            lambda groups, profile_dir: saved_groups.append(groups) or True,
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
            pending_category_changes = []
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
            lambda groups, profile_dir: saved_groups.append(groups) or True,
        )

        await app._manage_groups()

        assert saved_groups == [{"Current Group": ["Category"]}]
        assert app.data_manager.pending_category_groups is None
        assert app.data_manager.pending_category_changes == []

    async def test_group_changes_survive_dependent_category_config_rollback(self, monkeypatch):
        from moneyflow.tui import app as app_module
        from moneyflow.tui.app import MoneyflowApp

        dependent_timestamp = datetime(2026, 1, 2)

        class DataManagerStub:
            categories = {"target-category": {"name": "Target", "group": "Renamed Group"}}
            pending_edits = []
            pending_category_groups = {"Renamed Group": ["Target"]}
            pending_category_changes = [
                DeferredCategoryChange(
                    before_groups={"Original Group": ["Source", "Target"]},
                    after_groups={"Renamed Group": ["Target"]},
                    before_edits=[],
                    dependent_timestamps={dependent_timestamp},
                )
            ]
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
            lambda groups, profile_dir: saved_groups.append(groups) or True,
        )

        await app._manage_groups()

        assert saved_groups == [{"Final Group": ["Source", "Target"]}]
        assert app.data_manager.pending_category_groups == {"Final Group": ["Target"]}
        assert app.data_manager.pending_category_changes[0].before_groups == {
            "Final Group": ["Source", "Target"]
        }

    async def test_new_group_placeholder_survives_dependent_config_rollback(self, monkeypatch):
        from moneyflow.tui import app as app_module
        from moneyflow.tui.app import MoneyflowApp

        dependent_timestamp = datetime(2026, 1, 2)

        class DataManagerStub:
            categories = {"target-category": {"name": "Target", "group": "Group"}}
            pending_edits = []
            pending_category_groups = {"Group": ["Target"]}
            pending_category_changes = [
                DeferredCategoryChange(
                    before_groups={"Group": ["Source", "Target"]},
                    after_groups={"Group": ["Target"]},
                    before_edits=[],
                    dependent_timestamps={dependent_timestamp},
                )
            ]
            category_groups_config = {"Group": ["Target"]}
            category_to_group = {"Target": "Group"}
            profile_dir = object()

        app = MoneyflowApp()
        app.data_manager = DataManagerStub()
        saved_groups = []

        async def create_group(screen, **kwargs):
            app.data_manager.categories["new-group"] = {
                "name": "New Group",
                "group": "New Group",
            }
            return True

        monkeypatch.setattr(app, "push_screen", create_group)
        monkeypatch.setattr(app, "refresh_view", lambda *args, **kwargs: None)
        monkeypatch.setattr(app, "_restore_table_position", lambda *args: None)
        monkeypatch.setattr(app, "notify", lambda *args, **kwargs: None)
        monkeypatch.setattr(
            app_module,
            "save_categories_to_profile",
            lambda groups, profile_dir: saved_groups.append(groups) or True,
        )

        await app._manage_groups()

        expected_base = {"Group": ["Source", "Target"], "New Group": ["New Group"]}
        assert saved_groups == [expected_base]
        assert app.data_manager.pending_category_changes[0].before_groups == expected_base

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
            pending_category_changes = [
                DeferredCategoryChange(
                    before_groups=persisted_groups,
                    after_groups=deferred_groups,
                    before_edits=[ordinary_category_edit],
                    dependent_timestamps={dependent_category_edit.timestamp},
                    after_edits=[ordinary_category_edit, dependent_category_edit],
                )
            ]
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
            lambda groups, profile_dir: saved_groups.append(groups) or True,
        )
        app.action_undo_pending_edits()

        assert saved_groups == []
        assert app.data_manager.pending_category_groups is None
        assert app.data_manager.pending_category_changes == []
        assert app.data_manager.category_groups_config == persisted_groups
        assert "source-category" in app.data_manager.categories
        assert app.data_manager.pending_edits == [ordinary_category_edit]


class TestStagedCategoryStateCleanup:
    """A successful retry must discard the backend-staged snapshot, or the
    next startup restores an already-completed restructure."""

    def _app_with_pending(self, monkeypatch, staged_calls, pending_aliases):
        from moneyflow.tui import app as app_module
        from moneyflow.tui.app import MoneyflowApp

        class DataManagerStub:
            pending_category_groups = {"Group": ["Renamed"]}
            pending_category_changes = []
            pending_category_aliases = list(pending_aliases)
            profile_dir = object()

            @staticmethod
            def get_stats():
                return {"pending_changes": 0}

        app = MoneyflowApp()
        app.data_manager = DataManagerStub()
        app.backend = type(
            "Backend",
            (),
            {
                "record_category_alias": staticmethod(lambda *args: None),
                "save_pending_category_state": staticmethod(
                    lambda groups, aliases: staged_calls.append((groups, list(aliases)))
                ),
            },
        )()
        monkeypatch.setattr(app_module, "save_categories_to_profile", lambda *a, **k: True)
        monkeypatch.setattr(app, "notify", lambda *a, **k: None)
        return app

    def test_successful_retry_clears_staged_state(self, monkeypatch):
        staged_calls = []
        app = self._app_with_pending(monkeypatch, staged_calls, [])

        app.action_review_and_commit()

        assert staged_calls == [(None, [])]
        assert app.data_manager.pending_category_groups is None

    def test_staged_state_survives_while_aliases_remain_pending(self, monkeypatch):
        """If aliases still await the backend, the staged copy is their only
        durable record and must not be discarded."""
        staged_calls = []
        app = self._app_with_pending(monkeypatch, staged_calls, [])

        def failing(*args):
            raise RuntimeError("database is locked")

        app.backend.record_category_alias = failing
        app.data_manager.pending_category_aliases = [("cat_a", "cat_b", "B", 1)]

        app.action_review_and_commit()

        assert staged_calls == []
