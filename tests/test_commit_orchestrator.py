"""
Tests for commit_orchestrator module.

CRITICAL: This module tests the DataFrame update logic that runs
after successful commits. Any bugs here could corrupt the UI state.

These tests ensure edits are applied correctly to DataFrames.
"""

from datetime import date, datetime

import polars as pl
import pytest

from moneyflow.commit_orchestrator import CommitOrchestrator
from moneyflow.state import TransactionEdit


class TestApplyMerchantEdit:
    """Tests for apply_merchant_edit (single transaction by ID)."""

    def test_updates_single_transaction(self):
        """Should update only the specified transaction by ID."""
        df = pl.DataFrame(
            {
                "id": ["txn1", "txn2", "txn3"],
                "merchant": ["Amazon", "Amazon", "Target"],
            }
        )

        updated = CommitOrchestrator.apply_merchant_edit(df, "txn1", "Whole Foods")

        # Only txn1 should be updated
        assert updated.filter(pl.col("id") == "txn1")["merchant"][0] == "Whole Foods"
        # Others unchanged (even txn2 with same merchant)
        assert updated.filter(pl.col("id") == "txn2")["merchant"][0] == "Amazon"
        assert updated.filter(pl.col("id") == "txn3")["merchant"][0] == "Target"

    def test_handles_empty_dataframe(self):
        """Should handle empty DataFrame without error."""
        df = pl.DataFrame({"id": [], "merchant": []}, schema={"id": pl.Utf8, "merchant": pl.Utf8})

        updated = CommitOrchestrator.apply_merchant_edit(df, "txn1", "NewMerchant")

        assert updated.is_empty()

    def test_nonexistent_transaction_id(self):
        """Should return unchanged DataFrame for nonexistent transaction ID."""
        df = pl.DataFrame({"id": ["txn1"], "merchant": ["Amazon"]})

        updated = CommitOrchestrator.apply_merchant_edit(df, "txn999", "NewMerchant")

        assert updated["merchant"][0] == "Amazon"

    def test_multiple_applications(self):
        """Should handle applying edits to same transaction multiple times."""
        df = pl.DataFrame({"id": ["txn1"], "merchant": ["Amazon"]})

        updated1 = CommitOrchestrator.apply_merchant_edit(df, "txn1", "Whole Foods")
        updated2 = CommitOrchestrator.apply_merchant_edit(updated1, "txn1", "Trader Joes")

        assert updated2["merchant"][0] == "Trader Joes"

    def test_preserves_other_columns(self):
        """Should not modify other columns."""
        df = pl.DataFrame(
            {
                "id": ["txn1"],
                "merchant": ["Amazon"],
                "amount": [-99.99],
                "date": [date(2025, 1, 15)],
                "category": ["Shopping"],
            }
        )

        updated = CommitOrchestrator.apply_merchant_edit(df, "txn1", "Target")

        assert updated["amount"][0] == -99.99
        assert updated["category"][0] == "Shopping"


class TestApplyBulkMerchantEdit:
    """Tests for apply_bulk_merchant_edit (all transactions by merchant name)."""

    def test_updates_all_matching_merchants(self):
        """Should update ALL transactions with the old merchant name."""
        df = pl.DataFrame(
            {
                "id": ["txn1", "txn2", "txn3", "txn4"],
                "merchant": ["Amazon", "Amazon", "Amazon", "Target"],
            }
        )

        updated = CommitOrchestrator.apply_bulk_merchant_edit(df, "Amazon", "Whole Foods")

        # All Amazon transactions should be updated
        assert updated.filter(pl.col("id") == "txn1")["merchant"][0] == "Whole Foods"
        assert updated.filter(pl.col("id") == "txn2")["merchant"][0] == "Whole Foods"
        assert updated.filter(pl.col("id") == "txn3")["merchant"][0] == "Whole Foods"
        # Target unchanged
        assert updated.filter(pl.col("id") == "txn4")["merchant"][0] == "Target"

    def test_nonexistent_merchant(self):
        """Should return unchanged DataFrame for nonexistent merchant name."""
        df = pl.DataFrame({"id": ["txn1"], "merchant": ["Amazon"]})

        updated = CommitOrchestrator.apply_bulk_merchant_edit(df, "NonExistent", "NewMerchant")

        assert updated["merchant"][0] == "Amazon"

    def test_chained_bulk_renames(self):
        """Should handle chained bulk renames."""
        df = pl.DataFrame({"id": ["txn1", "txn2"], "merchant": ["Amazon", "Amazon"]})

        updated1 = CommitOrchestrator.apply_bulk_merchant_edit(df, "Amazon", "Whole Foods")
        updated2 = CommitOrchestrator.apply_bulk_merchant_edit(
            updated1, "Whole Foods", "Trader Joes"
        )

        assert updated2["merchant"][0] == "Trader Joes"
        assert updated2["merchant"][1] == "Trader Joes"


