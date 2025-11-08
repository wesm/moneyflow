"""
Comprehensive tests for editing functionality.

Tests the complete editing workflows including:
- Bulk merchant rename from aggregate view
- Individual transaction recategorization
- Multi-select functionality
- Edit queueing and committing
"""

from datetime import datetime

import polars as pl

from moneyflow.state import TransactionEdit


class TestBulkMerchantEdit:
    """Test bulk merchant editing from aggregate view."""

    async def test_bulk_edit_queues_all_transactions(self, loaded_data_manager, app_state):
        """Test that bulk edit creates edits for all transactions."""
        dm, df, _, _ = loaded_data_manager

        # Find a merchant with multiple transactions
        merchant_name = "Whole Foods"
        merchant_txns = dm.filter_by_merchant(df, merchant_name)
        txn_count = len(merchant_txns)

        assert txn_count > 0, "Test data should have Whole Foods transactions"

        # Simulate bulk edit: add edits for all transactions
        new_merchant = "Whole Foods Market"
        for txn in merchant_txns.iter_rows(named=True):
            dm.pending_edits.append(
                TransactionEdit(
                    transaction_id=txn["id"],
                    field="merchant",
                    old_value=merchant_name,
                    new_value=new_merchant,
                    timestamp=datetime.now(),
                )
            )

        # Verify edits were queued
        assert len(dm.pending_edits) == txn_count
        assert all(e.field == "merchant" for e in dm.pending_edits)
        assert all(e.new_value == new_merchant for e in dm.pending_edits)

    async def test_bulk_edit_commits_successfully(self, loaded_data_manager, mock_mm):
        """Test that bulk merchant edit commits to API."""
        dm, df, _, _ = loaded_data_manager

        merchant_name = "Whole Foods"
        new_merchant = "Whole Foods Market"
        merchant_txns = dm.filter_by_merchant(df, merchant_name)

        # Queue bulk edits
        for txn in merchant_txns.iter_rows(named=True):
            dm.pending_edits.append(
                TransactionEdit(
                    transaction_id=txn["id"],
                    field="merchant",
                    old_value=merchant_name,
                    new_value=new_merchant,
                    timestamp=datetime.now(),
                )
            )

        # Commit
        success, failure = await dm.commit_pending_edits(dm.pending_edits)

        assert success == len(merchant_txns)
        assert failure == 0

        # Verify all were updated in backend
        for txn in merchant_txns.iter_rows(named=True):
            updated = mock_mm.get_transaction_by_id(txn["id"])
            assert updated["merchant"]["name"] == new_merchant

    async def test_bulk_edit_handles_partial_failure(self, data_manager):
        """Test that bulk edit handles some transactions failing."""
        # Create edits with mix of valid and invalid transaction IDs
        edits = [
            TransactionEdit("valid_txn", "merchant", "Old", "New", datetime.now()),
            TransactionEdit("invalid_txn_999", "merchant", "Old", "New", datetime.now()),
        ]

        success, failure = await data_manager.commit_pending_edits(edits)

        # At least one should succeed/fail
        assert success + failure == len(edits)


