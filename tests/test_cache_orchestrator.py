"""
Tests for CacheOrchestrator cache flow logic.

These validate normal-use cache behavior without running the UI.
"""

import base64
from datetime import date, timedelta
from unittest.mock import AsyncMock, create_autospec

import polars as pl
import pytest
from cryptography.hazmat.primitives import hashes
from cryptography.hazmat.primitives.kdf.pbkdf2 import PBKDF2HMAC

from moneyflow.data.cache_manager import CacheManager, RefreshStrategy
from moneyflow.data.cache_orchestrator import CacheOrchestrator
from moneyflow.data.data_manager import DataManager


@pytest.fixture
def encryption_key():
    """Create a test encryption key using the same method as CredentialManager."""
    password = "test_password"
    salt = b"test_salt_123456"
    kdf = PBKDF2HMAC(
        algorithm=hashes.SHA256(),
        length=32,
        salt=salt,
        iterations=100000,
    )
    return base64.urlsafe_b64encode(kdf.derive(password.encode()))


@pytest.fixture
def temp_cache_dir(tmp_path):
    cache_dir = tmp_path / "cache"
    cache_dir.mkdir()
    return str(cache_dir)


@pytest.fixture
def cache_manager(temp_cache_dir, encryption_key):
    return CacheManager(cache_dir=temp_cache_dir, encryption_key=encryption_key)


@pytest.fixture
def sample_categories():
    return {"cat1": {"id": "cat1", "name": "Shopping", "group": "Shopping"}}


@pytest.fixture
def sample_category_groups():
    return {"Shopping": ["cat1"]}


def create_transactions_df(dates: list[str], prefix: str) -> pl.DataFrame:
    return (
        pl.DataFrame(
            {
                "id": [f"{prefix}{i}" for i in range(len(dates))],
                "date": dates,
                "merchant": [f"Merchant{i}" for i in range(len(dates))],
                "amount": [-10.0 * (i + 1) for i in range(len(dates))],
                "category": ["Shopping"] * len(dates),
                "category_id": ["cat1"] * len(dates),
            }
        )
        .with_columns(pl.col("date").str.to_date("%Y-%m-%d"))
        .sort("date")
    )


@pytest.fixture
def mock_data_manager(sample_categories, sample_category_groups):
    dm = create_autospec(DataManager, instance=True)
    dm.apply_category_groups.side_effect = lambda df: df
    dm.refresh_merchant_cache = AsyncMock(return_value=["Amazon", "Whole Foods"])
    dm.fetch_all_data = AsyncMock(return_value=(None, sample_categories, sample_category_groups))
    return dm


@pytest.fixture
def test_dates():
    today = date.today()
    boundary = today - timedelta(days=CacheManager.HOT_WINDOW_DAYS)
    return {
        "hot_recent": (today - timedelta(days=5)).isoformat(),
        "hot_boundary": (boundary + timedelta(days=1)).isoformat(),
        "cold_boundary": (boundary - timedelta(days=1)).isoformat(),
        "cold_old": (today - timedelta(days=120)).isoformat(),
        "hot_new": (today - timedelta(days=3)).isoformat(),
        "cold_new": (today - timedelta(days=200)).isoformat(),
        "custom_start": (today - timedelta(days=7)).isoformat(),
    }


@pytest.fixture
def setup_orchestrator(cache_manager, mock_data_manager, sample_categories, sample_category_groups):
    def _setup(dates, prefix="tx"):
        df = create_transactions_df(dates, prefix)
        cache_manager.save_cache(df, sample_categories, sample_category_groups)
        return CacheOrchestrator(cache_manager, mock_data_manager), df

    return _setup


@pytest.mark.asyncio
async def test_check_and_load_cache_returns_full_cache(
    setup_orchestrator, test_dates, sample_categories, sample_category_groups
):
    orchestrator, df = setup_orchestrator([test_dates["cold_boundary"], test_dates["hot_boundary"]])

    status = []
    data, strategy = await orchestrator.check_and_load_cache(
        force_refresh=False,
        custom_start_date=None,
        status_update=status.append,
    )

    assert strategy == RefreshStrategy.NONE
    assert data is not None
    loaded_df, categories, groups = data
    assert len(loaded_df) == len(df)
    assert categories == sample_categories
    assert groups == sample_category_groups


