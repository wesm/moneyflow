"""
App state management with change tracking and undo support.

This module contains the central AppState class that holds all application state
including view mode, filters, selections, and pending edits. State should be data,
not operations - complex operations belong in separate service classes.
"""

from dataclasses import dataclass
from dataclasses import field as dataclass_field
from datetime import date, datetime
from enum import Enum
from typing import Any, Dict, List, Optional

import polars as pl


class ViewMode(Enum):
    """Available view modes for transaction aggregation."""

    MERCHANT = "merchant"
    CATEGORY = "category"
    GROUP = "group"
    ACCOUNT = "account"
    TIME = "time"
    DETAIL = "detail"


class SortMode(Enum):
    """Sorting options for transactions."""

    COUNT = "count"
    AMOUNT = "amount"
    DATE = "date"
    MERCHANT = "merchant"
    CATEGORY = "category"
    GROUP = "group"
    ACCOUNT = "account"
    TIME_PERIOD = "time_period"


class SortDirection(Enum):
    """Sort direction."""

    DESC = "desc"
    ASC = "asc"


class TimeGranularity(Enum):
    """Granularity for time-based aggregation."""

    YEAR = "year"
    MONTH = "month"
    DAY = "day"


@dataclass
class NavigationState:
    """
    Represents saved navigation state for back navigation.

    When drilling down into a view, we save the current state so we can restore
    it when the user presses Escape. This includes view mode, drill-down selections,
    sub-grouping mode, cursor/scroll position, and sort preferences.
    """

    view_mode: ViewMode
    cursor_position: int = 0
    scroll_y: float = 0.0
    sort_by: SortMode = SortMode.AMOUNT
    sort_direction: SortDirection = SortDirection.DESC
    # Drill-down context
    selected_merchant: Optional[str] = None
    selected_category: Optional[str] = None
    selected_group: Optional[str] = None
    selected_account: Optional[str] = None
    selected_time_year: Optional[int] = None
    selected_time_month: Optional[int] = None
    selected_time_day: Optional[int] = None
    sub_grouping_mode: Optional[ViewMode] = None
    time_granularity: TimeGranularity = TimeGranularity.YEAR


@dataclass
class TransactionEdit:
    """
    Represents a pending transaction edit.

    Tracks a single change to a transaction (merchant, category, or hide flag)
    before it's committed to the backend API.
    """

    transaction_id: str
    field: str  # 'merchant', 'category', 'hide_from_reports'
    old_value: Any
    new_value: Any
    timestamp: datetime = dataclass_field(default_factory=datetime.now)  # When edit was queued


