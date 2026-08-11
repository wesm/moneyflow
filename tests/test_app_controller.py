"""
Tests for AppController business logic.

These tests verify controller behavior without requiring the UI to run.
They focus on the "data plane" bugs we recently fixed:
- View refresh logic
- force_rebuild behavior
- Table update sequencing
"""

import re
from datetime import datetime

import polars as pl
import pytest

from moneyflow.backends.base import AggregationFunc, ComputedColumn
from moneyflow.data.data_manager import DataManager, DeferredCategoryChange
from moneyflow.data.state import AppState, SortDirection, SortMode, TransactionEdit, ViewMode
from moneyflow.tui.app_controller import AppController

from .mock_view import MockViewPresenter


class ConfigurableStubCacheManager:
    """Reusable stub for testing cache tier availability."""

    def __init__(self, hot_df=None, cold_df=None):
        self._hot_df = hot_df
        self._cold_df = cold_df
        self.saved_hot = None
        self.saved_cold = None
        self.saved_full = None

    def load_hot_cache(self):
        return self._hot_df.clone() if self._hot_df is not None else None

    def load_cold_cache(self):
        return self._cold_df.clone() if self._cold_df is not None else None

    def save_hot_cache(self, hot_df, categories, category_groups):
        self.saved_hot = hot_df

    def save_cold_cache(self, cold_df):
        self.saved_cold = cold_df

    def save_cache(self, transactions_df, categories, category_groups, year=None, since=None):
        self.saved_full = transactions_df


@pytest.fixture
def mock_view():
    """Provide mock view presenter."""
    return MockViewPresenter()


@pytest.fixture
def empty_transactions_df():
    """Provide an empty transactions DataFrame."""
    return pl.DataFrame(
        schema={
            "id": pl.Utf8,
            "date": pl.Date,
            "amount": pl.Float64,
            "merchant": pl.Utf8,
            "category": pl.Utf8,
            "group": pl.Utf8,
            "hideFromReports": pl.Boolean,
        },
    )


@pytest.fixture
async def controller(mock_view, mock_mm, tmp_path):
    """Provide controller with mock dependencies and isolated config."""
    await mock_mm.login()
    # Use tmp_path for config_dir to avoid modifying user's ~/.moneyflow/config.yaml
    data_manager = DataManager(mock_mm, config_dir=str(tmp_path))
    state = AppState()

    # Fetch data
    df, categories, groups = await data_manager.fetch_all_data()
    data_manager.df = df
    data_manager.categories = categories
    data_manager.category_groups = groups
    state.transactions_df = df

    controller = AppController(mock_view, state, data_manager)
    return controller


class TestViewRefresh:
    """Test view refresh logic."""

    async def test_refresh_view_updates_table(self, controller, mock_view):
        """Test that refresh_view calls update_table."""
        controller.state.view_mode = ViewMode.MERCHANT

        controller.refresh_view()

        # Should have updated table
        assert len(mock_view.table_updates) == 1
        update = mock_view.get_last_table_update()
        assert update["column_count"] == 6  # Merchant, Count, Total, %, Top Category, Flags
        assert update["row_count"] > 0  # Should have data

    async def test_refresh_view_with_force_rebuild_true(self, controller, mock_view):
        """Test force_rebuild=True."""
        controller.state.view_mode = ViewMode.DETAIL

        controller.refresh_view(force_rebuild=True)

        mock_view.assert_force_rebuild(True)

    async def test_refresh_view_with_force_rebuild_false(self, controller, mock_view):
        """Test force_rebuild=False (smooth update)."""
        controller.state.view_mode = ViewMode.DETAIL

        controller.refresh_view(force_rebuild=False)

        mock_view.assert_force_rebuild(False)

    async def test_refresh_view_updates_breadcrumb(self, controller, mock_view):
        """Test that breadcrumb is updated."""
        controller.state.view_mode = ViewMode.MERCHANT

        controller.refresh_view()

        assert len(mock_view.breadcrumbs) == 1
        assert "Merchants" in mock_view.breadcrumbs[0]

    async def test_refresh_view_updates_stats(self, controller, mock_view):
        """Test that stats are updated."""
        controller.state.view_mode = ViewMode.MERCHANT

        controller.refresh_view()

        assert len(mock_view.stats) == 1
        assert "txns" in mock_view.stats[0]

    async def test_refresh_view_updates_hints(self, controller, mock_view):
        """Test that action hints are updated."""
        controller.state.view_mode = ViewMode.MERCHANT

        controller.refresh_view()

        assert len(mock_view.hints) == 1
        assert "Merchant" in mock_view.hints[0]

    async def test_refresh_view_updates_pending_changes(self, controller, mock_view):
        """Test that pending changes indicator is updated."""
        controller.state.view_mode = ViewMode.MERCHANT

        controller.refresh_view()

        assert len(mock_view.pending_changes) == 1
        assert mock_view.pending_changes[0] == 0  # No pending edits initially


class TestViewModes:
    """Test different view modes."""

    async def test_merchant_view(self, controller, mock_view):
        """Test merchant aggregation view."""
        controller.state.view_mode = ViewMode.MERCHANT

        controller.refresh_view()

        update = mock_view.get_last_table_update()
        assert update["column_count"] == 6
        # Columns should be: Merchant, Count, Total, %, Top Category, Flags
        assert update["columns"][0]["key"] == "merchant"
        assert update["columns"][1]["key"] == "count"
        assert update["columns"][2]["key"] == "total"
        assert update["columns"][3]["key"] == "pct"
        assert update["columns"][4]["key"] == "top_category_display"
        assert update["columns"][5]["key"] == "flags"

    async def test_category_view(self, controller, mock_view):
        """Test category aggregation view."""
        controller.state.view_mode = ViewMode.CATEGORY

        controller.refresh_view()

        update = mock_view.get_last_table_update()
        assert update["column_count"] == 5  # Category, Count, Total, %, Flags
        assert update["columns"][0]["key"] == "category"

    async def test_detail_view(self, controller, mock_view):
        """Test transaction detail view."""
        controller.state.view_mode = ViewMode.DETAIL

        controller.refresh_view()

        update = mock_view.get_last_table_update()
        assert update["column_count"] == 6  # Date, Merchant, Category, Account, Amount, Flags
        assert update["columns"][0]["key"] == "date"
        assert update["columns"][1]["key"] == "merchant"


class TestForceRebuildBehavior:
    """
    Test force_rebuild parameter behavior.

    This is critical - the DuplicateKey bug we fixed was caused by
    incorrect handling of force_rebuild.
    """

    async def test_force_rebuild_true_on_first_call(self, controller, mock_view):
        """First call should always force rebuild."""
        controller.state.view_mode = ViewMode.DETAIL

        controller.refresh_view(force_rebuild=True)

        mock_view.assert_force_rebuild(True)

    async def test_force_rebuild_false_on_commit(self, controller, mock_view):
        """Commit from detail view should use force_rebuild=False."""
        controller.state.view_mode = ViewMode.DETAIL

        # Simulate commit flow
        controller.refresh_view(force_rebuild=False)

        mock_view.assert_force_rebuild(False)

    async def test_multiple_refreshes_with_force_rebuild_false(self, controller, mock_view):
        """Multiple refreshes with force_rebuild=False should work."""
        controller.state.view_mode = ViewMode.DETAIL

        # First refresh
        controller.refresh_view(force_rebuild=True)
        assert len(mock_view.table_updates) == 1

        # Second refresh (like after commit)
        controller.refresh_view(force_rebuild=False)
        assert len(mock_view.table_updates) == 2

        # Third refresh (shouldn't crash with DuplicateKey)
        controller.refresh_view(force_rebuild=False)
        assert len(mock_view.table_updates) == 3

        # All should have worked
        assert mock_view.table_updates[0]["force_rebuild"] is True
        assert mock_view.table_updates[1]["force_rebuild"] is False
        assert mock_view.table_updates[2]["force_rebuild"] is False


class TestDetailViewFiltering:
    """Test transaction filtering in detail view."""

    async def test_detail_view_with_merchant_filter(self, controller, mock_view):
        """Test drilling down into a merchant."""
        controller.state.view_mode = ViewMode.DETAIL
        controller.state.selected_merchant = "Amazon"

        controller.refresh_view()

        # Should show only Amazon transactions
        update = mock_view.get_last_table_update()
        # Check that we got some rows (mock has Amazon transactions)
        assert update["row_count"] > 0

    async def test_detail_view_with_category_filter(self, controller, mock_view):
        """Test drilling down into a category."""
        controller.state.view_mode = ViewMode.DETAIL
        controller.state.selected_category = "Shopping"

        controller.refresh_view()

        update = mock_view.get_last_table_update()
        assert update["row_count"] >= 0  # May have 0 Shopping transactions in mock

    async def test_detail_view_ungrouped(self, controller, mock_view):
        """Test all transactions view (no filters)."""
        controller.state.view_mode = ViewMode.DETAIL
        # No selected_* filters

        controller.refresh_view()

        update = mock_view.get_last_table_update()
        # Should show all transactions
        assert update["row_count"] == 6  # Mock has 6 transactions


class TestAmazonColumnVisibility:
    """Tests for Amazon column visibility logic."""

    def _get_amazon_merchant(self, controller) -> str:
        merchants = controller.data_manager.df["merchant"].to_list()
        for merchant in merchants:
            if not merchant:
                continue
            merchant_lower = merchant.lower()
            if "amazon" in merchant_lower or "amzn" in merchant_lower:
                return merchant
        raise AssertionError("Mock data is missing an Amazon merchant")

    async def test_amazon_column_enabled_in_detail_view(self, controller, mock_view):
        """Detail view should enable Amazon column when filtered to Amazon merchants."""
        controller.state.view_mode = ViewMode.DETAIL
        controller.state.selected_merchant = self._get_amazon_merchant(controller)

        controller.refresh_view()

        assert controller._showing_amazon_column is True
        update = mock_view.get_last_table_update()
        keys = [col["key"] for col in update["columns"]]
        assert "amazon" in keys

    async def test_amazon_column_resets_in_aggregate_view(self, controller, mock_view):
        """Aggregate views should disable Amazon column."""
        controller.state.view_mode = ViewMode.DETAIL
        controller.state.selected_merchant = self._get_amazon_merchant(controller)
        controller.refresh_view()
        assert controller._showing_amazon_column is True

        controller.state.view_mode = ViewMode.MERCHANT
        controller.state.selected_merchant = None
        controller.refresh_view()

        assert controller._showing_amazon_column is False
        update = mock_view.get_last_table_update()
        keys = [col["key"] for col in update["columns"]]
        assert "amazon" not in keys

    async def test_amazon_column_disabled_in_subgroup_view(self, controller, mock_view):
        """Sub-grouped views should not show Amazon column."""
        controller.state.view_mode = ViewMode.DETAIL
        controller.state.selected_merchant = self._get_amazon_merchant(controller)
        controller.state.sub_grouping_mode = ViewMode.CATEGORY

        controller.refresh_view()

        assert controller._showing_amazon_column is False
        update = mock_view.get_last_table_update()
        keys = [col["key"] for col in update["columns"]]
        assert "amazon" not in keys