@pytest.mark.asyncio
async def test_check_and_load_cache_hot_only_mode(setup_orchestrator, cache_manager, test_dates):
    orchestrator, _ = setup_orchestrator(
        [test_dates["hot_recent"], test_dates["hot_boundary"], test_dates["cold_boundary"]]
    )

    cache_manager._expire_cache_for_testing("cold", days_old=40)

    hot_df = cache_manager.load_hot_cache()
    data, strategy = await orchestrator.check_and_load_cache(
        force_refresh=False,
        custom_start_date=test_dates["custom_start"],
        status_update=None,
    )

    assert strategy == RefreshStrategy.NONE
    assert data is not None
    loaded_df, _, _ = data
    assert len(loaded_df) == len(hot_df)


@pytest.mark.asyncio
async def test_partial_refresh_hot_only_updates_hot(
    setup_orchestrator,
    cache_manager,
    mock_data_manager,
    sample_categories,
    sample_category_groups,
    test_dates,
):
    orchestrator, _ = setup_orchestrator([test_dates["hot_recent"], test_dates["cold_old"]])

    new_hot_df = create_transactions_df([test_dates["hot_new"]], "new")
    mock_data_manager.fetch_all_data = AsyncMock(
        return_value=(new_hot_df, sample_categories, sample_category_groups)
    )

    result = await orchestrator.partial_refresh(
        strategy=RefreshStrategy.HOT_ONLY,
        creds=None,
        status_update=None,
    )

    assert result is not None
    merged_df, _, _ = result

    saved_cold = cache_manager.load_cold_cache()
    expected_len = len(saved_cold) + len(new_hot_df)
    assert len(merged_df) == expected_len

    saved_hot = cache_manager.load_hot_cache()
    assert len(saved_hot) == len(new_hot_df)
    assert saved_cold is not None and len(saved_cold) > 0


@pytest.mark.asyncio
async def test_partial_refresh_cold_only_updates_cold(
    setup_orchestrator,
    cache_manager,
    mock_data_manager,
    sample_categories,
    sample_category_groups,
    test_dates,
):
    orchestrator, _ = setup_orchestrator([test_dates["hot_recent"], test_dates["cold_old"]])

    new_cold_df = create_transactions_df([test_dates["cold_new"]], "cold")
    mock_data_manager.fetch_all_data = AsyncMock(
        return_value=(new_cold_df, sample_categories, sample_category_groups)
    )

    result = await orchestrator.partial_refresh(
        strategy=RefreshStrategy.COLD_ONLY,
        creds=None,
        status_update=None,
    )

    assert result is not None
    merged_df, _, _ = result

    saved_hot = cache_manager.load_hot_cache()
    expected_len = len(saved_hot) + len(new_cold_df)
    assert len(merged_df) == expected_len

    saved_cold = cache_manager.load_cold_cache()
    assert saved_hot is not None and len(saved_hot) > 0
    assert len(saved_cold) == len(new_cold_df)


@pytest.mark.asyncio
async def test_load_merchant_cache_protocol_double():
    """Regression test ensuring load_merchant_cache uses the return value of initialize_merchants."""

    class MockDataManagerDouble:
        def __init__(self):
            self.all_merchants = []
            self._merchants_to_return = ["Amazon", "Target"]

        async def initialize_merchants(self, force: bool = False) -> list[str]:
            # DO NOT set self.all_merchants here (simulating the bug's weakness)
            # Just return the loaded merchants
            return self._merchants_to_return

        def apply_category_groups(self, df):
            pass

        async def fetch_all_data(self, start_date=None, end_date=None, progress_callback=None):
            return None, {}, {}

    double = MockDataManagerDouble()
    orchestrator = CacheOrchestrator(cache_manager=None, data_manager=double)

    await orchestrator.load_merchant_cache()

    # Verify the return value was correctly assigned to all_merchants by the orchestrator
    assert double.all_merchants == ["Amazon", "Target"]