@dataclass
class AppState:
    """
    Central application state container.

    This class holds all state for the TUI application including:
    - Transaction data (Polars DataFrame)
    - View configuration (mode, sorting, time filters)
    - Navigation state (selected items, drill-down context)
    - Pending edits (before commit to API)
    - Search and filter settings

    The state is designed to be serializable and supports view state
    save/restore for complex navigation workflows (e.g., during commit review).

    Note: This class should primarily hold DATA, not implement complex operations.
    Business logic belongs in service classes (DataManager, FilterService, etc.).
    """

    # Data
    transactions_df: Optional[pl.DataFrame] = None
    categories: Dict[str, Any] = dataclass_field(default_factory=dict)
    category_groups: Dict[str, Any] = dataclass_field(default_factory=dict)
    merchants: Dict[str, Any] = dataclass_field(default_factory=dict)

    # View state
    view_mode: ViewMode = ViewMode.MERCHANT
    sort_by: SortMode = SortMode.AMOUNT  # What to sort by (count/amount/date)
    sort_direction: SortDirection = SortDirection.DESC  # Direction (asc/desc)

    # Time filtering
    start_date: Optional[date] = None
    end_date: Optional[date] = None

    # Navigation
    selected_merchant: Optional[str] = None
    selected_category: Optional[str] = None
    selected_group: Optional[str] = None
    selected_account: Optional[str] = None
    selected_time_year: Optional[int] = None  # Drill-down: which year
    selected_time_month: Optional[int] = None  # Drill-down: which month (1-12)
    selected_time_day: Optional[int] = None  # Drill-down: which day (1-31)
    selected_row: int = 0

    # Sub-grouping when drilled down (e.g., "Merchant > Amazon (by Category)")
    # When set, shows aggregated view of filtered data instead of detail view
    # Cycles: Category → Group → Account → Time → None (detail) → Category...
    sub_grouping_mode: Optional[ViewMode] = None

    # Time dimension
    time_granularity: TimeGranularity = TimeGranularity.YEAR

    # Multi-select for bulk operations
    selected_ids: set[str] = dataclass_field(default_factory=set)  # Transaction IDs in detail view
    selected_group_keys: set[str] = dataclass_field(
        default_factory=set
    )  # Group names in aggregate views

    # Search/filter
    search_query: str = ""
    show_transfers: bool = False  # Whether to show Transfer category transactions
    show_hidden: bool = True  # Whether to show transactions hidden from reports

    # Track navigation state when search was applied (for smart Escape behavior)
    # If user hasn't navigated since search, Escape clears search
    # If user has navigated deeper, Escape does normal back navigation
    search_navigation_state: Optional[tuple] = None  # (depth, sub_grouping_mode)

    # Change tracking
    pending_edits: List[TransactionEdit] = dataclass_field(default_factory=list)
    undo_stack: List[TransactionEdit] = dataclass_field(default_factory=list)

    # UI state
    loading: bool = False
    error_message: Optional[str] = None
    status_message: Optional[str] = None

    # Current view data (for display)
    current_data: Optional[pl.DataFrame] = None
    # Date range of current view data (for breadcrumb display)
    current_data_start_date: Optional[date] = None
    current_data_end_date: Optional[date] = None

    # Navigation history for breadcrumb and back navigation
    # Stores NavigationState objects for restoring state on go_back
    navigation_history: List[NavigationState] = dataclass_field(default_factory=list)

    def add_edit(self, transaction_id: str, field: str, old_value: Any, new_value: Any):
        """Add a pending edit to the change tracker."""
        edit = TransactionEdit(
            transaction_id=transaction_id, field=field, old_value=old_value, new_value=new_value
        )
        self.pending_edits.append(edit)
        self.undo_stack.append(edit)

    def undo_last_edit(self) -> Optional[TransactionEdit]:
        """Undo the last edit."""
        if not self.undo_stack:
            return None

        edit = self.undo_stack.pop()

        # Remove from pending edits
        if edit in self.pending_edits:
            self.pending_edits.remove(edit)

        return edit

    def clear_pending_edits(self):
        """Clear all pending edits after successful commit."""
        self.pending_edits.clear()
        self.undo_stack.clear()

    def has_unsaved_changes(self) -> bool:
        """Check if there are unsaved changes."""
        return len(self.pending_edits) > 0

    def toggle_selection(self, transaction_id: str):
        """Toggle selection of a transaction for bulk operations."""
        if transaction_id in self.selected_ids:
            self.selected_ids.remove(transaction_id)
        else:
            self.selected_ids.add(transaction_id)

    def toggle_group_selection(self, group_key: str):
        """Toggle selection of a group (merchant/category/etc) for bulk operations."""
        if group_key in self.selected_group_keys:
            self.selected_group_keys.remove(group_key)
        else:
            self.selected_group_keys.add(group_key)

    def clear_selection(self):
        """Clear all selected transactions and groups."""
        self.selected_ids.clear()
        self.selected_group_keys.clear()

    def clear_drill_down_and_selection(self):
        """
        Clear all drill-down filters and selections.

        This is a common operation when switching views or returning to top-level.
        Clears:
        - All drill-down filters (merchant, category, group, account, time)
        - Multi-select state (transaction IDs and group keys)
        """
        self.selected_merchant = None
        self.selected_category = None
        self.selected_group = None
        self.selected_account = None
        self.selected_time_year = None
        self.selected_time_month = None
        self.clear_selection()

    def is_time_period_selected(self) -> bool:
        """Check if drilled into a specific time period."""
        return self.selected_time_year is not None

    def get_selected_time_period(self) -> Optional[tuple[int, Optional[int]]]:
        """
        Get selected time period as (year, month) tuple.

        Returns:
            (year, month) if drilled into time, None otherwise.
            month will be None if only year is selected.
        """
        if self.selected_time_year is None:
            return None
        return (self.selected_time_year, self.selected_time_month)

    def clear_time_selection(self):
        """Clear time period drill-down."""
        self.selected_time_year = None
        self.selected_time_month = None
        self.selected_time_day = None

    def toggle_time_granularity(self) -> str:
        """
        Cycle through time granularities: Year → Month → Day → Year.

        Returns:
            Friendly name of new granularity ("Years", "Months", or "Days")
        """
        if self.time_granularity == TimeGranularity.YEAR:
            self.time_granularity = TimeGranularity.MONTH
            return "Months"
        elif self.time_granularity == TimeGranularity.MONTH:
            self.time_granularity = TimeGranularity.DAY
            return "Days"
        else:  # DAY
            self.time_granularity = TimeGranularity.YEAR
            return "Years"

    def navigate_time_period(self, direction: int) -> Optional[str]:
        """
        Navigate to adjacent time period (for arrow key navigation).

        Only works when drilled into a specific time period. Navigates based
        on current granularity:
        - YEAR: Navigate between years
        - MONTH: Navigate between months (wraps around year boundaries)
        - DAY: Navigate between days (wraps around month/year boundaries)

        Args:
            direction: -1 for previous period, +1 for next period

        Returns:
            Description of new period (e.g., "2024", "Mar 2024", "2024-03-15"), or None if
            not drilled into a time period

        Examples:
            >>> state = AppState()
            >>> state.selected_time_year = 2024
            >>> state.time_granularity = TimeGranularity.YEAR
            >>> state.navigate_time_period(1)  # Next year
            '2025'
            >>> state.selected_time_year
            2025

            >>> state.selected_time_year = 2024
            >>> state.selected_time_month = 12
            >>> state.time_granularity = TimeGranularity.MONTH
            >>> state.navigate_time_period(1)  # Next month (wraps to next year)
            'Jan 2025'
        """
        if not self.is_time_period_selected():
            return None

        # Type assertion: is_time_period_selected() ensures selected_time_year is not None
        assert self.selected_time_year is not None

        if self.time_granularity == TimeGranularity.YEAR:
            # Navigate years
            self.selected_time_year += direction
            return str(self.selected_time_year)
        elif self.time_granularity == TimeGranularity.MONTH:
            # Navigate months (handle year boundaries)
            if self.selected_time_month is None:
                # Shouldn't happen, but default to January if month not set
                self.selected_time_month = 1

            new_month = self.selected_time_month + direction

            if new_month < 1:
                # Wrap to previous year December
                self.selected_time_year -= 1
                self.selected_time_month = 12
            elif new_month > 12:
                # Wrap to next year January
                self.selected_time_year += 1
                self.selected_time_month = 1
            else:
                self.selected_time_month = new_month

            # Format as "Mon YYYY"
            month_names = [
                "Jan",
                "Feb",
                "Mar",
                "Apr",
                "May",
                "Jun",
                "Jul",
                "Aug",
                "Sep",
                "Oct",
                "Nov",
                "Dec",
            ]
            return f"{month_names[self.selected_time_month - 1]} {self.selected_time_year}"
        else:  # DAY
            # Navigate days (handle month/year boundaries)
            if self.selected_time_month is None or self.selected_time_day is None:
                # Default to first day of current year/month if not set
                self.selected_time_month = 1
                self.selected_time_day = 1

            # Use date class to handle day navigation properly
            from datetime import timedelta

            current_date = date(
                self.selected_time_year, self.selected_time_month, self.selected_time_day
            )
            new_date = current_date + timedelta(days=direction)

            # Update state with new date
            self.selected_time_year = new_date.year
            self.selected_time_month = new_date.month
            self.selected_time_day = new_date.day

            # Format as ISO date
            return str(new_date)

    def reverse_sort(self):
        """Reverse the current sort direction."""
        if self.sort_direction == SortDirection.DESC:
            self.sort_direction = SortDirection.ASC
        else:
            self.sort_direction = SortDirection.DESC

    def toggle_sort_field(self):
        """Toggle between sorting by count and amount."""
        if self.sort_by == SortMode.COUNT:
            self.sort_by = SortMode.AMOUNT
        else:
            self.sort_by = SortMode.COUNT

    def is_drilled_down(self) -> bool:
        """Check if we're currently drilled down into a specific item."""
        return any(
            [
                self.selected_merchant,
                self.selected_category,
                self.selected_group,
                self.selected_account,
                self.selected_time_year,
            ]
        )

    def get_navigation_depth(self) -> int:
        """
        Get current navigation depth (how many levels drilled down).

        Returns:
            0 = Top-level view (Merchants, Categories, etc.)
            1 = Drilled once (Merchant > Amazon)
            2 = Drilled twice (Merchant > Amazon > Groceries)
            etc.
        """
        if self.view_mode != ViewMode.DETAIL:
            return 0

        depth = 0
        if self.selected_merchant:
            depth += 1
        if self.selected_category:
            depth += 1
        if self.selected_group:
            depth += 1
        if self.selected_account:
            depth += 1

        return depth

    def get_navigation_state(self) -> tuple:
        """Get current navigation state for comparison."""
        return (self.get_navigation_depth(), self.sub_grouping_mode)

    def set_search(self, query: str) -> None:
        """
        Set search query and save current navigation state.

        Args:
            query: Search query string
        """
        self.search_query = query
        # Save navigation state when search is applied
        if query:
            self.search_navigation_state = self.get_navigation_state()
        else:
            self.search_navigation_state = None

    def cycle_sub_grouping(self) -> str:
        """
        Cycle through sub-grouping modes when drilled down.

        Order: MERCHANT → CATEGORY → GROUP → ACCOUNT → TIME → None (detail) → MERCHANT

        Skips the field we're already drilled into (e.g., if drilled into Category > Groceries,
        don't offer Category as sub-grouping since we're already filtered by that).

        When entering sub-grouping mode (transitioning from None to a mode), saves
        current state to navigation history so go_back() can restore it.

        When cycling between sub-groupings, validates that the current sort field is
        compatible with the new sub-grouping mode:
        - COUNT and AMOUNT are always valid
        - DATE is only valid in detail view (None)
        - Aggregate field sorts (MERCHANT/CATEGORY/GROUP/ACCOUNT) are only valid
          when they match the new sub-grouping mode
        - If invalid, falls back to AMOUNT sort

        Returns:
            Name of the new sub-grouping mode for notification
        """
        # Define cycle order (excluding the field we drilled into)
        available_modes = []

        # Add modes based on what we're NOT already filtered by
        # Check active selections, not view_mode (which is DETAIL when drilled down)
        if not self.selected_merchant:
            available_modes.append(ViewMode.MERCHANT)
        if not self.selected_category:
            available_modes.append(ViewMode.CATEGORY)
        if not self.selected_group:
            available_modes.append(ViewMode.GROUP)
        if not self.selected_account:
            available_modes.append(ViewMode.ACCOUNT)
        if not self.selected_time_year:
            available_modes.append(ViewMode.TIME)

        # Add None for detail view
        available_modes.append(None)

        # Find current index
        try:
            current_idx = available_modes.index(self.sub_grouping_mode)
        except ValueError:
            current_idx = -1

        # Cycle to next
        next_idx = (current_idx + 1) % len(available_modes)
        new_mode = available_modes[next_idx]

        # Save current state to navigation history when entering sub-grouping mode
        # This allows go_back() to restore the sort state before sub-grouping
        if self.sub_grouping_mode is None and new_mode is not None:
            # Entering sub-grouping for the first time - save current detail view state
            self.navigation_history.append(
                NavigationState(
                    view_mode=self.view_mode,
                    cursor_position=0,  # Don't save cursor when sub-grouping
                    scroll_y=0.0,
                    sort_by=self.sort_by,
                    sort_direction=self.sort_direction,
                    selected_merchant=self.selected_merchant,
                    selected_category=self.selected_category,
                    selected_group=self.selected_group,
                    selected_account=self.selected_account,
                    sub_grouping_mode=self.sub_grouping_mode,
                )
            )

        # Reset sort to valid field if current sort is not compatible with new mode
        # COUNT and AMOUNT are always valid for all modes
        # DATE is only valid for detail view (new_mode is None)
        # Aggregate fields (MERCHANT, CATEGORY, GROUP, ACCOUNT) are only valid
        # when they match the new sub-grouping mode

        # Map of sub-grouping mode to valid aggregate field sort
        mode_to_sort = {
            ViewMode.MERCHANT: SortMode.MERCHANT,
            ViewMode.CATEGORY: SortMode.CATEGORY,
            ViewMode.GROUP: SortMode.GROUP,
            ViewMode.ACCOUNT: SortMode.ACCOUNT,
            ViewMode.TIME: SortMode.TIME_PERIOD,
        }

        # Check if current sort is valid for the new mode
        if new_mode is None:
            # Switching to detail view - all sorts are valid (including DATE)
            pass
        else:
            # Switching to an aggregate sub-grouping
            # Check if current sort is valid
            if self.sort_by in [SortMode.COUNT, SortMode.AMOUNT]:
                # Always valid
                pass
            elif self.sort_by == SortMode.DATE:
                # DATE is not valid for aggregate views, fall back to AMOUNT DESC
                self.sort_by = SortMode.AMOUNT
                self.sort_direction = SortDirection.DESC
            elif self.sort_by in [
                SortMode.MERCHANT,
                SortMode.CATEGORY,
                SortMode.GROUP,
                SortMode.ACCOUNT,
                SortMode.TIME_PERIOD,
            ]:
                # Aggregate field sort - only valid if it matches the new mode
                if self.sort_by != mode_to_sort.get(new_mode):
                    # Current sort doesn't match new mode's field
                    # For TIME mode, default to chronological ascending
                    if new_mode == ViewMode.TIME:
                        self.sort_by = SortMode.TIME_PERIOD
                        self.sort_direction = SortDirection.ASC
                    else:
                        # For non-TIME modes, fall back to AMOUNT DESC (highest spending first)
                        self.sort_by = SortMode.AMOUNT
                        self.sort_direction = SortDirection.DESC

        self.sub_grouping_mode = new_mode

        # Return display name
        if self.sub_grouping_mode is None:
            return "Detail"
        elif self.sub_grouping_mode == ViewMode.MERCHANT:
            return "by Merchant"
        elif self.sub_grouping_mode == ViewMode.CATEGORY:
            return "by Category"
        elif self.sub_grouping_mode == ViewMode.GROUP:
            return "by Group"
        elif self.sub_grouping_mode == ViewMode.ACCOUNT:
            return "by Account"
        elif self.sub_grouping_mode == ViewMode.TIME:
            if self.time_granularity == TimeGranularity.YEAR:
                return "by Year"
            elif self.time_granularity == TimeGranularity.MONTH:
                return "by Month"
            else:
                return "by Day"
        else:
            return ""

    def cycle_grouping(self) -> str:
        """
        Cycle through grouping modes.

        If drilled down: Cycle sub-groupings within current filter
        If in top-level detail view: Go back to previous aggregate view (or MERCHANT)
        If in aggregate view: Cycle top-level aggregation views

        When cycling views, if currently sorting by an aggregate field (MERCHANT,
        CATEGORY, GROUP, or ACCOUNT), the sort field is updated to match the new
        view's aggregate field. For example, sorting by MERCHANT in merchant view
        becomes sorting by CATEGORY when cycling to category view.

        COUNT and AMOUNT sorts are preserved across all aggregate views.

        Returns:
            Name of the new view mode for notification
        """
        # If drilled down, cycle sub-grouping instead
        if self.is_drilled_down():
            return self.cycle_sub_grouping()

        # If in top-level detail view, go back to aggregate view (like Escape)
        if self.view_mode == ViewMode.DETAIL:
            # Try to restore from navigation history first
            if self.navigation_history:
                nav_state = self.navigation_history.pop()
                self.view_mode = nav_state.view_mode
                self.sort_by = nav_state.sort_by
                self.sort_direction = nav_state.sort_direction
                # Restore any drill-down context from history
                self.selected_merchant = nav_state.selected_merchant
                self.selected_category = nav_state.selected_category
                self.selected_group = nav_state.selected_group
                self.selected_account = nav_state.selected_account
                self.sub_grouping_mode = nav_state.sub_grouping_mode
                # Return friendly name for the restored view
                view_names = {
                    ViewMode.MERCHANT: "Merchants",
                    ViewMode.CATEGORY: "Categories",
                    ViewMode.GROUP: "Groups",
                    ViewMode.ACCOUNT: "Accounts",
                    ViewMode.TIME: "Years"
                    if nav_state.time_granularity == TimeGranularity.YEAR
                    else "Months",
                }
                return view_names.get(nav_state.view_mode, "")
            else:
                # No history - default to merchant view
                self.view_mode = ViewMode.MERCHANT
                return "Merchants"

        # Clear any drill-down selections when switching views
        self.selected_merchant = None
        self.selected_category = None
        self.selected_group = None
        self.selected_account = None
        self.selected_time_year = None
        self.selected_time_month = None
        self.sub_grouping_mode = None  # Clear sub-grouping too

        # Reset sort to valid field for aggregate views if needed
        # Now includes field-based sorting (MERCHANT, CATEGORY, GROUP, ACCOUNT, TIME_PERIOD)
        if self.sort_by not in [
            SortMode.COUNT,
            SortMode.AMOUNT,
            SortMode.MERCHANT,
            SortMode.CATEGORY,
            SortMode.GROUP,
            SortMode.ACCOUNT,
            SortMode.TIME_PERIOD,
        ]:
            self.sort_by = SortMode.AMOUNT

        # Check if currently sorting by an aggregate field (not COUNT/AMOUNT)
        # If so, we'll update it to match the new view's aggregate field
        is_sorting_by_aggregate_field = self.sort_by in [
            SortMode.MERCHANT,
            SortMode.CATEGORY,
            SortMode.GROUP,
            SortMode.ACCOUNT,
            SortMode.TIME_PERIOD,
        ]

        # Cycle through views and reset sort to appropriate default
        if self.view_mode == ViewMode.MERCHANT:
            self.view_mode = ViewMode.CATEGORY
            if is_sorting_by_aggregate_field:
                self.sort_by = SortMode.CATEGORY
            return "Categories"
        elif self.view_mode == ViewMode.CATEGORY:
            self.view_mode = ViewMode.GROUP
            if is_sorting_by_aggregate_field:
                self.sort_by = SortMode.GROUP
            return "Groups"
        elif self.view_mode == ViewMode.GROUP:
            self.view_mode = ViewMode.ACCOUNT
            if is_sorting_by_aggregate_field:
                self.sort_by = SortMode.ACCOUNT
            return "Accounts"
        elif self.view_mode == ViewMode.ACCOUNT:
            self.view_mode = ViewMode.TIME
            # TIME view always defaults to chronological order (period ASC)
            self.sort_by = SortMode.TIME_PERIOD
            self.sort_direction = SortDirection.ASC
            if self.time_granularity == TimeGranularity.YEAR:
                return "Years"
            elif self.time_granularity == TimeGranularity.MONTH:
                return "Months"
            else:
                return "Days"
        elif self.view_mode == ViewMode.TIME:
            self.view_mode = ViewMode.MERCHANT
            # When leaving TIME view, reset to default: highest spending first
            self.sort_by = SortMode.AMOUNT
            self.sort_direction = SortDirection.DESC
            return "Merchants"

        return ""

    def get_filtered_df(self) -> Optional[pl.DataFrame]:
        """
        Get filtered DataFrame based on current state.

        Applies multiple filters in sequence:
        1. Time range filter (start_date/end_date)
        2. Search query filter (merchant/category text search)
        3. Group filter (hide Transfers unless enabled)
        4. Hidden transactions filter (hide if show_hidden=False, but ONLY in aggregate views)
           - Detail views always show hidden transactions for review
        5. Drill-down filter (if viewing specific merchant/category/etc)

        Returns:
            Filtered DataFrame or None if no data loaded

        Note: This method contains business logic (Polars operations) that
        ideally should be extracted to a FilterService for better testability.
        """
        if self.transactions_df is None:
            return None

        df = self.transactions_df

        # Handle empty DataFrame (0 transactions) - return early to avoid column errors
        if len(df) == 0:
            return df

        # Apply time filter
        if self.start_date and self.end_date:
            df = df.filter((pl.col("date") >= self.start_date) & (pl.col("date") <= self.end_date))

        # Apply search filter
        if self.search_query:
            query = self.search_query.lower()
            df = df.filter(
                pl.col("merchant").str.to_lowercase().str.contains(query)
                | pl.col("category").str.to_lowercase().str.contains(query)
            )

        # Apply group filter (hide Transfers unless enabled)
        if not self.show_transfers:
            df = df.filter(pl.col("group") != "Transfers")

        # Apply hidden filter ONLY for aggregate views
        # Detail views should always show hidden transactions so users can review them
        if not self.show_hidden and self.view_mode != ViewMode.DETAIL:
            df = df.filter(~pl.col("hideFromReports"))

        # Apply time period drill-down filter (can combine with other dimensions)
        if self.selected_time_year is not None:
            df = df.filter(pl.col("date").dt.year() == self.selected_time_year)

            if self.selected_time_month is not None:
                df = df.filter(pl.col("date").dt.month() == self.selected_time_month)

                if self.selected_time_day is not None:
                    df = df.filter(pl.col("date").dt.day() == self.selected_time_day)

        # Apply view-specific filters
        if self.view_mode == ViewMode.DETAIL:
            if self.selected_merchant:
                df = df.filter(pl.col("merchant") == self.selected_merchant)
            elif self.selected_category:
                df = df.filter(pl.col("category") == self.selected_category)
            elif self.selected_group:
                df = df.filter(pl.col("group") == self.selected_group)
            elif self.selected_account:
                df = df.filter(pl.col("account") == self.selected_account)

        return df

    def drill_down(self, item_name: str, cursor_position: int = 0, scroll_y: float = 0) -> None:
        """
        Drill down from aggregate view into transaction detail view.

        When viewing an aggregate (e.g., Merchants view) and user presses Enter
        on a row, this method saves the current view context to navigation history
        and transitions to DETAIL view filtered to that item.

        Args:
            item_name: The merchant/category/group/account name to drill into
            cursor_position: Current cursor row position to save for go_back()
            scroll_y: Current scroll position to save for go_back()

        Examples:
            >>> state = AppState()
            >>> state.view_mode = ViewMode.MERCHANT
            >>> state.drill_down("Amazon", cursor_position=5, scroll_y=100.0)
            >>> state.view_mode
            <ViewMode.DETAIL: 'detail'>
            >>> state.selected_merchant
            'Amazon'
            >>> nav = state.navigation_history[-1]  # doctest: +SKIP
            >>> nav.view_mode  # doctest: +SKIP
            <ViewMode.MERCHANT: 'merchant'>
        """
        # Save current state to history (full context for proper restoration)
        self.navigation_history.append(
            NavigationState(
                view_mode=self.view_mode,
                cursor_position=cursor_position,
                scroll_y=scroll_y,
                sort_by=self.sort_by,
                sort_direction=self.sort_direction,
                selected_merchant=self.selected_merchant,
                selected_category=self.selected_category,
                selected_group=self.selected_group,
                selected_account=self.selected_account,
                selected_time_year=self.selected_time_year,
                selected_time_month=self.selected_time_month,
                selected_time_day=self.selected_time_day,
                sub_grouping_mode=self.sub_grouping_mode,
                time_granularity=self.time_granularity,
            )
        )

        # Determine which field to set based on current view
        # If in sub-grouped view, use sub_grouping_mode to determine the field
        effective_view_mode = self.sub_grouping_mode if self.sub_grouping_mode else self.view_mode

        # Set the selected item and clear sub-grouping
        if effective_view_mode == ViewMode.MERCHANT:
            self.selected_merchant = item_name
            self.view_mode = ViewMode.DETAIL
            self.sub_grouping_mode = None
        elif effective_view_mode == ViewMode.CATEGORY:
            self.selected_category = item_name
            self.view_mode = ViewMode.DETAIL
            self.sub_grouping_mode = None
        elif effective_view_mode == ViewMode.GROUP:
            self.selected_group = item_name
            self.view_mode = ViewMode.DETAIL
            self.sub_grouping_mode = None
        elif effective_view_mode == ViewMode.ACCOUNT:
            self.selected_account = item_name
            self.view_mode = ViewMode.DETAIL
            self.sub_grouping_mode = None
        elif effective_view_mode == ViewMode.TIME:
            # Drilling into a time period (e.g., "2024", "Mar 2024", or "2024-03-15")
            if self.time_granularity == TimeGranularity.YEAR:
                # item_name is a year like "2024"
                self.selected_time_year = int(item_name)
                self.selected_time_month = None
                self.selected_time_day = None
            elif self.time_granularity == TimeGranularity.MONTH:
                # item_name is like "Mar 2024"
                parts = item_name.split()
                month_name = parts[0]
                year_str = parts[1]

                month_names = [
                    "Jan",
                    "Feb",
                    "Mar",
                    "Apr",
                    "May",
                    "Jun",
                    "Jul",
                    "Aug",
                    "Sep",
                    "Oct",
                    "Nov",
                    "Dec",
                ]
                self.selected_time_year = int(year_str)
                self.selected_time_month = month_names.index(month_name) + 1
                self.selected_time_day = None
            else:  # DAY
                # item_name is like "2024-03-15" (ISO format)
                from datetime import datetime

                parsed_date = datetime.strptime(item_name, "%Y-%m-%d").date()
                self.selected_time_year = parsed_date.year
                self.selected_time_month = parsed_date.month
                self.selected_time_day = parsed_date.day

            self.view_mode = ViewMode.DETAIL
            self.sub_grouping_mode = None
            # When drilling into time period, sort by amount DESC to show biggest expenses first
            self.sort_by = SortMode.AMOUNT
            self.sort_direction = SortDirection.DESC

        # Reset sort to valid field for detail view if needed
        # Detail views don't have 'count' or 'time_period' columns
        if self.sort_by == SortMode.COUNT:
            self.sort_by = SortMode.DATE
            self.sort_direction = SortDirection.DESC
        elif self.sort_by == SortMode.TIME_PERIOD:
            # TIME_PERIOD not valid for detail view
            self.sort_by = SortMode.AMOUNT
            self.sort_direction = SortDirection.DESC

    def go_back(self) -> tuple[bool, int, float]:
        """
        Go back to previous view.

        Priority order:
        1. If search is active and no navigation since search: Clear search
        2. If sub-grouping is active: Clear sub-grouping first (stay drilled down)
           - Restores sort state from navigation history (pops if entering sub-grouping saved state)
           - This undoes sort changes made by cycle_sub_grouping()
        3. If drilled down (no sub-grouping): Go back to parent view
        4. If at top-level: Do nothing

        Returns:
            Tuple of (success: bool, cursor_position: int, scroll_y: float)
            success=True if went back, False if already at root
            cursor_position=Row to restore cursor to (0 if none saved)
            scroll_y=Scroll position to restore (0.0 if none saved)
        """
        # Check if search should be cleared (highest priority)
        if self.search_query and self.search_navigation_state:
            current_state = self.get_navigation_state()
            # If we're still at the same navigation state as when search was applied
            if current_state == self.search_navigation_state:
                # Clear search instead of navigating
                self.search_query = ""
                self.search_navigation_state = None
                return True, 0, 0.0

        # If in sub-grouped view, clear sub-grouping first (stay drilled down)
        if self.is_drilled_down() and self.sub_grouping_mode:
            self.sub_grouping_mode = None
            # Restore sort state from navigation history
            # If the top entry is DETAIL view with sub_grouping_mode=None, it was saved
            # when entering sub-grouping mode, so we should pop it and restore from it
            if (
                self.navigation_history
                and self.navigation_history[-1].view_mode == ViewMode.DETAIL
                and self.navigation_history[-1].sub_grouping_mode is None
            ):
                nav_state = self.navigation_history.pop()
                self.sort_by = nav_state.sort_by
                self.sort_direction = nav_state.sort_direction
            return True, 0, 0.0

        # If we have navigation history, restore from it
        if self.navigation_history:
            nav_state = self.navigation_history.pop()
            # Restore full state from history
            self.view_mode = nav_state.view_mode
            self.sort_by = nav_state.sort_by
            self.sort_direction = nav_state.sort_direction
            self.selected_merchant = nav_state.selected_merchant
            self.selected_category = nav_state.selected_category
            self.selected_group = nav_state.selected_group
            self.selected_account = nav_state.selected_account
            self.selected_time_year = nav_state.selected_time_year
            self.selected_time_month = nav_state.selected_time_month
            self.selected_time_day = nav_state.selected_time_day
            self.sub_grouping_mode = nav_state.sub_grouping_mode
            self.time_granularity = nav_state.time_granularity
            return True, nav_state.cursor_position, nav_state.scroll_y

        # No navigation history - we're at top level or not drilled down
        # Fallback: clear filters one at a time (innermost first)
        if self.view_mode == ViewMode.DETAIL:
            # Clear deepest level first (non-time dimensions)
            if self.selected_account:
                self.selected_account = None
            elif self.selected_group:
                self.selected_group = None
            elif self.selected_category:
                self.selected_category = None
            elif self.selected_merchant:
                self.selected_merchant = None
            elif self.selected_time_day:
                # Clear day but keep year/month
                self.selected_time_day = None
            elif self.selected_time_month:
                # Clear month but keep year
                self.selected_time_month = None
            elif self.selected_time_year:
                # Clear year (outermost time dimension)
                self.selected_time_year = None
            else:
                # No selections to clear, go to default view
                self.view_mode = ViewMode.MERCHANT
                return True, 0, 0.0

            # If no more selections after clearing, return to aggregate view
            if not self.is_drilled_down():
                # Determine which aggregate view to return to based on what was cleared
                self.view_mode = ViewMode.MERCHANT  # Default to merchant view

            return True, 0, 0.0

        # Already at top-level view
        return False, 0, 0.0

    def save_view_state(self) -> dict:
        """
        Save complete view state for later restoration.

        Saves everything that defines the current view including:
        - View mode and drill-down selections
        - Sort settings (column and direction)
        - Time filtering (start_date and end_date)
        - Search query
        - Filter settings (show_transfers, show_hidden)

        Used during commit review workflow to preserve the exact user context
        and return to it seamlessly after commit or cancel.
        """
        return {
            "view_mode": self.view_mode,
            "selected_merchant": self.selected_merchant,
            "selected_category": self.selected_category,
            "selected_group": self.selected_group,
            "selected_account": self.selected_account,
            "sort_by": self.sort_by,
            "sort_direction": self.sort_direction,
            "start_date": self.start_date,
            "end_date": self.end_date,
            "search_query": self.search_query,
            "show_transfers": self.show_transfers,
            "show_hidden": self.show_hidden,
        }

    def restore_view_state(self, saved_state: dict) -> None:
        """Restore complete view state including all filters and sort settings."""
        self.view_mode = saved_state["view_mode"]
        self.selected_merchant = saved_state["selected_merchant"]
        self.selected_category = saved_state["selected_category"]
        self.selected_group = saved_state["selected_group"]
        self.selected_account = saved_state.get("selected_account")
        self.sort_by = saved_state.get("sort_by", self.sort_by)
        self.sort_direction = saved_state.get("sort_direction", self.sort_direction)
        self.start_date = saved_state.get("start_date", self.start_date)
        self.end_date = saved_state.get("end_date", self.end_date)
        self.search_query = saved_state.get("search_query", self.search_query)
        self.show_transfers = saved_state.get("show_transfers", self.show_transfers)
        self.show_hidden = saved_state.get("show_hidden", self.show_hidden)

    def _get_drill_down_order(self) -> List[str]:
        """
        Determine the order in which dimensions were drilled down.

        Returns a list of dimension names in the order they were selected.
        Used to display breadcrumbs in the correct hierarchical order.

        Returns:
            List of dimension names ('time', 'merchant', 'category', 'group', 'account')
            in the order they were selected during navigation.
        """
        # Check which dimensions are currently selected
        current_selections = {
            "time": self.selected_time_year is not None,
            "merchant": self.selected_merchant is not None,
            "category": self.selected_category is not None,
            "group": self.selected_group is not None,
            "account": self.selected_account is not None,
        }

        selected_dims = [k for k, v in current_selections.items() if v]

        # If only one or zero dimensions, order is simple
        if len(selected_dims) <= 1:
            return selected_dims

        # Multiple dimensions - determine order from navigation_history
        order = []

        # Iterate through navigation history to build the drill order
        for nav_state in self.navigation_history:
            # Check which dimensions were selected at this point in history
            hist_selections = {
                "time": nav_state.selected_time_year is not None,
                "merchant": nav_state.selected_merchant is not None,
                "category": nav_state.selected_category is not None,
                "group": nav_state.selected_group is not None,
                "account": nav_state.selected_account is not None,
            }

            # Find dimensions that are in hist_selections but not yet in order
            for dim, selected in hist_selections.items():
                if selected and dim not in order and current_selections[dim]:
                    order.append(dim)

        # Add dimensions that are in current state but not in history (most recent drill)
        for dim in selected_dims:
            if dim not in order:
                order.append(dim)

        return order

    def get_breadcrumb(self, display_labels: Optional[Dict[str, str]] = None) -> str:
        """
        Get breadcrumb string showing current navigation path.

        Args:
            display_labels: Optional dict with backend-specific display labels.
                           Keys: merchant, account, accounts
                           If None, uses default labels.
        """
        # Use display labels if provided, otherwise defaults
        if display_labels is None:
            display_labels = {
                "merchant": "Merchant",
                "account": "Account",
                "accounts": "Accounts",
            }

        merchants_label = display_labels.get("merchant", "Merchant") + "s"  # Pluralize
        accounts_label = display_labels.get("accounts", "Accounts")
        account_label = display_labels.get("account", "Account")

        parts = []

        # Helper to format date range suffix
        def get_date_range_suffix() -> str:
            """Get date range suffix if available and no time drill-down active."""
            if (
                self.current_data_start_date
                and self.current_data_end_date
                and self.selected_time_year is None
            ):
                return f" ({self.current_data_start_date} to {self.current_data_end_date})"
            return ""

        # Add view mode
        if self.view_mode == ViewMode.MERCHANT:
            parts.append(merchants_label + get_date_range_suffix())
        elif self.view_mode == ViewMode.CATEGORY:
            parts.append("Categories" + get_date_range_suffix())
        elif self.view_mode == ViewMode.GROUP:
            parts.append("Groups" + get_date_range_suffix())
        elif self.view_mode == ViewMode.ACCOUNT:
            parts.append(accounts_label + get_date_range_suffix())
        elif self.view_mode == ViewMode.TIME:
            # TIME view - show granularity and date range if available
            if self.time_granularity == TimeGranularity.YEAR:
                granularity_label = "Years"
            elif self.time_granularity == TimeGranularity.MONTH:
                granularity_label = "Months"
            else:
                granularity_label = "Days"
            if self.current_data_start_date and self.current_data_end_date:
                # Show date range in ISO-8601 format
                date_range = f"{self.current_data_start_date} to {self.current_data_end_date}"
                parts.append(f"{granularity_label} ({date_range})")
            else:
                parts.append(granularity_label)
        elif self.view_mode == ViewMode.DETAIL:
            # Show all drill-down levels in the order they were drilled
            drill_order = self._get_drill_down_order()

            # Helper to add dimension to breadcrumb
            def add_dimension(dim: str, is_first: bool):
                """
                Add a dimension to the breadcrumb parts.

                Uses abbreviated format "X: Value" to save horizontal space.
                Abbreviations: M: (Merchant), C: (Category), G: (Group), A: (Account), T: (Time)
                Custom labels get first letter (e.g., "Item Name" → "I:", "Order" → "O:")
                """
                if dim == "time":
                    # Format time period value
                    if self.selected_time_day is not None:
                        # Format as ISO date "2024-03-15"
                        assert self.selected_time_year is not None
                        assert self.selected_time_month is not None
                        time_value = f"{self.selected_time_year:04d}-{self.selected_time_month:02d}-{self.selected_time_day:02d}"
                    elif self.selected_time_month is not None:
                        # Format as "Mar 2024"
                        month_names = [
                            "Jan",
                            "Feb",
                            "Mar",
                            "Apr",
                            "May",
                            "Jun",
                            "Jul",
                            "Aug",
                            "Sep",
                            "Oct",
                            "Nov",
                            "Dec",
                        ]
                        time_value = (
                            f"{month_names[self.selected_time_month - 1]} {self.selected_time_year}"
                        )
                    else:
                        # Just year
                        assert self.selected_time_year is not None
                        time_value = str(self.selected_time_year)

                    # Use "T: value" format
                    parts.append(f"T: {time_value}")

                elif dim == "merchant":
                    # Use abbreviated form: first letter of merchant label
                    assert self.selected_merchant is not None
                    merchant_label = display_labels.get("merchant", "Merchant")
                    abbrev = merchant_label[0].upper()
                    parts.append(f"{abbrev}: {self.selected_merchant}")

                elif dim == "category":
                    # Use "C: value" format
                    assert self.selected_category is not None
                    parts.append(f"C: {self.selected_category}")

                elif dim == "group":
                    # Use "G: value" format
                    assert self.selected_group is not None
                    parts.append(f"G: {self.selected_group}")

                elif dim == "account":
                    # Use abbreviated form: first letter of account label
                    assert self.selected_account is not None
                    abbrev = account_label[0].upper()
                    parts.append(f"{abbrev}: {self.selected_account}")

            # Add dimensions in drill-down order
            for i, dim in enumerate(drill_order):
                add_dimension(dim, i == 0)

            # If no dimensions selected, show default
            if not drill_order:
                parts.append("All Transactions")

            # Add sub-grouping indicator if active
            if self.sub_grouping_mode:
                if self.sub_grouping_mode == ViewMode.MERCHANT:
                    parts.append(f"(by {display_labels.get('merchant', 'Merchant')})")
                elif self.sub_grouping_mode == ViewMode.CATEGORY:
                    parts.append("(by Category)")
                elif self.sub_grouping_mode == ViewMode.GROUP:
                    parts.append("(by Group)")
                elif self.sub_grouping_mode == ViewMode.ACCOUNT:
                    parts.append(f"(by {account_label})")
                elif self.sub_grouping_mode == ViewMode.TIME:
                    if self.time_granularity == TimeGranularity.YEAR:
                        granularity = "Year"
                    elif self.time_granularity == TimeGranularity.MONTH:
                        granularity = "Month"
                    else:
                        granularity = "Day"
                    parts.append(f"(by {granularity})")

        # Time is now shown via drill-down (Time > 2024), not as a filter indicator
        # The old time_frame display logic has been removed

        # Add search indicator if active
        if self.search_query:
            parts.append(f"Search: '{self.search_query}'")

        return " > ".join(parts) if parts else "Home"
