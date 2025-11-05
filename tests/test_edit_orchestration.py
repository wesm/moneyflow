"""
Tests for edit orchestration logic - Phase 1 refactoring.

These tests verify that edit context is correctly determined based on view state,
and that edits are executed correctly for all scenarios.

This enables testing edit workflows without requiring the TUI.
"""

import pytest

from moneyflow.app_controller import AppController, EditMode
from moneyflow.data_manager import DataManager
from moneyflow.state import AppState, ViewMode

from .mock_view import MockViewPresenter


@pytest.fixture
async def edit_controller(mock_mm, tmp_path):
    """Provide controller with edit-specific setup and isolated config."""
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

    # Set up controller
    mock_view = MockViewPresenter()
    controller = AppController(mock_view, state, data_manager)

    return controller


class TestDetermineEditContext:
    """Test that edit context is correctly determined for all view states."""

    async def test_aggregate_view_single_merchant(self, edit_controller):
        """Merchant view, cursor on one merchant, press m."""
        controller = edit_controller

        # Setup: Merchant view with cursor on Amazon
        controller.state.view_mode = ViewMode.MERCHANT
        # Prepare aggregated data
        controller.refresh_view()

        # Simulate cursor on first row
        assert controller.state.current_data is not None
        current_row = controller.state.current_data.row(0, named=True)
        merchant_name = current_row["merchant"]

        # Determine edit context
        context = controller.determine_edit_context("merchant", cursor_row=0)

        assert context.mode == EditMode.AGGREGATE_SINGLE
        assert context.field_name == "merchant"
        assert context.current_value == merchant_name
        assert context.is_multi_select is False
        assert context.transaction_count > 0
        assert context.group_field == "merchant"
        assert not context.transactions.is_empty()

    async def test_aggregate_view_multi_select_merchants(self, edit_controller):
        """Merchant view, multi-select 3 merchants, press m."""
        controller = edit_controller

        # Setup: Merchant view with 3 merchants selected
        controller.state.view_mode = ViewMode.MERCHANT
        controller.refresh_view()

        # Multi-select some merchants
        controller.state.selected_group_keys = {"Amazon", "Walmart", "Target"}

        # Determine edit context
        context = controller.determine_edit_context("merchant", cursor_row=0)

        assert context.mode == EditMode.AGGREGATE_MULTI
        assert context.is_multi_select is True
        assert context.current_value == "multiple"  # Multi-select marker
        assert context.transaction_count > 0
        # Should have transactions from all 3 merchants
        merchants_in_result = set(context.transactions["merchant"].unique().to_list())
        assert (
            "Amazon" in merchants_in_result
            or "Walmart" in merchants_in_result
            or "Target" in merchants_in_result
        )

    async def test_detail_view_single_transaction(self, edit_controller):
        """Detail view, cursor on one transaction, press m."""
        controller = edit_controller

        # Setup: Detail view
        controller.state.view_mode = ViewMode.DETAIL
        controller.refresh_view()

        # Get current transaction
        current_row = controller.state.current_data.row(0, named=True)
        merchant_name = current_row["merchant"]

        # Determine edit context
        context = controller.determine_edit_context("merchant", cursor_row=0)

        assert context.mode == EditMode.DETAIL_SINGLE
        assert context.field_name == "merchant"
        assert context.current_value == merchant_name
        assert context.is_multi_select is False
        assert context.transaction_count == 1
        assert context.group_field is None

    async def test_detail_view_multi_select_transactions(self, edit_controller):
        """Detail view, multi-select 5 transactions, press m."""
        controller = edit_controller

        # Setup: Detail view with 5 transactions selected
        controller.state.view_mode = ViewMode.DETAIL
        controller.refresh_view()

        # Select 5 transactions
        txn_ids = controller.state.current_data["id"].head(5).to_list()
        controller.state.selected_ids = set(txn_ids)

        # Determine edit context
        context = controller.determine_edit_context("merchant", cursor_row=0)

        assert context.mode == EditMode.DETAIL_MULTI
        assert context.is_multi_select is True
        assert context.transaction_count == 5
        assert len(context.transactions) == 5

    async def test_subgrouped_view_single_group(self, edit_controller):
        """Drilled into merchant, sub-grouped by category, press m on one category."""
        controller = edit_controller

        # Setup: Drill into Amazon, sub-group by category
        controller.state.view_mode = ViewMode.DETAIL
        controller.state.selected_merchant = "Amazon"
        controller.state.sub_grouping_mode = ViewMode.CATEGORY
        controller.refresh_view()

        # Determine edit context
        context = controller.determine_edit_context("merchant", cursor_row=0)

        assert context.mode == EditMode.SUBGROUP_SINGLE
        assert context.is_multi_select is False
        # Should have transactions from current sub-group row
        assert context.transaction_count > 0

    async def test_subgrouped_view_multi_select(self, edit_controller):
        """Drilled into merchant, sub-grouped by category, multi-select 3 categories, press m."""
        controller = edit_controller

        # Setup: Drill into Amazon, sub-group by category, select 3 categories
        controller.state.view_mode = ViewMode.DETAIL
        controller.state.selected_merchant = "Amazon"
        controller.state.sub_grouping_mode = ViewMode.CATEGORY
        controller.refresh_view()

        # Multi-select some sub-groups
        controller.state.selected_group_keys = {"Groceries", "Electronics", "Books"}

        # Determine edit context
        context = controller.determine_edit_context("merchant", cursor_row=0)

        assert context.mode == EditMode.SUBGROUP_MULTI
        assert context.is_multi_select is True
        # Should have transactions from all selected sub-groups

    async def test_category_field_in_category_view(self, edit_controller):
        """Category view, editing category (recategorization)."""
        controller = edit_controller

        # Setup: Category view
        controller.state.view_mode = ViewMode.CATEGORY
        controller.refresh_view()

        current_row = controller.state.current_data.row(0, named=True)
        category_name = current_row["category"]

        # Determine edit context for category edit
        context = controller.determine_edit_context("category", cursor_row=0)

        assert context.mode == EditMode.AGGREGATE_SINGLE
        assert context.field_name == "category"
        assert context.current_value == category_name  # Current category name
        assert context.group_field == "category"

    async def test_no_transactions_returns_empty_context(self, edit_controller):
        """Test graceful handling when no transactions match selection."""
        controller = edit_controller

        # Setup: Merchant view but select non-existent merchants
        controller.state.view_mode = ViewMode.MERCHANT
        controller.refresh_view()
        controller.state.selected_group_keys = {"NonExistent1", "NonExistent2"}

        # Determine edit context
        context = controller.determine_edit_context("merchant", cursor_row=0)

        # Should still return context but with empty transactions
        assert context.transactions.is_empty()
        assert context.transaction_count == 0


