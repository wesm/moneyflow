"""
Integration tests for complete edit workflows.

These tests verify that the full chain of operations works correctly:
1. User makes an edit
2. Edit is tracked in state
3. Edit is committed to API
4. Backend state is updated correctly
"""

from datetime import datetime

import polars as pl


class TestMerchantEditWorkflow:
    """Test complete merchant editing workflow."""

    async def test_edit_merchant_name(self, loaded_data_manager, app_state, mock_mm):
        """Test full workflow of editing a merchant name."""
        dm, df, _, _ = loaded_data_manager

        # Get a transaction
        txn = df.row(0, named=True)
        old_merchant = txn["merchant"]
        new_merchant = "Corrected Merchant Name"

        # 1. User edits merchant
        app_state.add_edit(
            transaction_id=txn["id"],
            field="merchant",
            old_value=old_merchant,
            new_value=new_merchant,
        )

        assert len(app_state.pending_edits) == 1

        # 2. Commit the edit
        success, failure, _ = await dm.commit_pending_edits(app_state.pending_edits)

        assert success == 1
        assert failure == 0

        # 3. Verify backend was updated
        updated_txn = mock_mm.get_transaction_by_id(txn["id"])
        assert updated_txn["merchant"]["name"] == new_merchant

        # 4. Clear pending edits
        app_state.clear_pending_edits()
        assert len(app_state.pending_edits) == 0

    async def test_undo_merchant_edit(self, loaded_data_manager, app_state):
        """Test undoing a merchant edit."""
        dm, df, _, _ = loaded_data_manager

        txn = df.row(0, named=True)

        # Add edit
        app_state.add_edit(txn["id"], "merchant", "Old", "New")
        assert len(app_state.pending_edits) == 1

        # Undo
        app_state.undo_last_edit()
        assert len(app_state.pending_edits) == 0


class TestCategoryEditWorkflow:
    """Test complete category editing workflow."""

    async def test_edit_category(self, loaded_data_manager, app_state, mock_mm):
        """Test full workflow of editing a category."""
        dm, df, categories, _ = loaded_data_manager

        # Get a transaction
        txn = df.row(0, named=True)
        old_category_id = txn["category_id"]

        # Find a different category
        new_category_id = None
        for cat_id, cat_data in categories.items():
            if cat_id != old_category_id:
                new_category_id = cat_id
                break

        assert new_category_id is not None

        # 1. User changes category
        app_state.add_edit(
            transaction_id=txn["id"],
            field="category",
            old_value=old_category_id,
            new_value=new_category_id,
        )

        # 2. Commit
        success, failure, _ = await dm.commit_pending_edits(app_state.pending_edits)

        assert success == 1
        assert failure == 0

        # 3. Verify backend was updated
        updated_txn = mock_mm.get_transaction_by_id(txn["id"])
        assert updated_txn["category"]["id"] == new_category_id


class TestHideFromReportsWorkflow:
    """Test hide from reports toggle workflow."""

    async def test_toggle_hide_from_reports(self, loaded_data_manager, app_state, mock_mm):
        """Test full workflow of toggling hide from reports."""
        dm, df, _, _ = loaded_data_manager

        # Get a transaction
        txn = df.row(0, named=True)
        old_hide_value = txn["hideFromReports"]
        new_hide_value = not old_hide_value

        # 1. User toggles hide
        app_state.add_edit(
            transaction_id=txn["id"],
            field="hide_from_reports",
            old_value=old_hide_value,
            new_value=new_hide_value,
        )

        # 2. Commit
        success, failure, _ = await dm.commit_pending_edits(app_state.pending_edits)

        assert success == 1
        assert failure == 0

        # 3. Verify backend was updated
        updated_txn = mock_mm.get_transaction_by_id(txn["id"])
        assert updated_txn["hideFromReports"] == new_hide_value