class TestApplyCategoryEdit:
    """Tests for apply_category_edit method."""

    def test_updates_category_id_and_name(self):
        """Should update both category_id and category name."""
        df = pl.DataFrame(
            {
                "id": ["txn1", "txn2"],
                "category_id": ["cat1", "cat2"],
                "category": ["Old Category", "Dining"],
                "group": ["Retail", "Food"],
            }
        )

        def mock_apply_groups(df):
            # Just return unchanged for this test
            return df

        updated = CommitOrchestrator.apply_category_edit(
            df, "txn1", "cat3", "New Category", mock_apply_groups
        )

        txn1 = updated.filter(pl.col("id") == "txn1")
        assert txn1["category_id"][0] == "cat3"
        assert txn1["category"][0] == "New Category"
        # txn2 unchanged
        txn2 = updated.filter(pl.col("id") == "txn2")
        assert txn2["category_id"][0] == "cat2"
        assert txn2["category"][0] == "Dining"

    def test_calls_apply_groups_func(self):
        """Should call apply_groups_func to update groups."""
        df = pl.DataFrame(
            {"id": ["txn1"], "category_id": ["cat1"], "category": ["Old"], "group": ["OldGroup"]}
        )

        def mock_apply_groups(df):
            # Simulate updating groups based on category
            return df.with_columns(pl.lit("NewGroup").alias("group"))

        updated = CommitOrchestrator.apply_category_edit(
            df, "txn1", "cat2", "New", mock_apply_groups
        )

        assert updated["group"][0] == "NewGroup"

    def test_preserves_other_columns(self):
        """Should not modify unrelated columns."""
        df = pl.DataFrame(
            {
                "id": ["txn1"],
                "category_id": ["cat1"],
                "category": ["Old"],
                "group": ["OldGroup"],
                "merchant": ["Amazon"],
                "amount": [-99.99],
            }
        )

        def mock_apply_groups(df):
            return df

        updated = CommitOrchestrator.apply_category_edit(
            df, "txn1", "cat2", "New", mock_apply_groups
        )

        assert updated["merchant"][0] == "Amazon"
        assert updated["amount"][0] == -99.99


class TestApplyHideFromReportsEdit:
    """Tests for apply_hide_from_reports_edit method."""

    def test_sets_hide_to_true(self):
        """Should set hideFromReports to True."""
        df = pl.DataFrame({"id": ["txn1", "txn2"], "hideFromReports": [False, False]})

        updated = CommitOrchestrator.apply_hide_from_reports_edit(df, "txn1", True)

        assert updated.filter(pl.col("id") == "txn1")["hideFromReports"][0] is True
        assert updated.filter(pl.col("id") == "txn2")["hideFromReports"][0] is False

    def test_sets_hide_to_false(self):
        """Should set hideFromReports to False."""
        df = pl.DataFrame({"id": ["txn1"], "hideFromReports": [True]})

        updated = CommitOrchestrator.apply_hide_from_reports_edit(df, "txn1", False)

        assert updated["hideFromReports"][0] is False

    def test_toggles_correctly(self):
        """Should handle toggle workflow."""
        df = pl.DataFrame({"id": ["txn1"], "hideFromReports": [False]})

        # Toggle to True
        updated1 = CommitOrchestrator.apply_hide_from_reports_edit(df, "txn1", True)
        assert updated1["hideFromReports"][0] is True

        # Toggle back to False
        updated2 = CommitOrchestrator.apply_hide_from_reports_edit(updated1, "txn1", False)
        assert updated2["hideFromReports"][0] is False


