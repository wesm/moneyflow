"""
Pytest fixtures for YNAB integration tests.

These fixtures handle:
- Loading API credentials from environment
- Setting up test payees and transactions
- Cleaning up test data after tests
"""

import os
import uuid
from typing import Any, Dict, Generator, List, Optional

import pytest

from moneyflow.ynab_client import YNABClient


def get_test_credentials() -> tuple[Optional[str], Optional[str]]:
    """
    Get YNAB test credentials from environment.

    Returns:
        Tuple of (api_key, budget_id) where both may be None
    """
    api_key = os.environ.get("YNAB_TEST_API_KEY")
    budget_id = os.environ.get("YNAB_TEST_BUDGET_ID")
    return api_key, budget_id


def require_ynab_credentials():
    """
    Pytest marker to skip test if YNAB credentials are not available.
    """
    api_key, _ = get_test_credentials()
    return pytest.mark.skipif(
        not api_key,
        reason="YNAB_TEST_API_KEY environment variable not set",
    )


# Apply this to all tests in the integration_tests module
pytestmark = [
    require_ynab_credentials(),
    pytest.mark.integration,
]


@pytest.fixture(scope="session")
def ynab_credentials() -> tuple[str, Optional[str]]:
    """
    Session-scoped fixture providing YNAB test credentials.
    """
    api_key, budget_id = get_test_credentials()
    if not api_key:
        pytest.skip("YNAB_TEST_API_KEY not set")
    return api_key, budget_id


@pytest.fixture(scope="session")
def ynab_client(ynab_credentials: tuple[str, Optional[str]]) -> Generator[YNABClient, None, None]:
    """
    Session-scoped fixture providing an authenticated YNAB client.

    The client is authenticated once per test session for efficiency.
    If the test budget has no on-budget accounts, one is created automatically.
    """
    import ynab

    api_key, budget_id = ynab_credentials
    client = YNABClient()
    client.login(api_key, budget_id)

    # Check if test budget has at least one on-budget, open account
    on_budget_accounts = [
        acc
        for acc in (client._account_cache or {}).values()
        if acc["on_budget"] and not acc["closed"]
    ]

    if not on_budget_accounts:
        # No suitable account exists - create one for testing
        accounts_api = ynab.AccountsApi(client.api_client)

        save_account = ynab.SaveAccount(
            name="__test_checking_account__",
            type="checking",
            balance=0,  # Balance in milliunits
        )
        wrapper = ynab.PostAccountWrapper(account=save_account)

        response = accounts_api.create_account(budget_id=client.budget_id, data=wrapper)

        # Refresh account cache to include the newly created account
        client._fetch_and_cache_accounts()

        print(
            f"\nCreated test account: {response.data.account.name} (id={response.data.account.id})"
        )

    yield client
    client.close()


@pytest.fixture
def test_payee_prefix() -> str:
    """
    Generate a unique prefix for test payee names.

    This helps identify and clean up test data.
    """
    return f"__test_{uuid.uuid4().hex[:8]}_"


@pytest.fixture
def create_test_payee(ynab_client: YNABClient, test_payee_prefix: str):
    """
    Factory fixture for creating test payees.

    Returns a function that creates payees with the test prefix.

    Note: YNAB API doesn't have a direct "create payee" endpoint.
    Payees are created implicitly when used in transactions.
    This fixture returns payee info that can be used in transaction creation.
    """

    def _create_payee(name: str) -> Dict[str, Any]:
        """
        Create a test payee with the given name.

        The name will be prefixed with the test prefix for easy identification.
        """
        full_name = f"{test_payee_prefix}{name}"

        # For now, return payee info that can be used in transaction creation
        return {
            "name": full_name,
            "created_by_test": True,
        }

    yield _create_payee

    # Note: YNAB API doesn't support payee deletion.
    # Test payees will remain but are identifiable by prefix.
    # Consider periodically cleaning up payees with __test_ prefix.


