"""
Tests for merchant caching functionality.

Merchant caching allows MTD mode to have complete merchant autocomplete
without downloading all transactions.
"""

import json
from datetime import datetime, timedelta

import pytest

from moneyflow.data_manager import DataManager


@pytest.fixture
def temp_merchant_cache_dir(tmp_path):
    """Provide a temporary directory for merchant cache."""
    return str(tmp_path / "merchant_cache")


@pytest.fixture
async def data_manager_with_cache(mock_mm, temp_merchant_cache_dir, tmp_path):
    """Provide DataManager with temporary merchant cache and isolated config."""
    await mock_mm.login()
    # Use tmp_path for config_dir to avoid modifying user's ~/.moneyflow/config.yaml
    dm = DataManager(mock_mm, config_dir=str(tmp_path), merchant_cache_dir=temp_merchant_cache_dir)
    return dm


class TestMerchantCacheBasics:
    """Test basic merchant cache operations."""

    async def test_cache_file_created(self, data_manager_with_cache):
        """Test that cache file is created in specified directory."""
        dm = data_manager_with_cache
        assert dm.merchant_cache_file.parent.exists()

    async def test_fresh_cache_not_stale(self, data_manager_with_cache, temp_merchant_cache_dir):
        """Test that freshly saved cache is not considered stale."""
        dm = data_manager_with_cache

        # Save merchants
        merchants = ["Amazon", "Starbucks", "Whole Foods"]
        dm._save_merchant_cache(merchants)

        # Should not be stale
        assert not dm._is_merchant_cache_stale()

    async def test_old_cache_is_stale(self, data_manager_with_cache):
        """Test that cache older than 24 hours is considered stale."""
        dm = data_manager_with_cache

        # Save merchants with old timestamp
        data = {
            "timestamp": (datetime.now() - timedelta(hours=25)).isoformat(),
            "merchants": ["Amazon"],
            "count": 1,
        }

        with open(dm.merchant_cache_file, "w") as f:
            json.dump(data, f)

        # Should be stale
        assert dm._is_merchant_cache_stale()

    async def test_missing_cache_is_stale(self, data_manager_with_cache):
        """Test that missing cache is considered stale."""
        dm = data_manager_with_cache
        assert dm._is_merchant_cache_stale()


class TestMerchantCacheSaveLoad:
    """Test saving and loading merchant cache."""

    async def test_save_and_load_merchants(self, data_manager_with_cache):
        """Test basic save and load cycle."""
        dm = data_manager_with_cache

        merchants = ["Amazon", "Starbucks", "Whole Foods", "Shell"]
        dm._save_merchant_cache(merchants)

        loaded = dm._load_cached_merchants()
        assert set(loaded) == set(merchants)

    async def test_merchants_sorted_on_save(self, data_manager_with_cache):
        """Test that merchants are sorted alphabetically when saved."""
        dm = data_manager_with_cache

        merchants = ["Zebra Corp", "Apple Store", "Microsoft"]
        dm._save_merchant_cache(merchants)

        loaded = dm._load_cached_merchants()
        assert loaded == ["Apple Store", "Microsoft", "Zebra Corp"]

    async def test_duplicate_merchants_deduped(self, data_manager_with_cache):
        """Test that duplicate merchants are removed."""
        dm = data_manager_with_cache

        merchants = ["Amazon", "Amazon", "Starbucks", "Amazon"]
        dm._save_merchant_cache(merchants)

        loaded = dm._load_cached_merchants()
        assert loaded == ["Amazon", "Starbucks"]

    async def test_load_nonexistent_cache_returns_empty(self, data_manager_with_cache):
        """Test that loading nonexistent cache returns empty list."""
        dm = data_manager_with_cache

        loaded = dm._load_cached_merchants()
        assert loaded == []


class TestMerchantCacheRefresh:
    """Test merchant cache refresh logic."""

    async def test_refresh_fetches_from_api_when_stale(self, data_manager_with_cache):
        """Test that stale cache triggers API fetch."""
        dm = data_manager_with_cache

        # No cache exists, should fetch from API
        merchants = await dm.refresh_merchant_cache(force=False)

        # Should have merchants from mock backend
        assert len(merchants) > 0
        assert "Amazon" in merchants  # Mock has Amazon

    async def test_refresh_saves_to_cache(self, data_manager_with_cache):
        """Test that refresh saves merchants to cache file."""
        dm = data_manager_with_cache

        await dm.refresh_merchant_cache(force=False)

        # Cache file should exist
        assert dm.merchant_cache_file.exists()

        # Should be loadable
        loaded = dm._load_cached_merchants()
        assert len(loaded) > 0

    async def test_refresh_with_fresh_cache_uses_cache(self, data_manager_with_cache):
        """Test that refresh with fresh cache doesn't hit API."""
        dm = data_manager_with_cache

        # Pre-populate cache
        dm._save_merchant_cache(["Cached Merchant"])

        # Refresh without force - should use cache
        merchants = await dm.refresh_merchant_cache(force=False)

        # Should have cached merchant, not fresh from API
        assert merchants == ["Cached Merchant"]

    async def test_refresh_with_force_ignores_cache(self, data_manager_with_cache):
        """Test that force=True always fetches from API."""
        dm = data_manager_with_cache

        # Pre-populate cache
        dm._save_merchant_cache(["Cached Merchant"])

        # Force refresh - should hit API
        merchants = await dm.refresh_merchant_cache(force=True)

        # Should have API merchants, not cached
        assert "Cached Merchant" not in merchants
        assert len(merchants) > 0  # Has merchants from mock backend