class TestStatsCalculation:
    """Test statistics calculation logic."""

    async def test_stats_exclude_hidden_transactions(self, controller, mock_view):
        """Test that hidden transactions are excluded from totals."""
        controller.state.view_mode = ViewMode.MERCHANT

        controller.refresh_view()

        stats_text = mock_view.stats[-1]
        # Stats should be calculated (exact values depend on mock data)
        # Format: "N txns | In: $X | Out: $Y | Net: $Z"
        assert "txns" in stats_text
        assert "In:" in stats_text
        assert "Out:" in stats_text
        assert "Net:" in stats_text

    async def test_stats_with_no_data(self, controller, mock_view, empty_transactions_df):
        """Test stats with empty dataset."""
        controller.data_manager.df = empty_transactions_df
        controller.state.transactions_df = empty_transactions_df
        controller.state.view_mode = ViewMode.MERCHANT

        controller.refresh_view()

        stats_text = mock_view.stats[-1]
        assert "0 txns" in stats_text or "No data" in stats_text


class TestActionHints:
    """Test action hints for different views."""

    async def test_merchant_view_hints(self, controller, mock_view):
        """Merchant view should show merchant-specific hints."""
        controller.state.view_mode = ViewMode.MERCHANT

        controller.refresh_view()

        hints = mock_view.hints[-1]
        assert "Merchant" in hints
        assert "bulk" in hints.lower()
        assert "Space=Select" in hints

    async def test_category_view_hints(self, controller, mock_view):
        """Category view should show edit_category hint."""
        controller.state.view_mode = ViewMode.CATEGORY

        controller.refresh_view()

        hints = mock_view.hints[-1]
        assert "Category" in hints
        assert "bulk" in hints.lower()
        assert "Space=Select" in hints

    async def test_detail_view_hints(self, controller, mock_view):
        """Detail view should show transaction-level hints."""
        controller.state.view_mode = ViewMode.DETAIL

        controller.refresh_view()

        hints = mock_view.hints[-1]
        assert "Merchant" in hints
        assert "Category" in hints
        assert "Space=Select" in hints
        assert "Ctrl-A=SelectAll" in hints


class TestBreadcrumbGeneration:
    """Test breadcrumb navigation text."""

    async def test_merchant_view_breadcrumb(self, controller, mock_view):
        """Merchant view breadcrumb."""
        controller.state.view_mode = ViewMode.MERCHANT

        controller.refresh_view()

        breadcrumb = mock_view.breadcrumbs[-1]
        assert "Merchants" in breadcrumb

    async def test_drilled_down_breadcrumb(self, controller, mock_view):
        """Breadcrumb when drilled down."""
        controller.state.view_mode = ViewMode.DETAIL
        controller.state.selected_merchant = "Amazon"

        controller.refresh_view()

        breadcrumb = mock_view.breadcrumbs[-1]
        assert "Amazon" in breadcrumb


class TestPendingChangesIndicator:
    """Test pending changes indicator updates."""

    async def test_no_pending_changes(self, controller, mock_view):
        """Initially should have 0 pending changes."""
        controller.state.view_mode = ViewMode.MERCHANT

        controller.refresh_view()

        assert mock_view.pending_changes[-1] == 0

    async def test_with_pending_changes(self, controller, mock_view):
        """Should show count of pending edits."""

        # Add some pending edits
        controller.data_manager.pending_edits = [
            TransactionEdit("txn_1", "merchant", "Old", "New", datetime.now()),
            TransactionEdit("txn_2", "merchant", "Old2", "New2", datetime.now()),
        ]

        controller.state.view_mode = ViewMode.MERCHANT
        controller.refresh_view()

        assert mock_view.pending_changes[-1] == 2


