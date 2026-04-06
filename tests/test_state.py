"""
Tests for state management, undo/redo, and change tracking.
"""

from datetime import date

import polars as pl
import pytest

from moneyflow.data.state import (
    NavigationState,
    SortDirection,
    SortMode,
    TimeGranularity,
    ViewMode,
)


def create_mock_txns(**kwargs):
    """Factory to create a transaction with default values."""
    default = {
        "id": "txn_1",
        "date": date(2024, 10, 1),
        "amount": -10.0,
        "merchant": "Store",
        "merchant_id": "merch_1",
        "category": "Shopping",
        "category_id": "cat_1",
        "group": "Shopping",
        "account": "Checking",
        "account_id": "acc_1",
        "notes": "",
        "hideFromReports": False,
        "pending": False,
        "is_recurring": False,
    }
    return {**default, **kwargs}


class TestAppState:
    """Test AppState initialization and basic operations."""

    def test_initial_state(self, app_state):
        """Test that AppState initializes with correct defaults."""
        assert app_state.view_mode == ViewMode.MERCHANT
        assert app_state.sort_by == SortMode.AMOUNT
        assert app_state.sort_direction == SortDirection.DESC
        assert app_state.transactions_df is None
        assert len(app_state.pending_edits) == 0
        assert len(app_state.selected_ids) == 0
        assert app_state.search_query == ""

    def test_toggle_sort(self, app_state):
        """Test sort field toggling."""
        # Start with AMOUNT
        assert app_state.sort_by == SortMode.AMOUNT
        assert app_state.sort_direction == SortDirection.DESC

        # Toggle to COUNT
        app_state.toggle_sort_field()
        assert app_state.sort_by == SortMode.COUNT

        # Toggle back to AMOUNT
        app_state.toggle_sort_field()
        assert app_state.sort_by == SortMode.AMOUNT

        # Test reverse sort
        app_state.reverse_sort()
        assert app_state.sort_direction == SortDirection.ASC

        app_state.reverse_sort()
        assert app_state.sort_direction == SortDirection.DESC


class TestChangeTracking:
    """Test edit tracking, undo, and redo functionality."""

    def test_add_edit(self, app_state):
        """Test adding a pending edit."""
        app_state.add_edit(
            transaction_id="txn_1",
            field="merchant",
            old_value="Old Merchant",
            new_value="New Merchant",
        )

        assert len(app_state.pending_edits) == 1
        assert len(app_state.undo_stack) == 1

        edit = app_state.pending_edits[0]
        assert edit.transaction_id == "txn_1"
        assert edit.field == "merchant"
        assert edit.old_value == "Old Merchant"
        assert edit.new_value == "New Merchant"

    def test_multiple_edits(self, app_state):
        """Test adding multiple edits."""
        app_state.add_edit("txn_1", "merchant", "A", "B")
        app_state.add_edit("txn_2", "category", "Cat1", "Cat2")
        app_state.add_edit("txn_3", "hide_from_reports", False, True)

        assert len(app_state.pending_edits) == 3
        assert len(app_state.undo_stack) == 3

    def test_undo_single_edit(self, app_state):
        """Test undoing a single edit."""
        app_state.add_edit("txn_1", "merchant", "Old", "New")

        edit = app_state.undo_last_edit()

        assert edit is not None
        assert edit.transaction_id == "txn_1"
        assert len(app_state.pending_edits) == 0
        assert len(app_state.undo_stack) == 0

    def test_undo_multiple_edits(self, app_state):
        """Test undoing multiple edits in sequence."""
        app_state.add_edit("txn_1", "merchant", "A", "B")
        app_state.add_edit("txn_2", "merchant", "C", "D")
        app_state.add_edit("txn_3", "merchant", "E", "F")

        # Undo last edit
        edit1 = app_state.undo_last_edit()
        assert edit1.transaction_id == "txn_3"
        assert len(app_state.pending_edits) == 2

        # Undo second-to-last edit
        edit2 = app_state.undo_last_edit()
        assert edit2.transaction_id == "txn_2"
        assert len(app_state.pending_edits) == 1

        # Undo first edit
        edit3 = app_state.undo_last_edit()
        assert edit3.transaction_id == "txn_1"
        assert len(app_state.pending_edits) == 0

    def test_undo_when_empty(self, app_state):
        """Test undo when there are no edits."""
        edit = app_state.undo_last_edit()
        assert edit is None

    def test_has_unsaved_changes(self, app_state):
        """Test detecting unsaved changes."""
        assert not app_state.has_unsaved_changes()

        app_state.add_edit("txn_1", "merchant", "A", "B")
        assert app_state.has_unsaved_changes()

        app_state.clear_pending_edits()
        assert not app_state.has_unsaved_changes()

    def test_clear_pending_edits(self, app_state):
        """Test clearing all pending edits."""
        app_state.add_edit("txn_1", "merchant", "A", "B")
        app_state.add_edit("txn_2", "category", "C", "D")

        app_state.clear_pending_edits()

        assert len(app_state.pending_edits) == 0
        assert len(app_state.undo_stack) == 0


class TestMultiSelect:
    """Test multi-selection for bulk operations."""

    def test_toggle_selection_add(self, app_state):
        """Test adding a transaction to selection."""
        app_state.toggle_selection("txn_1")

        assert "txn_1" in app_state.selected_ids
        assert len(app_state.selected_ids) == 1

    def test_toggle_selection_remove(self, app_state):
        """Test removing a transaction from selection."""
        app_state.toggle_selection("txn_1")
        app_state.toggle_selection("txn_1")

        assert "txn_1" not in app_state.selected_ids
        assert len(app_state.selected_ids) == 0

    def test_multiple_selections(self, app_state):
        """Test selecting multiple transactions."""
        app_state.toggle_selection("txn_1")
        app_state.toggle_selection("txn_2")
        app_state.toggle_selection("txn_3")

        assert len(app_state.selected_ids) == 3
        assert "txn_1" in app_state.selected_ids
        assert "txn_2" in app_state.selected_ids
        assert "txn_3" in app_state.selected_ids

    def test_clear_selection(self, app_state):
        """Test clearing all selections."""
        app_state.toggle_selection("txn_1")
        app_state.toggle_selection("txn_2")

        app_state.clear_selection()

        assert len(app_state.selected_ids) == 0


