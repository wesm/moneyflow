"""
Xero backend implementation.

This wraps a Xero client (real or mock) to expose the FinanceBackend interface.
"""

from typing import Any, Dict, List, Optional

from ..xero_client import XeroClient, XeroClientProtocol
from .base import FinanceBackend


class XeroBackend(FinanceBackend):
    """Xero backend implementation."""

    def __init__(
        self,
        profile_dir: Optional[str] = None,
        config_dir: Optional[str] = None,
        client: Optional[XeroClientProtocol] = None,
    ):
        self.profile_dir = profile_dir
        self.config_dir = config_dir
        self.client: XeroClientProtocol = client or XeroClient(
            profile_dir=profile_dir, config_dir=config_dir
        )

    async def login(
        self,
        email: Optional[str] = None,
        password: Optional[str] = None,
        use_saved_session: bool = True,
        save_session: bool = True,
        mfa_secret_key: Optional[str] = None,
        client_id: Optional[str] = None,
        client_secret: Optional[str] = None,
        redirect_uri: Optional[str] = None,
        refresh_token: Optional[str] = None,
        scopes: Optional[str] = None,
        tenant_id: Optional[str] = None,
        **kwargs: Any,
    ) -> None:
        """
        Authenticate with Xero.

        OAuth is handled entirely by the client using OAuth app credentials; email/password are
        ignored.
        """
        await self.client.login(
            use_saved_session=use_saved_session,
            save_session=save_session,
            client_id=client_id,
            client_secret=client_secret,
            redirect_uri=redirect_uri,
            refresh_token=refresh_token,
            scopes=scopes,
            tenant_id=tenant_id,
        )

    async def get_transactions(
        self,
        limit: int = 100,
        offset: int = 0,
        start_date: Optional[str] = None,
        end_date: Optional[str] = None,
        **kwargs: Any,
    ) -> Dict[str, Any]:
        raw = await self.client.get_transactions(
            limit=limit, offset=offset, start_date=start_date, end_date=end_date
        )
        raw_txns = raw.get("transactions", [])
        transactions = [self._normalize_transaction(txn) for txn in raw_txns]
        total_count = raw.get("total_count", len(raw_txns))

        return {"allTransactions": {"results": transactions, "totalCount": total_count}}

    async def get_transaction_categories(self) -> Dict[str, Any]:
        return await self.client.get_transaction_categories()

    async def get_transaction_category_groups(self) -> Dict[str, Any]:
        return await self.client.get_transaction_category_groups()

    async def update_transaction(
        self,
        transaction_id: str,
        merchant_name: Optional[str] = None,
        category_id: Optional[str] = None,
        hide_from_reports: Optional[bool] = None,
        **kwargs: Any,
    ) -> Dict[str, Any]:
        return await self.client.update_transaction(
            transaction_id=transaction_id,
            merchant_name=merchant_name,
            category_id=category_id,
            hide_from_reports=hide_from_reports,
            **kwargs,
        )

    async def delete_transaction(self, transaction_id: str) -> bool:
        return await self.client.delete_transaction(transaction_id)

    async def get_all_merchants(self) -> List[str]:
        return await self.client.get_all_merchants()

    def get_currency_symbol(self) -> str:
        return self.client.get_currency_symbol()

    def delete_session(self) -> None:
        if hasattr(self.client, "delete_session"):
            self.client.delete_session()

    def clear_auth(self) -> None:
        self.client.clear_auth()

    def get_backend_type(self) -> str:
        return "xero"

    def _normalize_transaction(self, txn: Dict[str, Any]) -> Dict[str, Any]:
        """
        Convert a Xero transaction dict to the moneyflow transaction shape.

        Expected keys in txn (from Xero Accounting API):
        - TransactionID
        - Date (YYYY-MM-DD)
        - Total (float, negative for spend)
        - Contact {ContactID, Name}
        - BankAccount {AccountID, Name}
        - Account {AccountID, Name}  # chart-of-accounts category
        - Reference (notes/memo)
        - Status (AUTHORISED, DRAFT, VOIDED, etc.)
        - IsReconciled (bool)
        - TrackingCategories: list of {TrackingCategoryID, Name, Option}
        """
        contact = txn.get("Contact", {}) or {}
        bank_account = txn.get("BankAccount", {}) or {}
        account = txn.get("Account", {}) or {}
        status = str(txn.get("Status", "")).upper()

        pending = status not in {"AUTHORISED", "PAID"}
        hide_from_reports = status in {"VOIDED", "DELETED"}

        return {
            "id": txn.get("TransactionID") or txn.get("ID") or "",
            "date": txn.get("Date", ""),
            "amount": float(txn.get("Total", 0.0)),
            "merchant": {
                "id": contact.get("ContactID") or "unknown",
                "name": contact.get("Name") or "Unknown",
            },
            "category": {
                "id": account.get("AccountID") or account.get("Code") or "uncategorized",
                "name": account.get("Name") or account.get("Code") or "Uncategorized",
            },
            "account": {
                "id": bank_account.get("AccountID") or "unknown",
                "displayName": bank_account.get("Name") or "Unknown",
            },
            "notes": txn.get("Reference") or "",
            "hideFromReports": bool(hide_from_reports or txn.get("IsReconciled") is False),
            "pending": pending,
            "isRecurring": False,
        }
