"""
Tests for merchant caching functionality.

Merchant caching allows MTD mode to have complete merchant autocomplete
without downloading all transactions.
"""

import json
from datetime import datetime, timedelta

import pytest

from moneyflow.data.cache_orchestrator import CacheOrchestrator
from moneyflow.data.data_manager import DataManager


@pytest.fixture
def temp_merchant_cache_dir(tmp_path):
    """Provide a temporary directory for merchant cache."""
    return str(tmp_path / "merchant_cache")


@pytest.fixture
async def dm(mock_mm, temp_merchant_cache_dir, tmp_path):
    """Provide DataManager with temporary merchant cache and isolated config."""
    await mock_mm.login()
    # Use tmp_path for config_dir to avoid modifying user's ~/.moneyflow/config.yaml
    dm_instance = DataManager(
        mock_mm, config_dir=str(tmp_path), merchant_cache_dir=temp_merchant_cache_dir
    )
    return dm_instance


def write_raw_cache(dm, data_dict_or_string):
    """Helper to write raw data or dict to the cache file."""
    with open(dm.merchant_cache.cache_file, "w") as f:
        if isinstance(data_dict_or_string, str):
            f.write(data_dict_or_string)
        else:
            json.dump(data_dict_or_string, f)


class TestMerchantCacheBasics:
    """Test basic merchant cache operations."""

    async def test_cache_file_created(self, dm):
        """Test that cache file is created in specified directory."""
        assert dm.merchant_cache.cache_file.parent.exists()

    async def test_fresh_cache_not_stale(self, dm):
        """Test that freshly saved cache is not considered stale."""
        # Save merchants
        merchants = ["Amazon", "Starbucks", "Whole Foods"]
        dm.merchant_cache.save(merchants)

        # Should not be stale
        assert not dm.merchant_cache.is_stale()

    async def test_old_cache_is_stale(self, dm):
        """Test that cache older than 24 hours is considered stale."""
        # Save merchants with old timestamp
        data = {
            "timestamp": (datetime.now() - timedelta(hours=25)).isoformat(),
            "merchants": ["Amazon"],
            "count": 1,
        }

        write_raw_cache(dm, data)

        # Should be stale
        assert dm.merchant_cache.is_stale()

    async def test_missing_cache_is_stale(self, dm):
        """Test that missing cache is considered stale."""
        assert dm.merchant_cache.is_stale()


class TestMerchantCacheSaveLoad:
    """Test saving and loading merchant cache."""

    async def test_save_and_load_merchants(self, dm):
        """Test basic save and load cycle."""
        merchants = ["Amazon", "Starbucks", "Whole Foods", "Shell"]
        dm.merchant_cache.save(merchants)

        loaded = dm.merchant_cache.load()
        assert set(loaded) == set(merchants)

    async def test_merchants_sorted_on_save(self, dm):
        """Test that merchants are sorted alphabetically when saved."""
        merchants = ["Zebra Corp", "Apple Store", "Microsoft"]
        dm.merchant_cache.save(merchants)

        loaded = dm.merchant_cache.load()
        assert loaded == ["Apple Store", "Microsoft", "Zebra Corp"]

    async def test_duplicate_merchants_deduped(self, dm):
        """Test that duplicate merchants are removed."""
        merchants = ["Amazon", "Amazon", "Starbucks", "Amazon"]
        dm.merchant_cache.save(merchants)

        loaded = dm.merchant_cache.load()
        assert loaded == ["Amazon", "Starbucks"]

    async def test_load_nonexistent_cache_returns_empty(self, dm):
        """Test that loading nonexistent cache returns empty list."""
        loaded = dm.merchant_cache.load()
        assert loaded == []


class TestMerchantCacheRefresh:
    """Test merchant cache refresh logic."""

    async def test_refresh_fetches_from_api_when_stale(self, dm, mock_mm, mocker):
        """Test that stale cache triggers API fetch."""
        spy = mocker.spy(mock_mm, "get_all_merchants")
        # No cache exists, should fetch from API
        merchants = await dm.refresh_merchant_cache(force=False)

        # Should have merchants from mock backend
        assert len(merchants) > 0
        assert "Amazon" in merchants  # Mock has Amazon
        spy.assert_called()

    async def test_refresh_saves_to_cache(self, dm, mock_mm, mocker):
        """Test that refresh saves merchants to cache file."""
        spy = mocker.spy(mock_mm, "get_all_merchants")
        await dm.refresh_merchant_cache(force=False)

        # Cache file should exist
        assert dm.merchant_cache.cache_file.exists()

        # Should be loadable
        loaded = dm.merchant_cache.load()
        assert len(loaded) > 0
        spy.assert_called()

    async def test_refresh_with_fresh_cache_uses_cache(self, dm, mock_mm, mocker):
        """Test that refresh with fresh cache doesn't hit API."""
        # Pre-populate cache
        dm.merchant_cache.save(["Cached Merchant"])

        spy = mocker.spy(mock_mm, "get_all_merchants")

        # Refresh without force - should use cache
        merchants = await dm.refresh_merchant_cache(force=False)

        # Should have cached merchant, not fresh from API
        assert merchants == ["Cached Merchant"]
        # Explicitly verify the network call was bypassed
        spy.assert_not_called()

    async def test_refresh_with_force_ignores_cache(self, dm, mock_mm, mocker):
        """Test that force=True always fetches from API."""
        # Pre-populate cache
        dm.merchant_cache.save(["Cached Merchant"])

        spy = mocker.spy(mock_mm, "get_all_merchants")

        # Force refresh - should hit API
        merchants = await dm.refresh_merchant_cache(force=True)

        # Should have API merchants, not cached
        assert "Cached Merchant" not in merchants
        assert len(merchants) > 0  # Has merchants from mock backend
        spy.assert_called()