class TestDataFiltering:
    """Test filtered DataFrame operations."""

    def test_get_filtered_df_with_search(self, app_state, sample_transactions_df):
        """Test filtering by search query."""
        app_state.transactions_df = sample_transactions_df
        app_state.search_query = "starbucks"

        filtered = app_state.get_filtered_df()

        assert filtered is not None
        assert len(filtered) == 1
        assert filtered["merchant"][0] == "Starbucks"

    def test_get_filtered_df_with_dates(self, app_state, sample_transactions_df):
        """Test filtering by date range."""
        app_state.transactions_df = sample_transactions_df
        app_state.start_date = date(2024, 10, 2)
        app_state.end_date = date(2024, 10, 2)

        filtered = app_state.get_filtered_df()

        assert filtered is not None
        assert len(filtered) == 1
        assert filtered["date"][0] == date(2024, 10, 2)

    def test_get_filtered_df_no_filters(self, app_state, sample_transactions_df):
        """Test getting unfiltered DataFrame."""
        app_state.transactions_df = sample_transactions_df

        filtered = app_state.get_filtered_df()

        assert filtered is not None
        assert len(filtered) == len(sample_transactions_df)

    def test_get_filtered_df_none_when_no_data(self, app_state):
        """Test that get_filtered_df returns None when no data loaded."""
        assert app_state.transactions_df is None
        filtered = app_state.get_filtered_df()
        assert filtered is None

    def test_get_filtered_df_show_transfers_filter(self, app_state):
        """Test filtering out transfers."""
        data = [
            create_mock_txns(
                id="txn_1",
                date=date(2024, 10, 1),
                amount=-100.00,
                merchant="Transfer",
                category="Transfer",
                group="Transfers",
            ),
            create_mock_txns(
                id="txn_2",
                date=date(2024, 10, 2),
                amount=-50.00,
                merchant="Store",
                merchant_id="merch_2",
                category="Shopping",
                category_id="cat_2",
            ),
        ]
        app_state.transactions_df = pl.DataFrame(data)

        # By default, show_transfers should be False
        app_state.show_transfers = False
        filtered = app_state.get_filtered_df()
        assert len(filtered) == 1
        assert filtered["group"][0] == "Shopping"

        # When enabled, should show all
        app_state.show_transfers = True
        filtered = app_state.get_filtered_df()
        assert len(filtered) == 2

    def test_get_filtered_df_show_hidden_filter_in_aggregate_view(self, app_state):
        """Test filtering out hidden transactions in aggregate views."""
        data = [
            create_mock_txns(
                id="txn_1",
                date=date(2024, 10, 1),
                amount=-100.00,
                merchant="Hidden Merchant",
                hideFromReports=True,
            ),
            create_mock_txns(
                id="txn_2",
                date=date(2024, 10, 2),
                amount=-50.00,
                merchant="Visible Merchant",
                merchant_id="merch_2",
                category_id="cat_2",
            ),
        ]
        app_state.transactions_df = pl.DataFrame(data)
        app_state.view_mode = ViewMode.MERCHANT  # Aggregate view

        # When show_hidden is False in aggregate view, should filter out hidden transactions
        app_state.show_hidden = False
        filtered = app_state.get_filtered_df()
        assert len(filtered) == 1
        assert filtered["merchant"][0] == "Visible Merchant"

        # When enabled, should show all
        app_state.show_hidden = True
        filtered = app_state.get_filtered_df()
        assert len(filtered) == 2

    def test_get_filtered_df_show_hidden_in_detail_view(self, app_state):
        """Test that hidden transactions are ALWAYS shown in detail views."""
        data = [
            create_mock_txns(
                id="txn_1",
                date=date(2024, 10, 1),
                amount=-100.00,
                merchant="Amazon",
                hideFromReports=True,
            ),
            create_mock_txns(
                id="txn_2",
                date=date(2024, 10, 2),
                amount=-50.00,
                merchant="Amazon",
                category_id="cat_2",
            ),
        ]
        app_state.transactions_df = pl.DataFrame(data)
        app_state.view_mode = ViewMode.DETAIL
        app_state.selected_merchant = "Amazon"

        # In detail view, hidden transactions should ALWAYS be shown
        # even when show_hidden is False
        app_state.show_hidden = False
        filtered = app_state.get_filtered_df()
        assert len(filtered) == 2  # Both transactions shown
        assert filtered["hideFromReports"].to_list() == [True, False]

        # When enabled, should still show all
        app_state.show_hidden = True
        filtered = app_state.get_filtered_df()
        assert len(filtered) == 2

    def test_get_filtered_df_hidden_in_drilled_down_category(self, app_state):
        """Test that hidden transactions are shown when drilling down into a category."""
        data = [
            create_mock_txns(
                id="txn_1",
                date=date(2024, 10, 1),
                amount=-100.00,
                merchant="Store A",
                category="Groceries",
                group="Food & Dining",
                hideFromReports=True,
            ),
            create_mock_txns(
                id="txn_2",
                date=date(2024, 10, 2),
                amount=-50.00,
                merchant="Store B",
                merchant_id="merch_2",
                category="Groceries",
                group="Food & Dining",
            ),
            create_mock_txns(
                id="txn_3",
                date=date(2024, 10, 3),
                amount=-25.00,
                merchant="Store C",
                merchant_id="merch_3",
                category="Gas",
                category_id="cat_2",
                group="Transportation",
                hideFromReports=True,
            ),
        ]
        app_state.transactions_df = pl.DataFrame(data)
        app_state.view_mode = ViewMode.DETAIL
        app_state.selected_category = "Groceries"
        app_state.show_hidden = False

        # Should show both Groceries transactions (including hidden one)
        filtered = app_state.get_filtered_df()
        assert len(filtered) == 2
        assert set(filtered["merchant"].to_list()) == {"Store A", "Store B"}
        # One is hidden, one is not
        hidden_count = sum(filtered["hideFromReports"].to_list())
        assert hidden_count == 1

    def test_get_filtered_df_detail_view_by_merchant(self, app_state, sample_transactions_df):
        """Test filtering in detail view by selected merchant."""
        app_state.transactions_df = sample_transactions_df
        app_state.view_mode = ViewMode.DETAIL
        app_state.selected_merchant = "Starbucks"

        filtered = app_state.get_filtered_df()

        assert len(filtered) == 1
        assert filtered["merchant"][0] == "Starbucks"

    def test_get_filtered_df_detail_view_by_category(self, app_state, sample_transactions_df):
        """Test filtering in detail view by selected category."""
        app_state.transactions_df = sample_transactions_df
        app_state.view_mode = ViewMode.DETAIL
        app_state.selected_category = "Groceries"

        filtered = app_state.get_filtered_df()

        assert len(filtered) == 1
        assert filtered["category"][0] == "Groceries"

    def test_get_filtered_df_detail_view_by_group(self, app_state, sample_transactions_df):
        """Test filtering in detail view by selected group."""
        app_state.transactions_df = sample_transactions_df
        app_state.view_mode = ViewMode.DETAIL
        app_state.selected_group = "Food & Dining"

        filtered = app_state.get_filtered_df()

        assert len(filtered) == 2
        assert all(row["group"] == "Food & Dining" for row in filtered.iter_rows(named=True))

    def test_get_filtered_df_combined_filters(self, app_state):
        """Test combining multiple filters (time + search + group filter)."""
        data = [
            create_mock_txns(
                id="txn_1",
                date=date(2024, 1, 1),
                amount=-100.00,
                merchant="Starbucks Downtown",
                category="Restaurants & Bars",
                group="Food & Dining",
            ),
            create_mock_txns(
                id="txn_2",
                date=date(2024, 1, 15),
                amount=-50.00,
                merchant="Starbucks Uptown",
                merchant_id="merch_2",
                category="Restaurants & Bars",
                group="Food & Dining",
            ),
            create_mock_txns(
                id="txn_3",
                date=date(2024, 2, 1),
                amount=-75.00,
                merchant="Starbucks Mall",
                merchant_id="merch_3",
                category="Restaurants & Bars",
                group="Food & Dining",
            ),
            create_mock_txns(
                id="txn_4",
                date=date(2024, 1, 20),
                amount=200.00,
                merchant="Transfer In",
                merchant_id="merch_4",
                category="Transfer",
                category_id="cat_2",
                group="Transfers",
            ),
        ]
        app_state.transactions_df = pl.DataFrame(data)

        # Combine filters: time range (Jan only) + search (Starbucks) + no transfers
        app_state.start_date = date(2024, 1, 1)
        app_state.end_date = date(2024, 1, 31)
        app_state.search_query = "starbucks"
        app_state.show_transfers = False

        filtered = app_state.get_filtered_df()

        # Should only get Starbucks transactions from January, no transfers
        assert len(filtered) == 2
        assert all("Starbucks" in row["merchant"] for row in filtered.iter_rows(named=True))
        assert all(row["group"] != "Transfers" for row in filtered.iter_rows(named=True))

    def test_get_filtered_df_multi_level_drill_down(self, app_state):
        """Test multi-level drill-down filters all dimensions correctly.

        Regression test: ensures stats (transaction count, in/out totals) are
        calculated correctly for multi-level drill-downs like "Amazon > Groceries".
        Previously, only the first filter was applied due to elif chain.
        """
        data = [
            # Amazon Groceries transactions (should be included)
            create_mock_txns(
                id="txn_1",
                date=date(2024, 1, 1),
                amount=-50.00,
                merchant="Amazon",
                category="Groceries",
                group="Food & Dining",
            ),
            create_mock_txns(
                id="txn_2",
                date=date(2024, 1, 15),
                amount=-75.00,
                merchant="Amazon",
                category="Groceries",
                group="Food & Dining",
            ),
            # Amazon Electronics (excluded - wrong category)
            create_mock_txns(
                id="txn_3",
                date=date(2024, 1, 10),
                amount=-200.00,
                merchant="Amazon",
                category="Electronics",
                category_id="cat_2",
            ),
            # Target Groceries (excluded - wrong merchant)
            create_mock_txns(
                id="txn_4",
                date=date(2024, 1, 20),
                amount=-100.00,
                merchant="Target",
                merchant_id="merch_2",
                category="Groceries",
                group="Food & Dining",
            ),
        ]
        app_state.transactions_df = pl.DataFrame(data)

        # Simulate multi-level drill-down: Amazon > Groceries
        app_state.view_mode = ViewMode.DETAIL
        app_state.selected_merchant = "Amazon"
        app_state.selected_category = "Groceries"

        filtered = app_state.get_filtered_df()

        # Should only get Amazon + Groceries transactions (2 out of 4)
        assert len(filtered) == 2
        assert all(row["merchant"] == "Amazon" for row in filtered.iter_rows(named=True))
        assert all(row["category"] == "Groceries" for row in filtered.iter_rows(named=True))

        # Verify the total matches expected (stats calculation uses this)
        total = float(filtered["amount"].sum())
        assert total == -125.00  # -50 + -75