@pytest.fixture
def create_test_transaction(ynab_client: YNABClient, test_payee_prefix: str):
    """
    Factory fixture for creating test transactions.

    Returns a function that creates transactions in the test budget.
    All created transactions are tracked and deleted during cleanup.
    """
    import ynab

    created_transaction_ids: List[str] = []

    def _create_transaction(
        payee_name: str,
        amount_dollars: float,
        account_id: Optional[str] = None,
        category_id: Optional[str] = None,
        date: Optional[str] = None,
        memo: Optional[str] = None,
    ) -> Dict[str, Any]:
        """
        Create a test transaction.

        Args:
            payee_name: Payee name (will be prefixed with test prefix)
            amount_dollars: Amount in dollars (negative for expenses)
            account_id: Account ID (uses first budget account if not specified)
            category_id: Category ID (optional)
            date: Date in YYYY-MM-DD format (today if not specified)
            memo: Transaction memo

        Returns:
            Created transaction data
        """
        import datetime

        full_payee_name = f"{test_payee_prefix}{payee_name}"

        # Get first budget account if not specified
        if not account_id:
            if not ynab_client._account_cache:
                ynab_client._fetch_and_cache_accounts()
            budget_accounts = [
                acc_id
                for acc_id, acc in ynab_client._account_cache.items()
                if acc["on_budget"] and not acc["closed"]
            ]
            if not budget_accounts:
                # Provide helpful error message with account details
                all_accounts = [
                    f"{acc['name']} (on_budget={acc['on_budget']}, closed={acc['closed']})"
                    for acc in ynab_client._account_cache.values()
                ]
                account_list = "\n  ".join(all_accounts) if all_accounts else "(none)"
                raise ValueError(
                    f"No on-budget, open accounts found in test budget.\n"
                    f"Available accounts:\n  {account_list}\n\n"
                    f"Please add at least one on-budget account to your test budget at:\n"
                    f"https://app.ynab.com/settings/accounts"
                )
            account_id = budget_accounts[0]

        # Use today's date if not specified
        if not date:
            date = datetime.date.today().isoformat()

        # Convert dollars to milliunits (YNAB uses 1/1000 of currency unit)
        amount_milliunits = int(amount_dollars * 1000)

        transactions_api = ynab.TransactionsApi(ynab_client.api_client)

        # Create transaction using NewTransaction
        save_txn = ynab.NewTransaction.model_validate(
            {
                "account_id": account_id,
                "date": date,
                "amount": amount_milliunits,
                "payee_name": full_payee_name,
                "category_id": category_id,
                "memo": memo or "Integration test transaction",
            }
        )

        wrapper = ynab.PostTransactionsWrapper(transaction=save_txn)
        response = transactions_api.create_transaction(
            budget_id=ynab_client.budget_id, data=wrapper
        )

        transaction = response.data.transaction
        created_transaction_ids.append(transaction.id)

        return {
            "id": transaction.id,
            "date": str(transaction.var_date),
            "amount": float(transaction.amount) / 1000.0,
            "payee_id": transaction.payee_id,
            "payee_name": transaction.payee_name,
            "account_id": transaction.account_id,
            "category_id": transaction.category_id,
            "memo": transaction.memo,
        }

    yield _create_transaction

    # Cleanup: Delete all created transactions
    transactions_api = ynab.TransactionsApi(ynab_client.api_client)
    for txn_id in created_transaction_ids:
        try:
            transactions_api.delete_transaction(
                budget_id=ynab_client.budget_id, transaction_id=txn_id
            )
        except Exception:
            # Transaction may already be deleted, ignore errors
            pass

    # Invalidate cache after cleanup
    ynab_client._invalidate_cache()


@pytest.fixture
def get_payee_by_name(ynab_client: YNABClient):
    """
    Factory fixture for looking up payees by name.

    Returns a function that finds a payee by exact name match.
    """
    import ynab

    def _get_payee(name: str) -> Optional[Dict[str, Any]]:
        """
        Find a payee by exact name.

        Returns payee dict or None if not found.
        """
        payees_api = ynab.PayeesApi(ynab_client.api_client)
        response = payees_api.get_payees(budget_id=ynab_client.budget_id)

        for payee in response.data.payees:
            if payee.name == name:
                return {
                    "id": payee.id,
                    "name": payee.name,
                }
        return None

    return _get_payee


@pytest.fixture
def get_transactions_by_payee(ynab_client: YNABClient):
    """
    Factory fixture for getting all transactions for a payee.

    Returns a function that fetches transactions by payee ID.
    """
    import ynab

    def _get_transactions(payee_id: str) -> List[Dict[str, Any]]:
        """
        Get all transactions for a given payee ID.
        """
        transactions_api = ynab.TransactionsApi(ynab_client.api_client)
        response = transactions_api.get_transactions_by_payee(
            budget_id=ynab_client.budget_id, payee_id=payee_id
        )

        return [
            {
                "id": txn.id,
                "date": str(txn.var_date),
                "amount": float(txn.amount) / 1000.0,
                "payee_id": txn.payee_id,
                "payee_name": txn.payee_name,
            }
            for txn in response.data.transactions
        ]

    return _get_transactions