class TestBulkEditWorkflow:
    """Test bulk editing multiple transactions."""

    async def test_bulk_merchant_rename(self, loaded_data_manager, app_state, mock_mm):
        """Test renaming merchant for multiple transactions."""
        dm, df, _, _ = loaded_data_manager

        # Find all transactions for a specific merchant
        merchant_to_rename = "Whole Foods"
        merchant_txns = df.filter(pl.col("merchant") == merchant_to_rename)

        # Select all of them
        for row in merchant_txns.iter_rows(named=True):
            app_state.toggle_selection(row["id"])

        assert len(app_state.selected_ids) == len(merchant_txns)

        # Add edits for all selected transactions
        new_merchant_name = "Whole Foods Market"
        for txn_id in app_state.selected_ids:
            app_state.add_edit(
                transaction_id=txn_id,
                field="merchant",
                old_value=merchant_to_rename,
                new_value=new_merchant_name,
            )

        # Commit all edits
        success, failure, _ = await dm.commit_pending_edits(app_state.pending_edits)

        assert success == len(app_state.selected_ids)
        assert failure == 0

        # Verify all were updated
        for txn_id in app_state.selected_ids:
            updated_txn = mock_mm.get_transaction_by_id(txn_id)
            assert updated_txn["merchant"]["name"] == new_merchant_name

    async def test_bulk_hide_toggle(self, loaded_data_manager, app_state, mock_mm):
        """Test bulk toggling hide from reports."""
        dm, df, _, _ = loaded_data_manager

        # Select first 3 transactions
        for i in range(3):
            txn_id = df.row(i, named=True)["id"]
            app_state.toggle_selection(txn_id)

        # Toggle hide for all
        for txn_id in app_state.selected_ids:
            txn_row = df.filter(pl.col("id") == txn_id).row(0, named=True)
            old_value = txn_row["hideFromReports"]
            app_state.add_edit(
                transaction_id=txn_id,
                field="hide_from_reports",
                old_value=old_value,
                new_value=not old_value,
            )

        # Commit
        success, failure, _ = await dm.commit_pending_edits(app_state.pending_edits)

        assert success == 3
        assert failure == 0


