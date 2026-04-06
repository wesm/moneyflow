"""
Main moneyflow TUI Application.

A fast, keyboard-driven terminal interface for personal finance management.

This is the main application module containing the MoneyflowApp class which:
- Coordinates all UI components (screens, widgets, data table)
- Handles keyboard bindings and user actions
- Manages application state and data loading
- Orchestrates the commit workflow

Architecture:
- UI Layer: This file (Textual screens and widgets)
- Business Logic: Extracted to service classes (ViewPresenter, TimeNavigator, CommitOrchestrator)
- Data Layer: DataManager handles API operations and Polars DataFrames
- State Layer: AppState holds application state

The separation allows business logic to be thoroughly tested while keeping
the UI layer thin and focused on rendering and user interaction.
"""

import argparse
import sys
import traceback
from datetime import date as date_type
from pathlib import Path
from typing import Any, Optional

import polars as pl
from textual.app import App, ComposeResult
from textual.binding import Binding
from textual.containers import Container, Horizontal, Vertical
from textual.reactive import reactive
from textual.widgets import DataTable, Footer, Header, LoadingIndicator, Static

from ..backends import DemoBackend, get_backend
from ..data.account_manager import AccountManager
from ..data.cache_manager import CacheManager, RefreshStrategy
from ..data.cache_orchestrator import CacheOrchestrator
from ..data.data_manager import DataManager
from ..data.duplicate_detector import DuplicateDetector
from ..data.state import AppState, ViewMode
from ..logging_config import get_logger, setup_logging
from ..version import get_version
from . import notification_helper
from .app_controller import AppController
from .backend_config import get_backend_config

# Screen imports
from .screens.batch_scope_screen import BatchScopeScreen
from .screens.credential_screens import (
    FilterScreen,
    QuitConfirmationScreen,
)
from .screens.duplicates_screen import DuplicatesScreen
from .screens.edit_screens import DeleteConfirmationScreen, EditMerchantScreen, SelectCategoryScreen
from .screens.review_screen import ReviewChangesScreen
from .screens.search_screen import SearchScreen
from .screens.transaction_detail_screen import TransactionDetailScreen
from .textual_view import TextualViewPresenter
from .theme_manager import get_theme_css_paths, load_theme_from_config
from .widgets.help_screen import HelpScreen

# Module-level logger
logger = get_logger(__name__)


