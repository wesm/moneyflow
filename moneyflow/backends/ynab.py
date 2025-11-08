"""
YNAB backend implementation.

Wraps the YNABClient to implement the FinanceBackend interface.
"""

from typing import Any, Dict, List, Optional

from ..ynab_client import YNABClient
from .base import FinanceBackend


class YNABBackend(FinanceBackend):
    """
    YNAB backend implementation.

    This wraps the YNABClient to provide a standardized
    FinanceBackend interface for moneyflow.
    """

    def __init__(self):
        """Initialize the YNAB backend."""
        self.client = YNABClient()
        self._currency_symbol: Optional[str] = None

    async def login(
        self,
        email: Optional[str] = None,
        password: Optional[str] = None,
        use_saved_session: bool = True,
        save_session: bool = True,
        mfa_secret_key: Optional[str] = None,
    ) -> None:
        """
        Authenticate with YNAB using a Personal Access Token.

        Args:
            email: Not used (YNAB uses token auth)
            password: YNAB Personal Access Token
            use_saved_session: Not used (token is stateless)
            save_session: Not used (token is stateless)
            mfa_secret_key: Not used (YNAB doesn't use MFA)

        Raises:
            ValueError: If no token provided or no budgets found
        """
        if not password:
            raise ValueError(
                "YNAB backend requires an access token. "
                "The access token should be stored in the password field."
            )
        self.client.login(password)

    async def get_transactions(
        self,
        limit: int = 100,
        offset: int = 0,
        start_date: Optional[str] = None,
        end_date: Optional[str] = None,
        **kwargs,
    ) -> Dict[str, Any]:
        """
        Fetch transactions from YNAB.

        Args:
            limit: Maximum number of transactions to return
            offset: Number of transactions to skip (for pagination)
            start_date: Filter transactions from this date (ISO format: YYYY-MM-DD)
            end_date: Not supported by YNAB API
            **kwargs: Additional parameters (e.g., hidden_from_reports)

        Returns:
            Dictionary containing transaction data in moneyflow-compatible format
        """
        return self.client.get_transactions(
            limit=limit,
            offset=offset,
            start_date=start_date,
            **kwargs,
        )

    async def get_transaction_categories(self) -> Dict[str, Any]:
        """
        Fetch all available transaction categories from YNAB.

        Returns:
            Dictionary containing categories in moneyflow-compatible format
        """
        return self.client.get_transaction_categories()

    async def get_transaction_category_groups(self) -> Dict[str, Any]:
        """
        Fetch all category groups from YNAB.

        Returns:
            Dictionary containing category groups in moneyflow-compatible format
        """
        return self.client.get_transaction_category_groups()

    async def update_transaction(
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
            transaction_id: Unique identifier of the transaction to update
            merchant_name: New merchant/payee name (if changing)
            category_id: New category ID (if changing)
            hide_from_reports: New hidden status (if changing)
            **kwargs: Additional fields to update

        Returns:
            Dictionary containing the updated transaction data

        Raises:
            Exception: If transaction not found or update fails
        """
        return self.client.update_transaction(
            transaction_id=transaction_id,
            merchant_name=merchant_name,
            category_id=category_id,
            hide_from_reports=hide_from_reports,
            **kwargs,
        )

    async def delete_transaction(self, transaction_id: str) -> bool:
        """
        Delete a transaction from YNAB.

        Args:
            transaction_id: Unique identifier of the transaction to delete

        Returns:
            True if deletion was successful

        Raises:
            Exception: If transaction not found or deletion fails
        """
        return self.client.delete_transaction(transaction_id)

    async def get_all_merchants(self) -> List[str]:
        """
        Get all unique payee/merchant names from YNAB.

        Returns:
            List of merchant names, sorted alphabetically
        """
        return self.client.get_all_merchants()

    def batch_update_merchant(
        self, old_merchant_name: str, new_merchant_name: str
    ) -> Dict[str, Any]:
        """
        Batch update all transactions with a given merchant name (YNAB optimization).

        This is a YNAB-specific optimization that updates the payee once instead
        of updating each transaction individually. This cascades to all transactions
        with that payee, making bulk renames 100x faster.

        **Performance**:
        - Traditional: 100 transactions = 100 API calls
        - Optimized: 100 transactions = 1 API call

        Args:
            old_merchant_name: Current merchant/payee name to rename
            new_merchant_name: New merchant/payee name

        Returns:
            Dictionary with results (see YNABClient.batch_update_merchant for format)

        Example:
            >>> backend = YNABBackend()
            >>> await backend.login(password=token)
            >>> result = backend.batch_update_merchant("Amazon.com/abc", "Amazon")
            >>> if result['success']:
            ...     print(f"Optimized: Updated payee {result['payee_id']}")

        Note:
            This method is synchronous (not async) because the YNAB SDK is synchronous.
            Other backends may not support this optimization.
        """
        return self.client.batch_update_merchant(old_merchant_name, new_merchant_name)

    def get_currency_symbol(self) -> str:
        """
        Get the currency symbol from YNAB budget settings.

        Returns:
            Currency symbol (e.g., "$", "€", "£") from budget's currency_format
        """
        return self.client.currency_symbol

    def clear_auth(self) -> None:
        """
        Clear all authentication state and close the API client.

        This clears the access token, closes connections, and invalidates caches.
        """
        self.client.close()