class TestApplyEditToDataframe:
    """Tests for apply_edit_to_dataframe dispatcher."""

    def test_dispatches_merchant_edit(self):
        """Should call apply_merchant_edit for merchant field."""
        df = pl.DataFrame(
            {
                "id": ["txn1"],
                "merchant": ["Amazon"],
                "category_id": ["cat1"],
                "category": ["Shopping"],
                "group": ["Retail"],
                "hideFromReports": [False],
            }
        )

        edit = TransactionEdit(
            transaction_id="txn1",
            field="merchant",
            old_value="Amazon",
            new_value="Target",
            timestamp=datetime.now(),
        )

        def mock_apply_groups(df):
            return df

        updated = CommitOrchestrator.apply_edit_to_dataframe(df, edit, {}, mock_apply_groups)

        assert updated["merchant"][0] == "Target"

    def test_dispatches_category_edit(self):
        """Should call apply_category_edit for category field."""
        df = pl.DataFrame(
            {
                "id": ["txn1"],
                "merchant": ["Amazon"],
                "category_id": ["cat1"],
                "category": ["Shopping"],
                "group": ["Retail"],
                "hideFromReports": [False],
            }
        )

        categories = {"cat2": {"name": "Groceries", "group": "Food"}}

        edit = TransactionEdit(
            transaction_id="txn1",
            field="category",
            old_value="cat1",
            new_value="cat2",
            timestamp=datetime.now(),
        )

        def mock_apply_groups(df):
            return df

        updated = CommitOrchestrator.apply_edit_to_dataframe(
            df, edit, categories, mock_apply_groups
        )

        assert updated["category_id"][0] == "cat2"
        assert updated["category"][0] == "Groceries"

    def test_dispatches_hide_edit(self):
        """Should call apply_hide_from_reports_edit for hide field."""
        df = pl.DataFrame(
            {
                "id": ["txn1"],
                "merchant": ["Amazon"],
                "category_id": ["cat1"],
                "category": ["Shopping"],
                "group": ["Retail"],
                "hideFromReports": [False],
            }
        )

        edit = TransactionEdit(
            transaction_id="txn1",
            field="hide_from_reports",
            old_value=False,
            new_value=True,
            timestamp=datetime.now(),
        )

        def mock_apply_groups(df):
            return df

        updated = CommitOrchestrator.apply_edit_to_dataframe(df, edit, {}, mock_apply_groups)

        assert updated["hideFromReports"][0] is True

    def test_unknown_field_raises_error(self):
        """Should raise ValueError for unknown field."""
        df = pl.DataFrame({"id": ["txn1"], "merchant": ["Amazon"]})

        edit = TransactionEdit(
            transaction_id="txn1",
            field="invalid_field",
            old_value="old",
            new_value="new",
            timestamp=datetime.now(),
        )

        def mock_apply_groups(df):
            return df

        with pytest.raises(ValueError, match="Unknown edit field"):
            CommitOrchestrator.apply_edit_to_dataframe(df, edit, {}, mock_apply_groups)


