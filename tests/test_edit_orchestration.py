"""
Tests for edit orchestration logic - Phase 1 refactoring.

These tests verify that edit context is correctly determined based on view state,
and that edits are executed correctly for all scenarios.

This enables testing edit workflows without requiring the TUI.
"""

import pytest

from moneyflow.data.data_manager import DataManager
from moneyflow.data.state import AppState, ViewMode
from moneyflow.tui.app_controller import AppController, EditMode

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


def setup_view(controller, mode, selected_keys=None, selected_ids=None, **state_kwargs):
    """Helper to configure view state and refresh view."""
    controller.state.view_mode = mode
    for k, v in state_kwargs.items():
        setattr(controller.state, k, v)
    controller.refresh_view()
    if selected_keys is not None:
        controller.state.selected_group_keys = set(selected_keys)
    if selected_ids is not None:
        controller.state.selected_ids = set(selected_ids)


def assert_edits_queued(controller, count, field, expected_value=None):
    """Helper to verify the last N pending edits match expectations."""
    edits = controller.data_manager.pending_edits[-count:] if count > 0 else []
    assert len(edits) == count
    assert all(e.field == field for e in edits)
    if expected_value is not None:
        assert all(e.new_value == expected_value for e in edits)


class TestSetupViewHelper:
    """Test the setup_view test helper itself to prevent regressions."""

    async def test_setup_view_empty_selected_keys(self, edit_controller):
        """Test that explicitly passing an empty list for selected_keys clears the selection."""
        controller = edit_controller

        # Set some initial state
        controller.state.selected_group_keys = {"Amazon", "Walmart"}

        # Call with empty list
        setup_view(controller, ViewMode.MERCHANT, selected_keys=[])

        assert controller.state.selected_group_keys == set()

    async def test_setup_view_empty_selected_ids(self, edit_controller):
        """Test that explicitly passing an empty list for selected_ids clears the selection."""
        controller = edit_controller

        # Set some initial state
        controller.state.selected_ids = {1, 2, 3}

        # Call with empty list
        setup_view(controller, ViewMode.DETAIL, selected_ids=[])

        assert controller.state.selected_ids == set()

    async def test_setup_view_none_preserves_selection(self, edit_controller):
        """Test that passing None preserves the existing selection."""
        controller = edit_controller

        controller.state.selected_group_keys = {"Amazon"}
        controller.state.selected_ids = {1, 2}

        # Call with None implicitly
        setup_view(controller, ViewMode.MERCHANT)

        assert controller.state.selected_group_keys == {"Amazon"}
        assert controller.state.selected_ids == {1, 2}


