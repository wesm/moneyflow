"""
Tests for DataManager operations including aggregation, filtering, and API integration.
"""

from datetime import datetime

import polars as pl
import pytest

from moneyflow.data.categories import load_pending_category_aliases
from moneyflow.data.data_manager import (
    DataManager,
    flush_category_aliases,
    flush_category_aliases_durably,
)
from moneyflow.data.state import TimeGranularity, TransactionEdit


class MockBackendWithFailingBatch:
    """Mock backend where batch_update_merchant always fails."""

    def __init__(self, mm):
        self.mm = mm

    def batch_update_merchant(self, old_name, new_name):
        return {"success": False, "message": "Simulated batch failure"}

    async def update_transaction(self, **kwargs):
        return await self.mm.update_transaction(**kwargs)


class MockBackendNoBatch:
    """Mock backend without batch update support."""

    def __init__(self):
        self.update_calls = []

    async def update_transaction(self, **kwargs):
        self.update_calls.append(kwargs)
        # Keep consistent with original inline mock
        return {"updateTransaction": {"transaction": {"id": kwargs["transaction_id"]}}}


class MockBackendNoCount:
    """Mock backend without get_transaction_count_by_merchant."""

    pass


class StrictCategoryBackend:
    """Backend whose client does not accept backend-specific category names."""

    def __init__(self):
        self.update_calls = []

    def get_backend_type(self):
        return "monarch"

    async def update_transaction(
        self,
        transaction_id,
        merchant_name=None,
        category_id=None,
        hide_from_reports=None,
    ):
        self.update_calls.append(
            {
                "transaction_id": transaction_id,
                "merchant_name": merchant_name,
                "category_id": category_id,
                "hide_from_reports": hide_from_reports,
            }
        )
        return {"updateTransaction": {"transaction": {"id": transaction_id}}}


class CategoryNameBackend(StrictCategoryBackend):
    """Backend that persists local display category names."""

    def get_backend_type(self):
        return "simplefin"

    async def update_transaction(
        self,
        transaction_id,
        merchant_name=None,
        category_id=None,
        hide_from_reports=None,
        category_name=None,
    ):
        self.update_calls.append(
            {
                "transaction_id": transaction_id,
                "merchant_name": merchant_name,
                "category_id": category_id,
                "hide_from_reports": hide_from_reports,
                "category_name": category_name,
            }
        )
        return {"updateTransaction": {"transaction": {"id": transaction_id}}}


class MockBackendWithCount:
    """Mock backend that returns specific counts."""

    def __init__(self, count_mapping=None):
        self.count_mapping = count_mapping or {}

    def get_transaction_count_by_merchant(self, merchant_name):
        if isinstance(self.count_mapping, dict):
            return self.count_mapping.get(merchant_name, 0)
        return self.count_mapping


def make_transaction(**overrides):
    base = {
        "id": "txn_1",
        "date": "2024-10-01",
        "amount": -50.00,
        "merchant": {"id": "merch_1", "name": "Store"},
        "category": {"id": "cat_1", "name": "Groceries"},
        "account": {"id": "acc_1", "displayName": "Checking"},
        "notes": "",
        "hideFromReports": False,
        "pending": False,
        "isRecurring": False,
    }
    return {**base, **overrides}


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

    async def test_fetch_unfiltered_transactions_ignores_startup_date_range(self, data_manager):
        filtered_df, categories, _ = await data_manager.fetch_all_data(
            start_date="2024-10-02",
            end_date="2024-10-03",
        )
        data_manager.categories = categories

        complete_df = await data_manager.fetch_unfiltered_transactions()

        assert len(filtered_df) < len(complete_df)
        assert len(complete_df) == 6

    async def test_csv_backend_groups_recovered_from_profile_config(self, tmp_path):
        """CSV backend returns categories with no group name. The DataManager
        must fall back to the profile's category config so the category picker
        has usable group names instead of None for every category."""

        from moneyflow.backends.csv_backend import CsvFinanceBackend
        from moneyflow.data.categories import save_categories_to_profile

        profile_dir = tmp_path / "profile"
        profile_dir.mkdir()
        config_dir = tmp_path / "config"
        config_dir.mkdir()
        # Seed the profile config so the DataManager has a category_to_group
        # mapping to fall back to.
        save_categories_to_profile(
            {"Shopping": ["EXAMPLE STORE"]},
            profile_dir=profile_dir,
        )

        backend = CsvFinanceBackend(
            profile_dir=profile_dir,
            config_dir=str(config_dir),
            institution_name="chase_credit",
        )
        # Import one transaction so the backend has a category to surface.
        from moneyflow.importers.engine import InstitutionMapping, import_csv

        mapping = InstitutionMapping(
            name="chase_credit",
            display_name="Chase Credit Card",
            file_pattern="Chase*.csv",
            id_prefix="chase_",
            date_fmt="%m/%d/%Y",
            column_map={
                "Transaction Date": "date",
                "Description": "merchant",
                "Amount": "amount",
                "Category": "category",
            },
            amount_sign=1,
            skip_rows=0,
            dedup_fields=("date", "amount", "merchant"),
            extra_columns=(),
            date_columns=("date",),
            currency="USD",
            default_category="Uncategorized",
            default_category_id="cat_uncategorized",
            encoding="utf-8",
            debit_column=None,
            credit_column=None,
        )
        csv_dir = tmp_path / "csvs"
        csv_dir.mkdir()
        (csv_dir / "Chase_sample.csv").write_text(
            "Transaction Date,Description,Category,Amount\n"
            "01/15/2024,EXAMPLE STORE,EXAMPLE STORE,-25.00\n"
        )
        import_csv(str(csv_dir), mapping, backend)

        dm = DataManager(
            backend,
            config_dir=str(config_dir),
            profile_dir=profile_dir,
            backend_type="csv",
        )
        _, categories, _ = await dm.fetch_all_data()
        # Find the category by name (ids are hash-derived).
        by_name = {v["name"]: v for v in categories.values()}
        assert "EXAMPLE STORE" in by_name
        # Group name must be recovered from the profile config, not None.
        assert by_name["EXAMPLE STORE"]["group"] == "Shopping"