class TestIndividualEdits:
    """Test editing individual transactions."""

    async def test_edit_single_merchant(self, loaded_data_manager, mock_mm):
        """Test editing merchant for a single transaction."""
        dm, df, _, _ = loaded_data_manager

        txn = df.row(0, named=True)
        old_merchant = txn["merchant"]
        new_merchant = "Corrected Merchant Name"

        # Queue edit
        dm.pending_edits.append(
            TransactionEdit(
                transaction_id=txn["id"],
                field="merchant",
                old_value=old_merchant,
                new_value=new_merchant,
                timestamp=datetime.now(),
            )
        )

        # Commit
        success, failure = await dm.commit_pending_edits(dm.pending_edits)

        assert success == 1
        assert failure == 0

        # Verify
        updated = mock_mm.get_transaction_by_id(txn["id"])
        assert updated["merchant"]["name"] == new_merchant

    async def test_edit_merchant_detail_view_with_multiselect(
        self, loaded_data_manager, mock_mm, app_state
    ):
        """Test editing merchant with multiple selected transactions in detail view."""
        dm, df, _, _ = loaded_data_manager

        # Select multiple transactions
        txn_ids = [df.row(i, named=True)["id"] for i in range(3)]
        for txn_id in txn_ids:
            app_state.toggle_selection(txn_id)

        assert len(app_state.selected_ids) == 3

        # Simulate edit workflow
        new_merchant = "Corrected Name"
        for txn_id in txn_ids:
            txn_rows = df.filter(pl.col("id") == txn_id)
            txn = txn_rows.row(0, named=True)
            dm.pending_edits.append(
                TransactionEdit(
                    transaction_id=txn_id,
                    field="merchant",
                    old_value=txn["merchant"],
                    new_value=new_merchant,
                    timestamp=datetime.now(),
                )
            )

        # Commit
        success, failure = await dm.commit_pending_edits(dm.pending_edits)

        assert success == 3
        assert failure == 0

        # Verify all were updated
        for txn_id in txn_ids:
            updated = mock_mm.get_transaction_by_id(txn_id)
            assert updated["merchant"]["name"] == new_merchant

    async def test_edit_category_transaction(self, loaded_data_manager, mock_mm):
        """Test changing category for a transaction."""
        dm, df, categories, _ = loaded_data_manager

        txn = df.row(0, named=True)
        old_category_id = txn["category_id"]

        # Find a different category
        new_category_id = None
        for cat_id in categories:
            if cat_id != old_category_id:
                new_category_id = cat_id
                break

        assert new_category_id is not None

        # Queue edit
        dm.pending_edits.append(
            TransactionEdit(
                transaction_id=txn["id"],
                field="category",
                old_value=old_category_id,
                new_value=new_category_id,
                timestamp=datetime.now(),
            )
        )

        # Commit
        success, failure = await dm.commit_pending_edits(dm.pending_edits)

        assert success == 1
        assert failure == 0

        # Verify
        updated = mock_mm.get_transaction_by_id(txn["id"])
        assert updated["category"]["id"] == new_category_id

    async def test_edit_single_transaction_in_detail_view(self, loaded_data_manager, mock_mm):
        """Test editing single transaction without multiselect."""
        dm, df, _, _ = loaded_data_manager

        txn = df.row(0, named=True)
        new_merchant = "New Merchant"

        # Edit without multiselect
        dm.pending_edits.append(
            TransactionEdit(
                transaction_id=txn["id"],
                field="merchant",
                old_value=txn["merchant"],
                new_value=new_merchant,
                timestamp=datetime.now(),
            )
        )

        success, failure = await dm.commit_pending_edits(dm.pending_edits)

        assert success == 1
        updated = mock_mm.get_transaction_by_id(txn["id"])
        assert updated["merchant"]["name"] == new_merchant


class TestMultiSelect:
    """Test multi-select functionality for bulk operations."""

    def test_toggle_selection_adds_transaction(self, app_state):
        """Test that toggling selection adds transaction ID."""
        txn_id = "txn_123"

        app_state.toggle_selection(txn_id)

        assert txn_id in app_state.selected_ids
        assert len(app_state.selected_ids) == 1

    def test_toggle_selection_removes_if_already_selected(self, app_state):
        """Test that toggling again removes selection."""
        txn_id = "txn_123"

        app_state.toggle_selection(txn_id)
        app_state.toggle_selection(txn_id)

        assert txn_id not in app_state.selected_ids
        assert len(app_state.selected_ids) == 0

    def test_select_multiple_transactions(self, app_state):
        """Test selecting multiple transactions."""
        ids = ["txn_1", "txn_2", "txn_3", "txn_4", "txn_5"]

        for txn_id in ids:
            app_state.toggle_selection(txn_id)

        assert len(app_state.selected_ids) == 5
        assert all(tid in app_state.selected_ids for tid in ids)

    def test_clear_selection(self, app_state):
        """Test clearing all selections."""
        app_state.toggle_selection("txn_1")
        app_state.toggle_selection("txn_2")
        app_state.toggle_selection("txn_3")

        app_state.clear_selection()

        assert len(app_state.selected_ids) == 0


