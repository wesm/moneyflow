"""
Xero API client and test doubles.

Implements the minimal subset we need: read expense-side bank transactions,
list expense accounts (used as categories), and basic auth/token handling.

Notes:
- Auth flow assumes you have already created a Xero Web App and can supply
  client id/secret, redirect URI, and an initial refresh token.
- We store tokens in the profile directory (token.json) and auto-refresh.
- We pick the first available tenant unless a tenant id is already cached.
"""

from __future__ import annotations

import json
from dataclasses import dataclass
from datetime import datetime, timedelta, timezone
from pathlib import Path
from typing import Any, Dict, List, Optional, Protocol
from urllib.parse import urlencode

import aiohttp

TOKEN_FILENAME = "xero_token.json"
XERO_DEFAULT_SCOPES = (
    "offline_access accounting.transactions accounting.settings accounting.contacts"
)

def build_xero_auth_url(
    client_id: str,
    redirect_uri: str,
    scopes: str = XERO_DEFAULT_SCOPES,
    state: str = "moneyflow",
    prompt: str = "consent",
) -> str:
    """
    Build a Xero OAuth authorization URL for manual consent.

    Parameters
    ----------
    client_id : str
        Xero app client identifier.
    redirect_uri : str
        Redirect URI registered with the Xero app.
    scopes : str, optional
        Space-delimited list of scopes. Defaults to the moneyflow preset.
    state : str, optional
        Opaque string to maintain state between request/response.
    prompt : str, optional
        Optional prompt parameter (default: consent) to force the consent screen.

    Returns
    -------
    str
        Fully formed authorization URL that can be opened in a browser.
    """
    query = urlencode(
        {
            "response_type": "code",
            "client_id": client_id,
            "redirect_uri": redirect_uri,
            "scope": scopes,
            "state": state,
            "prompt": prompt,
        }
    )
    return f"https://login.xero.com/identity/connect/authorize?{query}"


@dataclass
class TokenData:
    access_token: str
    refresh_token: str
    expires_at: datetime
    tenant_id: Optional[str] = None

    @classmethod
    def from_dict(cls, data: Dict[str, Any]) -> "TokenData":
        expires_at_str = data.get("expires_at") or data.get("expiresAt")
        expires_at = (
            datetime.fromisoformat(expires_at_str)
            if expires_at_str
            else datetime.now(timezone.utc)
        )
        return cls(
            access_token=data.get("access_token", ""),
            refresh_token=data.get("refresh_token", ""),
            expires_at=expires_at,
            tenant_id=data.get("tenant_id"),
        )

    def to_dict(self) -> Dict[str, Any]:
        return {
            "access_token": self.access_token,
            "refresh_token": self.refresh_token,
            "expires_at": self.expires_at.isoformat(),
            "tenant_id": self.tenant_id,
        }

    def is_expired(self) -> bool:
        return datetime.now(timezone.utc) >= self.expires_at - timedelta(minutes=1)


class XeroClientProtocol(Protocol):
    """Interface required by XeroBackend."""

    async def login(
        self,
        *,
        use_saved_session: bool = True,
        save_session: bool = True,
        **kwargs: Any,
) -> None:
        ...

    async def get_transactions(
        self,
        limit: int = 100,
        offset: int = 0,
        start_date: Optional[str] = None,
        end_date: Optional[str] = None,
    ) -> Dict[str, Any]:
        ...

    async def get_transaction_categories(self) -> Dict[str, Any]:
        ...

    async def get_transaction_category_groups(self) -> Dict[str, Any]:
        ...

    async def update_transaction(
        self,
        transaction_id: str,
        merchant_name: Optional[str] = None,
        category_id: Optional[str] = None,
        hide_from_reports: Optional[bool] = None,
        **kwargs: Any,
    ) -> Dict[str, Any]:
        ...

    async def delete_transaction(self, transaction_id: str) -> bool:
        ...

    async def get_all_merchants(self) -> List[str]:
        ...

    def get_currency_symbol(self) -> str:
        ...

    def clear_auth(self) -> None:
        ...