class TestMerchantAutocomplete:
    """Test merchant autocomplete merging."""

    async def test_autocomplete_merges_cached_and_current(self, dm):
        """Test that autocomplete includes both cached and current merchants."""
        # Pre-populate cache with historical merchants not in current data
        dm.merchant_cache.save(["Historical Merchant 1", "Historical Merchant 2", "Amazon"])

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

    async def test_autocomplete_dedupes(self, dm):
        """Test that autocomplete removes duplicates."""
        # Set cached merchants that overlap with loaded transactions
        dm.merchant_cache.save(["Amazon", "Starbucks"])

        # Fetch transactions (mock has Amazon)
        df, cats, groups = await dm.fetch_all_data()
        dm.df = df

        # Get autocomplete list
        all_merchants = dm.get_all_merchants_for_autocomplete()

        # Should not have duplicates
        assert len(all_merchants) == len(set(all_merchants))

    async def test_autocomplete_sorted(self, dm):
        """Test that autocomplete list is sorted."""
        dm.merchant_cache.save(["Zebra", "Apple"])

        df, cats, groups = await dm.fetch_all_data()
        dm.df = df

        all_merchants = dm.get_all_merchants_for_autocomplete()

        # Should be sorted
        assert all_merchants == sorted(all_merchants)

    async def test_autocomplete_works_without_df(self, dm):
        """Test that autocomplete works with only cached merchants (no df loaded)."""
        dm.merchant_cache.save(["Cached Only"])

        # Load the cache properly via public method instead of directly mutating state
        orchestrator = CacheOrchestrator(cache_manager=None, data_manager=dm)
        await orchestrator.load_merchant_cache()

        all_merchants = dm.get_all_merchants_for_autocomplete()

        assert all_merchants == ["Cached Only"]


class TestMerchantCacheIntegration:
    """Test merchant caching integrated with fetch_all_data."""

    async def test_fetch_all_data_populates_merchants(self, dm):
        """Test that fetch_all_data populates all_merchants."""
        df, cats, groups = await dm.fetch_all_data()

        # all_merchants should be populated
        assert len(dm.all_merchants) > 0

    async def test_fetch_all_data_creates_cache_file(self, dm):
        """Test that fetch_all_data creates merchant cache file."""
        await dm.fetch_all_data()

        # Cache file should exist
        assert dm.merchant_cache.cache_file.exists()

    async def test_second_fetch_uses_cached_merchants(self, dm, mock_mm, mocker):
        """Test that second fetch within 24 hours uses cached merchants."""
        # First fetch
        await dm.fetch_all_data()
        first_merchants = dm.all_merchants.copy()

        spy = mocker.spy(mock_mm, "get_all_merchants")

        # Second fetch (cache should be fresh)
        dm.all_merchants = []  # Reset for testing
        await dm.fetch_all_data()

        # Should have same merchants from cache
        assert set(dm.all_merchants) == set(first_merchants)
        spy.assert_not_called()


class TestEdgeCases:
    """Test edge cases and error handling."""

    async def test_corrupt_cache_handled_gracefully(self, dm):
        """Test that corrupt cache doesn't crash the app."""
        # Write corrupt cache
        write_raw_cache(dm, "not valid json{{{")

        # Should not crash, just treat as stale
        assert dm.merchant_cache.is_stale()

        # Load should return empty
        assert dm.merchant_cache.load() == []

    async def test_cache_with_no_timestamp(self, dm):
        """Test cache file without timestamp is treated as stale."""
        # Save cache without timestamp
        write_raw_cache(dm, {"merchants": ["Test"]})

        assert dm.merchant_cache.is_stale()

    async def test_empty_merchant_list(self, dm):
        """Test saving and loading empty merchant list."""
        dm.merchant_cache.save([])

        loaded = dm.merchant_cache.load()
        assert loaded == []