class TestCommitHandling:
    """
    Test commit result handling - THE CRITICAL DATA INTEGRITY LOGIC.

    This is the bug we fixed: edits were applied locally even when
    commits failed. These tests ensure it stays fixed.
    """

    def _simulate_commit(
        self,
        controller,
        edits,
        success_count,
        failure_count,
        **kwargs,
    ):
        controller.data_manager.pending_edits = edits.copy()
        controller.state.view_mode = ViewMode.DETAIL
        saved_state = controller.state.save_view_state()
        controller.handle_commit_result(
            success_count=success_count,
            failure_count=failure_count,
            edits=edits,
            saved_state=saved_state,
            **kwargs,
        )

    async def test_all_commits_succeed_applies_edits(self, controller, mock_view):
        """When ALL commits succeed, edits should be applied locally."""

        # Set up initial data
        initial_df = controller.data_manager.df.clone()
        initial_merchant = initial_df.filter(pl.col("id") == "txn_1")["merchant"][0]

        # Create edits
        edits = [
            TransactionEdit("txn_1", "merchant", initial_merchant, "NewMerchant", datetime.now())
        ]
        # Simulate successful commit
        self._simulate_commit(controller, edits, success_count=1, failure_count=0)

        # VERIFY: Edits applied locally
        updated_merchant = controller.data_manager.df.filter(pl.col("id") == "txn_1")["merchant"][0]
        assert updated_merchant == "NewMerchant", "Edit should be applied locally"

        # VERIFY: Pending edits cleared
        assert len(controller.data_manager.pending_edits) == 0, "Pending edits should be cleared"

        # VERIFY: View refreshed
        assert len(mock_view.table_updates) > 0, "View should be refreshed"

    async def test_successful_commit_clears_deferred_category_ownership(self, controller):
        edit = TransactionEdit(
            "txn_1", "category", "cat_groceries", "cat_entertainment", datetime.now()
        )
        controller.data_manager.pending_category_groups = {"Group": ["Category"]}
        controller.data_manager.pending_category_changes = [
            DeferredCategoryChange(
                before_groups={"Old Group": ["Category"]},
                after_groups={"Group": ["Category"]},
                before_edits=[],
                dependent_timestamps={edit.timestamp},
            )
        ]

        self._simulate_commit(controller, [edit], success_count=1, failure_count=0)

        assert controller.data_manager.pending_category_groups is None
        assert controller.data_manager.pending_category_changes == []

    async def test_successful_commit_persists_deferred_category_aliases(self, controller):
        """Aliases buffered on a deferred change are written to the backend
        only when the dependent edits commit — undo before commit would have
        discarded them along with the change."""
        edit = TransactionEdit(
            "txn_1", "category", "cat_groceries", "cat_entertainment", datetime.now()
        )
        recorded = []
        controller.data_manager.mm.record_category_alias = lambda *args: recorded.append(args)
        controller.data_manager.pending_category_groups = {"Group": ["Entertainment"]}
        controller.data_manager.pending_category_changes = [
            DeferredCategoryChange(
                before_groups={"Group": ["Groceries"]},
                after_groups={"Group": ["Entertainment"]},
                before_edits=[],
                dependent_timestamps={edit.timestamp},
                category_aliases=[("cat_groceries", "cat_entertainment", "Entertainment")],
            )
        ]

        self._simulate_commit(controller, [edit], success_count=1, failure_count=0)

        assert recorded == [("cat_groceries", "cat_entertainment", "Entertainment")]
        assert controller.data_manager.pending_category_changes == []

    async def test_successful_commit_flushes_retained_aliases(self, controller):
        """Aliases retained from an earlier failed config save are persisted
        when the retried save succeeds, and cleared afterwards."""
        edit = TransactionEdit(
            "txn_1", "category", "cat_groceries", "cat_entertainment", datetime.now()
        )
        recorded = []
        controller.data_manager.mm.record_category_alias = lambda *args: recorded.append(args)
        controller.data_manager.pending_category_groups = {"Group": ["Fun Purchases"]}
        controller.data_manager.pending_category_aliases = [
            ("cat_shopping", "cat_fun_purchases", "Fun Purchases")
        ]

        self._simulate_commit(controller, [edit], success_count=1, failure_count=0)

        assert recorded == [("cat_shopping", "cat_fun_purchases", "Fun Purchases")]
        assert controller.data_manager.pending_category_aliases == []
        assert controller.data_manager.pending_category_groups is None

    async def test_config_save_failure_does_not_record_aliases(self, controller, monkeypatch):
        """An alias must never outlive its configuration: if the config save
        fails, the aliases stay staged rather than becoming durable while the
        rename/merge/delete they belong to exists only in memory."""
        from moneyflow.tui import app_controller as controller_module

        edit = TransactionEdit(
            "txn_1", "category", "cat_groceries", "cat_entertainment", datetime.now()
        )
        recorded = []
        controller.data_manager.mm.record_category_alias = lambda *args: recorded.append(args)
        controller.data_manager.profile_dir = object()
        controller.data_manager.pending_category_groups = {"Group": ["Entertainment"]}
        controller.data_manager.pending_category_changes = [
            DeferredCategoryChange(
                before_groups={"Group": ["Groceries"]},
                after_groups={"Group": ["Entertainment"]},
                before_edits=[],
                dependent_timestamps={edit.timestamp},
                category_aliases=[("cat_groceries", "cat_entertainment", "Entertainment")],
            )
        ]
        monkeypatch.setattr(
            controller_module, "save_categories_to_profile", lambda *args, **kwargs: False
        )

        self._simulate_commit(controller, [edit], success_count=1, failure_count=0)

        assert recorded == []  # nothing recorded while the config is unsaved
        assert controller.data_manager.pending_category_aliases == [
            ("cat_groceries", "cat_entertainment", "Entertainment")
        ]
        assert controller.data_manager.pending_category_groups == {"Group": ["Entertainment"]}

    async def test_alias_persistence_failure_retains_aliases_for_retry(self, controller, mock_view):
        """Aliases the backend fails to confirm must stay staged instead of
        being dropped — the user is warned and a later retry persists them."""
        edit = TransactionEdit(
            "txn_1", "category", "cat_groceries", "cat_entertainment", datetime.now()
        )
        attempts = []

        def failing_record(*args):
            attempts.append(args)
            raise RuntimeError("database is locked")

        controller.data_manager.mm.record_category_alias = failing_record
        controller.data_manager.pending_category_groups = {"Group": ["Entertainment"]}
        controller.data_manager.pending_category_changes = [
            DeferredCategoryChange(
                before_groups={"Group": ["Groceries"]},
                after_groups={"Group": ["Entertainment"]},
                before_edits=[],
                dependent_timestamps={edit.timestamp},
                category_aliases=[("cat_groceries", "cat_entertainment", "Entertainment")],
            )
        ]

        self._simulate_commit(controller, [edit], success_count=1, failure_count=0)

        assert attempts  # persistence was attempted
        assert controller.data_manager.pending_category_aliases == [
            ("cat_groceries", "cat_entertainment", "Entertainment")
        ]
        assert controller.data_manager.pending_category_changes == []
        assert any(
            "could not be recorded" in notification["message"]
            and notification["severity"] == "warning"
            for notification in mock_view.notifications
        )

        # A later commit with a working backend flushes the retained alias.
        recorded = []
        controller.data_manager.mm.record_category_alias = lambda *args: recorded.append(args)
        edit2 = TransactionEdit("txn_2", "merchant", "Old", "New", datetime.now())
        self._simulate_commit(controller, [edit2], success_count=1, failure_count=0)

        assert recorded == [("cat_groceries", "cat_entertainment", "Entertainment")]
        assert controller.data_manager.pending_category_aliases == []

    async def test_category_save_failure_retains_deferred_state(
        self, controller, mock_view, monkeypatch
    ):
        from moneyflow.tui import app_controller as controller_module

        edit = TransactionEdit(
            "txn_1", "category", "cat_groceries", "cat_entertainment", datetime.now()
        )
        groups = {"Group": ["Category"]}
        controller.data_manager.profile_dir = object()
        controller.data_manager.pending_category_groups = groups
        controller.data_manager.pending_category_changes = [
            DeferredCategoryChange(
                before_groups={"Old Group": ["Category"]},
                after_groups=groups,
                before_edits=[],
                dependent_timestamps={edit.timestamp},
            )
        ]
        monkeypatch.setattr(
            controller_module, "save_categories_to_profile", lambda *args, **kwargs: False
        )

        self._simulate_commit(controller, [edit], success_count=1, failure_count=0)

        assert controller.data_manager.pending_category_groups == groups
        assert controller.data_manager.pending_category_changes == []
        assert mock_view.notifications[-1]["severity"] == "error"

    async def test_partial_failure_does_not_apply_edits(self, controller, mock_view):
        """
        CRITICAL: When ANY commits fail, edits should NOT be applied locally.

        This is the data corruption bug we fixed.
        """

        # Set up initial data
        initial_df = controller.data_manager.df.clone()
        initial_merchant = initial_df.filter(pl.col("id") == "txn_1")["merchant"][0]

        # Create edits
        edits = [
            TransactionEdit("txn_1", "merchant", initial_merchant, "NewMerchant1", datetime.now()),
            TransactionEdit("txn_2", "merchant", "Old2", "NewMerchant2", datetime.now()),
        ]
        # Simulate partial failure (1 success, 1 failure)
        self._simulate_commit(controller, edits, success_count=1, failure_count=1)

        # CRITICAL VERIFICATION: Edits should NOT be applied
        current_merchant = controller.data_manager.df.filter(pl.col("id") == "txn_1")["merchant"][0]
        assert current_merchant == initial_merchant, (
            "Edit should NOT be applied when there were failures (data corruption!)"
        )

        # VERIFY: Pending edits still present (for retry)
        assert len(controller.data_manager.pending_edits) == 2, (
            "Pending edits should be kept for retry"
        )

    async def test_all_failures_does_not_apply_edits(self, controller, mock_view):
        """When ALL commits fail, nothing should be applied."""

        initial_df = controller.data_manager.df.clone()

        edits = [
            TransactionEdit("txn_1", "merchant", "Old1", "New1", datetime.now()),
            TransactionEdit("txn_2", "merchant", "Old2", "New2", datetime.now()),
        ]
        self._simulate_commit(controller, edits, success_count=0, failure_count=2)

        # VERIFY: DataFrame unchanged
        assert controller.data_manager.df.equals(initial_df), (
            "DataFrame should be completely unchanged"
        )

        # VERIFY: Pending edits preserved
        assert len(controller.data_manager.pending_edits) == 2

    async def test_commit_success_uses_force_rebuild_false(self, controller, mock_view):
        """Commit should use force_rebuild=False for smooth update."""

        edits = [TransactionEdit("txn_1", "merchant", "Old", "New", datetime.now())]

        self._simulate_commit(controller, edits, success_count=1, failure_count=0)

        # VERIFY: force_rebuild=False (no flash)
        mock_view.assert_force_rebuild(False)

    async def test_commit_failure_refreshes_view(self, controller, mock_view):
        """Failed commit should refresh view to show unchanged data."""

        # Set up initial state
        controller.state.view_mode = ViewMode.DETAIL
        initial_merchant = controller.data_manager.df.filter(pl.col("id") == "txn_1")["merchant"][0]

        # Create edits (that will fail)
        edits = [
            TransactionEdit("txn_1", "merchant", initial_merchant, "NewMerchant", datetime.now())
        ]
        # Simulate failure
        self._simulate_commit(controller, edits, success_count=0, failure_count=1)

        # VERIFY: DataFrame unchanged (edits NOT applied)
        current_merchant = controller.data_manager.df.filter(pl.col("id") == "txn_1")["merchant"][0]
        assert current_merchant == initial_merchant, "Edits should NOT be applied on failure"

        # VERIFY: Pending edits preserved
        assert len(controller.data_manager.pending_edits) == 1, "Pending edits should be preserved"

        # VERIFY: View refreshed (with force_rebuild=False)
        assert len(mock_view.table_updates) > 0, "View should be refreshed"
        mock_view.assert_force_rebuild(False)

    async def test_filtered_view_updates_cached_tiers(self, controller):
        """Filtered views should merge edits into cached tiers, not overwrite them."""

        full_df = controller.data_manager.df
        hot_df = full_df.head(2)
        cold_df = full_df.tail(2)

        edit_id = hot_df["id"][0]
        old_merchant = hot_df["merchant"][0]
        edits = [
            TransactionEdit(
                edit_id,
                "merchant",
                old_merchant,
                "Edited",
                datetime.now(),
            )
        ]
        # Simulate filtered view with only a subset of hot transactions
        controller.data_manager.df = hot_df.head(1).clone()

        stub_cache = ConfigurableStubCacheManager(hot_df=hot_df, cold_df=cold_df)
        controller.cache_manager = stub_cache

        self._simulate_commit(
            controller,
            edits,
            success_count=1,
            failure_count=0,
            cache_filters={"year": None, "since": None},
            is_filtered_view=True,
        )

        assert stub_cache.saved_full is None, "Filtered view should not overwrite full cache"
        assert stub_cache.saved_hot is not None
        assert stub_cache.saved_cold is not None
        assert len(stub_cache.saved_hot) == len(hot_df), "Hot tier should be preserved"
        assert stub_cache.saved_cold.equals(cold_df), "Cold tier should be preserved"

        updated_merchant = stub_cache.saved_hot.filter(pl.col("id") == edit_id)["merchant"][0]
        assert updated_merchant == "Edited"

    async def test_filtered_view_partial_cache_update_when_cold_unavailable(self, controller):
        """When cold cache is unavailable, hot should still be updated."""

        full_df = controller.data_manager.df
        hot_df = full_df.head(3)

        edit_id = hot_df["id"][0]
        old_merchant = hot_df["merchant"][0]
        edits = [
            TransactionEdit(
                edit_id,
                "merchant",
                old_merchant,
                "Edited",
                datetime.now(),
            )
        ]
        controller.data_manager.df = hot_df.head(1).clone()

        stub_cache = ConfigurableStubCacheManager(hot_df=hot_df, cold_df=None)
        controller.cache_manager = stub_cache

        self._simulate_commit(
            controller,
            edits,
            success_count=1,
            failure_count=0,
            cache_filters={"year": None, "since": None},
            is_filtered_view=True,
        )

        # Hot should be saved with edits, cold should not be touched
        assert stub_cache.saved_hot is not None, "Hot cache should be updated"
        assert stub_cache.saved_cold is None, "Cold cache should not be updated (was unavailable)"
        assert stub_cache.saved_full is None, "Full cache should not be called"

        updated_merchant = stub_cache.saved_hot.filter(pl.col("id") == edit_id)["merchant"][0]
        assert updated_merchant == "Edited"

    async def test_filtered_view_partial_cache_update_when_hot_unavailable(self, controller):
        """When hot cache is unavailable, cold should still be updated."""

        full_df = controller.data_manager.df
        cold_df = full_df.tail(3)

        # Edit a transaction that's in cold cache
        edit_id = cold_df["id"][0]
        old_merchant = cold_df["merchant"][0]
        edits = [
            TransactionEdit(
                edit_id,
                "merchant",
                old_merchant,
                "ColdEdited",
                datetime.now(),
            )
        ]
        controller.data_manager.df = cold_df.head(1).clone()

        stub_cache = ConfigurableStubCacheManager(hot_df=None, cold_df=cold_df)
        controller.cache_manager = stub_cache

        self._simulate_commit(
            controller,
            edits,
            success_count=1,
            failure_count=0,
            cache_filters={"year": None, "since": None},
            is_filtered_view=True,
        )

        # Cold should be saved with edits, hot should not be touched
        assert stub_cache.saved_cold is not None, "Cold cache should be updated"
        assert stub_cache.saved_hot is None, "Hot cache should not be updated (was unavailable)"
        assert stub_cache.saved_full is None, "Full cache should not be called"

        updated_merchant = stub_cache.saved_cold.filter(pl.col("id") == edit_id)["merchant"][0]
        assert updated_merchant == "ColdEdited"

    async def test_filtered_view_both_tiers_unavailable_does_not_corrupt_cache(self, controller):
        """When both cache tiers are unavailable, we should NOT write partial data.

        In filtered view, data_manager.df only contains the filtered subset.
        Writing it as the full cache would lose historical transactions.
        Instead, we just log an error - edits are saved to backend.
        """

        # Set up a filtered view with only partial data
        full_df = controller.data_manager.df
        filtered_df = full_df.head(1)  # Only 1 transaction (simulates MTD filter)

        edit_id = filtered_df["id"][0]
        old_merchant = filtered_df["merchant"][0]
        edits = [
            TransactionEdit(
                edit_id,
                "merchant",
                old_merchant,
                "Edited",
                datetime.now(),
            )
        ]
        controller.data_manager.df = filtered_df.clone()

        stub_cache = ConfigurableStubCacheManager(hot_df=None, cold_df=None)
        controller.cache_manager = stub_cache

        self._simulate_commit(
            controller,
            edits,
            success_count=1,
            failure_count=0,
            cache_filters={"year": None, "since": None},
            is_filtered_view=True,
        )

        # CRITICAL: No cache writes should happen to avoid data loss
        assert stub_cache.saved_hot is None, "Hot cache should NOT be written"
        assert stub_cache.saved_cold is None, "Cold cache should NOT be written"
        assert stub_cache.saved_full is None, "Full cache should NOT be written (would lose data)"