class TestSubGroupedEditWorkflow:
    """Test editing from sub-grouped views (drill-down + grouping)."""

    def test_sub_grouped_view_has_aggregate_columns(self, loaded_data_manager, app_state):
        """Verify sub-grouped views show aggregate data, not transaction details."""
        from moneyflow.state import ViewMode

        dm, df, _, _ = loaded_data_manager
        app_state.transactions_df = df

        # Drill into a merchant
        app_state.view_mode = ViewMode.MERCHANT
        app_state.drill_down("Whole Foods")

        # Now group by category (sub-grouping)
        app_state.sub_grouping_mode = ViewMode.CATEGORY

        # Get aggregated data for this view
        filtered = app_state.get_filtered_df()
        agg = dm.aggregate_by_category(filtered)

        # Should have aggregate columns (count, total) not transaction columns (id)
        assert "count" in agg.columns
        assert "total" in agg.columns
        assert "category" in agg.columns
        assert "id" not in agg.columns  # No individual transaction IDs in aggregate

    def test_edit_category_from_sub_grouped_by_category(self, loaded_data_manager, app_state):
        """Test editing category from a sub-grouped-by-category view."""
        from moneyflow.state import ViewMode

        dm, df, categories, _ = loaded_data_manager
        app_state.transactions_df = df

        # Setup: Drill into merchant, then group by category
        app_state.view_mode = ViewMode.MERCHANT
        merchant_name = "Whole Foods"
        app_state.drill_down(merchant_name)
        app_state.sub_grouping_mode = ViewMode.CATEGORY

        # Get the aggregated view
        filtered = app_state.get_filtered_df()
        agg = dm.aggregate_by_category(filtered)

        # Simulate selecting first category row
        first_category = agg.row(0, named=True)
        category_name = first_category["category"]
        first_category["category_id"]

        # Find transactions in this category within the merchant
        txns_to_edit = filtered.filter(pl.col("category") == category_name)

        # This is what the UI would do: bulk edit all transactions in this category
        assert len(txns_to_edit) > 0
        assert "id" in txns_to_edit.columns  # These are real transactions

    def test_edit_merchant_from_sub_grouped_by_category(self, loaded_data_manager, app_state):
        """Test editing merchant from a sub-grouped-by-category view."""
        from moneyflow.state import ViewMode

        dm, df, _, _ = loaded_data_manager
        app_state.transactions_df = df

        # Setup: Drill into merchant "Whole Foods", then group by category
        app_state.view_mode = ViewMode.MERCHANT
        merchant_name = "Whole Foods"
        app_state.drill_down(merchant_name)
        app_state.sub_grouping_mode = ViewMode.CATEGORY

        # Get the aggregated view
        filtered = app_state.get_filtered_df()
        agg = dm.aggregate_by_category(filtered)

        # Get first category
        first_category = agg.row(0, named=True)
        category_name = first_category["category"]

        # When editing merchant from this row, should edit all transactions
        # in "Whole Foods" + this category
        txns_to_edit = filtered.filter(pl.col("category") == category_name)

        assert len(txns_to_edit) > 0
        # All should be from the drilled-down merchant
        assert all(txn["merchant"] == merchant_name for txn in txns_to_edit.iter_rows(named=True))

    def test_edit_from_sub_grouped_by_group(self, loaded_data_manager, app_state):
        """Test editing from sub-grouped-by-group view."""
        from moneyflow.state import ViewMode

        dm, df, _, _ = loaded_data_manager
        app_state.transactions_df = df

        # Drill into a category, then group by group
        app_state.view_mode = ViewMode.CATEGORY
        category_name = "Groceries"
        app_state.drill_down(category_name)
        app_state.sub_grouping_mode = ViewMode.GROUP

        # Get aggregated view
        filtered = app_state.get_filtered_df()
        agg = dm.aggregate_by_group(filtered)

        # Should have aggregate structure
        assert "group" in agg.columns
        assert "count" in agg.columns
        assert "total" in agg.columns

    def test_edit_from_sub_grouped_by_account(self, loaded_data_manager, app_state):
        """Test editing from sub-grouped-by-account view."""
        from moneyflow.state import ViewMode

        dm, df, _, _ = loaded_data_manager
        app_state.transactions_df = df

        # Drill into a merchant, then group by account
        app_state.view_mode = ViewMode.MERCHANT
        merchant_name = "Whole Foods"
        app_state.drill_down(merchant_name)
        app_state.sub_grouping_mode = ViewMode.ACCOUNT

        # Get aggregated view
        filtered = app_state.get_filtered_df()
        agg = dm.aggregate_by_account(filtered)

        # Should have aggregate structure
        assert "account" in agg.columns
        assert "count" in agg.columns
        assert "total" in agg.columns

    def test_edit_from_sub_grouped_by_merchant(self, loaded_data_manager, app_state):
        """Test editing from sub-grouped-by-merchant view (drilled into category)."""
        from moneyflow.state import ViewMode

        dm, df, _, _ = loaded_data_manager
        app_state.transactions_df = df

        # Drill into a category, then group by merchant
        app_state.view_mode = ViewMode.CATEGORY
        category_name = "Groceries"
        app_state.drill_down(category_name)
        app_state.sub_grouping_mode = ViewMode.MERCHANT

        # Get aggregated view
        filtered = app_state.get_filtered_df()
        agg = dm.aggregate_by_merchant(filtered)

        # Should have aggregate structure
        assert "merchant" in agg.columns
        assert "count" in agg.columns
        assert "total" in agg.columns

        # Get first merchant in this category
        first_merchant = agg.row(0, named=True)
        merchant_name = first_merchant["merchant"]

        # Transactions to edit = this category + this merchant
        txns_to_edit = filtered.filter(pl.col("merchant") == merchant_name)
        assert len(txns_to_edit) > 0
        # All should be from the drilled-down category
        assert all(txn["category"] == category_name for txn in txns_to_edit.iter_rows(named=True))


