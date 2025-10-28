from typing import Any, Dict, List, Optional

import ynab

from .base import FinanceBackend


class YNABBackend(FinanceBackend):
    def __init__(self):
        self.api_client: Optional[ynab.ApiClient] = None
        self.access_token: Optional[str] = None
        self.budget_id: Optional[str] = None
        self._transaction_cache: Optional[List[Dict[str, Any]]] = None
        self._cache_params: Optional[Dict[str, Any]] = None

    async def login(
        self,
        email: Optional[str] = None,
        password: Optional[str] = None,
        use_saved_session: bool = True,
        save_session: bool = True,
        mfa_secret_key: Optional[str] = None,
    ) -> None:
        if not password:
            raise ValueError(
                "YNAB backend requires an access token. "
                "The access token should be stored in the password field."
            )

        self.access_token = password.strip()

        if not self.access_token:
            raise ValueError("YNAB access token cannot be empty")

        configuration = ynab.Configuration(access_token=self.access_token)
        self.api_client = ynab.ApiClient(configuration)

        try:
            budgets_api = ynab.BudgetsApi(self.api_client)
            budgets_response = budgets_api.get_budgets()

            if not budgets_response.data.budgets:
                raise ValueError("No budgets found in YNAB account")

            if not self.budget_id:
                self.budget_id = budgets_response.data.budgets[0].id

        except Exception as e:
            error_msg = str(e)
            if "401" in error_msg or "Unauthorized" in error_msg:
                raise RuntimeError(
                    "YNAB authentication failed: Invalid or expired access token. "
                    "Please verify your Personal Access Token is correct. "
                    "You can get a new token from: Account Settings → Developer Settings → New Token"
                )
            elif "403" in error_msg or "Forbidden" in error_msg:
                raise RuntimeError(
                    "YNAB authentication failed: Access forbidden. "
                    "Please check that your access token has the required permissions."
                )
            elif "404" in error_msg:
                raise RuntimeError(
                    "YNAB API endpoint not found. Please check your internet connection."
                )
            else:
                raise RuntimeError(f"Failed to connect to YNAB: {error_msg}")

    def set_access_token(self, access_token: str) -> None:
        self.access_token = access_token

    def set_budget_id(self, budget_id: str) -> None:
        self.budget_id = budget_id

    async def get_transactions(
        self,
        limit: int = 100,
        offset: int = 0,
        start_date: Optional[str] = None,
        end_date: Optional[str] = None,
        **kwargs,
    ) -> Dict[str, Any]:
        if not self.api_client or not self.budget_id:
            raise ValueError("Must call login() before get_transactions()")

        hidden_from_reports = kwargs.get("hidden_from_reports")
        cache_key = {"start_date": start_date, "end_date": end_date}
        
        if self._transaction_cache is None or self._cache_params != cache_key:
            transactions_api = ynab.TransactionsApi(self.api_client)

            try:
                if start_date:
                    transactions_response = transactions_api.get_transactions(
                        budget_id=self.budget_id, since_date=start_date
                    )
                else:
                    transactions_response = transactions_api.get_transactions(budget_id=self.budget_id)

                ynab_transactions = transactions_response.data.transactions

                converted_transactions = []
                for txn in ynab_transactions:
                    converted_transactions.append(
                        {
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
                    )

                self._transaction_cache = converted_transactions
                self._cache_params = cache_key

            except Exception as e:
                raise RuntimeError(f"Failed to fetch transactions from YNAB: {str(e)}")

        filtered_transactions = self._transaction_cache
        if hidden_from_reports is not None:
            filtered_transactions = [
                txn for txn in self._transaction_cache
                if txn["hideFromReports"] == hidden_from_reports
            ]

        return {
            "allTransactions": {
                "totalCount": len(filtered_transactions),
                "results": filtered_transactions[offset : offset + limit],
            }
        }

    async def get_transaction_categories(self) -> Dict[str, Any]:
        if not self.api_client or not self.budget_id:
            raise ValueError("Must call login() before get_transaction_categories()")

        try:
            categories_api = ynab.CategoriesApi(self.api_client)
            categories_response = categories_api.get_categories(budget_id=self.budget_id)

            categories = []
            for category_group in categories_response.data.category_groups:
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
        except Exception as e:
            raise RuntimeError(f"Failed to fetch categories from YNAB: {str(e)}")

    async def get_transaction_category_groups(self) -> Dict[str, Any]:
        if not self.api_client or not self.budget_id:
            raise ValueError("Must call login() before get_transaction_category_groups()")

        try:
            categories_api = ynab.CategoriesApi(self.api_client)
            categories_response = categories_api.get_categories(budget_id=self.budget_id)

            category_groups = []
            for category_group in categories_response.data.category_groups:
                category_groups.append(
                    {
                        "id": category_group.id,
                        "name": category_group.name,
                        "type": "expense",
                    }
                )

            return {"categoryGroups": category_groups}
        except Exception as e:
            raise RuntimeError(f"Failed to fetch category groups from YNAB: {str(e)}")

    async def update_transaction(
        self,
        transaction_id: str,
        merchant_name: Optional[str] = None,
        category_id: Optional[str] = None,
        hide_from_reports: Optional[bool] = None,
        **kwargs,
    ) -> Dict[str, Any]:
        if not self.api_client or not self.budget_id:
            raise ValueError("Must call login() before update_transaction()")

        transactions_api = ynab.TransactionsApi(self.api_client)

        txn_response = transactions_api.get_transaction_by_id(
            budget_id=self.budget_id, transaction_id=transaction_id
        )
        existing_txn = txn_response.data.transaction

        update_data = ynab.SaveTransactionWithOptionalFields(
            account_id=existing_txn.account_id,
            date=existing_txn.var_date,
            amount=existing_txn.amount,
        )

        if merchant_name is not None:
            payees_api = ynab.PayeesApi(self.api_client)
            payees_response = payees_api.get_payees(budget_id=self.budget_id)
            payee = next(
                (p for p in payees_response.data.payees if p.name == merchant_name),
                None,
            )
            if payee:
                update_data.payee_id = payee.id
            else:
                update_data.payee_name = merchant_name

        if category_id is not None:
            update_data.category_id = category_id

        if hide_from_reports is not None:
            update_data.deleted = hide_from_reports

        updated_txn_response = transactions_api.update_transaction(
            budget_id=self.budget_id,
            transaction_id=transaction_id,
            data=ynab.PutTransactionWrapper(transaction=update_data),
        )

        self._transaction_cache = None

        return {
            "updateTransaction": {"transaction": {"id": updated_txn_response.data.transaction.id}}
        }

    async def delete_transaction(self, transaction_id: str) -> bool:
        if not self.api_client or not self.budget_id:
            raise ValueError("Must call login() before delete_transaction()")

        transactions_api = ynab.TransactionsApi(self.api_client)

        try:
            transactions_api.delete_transaction(
                budget_id=self.budget_id, transaction_id=transaction_id
            )
            self._transaction_cache = None
            return True
        except Exception:
            return False

    async def get_all_merchants(self) -> List[str]:
        if not self.api_client or not self.budget_id:
            raise ValueError("Must call login() before get_all_merchants()")

        payees_api = ynab.PayeesApi(self.api_client)
        payees_response = payees_api.get_payees(budget_id=self.budget_id)

        merchants = [payee.name for payee in payees_response.data.payees]
        return sorted(merchants)

    def clear_auth(self) -> None:
        if self.api_client:
            self.api_client.close()
            self.api_client = None
        self.access_token = None
        self._transaction_cache = None
        self._cache_params = None