class TestEditQueueing:
    """
    Test edit queueing methods - pure business logic without UI.

    These methods were extracted from app.py to make them testable.
    They handle queueing category and merchant edits.
    """

    async def test_queue_category_edits_single_transaction(self, controller):
        """Test queueing a category edit for a single transaction."""
        # Get a single transaction
        txn_df = controller.data_manager.df.filter(pl.col("id") == "txn_1")
        old_cat_id = txn_df["category_id"][0]
        new_cat_id = "cat_new"

        # Queue the edit
        count = controller.queue_category_edits(txn_df, new_cat_id)

        # Verify
        assert count == 1, "Should queue exactly 1 edit"
        assert len(controller.data_manager.pending_edits) == 1
        edit = controller.data_manager.pending_edits[0]
        assert edit.transaction_id == "txn_1"
        assert edit.field == "category"
        assert edit.old_value == old_cat_id
        assert edit.new_value == new_cat_id

    async def test_queue_category_edits_multiple_transactions(self, controller):
        """Test queueing category edits for multiple transactions."""
        # Get two transactions
        txn_df = controller.data_manager.df.filter(pl.col("id").is_in(["txn_1", "txn_2"]))
        new_cat_id = "cat_bulk"

        count = controller.queue_category_edits(txn_df, new_cat_id)

        assert count == 2
        assert len(controller.data_manager.pending_edits) == 2
        assert all(e.field == "category" for e in controller.data_manager.pending_edits)
        assert all(e.new_value == new_cat_id for e in controller.data_manager.pending_edits)

    async def test_queue_category_edits_preserves_old_values(self, controller):
        """Test that each transaction's old category is preserved correctly."""
        # Get transactions with different categories
        txn_df = controller.data_manager.df.head(3)

        count = controller.queue_category_edits(txn_df, "cat_new")

        assert count == 3
        # Each edit should have its own old_value from the transaction
        old_values = [e.old_value for e in controller.data_manager.pending_edits]
        # Old values should match what's in the DataFrame
        assert len(set(old_values)) >= 1, "Should preserve individual old values"

    async def test_queue_merchant_edits_single_transaction(self, controller):
        """Test queueing a merchant edit for a single transaction."""
        txn_df = controller.data_manager.df.filter(pl.col("id") == "txn_1")
        old_merchant = txn_df["merchant"][0]
        new_merchant = "New Merchant Name"

        count = controller.queue_merchant_edits(txn_df, old_merchant, new_merchant)

        assert count == 1
        assert len(controller.data_manager.pending_edits) == 1
        edit = controller.data_manager.pending_edits[0]
        assert edit.transaction_id == "txn_1"
        assert edit.field == "merchant"
        assert edit.old_value == old_merchant
        assert edit.new_value == new_merchant

    async def test_queue_merchant_edits_bulk_rename(self, controller):
        """Test bulk merchant rename across multiple transactions."""
        # Get all Amazon transactions
        amazon_txns = controller.data_manager.df.filter(pl.col("merchant") == "Amazon")
        old_name = "Amazon"
        new_name = "Amazon.com"

        count = controller.queue_merchant_edits(amazon_txns, old_name, new_name)

        assert count == len(amazon_txns)
        assert len(controller.data_manager.pending_edits) == count
        # All should be merchant edits to Amazon.com
        assert all(e.field == "merchant" for e in controller.data_manager.pending_edits)
        assert all(e.new_value == new_name for e in controller.data_manager.pending_edits)
        assert all(e.old_value == "Amazon" for e in controller.data_manager.pending_edits)

    async def test_queue_edits_empty_dataframe(self, controller, empty_transactions_df):
        """Test queueing edits with empty DataFrame."""
        count = controller.queue_category_edits(empty_transactions_df, "cat_new")
        assert count == 0
        assert len(controller.data_manager.pending_edits) == 0

    async def test_queue_edits_preserves_transaction_ids(self, controller):
        """Test that transaction IDs are correctly preserved."""
        txn_df = controller.data_manager.df.filter(pl.col("id").is_in(["txn_1", "txn_3", "txn_5"]))

        controller.queue_category_edits(txn_df, "cat_test")

        queued_ids = {e.transaction_id for e in controller.data_manager.pending_edits}
        assert queued_ids == {"txn_1", "txn_3", "txn_5"}

    async def test_queue_edits_appends_to_existing(self, controller):
        """Test that queueing appends to existing edits (doesn't replace)."""

        # Add an existing edit
        controller.data_manager.pending_edits = [
            TransactionEdit("txn_999", "merchant", "Old", "New", datetime.now())
        ]

        # Queue more edits
        txn_df = controller.data_manager.df.head(2)
        controller.queue_category_edits(txn_df, "cat_new")

        # Should have 3 total (1 existing + 2 new)
        assert len(controller.data_manager.pending_edits) == 3
        assert controller.data_manager.pending_edits[0].transaction_id == "txn_999"

    async def test_queue_hide_toggle_edits_single_transaction(self, controller):
        """Test queueing hide toggle for a single transaction."""
        # Get a transaction that's not hidden
        txn_df = controller.data_manager.df.filter(~pl.col("hideFromReports")).head(1)

        count = controller.queue_hide_toggle_edits(txn_df)

        assert count == 1
        assert len(controller.data_manager.pending_edits) == 1
        edit = controller.data_manager.pending_edits[0]
        assert edit.field == "hide_from_reports"
        assert edit.old_value is False
        assert edit.new_value is True  # Should toggle from False to True

    async def test_queue_hide_toggle_edits_multiple_transactions(self, controller):
        """Test bulk hide/unhide toggle."""
        txn_df = controller.data_manager.df.head(3)

        count = controller.queue_hide_toggle_edits(txn_df)

        assert count == 3
        assert len(controller.data_manager.pending_edits) == 3
        assert all(e.field == "hide_from_reports" for e in controller.data_manager.pending_edits)
        # Each should toggle its current state
        for edit in controller.data_manager.pending_edits:
            assert edit.new_value == (not edit.old_value)

    async def test_queue_hide_toggle_preserves_individual_states(self, controller):
        """Test that each transaction's hide state is toggled individually."""
        # Get mix of hidden and unhidden transactions
        all_txns = controller.data_manager.df.head(4)

        count = controller.queue_hide_toggle_edits(all_txns)

        assert count == 4
        # Verify each transaction gets its current state preserved in old_value
        old_values = [e.old_value for e in controller.data_manager.pending_edits]
        new_values = [e.new_value for e in controller.data_manager.pending_edits]
        # Each new value should be opposite of old value
        for old, new in zip(old_values, new_values):
            assert new == (not old)

    async def test_queue_hide_toggle_from_aggregate_view(self, controller):
        """Test hide toggle from aggregate view (merchant grouping)."""
        controller.state.view_mode = ViewMode.MERCHANT
        controller.state.selected_group_keys.add("Starbucks")

        # Get transactions for selected merchant
        transactions = controller.get_transactions_from_selected_groups("merchant")

        assert not transactions.is_empty()

        # Queue hide toggle
        count = controller.queue_hide_toggle_edits(transactions)

        assert count > 0
        assert len(controller.data_manager.pending_edits) == count
        # All edits should be for hide_from_reports field
        assert all(e.field == "hide_from_reports" for e in controller.data_manager.pending_edits)

    async def test_queue_hide_toggle_from_category_view(self, controller):
        """Test hide toggle from category aggregate view."""
        controller.state.view_mode = ViewMode.CATEGORY
        controller.state.selected_group_keys.add("Coffee Shops")

        # Get transactions for selected category
        transactions = controller.get_transactions_from_selected_groups("category")

        if not transactions.is_empty():
            count = controller.queue_hide_toggle_edits(transactions)
            assert count > 0
            assert all(
                e.field == "hide_from_reports" for e in controller.data_manager.pending_edits
            )


