"""
Unit tests for moneyflow.backends.simplefin_client.

All HTTP calls are mocked — no real network access occurs.
"""

import base64
from datetime import datetime, timezone
from unittest.mock import AsyncMock, MagicMock, patch

import pytest

from moneyflow.backends.simplefin_client import (
    SimpleFinClient,
    _parse_transactions,
    _unix_to_date_str,
    claim_token,
    parse_access_url,
)

# ---------------------------------------------------------------------------
# Fixtures / helpers
# ---------------------------------------------------------------------------

VALID_ACCESS_URL = "https://testuser:testpass@bridge.simplefin.org/simplefin"

MINIMAL_ACCOUNT = {
    "id": "acct-1",
    "name": "Checking",
    "currency": "USD",
    "balance": "1000.00",
    "balance-date": 1700000000,
    "transactions": [
        {
            "id": "txn-1",
            "posted": 1700000000,
            "transacted_at": 1699999900,
            "amount": "-42.50",
            "description": "Grocery Store",
            "pending": False,
        }
    ],
}

MINIMAL_RESPONSE = {
    "accounts": [MINIMAL_ACCOUNT],
    "connections": [],
    "errlist": [],
}


def _make_mock_response(status: int, body: dict) -> MagicMock:
    """Build a mock aiohttp response context manager."""
    mock_resp = AsyncMock()
    mock_resp.status = status
    mock_resp.json = AsyncMock(return_value=body)
    # Support async context manager usage
    mock_resp.__aenter__ = AsyncMock(return_value=mock_resp)
    mock_resp.__aexit__ = AsyncMock(return_value=False)
    return mock_resp


def _make_mock_session(mock_resp: MagicMock) -> MagicMock:
    """Build a mock aiohttp.ClientSession context manager."""
    mock_session = MagicMock()
    mock_session.get = MagicMock(return_value=mock_resp)
    mock_session.__aenter__ = AsyncMock(return_value=mock_session)
    mock_session.__aexit__ = AsyncMock(return_value=False)
    return mock_session


# ---------------------------------------------------------------------------
# parse_access_url
# ---------------------------------------------------------------------------


class TestParseAccessUrl:
    def test_valid_url_extracts_credentials(self):
        result = parse_access_url("https://alice:s3cr3t@bridge.simplefin.org/simplefin")
        assert result["username"] == "alice"
        assert result["password"] == "s3cr3t"
        assert result["base_url"] == "https://bridge.simplefin.org/simplefin"

    def test_valid_url_base_url_has_no_trailing_slash(self):
        result = parse_access_url("https://u:p@host.example.com/path")
        assert not result["base_url"].endswith("/")

    def test_malformed_url_no_credentials_raises(self):
        with pytest.raises(ValueError, match="Could not parse"):
            parse_access_url("https://bridge.simplefin.org/simplefin")

    def test_http_url_raises(self):
        # Only HTTPS is accepted
        with pytest.raises(ValueError):
            parse_access_url("http://user:pass@bridge.simplefin.org/simplefin")

    def test_missing_password_raises(self):
        with pytest.raises(ValueError, match="Could not parse"):
            parse_access_url("https://useronly@bridge.simplefin.org/simplefin")


# ---------------------------------------------------------------------------
# _unix_to_date_str
# ---------------------------------------------------------------------------


class TestUnixToDateStr:
    def test_known_timestamp_returns_date_string(self):
        # 2023-11-14 22:13:20 UTC
        ts = 1700000000
        result = _unix_to_date_str(ts)
        assert result == datetime.fromtimestamp(ts, tz=timezone.utc).strftime("%Y-%m-%d")

    def test_zero_returns_none(self):
        assert _unix_to_date_str(0) is None

    def test_none_returns_none(self):
        assert _unix_to_date_str(None) is None

    def test_string_numeric_is_accepted(self):
        result = _unix_to_date_str("1700000000")
        assert result is not None
        assert len(result) == 10  # YYYY-MM-DD


# ---------------------------------------------------------------------------
# _parse_transactions
# ---------------------------------------------------------------------------


