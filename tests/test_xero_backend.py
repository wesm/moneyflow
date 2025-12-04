from datetime import datetime, timedelta, timezone

import pytest

from moneyflow.backends.xero import XeroBackend
from moneyflow.xero_client import (
    MockXeroClient,
    TokenData,
    XERO_DEFAULT_SCOPES,
    build_xero_auth_url,
    XeroClient,
)


@pytest.fixture
def mock_client():
    return MockXeroClient()


@pytest.fixture
def backend(mock_client):
    return XeroBackend(client=mock_client)


@pytest.mark.asyncio
async def test_get_transactions_normalizes_fields(backend):
    result = await backend.get_transactions(limit=10)

    assert "allTransactions" in result
    payload = result["allTransactions"]
    assert payload["totalCount"] == 2
    txn = payload["results"][0]

    assert txn["id"] == "txn-1"
    assert txn["merchant"]["name"] == "Coffee Shop"
    assert txn["account"]["displayName"] == "Checking"
    assert txn["category"]["name"] == "Meals"
    assert txn["notes"] == "Morning latte"
    assert txn["pending"] is False  # status AUTHORISED


@pytest.mark.asyncio
async def test_get_transaction_categories(backend):
    result = await backend.get_transaction_categories()
    assert "categories" in result
    assert any(cat["name"] == "Meals" for cat in result["categories"])


@pytest.mark.asyncio
async def test_get_transaction_category_groups(backend):
    result = await backend.get_transaction_category_groups()
    assert "categoryGroups" in result
    assert any(group["name"] == "Expenses" for group in result["categoryGroups"])


@pytest.mark.asyncio
async def test_update_transaction_and_delete(backend, mock_client):
    update_result = await backend.update_transaction(
        transaction_id="txn-1", merchant_name="New Cafe", category_id="expense-9"
    )
    assert update_result["updateTransaction"]["transaction"]["id"] == "txn-1"

    updated_txn = mock_client.transactions[0]
    assert updated_txn["Contact"]["Name"] == "New Cafe"
    assert updated_txn["Account"]["AccountID"] == "expense-9"

    delete_result = await backend.delete_transaction("txn-1")
    assert delete_result is True
    assert all(txn["TransactionID"] != "txn-1" for txn in mock_client.transactions)


@pytest.mark.asyncio
async def test_get_all_merchants_sorted(backend):
    merchants = await backend.get_all_merchants()
    assert merchants == ["Coffee Shop", "Employer Inc"]


def test_backend_type(backend):
    assert backend.get_backend_type() == "xero"


@pytest.mark.asyncio
async def test_xero_client_login_uses_profile_credentials(tmp_path, monkeypatch):
    client = XeroClient(profile_dir=str(tmp_path))
    captured = {}

    async def fake_refresh(self, refresh_token):
        captured.update(
            {
                "refresh_token": refresh_token,
                "client_id": self.client_id,
                "client_secret": self.client_secret,
                "redirect_uri": self.redirect_uri,
                "scopes": self.scopes,
            }
        )
        return TokenData(
            access_token="access",
            refresh_token="new-refresh",
            expires_at=datetime.now(timezone.utc) + timedelta(minutes=30),
            tenant_id="tenant-123",
        )

    monkeypatch.setattr(XeroClient, "_refresh_token", fake_refresh, raising=True)

    await client.login(
        use_saved_session=False,
        save_session=False,
        client_id="id-123",
        client_secret="secret-456",
        redirect_uri="https://example.com/callback",
        refresh_token="seed-refresh",
        scopes=None,
    )

    assert captured == {
        "refresh_token": "seed-refresh",
        "client_id": "id-123",
        "client_secret": "secret-456",
        "redirect_uri": "https://example.com/callback",
        "scopes": XERO_DEFAULT_SCOPES,
    }
    assert client.refresh_token == "new-refresh"
    assert client.tenant_id == "tenant-123"
    assert not (tmp_path / "xero_token.json").exists()


def test_build_xero_auth_url():
    url = build_xero_auth_url(
        client_id="cid",
        redirect_uri="https://example.com/callback",
        scopes="scope1 scope2",
        state="custom",
    )
    assert "client_id=cid" in url
    assert "redirect_uri=https%3A%2F%2Fexample.com%2Fcallback" in url
    assert "scope=scope1+scope2" in url
    assert "state=custom" in url
