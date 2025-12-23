"""
Tests for CacheOrchestrator cache flow logic.

These validate normal-use cache behavior without running the UI.
"""

import base64
from datetime import date, datetime, timedelta

import polars as pl
import pytest
from cryptography.hazmat.primitives import hashes
from cryptography.hazmat.primitives.kdf.pbkdf2 import PBKDF2HMAC

from moneyflow.cache_manager import CacheManager, RefreshStrategy
from moneyflow.cache_orchestrator import CacheOrchestrator


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


class DummyDataManager:
    def __init__(self, categories, category_groups):
        self.categories = categories
        self.category_groups = category_groups
        self.all_merchants = []
        self.fetch_calls = []
        self._fetch_df = None

    def apply_category_groups(self, df: pl.DataFrame) -> pl.DataFrame:
        return df

    async def refresh_merchant_cache(self, force: bool = False):
        return ["Amazon", "Whole Foods"]

    def set_fetch_result(self, df: pl.DataFrame):
        self._fetch_df = df

    async def fetch_all_data(self, start_date=None, end_date=None, progress_callback=None):
        self.fetch_calls.append((start_date, end_date))
        return self._fetch_df, self.categories, self.category_groups


@pytest.mark.asyncio
async def test_check_and_load_cache_returns_full_cache(
    cache_manager, sample_categories, sample_category_groups
):
    today = date.today()
    boundary = today - timedelta(days=CacheManager.HOT_WINDOW_DAYS)
    dates = [
        (boundary - timedelta(days=1)).isoformat(),
        (boundary + timedelta(days=1)).isoformat(),
    ]
    df = create_transactions_df(dates, "tx")
    cache_manager.save_cache(df, sample_categories, sample_category_groups)

    dm = DummyDataManager(sample_categories, sample_category_groups)
    orchestrator = CacheOrchestrator(cache_manager, dm)

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
async def test_check_and_load_cache_hot_only_mode(
    cache_manager, sample_categories, sample_category_groups
):
    today = date.today()
    boundary = today - timedelta(days=CacheManager.HOT_WINDOW_DAYS)
    dates = [
        (today - timedelta(days=5)).isoformat(),
        (boundary + timedelta(days=1)).isoformat(),
        (boundary - timedelta(days=1)).isoformat(),
    ]
    df = create_transactions_df(dates, "tx")
    cache_manager.save_cache(df, sample_categories, sample_category_groups)

    metadata = cache_manager.load_metadata()
    metadata["cold"]["fetch_timestamp"] = (datetime.now() - timedelta(days=40)).isoformat()
    cache_manager._save_metadata(metadata)

    dm = DummyDataManager(sample_categories, sample_category_groups)
    orchestrator = CacheOrchestrator(cache_manager, dm)

    hot_df = cache_manager.load_hot_cache()
    data, strategy = await orchestrator.check_and_load_cache(
        force_refresh=False,
        custom_start_date=(today - timedelta(days=7)).isoformat(),
        status_update=None,
    )

    assert strategy == RefreshStrategy.NONE
    assert data is not None
    loaded_df, _, _ = data
    assert len(loaded_df) == len(hot_df)


@pytest.mark.asyncio
async def test_partial_refresh_hot_only_updates_hot(
    cache_manager, sample_categories, sample_category_groups
):
    today = date.today()
    dates = [
        (today - timedelta(days=5)).isoformat(),
        (today - timedelta(days=120)).isoformat(),
    ]
    df = create_transactions_df(dates, "tx")
    cache_manager.save_cache(df, sample_categories, sample_category_groups)

    dm = DummyDataManager(sample_categories, sample_category_groups)
    orchestrator = CacheOrchestrator(cache_manager, dm)

    new_hot_df = create_transactions_df([(today - timedelta(days=3)).isoformat()], "new")
    dm.set_fetch_result(new_hot_df)

    result = await orchestrator.partial_refresh(
        strategy=RefreshStrategy.HOT_ONLY,
        creds=None,
        status_update=None,
    )

    assert result is not None
    merged_df, _, _ = result
    assert len(merged_df) >= len(new_hot_df)

    saved_hot = cache_manager.load_hot_cache()
    saved_cold = cache_manager.load_cold_cache()
    assert len(saved_hot) == len(new_hot_df)
    assert saved_cold is not None and len(saved_cold) > 0


@pytest.mark.asyncio
async def test_partial_refresh_cold_only_updates_cold(
    cache_manager, sample_categories, sample_category_groups
):
    today = date.today()
    dates = [
        (today - timedelta(days=5)).isoformat(),
        (today - timedelta(days=120)).isoformat(),
    ]
    df = create_transactions_df(dates, "tx")
    cache_manager.save_cache(df, sample_categories, sample_category_groups)

    dm = DummyDataManager(sample_categories, sample_category_groups)
    orchestrator = CacheOrchestrator(cache_manager, dm)

    new_cold_df = create_transactions_df([(today - timedelta(days=200)).isoformat()], "cold")
    dm.set_fetch_result(new_cold_df)

    result = await orchestrator.partial_refresh(
        strategy=RefreshStrategy.COLD_ONLY,
        creds=None,
        status_update=None,
    )

    assert result is not None
    merged_df, _, _ = result
    assert len(merged_df) >= len(new_cold_df)

    saved_hot = cache_manager.load_hot_cache()
    saved_cold = cache_manager.load_cold_cache()
    assert saved_hot is not None and len(saved_hot) > 0
    assert len(saved_cold) == len(new_cold_df)