class TestNavigation:
    """Test navigation and drill-down functionality."""

    def test_drill_down_from_merchant_view(self, app_state):
        """Test drilling down from merchant view to detail view."""
        app_state.view_mode = ViewMode.MERCHANT

        app_state.drill_down("Starbucks", cursor_position=5, scroll_y=150.5)

        assert app_state.view_mode == ViewMode.DETAIL
        assert app_state.selected_merchant == "Starbucks"
        assert app_state.selected_category is None
        assert app_state.selected_group is None
        assert len(app_state.navigation_history) == 1
        # Navigation history saves NavigationState object
        nav = app_state.navigation_history[0]
        assert nav.view_mode == ViewMode.MERCHANT
        assert nav.cursor_position == 5
        assert nav.scroll_y == 150.5
        assert nav.sort_by == SortMode.AMOUNT
        assert nav.sort_direction == SortDirection.DESC

    def test_drill_down_from_category_view(self, app_state):
        """Test drilling down from category view to detail view."""
        app_state.view_mode = ViewMode.CATEGORY

        app_state.drill_down("Groceries", cursor_position=3, scroll_y=200.0)

        assert app_state.view_mode == ViewMode.DETAIL
        assert app_state.selected_category == "Groceries"
        assert app_state.selected_merchant is None
        assert app_state.selected_group is None
        assert len(app_state.navigation_history) == 1
        # Navigation history saves NavigationState object
        nav = app_state.navigation_history[0]
        assert nav.view_mode == ViewMode.CATEGORY
        assert nav.cursor_position == 3
        assert nav.scroll_y == 200.0
        assert nav.sort_by == SortMode.AMOUNT
        assert nav.sort_direction == SortDirection.DESC

    def test_drill_down_from_group_view(self, app_state):
        """Test drilling down from group view to detail view."""
        app_state.view_mode = ViewMode.GROUP

        app_state.drill_down("Food & Dining", cursor_position=10, scroll_y=75.25)

        assert app_state.view_mode == ViewMode.DETAIL
        assert app_state.selected_group == "Food & Dining"
        assert app_state.selected_merchant is None
        assert app_state.selected_category is None
        assert len(app_state.navigation_history) == 1
        # Navigation history saves NavigationState object
        nav = app_state.navigation_history[0]
        assert nav.view_mode == ViewMode.GROUP
        assert nav.cursor_position == 10
        assert nav.scroll_y == 75.25
        assert nav.sort_by == SortMode.AMOUNT
        assert nav.sort_direction == SortDirection.DESC

    def test_go_back_from_detail_to_previous_view(self, app_state):
        """Test going back from detail view to previous view."""
        app_state.view_mode = ViewMode.MERCHANT
        app_state.drill_down("Starbucks", cursor_position=7, scroll_y=300.5)

        # Now go back
        success, cursor_position, scroll_y = app_state.go_back()

        assert success is True
        assert cursor_position == 7
        assert scroll_y == 300.5
        assert app_state.view_mode == ViewMode.MERCHANT
        assert app_state.selected_merchant is None
        assert app_state.selected_category is None
        assert app_state.selected_group is None
        assert len(app_state.navigation_history) == 0

    def test_go_back_from_detail_without_history(self, app_state):
        """Test going back from detail view when no history exists."""
        # Manually put into detail view without using drill_down
        app_state.view_mode = ViewMode.DETAIL
        app_state.selected_merchant = "Starbucks"

        success, cursor_position, scroll_y = app_state.go_back()

        assert success is True
        assert cursor_position == 0  # Default cursor position
        assert scroll_y == 0.0  # Default scroll position
        assert app_state.view_mode == ViewMode.MERCHANT  # Default back to MERCHANT
        assert app_state.selected_merchant is None

    def test_go_back_from_top_level_view(self, app_state):
        """Test that go_back returns False when already at top-level view."""
        app_state.view_mode = ViewMode.MERCHANT

        success, cursor_position, scroll_y = app_state.go_back()

        assert success is False
        assert cursor_position == 0
        assert scroll_y == 0.0
        assert app_state.view_mode == ViewMode.MERCHANT

    def test_multiple_drill_downs_and_backs(self, app_state):
        """Test multiple drill-downs and back navigations with scroll positions."""
        # Start at merchant view
        app_state.view_mode = ViewMode.MERCHANT
        app_state.drill_down("Starbucks", cursor_position=2, scroll_y=100.0)
        assert app_state.view_mode == ViewMode.DETAIL

        # Go back to merchant
        success, cursor_pos, scroll_y = app_state.go_back()
        assert success is True
        assert cursor_pos == 2
        assert scroll_y == 100.0
        assert app_state.view_mode == ViewMode.MERCHANT

        # Switch to category view and drill down
        app_state.view_mode = ViewMode.CATEGORY
        app_state.drill_down("Groceries", cursor_position=8, scroll_y=250.5)
        assert app_state.view_mode == ViewMode.DETAIL
        assert app_state.selected_category == "Groceries"

        # Go back to category view
        success, cursor_pos, scroll_y = app_state.go_back()
        assert success is True
        assert cursor_pos == 8
        assert scroll_y == 250.5
        assert app_state.view_mode == ViewMode.CATEGORY
        assert app_state.selected_category is None

    def test_drill_down_resets_count_sort_to_date(self, app_state):
        """Test that drilling down from aggregate view resets COUNT sort to DATE."""
        app_state.view_mode = ViewMode.MERCHANT
        app_state.sort_by = SortMode.COUNT
        app_state.sort_direction = SortDirection.DESC

        app_state.drill_down("Starbucks", cursor_position=5, scroll_y=100.0)

        # Should reset to DATE sort since detail views don't have 'count' column
        assert app_state.sort_by == SortMode.DATE
        assert app_state.sort_direction == SortDirection.DESC
        assert app_state.view_mode == ViewMode.DETAIL

    def test_drill_down_preserves_amount_sort(self, app_state):
        """Test that drilling down preserves AMOUNT sort (valid in both views)."""
        app_state.view_mode = ViewMode.MERCHANT
        app_state.sort_by = SortMode.AMOUNT
        app_state.sort_direction = SortDirection.ASC

        app_state.drill_down("Amazon", cursor_position=3, scroll_y=50.0)

        # AMOUNT is valid in detail views, should be preserved
        assert app_state.sort_by == SortMode.AMOUNT
        assert app_state.sort_direction == SortDirection.ASC

    def test_go_back_restores_count_sort_ascending(self, app_state):
        """Test that go_back restores COUNT sort ASC after drilling down."""
        # Start in Merchant view with COUNT sort ascending
        app_state.view_mode = ViewMode.MERCHANT
        app_state.sort_by = SortMode.COUNT
        app_state.sort_direction = SortDirection.ASC

        # Drill down - should switch to DATE sort for detail view
        app_state.drill_down("Starbucks", cursor_position=5, scroll_y=100.0)
        assert app_state.sort_by == SortMode.DATE  # Changed for detail view

        # Go back - should restore COUNT ASC
        success, cursor, scroll = app_state.go_back()
        assert success is True
        assert app_state.sort_by == SortMode.COUNT
        assert app_state.sort_direction == SortDirection.ASC
        assert app_state.view_mode == ViewMode.MERCHANT

    def test_go_back_restores_amount_sort_descending(self, app_state):
        """Test that go_back restores AMOUNT sort DESC after drilling down."""
        # Start in Category view with AMOUNT sort descending
        app_state.view_mode = ViewMode.CATEGORY
        app_state.sort_by = SortMode.AMOUNT
        app_state.sort_direction = SortDirection.DESC

        # Drill down
        app_state.drill_down("Groceries", cursor_position=10, scroll_y=250.0)
        assert app_state.sort_by == SortMode.AMOUNT  # Preserved for detail view

        # Go back - should restore AMOUNT DESC
        success, cursor, scroll = app_state.go_back()
        assert success is True
        assert app_state.sort_by == SortMode.AMOUNT
        assert app_state.sort_direction == SortDirection.DESC
        assert app_state.view_mode == ViewMode.CATEGORY

    def test_go_back_restores_merchant_sort(self, app_state):
        """Test that go_back restores MERCHANT field sort after drilling down."""
        # Start in Merchant view sorted by merchant name
        app_state.view_mode = ViewMode.MERCHANT
        app_state.sort_by = SortMode.MERCHANT
        app_state.sort_direction = SortDirection.ASC

        # Drill down
        app_state.drill_down("Amazon", cursor_position=3, scroll_y=50.0)

        # Go back - should restore MERCHANT sort
        success, cursor, scroll = app_state.go_back()
        assert success is True
        assert app_state.sort_by == SortMode.MERCHANT
        assert app_state.sort_direction == SortDirection.ASC

    def test_multiple_drill_downs_preserve_each_sort(self, app_state):
        """Test that multiple drill-downs preserve sort state at each level."""
        # Start in Merchant view with COUNT sort
        app_state.view_mode = ViewMode.MERCHANT
        app_state.sort_by = SortMode.COUNT
        app_state.sort_direction = SortDirection.ASC

        # First drill down
        app_state.drill_down("Starbucks", cursor_position=5, scroll_y=100.0)
        assert app_state.sort_by == SortMode.DATE

        # Go back once
        app_state.go_back()
        assert app_state.sort_by == SortMode.COUNT
        assert app_state.sort_direction == SortDirection.ASC

        # Now switch to Category view with AMOUNT DESC
        app_state.view_mode = ViewMode.CATEGORY
        app_state.sort_by = SortMode.AMOUNT
        app_state.sort_direction = SortDirection.DESC

        # Drill down from Category
        app_state.drill_down("Groceries", cursor_position=2, scroll_y=50.0)

        # Go back - should restore Category view's AMOUNT DESC
        app_state.go_back()
        assert app_state.view_mode == ViewMode.CATEGORY
        assert app_state.sort_by == SortMode.AMOUNT
        assert app_state.sort_direction == SortDirection.DESC