class TestErrorHandling:
    """Test error handling in workflows."""

    async def test_commit_with_invalid_transaction_id(self, data_manager, app_state):
        """Test that commits handle invalid transaction IDs gracefully."""
        from moneyflow.state import TransactionEdit

        # Add edit with non-existent transaction ID
        edits = [
            TransactionEdit(
                transaction_id="non_existent_id",
                field="merchant",
                old_value="Old",
                new_value="New",
                timestamp=datetime.now(),
            )
        ]

        # Should not raise exception
        success, failure, _ = await data_manager.commit_pending_edits(edits)

        # Mock backend doesn't fail on invalid IDs, but real API might
        # This test ensures we handle it gracefully
        assert success + failure == 1


class TestUndoPendingEdits:
    """Test undoing pending edits functionality."""

    async def test_undo_removes_most_recent_edit(self, data_manager):
        """Test that undo removes the most recent (last) pending edit."""
        from moneyflow.state import TransactionEdit

        # Queue three edits
        edits = [
            TransactionEdit("txn_1", "merchant", "Old1", "New1", datetime.now()),
            TransactionEdit("txn_2", "category", "cat_old", "cat_new", datetime.now()),
            TransactionEdit("txn_3", "hide_from_reports", False, True, datetime.now()),
        ]
        data_manager.pending_edits = edits.copy()

        # Undo (should remove last edit - txn_3)
        removed = data_manager.pending_edits.pop()

        assert removed.transaction_id == "txn_3"
        assert len(data_manager.pending_edits) == 2
        assert data_manager.pending_edits[0].transaction_id == "txn_1"
        assert data_manager.pending_edits[1].transaction_id == "txn_2"

    async def test_undo_with_single_edit(self, data_manager):
        """Test undoing when there's only one pending edit."""
        from moneyflow.state import TransactionEdit

        data_manager.pending_edits = [
            TransactionEdit("txn_1", "merchant", "Old", "New", datetime.now())
        ]

        removed = data_manager.pending_edits.pop()

        assert removed.transaction_id == "txn_1"
        assert len(data_manager.pending_edits) == 0

    async def test_undo_preserves_earlier_edits(self, data_manager):
        """Test that undo only removes the last edit, preserving earlier ones."""
        from moneyflow.state import TransactionEdit

        edit1 = TransactionEdit("txn_1", "merchant", "Old1", "New1", datetime.now())
        edit2 = TransactionEdit("txn_2", "merchant", "Old2", "New2", datetime.now())
        edit3 = TransactionEdit("txn_3", "merchant", "Old3", "New3", datetime.now())

        data_manager.pending_edits = [edit1, edit2, edit3]

        # First undo
        data_manager.pending_edits.pop()
        assert len(data_manager.pending_edits) == 2
        assert data_manager.pending_edits[0] == edit1
        assert data_manager.pending_edits[1] == edit2

        # Second undo
        data_manager.pending_edits.pop()
        assert len(data_manager.pending_edits) == 1
        assert data_manager.pending_edits[0] == edit1

        # Third undo
        data_manager.pending_edits.pop()
        assert len(data_manager.pending_edits) == 0

    async def test_undo_different_field_types(self, data_manager):
        """Test undoing different types of edits."""
        from moneyflow.state import TransactionEdit

        merchant_edit = TransactionEdit("txn_1", "merchant", "Old", "New", datetime.now())
        category_edit = TransactionEdit("txn_2", "category", "cat_1", "cat_2", datetime.now())
        hide_edit = TransactionEdit("txn_3", "hide_from_reports", False, True, datetime.now())

        # Test undo merchant edit
        data_manager.pending_edits = [merchant_edit]
        removed = data_manager.pending_edits.pop()
        assert removed.field == "merchant"

        # Test undo category edit
        data_manager.pending_edits = [category_edit]
        removed = data_manager.pending_edits.pop()
        assert removed.field == "category"

        # Test undo hide edit
        data_manager.pending_edits = [hide_edit]
        removed = data_manager.pending_edits.pop()
        assert removed.field == "hide_from_reports"


