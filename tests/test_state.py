"""
Tests for state management, undo/redo, and change tracking.
"""

from datetime import date

import polars as pl

from moneyflow.state import (
    AppState,
    NavigationState,
    SortDirection,
    SortMode,
    TimeFrame,
    ViewMode,
)


class TestAppState:
    """Test AppState initialization and basic operations."""

    def test_initial_state(self, app_state):
        """Test that AppState initializes with correct defaults."""
        assert app_state.view_mode == ViewMode.MERCHANT
        assert app_state.sort_by == SortMode.AMOUNT
        assert app_state.sort_direction == SortDirection.DESC
        assert app_state.time_frame == TimeFrame.THIS_YEAR
        assert app_state.transactions_df is None
        assert len(app_state.pending_edits) == 0
        assert len(app_state.selected_ids) == 0
        assert app_state.search_query == ""

    def test_set_timeframe_this_year(self, app_state):
        """Test setting timeframe to this year."""
        app_state.set_timeframe(TimeFrame.THIS_YEAR)

        assert app_state.time_frame == TimeFrame.THIS_YEAR
        assert app_state.start_date == date(date.today().year, 1, 1)
        assert app_state.end_date == date(date.today().year, 12, 31)

    def test_set_timeframe_this_month(self, app_state):
        """Test setting timeframe to this month."""
        app_state.set_timeframe(TimeFrame.THIS_MONTH)

        assert app_state.time_frame == TimeFrame.THIS_MONTH
        assert app_state.start_date.month == date.today().month
        assert app_state.start_date.day == 1

    def test_set_timeframe_custom(self, app_state):
        """Test setting custom timeframe."""
        start = date(2024, 1, 1)
        end = date(2024, 6, 30)

        app_state.set_timeframe(TimeFrame.CUSTOM, start_date=start, end_date=end)

        assert app_state.time_frame == TimeFrame.CUSTOM
        assert app_state.start_date == start
        assert app_state.end_date == end

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
            {
                "id": "txn_1",
                "date": date(2024, 10, 1),
                "amount": -100.00,
                "merchant": "Transfer",
                "merchant_id": "merch_1",
                "category": "Transfer",
                "category_id": "cat_1",
                "group": "Transfers",
                "account": "Checking",
                "account_id": "acc_1",
                "notes": "",
                "hideFromReports": False,
                "pending": False,
                "is_recurring": False,
            },
            {
                "id": "txn_2",
                "date": date(2024, 10, 2),
                "amount": -50.00,
                "merchant": "Store",
                "merchant_id": "merch_2",
                "category": "Shopping",
                "category_id": "cat_2",
                "group": "Shopping",
                "account": "Checking",
                "account_id": "acc_1",
                "notes": "",
                "hideFromReports": False,
                "pending": False,
                "is_recurring": False,
            },
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
            {
                "id": "txn_1",
                "date": date(2024, 10, 1),
                "amount": -100.00,
                "merchant": "Hidden Merchant",
                "merchant_id": "merch_1",
                "category": "Shopping",
                "category_id": "cat_1",
                "group": "Shopping",
                "account": "Checking",
                "account_id": "acc_1",
                "notes": "",
                "hideFromReports": True,
                "pending": False,
                "is_recurring": False,
            },
            {
                "id": "txn_2",
                "date": date(2024, 10, 2),
                "amount": -50.00,
                "merchant": "Visible Merchant",
                "merchant_id": "merch_2",
                "category": "Shopping",
                "category_id": "cat_2",
                "group": "Shopping",
                "account": "Checking",
                "account_id": "acc_1",
                "notes": "",
                "hideFromReports": False,
                "pending": False,
                "is_recurring": False,
            },
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
            {
                "id": "txn_1",
                "date": date(2024, 10, 1),
                "amount": -100.00,
                "merchant": "Amazon",
                "merchant_id": "merch_1",
                "category": "Shopping",
                "category_id": "cat_1",
                "group": "Shopping",
                "account": "Checking",
                "account_id": "acc_1",
                "notes": "",
                "hideFromReports": True,
                "pending": False,
                "is_recurring": False,
            },
            {
                "id": "txn_2",
                "date": date(2024, 10, 2),
                "amount": -50.00,
                "merchant": "Amazon",
                "merchant_id": "merch_1",
                "category": "Shopping",
                "category_id": "cat_2",
                "group": "Shopping",
                "account": "Checking",
                "account_id": "acc_1",
                "notes": "",
                "hideFromReports": False,
                "pending": False,
                "is_recurring": False,
            },
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
            {
                "id": "txn_1",
                "date": date(2024, 10, 1),
                "amount": -100.00,
                "merchant": "Store A",
                "merchant_id": "merch_1",
                "category": "Groceries",
                "category_id": "cat_1",
                "group": "Food & Dining",
                "account": "Checking",
                "account_id": "acc_1",
                "notes": "",
                "hideFromReports": True,
                "pending": False,
                "is_recurring": False,
            },
            {
                "id": "txn_2",
                "date": date(2024, 10, 2),
                "amount": -50.00,
                "merchant": "Store B",
                "merchant_id": "merch_2",
                "category": "Groceries",
                "category_id": "cat_1",
                "group": "Food & Dining",
                "account": "Checking",
                "account_id": "acc_1",
                "notes": "",
                "hideFromReports": False,
                "pending": False,
                "is_recurring": False,
            },
            {
                "id": "txn_3",
                "date": date(2024, 10, 3),
                "amount": -25.00,
                "merchant": "Store C",
                "merchant_id": "merch_3",
                "category": "Gas",
                "category_id": "cat_2",
                "group": "Transportation",
                "account": "Checking",
                "account_id": "acc_1",
                "notes": "",
                "hideFromReports": True,
                "pending": False,
                "is_recurring": False,
            },
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
            {
                "id": "txn_1",
                "date": date(2024, 1, 1),
                "amount": -100.00,
                "merchant": "Starbucks Downtown",
                "merchant_id": "merch_1",
                "category": "Restaurants & Bars",
                "category_id": "cat_1",
                "group": "Food & Dining",
                "account": "Checking",
                "account_id": "acc_1",
                "notes": "",
                "hideFromReports": False,
                "pending": False,
                "is_recurring": False,
            },
            {
                "id": "txn_2",
                "date": date(2024, 1, 15),
                "amount": -50.00,
                "merchant": "Starbucks Uptown",
                "merchant_id": "merch_2",
                "category": "Restaurants & Bars",
                "category_id": "cat_1",
                "group": "Food & Dining",
                "account": "Checking",
                "account_id": "acc_1",
                "notes": "",
                "hideFromReports": False,
                "pending": False,
                "is_recurring": False,
            },
            {
                "id": "txn_3",
                "date": date(2024, 2, 1),
                "amount": -75.00,
                "merchant": "Starbucks Mall",
                "merchant_id": "merch_3",
                "category": "Restaurants & Bars",
                "category_id": "cat_1",
                "group": "Food & Dining",
                "account": "Checking",
                "account_id": "acc_1",
                "notes": "",
                "hideFromReports": False,
                "pending": False,
                "is_recurring": False,
            },
            {
                "id": "txn_4",
                "date": date(2024, 1, 20),
                "amount": 200.00,
                "merchant": "Transfer In",
                "merchant_id": "merch_4",
                "category": "Transfer",
                "category_id": "cat_2",
                "group": "Transfers",
                "account": "Checking",
                "account_id": "acc_1",
                "notes": "",
                "hideFromReports": False,
                "pending": False,
                "is_recurring": False,
            },
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

        assert "Orders" in breadcrumb
        assert "113-1234567-8901234" in breadcrumb
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

    def test_breadcrumb_detail_view_merchant(self, app_state):
        """Test breadcrumb for detail view drilled down from merchant."""
        app_state.view_mode = ViewMode.DETAIL
        app_state.selected_merchant = "Starbucks"

        breadcrumb = app_state.get_breadcrumb()

        assert "Merchants" in breadcrumb
        assert "Starbucks" in breadcrumb

    def test_breadcrumb_detail_view_category(self, app_state):
        """Test breadcrumb for detail view drilled down from category."""
        app_state.view_mode = ViewMode.DETAIL
        app_state.selected_category = "Groceries"

        breadcrumb = app_state.get_breadcrumb()

        assert "Categories" in breadcrumb
        assert "Groceries" in breadcrumb

    def test_breadcrumb_detail_view_group(self, app_state):
        """Test breadcrumb for detail view drilled down from group."""
        app_state.view_mode = ViewMode.DETAIL
        app_state.selected_group = "Food & Dining"

        breadcrumb = app_state.get_breadcrumb()

        assert "Groups" in breadcrumb
        assert "Food & Dining" in breadcrumb

    def test_breadcrumb_detail_view_no_selection(self, app_state):
        """Test breadcrumb for detail view with no selection."""
        app_state.view_mode = ViewMode.DETAIL

        breadcrumb = app_state.get_breadcrumb()

        assert "Transactions" in breadcrumb

    def test_breadcrumb_with_this_year_timeframe(self, app_state):
        """Test breadcrumb includes year when in THIS_YEAR mode."""
        app_state.view_mode = ViewMode.MERCHANT
        app_state.set_timeframe(TimeFrame.THIS_YEAR)

        breadcrumb = app_state.get_breadcrumb()

        assert "Year" in breadcrumb
        assert str(date.today().year) in breadcrumb

    def test_breadcrumb_with_this_month_timeframe(self, app_state):
        """Test breadcrumb includes month when in THIS_MONTH mode."""
        app_state.view_mode = ViewMode.MERCHANT
        app_state.set_timeframe(TimeFrame.THIS_MONTH)

        breadcrumb = app_state.get_breadcrumb()

        # Should include month name
        month_name = date.today().strftime("%B")
        assert month_name in breadcrumb
        assert str(date.today().year) in breadcrumb

    def test_breadcrumb_with_custom_single_month(self, app_state):
        """Test breadcrumb for custom timeframe spanning a single month."""
        app_state.view_mode = ViewMode.MERCHANT
        app_state.set_timeframe(
            TimeFrame.CUSTOM, start_date=date(2024, 3, 1), end_date=date(2024, 3, 31)
        )

        breadcrumb = app_state.get_breadcrumb()

        assert "March" in breadcrumb
        assert "2024" in breadcrumb

    def test_breadcrumb_with_custom_date_range(self, app_state):
        """Test breadcrumb for custom timeframe spanning multiple months."""
        app_state.view_mode = ViewMode.MERCHANT
        app_state.set_timeframe(
            TimeFrame.CUSTOM, start_date=date(2024, 1, 1), end_date=date(2024, 6, 30)
        )

        breadcrumb = app_state.get_breadcrumb()

        assert "2024-01-01" in breadcrumb
        assert "2024-06-30" in breadcrumb
        assert "to" in breadcrumb