class TestBreadcrumbs:
    """Test breadcrumb generation for navigation."""

    def test_breadcrumb_merchant_view(self, app_state):
        """Test breadcrumb for merchant view."""
        app_state.view_mode = ViewMode.MERCHANT
        breadcrumb = app_state.get_breadcrumb()
        assert "Merchants" in breadcrumb

    def test_breadcrumb_with_custom_labels(self, app_state):
        """Test breadcrumb uses custom display labels from backend."""
        app_state.view_mode = ViewMode.MERCHANT

        # Amazon backend labels
        amazon_labels = {"merchant": "Item Name", "account": "Order", "accounts": "Orders"}
        breadcrumb = app_state.get_breadcrumb(amazon_labels)

        assert "Item Names" in breadcrumb  # Pluralized
        assert "Merchants" not in breadcrumb

    def test_breadcrumb_account_view_with_custom_labels(self, app_state):
        """Test breadcrumb for account view with custom labels."""
        app_state.view_mode = ViewMode.ACCOUNT

        amazon_labels = {"merchant": "Item Name", "account": "Order", "accounts": "Orders"}
        breadcrumb = app_state.get_breadcrumb(amazon_labels)

        assert "Orders" in breadcrumb
        assert "Accounts" not in breadcrumb

    def test_breadcrumb_drilled_account_with_custom_labels(self, app_state):
        """Test breadcrumb when drilled into account with custom labels."""
        app_state.view_mode = ViewMode.DETAIL
        app_state.selected_account = "113-1234567-8901234"

        amazon_labels = {"merchant": "Item Name", "account": "Order", "accounts": "Orders"}
        breadcrumb = app_state.get_breadcrumb(amazon_labels)

        assert "O: 113-1234567-8901234" in breadcrumb  # Abbreviated: Order → O:
        assert "Accounts" not in breadcrumb

    def test_breadcrumb_sub_grouping_with_custom_labels(self, app_state):
        """Test breadcrumb with sub-grouping uses custom labels."""
        app_state.view_mode = ViewMode.DETAIL
        app_state.selected_account = "113-1234567-8901234"
        app_state.sub_grouping_mode = ViewMode.MERCHANT

        amazon_labels = {"merchant": "Item Name", "account": "Order", "accounts": "Orders"}
        breadcrumb = app_state.get_breadcrumb(amazon_labels)

        assert "(by Item Name)" in breadcrumb
        assert "(by Merchant)" not in breadcrumb

    def test_breadcrumb_category_view(self, app_state):
        """Test breadcrumb for category view."""
        app_state.view_mode = ViewMode.CATEGORY
        breadcrumb = app_state.get_breadcrumb()
        assert "Categories" in breadcrumb

    def test_breadcrumb_group_view(self, app_state):
        """Test breadcrumb for group view."""
        app_state.view_mode = ViewMode.GROUP
        breadcrumb = app_state.get_breadcrumb()
        assert "Groups" in breadcrumb

    @pytest.mark.parametrize(
        "attr,value,expected",
        [
            ("selected_merchant", "Starbucks", "M: Starbucks"),
            ("selected_category", "Groceries", "C: Groceries"),
            ("selected_group", "Food & Dining", "G: Food & Dining"),
        ],
    )
    def test_breadcrumb_detail_views(self, app_state, attr, value, expected):
        """Test breadcrumb for detail views drilled down."""
        app_state.view_mode = ViewMode.DETAIL
        setattr(app_state, attr, value)
        assert expected in app_state.get_breadcrumb()

    def test_breadcrumb_detail_view_no_selection(self, app_state):
        """Test breadcrumb for detail view with no selection."""
        app_state.view_mode = ViewMode.DETAIL

        breadcrumb = app_state.get_breadcrumb()

        assert "Transactions" in breadcrumb

    def test_breadcrumb_with_date_filter(self, app_state):
        """Test breadcrumb does NOT include date range when using date filters."""
        app_state.view_mode = ViewMode.MERCHANT
        # Set date filters directly
        current_year = date.today().year
        app_state.start_date = date(current_year, 1, 1)
        app_state.end_date = date(current_year, 12, 31)

        breadcrumb = app_state.get_breadcrumb()

        # Time is only shown when drilled into via TIME view, not as a filter indicator
        assert "Year" not in breadcrumb
        assert breadcrumb == "Merchants"

    def test_breadcrumb_merchant_then_time(self, app_state):
        """Test breadcrumb shows merchant before time when drilled in that order."""
        # Simulate drilling: Merchants → Amazon → (by Time) → 2024
        app_state.view_mode = ViewMode.MERCHANT
        app_state.time_granularity = TimeGranularity.YEAR
        # First drill into Amazon
        app_state.drill_down("Amazon", cursor_position=0, scroll_y=0.0)
        # Cycle to sub-grouping by time
        app_state.sub_grouping_mode = ViewMode.TIME
        # Then drill into time period (this properly saves navigation history)
        app_state.drill_down("2024", cursor_position=0, scroll_y=0.0)

        breadcrumb = app_state.get_breadcrumb()

        # Should show: M: Amazon > T: 2024
        # NOT: T: 2024 > M: Amazon
        assert breadcrumb == "M: Amazon > T: 2024"

    def test_breadcrumb_merchant_then_time_month(self, app_state):
        """Test breadcrumb shows merchant before time month when drilled in that order."""
        # Simulate drilling: Merchants → Amazon → (by Time) → Mar 2024
        app_state.view_mode = ViewMode.MERCHANT
        app_state.time_granularity = TimeGranularity.MONTH
        app_state.drill_down("Amazon", cursor_position=0, scroll_y=0.0)
        app_state.sub_grouping_mode = ViewMode.TIME
        app_state.drill_down("Mar 2024", cursor_position=0, scroll_y=0.0)

        breadcrumb = app_state.get_breadcrumb()

        # Should show: M: Amazon > T: Mar 2024
        assert breadcrumb == "M: Amazon > T: Mar 2024"

    def test_breadcrumb_category_then_time(self, app_state):
        """Test breadcrumb shows category before time when drilled in that order."""
        # Simulate drilling: Categories → Groceries → (by Time) → 2024
        app_state.view_mode = ViewMode.CATEGORY
        app_state.time_granularity = TimeGranularity.YEAR
        app_state.drill_down("Groceries", cursor_position=0, scroll_y=0.0)
        app_state.sub_grouping_mode = ViewMode.TIME
        app_state.drill_down("2024", cursor_position=0, scroll_y=0.0)

        breadcrumb = app_state.get_breadcrumb()

        # Should show: C: Groceries > T: 2024
        assert breadcrumb == "C: Groceries > T: 2024"

    def test_breadcrumb_time_then_merchant(self, app_state):
        """Test breadcrumb shows time before merchant when drilled in that order."""
        # Simulate drilling: Time → 2024 → (by Merchant) → Amazon
        app_state.view_mode = ViewMode.TIME
        app_state.time_granularity = TimeGranularity.YEAR
        app_state.drill_down("2024", cursor_position=0, scroll_y=0.0)
        # Cycle to sub-grouping by merchant
        app_state.sub_grouping_mode = ViewMode.MERCHANT
        # Drill into merchant
        app_state.drill_down("Amazon", cursor_position=0, scroll_y=0.0)

        breadcrumb = app_state.get_breadcrumb()

        # Should show: T: 2024 > M: Amazon
        # NOT: M: Amazon > T: 2024
        # The order should be preserved based on navigation_history
        parts = breadcrumb.split(" > ")
        # Time should come before Merchant in the breadcrumb
        time_index = next((i for i, p in enumerate(parts) if "2024" in p), -1)
        merchant_index = next((i for i, p in enumerate(parts) if "Amazon" in p), -1)
        assert time_index < merchant_index
        assert breadcrumb == "T: 2024 > M: Amazon"

    def test_breadcrumb_time_only(self, app_state):
        """Test breadcrumb shows only time when that's the only drill-down."""
        # Simulate drilling: Time → 2024
        app_state.view_mode = ViewMode.TIME
        app_state.time_granularity = TimeGranularity.YEAR
        app_state.drill_down("2024", cursor_position=0, scroll_y=0.0)

        breadcrumb = app_state.get_breadcrumb()

        # Should show: T: 2024
        assert breadcrumb == "T: 2024"