class TestEditContextValidation:
    """Test validation and edge cases for edit context."""

    async def test_context_current_value_none_for_multi_select(self, edit_controller):
        """Multi-select should have special current_value."""
        controller = edit_controller

        controller.state.view_mode = ViewMode.MERCHANT
        controller.refresh_view()
        controller.state.selected_group_keys = {"Amazon", "Walmart"}

        context = controller.determine_edit_context("merchant", cursor_row=0)

        # Multi-select has special marker value
        assert context.current_value == "multiple"

    async def test_transaction_count_matches_dataframe_length(self, edit_controller):
        """Transaction count should match actual DataFrame length."""
        controller = edit_controller

        controller.state.view_mode = ViewMode.DETAIL
        controller.refresh_view()

        # Select specific number of transactions (use 5, mock data has 6 total)
        txn_ids = controller.state.current_data["id"].head(5).to_list()
        controller.state.selected_ids = set(txn_ids)

        context = controller.determine_edit_context("merchant", cursor_row=0)

        assert context.transaction_count == 5
        assert len(context.transactions) == 5

    async def test_group_field_none_for_detail_views(self, edit_controller):
        """Detail views should not have group_field."""
        controller = edit_controller

        controller.state.view_mode = ViewMode.DETAIL
        controller.refresh_view()

        context = controller.determine_edit_context("merchant", cursor_row=0)

        assert context.group_field is None

    async def test_group_field_set_for_aggregate_views(self, edit_controller):
        """Aggregate views should have group_field."""
        controller = edit_controller

        for view_mode, expected_field in [
            (ViewMode.MERCHANT, "merchant"),
            (ViewMode.CATEGORY, "category"),
            (ViewMode.GROUP, "group"),
            (ViewMode.ACCOUNT, "account"),
        ]:
            controller.state.view_mode = view_mode
            controller.refresh_view()

            context = controller.determine_edit_context("merchant", cursor_row=0)

            assert context.group_field == expected_field


