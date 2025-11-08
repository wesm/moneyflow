from unittest.mock import MagicMock, patch

import pytest

from moneyflow.backends.ynab import YNABBackend


class TestYNABBackend:
    @pytest.fixture
    def backend(self):
        return YNABBackend()

    @pytest.fixture
    def mock_ynab_api(self):
        with patch("moneyflow.ynab_client.ynab") as mock:
            yield mock

    @pytest.mark.asyncio
    async def test_login_success(self, backend, mock_ynab_api):
        mock_budget = MagicMock()
        mock_budget.id = "test-budget-id"
        mock_budget.name = "Test Budget"

        mock_budgets_response = MagicMock()
        mock_budgets_response.data.budgets = [mock_budget]

        mock_budgets_api = MagicMock()
        mock_budgets_api.get_budgets.return_value = mock_budgets_response

        mock_ynab_api.BudgetsApi.return_value = mock_budgets_api

        await backend.login(password="test-access-token")

        assert backend.client.access_token == "test-access-token"
        assert backend.client.budget_id == "test-budget-id"

    @pytest.mark.asyncio
    async def test_login_no_password(self, backend):
        with pytest.raises(ValueError, match="YNAB backend requires an access token"):
            await backend.login()

    @pytest.mark.asyncio
    async def test_login_no_budgets(self, backend, mock_ynab_api):
        mock_budgets_response = MagicMock()
        mock_budgets_response.data.budgets = []

        mock_budgets_api = MagicMock()
        mock_budgets_api.get_budgets.return_value = mock_budgets_response

        mock_ynab_api.BudgetsApi.return_value = mock_budgets_api

        with pytest.raises(ValueError, match="No budgets found"):
            await backend.login(password="test-access-token")

    @pytest.mark.asyncio
    async def test_get_transactions(self, backend, mock_ynab_api):
        backend.client.budget_id = "test-budget-id"
        backend.client.access_token = "test-token"
        backend.client.api_client = MagicMock()

        mock_txn = MagicMock()
        mock_txn.id = "txn-1"
        mock_txn.var_date = "2025-01-15"
        mock_txn.amount = -50000
        mock_txn.payee_id = "payee-1"
        mock_txn.payee_name = "Coffee Shop"
        mock_txn.category_id = "cat-1"
        mock_txn.category_name = "Food & Dining"
        mock_txn.account_id = "acc-1"
        mock_txn.account_name = "Checking"
        mock_txn.memo = "Morning coffee"
        mock_txn.deleted = False
        mock_txn.transfer_account_id = None
        mock_txn.cleared = "cleared"

        mock_response = MagicMock()
        mock_response.data.transactions = [mock_txn]

        mock_transactions_api = MagicMock()
        mock_transactions_api.get_transactions.return_value = mock_response

        mock_ynab_api.TransactionsApi.return_value = mock_transactions_api

        result = await backend.get_transactions(limit=10)

        assert "allTransactions" in result
        assert "totalCount" in result["allTransactions"]
        assert "results" in result["allTransactions"]
        assert len(result["allTransactions"]["results"]) == 1
        assert result["allTransactions"]["results"][0]["id"] == "txn-1"
        assert result["allTransactions"]["results"][0]["amount"] == -50.0
        assert result["allTransactions"]["results"][0]["merchant"]["name"] == "Coffee Shop"

    @pytest.mark.asyncio
    async def test_get_transaction_categories(self, backend, mock_ynab_api):
        backend.client.budget_id = "test-budget-id"
        backend.client.api_client = MagicMock()

        mock_category = MagicMock()
        mock_category.id = "cat-1"
        mock_category.name = "Groceries"

        mock_category_group = MagicMock()
        mock_category_group.id = "group-1"
        mock_category_group.name = "Food & Dining"
        mock_category_group.categories = [mock_category]

        mock_response = MagicMock()
        mock_response.data.category_groups = [mock_category_group]

        mock_categories_api = MagicMock()
        mock_categories_api.get_categories.return_value = mock_response

        mock_ynab_api.CategoriesApi.return_value = mock_categories_api

        result = await backend.get_transaction_categories()

        assert "categories" in result
        assert len(result["categories"]) == 1
        assert result["categories"][0]["name"] == "Groceries"
        assert result["categories"][0]["group"]["name"] == "Food & Dining"

    @pytest.mark.asyncio
    async def test_update_transaction(self, backend, mock_ynab_api):
        backend.client.budget_id = "test-budget-id"
        backend.client.api_client = MagicMock()

        mock_existing_txn = MagicMock()
        mock_existing_txn.account_id = "acc-1"
        mock_existing_txn.var_date = "2025-01-15"
        mock_existing_txn.amount = -50000

        mock_get_response = MagicMock()
        mock_get_response.data.transaction = mock_existing_txn

        mock_updated_txn = MagicMock()
        mock_updated_txn.id = "txn-1"

        mock_update_response = MagicMock()
        mock_update_response.data.transaction = mock_updated_txn

        mock_transactions_api = MagicMock()
        mock_transactions_api.get_transaction_by_id.return_value = mock_get_response
        mock_transactions_api.update_transaction.return_value = mock_update_response

        mock_payees_api = MagicMock()
        mock_payee = MagicMock()
        mock_payee.name = "New Merchant"
        mock_payee.id = "payee-new"
        mock_payees_response = MagicMock()
        mock_payees_response.data.payees = [mock_payee]
        mock_payees_api.get_payees.return_value = mock_payees_response

        mock_ynab_api.TransactionsApi.return_value = mock_transactions_api
        mock_ynab_api.PayeesApi.return_value = mock_payees_api

        result = await backend.update_transaction(
            transaction_id="txn-1", merchant_name="New Merchant", category_id="cat-2"
        )

        assert result["updateTransaction"]["transaction"]["id"] == "txn-1"

    @pytest.mark.asyncio
    async def test_delete_transaction_success(self, backend, mock_ynab_api):
        backend.client.budget_id = "test-budget-id"
        backend.client.api_client = MagicMock()

        mock_transactions_api = MagicMock()
        mock_transactions_api.delete_transaction.return_value = None

        mock_ynab_api.TransactionsApi.return_value = mock_transactions_api

        result = await backend.delete_transaction("txn-1")

        assert result is True

    @pytest.mark.asyncio
    async def test_get_all_merchants(self, backend, mock_ynab_api):
        backend.client.budget_id = "test-budget-id"
        backend.client.api_client = MagicMock()

        mock_payee1 = MagicMock()
        mock_payee1.name = "Starbucks"

        mock_payee2 = MagicMock()
        mock_payee2.name = "Amazon"

        mock_response = MagicMock()
        mock_response.data.payees = [mock_payee1, mock_payee2]

        mock_payees_api = MagicMock()
        mock_payees_api.get_payees.return_value = mock_response

        mock_ynab_api.PayeesApi.return_value = mock_payees_api

        result = await backend.get_all_merchants()

        assert result == ["Amazon", "Starbucks"]

    @pytest.mark.asyncio
    async def test_get_transactions_hides_transfers(self, backend, mock_ynab_api):
        backend.client.budget_id = "test-budget-id"
        backend.client.access_token = "test-token"
        backend.client.api_client = MagicMock()

        mock_transfer_txn = MagicMock()
        mock_transfer_txn.id = "txn-transfer"
        mock_transfer_txn.var_date = "2025-01-15"
        mock_transfer_txn.amount = 100000
        mock_transfer_txn.payee_id = "payee-1"
        mock_transfer_txn.payee_name = "Transfer"
        mock_transfer_txn.category_id = None
        mock_transfer_txn.category_name = None
        mock_transfer_txn.account_id = "acc-1"
        mock_transfer_txn.account_name = "Checking"
        mock_transfer_txn.memo = "Transfer to savings"
        mock_transfer_txn.deleted = False
        mock_transfer_txn.transfer_account_id = "acc-2"
        mock_transfer_txn.cleared = "cleared"

        mock_response = MagicMock()
        mock_response.data.transactions = [mock_transfer_txn]

        mock_transactions_api = MagicMock()
        mock_transactions_api.get_transactions.return_value = mock_response

        mock_ynab_api.TransactionsApi.return_value = mock_transactions_api

        result = await backend.get_transactions(limit=10)

        assert len(result["allTransactions"]["results"]) == 1
        assert result["allTransactions"]["results"][0]["hideFromReports"] is True

    @pytest.mark.asyncio
    async def test_get_transactions_caches_results(self, backend, mock_ynab_api):
        backend.client.budget_id = "test-budget-id"
        backend.client.access_token = "test-token"
        backend.client.api_client = MagicMock()

        mock_txn = MagicMock()
        mock_txn.id = "txn-1"
        mock_txn.var_date = "2025-01-15"
        mock_txn.amount = -50000
        mock_txn.payee_id = "payee-1"
        mock_txn.payee_name = "Coffee Shop"
        mock_txn.category_id = "cat-1"
        mock_txn.category_name = "Food & Dining"
        mock_txn.account_id = "acc-1"
        mock_txn.account_name = "Checking"
        mock_txn.memo = "Morning coffee"
        mock_txn.deleted = False
        mock_txn.transfer_account_id = None
        mock_txn.cleared = "cleared"

        mock_response = MagicMock()
        mock_response.data.transactions = [mock_txn]

        mock_transactions_api = MagicMock()
        mock_transactions_api.get_transactions.return_value = mock_response

        mock_ynab_api.TransactionsApi.return_value = mock_transactions_api

        result1 = await backend.get_transactions(limit=10, offset=0)
        result2 = await backend.get_transactions(limit=10, offset=0)

        assert mock_transactions_api.get_transactions.call_count == 1
        assert result1 == result2

    @pytest.mark.asyncio
    async def test_get_transactions_filters_by_hidden_from_reports(self, backend, mock_ynab_api):
        backend.client.budget_id = "test-budget-id"
        backend.client.access_token = "test-token"
        backend.client.api_client = MagicMock()

        mock_visible_txn = MagicMock()
        mock_visible_txn.id = "txn-visible"
        mock_visible_txn.var_date = "2025-01-15"
        mock_visible_txn.amount = -50000
        mock_visible_txn.payee_id = "payee-1"
        mock_visible_txn.payee_name = "Coffee Shop"
        mock_visible_txn.category_id = "cat-1"
        mock_visible_txn.category_name = "Food & Dining"
        mock_visible_txn.account_id = "acc-1"
        mock_visible_txn.account_name = "Checking"
        mock_visible_txn.memo = "Visible transaction"
        mock_visible_txn.deleted = False
        mock_visible_txn.transfer_account_id = None
        mock_visible_txn.cleared = "cleared"

        mock_hidden_txn = MagicMock()
        mock_hidden_txn.id = "txn-hidden"
        mock_hidden_txn.var_date = "2025-01-16"
        mock_hidden_txn.amount = 100000
        mock_hidden_txn.payee_id = "payee-2"
        mock_hidden_txn.payee_name = "Transfer"
        mock_hidden_txn.category_id = None
        mock_hidden_txn.category_name = None
        mock_hidden_txn.account_id = "acc-1"
        mock_hidden_txn.account_name = "Checking"
        mock_hidden_txn.memo = "Hidden transaction"
        mock_hidden_txn.deleted = False
        mock_hidden_txn.transfer_account_id = "acc-2"
        mock_hidden_txn.cleared = "cleared"

        mock_response = MagicMock()
        mock_response.data.transactions = [mock_visible_txn, mock_hidden_txn]

        mock_transactions_api = MagicMock()
        mock_transactions_api.get_transactions.return_value = mock_response

        mock_ynab_api.TransactionsApi.return_value = mock_transactions_api

        result_visible = await backend.get_transactions(limit=10, hidden_from_reports=False)
        result_hidden = await backend.get_transactions(limit=10, hidden_from_reports=True)
        result_all = await backend.get_transactions(limit=10)

        assert mock_transactions_api.get_transactions.call_count == 1
        assert len(result_visible["allTransactions"]["results"]) == 1
        assert result_visible["allTransactions"]["results"][0]["id"] == "txn-visible"
        assert len(result_hidden["allTransactions"]["results"]) == 1
        assert result_hidden["allTransactions"]["results"][0]["id"] == "txn-hidden"
        assert len(result_all["allTransactions"]["results"]) == 2

    def test_clear_auth(self, backend):
        backend.client.api_client = MagicMock()
        backend.client.access_token = "test-token"

        backend.clear_auth()

        assert backend.client.api_client is None
        assert backend.client.access_token is None

    def test_update_payee_success(self, backend, mock_ynab_api):
        """Test that update_payee successfully updates a payee name."""
        backend.client.budget_id = "test-budget-id"
        backend.client.api_client = MagicMock()

        mock_updated_payee = MagicMock()
        mock_updated_payee.name = "Amazon"

        mock_response = MagicMock()
        mock_response.data.payee = mock_updated_payee

        mock_payees_api = MagicMock()
        mock_payees_api.update_payee.return_value = mock_response

        mock_ynab_api.PayeesApi.return_value = mock_payees_api

        result = backend.client.update_payee("payee-123", "Amazon")

        assert result is True
        mock_payees_api.update_payee.assert_called_once()
        call_args = mock_payees_api.update_payee.call_args
        assert call_args[1]["budget_id"] == "test-budget-id"
        assert call_args[1]["payee_id"] == "payee-123"

    def test_update_payee_invalid_name_empty(self, backend):
        """Test that update_payee rejects empty names."""
        backend.client.budget_id = "test-budget-id"
        backend.client.api_client = MagicMock()

        result = backend.client.update_payee("payee-123", "")

        assert result is False

    def test_update_payee_invalid_name_too_long(self, backend):
        """Test that update_payee rejects names over 500 characters."""
        backend.client.budget_id = "test-budget-id"
        backend.client.api_client = MagicMock()

        long_name = "A" * 501

        result = backend.client.update_payee("payee-123", long_name)

        assert result is False

    def test_update_payee_api_error(self, backend, mock_ynab_api):
        """Test that update_payee handles API errors gracefully."""
        backend.client.budget_id = "test-budget-id"
        backend.client.api_client = MagicMock()

        mock_payees_api = MagicMock()
        mock_payees_api.update_payee.side_effect = Exception("API Error")

        mock_ynab_api.PayeesApi.return_value = mock_payees_api

        result = backend.client.update_payee("payee-123", "Amazon")

        assert result is False

    def test_update_payee_not_authenticated(self, backend):
        """Test that update_payee raises error when not authenticated."""
        with pytest.raises(ValueError, match="Must call login"):
            backend.client.update_payee("payee-123", "Amazon")

    def test_batch_update_merchant_success(self, backend, mock_ynab_api):
        """Test successful batch merchant update via payee update."""
        backend.client.budget_id = "test-budget-id"
        backend.client.api_client = MagicMock()

        # Mock finding the old payee
        mock_old_payee = MagicMock()
        mock_old_payee.id = "payee-old"
        mock_old_payee.name = "Amazon.com/abc123"

        mock_payees_response = MagicMock()
        mock_payees_response.data.payees = [mock_old_payee]

        # Mock updating the payee
        mock_updated_payee = MagicMock()
        mock_updated_payee.name = "Amazon"

        mock_update_response = MagicMock()
        mock_update_response.data.payee = mock_updated_payee

        mock_payees_api = MagicMock()
        mock_payees_api.get_payees.return_value = mock_payees_response
        mock_payees_api.update_payee.return_value = mock_update_response

        mock_ynab_api.PayeesApi.return_value = mock_payees_api

        result = backend.batch_update_merchant("Amazon.com/abc123", "Amazon")

        assert result["success"] is True
        assert result["payee_id"] == "payee-old"
        assert result["method"] == "payee_update"
        mock_payees_api.update_payee.assert_called_once()

    def test_batch_update_merchant_payee_not_found(self, backend, mock_ynab_api):
        """Test batch merchant update when old payee doesn't exist."""
        backend.client.budget_id = "test-budget-id"
        backend.client.api_client = MagicMock()

        # Mock no matching payee found
        mock_payees_response = MagicMock()
        mock_payees_response.data.payees = []

        mock_payees_api = MagicMock()
        mock_payees_api.get_payees.return_value = mock_payees_response

        mock_ynab_api.PayeesApi.return_value = mock_payees_api

        result = backend.batch_update_merchant("NonExistent", "Amazon")

        assert result["success"] is False
        assert result["payee_id"] is None
        assert result["method"] == "payee_not_found"
        assert "not found" in result["message"]

    def test_batch_update_merchant_update_fails(self, backend, mock_ynab_api):
        """Test batch merchant update when payee update fails."""
        backend.client.budget_id = "test-budget-id"
        backend.client.api_client = MagicMock()

        # Mock finding the old payee
        mock_old_payee = MagicMock()
        mock_old_payee.id = "payee-old"
        mock_old_payee.name = "Amazon.com/abc123"

        mock_payees_response = MagicMock()
        mock_payees_response.data.payees = [mock_old_payee]

        # Mock update failure
        mock_payees_api = MagicMock()
        mock_payees_api.get_payees.return_value = mock_payees_response
        mock_payees_api.update_payee.side_effect = Exception("Update failed")

        mock_ynab_api.PayeesApi.return_value = mock_payees_api

        result = backend.batch_update_merchant("Amazon.com/abc123", "Amazon")

        assert result["success"] is False
        assert result["payee_id"] == "payee-old"
        assert result["method"] == "payee_update_failed"

    def test_batch_update_merchant_integration(self, backend, mock_ynab_api):
        """
        Integration test: Verify that batch_update_merchant cascades to transactions.

        This test simulates the full flow:
        1. Fetch transactions (all have payee "Amazon.com/abc123")
        2. Batch update merchant to "Amazon"
        3. Verify the payee was updated (which would cascade to all transactions in real API)
        """
        backend.client.budget_id = "test-budget-id"
        backend.client.access_token = "test-token"
        backend.client.api_client = MagicMock()

        # Setup transactions with old merchant name
        mock_txn1 = MagicMock()
        mock_txn1.id = "txn-1"
        mock_txn1.payee_id = "payee-amazon-old"
        mock_txn1.payee_name = "Amazon.com/abc123"
        mock_txn1.var_date = "2025-01-15"
        mock_txn1.amount = -50000
        mock_txn1.category_id = "cat-1"
        mock_txn1.category_name = "Shopping"
        mock_txn1.account_id = "acc-1"
        mock_txn1.account_name = "Checking"
        mock_txn1.memo = ""
        mock_txn1.deleted = False
        mock_txn1.transfer_account_id = None
        mock_txn1.cleared = "cleared"

        mock_txn2 = MagicMock()
        mock_txn2.id = "txn-2"
        mock_txn2.payee_id = "payee-amazon-old"
        mock_txn2.payee_name = "Amazon.com/abc123"
        mock_txn2.var_date = "2025-01-16"
        mock_txn2.amount = -30000
        mock_txn2.category_id = "cat-1"
        mock_txn2.category_name = "Shopping"
        mock_txn2.account_id = "acc-1"
        mock_txn2.account_name = "Checking"
        mock_txn2.memo = ""
        mock_txn2.deleted = False
        mock_txn2.transfer_account_id = None
        mock_txn2.cleared = "cleared"

        mock_txns_response = MagicMock()
        mock_txns_response.data.transactions = [mock_txn1, mock_txn2]

        mock_transactions_api = MagicMock()
        mock_transactions_api.get_transactions.return_value = mock_txns_response

        # Setup payee for batch update
        mock_old_payee = MagicMock()
        mock_old_payee.id = "payee-amazon-old"
        mock_old_payee.name = "Amazon.com/abc123"

        mock_payees_response = MagicMock()
        mock_payees_response.data.payees = [mock_old_payee]

        mock_updated_payee = MagicMock()
        mock_updated_payee.name = "Amazon"

        mock_update_response = MagicMock()
        mock_update_response.data.payee = mock_updated_payee

        mock_payees_api = MagicMock()
        mock_payees_api.get_payees.return_value = mock_payees_response
        mock_payees_api.update_payee.return_value = mock_update_response

        mock_ynab_api.TransactionsApi.return_value = mock_transactions_api
        mock_ynab_api.PayeesApi.return_value = mock_payees_api

        # Perform batch update
        result = backend.batch_update_merchant("Amazon.com/abc123", "Amazon")

        # Verify batch update succeeded
        assert result["success"] is True
        assert result["payee_id"] == "payee-amazon-old"
        assert result["method"] == "payee_update"

        # Verify payee was updated (in real API, this cascades to all transactions)
        mock_payees_api.update_payee.assert_called_once()
        call_args = mock_payees_api.update_payee.call_args
        assert call_args[1]["budget_id"] == "test-budget-id"
        assert call_args[1]["payee_id"] == "payee-amazon-old"

        # Verify cache was invalidated (so next fetch gets updated data)
        assert backend.client._transaction_cache is None

    def test_find_payee_detects_duplicates(self, backend, mock_ynab_api):
        """Test that _find_or_create_payee detects duplicate payee names."""
        backend.client.budget_id = "test-budget-id"
        backend.client.api_client = MagicMock()

        # Mock duplicate payees with the same name
        mock_payee1 = MagicMock()
        mock_payee1.id = "payee-amazon-1"
        mock_payee1.name = "Amazon"

        mock_payee2 = MagicMock()
        mock_payee2.id = "payee-amazon-2"
        mock_payee2.name = "Amazon"

        mock_payees_response = MagicMock()
        mock_payees_response.data.payees = [mock_payee1, mock_payee2]

        mock_payees_api = MagicMock()
        mock_payees_api.get_payees.return_value = mock_payees_response

        mock_ynab_api.PayeesApi.return_value = mock_payees_api

        # Call should return a dict with warning about duplicates
        result = backend.client._find_or_create_payee("Amazon")

        assert result is not None
        assert "payee" in result
        assert "duplicates_found" in result
        assert result["duplicates_found"] is True
        assert len(result["duplicate_ids"]) == 2
        assert "payee-amazon-1" in result["duplicate_ids"]
        assert "payee-amazon-2" in result["duplicate_ids"]

    def test_find_payee_no_duplicates(self, backend, mock_ynab_api):
        """Test that _find_or_create_payee works normally with unique names."""
        backend.client.budget_id = "test-budget-id"
        backend.client.api_client = MagicMock()

        mock_payee = MagicMock()
        mock_payee.id = "payee-amazon"
        mock_payee.name = "Amazon"

        mock_payees_response = MagicMock()
        mock_payees_response.data.payees = [mock_payee]

        mock_payees_api = MagicMock()
        mock_payees_api.get_payees.return_value = mock_payees_response

        mock_ynab_api.PayeesApi.return_value = mock_payees_api

        result = backend.client._find_or_create_payee("Amazon")

        assert result is not None
        assert "payee" in result
        assert "duplicates_found" in result
        assert result["duplicates_found"] is False
        assert result["duplicate_ids"] == []

    def test_batch_update_merchant_with_duplicates(self, backend, mock_ynab_api):
        """Test that batch_update_merchant warns about duplicate payees."""
        backend.client.budget_id = "test-budget-id"
        backend.client.api_client = MagicMock()

        # Mock duplicate payees with the same name
        mock_payee1 = MagicMock()
        mock_payee1.id = "payee-amazon-1"
        mock_payee1.name = "Amazon.com/abc123"

        mock_payee2 = MagicMock()
        mock_payee2.id = "payee-amazon-2"
        mock_payee2.name = "Amazon.com/abc123"

        mock_payees_response = MagicMock()
        mock_payees_response.data.payees = [mock_payee1, mock_payee2]

        mock_payees_api = MagicMock()
        mock_payees_api.get_payees.return_value = mock_payees_response

        mock_ynab_api.PayeesApi.return_value = mock_payees_api

        result = backend.batch_update_merchant("Amazon.com/abc123", "Amazon")

        # Should fail and report duplicates
        assert result["success"] is False
        assert result["method"] == "duplicate_payees_found"
        assert "duplicate" in result["message"].lower()
        assert "duplicate_ids" in result
        assert len(result["duplicate_ids"]) == 2
