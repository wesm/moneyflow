"""
Tests for DataManager operations including aggregation, filtering, and API integration.
"""

from datetime import datetime

import polars as pl

from moneyflow.data_manager import DataManager
from moneyflow.state import TimeGranularity, TransactionEdit


class TestDataFetching:
    """Test data fetching from API."""

    async def test_fetch_all_data(self, data_manager):
        """Test fetching all transactions and metadata."""
        df, categories, category_groups = await data_manager.fetch_all_data()

        assert df is not None
        assert len(df) > 0
        assert isinstance(df, pl.DataFrame)
        assert len(categories) > 0
        assert len(category_groups) > 0

    async def test_fetch_with_date_filter(self, data_manager):
        """Test fetching with date range."""
        df, _, _ = await data_manager.fetch_all_data(start_date="2024-10-01", end_date="2024-10-03")

        assert df is not None
        # Should have filtered transactions
        dates = df["date"].to_list()
        for d in dates:
            assert d.year == 2024
            assert d.month == 10
            assert 1 <= d.day <= 3


class TestAggregation:
    """Test data aggregation functions."""

    async def test_aggregate_by_merchant(self, loaded_data_manager):
        """Test merchant aggregation."""
        dm, df, _, _ = loaded_data_manager

        agg = dm.aggregate_by_merchant(df)

        assert len(agg) > 0
        assert "merchant" in agg.columns
        assert "count" in agg.columns
        assert "total" in agg.columns
        assert "top_category" in agg.columns
        assert "top_category_pct" in agg.columns

        # Note: Sorting is now handled by app.py, not by aggregate methods
        # The aggregation just returns grouped data

    async def test_aggregate_by_merchant_top_category_single(self, mock_mm, tmp_path):
        """Test top category when all transactions have same category."""
        dm = DataManager(mock_mm, config_dir=str(tmp_path))

        # All Whole Foods transactions are Groceries
        df = pl.DataFrame(
            {
                "id": ["1", "2", "3"],
                "merchant": ["Whole Foods", "Whole Foods", "Whole Foods"],
                "merchant_id": ["m1", "m1", "m1"],
                "category": ["Groceries", "Groceries", "Groceries"],
                "amount": [-50.0, -30.0, -20.0],
                "hideFromReports": [False, False, False],
            }
        )

        agg = dm.aggregate_by_merchant(df)

        assert len(agg) == 1
        row = agg.row(0, named=True)
        assert row["merchant"] == "Whole Foods"
        assert row["count"] == 3
        assert row["top_category"] == "Groceries"
        assert row["top_category_pct"] == 100  # 100% are Groceries

    async def test_aggregate_by_merchant_top_category_mixed(self, mock_mm, tmp_path):
        """Test top category when transactions have different categories."""
        dm = DataManager(mock_mm, config_dir=str(tmp_path))

        # Starbucks: 7 Coffee Shops, 3 Groceries
        df = pl.DataFrame(
            {
                "id": ["1", "2", "3", "4", "5", "6", "7", "8", "9", "10"],
                "merchant": ["Starbucks"] * 10,
                "merchant_id": ["m1"] * 10,
                "category": ["Coffee Shops"] * 7 + ["Groceries"] * 3,
                "amount": [-5.0] * 10,
                "hideFromReports": [False] * 10,
            }
        )

        agg = dm.aggregate_by_merchant(df)

        assert len(agg) == 1
        row = agg.row(0, named=True)
        assert row["merchant"] == "Starbucks"
        assert row["count"] == 10
        assert row["top_category"] == "Coffee Shops"
        assert row["top_category_pct"] == 70  # 7/10 = 70%

    async def test_aggregate_by_merchant_top_category_multiple_merchants(self, mock_mm, tmp_path):
        """Test top category with multiple merchants."""
        dm = DataManager(mock_mm, config_dir=str(tmp_path))

        df = pl.DataFrame(
            {
                "id": ["1", "2", "3", "4", "5"],
                "merchant": ["Whole Foods", "Whole Foods", "Amazon", "Amazon", "Amazon"],
                "merchant_id": ["m1", "m1", "m2", "m2", "m2"],
                "category": ["Groceries", "Groceries", "Shopping", "Electronics", "Electronics"],
                "amount": [-50.0, -30.0, -25.0, -100.0, -75.0],
                "hideFromReports": [False, False, False, False, False],
            }
        )

        agg = dm.aggregate_by_merchant(df)

        assert len(agg) == 2

        # Check Whole Foods
        wf = agg.filter(pl.col("merchant") == "Whole Foods").row(0, named=True)
        assert wf["top_category"] == "Groceries"
        assert wf["top_category_pct"] == 100

        # Check Amazon (2 Electronics, 1 Shopping = 67% Electronics)
        amz = agg.filter(pl.col("merchant") == "Amazon").row(0, named=True)
        assert amz["top_category"] == "Electronics"
        assert amz["top_category_pct"] == 67  # 2/3 rounded

    async def test_aggregate_by_category(self, loaded_data_manager):
        """Test category aggregation."""
        dm, df, _, _ = loaded_data_manager

        agg = dm.aggregate_by_category(df)

        assert len(agg) > 0
        assert "category" in agg.columns
        assert "count" in agg.columns
        assert "total" in agg.columns
        assert "group" in agg.columns

    async def test_aggregate_by_group(self, loaded_data_manager):
        """Test group aggregation."""
        dm, df, _, _ = loaded_data_manager

        agg = dm.aggregate_by_group(df)

        assert len(agg) > 0
        assert "group" in agg.columns
        assert "count" in agg.columns
        assert "total" in agg.columns

    async def test_aggregate_empty_dataframe(self, data_manager):
        """Test aggregation on empty DataFrame."""
        empty_df = pl.DataFrame()

        agg_merchant = data_manager.aggregate_by_merchant(empty_df)
        agg_category = data_manager.aggregate_by_category(empty_df)
        agg_group = data_manager.aggregate_by_group(empty_df)

        assert agg_merchant.is_empty()
        assert agg_category.is_empty()
        assert agg_group.is_empty()