class TestSubGrouping:
    """Tests for sub-grouping within drilled-down views."""

    def test_is_drilled_down_with_merchant(self, app_state):
        """Should return True when merchant is selected."""
        app_state.selected_merchant = "Amazon"
        assert app_state.is_drilled_down() is True

    def test_is_drilled_down_with_category(self, app_state):
        """Should return True when category is selected."""
        app_state.selected_category = "Groceries"
        assert app_state.is_drilled_down() is True

    def test_is_drilled_down_no_selection(self, app_state):
        """Should return False with no selections."""
        assert app_state.is_drilled_down() is False

    def test_cycle_sub_grouping_from_merchant_includes_category(self, app_state):
        """When drilled into Merchant, should offer Category sub-grouping."""
        app_state.view_mode = ViewMode.DETAIL
        app_state.selected_merchant = "Amazon"

        result = app_state.cycle_sub_grouping()

        # First cycle should go to Category (Merchant is excluded)
        assert app_state.sub_grouping_mode == ViewMode.CATEGORY
        assert result == "by Category"

    def test_cycle_sub_grouping_from_category_includes_merchant(self, app_state):
        """When drilled into Category, should offer Merchant sub-grouping."""
        app_state.view_mode = ViewMode.DETAIL
        app_state.selected_category = "Groceries"

        result = app_state.cycle_sub_grouping()

        # First cycle should go to Merchant (Category is excluded)
        assert app_state.sub_grouping_mode == ViewMode.MERCHANT
        assert result == "by Merchant"

    def test_cycle_sub_grouping_full_cycle_from_merchant(self, app_state):
        """Should cycle through all modes (excluding Merchant) then back."""
        app_state.view_mode = ViewMode.DETAIL
        app_state.selected_merchant = "Amazon"

        # Cycle: Category → Group → Account → TIME → Detail → Category
        assert app_state.cycle_sub_grouping() == "by Category"
        assert app_state.sub_grouping_mode == ViewMode.CATEGORY

        assert app_state.cycle_sub_grouping() == "by Group"
        assert app_state.sub_grouping_mode == ViewMode.GROUP

        assert app_state.cycle_sub_grouping() == "by Account"
        assert app_state.sub_grouping_mode == ViewMode.ACCOUNT

        assert app_state.cycle_sub_grouping() == "by Year"  # TIME now in cycle
        assert app_state.sub_grouping_mode == ViewMode.TIME

        assert app_state.cycle_sub_grouping() == "Detail"
        assert app_state.sub_grouping_mode is None

        # Back to Category
        assert app_state.cycle_sub_grouping() == "by Category"
        assert app_state.sub_grouping_mode == ViewMode.CATEGORY

    def test_cycle_sub_grouping_full_cycle_from_category(self, app_state):
        """Should cycle through all modes (excluding Category) then back."""
        app_state.view_mode = ViewMode.DETAIL
        app_state.selected_category = "Groceries"

        # Cycle: Merchant → Group → Account → TIME → Detail → Merchant
        assert app_state.cycle_sub_grouping() == "by Merchant"
        assert app_state.sub_grouping_mode == ViewMode.MERCHANT

        assert app_state.cycle_sub_grouping() == "by Group"
        assert app_state.sub_grouping_mode == ViewMode.GROUP

        assert app_state.cycle_sub_grouping() == "by Account"
        assert app_state.sub_grouping_mode == ViewMode.ACCOUNT

        assert app_state.cycle_sub_grouping() == "by Year"  # TIME now in cycle
        assert app_state.sub_grouping_mode == ViewMode.TIME

        assert app_state.cycle_sub_grouping() == "Detail"
        assert app_state.sub_grouping_mode is None

        # Back to Merchant
        assert app_state.cycle_sub_grouping() == "by Merchant"
        assert app_state.sub_grouping_mode == ViewMode.MERCHANT

    def test_cycle_grouping_delegates_to_sub_grouping_when_drilled_down(self, app_state):
        """When drilled down, cycle_grouping should delegate to sub-grouping."""
        app_state.view_mode = ViewMode.DETAIL
        app_state.selected_merchant = "Amazon"

        result = app_state.cycle_grouping()

        # Should have called cycle_sub_grouping
        assert app_state.sub_grouping_mode == ViewMode.CATEGORY
        assert result == "by Category"

    def test_cycle_grouping_works_normally_when_not_drilled_down(self, app_state):
        """When not drilled down, should cycle top-level views."""
        app_state.view_mode = ViewMode.MERCHANT

        result = app_state.cycle_grouping()

        # Should cycle to Category view
        assert app_state.view_mode == ViewMode.CATEGORY
        assert result == "Categories"

    def test_cycle_grouping_from_detail_view_with_history_restores_previous_view(self, app_state):
        """Pressing 'g' from top-level DETAIL view with navigation history should restore previous view."""
        app_state.view_mode = ViewMode.DETAIL
        app_state.sort_by = SortMode.DATE
        app_state.sort_direction = SortDirection.ASC

        # Simulate having navigation history from a previous CATEGORY view
        nav_state = NavigationState(
            view_mode=ViewMode.CATEGORY,
            sort_by=SortMode.CATEGORY,
            sort_direction=SortDirection.DESC,
            cursor_position=5,
            scroll_y=100.0,
        )
        app_state.navigation_history.append(nav_state)

        result = app_state.cycle_grouping()

        # Should restore to CATEGORY view with previous sort settings
        assert app_state.view_mode == ViewMode.CATEGORY
        assert app_state.sort_by == SortMode.CATEGORY
        assert app_state.sort_direction == SortDirection.DESC
        assert result == "Categories"
        # Navigation history should be consumed
        assert len(app_state.navigation_history) == 0

    def test_cycle_grouping_from_detail_view_without_history_defaults_to_merchant(self, app_state):
        """Pressing 'g' from top-level DETAIL view without history should default to MERCHANT view."""
        app_state.view_mode = ViewMode.DETAIL
        app_state.sort_by = SortMode.DATE
        app_state.sort_direction = SortDirection.ASC
        # No navigation history

        result = app_state.cycle_grouping()

        # Should default to MERCHANT view
        assert app_state.view_mode == ViewMode.MERCHANT
        # Sort settings should be preserved from current state
        assert app_state.sort_by == SortMode.DATE
        assert app_state.sort_direction == SortDirection.ASC
        assert result == "Merchants"

    def test_cycle_sub_grouping_resets_date_sort_to_amount(self, app_state):
        """When cycling from detail to aggregated sub-grouping, should reset DATE sort to AMOUNT DESC."""
        app_state.view_mode = ViewMode.DETAIL
        app_state.selected_category = "Coffee Shops"
        app_state.sub_grouping_mode = None  # Currently in detail view
        app_state.sort_by = SortMode.DATE  # Sorted by date (valid for detail)

        # Cycle to aggregated sub-grouping
        result = app_state.cycle_sub_grouping()

        # Should switch to aggregated view and reset sort from DATE to AMOUNT DESC (highest spending first)
        assert app_state.sub_grouping_mode == ViewMode.MERCHANT
        assert app_state.sort_by == SortMode.AMOUNT
        assert app_state.sort_direction == SortDirection.DESC
        assert result == "by Merchant"

    def test_cycle_sub_grouping_preserves_count_sort(self, app_state):
        """When cycling from detail to aggregated sub-grouping, COUNT sort should be preserved."""
        app_state.view_mode = ViewMode.DETAIL
        app_state.selected_merchant = "Amazon"
        app_state.sub_grouping_mode = None  # Currently in detail view
        app_state.sort_by = SortMode.COUNT  # Already valid for aggregated views

        # Cycle to aggregated sub-grouping
        result = app_state.cycle_sub_grouping()

        # Should preserve COUNT sort
        assert app_state.sub_grouping_mode == ViewMode.CATEGORY
        assert app_state.sort_by == SortMode.COUNT
        assert result == "by Category"

    def test_cycle_sub_grouping_preserves_amount_sort(self, app_state):
        """When cycling from detail to aggregated sub-grouping, AMOUNT sort should be preserved."""
        app_state.view_mode = ViewMode.DETAIL
        app_state.selected_group = "Food & Dining"
        app_state.sub_grouping_mode = None  # Currently in detail view
        app_state.sort_by = SortMode.AMOUNT

        # Cycle to aggregated sub-grouping
        app_state.cycle_sub_grouping()

        # Should preserve AMOUNT sort
        assert app_state.sub_grouping_mode == ViewMode.MERCHANT
        assert app_state.sort_by == SortMode.AMOUNT

    def test_cycle_sub_grouping_resets_invalid_aggregate_field_sort(self, app_state):
        """When cycling between sub-groupings, invalid aggregate field sorts should reset to AMOUNT."""
        app_state.view_mode = ViewMode.DETAIL
        app_state.selected_category = "Groceries"  # Drilled into category
        app_state.sub_grouping_mode = ViewMode.MERCHANT  # Sub-grouped by merchant
        app_state.sort_by = (
            SortMode.MERCHANT
        )  # Sorting by merchant (valid for current sub-grouping)

        # Cycle to next sub-grouping (will be GROUP since CATEGORY is excluded)
        app_state.cycle_sub_grouping()

        # Should be sub-grouped by group now, and MERCHANT sort should reset to AMOUNT
        assert app_state.sub_grouping_mode == ViewMode.GROUP
        assert app_state.sort_by == SortMode.AMOUNT  # MERCHANT is not valid for group sub-grouping

    def test_cycle_sub_grouping_resets_merchant_sort_when_switching_to_category(self, app_state):
        """MERCHANT sort should reset to AMOUNT DESC when cycling to category sub-grouping."""
        app_state.view_mode = ViewMode.DETAIL
        app_state.selected_account = "Chase Checking"
        app_state.sub_grouping_mode = ViewMode.MERCHANT
        app_state.sort_by = SortMode.MERCHANT

        app_state.cycle_sub_grouping()  # Merchant → Category

        assert app_state.sub_grouping_mode == ViewMode.CATEGORY
        assert app_state.sort_by == SortMode.AMOUNT
        assert app_state.sort_direction == SortDirection.DESC

    def test_cycle_sub_grouping_resets_category_sort_when_switching_to_merchant(self, app_state):
        """CATEGORY sort should reset to AMOUNT when cycling to merchant sub-grouping."""
        app_state.view_mode = ViewMode.DETAIL
        app_state.selected_group = "Food & Dining"
        app_state.sub_grouping_mode = ViewMode.CATEGORY
        app_state.sort_by = SortMode.CATEGORY

        app_state.cycle_sub_grouping()  # Category → Account

        assert app_state.sub_grouping_mode == ViewMode.ACCOUNT
        assert app_state.sort_by == SortMode.AMOUNT

    def test_cycle_sub_grouping_preserves_matching_aggregate_field_sort(self, app_state):
        """When cycling to sub-grouping by X, and already sorting by X, preserve the sort."""
        app_state.view_mode = ViewMode.DETAIL
        app_state.selected_category = "Groceries"
        app_state.sub_grouping_mode = ViewMode.ACCOUNT
        app_state.sort_by = SortMode.ACCOUNT  # Sorting by account

        # Cycle through: Account → TIME → None(detail) → Merchant
        app_state.cycle_sub_grouping()  # Account → TIME
        assert app_state.sub_grouping_mode == ViewMode.TIME
        # ACCOUNT sort is not valid for TIME, should reset to TIME_PERIOD
        assert app_state.sort_by == SortMode.TIME_PERIOD

        app_state.cycle_sub_grouping()  # TIME → None (detail)
        assert app_state.sub_grouping_mode is None
        # When we go to detail, TIME_PERIOD sort should be preserved (it's valid for detail)
        assert app_state.sort_by == SortMode.TIME_PERIOD

        app_state.cycle_sub_grouping()  # None → Merchant
        # TIME_PERIOD sort is not valid for merchant sub-grouping, should reset
        assert app_state.sub_grouping_mode == ViewMode.MERCHANT
        assert app_state.sort_by == SortMode.AMOUNT

    def test_cycle_sub_grouping_preserves_count_when_cycling_between_modes(self, app_state):
        """COUNT sort should be preserved when cycling between any sub-grouping modes."""
        app_state.view_mode = ViewMode.DETAIL
        app_state.selected_merchant = "Amazon"  # MERCHANT is excluded from available modes
        app_state.sub_grouping_mode = ViewMode.CATEGORY
        app_state.sort_by = SortMode.COUNT

        app_state.cycle_sub_grouping()  # Category → Group (not Account, since available: Category, Group, Account, None)

        assert app_state.sub_grouping_mode == ViewMode.GROUP
        assert app_state.sort_by == SortMode.COUNT  # Preserved

    def test_cycle_sub_grouping_preserves_amount_when_cycling_between_modes(self, app_state):
        """AMOUNT sort should be preserved when cycling between any sub-grouping modes."""
        app_state.view_mode = ViewMode.DETAIL
        app_state.selected_category = "Groceries"  # CATEGORY is excluded from available modes
        app_state.sub_grouping_mode = ViewMode.MERCHANT
        app_state.sort_by = SortMode.AMOUNT

        app_state.cycle_sub_grouping()  # Merchant → Group (available: Merchant, Group, Account, None)

        assert app_state.sub_grouping_mode == ViewMode.GROUP
        assert app_state.sort_by == SortMode.AMOUNT  # Preserved

    def test_go_back_clears_sub_grouping_first(self, app_state):
        """Escape should clear sub-grouping before going back."""
        app_state.view_mode = ViewMode.DETAIL
        app_state.selected_merchant = "Amazon"
        app_state.sub_grouping_mode = ViewMode.CATEGORY

        success, cursor, _ = app_state.go_back()

        # Should clear sub-grouping, stay drilled into Amazon
        assert success is True
        assert app_state.sub_grouping_mode is None
        assert app_state.selected_merchant == "Amazon"
        assert app_state.view_mode == ViewMode.DETAIL

    def test_go_back_then_clears_drill_down(self, app_state):
        """Second Escape should clear drill-down."""
        app_state.view_mode = ViewMode.DETAIL
        app_state.selected_merchant = "Amazon"
        app_state.sub_grouping_mode = ViewMode.CATEGORY
        # Navigation history uses NavigationState object
        app_state.navigation_history.append(
            NavigationState(
                view_mode=ViewMode.MERCHANT,
                cursor_position=5,
                scroll_y=125.0,
                sort_by=SortMode.AMOUNT,
                sort_direction=SortDirection.DESC,
            )
        )

        # First escape: clear sub-grouping
        success1, _, _ = app_state.go_back()
        assert success1 is True
        assert app_state.sub_grouping_mode is None
        assert app_state.selected_merchant == "Amazon"

        # Second escape: clear drill-down
        success2, cursor, scroll_y = app_state.go_back()
        assert success2 is True
        assert app_state.selected_merchant is None
        assert app_state.view_mode == ViewMode.MERCHANT
        assert cursor == 5
        assert scroll_y == 125.0

    def test_go_back_clears_sub_grouping_and_restores_sort(self, app_state):
        """When clearing sub-grouping, should restore sort state from navigation history."""
        app_state.view_mode = ViewMode.DETAIL
        app_state.selected_merchant = "Amazon"
        app_state.sub_grouping_mode = ViewMode.CATEGORY
        app_state.sort_by = SortMode.AMOUNT  # Changed to AMOUNT due to sub-grouping

        # Simulate proper navigation flow:
        # 1. Drill down saved merchant view state
        app_state.navigation_history.append(
            NavigationState(
                view_mode=ViewMode.MERCHANT,
                cursor_position=10,
                scroll_y=200.0,
                sort_by=SortMode.AMOUNT,
                sort_direction=SortDirection.DESC,
            )
        )
        # 2. Entering sub-grouping saved detail view state (with ACCOUNT sort)
        app_state.navigation_history.append(
            NavigationState(
                view_mode=ViewMode.DETAIL,
                cursor_position=0,
                scroll_y=0.0,
                sort_by=SortMode.ACCOUNT,  # Sort before sub-grouping
                sort_direction=SortDirection.ASC,
                selected_merchant="Amazon",
                sub_grouping_mode=None,  # Was not sub-grouped
            )
        )

        # Press Esc to clear sub-grouping
        success, _, _ = app_state.go_back()

        # Should clear sub-grouping AND restore sort from detail view state
        assert success is True
        assert app_state.sub_grouping_mode is None
        assert app_state.selected_merchant == "Amazon"  # Still drilled down
        assert app_state.sort_by == SortMode.ACCOUNT  # Restored from entering-sub-grouping state
        assert app_state.sort_direction == SortDirection.ASC  # Restored from history
        # Navigation history should have popped the sub-grouping entry, leaving drill-down entry
        assert len(app_state.navigation_history) == 1

    def test_go_back_clears_sub_grouping_preserves_count_sort(self, app_state):
        """When clearing sub-grouping with COUNT sort, preserve it (it's valid)."""
        app_state.view_mode = ViewMode.DETAIL
        app_state.selected_category = "Groceries"
        app_state.sub_grouping_mode = ViewMode.MERCHANT
        app_state.sort_by = SortMode.COUNT  # COUNT is valid everywhere

        app_state.navigation_history.append(
            NavigationState(
                view_mode=ViewMode.CATEGORY,
                sort_by=SortMode.COUNT,  # Was also COUNT
                sort_direction=SortDirection.DESC,
            )
        )

        # Press Esc to clear sub-grouping
        app_state.go_back()

        # COUNT should be preserved (it's valid in both modes)
        assert app_state.sub_grouping_mode is None
        assert app_state.sort_by == SortMode.COUNT

    def test_cycle_sub_grouping_saves_state_when_entering_subgrouping(self, app_state):
        """When entering sub-grouping mode, should save current state to navigation history."""
        app_state.view_mode = ViewMode.DETAIL
        app_state.selected_merchant = "Amazon"
        app_state.sub_grouping_mode = None  # Not yet sub-grouped
        app_state.sort_by = SortMode.ACCOUNT  # Current sort
        app_state.sort_direction = SortDirection.ASC

        # Navigation history has one entry from drill-down
        app_state.navigation_history.append(
            NavigationState(
                view_mode=ViewMode.MERCHANT,
                sort_by=SortMode.AMOUNT,
                sort_direction=SortDirection.DESC,
            )
        )

        # Press g to enter sub-grouping
        app_state.cycle_sub_grouping()

        # Should save detail view state before changing sort
        assert len(app_state.navigation_history) == 2
        saved_state = app_state.navigation_history[-1]
        assert saved_state.view_mode == ViewMode.DETAIL
        assert saved_state.sort_by == SortMode.ACCOUNT  # Saved before changing
        assert saved_state.sort_direction == SortDirection.ASC
        assert saved_state.selected_merchant == "Amazon"
        assert saved_state.sub_grouping_mode is None

    def test_go_back_from_subgrouping_pops_navigation_history(self, app_state):
        """When clearing sub-grouping, should pop from navigation history."""
        app_state.view_mode = ViewMode.DETAIL
        app_state.selected_merchant = "Amazon"
        app_state.sub_grouping_mode = ViewMode.CATEGORY
        app_state.sort_by = SortMode.AMOUNT  # Changed to AMOUNT by sub-grouping

        # Two entries: drill-down + entering sub-grouping
        app_state.navigation_history.append(
            NavigationState(view_mode=ViewMode.MERCHANT, sort_by=SortMode.AMOUNT)
        )
        app_state.navigation_history.append(
            NavigationState(
                view_mode=ViewMode.DETAIL,
                sort_by=SortMode.ACCOUNT,  # Before sub-grouping
                sort_direction=SortDirection.ASC,
                selected_merchant="Amazon",
            )
        )

        # Press Esc to clear sub-grouping
        app_state.go_back()

        # Should restore ACCOUNT sort and pop from history
        assert app_state.sort_by == SortMode.ACCOUNT
        assert app_state.sort_direction == SortDirection.ASC
        assert len(app_state.navigation_history) == 1  # Popped the sub-grouping entry

    def test_breadcrumb_shows_sub_grouping(self, app_state):
        """Breadcrumb should show sub-grouping mode but NOT date filter."""
        app_state.view_mode = ViewMode.DETAIL
        app_state.selected_merchant = "Amazon"
        app_state.sub_grouping_mode = ViewMode.CATEGORY
        app_state.start_date = date(2025, 1, 1)
        app_state.end_date = date(2025, 12, 31)

        breadcrumb = app_state.get_breadcrumb()

        assert "M:" in breadcrumb  # Abbreviated merchant
        assert "Amazon" in breadcrumb
        assert "(by Category)" in breadcrumb
        # Time is only shown when drilled into via TIME view, not as a filter indicator
        assert "Year 2025" not in breadcrumb
        assert breadcrumb == "M: Amazon > (by Category)"

    def test_breadcrumb_multi_level_drill_down(self, app_state):
        """Breadcrumb should show multiple drill-down levels but NOT date filter."""
        app_state.view_mode = ViewMode.DETAIL
        app_state.selected_merchant = "Amazon"
        app_state.selected_category = "Groceries"
        app_state.start_date = date(2025, 10, 1)
        app_state.end_date = date(2025, 10, 31)

        breadcrumb = app_state.get_breadcrumb()

        assert "M:" in breadcrumb  # Abbreviated merchant
        assert "Amazon" in breadcrumb
        assert "Groceries" in breadcrumb
        # Time is only shown when drilled into via TIME view, not as a filter indicator
        assert "October 2025" not in breadcrumb
        assert breadcrumb == "M: Amazon > C: Groceries"

    def test_multi_level_go_back_clears_deepest_first(self, app_state):
        """Multi-level drill-down should clear deepest selection first."""
        app_state.view_mode = ViewMode.DETAIL
        app_state.selected_merchant = "Amazon"
        app_state.selected_category = "Groceries"

        # First go_back: clear category (deepest)
        success, _, _ = app_state.go_back()
        assert success is True
        assert app_state.selected_category is None
        assert app_state.selected_merchant == "Amazon"

        # Second go_back: clear merchant
        success, _, _ = app_state.go_back()
        assert success is True
        assert app_state.selected_merchant is None