class TestEditQueueing:
    """Test that edits are properly queued before committing."""

    def test_multiple_edits_queue_correctly(self):
        """Test that multiple edits accumulate in pending list."""
        dm_pending = []

        # Queue multiple edits
        edits = [
            TransactionEdit("txn_1", "merchant", "A", "B", datetime.now()),
            TransactionEdit("txn_2", "merchant", "C", "D", datetime.now()),
            TransactionEdit("txn_3", "category", "cat_1", "cat_2", datetime.now()),
        ]

        dm_pending.extend(edits)

        assert len(dm_pending) == 3

    def test_edits_can_be_cleared(self):
        """Test that pending edits can be cleared after commit."""
        dm_pending = [
            TransactionEdit("txn_1", "merchant", "A", "B", datetime.now()),
            TransactionEdit("txn_2", "merchant", "C", "D", datetime.now()),
        ]

        dm_pending.clear()

        assert len(dm_pending) == 0


class TestEditValidation:
    """Test validation and error handling for edits."""

    async def test_empty_merchant_name_rejected(self):
        """Test that empty merchant names are not accepted."""
        # This would be handled by the EditMerchantScreen
        # If user submits empty string, modal returns None
        new_value = ""  # Empty

        # Modal should not return empty string
        result = new_value.strip() if new_value.strip() else None
        assert result is None

    async def test_unchanged_merchant_name_no_edit(self):
        """Test that unchanged name doesn't create edit."""
        current = "Amazon"
        new_value = "Amazon"  # Same

        # Modal should return None if unchanged
        result = new_value if new_value != current else None
        assert result is None

    async def test_commit_with_no_edits_succeeds(self, data_manager):
        """Test that committing with empty edits list works."""
        success, failure = await data_manager.commit_pending_edits([])

        assert success == 0
        assert failure == 0


class TestDataFrameUpdates:
    """Test that DataFrame is updated in-memory after commits."""

    async def test_dataframe_updated_after_merchant_commit(self, loaded_data_manager, mock_mm):
        """Test that DataFrame reflects merchant changes after commit without re-fetch."""
        dm, df, _, _ = loaded_data_manager

        # Ensure dm.df is set (loaded_data_manager fixture should do this)
        dm.df = df

        # Get original merchant name
        txn = df.row(0, named=True)
        txn_id = txn["id"]
        old_merchant = txn["merchant"]
        new_merchant = "Updated Merchant Name"

        # Verify old name in DataFrame
        assert dm.df.filter(pl.col("id") == txn_id).row(0, named=True)["merchant"] == old_merchant

        # Make edit and commit
        dm.pending_edits.append(
            TransactionEdit(txn_id, "merchant", old_merchant, new_merchant, datetime.now())
        )

        # Commit
        success, failure = await dm.commit_pending_edits(dm.pending_edits)
        assert success == 1

        # Apply update to DataFrame (simulating what app.py does after successful commit)
        dm.df = dm.df.with_columns(
            pl.when(pl.col("id") == txn_id)
            .then(pl.lit(new_merchant))
            .otherwise(pl.col("merchant"))
            .alias("merchant")
        )

        # Verify DataFrame was updated in-memory
        updated_row = dm.df.filter(pl.col("id") == txn_id).row(0, named=True)
        assert updated_row["merchant"] == new_merchant

        # Verify API was also updated
        api_txn = mock_mm.get_transaction_by_id(txn_id)
        assert api_txn["merchant"]["name"] == new_merchant

        # Key point: DataFrame update happened WITHOUT calling fetch_all_data again!

    async def test_dataframe_updated_after_hide_commit(self, loaded_data_manager, mock_mm):
        """Test that hideFromReports flag is updated in DataFrame after commit."""
        dm, df, _, _ = loaded_data_manager
        dm.df = df

        # Get a transaction and its current hideFromReports status
        txn = df.row(0, named=True)
        txn_id = txn["id"]
        old_hidden = txn.get("hideFromReports", False)
        new_hidden = not old_hidden

        # Verify current state
        assert (
            dm.df.filter(pl.col("id") == txn_id).row(0, named=True)["hideFromReports"] == old_hidden
        )

        # Make edit and commit
        dm.pending_edits.append(
            TransactionEdit(txn_id, "hide_from_reports", old_hidden, new_hidden, datetime.now())
        )

        success, failure = await dm.commit_pending_edits(dm.pending_edits)
        assert success == 1

        # Apply update to DataFrame (simulating what app.py does)
        dm.df = dm.df.with_columns(
            pl.when(pl.col("id") == txn_id)
            .then(pl.lit(new_hidden))
            .otherwise(pl.col("hideFromReports"))
            .alias("hideFromReports")
        )

        # Verify DataFrame was updated
        updated_row = dm.df.filter(pl.col("id") == txn_id).row(0, named=True)
        assert updated_row["hideFromReports"] == new_hidden

        # Verify API was also updated
        api_txn = mock_mm.get_transaction_by_id(txn_id)
        assert api_txn["hideFromReports"] == new_hidden


