"""
Async SimpleFIN Bridge API client for moneyflow.

The SimpleFIN protocol gives read-only access to bank account balances and
transactions via a simple HTTP+JSON interface. See https://www.simplefin.org/protocol.html

HTTP/parse logic adapted from andtheWings/simplefin2polars (MIT (c) 2026 Daniel Riggins).
"""

import base64
import ipaddress
import re
import urllib.error
import urllib.parse
import urllib.request
from datetime import date, datetime, timedelta, timezone
from typing import Any, Dict, List, Optional

import aiohttp

# Expected SimpleFIN bridge claim endpoints.  The Base64 token decodes to
# a URL on one of these hosts; any other host is rejected to prevent SSRF.
_SIMPLEFIN_CLAIM_HOSTS = frozenset(
    {
        "bridge.simplefin.org",
        "beta-bridge.simplefin.org",
    }
)

# ---------------------------------------------------------------------------
# Low-level helpers
# ---------------------------------------------------------------------------


def _unix_to_date_str(x: Any) -> Optional[str]:
    """Convert a Unix epoch integer to a YYYY-MM-DD string (UTC). Returns None for 0/null."""
    if x is None:
        return None
    try:
        val = float(x)
    except (TypeError, ValueError):
        return None
    if val == 0:
        return None
    dt = datetime.fromtimestamp(val, tz=timezone.utc)
    return dt.strftime("%Y-%m-%d")


def _to_unix(x: Any) -> int:
    """Convert a date-like value (date, datetime, or numeric) to a Unix timestamp integer."""
    if isinstance(x, datetime):
        return int(x.timestamp())
    if isinstance(x, date):
        return int(datetime(x.year, x.month, x.day, tzinfo=timezone.utc).timestamp())
    return int(x)


def _coerce_str(x: Any) -> Optional[str]:
    return None if x is None else str(x)


def _coerce_float(x: Any) -> Optional[float]:
    if x is None:
        return None
    try:
        return float(x)
    except (TypeError, ValueError):
        return None


def _coerce_bool(x: Any) -> bool:
    return bool(x) if x is not None else False


def _normalize_currency_code(x: Any) -> str:
    """Return a normalized ISO 4217 code or reject unsupported custom currencies."""
    currency = _coerce_str(x) or ""
    if not currency:
        return ""
    if not re.fullmatch(r"[A-Za-z]{3}", currency):
        raise RuntimeError(
            "SimpleFIN account uses a custom currency; "
            "moneyflow currently supports ISO 4217 currencies only."
        )
    return currency.upper()


# ---------------------------------------------------------------------------
# Access URL utilities
# ---------------------------------------------------------------------------


def parse_access_url(access_url: str) -> Dict[str, str]:
    """
    Parse embedded Basic Auth credentials from a SimpleFIN Access URL.

    Access URLs take the form: https://username:password@host/path

    Args:
        access_url: The SimpleFIN Access URL with embedded credentials.

    Returns:
        Dict with keys 'username', 'password', and 'base_url'.

    Raises:
        ValueError: If the URL cannot be parsed or is not HTTPS.
    """
    m = re.match(r"^(https)://([^:@/]+):([^@/]+)@(.+)$", access_url)
    if not m:
        raise ValueError(
            "Could not parse the SimpleFIN Access URL. "
            "Expected format: https://username:password@host/path. "
            "Obtain a new Access URL from https://bridge.simplefin.org/simplefin/create"
        )
    scheme, username, password, rest = m.groups()
    return {
        "username": username,
        "password": password,
        "base_url": f"{scheme}://{rest}",
    }


# ---------------------------------------------------------------------------
# One-time token claiming (synchronous, used during credential setup)
# ---------------------------------------------------------------------------