class TestFiltering:
    """Test data filtering operations."""

    async def test_filter_by_merchant(self, loaded_data_manager):
        """Test filtering by merchant name."""
        dm, df, _, _ = loaded_data_manager

        # Filter by a merchant we know exists
        filtered = dm.filter_by_merchant(df, "Whole Foods")

        assert len(filtered) > 0
        merchants = filtered["merchant"].unique().to_list()
        assert merchants == ["Whole Foods"]

    async def test_filter_by_category(self, loaded_data_manager):
        """Test filtering by category name."""
        dm, df, _, _ = loaded_data_manager

        filtered = dm.filter_by_category(df, "Groceries")

        assert len(filtered) > 0
        categories = filtered["category"].unique().to_list()
        assert categories == ["Groceries"]

    async def test_filter_by_group(self, loaded_data_manager):
        """Test filtering by group name."""
        dm, df, _, _ = loaded_data_manager

        filtered = dm.filter_by_group(df, "Food & Dining")

        assert len(filtered) > 0
        groups = filtered["group"].unique().to_list()
        assert groups == ["Food & Dining"]

    async def test_search_transactions(self, loaded_data_manager):
        """Test search functionality."""
        dm, df, _, _ = loaded_data_manager

        # Search for "starbucks"
        results = dm.search_transactions(df, "starbucks")

        assert len(results) > 0
        # All results should contain "starbucks" in merchant, category, or notes
        for row in results.iter_rows(named=True):
            text = f"{row['merchant']} {row['category']} {row['notes']}".lower()
            assert "starbucks" in text

    async def test_search_empty_query(self, loaded_data_manager):
        """Test search with empty query returns all."""
        dm, df, _, _ = loaded_data_manager

        results = dm.search_transactions(df, "")

        assert len(results) == len(df)


class TestCommitEdits:
    """Test committing pending edits to the API."""

    async def test_commit_single_edit(self, data_manager, mock_mm):
        """Test committing a single edit."""
        edits = [TransactionEdit("txn_1", "merchant", "Old Name", "New Name", datetime.now())]

        success, failure = await data_manager.commit_pending_edits(edits)

        assert success == 1
        assert failure == 0
        assert len(mock_mm.update_calls) == 1

        # Verify the update call
        call = mock_mm.update_calls[0]
        assert call["transaction_id"] == "txn_1"
        assert call["merchant_name"] == "New Name"

    async def test_commit_multiple_edits(self, data_manager, mock_mm):
        """Test committing multiple edits."""
        edits = [
            TransactionEdit("txn_1", "merchant", "A", "B", datetime.now()),
            TransactionEdit("txn_2", "category", "cat_old", "cat_new", datetime.now()),
            TransactionEdit("txn_3", "hide_from_reports", False, True, datetime.now()),
        ]

        success, failure = await data_manager.commit_pending_edits(edits)

        assert success == 3
        assert failure == 0
        assert len(mock_mm.update_calls) == 3

    async def test_commit_empty_edits(self, data_manager, mock_mm):
        """Test committing with no edits."""
        success, failure = await data_manager.commit_pending_edits([])

        assert success == 0
        assert failure == 0
        assert len(mock_mm.update_calls) == 0

    async def test_commit_merchant_rename(self, data_manager, mock_mm):
        """Test committing a merchant rename."""
        edits = [TransactionEdit("txn_1", "merchant", "Amazon.com", "Amazon", datetime.now())]

        await data_manager.commit_pending_edits(edits)

        # Verify the transaction was updated in mock backend
        txn = mock_mm.get_transaction_by_id("txn_1")
        assert txn is not None
        assert txn["merchant"]["name"] == "Amazon"

    async def test_commit_category_change(self, data_manager, mock_mm):
        """Test committing a category change."""
        edits = [
            TransactionEdit("txn_1", "category", "cat_groceries", "cat_shopping", datetime.now())
        ]

        await data_manager.commit_pending_edits(edits)

        # Verify the transaction was updated
        txn = mock_mm.get_transaction_by_id("txn_1")
        assert txn is not None
        assert txn["category"]["id"] == "cat_shopping"

    async def test_commit_hide_toggle(self, data_manager, mock_mm):
        """Test committing hide from reports toggle."""
        edits = [TransactionEdit("txn_1", "hide_from_reports", False, True, datetime.now())]

        await data_manager.commit_pending_edits(edits)

        # Verify the transaction was updated
        txn = mock_mm.get_transaction_by_id("txn_1")
        assert txn is not None
        assert txn["hideFromReports"] is True


class TestCategoryGroupMapping:
    """Test category to group mapping."""

    def test_category_mapping_exists(self, data_manager):
        """Test that category to group mapping is initialized."""
        assert len(data_manager.category_to_group) > 0

    def test_groceries_mapped_to_food(self, data_manager):
        """Test that Groceries maps to Food & Dining."""
        assert data_manager.category_to_group.get("Groceries") == "Food & Dining"

    def test_gas_mapped_to_auto_transport(self, data_manager):
        """Test that Gas maps to Auto & Transport."""
        assert data_manager.category_to_group.get("Gas") == "Auto & Transport"

    async def test_transactions_have_groups(self, loaded_data_manager):
        """Test that loaded transactions have group field."""
        dm, df, _, _ = loaded_data_manager

        assert "group" in df.columns
        groups = df["group"].unique().to_list()
        assert len(groups) > 0
        assert all(g is not None for g in groups)