class TestApplyEditsToDataframe:
    """Tests for apply_edits_to_dataframe batch method."""

    def test_applies_multiple_edits(self):
        """Should apply all edits in order."""
        df = pl.DataFrame(
            {
                "id": ["txn1", "txn2", "txn3"],
                "merchant": ["Amazon", "Starbucks", "Target"],
                "category_id": ["cat1", "cat2", "cat3"],
                "category": ["Shopping", "Dining", "Shopping"],
                "group": ["Retail", "Food", "Retail"],
                "hideFromReports": [False, False, False],
            }
        )

        edits = [
            TransactionEdit("txn1", "merchant", "Amazon", "Whole Foods", datetime.now()),
            TransactionEdit("txn2", "hide_from_reports", False, True, datetime.now()),
            TransactionEdit("txn3", "merchant", "Target", "Costco", datetime.now()),
        ]

        def mock_apply_groups(df):
            return df

        updated = CommitOrchestrator.apply_edits_to_dataframe(df, edits, {}, mock_apply_groups)

        assert updated.filter(pl.col("id") == "txn1")["merchant"][0] == "Whole Foods"
        assert updated.filter(pl.col("id") == "txn2")["hideFromReports"][0] is True
        assert updated.filter(pl.col("id") == "txn3")["merchant"][0] == "Costco"

    def test_applies_multiple_edits_same_transaction(self):
        """Should handle multiple edits to same transaction."""
        df = pl.DataFrame(
            {
                "id": ["txn1"],
                "merchant": ["Amazon"],
                "category_id": ["cat1"],
                "category": ["Shopping"],
                "group": ["Retail"],
                "hideFromReports": [False],
            }
        )

        categories = {"cat2": {"name": "Groceries"}}

        edits = [
            TransactionEdit("txn1", "merchant", "Amazon", "Whole Foods", datetime.now()),
            TransactionEdit("txn1", "category", "cat1", "cat2", datetime.now()),
            TransactionEdit("txn1", "hide_from_reports", False, True, datetime.now()),
        ]

        def mock_apply_groups(df):
            return df.with_columns(pl.lit("Food").alias("group"))

        updated = CommitOrchestrator.apply_edits_to_dataframe(
            df, edits, categories, mock_apply_groups
        )

        result = updated.filter(pl.col("id") == "txn1")
        assert result["merchant"][0] == "Whole Foods"
        assert result["category"][0] == "Groceries"
        assert result["hideFromReports"][0] is True
        assert result["group"][0] == "Food"

    def test_empty_edits_list(self):
        """Should return unchanged DataFrame for empty edits."""
        df = pl.DataFrame({"id": ["txn1"], "merchant": ["Amazon"]})

        def mock_apply_groups(df):
            return df

        updated = CommitOrchestrator.apply_edits_to_dataframe(df, [], {}, mock_apply_groups)

        assert updated["merchant"][0] == "Amazon"

    def test_category_name_lookup_fallback(self):
        """Should use 'Unknown' for missing category."""
        df = pl.DataFrame(
            {"id": ["txn1"], "category_id": ["cat1"], "category": ["Old"], "group": ["OldGroup"]}
        )

        # Category cat999 not in categories dict
        edit = TransactionEdit("txn1", "category", "cat1", "cat999", datetime.now())

        def mock_apply_groups(df):
            return df

        updated = CommitOrchestrator.apply_edit_to_dataframe(df, edit, {}, mock_apply_groups)

        assert updated["category"][0] == "Unknown"


class TestDataFramePurity:
    """Tests ensuring DataFrame operations are pure (no mutations)."""

    def test_does_not_mutate_original_dataframe(self):
        """Should not mutate the original DataFrame."""
        original_df = pl.DataFrame({"id": ["txn1"], "merchant": ["Amazon"]})

        # Create a copy to check against
        original_merchant = original_df["merchant"][0]

        updated = CommitOrchestrator.apply_merchant_edit(original_df, "txn1", "Target")

        # Original should be unchanged
        assert original_df["merchant"][0] == original_merchant
        # Updated should be different
        assert updated["merchant"][0] == "Target"

    def test_multiple_edits_dont_mutate(self):
        """Multiple edits should not mutate intermediate DataFrames."""
        df = pl.DataFrame({"id": ["txn1"], "merchant": ["Amazon"], "hideFromReports": [False]})

        original_merchant = df["merchant"][0]

        edit1 = TransactionEdit("txn1", "merchant", "Amazon", "Target", datetime.now())
        edit2 = TransactionEdit("txn1", "hide_from_reports", False, True, datetime.now())

        def mock_apply_groups(df):
            return df

        CommitOrchestrator.apply_edits_to_dataframe(df, [edit1, edit2], {}, mock_apply_groups)

        # Original unchanged
        assert df["merchant"][0] == original_merchant
        assert df["hideFromReports"][0] is False


