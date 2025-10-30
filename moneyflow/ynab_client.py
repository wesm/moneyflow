"""
YNAB API client wrapper.

Wraps the ynab-python SDK to provide a cleaner interface and handle
YNAB-specific data transformations (milliunits, transfers, etc.).
"""

from typing import Any, Dict, List, Optional

import ynab


class YNABClient:
    """
    Wrapper around the YNAB Python SDK.

    Handles authentication, data transformation, and caching for optimal performance.
    """

    def __init__(self):
        """Initialize the YNAB client."""
        self.api_client: Optional[ynab.ApiClient] = None
        self.access_token: Optional[str] = None
        self.budget_id: Optional[str] = None
        self.currency_symbol: str = "$"  # Default to USD, updated during login
        self._transaction_cache: Optional[List[Dict[str, Any]]] = None
        self._cache_params: Optional[Dict[str, Any]] = None

    def login(self, access_token: str) -> None:
        """
        Authenticate with YNAB using a Personal Access Token.

        Args:
            access_token: YNAB Personal Access Token

        Raises:
            ValueError: If no budgets found or token is invalid
        """
        if not access_token:
            raise ValueError("YNAB access token cannot be empty")

        self.access_token = access_token.strip()
        configuration = ynab.Configuration(access_token=self.access_token)
        self.api_client = ynab.ApiClient(configuration)

        budgets_api = ynab.BudgetsApi(self.api_client)
        budgets_response = budgets_api.get_budgets()

        if not budgets_response.data.budgets:
            raise ValueError("No budgets found in YNAB account")

        if not self.budget_id:
            budget = budgets_response.data.budgets[0]
            self.budget_id = budget.id

            # Fetch currency symbol from budget settings
            if budget.currency_format and budget.currency_format.currency_symbol:
                self.currency_symbol = budget.currency_format.currency_symbol

    def get_transactions(
        self,
        limit: int = 100,
        offset: int = 0,
        start_date: Optional[str] = None,
        hidden_from_reports: Optional[bool] = None,
        **kwargs,
    ) -> Dict[str, Any]:
        """
        Fetch transactions from YNAB.

        YNAB API returns all transactions at once (no native pagination),
        so we cache them and handle pagination + filtering client-side.

        Args:
            limit: Maximum number of transactions to return
            offset: Number of transactions to skip
            start_date: Filter transactions from this date (ISO format)
            hidden_from_reports: Filter by visibility status
            **kwargs: Additional parameters (ignored)

        Returns:
            Dictionary in moneyflow-compatible format with allTransactions structure
        """
        self._ensure_authenticated()

        cache_key = {"start_date": start_date}

        if self._transaction_cache is None or self._cache_params != cache_key:
            transactions_api = ynab.TransactionsApi(self.api_client)

            if start_date:
                response = transactions_api.get_transactions(
                    budget_id=self.budget_id, since_date=start_date
                )
            else:
                response = transactions_api.get_transactions(budget_id=self.budget_id)

            self._transaction_cache = [
                self._convert_transaction(txn) for txn in response.data.transactions
            ]
            self._cache_params = cache_key

        filtered = self._transaction_cache
        if hidden_from_reports is not None:
            filtered = [txn for txn in filtered if txn["hideFromReports"] == hidden_from_reports]

        return {
            "allTransactions": {
                "totalCount": len(filtered),
                "results": filtered[offset : offset + limit],
            }
        }

    def get_transaction_categories(self) -> Dict[str, Any]:
        """
        Fetch all transaction categories from YNAB.

        Returns:
            Dictionary with categories list in moneyflow-compatible format
        """
        self._ensure_authenticated()

        categories_api = ynab.CategoriesApi(self.api_client)
        response = categories_api.get_categories(budget_id=self.budget_id)

        categories = []
        for category_group in response.data.category_groups:
            for category in category_group.categories:
                categories.append(
                    {
                        "id": category.id,
                        "name": category.name,
                        "group": {
                            "id": category_group.id,
                            "name": category_group.name,
                            "type": "expense",
                        },
                    }
                )

        return {"categories": categories}

    def get_transaction_category_groups(self) -> Dict[str, Any]:
        """
        Fetch all category groups from YNAB.

        Returns:
            Dictionary with categoryGroups list in moneyflow-compatible format
        """
        self._ensure_authenticated()

        categories_api = ynab.CategoriesApi(self.api_client)
        response = categories_api.get_categories(budget_id=self.budget_id)

        category_groups = [
            {
                "id": group.id,
                "name": group.name,
                "type": "expense",
            }
            for group in response.data.category_groups
        ]

        return {"categoryGroups": category_groups}

    def update_transaction(
        self,
        transaction_id: str,
        merchant_name: Optional[str] = None,
        category_id: Optional[str] = None,
        hide_from_reports: Optional[bool] = None,
        **kwargs,
    ) -> Dict[str, Any]:
        """
        Update a transaction in YNAB.

        Args:
            transaction_id: Transaction ID to update
            merchant_name: New payee/merchant name
            category_id: New category ID
            hide_from_reports: New hidden status (maps to YNAB's deleted field)
            **kwargs: Additional fields (ignored)

        Returns:
            Dictionary with updated transaction ID
        """
        self._ensure_authenticated()

        transactions_api = ynab.TransactionsApi(self.api_client)

        txn_response = transactions_api.get_transaction_by_id(
            budget_id=self.budget_id, transaction_id=transaction_id
        )
        existing_txn = txn_response.data.transaction

        # Use ExistingTransaction for updates (required by PutTransactionWrapper)
        # Using model_validate to avoid pyright issues with Pydantic v2 __init__
        update_data = ynab.ExistingTransaction.model_validate(
            {
                "account_id": existing_txn.account_id,
                "var_date": existing_txn.var_date,
                "amount": existing_txn.amount,
            }
        )

        if merchant_name is not None:
            payee = self._find_or_create_payee(merchant_name)
            if payee:
                update_data.payee_id = payee.id
            else:
                update_data.payee_name = merchant_name

        if category_id is not None:
            update_data.category_id = category_id

        # Note: YNAB API doesn't support setting deleted via update
        # The deleted field is read-only. To "hide" transactions,
        # we would need to actually delete them, which we avoid here.

        updated = transactions_api.update_transaction(
            budget_id=self.budget_id,
            transaction_id=transaction_id,
            data=ynab.PutTransactionWrapper(transaction=update_data),
        )

        self._invalidate_cache()

        return {"updateTransaction": {"transaction": {"id": updated.data.transaction.id}}}

    def delete_transaction(self, transaction_id: str) -> bool:
        """
        Delete a transaction from YNAB.

        Args:
            transaction_id: Transaction ID to delete

        Returns:
            True if successful, False otherwise
        """
        self._ensure_authenticated()

        try:
            transactions_api = ynab.TransactionsApi(self.api_client)
            transactions_api.delete_transaction(
                budget_id=self.budget_id, transaction_id=transaction_id
            )
            self._invalidate_cache()
            return True
        except Exception:
            return False

    def get_all_merchants(self) -> List[str]:
        """
        Get all payee/merchant names from YNAB.

        Returns:
            Sorted list of merchant names
        """
        self._ensure_authenticated()

        payees_api = ynab.PayeesApi(self.api_client)
        response = payees_api.get_payees(budget_id=self.budget_id)

        return sorted(payee.name for payee in response.data.payees)

    def close(self) -> None:
        """Close the API client and clear all state."""
        # Note: ynab.ApiClient doesn't have a close() method
        # Just clear the references
        self.api_client = None
        self.access_token = None
        self.budget_id = None
        self._invalidate_cache()

    def _ensure_authenticated(self) -> None:
        """Ensure client is authenticated before API calls."""
        if not self.api_client or not self.budget_id:
            raise ValueError("Must call login() before making API requests")

    def _invalidate_cache(self) -> None:
        """Clear the transaction cache."""
        self._transaction_cache = None
        self._cache_params = None

    def _convert_transaction(self, txn: Any) -> Dict[str, Any]:
        """
        Convert a YNAB transaction to moneyflow-compatible format.

        Args:
            txn: YNAB TransactionDetail object

        Returns:
            Dictionary in moneyflow format
        """
        return {
            "id": txn.id,
            "date": str(txn.var_date),
            "amount": float(txn.amount) / 1000.0,
            "merchant": {
                "id": txn.payee_id or "unknown",
                "name": txn.payee_name or "Unknown",
            },
            "category": {
                "id": txn.category_id or "uncategorized",
                "name": txn.category_name or "Uncategorized",
            },
            "account": {
                "id": txn.account_id,
                "displayName": txn.account_name,
            },
            "notes": txn.memo or "",
            "hideFromReports": txn.deleted or txn.transfer_account_id is not None,
            "pending": txn.cleared == "uncleared",
            "isRecurring": False,
        }

    def _find_or_create_payee(self, merchant_name: str) -> Optional[Any]:
        """
        Find a payee by name.

        Args:
            merchant_name: Payee name to search for

        Returns:
            Payee object if found, None otherwise
        """
        payees_api = ynab.PayeesApi(self.api_client)
        response = payees_api.get_payees(budget_id=self.budget_id)
        return next((p for p in response.data.payees if p.name == merchant_name), None)