class TestEdgeCases:
    """Test edge cases and malformed data handling."""

    async def test_transactions_to_dataframe_empty_list(self, data_manager):
        """Test converting empty transaction list to DataFrame."""
        df = data_manager._transactions_to_dataframe([], {})

        assert df is not None
        assert df.is_empty()

    async def test_transactions_to_dataframe_none_merchant(self, data_manager):
        """Test transaction with None merchant field."""
        transactions = [
            {
                "id": "txn_1",
                "date": "2024-10-01",
                "amount": -50.00,
                "merchant": None,  # None merchant
                "category": {"id": "cat_1", "name": "Groceries"},
                "account": {"id": "acc_1", "displayName": "Checking"},
                "notes": "test",
                "hideFromReports": False,
                "pending": False,
                "isRecurring": False,
            }
        ]

        df = data_manager._transactions_to_dataframe(transactions, {})

        assert len(df) == 1
        assert df["merchant"][0] == "Unknown"

    async def test_transactions_to_dataframe_empty_merchant_name(self, data_manager):
        """Test transaction with empty merchant name."""
        transactions = [
            {
                "id": "txn_1",
                "date": "2024-10-01",
                "amount": -50.00,
                "merchant": {"id": "merch_1", "name": ""},  # Empty name
                "category": {"id": "cat_1", "name": "Groceries"},
                "account": {"id": "acc_1", "displayName": "Checking"},
                "notes": None,
                "hideFromReports": False,
                "pending": False,
                "isRecurring": False,
            }
        ]

        df = data_manager._transactions_to_dataframe(transactions, {})

        assert len(df) == 1
        assert df["merchant"][0] == "Unknown"

    async def test_transactions_to_dataframe_none_category(self, data_manager):
        """Test transaction with None category field."""
        transactions = [
            {
                "id": "txn_1",
                "date": "2024-10-01",
                "amount": -50.00,
                "merchant": {"id": "merch_1", "name": "Store"},
                "category": None,  # None category
                "account": {"id": "acc_1", "displayName": "Checking"},
                "notes": "",
                "hideFromReports": False,
                "pending": False,
                "isRecurring": False,
            }
        ]

        df = data_manager._transactions_to_dataframe(transactions, {})
        # Apply grouping (now done separately)
        df = data_manager.apply_category_groups(df)

        assert len(df) == 1
        assert df["category"][0] == "Uncategorized"
        assert df["group"][0] == "Uncategorized"

    async def test_transactions_to_dataframe_empty_category_name(self, data_manager):
        """Test transaction with empty category name."""
        transactions = [
            {
                "id": "txn_1",
                "date": "2024-10-01",
                "amount": -50.00,
                "merchant": {"id": "merch_1", "name": "Store"},
                "category": {"id": "cat_1", "name": ""},  # Empty name
                "account": {"id": "acc_1", "displayName": "Checking"},
                "notes": "",
                "hideFromReports": False,
                "pending": False,
                "isRecurring": False,
            }
        ]

        df = data_manager._transactions_to_dataframe(transactions, {})

        assert len(df) == 1
        assert df["category"][0] == "Uncategorized"

    async def test_transactions_to_dataframe_none_account(self, data_manager):
        """Test transaction with None account field."""
        transactions = [
            {
                "id": "txn_1",
                "date": "2024-10-01",
                "amount": -50.00,
                "merchant": {"id": "merch_1", "name": "Store"},
                "category": {"id": "cat_1", "name": "Groceries"},
                "account": None,  # None account
                "notes": "",
                "hideFromReports": False,
                "pending": False,
                "isRecurring": False,
            }
        ]

        df = data_manager._transactions_to_dataframe(transactions, {})

        assert len(df) == 1
        assert df["account"][0] == ""

    async def test_transactions_to_dataframe_empty_account_name(self, data_manager):
        """Test transaction with empty account display name."""
        transactions = [
            {
                "id": "txn_1",
                "date": "2024-10-01",
                "amount": -50.00,
                "merchant": {"id": "merch_1", "name": "Store"},
                "category": {"id": "cat_1", "name": "Groceries"},
                "account": {"id": "acc_1", "displayName": ""},  # Empty name
                "notes": "",
                "hideFromReports": False,
                "pending": False,
                "isRecurring": False,
            }
        ]

        df = data_manager._transactions_to_dataframe(transactions, {})

        assert len(df) == 1
        assert df["account"][0] == ""

    async def test_transactions_to_dataframe_none_notes(self, data_manager):
        """Test transaction with None notes field."""
        transactions = [
            {
                "id": "txn_1",
                "date": "2024-10-01",
                "amount": -50.00,
                "merchant": {"id": "merch_1", "name": "Store"},
                "category": {"id": "cat_1", "name": "Groceries"},
                "account": {"id": "acc_1", "displayName": "Checking"},
                "notes": None,  # None notes
                "hideFromReports": False,
                "pending": False,
                "isRecurring": False,
            }
        ]

        df = data_manager._transactions_to_dataframe(transactions, {})

        assert len(df) == 1
        assert df["notes"][0] == ""

    async def test_transactions_to_dataframe_missing_optional_fields(self, data_manager):
        """Test transaction with missing optional fields."""
        transactions = [
            {
                "id": "txn_1",
                "date": "2024-10-01",
                "amount": -50.00,
                "merchant": {"id": "merch_1", "name": "Store"},
                "category": {"id": "cat_1", "name": "Groceries"},
                "account": {"id": "acc_1", "displayName": "Checking"},
                # Missing notes, hideFromReports, pending, isRecurring
            }
        ]

        df = data_manager._transactions_to_dataframe(transactions, {})

        assert len(df) == 1
        assert df["notes"][0] == ""
        assert df["hideFromReports"][0] is False
        assert df["pending"][0] is False
        assert df["isRecurring"][0] is False

    async def test_transactions_to_dataframe_unknown_category_group(self, data_manager):
        """Test transaction with category not in group mapping."""
        transactions = [
            {
                "id": "txn_1",
                "date": "2024-10-01",
                "amount": -50.00,
                "merchant": {"id": "merch_1", "name": "Store"},
                "category": {"id": "cat_unknown", "name": "Unknown Category XYZ"},
                "account": {"id": "acc_1", "displayName": "Checking"},
                "notes": "",
                "hideFromReports": False,
                "pending": False,
                "isRecurring": False,
            }
        ]

        df = data_manager._transactions_to_dataframe(transactions, {})
        # Apply grouping (now done separately)
        df = data_manager.apply_category_groups(df)

        assert len(df) == 1
        assert df["group"][0] == "Uncategorized"


