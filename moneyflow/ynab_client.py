"""
YNAB API client wrapper.

Wraps the ynab-python SDK to provide a cleaner interface and handle
YNAB-specific data transformations (milliunits, transfers, etc.).
"""

from typing import Any, Dict, List, Optional

import ynab

from .logging_config import get_logger

logger = get_logger(__name__)


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
        self._account_cache: Optional[Dict[str, Dict[str, Any]]] = None

    def login(self, access_token: str, budget_id: Optional[str] = None) -> None:
        """
        Authenticate with YNAB using a Personal Access Token.

        Args:
            access_token: YNAB Personal Access Token
            budget_id: Optional specific budget ID to use

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

        if budget_id:
            # Verify the specified budget exists
            budget = next((b for b in budgets_response.data.budgets if b.id == budget_id), None)
            if not budget:
                raise ValueError(f"Budget with ID '{budget_id}' not found")
            self.budget_id = budget_id
        elif not self.budget_id:
            # Use first budget if no budget specified
            budget = budgets_response.data.budgets[0]
            self.budget_id = budget.id
        else:
            # Use existing budget_id
            budget = next(
                (b for b in budgets_response.data.budgets if b.id == self.budget_id), None
            )

        # Fetch currency symbol from budget settings
        if budget and budget.currency_format and budget.currency_format.currency_symbol:
            self.currency_symbol = budget.currency_format.currency_symbol

        # Fetch and cache account information (including on_budget status)
        self._fetch_and_cache_accounts()

    def get_budgets(self) -> List[Dict[str, Any]]:
        """
        Get all budgets from YNAB account.

        Returns:
            List of budget dictionaries with id, name, and last_modified_on fields

        Raises:
            ValueError: If not authenticated
        """
        if not self.api_client:
            raise ValueError("Must authenticate first")

        budgets_api = ynab.BudgetsApi(self.api_client)
        budgets_response = budgets_api.get_budgets()

        return [
            {
                "id": budget.id,
                "name": budget.name,
                "last_modified_on": str(budget.last_modified_on)
                if budget.last_modified_on
                else None,
                "currency_format": {
                    "currency_symbol": budget.currency_format.currency_symbol
                    if budget.currency_format
                    else "$"
                },
            }
            for budget in budgets_response.data.budgets
        ]

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
            payee_result = self._find_or_create_payee(merchant_name)
            if payee_result["payee"]:
                update_data.payee_id = payee_result["payee"].id
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

    def update_payee(self, payee_id: str, new_name: str) -> bool:
        """
        Update a payee's name via the YNAB API.

        This method updates the payee name, which cascades to ALL transactions
        that reference this payee_id. This is much more efficient than updating
        transactions individually.

        Args:
            payee_id: The unique identifier of the payee to update
            new_name: The new name for the payee (max 500 characters)

        Returns:
            True if the payee was successfully updated, False otherwise

        Example:
            >>> client = YNABClient()
            >>> client.login(token)
            >>> success = client.update_payee("payee-123", "Amazon")
            >>> # All transactions with payee_id "payee-123" now show "Amazon"
        """
        self._ensure_authenticated()

        if not new_name or len(new_name) > 500:
            logger.error(f"Invalid payee name: must be 1-500 characters, got {len(new_name)}")
            return False

        try:
            payees_api = ynab.PayeesApi(self.api_client)

            # Create the update payload
            save_payee = ynab.SavePayee(name=new_name)
            wrapper = ynab.PatchPayeeWrapper(payee=save_payee)

            # Call the PATCH /payees/{payee_id} endpoint
            response = payees_api.update_payee(
                budget_id=self.budget_id, payee_id=payee_id, data=wrapper
            )

            logger.info(
                f"Successfully updated payee {payee_id} to '{new_name}' "
                f"(API returned: {response.data.payee.name})"
            )

            # Invalidate transaction cache since payee names may have changed
            self._invalidate_cache()

            return True

        except Exception as e:
            logger.error(f"Failed to update payee {payee_id}: {e}", exc_info=True)
            return False

    def batch_update_merchant(
        self, old_merchant_name: str, new_merchant_name: str
    ) -> Dict[str, Any]:
        """
        Batch update all transactions with a given merchant name.

        This is a highly optimized alternative to updating transactions individually.
        Instead of calling update_transaction() N times (one per transaction), this
        finds the payee and updates it once, which cascades to all transactions.

        **Performance**: 100x faster than individual updates for large batches.
        - Traditional: 100 transactions = 100 API calls
        - Optimized: 100 transactions = 1 API call

        Args:
            old_merchant_name: Current merchant/payee name to rename
            new_merchant_name: New merchant/payee name

        Returns:
            Dictionary with results:
            - success: True if payee was found and updated
            - payee_id: ID of the updated payee (if successful)
            - transactions_affected: Estimated count (if available)
            - method: "payee_update" for this optimized path

        Example:
            >>> client = YNABClient()
            >>> client.login(token)
            >>> result = client.batch_update_merchant("Amazon.com/abc123", "Amazon")
            >>> print(f"Updated {result['transactions_affected']} transactions")

        Note:
            Falls back to creating a new payee if old merchant not found.
            In this case, future transactions will use the new name, but
            existing transactions keep their old merchant name.
        """
        self._ensure_authenticated()

        # Skip no-op renames to avoid creating duplicate payees
        if old_merchant_name == new_merchant_name:
            logger.info(f"Skipping no-op rename: '{old_merchant_name}' -> '{new_merchant_name}'")
            return {
                "success": True,
                "payee_id": None,
                "transactions_affected": 0,
                "method": "no_change",
                "message": "Old and new names are the same, no update needed",
            }

        logger.info(f"Batch updating merchant: '{old_merchant_name}' -> '{new_merchant_name}'")

        # Find the payee for the old merchant name
        payee_result = self._find_or_create_payee(old_merchant_name)

        if not payee_result["payee"]:
            logger.warning(
                f"Payee '{old_merchant_name}' not found. "
                "This merchant may not exist or transactions use payee_name directly."
            )
            return {
                "success": False,
                "payee_id": None,
                "transactions_affected": 0,
                "method": "payee_not_found",
                "message": f"Payee '{old_merchant_name}' not found",
            }

        # Check for duplicates - abort if found
        if payee_result["duplicates_found"]:
            logger.error(
                f"Found duplicate payees with name '{old_merchant_name}': "
                f"{payee_result['duplicate_ids']}. Cannot safely perform batch update. "
                "Please merge duplicate payees in YNAB first."
            )
            return {
                "success": False,
                "payee_id": None,
                "transactions_affected": 0,
                "method": "duplicate_payees_found",
                "message": (
                    f"Found {len(payee_result['duplicate_ids'])} duplicate payees "
                    f"with name '{old_merchant_name}'. Merge duplicates in YNAB first."
                ),
                "duplicate_ids": payee_result["duplicate_ids"],
            }

        old_payee = payee_result["payee"]

        # Check if target payee name already exists
        target_payee_result = self._find_or_create_payee(new_merchant_name)
        if target_payee_result["payee"]:
            # Check for duplicates in target - abort if found
            if target_payee_result["duplicates_found"]:
                logger.error(
                    f"Found duplicate target payees with name '{new_merchant_name}': "
                    f"{target_payee_result['duplicate_ids']}. Cannot safely perform batch merge. "
                    "Please merge duplicate payees in YNAB first."
                )
                return {
                    "success": False,
                    "payee_id": None,
                    "transactions_affected": 0,
                    "method": "duplicate_target_payees_found",
                    "message": (
                        f"Target payee '{new_merchant_name}' has "
                        f"{len(target_payee_result['duplicate_ids'])} duplicates. "
                        "Merge duplicates in YNAB first."
                    ),
                    "duplicate_ids": target_payee_result["duplicate_ids"],
                }

            # Target payee already exists - use batch transaction update to merge
            target_payee = target_payee_result["payee"]
            logger.info(
                f"Target payee '{new_merchant_name}' already exists (id={target_payee.id}). "
                f"Using batch transaction update to reassign from {old_payee.id} to {target_payee.id}."
            )
            return self._batch_reassign_transactions(
                old_payee_id=old_payee.id,
                target_payee_id=target_payee.id,
                old_merchant_name=old_merchant_name,
                new_merchant_name=new_merchant_name,
            )

        # Target payee doesn't exist - update the payee name (cascades to all transactions)
        success = self.update_payee(old_payee.id, new_merchant_name)

        if success:
            logger.info(
                f"Successfully batch-updated payee {old_payee.id}: "
                f"'{old_merchant_name}' -> '{new_merchant_name}'"
            )
            return {
                "success": True,
                "payee_id": old_payee.id,
                "transactions_affected": -1,  # YNAB doesn't provide this count
                "method": "payee_update",
                "message": f"Updated payee {old_payee.id} from '{old_merchant_name}' to '{new_merchant_name}'",
            }
        else:
            logger.error(f"Failed to update payee {old_payee.id}")
            return {
                "success": False,
                "payee_id": old_payee.id,
                "transactions_affected": 0,
                "method": "payee_update_failed",
                "message": f"Failed to update payee {old_payee.id}",
            }

    def get_transaction_count_by_payee(self, payee_name: str) -> int:
        """
        Return count of transactions with the given payee name.

        This is used to determine if a batch merchant rename would affect
        more transactions than are currently selected in the queue.

        Args:
            payee_name: The payee name to count transactions for

        Returns:
            Number of transactions with this payee (0 if payee not found)
        """
        self._ensure_authenticated()

        payee_result = self._find_or_create_payee(payee_name)
        if not payee_result["payee"]:
            return 0

        transactions_api = ynab.TransactionsApi(self.api_client)
        response = transactions_api.get_transactions_by_payee(
            budget_id=self.budget_id, payee_id=payee_result["payee"].id
        )
        return len(response.data.transactions)

    def close(self) -> None:
        """Close the API client and clear all state."""
        # Note: ynab.ApiClient doesn't have a close() method
        # Just clear the references
        self.api_client = None
        self.access_token = None
        self.budget_id = None
        self._invalidate_cache()
        self._account_cache = None

    def _batch_reassign_transactions(
        self,
        old_payee_id: str,
        target_payee_id: str,
        old_merchant_name: str,
        new_merchant_name: str,
    ) -> Dict[str, Any]:
        """
        Batch reassign all transactions from one payee to another.

        This is used when merging merchants - instead of renaming the old payee
        (which would create a duplicate), we reassign all its transactions to
        the existing target payee using YNAB's batch update API.

        **Performance**: 2 API calls total regardless of transaction count:
        1. get_transactions_by_payee - fetch all transactions for old payee
        2. update_transactions - batch reassign to target payee

        Args:
            old_payee_id: ID of the payee being merged from
            target_payee_id: ID of the existing payee to merge into
            old_merchant_name: Name of old payee (for logging)
            new_merchant_name: Name of target payee (for logging)

        Returns:
            Dictionary with results (same format as batch_update_merchant)
        """
        # Guard against attempting to reassign payee to itself
        if old_payee_id == target_payee_id:
            logger.warning(
                f"Attempted to reassign payee {old_payee_id} to itself. "
                f"This suggests '{old_merchant_name}' and '{new_merchant_name}' "
                "are the same payee (possible duplicate)."
            )
            return {
                "success": False,
                "payee_id": old_payee_id,
                "transactions_affected": 0,
                "method": "same_payee_error",
                "message": (
                    f"Cannot reassign: '{old_merchant_name}' and '{new_merchant_name}' "
                    f"are the same payee (id={old_payee_id})"
                ),
            }

        try:
            transactions_api = ynab.TransactionsApi(self.api_client)

            # Get all transactions for the old payee
            response = transactions_api.get_transactions_by_payee(
                budget_id=self.budget_id, payee_id=old_payee_id
            )
            transactions = response.data.transactions

            if not transactions:
                logger.info(
                    f"No transactions found for payee {old_payee_id} ('{old_merchant_name}')"
                )
                return {
                    "success": True,
                    "payee_id": old_payee_id,
                    "transactions_affected": 0,
                    "method": "batch_reassign",
                    "message": f"No transactions to reassign from '{old_merchant_name}'",
                }

            # Build batch update payload - only set id and payee_id for each transaction
            update_list = [
                ynab.SaveTransactionWithIdOrImportId.model_validate(
                    {"id": txn.id, "payee_id": target_payee_id}
                )
                for txn in transactions
            ]

            # Execute batch update
            wrapper = ynab.PatchTransactionsWrapper(transactions=update_list)
            transactions_api.update_transactions(budget_id=self.budget_id, data=wrapper)

            self._invalidate_cache()

            logger.info(
                f"Successfully batch-reassigned {len(transactions)} transactions "
                f"from '{old_merchant_name}' ({old_payee_id}) to '{new_merchant_name}' ({target_payee_id})"
            )
            return {
                "success": True,
                "payee_id": target_payee_id,
                "transactions_affected": len(transactions),
                "method": "batch_reassign",
                "message": (
                    f"Reassigned {len(transactions)} transactions from "
                    f"'{old_merchant_name}' to existing payee '{new_merchant_name}'"
                ),
            }

        except Exception as e:
            logger.error(
                f"Failed to batch reassign transactions from {old_payee_id} to {target_payee_id}: {e}",
                exc_info=True,
            )
            return {
                "success": False,
                "payee_id": old_payee_id,
                "transactions_affected": 0,
                "method": "batch_reassign_failed",
                "message": f"Failed to batch reassign: {e}",
            }

    def _ensure_authenticated(self) -> None:
        """Ensure client is authenticated before API calls."""
        if not self.api_client or not self.budget_id:
            raise ValueError("Must call login() before making API requests")

    def _invalidate_cache(self) -> None:
        """Clear the transaction cache."""
        self._transaction_cache = None
        self._cache_params = None

    def _fetch_and_cache_accounts(self) -> None:
        """
        Fetch all accounts from YNAB and cache account metadata.

        Caches account information including on_budget status to determine
        if transactions should be hidden from reports (tracking accounts).
        """
        self._ensure_authenticated()

        accounts_api = ynab.AccountsApi(self.api_client)
        response = accounts_api.get_accounts(budget_id=self.budget_id)

        self._account_cache = {
            account.id: {
                "id": account.id,
                "name": account.name,
                "on_budget": account.on_budget,
                "closed": account.closed,
                "type": str(account.type) if account.type else "unknown",
            }
            for account in response.data.accounts
        }

        tracking_count = sum(1 for a in self._account_cache.values() if not a["on_budget"])
        logger.info(
            f"Cached {len(self._account_cache)} accounts "
            f"({tracking_count} tracking, {len(self._account_cache) - tracking_count} budget)"
        )

    def _convert_transaction(self, txn: Any) -> Dict[str, Any]:
        """
        Convert a YNAB transaction to moneyflow-compatible format.

        Transactions are hidden from reports if:
        1. They are deleted (txn.deleted)
        2. They are transfers (txn.transfer_account_id is not None)
        3. They belong to a tracking account (on_budget=False)

        Args:
            txn: YNAB TransactionDetail object

        Returns:
            Dictionary in moneyflow format
        """
        # Check if transaction belongs to a tracking account
        is_tracking_account = False
        if self._account_cache and txn.account_id in self._account_cache:
            is_tracking_account = not self._account_cache[txn.account_id]["on_budget"]

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
            "hideFromReports": (
                txn.deleted or txn.transfer_account_id is not None or is_tracking_account
            ),
            "pending": txn.cleared == "uncleared",
            "isRecurring": False,
        }

    def _find_or_create_payee(self, merchant_name: str) -> Dict[str, Any]:
        """
        Find a payee by name and detect duplicates.

        Args:
            merchant_name: Payee name to search for

        Returns:
            Dictionary with:
            - payee: The payee object (first match) or None
            - duplicates_found: True if multiple payees with same name exist
            - duplicate_ids: List of all payee IDs with matching names
        """
        payees_api = ynab.PayeesApi(self.api_client)
        response = payees_api.get_payees(budget_id=self.budget_id)

        # Find all payees with matching name
        # Note: Linear search is intentional - we need ALL matches to detect duplicates
        # Using a dict would only find one match and miss duplicate detection
        matching_payees = [p for p in response.data.payees if p.name == merchant_name]

        if not matching_payees:
            return {
                "payee": None,
                "duplicates_found": False,
                "duplicate_ids": [],
            }

        # Detect duplicates
        duplicates_found = len(matching_payees) > 1
        duplicate_ids = [p.id for p in matching_payees] if duplicates_found else []

        if duplicates_found:
            logger.warning(
                f"Found {len(matching_payees)} payees with name '{merchant_name}': {duplicate_ids}. "
                "Using first match, but this may cause unexpected behavior. "
                "Consider merging duplicate payees in YNAB."
            )

        return {
            "payee": matching_payees[0],
            "duplicates_found": duplicates_found,
            "duplicate_ids": duplicate_ids,
        }
