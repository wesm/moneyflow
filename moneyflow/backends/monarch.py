"""
Monarch Money backend implementation.

Wraps the MonarchMoney GraphQL client to implement the FinanceBackend interface.
"""

from typing import Any, Dict, List, Optional

from ..monarchmoney import MonarchMoney
from .base import FinanceBackend


class MonarchBackend(FinanceBackend):
    """
    Monarch Money backend implementation.

    This wraps the MonarchMoney GraphQL client to provide a standardized
    FinanceBackend interface for moneyflow.
    """

    def __init__(self, profile_dir: Optional[str] = None):
        """
        Initialize the Monarch Money backend.

        Args:
            profile_dir: Optional profile directory for storing session files.
                        If provided, session will be stored in {profile_dir}/.mm/
                        If not provided, falls back to .mm/ in current directory.
        """
        self.client = MonarchMoney(profile_dir=profile_dir)

    async def login(
        self,
        email: Optional[str] = None,
        password: Optional[str] = None,
        use_saved_session: bool = True,
        save_session: bool = True,
        mfa_secret_key: Optional[str] = None,
    ) -> None:
        """
        Authenticate with Monarch Money.

        Args:
            email: User's email address
            password: User's password
            use_saved_session: Whether to try using a saved session
            save_session: Whether to save the session for future use
            mfa_secret_key: MFA/2FA secret key for automatic TOTP generation

        Raises:
            RequireMFAException: If MFA is required but not provided
            LoginFailedException: If login fails
        """
        await self.client.login(
            email=email,
            password=password,
            use_saved_session=use_saved_session,
            save_session=save_session,
            mfa_secret_key=mfa_secret_key,
        )

    async def get_transactions(
        self,
        limit: int = 100,
        offset: int = 0,
        start_date: Optional[str] = None,
        end_date: Optional[str] = None,
        **kwargs,
    ) -> Dict[str, Any]:
        """
        Fetch transactions from Monarch Money.

        Args:
            limit: Maximum number of transactions to return
            offset: Number of transactions to skip (for pagination)
            start_date: Filter transactions from this date (ISO format: YYYY-MM-DD)
            end_date: Filter transactions to this date (ISO format: YYYY-MM-DD)
            **kwargs: Additional parameters passed to MonarchMoney client

        Returns:
            Dictionary containing transaction data in Monarch format
        """
        return await self.client.get_transactions(
            limit=limit,
            offset=offset,
            start_date=start_date,
            end_date=end_date,
            **kwargs,
        )

    async def get_transaction_categories(self) -> Dict[str, Any]:
        """
        Fetch all available transaction categories from Monarch Money.

        Returns:
            Dictionary containing categories in Monarch format
        """
        return await self.client.get_transaction_categories()

    async def get_transaction_category_groups(self) -> Dict[str, Any]:
        """
        Fetch all category groups from Monarch Money.

        Returns:
            Dictionary containing category groups in Monarch format
        """
        return await self.client.get_transaction_category_groups()

    async def update_transaction(
        self,
        transaction_id: str,
        merchant_name: Optional[str] = None,
        category_id: Optional[str] = None,
        hide_from_reports: Optional[bool] = None,
        **kwargs,
    ) -> Dict[str, Any]:
        """
        Update a transaction in Monarch Money.

        Args:
            transaction_id: Unique identifier of the transaction to update
            merchant_name: New merchant name (if changing)
            category_id: New category ID (if changing)
            hide_from_reports: New hidden status (if changing)
            **kwargs: Additional fields to update

        Returns:
            Dictionary containing the updated transaction data

        Raises:
            Exception: If transaction not found or update fails
        """
        return await self.client.update_transaction(
            transaction_id=transaction_id,
            merchant_name=merchant_name,
            category_id=category_id,
            hide_from_reports=hide_from_reports,
            **kwargs,
        )

    async def delete_transaction(self, transaction_id: str) -> bool:
        """
        Delete a transaction from Monarch Money.

        Args:
            transaction_id: Unique identifier of the transaction to delete

        Returns:
            True if deletion was successful

        Raises:
            Exception: If transaction not found or deletion fails
        """
        return await self.client.delete_transaction(transaction_id)

    async def get_all_merchants(self) -> List[str]:
        """
        Get all unique merchant names using GraphQL aggregation.

        Returns:
            List of merchant names, sorted alphabetically
        """
        return await self.client.get_all_merchants()

    def delete_session(self, filename: Optional[str] = None) -> None:
        """
        Delete the saved session file.

        Args:
            filename: Optional path to session file (uses default if not provided)
        """
        self.client.delete_session(filename)

    def clear_auth(self) -> None:
        """
        Clear all authentication state (token, headers).

        This clears both the in-memory token and Authorization header,
        ensuring no stale auth data is used on next login.
        """
        self.client.set_token(None)
        if "Authorization" in self.client._headers:
            del self.client._headers["Authorization"]
