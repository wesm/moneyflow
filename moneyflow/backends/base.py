"""
Abstract base class for finance backend implementations.

This defines the interface that all finance backends (Monarch, YNAB, etc.) must implement
to be compatible with moneyflow.
"""

from abc import ABC, abstractmethod
from typing import Any, Dict, List, Optional


class FinanceBackend(ABC):
    """
    Abstract base class for finance backend implementations.

    All backends must implement these methods to be compatible with moneyflow.
    This allows the UI layer to work with any supported finance platform.
    """

    @abstractmethod
    async def login(
        self,
        email: Optional[str] = None,
        password: Optional[str] = None,
        use_saved_session: bool = True,
        save_session: bool = True,
        mfa_secret_key: Optional[str] = None,
    ) -> None:
        """
        Authenticate with the finance backend.

        Args:
            email: User's email address (if applicable)
            password: User's password (if applicable)
            use_saved_session: Whether to try using a saved session
            save_session: Whether to save the session for future use
            mfa_secret_key: MFA/2FA secret key (if applicable)

        Raises:
            Exception: If login fails (backend-specific exceptions)
        """
        pass

    @abstractmethod
    async def get_transactions(
        self,
        limit: int = 100,
        offset: int = 0,
        start_date: Optional[str] = None,
        end_date: Optional[str] = None,
        **kwargs,
    ) -> Dict[str, Any]:
        """
        Fetch transactions from the backend.

        Args:
            limit: Maximum number of transactions to return
            offset: Number of transactions to skip (for pagination)
            start_date: Filter transactions from this date (ISO format: YYYY-MM-DD)
            end_date: Filter transactions to this date (ISO format: YYYY-MM-DD)
            **kwargs: Backend-specific additional parameters

        Returns:
            Dictionary containing:
                - 'allTransactions' or 'results': List of transaction dictionaries
                - 'totalCount' (optional): Total number of transactions matching filters

            Each transaction dict must contain at minimum:
                - id: Unique transaction identifier
                - date: Transaction date (ISO format: YYYY-MM-DD)
                - amount: Transaction amount (negative for expenses, positive for income)
                - merchant: Dict with 'id' and 'name' keys
                - category: Dict with 'id' and 'name' keys
                - account: Dict with 'id' and 'displayName' keys
                - notes: Transaction notes/memo (string)
                - hideFromReports: Boolean indicating if hidden from reports
                - pending: Boolean indicating if transaction is pending
                - isRecurring: Boolean indicating if transaction is recurring
        """
        pass

    @abstractmethod
    async def get_transaction_categories(self) -> Dict[str, Any]:
        """
        Fetch all available transaction categories.

        Returns:
            Dictionary containing:
                - 'categories': List of category dictionaries

            Each category dict must contain:
                - id: Unique category identifier
                - name: Human-readable category name
                - group: Dict with 'id' and 'type' keys (optional)
        """
        pass

    @abstractmethod
    async def get_transaction_category_groups(self) -> Dict[str, Any]:
        """
        Fetch all category groups (high-level category organization).

        Returns:
            Dictionary containing:
                - 'categoryGroups': List of category group dictionaries

            Each group dict must contain:
                - id: Unique group identifier
                - name: Human-readable group name
                - type: Group type (e.g., 'expense', 'income', 'transfer')
        """
        pass

    @abstractmethod
    async def update_transaction(
        self,
        transaction_id: str,
        merchant_name: Optional[str] = None,
        category_id: Optional[str] = None,
        hide_from_reports: Optional[bool] = None,
        **kwargs,
    ) -> Dict[str, Any]:
        """
        Update a transaction's properties.

        Args:
            transaction_id: Unique identifier of the transaction to update
            merchant_name: New merchant name (if changing)
            category_id: New category ID (if changing)
            hide_from_reports: New hidden status (if changing)
            **kwargs: Backend-specific additional fields to update

        Returns:
            Dictionary containing the updated transaction data.
            Minimum format: {'updateTransaction': {'transaction': {'id': transaction_id}}}

        Raises:
            Exception: If transaction not found or update fails
        """
        pass

    @abstractmethod
    async def delete_transaction(self, transaction_id: str) -> bool:
        """
        Delete a transaction permanently.

        Args:
            transaction_id: Unique identifier of the transaction to delete

        Returns:
            True if deletion was successful

        Raises:
            Exception: If transaction not found or deletion fails
        """
        pass

    @abstractmethod
    async def get_all_merchants(self) -> List[str]:
        """
        Get all unique merchant names from all transactions.

        Uses aggregation to fetch distinct merchants without downloading all transactions.
        This is much faster than fetching all transaction details.

        Returns:
            List of merchant names, sorted alphabetically

        Example:
            >>> merchants = await backend.get_all_merchants()
            >>> print(f"Found {len(merchants)} merchants")
        """
        pass

    def get_display_labels(self) -> Dict[str, str]:
        """
        Get backend-specific display labels for UI elements.

        This allows backends to customize how fields are displayed in the UI.
        For example, Amazon backend shows "Item Name" instead of "Merchant",
        and "Order" instead of "Account".

        Returns:
            Dictionary mapping standard field names to display names.
            Default returns standard labels (no customization).

        Example:
            >>> backend.get_display_labels()
            {'merchant': 'Merchant', 'account': 'Account', 'accounts': 'Accounts'}
        """
        return {
            "merchant": "Merchant",
            "account": "Account",
            "accounts": "Accounts",
        }

    def get_column_config(self) -> Dict[str, Any]:
        """
        Get backend-specific column display configuration.

        This allows backends to customize column widths and other display properties.
        For example, Amazon backend uses wider columns for Item Names since product
        names are typically longer than merchant names.

        Returns:
            Dictionary with column configuration:
            - merchant_width_pct: Percentage width for merchant column (default: 25)
            - account_width_pct: Percentage width for account column (default: 30)
            - Other columns auto-size based on remaining space

        Example:
            >>> backend.get_column_config()
            {'merchant_width_pct': 40, 'account_width_pct': 40}
        """
        return {
            "merchant_width_pct": 40,  # Default 40% width
            "account_width_pct": 40,  # Default 40% width
        }

    def get_currency_symbol(self) -> str:
        """
        Get the currency symbol for this backend.

        Returns currency symbol to display in column headers (e.g., "$", "€", "£").
        Defaults to "$" (USD). Backends can override to fetch from API if available.

        Returns:
            Currency symbol as string (e.g., "$", "€", "£")

        Example:
            >>> backend.get_currency_symbol()
            '$'
        """
        return "$"  # Default to USD

    def delete_session(self) -> None:
        """
        Delete saved session data.

        This is optional - backends that don't support session persistence
        can use the default no-op implementation.

        Default implementation does nothing.
        """
        pass  # Default: no-op

    def clear_auth(self) -> None:
        """
        Clear all authentication state (in-memory tokens, headers, etc.).

        This should be called before a fresh login to ensure no stale auth data
        is present. Backends should override this to clear their specific auth state.

        Default implementation does nothing.
        """
        pass  # Default: no-op