class TestGetStats:
    """Test get_stats() method with various DataFrame states."""

    async def test_get_stats_with_none_df(self, data_manager):
        """Test get_stats when df is None."""
        data_manager.df = None

        stats = data_manager.get_stats()

        assert stats["total_transactions"] == 0
        assert stats["total_income"] == 0.0
        assert stats["total_expenses"] == 0.0
        assert stats["net_savings"] == 0.0
        assert stats["pending_changes"] == 0

    async def test_get_stats_with_empty_df(self, data_manager):
        """Test get_stats with empty DataFrame."""
        data_manager.df = pl.DataFrame()

        stats = data_manager.get_stats()

        assert stats["total_transactions"] == 0
        assert stats["total_income"] == 0.0
        assert stats["total_expenses"] == 0.0
        assert stats["net_savings"] == 0.0
        assert stats["pending_changes"] == 0

    async def test_get_stats_with_data(self, loaded_data_manager):
        """Test get_stats with actual data."""
        dm, df, _, _ = loaded_data_manager
        dm.df = df

        stats = dm.get_stats()

        assert stats["total_transactions"] == len(df)
        # Stats now return income/expenses/savings breakdown
        assert "total_income" in stats
        assert "total_expenses" in stats
        assert "net_savings" in stats
        assert stats["pending_changes"] == 0

    async def test_get_stats_with_pending_edits(self, loaded_data_manager):
        """Test get_stats with pending edits."""
        dm, df, _, _ = loaded_data_manager
        dm.df = df
        dm.pending_edits = [
            TransactionEdit("txn_1", "merchant", "A", "B", datetime.now()),
            TransactionEdit("txn_2", "category", "C", "D", datetime.now()),
        ]

        stats = dm.get_stats()

        assert stats["total_transactions"] == len(df)
        assert stats["pending_changes"] == 2


class TestProgressCallbacks:
    """Test progress callback functionality."""

    async def test_fetch_all_data_with_progress_callback(self, data_manager):
        """Test fetch_all_data calls progress callback."""
        progress_messages = []

        def progress_callback(msg: str):
            progress_messages.append(msg)

        await data_manager.fetch_all_data(progress_callback=progress_callback)

        # Verify progress callbacks were made
        assert len(progress_messages) > 0
        assert any("Fetching categories" in msg for msg in progress_messages)
        assert any("Fetching transactions" in msg for msg in progress_messages)
        assert any("Processing transactions" in msg for msg in progress_messages)

    async def test_fetch_all_data_without_progress_callback(self, data_manager):
        """Test fetch_all_data works without progress callback."""
        df, categories, category_groups = await data_manager.fetch_all_data()

        assert df is not None
        assert len(df) > 0
        assert len(categories) > 0
        assert len(category_groups) > 0


class TestCommitEditsAdvanced:
    """Advanced tests for commit_pending_edits."""

    async def test_commit_multiple_edits_same_transaction(self, data_manager, mock_mm):
        """Test committing multiple edits to same transaction."""
        # Multiple edits to the same transaction should be grouped
        edits = [
            TransactionEdit("txn_1", "merchant", "A", "B", datetime.now()),
            TransactionEdit("txn_1", "category", "cat_old", "cat_new", datetime.now()),
            TransactionEdit("txn_1", "hide_from_reports", False, True, datetime.now()),
        ]

        success, failure = await data_manager.commit_pending_edits(edits)

        assert success == 1  # Only one transaction updated
        assert failure == 0
        assert len(mock_mm.update_calls) == 1

        # Verify all three fields were updated in single call
        call = mock_mm.update_calls[0]
        assert call["transaction_id"] == "txn_1"
        assert call["merchant_name"] == "B"
        assert call["category_id"] == "cat_new"
        assert call["hide_from_reports"] is True

    async def test_commit_with_api_failure(self, data_manager, mock_mm):
        """Test commit_pending_edits handles API failures gracefully."""
        # Create a mock that raises an exception
        original_update = mock_mm.update_transaction

        async def failing_update(*args, **kwargs):
            if kwargs.get("transaction_id") == "txn_2":
                raise Exception("API Error")
            return await original_update(*args, **kwargs)

        mock_mm.update_transaction = failing_update

        edits = [
            TransactionEdit("txn_1", "merchant", "A", "B", datetime.now()),
            TransactionEdit("txn_2", "merchant", "C", "D", datetime.now()),
            TransactionEdit("txn_3", "merchant", "E", "F", datetime.now()),
        ]

        success, failure = await data_manager.commit_pending_edits(edits)

        # Should have 2 successes and 1 failure
        assert success == 2
        assert failure == 1

    async def test_commit_mixed_edit_types(self, data_manager, mock_mm):
        """Test committing different types of edits together."""
        edits = [
            TransactionEdit("txn_1", "merchant", "Old", "New", datetime.now()),
            TransactionEdit("txn_2", "category", "cat_1", "cat_2", datetime.now()),
            TransactionEdit("txn_3", "hide_from_reports", False, True, datetime.now()),
            TransactionEdit("txn_4", "merchant", "X", "Y", datetime.now()),
        ]

        success, failure = await data_manager.commit_pending_edits(edits)

        assert success == 4
        assert failure == 0
        assert len(mock_mm.update_calls) == 4

        # Verify each update has correct field
        merchants = [c for c in mock_mm.update_calls if c["merchant_name"] is not None]
        categories = [c for c in mock_mm.update_calls if c["category_id"] is not None]
        hides = [c for c in mock_mm.update_calls if c["hide_from_reports"] is not None]

        assert len(merchants) == 2
        assert len(categories) == 1
        assert len(hides) == 1