class MoneyflowApp(App):
    """
    Main application class for the moneyflow terminal UI.

    This Textual application provides a keyboard-driven interface for managing
    personal finance transactions with a focus on power user workflows:

    **Key Features**:
    - Aggregated views (merchant, category, group, account)
    - Drill-down navigation with breadcrumbs
    - Bulk editing with multi-select
    - Time period navigation (year/month with arrow keys)
    - Search and filtering
    - Review-before-commit workflow
    - Offline-first (fetch once, work locally, commit when ready)

    **State Management**:
    - AppState: Holds all application state
    - DataManager: Manages transaction data and API operations
    - Backend: Pluggable backend (MonarchBackend, DemoBackend, etc.)

    **Keyboard Bindings**:
    See BINDINGS class attribute for full list. Key actions:
    - g: Cycle grouping modes
    - u: View all transactions
    - Enter: Drill down
    - Esc: Go back
    - m/r/h/d: Edit operations
    - w: Review and commit
    - ←/→: Navigate time periods
    - y/t/a: Year/month/all time

    **Architecture**:
    Business logic has been extracted to testable service classes:
    - ViewPresenter: Presentation logic (formatting, flags)
    - TimeNavigator: Date calculations
    - CommitOrchestrator: DataFrame updates after commits

    This allows the UI layer to focus on rendering and user interaction
    while keeping complex logic fully tested.
    """

    # CSS_PATH will be set dynamically based on theme configuration
    # This is set in __init__ to allow theme selection from config
    CSS_PATH = None

    BINDINGS = [
        # View mode
        Binding("g", "cycle_grouping", "Group By", show=True),
        Binding("d", "view_ungrouped", "Detail", show=True),
        Binding("D", "find_duplicates", "Duplicates", show=True, key_display="D"),
        # Hidden direct access bindings (still available in aggregate views, not shown in footer)
        # Note: 'm' conflicts with edit_merchant in detail view, so view_merchants removed
        # Note: 'c' removed - conflicts with commit confirmation in review screen
        Binding("A", "view_accounts", "Accounts", show=False, key_display="A"),
        # Time granularity (only active in TIME view)
        Binding("t", "toggle_time_granularity", "Toggle Time", show=False),
        Binding("a", "clear_time_period", "Clear Time", show=False),
        # Sorting
        Binding("s", "toggle_sort_field", "Sort", show=True),
        Binding("v", "reverse_sort", "↕ Reverse", show=True),
        # Time navigation with arrows
        Binding("left", "prev_period", "← Prev", show=True),
        Binding("right", "next_period", "→ Next", show=True),
        # Editing
        Binding("m", "edit_merchant", "Edit Merchant", show=False),
        Binding("c", "edit_category", "Edit Category", show=False),
        Binding("h", "toggle_hide_from_reports", "Hide/Unhide", show=False),
        Binding("x", "delete_transaction", "Delete", show=False),
        Binding("i", "show_transaction_details", "Info", show=False),
        Binding("space", "toggle_select", "Select", show=False),
        Binding("ctrl+a", "select_all", "Select All", show=False),
        Binding("u", "undo_pending_edits", "Undo", show=True),
        # Other actions
        Binding("f", "show_filters", "Filters", show=True),
        Binding("question_mark", "help", "Help", show=True, key_display="?"),
        Binding("slash", "search", "Search", show=True, key_display="/"),
        Binding("escape", "go_back", "Back", show=False),
        Binding("w", "review_and_commit", "Commit", show=True),
        Binding("q", "quit_app", "Quit", show=True),
        Binding("ctrl+c", "quit_app", "Force Quit", show=False),  # Also allow Ctrl+C
    ]

    # Reactive state
    status_message = reactive("Ready")
    pending_changes_count = reactive(0)

    def _notify(self, notification_tuple: tuple[str, str, int]) -> None:
        """
        Wrapper for self.notify() that unpacks NotificationHelper tuples.

        Usage:
            self._notify(notification_helper.commit_success(10))

        Instead of:
            msg, severity, timeout = notification_helper.commit_success(10)
            self.notify(msg, severity=severity, timeout=timeout)
        """
        msg, severity, timeout = notification_tuple
        self.notify(msg, severity=severity, timeout=timeout)

    def __init__(
        self,
        start_year: Optional[int] = None,
        custom_start_date: Optional[str] = None,
        demo_mode: bool = False,
        cache_path: Optional[str] = None,
        force_refresh: bool = False,
        backend: Optional[Any] = None,
        config: Optional[Any] = None,
        config_dir: Optional[str] = None,
        profile_dir: Optional[Path] = None,
        backend_type: Optional[str] = None,
        theme_override: Optional[str] = None,
    ):
        # Load theme before calling super().__init__() so CSS is ready
        # config_dir may be None (defaults to ~/.moneyflow)
        # theme_override takes precedence over config file
        theme_name = load_theme_from_config(config_dir, theme_override=theme_override)
        css_paths = get_theme_css_paths(theme_name)

        # Set CSS_PATH on the class before super().__init__()
        # Textual will load these CSS files during initialization
        # Convert to List[str | PurePath] for Textual's type requirements
        from pathlib import PurePath
        from typing import List, cast

        MoneyflowApp.CSS_PATH = cast(List[str | PurePath], css_paths)

        super().__init__()
        self.demo_mode = demo_mode
        self.start_year = start_year

        # Backend configuration (for Amazon/YNAB/etc)
        # Import here to avoid circular dependency
        from moneyflow.tui.backend_config import MONARCH_CONFIG

        self.backend_config = config or MONARCH_CONFIG

        # Backend will be initialized in initialize_data() based on credentials
        # unless explicitly provided (e.g., for Amazon mode)
        self.backend = backend
        self.config_dir = config_dir  # Custom config directory (None = default ~/.moneyflow)

        from .amazon_presentation import AmazonPresentationManager

        self.amazon_presentation = AmazonPresentationManager(self.demo_mode, self.config_dir)
        from .account_flow import AccountFlowCoordinator

        self.account_flow = AccountFlowCoordinator(self)
        from .backend_task_runner import BackendTaskRunner

        self.task_runner = BackendTaskRunner(self)

        if backend is not None:
            # Backend provided externally (Amazon mode, etc.)
            pass
        elif demo_mode:
            # Default to 3 years of data (2023-2025) for showcasing multi-year TIME views
            self.backend = DemoBackend(start_year=start_year or 2023, years=3)
            version = get_version()
            self.title = f"moneyflow [{version}] [DEMO MODE]"
            # Create demo Amazon database for Amazon linking feature demo
            self.amazon_presentation.create_demo_amazon_db(self.backend.transactions)
        else:
            # Backend will be set in initialize_data() based on credentials
            version = get_version()
            self.title = f"moneyflow [{version}]"

        self.data_manager: Optional[DataManager] = None
        self.state = AppState()

        # Store profile_dir and backend_type for pre-configured backends (e.g., Amazon via CLI)
        self._preconfigured_profile_dir = profile_dir
        self._preconfigured_backend_type = backend_type
        # Demo mode shows all years of data (no time filtering by default)
        self.loading = False
        self.custom_start_date = custom_start_date
        self.stored_credentials: Optional[dict] = None
        self.cache_path = cache_path
        self.force_refresh = force_refresh
        self.cache_manager = None  # Will be set if caching is enabled
        self.cache_orchestrator = None  # Coordinates cache refresh/load behavior
        self.cache_year_filter = None  # Track what filters the cache uses
        self.cache_since_filter = None
        self.display_start_date = None  # Display filter (--mtd/--since) separate from cache
        self.config_dir = config_dir  # Custom config directory (None = default ~/.moneyflow)
        self.encryption_key: Optional[bytes] = None  # Encryption key for cache (set after login)
        # Controller will be initialized after data_manager is ready
        self.controller: Optional[AppController] = None

    def compose(self) -> ComposeResult:
        """Compose the main UI."""
        yield Header(show_clock=True)

        with Container(id="app-body"):
            # Top status bar
            with Horizontal(id="status-bar"):
                yield Static("", id="breadcrumb")
                yield Static("", id="stats")

            # Main content area
            with Vertical(id="content-area"):
                yield LoadingIndicator(id="loading")
                yield Static("", id="loading-status")
                yield DataTable(id="data-table", cursor_type="row")

            # Bottom action hints
            with Horizontal(id="action-bar"):
                yield Static("", id="action-hints")
                yield Static("", id="pending-changes")

        yield Footer()

    async def on_mount(self) -> None:
        """Initialize the app after mounting."""
        try:
            # Set up data table
            table = self.query_one("#data-table", DataTable)
            table.cursor_type = "row"
            table.zebra_stripes = True

            # Hide loading initially
            self.query_one("#loading", LoadingIndicator).display = False
            self.query_one("#loading-status", Static).display = False

            # Start data initialization in a worker
            self.run_worker(self.initialize_data(), exclusive=True)
        except Exception as e:
            # Try to show error to user
            try:
                loading_status = self.query_one("#loading-status", Static)
                loading_status.update(f"❌ Startup failed: {e}\n\nPress 'q' to quit")
                loading_status.display = True
            except Exception:
                pass  # UI not ready yet, error will be shown in console
            raise

    def _setup_loading_ui(self):
        """Setup loading UI and return loading status widget."""
        self.loading = True
        self.query_one("#loading", LoadingIndicator).display = True
        loading_status = self.query_one("#loading-status", Static)
        loading_status.display = True
        return loading_status

    def _initialize_managers(
        self, profile_dir: Optional[Path] = None, backend_type: Optional[str] = None
    ):
        """
        Initialize data manager, cache manager, and controller.

        Args:
            profile_dir: Optional profile directory for multi-account mode
                        If provided, merchant cache and transaction cache will be
                        stored in this directory to isolate accounts
            backend_type: Backend type (amazon, monarch, ynab) for category logic
        """
        # config_dir is required - default to ~/.moneyflow if not specified
        config_dir = self.config_dir if self.config_dir else str(Path.home() / ".moneyflow")

        # Determine merchant cache directory
        if self.demo_mode:
            # Demo mode: use temp directory (don't pollute ~/.moneyflow)
            import tempfile

            merchant_cache_dir = str(Path(tempfile.gettempdir()) / "moneyflow_demo")
        elif profile_dir:
            # Multi-account mode: use profile directory to isolate merchant caches
            merchant_cache_dir = str(profile_dir)
        else:
            # Legacy single-account mode: use config_dir
            merchant_cache_dir = ""

        self.data_manager = DataManager(
            self.backend,
            config_dir=config_dir,
            merchant_cache_dir=merchant_cache_dir,
            profile_dir=profile_dir,
            backend_type=backend_type,
        )

        # Initialize cache manager for backends that support caching
        # cache_path is None for backends like Amazon that don't need caching
        # encryption_key can be None for plaintext credentials (CacheManager supports both modes)
        if self.cache_path is not None:
            # Determine cache directory
            if self.cache_path == "":
                # Default cache location - use profile-specific or legacy location
                if profile_dir:
                    # Multi-account mode: cache inside profile directory
                    cache_dir = str(profile_dir / "cache")
                else:
                    # Legacy single-account mode: use default location
                    cache_dir = str(Path.home() / ".moneyflow" / "cache")
            else:
                # User specified explicit cache path
                cache_dir = self.cache_path

            self.cache_manager = CacheManager(
                cache_dir=cache_dir, encryption_key=self.encryption_key
            )
            self.cache_orchestrator = CacheOrchestrator(
                self.cache_manager, self.data_manager, notify=self.notify
            )
        else:
            self.cache_orchestrator = None

        # Initialize controller with view presenter pattern
        view = TextualViewPresenter(self)
        self.controller = AppController(view, self.state, self.data_manager, self.cache_manager)
        self.controller.amazon_match_cache = self.amazon_presentation.get_cache()

    def _determine_date_range(self):
        """Determine date range based on CLI arguments.

        Separates display filtering (--mtd, --since) from cache behavior:
        - display_start_date: What the user wants to VIEW (filters the UI)
        - cache filters: What the cache actually STORES (preserved on refresh)

        Returns:
            tuple: (display_start_date, cache_year_filter, cache_since_filter)
        """
        # Display filter - what user wants to see
        if self.custom_start_date:
            display_start_date = self.custom_start_date
        elif self.start_year:
            display_start_date = f"{self.start_year}-01-01"
        else:
            display_start_date = None

        # Cache filters - determined by existing cache or first fetch
        # These are set later based on what's actually cached
        cache_year_filter = None
        cache_since_filter = None

        return display_start_date, cache_year_filter, cache_since_filter

    @staticmethod
    def _filter_df_by_start_date(df: pl.DataFrame, start_date: str) -> pl.DataFrame:
        """Filter DataFrame to only include transactions on or after start_date.

        Used to filter cached data when --mtd or --since is specified, since the cache
        may contain more data than requested (e.g., full year cache for MTD request).

        Args:
            df: Transaction DataFrame with a 'date' column
            start_date: Start date string in YYYY-MM-DD format

        Returns:
            Filtered DataFrame with only transactions >= start_date
        """
        return df.filter(pl.col("date") >= pl.lit(start_date).str.to_date())

    def _store_data(self, df, categories, category_groups):
        """Store data in data manager and state."""
        self.data_manager.df = df
        self.data_manager.categories = categories
        self.data_manager.category_groups = category_groups
        self.state.transactions_df = df

    def _initialize_view(self):
        """Initialize view and show all data."""
        # Show all data by default (start_date and end_date remain None)
        # The --year and --since flags control API fetching, not view filtering

        # Show initial view (merchants)
        self.refresh_view()

    async def _check_and_load_cache(self, loading_status):
        """Check cache status and determine refresh strategy.

        Uses the two-tier cache system to determine what data needs refreshing:
        - Hot cache: Recent 90 days, refreshed every 6 hours
        - Cold cache: Historical data (>90 days), refreshed every 30 days

        Optimization: If --mtd or --since is within hot window (90 days),
        only hot cache is loaded for faster startup.

        Args:
            loading_status: Loading status widget

        Returns:
            tuple: (data, strategy) where:
                - data is (df, categories, category_groups) or None
                - strategy is RefreshStrategy indicating what to fetch
        """
        if not self.cache_orchestrator:
            return None, RefreshStrategy.ALL

        return await self.cache_orchestrator.check_and_load_cache(
            force_refresh=self.force_refresh,
            custom_start_date=self.custom_start_date,
            status_update=loading_status.update,
        )

    async def _partial_refresh(self, strategy, creds, loading_status):
        """Perform a partial refresh of cache data.

        This is called when one tier is valid but the other needs refreshing:
        - HOT_ONLY: Hot tier expired, cold is valid. Fetch recent 90 days.
        - COLD_ONLY: Cold tier expired, hot is valid. Fetch historical data.

        Args:
            strategy: RefreshStrategy (HOT_ONLY or COLD_ONLY)
            creds: Credentials dict (may be None in demo mode)
            loading_status: Loading status widget

        Returns:
            tuple: (df, categories, category_groups) or None on failure
        """
        if not self.cache_orchestrator:
            return None

        return await self.cache_orchestrator.partial_refresh(
            strategy=strategy,
            creds=creds,
            status_update=loading_status.update,
        )

    async def _handle_init_error(self, error, loading_status):
        """Handle initialization errors.

        Args:
            error: The exception that occurred
            loading_status: Loading status widget
        """

        logger = get_logger(__name__)

        error_str = str(error).lower()

        # Check if it's a 401/unauthorized error
        if "401" in error_str or "unauthorized" in error_str:
            logger.error("401/Unauthorized in outer handler - recovery already attempted")
            # If we get here, session recovery already failed in the fetch block above
            # Delete the bad session
            try:
                if self.backend:
                    self.backend.delete_session()
                    logger.debug("Session deleted")
            except Exception as del_err:
                logger.error(f"Failed to delete session: {del_err}")

            # Show helpful error
            loading_status.update(
                "❌ Session error.\n\n"
                "Could not authenticate with backend.\n"
                "Please restart the app to login fresh.\n\n"
                "Press 'q' to quit"
            )
        else:
            error_msg = f"Failed to load data: {error}"
            loading_status.update(f"❌ {error_msg}\n\nPress 'q' to quit")

        # Log detailed error for debugging
        logger.error(f"DATA LOADING ERROR: {error} (Type: {type(error).__name__})", exc_info=True)

    async def initialize_data(self) -> None:
        """
        Load data from backend API or cache.

        This is the main orchestrator for data initialization. It coordinates:
        1. Credential handling (unlock/setup)
        2. Backend login with retry logic
        3. Cache checking and loading
        4. Data fetching from API with retry logic
        5. Data storage and view initialization
        6. Error handling and cleanup
        """

        logger = get_logger(__name__)
        logger.debug("initialize_data started")
        has_error = False  # Track if we encountered an error

        # Setup loading UI
        try:
            loading_status = self._setup_loading_ui()
        except Exception as e:
            logger.error(f"Failed to initialize UI: {e}", exc_info=True)
            raise

        # Set initial status
        if self.demo_mode:
            loading_status.update("🎮 DEMO MODE - Loading sample data...")
        else:
            loading_status.update("🔄 Connecting to backend...")

        try:
            # Step 1: Handle account selection (unless in demo mode or backend pre-configured)
            profile_dir = None
            creds = None

            if self.demo_mode:
                # Demo mode - no account selection needed
                account_id = "demo"
                loading_status.update("🎮 DEMO MODE - Loading sample data...")
            elif self.backend is not None:
                # Backend pre-configured (e.g., Amazon mode via CLI)
                # Use preconfigured profile_dir if available
                profile_dir = self._preconfigured_profile_dir
                account_id = None  # No account tracking for pre-configured backends
                if self.backend_config.requires_auth:
                    creds = await self.account_flow.handle_credentials()
                    if creds is None:
                        return  # User exited
            else:
                # Normal multi-account flow
                account_id, profile_dir, creds = await self.account_flow.handle_account_selection()

                if account_id is None:
                    # User chose to exit from account selector
                    self.exit()
                    return

                if account_id == "demo":
                    # User selected demo mode from account selector
                    self.demo_mode = True
                    self.backend = DemoBackend(start_year=self.start_year or 2023, years=3)
                    self.title = "moneyflow [DEMO MODE]"
                    self.amazon_presentation.create_demo_amazon_db(self.backend.transactions)
                    loading_status.update("🎮 DEMO MODE - Loading sample data...")
                else:
                    # Load account info to get backend_type
                    config_path = Path(self.config_dir) if self.config_dir else None
                    account_manager = AccountManager(config_dir=config_path)
                    account = account_manager.get_account(account_id)

                    if account and account.backend_type == "amazon" and profile_dir:
                        # Initialize Amazon backend with profile-scoped database
                        from moneyflow.backends.amazon import AmazonBackend

                        db_path = str(profile_dir / "amazon.db")
                        self.backend = AmazonBackend(
                            db_path=db_path, config_dir=self.config_dir, profile_dir=profile_dir
                        )
                        self.backend_config = get_backend_config("amazon")
                        loading_status.update("📦 Loading Amazon data...")

            # Step 2: Initialize backend (if not already set)
            if self.backend is None and creds:
                backend_type = creds.get("backend_type", "monarch")
                loading_status.update(f"🔄 Initializing {backend_type} backend...")

                # Pass profile_dir for Monarch backend to store session in profile
                backend_kwargs = {}
                if backend_type == "monarch" and self._preconfigured_profile_dir:
                    backend_kwargs["profile_dir"] = str(self._preconfigured_profile_dir)

                self.backend = get_backend(backend_type, **backend_kwargs)
                self.backend_config = get_backend_config(backend_type)

                # Step 3: Login with retry logic
                # For YNAB, get budget_id from account if available
                budget_id = None
                if backend_type == "ynab" and account_id:
                    budget_id = self.account_flow.get_ynab_budget_id(account_id)

                login_success = await self.task_runner.login_with_retry(
                    creds, loading_status, budget_id
                )
                if not login_success:
                    has_error = True
                    return
            elif self.backend and not self.demo_mode:
                # Backend exists but might need login
                if self.backend_config.requires_auth and creds:
                    # For pre-configured backends, we don't have account_id to look up budget_id
                    login_success = await self.task_runner.login_with_retry(creds, loading_status)
                    if not login_success:
                        has_error = True
                        return
                else:
                    await self.backend.login()  # No-op for backends without auth

            # Step 4: Initialize managers (pass profile_dir for multi-account isolation)
            # Determine backend_type for category loading
            # Use preconfigured values if backend was set externally (e.g., Amazon via CLI)
            determined_backend_type = self._preconfigured_backend_type
            determined_profile_dir = self._preconfigured_profile_dir or profile_dir

            if not determined_backend_type:
                if self.demo_mode:
                    determined_backend_type = "demo"
                elif self.backend:
                    # Get backend type from backend instance (Open/Closed Principle)
                    determined_backend_type = self.backend.get_backend_type()
                elif creds:
                    determined_backend_type = creds.get("backend_type")

            self._initialize_managers(
                profile_dir=determined_profile_dir, backend_type=determined_backend_type
            )

            # Step 4: Determine display filter (separate from cache)
            self.display_start_date, self.cache_year_filter, self.cache_since_filter = (
                self._determine_date_range()
            )

            # Step 5: Check cache and determine refresh strategy
            cached_data, strategy = await self._check_and_load_cache(loading_status)

            if strategy == RefreshStrategy.NONE and cached_data:
                # Both cache tiers valid - use cached data entirely
                df, categories, category_groups = cached_data
                # Filter cached data to match requested date range (e.g., --mtd)
                # Cache may contain more data than requested (e.g., full year cache for MTD request)
                if self.display_start_date:
                    original_count = len(df)
                    df = self._filter_df_by_start_date(df, self.display_start_date)
                    if len(df) < original_count:
                        loading_status.update(
                            f"📦 Filtered cache: {len(df):,} of {original_count:,} transactions"
                        )
            elif strategy in (RefreshStrategy.HOT_ONLY, RefreshStrategy.COLD_ONLY):
                # Partial refresh - one tier valid, refresh the other
                partial_result = await self._partial_refresh(strategy, creds, loading_status)
                if partial_result:
                    df, categories, category_groups = partial_result
                    # Filter if needed
                    if self.display_start_date:
                        original_count = len(df)
                        df = self._filter_df_by_start_date(df, self.display_start_date)
                        if len(df) < original_count:
                            loading_status.update(
                                f"📦 Filtered: {len(df):,} of {original_count:,} transactions"
                            )
                else:
                    # Partial refresh failed, fall back to full fetch
                    # Always fetch full data - display filter applied after
                    fetch_result = await self.task_runner.fetch_data_with_retry(
                        creds, None, None, loading_status
                    )
                    if fetch_result is None:
                        has_error = True
                        return
                    df, categories, category_groups = fetch_result
            else:
                # Step 6: Full fetch from API (BOTH, ALL, or no cache)
                # Always fetch full data - display filter applied after
                fetch_result = await self.task_runner.fetch_data_with_retry(
                    creds, None, None, loading_status
                )
                if fetch_result is None:
                    has_error = True
                    return
                df, categories, category_groups = fetch_result

            # Apply display filter after fetch (cache stores full data)
            if self.display_start_date and strategy != RefreshStrategy.NONE:
                original_count = len(df)
                df = self._filter_df_by_start_date(df, self.display_start_date)
                if len(df) < original_count:
                    loading_status.update(
                        f"📦 Filtered: {len(df):,} of {original_count:,} transactions"
                    )

            # Step 7: Store data
            self._store_data(df, categories, category_groups)

            # Step 8: Initialize view
            loading_status.update(f"✅ Ready! Showing {len(df):,} transactions")
            self._initialize_view()

        except Exception as e:
            await self._handle_init_error(e, loading_status)
            has_error = True

        finally:
            self.loading = False
            # Safely hide loading UI (may fail if app is shutting down)
            try:
                self.query_one("#loading", LoadingIndicator).display = False
                # DON'T hide loading-status if we had an error
                if not has_error:
                    self.query_one("#loading-status", Static).display = False
                # If there was an error, keep the error message visible
            except Exception:
                # DOM already torn down during shutdown - this is fine
                pass

    def update_loading_progress(self, current: int, total: int, message: str) -> None:
        """Update loading progress message."""
        self.status_message = f"{message} ({current}/{total})"

    def _save_table_position(self) -> dict:
        """
        Save current table cursor and scroll position.

        Returns:
            Dict with cursor_row and scroll_y
        """
        try:
            table = self.query_one("#data-table", DataTable)
            return {
                "cursor_row": table.cursor_row,
                "scroll_y": table.scroll_y,
            }
        except Exception:
            return {"cursor_row": 0, "scroll_y": 0}

    def _restore_table_position(self, saved_position: dict) -> None:
        """
        Restore table cursor and scroll position after refresh.

        CRITICAL ORDER OF OPERATIONS:
        1. Move cursor first (this auto-scrolls to show the row)
        2. Set scroll_y AFTER cursor move (to override auto-scroll)

        This order is counterintuitive but required because Textual's
        move_cursor() auto-scrolls to bring the row into view, which
        would override any scroll_y we set before it.

        Args:
            saved_position: Dict from _save_table_position()
        """
        from ..logging_config import get_logger

        logger = get_logger(__name__)

        try:
            table = self.query_one("#data-table", DataTable)
            cursor_row = saved_position.get("cursor_row", 0)
            scroll_y = saved_position.get("scroll_y", 0)

            logger.debug(
                f"Restoring table position: cursor {table.cursor_row}→{cursor_row}, scroll {table.scroll_y}→{scroll_y}"
            )

            # Step 1: Move cursor (this will auto-scroll)
            if cursor_row < table.row_count:
                table.move_cursor(row=cursor_row)

            # Step 2: Override auto-scroll with saved scroll position
            # DO NOT change this order - move_cursor must happen first
            table.scroll_y = scroll_y

            logger.debug(f"Position restored: cursor={table.cursor_row}, scroll_y={table.scroll_y}")
        except Exception as e:
            logger.error(f"Failed to restore table position: {e}")
            pass  # Table might not be ready yet

    def refresh_view(self, force_rebuild: bool = True) -> None:
        """
        Refresh the current view based on state.

        Delegates to AppController which handles all business logic.
        This method is now just a thin wrapper for backwards compatibility.

        Args:
            force_rebuild: If True, clear columns and rebuild entire table.
                          If False, only update rows (avoids flash when staying in same view).
        """
        if self.controller is None:
            return

        # Delegate to controller - it handles all the business logic
        # Note: controller.refresh_view() will call view.on_table_updated()
        # which triggers on_table_updated() automatically via message
        self.controller.refresh_view(force_rebuild=force_rebuild)

    def on_table_updated(self, event) -> None:
        """
        Handle Amazon column lazy loading after a table update.

        Called by TextualViewPresenter posting TableUpdated message after the controller
        updates the table. This ensures Amazon match data is loaded regardless
        of whether refresh_view() was called from the app or directly from
        the controller (e.g., after a commit).
        """
        if self.controller is None:
            return

        # Check if Amazon column is being shown and handle lazy loading
        self.amazon_presentation.set_visibility(
            self.controller._showing_amazon_column,
            getattr(self.amazon_presentation, "_column_index", None),
        )

        if self.amazon_presentation._column_visible:
            table = self.query_one("#data-table", DataTable)
            column_keys = list(table.columns.keys())
            logger.debug(f"Amazon column check: column_keys={column_keys}")
            self.amazon_presentation._column_index = (
                column_keys.index("amazon") if "amazon" in column_keys else None
            )
            logger.debug(f"Amazon column index: {self.amazon_presentation._column_index}")

            # Rows are always rebuilt, so reload Amazon match statuses each refresh.
            self.amazon_presentation.on_amazon_view_refresh(self.state.current_data)
        else:
            self.amazon_presentation._column_index = None

    # Actions
    def action_view_merchants(self) -> None:
        """Switch to merchant view."""
        self.controller.switch_to_merchant_view()

    def action_view_categories(self) -> None:
        """Switch to category view."""
        self.controller.switch_to_category_view()

    def action_view_groups(self) -> None:
        """Switch to group view."""
        self.controller.switch_to_group_view()

    def action_view_accounts(self) -> None:
        """Switch to account view."""
        self.controller.switch_to_account_view()

    def action_cycle_grouping(self) -> None:
        """
        Cycle through grouping views.

        If drilled down: Cycle sub-groupings (Category/Group/Account/Detail)
        If not drilled down: Cycle top-level views (Merchant/Category/Group/Account)
        """
        view_name = self.controller.cycle_grouping()
        if view_name:
            self._notify(notification_helper.view_changed(view_name))

    def action_view_ungrouped(self) -> None:
        """Switch to ungrouped transactions view (all transactions in reverse chronological order)."""
        self.controller.switch_to_detail_view(set_default_sort=True)
        self._notify(notification_helper.ALL_TRANSACTIONS_VIEW)

    def action_find_duplicates(self) -> None:
        """Find and display duplicate transactions."""
        if self.data_manager is None or self.data_manager.df is None:
            return
        # Run in worker to support async operations
        self.run_worker(self._find_duplicates_async(), exclusive=False)

    async def _find_duplicates_async(self) -> None:
        """Find duplicates and show duplicates screen."""
        # Find duplicates in current filtered view
        filtered_df = self.state.get_filtered_df()
        if filtered_df is None or filtered_df.is_empty():
            self.notify("No transactions to check", timeout=2)
            return

        self.notify("Scanning for duplicates...", timeout=1)
        duplicates = DuplicateDetector.find_duplicates(filtered_df)

        if duplicates.is_empty():
            self.notify("✅ No duplicates found!", severity="information", timeout=3)
        else:
            groups = DuplicateDetector.get_duplicate_groups(filtered_df, duplicates)
            # Show duplicates screen (user can delete multiple times before closing)
            # Pass reference to main app so screen can call delete methods
            self.push_screen(DuplicatesScreen(duplicates, groups, filtered_df, self))

    def action_undo_pending_edits(self) -> None:
        """Undo the most recent pending edit or bulk edit batch."""
        if self.data_manager is None or not self.data_manager.pending_edits:
            self.notify("No pending edits to undo", timeout=2)
            return

        # Save cursor and scroll position
        saved_position = self._save_table_position()

        # Get the timestamp of the most recent edit for notification
        last_edit = self.data_manager.pending_edits[-1]
        field_name = last_edit.field.replace("_", " ").title()

        # Undo the last batch of edits
        edits_to_undo = self.data_manager.undo_last_batch()

        # Refresh view to update indicators
        self.refresh_view(force_rebuild=False)

        # Restore cursor and scroll position
        self._restore_table_position(saved_position)

        # Show notification with what was undone
        count_undone = len(edits_to_undo)
        count_remaining = len(self.data_manager.pending_edits)

        if count_undone == 1:
            self.notify(
                f"Undone {field_name} edit ({count_remaining} remaining)",
                severity="information",
                timeout=2,
            )
        else:
            self.notify(
                f"Undone {count_undone} {field_name} edits ({count_remaining} remaining)",
                severity="information",
                timeout=2,
            )

    # Time navigation actions
    def action_toggle_time_granularity(self) -> None:
        """Cycle through time granularities: Year → Month → Day → Year."""
        # Allow in TIME view or when sub-grouping by time
        if not (
            self.state.view_mode == ViewMode.TIME or self.state.sub_grouping_mode == ViewMode.TIME
        ):
            return  # Ignore if not in TIME context

        view_name = self.controller.toggle_time_granularity()
        self.notify(f"Switched to {view_name}", timeout=1)

    def action_clear_time_period(self) -> None:
        """Clear time period selection (shortcut for Escape when drilled into time)."""
        if not self.state.is_time_period_selected():
            return  # Nothing to clear

        self.state.clear_time_selection()
        self.controller.refresh_view()
        self.notify("Cleared time period filter", timeout=1)

    def _select_month(self, month: int, month_name: str) -> None:
        """Helper to select a specific month of the current year."""
        description = self.controller.select_month(month)
        self.notify(f"Viewing: {description}", timeout=1)

    def action_prev_period(self) -> None:
        """Navigate to previous time period (only when drilled into time)."""
        description = self.state.navigate_time_period(-1)

        if description:
            self.controller.refresh_view()
            self.notify(f"← {description}", timeout=1)
        # Otherwise do nothing (not drilled into time)

    def action_next_period(self) -> None:
        """Navigate to next time period (only when drilled into time)."""
        description = self.state.navigate_time_period(1)

        if description:
            self.controller.refresh_view()
            self.notify(f"→ {description}", timeout=1)
        # Otherwise do nothing (not drilled into time)

    def action_reverse_sort(self) -> None:
        """Reverse the current sort direction."""
        direction = self.controller.reverse_sort()
        self.notify(f"Sort: {direction}", timeout=1)

    def action_toggle_sort_field(self) -> None:
        """Toggle sorting field."""
        field_name = self.controller.toggle_sort_field()
        self.notify(f"Sorting by: {field_name}", timeout=1)

    def action_show_filters(self) -> None:
        """Show filter options modal."""
        self.run_worker(self._show_filter_modal(), exclusive=False)

    async def _show_filter_modal(self) -> None:
        """Show filter modal and apply selected filters."""
        result = await self.push_screen(
            FilterScreen(
                show_transfers=self.state.show_transfers, show_hidden=self.state.show_hidden
            ),
            wait_for_dismiss=True,
        )

        if result is not None:
            # Apply filters via controller
            self.controller.apply_filters(
                show_transfers=result["show_transfers"], show_hidden=result["show_hidden"]
            )

            # Build status message
            statuses = []
            if result["show_hidden"]:
                statuses.append("hidden items shown")
            else:
                statuses.append("hidden items excluded")
            if result["show_transfers"]:
                statuses.append("transfers shown")
            else:
                statuses.append("transfers excluded")

            self.notify(f"Filters: {', '.join(statuses)}", timeout=3)

    def action_help(self) -> None:
        """Show help screen."""
        self.push_screen(HelpScreen())

    def action_search(self) -> None:
        """Show search input with live filtering."""
        self.run_worker(self._show_search(), exclusive=False)

    async def _show_search(self) -> None:
        """Show search modal and apply filter."""
        # Show search modal with current query
        new_query = await self.push_screen(
            SearchScreen(current_query=self.state.search_query), wait_for_dismiss=True
        )

        if new_query is not None:  # None means cancelled
            # Apply search via controller
            if new_query:
                # Search Amazon items for the query (may be slow)
                amazon_match_ids = self.amazon_presentation.search_amazon_items_for_query(
                    new_query,
                    self.state.transactions_df,
                    self.state.start_date,
                    self.state.end_date,
                )
                count = self.controller.apply_search(new_query, amazon_match_ids)
                self.notify(f"Search: '{new_query}' - {count} results", timeout=2)
            else:
                self.controller.clear_search()
                self.notify("Search cleared", timeout=1)

    def action_toggle_select(self) -> None:
        """Toggle selection of current row for bulk operations."""
        if self.controller is None or self.state.current_data is None:
            return

        table = self.query_one("#data-table", DataTable)
        if table.cursor_row < 0:
            return

        # Save cursor and scroll position
        saved_position = self._save_table_position()

        # Use controller to handle the selection logic
        count, item_type = self.controller.toggle_selection_at_row(table.cursor_row)

        # Refresh view to show checkmark (smooth update - don't rebuild columns)
        self.refresh_view(force_rebuild=False)

        # Restore cursor and scroll position
        self._restore_table_position(saved_position)

        # Notify user
        item_label = "group(s)" if item_type == "group" else "transaction(s)"
        self.notify(f"Selected: {count} {item_label}", timeout=1)

    def action_select_all(self) -> None:
        """Toggle select all / deselect all rows in the current view."""
        if self.controller is None or self.state.current_data is None:
            return

        table = self.query_one("#data-table", DataTable)
        saved_cursor_row = table.cursor_row if table.cursor_row >= 0 else 0

        # Use controller to handle the select all logic
        count, all_selected, item_type = self.controller.toggle_select_all_visible()

        # Refresh view to show/hide checkmarks (smooth update - don't rebuild columns)
        self.refresh_view(force_rebuild=False)

        # Restore cursor position
        if saved_cursor_row < table.row_count:
            table.move_cursor(row=saved_cursor_row)

        # Notify user
        item_label = "group(s)" if item_type == "group" else "transaction(s)"
        if all_selected:
            self.notify(f"Selected all {count} {item_label}", timeout=2)
        else:
            self.notify(f"Deselected all {item_label}", timeout=2)

    def action_edit_merchant(self) -> None:
        """
        Edit merchant name for current selection.

        Uses controller.edit_merchant_current_selection() which handles all edit modes.
        """
        if self.data_manager is None:
            return

        self.run_worker(self._edit_merchant(), exclusive=False)

    async def _edit_merchant(self) -> None:
        """
        Edit merchant name using controller orchestration.

        Flow:
        1. Get merchant suggestions (for autocomplete)
        2. Get edit context from controller (what to edit)
        3. Show modal with current value
        4. Call controller to execute edit
        5. Display result
        """
        # Get cursor position
        table = self.query_one("#data-table", DataTable)
        cursor_row = table.cursor_row if table.cursor_row >= 0 else 0

        # Get edit context from controller (determines what to edit)
        context = self.controller.determine_edit_context("merchant", cursor_row=cursor_row)

        if context.transactions.is_empty():
            self.notify("No transactions to edit", timeout=2)
            return

        # Get merchant suggestions for autocomplete
        all_merchants = self.controller.get_merchant_suggestions()

        # Show edit modal
        new_merchant = await self.push_screen(
            EditMerchantScreen(
                current_merchant=context.current_value or "",
                transaction_count=context.transaction_count,
                all_merchants=all_merchants,
                transaction_details=None,  # Could add summary from context if needed
            ),
            wait_for_dismiss=True,
        )

        if new_merchant:
            # Save position before refresh
            saved_position = self._save_table_position()

            # Execute edit via controller (business logic)
            count = self.controller.edit_merchant_current_selection(
                new_merchant, cursor_row=cursor_row
            )

            # Clear selection if multi-select
            if context.is_multi_select:
                self.state.clear_selection()

            # Display result
            self._notify(notification_helper.edit_queued(count))
            self.refresh_view()
            self._restore_table_position(saved_position)

    def action_edit_category(self) -> None:
        """
        Change category for current selection.

        Uses controller.edit_category_current_selection().
        """
        if self.data_manager is None:
            return

        self.run_worker(self._edit_category(), exclusive=False)

    async def _edit_category(self) -> None:
        """Simplified category edit using controller orchestration."""
        # Get cursor position
        table = self.query_one("#data-table", DataTable)
        cursor_row = table.cursor_row if table.cursor_row >= 0 else 0

        # Get edit context from controller
        context = self.controller.determine_edit_context("category", cursor_row=cursor_row)

        if context.transactions.is_empty():
            self.notify("No transactions to edit", timeout=2)
            return

        # Show category selection modal
        new_category_id = await self.push_screen(
            SelectCategoryScreen(
                self.data_manager.categories,
                current_category_id=None,
                transaction_details=None,
                transaction_count=context.transaction_count,
            ),
            wait_for_dismiss=True,
        )

        if new_category_id:
            # Save position before refresh
            saved_position = self._save_table_position()

            # Execute edit via controller
            count = self.controller.edit_category_current_selection(
                new_category_id, cursor_row=cursor_row
            )

            # Clear selection if multi-select
            if context.is_multi_select:
                self.state.clear_selection()

            # Display result
            new_cat_name = self.data_manager.categories.get(new_category_id, {}).get(
                "name", "Unknown"
            )
            self.notify(
                f"Queued {count} category changes to {new_cat_name}. Press w to commit.", timeout=3
            )
            self.refresh_view()
            self._restore_table_position(saved_position)

    def action_toggle_hide_from_reports(self) -> None:
        """
        Toggle hide from reports flag for current transaction(s) or selected groups.

        Uses controller.toggle_hide_current_selection().
        """
        if self.data_manager is None or self.state.current_data is None:
            return

        table = self.query_one("#data-table", DataTable)
        if table.cursor_row < 0:
            return

        cursor_row = table.cursor_row

        # Check for existing pending hide toggle on current transaction (for undo in detail view ONLY)
        # Only applies to actual transaction detail view, not aggregate or sub-grouped views
        is_transaction_detail_view = (
            self.state.view_mode == ViewMode.DETAIL
            and not self.state.sub_grouping_mode  # Not sub-grouped (showing transactions, not aggregates)
            and len(self.state.selected_ids) == 0  # Single transaction (not multi-select)
        )

        if is_transaction_detail_view:
            # Single transaction in detail view - check for existing edit to undo
            row_data = self.state.current_data.row(cursor_row, named=True)
            txn_id = row_data.get("id")

            if txn_id:  # Ensure this is actually a transaction row
                existing_edit = None
                for edit in self.data_manager.pending_edits:
                    if edit.transaction_id == txn_id and edit.field == "hide_from_reports":
                        existing_edit = edit
                        break

                if existing_edit:
                    # Undo the pending toggle
                    saved_position = self._save_table_position()
                    self.data_manager.pending_edits.remove(existing_edit)
                    self.notify("Reverted hide/unhide change", timeout=2)
                    self.refresh_view()
                    self._restore_table_position(saved_position)
                    return

        # Save position before refresh
        saved_position = self._save_table_position()

        # Get edit context from controller (what transactions are we toggling?)
        context = self.controller.determine_edit_context("merchant", cursor_row=cursor_row)

        if context.transactions.is_empty():
            self.notify("No transactions to toggle", timeout=2)
            return

        # Execute hide toggle via controller (includes undo detection)
        count, was_undo = self.controller.toggle_hide_current_selection(cursor_row=cursor_row)

        # Clear selection if multi-select
        if context.is_multi_select:
            self.state.clear_selection()

        # Display appropriate message
        if was_undo:
            self.notify(
                f"Reverted hide/unhide for {count} transactions",
                severity="information",
                timeout=2,
            )
        else:
            self.notify(
                f"Toggled hide/unhide for {count} transactions. Press w to commit.", timeout=3
            )

        self.refresh_view()
        self._restore_table_position(saved_position)

    def action_show_transaction_details(self) -> None:
        """Show detailed information about current transaction."""
        # Must be in detail view showing actual transactions (not sub-grouped aggregates)
        is_transaction_view = (
            self.state.view_mode == ViewMode.DETAIL and not self.state.sub_grouping_mode
        )
        if self.data_manager is None or not is_transaction_view:
            self.notify("Details only available in transaction view", timeout=2)
            return

        if self.state.current_data is None:
            return

        table = self.query_one("#data-table", DataTable)
        if table.cursor_row < 0:
            return

        # Get current transaction data
        row_data = self.state.current_data.row(table.cursor_row, named=True)
        transaction_dict = dict(row_data)

        # Look for matching Amazon orders if this looks like an Amazon transaction
        amazon_matches, amazon_searched = self.amazon_presentation.find_amazon_matches(
            transaction_dict
        )

        # Show detail modal with any Amazon matches
        self.push_screen(
            TransactionDetailScreen(
                transaction_dict,
                amazon_matches=amazon_matches,
                amazon_searched=amazon_searched,
            )
        )

    def action_delete_transaction(self) -> None:
        """Delete current transaction with confirmation."""
        # Must be in detail view showing actual transactions (not sub-grouped aggregates)
        is_transaction_view = (
            self.state.view_mode == ViewMode.DETAIL and not self.state.sub_grouping_mode
        )
        if self.data_manager is None or not is_transaction_view:
            self.notify("Delete only works in transaction detail view", timeout=2)
            return

        self.run_worker(self._delete_transaction(), exclusive=False)

    async def _delete_transaction(self) -> None:
        """Show delete confirmation and delete if confirmed."""
        if self.state.current_data is None:
            return

        table = self.query_one("#data-table", DataTable)
        if table.cursor_row < 0:
            return

        # Check if multi-select is active
        if len(self.state.selected_ids) > 0:
            # Multi-select delete
            transaction_ids = list(self.state.selected_ids)
            count = len(transaction_ids)
        else:
            # Single transaction delete
            row_data = self.state.current_data.row(table.cursor_row, named=True)
            transaction_ids = [row_data["id"]]
            count = 1

        # Show confirmation
        confirmed = await self.push_screen(
            DeleteConfirmationScreen(transaction_count=count), wait_for_dismiss=True
        )

        if confirmed:
            # Save position for refresh
            saved_position = self._save_table_position()

            from ..logging_config import get_logger

            logger = get_logger(__name__)

            success_count = 0
            failure_count = 0

            try:
                # Delete each transaction via API (with session renewal if needed)
                for txn_id in transaction_ids:
                    try:
                        await self.task_runner.delete_with_retry(txn_id)
                        success_count += 1
                    except Exception as e:
                        logger.error(f"Failed to delete transaction {txn_id}: {e}")
                        failure_count += 1

                # Update local DataFrame to remove deleted transactions
                if success_count > 0 and self.data_manager.df is not None:
                    # Remove deleted transactions from DataFrame
                    deleted_ids = transaction_ids[:success_count]
                    self.data_manager.df = self.data_manager.df.filter(
                        ~pl.col("id").is_in(deleted_ids)
                    )
                    self.state.transactions_df = self.data_manager.df

                    # Update cache to reflect deletions
                    if self.cache_manager:
                        try:
                            self.cache_manager.save_cache(
                                transactions_df=self.data_manager.df,
                                categories=self.data_manager.categories,
                                category_groups=self.data_manager.category_groups,
                                year=self.cache_year_filter,
                                since=self.cache_since_filter,
                            )
                        except Exception as e:
                            # Cache update failed - not critical, just log
                            logger.warning(f"Cache update after delete failed: {e}")

                # Clear selection
                self.state.clear_selection()

                # Show result notification
                if failure_count == 0:
                    self.notify(
                        f"Deleted {success_count} transaction(s)", severity="information", timeout=2
                    )
                else:
                    self.notify(
                        f"Deleted {success_count}, failed {failure_count}",
                        severity="warning",
                        timeout=3,
                    )

                # Refresh view to show updated data
                self.refresh_view()
                self._restore_table_position(saved_position)

            except Exception as e:
                self.notify(f"Error deleting: {e}", severity="error", timeout=5)

    def action_go_back(self) -> None:
        """
        Go back to previous view and restore cursor and scroll position.

        To clear search: Press / then Enter with empty search box.
        """
        success, cursor_position, scroll_y = self.state.go_back()
        if success:
            self.refresh_view()
            # Restore cursor and scroll position after DOM updates
            # Use set_timer to defer until table is fully rendered
            saved_position = {"cursor_row": cursor_position, "scroll_y": scroll_y}
            self.set_timer(0.01, lambda: self._restore_table_position(saved_position))

    def action_review_and_commit(self) -> None:
        """Review pending changes and commit if confirmed."""
        if self.data_manager is None:
            return

        count = self.data_manager.get_stats()["pending_changes"]
        if count == 0:
            self._notify(notification_helper.NO_PENDING_CHANGES)
            return

        # Show review screen
        self.run_worker(self._review_and_commit(), exclusive=False)

    async def _review_and_commit(self) -> None:
        """Show review screen and commit if confirmed."""
        logger = get_logger(__name__)

        # Save view state AND table position before showing review screen
        saved_state = self.state.save_view_state()
        saved_table_position = self._save_table_position()
        logger.debug(
            f"Saved view state: view_mode={saved_state['view_mode']}, selected_category={saved_state.get('selected_category')}"
        )
        logger.debug(
            f"Saved table position: cursor_row={saved_table_position['cursor_row']}, scroll_y={saved_table_position['scroll_y']}"
        )

        # Show review screen with category names for readable display
        should_commit = await self.push_screen(
            ReviewChangesScreen(self.data_manager.pending_edits, self.data_manager.categories),
            wait_for_dismiss=True,
        )

        if should_commit:
            # Restore view IMMEDIATELY after review screen dismisses to avoid flash
            # User should see their original view while commits are happening
            logger.debug(f"Before restore: view_mode={self.state.view_mode}")
            self.state.restore_view_state(saved_state)
            logger.debug(
                f"After restore: view_mode={self.state.view_mode}, selected_category={self.state.selected_category}"
            )
            self.refresh_view(force_rebuild=False)
            # Restore table position after refresh
            self._restore_table_position(saved_table_position)

            # Check for batch scope mismatches (YNAB only)
            # This identifies merchant renames where batch update would affect more
            # transactions than the user has selected
            scope_mismatches = await self.data_manager.check_batch_scope(
                self.data_manager.pending_edits
            )

            # Track user choices: which renames should use individual updates instead of batch
            skip_batch_for: set[tuple[str, str]] = set()

            for (old_name, new_name), counts in scope_mismatches.items():
                choice = await self.push_screen(
                    BatchScopeScreen(
                        merchant_name=old_name,
                        selected_count=counts["selected"],
                        total_count=counts["total"],
                    ),
                    wait_for_dismiss=True,
                )

                if choice == "cancel":
                    # User cancelled - abort the entire commit
                    self._notify(notification_helper.COMMIT_CANCELLED)
                    return
                elif choice == "selected":
                    # User chose individual updates for this rename
                    skip_batch_for.add((old_name, new_name))
                # "all" → use batch (default behavior, nothing to track)

            count = len(self.data_manager.pending_edits)
            self._notify(notification_helper.commit_starting(count))

            try:
                (
                    success_count,
                    failure_count,
                    bulk_merchant_renames,
                ) = await self.task_runner.commit_with_retry(
                    self.data_manager.pending_edits, skip_batch_for=skip_batch_for
                )

                # Show notification based on results
                if failure_count > 0:
                    self._notify(notification_helper.commit_partial(success_count, failure_count))
                else:
                    self._notify(notification_helper.commit_success(success_count))

                # Delegate to controller for data integrity logic
                # Controller handles: apply edits if success, keep current view if failure
                cache_filters = (
                    {"year": self.cache_year_filter, "since": self.cache_since_filter}
                    if self.cache_manager
                    else None
                )

                # Detect if we're showing filtered data (--mtd, --year, --since).
                # When filtered, cache updates must use save_hot_cache() to preserve
                # the cold cache data.
                is_filtered_view = self.display_start_date is not None

                self.controller.handle_commit_result(
                    success_count=success_count,
                    failure_count=failure_count,
                    edits=self.data_manager.pending_edits,
                    saved_state=saved_state,
                    cache_filters=cache_filters,
                    bulk_merchant_renames=bulk_merchant_renames,
                    is_filtered_view=is_filtered_view,
                )
                # Restore table position after commit completes
                self._restore_table_position(saved_table_position)
            except Exception as e:
                self._notify(notification_helper.commit_error(str(e)))
                # View already restored above, just refresh to show current state
                self.refresh_view(force_rebuild=False)
                # Restore table position after error refresh
                self._restore_table_position(saved_table_position)
        else:
            # User pressed Escape - restore view state and refresh to go back to where they were
            self.state.restore_view_state(saved_state)
            self.refresh_view(force_rebuild=False)
            # Restore table position after cancel
            self._restore_table_position(saved_table_position)

    def action_quit_app(self) -> None:
        """Quit the application - show confirmation first."""
        # If we're in an error state (no data_manager), just exit immediately
        if self.data_manager is None:
            self.exit()
            return
        # Show confirmation in a worker (required for push_screen with wait_for_dismiss)
        self.run_worker(self._confirm_and_quit(), exclusive=False)

    async def _confirm_and_quit(self) -> None:
        """Show quit confirmation dialog and exit if confirmed."""
        has_changes = (
            (self.data_manager and self.data_manager.get_stats()["pending_changes"] > 0)
            if self.data_manager
            else False
        )

        should_quit = await self.push_screen(
            QuitConfirmationScreen(has_unsaved_changes=has_changes), wait_for_dismiss=True
        )

        if should_quit:
            self.exit()

    def on_data_table_row_highlighted(self, event: DataTable.RowHighlighted) -> None:
        """Handle row highlight (cursor movement)."""
        logger.debug(f"Row highlighted: visible={self.amazon_presentation._column_visible}")
        if not self.amazon_presentation._column_visible:
            return

        table = self.query_one("#data-table", DataTable)

        # Calculate visible row range from scroll position and viewport height
        # Each row is approximately 1 cell high (can be up to 2 for wrapped text)
        row_height = 1
        header_height = 1

        # Get viewport height (number of visible rows)
        viewport_height = table.size.height - header_height
        if viewport_height <= 0:
            viewport_height = 20  # Fallback

        # Calculate first visible row from scroll position
        first_visible = int(table.scroll_y / row_height)
        last_visible = first_visible + viewport_height

        # Add small buffer for smooth scrolling
        start_row = max(0, first_visible - 2)
        end_row = last_visible + 2

        # Schedule loading to avoid blocking
        self.set_timer(
            0.01,
            lambda: self.amazon_presentation.load_matches_for_rows(
                self.query_one("#data-table", DataTable),
                self.state.current_data,
                start_row,
                end_row,
            ),
        )

    async def on_data_table_row_selected(self, event: DataTable.RowSelected) -> None:
        """Handle row selection (Enter key)."""
        table = self.query_one("#data-table", DataTable)
        row_key = event.row_key
        row = table.get_row(row_key)
        item_name = str(row[0])

        # Check if we're in a sub-grouped view (drilled down with sub-grouping)
        if self.state.is_drilled_down() and self.state.sub_grouping_mode:
            # Drilling down from sub-grouped view - save to navigation history
            cursor_position = table.cursor_row
            scroll_y = table.scroll_y
            self.state.drill_down(item_name, cursor_position, scroll_y)
            self.refresh_view()

        elif self.state.view_mode in [
            ViewMode.MERCHANT,
            ViewMode.CATEGORY,
            ViewMode.GROUP,
            ViewMode.ACCOUNT,
            ViewMode.TIME,
        ]:
            # Drill down from top-level view - save cursor and scroll position for restoration on go_back
            from ..logging_config import get_logger

            logger = get_logger(__name__)

            cursor_position = table.cursor_row
            scroll_y = table.scroll_y
            logger.debug(f"Drilling down: saving cursor={cursor_position}, scroll_y={scroll_y}")
            self.state.drill_down(item_name, cursor_position, scroll_y)
            self.refresh_view()