class TestSmartSearchEscape:
    """Tests for smart search escape behavior."""

    def test_get_navigation_depth_top_level(self, app_state):
        """Top-level views should have depth 0."""
        app_state.view_mode = ViewMode.MERCHANT
        assert app_state.get_navigation_depth() == 0

    def test_get_navigation_depth_one_level(self, app_state):
        """Drilled once should have depth 1."""
        app_state.view_mode = ViewMode.DETAIL
        app_state.selected_merchant = "Amazon"
        assert app_state.get_navigation_depth() == 1

    def test_get_navigation_depth_two_levels(self, app_state):
        """Drilled twice should have depth 2."""
        app_state.view_mode = ViewMode.DETAIL
        app_state.selected_merchant = "Amazon"
        app_state.selected_category = "Groceries"
        assert app_state.get_navigation_depth() == 2

    def test_set_search_saves_navigation_state(self, app_state):
        """Setting search should save current navigation app_state."""
        app_state.view_mode = ViewMode.DETAIL
        app_state.selected_merchant = "Amazon"

        app_state.set_search("coffee")

        assert app_state.search_query == "coffee"
        assert app_state.search_navigation_state is not None
        assert app_state.search_navigation_state == (1, None)  # depth 1, no sub-grouping

    def test_set_search_with_sub_grouping(self, app_state):
        """Search with sub-grouping should save that app_state."""
        app_state.view_mode = ViewMode.DETAIL
        app_state.selected_merchant = "Amazon"
        app_state.sub_grouping_mode = ViewMode.CATEGORY

        app_state.set_search("grocery")

        assert app_state.search_navigation_state == (1, ViewMode.CATEGORY)

    def test_clear_search_clears_navigation_state(self, app_state):
        """Clearing search should clear navigation app_state."""
        app_state.set_search("coffee")

        app_state.set_search("")

        assert app_state.search_query == ""
        assert app_state.search_navigation_state is None

    def test_escape_clears_search_when_no_navigation(self, app_state):
        """Scenario 1: Search without further navigation, Escape clears search."""
        app_state.view_mode = ViewMode.MERCHANT
        app_state.set_search("coffee")

        # No navigation happened, just searched
        success, _, _ = app_state.go_back()

        assert success is True
        assert app_state.search_query == ""
        assert app_state.view_mode == ViewMode.MERCHANT  # Still in Merchants view

    def test_escape_navigates_after_drill_down_with_search(self, app_state):
        """Scenario 2: Search then drill down, Escape navigates (search persists)."""
        app_state.view_mode = ViewMode.MERCHANT
        app_state.set_search("coffee")

        # Drill down (navigation happened)
        app_state.drill_down("Starbucks", 5)

        # Now Escape should navigate back, not clear search
        success, cursor, _ = app_state.go_back()

        assert success is True
        assert app_state.search_query == "coffee"  # Search still active
        assert app_state.view_mode == ViewMode.MERCHANT
        assert cursor == 5

    def test_escape_twice_after_drill_clears_search(self, app_state):
        """After navigating back to search level, second Escape clears search."""
        app_state.view_mode = ViewMode.MERCHANT
        app_state.set_search("coffee")
        app_state.drill_down("Starbucks", 5)

        # First Escape: navigate back
        app_state.go_back()
        assert app_state.search_query == "coffee"  # Still active

        # Second Escape: clear search (back at original depth)
        success, _, _ = app_state.go_back()
        assert success is True
        assert app_state.search_query == ""

    def test_escape_with_search_and_sub_grouping(self, app_state):
        """Scenario 3: Search then sub-group, Escape clears sub-grouping first."""
        app_state.view_mode = ViewMode.DETAIL
        app_state.selected_merchant = "Amazon"
        app_state.set_search("grocery")

        # Sub-group (navigation happened - depth same but state changed)
        app_state.sub_grouping_mode = ViewMode.CATEGORY

        # Escape should clear sub-grouping (search still active, navigation happened)
        success, _, _ = app_state.go_back()

        assert success is True
        assert app_state.sub_grouping_mode is None
        assert app_state.search_query == "grocery"  # Search persists
        assert app_state.selected_merchant == "Amazon"  # Still drilled down

    def test_escape_after_clearing_sub_grouping_clears_search(self, app_state):
        """After clearing sub-grouping, if back at search level, Escape clears search."""
        app_state.view_mode = ViewMode.DETAIL
        app_state.selected_merchant = "Amazon"
        app_state.set_search("grocery")
        app_state.sub_grouping_mode = ViewMode.CATEGORY

        # First Escape: clear sub-grouping
        app_state.go_back()
        assert app_state.sub_grouping_mode is None
        assert app_state.search_query == "grocery"

        # Now we're at same state as when search was set
        # Second Escape: should clear search
        success, _, _ = app_state.go_back()
        assert success is True
        assert app_state.search_query == ""
        assert app_state.selected_merchant == "Amazon"  # Still drilled down

    def test_search_persists_across_navigation(self, app_state):
        """Search should stay active when navigating away and back."""
        app_state.view_mode = ViewMode.MERCHANT
        app_state.set_search("coffee")
        app_state.drill_down("Starbucks", 5)

        # Navigate back
        app_state.go_back()

        # Search should still be active
        assert app_state.search_query == "coffee"

    def test_get_navigation_state_comparison(self, app_state):
        """Navigation state should change when sub-grouping changes."""
        app_state.view_mode = ViewMode.DETAIL
        app_state.selected_merchant = "Amazon"

        state1 = app_state.get_navigation_state()
        assert state1 == (1, None)

        app_state.sub_grouping_mode = ViewMode.CATEGORY
        state2 = app_state.get_navigation_state()
        assert state2 == (1, ViewMode.CATEGORY)
        assert state1 != state2