def claim_token(token: str) -> str:
    """
    Exchange a Base64-encoded SimpleFIN token for a persistent Access URL.

    This is a one-time operation. Each token can only be claimed once.

    Args:
        token: A Base64-encoded SimpleFIN token string from
               https://bridge.simplefin.org/simplefin/create

    Returns:
        The Access URL (e.g. 'https://user:pass@bridge.simplefin.org/simplefin').
        Store this securely — treat it like a password.

    Raises:
        ValueError: If the token cannot be Base64-decoded or does not decode
                    to an HTTPS URL.
        PermissionError: On HTTP 403 (token already claimed or invalid).
        RuntimeError: On any unexpected HTTP status code.
    """
    # SimpleFIN tokens use URL-safe Base64 and may omit padding.
    padded = token.strip()
    padded += "=" * (-len(padded) % 4)
    try:
        claim_url = base64.urlsafe_b64decode(padded).decode("utf-8")
    except Exception as exc:
        raise ValueError(f"Failed to Base64-decode the SimpleFIN token: {exc}") from exc

    if not claim_url.startswith("https://"):
        raise ValueError(
            "The decoded SimpleFIN token must point to an HTTPS URL. "
            "Only HTTPS connections are permitted."
        )

    # Validate the claim URL host to prevent SSRF via crafted tokens.
    parsed = urllib.parse.urlparse(claim_url)
    hostname = parsed.hostname
    if not hostname:
        raise ValueError("The decoded SimpleFIN token does not contain a valid hostname.")

    # Reject raw IP addresses in private/reserved ranges.
    try:
        ip = ipaddress.ip_address(hostname)
        if ip.is_loopback or ip.is_private or ip.is_link_local or ip.is_reserved:
            raise ValueError(
                f"The decoded SimpleFIN token points to a restricted IP address "
                f"({hostname}). Only public SimpleFIN endpoints are permitted."
            )
    except ValueError:
        pass  # Not an IP address — proceed to hostname check.

    if hostname not in _SIMPLEFIN_CLAIM_HOSTS:
        raise ValueError(
            f"The decoded SimpleFIN token points to an unrecognized host "
            f"({hostname}). Only SimpleFIN bridge endpoints are permitted."
        )

    req = urllib.request.Request(claim_url, method="POST")
    req.add_header("User-Agent", "moneyflow/1.0")
    try:
        with urllib.request.urlopen(req) as resp:
            if resp.status != 200:
                raise RuntimeError(
                    f"Unexpected HTTP {resp.status} response while claiming the token."
                )
            access_url = resp.read().decode("utf-8").strip()
            parse_access_url(access_url)
            return access_url
    except urllib.error.HTTPError as exc:
        if exc.code == 403:
            raise PermissionError(
                "HTTP 403: The SimpleFIN token has already been claimed or is invalid. "
                "The token may be compromised — revoke and regenerate it at "
                "https://beta-bridge.simplefin.org"
            ) from exc
        raise RuntimeError(
            f"Unexpected HTTP {exc.code} response while claiming the token."
        ) from exc


# ---------------------------------------------------------------------------
# Transaction parser
# ---------------------------------------------------------------------------


def _parse_transactions(raw_accounts: List[Dict[str, Any]]) -> List[Dict[str, Any]]:
    """
    Flatten the nested SimpleFIN accounts+transactions JSON into a list of
    moneyflow-format transaction dicts.

    Each returned dict satisfies the moneyflow FinanceBackend.get_transactions()
    contract (id, date, amount, merchant, category, account, notes,
    hideFromReports, pending, isRecurring).
    """
    result: List[Dict[str, Any]] = []

    for acct in raw_accounts:
        acct_id = _coerce_str(acct.get("id")) or ""
        acct_name = _coerce_str(acct.get("name")) or acct_id
        currency = _normalize_currency_code(acct.get("currency"))
        txns = acct.get("transactions") or []

        for txn in txns:
            txn_id = _coerce_str(txn.get("id")) or ""
            description = _coerce_str(txn.get("description")) or ""
            amount = _coerce_float(txn.get("amount"))
            pending = _coerce_bool(txn.get("pending"))

            # Date: prefer posted; fall back to transacted_at for pending txns
            date_str = _unix_to_date_str(txn.get("posted"))
            if date_str is None:
                date_str = _unix_to_date_str(txn.get("transacted_at"))
            if date_str is None:
                # Skip transactions with no usable date
                continue

            result.append(
                {
                    "id": f"{acct_id}:{txn_id}",
                    "date": date_str,
                    "amount": amount,
                    "merchant": {"id": description, "name": description},
                    "category": {"id": "uncategorized", "name": "Uncategorized"},
                    "account": {"id": acct_id, "displayName": acct_name},
                    "currency": currency,
                    "notes": "",
                    "hideFromReports": False,
                    "pending": pending,
                    "isRecurring": False,
                }
            )

    return result