class XeroClient:
    """
    Minimal async client for Xero Accounting API using direct HTTP calls.

    Parameters
    ----------
    profile_dir : str, optional
        Required profile directory for token storage.
    config_dir : str, optional
        Base config directory (not used for token storage, kept for parity).

    Notes
    -----
    Auth strategy:
    - Requires client id/secret/redirect URI and an initial refresh token supplied
      by the credential store (no environment variables).
    - Stores tokens (with tenant id if discovered) under ``profile_dir/xero_token.json``.
    """

    def __init__(self, profile_dir: Optional[str] = None, config_dir: Optional[str] = None):
        """
        Args:
            profile_dir: Required profile directory; tokens are stored here.
            config_dir: Base config directory (unused for storage; kept for parity with other backends).
        """
        if not profile_dir:
            raise ValueError("XeroClient requires a profile_dir for token storage")

        self.config_dir = Path(config_dir) if config_dir else None
        self.profile_dir = Path(profile_dir)
        self.token_path = self._resolve_token_path()
        self.token_data: Optional[TokenData] = None
        self.currency_symbol: str = "$"
        self.client_id: Optional[str] = None
        self.client_secret: Optional[str] = None
        self.redirect_uri: Optional[str] = None
        self.scopes: str = XERO_DEFAULT_SCOPES
        self.refresh_token: Optional[str] = None
        self.tenant_id: Optional[str] = None

    async def login(
        self,
        *,
        use_saved_session: bool = True,
        save_session: bool = True,
        client_id: Optional[str] = None,
        client_secret: Optional[str] = None,
        redirect_uri: Optional[str] = None,
        refresh_token: Optional[str] = None,
        scopes: Optional[str] = None,
        tenant_id: Optional[str] = None,
    ) -> None:
        """
        Authenticate using a refresh token and cache tenant/token state.

        Parameters
        ----------
        use_saved_session : bool, default True
            Load cached token from ``profile_dir/xero_token.json`` if present.
        save_session : bool, default True
            Persist refreshed token/tenant back to disk.

        Raises
        ------
        ValueError
            If OAuth credentials or refresh tokens are missing.
        """
        if client_id:
            self.client_id = client_id
        if client_secret:
            self.client_secret = client_secret
        if redirect_uri:
            self.redirect_uri = redirect_uri
        if scopes is not None:
            self.scopes = scopes or XERO_DEFAULT_SCOPES
        if refresh_token:
            self.refresh_token = refresh_token
        if tenant_id:
            self.tenant_id = tenant_id

        if use_saved_session:
            self.token_data = self._load_token()

        if not self.token_data:
            if not self.refresh_token:
                raise ValueError(
                    "Xero refresh token not found. Provide credentials in the profile setup to continue."
                )
            self.token_data = await self._refresh_token(self.refresh_token)

        if self.token_data.is_expired():
            self.token_data = await self._refresh_token(self.token_data.refresh_token)

        if not self.token_data.tenant_id:
            tenant_id = self.tenant_id or await self._fetch_tenant_id(self.token_data.access_token)
            self.token_data.tenant_id = tenant_id

        if save_session:
            self._save_token(self.token_data)

        if self.token_data:
            self.refresh_token = self.token_data.refresh_token
            self.tenant_id = self.token_data.tenant_id

    async def get_transactions(
        self,
        limit: int = 100,
        offset: int = 0,
        start_date: Optional[str] = None,
        end_date: Optional[str] = None,
    ) -> Dict[str, Any]:
        """
        Fetch spend-side bank transactions.

        Parameters
        ----------
        limit : int, default 100
            Page size (converted to Xero page index).
        offset : int, default 0
            Offset for pagination.
        start_date : str, optional
            Filter transactions on or after this ISO date.
        end_date : str, optional
            Filter transactions on or before this ISO date.

        Returns
        -------
        dict
            ``{"transactions": [...], "total_count": int}``
        """
        await self._ensure_ready()
        params = {
            "page": offset // limit + 1,
            "order": "Date DESC",
            "where": 'Type=="SPEND"',
        }
        if start_date:
            params["where"] += f'&&Date>={start_date}'
        if end_date:
            params["where"] += f'&&Date<={end_date}'

        resp = await self._api_get("/api.xro/2.0/BankTransactions", params=params)
        transactions = resp.get("BankTransactions", [])
        total_count = resp.get("BankTransactions", []).__len__()
        return {"transactions": transactions, "total_count": total_count}

    async def get_transaction_categories(self) -> Dict[str, Any]:
        """
        Return expense accounts as categories.

        Returns
        -------
        dict
            ``{"categories": [{"id": ..., "name": ..., "group": {...}}, ...]}``
        """
        await self._ensure_ready()
        resp = await self._api_get("/api.xro/2.0/Accounts")
        accounts = resp.get("Accounts", [])
        expense_accounts = [
            acc
            for acc in accounts
            if acc.get("Type") in {"EXPENSE", "OVERHEADS"} and acc.get("Status") == "ACTIVE"
        ]

        categories = [
            {
                "id": acc.get("AccountID") or acc.get("Code"),
                "name": acc.get("Name") or acc.get("Code"),
                "group": {"id": "expenses", "type": "expense"},
            }
            for acc in expense_accounts
        ]
        return {"categories": categories}

    async def get_transaction_category_groups(self) -> Dict[str, Any]:
        """
        Return a single expense group placeholder.

        Returns
        -------
        dict
            ``{"categoryGroups": [{"id": "expenses", "name": "Expenses", "type": "expense"}]}``
        """
        return {"categoryGroups": [{"id": "expenses", "name": "Expenses", "type": "expense"}]}

    async def update_transaction(
        self,
        transaction_id: str,
        merchant_name: Optional[str] = None,
        category_id: Optional[str] = None,
        hide_from_reports: Optional[bool] = None,
        **kwargs: Any,
    ) -> Dict[str, Any]:
        """
        Recategorize a spend transaction by updating its line items' account.

        Parameters
        ----------
        transaction_id : str
            BankTransactionID to update.
        merchant_name : str, optional
            Ignored currently (Xero lacks a direct merchant rename for bank transactions).
        category_id : str, optional
            Target AccountCode/AccountID for the line item (required).
        hide_from_reports : bool, optional
            Ignored; Xero has no hide flag on bank transactions.
        **kwargs : Any
            Unused.

        Returns
        -------
        dict
            ``{"updateTransaction": {"transaction": {"id": ...}}}``

        Raises
        ------
        ValueError
            If category_id missing, transaction not found, or multi-line transactions are passed.

        Notes
        -----
        Only supports single-line-item spend bank transactions.
        """
        await self._ensure_ready()
        if category_id is None:
            raise ValueError("category_id is required for Xero recategorization")

        # Fetch transaction to get line items
        current = await self._api_get(
            f"/api.xro/2.0/BankTransactions/{transaction_id}",
        )
        bank_transactions = current.get("BankTransactions", [])
        if not bank_transactions:
            raise ValueError(f"Transaction {transaction_id} not found")

        txn = bank_transactions[0]
        line_items = txn.get("LineItems") or []
        if len(line_items) != 1:
            raise ValueError("Only single-line transactions are supported for recategorization")

        line_items[0]["AccountCode"] = category_id

        payload = {"BankTransactions": [{"BankTransactionID": transaction_id, "LineItems": line_items}]}
        updated = await self._api_post("/api.xro/2.0/BankTransactions", json_body=payload, method="post")

        updated_txn = updated.get("BankTransactions", [{}])[0]
        return {"updateTransaction": {"transaction": {"id": updated_txn.get("BankTransactionID")}}}

    async def delete_transaction(self, transaction_id: str) -> bool:
        """
        Mark a bank transaction as deleted.

        Parameters
        ----------
        transaction_id : str
            BankTransactionID to delete.

        Returns
        -------
        bool
            True if API reports status DELETED.
        """
        await self._ensure_ready()
        payload = {"BankTransactions": [{"BankTransactionID": transaction_id, "Status": "DELETED"}]}
        resp = await self._api_post("/api.xro/2.0/BankTransactions", json_body=payload, method="post")
        updated = resp.get("BankTransactions", [{}])[0]
        return updated.get("Status") == "DELETED"

    async def get_all_merchants(self) -> List[str]:
        """
        List unique contact names from fetched spend transactions.

        Returns
        -------
        list[str]
            Sorted list of contact names.
        """
        await self._ensure_ready()
        resp = await self.get_transactions(limit=500, offset=0)
        merchants = {
            (txn.get("Contact") or {}).get("Name", "")
            for txn in resp.get("transactions", [])
        }
        return sorted(name for name in merchants if name)

    def get_currency_symbol(self) -> str:
        return self.currency_symbol

    def clear_auth(self) -> None:
        self.token_data = None

    # Internal helpers -------------------------------------------------
    def _resolve_token_path(self) -> Optional[Path]:
        """
        Resolve where to store session/oauth metadata.

        Always profile_dir; this is required for Xero.
        """
        target_dir = self.profile_dir
        target_dir.mkdir(parents=True, exist_ok=True)
        return target_dir / TOKEN_FILENAME

    def _load_token(self) -> Optional[TokenData]:
        if not self.token_path or not self.token_path.exists():
            return None
        try:
            data = json.loads(self.token_path.read_text())
            token = TokenData.from_dict(data)
            return token
        except Exception:
            return None

    def _save_token(self, token: TokenData) -> None:
        if not self.token_path:
            return
        self.token_path.write_text(json.dumps(token.to_dict(), indent=2))
        self.token_path.chmod(0o600)

    def delete_session(self) -> None:
        """Remove any cached OAuth token data from disk and memory."""
        if self.token_path and self.token_path.exists():
            self.token_path.unlink()
        self.token_data = None

    async def _refresh_token(self, refresh_token: str) -> TokenData:
        client_id = self.client_id
        client_secret = self.client_secret
        redirect_uri = self.redirect_uri
        scopes = self.scopes or XERO_DEFAULT_SCOPES

        if not client_id or not client_secret or not redirect_uri:
            raise ValueError("Xero OAuth credentials are not configured for token refresh")

        data = {
            "grant_type": "refresh_token",
            "refresh_token": refresh_token,
            "client_id": client_id,
            "client_secret": client_secret,
            "redirect_uri": redirect_uri,
        }
        if scopes.strip():
            data["scope"] = scopes

        async with aiohttp.ClientSession() as session:
            async with session.post("https://identity.xero.com/connect/token", data=data) as resp:
                if resp.status != 200:
                    raise ValueError(f"Failed to refresh token: {resp.status} {await resp.text()}")
                payload = await resp.json()

        expires_in = payload.get("expires_in", 1800)
        expires_at = datetime.now(timezone.utc) + timedelta(seconds=expires_in)

        token = TokenData(
            access_token=payload["access_token"],
            refresh_token=payload.get("refresh_token", refresh_token),
            expires_at=expires_at,
            tenant_id=self.tenant_id,
        )
        self.refresh_token = token.refresh_token
        return token

    async def _fetch_tenant_id(self, access_token: str) -> str:
        async with aiohttp.ClientSession() as session:
            headers = {"Authorization": f"Bearer {access_token}"}
            async with session.get("https://api.xero.com/connections", headers=headers) as resp:
                if resp.status != 200:
                    raise ValueError(f"Failed to fetch connections: {resp.status} {await resp.text()}")
                data = await resp.json()
        if not data:
            raise ValueError("No Xero connections found for this token")
        return data[0]["tenantId"]

    async def _ensure_ready(self) -> None:
        if not self.token_data:
            await self.login()
        elif self.token_data.is_expired():
            self.token_data = await self._refresh_token(self.token_data.refresh_token)
            self._save_token(self.token_data)
        if not self.token_data or not self.token_data.tenant_id:
            raise ValueError("Xero client not authenticated or tenant not selected")

    async def _api_get(self, path: str, params: Optional[Dict[str, Any]] = None) -> Dict[str, Any]:
        assert self.token_data is not None
        headers = {
            "Authorization": f"Bearer {self.token_data.access_token}",
            "xero-tenant-id": self.token_data.tenant_id or "",
            "Accept": "application/json",
        }
        url = f"https://api.xero.com{path}"
        async with aiohttp.ClientSession() as session:
            async with session.get(url, headers=headers, params=params) as resp:
                if resp.status != 200:
                    raise ValueError(f"Xero GET {path} failed: {resp.status} {await resp.text()}")
                return await resp.json()

    async def _api_post(
        self,
        path: str,
        json_body: Dict[str, Any],
        method: str = "post",
    ) -> Dict[str, Any]:
        assert self.token_data is not None
        headers = {
            "Authorization": f"Bearer {self.token_data.access_token}",
            "xero-tenant-id": self.token_data.tenant_id or "",
            "Accept": "application/json",
            "Content-Type": "application/json",
        }
        url = f"https://api.xero.com{path}"
        async with aiohttp.ClientSession() as session:
            request = session.post if method.lower() == "post" else session.put
            async with request(url, headers=headers, json=json_body) as resp:
                if resp.status not in (200, 201):
                    raise ValueError(f"Xero POST {path} failed: {resp.status} {await resp.text()}")
                return await resp.json()