class TestMultiLevelDrillDownNavigation:
    """Test complex multi-level drill-down and go_back scenarios."""

    def test_drill_into_category_subgroup_by_account_drill_into_account_go_back(self, app_state):
        """
        Test the reported bug: Drill into category, sub-group by account,
        drill into account, then go back should restore sub-grouped view.

        Steps:
        1. Category view → drill into "Groceries"
        2. Press g → sub-group by Account
        3. Press Enter on account → drill into that account
        4. Press Escape → should go back to Groceries > (by Account), not Category view
        """

        # Step 1: Start in Category view, drill into Groceries
        app_state.view_mode = ViewMode.CATEGORY
        app_state.drill_down("Groceries", cursor_position=5, scroll_y=100.0)

        assert app_state.view_mode == ViewMode.DETAIL
        assert app_state.selected_category == "Groceries"
        assert app_state.sub_grouping_mode is None
        assert len(app_state.navigation_history) == 1

        # Step 2: Sub-group by Account
        app_state.sub_grouping_mode = ViewMode.ACCOUNT

        # Step 3: Drill into a specific account from sub-grouped view
        # This should save the current state (Groceries + sub_grouping_mode=ACCOUNT)
        app_state.drill_down("Chase Checking", cursor_position=3, scroll_y=50.0)

        assert app_state.view_mode == ViewMode.DETAIL
        assert app_state.selected_category == "Groceries"  # Still filtered to Groceries
        assert app_state.selected_account == "Chase Checking"  # Now also filtered to account
        assert app_state.sub_grouping_mode is None  # Cleared when drilling into account
        assert len(app_state.navigation_history) == 2  # Two drill-downs saved

        # Step 4: Go back - should restore Groceries > (by Account)
        success, cursor, scroll = app_state.go_back()

        assert success is True
        assert app_state.view_mode == ViewMode.DETAIL
        assert app_state.selected_category == "Groceries"  # Still Groceries
        assert app_state.selected_account is None  # Account filter cleared
        assert app_state.sub_grouping_mode == ViewMode.ACCOUNT  # Sub-grouping restored!
        assert cursor == 3  # Cursor restored
        assert scroll == 50.0  # Scroll restored
        assert len(app_state.navigation_history) == 1  # One drill-down remains

        # Step 5: Go back again - should clear sub-grouping, stay in Groceries detail
        success, cursor, scroll = app_state.go_back()

        assert success is True
        assert app_state.view_mode == ViewMode.DETAIL
        assert app_state.selected_category == "Groceries"  # Still in Groceries
        assert app_state.sub_grouping_mode is None  # Sub-grouping cleared
        assert len(app_state.navigation_history) == 1  # One entry remains

        # Step 6: Go back a third time - now should return to Category view
        success, cursor, scroll = app_state.go_back()

        assert success is True
        assert app_state.view_mode == ViewMode.CATEGORY
        assert app_state.selected_category is None  # Category filter cleared
        assert app_state.sub_grouping_mode is None
        assert cursor == 5  # Original cursor restored
        assert scroll == 100.0  # Original scroll restored
        assert len(app_state.navigation_history) == 0  # Back to root

    def test_drill_into_merchant_subgroup_by_category_drill_into_category(self, app_state):
        """Test multi-level navigation: Merchant → sub-group by Category → drill into category."""

        # Step 1: Drill into Amazon from Merchant view
        app_state.view_mode = ViewMode.MERCHANT
        app_state.drill_down("Amazon", cursor_position=10, scroll_y=200.0)

        assert app_state.selected_merchant == "Amazon"
        assert app_state.sub_grouping_mode is None

        # Step 2: Sub-group by Category
        app_state.sub_grouping_mode = ViewMode.CATEGORY

        # Step 3: Drill into Shopping category
        app_state.drill_down("Shopping", cursor_position=2, scroll_y=25.0)

        assert app_state.selected_merchant == "Amazon"
        assert app_state.selected_category == "Shopping"
        assert app_state.sub_grouping_mode is None  # Cleared on drill-down

        # Go back should restore Amazon > (by Category)
        success, cursor, scroll = app_state.go_back()

        assert success is True
        assert app_state.selected_merchant == "Amazon"
        assert app_state.selected_category is None
        assert app_state.sub_grouping_mode == ViewMode.CATEGORY
        assert cursor == 2
        assert scroll == 25.0

    def test_navigation_history_saves_all_selections(self, app_state):
        """Test that navigation history saves all drill-down selections."""

        # Drill down with multiple selections active
        app_state.view_mode = ViewMode.CATEGORY
        app_state.selected_merchant = "Amazon"  # Already filtered by merchant
        app_state.selected_category = None
        app_state.sub_grouping_mode = ViewMode.CATEGORY

        app_state.drill_down("Groceries", cursor_position=7, scroll_y=140.0)

        # Check saved state preserves everything
        saved_nav = app_state.navigation_history[-1]
        assert saved_nav.view_mode == ViewMode.CATEGORY
        assert saved_nav.selected_merchant == "Amazon"  # Merchant filter saved
        assert saved_nav.sub_grouping_mode == ViewMode.CATEGORY  # Sub-grouping saved

        # Now state should have both filters
        assert app_state.selected_merchant == "Amazon"  # Preserved
        assert app_state.selected_category == "Groceries"  # Added

    def test_go_back_restores_subgrouping_mode(self, app_state):
        """Test that go_back specifically restores sub_grouping_mode."""

        # Set up: Drilled into merchant with sub-grouping
        app_state.view_mode = ViewMode.DETAIL
        app_state.selected_merchant = "Starbucks"
        app_state.sub_grouping_mode = ViewMode.CATEGORY

        # Drill down into a category
        app_state.drill_down("Coffee Shops", cursor_position=1, scroll_y=10.0)

        # Verify sub-grouping was cleared
        assert app_state.sub_grouping_mode is None

        # Go back
        app_state.go_back()

        # Sub-grouping should be restored
        assert app_state.sub_grouping_mode == ViewMode.CATEGORY
        assert app_state.selected_merchant == "Starbucks"
        assert app_state.selected_category is None  # Cleared by go_back

    def test_three_level_drill_down_and_back(self, app_state):
        """Test three levels deep: Category → sub-group → drill → drill."""

        # Level 1: Drill into Travel from Group view
        app_state.view_mode = ViewMode.GROUP
        app_state.drill_down("Travel", cursor_position=2, scroll_y=20.0)

        # Level 2: Sub-group by Merchant
        app_state.sub_grouping_mode = ViewMode.MERCHANT

        # Level 3: Drill into United Airlines
        app_state.drill_down("United Airlines", cursor_position=0, scroll_y=0.0)

        assert len(app_state.navigation_history) == 2

        # First go_back: Restore Travel > (by Merchant)
        app_state.go_back()
        assert app_state.selected_group == "Travel"
        assert app_state.selected_merchant is None
        assert app_state.sub_grouping_mode == ViewMode.MERCHANT

        # Second go_back: Clear sub-grouping, stay in Travel detail
        app_state.go_back()
        assert app_state.view_mode == ViewMode.DETAIL
        assert app_state.selected_group == "Travel"
        assert app_state.sub_grouping_mode is None

        # Third go_back: Return to Group view
        app_state.go_back()
        assert app_state.view_mode == ViewMode.GROUP
        assert app_state.selected_group is None
        assert app_state.sub_grouping_mode is None