class TestAggregation:
    """Test data aggregation functions."""

    async def test_aggregate_by_merchant(self, loaded_data_manager):
        """Test merchant aggregation."""
        dm, df, _, _ = loaded_data_manager

        agg = dm.aggregate_by_merchant(df)

        assert len(agg) > 0
        assert "merchant" in agg.columns
        assert "count" in agg.columns
        assert "amount_in" in agg.columns
        assert "amount_out" in agg.columns
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
        """Test top category is based on spending amount, not transaction count."""
        dm = DataManager(mock_mm, config_dir=str(tmp_path))

        # Starbucks: 2 Coffee Shops at $50 each = $100, 8 Groceries at $5 each = $40
        # By count: Groceries would win (80%)
        # By spending: Coffee Shops wins ($100 / $140 = 71%)
        df = pl.DataFrame(
            {
                "id": ["1", "2", "3", "4", "5", "6", "7", "8", "9", "10"],
                "merchant": ["Starbucks"] * 10,
                "merchant_id": ["m1"] * 10,
                "category": ["Coffee Shops"] * 2 + ["Groceries"] * 8,
                "amount": [-50.0] * 2 + [-5.0] * 8,
                "hideFromReports": [False] * 10,
            }
        )

        agg = dm.aggregate_by_merchant(df)

        assert len(agg) == 1
        row = agg.row(0, named=True)
        assert row["merchant"] == "Starbucks"
        assert row["count"] == 10
        # Top category is based on spending, not transaction count
        assert row["top_category"] == "Coffee Shops"
        assert row["top_category_pct"] == 71  # $100 / $140 = 71%

    async def test_aggregate_by_merchant_top_category_multiple_merchants(self, mock_mm, tmp_path):
        """Test top category with multiple merchants, percentage based on spending."""
        dm = DataManager(mock_mm, config_dir=str(tmp_path))

        # Whole Foods: All Groceries = 100%
        # Amazon: Shopping $25, Electronics $175 total
        #   By count: 2/3 = 67%
        #   By spending: $175 / $200 = 88%
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

        # Check Amazon - percentage is by spending, not count
        amz = agg.filter(pl.col("merchant") == "Amazon").row(0, named=True)
        assert amz["top_category"] == "Electronics"
        assert amz["top_category_pct"] == 88  # $175 / $200 = 87.5% -> 88%

    async def test_aggregate_by_merchant_top_category_excludes_hidden(self, mock_mm, tmp_path):
        """Test that hidden items are excluded from top category calculation."""
        dm = DataManager(mock_mm, config_dir=str(tmp_path))

        # Starbucks: 3 Coffee Shops (non-hidden) at $10 each = $30
        #           5 Groceries (hidden) at $20 each = $100
        # If hidden were included: Groceries would win by spending ($100 vs $30)
        # With hidden excluded: Coffee Shops is 100% of non-hidden spending
        df = pl.DataFrame(
            {
                "id": ["1", "2", "3", "4", "5", "6", "7", "8"],
                "merchant": ["Starbucks"] * 8,
                "merchant_id": ["m1"] * 8,
                "category": ["Coffee Shops"] * 3 + ["Groceries"] * 5,
                "amount": [-10.0] * 3 + [-20.0] * 5,
                "hideFromReports": [False] * 3 + [True] * 5,
            }
        )

        agg = dm.aggregate_by_merchant(df)

        assert len(agg) == 1
        row = agg.row(0, named=True)
        assert row["merchant"] == "Starbucks"
        # Total count includes all transactions
        assert row["count"] == 8
        # But top category is based only on non-hidden items
        assert row["top_category"] == "Coffee Shops"
        assert row["top_category_pct"] == 100  # All non-hidden spending is Coffee Shops

    async def test_aggregate_by_merchant_top_category_mixed_hidden(self, mock_mm, tmp_path):
        """Test top category with mix of hidden and non-hidden in same category."""
        dm = DataManager(mock_mm, config_dir=str(tmp_path))

        # Amazon: Electronics $100 (non-hidden), Electronics $50 (hidden)
        #         Shopping $80 (non-hidden)
        # Non-hidden only: Electronics $100, Shopping $80 = $180 total
        # Electronics percentage: $100 / $180 = 56%
        df = pl.DataFrame(
            {
                "id": ["1", "2", "3"],
                "merchant": ["Amazon"] * 3,
                "merchant_id": ["m1"] * 3,
                "category": ["Electronics", "Electronics", "Shopping"],
                "amount": [-100.0, -50.0, -80.0],
                "hideFromReports": [False, True, False],
            }
        )

        agg = dm.aggregate_by_merchant(df)

        assert len(agg) == 1
        row = agg.row(0, named=True)
        assert row["merchant"] == "Amazon"
        assert row["count"] == 3
        # Top category based on non-hidden spending only
        assert row["top_category"] == "Electronics"
        assert row["top_category_pct"] == 56  # $100 / $180 = 55.6% -> 56%

    async def test_aggregate_by_merchant_top_category_mixed_income_spending(
        self, mock_mm, tmp_path
    ):
        """Test top category percentage with mixed income and spending.

        Percentage is based on absolute values of all amounts, capturing total
        activity in each category regardless of direction.
        """
        dm = DataManager(mock_mm, config_dir=str(tmp_path))

        # Groceries: |-$100| + |+$30| = $130 absolute activity
        # Shopping: |-$50| = $50 absolute activity
        # Total absolute: $180
        # Groceries = $130 / $180 = 72%
        df = pl.DataFrame(
            {
                "id": ["1", "2", "3"],
                "merchant": ["Store"] * 3,
                "merchant_id": ["m1"] * 3,
                "category": ["Groceries", "Groceries", "Shopping"],
                "amount": [-100.0, 30.0, -50.0],
                "hideFromReports": [False, False, False],
            }
        )

        agg = dm.aggregate_by_merchant(df)

        assert len(agg) == 1
        row = agg.row(0, named=True)
        assert row["merchant"] == "Store"
        assert row["amount_in"] == 30.0
        assert row["amount_out"] == -150.0
        assert row["total"] == -120.0
        # Percentage based on absolute values (total activity)
        # Groceries: $130, Shopping: $50, total: $180
        # Groceries = $130 / $180 = 72%
        assert row["top_category"] == "Groceries"
        assert row["top_category_pct"] == 72

    async def test_aggregate_by_merchant_top_category_net_income(self, mock_mm, tmp_path):
        """Test top category when merchant has net income (positive total)."""
        dm = DataManager(mock_mm, config_dir=str(tmp_path))

        # Refunds: |+$200| = $200 absolute
        # Returns: |+$100| = $100 absolute
        # Fees: |-$50| = $50 absolute
        # Total absolute: $350
        # Refunds = $200 / $350 = 57%
        df = pl.DataFrame(
            {
                "id": ["1", "2", "3"],
                "merchant": ["Store"] * 3,
                "merchant_id": ["m1"] * 3,
                "category": ["Refunds", "Returns", "Fees"],
                "amount": [200.0, 100.0, -50.0],
                "hideFromReports": [False, False, False],
            }
        )

        agg = dm.aggregate_by_merchant(df)

        assert len(agg) == 1
        row = agg.row(0, named=True)
        assert row["merchant"] == "Store"
        # Percentage based on absolute values (total activity)
        # Refunds: $200, Returns: $100, Fees: $50, total: $350
        # Refunds = $200 / $350 = 57%
        assert row["top_category"] == "Refunds"
        assert row["top_category_pct"] == 57

    async def test_aggregate_by_category(self, loaded_data_manager):
        """Test category aggregation."""
        dm, df, _, _ = loaded_data_manager

        agg = dm.aggregate_by_category(df)

        assert len(agg) > 0
        assert "category" in agg.columns
        assert "count" in agg.columns
        assert "amount_in" in agg.columns
        assert "amount_out" in agg.columns
        assert "total" in agg.columns
        assert "group" in agg.columns

    async def test_aggregate_by_category_splits_visible_cash_flow(self, mock_mm, tmp_path):
        """Income, expenses, and net exclude hidden amounts while count includes them."""
        dm = DataManager(mock_mm, config_dir=str(tmp_path))
        df = pl.DataFrame(
            {
                "id": ["1", "2", "3", "4"],
                "category": ["Example"] * 4,
                "category_id": ["cat_example"] * 4,
                "group": ["Example Group"] * 4,
                "amount": [100.0, 25.0, -40.0, -500.0],
                "hideFromReports": [False, False, False, True],
            }
        )

        result = dm.aggregate_by_category(df)

        row = result.row(0, named=True)
        assert row["count"] == 4
        assert row["amount_in"] == 125.0
        assert row["amount_out"] == -40.0
        assert row["total"] == 85.0

    async def test_aggregate_by_group(self, loaded_data_manager):
        """Test group aggregation."""
        dm, df, _, _ = loaded_data_manager

        agg = dm.aggregate_by_group(df)

        assert len(agg) > 0
        assert "group" in agg.columns
        assert "count" in agg.columns
        assert "amount_in" in agg.columns
        assert "amount_out" in agg.columns
        assert "total" in agg.columns

    async def test_aggregate_by_account_exposes_cash_flow_columns(self, loaded_data_manager):
        """Account aggregation exposes the shared cash-flow schema."""
        dm, df, _, _ = loaded_data_manager

        agg = dm.aggregate_by_account(df)

        assert "account" in agg.columns
        assert "count" in agg.columns
        assert "amount_in" in agg.columns
        assert "amount_out" in agg.columns
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

    async def test_search_transactions_regex_literal(self, loaded_data_manager):
        """Test search with regex metacharacters treats them literally."""
        dm, df, _, _ = loaded_data_manager

        # Positive control: plain "Amazon" matches
        results = dm.search_transactions(df, "Amazon")
        assert len(results) > 0

        # Regex metacharacters must be treated as literal text
        for pattern in ["Amazon.*", "Amazon.+", "Ama(zon)", "[A]mazon", "Amazon$", "^Amazon"]:
            results = dm.search_transactions(df, pattern)
            assert len(results) == 0, f"Expected 0 results for literal {pattern!r}"