class MockXeroClient(XeroClientProtocol):
    """In-memory mock used for unit tests and early wiring."""

    def __init__(
        self,
        transactions: Optional[List[Dict[str, Any]]] = None,
        categories: Optional[List[Dict[str, Any]]] = None,
        category_groups: Optional[List[Dict[str, Any]]] = None,
        currency_symbol: str = "$",
    ):
        self._logged_in = False
        self.transactions = transactions or self._default_transactions()
        self.categories = categories or self._default_categories()
        self.category_groups = category_groups or self._default_category_groups()
        self.currency_symbol = currency_symbol

    async def login(
        self, *, use_saved_session: bool = True, save_session: bool = True, **kwargs: Any
    ) -> None:
        self._logged_in = True

    async def get_transactions(
        self,
        limit: int = 100,
        offset: int = 0,
        start_date: Optional[str] = None,
        end_date: Optional[str] = None,
    ) -> Dict[str, Any]:
        # Simple slice; ignores date filtering for now
        results = self.transactions[offset : offset + limit]
        return {"transactions": results, "total_count": len(self.transactions)}

    async def get_transaction_categories(self) -> Dict[str, Any]:
        return {"categories": self.categories}

    async def get_transaction_category_groups(self) -> Dict[str, Any]:
        return {"categoryGroups": self.category_groups}

    async def update_transaction(
        self,
        transaction_id: str,
        merchant_name: Optional[str] = None,
        category_id: Optional[str] = None,
        hide_from_reports: Optional[bool] = None,
        **kwargs: Any,
    ) -> Dict[str, Any]:
        for txn in self.transactions:
            if txn.get("TransactionID") == transaction_id:
                if merchant_name is not None and txn.get("Contact"):
                    txn["Contact"]["Name"] = merchant_name
                if category_id is not None:
                    txn["Account"] = {"AccountID": category_id, "Name": f"Category {category_id}"}
                if hide_from_reports is not None:
                    txn["IsReconciled"] = hide_from_reports
                return {"updateTransaction": {"transaction": {"id": transaction_id}}}
        raise ValueError(f"Transaction {transaction_id} not found")

    async def delete_transaction(self, transaction_id: str) -> bool:
        before = len(self.transactions)
        self.transactions = [t for t in self.transactions if t.get("TransactionID") != transaction_id]
        return len(self.transactions) < before

    async def get_all_merchants(self) -> List[str]:
        merchants = {txn.get("Contact", {}).get("Name", "") for txn in self.transactions}
        return sorted(name for name in merchants if name)

    def get_currency_symbol(self) -> str:
        return self.currency_symbol

    def clear_auth(self) -> None:
        self._logged_in = False

    def _default_transactions(self) -> List[Dict[str, Any]]:
        return [
            {
                "TransactionID": "txn-1",
                "Date": "2025-01-05",
                "Total": -25.75,
                "Contact": {"ContactID": "contact-1", "Name": "Coffee Shop"},
                "BankAccount": {"AccountID": "account-1", "Name": "Checking"},
                "Account": {"AccountID": "expense-1", "Name": "Meals"},
                "Reference": "Morning latte",
                "Status": "AUTHORISED",
                "IsReconciled": False,
                "TrackingCategories": [
                    {
                        "TrackingCategoryID": "track-1",
                        "Name": "Region",
                        "Option": "HQ",
                    }
                ],
            },
            {
                "TransactionID": "txn-2",
                "Date": "2025-01-06",
                "Total": 1200.00,
                "Contact": {"ContactID": "contact-2", "Name": "Employer Inc"},
                "BankAccount": {"AccountID": "account-1", "Name": "Checking"},
                "Account": {"AccountID": "income-1", "Name": "Salary"},
                "Reference": "January salary",
                "Status": "AUTHORISED",
                "IsReconciled": True,
                "TrackingCategories": [],
            },
        ]

    def _default_categories(self) -> List[Dict[str, Any]]:
        return [
            {"id": "expense-1", "name": "Meals", "group": {"id": "expenses", "type": "expense"}},
            {"id": "income-1", "name": "Salary", "group": {"id": "income", "type": "income"}},
        ]

    def _default_category_groups(self) -> List[Dict[str, Any]]:
        return [
            {"id": "expenses", "name": "Expenses", "type": "expense"},
            {"id": "income", "name": "Income", "type": "income"},
        ]