class TestBatchMerchantOptimization:
    """Test batch merchant update optimization for backends that support it."""

    async def test_batch_update_merchant_single_rename(self, data_manager, mock_mm):
        """Test batch optimization for multiple transactions with same merchant rename."""
        # Create multiple transactions with same old merchant name
        txn_id_1 = mock_mm.add_test_transaction(merchant={"id": "m1", "name": "Amazon.com/abc123"})
        txn_id_2 = mock_mm.add_test_transaction(merchant={"id": "m1", "name": "Amazon.com/abc123"})
        txn_id_3 = mock_mm.add_test_transaction(merchant={"id": "m1", "name": "Amazon.com/abc123"})

        # Create edits to rename all 3 to "Amazon"
        edits = [
            TransactionEdit(txn_id_1, "merchant", "Amazon.com/abc123", "Amazon", datetime.now()),
            TransactionEdit(txn_id_2, "merchant", "Amazon.com/abc123", "Amazon", datetime.now()),
            TransactionEdit(txn_id_3, "merchant", "Amazon.com/abc123", "Amazon", datetime.now()),
        ]

        # Reset update call tracking
        mock_mm.reset_update_calls()

        # Commit edits
        success, failure = await data_manager.commit_pending_edits(edits)

        # Should use batch update (1 batch call) instead of 3 individual calls
        assert success == 3
        assert failure == 0

        # Verify NO individual transaction updates were made (used batch instead)
        assert len(mock_mm.update_calls) == 0

        # Verify all transactions were updated
        assert mock_mm.get_transaction_by_id(txn_id_1)["merchant"]["name"] == "Amazon"
        assert mock_mm.get_transaction_by_id(txn_id_2)["merchant"]["name"] == "Amazon"
        assert mock_mm.get_transaction_by_id(txn_id_3)["merchant"]["name"] == "Amazon"

    async def test_batch_update_multiple_merchant_groups(self, data_manager, mock_mm):
        """Test batch optimization with multiple different merchant renames."""
        # Create transactions with different old merchant names
        txn_id_1 = mock_mm.add_test_transaction(merchant={"id": "m1", "name": "Amazon.com/abc"})
        txn_id_2 = mock_mm.add_test_transaction(merchant={"id": "m1", "name": "Amazon.com/abc"})
        txn_id_3 = mock_mm.add_test_transaction(merchant={"id": "m2", "name": "Starbucks #123"})
        txn_id_4 = mock_mm.add_test_transaction(merchant={"id": "m2", "name": "Starbucks #123"})

        # Create edits for two different rename groups
        edits = [
            TransactionEdit(txn_id_1, "merchant", "Amazon.com/abc", "Amazon", datetime.now()),
            TransactionEdit(txn_id_2, "merchant", "Amazon.com/abc", "Amazon", datetime.now()),
            TransactionEdit(txn_id_3, "merchant", "Starbucks #123", "Starbucks", datetime.now()),
            TransactionEdit(txn_id_4, "merchant", "Starbucks #123", "Starbucks", datetime.now()),
        ]

        mock_mm.reset_update_calls()

        success, failure = await data_manager.commit_pending_edits(edits)

        # Should succeed for all 4 transactions
        assert success == 4
        assert failure == 0

        # Verify batch updates were used (no individual transaction calls)
        assert len(mock_mm.update_calls) == 0

        # Verify all transactions updated correctly
        assert mock_mm.get_transaction_by_id(txn_id_1)["merchant"]["name"] == "Amazon"
        assert mock_mm.get_transaction_by_id(txn_id_2)["merchant"]["name"] == "Amazon"
        assert mock_mm.get_transaction_by_id(txn_id_3)["merchant"]["name"] == "Starbucks"
        assert mock_mm.get_transaction_by_id(txn_id_4)["merchant"]["name"] == "Starbucks"

    async def test_batch_update_mixed_with_other_edits(self, data_manager, mock_mm):
        """Test that merchant batch updates work alongside category/hide edits."""
        # Add transactions
        txn_id_1 = mock_mm.add_test_transaction(merchant={"id": "m1", "name": "Amazon.com/abc"})
        txn_id_2 = mock_mm.add_test_transaction(merchant={"id": "m1", "name": "Amazon.com/abc"})

        # Create mixed edits: merchant renames + category change + hide toggle
        edits = [
            TransactionEdit(txn_id_1, "merchant", "Amazon.com/abc", "Amazon", datetime.now()),
            TransactionEdit(txn_id_2, "merchant", "Amazon.com/abc", "Amazon", datetime.now()),
            TransactionEdit("txn_1", "category", "cat_groceries", "cat_shopping", datetime.now()),
            TransactionEdit("txn_2", "hide_from_reports", False, True, datetime.now()),
        ]

        mock_mm.reset_update_calls()

        success, failure = await data_manager.commit_pending_edits(edits)

        # All 4 edits should succeed
        assert success == 4
        assert failure == 0

        # Only 2 individual transaction updates (for category and hide, not merchant)
        assert len(mock_mm.update_calls) == 2

        # Verify merchants were batch updated
        assert mock_mm.get_transaction_by_id(txn_id_1)["merchant"]["name"] == "Amazon"
        assert mock_mm.get_transaction_by_id(txn_id_2)["merchant"]["name"] == "Amazon"

        # Verify other edits were processed individually
        assert mock_mm.get_transaction_by_id("txn_1")["category"]["id"] == "cat_shopping"
        assert mock_mm.get_transaction_by_id("txn_2")["hideFromReports"] is True

    async def test_batch_update_fallback_on_failure(self, data_manager, mock_mm):
        """Test fallback to individual updates when batch update fails."""
        # Add transactions first
        txn_id_1 = mock_mm.add_test_transaction(merchant={"id": "m1", "name": "Amazon.com/abc"})
        txn_id_2 = mock_mm.add_test_transaction(merchant={"id": "m1", "name": "Amazon.com/abc"})

        # Create a mock backend that will fail batch updates
        class MockBackendWithFailingBatch:
            """Mock backend where batch_update_merchant always fails."""

            def __init__(self, mm):
                self.mm = mm

            def batch_update_merchant(self, old_name, new_name):
                return {"success": False, "message": "Simulated batch failure"}

            async def update_transaction(self, **kwargs):
                return await self.mm.update_transaction(**kwargs)

        # Wrap the mock backend
        original_mm = data_manager.mm
        wrapped_mm = MockBackendWithFailingBatch(original_mm)

        # Give it both batch and update methods
        for attr in dir(original_mm):
            if not hasattr(wrapped_mm, attr) and not attr.startswith("_"):
                setattr(wrapped_mm, attr, getattr(original_mm, attr))

        data_manager.mm = wrapped_mm

        # Create edits using the actual transaction IDs
        edits = [
            TransactionEdit(txn_id_1, "merchant", "Amazon.com/abc", "Amazon", datetime.now()),
            TransactionEdit(txn_id_2, "merchant", "Amazon.com/abc", "Amazon", datetime.now()),
        ]

        original_mm.reset_update_calls()

        success, failure = await data_manager.commit_pending_edits(edits)

        # Should fall back to individual updates
        assert success == 2
        assert failure == 0

        # Verify individual transaction updates were made (fallback path)
        assert len(original_mm.update_calls) == 2

        # Restore original backend
        data_manager.mm = original_mm

    async def test_batch_update_with_nonexistent_merchant(self, data_manager, mock_mm):
        """Test batch update gracefully handles nonexistent merchant names."""
        # Use existing transactions but try to rename a merchant that doesn't exist
        # The batch update will return "not found" and fall back to individual updates
        # Individual updates will also fail (merchant doesn't match)
        edits = [
            TransactionEdit(
                "txn_1", "merchant", "NonExistent Merchant", "New Name", datetime.now()
            ),
            TransactionEdit(
                "txn_2", "merchant", "NonExistent Merchant", "New Name", datetime.now()
            ),
        ]

        mock_mm.reset_update_calls()

        success, failure = await data_manager.commit_pending_edits(edits)

        # Batch update will fail (merchant not found), falls back to individual updates
        # Individual updates succeed (they update the transactions with txn_1 and txn_2 IDs)
        assert success == 2
        assert failure == 0

        # Verify individual transaction updates were made (fallback path)
        assert len(mock_mm.update_calls) == 2

    async def test_no_batch_update_for_backends_without_support(self, data_manager):
        """Test that backends without batch_update_merchant use individual updates."""

        # Create a mock backend without batch_update_merchant
        class MockBackendNoBatch:
            """Mock backend without batch update support."""

            def __init__(self):
                self.update_calls = []

            async def update_transaction(self, **kwargs):
                self.update_calls.append(kwargs)
                return {"updateTransaction": {"transaction": {"id": kwargs["transaction_id"]}}}

        # Replace backend
        original_mm = data_manager.mm
        no_batch_mm = MockBackendNoBatch()
        data_manager.mm = no_batch_mm

        # Create edits
        edits = [
            TransactionEdit("txn_1", "merchant", "Old", "New", datetime.now()),
            TransactionEdit("txn_2", "merchant", "Old", "New", datetime.now()),
        ]

        success, failure = await data_manager.commit_pending_edits(edits)

        # Should use individual updates
        assert success == 2
        assert failure == 0
        assert len(no_batch_mm.update_calls) == 2

        # Restore original backend
        data_manager.mm = original_mm