class TestTimeFrameEdgeCases:
    """Test edge cases in time frame handling."""

    def test_set_timeframe_all_time(self, app_state):
        """Test setting timeframe to ALL_TIME clears dates."""
        # First set some dates
        app_state.set_timeframe(
            TimeFrame.CUSTOM, start_date=date(2024, 1, 1), end_date=date(2024, 12, 31)
        )
        assert app_state.start_date is not None
        assert app_state.end_date is not None

        # Now set to ALL_TIME
        app_state.set_timeframe(TimeFrame.ALL_TIME)

        assert app_state.time_frame == TimeFrame.ALL_TIME
        assert app_state.start_date is None
        assert app_state.end_date is None

    def test_set_timeframe_this_month_december(self, app_state):
        """Test setting timeframe to THIS_MONTH handles December correctly."""
        # Mock today being in December - must mock in time_navigator module
        from unittest.mock import patch

        with patch("moneyflow.time_navigator.date") as mock_date:
            mock_date.today.return_value = date(2024, 12, 15)
            mock_date.side_effect = lambda *args, **kwargs: date(*args, **kwargs)

            app_state.set_timeframe(TimeFrame.THIS_MONTH)

            assert app_state.start_date == date(2024, 12, 1)
            assert app_state.end_date == date(2024, 12, 31)

    def test_set_timeframe_this_month_february_leap_year(self, app_state):
        """Test THIS_MONTH handles February in a leap year."""
        from unittest.mock import patch

        with patch("moneyflow.time_navigator.date") as mock_date:
            mock_date.today.return_value = date(2024, 2, 15)  # 2024 is leap year
            mock_date.side_effect = lambda *args, **kwargs: date(*args, **kwargs)

            app_state.set_timeframe(TimeFrame.THIS_MONTH)

            assert app_state.start_date == date(2024, 2, 1)
            assert app_state.end_date == date(2024, 2, 29)  # Leap year has 29 days

    def test_set_timeframe_this_month_february_non_leap_year(self, app_state):
        """Test THIS_MONTH handles February in a non-leap year."""
        from unittest.mock import patch

        with patch("moneyflow.time_navigator.date") as mock_date:
            mock_date.today.return_value = date(2023, 2, 15)  # 2023 is not leap year
            mock_date.side_effect = lambda *args, **kwargs: date(*args, **kwargs)

            app_state.set_timeframe(TimeFrame.THIS_MONTH)

            assert app_state.start_date == date(2023, 2, 1)
            assert app_state.end_date == date(2023, 2, 28)  # Non-leap year has 28 days