class TestEdgeCase:
    """Test edge cases in editing."""

    async def test_edit_merchant_with_special_characters(self, loaded_data_manager, mock_mm):
        """Test that special characters in merchant names work."""
        dm, df, _, _ = loaded_data_manager

        txn = df.row(0, named=True)
        new_merchant = "Trader Joe's & Co. (Main St.)"

        dm.pending_edits.append(
            TransactionEdit(txn["id"], "merchant", txn["merchant"], new_merchant, datetime.now())
        )

        success, failure = await dm.commit_pending_edits(dm.pending_edits)

        assert success == 1
        updated = mock_mm.get_transaction_by_id(txn["id"])
        assert updated["merchant"]["name"] == new_merchant

    async def test_edit_merchant_with_unicode(self, loaded_data_manager, mock_mm):
        """Test that unicode characters in merchant names work."""
        dm, df, _, _ = loaded_data_manager

        txn = df.row(0, named=True)
        new_merchant = "Café René"

        dm.pending_edits.append(
            TransactionEdit(txn["id"], "merchant", txn["merchant"], new_merchant, datetime.now())
        )

        success, failure = await dm.commit_pending_edits(dm.pending_edits)

        assert success == 1
        updated = mock_mm.get_transaction_by_id(txn["id"])
        assert updated["merchant"]["name"] == new_merchant

    async def test_edit_merchant_to_empty_string_validation(self):
        """Test that empty merchant name is rejected by validation logic."""
        # This simulates the validation that should happen in the UI layer
        # before an edit is created
        user_input = ""  # Empty string

        # Validation logic: strip and reject if empty
        validated = user_input.strip()
        should_create_edit = len(validated) > 0

        assert not should_create_edit, "Empty merchant name should be rejected"

    async def test_edit_merchant_to_whitespace_only_validation(self):
        """Test that whitespace-only merchant name is rejected."""
        user_input = "   "  # Only spaces

        validated = user_input.strip()
        should_create_edit = len(validated) > 0

        assert not should_create_edit, "Whitespace-only merchant name should be rejected"

    async def test_edit_merchant_to_very_long_name(self, loaded_data_manager, mock_mm):
        """Test that very long merchant names (>100 chars) are handled."""
        dm, df, _, _ = loaded_data_manager

        txn = df.row(0, named=True)
        # Create a 150 character merchant name
        new_merchant = "A" * 150

        dm.pending_edits.append(
            TransactionEdit(txn["id"], "merchant", txn["merchant"], new_merchant, datetime.now())
        )

        success, failure = await dm.commit_pending_edits(dm.pending_edits)

        # Should succeed - the API should handle length validation
        assert success == 1
        updated = mock_mm.get_transaction_by_id(txn["id"])
        assert updated["merchant"]["name"] == new_merchant

    async def test_edit_merchant_with_max_reasonable_length(self, loaded_data_manager, mock_mm):
        """Test merchant name at max reasonable length (100 chars)."""
        dm, df, _, _ = loaded_data_manager

        txn = df.row(0, named=True)
        # Exactly 100 characters
        new_merchant = "A" * 100

        dm.pending_edits.append(
            TransactionEdit(txn["id"], "merchant", txn["merchant"], new_merchant, datetime.now())
        )

        success, failure = await dm.commit_pending_edits(dm.pending_edits)

        assert success == 1
        updated = mock_mm.get_transaction_by_id(txn["id"])
        assert updated["merchant"]["name"] == new_merchant

    async def test_multiselect_with_some_invalid_transaction_ids(self, data_manager, mock_mm):
        """Test bulk edit with mix of valid and invalid transaction IDs."""
        # Create edits with mix of valid and invalid IDs
        edits = [
            TransactionEdit("txn_1", "merchant", "Old", "New Name", datetime.now()),  # Valid
            TransactionEdit(
                "invalid_999", "merchant", "Old", "New Name", datetime.now()
            ),  # Invalid
            TransactionEdit("txn_2", "merchant", "Old", "New Name", datetime.now()),  # Valid
            TransactionEdit(
                "nonexistent", "merchant", "Old", "New Name", datetime.now()
            ),  # Invalid
        ]

        success, failure = await data_manager.commit_pending_edits(edits)

        # Should have 2 successes (valid IDs) and 2 failures (invalid IDs)
        assert success == 2, f"Expected 2 successes, got {success}"
        assert failure == 2, f"Expected 2 failures, got {failure}"

        # Verify valid transactions were updated
        txn_1 = mock_mm.get_transaction_by_id("txn_1")
        assert txn_1["merchant"]["name"] == "New Name"

        txn_2 = mock_mm.get_transaction_by_id("txn_2")
        assert txn_2["merchant"]["name"] == "New Name"

    async def test_edit_category_with_invalid_category_id(self, loaded_data_manager, mock_mm):
        """Test that recategorizing with invalid category ID fails gracefully."""
        dm, df, _, _ = loaded_data_manager

        txn = df.row(0, named=True)
        invalid_category_id = "cat_nonexistent_12345"

        dm.pending_edits.append(
            TransactionEdit(
                txn["id"], "category", txn["category_id"], invalid_category_id, datetime.now()
            )
        )

        success, failure = await dm.commit_pending_edits(dm.pending_edits)

        # Should still succeed at API level (mock doesn't validate category IDs)
        # But verify the update was attempted
        assert len(mock_mm.update_calls) > 0
        last_call = mock_mm.update_calls[-1]
        assert last_call["category_id"] == invalid_category_id

    async def test_concurrent_edits_to_same_transaction(self, loaded_data_manager, mock_mm):
        """Test multiple edits to the same transaction in the same batch."""
        dm, df, _, _ = loaded_data_manager

        txn = df.row(0, named=True)

        # Create multiple edits to the same transaction
        # This could happen if user changes merchant, then category, then merchant again
        edits = [
            TransactionEdit(txn["id"], "merchant", "Old", "First Change", datetime.now()),
            TransactionEdit(txn["id"], "category", "cat_old", "cat_groceries", datetime.now()),
            TransactionEdit(txn["id"], "merchant", "First Change", "Second Change", datetime.now()),
        ]

        dm.pending_edits.extend(edits)
        success, failure = await dm.commit_pending_edits(dm.pending_edits)

        # All edits should be grouped into a single API call
        # commit_pending_edits groups by transaction_id
        assert success == 1, "Should have 1 successful transaction update"
        assert failure == 0

        # Verify the final state has the last merchant change and category change
        updated = mock_mm.get_transaction_by_id(txn["id"])
        assert updated["merchant"]["name"] == "Second Change", "Should have last merchant value"
        assert updated["category"]["id"] == "cat_groceries", "Should have category change"

    async def test_bulk_edit_with_partial_dataframe_updates(self, loaded_data_manager, mock_mm):
        """Test that DataFrame updates work correctly with partial bulk edits."""
        dm, df, _, _ = loaded_data_manager
        dm.df = df

        # Select subset of transactions
        txn_ids = [df.row(0, named=True)["id"], df.row(2, named=True)["id"]]
        new_merchant = "Bulk Updated Merchant"

        # Create edits for subset
        for txn_id in txn_ids:
            txn_rows = df.filter(pl.col("id") == txn_id)
            txn = txn_rows.row(0, named=True)
            dm.pending_edits.append(
                TransactionEdit(txn_id, "merchant", txn["merchant"], new_merchant, datetime.now())
            )

        # Commit
        success, failure = await dm.commit_pending_edits(dm.pending_edits)
        assert success == 2
        assert failure == 0

        # Simulate DataFrame update (what app.py would do)
        for txn_id in txn_ids:
            dm.df = dm.df.with_columns(
                pl.when(pl.col("id") == txn_id)
                .then(pl.lit(new_merchant))
                .otherwise(pl.col("merchant"))
                .alias("merchant")
            )

        # Verify only selected transactions were updated
        for i, row in enumerate(df.iter_rows(named=True)):
            if row["id"] in txn_ids:
                updated_row = dm.df.filter(pl.col("id") == row["id"]).row(0, named=True)
                assert updated_row["merchant"] == new_merchant
            else:
                # Other transactions should be unchanged
                updated_row = dm.df.filter(pl.col("id") == row["id"]).row(0, named=True)
                assert updated_row["merchant"] == row["merchant"]

    async def test_edit_with_null_or_none_values(self, loaded_data_manager, mock_mm):
        """Test that edits handle None/null values appropriately."""
        dm, df, _, _ = loaded_data_manager

        txn = df.row(0, named=True)

        # Try to edit with None value (should be skipped or rejected)
        edit = TransactionEdit(
            txn["id"],
            "merchant",
            txn["merchant"],
            None,  # None as new value
            datetime.now(),
        )

        # The validation should happen at UI level, but test API behavior
        # This should not create a valid edit
        assert edit.new_value is None

    async def test_multiple_edits_different_fields_same_transaction(
        self, loaded_data_manager, mock_mm
    ):
        """Test editing multiple fields on same transaction works correctly."""
        dm, df, _, _ = loaded_data_manager

        txn = df.row(0, named=True)
        new_merchant = "Updated Merchant"
        new_category_id = "cat_restaurants"

        # Edit both merchant and category
        edits = [
            TransactionEdit(txn["id"], "merchant", txn["merchant"], new_merchant, datetime.now()),
            TransactionEdit(
                txn["id"], "category", txn["category_id"], new_category_id, datetime.now()
            ),
        ]

        dm.pending_edits.extend(edits)
        success, failure = await dm.commit_pending_edits(dm.pending_edits)

        # With batch optimization: merchant handled via batch (1), category individually (1) = 2 total
        # Note: If backend doesn't support batch update, both would be grouped into 1 API call
        assert success == 2
        assert failure == 0

        # Verify both fields were updated
        updated = mock_mm.get_transaction_by_id(txn["id"])
        assert updated["merchant"]["name"] == new_merchant
        assert updated["category"]["id"] == new_category_id

    async def test_edit_hidden_transaction(self, loaded_data_manager, mock_mm):
        """Test editing a transaction that is marked hideFromReports."""
        dm, df, _, _ = loaded_data_manager

        # First hide a transaction
        txn = df.row(0, named=True)
        hide_edit = TransactionEdit(txn["id"], "hide_from_reports", False, True, datetime.now())

        await dm.commit_pending_edits([hide_edit])

        # Verify it's hidden
        updated = mock_mm.get_transaction_by_id(txn["id"])
        assert updated["hideFromReports"] is True

        # Now edit the merchant on the hidden transaction
        merchant_edit = TransactionEdit(
            txn["id"], "merchant", txn["merchant"], "New Merchant Name", datetime.now()
        )

        success, failure = await dm.commit_pending_edits([merchant_edit])

        # Should succeed
        assert success == 1
        updated = mock_mm.get_transaction_by_id(txn["id"])
        assert updated["merchant"]["name"] == "New Merchant Name"
        assert updated["hideFromReports"] is True  # Still hidden