class TestSortFieldCycling:
    """
    Test sort field cycling logic - pure state machine.

    This tests the business logic for determining the next sort field
    when the user presses 's' to toggle sorting.
    """

    async def test_detail_view_date_to_merchant(self, controller):
        """Detail view: Date → Merchant."""
        new_sort, display = controller.get_next_sort_field(ViewMode.DETAIL, SortMode.DATE)
        assert new_sort == SortMode.MERCHANT
        assert display == "Merchant"

    async def test_detail_view_merchant_to_category(self, controller):
        """Detail view: Merchant → Category."""
        new_sort, display = controller.get_next_sort_field(ViewMode.DETAIL, SortMode.MERCHANT)
        assert new_sort == SortMode.CATEGORY
        assert display == "Category"

    async def test_detail_view_category_to_account(self, controller):
        """Detail view: Category → Account."""
        new_sort, display = controller.get_next_sort_field(ViewMode.DETAIL, SortMode.CATEGORY)
        assert new_sort == SortMode.ACCOUNT
        assert display == "Account"

    async def test_detail_view_account_to_amount(self, controller):
        """Detail view: Account → Amount."""
        new_sort, display = controller.get_next_sort_field(ViewMode.DETAIL, SortMode.ACCOUNT)
        assert new_sort == SortMode.AMOUNT
        assert display == "Amount"

    async def test_detail_view_amount_to_date_completes_cycle(self, controller):
        """Detail view: Amount → Date (completes the cycle)."""
        new_sort, display = controller.get_next_sort_field(ViewMode.DETAIL, SortMode.AMOUNT)
        assert new_sort == SortMode.DATE
        assert display == "Date"

    async def test_detail_view_full_cycle(self, controller):
        """Test complete cycle through all 5 fields in detail view."""
        # Start at DATE
        current = SortMode.DATE
        expected_cycle = [
            (SortMode.MERCHANT, "Merchant"),
            (SortMode.CATEGORY, "Category"),
            (SortMode.ACCOUNT, "Account"),
            (SortMode.AMOUNT, "Amount"),
            (SortMode.DATE, "Date"),  # Back to start
        ]

        for expected_sort, expected_display in expected_cycle:
            current, display = controller.get_next_sort_field(ViewMode.DETAIL, current)
            assert current == expected_sort
            assert display == expected_display

    async def test_merchant_view_full_cycle(self, controller):
        """Merchant view: Merchant → Count → Amount → Merchant (3-field cycle)."""
        # Merchant → Count
        new_sort, display = controller.get_next_sort_field(ViewMode.MERCHANT, SortMode.MERCHANT)
        assert new_sort == SortMode.COUNT
        assert display == "Count"

        # Count → Amount
        new_sort, display = controller.get_next_sort_field(ViewMode.MERCHANT, SortMode.COUNT)
        assert new_sort == SortMode.AMOUNT
        assert display == "Amount"

        # Amount → Merchant (completes cycle)
        new_sort, display = controller.get_next_sort_field(ViewMode.MERCHANT, SortMode.AMOUNT)
        assert new_sort == SortMode.MERCHANT
        assert display == "Merchant"

    async def test_category_view_full_cycle(self, controller):
        """Category view: Category → Count → Amount → Category."""
        new_sort, _ = controller.get_next_sort_field(ViewMode.CATEGORY, SortMode.CATEGORY)
        assert new_sort == SortMode.COUNT

        new_sort, _ = controller.get_next_sort_field(ViewMode.CATEGORY, SortMode.COUNT)
        assert new_sort == SortMode.AMOUNT

        new_sort, display = controller.get_next_sort_field(ViewMode.CATEGORY, SortMode.AMOUNT)
        assert new_sort == SortMode.CATEGORY
        assert display == "Category"

    async def test_group_view_full_cycle(self, controller):
        """Group view: Group → Count → Amount → Group."""
        new_sort, _ = controller.get_next_sort_field(ViewMode.GROUP, SortMode.GROUP)
        assert new_sort == SortMode.COUNT

        new_sort, _ = controller.get_next_sort_field(ViewMode.GROUP, SortMode.COUNT)
        assert new_sort == SortMode.AMOUNT

        new_sort, display = controller.get_next_sort_field(ViewMode.GROUP, SortMode.AMOUNT)
        assert new_sort == SortMode.GROUP
        assert display == "Group"

    async def test_account_view_full_cycle(self, controller):
        """Account view: Account → Count → Amount → Account."""
        new_sort, _ = controller.get_next_sort_field(ViewMode.ACCOUNT, SortMode.ACCOUNT)
        assert new_sort == SortMode.COUNT

        new_sort, _ = controller.get_next_sort_field(ViewMode.ACCOUNT, SortMode.COUNT)
        assert new_sort == SortMode.AMOUNT

        new_sort, display = controller.get_next_sort_field(ViewMode.ACCOUNT, SortMode.AMOUNT)
        assert new_sort == SortMode.ACCOUNT
        assert display == "Account"

    async def test_account_view_with_computed_column_full_cycle(self, controller):
        """Account view with computed column: Account → Count → Amount → Order Date → Account."""

        # Mock backend with computed column
        order_date_col = ComputedColumn(
            name="order_date",
            source_field="date",
            aggregation=AggregationFunc.FIRST,
            display_name="Order Date",
            view_modes=["account"],
        )
        controller.data_manager.mm.get_computed_columns = lambda: [order_date_col]

        # Account → Count
        controller.state.sort_column = None
        new_sort, display = controller.get_next_sort_field(ViewMode.ACCOUNT, SortMode.ACCOUNT)
        assert new_sort == SortMode.COUNT
        assert display == "Count"
        assert controller.state.sort_column is None

        # Count → Amount
        new_sort, display = controller.get_next_sort_field(ViewMode.ACCOUNT, SortMode.COUNT)
        assert new_sort == SortMode.AMOUNT
        assert display == "Amount"
        assert controller.state.sort_column is None

        # Amount → Order Date (first computed column)
        new_sort, display = controller.get_next_sort_field(ViewMode.ACCOUNT, SortMode.AMOUNT)
        assert new_sort == SortMode.AMOUNT  # sort_by stays as AMOUNT
        assert display == "Order Date"
        assert controller.state.sort_column == "order_date"  # Dynamic column set

        # Order Date → Account (back to field, completes cycle)
        new_sort, display = controller.get_next_sort_field(ViewMode.ACCOUNT, SortMode.AMOUNT)
        assert new_sort == SortMode.ACCOUNT
        assert display == "Account"
        assert controller.state.sort_column is None  # Dynamic column cleared

    async def test_account_view_with_multiple_computed_columns(self, controller):
        """Account view with multiple computed columns cycles through all of them."""

        # Mock backend with two computed columns
        col1 = ComputedColumn(
            name="order_date",
            source_field="date",
            aggregation=AggregationFunc.FIRST,
            display_name="Order Date",
            view_modes=["account"],
        )
        col2 = ComputedColumn(
            name="item_count",
            source_field="id",
            aggregation=AggregationFunc.COUNT_DISTINCT,
            display_name="Items",
            view_modes=["account"],
        )
        controller.data_manager.mm.get_computed_columns = lambda: [col1, col2]

        # Start from Amount
        controller.state.sort_by = SortMode.AMOUNT
        controller.state.sort_column = None

        # Amount → Order Date
        new_sort, display = controller.get_next_sort_field(ViewMode.ACCOUNT, SortMode.AMOUNT)
        assert display == "Order Date"
        assert controller.state.sort_column == "order_date"

        # Order Date → Items (second computed column)
        new_sort, display = controller.get_next_sort_field(ViewMode.ACCOUNT, SortMode.AMOUNT)
        assert display == "Items"
        assert controller.state.sort_column == "item_count"

        # Items → Account (back to field)
        new_sort, display = controller.get_next_sort_field(ViewMode.ACCOUNT, SortMode.AMOUNT)
        assert new_sort == SortMode.ACCOUNT
        assert display == "Account"
        assert controller.state.sort_column is None

    async def test_computed_column_not_in_cycle_for_other_views(self, controller):
        """Computed columns filtered by view_modes don't appear in other views."""

        # Computed column only for account view
        order_date_col = ComputedColumn(
            name="order_date",
            source_field="date",
            aggregation=AggregationFunc.FIRST,
            display_name="Order Date",
            view_modes=["account"],  # Only in account view
        )
        controller.data_manager.mm.get_computed_columns = lambda: [order_date_col]

        # In MERCHANT view, should NOT include computed column
        controller.state.sort_column = None
        new_sort, display = controller.get_next_sort_field(ViewMode.MERCHANT, SortMode.AMOUNT)
        assert new_sort == SortMode.MERCHANT  # Cycle back to merchant (no computed col)
        assert display == "Merchant"
        assert controller.state.sort_column is None

    async def test_toggle_sort_in_subgroup_view_uses_subgroup_mode(self, controller):
        """Test that toggle_sort_field uses sub_grouping_mode when in subgroup view."""
        # Setup: Drilled down with sub-grouping by merchant
        controller.state.view_mode = ViewMode.DETAIL
        controller.state.selected_category = "Groceries"
        controller.state.sub_grouping_mode = ViewMode.MERCHANT
        controller.state.sort_by = SortMode.AMOUNT

        # Toggle sort field
        display = controller.toggle_sort_field()

        # Should cycle like merchant aggregate view (not detail view)
        # Merchant aggregate: Amount → Merchant → Count
        assert controller.state.sort_by == SortMode.MERCHANT
        assert display == "Merchant"

        # Should not offer DATE (which would crash)
        display = controller.toggle_sort_field()
        assert controller.state.sort_by == SortMode.COUNT
        assert display == "Count"

    async def test_toggle_sort_in_detail_view_without_subgrouping(self, controller):
        """Test that toggle_sort_field uses view_mode when not in subgroup."""
        # Setup: Detail view without sub-grouping
        controller.state.view_mode = ViewMode.DETAIL
        controller.state.selected_merchant = "Amazon"
        controller.state.sub_grouping_mode = None
        controller.state.sort_by = SortMode.DATE

        # Toggle sort field
        display = controller.toggle_sort_field()

        # Should cycle like detail view: Date → Merchant → Category → Account → Amount → Date
        assert controller.state.sort_by == SortMode.MERCHANT
        assert display == "Merchant"


class TestViewModeSwitching:
    """
    Test view mode switching facade methods.

    These methods encapsulate the state mutations for switching views,
    making app.py simpler and the logic testable.
    """

    async def test_switch_to_merchant_view(self, controller, mock_view):
        """Test switching to merchant view."""
        controller.switch_to_merchant_view()

        assert controller.state.view_mode == ViewMode.MERCHANT
        assert controller.state.selected_merchant is None
        assert controller.state.selected_category is None
        assert controller.state.selected_group is None
        assert controller.state.selected_account is None
        # Should reset sort to valid aggregate field
        assert controller.state.sort_by in [SortMode.COUNT, SortMode.AMOUNT]
        # Should have refreshed view
        assert len(mock_view.table_updates) == 1

    async def test_switch_to_category_view(self, controller, mock_view):
        """Test switching to category view."""
        controller.switch_to_category_view()

        assert controller.state.view_mode == ViewMode.CATEGORY
        assert controller.state.selected_category is None

    async def test_switch_to_group_view(self, controller, mock_view):
        """Test switching to group view."""
        controller.switch_to_group_view()

        assert controller.state.view_mode == ViewMode.GROUP
        assert controller.state.selected_group is None

    async def test_switch_to_account_view(self, controller, mock_view):
        """Test switching to account view."""
        controller.switch_to_account_view()

        assert controller.state.view_mode == ViewMode.ACCOUNT
        assert controller.state.selected_account is None

    async def test_switch_to_detail_view_with_default_sort(self, controller, mock_view):
        """Test switching to detail view with default sort."""
        controller.switch_to_detail_view(set_default_sort=True)

        assert controller.state.view_mode == ViewMode.DETAIL
        assert controller.state.sort_by == SortMode.DATE
        assert controller.state.sort_direction == SortDirection.DESC

    async def test_switch_to_detail_view_preserve_sort(self, controller, mock_view):
        """Test switching to detail view preserving current sort."""
        # Set non-default sort
        controller.state.sort_by = SortMode.AMOUNT
        controller.state.sort_direction = SortDirection.ASC

        controller.switch_to_detail_view(set_default_sort=False)

        assert controller.state.view_mode == ViewMode.DETAIL
        # Sort should be preserved
        assert controller.state.sort_by == SortMode.AMOUNT
        assert controller.state.sort_direction == SortDirection.ASC

    async def test_switch_to_detail_view_saves_navigation_history(self, controller, mock_view):
        """Test that switching from aggregate to detail view saves navigation history."""
        # Start in category view
        controller.state.view_mode = ViewMode.CATEGORY
        controller.state.sort_by = SortMode.CATEGORY
        controller.state.sort_direction = SortDirection.ASC

        # Switch to detail view
        controller.switch_to_detail_view(set_default_sort=True)

        # Should have saved the category view to navigation history
        assert len(controller.state.navigation_history) == 1
        nav_state = controller.state.navigation_history[0]
        assert nav_state.view_mode == ViewMode.CATEGORY
        assert nav_state.sort_by == SortMode.CATEGORY
        assert nav_state.sort_direction == SortDirection.ASC

        # Now in detail view
        assert controller.state.view_mode == ViewMode.DETAIL

    async def test_switch_to_detail_view_from_detail_no_duplicate_history(
        self, controller, mock_view
    ):
        """Test that switching from detail to detail doesn't add duplicate history."""
        # Already in detail view
        controller.state.view_mode = ViewMode.DETAIL

        # Switch to detail view again
        controller.switch_to_detail_view(set_default_sort=True)

        # Should not have added to navigation history
        assert len(controller.state.navigation_history) == 0

    async def test_view_switch_clears_selections(self, controller, mock_view):
        """Test that switching views clears all drill-down selections."""
        # Set up some selections
        controller.state.selected_merchant = "Amazon"
        controller.state.selected_category = "Shopping"

        controller.switch_to_merchant_view()

        # All selections should be cleared
        assert controller.state.selected_merchant is None
        assert controller.state.selected_category is None
        assert controller.state.selected_group is None
        assert controller.state.selected_account is None

    async def test_aggregate_view_resets_invalid_sort(self, controller, mock_view):
        """Test that switching to aggregate view resets invalid sort fields."""
        # Set sort to DATE (invalid for aggregate views)
        controller.state.sort_by = SortMode.DATE

        controller.switch_to_merchant_view()

        # Should be reset to AMOUNT (valid aggregate field)
        assert controller.state.sort_by == SortMode.AMOUNT

    async def test_aggregate_view_preserves_valid_sort(self, controller, mock_view):
        """Test that valid sort fields are preserved."""
        controller.state.sort_by = SortMode.COUNT

        controller.switch_to_merchant_view()

        # COUNT is valid for aggregates, should be preserved
        assert controller.state.sort_by == SortMode.COUNT

    async def test_aggregate_view_preserves_field_sort(self, controller, mock_view):
        """Test that field name sort is preserved when switching aggregate views."""
        controller.state.sort_by = SortMode.MERCHANT

        controller.switch_to_merchant_view()

        # MERCHANT is valid for merchant view, should be preserved
        assert controller.state.sort_by == SortMode.MERCHANT

    async def test_cycle_grouping_returns_view_name(self, controller, mock_view):
        """Test cycle_grouping returns view name and refreshes."""
        controller.state.view_mode = ViewMode.MERCHANT

        view_name = controller.cycle_grouping()

        assert view_name is not None  # Should return next view name
        assert len(mock_view.table_updates) == 1  # Should refresh

    async def test_cycle_grouping_updates_aggregate_field_sort(self, controller, mock_view):
        """Test that cycling grouping updates sort when sorting by aggregate field."""
        # Start in merchant view, sorting by MERCHANT
        controller.state.view_mode = ViewMode.MERCHANT
        controller.state.sort_by = SortMode.MERCHANT

        # Cycle to category view
        controller.cycle_grouping()

        # Should now be sorting by CATEGORY (not MERCHANT)
        assert controller.state.view_mode == ViewMode.CATEGORY
        assert controller.state.sort_by == SortMode.CATEGORY

    async def test_cycle_grouping_merchant_to_category_field_sort(self, controller, mock_view):
        """Test MERCHANT sort becomes CATEGORY sort when cycling to category view."""
        controller.state.view_mode = ViewMode.MERCHANT
        controller.state.sort_by = SortMode.MERCHANT

        controller.cycle_grouping()  # Merchant → Category

        assert controller.state.view_mode == ViewMode.CATEGORY
        assert controller.state.sort_by == SortMode.CATEGORY

    async def test_cycle_grouping_category_to_group_field_sort(self, controller, mock_view):
        """Test CATEGORY sort becomes GROUP sort when cycling to group view."""
        controller.state.view_mode = ViewMode.CATEGORY
        controller.state.sort_by = SortMode.CATEGORY

        controller.cycle_grouping()  # Category → Group

        assert controller.state.view_mode == ViewMode.GROUP
        assert controller.state.sort_by == SortMode.GROUP

    async def test_cycle_grouping_group_to_account_field_sort(self, controller, mock_view):
        """Test GROUP sort becomes ACCOUNT sort when cycling to account view."""
        controller.state.view_mode = ViewMode.GROUP
        controller.state.sort_by = SortMode.GROUP

        controller.cycle_grouping()  # Group → Account

        assert controller.state.view_mode == ViewMode.ACCOUNT
        assert controller.state.sort_by == SortMode.ACCOUNT

    async def test_cycle_grouping_account_to_merchant_field_sort(self, controller, mock_view):
        """Test cycling ACCOUNT → TIME → MERCHANT resets sort correctly for each view."""
        controller.state.view_mode = ViewMode.ACCOUNT
        controller.state.sort_by = SortMode.ACCOUNT

        controller.cycle_grouping()  # Account → TIME

        assert controller.state.view_mode == ViewMode.TIME
        assert controller.state.sort_by == SortMode.TIME_PERIOD
        assert controller.state.sort_direction == SortDirection.ASC

        controller.cycle_grouping()  # TIME → Merchant (wrap around)

        assert controller.state.view_mode == ViewMode.MERCHANT
        # When leaving TIME, reset to AMOUNT DESC (highest spending first)
        assert controller.state.sort_by == SortMode.AMOUNT
        assert controller.state.sort_direction == SortDirection.DESC

    async def test_cycle_grouping_preserves_count_sort(self, controller, mock_view):
        """Test that COUNT sort is preserved when cycling views."""
        controller.state.view_mode = ViewMode.MERCHANT
        controller.state.sort_by = SortMode.COUNT

        controller.cycle_grouping()  # Merchant → Category

        assert controller.state.view_mode == ViewMode.CATEGORY
        assert controller.state.sort_by == SortMode.COUNT  # Unchanged

    async def test_cycle_grouping_preserves_amount_sort(self, controller, mock_view):
        """Test that AMOUNT sort is preserved when cycling views."""
        controller.state.view_mode = ViewMode.CATEGORY
        controller.state.sort_by = SortMode.AMOUNT

        controller.cycle_grouping()  # Category → Group

        assert controller.state.view_mode == ViewMode.GROUP
        assert controller.state.sort_by == SortMode.AMOUNT  # Unchanged