def main():
    """Entry point for the TUI."""
    parser = argparse.ArgumentParser(
        description="moneyflow - Terminal UI for personal finance management"
    )
    parser.add_argument(
        "--year",
        type=int,
        metavar="YYYY",
        help="Only load transactions from this year onwards (e.g., --year 2025 loads from 2025-01-01 to now). Default: load all transactions.",
    )
    parser.add_argument(
        "--since",
        type=str,
        metavar="YYYY-MM-DD",
        help="Only load transactions from this date onwards (e.g., --since 2024-06-01). Overrides --year if both provided.",
    )
    parser.add_argument(
        "--mtd",
        action="store_true",
        help="Load month-to-date transactions (from 1st of current month to today). Fast startup for editing recent transactions. Overrides --year and --since.",
    )
    parser.add_argument(
        "--cache",
        type=str,
        nargs="?",
        const="",  # Use default location if flag given without path
        metavar="PATH",
        help="Enable caching. Optionally specify cache directory (default: ~/.moneyflow/cache/). Without this flag, always fetches fresh data.",
    )
    parser.add_argument(
        "--refresh",
        action="store_true",
        help="Force refresh from API, skip cache even if valid cache exists",
    )
    parser.add_argument(
        "--demo",
        action="store_true",
        help="Run in demo mode with sample data (no authentication required)",
    )

    args = parser.parse_args()

    # Initialize logging (file only - Textual swallows console output anyway)
    logger = setup_logging(console_output=False, config_dir=None)
    logger.info("Starting moneyflow application")

    # Determine start year or date range
    start_year = None
    custom_start_date = None

    if args.mtd:
        # Month-to-date: Load from 1st of current month to today

        today = date_type.today()
        first_of_month = date_type(today.year, today.month, 1)
        custom_start_date = first_of_month.strftime("%Y-%m-%d")
    elif args.since:
        custom_start_date = args.since
    elif args.year:
        start_year = args.year

    # Handle cache path
    # If --cache passed without path, use empty string (triggers default in CacheManager)
    # If --cache not passed at all, args.cache is None (no caching)
    cache_path = args.cache if hasattr(args, "cache") and args.cache is not None else None

    try:
        app = MoneyflowApp(
            start_year=start_year,
            custom_start_date=custom_start_date,
            demo_mode=args.demo,
            cache_path=cache_path,
            force_refresh=args.refresh,
        )

        app.run()
    except Exception:
        # Print full traceback to console
        print("\n" + "=" * 80, file=sys.stderr)
        print("FATAL ERROR - moneyflow TUI crashed!", file=sys.stderr)
        print("=" * 80, file=sys.stderr)
        traceback.print_exc(file=sys.stderr)
        print("\n" + "=" * 80, file=sys.stderr)
        print("Please report this error with the traceback above.", file=sys.stderr)
        print("=" * 80 + "\n", file=sys.stderr)
        sys.exit(1)


