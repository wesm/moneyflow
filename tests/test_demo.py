"""
Tests for demo mode system - data generator and backend.

Ensures the demo mode provides realistic, testable data for showcasing
the TUI without requiring real finance account credentials.

Demo mode generates a full year of synthetic transactions for a dual-income
household, with realistic spending patterns, merchant variations, and
intentional edge cases for testing features.
"""

from typing import Dict, List

import pytest

from moneyflow.backends import DemoBackend
from moneyflow.demo_data_generator import DemoDataGenerator, generate_demo_data

# ============================================================================
# FIXTURES
# ============================================================================


@pytest.fixture
def demo_generator():
    """Provide a fresh DemoDataGenerator with fixed seed for reproducible tests."""
    return DemoDataGenerator(start_year=2025, years=1, seed=42)


@pytest.fixture
def demo_backend():
    """Provide a fresh DemoBackend instance with 1 year of data for faster tests."""
    return DemoBackend(start_year=2025, years=1)


@pytest.fixture
def sample_demo_data(demo_generator):
    """Generate a full year of demo data for testing."""
    return demo_generator.generate_full_year()


# ============================================================================
# DEMO DATA GENERATOR TESTS
# ============================================================================


class TestDemoDataGeneratorBasics:
    """Test basic data generation functionality."""

    def test_generator_initialization(self, demo_generator):
        """Test that generator initializes with correct year and seed."""
        assert demo_generator.start_year == 2025
        assert demo_generator.transaction_counter == 1000

    def test_generate_full_year_returns_tuple(self, demo_generator):
        """Test that generate_full_year returns correct structure."""
        result = demo_generator.generate_full_year()

        assert isinstance(result, tuple)
        assert len(result) == 3

        transactions, categories, category_groups = result
        assert isinstance(transactions, list)
        assert isinstance(categories, list)
        assert isinstance(category_groups, list)

    def test_generate_full_year_produces_transactions(self, sample_demo_data):
        """Test that data generation produces transactions."""
        transactions, categories, category_groups = sample_demo_data

        # Should have transactions for a full year
        assert len(transactions) > 0
        # Should have at least 80 transactions per month on average (960/year)
        assert len(transactions) >= 900

    def test_transactions_have_required_fields(self, sample_demo_data):
        """Test that all transactions have required fields."""
        transactions, _, _ = sample_demo_data

        required_fields = [
            "id",
            "date",
            "amount",
            "merchant",
            "category",
            "account",
            "notes",
            "hideFromReports",
            "pending",
            "isRecurring",
        ]

        for txn in transactions[:10]:  # Check first 10
            for field in required_fields:
                assert field in txn, f"Transaction missing field: {field}"

            # Check nested structures
            assert "id" in txn["merchant"]
            assert "name" in txn["merchant"]
            assert "id" in txn["category"]
            assert "name" in txn["category"]
            assert "id" in txn["account"]
            assert "displayName" in txn["account"]

    def test_transaction_ids_are_unique(self, sample_demo_data):
        """Test that all transaction IDs are unique."""
        transactions, _, _ = sample_demo_data

        ids = [txn["id"] for txn in transactions]
        unique_ids = set(ids)

        # Should have some duplicates for testing, but most should be unique
        # Allow up to 5% duplicates
        assert len(unique_ids) >= len(ids) * 0.95

    def test_dates_are_in_correct_year(self, sample_demo_data):
        """Test that all dates are in the specified year."""
        transactions, _, _ = sample_demo_data

        for txn in transactions:
            txn_date = txn["date"]
            assert txn_date.startswith("2025-")

    def test_dates_are_valid_format(self, sample_demo_data):
        """Test that dates are in correct ISO format."""
        transactions, _, _ = sample_demo_data

        for txn in transactions[:10]:  # Check first 10
            txn_date = txn["date"]
            # Should parse as valid date
            year, month, day = map(int, txn_date.split("-"))
            assert 2025 == year
            assert 1 <= month <= 12
            assert 1 <= day <= 31