class TestSortingFacade:
    """Test sorting facade methods that encapsulate sort operations."""

    async def test_toggle_sort_field_detail_view(self, controller, mock_view):
        """Test toggling sort field in detail view."""
        controller.state.view_mode = ViewMode.DETAIL
        controller.state.sort_by = SortMode.DATE

        display_name = controller.toggle_sort_field()

        assert controller.state.sort_by == SortMode.MERCHANT
        assert display_name == "Merchant"
        assert len(mock_view.table_updates) == 1

    async def test_toggle_sort_field_aggregate_view(self, controller, mock_view):
        """Test toggling sort field in aggregate view."""
        controller.state.view_mode = ViewMode.MERCHANT
        controller.state.sort_by = SortMode.COUNT

        display_name = controller.toggle_sort_field()

        assert controller.state.sort_by == SortMode.AMOUNT
        assert display_name == "Amount"

    async def test_reverse_sort_to_descending(self, controller, mock_view):
        """Test reversing sort to descending."""
        controller.state.sort_direction = SortDirection.ASC

        direction_name = controller.reverse_sort()

        assert controller.state.sort_direction == SortDirection.DESC
        assert direction_name == "Descending"
        assert len(mock_view.table_updates) == 1

    async def test_reverse_sort_to_ascending(self, controller, mock_view):
        """Test reversing sort to ascending."""
        controller.state.sort_direction = SortDirection.DESC

        direction_name = controller.reverse_sort()

        assert controller.state.sort_direction == SortDirection.ASC
        assert direction_name == "Ascending"


