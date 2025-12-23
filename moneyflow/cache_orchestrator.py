"""
Cache orchestration for the Moneyflow app.

Extracts cache strategy decisions and partial refresh logic from app.py so it
can be unit tested without running the UI.
"""

from __future__ import annotations

from datetime import date as date_type
from datetime import datetime, timedelta
from typing import Any, Callable, Optional, Tuple

from .cache_manager import CacheManager, RefreshStrategy
from .logging_config import get_logger

StatusUpdate = Optional[Callable[[str], None]]
NotifyFn = Optional[Callable[..., None]]


class CacheOrchestrator:
    """Coordinate cache loading and refresh operations."""

    def __init__(
        self,
        cache_manager: CacheManager,
        data_manager: Any,
        notify: NotifyFn = None,
    ) -> None:
        self.cache_manager = cache_manager
        self.data_manager = data_manager
        self._notify_fn = notify
        self.logger = get_logger(__name__)

    @staticmethod
    def is_within_hot_window(custom_start_date: Optional[str]) -> bool:
        """Check if requested date range is entirely within hot cache window."""
        if not custom_start_date:
            return False

        try:
            start = datetime.strptime(custom_start_date, "%Y-%m-%d").date()
            boundary = date_type.today() - timedelta(days=CacheManager.HOT_WINDOW_DAYS)
            return start >= boundary
        except ValueError:
            return False

    async def load_merchant_cache(self) -> None:
        """Load merchant cache for autocomplete. Logs warning on failure."""
        try:
            cached_merchants = await self.data_manager.refresh_merchant_cache(force=False)
            self.data_manager.all_merchants = cached_merchants
            self.logger.debug(f"Loaded {len(cached_merchants)} merchants from cache")
        except Exception as exc:
            self.logger.warning(f"Merchant cache load failed: {exc}")
            self.data_manager.all_merchants = []

    def _status_update(self, status_update: StatusUpdate, message: str) -> None:
        if status_update:
            status_update(message)

    def _notify(self, message: str, severity: str = "information", timeout: float = 4.0) -> None:
        if self._notify_fn:
            self._notify_fn(message, severity=severity, timeout=timeout)

    async def check_and_load_cache(
        self,
        force_refresh: bool,
        custom_start_date: Optional[str],
        status_update: StatusUpdate,
    ) -> Tuple[Optional[Tuple[Any, Any, Any]], RefreshStrategy]:
        """
        Check cache status and determine refresh strategy.

        Returns:
            tuple: (data, strategy) where data is (df, categories, category_groups) or None.
        """
        # No cache manager = always fetch from API
        if not self.cache_manager:
            return None, RefreshStrategy.ALL

        # Get refresh strategy from cache manager
        strategy = self.cache_manager.get_refresh_strategy(force_refresh=force_refresh)

        # Override: in hot-only view, --refresh only refreshes hot tier
        if (
            force_refresh
            and self.is_within_hot_window(custom_start_date)
            and self.cache_manager.is_cold_cache_valid()
        ):
            strategy = RefreshStrategy.HOT_ONLY
        self.logger.debug(f"Cache refresh strategy: {strategy.value}")

        # Check if we can use hot-only optimization
        hot_only_mode = (
            self.is_within_hot_window(custom_start_date) and self.cache_manager.is_hot_cache_valid()
        )

        if strategy == RefreshStrategy.NONE or (
            hot_only_mode and strategy == RefreshStrategy.COLD_ONLY
        ):
            if hot_only_mode:
                self._status_update(status_update, "📦 Loading recent transactions from cache...")
                self.logger.info("Hot-only optimization: skipping cold cache for recent query")
                hot_df = self.cache_manager.load_hot_cache()
                full_result = self.cache_manager.load_cache()
                if hot_df is not None and full_result:
                    _, categories, category_groups, _ = full_result
                    self._status_update(status_update, "🔄 Applying category groupings...")
                    df = self.data_manager.apply_category_groups(hot_df)
                    await self.load_merchant_cache()
                    self._status_update(
                        status_update, f"✅ Loaded {len(df):,} recent transactions!"
                    )
                    self._notify("📦 Loaded recent transactions (hot cache only)")
                    return (df, categories, category_groups), RefreshStrategy.NONE
            else:
                self._status_update(status_update, "📦 Loading from cache...")
                cache_info = self.cache_manager.get_cache_info()
                result = self.cache_manager.load_cache()
                if result:
                    df, categories, category_groups, _ = result
                    self._status_update(status_update, "🔄 Applying category groupings...")
                    df = self.data_manager.apply_category_groups(df)
                    await self.load_merchant_cache()
                    self._status_update(
                        status_update, f"✅ Loaded {len(df):,} transactions from cache!"
                    )
                    if cache_info:
                        self._notify(
                            f"📦 Loaded from cache ({cache_info['age']}) • Use --refresh to force update"
                        )
                    return (df, categories, category_groups), strategy

            self._status_update(status_update, "⚠ Cache load failed, fetching from API...")
            return None, RefreshStrategy.ALL

        return None, strategy

    async def partial_refresh(
        self, strategy: RefreshStrategy, creds: Optional[dict], status_update: StatusUpdate
    ) -> Optional[Tuple[Any, Any, Any]]:
        """
        Perform a partial refresh of cache data.

        Returns:
            tuple: (df, categories, category_groups) or None on failure
        """
        # Determine which tier to load from cache vs fetch from API
        is_hot_refresh = strategy == RefreshStrategy.HOT_ONLY
        tier_name = "hot" if is_hot_refresh else "cold"

        # Load the valid tier from cache
        cached_df = (
            self.cache_manager.load_cold_cache()
            if is_hot_refresh
            else self.cache_manager.load_hot_cache()
        )
        if cached_df is None:
            self.logger.warning(f"Failed to load {tier_name} cache, falling back to full refresh")
            return None

        self.logger.info(f"Partial refresh: {strategy.value}")

        def update_progress(msg: str) -> None:
            self._status_update(status_update, f"📊 {msg}")

        try:
            if is_hot_refresh:
                fetch_start, fetch_end = self.cache_manager.get_hot_refresh_date_range()
                self._status_update(
                    status_update,
                    f"🔄 Refreshing recent transactions ({fetch_start} to {fetch_end})...",
                )
            else:
                fetch_start, fetch_end = self.cache_manager.get_cold_refresh_date_range()
                self._status_update(
                    status_update,
                    f"🔄 Refreshing historical transactions ({fetch_start} to {fetch_end})...",
                )

            fetched_df, categories, category_groups = await self.data_manager.fetch_all_data(
                start_date=fetch_start,
                end_date=fetch_end,
                progress_callback=update_progress,
            )

            hot_df = fetched_df if is_hot_refresh else cached_df
            cold_df = cached_df if is_hot_refresh else fetched_df
            merged_df = self.cache_manager.merge_tiers(hot_df, cold_df)

            self._status_update(status_update, f"💾 Saving {tier_name} cache...")
            if is_hot_refresh:
                self.cache_manager.save_hot_cache(
                    hot_df=fetched_df,
                    categories=categories,
                    category_groups=category_groups,
                )
            else:
                self.cache_manager.save_cold_cache(cold_df=fetched_df)

            self._status_update(status_update, "🔄 Applying category groupings...")
            merged_df = self.data_manager.apply_category_groups(merged_df)

            self._status_update(status_update, f"✅ Loaded {len(merged_df):,} transactions")
            if is_hot_refresh:
                self._notify(
                    f"🔄 Fetched recent ({fetch_start} to {fetch_end}) • Historical from cache"
                )
            else:
                self._notify(
                    f"🔄 Fetched historical ({fetch_start} to {fetch_end}) • Recent from cache"
                )
            return merged_df, categories, category_groups

        except Exception as exc:
            self.logger.error(f"Partial refresh failed: {exc}", exc_info=True)
            self._status_update(
                status_update, "⚠ Partial refresh failed, falling back to full fetch..."
            )
            return None