class TestDemoDataRealisticPatterns:
    """Test that generated data has realistic financial patterns."""

    def test_biweekly_paychecks_exist(self, sample_demo_data):
        """Test that paychecks occur biweekly (1st and 15th)."""
        transactions, _, _ = sample_demo_data

        # Find paycheck transactions
        paychecks = [txn for txn in transactions if txn["category"]["id"] == "cat_paycheck"]

        # Should have ~24 paychecks per year (2 per month, 2 people)
        # 2 people * 2 paychecks/month * 12 months = 48 paychecks
        assert 40 <= len(paychecks) <= 52

        # Check amounts are positive (income)
        for paycheck in paychecks:
            assert paycheck["amount"] > 0
            # Should be reasonable paycheck amounts (Person 1: ~4300, Person 2: ~2900)
            assert 2800 <= paycheck["amount"] <= 4400

    def test_recurring_bills_exist(self, sample_demo_data):
        """Test that recurring bills appear monthly."""
        transactions, _, _ = sample_demo_data

        # Find rent transactions
        rent_txns = [txn for txn in transactions if txn["category"]["id"] == "cat_rent"]

        # Should have 12 rent payments (one per month)
        assert len(rent_txns) == 12

        # All should be same amount
        rent_amounts = [txn["amount"] for txn in rent_txns]
        assert len(set(rent_amounts)) == 1  # All identical
        assert rent_amounts[0] < 0  # Negative (expense)

    def test_streaming_services_marked_recurring(self, sample_demo_data):
        """Test that streaming services are marked as recurring."""
        transactions, _, _ = sample_demo_data

        streaming_services = ["Netflix", "Spotify Premium", "HBO Max"]

        for service in streaming_services:
            service_txns = [txn for txn in transactions if service in txn["merchant"]["name"]]

            # Should have 12 transactions (one per month)
            assert len(service_txns) >= 10  # Allow some variation

            # Should be marked as recurring
            for txn in service_txns:
                assert txn["isRecurring"] is True

    def test_transaction_counts_reasonable(self, sample_demo_data):
        """Test that transaction counts per month are realistic (70-110/month)."""
        transactions, _, _ = sample_demo_data

        # Group by month
        by_month: Dict[int, List] = {}
        for txn in transactions:
            month = int(txn["date"].split("-")[1])
            by_month.setdefault(month, []).append(txn)

        # Check each month has reasonable count
        # Expecting around 80-90 transactions per month
        for month in range(1, 13):
            count = len(by_month.get(month, []))
            assert 65 <= count <= 120, f"Month {month} has {count} transactions"

    def test_income_and_expense_totals_realistic(self, sample_demo_data):
        """Test that total income and expenses are realistic for ~$250k gross income."""
        transactions, _, _ = sample_demo_data

        total_income = sum(txn["amount"] for txn in transactions if txn["amount"] > 0)
        total_expenses = sum(txn["amount"] for txn in transactions if txn["amount"] < 0)

        # Should have roughly biweekly paychecks for 2 people:
        # Person 1: ~$4300 * 24 = ~$103k
        # Person 2: ~$2900 * 24 = ~$70k
        # Total: ~$173k (after taxes for ~$250k gross household income)
        assert 160000 <= total_income <= 190000

        # Expenses should be realistic (high cost of living area)
        # Allow expenses to be close to income (within 110%)
        assert abs(total_expenses) <= total_income * 1.1

        # Net should be reasonable (can be slightly negative due to randomness)
        net = total_income + total_expenses
        # Allow small deficit due to random variation in spending
        assert net > -10000  # No more than $10k deficit

    def test_transfers_hidden_from_reports(self, sample_demo_data):
        """Test that transfer transactions are marked hidden from reports."""
        transactions, _, _ = sample_demo_data

        transfers = [txn for txn in transactions if txn["category"]["id"] == "cat_transfer"]

        # Should have some transfers
        assert len(transfers) > 0

        # All should be hidden from reports
        for txn in transfers:
            assert txn["hideFromReports"] is True