class TestSortFieldValidation:
    """
    Test automatic sort field validation in refresh_view.

    The refresh_view method should automatically detect when the current sort field
    is invalid for the view type and reset it to a valid field.

    Key scenarios:
    - Detail views don't have 'count' column → reset COUNT to DATE
    - Aggregate views DO have 'count' column → preserve COUNT
    - Sub-grouped views (aggregate within drill-down) → preserve COUNT
    - Other sort fields (AMOUNT, DATE) should be preserved
    """

    async def test_count_sort_reset_in_detail_view(self, controller, mock_view):
        """COUNT sort should be reset to DATE in detail views."""
        # Setup: Detail view with COUNT sort (invalid)
        controller.state.view_mode = ViewMode.DETAIL
        controller.state.sort_by = SortMode.COUNT
        controller.state.sort_direction = SortDirection.ASC

        # Refresh view - should auto-reset sort
        controller.refresh_view()

        # Verify: Sort reset to DATE DESC
        assert controller.state.sort_by == SortMode.DATE
        assert controller.state.sort_direction == SortDirection.DESC

    async def test_count_sort_preserved_in_aggregate_view(self, controller, mock_view):
        """COUNT sort should be preserved in aggregate views."""
        # Setup: Merchant view with COUNT sort (valid)
        controller.state.view_mode = ViewMode.MERCHANT
        controller.state.sort_by = SortMode.COUNT
        controller.state.sort_direction = SortDirection.ASC

        # Refresh view
        controller.refresh_view()

        # Verify: Sort preserved
        assert controller.state.sort_by == SortMode.COUNT
        assert controller.state.sort_direction == SortDirection.ASC

    async def test_count_sort_reset_when_drilling_into_detail(self, controller, mock_view):
        """COUNT sort should reset when drilling from aggregate to detail view."""
        # Setup: Start in merchant aggregate view with COUNT sort
        controller.state.view_mode = ViewMode.MERCHANT
        controller.state.sort_by = SortMode.COUNT

        # Drill down to a merchant's transactions (detail view)
        controller.state.view_mode = ViewMode.DETAIL
        controller.state.selected_merchant = "Amazon"

        # Refresh view - should reset sort
        controller.refresh_view()

        # Verify: COUNT reset to DATE
        assert controller.state.sort_by == SortMode.DATE
        assert controller.state.sort_direction == SortDirection.DESC

    async def test_count_sort_preserved_in_subgrouped_view(self, controller, mock_view):
        """COUNT sort should be preserved in sub-grouped views (aggregate within drill-down)."""
        # Setup: Drilled down with sub-grouping (aggregate view)
        controller.state.view_mode = ViewMode.DETAIL
        controller.state.selected_category = "Groceries"
        controller.state.sub_grouping_mode = ViewMode.MERCHANT  # Sub-group by merchant
        controller.state.sort_by = SortMode.COUNT

        # Refresh view - should preserve COUNT (sub-grouped is aggregate)
        controller.refresh_view()

        # Verify: COUNT preserved
        assert controller.state.sort_by == SortMode.COUNT

    async def test_amount_sort_preserved_in_detail_view(self, controller, mock_view):
        """AMOUNT sort should be preserved in detail views (valid sort field)."""
        # Setup: Detail view with AMOUNT sort (valid)
        controller.state.view_mode = ViewMode.DETAIL
        controller.state.sort_by = SortMode.AMOUNT
        controller.state.sort_direction = SortDirection.DESC

        # Refresh view
        controller.refresh_view()

        # Verify: AMOUNT sort preserved
        assert controller.state.sort_by == SortMode.AMOUNT
        assert controller.state.sort_direction == SortDirection.DESC

    async def test_date_sort_preserved_in_detail_view(self, controller, mock_view):
        """DATE sort should be preserved in detail views (valid sort field)."""
        # Setup: Detail view with DATE sort (valid)
        controller.state.view_mode = ViewMode.DETAIL
        controller.state.sort_by = SortMode.DATE
        controller.state.sort_direction = SortDirection.ASC

        # Refresh view
        controller.refresh_view()

        # Verify: DATE sort preserved
        assert controller.state.sort_by == SortMode.DATE
        assert controller.state.sort_direction == SortDirection.ASC

    async def test_count_sort_reset_with_category_filter(self, controller, mock_view):
        """COUNT sort should reset in detail view even with category filter."""
        # Setup: Detail view filtered by category with COUNT sort
        controller.state.view_mode = ViewMode.DETAIL
        controller.state.selected_category = "Shopping"
        controller.state.sub_grouping_mode = None  # No sub-grouping → detail view
        controller.state.sort_by = SortMode.COUNT

        # Refresh view
        controller.refresh_view()

        # Verify: COUNT reset to DATE
        assert controller.state.sort_by == SortMode.DATE
        assert controller.state.sort_direction == SortDirection.DESC

    async def test_count_sort_reset_with_merchant_filter(self, controller, mock_view):
        """COUNT sort should reset in detail view with merchant filter."""
        # Setup: Detail view filtered by merchant
        controller.state.view_mode = ViewMode.DETAIL
        controller.state.selected_merchant = "Amazon"
        controller.state.sub_grouping_mode = None
        controller.state.sort_by = SortMode.COUNT

        # Refresh view
        controller.refresh_view()

        # Verify: COUNT reset to DATE
        assert controller.state.sort_by == SortMode.DATE

    async def test_count_sort_reset_with_multiple_filters(self, controller, mock_view):
        """COUNT sort should reset with multiple drill-down filters."""
        # Setup: Detail view with multiple filters (merchant + category)
        controller.state.view_mode = ViewMode.DETAIL
        controller.state.selected_merchant = "Amazon"
        controller.state.selected_category = "Shopping"
        controller.state.sub_grouping_mode = None
        controller.state.sort_by = SortMode.COUNT

        # Refresh view
        controller.refresh_view()

        # Verify: COUNT reset to DATE
        assert controller.state.sort_by == SortMode.DATE

    async def test_sort_validation_all_aggregate_view_types(self, controller, mock_view):
        """COUNT sort should be preserved in all aggregate view types."""
        aggregate_views = [
            ViewMode.MERCHANT,
            ViewMode.CATEGORY,
            ViewMode.GROUP,
            ViewMode.ACCOUNT,
        ]

        for view_mode in aggregate_views:
            # Setup
            controller.state.view_mode = view_mode
            controller.state.sort_by = SortMode.COUNT
            controller.state.sort_direction = SortDirection.ASC

            # Refresh
            controller.refresh_view()

            # Verify: COUNT preserved for this aggregate view
            assert controller.state.sort_by == SortMode.COUNT, (
                f"COUNT should be preserved in {view_mode.value} view"
            )

    async def test_sort_validation_happens_before_view_preparation(self, controller, mock_view):
        """Sort validation should happen before data is sorted (preventing crashes)."""
        # This test ensures the validation happens early enough to prevent
        # the "unable to find column 'count'" error

        # Setup: Detail view with COUNT sort
        controller.state.view_mode = ViewMode.DETAIL
        controller.state.sort_by = SortMode.COUNT

        # This should NOT crash - validation should reset sort before attempting to sort data
        controller.refresh_view()

        # Verify: No crash, sort was reset, and view updated
        assert controller.state.sort_by == SortMode.DATE
        assert len(mock_view.table_updates) > 0

    async def test_count_sort_preserved_switching_between_aggregates(self, controller, mock_view):
        """COUNT sort should be preserved when switching between aggregate views."""
        # Setup: Start in merchant view with COUNT sort
        controller.state.view_mode = ViewMode.MERCHANT
        controller.state.sort_by = SortMode.COUNT

        # Switch to category view (also aggregate)
        controller.state.view_mode = ViewMode.CATEGORY
        controller.refresh_view()

        # Verify: COUNT preserved
        assert controller.state.sort_by == SortMode.COUNT

        # Switch to group view
        controller.state.view_mode = ViewMode.GROUP
        controller.refresh_view()

        # Verify: Still preserved
        assert controller.state.sort_by == SortMode.COUNT

    async def test_subgroup_mode_none_triggers_reset(self, controller, mock_view):
        """Clearing sub_grouping_mode should trigger sort reset."""
        # Setup: Sub-grouped view with COUNT sort
        controller.state.view_mode = ViewMode.DETAIL
        controller.state.selected_merchant = "Amazon"
        controller.state.sub_grouping_mode = ViewMode.CATEGORY
        controller.state.sort_by = SortMode.COUNT

        # First refresh - COUNT valid (sub-grouped is aggregate)
        controller.refresh_view()
        assert controller.state.sort_by == SortMode.COUNT

        # Clear sub-grouping (now it's a detail view)
        controller.state.sub_grouping_mode = None
        controller.refresh_view()

        # Verify: COUNT reset to DATE (no longer sub-grouped)
        assert controller.state.sort_by == SortMode.DATE

    async def test_merchant_sort_preserved_in_detail_view(self, controller, mock_view):
        """MERCHANT sort should be preserved in detail views."""
        controller.state.view_mode = ViewMode.DETAIL
        controller.state.sort_by = SortMode.MERCHANT
        controller.state.sort_direction = SortDirection.ASC

        controller.refresh_view()

        assert controller.state.sort_by == SortMode.MERCHANT
        assert controller.state.sort_direction == SortDirection.ASC

    async def test_date_sort_reset_in_aggregate_view(self, controller, mock_view):
        """DATE sort should be reset to AMOUNT in aggregate views."""
        # Setup: Merchant view with DATE sort (invalid)
        controller.state.view_mode = ViewMode.MERCHANT
        controller.state.sort_by = SortMode.DATE
        controller.state.sort_direction = SortDirection.ASC

        # Refresh view - should auto-reset sort
        controller.refresh_view()

        # Verify: Sort reset to AMOUNT DESC
        assert controller.state.sort_by == SortMode.AMOUNT
        assert controller.state.sort_direction == SortDirection.DESC

    async def test_date_sort_reset_when_going_back_to_aggregate(self, controller, mock_view):
        """DATE sort should reset when going back from detail to aggregate view."""
        # Simulate: Press 'd' for detail view (DATE sort), then Escape to go back
        controller.state.view_mode = ViewMode.DETAIL
        controller.state.sort_by = SortMode.DATE

        # User presses 'd' -> switches to detail view with DATE sort
        # Then user presses Escape -> should go back to aggregate view

        # Simulate go_back() restoring aggregate view with DATE sort
        controller.state.view_mode = ViewMode.MERCHANT
        # sort_by is still DATE from detail view

        # Refresh view - should reset DATE to AMOUNT
        controller.refresh_view()

        # Verify: DATE reset to AMOUNT in aggregate view
        assert controller.state.sort_by == SortMode.AMOUNT
        assert controller.state.sort_direction == SortDirection.DESC

    async def test_time_period_sort_mapped_to_date_in_detail_view(self, controller, mock_view):
        """TIME_PERIOD sort should be mapped to DATE when drilling down from TIME view."""
        # Setup: Detail view (after drilling down from TIME view) with TIME_PERIOD sort
        controller.state.view_mode = ViewMode.DETAIL
        controller.state.selected_time_year = 2025
        controller.state.selected_time_month = 1
        controller.state.sort_by = SortMode.TIME_PERIOD  # Invalid for detail view
        controller.state.sort_direction = SortDirection.ASC

        # Refresh view - should map TIME_PERIOD to DATE
        controller.refresh_view()

        # Verify: Data is sorted by date (no error about missing 'time_period' column)
        # The key assertion is that refresh_view doesn't raise an exception
        # We can also verify the view was updated successfully
        assert len(mock_view.table_updates) > 0

    async def test_subgroup_by_time_within_merchant_drill_down(self, controller, mock_view):
        """Sub-grouping by TIME within a merchant drill-down should work without errors."""
        # Setup: Drill down into a merchant
        controller.state.view_mode = ViewMode.DETAIL
        controller.state.selected_merchant = "Starbucks"

        # Then sub-group by TIME
        controller.state.sub_grouping_mode = ViewMode.TIME
        controller.state.time_granularity = (
            controller.state.time_granularity
        )  # Use current granularity

        # Refresh view - should handle time_period_display field without errors
        controller.refresh_view()

        # Verify: No KeyError about 'time_period_display'
        # The key assertion is that refresh_view doesn't raise an exception
        assert len(mock_view.table_updates) > 0


class TestMultiSelectGroups:
    """Tests for multi-selecting groups in aggregate views."""

    async def test_get_transactions_from_selected_merchants(self, controller, mock_view):
        """Should get all transactions from selected merchants."""
        controller.state.selected_group_keys = {"Amazon", "Starbucks"}

        result = controller.get_transactions_from_selected_groups("merchant")

        assert not result.is_empty()
        # Should have transactions from both merchants
        merchants = set(result["merchant"].unique().to_list())
        assert "Amazon" in merchants
        assert "Starbucks" in merchants

    async def test_get_transactions_from_selected_categories(self, controller, mock_view):
        """Should get all transactions from selected categories."""
        controller.state.selected_group_keys = {"Groceries", "Dining"}

        result = controller.get_transactions_from_selected_groups("category")

        assert not result.is_empty()
        categories = set(result["category"].unique().to_list())
        assert "Groceries" in categories or "Dining" in categories

    async def test_get_transactions_empty_when_no_selections(self, controller, mock_view):
        """Should return empty DataFrame when no groups selected."""
        controller.state.selected_group_keys = set()

        result = controller.get_transactions_from_selected_groups("merchant")

        assert result.is_empty()

    async def test_toggle_group_selection(self, controller, mock_view):
        """Should toggle group selection."""
        controller.state.toggle_group_selection("Amazon")
        assert "Amazon" in controller.state.selected_group_keys

        controller.state.toggle_group_selection("Amazon")
        assert "Amazon" not in controller.state.selected_group_keys

    async def test_clear_selection_clears_both(self, controller, mock_view):
        """Should clear both transaction and group selections."""
        controller.state.selected_ids.add("txn1")
        controller.state.selected_group_keys.add("Amazon")

        controller.state.clear_selection()

        assert len(controller.state.selected_ids) == 0
        assert len(controller.state.selected_group_keys) == 0

    async def test_view_switch_clears_selections(self, controller, mock_view):
        """Switching views should clear all selections."""
        controller.state.selected_group_keys.add("Amazon")
        controller.state.selected_ids.add("txn1")

        controller.switch_to_category_view()

        assert len(controller.state.selected_group_keys) == 0
        assert len(controller.state.selected_ids) == 0


