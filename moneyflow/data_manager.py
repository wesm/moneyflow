"""
Data management layer using Polars for high-performance aggregation and filtering.

This module handles all data operations for the application:
- Fetching transactions from backend API (with pagination)
- Converting API responses to Polars DataFrames
- Aggregating transactions by merchant, category, group, account
- Filtering and searching transactions
- Committing edits back to the API
- Applying category-to-group mappings

The DataManager acts as the boundary between the backend API and the UI layer,
providing a clean interface for data operations without exposing API details.
"""

import asyncio
import json
from datetime import datetime
from pathlib import Path
from typing import Any, Callable, Dict, List, Optional, Set, Tuple

import polars as pl

from .backends.base import FinanceBackend
from .categories import (
    build_category_to_group_mapping,
    convert_api_categories_to_groups,
    get_effective_category_groups,
    get_profile_category_groups,
    save_categories_to_config,
    save_categories_to_profile,
)
from .logging_config import get_logger
from .state import TimeGranularity

logger = get_logger(__name__)


class DataManager:
    """
    Manages all transaction data operations.

    This class serves as the data layer between the backend API and the UI,
    handling:

    **Data Loading**:
    - Fetch transactions from API with pagination (batches of 1000)
    - Fetch categories and category groups
    - Convert API responses to Polars DataFrames for fast operations
    - Cache merchants for fast autocomplete in MTD mode

    **Data Transformation**:
    - Apply category-to-group mappings (from categories module)
    - Aggregate transactions by merchant, category, group, account
    - Filter transactions by various criteria
    - Search transactions by text

    **Data Persistence**:
    - Commit pending edits back to API (in parallel for speed)
    - Track success/failure counts for commit operations
    - Cache merchants with daily refresh

    **Design Philosophy**:
    - All aggregations done locally with Polars (fast, no API calls)
    - Batch API updates to minimize round trips
    - Separate data operations from presentation (no formatting here)

    Attributes:
        mm: Backend API instance (MonarchBackend, DemoBackend, etc.)
        df: Main transaction DataFrame (loaded on startup)
        categories: Category lookup dict {id: {name, group_id, ...}}
        category_groups: Group lookup dict {id: {name, type, ...}}
        pending_edits: List of edits queued for commit
        category_to_group: Reverse mapping {category_name: group_name}
        all_merchants: Merged list of cached + current merchants for autocomplete
    """

    MERCHANT_CACHE_MAX_AGE_HOURS = 24  # Refresh once per day

    def __init__(
        self,
        mm: FinanceBackend,
        config_dir: str,
        merchant_cache_dir: str = "",
        profile_dir: Optional[Path] = None,
        backend_type: Optional[str] = None,
    ):
        """
        Initialize DataManager with a finance backend.

        Args:
            mm: Backend instance (must implement FinanceBackend interface)
            config_dir: Config directory for global config.yaml (required)
            merchant_cache_dir: Directory for merchant cache (defaults to config_dir if empty)
                               For multi-account mode, pass profile directory to isolate merchant
                               caches per account (e.g., ~/.moneyflow/profiles/monarch-personal/)
            profile_dir: Profile directory for profile-local config.yaml (for multi-account mode)
            backend_type: Backend type (amazon, monarch, ynab) for category inheritance logic
        """
        self.mm = mm
        self.config_dir = config_dir
        self.profile_dir = profile_dir
        self.backend_type = backend_type

        # Load category groups (profile-aware if profile_dir provided)
        if profile_dir:
            self.category_groups_config = get_profile_category_groups(
                profile_dir=profile_dir, config_dir=config_dir, backend_type=backend_type
            )
        else:
            # Legacy mode - use global config
            self.category_groups_config = get_effective_category_groups(config_dir)

        self.category_to_group = build_category_to_group_mapping(self.category_groups_config)

        # Data storage
        self.df: Optional[pl.DataFrame] = None
        self.categories: Dict[str, Any] = {}
        self.category_groups: Dict[str, Any] = {}
        self.pending_edits: List[Any] = []
        self.all_merchants: List[str] = []  # Cached + current merchants

        # Merchant cache setup
        if not merchant_cache_dir:
            # Use config_dir if available, otherwise default to ~/.moneyflow
            merchant_cache_dir = (
                self.config_dir if self.config_dir else str(Path.home() / ".moneyflow")
            )
        self.merchant_cache_dir = Path(merchant_cache_dir)
        self.merchant_cache_dir.mkdir(parents=True, exist_ok=True)
        self.merchant_cache_file = self.merchant_cache_dir / "merchants.json"

    def _is_merchant_cache_stale(self) -> bool:
        """Check if merchant cache needs refresh (older than 24 hours)."""
        if not self.merchant_cache_file.exists():
            return True

        try:
            with open(self.merchant_cache_file, "r") as f:
                data = json.load(f)

            timestamp_str = data.get("timestamp")
            if not timestamp_str:
                return True

            cache_time = datetime.fromisoformat(timestamp_str)
            age_hours = (datetime.now() - cache_time).total_seconds() / 3600

            return age_hours >= self.MERCHANT_CACHE_MAX_AGE_HOURS

        except (json.JSONDecodeError, KeyError, ValueError):
            return True

    def _load_cached_merchants(self) -> List[str]:
        """Load merchants from cache file."""
        if not self.merchant_cache_file.exists():
            return []

        try:
            with open(self.merchant_cache_file, "r") as f:
                data = json.load(f)
            return data.get("merchants", [])
        except (json.JSONDecodeError, KeyError):
            logger.warning("Corrupt merchant cache, will refresh")
            return []

    def _save_merchant_cache(self, merchants: List[str]) -> None:
        """Save merchants to cache with timestamp."""
        data = {
            "timestamp": datetime.now().isoformat(),
            "merchants": sorted(set(merchants)),
            "count": len(set(merchants)),
        }

        with open(self.merchant_cache_file, "w") as f:
            json.dump(data, f, indent=2)

        logger.info(f"Saved {data['count']} merchants to cache")

    async def refresh_merchant_cache(
        self, force: bool = False, skip_cache: bool = False
    ) -> List[str]:
        """
        Refresh merchant cache from API if stale or forced.

        Args:
            force: If True, refresh even if cache is fresh
            skip_cache: If True, don't save to cache (for demo mode)

        Returns:
            List of merchant names
        """
        if not force and not self._is_merchant_cache_stale():
            logger.debug("Merchant cache is fresh, loading from cache")
            return self._load_cached_merchants()

        logger.info("Fetching all merchants from API...")
        merchants = await self.mm.get_all_merchants()

        if not skip_cache:
            self._save_merchant_cache(merchants)
        else:
            logger.debug("Skipping merchant cache save (demo/test mode)")

        return merchants

    def get_all_merchants_for_autocomplete(self) -> List[str]:
        """
        Get merged list of cached merchants + merchants from loaded transactions.

        This ensures:
        - MTD mode has access to all historical merchants (from cache)
        - Recent merchant edits are immediately available (from current df)

        Returns:
            Sorted, deduplicated list of all merchants
        """
        # Use Polars operations for performance with large merchant lists
        # Convert cached merchants to Series (ensure str dtype even if empty)
        cached_series = pl.Series("merchant", self.all_merchants, dtype=pl.Utf8)

        # Merge with current merchants if we have loaded data
        if self.df is not None and not self.df.is_empty():
            current_series = self.df["merchant"].unique()
            # Concatenate and deduplicate using Polars
            all_merchants = pl.concat([cached_series, current_series]).unique().sort()
        else:
            all_merchants = cached_series.unique().sort()

        return all_merchants.to_list()

    async def fetch_all_data(
        self,
        start_date: Optional[str] = None,
        end_date: Optional[str] = None,
        progress_callback: Optional[Callable[[str], None]] = None,
    ) -> Tuple[pl.DataFrame, Dict, Dict]:
        """
        Fetch all transactions and metadata from backend API.

        This is the main data loading method, called on app startup. It:
        1. Fetches categories and category groups (in parallel)
        2. Fetches transactions in batches of 1000 with pagination
        3. Converts API responses to Polars DataFrame
        4. Applies category group mappings

        For large accounts (10k+ transactions), this may take 1-2 minutes.
        Progress updates are sent via the callback for UI display.

        Args:
            start_date: Optional start date filter (YYYY-MM-DD format)
            end_date: Optional end date filter (YYYY-MM-DD format)
            progress_callback: Optional callback for progress updates (e.g., "Downloaded 500/1000...")

        Returns:
            Tuple of:
            - transactions_df: Polars DataFrame with all transactions
            - categories: Dict mapping category_id to {name, group_id, ...}
            - category_groups: Dict mapping group_id to {name, type, ...}

        Example:
            >>> dm = DataManager(backend, config_dir="~/.moneyflow")
            >>> df, cats, groups = await dm.fetch_all_data(
            ...     start_date="2025-01-01",
            ...     progress_callback=lambda msg: print(msg)
            ... )
        """
        # Fetch categories and groups in parallel
        if progress_callback:
            progress_callback("Fetching categories and groups...")

        categories_task = self.mm.get_transaction_categories()
        groups_task = self.mm.get_transaction_category_groups()

        categories_data, groups_data = await asyncio.gather(categories_task, groups_task)

        # Parse categories
        categories = {}
        for cat in categories_data.get("categories", []):
            group_data = cat.get("group") or {}
            categories[cat["id"]] = {
                "name": cat["name"],
                "group_id": group_data.get("id") if group_data else None,
                "group_type": group_data.get("type") if group_data else None,
            }

        # Parse category groups
        category_groups = {}
        for group in groups_data.get("categoryGroups", []):
            category_groups[group["id"]] = {
                "name": group["name"],
                "type": group["type"],
            }

        # Convert and save categories to profile config for Monarch/YNAB backends
        # Skip for demo mode (uses built-in defaults) and Amazon (local-only)
        backend_class = (
            getattr(self.mm, "__class__", None).__name__ if hasattr(self.mm, "__class__") else None
        )
        if backend_class and backend_class not in ["DemoBackend", "AmazonBackend"]:
            try:
                simple_groups = convert_api_categories_to_groups(categories_data, groups_data)

                # Save to profile-local config if available, otherwise legacy global config
                if self.profile_dir:
                    save_categories_to_profile(simple_groups, profile_dir=self.profile_dir)
                else:
                    # Legacy: save to global config
                    save_categories_to_config(simple_groups, config_dir=self.config_dir)

                # Rebuild category mapping after saving fresh categories
                # This fixes bug where stale mapping causes transfers to not be filtered
                if self.profile_dir:
                    self.category_groups_config = get_profile_category_groups(
                        profile_dir=self.profile_dir,
                        config_dir=self.config_dir,
                        backend_type=self.backend_type,
                    )
                else:
                    self.category_groups_config = get_effective_category_groups(self.config_dir)

                self.category_to_group = build_category_to_group_mapping(
                    self.category_groups_config
                )
                logger.debug("Rebuilt category-to-group mapping with fresh categories")
            except Exception as e:
                logger.warning(f"Failed to save categories to config.yaml: {e}")

        # Fetch transactions in batches
        if progress_callback:
            if start_date and end_date:
                progress_callback(f"Fetching transactions ({start_date} to {end_date})...")
            elif start_date:
                progress_callback(f"Fetching transactions from {start_date}...")
            else:
                progress_callback("Fetching all transactions...")

        transactions = await self._fetch_all_transactions(
            start_date=start_date, end_date=end_date, progress_callback=progress_callback
        )

        # Convert to Polars DataFrame
        if progress_callback:
            progress_callback("Processing transactions...")

        df = self._transactions_to_dataframe(transactions, categories)

        # Apply category grouping (done dynamically so CATEGORY_GROUPS changes take effect)
        df = self.apply_category_groups(df)

        # Load/refresh merchant cache for autocomplete
        # Do this in background - don't block on merchant fetch
        if progress_callback:
            progress_callback("Refreshing merchant cache...")

        try:
            cached_merchants = await self.refresh_merchant_cache(force=False)
            self.all_merchants = cached_merchants
            logger.info(f"Loaded {len(cached_merchants)} cached merchants")
        except Exception as e:
            logger.warning(f"Merchant cache refresh failed: {e}")
            # Not critical - fall back to merchants from loaded transactions
            self.all_merchants = []

        return df, categories, category_groups

    async def _fetch_all_transactions(
        self,
        start_date: Optional[str] = None,
        end_date: Optional[str] = None,
        progress_callback: Optional[Callable[[str], None]] = None,
    ) -> List[Dict]:
        """
        Fetch all transactions from API in batches, including hidden transactions.

        Monarch Money's API behavior: When hideFromReports filter is not specified,
        the API excludes hidden transactions by default. To ensure we get ALL
        transactions including hidden ones, we make two separate API calls:
        1. hideFromReports=False → Get all non-hidden transactions
        2. hideFromReports=True → Get all hidden transactions

        These filters are mutually exclusive, so there's no overlap. The results
        are combined into a single list containing all transactions.
        """
        all_transactions = []
        non_hidden_count = 0
        hidden_count = 0

        # Fetch both hidden and non-hidden transactions
        for hide_value in [False, True]:
            batch_size = 1000
            offset = 0
            batch_num = 1
            total_count = None
            batch_transactions = []

            while True:
                batch = await self.mm.get_transactions(
                    start_date=start_date,
                    end_date=end_date,
                    limit=batch_size,
                    offset=offset,
                    hidden_from_reports=hide_value,
                )

                # Get total count on first batch
                if total_count is None and "allTransactions" in batch:
                    total_count = batch["allTransactions"].get("totalCount", 0)
                    if progress_callback and total_count:
                        hide_label = "hidden" if hide_value else ""
                        date_range = ""
                        if start_date and end_date:
                            date_range = f" ({start_date} to {end_date})"
                        elif start_date:
                            date_range = f" (from {start_date})"
                        label = f"{hide_label} " if hide_label else ""
                        progress_callback(
                            f"Downloading {total_count:,} {label}transactions{date_range}..."
                        )

                # Get results from batch
                batch_results = []
                if "allTransactions" in batch:
                    batch_results = batch["allTransactions"].get("results", [])
                elif "results" in batch:
                    batch_results = batch["results"]

                if not batch_results:
                    break

                batch_transactions.extend(batch_results)

                # Show incremental progress after each batch
                if progress_callback and total_count:
                    downloaded = len(batch_transactions)
                    hide_label = "hidden" if hide_value else "visible"
                    progress_callback(
                        f"Downloaded {downloaded:,}/{total_count:,} {hide_label} transactions..."
                    )

                offset += batch_size
                batch_num += 1

            # Track counts for final summary
            if hide_value:
                hidden_count = len(batch_transactions)
            else:
                non_hidden_count = len(batch_transactions)

            all_transactions.extend(batch_transactions)

        # Show clear final summary
        if progress_callback:
            progress_callback(
                f"✓ Downloaded {len(all_transactions):,} total transactions "
                f"({non_hidden_count:,} visible, {hidden_count:,} hidden)"
            )

        return all_transactions

    def _transactions_to_dataframe(
        self, transactions: List[Dict], categories: Dict
    ) -> pl.DataFrame:
        """
        Convert raw transaction data to Polars DataFrame with enriched fields.

        Note: Does NOT include 'group' field - groups are applied dynamically
        via apply_category_groups() so changes to config.yaml take effect
        on cached data.
        """
        if not transactions:
            return pl.DataFrame()

        # Prepare data for DataFrame
        rows = []
        for txn in transactions:
            merchant_obj = txn.get("merchant", {}) or {}
            category_obj = txn.get("category", {}) or {}
            account_obj = txn.get("account", {}) or {}

            category_id = category_obj.get("id", "")
            category_name = category_obj.get("name", "Uncategorized")

            row = {
                "id": str(txn.get("id", "")),
                "date": str(txn.get("date", "")),
                "amount": float(txn.get("amount", 0)),
                "merchant": str(
                    merchant_obj.get("name", "") if merchant_obj.get("name") else "Unknown"
                ),
                "merchant_id": str(merchant_obj.get("id", "")),
                "category": str(category_name if category_name else "Uncategorized"),
                "category_id": str(category_id),
                # Note: 'group' field NOT included here - added dynamically
                "account": str(
                    account_obj.get("displayName", "") if account_obj.get("displayName") else ""
                ),
                "account_id": str(account_obj.get("id", "")),
                "notes": str(txn.get("notes", "") if txn.get("notes") else ""),
                "hideFromReports": bool(txn.get("hideFromReports", False)),
                "pending": bool(txn.get("pending", False)),
                "isRecurring": bool(txn.get("isRecurring", False)),
            }

            # Preserve extra backend-specific fields (e.g., Amazon: quantity, asin, order_id, etc.)
            # Skip fields that were already processed (standard fields or nested objects)
            # IMPORTANT: Convert all extra fields to strings for schema consistency
            # Polars requires all rows to have the same schema, and backend data can be inconsistent
            standard_fields = {
                "id",
                "date",
                "amount",
                "merchant",
                "category",
                "account",
                "notes",
                "hideFromReports",
                "pending",
                "isRecurring",
            }
            for key, value in txn.items():
                if key not in standard_fields and not isinstance(value, dict):
                    # Convert to string to ensure schema consistency across all transactions
                    # This prevents Polars errors when backends have inconsistent field types
                    if value is None:
                        row[key] = None
                    else:
                        row[key] = str(value)

            rows.append(row)

        # Create DataFrame
        # Use infer_schema_length=None to scan ALL rows for schema inference
        # This prevents errors when extra backend fields have inconsistent presence/types
        df = pl.DataFrame(rows, infer_schema_length=None)

        # Convert date column to date type
        df = df.with_columns(pl.col("date").str.strptime(pl.Date, format="%Y-%m-%d"))

        return df

    def apply_category_groups(self, df: pl.DataFrame) -> pl.DataFrame:
        """
        Apply category-to-group mapping to a DataFrame.

        This adds/updates the 'group' column based on category groups from
        config.yaml (or built-in defaults if config.yaml not present).
        Called after loading data (from API or cache) so that changes to
        config.yaml always take effect.

        Args:
            df: DataFrame with 'category' column

        Returns:
            DataFrame with 'group' column added/updated
        """
        if df.is_empty():
            return df

        # Create a mapping expression for Polars
        # For each category, map to its group (or "Uncategorized" if not mapped)
        def get_group(category: str) -> str:
            return self.category_to_group.get(category, "Uncategorized")

        # Apply mapping - use Polars map_elements for efficient lookup
        df = df.with_columns(
            pl.col("category").map_elements(get_group, return_dtype=pl.String).alias("group")
        )

        return df

    def _apply_computed_columns(
        self, df: pl.DataFrame, computed_columns: List[Any], agg_exprs: List[Any]
    ) -> None:
        """
        Apply computed column aggregations to aggregation expressions list.

        This is a helper to avoid duplicating the aggregation function mapping logic.

        Args:
            df: DataFrame being aggregated (to check column existence)
            computed_columns: List of ComputedColumn configurations
            agg_exprs: List of aggregation expressions to append to (modified in place)
        """
        from .backends.base import AggregationFunc

        # Map aggregation functions to Polars methods
        agg_func_map = {
            AggregationFunc.FIRST: lambda expr: expr.first(),
            AggregationFunc.LAST: lambda expr: expr.last(),
            AggregationFunc.MIN: lambda expr: expr.min(),
            AggregationFunc.MAX: lambda expr: expr.max(),
            AggregationFunc.COUNT_DISTINCT: lambda expr: expr.n_unique(),
            AggregationFunc.SUM: lambda expr: expr.sum(),
            AggregationFunc.MEAN: lambda expr: expr.mean(),
        }

        for col_config in computed_columns:
            # Check if source field exists in DataFrame
            if col_config.source_field not in df.columns:
                continue

            # Apply aggregation function
            col_expr = pl.col(col_config.source_field)
            agg_func = agg_func_map.get(col_config.aggregation)
            if agg_func:
                agg_exprs.append(agg_func(col_expr).alias(col_config.name))

    def _aggregate_by_field(
        self,
        df: pl.DataFrame,
        group_field: str,
        include_id: bool = True,
        include_group: bool = False,
        computed_columns: Optional[List[Any]] = None,
    ) -> pl.DataFrame:
        """
        Generic aggregation method to eliminate duplication.

        This is the shared implementation for all aggregate_by_* methods.
        It groups by the specified field and computes count and total.

        Args:
            df: DataFrame to aggregate
            group_field: Field name to group by ('merchant', 'category', 'group', 'account')
            include_id: Whether to include the field's _id column (e.g., merchant_id)
            include_group: Whether to include group column (for category aggregation)
            computed_columns: Optional list of ComputedColumn configurations to add

        Returns:
            Aggregated DataFrame with columns: [group_field, count, total, ...]
            Additional columns based on include_id and include_group flags

        Example:
            >>> # Aggregate by merchant with merchant_id
            >>> agg = dm._aggregate_by_field(df, "merchant", include_id=True)
            >>> agg.columns
            ['merchant', 'count', 'total', 'merchant_id']
        """
        if df.is_empty():
            return pl.DataFrame()

        agg_exprs = [
            pl.count("id").alias("count"),
            # Exclude hidden transactions from totals
            pl.col("amount").filter(~pl.col("hideFromReports")).sum().alias("total"),
        ]

        if include_id:
            id_field = f"{group_field}_id"
            agg_exprs.append(pl.first(id_field).alias(id_field))

        if include_group:
            agg_exprs.append(pl.first("group").alias("group"))

        # Add computed columns if provided
        if computed_columns:
            self._apply_computed_columns(df, computed_columns, agg_exprs)

        return df.group_by(group_field).agg(agg_exprs)

    def aggregate_by_merchant(self, df: pl.DataFrame) -> pl.DataFrame:
        """
        Aggregate transactions by merchant.

        Groups all transactions by merchant name and computes:
        - count: Number of transactions
        - total: Sum of transaction amounts (excluding hidden)
        - merchant_id: ID of the merchant (for API operations)
        - top_category: Category with highest activity (excluding hidden)
        - top_category_pct: Percentage of activity in top category (excluding hidden)
        - Plus any backend-specific computed columns

        For top_category calculation:
        - Hidden transactions are excluded
        - Uses absolute values to measure total activity per category
        - Captures both spending and income activity in each category

        Args:
            df: Transaction DataFrame to aggregate

        Returns:
            Aggregated DataFrame with columns:
            [merchant, count, total, merchant_id, top_category, top_category_pct]
            Empty DataFrame if input is empty
        """
        if df.is_empty():
            return pl.DataFrame()

        # Get computed columns from backend (if any apply to merchant view)
        computed_cols = []
        if hasattr(self.mm, "get_computed_columns"):
            all_computed = self.mm.get_computed_columns()
            computed_cols = [
                col for col in all_computed if not col.view_modes or "merchant" in col.view_modes
            ]

        # Build aggregation expressions (without top_category - computed separately)
        agg_exprs = [
            pl.count("id").alias("count"),
            # Exclude hidden transactions from totals
            pl.col("amount").filter(~pl.col("hideFromReports")).sum().alias("total"),
            pl.first("merchant_id").alias("merchant_id"),
        ]

        # Add computed columns
        if computed_cols:
            self._apply_computed_columns(df, computed_cols, agg_exprs)

        # Group by merchant and compute basic aggregations
        result = df.group_by("merchant").agg(agg_exprs)

        # Compute top category based on absolute transaction amounts (excluding hidden)
        # Using absolute values captures total activity regardless of spending vs income
        non_hidden = df.filter(~pl.col("hideFromReports"))

        if not non_hidden.is_empty():
            # Sum absolute amounts per category per merchant
            cat_activity = non_hidden.group_by(["merchant", "category"]).agg(
                pl.col("amount").abs().sum().alias("cat_activity")
            )

            # Find top category per merchant (highest absolute activity)
            top_cats = (
                cat_activity.sort("cat_activity", descending=True)
                .group_by("merchant")
                .agg(
                    [
                        pl.first("category").alias("top_category"),
                        pl.first("cat_activity").alias("top_cat_activity"),
                        pl.col("cat_activity").sum().alias("total_activity"),
                    ]
                )
            )

            # Calculate percentage of total activity
            top_cats = top_cats.with_columns(
                ((pl.col("top_cat_activity") / pl.col("total_activity")) * 100)
                .round(0)
                .fill_nan(0)
                .cast(pl.Int32)
                .alias("top_category_pct")
            ).select(["merchant", "top_category", "top_category_pct"])

            # Join back to result
            result = result.join(top_cats, on="merchant", how="left")
        else:
            # All transactions are hidden - no top category data
            result = result.with_columns(
                [
                    pl.lit(None).cast(pl.Utf8).alias("top_category"),
                    pl.lit(None).cast(pl.Int32).alias("top_category_pct"),
                ]
            )

        return result

    def aggregate_by_category(self, df: pl.DataFrame) -> pl.DataFrame:
        """
        Aggregate transactions by category.

        Returns:
            Aggregated DataFrame with columns: [category, count, total, category_id, group]
        """
        return self._aggregate_by_field(df, "category", include_id=True, include_group=True)

    def aggregate_by_group(self, df: pl.DataFrame) -> pl.DataFrame:
        """
        Aggregate transactions by category group.

        Returns:
            Aggregated DataFrame with columns: [group, count, total]
        """
        return self._aggregate_by_field(df, "group", include_id=False, include_group=False)

    def aggregate_by_account(self, df: pl.DataFrame) -> pl.DataFrame:
        """
        Aggregate transactions by account.

        Returns:
            Aggregated DataFrame with columns: [account, count, total, account_id]
            Plus any backend-specific computed columns (e.g., order_date for Amazon)
        """
        # Get computed columns from backend (e.g., order_date for Amazon)
        computed_cols = []
        if hasattr(self.mm, "get_computed_columns"):
            all_computed = self.mm.get_computed_columns()
            # Filter to columns that apply to ACCOUNT view mode
            computed_cols = [
                col for col in all_computed if not col.view_modes or "account" in col.view_modes
            ]

        return self._aggregate_by_field(
            df, "account", include_id=True, include_group=False, computed_columns=computed_cols
        )

    def _generate_all_months(self, min_year: int, max_year: int) -> pl.DataFrame:
        """
        Generate DataFrame with all months between min and max year (inclusive).

        Args:
            min_year: Starting year
            max_year: Ending year

        Returns:
            DataFrame with columns: [year, month, time_period_display]
            where time_period_display is formatted as "YYYY-MM" for sorting
        """
        periods = []

        for year in range(min_year, max_year + 1):
            for month in range(1, 13):
                periods.append(
                    {
                        "year": year,
                        "month": month,
                        "time_period_display": f"{year}-{month:02d}",
                    }
                )

        return pl.DataFrame(periods)

    def _fill_time_gaps(self, df: pl.DataFrame, granularity: TimeGranularity) -> pl.DataFrame:
        """
        Fill gaps in time series with zero-value rows.

        Ensures continuous time series between earliest and latest period,
        filling missing periods with count=0 and total=0.

        Args:
            df: Aggregated time DataFrame with year, month, day, count, total columns
            granularity: TIME granularity (YEAR, MONTH, or DAY)

        Returns:
            DataFrame with all periods filled, sorted chronologically
        """
        if df.is_empty():
            return df

        if granularity == TimeGranularity.YEAR:
            # Determine range of years in the data
            min_year = df["year"].min()
            max_year = df["year"].max()

            # Type assertions: df is not empty so min/max are not None
            assert min_year is not None
            assert max_year is not None
            # Ensure we have ints for range() function
            min_year_int = int(min_year)
            max_year_int = int(max_year)

            # Create all years in range
            all_periods = pl.DataFrame(
                {
                    "year": list(range(min_year_int, max_year_int + 1)),
                    "time_period_display": [str(y) for y in range(min_year_int, max_year_int + 1)],
                }
            )
            join_cols = ["year", "time_period_display"]
        elif granularity == TimeGranularity.MONTH:
            # Find actual min and max months (not just years)
            # Sort by time_period_display to get actual earliest/latest
            sorted_df = df.sort("time_period_display")
            first_row = sorted_df.head(1)
            last_row = sorted_df.tail(1)

            min_year = first_row["year"][0]
            min_month = first_row["month"][0]
            max_year = last_row["year"][0]
            max_month = last_row["month"][0]

            # Generate all months between min and max
            periods = []
            current_year = min_year
            current_month = min_month

            while (current_year < max_year) or (
                current_year == max_year and current_month <= max_month
            ):
                periods.append(
                    {
                        "year": current_year,
                        "month": current_month,
                        "time_period_display": f"{current_year}-{current_month:02d}",
                    }
                )

                # Advance to next month
                current_month += 1
                if current_month > 12:
                    current_month = 1
                    current_year += 1

            all_periods = pl.DataFrame(periods)
            join_cols = ["year", "month", "time_period_display"]
        else:  # DAY
            # Find actual min and max days
            from datetime import date, timedelta

            sorted_df = df.sort("time_period_display")
            first_row = sorted_df.head(1)
            last_row = sorted_df.tail(1)

            min_year = first_row["year"][0]
            min_month = first_row["month"][0]
            min_day = first_row["day"][0]
            max_year = last_row["year"][0]
            max_month = last_row["month"][0]
            max_day = last_row["day"][0]

            # Generate all days between min and max
            periods = []
            current_date = date(min_year, min_month, min_day)
            end_date = date(max_year, max_month, max_day)

            while current_date <= end_date:
                periods.append(
                    {
                        "year": current_date.year,
                        "month": current_date.month,
                        "day": current_date.day,
                        "time_period_display": f"{current_date.year}-{current_date.month:02d}-{current_date.day:02d}",
                    }
                )
                current_date += timedelta(days=1)

            all_periods = pl.DataFrame(periods)
            join_cols = ["year", "month", "day", "time_period_display"]

        # Left join to preserve all periods, filling missing with 0
        result = (
            all_periods.join(df, on=join_cols, how="left")
            .with_columns(
                [
                    pl.col("count").fill_null(0),
                    pl.col("total").fill_null(0.0),
                ]
            )
            .sort("time_period_display")
        )

        return result

    def aggregate_by_time(self, df: pl.DataFrame, granularity: TimeGranularity) -> pl.DataFrame:
        """
        Aggregate transactions by time period (year or month).

        Groups transactions by time period with gap filling between
        earliest and latest period.

        Args:
            df: Transaction DataFrame to aggregate
            granularity: TIME granularity (YEAR, MONTH, or DAY)

        Returns:
            Aggregated DataFrame with columns:
            - time_period_display: "2024", "2024-03", or "2024-03-15" (for sorting)
            - year: int
            - month: int (for MONTH and DAY granularity)
            - day: int (only for DAY granularity)
            - count: int (number of transactions)
            - total: float (sum of amounts, excluding hidden)

            Sorted chronologically by time_period_display.
            Includes zero-value rows for gaps between min and max period.

        Example:
            >>> # Aggregate by year
            >>> agg = dm.aggregate_by_time(df, TimeGranularity.YEAR)
            >>> agg.columns
            ['time_period_display', 'year', 'count', 'total']
        """
        if df.is_empty():
            return pl.DataFrame()

        # Add year, month, and day columns extracted from date
        df = df.with_columns(
            [
                pl.col("date").dt.year().alias("year"),
                pl.col("date").dt.month().alias("month"),
                pl.col("date").dt.day().alias("day"),
            ]
        )

        if granularity == TimeGranularity.YEAR:
            # Group by year
            df = df.with_columns([pl.col("year").cast(pl.Utf8).alias("time_period_display")])

            aggregated = df.group_by(["year", "time_period_display"]).agg(
                [
                    pl.count("id").alias("count"),
                    # Exclude hidden transactions from totals
                    pl.col("amount").filter(~pl.col("hideFromReports")).sum().alias("total"),
                ]
            )
        elif granularity == TimeGranularity.MONTH:
            # Group by year and month
            df = df.with_columns(
                [
                    (
                        pl.col("year").cast(pl.Utf8)
                        + "-"
                        + pl.col("month").cast(pl.Utf8).str.zfill(2)
                    ).alias("time_period_display")
                ]
            )

            aggregated = df.group_by(["year", "month", "time_period_display"]).agg(
                [
                    pl.count("id").alias("count"),
                    # Exclude hidden transactions from totals
                    pl.col("amount").filter(~pl.col("hideFromReports")).sum().alias("total"),
                ]
            )
        else:  # DAY
            # Group by year, month, and day
            df = df.with_columns(
                [
                    (
                        pl.col("year").cast(pl.Utf8)
                        + "-"
                        + pl.col("month").cast(pl.Utf8).str.zfill(2)
                        + "-"
                        + pl.col("day").cast(pl.Utf8).str.zfill(2)
                    ).alias("time_period_display")
                ]
            )

            aggregated = df.group_by(["year", "month", "day", "time_period_display"]).agg(
                [
                    pl.count("id").alias("count"),
                    # Exclude hidden transactions from totals
                    pl.col("amount").filter(~pl.col("hideFromReports")).sum().alias("total"),
                ]
            )

        # Fill gaps between earliest and latest period
        result = self._fill_time_gaps(aggregated, granularity)

        return result

    def filter_by_merchant(self, df: pl.DataFrame, merchant: str) -> pl.DataFrame:
        """Filter transactions by merchant name."""
        return df.filter(pl.col("merchant") == merchant)

    def filter_by_category(self, df: pl.DataFrame, category: str) -> pl.DataFrame:
        """Filter transactions by category name."""
        return df.filter(pl.col("category") == category)

    def filter_by_group(self, df: pl.DataFrame, group: str) -> pl.DataFrame:
        """Filter transactions by group name."""
        return df.filter(pl.col("group") == group)

    def filter_by_account(self, df: pl.DataFrame, account: str) -> pl.DataFrame:
        """Filter transactions by account name."""
        return df.filter(pl.col("account") == account)

    def search_transactions(self, df: pl.DataFrame, query: str) -> pl.DataFrame:
        """Search transactions by merchant, category, or notes."""
        if not query:
            return df

        query_lower = query.lower()
        return df.filter(
            pl.col("merchant").str.to_lowercase().str.contains(query_lower)
            | pl.col("category").str.to_lowercase().str.contains(query_lower)
            | pl.col("notes").str.to_lowercase().str.contains(query_lower)
        )

    async def check_batch_scope(self, edits: List[Any]) -> Dict[Tuple[str, str], Dict[str, int]]:
        """
        Check if any merchant renames would affect more transactions than selected.

        This is used to prompt the user before committing, so they can choose
        between batch rename (affects all transactions) or individual updates
        (affects only selected transactions).

        Only applicable for backends that support batch_update_merchant (e.g., YNAB).

        Args:
            edits: List of TransactionEdit objects to check

        Returns:
            Dict mapping (old_merchant, new_merchant) to counts:
            {"selected": count_in_queue, "total": count_on_backend}

            Empty dict if backend doesn't support this check or no mismatches found.

        Example:
            >>> mismatches = await dm.check_batch_scope(edits)
            >>> for (old, new), counts in mismatches.items():
            ...     print(f"'{old}' -> '{new}': {counts['selected']} selected, "
            ...           f"{counts['total']} total on backend")
        """
        # Only supported for backends with get_transaction_count_by_merchant
        if not hasattr(self.mm, "get_transaction_count_by_merchant"):
            return {}

        # Filter to merchant edits only
        merchant_edits = [e for e in edits if e.field == "merchant"]
        if not merchant_edits:
            return {}

        # Group by (old_value, new_value)
        groups: Dict[Tuple[str, str], List[Any]] = {}
        for edit in merchant_edits:
            key = (edit.old_value, edit.new_value)
            groups.setdefault(key, []).append(edit)

        result: Dict[Tuple[str, str], Dict[str, int]] = {}
        for (old_name, new_name), group_edits in groups.items():
            # Call backend to get total count
            total = await asyncio.to_thread(self.mm.get_transaction_count_by_merchant, old_name)

            # Only report if backend returned a count and it's more than selected
            if total is not None and total > len(group_edits):
                result[(old_name, new_name)] = {
                    "selected": len(group_edits),
                    "total": total,
                }

        return result

    async def commit_pending_edits(
        self, edits: List[Any], skip_batch_for: Optional[Set[Tuple[str, str]]] = None
    ) -> Tuple[int, int, Set[Tuple[str, str]]]:
        """
        Commit pending edits to backend API in parallel.

        This method intelligently optimizes commits based on backend capabilities:
        - For backends with batch_update_merchant (e.g., YNAB), bulk merchant
          renames are handled with a single API call per (old, new) pair instead
          of one call per transaction (100x performance improvement)
        - For other backends, or non-merchant edits, uses individual transaction
          updates in parallel for maximum speed

        The method is resilient to partial failures - if some updates fail,
        others will still succeed. The caller receives counts for both.

        Args:
            edits: List of TransactionEdit objects to commit
            skip_batch_for: Optional set of (old_merchant, new_merchant) tuples
                to process individually instead of using batch update. Used when
                user chooses "rename selected only" for renames that would affect
                more transactions than selected.

        Returns:
            Tuple of (success_count, failure_count, bulk_merchant_renames)
            - success_count: Number of successful API updates
            - failure_count: Number of failed API updates
            - bulk_merchant_renames: Set of (old_merchant, new_merchant) tuples
              that were batch-updated. Pass this to CommitOrchestrator to
              ensure all matching transactions are updated locally.

        Example:
            >>> edits = [
            ...     TransactionEdit("txn1", "merchant", "Old", "New", ...),
            ...     TransactionEdit("txn2", "category", "cat1", "cat2", ...)
            ... ]
            >>> success, failure, bulk_renames = await dm.commit_pending_edits(edits)
            >>> print(f"Committed {success} edits, {failure} failed")

        Note: After successful commit, caller should use CommitOrchestrator
        to apply edits to local DataFrames for instant UI update.
        """
        logger.info(f"Starting commit of {len(edits)} edits")

        if not edits:
            logger.info("No edits to commit")
            return 0, 0, set()

        success_count = 0
        failure_count = 0
        auth_errors = []
        bulk_merchant_renames: Set[Tuple[str, str]] = set()

        # Check if backend supports batch merchant updates
        has_batch_update = hasattr(self.mm, "batch_update_merchant")

        # Separate merchant edits from other edits
        merchant_edits = [e for e in edits if e.field == "merchant"]
        other_edits = [e for e in edits if e.field != "merchant"]

        # OPTIMIZATION: Group merchant edits by (old_value, new_value) for batch updates
        if has_batch_update and merchant_edits:
            logger.info(
                f"Backend supports batch updates - optimizing {len(merchant_edits)} merchant edits"
            )

            # Group merchant edits by (old_name, new_name)
            merchant_groups: Dict[Tuple[str, str], List[Any]] = {}
            for edit in merchant_edits:
                key = (edit.old_value, edit.new_value)
                if key not in merchant_groups:
                    merchant_groups[key] = []
                merchant_groups[key].append(edit)

            # Try batch update for each (old, new) pair
            successfully_batched_edits = []
            failed_batch_edits = []

            # Track processed transaction IDs to prevent double-counting
            # Note: If the same transaction has multiple merchant edits (e.g., A→B then B→C),
            # they'll be in different batch groups. We can only batch one of them.
            processed_txn_ids = set()

            # Initialize skip_batch_for if not provided
            skip_batch_set = skip_batch_for or set()

            for (old_name, new_name), group_edits in merchant_groups.items():
                # Check if user chose to skip batch for this rename
                if (old_name, new_name) in skip_batch_set:
                    logger.info(
                        f"User chose individual updates for '{old_name}' -> '{new_name}' "
                        f"({len(group_edits)} transactions)"
                    )
                    failed_batch_edits.extend(group_edits)
                    continue

                # Filter out edits for transactions already processed in a different batch
                unprocessed_edits = [
                    e for e in group_edits if e.transaction_id not in processed_txn_ids
                ]

                if not unprocessed_edits:
                    # All edits in this group were already processed in a previous batch
                    # Add them to failed list so they get individual processing with latest values
                    failed_batch_edits.extend(group_edits)
                    continue

                group_edits = unprocessed_edits  # Only batch the unprocessed ones
                group_txn_ids = {e.transaction_id for e in group_edits}
                logger.info(
                    f"Attempting batch update: '{old_name}' -> '{new_name}' "
                    f"({len(group_edits)} transactions)"
                )

                try:
                    # Call batch_update_merchant in thread to avoid blocking event loop
                    result = await asyncio.to_thread(
                        self.mm.batch_update_merchant,  # type: ignore[attr-defined]
                        old_name,
                        new_name,
                    )

                    if result.get("success"):
                        # Batch update succeeded - mark edits as processed and count as successful
                        processed_txn_ids.update(group_txn_ids)
                        success_count += len(group_edits)
                        successfully_batched_edits.extend(group_edits)
                        # Track this as a bulk rename for local cache update
                        bulk_merchant_renames.add((old_name, new_name))
                        logger.info(
                            f"✓ Batch update succeeded for '{old_name}' -> '{new_name}' "
                            f"({len(group_edits)} transactions updated via 1 API call)"
                        )
                    else:
                        # Batch update failed - mark as processed but add to fallback list
                        processed_txn_ids.update(group_txn_ids)
                        logger.warning(
                            f"Batch update failed for '{old_name}' -> '{new_name}': "
                            f"{result.get('message', 'Unknown error')}. "
                            f"Falling back to individual transaction updates."
                        )
                        failed_batch_edits.extend(group_edits)

                except Exception as e:
                    # Exception during batch - mark as processed and add to fallback list
                    processed_txn_ids.update(group_txn_ids)
                    logger.warning(
                        f"Batch update exception for '{old_name}' -> '{new_name}': {e}. "
                        f"Falling back to individual transaction updates.",
                        exc_info=True,
                    )
                    failed_batch_edits.extend(group_edits)

            # Add failed batch edits back to the list for individual processing
            merchant_edits = failed_batch_edits

            # Safety check: ensure no overlap between successful and failed batches
            successful_ids = {e.transaction_id for e in successfully_batched_edits}
            failed_ids = {e.transaction_id for e in failed_batch_edits}
            overlap = successful_ids & failed_ids
            assert not overlap, (
                f"Found {len(overlap)} edits in both successful and failed batches - "
                "this indicates a race condition or logic error"
            )

        # Process remaining edits (non-merchant + failed batch updates) individually
        edits_to_process = merchant_edits + other_edits

        if edits_to_process:
            logger.info(
                f"Processing {len(edits_to_process)} edits individually "
                f"({len(merchant_edits)} merchant, {len(other_edits)} other)"
            )

            # Group edits by transaction ID
            edits_by_txn: Dict[str, Dict[str, Any]] = {}
            for edit in edits_to_process:
                txn_id = edit.transaction_id
                if txn_id not in edits_by_txn:
                    edits_by_txn[txn_id] = {}

                if edit.field == "merchant":
                    edits_by_txn[txn_id]["merchant_name"] = edit.new_value
                elif edit.field == "category":
                    edits_by_txn[txn_id]["category_id"] = edit.new_value
                elif edit.field == "hide_from_reports":
                    edits_by_txn[txn_id]["hide_from_reports"] = edit.new_value

            # Create update tasks
            tasks = []
            for txn_id, updates in edits_by_txn.items():
                tasks.append(self.mm.update_transaction(transaction_id=txn_id, **updates))

            # Execute in parallel
            results = await asyncio.gather(*tasks, return_exceptions=True)

            # Count successes and failures
            for i, result in enumerate(results):
                if isinstance(result, Exception):
                    failure_count += 1
                    logger.error(
                        f"Transaction update {i + 1}/{len(results)} FAILED: {result}",
                        exc_info=result,
                    )

                    # Check if it's a 401/auth error
                    error_str = str(result).lower()
                    if "401" in error_str or "unauthorized" in error_str:
                        auth_errors.append(result)
                else:
                    success_count += 1

        logger.info(f"Commit completed: {success_count} succeeded, {failure_count} failed")

        # If ALL failures were auth errors, raise one so retry logic can kick in
        if failure_count > 0 and len(auth_errors) == failure_count:
            logger.warning("All failures were auth errors - raising to trigger retry")
            raise auth_errors[0]  # Raise first auth error to trigger retry

        return success_count, failure_count, bulk_merchant_renames

    def get_stats(self) -> Dict[str, Any]:
        """Get statistics about current data."""
        if self.df is None or self.df.is_empty():
            return {
                "total_transactions": 0,
                "total_income": 0.0,
                "total_expenses": 0.0,
                "net_savings": 0.0,
                "pending_changes": len(self.pending_edits),
            }

        # Calculate income (from Income group, excluding Transfers)
        income_df = self.df.filter(pl.col("group") == "Income")
        total_income = float(income_df["amount"].sum()) if not income_df.is_empty() else 0.0

        # Calculate expenses (all non-Income, non-Transfer transactions)
        # Expenses are negative, so this sum will be negative
        expense_df = self.df.filter(
            (pl.col("group") != "Income") & (pl.col("group") != "Transfers")
        )
        total_expenses = float(expense_df["amount"].sum()) if not expense_df.is_empty() else 0.0

        # Net savings = Income + Expenses (expenses are negative)
        net_savings = total_income + total_expenses

        return {
            "total_transactions": len(self.df),
            "total_income": total_income,
            "total_expenses": total_expenses,
            "net_savings": net_savings,
            "pending_changes": len(self.pending_edits),
        }