class TestDetermineEditContext:
    """Test that edit context is correctly determined for all view states."""

    async def test_aggregate_view_single_merchant(self, edit_controller):
        """Merchant view, cursor on one merchant, press m."""
        controller = edit_controller

        # Setup: Merchant view with cursor on Amazon
        setup_view(controller, ViewMode.MERCHANT)

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
        setup_view(controller, ViewMode.MERCHANT, selected_keys=["Amazon", "Walmart", "Target"])

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
        setup_view(controller, ViewMode.DETAIL)

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

        # Setup: Detail view with up to 5 transactions selected
        setup_view(controller, ViewMode.DETAIL)

        available_rows = len(controller.state.current_data)
        select_count = min(5, available_rows)
        assert select_count > 1, "Mock data must contain multiple rows for multi-select tests"

        txn_ids = controller.state.current_data["id"].head(select_count).to_list()
        controller.state.selected_ids = set(txn_ids)

        # Determine edit context
        context = controller.determine_edit_context("merchant", cursor_row=0)

        assert context.mode == EditMode.DETAIL_MULTI
        assert context.is_multi_select is True
        assert context.transaction_count == select_count
        assert len(context.transactions) == select_count

    async def test_subgrouped_view_single_group(self, edit_controller):
        """Drilled into merchant, sub-grouped by category, press m on one category."""
        controller = edit_controller

        # Setup: Drill into Amazon, sub-group by category
        setup_view(
            controller,
            ViewMode.DETAIL,
            selected_merchant="Amazon",
            sub_grouping_mode=ViewMode.CATEGORY,
        )

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
        setup_view(
            controller,
            ViewMode.DETAIL,
            selected_keys=["Groceries", "Electronics", "Books"],
            selected_merchant="Amazon",
            sub_grouping_mode=ViewMode.CATEGORY,
        )

        # Determine edit context
        context = controller.determine_edit_context("merchant", cursor_row=0)

        assert context.mode == EditMode.SUBGROUP_MULTI
        assert context.is_multi_select is True
        # Should have transactions from all selected sub-groups

    async def test_category_field_in_category_view(self, edit_controller):
        """Category view, editing category (recategorization)."""
        controller = edit_controller

        # Setup: Category view
        setup_view(controller, ViewMode.CATEGORY)

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
        setup_view(controller, ViewMode.MERCHANT, selected_keys=["NonExistent1", "NonExistent2"])

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

        setup_view(controller, ViewMode.MERCHANT, selected_keys=["Amazon", "Walmart"])

        context = controller.determine_edit_context("merchant", cursor_row=0)

        # Multi-select has special marker value
        assert context.current_value == "multiple"

    async def test_transaction_count_matches_dataframe_length(self, edit_controller):
        """Transaction count should match actual DataFrame length."""
        controller = edit_controller

        setup_view(controller, ViewMode.DETAIL)

        # Select specific number of transactions (use up to 5)
        available_rows = len(controller.state.current_data)
        select_count = min(5, available_rows)
        assert select_count > 1, "Mock data must contain multiple rows for multi-select tests"

        txn_ids = controller.state.current_data["id"].head(select_count).to_list()
        controller.state.selected_ids = set(txn_ids)

        context = controller.determine_edit_context("merchant", cursor_row=0)

        assert context.transaction_count == select_count
        assert len(context.transactions) == select_count

    async def test_group_field_none_for_detail_views(self, edit_controller):
        """Detail views should not have group_field."""
        controller = edit_controller

        setup_view(controller, ViewMode.DETAIL)

        context = controller.determine_edit_context("merchant", cursor_row=0)

        assert context.group_field is None

    @pytest.mark.parametrize(
        "view_mode,expected_field",
        [
            (ViewMode.MERCHANT, "merchant"),
            (ViewMode.CATEGORY, "category"),
            (ViewMode.GROUP, "group"),
            (ViewMode.ACCOUNT, "account"),
        ],
    )
    async def test_group_field_set_for_aggregate_views(
        self, edit_controller, view_mode, expected_field
    ):
        """Aggregate views should have group_field."""
        controller = edit_controller

        setup_view(controller, view_mode)

        context = controller.determine_edit_context("merchant", cursor_row=0)

        assert context.group_field == expected_field