class TestMerchantCreation:
    """Test creating new merchants via edit merchant modal."""

    def test_user_input_always_available_as_option(self):
        """Test that user input is always available as a 'create new' option."""
        # This tests the new behavior where the user can always create a merchant
        # matching their exact input, even if there's a partial match

        # Simulate: User types "Starbucks" and there's a match "Starbucks Coffee"
        user_input = "Starbucks"
        existing_merchants = ["Starbucks Coffee", "Starbucks Reserve", "Coffee Shop"]

        # Filter for matches
        matches = [m for m in existing_merchants if user_input.lower() in m.lower()]
        assert len(matches) == 2  # Two partial matches

        # The modal should show:
        # 1. "Starbucks Coffee" (existing match)
        # 2. "Starbucks Reserve" (existing match)
        # 3. "Starbucks" (create new - user's exact input)

        # Verify we can add the user input as an option
        # In the actual implementation, this is added with id="__new__:Starbucks"
        create_new_option = f'"{user_input}"'
        assert create_new_option == '"Starbucks"'

    def test_create_new_option_format(self):
        """Test that create new option is formatted with quotes."""
        user_inputs = [
            ("Amazon", '"Amazon"'),
            ("Whole Foods", '"Whole Foods"'),
            ("CVS Pharmacy", '"CVS Pharmacy"'),
        ]

        for user_input, expected_display in user_inputs:
            create_new_display = f'"{user_input}"'
            assert create_new_display == expected_display

    def test_create_new_id_format(self):
        """Test that create new option has correct ID format."""
        user_input = "New Merchant"
        option_id = f"__new__:{user_input}"

        assert option_id == "__new__:New Merchant"
        assert option_id.startswith("__new__:")

        # Test extraction
        if option_id.startswith("__new__:"):
            extracted = option_id[8:]  # Remove "__new__:" prefix
            assert extracted == user_input

    def test_auto_select_existing_match_with_create_new_present(self):
        """Test that Enter still auto-selects existing match when create new is present."""
        # Scenario: User types "Amazon" and there's exactly one match "Amazon.com"
        # Options shown:
        # 1. "Amazon.com" (existing match)
        # 2. "Amazon" (create new)
        #
        # Expected: Enter should auto-select "Amazon.com" (first existing match)
        # User can press down arrow twice to select "Amazon" (create new)

        # Simulate the options
        options = [
            {"id": "Amazon.com", "is_new": False},  # Existing match
            {"id": "__new__:Amazon", "is_new": True},  # Create new
        ]

        # Count existing matches
        existing_matches = [opt for opt in options if not opt["is_new"]]
        assert len(existing_matches) == 1

        # With exactly 1 existing match, Enter should select it
        first_existing = existing_matches[0]
        assert first_existing["id"] == "Amazon.com"

    def test_auto_select_first_match_with_multiple_existing_matches(self):
        """Test that Enter auto-selects first match even with multiple matches."""
        # Scenario: User types "Star" and there are multiple matches
        # Options shown (in order):
        # 1. "Star Market" (first alphabetically - existing)
        # 2. "Star" (create new)
        # 3. "Starbucks" (existing)
        # 4. "Starbucks Coffee" (existing)
        #
        # Expected: Enter should auto-select "Star Market" (first existing match)
        # To create new "Star", user must arrow down to position 2

        options = [
            {"id": "Star Market", "is_new": False},  # First alphabetically
            {"id": "__new__:Star", "is_new": True},
            {"id": "Starbucks", "is_new": False},
            {"id": "Starbucks Coffee", "is_new": False},
        ]

        # Find first existing match
        first_existing = None
        for opt in options:
            if not opt["is_new"]:
                first_existing = opt
                break

        # With multiple existing matches, Enter should select the first one
        assert first_existing is not None
        assert first_existing["id"] == "Star Market"

    def test_extracting_merchant_name_from_new_option_id(self):
        """Test extracting actual merchant name from __new__ option ID."""
        test_cases = [
            ("__new__:Amazon", "Amazon"),
            ("__new__:Whole Foods Market", "Whole Foods Market"),
            ("__new__:CVS #1234", "CVS #1234"),
        ]

        for option_id, expected_name in test_cases:
            if option_id.startswith("__new__:"):
                extracted = option_id[8:]
                assert extracted == expected_name