class TestRealWorldScenarios:
    """Integration tests with realistic scenarios."""

    def test_bulk_merchant_rename(self):
        """Should handle bulk merchant rename correctly."""
        # Scenario: Rename all "AMZN*ABC123" to "Amazon"
        df = pl.DataFrame(
            {
                "id": ["txn1", "txn2", "txn3", "txn4"],
                "merchant": ["AMZN*ABC123", "AMZN*ABC123", "Starbucks", "AMZN*ABC123"],
                "category_id": ["cat1", "cat1", "cat2", "cat1"],
                "category": ["Shopping", "Shopping", "Dining", "Shopping"],
                "group": ["Retail", "Retail", "Food", "Retail"],
                "hideFromReports": [False, False, False, False],
            }
        )

        edits = [
            TransactionEdit("txn1", "merchant", "AMZN*ABC123", "Amazon", datetime.now()),
            TransactionEdit("txn2", "merchant", "AMZN*ABC123", "Amazon", datetime.now()),
            TransactionEdit("txn4", "merchant", "AMZN*ABC123", "Amazon", datetime.now()),
        ]

        def mock_apply_groups(df):
            return df

        updated = CommitOrchestrator.apply_edits_to_dataframe(df, edits, {}, mock_apply_groups)

        assert updated.filter(pl.col("id") == "txn1")["merchant"][0] == "Amazon"
        assert updated.filter(pl.col("id") == "txn2")["merchant"][0] == "Amazon"
        assert updated.filter(pl.col("id") == "txn3")["merchant"][0] == "Starbucks"  # Unchanged
        assert updated.filter(pl.col("id") == "txn4")["merchant"][0] == "Amazon"

    def test_edit_category_with_group_update(self):
        """Should handle recategorization with group updates."""
        df = pl.DataFrame(
            {
                "id": ["txn1"],
                "merchant": ["Whole Foods"],
                "category_id": ["cat1"],
                "category": ["Shopping"],
                "group": ["Retail"],
                "hideFromReports": [False],
            }
        )

        categories = {"cat_groceries": {"name": "Groceries", "group": "Food & Dining"}}

        edit = TransactionEdit("txn1", "category", "cat1", "cat_groceries", datetime.now())

        def apply_food_group(df):
            # Simulate real category group application
            return df.with_columns(
                pl.when(pl.col("category") == "Groceries")
                .then(pl.lit("Food & Dining"))
                .otherwise(pl.col("group"))
                .alias("group")
            )

        updated = CommitOrchestrator.apply_edit_to_dataframe(df, edit, categories, apply_food_group)

        assert updated["category"][0] == "Groceries"
        assert updated["group"][0] == "Food & Dining"

    def test_hide_transfers(self):
        """Should handle hiding transfer transactions."""
        df = pl.DataFrame(
            {
                "id": ["txn1", "txn2", "txn3"],
                "merchant": ["Transfer", "Transfer", "Amazon"],
                "hideFromReports": [False, False, False],
            }
        )

        edits = [
            TransactionEdit("txn1", "hide_from_reports", False, True, datetime.now()),
            TransactionEdit("txn2", "hide_from_reports", False, True, datetime.now()),
        ]

        def mock_apply_groups(df):
            return df

        updated = CommitOrchestrator.apply_edits_to_dataframe(df, edits, {}, mock_apply_groups)

        assert updated.filter(pl.col("id") == "txn1")["hideFromReports"][0] is True
        assert updated.filter(pl.col("id") == "txn2")["hideFromReports"][0] is True
        assert updated.filter(pl.col("id") == "txn3")["hideFromReports"][0] is False

    def test_complex_workflow_sequence(self):
        """Test realistic workflow: rename merchant, edit_category, hide."""
        df = pl.DataFrame(
            {
                "id": ["txn1"],
                "merchant": ["WHOLE FOODS MKT #123"],
                "category_id": ["cat_misc"],
                "category": ["Miscellaneous"],
                "group": ["Other"],
                "hideFromReports": [False],
            }
        )

        categories = {"cat_groceries": {"name": "Groceries", "group": "Food & Dining"}}

        edits = [
            # Step 1: Clean up merchant name
            TransactionEdit(
                "txn1", "merchant", "WHOLE FOODS MKT #123", "Whole Foods", datetime.now()
            ),
            # Step 2: Edit Category to Groceries
            TransactionEdit("txn1", "category", "cat_misc", "cat_groceries", datetime.now()),
        ]

        def mock_apply_groups(df):
            return df.with_columns(pl.lit("Food & Dining").alias("group"))

        updated = CommitOrchestrator.apply_edits_to_dataframe(
            df, edits, categories, mock_apply_groups
        )

        result = updated.filter(pl.col("id") == "txn1")
        assert result["merchant"][0] == "Whole Foods"
        assert result["category"][0] == "Groceries"
        assert result["group"][0] == "Food & Dining"


