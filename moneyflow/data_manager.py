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
from typing import Any, Callable, Dict, List, Optional, Tuple

import polars as pl

from .backends.base import FinanceBackend
from .categories import (
    build_category_to_group_mapping,
    convert_api_categories_to_groups,
    get_effective_category_groups,
    save_categories_to_config,
)
from .logging_config import get_logger

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
        self, mm: FinanceBackend, merchant_cache_dir: str = "", config_dir: Optional[str] = None
    ):
        """
        Initialize DataManager with a finance backend.

        Args:
            mm: Backend instance (must implement FinanceBackend interface)
            merchant_cache_dir: Directory for merchant cache (defaults to ~/.moneyflow/)
            config_dir: Optional config directory for config.yaml (defaults to ~/.moneyflow/)
        """
        self.mm = mm
        self.config_dir = config_dir  # Store for apply_category_groups

        # Load effective category groups (defaults + custom from YAML)
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
        # Convert cached merchants to Series
        cached_series = pl.Series("merchant", self.all_merchants)

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
            >>> dm = DataManager(backend)
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

        # Convert and save categories to config.yaml for Monarch/YNAB backends
        # This allows Amazon mode and other backends to use the same category structure
        # Skip for demo mode (uses built-in defaults)
        backend_type = (
            getattr(self.mm, "__class__", None).__name__ if hasattr(self.mm, "__class__") else None
        )
        if backend_type and backend_type not in ["DemoBackend", "AmazonBackend"]:
            try:
                simple_groups = convert_api_categories_to_groups(categories_data, groups_data)
                save_categories_to_config(simple_groups, config_dir=self.config_dir)

                # Rebuild category mapping after saving fresh categories
                # This fixes bug where stale mapping causes transfers to not be filtered
                self.category_groups_config = get_effective_category_groups(self.config_dir)
                self.category_to_group = build_category_to_group_mapping(
                    self.category_groups_config
                )
                logger.debug("Rebuilt category-to-group mapping with fresh categories")
            except Exception as e:
                logger.warning(f"Failed to save categories to config.yaml: {e}")

        # Fetch transactions in batches
        if progress_callback:
            progress_callback("Fetching transactions...")

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
                        hide_label = "hidden" if hide_value else "visible"
                        progress_callback(f"Fetching {total_count:,} {hide_label} transactions...")

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
            rows.append(row)

        # Create DataFrame
        df = pl.DataFrame(rows)

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

    def _aggregate_by_field(
        self,
        df: pl.DataFrame,
        group_field: str,
        include_id: bool = True,
        include_group: bool = False,
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

        return df.group_by(group_field).agg(agg_exprs)

    def aggregate_by_merchant(self, df: pl.DataFrame) -> pl.DataFrame:
        """
        Aggregate transactions by merchant.

        Groups all transactions by merchant name and computes:
        - count: Number of transactions
        - total: Sum of transaction amounts
        - merchant_id: ID of the merchant (for API operations)
        - top_category: Most common category for this merchant
        - top_category_pct: Percentage of transactions in top category

        Args:
            df: Transaction DataFrame to aggregate

        Returns:
            Aggregated DataFrame with columns:
            [merchant, count, total, merchant_id, top_category, top_category_pct]
            Empty DataFrame if input is empty
        """
        if df.is_empty():
            return pl.DataFrame()

        # Group by merchant and compute aggregations including top category
        result = df.group_by("merchant").agg(
            [
                pl.count("id").alias("count"),
                # Exclude hidden transactions from totals
                pl.col("amount").filter(~pl.col("hideFromReports")).sum().alias("total"),
                pl.first("merchant_id").alias("merchant_id"),
                # Get most common category and its count
                pl.col("category")
                .value_counts(sort=True)
                .first()
                .struct.field("category")
                .alias("top_category"),
                pl.col("category")
                .value_counts(sort=True)
                .first()
                .struct.field("count")
                .alias("top_category_count"),
            ]
        )

        # Calculate percentage (top_category_count / count * 100)
        result = result.with_columns(
            ((pl.col("top_category_count") / pl.col("count")) * 100)
            .round(0)
            .cast(pl.Int32)
            .alias("top_category_pct")
        )

        # Drop the intermediate count column
        result = result.drop("top_category_count")

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
        """
        return self._aggregate_by_field(df, "account", include_id=True, include_group=False)

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

    async def commit_pending_edits(self, edits: List[Any]) -> Tuple[int, int]:
        """
        Commit pending edits to backend API in parallel.

        This method groups edits by transaction ID (in case multiple edits
        affect the same transaction) and sends update requests in parallel
        for maximum speed.

        The method is resilient to partial failures - if some updates fail,
        others will still succeed. The caller receives counts for both.

        Args:
            edits: List of TransactionEdit objects to commit

        Returns:
            Tuple of (success_count, failure_count)
            - success_count: Number of successful API updates
            - failure_count: Number of failed API updates

        Example:
            >>> edits = [
            ...     TransactionEdit("txn1", "merchant", "Old", "New", ...),
            ...     TransactionEdit("txn2", "category", "cat1", "cat2", ...)
            ... ]
            >>> success, failure = await dm.commit_pending_edits(edits)
            >>> print(f"Committed {success} edits, {failure} failed")

        Note: After successful commit, caller should use CommitOrchestrator
        to apply edits to local DataFrames for instant UI update.
        """
        logger.info(f"Starting commit of {len(edits)} edits")

        if not edits:
            logger.info("No edits to commit")
            return 0, 0

        # Group edits by transaction ID
        edits_by_txn: Dict[str, Dict[str, Any]] = {}
        for edit in edits:
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

        # Count successes and failures, and log errors
        success_count = 0
        failure_count = 0

        # Check for auth errors that should trigger retry
        auth_errors = []

        for i, result in enumerate(results):
            if isinstance(result, Exception):
                failure_count += 1
                logger.error(
                    f"Transaction update {i + 1}/{len(results)} FAILED: {result}", exc_info=result
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

        return success_count, failure_count

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