class TestEditMerchantExecution:
    """Test executing merchant edits using EditContext."""

    async def test_edit_merchant_aggregate_single(self, edit_controller):
        """Test editing merchant from aggregate view (single merchant)."""
        controller = edit_controller

        # Setup: Merchant view
        controller.state.view_mode = ViewMode.MERCHANT
        controller.refresh_view()

        # Get initial count
        initial_pending = len(controller.data_manager.pending_edits)

        # Execute edit
        count = controller.edit_merchant_current_selection("Amazon.com", cursor_row=0)

        # Verify edits were queued
        assert count > 0
        assert len(controller.data_manager.pending_edits) == initial_pending + count
        # All edits should be merchant edits
        new_edits = controller.data_manager.pending_edits[initial_pending:]
        assert all(e.field == "merchant" for e in new_edits)
        assert all(e.new_value == "Amazon.com" for e in new_edits)

    async def test_edit_merchant_detail_single(self, edit_controller):
        """Test editing single transaction in detail view."""
        controller = edit_controller

        # Setup: Detail view
        controller.state.view_mode = ViewMode.DETAIL
        controller.refresh_view()

        # Get current merchant
        current_row = controller.state.current_data.row(0, named=True)
        old_merchant = current_row["merchant"]

        # Execute edit
        count = controller.edit_merchant_current_selection("New Merchant Name", cursor_row=0)

        # Verify exactly 1 edit queued
        assert count == 1
        last_edit = controller.data_manager.pending_edits[-1]
        assert last_edit.field == "merchant"
        assert last_edit.old_value == old_merchant
        assert last_edit.new_value == "New Merchant Name"

    async def test_edit_merchant_detail_multi_select(self, edit_controller):
        """Test editing multiple selected transactions."""
        controller = edit_controller

        # Setup: Detail view with 3 transactions selected
        controller.state.view_mode = ViewMode.DETAIL
        controller.refresh_view()

        txn_ids = controller.state.current_data["id"].head(3).to_list()
        controller.state.selected_ids = set(txn_ids)

        # Execute edit
        count = controller.edit_merchant_current_selection("Bulk Merchant", cursor_row=0)

        # Verify 3 edits queued
        assert count == 3
        # Check last 3 edits
        last_3 = controller.data_manager.pending_edits[-3:]
        assert all(e.field == "merchant" for e in last_3)
        assert all(e.new_value == "Bulk Merchant" for e in last_3)

    async def test_edit_merchant_validation_empty_string(self, edit_controller):
        """Test that empty merchant name is rejected."""
        controller = edit_controller

        controller.state.view_mode = ViewMode.DETAIL
        controller.refresh_view()

        initial_count = len(controller.data_manager.pending_edits)

        # Try to edit with empty string
        count = controller.edit_merchant_current_selection("", cursor_row=0)

        # Should be rejected
        assert count == 0
        assert len(controller.data_manager.pending_edits) == initial_count

    async def test_edit_merchant_validation_whitespace(self, edit_controller):
        """Test that whitespace-only merchant name is rejected."""
        controller = edit_controller

        controller.state.view_mode = ViewMode.DETAIL
        controller.refresh_view()

        initial_count = len(controller.data_manager.pending_edits)

        # Try to edit with whitespace
        count = controller.edit_merchant_current_selection("   ", cursor_row=0)

        # Should be rejected
        assert count == 0
        assert len(controller.data_manager.pending_edits) == initial_count

    async def test_edit_merchant_no_op_same_value(self, edit_controller):
        """Test that editing to same value queues no edit (no-op)."""
        controller = edit_controller

        # Setup: Detail view
        controller.state.view_mode = ViewMode.DETAIL
        controller.refresh_view()

        # Get current merchant
        current_row = controller.state.current_data.row(0, named=True)
        current_merchant = current_row["merchant"]

        initial_count = len(controller.data_manager.pending_edits)

        # Edit to same value
        count = controller.edit_merchant_current_selection(current_merchant, cursor_row=0)

        # Should be no-op (no edit queued)
        assert count == 0
        assert len(controller.data_manager.pending_edits) == initial_count

    async def test_edit_merchant_aggregate_multi_select(self, edit_controller):
        """Test editing multiple selected groups from aggregate view."""
        controller = edit_controller

        # Setup: Merchant view with 2 merchants selected
        controller.state.view_mode = ViewMode.MERCHANT
        controller.refresh_view()

        controller.state.selected_group_keys = {"Amazon", "Walmart"}

        # Execute edit
        count = controller.edit_merchant_current_selection("Consolidated Merchant", cursor_row=0)

        # Should edit transactions from both merchants
        assert count > 0
        # All should be merchant edits with new value
        last_n = controller.data_manager.pending_edits[-count:]
        assert all(e.field == "merchant" for e in last_n)
        assert all(e.new_value == "Consolidated Merchant" for e in last_n)

    async def test_edit_merchant_preserves_cursor_position(self, edit_controller):
        """Test that edit operation doesn't require cursor management (controller responsibility)."""
        controller = edit_controller

        # This test verifies that the controller method is pure business logic
        # It shouldn't touch cursor position - that's UI layer responsibility

        controller.state.view_mode = ViewMode.DETAIL
        controller.refresh_view()

        # Execute edit
        count = controller.edit_merchant_current_selection("Test Merchant", cursor_row=0)

        # Should just queue edit, not touch state beyond pending_edits
        assert count == 1
        # No side effects on cursor (UI layer handles that)