class TestMerchantAutocomplete:
    """Test merchant autocomplete merging."""

    async def test_autocomplete_merges_cached_and_current(self, data_manager_with_cache):
        """Test that autocomplete includes both cached and current merchants."""
        dm = data_manager_with_cache

        # Pre-populate cache with historical merchants not in current data
        dm._save_merchant_cache(["Historical Merchant 1", "Historical Merchant 2", "Amazon"])

        # Fetch transactions (will load cached merchants and current merchants)
        df, cats, groups = await dm.fetch_all_data()
        dm.df = df

        # Get autocomplete list
        all_merchants = dm.get_all_merchants_for_autocomplete()

        # Should have historical merchants from cache
        assert "Historical Merchant 1" in all_merchants
        assert "Historical Merchant 2" in all_merchants
        # Should also have merchants from current df
        current_merchants = df["merchant"].unique().to_list()
        for m in current_merchants:
            assert m in all_merchants

    async def test_autocomplete_dedupes(self, data_manager_with_cache):
        """Test that autocomplete removes duplicates."""
        dm = data_manager_with_cache

        # Set cached merchants that overlap with loaded transactions
        dm.all_merchants = ["Amazon", "Starbucks"]

        # Fetch transactions (mock has Amazon)
        df, cats, groups = await dm.fetch_all_data()
        dm.df = df

        # Get autocomplete list
        all_merchants = dm.get_all_merchants_for_autocomplete()

        # Should not have duplicates
        assert len(all_merchants) == len(set(all_merchants))

    async def test_autocomplete_sorted(self, data_manager_with_cache):
        """Test that autocomplete list is sorted."""
        dm = data_manager_with_cache

        dm.all_merchants = ["Zebra", "Apple"]

        df, cats, groups = await dm.fetch_all_data()
        dm.df = df

        all_merchants = dm.get_all_merchants_for_autocomplete()

        # Should be sorted
        assert all_merchants == sorted(all_merchants)

    async def test_autocomplete_works_without_df(self, data_manager_with_cache):
        """Test that autocomplete works with only cached merchants (no df loaded)."""
        dm = data_manager_with_cache

        dm.all_merchants = ["Cached Only"]
        dm.df = None

        all_merchants = dm.get_all_merchants_for_autocomplete()

        assert all_merchants == ["Cached Only"]


class TestMerchantCacheIntegration:
    """Test merchant caching integrated with fetch_all_data."""

    async def test_fetch_all_data_populates_merchants(self, data_manager_with_cache):
        """Test that fetch_all_data populates all_merchants."""
        dm = data_manager_with_cache

        df, cats, groups = await dm.fetch_all_data()

        # all_merchants should be populated
        assert len(dm.all_merchants) > 0

    async def test_fetch_all_data_creates_cache_file(self, data_manager_with_cache):
        """Test that fetch_all_data creates merchant cache file."""
        dm = data_manager_with_cache

        await dm.fetch_all_data()

        # Cache file should exist
        assert dm.merchant_cache_file.exists()

    async def test_second_fetch_uses_cached_merchants(self, data_manager_with_cache):
        """Test that second fetch within 24 hours uses cached merchants."""
        dm = data_manager_with_cache

        # First fetch
        await dm.fetch_all_data()
        first_merchants = dm.all_merchants.copy()

        # Second fetch (cache should be fresh)
        dm.all_merchants = []  # Reset
        await dm.fetch_all_data()

        # Should have same merchants from cache
        assert set(dm.all_merchants) == set(first_merchants)


class TestEdgeCases:
    """Test edge cases and error handling."""

    async def test_corrupt_cache_handled_gracefully(self, data_manager_with_cache):
        """Test that corrupt cache doesn't crash the app."""
        dm = data_manager_with_cache

        # Write corrupt cache
        with open(dm.merchant_cache_file, "w") as f:
            f.write("not valid json{{{")

        # Should not crash, just treat as stale
        assert dm._is_merchant_cache_stale()

        # Load should return empty
        assert dm._load_cached_merchants() == []

    async def test_cache_with_no_timestamp(self, data_manager_with_cache):
        """Test cache file without timestamp is treated as stale."""
        dm = data_manager_with_cache

        # Save cache without timestamp
        with open(dm.merchant_cache_file, "w") as f:
            json.dump({"merchants": ["Test"]}, f)

        assert dm._is_merchant_cache_stale()

    async def test_empty_merchant_list(self, data_manager_with_cache):
        """Test saving and loading empty merchant list."""
        dm = data_manager_with_cache

        dm._save_merchant_cache([])

        loaded = dm._load_cached_merchants()
        assert loaded == []
