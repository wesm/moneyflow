"""
Pytest configuration and fixtures for moneyflow tests.

This module provides reusable fixtures and test data for the test suite,
including sample transactions, categories, and mock backends.
"""

from datetime import date
from typing import Union

import polars as pl
import pytest
from rich.text import Text

from moneyflow.data_manager import DataManager
from moneyflow.formatters import ViewPresenter
from moneyflow.state import AppState
from tests.mock_backend import MockMonarchMoney

# ============================================================================
# TEST HELPER FUNCTIONS
# ============================================================================


def expected_amount(amount: float, for_table: bool = False) -> Union[str, Text]:
    """
    Format expected amount for test assertions.

    Centralizes amount formatting expectations so tests are easy to update
    when formatting logic changes. Currency symbol is NOT included in cell
    values (it's shown in column header instead).

    Args:
        amount: The amount to format
        for_table: If True, returns Rich Text object

    Returns:
        Expected formatted string (e.g., "-1,234.56", "+5,000.00")
        Or Rich Text object if for_table=True

    Examples:
        >>> expected_amount(-1234.56)
        '-1,234.56'
        >>> expected_amount(5000.00)
        '+5,000.00'
    """
    return ViewPresenter.format_amount(amount, for_table=for_table)


def save_test_credentials(
    credential_manager,
    email: str = "test@example.com",
    password: str = "test_password",
    mfa_secret: str = "TEST_SECRET_KEY",
    encryption_password: str = "encryption_pass",
    backend_type: str = "monarch",
):
    """
    Save test credentials with default values.

    Helper to eliminate repeated credential_manager.save_credentials() calls
    with the same test data.

    Args:
        credential_manager: CredentialManager instance
        email: Email (default: test@example.com)
        password: Password (default: test_password)
        mfa_secret: MFA secret (default: TEST_SECRET_KEY)
        encryption_password: Encryption password (default: encryption_pass)
        backend_type: Backend type (default: monarch)

    Example:
        >>> save_test_credentials(mgr)  # Uses all defaults
        >>> save_test_credentials(mgr, email="custom@example.com")
    """
    credential_manager.save_credentials(
        email=email,
        password=password,
        mfa_secret=mfa_secret,
        encryption_password=encryption_password,
        backend_type=backend_type,
    )


# ============================================================================
# PYTEST FIXTURES
# ============================================================================


@pytest.fixture
def mock_mm():
    """Provide a fresh MockMonarchMoney instance for each test."""
    return MockMonarchMoney()


@pytest.fixture
async def data_manager(mock_mm, tmp_path):
    """Provide a DataManager with mock backend and isolated config."""
    await mock_mm.login()
    # Use tmp_path as config_dir to avoid using ~/.moneyflow/config.yaml
    return DataManager(mock_mm, config_dir=str(tmp_path))


@pytest.fixture
async def loaded_data_manager(mock_mm, tmp_path):
    """Provide a DataManager with data already loaded and isolated config."""
    await mock_mm.login()
    # Use tmp_path as config_dir to avoid using ~/.moneyflow/config.yaml
    dm = DataManager(mock_mm, config_dir=str(tmp_path))

    # Load all data
    df, categories, category_groups = await dm.fetch_all_data()

    return dm, df, categories, category_groups


@pytest.fixture
def app_state():
    """Provide a fresh AppState for each test."""
    return AppState()


@pytest.fixture
def sample_transactions_df():
    """Provide a sample Polars DataFrame of transactions for testing."""
    data = [
        {
            "id": "txn_1",
            "date": date(2024, 10, 1),
            "amount": -45.67,
            "merchant": "Whole Foods",
            "merchant_id": "merch_wholef",
            "category": "Groceries",
            "category_id": "cat_groceries",
            "group": "Food & Dining",
            "account": "Chase Checking",
            "account_id": "acc_checking",
            "notes": "",
            "hide_from_reports": False,
            "pending": False,
            "is_recurring": False,
        },
        {
            "id": "txn_2",
            "date": date(2024, 10, 2),
            "amount": -23.45,
            "merchant": "Starbucks",
            "merchant_id": "merch_starbucks",
            "category": "Restaurants & Bars",
            "category_id": "cat_restaurants",
            "group": "Food & Dining",
            "account": "Chase Checking",
            "account_id": "acc_checking",
            "notes": "",
            "hide_from_reports": False,
            "pending": False,
            "is_recurring": False,
        },
        {
            "id": "txn_3",
            "date": date(2024, 10, 3),
            "amount": -52.00,
            "merchant": "Shell Gas Station",
            "merchant_id": "merch_shell",
            "category": "Gas",
            "category_id": "cat_gas",
            "group": "Transportation",
            "account": "Chase Checking",
            "account_id": "acc_checking",
            "notes": "",
            "hide_from_reports": False,
            "pending": False,
            "is_recurring": False,
        },
    ]

    return pl.DataFrame(data)


@pytest.fixture
def duplicate_transactions_df():
    """Provide a DataFrame with duplicate transactions for testing."""
    data = [
        {
            "id": "txn_1",
            "date": date(2024, 10, 1),
            "amount": -45.67,
            "merchant": "Starbucks",
            "merchant_id": "merch_1",
            "category": "Restaurants & Bars",
            "category_id": "cat_1",
            "group": "Food & Dining",
            "account": "Chase Checking",
            "account_id": "acc_1",
            "notes": "",
            "hide_from_reports": False,
            "pending": False,
            "is_recurring": False,
        },
        {
            "id": "txn_2",
            "date": date(2024, 10, 1),  # Same date
            "amount": -45.67,  # Same amount
            "merchant": "Starbucks",  # Same merchant
            "merchant_id": "merch_1",
            "category": "Restaurants & Bars",
            "category_id": "cat_1",
            "group": "Food & Dining",
            "account": "Chase Checking",  # Same account
            "account_id": "acc_1",
            "notes": "",
            "hide_from_reports": False,
            "pending": False,
            "is_recurring": False,
        },
        {
            "id": "txn_3",
            "date": date(2024, 10, 2),
            "amount": -50.00,
            "merchant": "Different Merchant",
            "merchant_id": "merch_2",
            "category": "Shopping",
            "category_id": "cat_2",
            "group": "Shopping",
            "account": "Chase Checking",
            "account_id": "acc_1",
            "notes": "",
            "hide_from_reports": False,
            "pending": False,
            "is_recurring": False,
        },
    ]

    return pl.DataFrame(data)