class TestDemoDataCategories:
    """Test that all required categories exist and are properly structured."""

    def test_categories_exist(self, sample_demo_data):
        """Test that category list is not empty."""
        _, categories, _ = sample_demo_data
        assert len(categories) > 0

    def test_all_required_categories_present(self, sample_demo_data):
        """Test that all expected categories are present."""
        _, categories, _ = sample_demo_data

        category_ids = {cat["id"] for cat in categories}

        required_categories = [
            "cat_groceries",
            "cat_restaurants",
            "cat_coffee",
            "cat_gas",
            "cat_rent",
            "cat_utilities",
            "cat_internet",
            "cat_shopping",
            "cat_amazon",
            "cat_streaming",
            "cat_gym",
            "cat_phone",
            "cat_insurance",
            "cat_paycheck",
            "cat_transfer",
        ]

        for cat_id in required_categories:
            assert cat_id in category_ids, f"Missing category: {cat_id}"

    def test_categories_have_required_fields(self, sample_demo_data):
        """Test that categories have required structure."""
        _, categories, _ = sample_demo_data

        for cat in categories:
            assert "id" in cat
            assert "name" in cat
            assert "group" in cat
            assert "id" in cat["group"]
            assert "type" in cat["group"]

    def test_category_groups_exist(self, sample_demo_data):
        """Test that category groups are defined."""
        _, _, category_groups = sample_demo_data

        assert len(category_groups) > 0

        # Check expected groups
        group_names = {grp["name"] for grp in category_groups}
        expected_groups = [
            "Food & Dining",
            "Transportation",
            "Home",
            "Shopping",
            "Entertainment",
            "Health & Fitness",
            "Bills & Utilities",
            "Income",
            "Transfers",
        ]

        for expected in expected_groups:
            assert expected in group_names, f"Missing group: {expected}"


class TestDemoDataDuplicates:
    """Test that demo data includes duplicates for testing duplicate detection."""

    def test_duplicates_exist(self, sample_demo_data):
        """Test that some duplicate transactions exist."""
        transactions, _, _ = sample_demo_data

        # Check for exact duplicate transactions (same date, amount, merchant)
        seen = set()
        duplicates_found = 0

        for txn in transactions:
            key = (txn["date"], txn["amount"], txn["merchant"]["name"])
            if key in seen:
                duplicates_found += 1
            seen.add(key)

        # Should have at least a few duplicates
        assert duplicates_found >= 3

    def test_duplicate_transactions_have_different_ids(self, sample_demo_data):
        """Test that duplicate transactions have different IDs."""
        transactions, _, _ = sample_demo_data

        # Group by (date, amount, merchant)
        groups: Dict[tuple, List[Dict]] = {}
        for txn in transactions:
            key = (txn["date"], txn["amount"], txn["merchant"]["name"])
            groups.setdefault(key, []).append(txn)

        # Find groups with duplicates
        duplicate_groups = [txns for txns in groups.values() if len(txns) > 1]

        # Should have some duplicate groups
        assert len(duplicate_groups) > 0

        # Check IDs are different within each group
        for group in duplicate_groups:
            ids = [txn["id"] for txn in group]
            assert len(ids) == len(set(ids)), "Duplicate IDs found in duplicate group"


class TestDemoDataMerchantVariations:
    """Test that merchant names have variations for testing normalization."""

    def test_merchant_name_variations_exist(self, sample_demo_data):
        """Test that some merchants have name variations."""
        transactions, _, _ = sample_demo_data

        # Look for merchants with variations (e.g., "Whole Foods" vs "WHOLE FOODS MARKET #123")
        whole_foods_variations = [
            txn["merchant"]["name"]
            for txn in transactions
            if "whole foods" in txn["merchant"]["name"].lower()
        ]

        # Should have different name variations
        unique_names = set(whole_foods_variations)
        assert len(unique_names) >= 2

        # Check for Starbucks variations
        starbucks_variations = [
            txn["merchant"]["name"]
            for txn in transactions
            if "starbucks" in txn["merchant"]["name"].lower()
        ]

        unique_starbucks = set(starbucks_variations)
        assert len(unique_starbucks) >= 2

        # Check for Amazon variations
        amazon_variations = [
            txn["merchant"]["name"]
            for txn in transactions
            if "amazon" in txn["merchant"]["name"].lower()
            or "amzn" in txn["merchant"]["name"].lower()
        ]

        unique_amazon = set(amazon_variations)
        assert len(unique_amazon) >= 2


# ============================================================================
# DEMO BACKEND TESTS
# ============================================================================


class TestDemoBackendInitialization:
    """Test DemoBackend initialization and setup."""

    def test_backend_initializes(self, demo_backend):
        """Test that backend initializes correctly."""
        assert demo_backend.start_year == 2025
        assert demo_backend.years == 1
        assert demo_backend.is_logged_in is False
        assert len(demo_backend.transactions) > 0
        assert len(demo_backend.categories) > 0
        assert len(demo_backend.category_groups) > 0
        assert len(demo_backend.update_calls) == 0

    def test_backend_has_data_on_init(self, demo_backend):
        """Test that backend generates data on initialization."""
        # Should have substantial data
        assert len(demo_backend.transactions) >= 900  # ~75/month minimum