class TestFetchTransactionsPagination:
    """Test transaction fetching with pagination."""

    async def test_fetch_all_transactions_single_batch(self, data_manager):
        """Test fetching when all transactions fit in one batch."""
        transactions = await data_manager._fetch_all_transactions()

        assert len(transactions) > 0
        # Mock backend has 6 transactions, all fit in default batch

    async def test_fetch_with_progress_updates(self, data_manager):
        """Test progress updates during transaction fetching."""
        progress_messages = []

        def progress_callback(msg: str):
            progress_messages.append(msg)

        transactions = await data_manager._fetch_all_transactions(
            progress_callback=progress_callback
        )

        assert len(transactions) > 0
        assert len(progress_messages) > 0
        # Should see "Downloaded X transactions" messages
        assert any("Downloaded" in msg for msg in progress_messages)

    async def test_fetch_with_date_filters(self, data_manager):
        """Test fetching with start and end date filters."""
        transactions = await data_manager._fetch_all_transactions(
            start_date="2024-10-02", end_date="2024-10-03"
        )

        assert len(transactions) > 0
        # All transactions should be within date range
        for txn in transactions:
            assert "2024-10-02" <= txn["date"] <= "2024-10-03"

    async def test_fetch_alternative_results_format(self, mock_mm, tmp_path):
        """Test fetching with alternative results format (bare 'results' key)."""
        # Temporarily change mock to return bare 'results' format
        original_get_transactions = mock_mm.get_transactions

        async def alternate_format_get_transactions(*args, **kwargs):
            result = await original_get_transactions(*args, **kwargs)
            # Return in alternate format: {"results": [...]} instead of {"allTransactions": {"results": [...]}}
            if "allTransactions" in result:
                return {"results": result["allTransactions"]["results"]}
            return result

        mock_mm.get_transactions = alternate_format_get_transactions

        await mock_mm.login()
        dm = DataManager(mock_mm, config_dir=str(tmp_path))

        transactions = await dm._fetch_all_transactions()

        assert len(transactions) > 0
        assert len(transactions) == 6  # All mock transactions

    async def test_fetch_empty_results(self, mock_mm, tmp_path):
        """Test fetching when API returns empty results."""
        # Clear all transactions
        mock_mm.transactions = []

        await mock_mm.login()
        dm = DataManager(mock_mm, config_dir=str(tmp_path))

        transactions = await dm._fetch_all_transactions()

        assert len(transactions) == 0

    async def test_fetch_progress_without_total_count(self, mock_mm, tmp_path):
        """Test progress callback when total count is not available."""
        # Modify mock to not include totalCount
        original_get_transactions = mock_mm.get_transactions

        async def no_total_count_get_transactions(*args, **kwargs):
            result = await original_get_transactions(*args, **kwargs)
            # Remove totalCount from response
            if "allTransactions" in result:
                result["allTransactions"].pop("totalCount", None)
            return result

        mock_mm.get_transactions = no_total_count_get_transactions

        await mock_mm.login()
        dm = DataManager(mock_mm, config_dir=str(tmp_path))

        progress_messages = []

        def progress_callback(msg: str):
            progress_messages.append(msg)

        transactions = await dm._fetch_all_transactions(progress_callback=progress_callback)

        assert len(transactions) > 0
        # Should have progress messages without percentage
        assert any("Downloaded" in msg and "%" not in msg for msg in progress_messages)