class TestTimeNavigation:
    """Tests for time period navigation and granularity."""

    def test_is_time_period_selected_when_year_set(self, app_state):
        """Should return True when year is selected."""
        app_state.selected_time_year = 2024
        assert app_state.is_time_period_selected() is True

    def test_is_time_period_selected_when_not_set(self, app_state):
        """Should return False when year is not selected."""
        assert app_state.is_time_period_selected() is False

    def test_get_selected_time_period_year_only(self, app_state):
        """Should return (year, None) when only year selected."""
        app_state.selected_time_year = 2024
        assert app_state.get_selected_time_period() == (2024, None)

    def test_get_selected_time_period_year_and_month(self, app_state):
        """Should return (year, month) when both selected."""
        app_state.selected_time_year = 2024
        app_state.selected_time_month = 3
        assert app_state.get_selected_time_period() == (2024, 3)

    def test_get_selected_time_period_none(self, app_state):
        """Should return None when no time period selected."""
        assert app_state.get_selected_time_period() is None

    def test_clear_time_selection(self, app_state):
        """Should clear both year and month."""
        app_state.selected_time_year = 2024
        app_state.selected_time_month = 6
        app_state.clear_time_selection()
        assert app_state.selected_time_year is None
        assert app_state.selected_time_month is None

    @pytest.mark.parametrize(
        "start_granularity,expected_granularity,expected_result",
        [
            (TimeGranularity.YEAR, TimeGranularity.MONTH, "Months"),
            (TimeGranularity.MONTH, TimeGranularity.DAY, "Days"),
            (TimeGranularity.DAY, TimeGranularity.YEAR, "Years"),
        ],
    )
    def test_toggle_time_granularity(
        self, app_state, start_granularity, expected_granularity, expected_result
    ):
        """Test cycling time granularity."""
        app_state.time_granularity = start_granularity
        assert app_state.toggle_time_granularity() == expected_result
        assert app_state.time_granularity == expected_granularity

    @pytest.mark.parametrize(
        "start_year, start_month, granularity, direction, expected_year, expected_month, expected_result",
        [
            (2024, None, TimeGranularity.YEAR, 1, 2025, None, "2025"),
            (2024, None, TimeGranularity.YEAR, -1, 2023, None, "2023"),
            (2024, 3, TimeGranularity.MONTH, 1, 2024, 4, "Apr 2024"),
            (2024, 3, TimeGranularity.MONTH, -1, 2024, 2, "Feb 2024"),
            (2024, 12, TimeGranularity.MONTH, 1, 2025, 1, "Jan 2025"),
            (2024, 1, TimeGranularity.MONTH, -1, 2023, 12, "Dec 2023"),
        ],
    )
    def test_navigate_time_period(
        self,
        app_state,
        start_year,
        start_month,
        granularity,
        direction,
        expected_year,
        expected_month,
        expected_result,
    ):
        """Test time period navigation."""
        app_state.selected_time_year = start_year
        if start_month is not None:
            app_state.selected_time_month = start_month
        app_state.time_granularity = granularity

        result = app_state.navigate_time_period(direction)

        assert app_state.selected_time_year == expected_year
        assert app_state.selected_time_month == expected_month
        assert result == expected_result

    def test_navigate_time_period_returns_none_when_not_selected(self, app_state):
        """Should return None when no time period is selected."""
        result = app_state.navigate_time_period(1)
        assert result is None