class TestCommitEdits:
    """Test committing pending edits to the API."""

    async def test_commit_single_edit(self, data_manager, mock_mm):
        """Test committing a single edit."""
        edits = [TransactionEdit("txn_1", "merchant", "Old Name", "New Name", datetime.now())]

        success, failure, _ = await data_manager.commit_pending_edits(edits)

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

        success, failure, _ = await data_manager.commit_pending_edits(edits)

        assert success == 3
        assert failure == 0
        assert len(mock_mm.update_calls) == 3

    async def test_commit_empty_edits(self, data_manager, mock_mm):
        """Test committing with no edits."""
        success, failure, _ = await data_manager.commit_pending_edits([])

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

    async def test_commit_category_change_omits_category_name_for_strict_backends(self, tmp_path):
        """Category display names must not leak into remote backend client calls."""
        backend = StrictCategoryBackend()
        dm = DataManager(backend, config_dir=str(tmp_path))
        dm.categories = {"cat_new": {"name": "New Category"}}

        edits = [TransactionEdit("txn_1", "category", "cat_old", "cat_new", datetime.now())]

        success, failure, _ = await dm.commit_pending_edits(edits)

        assert success == 1
        assert failure == 0
        assert backend.update_calls == [
            {
                "transaction_id": "txn_1",
                "merchant_name": None,
                "category_id": "cat_new",
                "hide_from_reports": None,
            }
        ]

    async def test_commit_category_change_includes_category_name_for_simplefin(self, tmp_path):
        """SimpleFIN stores both category ID and display name locally."""
        backend = CategoryNameBackend()
        dm = DataManager(backend, config_dir=str(tmp_path))
        dm.categories = {"cat_new": {"name": "New Category"}}

        edits = [TransactionEdit("txn_1", "category", "cat_old", "cat_new", datetime.now())]

        success, failure, _ = await dm.commit_pending_edits(edits)

        assert success == 1
        assert failure == 0
        assert backend.update_calls[0]["category_name"] == "New Category"

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
        assert df.schema == {
            "id": pl.String,
            "date": pl.Date,
            "amount": pl.Float64,
            "merchant": pl.String,
            "merchant_id": pl.String,
            "category": pl.String,
            "category_id": pl.String,
            "account": pl.String,
            "account_id": pl.String,
            "notes": pl.String,
            "hideFromReports": pl.Boolean,
            "pending": pl.Boolean,
            "isRecurring": pl.Boolean,
        }

    async def test_transactions_to_dataframe_none_merchant(self, data_manager):
        """Test transaction with None merchant field."""
        transactions = [make_transaction(merchant=None, notes="test")]

        df = data_manager._transactions_to_dataframe(transactions, {})

        assert len(df) == 1
        assert df["merchant"][0] == "Unknown"

    async def test_transactions_to_dataframe_empty_merchant_name(self, data_manager):
        """Test transaction with empty merchant name."""
        transactions = [make_transaction(merchant={"id": "merch_1", "name": ""}, notes=None)]

        df = data_manager._transactions_to_dataframe(transactions, {})

        assert len(df) == 1
        assert df["merchant"][0] == "Unknown"

    async def test_transactions_to_dataframe_none_category(self, data_manager):
        """Test transaction with None category field."""
        transactions = [make_transaction(category=None)]

        df = data_manager._transactions_to_dataframe(transactions, {})
        # Apply grouping (now done separately)
        df = data_manager.apply_category_groups(df)

        assert len(df) == 1
        assert df["category"][0] == "Uncategorized"
        assert df["group"][0] == "Uncategorized"

    async def test_transactions_to_dataframe_empty_category_name(self, data_manager):
        """Test transaction with empty category name."""
        transactions = [make_transaction(category={"id": "cat_1", "name": ""})]

        df = data_manager._transactions_to_dataframe(transactions, {})

        assert len(df) == 1
        assert df["category"][0] == "Uncategorized"

    async def test_transactions_to_dataframe_none_account(self, data_manager):
        """Test transaction with None account field."""
        transactions = [make_transaction(account=None)]

        df = data_manager._transactions_to_dataframe(transactions, {})

        assert len(df) == 1
        assert df["account"][0] == ""

    async def test_transactions_to_dataframe_empty_account_name(self, data_manager):
        """Test transaction with empty account display name."""
        transactions = [make_transaction(account={"id": "acc_1", "displayName": ""})]

        df = data_manager._transactions_to_dataframe(transactions, {})

        assert len(df) == 1
        assert df["account"][0] == ""

    async def test_transactions_to_dataframe_none_notes(self, data_manager):
        """Test transaction with None notes field."""
        transactions = [make_transaction(notes=None)]

        df = data_manager._transactions_to_dataframe(transactions, {})

        assert len(df) == 1
        assert df["notes"][0] == ""

    async def test_transactions_to_dataframe_missing_optional_fields(self, data_manager):
        """Test transaction with missing optional fields."""
        transactions = [make_transaction()]
        # Remove optional fields to test missing fields
        del transactions[0]["notes"]
        del transactions[0]["hideFromReports"]
        del transactions[0]["pending"]
        del transactions[0]["isRecurring"]

        df = data_manager._transactions_to_dataframe(transactions, {})

        assert len(df) == 1
        assert df["notes"][0] == ""
        assert df["hideFromReports"][0] is False
        assert df["pending"][0] is False
        assert df["isRecurring"][0] is False

    async def test_transactions_to_dataframe_unknown_category_group(self, data_manager):
        """Test transaction with category not in group mapping."""
        transactions = [
            make_transaction(category={"id": "cat_unknown", "name": "Unknown Category XYZ"})
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
        # New message format includes "Fetching all transactions..." or "Fetching transactions (date range)..."
        assert any("transactions" in msg and "Fetching" in msg for msg in progress_messages)
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

        success, failure, _ = await data_manager.commit_pending_edits(edits)

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

        success, failure, _ = await data_manager.commit_pending_edits(edits)

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

        success, failure, _ = await data_manager.commit_pending_edits(edits)

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
        success, failure, _ = await data_manager.commit_pending_edits(edits)

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

        success, failure, _ = await data_manager.commit_pending_edits(edits)

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

        success, failure, _ = await data_manager.commit_pending_edits(edits)

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

    async def test_batch_update_fallback_on_failure(self, data_manager, mock_mm, monkeypatch):
        """Test fallback to individual updates when batch update fails."""
        # Add transactions first
        txn_id_1 = mock_mm.add_test_transaction(merchant={"id": "m1", "name": "Amazon.com/abc"})
        txn_id_2 = mock_mm.add_test_transaction(merchant={"id": "m1", "name": "Amazon.com/abc"})
        # Wrap the mock backend
        original_mm = data_manager.mm
        wrapped_mm = MockBackendWithFailingBatch(original_mm)

        # Give it both batch and update methods
        for attr in dir(original_mm):
            if not hasattr(wrapped_mm, attr) and not attr.startswith("_"):
                setattr(wrapped_mm, attr, getattr(original_mm, attr))

        monkeypatch.setattr(data_manager, "mm", wrapped_mm)

        # Create edits using the actual transaction IDs
        edits = [
            TransactionEdit(txn_id_1, "merchant", "Amazon.com/abc", "Amazon", datetime.now()),
            TransactionEdit(txn_id_2, "merchant", "Amazon.com/abc", "Amazon", datetime.now()),
        ]

        original_mm.reset_update_calls()

        success, failure, _ = await data_manager.commit_pending_edits(edits)

        # Should fall back to individual updates
        assert success == 2
        assert failure == 0

        # Verify individual transaction updates were made (fallback path)
        assert len(original_mm.update_calls) == 2

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

        success, failure, _ = await data_manager.commit_pending_edits(edits)

        # Batch update will fail (merchant not found), falls back to individual updates
        # Individual updates succeed (they update the transactions with txn_1 and txn_2 IDs)
        assert success == 2
        assert failure == 0

        # Verify individual transaction updates were made (fallback path)
        assert len(mock_mm.update_calls) == 2

    async def test_no_batch_update_for_backends_without_support(self, data_manager, monkeypatch):
        """Test that backends without batch_update_merchant use individual updates."""
        no_batch_mm = MockBackendNoBatch()
        monkeypatch.setattr(data_manager, "mm", no_batch_mm)

        # Create edits
        edits = [
            TransactionEdit("txn_1", "merchant", "Old", "New", datetime.now()),
            TransactionEdit("txn_2", "merchant", "Old", "New", datetime.now()),
        ]

        success, failure, _ = await data_manager.commit_pending_edits(edits)

        # Should use individual updates
        assert success == 2
        assert failure == 0
        assert len(no_batch_mm.update_calls) == 2


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
        assert year_2024["amount_in"][0] == 0.0
        assert year_2024["amount_out"][0] == -350.0
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
        assert year_2024["amount_in"][0] == 0.0
        assert year_2024["amount_out"][0] == 0.0
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
        assert feb_2024["amount_in"][0] == 0.0
        assert feb_2024["amount_out"][0] == 0.0
        assert feb_2024["total"][0] == 0.0

    async def test_aggregate_by_time_empty_dataframe(self, data_manager):
        """Test aggregating empty DataFrame."""
        df = pl.DataFrame()
        result = data_manager.aggregate_by_time(df, TimeGranularity.YEAR)
        assert result.is_empty()

    async def test_aggregate_by_time_splits_visible_cash_flow(self, data_manager):
        """Time cash flow excludes hidden amounts but count includes hidden transactions."""
        df = pl.DataFrame(
            {
                "id": ["1", "2", "3", "4"],
                "date": [
                    datetime(2024, 1, 1),
                    datetime(2024, 1, 2),
                    datetime(2024, 1, 3),
                    datetime(2024, 1, 4),
                ],
                "amount": [-100.0, -200.0, -50.0, 250.0],
                "hideFromReports": [False, True, False, False],
            }
        )

        result = data_manager.aggregate_by_time(df, TimeGranularity.YEAR)

        year_2024 = result.filter(pl.col("year") == 2024)
        assert year_2024["count"][0] == 4
        assert year_2024["amount_in"][0] == 250.0
        assert year_2024["amount_out"][0] == -150.0
        assert year_2024["total"][0] == 100.0


class TestCheckBatchScope:
    """Test check_batch_scope method for batch scope prompts."""

    async def test_check_batch_scope_no_merchant_edits(self, data_manager):
        """Test check_batch_scope returns empty when no merchant edits."""
        edits = [
            TransactionEdit("txn_1", "category", "old_cat", "new_cat", datetime.now()),
            TransactionEdit("txn_2", "hide_from_reports", False, True, datetime.now()),
        ]

        result = await data_manager.check_batch_scope(edits)

        assert result == {}

    async def test_check_batch_scope_backend_without_support(self, data_manager, monkeypatch):
        """Test check_batch_scope returns empty when backend doesn't support counting."""
        monkeypatch.setattr(data_manager, "mm", MockBackendNoCount())

        edits = [
            TransactionEdit("txn_1", "merchant", "Amazon.com/abc", "Amazon", datetime.now()),
        ]

        result = await data_manager.check_batch_scope(edits)

        assert result == {}

    async def test_check_batch_scope_count_equals_selected(self, data_manager, monkeypatch):
        """Test check_batch_scope returns empty when total equals selected."""
        monkeypatch.setattr(data_manager, "mm", MockBackendWithCount({"Amazon.com/abc": 2}))

        edits = [
            TransactionEdit("txn_1", "merchant", "Amazon.com/abc", "Amazon", datetime.now()),
            TransactionEdit("txn_2", "merchant", "Amazon.com/abc", "Amazon", datetime.now()),
        ]

        result = await data_manager.check_batch_scope(edits)

        assert result == {}

    async def test_check_batch_scope_count_more_than_selected(self, data_manager, monkeypatch):
        """Test check_batch_scope returns mismatch when total > selected."""
        monkeypatch.setattr(data_manager, "mm", MockBackendWithCount({"Amazon.com/abc": 5}))

        edits = [
            TransactionEdit("txn_1", "merchant", "Amazon.com/abc", "Amazon", datetime.now()),
            TransactionEdit("txn_2", "merchant", "Amazon.com/abc", "Amazon", datetime.now()),
        ]

        result = await data_manager.check_batch_scope(edits)

        assert ("Amazon.com/abc", "Amazon") in result
        assert result[("Amazon.com/abc", "Amazon")] == {"selected": 2, "total": 5}

    async def test_check_batch_scope_multiple_rename_groups(self, data_manager, monkeypatch):
        """Test check_batch_scope handles multiple merchant rename groups."""
        monkeypatch.setattr(
            data_manager, "mm", MockBackendWithCount({"Amazon.com/abc": 5, "Starbucks #123": 3})
        )

        edits = [
            TransactionEdit("txn_1", "merchant", "Amazon.com/abc", "Amazon", datetime.now()),
            TransactionEdit("txn_2", "merchant", "Amazon.com/abc", "Amazon", datetime.now()),
            TransactionEdit("txn_3", "merchant", "Starbucks #123", "Starbucks", datetime.now()),
            TransactionEdit("txn_4", "merchant", "Starbucks #123", "Starbucks", datetime.now()),
            TransactionEdit("txn_5", "merchant", "Starbucks #123", "Starbucks", datetime.now()),
        ]

        result = await data_manager.check_batch_scope(edits)

        # Only Amazon should have mismatch (5 total > 2 selected)
        assert ("Amazon.com/abc", "Amazon") in result
        assert result[("Amazon.com/abc", "Amazon")] == {"selected": 2, "total": 5}

        # Starbucks should NOT have mismatch (3 total == 3 selected)
        assert ("Starbucks #123", "Starbucks") not in result


class TestSkipBatchFor:
    """Test skip_batch_for parameter in commit_pending_edits."""

    async def test_skip_batch_for_uses_individual_updates(self, data_manager, mock_mm):
        """Test that skip_batch_for forces individual transaction updates."""
        # Add test transactions
        txn_id_1 = mock_mm.add_test_transaction(merchant={"id": "m1", "name": "Amazon.com/abc"})
        txn_id_2 = mock_mm.add_test_transaction(merchant={"id": "m1", "name": "Amazon.com/abc"})

        edits = [
            TransactionEdit(txn_id_1, "merchant", "Amazon.com/abc", "Amazon", datetime.now()),
            TransactionEdit(txn_id_2, "merchant", "Amazon.com/abc", "Amazon", datetime.now()),
        ]

        mock_mm.reset_update_calls()

        # Skip batch for this rename
        skip_batch_for = {("Amazon.com/abc", "Amazon")}
        success, failure, bulk_renames = await data_manager.commit_pending_edits(
            edits, skip_batch_for=skip_batch_for
        )

        assert success == 2
        assert failure == 0

        # Should use individual updates, NOT batch
        assert len(mock_mm.update_calls) == 2

        # Bulk renames should NOT include this rename (we skipped batch)
        assert ("Amazon.com/abc", "Amazon") not in bulk_renames

    async def test_skip_batch_for_partial(self, data_manager, mock_mm):
        """Test skip_batch_for only affects specified renames."""
        # Add test transactions for two different merchants
        txn_id_1 = mock_mm.add_test_transaction(merchant={"id": "m1", "name": "Amazon.com/abc"})
        txn_id_2 = mock_mm.add_test_transaction(merchant={"id": "m2", "name": "Starbucks #123"})

        edits = [
            TransactionEdit(txn_id_1, "merchant", "Amazon.com/abc", "Amazon", datetime.now()),
            TransactionEdit(txn_id_2, "merchant", "Starbucks #123", "Starbucks", datetime.now()),
        ]

        mock_mm.reset_update_calls()

        # Skip batch only for Amazon, let Starbucks use batch
        skip_batch_for = {("Amazon.com/abc", "Amazon")}
        success, failure, bulk_renames = await data_manager.commit_pending_edits(
            edits, skip_batch_for=skip_batch_for
        )

        assert success == 2
        assert failure == 0

        # Amazon should use individual (1 call), Starbucks should use batch (0 calls)
        assert len(mock_mm.update_calls) == 1

        # Starbucks should be in bulk renames, Amazon should not
        assert ("Starbucks #123", "Starbucks") in bulk_renames
        assert ("Amazon.com/abc", "Amazon") not in bulk_renames

    async def test_skip_batch_for_empty_set(self, data_manager, mock_mm):
        """Test empty skip_batch_for uses normal batch behavior."""
        txn_id_1 = mock_mm.add_test_transaction(merchant={"id": "m1", "name": "Amazon.com/abc"})
        txn_id_2 = mock_mm.add_test_transaction(merchant={"id": "m1", "name": "Amazon.com/abc"})

        edits = [
            TransactionEdit(txn_id_1, "merchant", "Amazon.com/abc", "Amazon", datetime.now()),
            TransactionEdit(txn_id_2, "merchant", "Amazon.com/abc", "Amazon", datetime.now()),
        ]

        mock_mm.reset_update_calls()

        # Empty skip set
        success, failure, bulk_renames = await data_manager.commit_pending_edits(
            edits, skip_batch_for=set()
        )

        assert success == 2
        assert failure == 0

        # Should use batch (0 individual calls)
        assert len(mock_mm.update_calls) == 0

        # Should be in bulk renames
        assert ("Amazon.com/abc", "Amazon") in bulk_renames

    async def test_skip_batch_for_none(self, data_manager, mock_mm):
        """Test None skip_batch_for uses normal batch behavior."""
        txn_id_1 = mock_mm.add_test_transaction(merchant={"id": "m1", "name": "Amazon.com/abc"})

        edits = [
            TransactionEdit(txn_id_1, "merchant", "Amazon.com/abc", "Amazon", datetime.now()),
        ]

        mock_mm.reset_update_calls()

        # None (default)
        success, failure, bulk_renames = await data_manager.commit_pending_edits(
            edits, skip_batch_for=None
        )

        assert success == 1
        assert failure == 0

        # Should use batch
        assert len(mock_mm.update_calls) == 0
        assert ("Amazon.com/abc", "Amazon") in bulk_renames


class TestFlushCategoryAliases:
    def test_removes_only_confirmed_aliases(self):
        """A persistence failure retains the failed alias and everything
        after it, so structural edits are never silently dropped."""
        recorded = []

        def flaky(source, target, name):
            if source == "cat_fail":
                raise RuntimeError("backend unavailable")
            recorded.append((source, target, name))

        backend = type("Backend", (), {"record_category_alias": staticmethod(flaky)})()
        aliases = [
            ("cat_ok", "cat_x", "X"),
            ("cat_fail", "cat_y", "Y"),
            ("cat_later", "cat_z", "Z"),
        ]
        remaining = flush_category_aliases(backend, aliases)
        assert recorded == [("cat_ok", "cat_x", "X")]
        assert remaining == [("cat_fail", "cat_y", "Y"), ("cat_later", "cat_z", "Z")]

    def test_backend_without_alias_support_has_nothing_to_retain(self):
        assert flush_category_aliases(object(), [("a", "b", "B")]) == []


class TestDurableAliasRetention:
    def test_unconfirmed_aliases_survive_restart(self, tmp_path):
        """An alias the backend fails to confirm is durably retained in the
        profile config, reloaded by a fresh DataManager, and cleared from
        storage once a retry succeeds."""

        def failing(*args):
            raise RuntimeError("backend unavailable")

        failing_backend = type("Backend", (), {"record_category_alias": staticmethod(failing)})()
        alias = ("cat_shopping", "cat_fun", "Fun")
        remaining = flush_category_aliases_durably(failing_backend, tmp_path, [alias])
        assert [record[:3] for record in remaining] == [alias]
        assert [record[:3] for record in load_pending_category_aliases(tmp_path)] == [alias]

        manager = DataManager(mm=failing_backend, config_dir=str(tmp_path), profile_dir=tmp_path)
        assert [record[:3] for record in manager.pending_category_aliases] == [alias]

        recorded = []
        working_backend = type(
            "Backend",
            (),
            {"record_category_alias": staticmethod(lambda *args: recorded.append(args))},
        )()
        assert flush_category_aliases_durably(working_backend, tmp_path, remaining) == []
        assert [record[:3] for record in recorded] == [alias]
        assert load_pending_category_aliases(tmp_path) == []


class TestAliasQueuePersistenceFailure:
    def test_confirmed_aliases_are_not_replayed_when_queue_write_fails(self, tmp_path, monkeypatch):
        """A queue-write failure must not cause confirmed aliases to be
        replayed later — they are already durable in the backend, and a
        replay could overwrite a newer mapping."""
        recorded = []
        backend = type(
            "Backend",
            (),
            {"record_category_alias": staticmethod(lambda *args: recorded.append(args))},
        )()
        monkeypatch.setattr(
            "moneyflow.data.data_manager.save_pending_category_aliases",
            lambda *args, **kwargs: False,
        )

        remaining = flush_category_aliases_durably(backend, tmp_path, [("cat_a", "cat_b", "B")])

        assert recorded == [("cat_a", "cat_b", "B")]
        assert remaining == []  # confirmed, so not queued for replay


class TestAliasQueueSelfCorrection:
    def test_confirmed_queue_entries_are_not_replayed(self, tmp_path):
        """If clearing the queue failed, the stale entries the backend
        already records must be dropped on reload rather than replayed over
        a newer mapping."""
        from moneyflow.data.categories import save_pending_category_aliases
        from moneyflow.data.data_manager import load_unconfirmed_category_aliases

        # The backend already records cat_a with a NEWER target than the
        # stale queue entry, plus an entry that was never confirmed.
        # The backend recorded cat_a at revision 7 (a NEWER operation than
        # the queued revision-3 entry) and cat_c at the queued revision.
        backend = type(
            "Backend",
            (),
            {"get_category_alias_revisions": staticmethod(lambda: {"cat_a": 7, "cat_c": 4})},
        )()
        save_pending_category_aliases(
            tmp_path,
            [
                ("cat_a", "cat_old", "Old", 3),  # older than the backend: stale
                ("cat_c", "cat_d", "D", 4),  # same revision: already confirmed
                ("cat_e", "cat_f", "F", 5),  # backend has no record: retry
            ],
        )

        unconfirmed = load_unconfirmed_category_aliases(backend, tmp_path)

        # Revisions survive the round trip so a later flush cannot restamp a
        # stale record with a fresh, higher number.
        assert unconfirmed == [("cat_e", "cat_f", "F", 5)]

    def test_unreadable_revisions_raise_rather_than_look_empty(self, tmp_path):
        """Without the backend's revisions staleness is undecidable. The
        caller must be able to tell that apart from an empty queue, or an
        import would proceed with a stale category map."""
        from moneyflow.data.categories import (
            load_pending_category_aliases,
            save_pending_category_aliases,
        )
        from moneyflow.data.data_manager import (
            AliasStateUnavailable,
            load_unconfirmed_category_aliases,
        )

        def failing():
            raise RuntimeError("database is locked")

        backend = type("Backend", (), {"get_category_alias_revisions": staticmethod(failing)})()
        save_pending_category_aliases(tmp_path, [("cat_a", "cat_b", "B", 2)])

        with pytest.raises(AliasStateUnavailable):
            load_unconfirmed_category_aliases(backend, tmp_path)
        # The queue survives for a later run to retry.
        assert load_pending_category_aliases(tmp_path) == [("cat_a", "cat_b", "B", 2)]

    def test_new_revisions_outrank_staged_ones(self, tmp_path):
        """A new alias must never get a lower revision than a queued one it
        supersedes, or the queued entry would win after a restart."""
        from moneyflow.data.data_manager import assign_alias_revisions

        backend = type("Backend", (), {"next_category_alias_revision": staticmethod(lambda: 2)})()
        stamped = assign_alias_revisions(
            backend, [("cat_queued", "cat_x", "X", 9), ("cat_new", "cat_y", "Y")]
        )

        assert stamped[0] == ("cat_queued", "cat_x", "X", 9)  # existing kept
        assert stamped[1][3] > 9  # new one outranks it


class TestBackendAwareCategoryIds:
    def test_populate_uses_backend_id_format(self, tmp_path):
        """Undo and rollback repopulate categories from config; using the
        legacy slug for a CSV backend would split one category across two
        ids ("groceries" and "cat_groceries")."""
        from moneyflow.data.categories import stable_category_id

        backend = type("Backend", (), {"make_category_id": staticmethod(stable_category_id)})()
        manager = DataManager(mm=backend, config_dir=str(tmp_path))
        manager.categories = {}
        manager.category_groups_config = {"Expenses": ["Groceries", "Food & Dining"]}

        manager._populate_categories_from_config()

        assert set(manager.categories) == {"cat_groceries", "cat_food_dining"}
        assert manager.categories["cat_groceries"]["group"] == "Expenses"

    def test_populate_falls_back_to_legacy_slug(self, tmp_path):
        """Backends without an id factory keep the legacy bare slug, whose
        format their stored transactions already use."""
        manager = DataManager(mm=object(), config_dir=str(tmp_path))
        manager.categories = {}
        manager.category_groups_config = {"Expenses": ["Groceries"]}

        manager._populate_categories_from_config()

        assert set(manager.categories) == {"groceries"}


class TestStagedCategoryStateRecovery:
    def test_staged_state_is_recovered_on_restart(self, tmp_path):
        """A restructure staged by a commit whose config write failed must be
        recovered, not lost, so it can be retried."""
        staged_groups = {"Group": ["Renamed"]}
        staged_aliases = [("cat_old", "cat_renamed", "Renamed", 3)]

        class Backend:
            @staticmethod
            def load_pending_category_state():
                return staged_groups, staged_aliases

        manager = DataManager(mm=Backend(), config_dir=str(tmp_path))

        assert manager.pending_category_groups == staged_groups
        assert manager.pending_category_aliases == staged_aliases


class TestStagedStateAfterSuccessfulRetry:
    def test_recorded_staged_aliases_are_not_requeued(self, tmp_path):
        """failure -> retry -> restart: once the retry recorded the aliases,
        a staged copy that outlived it must not be replayed, or it would
        restamp a stale mapping with a fresh revision."""

        class Backend:
            @staticmethod
            def load_pending_category_state():
                # Staged during the failed save, left behind afterwards.
                return {"Group": ["Renamed"]}, [("cat_old", "cat_renamed", "Renamed", 3)]

            @staticmethod
            def get_category_alias_revisions():
                # The retry recorded it at the same revision.
                return {"cat_old": 3}

        manager = DataManager(mm=Backend(), config_dir=str(tmp_path))

        assert manager.pending_category_aliases == []

    def test_unrecorded_staged_aliases_are_requeued(self, tmp_path):
        """A staged alias the backend never recorded is still retried."""

        class Backend:
            @staticmethod
            def load_pending_category_state():
                return {"Group": ["Renamed"]}, [("cat_old", "cat_renamed", "Renamed", 3)]

            @staticmethod
            def get_category_alias_revisions():
                return {}

        manager = DataManager(mm=Backend(), config_dir=str(tmp_path))

        assert manager.pending_category_aliases == [("cat_old", "cat_renamed", "Renamed", 3)]


class TestFailClosedAliasState:
    def test_unreadable_revisions_raise_from_shared_filter(self):
        """drop_recorded_aliases must fail closed: replaying a record whose
        staleness cannot be checked could overwrite a newer mapping."""
        from moneyflow.data.data_manager import AliasStateUnavailable, drop_recorded_aliases

        def failing():
            raise RuntimeError("database is locked")

        backend = type("Backend", (), {"get_category_alias_revisions": staticmethod(failing)})()

        with pytest.raises(AliasStateUnavailable):
            drop_recorded_aliases(backend, [("cat_a", "cat_b", "B", 1)])

    def test_unreadable_staged_state_aborts_initialization(self, tmp_path):
        """The staged state shares the transaction database, so an unreadable
        one means the profile cannot be used with confidence."""
        from moneyflow.data.data_manager import AliasStateUnavailable

        class Backend:
            @staticmethod
            def load_pending_category_state():
                raise RuntimeError("database is locked")

        with pytest.raises(AliasStateUnavailable):
            DataManager(mm=Backend(), config_dir=str(tmp_path), profile_dir=tmp_path)


class TestUnstampableAliases:
    def test_records_are_not_serialized_as_revision_zero(self, tmp_path):
        """If a revision cannot be allocated, the records must stay in memory
        rather than being queued unstamped: a stored revision 0 loses to any
        positive backend revision and the correction would be dropped."""
        from moneyflow.data.categories import load_pending_category_aliases
        from moneyflow.data.data_manager import flush_category_aliases_durably

        def failing():
            raise RuntimeError("database is locked")

        recorded = []
        backend = type(
            "Backend",
            (),
            {
                "next_category_alias_revision": staticmethod(failing),
                "record_category_alias": staticmethod(lambda *a: recorded.append(a)),
            },
        )()

        remaining = flush_category_aliases_durably(backend, tmp_path, [("cat_a", "cat_b", "B")])

        assert remaining == [("cat_a", "cat_b", "B")]  # retained for retry
        assert recorded == []  # nothing recorded without a revision
        assert load_pending_category_aliases(tmp_path) == []  # nothing queued as 0