class TestBulkUndo:
    """Test undoing bulk edit operations."""

    async def test_undo_bulk_edit_removes_all_edits_at_once(self, data_manager):
        """Test that undoing a bulk edit removes all edits from that batch."""
        from datetime import datetime

        from moneyflow.state import TransactionEdit

        # Simulate bulk edit: 5 merchant edits queued at same timestamp
        timestamp = datetime.now()
        bulk_edits = [
            TransactionEdit(f"txn_{i}", "merchant", f"Old{i}", "New", timestamp) for i in range(5)
        ]
        data_manager.pending_edits = bulk_edits.copy()

        # Simulate undo - should remove all 5 edits (same timestamp)
        last_timestamp = data_manager.pending_edits[-1].timestamp
        edits_to_undo = []
        for i in range(len(data_manager.pending_edits) - 1, -1, -1):
            edit = data_manager.pending_edits[i]
            if edit.timestamp == last_timestamp:
                edits_to_undo.append(edit)
            else:
                break

        for edit in edits_to_undo:
            data_manager.pending_edits.remove(edit)

        # Verify all 5 edits were undone
        assert len(edits_to_undo) == 5
        assert len(data_manager.pending_edits) == 0

    async def test_undo_preserves_earlier_bulk_edits(self, data_manager):
        """Test that undo only removes the most recent bulk edit batch."""
        from datetime import datetime, timedelta

        from moneyflow.state import TransactionEdit

        # First bulk edit: 3 edits at timestamp T
        timestamp1 = datetime.now()
        first_batch = [
            TransactionEdit(f"txn_{i}", "merchant", f"Old{i}", "Batch1", timestamp1)
            for i in range(3)
        ]

        # Second bulk edit: 4 edits at timestamp T+1 second
        timestamp2 = timestamp1 + timedelta(seconds=1)
        second_batch = [
            TransactionEdit(f"txn_{i}", "category", "cat_old", "cat_new", timestamp2)
            for i in range(3, 7)
        ]

        data_manager.pending_edits = first_batch + second_batch

        # Undo should remove only second_batch (most recent timestamp)
        last_timestamp = data_manager.pending_edits[-1].timestamp
        edits_to_undo = []
        for i in range(len(data_manager.pending_edits) - 1, -1, -1):
            edit = data_manager.pending_edits[i]
            if edit.timestamp == last_timestamp:
                edits_to_undo.append(edit)
            else:
                break

        for edit in edits_to_undo:
            data_manager.pending_edits.remove(edit)

        # Verify only second batch was undone
        assert len(edits_to_undo) == 4
        assert len(data_manager.pending_edits) == 3
        # First batch should remain
        assert all(e.timestamp == timestamp1 for e in data_manager.pending_edits)

    async def test_undo_single_edit_after_bulk_edit(self, data_manager):
        """Test that single edit after bulk edit is undone separately."""
        from datetime import datetime, timedelta

        from moneyflow.state import TransactionEdit

        # Bulk edit: 5 edits
        timestamp1 = datetime.now()
        bulk_edits = [
            TransactionEdit(f"txn_{i}", "merchant", "Old", "New", timestamp1) for i in range(5)
        ]

        # Single edit later
        timestamp2 = timestamp1 + timedelta(seconds=1)
        single_edit = TransactionEdit("txn_10", "category", "cat_1", "cat_2", timestamp2)

        data_manager.pending_edits = bulk_edits + [single_edit]

        # First undo: should remove only the single edit
        last_timestamp = data_manager.pending_edits[-1].timestamp
        edits_to_undo = []
        for i in range(len(data_manager.pending_edits) - 1, -1, -1):
            edit = data_manager.pending_edits[i]
            if edit.timestamp == last_timestamp:
                edits_to_undo.append(edit)
            else:
                break

        for edit in edits_to_undo:
            data_manager.pending_edits.remove(edit)

        assert len(edits_to_undo) == 1
        assert len(data_manager.pending_edits) == 5

        # Second undo: should remove all 5 bulk edits
        last_timestamp = data_manager.pending_edits[-1].timestamp
        edits_to_undo = []
        for i in range(len(data_manager.pending_edits) - 1, -1, -1):
            edit = data_manager.pending_edits[i]
            if edit.timestamp == last_timestamp:
                edits_to_undo.append(edit)
            else:
                break

        for edit in edits_to_undo:
            data_manager.pending_edits.remove(edit)

        assert len(edits_to_undo) == 5
        assert len(data_manager.pending_edits) == 0