class TestEdgeCases:
    """Edge case and error handling tests."""

    def test_special_characters_in_merchant(self):
        """Should handle special characters in merchant names."""
        df = pl.DataFrame({"id": ["txn1"], "merchant": ["Old & Busted"]})

        updated = CommitOrchestrator.apply_merchant_edit(df, "txn1", "New & Improved")

        assert updated["merchant"][0] == "New & Improved"

    def test_unicode_in_merchant(self):
        """Should handle unicode characters."""
        df = pl.DataFrame({"id": ["txn1"], "merchant": ["Café"]})

        updated = CommitOrchestrator.apply_merchant_edit(df, "txn1", "Café Français")

        assert updated["merchant"][0] == "Café Français"

    def test_very_long_merchant_name(self):
        """Should handle very long merchant names."""
        df = pl.DataFrame({"id": ["txn1"], "merchant": ["Short"]})

        long_name = "A" * 200

        updated = CommitOrchestrator.apply_merchant_edit(df, "txn1", long_name)

        assert updated["merchant"][0] == long_name

    def test_empty_string_merchant(self):
        """Should handle empty string merchant."""
        df = pl.DataFrame({"id": ["txn1"], "merchant": ["Amazon"]})

        updated = CommitOrchestrator.apply_merchant_edit(df, "txn1", "")

        assert updated["merchant"][0] == ""

    def test_large_dataset_single_transaction(self):
        """Without bulk_merchant_renames, only the specific transaction is updated."""
        num_txns = 1000

        df = pl.DataFrame(
            {
                "id": [f"txn{i}" for i in range(num_txns)],
                "merchant": ["Amazon"] * num_txns,
                "category_id": ["cat1"] * num_txns,
                "category": ["Shopping"] * num_txns,
                "group": ["Retail"] * num_txns,
                "hideFromReports": [False] * num_txns,
            }
        )

        # Without bulk_merchant_renames, only txn0 should be updated
        edits = [TransactionEdit("txn0", "merchant", "Amazon", "Target", datetime.now())]

        def mock_apply_groups(df):
            return df

        updated = CommitOrchestrator.apply_edits_to_dataframe(
            df, edits, {}, mock_apply_groups, bulk_merchant_renames=None
        )

        # Only txn0 updated (Monarch Money behavior)
        assert updated.filter(pl.col("id") == "txn0")["merchant"][0] == "Target"
        assert updated.filter(pl.col("id") == "txn1")["merchant"][0] == "Amazon"
        assert updated.filter(pl.col("id") == "txn999")["merchant"][0] == "Amazon"

    def test_large_dataset_bulk_update(self):
        """With bulk_merchant_renames, ALL matching transactions are updated."""
        num_txns = 1000

        df = pl.DataFrame(
            {
                "id": [f"txn{i}" for i in range(num_txns)],
                "merchant": ["Amazon"] * num_txns,
                "category_id": ["cat1"] * num_txns,
                "category": ["Shopping"] * num_txns,
                "group": ["Retail"] * num_txns,
                "hideFromReports": [False] * num_txns,
            }
        )

        # With bulk_merchant_renames, ALL Amazon transactions should be updated
        edits = [TransactionEdit("txn0", "merchant", "Amazon", "Target", datetime.now())]

        def mock_apply_groups(df):
            return df

        updated = CommitOrchestrator.apply_edits_to_dataframe(
            df, edits, {}, mock_apply_groups, bulk_merchant_renames={("Amazon", "Target")}
        )

        # ALL transactions updated (YNAB behavior)
        assert updated.filter(pl.col("id") == "txn0")["merchant"][0] == "Target"
        assert updated.filter(pl.col("id") == "txn1")["merchant"][0] == "Target"
        assert updated.filter(pl.col("id") == "txn999")["merchant"][0] == "Target"
