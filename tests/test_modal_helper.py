"""
Unit tests for modal_helper functions.

These tests verify that modal parameter preparation logic is correct
and can be tested without requiring the UI to be running.
"""

from datetime import datetime

import polars as pl
import pytest

from moneyflow.data.state import TransactionEdit
from moneyflow.tui.modal_helper import (
    get_cache_prompt_params,
    get_delete_confirmation_params,
    get_duplicates_params,
    get_edit_merchant_params,
    get_filter_params,
    get_quit_confirmation_params,
    get_review_changes_params,
    get_search_params,
    get_select_category_params,
    get_transaction_detail_params,
)


class TestEditMerchantParams:
    """Test parameters for Edit Merchant modal."""

    def test_basic_params(self):
        params = get_edit_merchant_params(
            merchant_name="Amazon",
            transaction_count=5,
            all_merchants=["Amazon", "Walmart", "Target"],
        )

        assert params["current_merchant"] == "Amazon"
        assert params["transaction_count"] == 5
        assert params["all_merchants"] == ["Amazon", "Walmart", "Target"]
        assert "bulk_summary" not in params
        assert "txn_details" not in params

    def test_with_bulk_summary(self):
        params = get_edit_merchant_params(
            merchant_name="Amazon",
            transaction_count=15,
            all_merchants=["Amazon"],
            bulk_summary={"total_amount": -250.50},
        )

        assert params["current_merchant"] == "Amazon"
        assert params["transaction_count"] == 15
        assert params["all_merchants"] == ["Amazon"]
        assert params["bulk_summary"]["total_amount"] == -250.50

    def test_with_txn_details(self):
        params = get_edit_merchant_params(
            merchant_name="Amazon",
            transaction_count=1,
            all_merchants=["Amazon"],
            txn_details={"date": "2025-10-14", "amount": -42.99, "category": "Shopping"},
        )

        assert params["current_merchant"] == "Amazon"
        assert params["transaction_count"] == 1
        assert params["all_merchants"] == ["Amazon"]
        assert params["txn_details"]["date"] == "2025-10-14"
        assert params["txn_details"]["amount"] == -42.99
        assert params["txn_details"]["category"] == "Shopping"

    def test_both_bulk_and_details(self):
        """Can include both bulk summary and transaction details."""
        params = get_edit_merchant_params(
            merchant_name="Test",
            transaction_count=1,
            all_merchants=["Test"],
            bulk_summary={"total_amount": -100.0},
            txn_details={"date": "2025-10-14", "amount": -100.0},
        )

        assert params["current_merchant"] == "Test"
        assert params["transaction_count"] == 1
        assert params["all_merchants"] == ["Test"]
        assert "bulk_summary" in params
        assert "txn_details" in params


class TestSelectCategoryParams:
    """Test parameters for Category Selection modal."""

    def test_basic_params(self):
        categories = {
            "cat_1": {"name": "Groceries", "group": "Food"},
            "cat_2": {"name": "Gas", "group": "Automotive"},
        }

        params = get_select_category_params(categories)

        assert params["categories"] == categories
        assert params["current_category_id"] is None
        assert "txn_details" not in params

    def test_with_current_category(self):
        categories = {"cat_1": {"name": "Groceries"}}

        params = get_select_category_params(categories, current_category_id="cat_1")

        assert params["current_category_id"] == "cat_1"

    def test_with_txn_details(self):
        params = get_select_category_params(
            categories={},
            current_category_id="cat_1",
            txn_details={"date": "2025-10-14", "amount": -25.0, "merchant": "Safeway"},
        )

        assert params["txn_details"]["merchant"] == "Safeway"


class TestReviewChangesParams:
    """Test parameters for Review Changes modal."""

    def test_basic_params(self):
        edits = [
            TransactionEdit("txn_1", "merchant", "Old", "New", datetime.now()),
            TransactionEdit("txn_2", "category", "cat_1", "cat_2", datetime.now()),
        ]
        categories = {"cat_1": {"name": "Food"}, "cat_2": {"name": "Gas"}}

        params = get_review_changes_params(edits, categories)

        assert params["edits"] == edits
        assert params["categories"] == categories