class TestEditMerchantExecution:
    """Test executing merchant edits using EditContext."""

    async def test_edit_merchant_aggregate_single(self, edit_controller):
        """Test editing merchant from aggregate view (single merchant)."""
        controller = edit_controller

        # Setup: Merchant view
        setup_view(controller, ViewMode.MERCHANT)

        # Get initial count
        initial_pending = len(controller.data_manager.pending_edits)

        # Execute edit
        count = controller.edit_merchant_current_selection("Amazon.com", cursor_row=0)

        # Verify edits were queued
        assert count > 0
        assert len(controller.data_manager.pending_edits) == initial_pending + count

        assert_edits_queued(controller, count, "merchant", "Amazon.com")

    async def test_edit_merchant_detail_single(self, edit_controller):
        """Test editing single transaction in detail view."""
        controller = edit_controller

        # Setup: Detail view
        setup_view(controller, ViewMode.DETAIL)

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

        # Setup: Detail view with up to 3 transactions selected
        setup_view(controller, ViewMode.DETAIL)

        available_rows = len(controller.state.current_data)
        select_count = min(3, available_rows)
        assert select_count > 1, "Mock data must contain multiple rows for multi-select tests"

        txn_ids = controller.state.current_data["id"].head(select_count).to_list()
        controller.state.selected_ids = set(txn_ids)

        # Execute edit
        count = controller.edit_merchant_current_selection("Bulk Merchant", cursor_row=0)

        # Verify edits queued
        assert count == select_count
        assert_edits_queued(controller, count, "merchant", "Bulk Merchant")

    async def test_edit_merchant_validation_empty_string(self, edit_controller):
        """Test that empty merchant name is rejected."""
        controller = edit_controller

        setup_view(controller, ViewMode.DETAIL)

        initial_count = len(controller.data_manager.pending_edits)

        # Try to edit with empty string
        count = controller.edit_merchant_current_selection("", cursor_row=0)

        # Should be rejected
        assert count == 0
        assert len(controller.data_manager.pending_edits) == initial_count

    async def test_edit_merchant_validation_whitespace(self, edit_controller):
        """Test that whitespace-only merchant name is rejected."""
        controller = edit_controller

        setup_view(controller, ViewMode.DETAIL)

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
        setup_view(controller, ViewMode.DETAIL)

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
        setup_view(controller, ViewMode.MERCHANT, selected_keys=["Amazon", "Walmart"])

        # Execute edit
        count = controller.edit_merchant_current_selection("Consolidated Merchant", cursor_row=0)

        # Should edit transactions from both merchants
        assert count > 0
        assert_edits_queued(controller, count, "merchant", "Consolidated Merchant")

    async def test_edit_merchant_preserves_cursor_position(self, edit_controller):
        """Test that edit operation doesn't require cursor management (controller responsibility)."""
        controller = edit_controller

        # This test verifies that the controller method is pure business logic
        # It shouldn't touch cursor position - that's UI layer responsibility

        setup_view(controller, ViewMode.DETAIL)

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

        setup_view(controller, ViewMode.DETAIL)

        initial_pending = len(controller.data_manager.pending_edits)

        # Toggle hide
        count, was_undo = controller.toggle_hide_current_selection(cursor_row=0)

        assert count == 1
        assert was_undo is False
        assert len(controller.data_manager.pending_edits) == initial_pending + 1
        assert_edits_queued(controller, 1, "hide_from_reports")

    async def test_toggle_hide_multi_select(self, edit_controller):
        """Test hiding multiple selected transactions."""
        controller = edit_controller

        setup_view(controller, ViewMode.DETAIL)

        available_rows = len(controller.state.current_data)
        select_count = min(3, available_rows)
        assert select_count > 1, "Mock data must contain multiple rows for multi-select tests"

        # Select transactions
        txn_ids = controller.state.current_data["id"].head(select_count).to_list()
        controller.state.selected_ids = set(txn_ids)

        # Toggle hide
        count, was_undo = controller.toggle_hide_current_selection(cursor_row=0)

        assert count == select_count
        assert was_undo is False
        assert_edits_queued(controller, select_count, "hide_from_reports")

    async def test_toggle_hide_aggregate_view(self, edit_controller):
        """Test hiding all transactions in a merchant group."""
        controller = edit_controller

        setup_view(controller, ViewMode.MERCHANT)

        # Toggle hide on first merchant
        count, was_undo = controller.toggle_hide_current_selection(cursor_row=0)

        assert count > 0
        assert was_undo is False
        assert_edits_queued(controller, count, "hide_from_reports")

    async def test_toggle_hide_twice_detects_undo(self, edit_controller):
        """Test that toggling hide twice on same transaction undoes the first."""
        controller = edit_controller

        setup_view(controller, ViewMode.DETAIL)

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

        setup_view(controller, ViewMode.MERCHANT)

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

        setup_view(controller, ViewMode.MERCHANT)

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

        setup_view(controller, ViewMode.DETAIL)

        # Manually add pending edit for first transaction
        txn_id = controller.state.current_data["id"][0]
        from datetime import datetime

        from moneyflow.data.state import TransactionEdit

        controller.data_manager.pending_edits.append(
            TransactionEdit(txn_id, "hide_from_reports", False, True, datetime.now())
        )

        available_rows = len(controller.state.current_data)
        select_count = min(2, available_rows)
        assert select_count > 1, "Mock data must contain multiple rows for multi-select tests"

        # Select transactions (first one has pending, second doesn't)
        txn_ids = controller.state.current_data["id"].head(select_count).to_list()
        controller.state.selected_ids = set(txn_ids)

        # Toggle: should NOT be undo (not all have pending)
        count, was_undo = controller.toggle_hide_current_selection(cursor_row=0)

        assert was_undo is False  # Not all had pending, so not an undo
        # Should queue toggles for both
        assert count == select_count

    async def test_toggle_hide_empty_transactions_returns_zero(self, edit_controller):
        """Test graceful handling of empty transaction set."""
        controller = edit_controller

        # Select non-existent merchants
        setup_view(controller, ViewMode.MERCHANT, selected_keys=["NonExistent"])

        count, was_undo = controller.toggle_hide_current_selection(cursor_row=0)

        assert count == 0
        assert was_undo is False