class TestSubGrouping:
    """Tests for sub-grouping within drilled-down views."""

    def test_is_drilled_down_with_merchant(self):
        """Should return True when merchant is selected."""
        state = AppState()
        state.selected_merchant = "Amazon"
        assert state.is_drilled_down() is True

    def test_is_drilled_down_with_category(self):
        """Should return True when category is selected."""
        state = AppState()
        state.selected_category = "Groceries"
        assert state.is_drilled_down() is True

    def test_is_drilled_down_no_selection(self):
        """Should return False with no selections."""
        state = AppState()
        assert state.is_drilled_down() is False

    def test_cycle_sub_grouping_from_merchant_includes_category(self):
        """When drilled into Merchant, should offer Category sub-grouping."""
        state = AppState()
        state.view_mode = ViewMode.DETAIL
        state.selected_merchant = "Amazon"

        result = state.cycle_sub_grouping()

        # First cycle should go to Category (Merchant is excluded)
        assert state.sub_grouping_mode == ViewMode.CATEGORY
        assert result == "by Category"

    def test_cycle_sub_grouping_from_category_includes_merchant(self):
        """When drilled into Category, should offer Merchant sub-grouping."""
        state = AppState()
        state.view_mode = ViewMode.DETAIL
        state.selected_category = "Groceries"

        result = state.cycle_sub_grouping()

        # First cycle should go to Merchant (Category is excluded)
        assert state.sub_grouping_mode == ViewMode.MERCHANT
        assert result == "by Merchant"

    def test_cycle_sub_grouping_full_cycle_from_merchant(self):
        """Should cycle through all modes (excluding Merchant) then back."""
        state = AppState()
        state.view_mode = ViewMode.DETAIL
        state.selected_merchant = "Amazon"

        # Cycle: Category → Group → Account → Detail → Category
        assert state.cycle_sub_grouping() == "by Category"
        assert state.sub_grouping_mode == ViewMode.CATEGORY

        assert state.cycle_sub_grouping() == "by Group"
        assert state.sub_grouping_mode == ViewMode.GROUP

        assert state.cycle_sub_grouping() == "by Account"
        assert state.sub_grouping_mode == ViewMode.ACCOUNT

        assert state.cycle_sub_grouping() == "Detail"
        assert state.sub_grouping_mode is None

        # Back to Category
        assert state.cycle_sub_grouping() == "by Category"
        assert state.sub_grouping_mode == ViewMode.CATEGORY

    def test_cycle_sub_grouping_full_cycle_from_category(self):
        """Should cycle through all modes (excluding Category) then back."""
        state = AppState()
        state.view_mode = ViewMode.DETAIL
        state.selected_category = "Groceries"

        # Cycle: Merchant → Group → Account → Detail → Merchant
        assert state.cycle_sub_grouping() == "by Merchant"
        assert state.sub_grouping_mode == ViewMode.MERCHANT

        assert state.cycle_sub_grouping() == "by Group"
        assert state.sub_grouping_mode == ViewMode.GROUP

        assert state.cycle_sub_grouping() == "by Account"
        assert state.sub_grouping_mode == ViewMode.ACCOUNT

        assert state.cycle_sub_grouping() == "Detail"
        assert state.sub_grouping_mode is None

        # Back to Merchant
        assert state.cycle_sub_grouping() == "by Merchant"
        assert state.sub_grouping_mode == ViewMode.MERCHANT

    def test_cycle_grouping_delegates_to_sub_grouping_when_drilled_down(self):
        """When drilled down, cycle_grouping should delegate to sub-grouping."""
        state = AppState()
        state.view_mode = ViewMode.DETAIL
        state.selected_merchant = "Amazon"

        result = state.cycle_grouping()

        # Should have called cycle_sub_grouping
        assert state.sub_grouping_mode == ViewMode.CATEGORY
        assert result == "by Category"

    def test_cycle_grouping_works_normally_when_not_drilled_down(self):
        """When not drilled down, should cycle top-level views."""
        state = AppState()
        state.view_mode = ViewMode.MERCHANT

        result = state.cycle_grouping()

        # Should cycle to Category view
        assert state.view_mode == ViewMode.CATEGORY
        assert result == "Categories"

    def test_cycle_grouping_from_detail_view_with_history_restores_previous_view(self):
        """Pressing 'g' from top-level DETAIL view with navigation history should restore previous view."""
        state = AppState()
        state.view_mode = ViewMode.DETAIL
        state.sort_by = SortMode.DATE
        state.sort_direction = SortDirection.ASC

        # Simulate having navigation history from a previous CATEGORY view
        nav_state = NavigationState(
            view_mode=ViewMode.CATEGORY,
            sort_by=SortMode.CATEGORY,
            sort_direction=SortDirection.DESC,
            cursor_position=5,
            scroll_y=100.0,
        )
        state.navigation_history.append(nav_state)

        result = state.cycle_grouping()

        # Should restore to CATEGORY view with previous sort settings
        assert state.view_mode == ViewMode.CATEGORY
        assert state.sort_by == SortMode.CATEGORY
        assert state.sort_direction == SortDirection.DESC
        assert result == "Categories"
        # Navigation history should be consumed
        assert len(state.navigation_history) == 0

    def test_cycle_grouping_from_detail_view_without_history_defaults_to_merchant(self):
        """Pressing 'g' from top-level DETAIL view without history should default to MERCHANT view."""
        state = AppState()
        state.view_mode = ViewMode.DETAIL
        state.sort_by = SortMode.DATE
        state.sort_direction = SortDirection.ASC
        # No navigation history

        result = state.cycle_grouping()

        # Should default to MERCHANT view
        assert state.view_mode == ViewMode.MERCHANT
        # Sort settings should be preserved from current state
        assert state.sort_by == SortMode.DATE
        assert state.sort_direction == SortDirection.ASC
        assert result == "Merchants"

    def test_cycle_sub_grouping_resets_date_sort_to_amount(self):
        """When cycling from detail to aggregated sub-grouping, should reset DATE sort to AMOUNT."""
        state = AppState()
        state.view_mode = ViewMode.DETAIL
        state.selected_category = "Coffee Shops"
        state.sub_grouping_mode = None  # Currently in detail view
        state.sort_by = SortMode.DATE  # Sorted by date (valid for detail)

        # Cycle to aggregated sub-grouping
        result = state.cycle_sub_grouping()

        # Should switch to aggregated view and reset sort from DATE to AMOUNT
        assert state.sub_grouping_mode == ViewMode.MERCHANT
        assert state.sort_by == SortMode.AMOUNT
        assert result == "by Merchant"

    def test_cycle_sub_grouping_preserves_count_sort(self):
        """When cycling from detail to aggregated sub-grouping, COUNT sort should be preserved."""
        state = AppState()
        state.view_mode = ViewMode.DETAIL
        state.selected_merchant = "Amazon"
        state.sub_grouping_mode = None  # Currently in detail view
        state.sort_by = SortMode.COUNT  # Already valid for aggregated views

        # Cycle to aggregated sub-grouping
        result = state.cycle_sub_grouping()

        # Should preserve COUNT sort
        assert state.sub_grouping_mode == ViewMode.CATEGORY
        assert state.sort_by == SortMode.COUNT
        assert result == "by Category"

    def test_cycle_sub_grouping_preserves_amount_sort(self):
        """When cycling from detail to aggregated sub-grouping, AMOUNT sort should be preserved."""
        state = AppState()
        state.view_mode = ViewMode.DETAIL
        state.selected_group = "Food & Dining"
        state.sub_grouping_mode = None  # Currently in detail view
        state.sort_by = SortMode.AMOUNT

        # Cycle to aggregated sub-grouping
        state.cycle_sub_grouping()

        # Should preserve AMOUNT sort
        assert state.sub_grouping_mode == ViewMode.MERCHANT
        assert state.sort_by == SortMode.AMOUNT

    def test_cycle_sub_grouping_resets_invalid_aggregate_field_sort(self):
        """When cycling between sub-groupings, invalid aggregate field sorts should reset to AMOUNT."""
        state = AppState()
        state.view_mode = ViewMode.DETAIL
        state.selected_category = "Groceries"  # Drilled into category
        state.sub_grouping_mode = ViewMode.MERCHANT  # Sub-grouped by merchant
        state.sort_by = SortMode.MERCHANT  # Sorting by merchant (valid for current sub-grouping)

        # Cycle to next sub-grouping (will be GROUP since CATEGORY is excluded)
        state.cycle_sub_grouping()

        # Should be sub-grouped by group now, and MERCHANT sort should reset to AMOUNT
        assert state.sub_grouping_mode == ViewMode.GROUP
        assert state.sort_by == SortMode.AMOUNT  # MERCHANT is not valid for group sub-grouping

    def test_cycle_sub_grouping_resets_merchant_sort_when_switching_to_category(self):
        """MERCHANT sort should reset to AMOUNT when cycling to category sub-grouping."""
        state = AppState()
        state.view_mode = ViewMode.DETAIL
        state.selected_account = "Chase Checking"
        state.sub_grouping_mode = ViewMode.MERCHANT
        state.sort_by = SortMode.MERCHANT

        state.cycle_sub_grouping()  # Merchant → Category

        assert state.sub_grouping_mode == ViewMode.CATEGORY
        assert state.sort_by == SortMode.AMOUNT

    def test_cycle_sub_grouping_resets_category_sort_when_switching_to_merchant(self):
        """CATEGORY sort should reset to AMOUNT when cycling to merchant sub-grouping."""
        state = AppState()
        state.view_mode = ViewMode.DETAIL
        state.selected_group = "Food & Dining"
        state.sub_grouping_mode = ViewMode.CATEGORY
        state.sort_by = SortMode.CATEGORY

        state.cycle_sub_grouping()  # Category → Account

        assert state.sub_grouping_mode == ViewMode.ACCOUNT
        assert state.sort_by == SortMode.AMOUNT

    def test_cycle_sub_grouping_preserves_matching_aggregate_field_sort(self):
        """When cycling to sub-grouping by X, and already sorting by X, preserve the sort."""
        state = AppState()
        state.view_mode = ViewMode.DETAIL
        state.selected_category = "Groceries"
        state.sub_grouping_mode = ViewMode.ACCOUNT
        state.sort_by = SortMode.ACCOUNT  # Sorting by account

        # Cycle through: Account → None(detail) → Merchant
        state.cycle_sub_grouping()  # Account → None (detail)
        assert state.sub_grouping_mode is None
        # When we go to detail, account sort should be preserved (it's valid for detail)
        assert state.sort_by == SortMode.ACCOUNT

        state.cycle_sub_grouping()  # None → Merchant
        # ACCOUNT sort is not valid for merchant sub-grouping, should reset
        assert state.sub_grouping_mode == ViewMode.MERCHANT
        assert state.sort_by == SortMode.AMOUNT

    def test_cycle_sub_grouping_preserves_count_when_cycling_between_modes(self):
        """COUNT sort should be preserved when cycling between any sub-grouping modes."""
        state = AppState()
        state.view_mode = ViewMode.DETAIL
        state.selected_merchant = "Amazon"  # MERCHANT is excluded from available modes
        state.sub_grouping_mode = ViewMode.CATEGORY
        state.sort_by = SortMode.COUNT

        state.cycle_sub_grouping()  # Category → Group (not Account, since available: Category, Group, Account, None)

        assert state.sub_grouping_mode == ViewMode.GROUP
        assert state.sort_by == SortMode.COUNT  # Preserved

    def test_cycle_sub_grouping_preserves_amount_when_cycling_between_modes(self):
        """AMOUNT sort should be preserved when cycling between any sub-grouping modes."""
        state = AppState()
        state.view_mode = ViewMode.DETAIL
        state.selected_category = "Groceries"  # CATEGORY is excluded from available modes
        state.sub_grouping_mode = ViewMode.MERCHANT
        state.sort_by = SortMode.AMOUNT

        state.cycle_sub_grouping()  # Merchant → Group (available: Merchant, Group, Account, None)

        assert state.sub_grouping_mode == ViewMode.GROUP
        assert state.sort_by == SortMode.AMOUNT  # Preserved

    def test_go_back_clears_sub_grouping_first(self):
        """Escape should clear sub-grouping before going back."""
        state = AppState()
        state.view_mode = ViewMode.DETAIL
        state.selected_merchant = "Amazon"
        state.sub_grouping_mode = ViewMode.CATEGORY

        success, cursor, _ = state.go_back()

        # Should clear sub-grouping, stay drilled into Amazon
        assert success is True
        assert state.sub_grouping_mode is None
        assert state.selected_merchant == "Amazon"
        assert state.view_mode == ViewMode.DETAIL

    def test_go_back_then_clears_drill_down(self):
        """Second Escape should clear drill-down."""
        state = AppState()
        state.view_mode = ViewMode.DETAIL
        state.selected_merchant = "Amazon"
        state.sub_grouping_mode = ViewMode.CATEGORY
        # Navigation history uses NavigationState object
        state.navigation_history.append(
            NavigationState(
                view_mode=ViewMode.MERCHANT,
                cursor_position=5,
                scroll_y=125.0,
                sort_by=SortMode.AMOUNT,
                sort_direction=SortDirection.DESC,
            )
        )

        # First escape: clear sub-grouping
        success1, _, _ = state.go_back()
        assert success1 is True
        assert state.sub_grouping_mode is None
        assert state.selected_merchant == "Amazon"

        # Second escape: clear drill-down
        success2, cursor, scroll_y = state.go_back()
        assert success2 is True
        assert state.selected_merchant is None
        assert state.view_mode == ViewMode.MERCHANT
        assert cursor == 5
        assert scroll_y == 125.0

    def test_go_back_clears_sub_grouping_and_restores_sort(self):
        """When clearing sub-grouping, should restore sort state from navigation history."""
        state = AppState()
        state.view_mode = ViewMode.DETAIL
        state.selected_merchant = "Amazon"
        state.sub_grouping_mode = ViewMode.CATEGORY
        state.sort_by = SortMode.AMOUNT  # Changed to AMOUNT due to sub-grouping

        # Simulate proper navigation flow:
        # 1. Drill down saved merchant view state
        state.navigation_history.append(
            NavigationState(
                view_mode=ViewMode.MERCHANT,
                cursor_position=10,
                scroll_y=200.0,
                sort_by=SortMode.AMOUNT,
                sort_direction=SortDirection.DESC,
            )
        )
        # 2. Entering sub-grouping saved detail view state (with ACCOUNT sort)
        state.navigation_history.append(
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
        success, _, _ = state.go_back()

        # Should clear sub-grouping AND restore sort from detail view state
        assert success is True
        assert state.sub_grouping_mode is None
        assert state.selected_merchant == "Amazon"  # Still drilled down
        assert state.sort_by == SortMode.ACCOUNT  # Restored from entering-sub-grouping state
        assert state.sort_direction == SortDirection.ASC  # Restored from history
        # Navigation history should have popped the sub-grouping entry, leaving drill-down entry
        assert len(state.navigation_history) == 1

    def test_go_back_clears_sub_grouping_preserves_count_sort(self):
        """When clearing sub-grouping with COUNT sort, preserve it (it's valid)."""
        state = AppState()
        state.view_mode = ViewMode.DETAIL
        state.selected_category = "Groceries"
        state.sub_grouping_mode = ViewMode.MERCHANT
        state.sort_by = SortMode.COUNT  # COUNT is valid everywhere

        state.navigation_history.append(
            NavigationState(
                view_mode=ViewMode.CATEGORY,
                sort_by=SortMode.COUNT,  # Was also COUNT
                sort_direction=SortDirection.DESC,
            )
        )

        # Press Esc to clear sub-grouping
        state.go_back()

        # COUNT should be preserved (it's valid in both modes)
        assert state.sub_grouping_mode is None
        assert state.sort_by == SortMode.COUNT

    def test_cycle_sub_grouping_saves_state_when_entering_subgrouping(self):
        """When entering sub-grouping mode, should save current state to navigation history."""
        state = AppState()
        state.view_mode = ViewMode.DETAIL
        state.selected_merchant = "Amazon"
        state.sub_grouping_mode = None  # Not yet sub-grouped
        state.sort_by = SortMode.ACCOUNT  # Current sort
        state.sort_direction = SortDirection.ASC

        # Navigation history has one entry from drill-down
        state.navigation_history.append(
            NavigationState(
                view_mode=ViewMode.MERCHANT,
                sort_by=SortMode.AMOUNT,
                sort_direction=SortDirection.DESC,
            )
        )

        # Press g to enter sub-grouping
        state.cycle_sub_grouping()

        # Should save detail view state before changing sort
        assert len(state.navigation_history) == 2
        saved_state = state.navigation_history[-1]
        assert saved_state.view_mode == ViewMode.DETAIL
        assert saved_state.sort_by == SortMode.ACCOUNT  # Saved before changing
        assert saved_state.sort_direction == SortDirection.ASC
        assert saved_state.selected_merchant == "Amazon"
        assert saved_state.sub_grouping_mode is None

    def test_go_back_from_subgrouping_pops_navigation_history(self):
        """When clearing sub-grouping, should pop from navigation history."""
        state = AppState()
        state.view_mode = ViewMode.DETAIL
        state.selected_merchant = "Amazon"
        state.sub_grouping_mode = ViewMode.CATEGORY
        state.sort_by = SortMode.AMOUNT  # Changed to AMOUNT by sub-grouping

        # Two entries: drill-down + entering sub-grouping
        state.navigation_history.append(
            NavigationState(view_mode=ViewMode.MERCHANT, sort_by=SortMode.AMOUNT)
        )
        state.navigation_history.append(
            NavigationState(
                view_mode=ViewMode.DETAIL,
                sort_by=SortMode.ACCOUNT,  # Before sub-grouping
                sort_direction=SortDirection.ASC,
                selected_merchant="Amazon",
            )
        )

        # Press Esc to clear sub-grouping
        state.go_back()

        # Should restore ACCOUNT sort and pop from history
        assert state.sort_by == SortMode.ACCOUNT
        assert state.sort_direction == SortDirection.ASC
        assert len(state.navigation_history) == 1  # Popped the sub-grouping entry

    def test_breadcrumb_shows_sub_grouping(self):
        """Breadcrumb should show sub-grouping mode."""
        state = AppState()
        state.view_mode = ViewMode.DETAIL
        state.selected_merchant = "Amazon"
        state.sub_grouping_mode = ViewMode.CATEGORY
        state.start_date = date(2025, 1, 1)
        state.end_date = date(2025, 12, 31)
        state.time_frame = TimeFrame.THIS_YEAR

        breadcrumb = state.get_breadcrumb()

        assert "Merchants" in breadcrumb
        assert "Amazon" in breadcrumb
        assert "(by Category)" in breadcrumb
        assert "Year 2025" in breadcrumb

    def test_breadcrumb_multi_level_drill_down(self):
        """Breadcrumb should show multiple drill-down levels."""
        state = AppState()
        state.view_mode = ViewMode.DETAIL
        state.selected_merchant = "Amazon"
        state.selected_category = "Groceries"
        state.start_date = date(2025, 10, 1)
        state.end_date = date(2025, 10, 31)
        state.time_frame = TimeFrame.THIS_MONTH

        breadcrumb = state.get_breadcrumb()

        assert "Merchants" in breadcrumb
        assert "Amazon" in breadcrumb
        assert "Groceries" in breadcrumb
        assert "October 2025" in breadcrumb

    def test_multi_level_go_back_clears_deepest_first(self):
        """Multi-level drill-down should clear deepest selection first."""
        state = AppState()
        state.view_mode = ViewMode.DETAIL
        state.selected_merchant = "Amazon"
        state.selected_category = "Groceries"

        # First go_back: clear category (deepest)
        success, _, _ = state.go_back()
        assert success is True
        assert state.selected_category is None
        assert state.selected_merchant == "Amazon"

        # Second go_back: clear merchant
        success, _, _ = state.go_back()
        assert success is True
        assert state.selected_merchant is None


class TestSmartSearchEscape:
    """Tests for smart search escape behavior."""

    def test_get_navigation_depth_top_level(self):
        """Top-level views should have depth 0."""
        state = AppState()
        state.view_mode = ViewMode.MERCHANT
        assert state.get_navigation_depth() == 0

    def test_get_navigation_depth_one_level(self):
        """Drilled once should have depth 1."""
        state = AppState()
        state.view_mode = ViewMode.DETAIL
        state.selected_merchant = "Amazon"
        assert state.get_navigation_depth() == 1

    def test_get_navigation_depth_two_levels(self):
        """Drilled twice should have depth 2."""
        state = AppState()
        state.view_mode = ViewMode.DETAIL
        state.selected_merchant = "Amazon"
        state.selected_category = "Groceries"
        assert state.get_navigation_depth() == 2

    def test_set_search_saves_navigation_state(self):
        """Setting search should save current navigation state."""
        state = AppState()
        state.view_mode = ViewMode.DETAIL
        state.selected_merchant = "Amazon"

        state.set_search("coffee")

        assert state.search_query == "coffee"
        assert state.search_navigation_state is not None
        assert state.search_navigation_state == (1, None)  # depth 1, no sub-grouping

    def test_set_search_with_sub_grouping(self):
        """Search with sub-grouping should save that state."""
        state = AppState()
        state.view_mode = ViewMode.DETAIL
        state.selected_merchant = "Amazon"
        state.sub_grouping_mode = ViewMode.CATEGORY

        state.set_search("grocery")

        assert state.search_navigation_state == (1, ViewMode.CATEGORY)

    def test_clear_search_clears_navigation_state(self):
        """Clearing search should clear navigation state."""
        state = AppState()
        state.set_search("coffee")

        state.set_search("")

        assert state.search_query == ""
        assert state.search_navigation_state is None

    def test_escape_clears_search_when_no_navigation(self):
        """Scenario 1: Search without further navigation, Escape clears search."""
        state = AppState()
        state.view_mode = ViewMode.MERCHANT
        state.set_search("coffee")

        # No navigation happened, just searched
        success, _, _ = state.go_back()

        assert success is True
        assert state.search_query == ""
        assert state.view_mode == ViewMode.MERCHANT  # Still in Merchants view

    def test_escape_navigates_after_drill_down_with_search(self):
        """Scenario 2: Search then drill down, Escape navigates (search persists)."""
        state = AppState()
        state.view_mode = ViewMode.MERCHANT
        state.set_search("coffee")

        # Drill down (navigation happened)
        state.drill_down("Starbucks", 5)

        # Now Escape should navigate back, not clear search
        success, cursor, _ = state.go_back()

        assert success is True
        assert state.search_query == "coffee"  # Search still active
        assert state.view_mode == ViewMode.MERCHANT
        assert cursor == 5

    def test_escape_twice_after_drill_clears_search(self):
        """After navigating back to search level, second Escape clears search."""
        state = AppState()
        state.view_mode = ViewMode.MERCHANT
        state.set_search("coffee")
        state.drill_down("Starbucks", 5)

        # First Escape: navigate back
        state.go_back()
        assert state.search_query == "coffee"  # Still active

        # Second Escape: clear search (back at original depth)
        success, _, _ = state.go_back()
        assert success is True
        assert state.search_query == ""

    def test_escape_with_search_and_sub_grouping(self):
        """Scenario 3: Search then sub-group, Escape clears sub-grouping first."""
        state = AppState()
        state.view_mode = ViewMode.DETAIL
        state.selected_merchant = "Amazon"
        state.set_search("grocery")

        # Sub-group (navigation happened - depth same but state changed)
        state.sub_grouping_mode = ViewMode.CATEGORY

        # Escape should clear sub-grouping (search still active, navigation happened)
        success, _, _ = state.go_back()

        assert success is True
        assert state.sub_grouping_mode is None
        assert state.search_query == "grocery"  # Search persists
        assert state.selected_merchant == "Amazon"  # Still drilled down

    def test_escape_after_clearing_sub_grouping_clears_search(self):
        """After clearing sub-grouping, if back at search level, Escape clears search."""
        state = AppState()
        state.view_mode = ViewMode.DETAIL
        state.selected_merchant = "Amazon"
        state.set_search("grocery")
        state.sub_grouping_mode = ViewMode.CATEGORY

        # First Escape: clear sub-grouping
        state.go_back()
        assert state.sub_grouping_mode is None
        assert state.search_query == "grocery"

        # Now we're at same state as when search was set
        # Second Escape: should clear search
        success, _, _ = state.go_back()
        assert success is True
        assert state.search_query == ""
        assert state.selected_merchant == "Amazon"  # Still drilled down

    def test_search_persists_across_navigation(self):
        """Search should stay active when navigating away and back."""
        state = AppState()
        state.view_mode = ViewMode.MERCHANT
        state.set_search("coffee")
        state.drill_down("Starbucks", 5)

        # Navigate back
        state.go_back()

        # Search should still be active
        assert state.search_query == "coffee"

    def test_get_navigation_state_comparison(self):
        """Navigation state should change when sub-grouping changes."""
        state = AppState()
        state.view_mode = ViewMode.DETAIL
        state.selected_merchant = "Amazon"

        state1 = state.get_navigation_state()
        assert state1 == (1, None)

        state.sub_grouping_mode = ViewMode.CATEGORY
        state2 = state.get_navigation_state()
        assert state2 == (1, ViewMode.CATEGORY)
        assert state1 != state2


class TestMultiLevelDrillDownNavigation:
    """Test complex multi-level drill-down and go_back scenarios."""

    def test_drill_into_category_subgroup_by_account_drill_into_account_go_back(self):
        """
        Test the reported bug: Drill into category, sub-group by account,
        drill into account, then go back should restore sub-grouped view.

        Steps:
        1. Category view → drill into "Groceries"
        2. Press g → sub-group by Account
        3. Press Enter on account → drill into that account
        4. Press Escape → should go back to Groceries > (by Account), not Category view
        """
        state = AppState()

        # Step 1: Start in Category view, drill into Groceries
        state.view_mode = ViewMode.CATEGORY
        state.drill_down("Groceries", cursor_position=5, scroll_y=100.0)

        assert state.view_mode == ViewMode.DETAIL
        assert state.selected_category == "Groceries"
        assert state.sub_grouping_mode is None
        assert len(state.navigation_history) == 1

        # Step 2: Sub-group by Account
        state.sub_grouping_mode = ViewMode.ACCOUNT

        # Step 3: Drill into a specific account from sub-grouped view
        # This should save the current state (Groceries + sub_grouping_mode=ACCOUNT)
        state.drill_down("Chase Checking", cursor_position=3, scroll_y=50.0)

        assert state.view_mode == ViewMode.DETAIL
        assert state.selected_category == "Groceries"  # Still filtered to Groceries
        assert state.selected_account == "Chase Checking"  # Now also filtered to account
        assert state.sub_grouping_mode is None  # Cleared when drilling into account
        assert len(state.navigation_history) == 2  # Two drill-downs saved

        # Step 4: Go back - should restore Groceries > (by Account)
        success, cursor, scroll = state.go_back()

        assert success is True
        assert state.view_mode == ViewMode.DETAIL
        assert state.selected_category == "Groceries"  # Still Groceries
        assert state.selected_account is None  # Account filter cleared
        assert state.sub_grouping_mode == ViewMode.ACCOUNT  # Sub-grouping restored!
        assert cursor == 3  # Cursor restored
        assert scroll == 50.0  # Scroll restored
        assert len(state.navigation_history) == 1  # One drill-down remains

        # Step 5: Go back again - should clear sub-grouping, stay in Groceries detail
        success, cursor, scroll = state.go_back()

        assert success is True
        assert state.view_mode == ViewMode.DETAIL
        assert state.selected_category == "Groceries"  # Still in Groceries
        assert state.sub_grouping_mode is None  # Sub-grouping cleared
        assert len(state.navigation_history) == 1  # One entry remains

        # Step 6: Go back a third time - now should return to Category view
        success, cursor, scroll = state.go_back()

        assert success is True
        assert state.view_mode == ViewMode.CATEGORY
        assert state.selected_category is None  # Category filter cleared
        assert state.sub_grouping_mode is None
        assert cursor == 5  # Original cursor restored
        assert scroll == 100.0  # Original scroll restored
        assert len(state.navigation_history) == 0  # Back to root

    def test_drill_into_merchant_subgroup_by_category_drill_into_category(self):
        """Test multi-level navigation: Merchant → sub-group by Category → drill into category."""
        state = AppState()

        # Step 1: Drill into Amazon from Merchant view
        state.view_mode = ViewMode.MERCHANT
        state.drill_down("Amazon", cursor_position=10, scroll_y=200.0)

        assert state.selected_merchant == "Amazon"
        assert state.sub_grouping_mode is None

        # Step 2: Sub-group by Category
        state.sub_grouping_mode = ViewMode.CATEGORY

        # Step 3: Drill into Shopping category
        state.drill_down("Shopping", cursor_position=2, scroll_y=25.0)

        assert state.selected_merchant == "Amazon"
        assert state.selected_category == "Shopping"
        assert state.sub_grouping_mode is None  # Cleared on drill-down

        # Go back should restore Amazon > (by Category)
        success, cursor, scroll = state.go_back()

        assert success is True
        assert state.selected_merchant == "Amazon"
        assert state.selected_category is None
        assert state.sub_grouping_mode == ViewMode.CATEGORY
        assert cursor == 2
        assert scroll == 25.0

    def test_navigation_history_saves_all_selections(self):
        """Test that navigation history saves all drill-down selections."""
        state = AppState()

        # Drill down with multiple selections active
        state.view_mode = ViewMode.CATEGORY
        state.selected_merchant = "Amazon"  # Already filtered by merchant
        state.selected_category = None
        state.sub_grouping_mode = ViewMode.CATEGORY

        state.drill_down("Groceries", cursor_position=7, scroll_y=140.0)

        # Check saved state preserves everything
        saved_nav = state.navigation_history[-1]
        assert saved_nav.view_mode == ViewMode.CATEGORY
        assert saved_nav.selected_merchant == "Amazon"  # Merchant filter saved
        assert saved_nav.sub_grouping_mode == ViewMode.CATEGORY  # Sub-grouping saved

        # Now state should have both filters
        assert state.selected_merchant == "Amazon"  # Preserved
        assert state.selected_category == "Groceries"  # Added

    def test_go_back_restores_subgrouping_mode(self):
        """Test that go_back specifically restores sub_grouping_mode."""
        state = AppState()

        # Set up: Drilled into merchant with sub-grouping
        state.view_mode = ViewMode.DETAIL
        state.selected_merchant = "Starbucks"
        state.sub_grouping_mode = ViewMode.CATEGORY

        # Drill down into a category
        state.drill_down("Coffee Shops", cursor_position=1, scroll_y=10.0)

        # Verify sub-grouping was cleared
        assert state.sub_grouping_mode is None

        # Go back
        state.go_back()

        # Sub-grouping should be restored
        assert state.sub_grouping_mode == ViewMode.CATEGORY
        assert state.selected_merchant == "Starbucks"
        assert state.selected_category is None  # Cleared by go_back

    def test_three_level_drill_down_and_back(self):
        """Test three levels deep: Category → sub-group → drill → drill."""
        state = AppState()

        # Level 1: Drill into Travel from Group view
        state.view_mode = ViewMode.GROUP
        state.drill_down("Travel", cursor_position=2, scroll_y=20.0)

        # Level 2: Sub-group by Merchant
        state.sub_grouping_mode = ViewMode.MERCHANT

        # Level 3: Drill into United Airlines
        state.drill_down("United Airlines", cursor_position=0, scroll_y=0.0)

        assert len(state.navigation_history) == 2

        # First go_back: Restore Travel > (by Merchant)
        state.go_back()
        assert state.selected_group == "Travel"
        assert state.selected_merchant is None
        assert state.sub_grouping_mode == ViewMode.MERCHANT

        # Second go_back: Clear sub-grouping, stay in Travel detail
        state.go_back()
        assert state.view_mode == ViewMode.DETAIL
        assert state.selected_group == "Travel"
        assert state.sub_grouping_mode is None

        # Third go_back: Return to Group view
        state.go_back()
        assert state.view_mode == ViewMode.GROUP
        assert state.selected_group is None
        assert state.sub_grouping_mode is None