class TestDeleteConfirmationParams:
    """Test parameters for Delete Confirmation modal."""

    def test_default_single_transaction(self):
        params = get_delete_confirmation_params()
        assert params["transaction_count"] == 1

    def test_multiple_transactions(self):
        params = get_delete_confirmation_params(transaction_count=10)
        assert params["transaction_count"] == 10


class TestQuitConfirmationParams:
    """Test parameters for Quit Confirmation modal."""

    def test_with_unsaved_changes(self):
        params = get_quit_confirmation_params(has_unsaved_changes=True)
        assert params["has_unsaved_changes"] is True

    def test_without_unsaved_changes(self):
        params = get_quit_confirmation_params(has_unsaved_changes=False)
        assert params["has_unsaved_changes"] is False


class TestFilterParams:
    """Test parameters for Filter Settings modal."""

    def test_basic_params(self):
        params = get_filter_params(show_transfers=True, show_hidden=False)

        assert params["show_transfers"] is True
        assert params["show_hidden"] is False


class TestSearchParams:
    """Test parameters for Search modal."""

    def test_default_empty_query(self):
        params = get_search_params()
        assert params["current_query"] == ""

    def test_with_existing_query(self):
        params = get_search_params(current_query="Amazon")
        assert params["current_query"] == "Amazon"


class TestCachePromptParams:
    """Test parameters for Cache Prompt modal."""

    def test_basic_params(self):
        params = get_cache_prompt_params(
            age="2 hours ago", transaction_count=1500, filter_desc="All transactions"
        )

        assert params["age"] == "2 hours ago"
        assert params["transaction_count"] == 1500
        assert params["filter_desc"] == "All transactions"


class TestTransactionDetailParams:
    """Test parameters for Transaction Detail modal."""

    def test_basic_params(self):
        txn = {
            "id": "txn_123",
            "date": "2025-10-14",
            "merchant": "Starbucks",
            "amount": -5.75,
            "category": "Coffee Shops",
        }

        params = get_transaction_detail_params(txn)

        assert params["transaction"] == txn


class TestDuplicatesParams:
    """Test parameters for Duplicates modal."""

    def test_basic_params(self):
        duplicates_df = pl.DataFrame(
            {
                "id": ["txn_1", "txn_2"],
                "amount": [-100.0, -100.0],
            }
        )

        all_txns_df = pl.DataFrame(
            {
                "id": ["txn_1", "txn_2", "txn_3"],
                "amount": [-100.0, -100.0, -50.0],
            }
        )

        groups = [["txn_1", "txn_2"]]

        params = get_duplicates_params(duplicates_df, groups, all_txns_df)

        assert params["duplicates"].equals(duplicates_df)
        assert params["groups"] == groups
        assert params["all_transactions"].equals(all_txns_df)


class TestParameterTypeConsistency:
    """Test that parameter dictionaries have correct types."""

    @pytest.mark.parametrize(
        "method,args",
        [
            (get_edit_merchant_params, ("Amazon", 1, ["Amazon"])),
            (get_select_category_params, ({"cat_1": {"name": "Food"}},)),
            (get_review_changes_params, ([], {})),
            (get_delete_confirmation_params, ()),
            (get_quit_confirmation_params, (True,)),
            (get_filter_params, (True, False)),
            (get_search_params, ()),
            (get_cache_prompt_params, ("2 hours", 1, "All")),
            (get_transaction_detail_params, ({"id": "txn_1"},)),
            (
                get_duplicates_params,
                (pl.DataFrame({"id": ["txn_1"]}), [["txn_1"]], pl.DataFrame({"id": ["txn_1"]})),
            ),
        ],
    )
    def test_all_methods_return_dict(self, method, args):
        """All helper methods should return dictionaries."""
        result = method(*args)
        assert isinstance(result, dict), f"{method.__name__} didn't return dict"
        assert len(result) > 0, f"{method.__name__} returned empty dict"