# ---------------------------------------------------------------------------
# Async client
# ---------------------------------------------------------------------------


class SimpleFinClient:
    """
    Async SimpleFIN Bridge API client.

    Wraps the SimpleFIN /accounts endpoint with aiohttp, parses the JSON
    response, and returns transactions in the moneyflow dict format.
    """

    def __init__(self, access_url: str) -> None:
        """
        Initialise the client by parsing and validating the Access URL.

        Args:
            access_url: A SimpleFIN Access URL with embedded Basic Auth
                        credentials, e.g.
                        'https://user:pass@bridge.simplefin.org/simplefin'.

        Raises:
            ValueError: If the URL is malformed or not HTTPS.
        """
        parsed = parse_access_url(access_url)
        self._username = parsed["username"]
        self._password = parsed["password"]
        self._base_url = parsed["base_url"].rstrip("/")
        self.currency_code: Optional[str] = None

    async def fetch_transactions(
        self,
        start_date: Optional[str] = None,
        end_date: Optional[str] = None,
    ) -> List[Dict[str, Any]]:
        """
        Fetch transactions from the SimpleFIN Bridge in batched 90-day windows.

        SimpleFIN Bridge enforces a 90-day limit on single requests. When the
        requested date range exceeds 90 days, this method splits the range into
        overlapping batches and merges the results.

        When *neither* start_date nor end_date is provided, a 90-day lookback
        from today is used (safe default for refreshes). Callers that want
        deeper history (e.g. first-time populate) should pass an explicit
        start_date.

        Args:
            start_date: ISO date string 'YYYY-MM-DD'. Transactions on or after
                        this date are included. Defaults to 90 days ago.
            end_date: ISO date string 'YYYY-MM-DD'. Transactions before
                      (exclusive) this date are included. Defaults to today.

        Returns:
            List of transaction dicts in moneyflow format.

        Raises:
            PermissionError: On HTTP 403 (credentials revoked).
            RuntimeError: On HTTP 402 (payment required) or other non-200 status.
        """
        today = date.today()
        start = date.fromisoformat(start_date) if start_date else today - timedelta(days=90)
        end = date.fromisoformat(end_date) if end_date else today

        if start >= end:
            return []

        batch_days = 90
        all_transactions: List[Dict[str, Any]] = []
        currencies: set[str] = set()
        batch_start = start

        url = f"{self._base_url}/accounts"
        auth = aiohttp.BasicAuth(self._username, self._password)

        async with aiohttp.ClientSession() as session:
            while batch_start < end:
                batch_end = min(batch_start + timedelta(days=batch_days), end)
                params: List[tuple] = [
                    ("version", "2"),
                    ("start-date", str(_to_unix(batch_start))),
                    ("end-date", str(_to_unix(batch_end))),
                ]
                async with session.get(url, params=params, auth=auth) as resp:
                    if resp.status == 403:
                        raise PermissionError(
                            "HTTP 403: SimpleFIN access denied. Credentials may be "
                            "invalid or revoked. Visit https://bridge.simplefin.org "
                            "to reconnect."
                        )
                    if resp.status == 402:
                        raise RuntimeError(
                            "HTTP 402: Payment required to access this SimpleFIN server."
                        )
                    if resp.status != 200:
                        raise RuntimeError(
                            f"Unexpected HTTP {resp.status} response from the SimpleFIN server."
                        )
                    body = await resp.json(content_type=None)
                response_errors = body.get("errlist") or []
                if response_errors:
                    raise RuntimeError(
                        "SimpleFIN returned partial account data "
                        f"({len(response_errors)} reported error(s)); refresh was not saved."
                    )
                raw_accounts: List[Dict[str, Any]] = body.get("accounts") or []
                currencies.update(
                    currency
                    for account in raw_accounts
                    if (currency := _normalize_currency_code(account.get("currency")))
                )
                if len(currencies) > 1:
                    self.currency_code = None
                    raise RuntimeError(
                        "SimpleFIN profile contains multiple currencies; "
                        "moneyflow cannot aggregate them safely."
                    )
                all_transactions.extend(_parse_transactions(raw_accounts))
                batch_start = batch_end

        self.currency_code = next(iter(currencies), None)
        return all_transactions
