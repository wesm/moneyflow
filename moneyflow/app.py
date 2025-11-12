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
from datetime import datetime
from pathlib import Path
from typing import Any, Optional

import polars as pl
from textual.app import App, ComposeResult
from textual.binding import Binding
from textual.containers import Container, Horizontal, Vertical
from textual.reactive import reactive
from textual.widgets import DataTable, Footer, Header, LoadingIndicator, Static

from .account_manager import AccountManager
from .app_controller import AppController
from .backends import DemoBackend, get_backend
from .cache_manager import CacheManager
from .credentials import CredentialManager
from .data_manager import DataManager
from .duplicate_detector import DuplicateDetector
from .logging_config import get_logger, setup_logging
from .migration import (
    migrate_global_categories_to_profiles,
    migrate_legacy_amazon_db,
    migrate_legacy_credentials,
)
from .notification_helper import NotificationHelper
from .retry_logic import RetryAborted, retry_with_backoff

# Screen imports
from .screens.account_name_input_screen import AccountNameInputScreen
from .screens.account_selector_screen import AccountSelectorScreen
from .screens.credential_screens import (
    BackendSelectionScreen,
    CachePromptScreen,
    CredentialSetupScreen,
    CredentialUnlockScreen,
    FilterScreen,
    QuitConfirmationScreen,
)
from .screens.duplicates_screen import DuplicatesScreen
from .screens.edit_screens import DeleteConfirmationScreen, EditMerchantScreen, SelectCategoryScreen
from .screens.review_screen import ReviewChangesScreen
from .screens.search_screen import SearchScreen
from .screens.transaction_detail_screen import TransactionDetailScreen
from .state import AppState, ViewMode
from .textual_view import TextualViewPresenter
from .widgets.help_screen import HelpScreen


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

    # Use Path object to properly resolve CSS file location
    # __file__ is moneyflow/app.py, so parent/styles/moneyflow.tcss is correct
    CSS_PATH = str(Path(__file__).parent / "styles" / "moneyflow.tcss")

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
            self._notify(NotificationHelper.commit_success(10))

        Instead of:
            msg, severity, timeout = NotificationHelper.commit_success(10)
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
    ):
        super().__init__()
        self.demo_mode = demo_mode
        self.start_year = start_year

        # Backend configuration (for Amazon/YNAB/etc)
        # Import here to avoid circular dependency
        from moneyflow.backend_config import BackendConfig

        self.backend_config = config or BackendConfig.for_monarch()

        # Backend will be initialized in initialize_data() based on credentials
        # unless explicitly provided (e.g., for Amazon mode)
        self.backend = backend
        if backend is not None:
            # Backend provided externally (Amazon mode, etc.)
            pass
        elif demo_mode:
            # Default to 3 years of data (2023-2025) for showcasing multi-year TIME views
            self.backend = DemoBackend(start_year=start_year or 2023, years=3)
            self.title = "moneyflow [DEMO MODE]"
        else:
            # Backend will be set in initialize_data() based on credentials
            self.title = "moneyflow"

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
        self.cache_year_filter = None  # Track what filters the cache uses
        self.cache_since_filter = None
        self.config_dir = config_dir  # Custom config directory (None = default ~/.moneyflow)
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

            # Attempt to use saved session or show login prompt
            # Must run in a worker to use push_screen with wait_for_dismiss
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
            merchant_cache_dir = "/tmp/moneyflow_demo"
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

        # Initialize cache manager
        if self.cache_path is not None:
            # If profile_dir provided, use profile-scoped cache directory
            if profile_dir and self.cache_path == "":
                # User passed --cache without path, use default cache location
                # For multi-account: cache inside profile directory
                cache_dir = str(profile_dir / "cache")
            else:
                # User specified explicit cache path or using legacy mode
                cache_dir = self.cache_path

            self.cache_manager = CacheManager(cache_dir=cache_dir)

        # Initialize controller with view presenter pattern
        view = TextualViewPresenter(self)
        self.controller = AppController(view, self.state, self.data_manager, self.cache_manager)

    def _determine_date_range(self):
        """Determine date range based on CLI arguments.

        Returns:
            tuple: (start_date, end_date, cache_year_filter, cache_since_filter)
        """
        if self.custom_start_date:
            start_date = self.custom_start_date
            end_date = datetime.now().strftime("%Y-%m-%d")
            cache_year_filter = None
            cache_since_filter = self.custom_start_date
        elif self.start_year:
            start_date = f"{self.start_year}-01-01"
            end_date = datetime.now().strftime("%Y-%m-%d")
            cache_year_filter = self.start_year
            cache_since_filter = None
        else:
            # Fetch ALL transactions (no date filter for offline-first approach)
            start_date = None
            end_date = None
            cache_year_filter = None
            cache_since_filter = None

        return start_date, end_date, cache_year_filter, cache_since_filter

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

    async def _handle_credentials(self):
        """Handle credential unlock/setup flow.

        Returns:
            dict: Credentials dict or None if user exits
        """

        # Convert config_dir string to Path if provided
        config_path = Path(self.config_dir) if self.config_dir else None
        cred_manager = CredentialManager(config_dir=config_path)

        logger = get_logger(__name__)
        logger.debug(f"Credentials exist: {cred_manager.credentials_exist()}")

        if cred_manager.credentials_exist():
            # Show unlock screen
            result = await self.push_screen(CredentialUnlockScreen(), wait_for_dismiss=True)

            if result is None:
                # User chose to reset - show backend selection then setup screen
                backend_type = await self.push_screen(
                    BackendSelectionScreen(), wait_for_dismiss=True
                )
                if not backend_type:
                    self.exit()
                    return None

                creds = await self.push_screen(
                    CredentialSetupScreen(backend_type=backend_type), wait_for_dismiss=True
                )
                if not creds:
                    self.exit()
                    return None
                return creds
            else:
                return result
        else:
            # No credentials - show backend selection first, then setup screen
            backend_type = await self.push_screen(BackendSelectionScreen(), wait_for_dismiss=True)
            if not backend_type:
                self.exit()
                return None

            creds = await self.push_screen(
                CredentialSetupScreen(backend_type=backend_type), wait_for_dismiss=True
            )
            if not creds:
                self.exit()
                return None
            return creds

    async def _handle_account_selection(self):
        """
        Handle account selection flow for multi-account support.

        Shows account selector, handles add new account flow, and sets up profile.

        Returns:
            tuple: (account_id, profile_dir, credentials_dict) or (None, None, None) if user exits
                   account_id can be "demo" for demo mode
        """
        logger = get_logger(__name__)

        # Initialize account manager
        config_path = Path(self.config_dir) if self.config_dir else None

        # Check for legacy credentials and migrate if needed
        migrated = migrate_legacy_credentials(config_dir=config_path)
        if migrated:
            logger.info("Migrated legacy credentials to default profile")

        # Check for legacy Amazon database and migrate if needed
        amazon_migrated = migrate_legacy_amazon_db(config_dir=config_path)
        if amazon_migrated:
            logger.info("Migrated legacy Amazon database to amazon profile")

        # Migrate global config.yaml categories to profile-local configs
        categories_migrated = migrate_global_categories_to_profiles(config_dir=config_path)
        if categories_migrated:
            logger.info("Migrated global categories to profile-local configs")

        account_manager = AccountManager(config_dir=config_path)

        while True:  # Loop to handle "add new account" flow
            # Show account selector
            result = await self.push_screen(
                AccountSelectorScreen(config_dir=str(config_path) if config_path else None),
                wait_for_dismiss=True,
            )

            if result is None:
                # User chose to exit
                return None, None, None

            if result == "demo":
                # Demo mode - no account/credentials needed
                return "demo", None, None

            if result == "add_new":
                # Add new account flow
                new_account_info = await self._handle_add_new_account(account_manager)

                if new_account_info is None:
                    # User cancelled - return to account selector
                    continue

                # New account created - return its info
                account_id, profile_dir, creds = new_account_info
                return account_id, profile_dir, creds

            # result is an account_id - load that account
            account = account_manager.get_account(result)
            if account is None:
                logger.error(f"Account {result} not found in registry")
                # Show error and return to selector
                continue

            # Get profile directory for this account
            profile_dir = account_manager.get_profile_dir(account.id)

            # Amazon backend doesn't require credentials (local-only)
            if account.backend_type == "amazon":
                logger.info(f"Loading Amazon account {account.id} (no credentials needed)")
                return account.id, profile_dir, None

            # Load credentials for this account (if backend requires auth)
            cred_manager = CredentialManager(config_dir=config_path, profile_dir=profile_dir)

            if not cred_manager.credentials_exist():
                # Account exists in registry but has no credentials
                # (shouldn't happen, but handle gracefully)
                logger.warning(f"Account {account.id} has no credentials, prompting setup")

                # Show credential setup
                creds = await self.push_screen(
                    CredentialSetupScreen(
                        backend_type=account.backend_type, profile_dir=profile_dir
                    ),
                    wait_for_dismiss=True,
                )

                if not creds:
                    # User cancelled - return to account selector
                    continue

                return account.id, profile_dir, creds

            # Load existing credentials
            creds = await self.push_screen(
                CredentialUnlockScreen(profile_dir=profile_dir), wait_for_dismiss=True
            )

            if creds is None:
                # User chose to reset credentials or cancelled
                # For now, just return to account selector
                # TODO: Could add option to reset/delete account here
                continue

            # Success - return account info
            return account.id, profile_dir, creds

    async def _handle_add_new_account(self, account_manager: AccountManager):
        """
        Handle adding a new account.

        Args:
            account_manager: AccountManager instance

        Returns:
            tuple: (account_id, profile_dir, credentials) or None if cancelled
        """
        # Step 1: Get account name from user
        account_name = await self.push_screen(
            AccountNameInputScreen(
                backend_type="monarch"
            ),  # Placeholder, will get actual type next
            wait_for_dismiss=True,
        )

        if not account_name:
            return None  # User cancelled

        # Step 2: Select backend type
        backend_type = await self.push_screen(BackendSelectionScreen(), wait_for_dismiss=True)

        if not backend_type:
            return None  # User cancelled

        # Step 3: Create account profile
        try:
            account = account_manager.create_account(name=account_name, backend_type=backend_type)
        except ValueError as e:
            # Duplicate account ID - shouldn't happen with our ID generation, but handle it
            logger = get_logger(__name__)
            logger.error(f"Failed to create account: {e}")
            return None

        # Step 4: Get credentials (if backend requires auth)
        from .backend_config import BackendConfig

        backend_config = {
            "monarch": BackendConfig.for_monarch(),
            "ynab": BackendConfig.for_ynab(),
            "amazon": BackendConfig.for_amazon(),
            "demo": BackendConfig.for_demo(),
        }.get(backend_type, BackendConfig.for_monarch())

        # Get profile directory
        profile_dir = account_manager.get_profile_dir(account.id)

        if backend_config.requires_auth:
            # Show credential setup
            creds = await self.push_screen(
                CredentialSetupScreen(backend_type=backend_type, profile_dir=profile_dir),
                wait_for_dismiss=True,
            )

            if not creds:
                # User cancelled - delete the account we just created
                account_manager.delete_account(account.id)
                return None
        else:
            # Backend doesn't need credentials (Amazon, Demo)
            creds = {"backend_type": backend_type}

        return account.id, profile_dir, creds

    async def _login_with_retry(self, creds, loading_status):
        """Login with retry logic for robustness.

        Args:
            creds: Credentials dict
            loading_status: Loading status widget

        Returns:
            bool: True on success, False on failure
        """

        logger = get_logger(__name__)

        backend_type = creds.get("backend_type", "monarch")
        loading_status.update(f"🔐 Logging in to {backend_type.capitalize()}...")

        logger.debug(f"Starting login flow for {backend_type}")
        logger.debug(f"Email: {creds['email']}")
        logger.debug(f"Has MFA secret: {bool(creds.get('mfa_secret'))}")

        def on_login_retry(attempt: int, wait_seconds: float) -> None:
            """Show retry progress during login."""
            loading_status.update(
                f"⚠ Login failed. Retrying in {wait_seconds:.0f}s (attempt {attempt + 1}/5). Press Ctrl-C to abort."
            )

        async def login_operation():
            """Login with automatic retry on session expiration."""
            try:
                logger.debug("Attempting login with saved session...")
                await self.backend.login(
                    email=creds["email"],
                    password=creds["password"],
                    use_saved_session=True,  # Try saved session first
                    save_session=True,
                    mfa_secret_key=creds["mfa_secret"],
                )
                logger.debug("Login succeeded!")
                return True
            except Exception as e:
                logger.warning(f"Login failed: {e}", exc_info=True)
                error_str = str(e).lower()
                # Check if it's a stale session
                if "401" in error_str or "unauthorized" in error_str:
                    logger.debug("Detected stale session, performing fresh login")
                    # Use centralized fresh login logic
                    await self._do_fresh_login(creds)
                    return True
                # Not a session issue, re-raise for retry logic
                raise

        try:
            await retry_with_backoff(
                operation=login_operation,
                operation_name="Login to backend",
                max_retries=5,
                initial_wait=60.0,
                on_retry=on_login_retry,
            )
            # Store credentials for automatic session refresh if needed
            self.stored_credentials = creds
            loading_status.update("✅ Logged in successfully!")
            logger.debug("Login flow completed successfully")
            return True
        except RetryAborted:
            # User pressed Ctrl-C
            logger.debug("Login cancelled by user")
            loading_status.update("Login cancelled by user. Press 'q' to quit.")
            return False
        except Exception as e:
            # All retries exhausted
            logger.error(f"Login failed after all retries: {e}", exc_info=True)
            error_msg = f"Login failed: {e}"
            log_path = (
                f"{self.config_dir}/moneyflow.log"
                if self.config_dir
                else "~/.moneyflow/moneyflow.log"
            )
            loading_status.update(
                f"❌ {error_msg}\n\nCheck {log_path} for details.\n\nPress 'q' to quit"
            )
            return False

    async def _check_and_load_cache(self, loading_status):
        """Check if cache is valid and load from cache if user approves.

        Args:
            loading_status: Loading status widget

        Returns:
            tuple: (df, categories, category_groups) or None if not using cache
        """
        logger = get_logger(__name__)

        use_cache = False
        if (
            self.cache_manager
            and not self.force_refresh
            and self.cache_manager.is_cache_valid(
                year=self.cache_year_filter, since=self.cache_since_filter
            )
        ):
            # Cache is valid - show prompt
            cache_info = self.cache_manager.get_cache_info()
            if cache_info:
                use_cache = await self.push_screen(
                    CachePromptScreen(
                        age=cache_info["age"],
                        transaction_count=cache_info["transaction_count"],
                        filter_desc=cache_info["filter"],
                    ),
                    wait_for_dismiss=True,
                )

        if use_cache:
            # Load from cache
            loading_status.update("📦 Loading from cache...")
            result = self.cache_manager.load_cache()
            if result:
                df, categories, category_groups, metadata = result

                # Apply category grouping dynamically (so CATEGORY_GROUPS changes take effect)
                # Note: DataManager.__init__() already loaded config.yaml and built the mapping
                loading_status.update("🔄 Applying category groupings...")
                df = self.data_manager.apply_category_groups(df)

                # Load merchant cache for autocomplete (especially important for MTD mode)
                try:
                    cached_merchants = await self.data_manager.refresh_merchant_cache(force=False)
                    self.data_manager.all_merchants = cached_merchants
                    logger.debug(f"Loaded {len(cached_merchants)} merchants from cache")
                except Exception as e:
                    logger.warning(f"Merchant cache load failed: {e}")
                    self.data_manager.all_merchants = []

                loading_status.update(f"✅ Loaded {len(df):,} transactions from cache!")
                return df, categories, category_groups
            else:
                # Cache load failed, fall back to API
                loading_status.update("⚠ Cache load failed, fetching from API...")
                return None

        return None

    async def _fetch_data_with_retry(self, creds, start_date, end_date, loading_status):
        """Fetch data from API with retry logic.

        Args:
            creds: Credentials dict (may be None in demo mode)
            start_date: Start date for fetch
            end_date: End date for fetch
            loading_status: Loading status widget

        Returns:
            tuple: (df, categories, category_groups) or None on failure
        """

        logger = get_logger(__name__)

        # Update status based on date range
        if self.custom_start_date:
            loading_status.update(
                f"📊 Fetching transactions from {self.custom_start_date} onwards..."
            )
        elif self.start_year:
            loading_status.update(f"📊 Fetching transactions from {self.start_year} onwards...")
        else:
            loading_status.update("📊 Fetching ALL transaction data from backend...")

        loading_status.update("⏳ This may take a minute for large accounts (10k+ transactions)...")
        loading_status.update(
            "💡 TIP: This is a one-time download. Future operations will be instant!"
        )

        def update_progress(msg: str) -> None:
            """Update the loading status display."""
            loading_status.update(f"📊 {msg}")

        def on_fetch_retry(attempt: int, wait_seconds: float) -> None:
            """Show retry progress during data fetch."""
            loading_status.update(
                f"⚠ Data fetch failed. Retrying in {wait_seconds:.0f}s (attempt {attempt + 1}/5). Press Ctrl-C to abort."
            )

        async def fetch_operation():
            """Fetch data with automatic error logging."""
            try:
                logger.debug(f"Fetching transactions (start={start_date}, end={end_date})")
                result = await self.data_manager.fetch_all_data(
                    start_date=start_date, end_date=end_date, progress_callback=update_progress
                )
                logger.debug(f"Data fetch succeeded - loaded {len(result[0])} transactions")
                return result
            except Exception as e:
                logger.error(f"Data fetch failed: {e}", exc_info=True)
                # Check if session expiration
                error_str = str(e).lower()
                if ("401" in error_str or "unauthorized" in error_str) and creds:
                    logger.info("Session expired during fetch, attempting fresh login...")
                    loading_status.update("🔄 Session expired. Re-authenticating...")
                    # Use centralized fresh login logic
                    try:
                        await self._do_fresh_login(creds)
                        loading_status.update("✅ Re-authenticated. Retrying fetch...")
                        result = await self.data_manager.fetch_all_data(
                            start_date=start_date,
                            end_date=end_date,
                            progress_callback=update_progress,
                        )
                        logger.info(f"Fetch retry succeeded - loaded {len(result[0])} transactions")
                        return result
                    except Exception as reauth_error:
                        logger.error(f"Re-authentication failed: {reauth_error}", exc_info=True)
                        # Re-auth failed, let retry logic handle it with backoff
                        raise Exception(f"Session refresh failed: {reauth_error}")
                # Not auth error, re-raise for retry logic
                raise

        try:
            df, categories, category_groups = await retry_with_backoff(  # type: ignore
                operation=fetch_operation,
                operation_name="Fetch transaction data",
                max_retries=5,
                initial_wait=60.0,
                on_retry=on_fetch_retry,
            )

            # Save to cache for next time (only if --cache was passed)
            if self.cache_manager:
                loading_status.update("💾 Saving to cache...")
                self.cache_manager.save_cache(
                    transactions_df=df,
                    categories=categories,
                    category_groups=category_groups,
                    year=self.cache_year_filter,
                    since=self.cache_since_filter,
                )
                loading_status.update(f"✅ Loaded {len(df):,} transactions and cached!")
            else:
                loading_status.update(f"✅ Loaded {len(df):,} transactions!")

            return df, categories, category_groups
        except RetryAborted:
            logger.debug("Data fetch cancelled by user")
            loading_status.update("Data fetch cancelled. Press 'q' to quit.")
            return None
        except Exception as e:
            logger.error(f"Data fetch failed after all retries: {e}", exc_info=True)
            log_path = (
                f"{self.config_dir}/moneyflow.log"
                if self.config_dir
                else "~/.moneyflow/moneyflow.log"
            )
            loading_status.update(
                f"❌ Failed to load data: {e}\n\nCheck {log_path} for details.\n\nPress 'q' to quit"
            )
            return None

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
                    creds = await self._handle_credentials()
                    if creds is None:
                        return  # User exited
            else:
                # Normal multi-account flow
                account_id, profile_dir, creds = await self._handle_account_selection()

                if account_id is None:
                    # User chose to exit from account selector
                    self.exit()
                    return

                if account_id == "demo":
                    # User selected demo mode from account selector
                    self.demo_mode = True
                    self.backend = DemoBackend(start_year=self.start_year or 2023, years=3)
                    self.title = "moneyflow [DEMO MODE]"
                    loading_status.update("🎮 DEMO MODE - Loading sample data...")
                else:
                    # Load account info to get backend_type
                    from moneyflow.account_manager import AccountManager

                    config_path = Path(self.config_dir) if self.config_dir else None
                    account_manager = AccountManager(config_dir=config_path)
                    account = account_manager.get_account(account_id)

                    if account and account.backend_type == "amazon" and profile_dir:
                        # Initialize Amazon backend with profile-scoped database
                        from moneyflow.backend_config import BackendConfig
                        from moneyflow.backends.amazon import AmazonBackend

                        db_path = str(profile_dir / "amazon.db")
                        self.backend = AmazonBackend(
                            db_path=db_path, config_dir=self.config_dir, profile_dir=profile_dir
                        )
                        self.backend_config = BackendConfig.for_amazon()
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

                from moneyflow.backend_config import BackendConfig

                if backend_type == "ynab":
                    self.backend_config = BackendConfig.for_ynab()
                elif backend_type == "monarch":
                    self.backend_config = BackendConfig.for_monarch()
                elif backend_type == "amazon":
                    self.backend_config = BackendConfig.for_amazon()
                elif backend_type == "demo":
                    self.backend_config = BackendConfig.for_demo()

                # Step 3: Login with retry logic
                login_success = await self._login_with_retry(creds, loading_status)
                if not login_success:
                    has_error = True
                    return
            elif self.backend and not self.demo_mode:
                # Backend exists but might need login
                if self.backend_config.requires_auth and creds:
                    login_success = await self._login_with_retry(creds, loading_status)
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
                    # Get backend type from backend instance
                    backend_class = self.backend.__class__.__name__
                    if "Amazon" in backend_class:
                        determined_backend_type = "amazon"
                    elif "Monarch" in backend_class:
                        determined_backend_type = "monarch"
                    elif "YNAB" in backend_class:
                        determined_backend_type = "ynab"
                elif creds:
                    determined_backend_type = creds.get("backend_type")

            self._initialize_managers(
                profile_dir=determined_profile_dir, backend_type=determined_backend_type
            )

            # Step 4: Determine date range
            start_date, end_date, self.cache_year_filter, self.cache_since_filter = (
                self._determine_date_range()
            )

            # Step 5: Check and load cache
            cached_data = await self._check_and_load_cache(loading_status)

            if cached_data:
                df, categories, category_groups = cached_data
            else:
                # Step 6: Fetch from API with retry logic
                fetch_result = await self._fetch_data_with_retry(
                    creds, start_date, end_date, loading_status
                )
                if fetch_result is None:
                    has_error = True
                    return
                df, categories, category_groups = fetch_result

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
        from .logging_config import get_logger

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
        self.controller.refresh_view(force_rebuild=force_rebuild)

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
            self._notify(NotificationHelper.view_changed(view_name))

    def action_view_ungrouped(self) -> None:
        """Switch to ungrouped transactions view (all transactions in reverse chronological order)."""
        self.controller.switch_to_detail_view(set_default_sort=True)
        self._notify(NotificationHelper.all_transactions_view())

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

        # Get the timestamp of the most recent edit
        last_edit = self.data_manager.pending_edits[-1]
        last_timestamp = last_edit.timestamp

        # Count how many edits from the end have the same timestamp (bulk edit batch)
        # Bulk edits are queued in a single operation, so they have the same timestamp
        edits_to_undo = []
        for i in range(len(self.data_manager.pending_edits) - 1, -1, -1):
            edit = self.data_manager.pending_edits[i]
            if edit.timestamp == last_timestamp:
                edits_to_undo.append(edit)
            else:
                # Different timestamp - stop here
                break

        # Remove all edits from this batch (reverse order since we found them backwards)
        for edit in edits_to_undo:
            self.data_manager.pending_edits.remove(edit)

        # Refresh view to update indicators
        self.refresh_view(force_rebuild=False)

        # Restore cursor and scroll position
        self._restore_table_position(saved_position)

        # Show notification with what was undone
        count_undone = len(edits_to_undo)
        count_remaining = len(self.data_manager.pending_edits)
        field_name = last_edit.field.replace("_", " ").title()

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
                count = self.controller.apply_search(new_query)
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
            self._notify(NotificationHelper.edit_queued(count))
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
        if self.data_manager is None or self.state.view_mode != ViewMode.DETAIL:
            self.notify("Details only available in transaction view", timeout=2)
            return

        if self.state.current_data is None:
            return

        table = self.query_one("#data-table", DataTable)
        if table.cursor_row < 0:
            return

        # Get current transaction data
        row_data = self.state.current_data.row(table.cursor_row, named=True)

        # Show detail modal (doesn't change view state, just displays info)
        self.push_screen(TransactionDetailScreen(dict(row_data)))

    def action_delete_transaction(self) -> None:
        """Delete current transaction with confirmation."""
        if self.data_manager is None or self.state.view_mode != ViewMode.DETAIL:
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

            from .logging_config import get_logger

            logger = get_logger(__name__)

            success_count = 0
            failure_count = 0

            try:
                # Delete each transaction via API (with session renewal if needed)
                for txn_id in transaction_ids:
                    try:
                        await self._delete_with_retry(txn_id)
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

    async def _do_fresh_login(self, creds):
        """
        Delete stale session and perform fresh login.

        This is the common pattern used in 3 places (login, fetch, commit).
        Extracted to eliminate duplication while preserving the exact logic
        that evolved through multiple bug fixes.

        Args:
            creds: Credentials dict with email, password, mfa_secret

        Raises:
            Exception: If login fails
        """

        logger = get_logger(__name__)

        logger.info("Deleting stale session and performing fresh login")
        self.backend.delete_session()
        self.backend.clear_auth()  # Clear in-memory token/headers

        await self.backend.login(
            email=creds["email"],
            password=creds["password"],
            use_saved_session=False,  # Force fresh login
            save_session=True,
            mfa_secret_key=creds["mfa_secret"],
        )
        logger.info("Fresh login succeeded")

    async def _refresh_session(self) -> bool:
        """Refresh expired session by re-authenticating with stored credentials."""

        logger = get_logger(__name__)

        if self.stored_credentials is None:
            logger.error("Cannot refresh session - no stored credentials")
            return False

        try:
            logger.info("Session expired - attempting to refresh")
            self._notify(NotificationHelper.session_refreshing())
            # Use centralized fresh login logic
            await self._do_fresh_login(self.stored_credentials)
            logger.info("Session refresh succeeded")
            self._notify(NotificationHelper.session_refresh_success())
            return True
        except Exception as e:
            logger.error(f"Session refresh failed: {e}", exc_info=True)
            self._notify(NotificationHelper.session_refresh_failed(str(e)))
            return False

    async def _delete_with_retry(self, transaction_id: str) -> None:
        """
        Delete transaction with automatic retry on session expiration.

        Args:
            transaction_id: ID of transaction to delete

        Raises:
            Exception: If delete fails after session refresh attempt
        """
        logger = get_logger(__name__)

        try:
            await self.backend.delete_transaction(transaction_id)
        except Exception as e:
            # Check if it's an auth error (session expired)
            error_msg = str(e).lower()
            if "401" in error_msg or "unauthorized" in error_msg or "token" in error_msg:
                logger.debug("Delete failed with auth error, attempting session refresh")
                # Try to refresh session once
                if await self._refresh_session():
                    logger.debug("Session refreshed, retrying delete immediately")
                    # Session refreshed - try delete again immediately
                    await self.backend.delete_transaction(transaction_id)
                else:
                    logger.error("Session refresh failed during delete")
                    raise Exception("Session refresh failed - cannot delete transaction")
            else:
                # Re-raise other errors
                raise

    async def _commit_with_retry(self, edits):
        """
        Commit edits with automatic retry on session expiration.

        Uses exponential backoff (60s, 120s, 240s, 480s, 960s) for transient failures.
        User can press Ctrl-C to abort during retry waits.

        **User Experience:**
        - On auth error: "Session expired, re-authenticating..." → immediate retry
        - On other error: "Commit failed due to {reason}. Retrying in Xs (attempt N/5). Press Ctrl-C to abort."
        - On retry success: Returns normally (no extra notification)
        - On all retries exhausted: Re-raises exception (caller shows error)
        - On user cancel: "Commit cancelled by user"
        """

        logger = get_logger(__name__)

        def on_retry_notification(attempt: int, wait_seconds: float) -> None:
            """
            Show retry progress to user.

            Called AFTER the first failure and BEFORE waiting to retry.
            """
            self._notify(NotificationHelper.retry_waiting(attempt, wait_seconds))

        async def commit_operation():
            """Wrapper to commit and re-authenticate if needed."""
            try:
                return await self.data_manager.commit_pending_edits(edits)
            except Exception as e:
                # Check if it's an auth error (session expired)
                error_msg = str(e).lower()
                if "401" in error_msg or "unauthorized" in error_msg or "token" in error_msg:
                    logger.debug("Commit failed with auth error, attempting session refresh")
                    # Show clear message to user
                    self._notify(NotificationHelper.session_expired())
                    # Try to refresh session once
                    if await self._refresh_session():
                        logger.debug("Session refreshed, retrying commit immediately")
                        # Session refreshed - try commit again immediately
                        return await self.data_manager.commit_pending_edits(edits)
                    else:
                        logger.error("Session refresh failed")
                        # Session refresh failed - will trigger retry with backoff
                        raise Exception("Session refresh failed - will retry with backoff")
                # Re-raise for retry logic to handle
                logger.warning(f"Commit failed: {e}")
                raise

        try:
            # Use retry_with_backoff for robust error handling
            return await retry_with_backoff(
                operation=commit_operation,
                operation_name="Commit changes",
                max_retries=5,
                initial_wait=60.0,
                on_retry=on_retry_notification,
            )
        except RetryAborted:
            # User pressed Ctrl-C
            logger.debug("Commit retry cancelled by user")
            self._notify(NotificationHelper.retry_cancelled())
            raise
        except Exception as e:
            # All retries exhausted
            logger.error(f"All commit retries exhausted: {e}")
            raise

    def action_review_and_commit(self) -> None:
        """Review pending changes and commit if confirmed."""
        if self.data_manager is None:
            return

        count = self.data_manager.get_stats()["pending_changes"]
        if count == 0:
            self._notify(NotificationHelper.no_pending_changes())
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

            count = len(self.data_manager.pending_edits)
            self._notify(NotificationHelper.commit_starting(count))

            try:
                success_count, failure_count = await self._commit_with_retry(  # type: ignore
                    self.data_manager.pending_edits
                )

                # Show notification based on results
                if failure_count > 0:
                    self._notify(NotificationHelper.commit_partial(success_count, failure_count))
                else:
                    self._notify(NotificationHelper.commit_success(success_count))

                # Delegate to controller for data integrity logic
                # Controller handles: apply edits if success, keep current view if failure
                cache_filters = (
                    {"year": self.cache_year_filter, "since": self.cache_since_filter}
                    if self.cache_manager
                    else None
                )

                self.controller.handle_commit_result(
                    success_count=success_count,
                    failure_count=failure_count,
                    edits=self.data_manager.pending_edits,
                    saved_state=saved_state,
                    cache_filters=cache_filters,
                )
                # Restore table position after commit completes
                self._restore_table_position(saved_table_position)
            except Exception as e:
                self._notify(NotificationHelper.commit_error(str(e)))
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
            from .logging_config import get_logger

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
    from moneyflow.backend_config import BackendConfig
    from moneyflow.backends.amazon import AmazonBackend

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
        config = BackendConfig.for_amazon()

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