class TestDemoBackendLogin:
    """Test demo backend login functionality."""

    @pytest.mark.asyncio
    async def test_login_succeeds_without_credentials(self, demo_backend):
        """Test that login succeeds without providing credentials."""
        await demo_backend.login()
        assert demo_backend.is_logged_in is True

    @pytest.mark.asyncio
    async def test_login_succeeds_with_any_credentials(self, demo_backend):
        """Test that login succeeds regardless of credentials provided."""
        await demo_backend.login(email="fake@example.com", password="fake_password")
        assert demo_backend.is_logged_in is True

    @pytest.mark.asyncio
    async def test_login_with_mfa(self, demo_backend):
        """Test that login succeeds even with MFA parameters."""
        await demo_backend.login(mfa_secret_key="fake_mfa_key")
        assert demo_backend.is_logged_in is True


class TestDemoBackendGetTransactions:
    """Test fetching transactions from demo backend."""

    @pytest.mark.asyncio
    async def test_get_transactions_returns_data(self, demo_backend):
        """Test that get_transactions returns data."""
        result = await demo_backend.get_transactions(limit=100)

        assert "allTransactions" in result
        assert "results" in result["allTransactions"]
        assert "totalCount" in result["allTransactions"]

        transactions = result["allTransactions"]["results"]
        assert len(transactions) > 0

    @pytest.mark.asyncio
    async def test_get_transactions_pagination(self, demo_backend):
        """Test that pagination works correctly."""
        # Get first page
        page1 = await demo_backend.get_transactions(limit=10, offset=0)
        results1 = page1["allTransactions"]["results"]

        # Get second page
        page2 = await demo_backend.get_transactions(limit=10, offset=10)
        results2 = page2["allTransactions"]["results"]

        assert len(results1) == 10
        assert len(results2) == 10

        # Pages should have different transactions
        ids1 = {txn["id"] for txn in results1}
        ids2 = {txn["id"] for txn in results2}
        assert len(ids1.intersection(ids2)) == 0

    @pytest.mark.asyncio
    async def test_get_transactions_total_count(self, demo_backend):
        """Test that totalCount is accurate."""
        result = await demo_backend.get_transactions(limit=10)

        total_count = result["allTransactions"]["totalCount"]
        actual_count = len(demo_backend.transactions)

        assert total_count == actual_count

    @pytest.mark.asyncio
    async def test_get_transactions_sorted_by_date_desc(self, demo_backend):
        """Test that transactions are sorted by date descending (newest first)."""
        result = await demo_backend.get_transactions(limit=100)
        transactions = result["allTransactions"]["results"]

        dates = [txn["date"] for txn in transactions]

        # Should be sorted descending
        assert dates == sorted(dates, reverse=True)


class TestDemoBackendDateFiltering:
    """Test date filtering functionality."""

    @pytest.mark.asyncio
    async def test_filter_by_start_date(self, demo_backend):
        """Test filtering by start date."""
        result = await demo_backend.get_transactions(limit=1000, start_date="2025-06-01")

        transactions = result["allTransactions"]["results"]

        # All transactions should be on or after start date
        for txn in transactions:
            assert txn["date"] >= "2025-06-01"

    @pytest.mark.asyncio
    async def test_filter_by_end_date(self, demo_backend):
        """Test filtering by end date."""
        result = await demo_backend.get_transactions(limit=1000, end_date="2025-06-30")

        transactions = result["allTransactions"]["results"]

        # All transactions should be on or before end date
        for txn in transactions:
            assert txn["date"] <= "2025-06-30"

    @pytest.mark.asyncio
    async def test_filter_by_date_range(self, demo_backend):
        """Test filtering by both start and end date."""
        result = await demo_backend.get_transactions(
            limit=1000, start_date="2025-03-01", end_date="2025-03-31"
        )

        transactions = result["allTransactions"]["results"]

        # All transactions should be in March 2025
        for txn in transactions:
            assert txn["date"] >= "2025-03-01"
            assert txn["date"] <= "2025-03-31"

        # Should have some transactions (not empty)
        assert len(transactions) > 0

    @pytest.mark.asyncio
    async def test_date_filter_affects_total_count(self, demo_backend):
        """Test that totalCount reflects filtered results."""
        # Get all transactions
        all_result = await demo_backend.get_transactions(limit=10000)
        all_count = all_result["allTransactions"]["totalCount"]

        # Get filtered transactions
        filtered_result = await demo_backend.get_transactions(
            limit=10000, start_date="2025-06-01", end_date="2025-06-30"
        )
        filtered_count = filtered_result["allTransactions"]["totalCount"]

        # Filtered count should be less than total
        assert filtered_count < all_count