class TestParseTransactions:
    def test_single_account_single_txn(self):
        result = _parse_transactions([MINIMAL_ACCOUNT])
        assert len(result) == 1
        txn = result[0]
        assert txn["id"] == "acct-1:txn-1"
        assert txn["amount"] == -42.50
        assert txn["merchant"]["name"] == "Grocery Store"
        assert txn["account"]["id"] == "acct-1"
        assert txn["account"]["displayName"] == "Checking"
        assert txn["pending"] is False
        assert txn["hideFromReports"] is False
        assert txn["isRecurring"] is False
        assert txn["notes"] == ""
        assert txn["category"] == {"id": "uncategorized", "name": "Uncategorized"}
        assert txn["currency"] == "USD"

    def test_date_from_posted_timestamp(self):
        result = _parse_transactions([MINIMAL_ACCOUNT])
        expected = datetime.fromtimestamp(1700000000, tz=timezone.utc).strftime("%Y-%m-%d")
        assert result[0]["date"] == expected

    def test_pending_txn_falls_back_to_transacted_at(self):
        acct = {
            "id": "a",
            "name": "Savings",
            "transactions": [
                {
                    "id": "p1",
                    "posted": 0,  # 0 = pending, no post date
                    "transacted_at": 1700000000,
                    "amount": "-10.00",
                    "description": "Pending charge",
                    "pending": True,
                }
            ],
        }
        result = _parse_transactions([acct])
        assert len(result) == 1
        expected = datetime.fromtimestamp(1700000000, tz=timezone.utc).strftime("%Y-%m-%d")
        assert result[0]["date"] == expected

    def test_txn_with_no_usable_date_is_skipped(self):
        acct = {
            "id": "a",
            "name": "X",
            "transactions": [
                {
                    "id": "no-date",
                    "posted": 0,
                    "transacted_at": 0,
                    "amount": "-5.00",
                    "description": "Mystery",
                    "pending": True,
                }
            ],
        }
        result = _parse_transactions([acct])
        assert result == []

    def test_multiple_accounts_flattened(self):
        acct2 = {
            "id": "acct-2",
            "name": "Savings",
            "transactions": [
                {
                    "id": "txn-2",
                    "posted": 1700100000,
                    "transacted_at": None,
                    "amount": "500.00",
                    "description": "Payroll",
                    "pending": False,
                }
            ],
        }
        result = _parse_transactions([MINIMAL_ACCOUNT, acct2])
        assert len(result) == 2
        ids = {t["id"] for t in result}
        assert ids == {"acct-1:txn-1", "acct-2:txn-2"}

    def test_account_with_no_transactions_produces_no_rows(self):
        acct = {"id": "a", "name": "Empty", "transactions": []}
        result = _parse_transactions([acct])
        assert result == []

    def test_empty_accounts_list_returns_empty(self):
        assert _parse_transactions([]) == []

    def test_merchant_id_and_name_equal_description(self):
        result = _parse_transactions([MINIMAL_ACCOUNT])
        txn = result[0]
        assert txn["merchant"]["id"] == txn["merchant"]["name"] == "Grocery Store"


# ---------------------------------------------------------------------------
# SimpleFinClient (async)
# ---------------------------------------------------------------------------