class TestCategoryMappingRefresh:
    """
    Regression test for category mapping refresh bug.

    Bug: category_to_group mapping was built once from stale config.yaml
    and never updated after fetching fresh categories from API, causing
    transfers to not be filtered correctly.
    """

    async def test_category_mapping_refreshes_after_fetch(self, mock_mm, tmp_path):
        """
        Test that category_to_group mapping is rebuilt after fetching fresh categories.

        Bug scenario:
        1. config.yaml has stale/incomplete categories (missing Transfers)
        2. DataManager.__init__() loads stale categories, builds mapping
        3. fetch_all_data() fetches fresh categories (including Transfers)
        4. category_to_group mapping should be updated to include Transfers

        Without the fix, transfers wouldn't be filtered correctly.
        """
        import yaml

        config_dir = tmp_path / "config"
        config_dir.mkdir()
        config_path = config_dir / "config.yaml"

        # Create stale config.yaml with ONLY 2 groups (missing Transfers)
        stale_config = {
            "version": 1,
            "fetched_categories": {
                "Food & Dining": ["Groceries", "Restaurants"],
                "Shopping": ["Clothing"],
            },
        }

        with open(config_path, "w") as f:
            yaml.dump(stale_config, f)

        # Login to mock backend
        await mock_mm.login()

        # Initialize DataManager with stale config
        dm = DataManager(mock_mm, config_dir=str(config_dir))

        # Verify initial mapping is stale (only has 2 groups, no Transfers)
        initial_mapping = dm.category_to_group
        assert "Transfers" not in set(initial_mapping.values())

        # Fetch all data (should fetch fresh categories from API and save to config.yaml)
        df, categories, category_groups = await dm.fetch_all_data()

        # Verify fresh categories were saved to config.yaml
        with open(config_path, "r") as f:
            saved_config = yaml.safe_load(f)

        assert "fetched_categories" in saved_config
        # Mock backend returns 3 groups (Food & Dining, Auto & Transport, Shopping)
        assert len(saved_config["fetched_categories"]) == 3

        # Verify category_to_group mapping was rebuilt with fresh data
        updated_mapping = dm.category_to_group

        # Verify categories from mock backend are now in the mapping
        assert updated_mapping.get("Groceries") == "Food & Dining"
        assert updated_mapping.get("Gas") == "Auto & Transport"

        # Verify mapping has all 3 groups from mock backend
        assert len(set(updated_mapping.values())) == 3

    async def test_categories_get_correct_group_in_dataframe(self, mock_mm, tmp_path):
        """
        Test that transactions get correct group after mapping refresh.

        User-facing symptom: categories should be correctly mapped to groups
        so filtering works properly.
        """
        import yaml

        config_dir = tmp_path / "config"
        config_dir.mkdir()
        config_path = config_dir / "config.yaml"

        # Create stale config with wrong mappings
        stale_config = {
            "version": 1,
            "fetched_categories": {
                "Wrong Group": ["Groceries", "Gas"],  # Both in wrong group
            },
        }

        with open(config_path, "w") as f:
            yaml.dump(stale_config, f)

        # Login to mock backend
        await mock_mm.login()

        # Initialize DataManager with stale config
        dm = DataManager(mock_mm, config_dir=str(config_dir))

        # Fetch all data (refreshes categories and rebuilds mapping)
        df, _, _ = await dm.fetch_all_data()

        # Verify Groceries transactions have correct group
        groceries_txns = df.filter(df["category"] == "Groceries")
        if len(groceries_txns) > 0:
            groups = groceries_txns["group"].unique().to_list()
            assert "Food & Dining" in groups
            assert "Wrong Group" not in groups

        # Verify Gas transactions have correct group
        gas_txns = df.filter(df["category"] == "Gas")
        if len(gas_txns) > 0:
            groups = gas_txns["group"].unique().to_list()
            assert "Auto & Transport" in groups
            assert "Wrong Group" not in groups

    async def test_category_mapping_correct_after_restart_with_cache(self, mock_mm, tmp_path):
        """
        Test that categories work correctly when loading from cache after app restart.

        Regression test for bug where transfers would appear when loading from cache.
        The root cause was that tests were corrupting the user's config.yaml by
        not using isolated config directories.

        This test verifies:
        1. fetch_all_data() updates config.yaml with fresh categories from API
        2. After app restart, DataManager.__init__() loads updated config.yaml
        3. apply_category_groups() works correctly with the mapping from config.yaml
        """
        import yaml

        config_dir = tmp_path / "config"
        config_dir.mkdir()
        config_path = config_dir / "config.yaml"

        # Scenario: config.yaml starts with incomplete categories (before first API fetch)
        initial_config = {
            "version": 1,
            "fetched_categories": {
                "Food & Dining": ["Groceries", "Restaurants"],
                "Shopping": ["Clothing"],
                # NOTE: Missing "Auto & Transport" group with "Gas" category
            },
        }

        with open(config_path, "w") as f:
            yaml.dump(initial_config, f)

        # Login to mock backend
        await mock_mm.login()

        # Initialize DataManager (loads incomplete categories from config.yaml)
        dm = DataManager(mock_mm, config_dir=str(config_dir))

        # Verify initial mapping is incomplete (missing Auto & Transport)
        initial_mapping = dm.category_to_group
        assert initial_mapping.get("Gas") is None  # Not in mapping

        # Fetch data from API (saves fresh categories to config.yaml)
        df_from_api, _, _ = await dm.fetch_all_data()

        # Verify fresh categories were saved to config.yaml
        with open(config_path, "r") as f:
            saved_config = yaml.safe_load(f)
        assert len(saved_config["fetched_categories"]) == 3  # Now has all 3 groups

        # Verify mapping was rebuilt by fetch_all_data()
        assert dm.category_to_group.get("Gas") == "Auto & Transport"

        # ========== SIMULATE APP RESTART WITH CACHE ==========

        # Simulate app restart: Create NEW DataManager instance (like when app restarts)
        dm_after_restart = DataManager(mock_mm, config_dir=str(config_dir))

        # DataManager.__init__() should load the UPDATED config.yaml
        # This is the KEY FIX: config.yaml must be protected from test corruption
        assert dm_after_restart.category_to_group.get("Gas") == "Auto & Transport"

        # Apply category groups (simulates what app.py does when loading from cache)
        df_from_cache = df_from_api.clone()  # Simulate cached DataFrame
        df_with_groups = dm_after_restart.apply_category_groups(df_from_cache)

        # Verify Gas transactions have correct group
        gas_txns = df_with_groups.filter(df_with_groups["category"] == "Gas")
        if len(gas_txns) > 0:
            assert gas_txns["group"].unique().to_list()[0] == "Auto & Transport"