class TestDemoBackendUpdateTransaction:
    """Test updating transactions in demo backend."""

    @pytest.mark.asyncio
    async def test_update_transaction_merchant(self, demo_backend):
        """Test updating a transaction's merchant name."""
        # Get a transaction
        result = await demo_backend.get_transactions(limit=1)
        txn = result["allTransactions"]["results"][0]
        txn_id = txn["id"]
        original_merchant = txn["merchant"]["name"]

        # Update merchant name
        new_merchant = "Updated Merchant Name"
        await demo_backend.update_transaction(transaction_id=txn_id, merchant_name=new_merchant)

        # Verify update was recorded
        assert len(demo_backend.update_calls) == 1
        assert demo_backend.update_calls[0]["transaction_id"] == txn_id
        assert demo_backend.update_calls[0]["merchant_name"] == new_merchant

        # Verify data was actually updated
        updated_txn = demo_backend.get_transaction_by_id(txn_id)
        assert updated_txn["merchant"]["name"] == new_merchant
        assert updated_txn["merchant"]["name"] != original_merchant

    @pytest.mark.asyncio
    async def test_update_transaction_category(self, demo_backend):
        """Test updating a transaction's category."""
        # Get a transaction
        result = await demo_backend.get_transactions(limit=1)
        txn = result["allTransactions"]["results"][0]
        txn_id = txn["id"]

        # Update category
        new_category_id = "cat_shopping"
        await demo_backend.update_transaction(transaction_id=txn_id, category_id=new_category_id)

        # Verify update was applied
        updated_txn = demo_backend.get_transaction_by_id(txn_id)
        assert updated_txn["category"]["id"] == new_category_id
        assert updated_txn["category"]["name"] == "Shopping"

    @pytest.mark.asyncio
    async def test_update_transaction_hide_from_reports(self, demo_backend):
        """Test updating a transaction's hide_from_reports flag."""
        # Get a transaction
        result = await demo_backend.get_transactions(limit=1)
        txn = result["allTransactions"]["results"][0]
        txn_id = txn["id"]
        original_hidden = txn["hideFromReports"]

        # Toggle hide_from_reports
        new_hidden = not original_hidden
        await demo_backend.update_transaction(transaction_id=txn_id, hide_from_reports=new_hidden)

        # Verify update was applied
        updated_txn = demo_backend.get_transaction_by_id(txn_id)
        assert updated_txn["hideFromReports"] == new_hidden

    @pytest.mark.asyncio
    async def test_update_transaction_multiple_fields(self, demo_backend):
        """Test updating multiple fields at once."""
        # Get a transaction
        result = await demo_backend.get_transactions(limit=1)
        txn = result["allTransactions"]["results"][0]
        txn_id = txn["id"]

        # Update multiple fields
        await demo_backend.update_transaction(
            transaction_id=txn_id,
            merchant_name="New Merchant",
            category_id="cat_groceries",
            hide_from_reports=True,
        )

        # Verify all updates were applied
        updated_txn = demo_backend.get_transaction_by_id(txn_id)
        assert updated_txn["merchant"]["name"] == "New Merchant"
        assert updated_txn["category"]["id"] == "cat_groceries"
        assert updated_txn["hideFromReports"] is True

    @pytest.mark.asyncio
    async def test_update_transaction_not_found_raises_error(self, demo_backend):
        """Test that updating a non-existent transaction raises an error."""
        with pytest.raises(Exception, match="Transaction not found"):
            await demo_backend.update_transaction(
                transaction_id="nonexistent_id", merchant_name="New Name"
            )

    @pytest.mark.asyncio
    async def test_update_persists_across_get_transactions(self, demo_backend):
        """Test that updates persist when fetching transactions again."""
        # Get a transaction
        result = await demo_backend.get_transactions(limit=1)
        txn = result["allTransactions"]["results"][0]
        txn_id = txn["id"]

        # Update it
        new_merchant = "Persistent Update"
        await demo_backend.update_transaction(transaction_id=txn_id, merchant_name=new_merchant)

        # Fetch transactions again
        result2 = await demo_backend.get_transactions(limit=10000)
        updated_txn = next(
            (t for t in result2["allTransactions"]["results"] if t["id"] == txn_id), None
        )

        assert updated_txn is not None
        assert updated_txn["merchant"]["name"] == new_merchant