def launch_monarch_mode(
    year: Optional[int] = None,
    since: Optional[str] = None,
    mtd: bool = False,
    cache: Optional[str] = None,
    refresh: bool = False,
    demo: bool = False,
    config_dir: Optional[str] = None,
    theme: Optional[str] = None,
) -> None:
    """
    Launch moneyflow with default backend (Monarch Money).

    Args:
        year: Only load transactions from this year onwards
        since: Only load transactions from this date onwards (overrides year)
        mtd: Load month-to-date transactions only
        cache: Cache directory path (enables caching if provided, None to disable)
        refresh: Force refresh from API, skip cache
        demo: Run in demo mode with sample data
        config_dir: Config directory (None = ~/.moneyflow)
        theme: Override theme (temporary, doesn't modify config.yaml)
    """
    from datetime import date as date_type

    # Initialize logging
    logger = setup_logging(console_output=False, config_dir=config_dir)
    logger.info("Starting moneyflow with Monarch Money backend")
    if config_dir:
        logger.info(f"Using custom config directory: {config_dir}")

    # Determine start year or date range
    start_year = None
    custom_start_date = None

    if mtd:
        today = date_type.today()
        first_of_month = date_type(today.year, today.month, 1)
        custom_start_date = first_of_month.strftime("%Y-%m-%d")
    elif since:
        custom_start_date = since
    elif year:
        start_year = year

    try:
        app = MoneyflowApp(
            start_year=start_year,
            custom_start_date=custom_start_date,
            demo_mode=demo,
            cache_path=cache,
            force_refresh=refresh,
            config_dir=config_dir,
            theme_override=theme,
        )
        app.run()
    except Exception:
        print("\n" + "=" * 80, file=sys.stderr)
        print("FATAL ERROR - moneyflow TUI crashed!", file=sys.stderr)
        print("=" * 80, file=sys.stderr)
        traceback.print_exc(file=sys.stderr)
        print("\n" + "=" * 80, file=sys.stderr)
        print("Please report this error with the traceback above.", file=sys.stderr)
        print("=" * 80 + "\n", file=sys.stderr)
        sys.exit(1)