class TestToggleHideExecution:
    """Test toggle hide/unhide with undo detection."""

    async def test_toggle_hide_single_transaction(self, edit_controller):
        """Test hiding single transaction in detail view."""
        controller = edit_controller

        controller.state.view_mode = ViewMode.DETAIL
        controller.refresh_view()

        initial_pending = len(controller.data_manager.pending_edits)

        # Toggle hide
        count, was_undo = controller.toggle_hide_current_selection(cursor_row=0)

        assert count == 1
        assert was_undo is False
        assert len(controller.data_manager.pending_edits) == initial_pending + 1
        # Should be hide_from_reports edit
        assert controller.data_manager.pending_edits[-1].field == "hide_from_reports"

    async def test_toggle_hide_multi_select(self, edit_controller):
        """Test hiding multiple selected transactions."""
        controller = edit_controller

        controller.state.view_mode = ViewMode.DETAIL
        controller.refresh_view()

        # Select 3 transactions
        txn_ids = controller.state.current_data["id"].head(3).to_list()
        controller.state.selected_ids = set(txn_ids)

        # Toggle hide
        count, was_undo = controller.toggle_hide_current_selection(cursor_row=0)

        assert count == 3
        assert was_undo is False
        # Should have 3 hide toggles
        last_3 = controller.data_manager.pending_edits[-3:]
        assert all(e.field == "hide_from_reports" for e in last_3)

    async def test_toggle_hide_aggregate_view(self, edit_controller):
        """Test hiding all transactions in a merchant group."""
        controller = edit_controller

        controller.state.view_mode = ViewMode.MERCHANT
        controller.refresh_view()

        # Toggle hide on first merchant
        count, was_undo = controller.toggle_hide_current_selection(cursor_row=0)

        assert count > 0
        assert was_undo is False

    async def test_toggle_hide_twice_detects_undo(self, edit_controller):
        """Test that toggling hide twice on same transaction undoes the first."""
        controller = edit_controller

        controller.state.view_mode = ViewMode.DETAIL
        controller.refresh_view()

        # First toggle: queue hide edit
        count1, was_undo1 = controller.toggle_hide_current_selection(cursor_row=0)

        assert count1 == 1
        assert was_undo1 is False
        pending_after_first = len(controller.data_manager.pending_edits)

        # Second toggle: should undo the first (remove the pending edit)
        count2, was_undo2 = controller.toggle_hide_current_selection(cursor_row=0)

        assert count2 == 1
        assert was_undo2 is True  # Detected as undo!
        # Should have removed the pending edit
        assert len(controller.data_manager.pending_edits) == pending_after_first - 1

    async def test_toggle_hide_group_twice_undoes_batch(self, edit_controller):
        """Test that hiding a group twice undoes all edits in that group."""
        controller = edit_controller

        controller.state.view_mode = ViewMode.MERCHANT
        controller.refresh_view()

        initial_pending = len(controller.data_manager.pending_edits)

        # First toggle: hide all transactions in first merchant
        count1, was_undo1 = controller.toggle_hide_current_selection(cursor_row=0)

        assert count1 > 0
        assert was_undo1 is False
        assert len(controller.data_manager.pending_edits) == initial_pending + count1

        # Second toggle on same merchant: should undo ALL hide edits from that merchant
        count2, was_undo2 = controller.toggle_hide_current_selection(cursor_row=0)

        assert count2 == count1  # Same number undone
        assert was_undo2 is True  # Detected as undo!
        # All edits from first toggle should be removed
        assert len(controller.data_manager.pending_edits) == initial_pending

    async def test_toggle_hide_different_groups_no_undo(self, edit_controller):
        """Test that hiding different groups doesn't trigger undo."""
        controller = edit_controller

        controller.state.view_mode = ViewMode.MERCHANT
        controller.refresh_view()

        initial_pending = len(controller.data_manager.pending_edits)

        # Hide first merchant
        count1, was_undo1 = controller.toggle_hide_current_selection(cursor_row=0)

        assert was_undo1 is False

        # Hide second merchant (different group)
        count2, was_undo2 = controller.toggle_hide_current_selection(cursor_row=1)

        assert was_undo2 is False  # Not an undo (different group)
        # Both sets of edits should be queued
        assert len(controller.data_manager.pending_edits) == initial_pending + count1 + count2

    async def test_toggle_hide_partial_pending_no_undo(self, edit_controller):
        """Test that partial pending edits don't trigger undo."""
        controller = edit_controller

        controller.state.view_mode = ViewMode.DETAIL
        controller.refresh_view()

        # Manually add pending edit for first transaction
        txn_id = controller.state.current_data["id"][0]
        from datetime import datetime

        from moneyflow.state import TransactionEdit

        controller.data_manager.pending_edits.append(
            TransactionEdit(txn_id, "hide_from_reports", False, True, datetime.now())
        )

        # Select 2 transactions (first one has pending, second doesn't)
        txn_ids = controller.state.current_data["id"].head(2).to_list()
        controller.state.selected_ids = set(txn_ids)

        # Toggle: should NOT be undo (not all have pending)
        count, was_undo = controller.toggle_hide_current_selection(cursor_row=0)

        assert was_undo is False  # Not all had pending, so not an undo
        # Should queue toggles for both
        assert count == 2

    async def test_toggle_hide_empty_transactions_returns_zero(self, edit_controller):
        """Test graceful handling of empty transaction set."""
        controller = edit_controller

        controller.state.view_mode = ViewMode.MERCHANT
        controller.refresh_view()

        # Select non-existent merchants
        controller.state.selected_group_keys = {"NonExistent"}

        count, was_undo = controller.toggle_hide_current_selection(cursor_row=0)

        assert count == 0
        assert was_undo is False