class TestDemoBackendDeleteTransaction:
    """Test deleting transactions from demo backend."""

    @pytest.mark.asyncio
    async def test_delete_transaction(self, demo_backend):
        """Test deleting a transaction."""
        # Get initial count
        initial_count = len(demo_backend.transactions)

        # Get a transaction to delete
        result = await demo_backend.get_transactions(limit=1)
        txn = result["allTransactions"]["results"][0]
        txn_id = txn["id"]

        # Delete it
        success = await demo_backend.delete_transaction(txn_id)

        assert success is True
        assert len(demo_backend.transactions) == initial_count - 1

        # Verify it's actually gone
        deleted_txn = demo_backend.get_transaction_by_id(txn_id)
        assert deleted_txn is None

    @pytest.mark.asyncio
    async def test_delete_transaction_not_found_raises_error(self, demo_backend):
        """Test that deleting a non-existent transaction raises an error."""
        with pytest.raises(Exception, match="Transaction not found"):
            await demo_backend.delete_transaction("nonexistent_id")

    @pytest.mark.asyncio
    async def test_delete_persists_across_get_transactions(self, demo_backend):
        """Test that deletion persists when fetching transactions again."""
        # Get a transaction to delete
        result = await demo_backend.get_transactions(limit=1)
        txn = result["allTransactions"]["results"][0]
        txn_id = txn["id"]

        # Delete it
        await demo_backend.delete_transaction(txn_id)

        # Fetch all transactions
        result2 = await demo_backend.get_transactions(limit=10000)
        all_ids = {t["id"] for t in result2["allTransactions"]["results"]}

        # Deleted transaction should not be in results
        assert txn_id not in all_ids


class TestDemoBackendCategories:
    """Test fetching categories and category groups."""

    @pytest.mark.asyncio
    async def test_get_transaction_categories(self, demo_backend):
        """Test fetching transaction categories."""
        result = await demo_backend.get_transaction_categories()

        assert "categories" in result
        categories = result["categories"]

        assert len(categories) > 0
        assert all("id" in cat for cat in categories)
        assert all("name" in cat for cat in categories)

    @pytest.mark.asyncio
    async def test_get_transaction_category_groups(self, demo_backend):
        """Test fetching transaction category groups."""
        result = await demo_backend.get_transaction_category_groups()

        assert "categoryGroups" in result
        groups = result["categoryGroups"]

        assert len(groups) > 0
        assert all("id" in grp for grp in groups)
        assert all("name" in grp for grp in groups)
        assert all("type" in grp for grp in groups)


class TestDemoBackendStats:
    """Test demo backend statistics functionality."""

    def test_get_demo_stats(self, demo_backend):
        """Test retrieving demo statistics."""
        stats = demo_backend.get_demo_stats()

        assert "total_transactions" in stats
        assert "total_income" in stats
        assert "total_expenses" in stats
        assert "net" in stats
        assert "updates_made" in stats

        # Verify stats are reasonable
        assert stats["total_transactions"] > 1000
        assert stats["total_income"] > 0
        assert stats["total_expenses"] < 0
        assert stats["net"] == stats["total_income"] + stats["total_expenses"]
        assert stats["updates_made"] == 0  # No updates yet

    @pytest.mark.asyncio
    async def test_stats_update_after_modification(self, demo_backend):
        """Test that stats update after making changes."""
        initial_stats = demo_backend.get_demo_stats()

        # Make an update
        result = await demo_backend.get_transactions(limit=1)
        txn_id = result["allTransactions"]["results"][0]["id"]
        await demo_backend.update_transaction(txn_id, merchant_name="Updated")

        updated_stats = demo_backend.get_demo_stats()

        # Updates count should increase
        assert updated_stats["updates_made"] == 1

        # Total transactions should stay the same
        assert updated_stats["total_transactions"] == initial_stats["total_transactions"]

    @pytest.mark.asyncio
    async def test_stats_after_deletion(self, demo_backend):
        """Test that stats update after deleting a transaction."""
        initial_stats = demo_backend.get_demo_stats()

        # Delete a transaction
        result = await demo_backend.get_transactions(limit=1)
        txn_id = result["allTransactions"]["results"][0]["id"]
        await demo_backend.delete_transaction(txn_id)

        updated_stats = demo_backend.get_demo_stats()

        # Transaction count should decrease
        assert updated_stats["total_transactions"] == initial_stats["total_transactions"] - 1