def launch_amazon_mode(
    db_path: Optional[str] = None,
    config_dir: Optional[str] = None,
    profile_dir: Optional[Path] = None,
) -> None:
    """
    Launch moneyflow in Amazon purchase analysis mode.

    Args:
        db_path: Path to Amazon SQLite database (default: ~/.moneyflow/amazon.db)
        config_dir: Config directory for loading categories (default: ~/.moneyflow)
        profile_dir: Profile directory for category inheritance (optional)

    Uses the AmazonBackend with data stored in SQLite.
    Data must be imported first using: moneyflow amazon import <csv>
    """
    from moneyflow.backends.amazon import AmazonBackend
    from moneyflow.tui.backend_config import get_backend_config

    # Initialize logging
    logger = setup_logging(console_output=False, config_dir=config_dir)
    logger.info("Starting moneyflow in Amazon mode")
    if config_dir:
        logger.info(f"Using custom config directory: {config_dir}")
    if profile_dir:
        logger.info(f"Using profile directory: {profile_dir}")

    try:
        # Create Amazon backend and config
        backend = AmazonBackend(db_path=db_path, config_dir=config_dir, profile_dir=profile_dir)
        config = get_backend_config("amazon")

        # Create MoneyflowApp in Amazon mode
        app = MoneyflowApp(
            demo_mode=False,
            backend=backend,
            config=config,
            profile_dir=profile_dir,
            backend_type="amazon",
        )
        app.title = "moneyflow [Amazon]"

        app.run()
    except Exception:
        print("\n" + "=" * 80, file=sys.stderr)
        print("FATAL ERROR - moneyflow Amazon mode crashed!", file=sys.stderr)
        print("=" * 80, file=sys.stderr)
        traceback.print_exc(file=sys.stderr)
        print("\n" + "=" * 80, file=sys.stderr)
        print("Please report this error with the traceback above.", file=sys.stderr)
        print("=" * 80 + "\n", file=sys.stderr)
        sys.exit(1)


if __name__ == "__main__":
    main()