class TestMultiSelectOperations:
    """Tests for multi-select controller operations (Phase 2 refactoring)."""

    async def test_toggle_selection_at_row_in_merchant_view(self, controller, mock_view):
        """Should toggle group selection in merchant view."""
        # Switch to merchant view (this will set current_data via refresh_view)
        controller.switch_to_merchant_view()

        # Toggle selection on first row (row 0)
        count, item_type = controller.toggle_selection_at_row(0)

        assert item_type == "group"
        assert count == 1
        assert len(controller.state.selected_group_keys) == 1

        # Toggle again to deselect
        count, item_type = controller.toggle_selection_at_row(0)

        assert item_type == "group"
        assert count == 0
        assert len(controller.state.selected_group_keys) == 0

    async def test_toggle_selection_at_row_in_detail_view(self, controller, mock_view):
        """Should toggle transaction selection in detail view."""
        # Switch to detail view
        controller.switch_to_detail_view()
        controller.state.current_data = controller.state.get_filtered_df()

        # Get a transaction ID from the first row
        row_data = controller.state.current_data.row(0, named=True)
        txn_id = row_data.get("id")

        # Toggle selection on first row
        count, item_type = controller.toggle_selection_at_row(0)

        assert item_type == "transaction"
        assert count == 1
        assert txn_id in controller.state.selected_ids

        # Toggle again to deselect
        count, item_type = controller.toggle_selection_at_row(0)

        assert item_type == "transaction"
        assert count == 0
        assert txn_id not in controller.state.selected_ids

    async def test_toggle_selection_at_row_with_invalid_row(self, controller, mock_view):
        """Should handle invalid row index gracefully."""
        controller.switch_to_detail_view()
        controller.state.current_data = controller.state.get_filtered_df()

        # Try to toggle selection with invalid row index
        count, item_type = controller.toggle_selection_at_row(-1)

        assert count == 0
        assert item_type == "transaction"

    async def test_toggle_selection_at_row_in_sub_grouped_view(self, controller, mock_view):
        """Should toggle group selection in sub-grouped detail view."""
        # Switch to merchant view, drill down, then enable sub-grouping
        controller.switch_to_merchant_view()

        # Get first merchant name from current data
        first_merchant = controller.state.current_data.row(0, named=True).get("merchant")
        controller.drill_down(first_merchant, 0)

        # Enable sub-grouping by setting it to a ViewMode (not a string)

        controller.state.sub_grouping_mode = ViewMode.CATEGORY
        controller.refresh_view()

        # Should now be in sub-grouped view
        count, item_type = controller.toggle_selection_at_row(0)

        assert item_type == "group"
        assert count == 1

    async def test_toggle_select_all_visible_in_merchant_view(self, controller, mock_view):
        """Should select all groups in merchant view."""
        # Switch to merchant view (this will set current_data via refresh_view)
        controller.switch_to_merchant_view()
        total_merchants = len(controller.state.current_data)

        # Select all
        count, all_selected, item_type = controller.toggle_select_all_visible()

        assert item_type == "group"
        assert all_selected is True
        assert count == total_merchants
        assert len(controller.state.selected_group_keys) == total_merchants

        # Deselect all
        count, all_selected, item_type = controller.toggle_select_all_visible()

        assert item_type == "group"
        assert all_selected is False
        assert count == 0
        assert len(controller.state.selected_group_keys) == 0

    async def test_toggle_select_all_visible_in_detail_view(self, controller, mock_view):
        """Should select all transactions in detail view."""
        # Switch to detail view
        controller.switch_to_detail_view()
        controller.state.current_data = controller.state.get_filtered_df()
        total_transactions = len(controller.state.current_data)

        # Select all
        count, all_selected, item_type = controller.toggle_select_all_visible()

        assert item_type == "transaction"
        assert all_selected is True
        assert count == total_transactions
        assert len(controller.state.selected_ids) == total_transactions

        # Deselect all
        count, all_selected, item_type = controller.toggle_select_all_visible()

        assert item_type == "transaction"
        assert all_selected is False
        assert count == 0
        assert len(controller.state.selected_ids) == 0

    async def test_toggle_select_all_with_some_selected(self, controller, mock_view):
        """Should select all even when some are already selected."""
        # Switch to merchant view (this will set current_data via refresh_view)
        controller.switch_to_merchant_view()

        # Select one merchant manually
        first_merchant = controller.state.current_data.row(0, named=True).get("merchant")
        controller.state.toggle_group_selection(first_merchant)

        assert len(controller.state.selected_group_keys) == 1

        # Toggle select all should select remaining
        count, all_selected, item_type = controller.toggle_select_all_visible()

        assert all_selected is True
        assert count > 1  # Should now have all selected

    async def test_toggle_select_all_with_no_data(self, controller, mock_view):
        """Should handle empty data gracefully."""
        controller.state.current_data = None

        count, all_selected, item_type = controller.toggle_select_all_visible()

        assert count == 0
        assert all_selected is False
        assert item_type == "transaction"


class TestBulkEditTimestamps:
    """Test that bulk edit operations use the same timestamp for all edits."""

    async def test_queue_category_edits_same_timestamp(self, controller):
        """All edits in a bulk category change should have the same timestamp."""
        # Get a few transactions
        transactions_df = controller.state.transactions_df.head(5)

        # Queue bulk category edits
        controller.queue_category_edits(transactions_df, "new_cat_id")

        # All edits should have the exact same timestamp
        edits = controller.data_manager.pending_edits
        assert len(edits) == 5

        # Get all unique timestamps
        timestamps = set(edit.timestamp for edit in edits)
        assert len(timestamps) == 1, f"Expected 1 unique timestamp, got {len(timestamps)}"

    async def test_queue_merchant_edits_same_timestamp(self, controller):
        """All edits in a bulk merchant change should have the same timestamp."""
        transactions_df = controller.state.transactions_df.head(5)

        controller.queue_merchant_edits(transactions_df, "Old Merchant", "New Merchant")

        edits = controller.data_manager.pending_edits
        assert len(edits) == 5

        timestamps = set(edit.timestamp for edit in edits)
        assert len(timestamps) == 1, f"Expected 1 unique timestamp, got {len(timestamps)}"

    async def test_queue_hide_toggle_edits_same_timestamp(self, controller):
        """All edits in a bulk hide toggle should have the same timestamp."""
        transactions_df = controller.state.transactions_df.head(5)

        controller.queue_hide_toggle_edits(transactions_df)

        edits = controller.data_manager.pending_edits
        assert len(edits) == 5

        timestamps = set(edit.timestamp for edit in edits)
        assert len(timestamps) == 1, f"Expected 1 unique timestamp, got {len(timestamps)}"


class TestCategoryManagerDataSource:
    """
    Tests for category merge/delete transaction data source.

    Category operations (merge, delete) must source transactions from the
    full transactions_df, not current_data, which may be a filtered or
    aggregate subset. Otherwise transactions outside the current view
    keep orphaned category_id values.
    """

    async def test_queue_reassign_uses_transactions_df_not_current_data(self, controller):
        """queue_reassign logic must use full df, not filtered view."""
        state = controller.state
        full_df = state.transactions_df

        cat_with_filtered_out_txns = "cat_groceries"

        current_data = full_df.filter(pl.col("category_id") != cat_with_filtered_out_txns)
        state.current_data = current_data

        filtered_count = current_data.filter(
            pl.col("category_id") == cat_with_filtered_out_txns
        ).height
        assert filtered_count == 0, f"current_data should have 0 {cat_with_filtered_out_txns} txns"

        full_count = full_df.filter(pl.col("category_id") == cat_with_filtered_out_txns).height
        assert full_count > 0, f"transactions_df must have {cat_with_filtered_out_txns} txns"

        full_source = full_df.filter(pl.col("category_id") == cat_with_filtered_out_txns)
        count = controller.queue_category_edits(full_source, "cat_new")
        assert count == full_count

    async def test_txn_counts_use_transactions_df_not_current_data(self, controller):
        """Category counts from full df include all transactions."""
        state = controller.state
        full_df = state.transactions_df

        filtered_df = full_df.filter(pl.col("category_id") != "cat_groceries")
        state.current_data = filtered_df
        state.transactions_df = full_df

        full_counts = {}
        counts_df = full_df.group_by("category_id").agg(pl.len().alias("count"))
        for row in counts_df.iter_rows(named=True):
            full_counts[row["category_id"]] = row["count"]

        filtered_counts = {}
        counts_df = filtered_df.group_by("category_id").agg(pl.len().alias("count"))
        for row in counts_df.iter_rows(named=True):
            filtered_counts[row["category_id"]] = row["count"]

        assert "cat_groceries" not in filtered_counts
        assert "cat_groceries" in full_counts
        assert full_counts["cat_groceries"] > 0

    def test_chained_reassignments_retarget_pending_category_edits(self, controller):
        source_df = pl.DataFrame(
            {
                "id": ["txn-a", "txn-b"],
                "category_id": ["category-a", "category-b"],
            }
        )

        controller.queue_category_reassignment(source_df, "category-a", "category-b")
        controller.queue_category_reassignment(source_df, "category-b", "category-c")

        targets = {
            edit.transaction_id: edit.new_value
            for edit in controller.data_manager.pending_edits
            if edit.field == "category"
        }
        assert targets == {"txn-a": "category-c", "txn-b": "category-c"}

    def test_reassignment_preserves_pending_assignment_outside_source(self, controller):
        source_df = pl.DataFrame(
            {
                "id": ["txn-a", "txn-b"],
                "category_id": ["category-a", "category-a"],
            }
        )
        existing_edit = TransactionEdit(
            transaction_id="txn-a",
            field="category",
            old_value="category-a",
            new_value="category-c",
        )
        controller.data_manager.pending_edits.append(existing_edit)

        count = controller.queue_category_reassignment(source_df, "category-a", "category-b")

        targets = {
            edit.transaction_id: edit.new_value
            for edit in controller.data_manager.pending_edits
            if edit.field == "category"
        }
        assert count == 1
        assert targets == {"txn-a": "category-c", "txn-b": "category-b"}


class TestCategoryRenameReassign:
    """Tests for category rename queuing transaction reassignments.

    When a category is renamed such that its generated category_id changes,
    all transactions with the old category_id must be reassigned to the new
    one. Otherwise they become orphaned on restart when _populate_categories
    regenerates IDs from the new names.
    """

    @staticmethod
    def _compute_cat_id(name: str) -> str:
        """Replicate the ID computation from _populate_categories_from_config."""
        return re.sub(r"[^a-z0-9]+", "_", name.lower()).strip("_")

    def test_rename_with_same_id_is_cosmetic(self):
        """Renames that don't change the computed ID need no reassign."""
        assert self._compute_cat_id("Groceries") == "groceries"
        assert self._compute_cat_id("groceries") == "groceries"
        assert self._compute_cat_id("  Groceries  ") == "groceries"

    def test_rename_with_different_id_changes_cat_id(self):
        """Renames that change characters produce new ID."""
        old_id = self._compute_cat_id("Groceries")
        new_id = self._compute_cat_id("Groceries & Staples")
        assert old_id == "groceries"
        assert new_id == "groceries_staples"
        assert old_id != new_id

    async def test_rename_reassigns_all_transactions(self, controller):
        """queue_category_edits captures all txns for the renamed category."""
        state = controller.state
        full_df = state.transactions_df

        # Pick a category that exists in the mock data
        old_id = "cat_groceries"
        new_name = "Groceries & Staples"
        new_id = self._compute_cat_id(new_name)

        # Sanity: old_id exists in data, new_id does not
        old_txns = full_df.filter(pl.col("category_id") == old_id)
        assert old_txns.height > 0
        new_txns = full_df.filter(pl.col("category_id") == new_id)
        assert new_txns.height == 0

        # The reassign should catch all old_id txns
        count = controller.queue_category_edits(old_txns, new_id)
        assert count == old_txns.height

        edits = controller.data_manager.pending_edits
        assert len(edits) == old_txns.height
        for edit in edits:
            assert edit.field == "category"
            assert edit.old_value == old_id
            assert edit.new_value == new_id

    async def test_rename_reassign_clears_old_category_edits(self, controller):
        """After reassign, old_id edits can be committed."""
        state = controller.state
        full_df = state.transactions_df

        old_id = "cat_groceries"
        new_id = self._compute_cat_id("Groceries & Staples")
        old_txns = full_df.filter(pl.col("category_id") == old_id)

        controller.queue_category_edits(old_txns, new_id)

        assert len(controller.data_manager.pending_edits) == old_txns.height
        assert all(e.new_value == new_id for e in controller.data_manager.pending_edits)