class TestSimpleFinClient:
    def test_init_with_valid_url_succeeds(self):
        client = SimpleFinClient(VALID_ACCESS_URL)
        assert client._username == "testuser"
        assert client._password == "testpass"
        assert "bridge.simplefin.org" in client._base_url

    def test_init_with_malformed_url_raises(self):
        with pytest.raises(ValueError):
            SimpleFinClient("https://no-credentials-here.example.com/path")

    @pytest.mark.asyncio
    async def test_fetch_transactions_happy_path(self):
        client = SimpleFinClient(VALID_ACCESS_URL)
        mock_resp = _make_mock_response(200, MINIMAL_RESPONSE)
        mock_session = _make_mock_session(mock_resp)

        with patch("aiohttp.ClientSession", return_value=mock_session):
            result = await client.fetch_transactions()

        assert len(result) == 1
        assert result[0]["id"] == "acct-1:txn-1"

    @pytest.mark.asyncio
    async def test_fetch_transactions_rejects_mixed_account_currencies(self):
        eur_account = {
            **MINIMAL_ACCOUNT,
            "id": "acct-2",
            "name": "Euro Account",
            "currency": "EUR",
        }
        response = {"accounts": [MINIMAL_ACCOUNT, eur_account], "connections": [], "errlist": []}
        client = SimpleFinClient(VALID_ACCESS_URL)
        mock_resp = _make_mock_response(200, response)
        mock_session = _make_mock_session(mock_resp)

        with patch("aiohttp.ClientSession", return_value=mock_session):
            with pytest.raises(RuntimeError, match="multiple currencies"):
                await client.fetch_transactions()

    @pytest.mark.asyncio
    async def test_fetch_transactions_rejects_custom_currency_identifier(self):
        custom_currency_account = {
            **MINIMAL_ACCOUNT,
            "currency": "https://example.com/reward-points",
        }
        response = {
            "accounts": [custom_currency_account],
            "connections": [],
            "errlist": [],
        }
        client = SimpleFinClient(VALID_ACCESS_URL)
        mock_resp = _make_mock_response(200, response)
        mock_session = _make_mock_session(mock_resp)

        with patch("aiohttp.ClientSession", return_value=mock_session):
            with pytest.raises(RuntimeError, match="custom currency"):
                await client.fetch_transactions()

    @pytest.mark.asyncio
    async def test_fetch_transactions_rejects_partial_response_errors(self):
        response = {
            "accounts": [MINIMAL_ACCOUNT],
            "connections": [],
            "errlist": ["One account could not be refreshed"],
        }
        client = SimpleFinClient(VALID_ACCESS_URL)
        mock_resp = _make_mock_response(200, response)
        mock_session = _make_mock_session(mock_resp)

        with patch("aiohttp.ClientSession", return_value=mock_session):
            with pytest.raises(RuntimeError, match="partial account data"):
                await client.fetch_transactions()

    @pytest.mark.asyncio
    async def test_fetch_transactions_passes_start_date_param(self):
        client = SimpleFinClient(VALID_ACCESS_URL)
        mock_resp = _make_mock_response(200, MINIMAL_RESPONSE)
        mock_session = _make_mock_session(mock_resp)

        with patch("aiohttp.ClientSession", return_value=mock_session):
            await client.fetch_transactions(start_date="2024-01-01")

        call_kwargs = mock_session.get.call_args
        params = dict(call_kwargs[1]["params"])
        assert "start-date" in params

    @pytest.mark.asyncio
    async def test_fetch_transactions_403_raises_permission_error(self):
        client = SimpleFinClient(VALID_ACCESS_URL)
        mock_resp = _make_mock_response(403, {})
        mock_session = _make_mock_session(mock_resp)

        with patch("aiohttp.ClientSession", return_value=mock_session):
            with pytest.raises(PermissionError, match="403"):
                await client.fetch_transactions()

    @pytest.mark.asyncio
    async def test_fetch_transactions_402_raises_runtime_error(self):
        client = SimpleFinClient(VALID_ACCESS_URL)
        mock_resp = _make_mock_response(402, {})
        mock_session = _make_mock_session(mock_resp)

        with patch("aiohttp.ClientSession", return_value=mock_session):
            with pytest.raises(RuntimeError, match="402"):
                await client.fetch_transactions()

    @pytest.mark.asyncio
    async def test_fetch_transactions_500_raises_runtime_error(self):
        client = SimpleFinClient(VALID_ACCESS_URL)
        mock_resp = _make_mock_response(500, {})
        mock_session = _make_mock_session(mock_resp)

        with patch("aiohttp.ClientSession", return_value=mock_session):
            with pytest.raises(RuntimeError, match="500"):
                await client.fetch_transactions()

    @pytest.mark.asyncio
    async def test_fetch_transactions_empty_accounts_returns_empty(self):
        client = SimpleFinClient(VALID_ACCESS_URL)
        mock_resp = _make_mock_response(200, {"accounts": [], "connections": [], "errlist": []})
        mock_session = _make_mock_session(mock_resp)

        with patch("aiohttp.ClientSession", return_value=mock_session):
            result = await client.fetch_transactions()

        assert result == []


# ---------------------------------------------------------------------------
# claim_token
# ---------------------------------------------------------------------------


class TestClaimToken:
    def _make_token(self, url: str) -> str:
        """Encode a URL as a Base64 token string."""
        return base64.b64encode(url.encode()).decode()

    def test_valid_token_returns_access_url(self):
        token = self._make_token("https://bridge.simplefin.org/simplefin/claim/abc123")
        access_url = "https://user:pass@bridge.simplefin.org/simplefin"

        mock_resp = MagicMock()
        mock_resp.status = 200
        mock_resp.read.return_value = access_url.encode()
        mock_resp.__enter__ = MagicMock(return_value=mock_resp)
        mock_resp.__exit__ = MagicMock(return_value=False)

        with patch("urllib.request.urlopen", return_value=mock_resp):
            result = claim_token(token)

        assert result == access_url

    def test_bad_base64_raises_value_error(self):
        with pytest.raises(ValueError, match="Base64"):
            claim_token("not-valid-base64!!!")

    def test_decoded_http_url_raises_value_error(self):
        # Must be HTTPS
        token = self._make_token("http://insecure.example.com/claim")
        with pytest.raises(ValueError, match="HTTPS"):
            claim_token(token)

    def test_403_raises_permission_error(self):
        token = self._make_token("https://bridge.simplefin.org/claim/used")
        import urllib.error

        http_error = urllib.error.HTTPError(
            url="https://bridge.simplefin.org/claim/used",
            code=403,
            msg="Forbidden",
            hdrs=MagicMock(),
            fp=None,
        )
        with patch("urllib.request.urlopen", side_effect=http_error):
            with pytest.raises(PermissionError, match="403"):
                claim_token(token)