class TestDemoBackendHelperMethods:
    """Test helper methods on demo backend."""

    def test_get_transaction_by_id_found(self, demo_backend):
        """Test getting a transaction by ID when it exists."""
        # Get a transaction ID
        first_txn = demo_backend.transactions[0]
        txn_id = first_txn["id"]

        # Get by ID
        txn = demo_backend.get_transaction_by_id(txn_id)

        assert txn is not None
        assert txn["id"] == txn_id

    def test_get_transaction_by_id_not_found(self, demo_backend):
        """Test getting a transaction by ID when it doesn't exist."""
        txn = demo_backend.get_transaction_by_id("nonexistent_id")
        assert txn is None


# ============================================================================
# INTEGRATION TESTS
# ============================================================================


class TestDemoModeIntegration:
    """Integration tests for complete demo mode workflows."""

    @pytest.mark.asyncio
    async def test_full_demo_workflow(self, demo_backend):
        """Test a complete demo mode workflow: login, fetch, update, delete."""
        # 1. Login
        await demo_backend.login()
        assert demo_backend.is_logged_in is True

        # 2. Fetch categories
        cat_result = await demo_backend.get_transaction_categories()
        assert len(cat_result["categories"]) > 0

        # 3. Fetch transactions
        txn_result = await demo_backend.get_transactions(limit=100)
        transactions = txn_result["allTransactions"]["results"]
        assert len(transactions) > 0

        # 4. Update a transaction
        txn_id = transactions[0]["id"]
        await demo_backend.update_transaction(transaction_id=txn_id, merchant_name="Test Update")

        # 5. Verify update
        updated = demo_backend.get_transaction_by_id(txn_id)
        assert updated["merchant"]["name"] == "Test Update"

        # 6. Delete a transaction
        delete_id = transactions[1]["id"]
        await demo_backend.delete_transaction(delete_id)

        # 7. Verify deletion
        deleted = demo_backend.get_transaction_by_id(delete_id)
        assert deleted is None

        # 8. Check stats
        stats = demo_backend.get_demo_stats()
        assert stats["updates_made"] == 1

    @pytest.mark.asyncio
    async def test_demo_data_suitable_for_testing(self, demo_backend):
        """Test that demo data has characteristics needed for testing features."""
        await demo_backend.login()

        # Get all transactions
        result = await demo_backend.get_transactions(limit=10000)
        transactions = result["allTransactions"]["results"]

        # 1. Should have duplicates for testing duplicate detection
        seen = set()
        duplicates = 0
        for txn in transactions:
            key = (txn["date"], txn["amount"], txn["merchant"]["name"])
            if key in seen:
                duplicates += 1
            seen.add(key)
        # Demo data randomly creates duplicates, should have at least 1
        assert duplicates >= 1

        # 2. Should have merchant name variations for testing normalization
        merchants = [txn["merchant"]["name"] for txn in transactions]
        merchant_set = set(merchants)
        # Should have variations (e.g., "Starbucks" and "STARBUCKS #1234")
        starbucks_variations = [m for m in merchant_set if "starbucks" in m.lower()]
        assert len(starbucks_variations) >= 2

        # 3. Should have transfers for testing hide/show functionality
        transfers = [txn for txn in transactions if txn["category"]["id"] == "cat_transfer"]
        assert len(transfers) > 0
        assert all(txn["hideFromReports"] for txn in transfers)

        # 4. Should have multiple categories for testing categorization
        categories = {txn["category"]["id"] for txn in transactions}
        assert len(categories) >= 10

        # 5. Should have income and expenses for testing filtering
        income = [txn for txn in transactions if txn["amount"] > 0]
        expenses = [txn for txn in transactions if txn["amount"] < 0]
        assert len(income) > 0
        assert len(expenses) > 0


# ============================================================================
# STANDALONE FUNCTION TESTS
# ============================================================================


def test_generate_demo_data_function():
    """Test the standalone generate_demo_data function."""
    transactions, categories, category_groups = generate_demo_data(start_year=2025, years=1)

    assert len(transactions) > 0
    assert len(categories) > 0
    assert len(category_groups) > 0

    # Verify structure
    assert isinstance(transactions, list)
    assert isinstance(categories, list)
    assert isinstance(category_groups, list)