class TestTimeAggregation:
    """Test time-based aggregation."""

    async def test_aggregate_by_time_year_basic(self, data_manager):
        """Test basic year aggregation."""
        # Create test data spanning multiple years
        df = pl.DataFrame(
            {
                "id": ["1", "2", "3", "4"],
                "date": [
                    datetime(2023, 6, 15),
                    datetime(2024, 3, 10),
                    datetime(2024, 8, 20),
                    datetime(2025, 1, 5),
                ],
                "amount": [-100.0, -200.0, -150.0, -50.0],
                "hideFromReports": [False, False, False, False],
            }
        )

        result = data_manager.aggregate_by_time(df, TimeGranularity.YEAR)

        # Should have 3 years: 2023, 2024, 2025
        assert len(result) == 3
        assert set(result["year"].to_list()) == {2023, 2024, 2025}

        # Check 2024 has 2 transactions
        year_2024 = result.filter(pl.col("year") == 2024)
        assert year_2024["count"][0] == 2
        assert year_2024["total"][0] == -350.0

    async def test_aggregate_by_time_month_basic(self, data_manager):
        """Test basic month aggregation."""
        df = pl.DataFrame(
            {
                "id": ["1", "2", "3"],
                "date": [
                    datetime(2024, 1, 15),
                    datetime(2024, 1, 20),
                    datetime(2024, 3, 10),
                ],
                "amount": [-100.0, -50.0, -200.0],
                "hideFromReports": [False, False, False],
            }
        )

        result = data_manager.aggregate_by_time(df, TimeGranularity.MONTH)

        # Should have months
        assert len(result) >= 2  # At least Jan and Mar

        # Check January 2024 has 2 transactions
        jan_2024 = result.filter((pl.col("year") == 2024) & (pl.col("month") == 1))
        assert len(jan_2024) == 1
        assert jan_2024["count"][0] == 2
        assert jan_2024["total"][0] == -150.0

    async def test_aggregate_by_time_fills_year_gaps(self, data_manager):
        """Test that gaps in years are filled with zeros."""
        df = pl.DataFrame(
            {
                "id": ["1", "2"],
                "date": [datetime(2023, 1, 1), datetime(2025, 1, 1)],
                "amount": [-100.0, -200.0],
                "hideFromReports": [False, False],
            }
        )

        result = data_manager.aggregate_by_time(df, TimeGranularity.YEAR)

        # Should have 3 years with 2024 filled as zero
        assert len(result) == 3
        years = result["year"].to_list()
        assert sorted(years) == [2023, 2024, 2025]

        # 2024 should have zero count and total
        year_2024 = result.filter(pl.col("year") == 2024)
        assert year_2024["count"][0] == 0
        assert year_2024["total"][0] == 0.0

    async def test_aggregate_by_time_fills_month_gaps(self, data_manager):
        """Test that gaps in months are filled with zeros."""
        df = pl.DataFrame(
            {
                "id": ["1", "2"],
                "date": [datetime(2024, 1, 1), datetime(2024, 3, 1)],
                "amount": [-100.0, -200.0],
                "hideFromReports": [False, False],
            }
        )

        result = data_manager.aggregate_by_time(df, TimeGranularity.MONTH)

        # Should have all months from Jan to Mar (3 months)
        assert len(result) == 3

        # February should be filled with zeros
        feb_2024 = result.filter((pl.col("year") == 2024) & (pl.col("month") == 2))
        assert len(feb_2024) == 1
        assert feb_2024["count"][0] == 0
        assert feb_2024["total"][0] == 0.0

    async def test_aggregate_by_time_empty_dataframe(self, data_manager):
        """Test aggregating empty DataFrame."""
        df = pl.DataFrame()
        result = data_manager.aggregate_by_time(df, TimeGranularity.YEAR)
        assert result.is_empty()

    async def test_aggregate_by_time_excludes_hidden_from_total(self, data_manager):
        """Test that hidden transactions are excluded from totals but included in count."""
        df = pl.DataFrame(
            {
                "id": ["1", "2", "3"],
                "date": [datetime(2024, 1, 1), datetime(2024, 1, 2), datetime(2024, 1, 3)],
                "amount": [-100.0, -200.0, -50.0],
                "hideFromReports": [False, True, False],
            }
        )

        result = data_manager.aggregate_by_time(df, TimeGranularity.YEAR)

        year_2024 = result.filter(pl.col("year") == 2024)
        assert year_2024["count"][0] == 3  # All 3 counted
        assert year_2024["total"][0] == -150.0  # Only non-hidden: -100 + -50
