# Architecture

moneyflow's data pipeline uses a three-layer architecture: a **TUI layer**, an
**orchestration layer**, and a **data layer**. The orchestration layer decides
whether to serve data from a local two-tier cache or fetch from the backend API.

## Layer Overview

```text
TUI (Textual app)
  |
  |-- CacheOrchestrator -- coordinates cache vs. API decision
  |     |
  |     |-- CacheManager -- reads/writes encrypted Parquet + JSON on disk
  |     |-- DataManager  -- wraps the backend, converts API responses to DataFrames
  |
  |-- Backend (FinanceBackend ABC)
        |
        |-- Monarch Money API
        |-- YNAB API
        |-- Amazon Orders scraper
        |-- Demo data
```

## Startup Data Flow

When the TUI mounts, `on_mount` fires a worker running `initialize_data`.
The flow has five stages:

### 1. Bootstrap

- Load credentials from the selected profile
- Call `backend.login()` with retry (up to 3 attempts on failure)
- Initialize `DataManager`, `CacheManager`, and `CacheOrchestrator`
- Determine date range from CLI flags (`--year`, `--since`, `--mtd`, `--all`)

### 2. Cache Check

`CacheOrchestrator.check_and_load_cache()` calls `CacheManager.get_refresh_strategy()`
to inspect the on-disk cache. The strategy is determined by:

- **First launch** — no cache files exist -> `ALL`
- **Both tiers fresh** (hot < 6h, cold < 30d) -> `NONE`
- **Hot stale, cold fresh** -> `HOT_ONLY`
- **Cold stale, hot fresh** -> `COLD_ONLY`
- **Both stale** -> `ALL`
- **`--refresh` flag** -> overrides to `ALL` (or `HOT_ONLY` if viewing hot window only and cold is valid)

A **hot-only optimization** applies when the user's date filter (`--mtd`,
`--since`, `--year`) falls entirely within the 90-day hot window and the hot
cache is valid. In this case even a `COLD_ONLY` strategy is short-circuited
to serve data from the hot cache alone, skipping cold data entirely for
faster startup.

### 3. Three Paths

The strategy returned from step 2 determines the next action:

**Path A — NONE**: Load from cache. `CacheManager.load_cache()` reads both
tiers, merges them with deduplication by `id`, and returns the combined
DataFrame plus categories. No API call.

**Path B — HOT_ONLY or COLD_ONLY**: Partial refresh.

1. Load the valid tier from cache (the one that hasn't expired)
2. Compute date range for the stale tier via
   `get_hot_refresh_date_range()` or `get_cold_refresh_date_range()`
3. Call `DataManager.fetch_all_data(start_date, end_date)` with that
   narrowed range
4. Merge fetched data with cached tier via `merge_tiers()` (dedup by `id`)
5. Save the refreshed tier to disk, preserving the other tier

**Path C — ALL**: Full fetch.

1. `BackendTaskRunner.fetch_data_with_retry()` calls
   `DataManager.fetch_all_data()` with the full date range
2. On 401, it attempts a session refresh and retries once
3. On success, `CacheManager.save_cache()` splits the data into hot/cold
   tiers at the 90-day boundary, writes both Parquet files, categories JSON,
   and metadata JSON

### 4. API Call Details

`DataManager.fetch_all_data()` makes three API calls:

- **Categories** (parallel): `backend.get_transaction_categories()` and
  `backend.get_transaction_category_groups()` run concurrently via
  `asyncio.gather`
- **Transactions**: `_fetch_all_transactions()` makes two paginated passes:

  1. Visible transactions: `get_transactions(hideFromReports=False)`
  2. Hidden transactions: `get_transactions(hideFromReports=True)`

  Each pass paginates with `limit=1000, offset=N` until an empty batch is
  returned. The API response is expected as `allTransactions.results[]` or
  `results[]`.

### 5. Post-Processing

After fetching, the data pipeline applies:

1. `_transactions_to_dataframe()` — converts raw API dicts to a Polars
   DataFrame with typed columns (dates, floats, bools) and preserves
   backend-specific extra fields as strings
2. `apply_category_groups()` — adds the `group` column by mapping
   `category` through `config.yaml` category groups. This is done on
   every load (including from cache) so that config changes take effect
   immediately

## Two-Tier Cache

| Tier | Scope | Refresh Interval | Overlap |
|------|-------|-----------------|---------|
| Hot | Last 90 days (rolling window) | 6 hours | 30 days into cold zone |
| Cold | All historical transactions | 30 days | 7-day overlap on fetch boundaries |

The overlap ensures no gaps when the cold cache expires: the cold tier
includes 30 days of data past the boundary date, so even after 30 days
the re-fetched cold data still meets the hot tier.

Tiers are merged at load time with deduplication by `id` (hot takes
precedence). This handles the case where a transaction is edited after
being saved in the cold tier.

## Backend Abstraction

All backends subclass the `FinanceBackend` abstract base class defined in
`moneyflow/backends/base.py`:

```python
class FinanceBackend(ABC):
    @abstractmethod
    async def login(self, email=None, password=None, use_saved_session=True,
                    save_session=True, mfa_secret_key=None) -> None: ...

    @abstractmethod
    async def get_transactions(self, limit=100, offset=0, start_date=None,
                               end_date=None, **kwargs) -> dict: ...

    @abstractmethod
    async def get_transaction_categories(self) -> dict: ...

    @abstractmethod
    async def get_transaction_category_groups(self) -> dict: ...

    @abstractmethod
    async def update_transaction(self, transaction_id, merchant_name=None,
                                 category_id=None, hide_from_reports=None,
                                 **kwargs) -> dict: ...

    @abstractmethod
    async def delete_transaction(self, transaction_id: str) -> bool: ...

    @abstractmethod
    async def get_all_merchants(self) -> list[str]: ...

    @abstractmethod
    def get_backend_type(self) -> str: ...
```

Backends also inherit concrete helpers with sensible defaults — for example
`get_display_labels`, `get_column_config`, `get_currency_symbol`,
`get_computed_columns`, and `clear_auth` — which they can override as needed.

Supported backends:

- **Monarch Money** (`backends/monarch.py`) — GraphQL API client
- **YNAB** (`backends/ynab.py`) — REST API client
- **Amazon Orders** (`backends/amazon.py`) — Amazon order history scraper
- **Demo** (`backends/demo.py`) — Built-in sample data for evaluation

## File Format Reference

See [Caching -> Cache File Format](../config/caching.md#cache-file-format)
for the on-disk schema of the Parquet data, categories JSON, and metadata.
