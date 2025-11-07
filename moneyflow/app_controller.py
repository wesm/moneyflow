"""
Application controller - business logic without UI dependencies.

This module contains the AppController which orchestrates all business logic
for the application. It delegates all UI operations to an IViewPresenter,
making the business logic testable without requiring a UI.

The controller handles:
- View refresh logic (what to show, when to force rebuild)
- Navigation between views
- Commit workflow
- All business decisions

The controller does NOT:
- Render anything directly
- Know about Textual widgets
- Manage keyboard bindings (that's UI layer)
"""

from dataclasses import dataclass
from datetime import date as date_type
from datetime import datetime
from enum import Enum
from typing import List, Optional

import polars as pl

from .commit_orchestrator import CommitOrchestrator
from .data_manager import DataManager
from .formatters import ViewPresenter
from .logging_config import get_logger
from .state import (
    AppState,
    NavigationState,
    SortDirection,
    SortMode,
    TimeGranularity,
    TransactionEdit,
    ViewMode,
)
from .time_navigator import TimeNavigator
from .view_interface import IViewPresenter

logger = get_logger(__name__)


class EditMode(Enum):
    """
    Edit operation modes based on current view and selection state.

    Determines how an edit operation should be executed:
    - Which transactions to edit
    - What the current value is
    - How to display the operation to the user
    """

    AGGREGATE_SINGLE = (
        "aggregate_single"  # Press m on one merchant/category/group in aggregate view
    )
    AGGREGATE_MULTI = "aggregate_multi"  # Multi-select groups, then press m
    DETAIL_SINGLE = "detail_single"  # Press m on one transaction in detail view
    DETAIL_MULTI = "detail_multi"  # Multi-select transactions, then press m
    SUBGROUP_SINGLE = "subgroup_single"  # Press m in sub-grouped view (one row)
    SUBGROUP_MULTI = "subgroup_multi"  # Multi-select in sub-grouped view


@dataclass
class EditContext:
    """
    Context for an edit operation - encapsulates all information needed to execute an edit.

    This separates business logic (what to edit) from UI logic (how to show the modal).
    Makes edit operations testable without requiring the TUI.

    Attributes:
        mode: Type of edit operation (aggregate single/multi, detail single/multi, etc.)
        transactions: DataFrame of transactions to edit
        current_value: Current value of the field being edited (for display/validation)
        field_name: Name of field being edited ("merchant", "category", etc.)
        is_multi_select: Whether multiple items are selected
        transaction_count: Number of transactions affected
        group_field: For aggregate edits, which field groups transactions (merchant/category/group/account)
    """

    mode: EditMode
    transactions: pl.DataFrame
    current_value: Optional[str]
    field_name: str
    is_multi_select: bool
    transaction_count: int
    group_field: Optional[str] = None

    def get_display_summary(self) -> str:
        """Get human-readable summary of edit operation."""
        if self.is_multi_select:
            return f"Editing {self.transaction_count} transactions from multiple selections"
        elif self.mode in [EditMode.AGGREGATE_SINGLE, EditMode.SUBGROUP_SINGLE]:
            return f"Editing all {self.transaction_count} transactions"
        else:
            return f"Editing {self.transaction_count} transaction(s)"


class AppController:
    """
    UI-agnostic application controller.

    Handles all business logic and delegates UI operations to IViewPresenter.
    This separation allows testing business logic without running the TUI.

    Example:
        controller = AppController(view, state, data_manager)
        controller.refresh_view(force_rebuild=False)  # Smooth update
    """

    def __init__(
        self, view: IViewPresenter, state: AppState, data_manager: DataManager, cache_manager=None
    ):
        """
        Initialize controller.

        Args:
            view: UI implementation (TextualView, WebView, MockView, etc.)
            state: Application state
            data_manager: Data operations layer
            cache_manager: Optional cache manager for saving updated data
        """
        self.view = view
        self.state = state
        self.data_manager = data_manager
        self.cache_manager = cache_manager

    def _get_display_labels(self) -> dict:
        """Get display labels from backend, with safe fallback to defaults."""
        try:
            return self.data_manager.mm.get_display_labels()
        except (AttributeError, Exception):
            # Fallback to default labels if backend doesn't support it
            return {
                "merchant": "Merchant",
                "account": "Account",
                "accounts": "Accounts",
            }

    def _get_column_config(self) -> dict:
        """Get column configuration from backend, with safe fallback to defaults."""
        try:
            config = self.data_manager.mm.get_column_config()
            # Add currency symbol to config
            config["currency_symbol"] = self.data_manager.mm.get_currency_symbol()
            return config
        except (AttributeError, Exception):
            # Fallback to default widths if backend doesn't support it
            return {
                "merchant_width_pct": 40,
                "account_width_pct": 40,
                "currency_symbol": "$",
            }

    def refresh_view(self, force_rebuild: bool = True) -> None:
        """
        Refresh the current view.

        This is the core view refresh logic that was previously in MoneyflowTUI.
        Now it's testable business logic that delegates rendering to the view.

        Args:
            force_rebuild: If True, rebuild columns (view mode changed).
                          If False, update rows only (smooth update for same view).

        The business logic here decides:
        - What data to show (based on state.view_mode)
        - What columns/rows to prepare (using ViewPresenter)
        - Whether to rebuild or smooth update

        The view implementation handles:
        - How to render the table
        - How to clear columns/rows
        - Widget management
        """
        if self.data_manager is None or self.data_manager.df is None:
            return

        # Validate sort field for current view type
        # Detail views (transaction lists) don't have a 'count' column
        # Aggregate views and sub-grouped views DO have a 'count' column
        is_aggregate_view = self.state.view_mode in [
            ViewMode.MERCHANT,
            ViewMode.CATEGORY,
            ViewMode.GROUP,
            ViewMode.ACCOUNT,
            ViewMode.TIME,
        ]
        is_sub_grouped = self.state.is_drilled_down() and self.state.sub_grouping_mode is not None
        is_detail_view = not is_aggregate_view and not is_sub_grouped

        if is_detail_view and self.state.sort_by == SortMode.COUNT:
            # COUNT sort is invalid for detail views - reset to DATE descending
            self.state.sort_by = SortMode.DATE
            self.state.sort_direction = SortDirection.DESC
        elif (is_aggregate_view or is_sub_grouped) and self.state.sort_by == SortMode.DATE:
            # DATE sort is invalid for aggregate views - reset to AMOUNT descending
            self.state.sort_by = SortMode.AMOUNT
            self.state.sort_direction = SortDirection.DESC

        # Prepare view data based on current state
        if self.state.view_mode == ViewMode.TIME:
            # TIME view - special handling for time aggregation
            view_data = self._prepare_time_aggregate_view()
            if view_data is None:
                return

        elif self.state.view_mode in [
            ViewMode.MERCHANT,
            ViewMode.CATEGORY,
            ViewMode.GROUP,
            ViewMode.ACCOUNT,
        ]:
            # All aggregate views use the same pattern
            view_data = self._prepare_aggregate_view(self.state.view_mode)
            if view_data is None:
                return

        elif self.state.view_mode == ViewMode.DETAIL:
            filtered_df = self.state.get_filtered_df()
            if filtered_df is None:
                return

            # Apply drill-down filters (can have multiple levels)
            # Apply in order: merchant → category → group → account
            txns = filtered_df

            if self.state.selected_merchant:
                txns = self.data_manager.filter_by_merchant(txns, self.state.selected_merchant)

            if self.state.selected_category:
                txns = self.data_manager.filter_by_category(txns, self.state.selected_category)

            if self.state.selected_group:
                txns = self.data_manager.filter_by_group(txns, self.state.selected_group)

            if self.state.selected_account:
                txns = self.data_manager.filter_by_account(txns, self.state.selected_account)

            # Check if sub-grouping is active (drilled down with aggregation)
            if self.state.is_drilled_down() and self.state.sub_grouping_mode:
                # Show aggregated view within drill-down
                if self.state.sub_grouping_mode == ViewMode.TIME:
                    # Special handling for time sub-grouping
                    agg = self.data_manager.aggregate_by_time(txns, self.state.time_granularity)
                    field_name = "time_period_display"  # Actual column name in time aggregation
                else:
                    sub_group_map = {
                        ViewMode.CATEGORY: (self.data_manager.aggregate_by_category, "category"),
                        ViewMode.GROUP: (self.data_manager.aggregate_by_group, "group"),
                        ViewMode.ACCOUNT: (self.data_manager.aggregate_by_account, "account"),
                        ViewMode.MERCHANT: (self.data_manager.aggregate_by_merchant, "merchant"),
                    }

                    aggregate_func, field_name = sub_group_map[self.state.sub_grouping_mode]
                    agg = aggregate_func(txns)

                # Apply sorting with secondary sort key for deterministic ordering
                sort_col = self.state.sort_by.value
                if sort_col == "amount":
                    sort_col = "total"
                elif sort_col == "time_period":
                    sort_col = "time_period_display"
                elif sort_col in ["merchant", "category", "group", "account"]:
                    sort_col = field_name

                descending = ViewPresenter.should_sort_descending(
                    sort_col, self.state.sort_direction
                )
                if not agg.is_empty():
                    # Use secondary sort by field_name for deterministic ordering
                    # when primary sort values are equal (e.g., same amount)
                    agg = agg.sort([sort_col, field_name], descending=[descending, False])

                self.state.current_data = agg

                # Get pending edit IDs for flags
                pending_edit_ids = {edit.transaction_id for edit in self.data_manager.pending_edits}

                view_data = ViewPresenter.prepare_aggregation_view(
                    agg,
                    field_name,
                    self.state.sort_by,
                    self.state.sort_direction,
                    detail_df=txns,
                    pending_edit_ids=pending_edit_ids,
                    selected_group_keys=self.state.selected_group_keys,
                    column_config=self._get_column_config(),
                    display_labels=self._get_display_labels(),
                )
            else:
                # Show detail view (normal behavior)
                # Sort
                if not txns.is_empty():
                    sort_field = self.state.sort_by.value
                    # Map TIME_PERIOD to DATE for detail view (transactions don't have time_period column)
                    if sort_field == "time_period":
                        sort_field = "date"
                    descending = ViewPresenter.should_sort_descending(
                        sort_field, self.state.sort_direction
                    )
                    txns = txns.sort(sort_field, descending=descending)

                self.state.current_data = txns

                # Get pending edit IDs
                pending_txn_ids = {edit.transaction_id for edit in self.data_manager.pending_edits}

                view_data = ViewPresenter.prepare_transaction_view(
                    txns,
                    self.state.sort_by,
                    self.state.sort_direction,
                    self.state.selected_ids,
                    pending_txn_ids,
                    column_config=self._get_column_config(),
                    display_labels=self._get_display_labels(),
                )
        else:
            return

        # Update date range for breadcrumb display (compute from filtered data)
        # Use transactions_df filtered by current timeframe for date range
        filtered_df = self.state.get_filtered_df()
        if filtered_df is not None and not filtered_df.is_empty() and "date" in filtered_df.columns:
            # Get min/max dates as Python date objects
            min_val = filtered_df["date"].min()
            max_val = filtered_df["date"].max()
            # Polars returns date objects directly for date columns
            self.state.current_data_start_date = min_val if isinstance(min_val, date_type) else None
            self.state.current_data_end_date = max_val if isinstance(max_val, date_type) else None
        else:
            self.state.current_data_start_date = None
            self.state.current_data_end_date = None

        # Delegate rendering to view - it handles the details of clearing/rebuilding
        self.view.update_table(
            columns=view_data["columns"], rows=view_data["rows"], force_rebuild=force_rebuild
        )

        # Update other UI elements
        self.view.update_breadcrumb(self.state.get_breadcrumb(self._get_display_labels()))

        # Calculate stats
        filtered_df = self.state.get_filtered_df()
        if filtered_df is not None and not filtered_df.is_empty():
            # Exclude hidden from totals
            non_hidden_df = filtered_df.filter(~filtered_df["hideFromReports"])

            income_df = non_hidden_df.filter(pl.col("group") == "Income")
            total_income = float(income_df["amount"].sum()) if not income_df.is_empty() else 0.0
            expense_df = non_hidden_df.filter(
                (pl.col("group") != "Income") & (pl.col("group") != "Transfers")
            )
            total_expenses = float(expense_df["amount"].sum()) if not expense_df.is_empty() else 0.0
            net_savings = total_income + total_expenses

            # Use abbreviated labels for compact display
            # In: (Income), Out: (Expenses/Outflow), Net: (Savings)
            stats_text = (
                f"{len(filtered_df):,} txns | "
                f"In: {ViewPresenter.format_amount(total_income)} | "
                f"Out: {ViewPresenter.format_amount(total_expenses)} | "
                f"Net: {ViewPresenter.format_amount(net_savings)}"
            )
            self.view.update_stats(stats_text)
        else:
            self.view.update_stats("0 txns | No data in view")

        # Update action hints
        hints_text = self._get_action_hints()
        self.view.update_hints(hints_text)

        # Update pending changes
        count = len(self.data_manager.pending_edits)
        self.view.update_pending_changes(count)

    def _prepare_aggregate_view(self, view_mode: ViewMode):
        """
        Prepare aggregated view data (merchant, category, group, or account).

        This helper eliminates 64 lines of duplication from refresh_view.
        The pattern is identical for all aggregate views:
        1. Get filtered data
        2. Aggregate by field
        3. Sort by current sort field
        4. Prepare view data

        Args:
            view_mode: Which aggregate view to prepare

        Returns:
            dict: View data with columns and rows, or None if no data
        """
        filtered_df = self.state.get_filtered_df()
        if filtered_df is None:
            return None

        # Map view mode to aggregation method and field name
        aggregation_map = {
            ViewMode.MERCHANT: (self.data_manager.aggregate_by_merchant, "merchant"),
            ViewMode.CATEGORY: (self.data_manager.aggregate_by_category, "category"),
            ViewMode.GROUP: (self.data_manager.aggregate_by_group, "group"),
            ViewMode.ACCOUNT: (self.data_manager.aggregate_by_account, "account"),
        }

        aggregate_func, field_name = aggregation_map[view_mode]
        agg = aggregate_func(filtered_df)

        # Apply sorting with secondary sort key for deterministic ordering
        sort_col = self.state.sort_by.value

        # Map sort field to actual column name in aggregation DataFrame
        if sort_col == "amount":
            sort_col = "total"  # Aggregations use "total" not "amount"
        elif sort_col in ["merchant", "category", "group", "account"]:
            # Use the grouping field name (e.g., "merchant" column in merchant aggregation)
            sort_col = field_name
        # else: "count" stays as "count"

        descending = ViewPresenter.should_sort_descending(sort_col, self.state.sort_direction)
        if not agg.is_empty():
            # Use secondary sort by field_name for deterministic ordering
            # when primary sort values are equal (e.g., same amount/count)
            agg = agg.sort([sort_col, field_name], descending=[descending, False])

        self.state.current_data = agg

        # Get pending edit transaction IDs for flags column
        pending_edit_ids = {edit.transaction_id for edit in self.data_manager.pending_edits}

        return ViewPresenter.prepare_aggregation_view(
            agg,
            field_name,
            self.state.sort_by,
            self.state.sort_direction,
            detail_df=filtered_df,
            pending_edit_ids=pending_edit_ids,
            selected_group_keys=self.state.selected_group_keys,
            column_config=self._get_column_config(),
            display_labels=self._get_display_labels(),
        )

    def _prepare_time_aggregate_view(self):
        """
        Prepare TIME aggregation view (by year or month).

        Returns:
            dict: View data with columns and rows, or None if no data
        """
        filtered_df = self.state.get_filtered_df()
        if filtered_df is None:
            return None

        # Aggregate by time with current granularity
        agg = self.data_manager.aggregate_by_time(filtered_df, self.state.time_granularity)

        # Apply sorting
        sort_col = self.state.sort_by.value
        if sort_col == "amount":
            sort_col = "total"
        elif sort_col == "time_period":
            sort_col = "time_period_display"

        descending = ViewPresenter.should_sort_descending(sort_col, self.state.sort_direction)
        if not agg.is_empty():
            agg = agg.sort(sort_col, descending=descending)

        self.state.current_data = agg

        # Prepare view - TIME view has special column handling (no Top Category)
        # We'll need to add a new method or extend prepare_aggregation_view
        # For now, let's use a simplified version
        return self._prepare_time_view_data(agg)

    def _prepare_time_view_data(self, agg: pl.DataFrame):
        """
        Prepare TIME view data with custom column handling.

        TIME view shows: Period | Count | Total (no Top Category).

        Args:
            agg: Aggregated time DataFrame

        Returns:
            dict with columns and rows for view
        """
        # Build columns
        columns = []

        # Period column (with sort arrow)
        period_label = "Period"
        if self.state.sort_by == SortMode.TIME_PERIOD:
            arrow = ViewPresenter.get_sort_arrow(
                self.state.sort_by, self.state.sort_direction, SortMode.TIME_PERIOD
            )
            period_label = f"{period_label} {arrow}"
        columns.append({"label": period_label, "key": "period", "width": None})

        # Count column
        count_label = "Count"
        if self.state.sort_by == SortMode.COUNT:
            arrow = ViewPresenter.get_sort_arrow(
                self.state.sort_by, self.state.sort_direction, SortMode.COUNT
            )
            count_label = f"{count_label} {arrow}"
        columns.append({"label": count_label, "key": "count", "width": 10})

        # Total column (with currency symbol) - consistent with other aggregate views
        currency_symbol = self._get_column_config().get("currency_symbol", "$")
        arrow = ""
        if self.state.sort_by == SortMode.AMOUNT:
            arrow = " " + ViewPresenter.get_sort_arrow(
                self.state.sort_by, self.state.sort_direction, SortMode.AMOUNT
            )
        total_label = f"Total ({currency_symbol}){arrow}"
        columns.append({"label": total_label, "key": "total", "width": 15})

        # Build rows
        rows = []
        for row_dict in agg.to_dicts():
            year = row_dict["year"]
            month = row_dict.get("month")
            day = row_dict.get("day")
            period_str = ViewPresenter.format_time_period(
                year, month, day, self.state.time_granularity
            )
            count_str = str(row_dict["count"])
            total_str = ViewPresenter.format_amount(row_dict["total"])

            rows.append((period_str, count_str, total_str))

        return {"columns": columns, "rows": rows, "empty": len(rows) == 0}

    # View mode switching operations
    def switch_to_merchant_view(self):
        """Switch to merchant aggregation view."""
        self.state.view_mode = ViewMode.MERCHANT
        self.state.clear_drill_down_and_selection()
        # Reset sort to valid field for aggregate views (now includes field name)
        if self.state.sort_by not in [SortMode.MERCHANT, SortMode.COUNT, SortMode.AMOUNT]:
            self.state.sort_by = SortMode.AMOUNT
        self.refresh_view()

    def switch_to_category_view(self):
        """Switch to category aggregation view."""
        self.state.view_mode = ViewMode.CATEGORY
        self.state.clear_drill_down_and_selection()
        if self.state.sort_by not in [SortMode.CATEGORY, SortMode.COUNT, SortMode.AMOUNT]:
            self.state.sort_by = SortMode.AMOUNT
        self.refresh_view()

    def switch_to_group_view(self):
        """Switch to group aggregation view."""
        self.state.view_mode = ViewMode.GROUP
        self.state.clear_drill_down_and_selection()
        if self.state.sort_by not in [SortMode.GROUP, SortMode.COUNT, SortMode.AMOUNT]:
            self.state.sort_by = SortMode.AMOUNT
        self.refresh_view()

    def switch_to_account_view(self):
        """Switch to account aggregation view."""
        self.state.view_mode = ViewMode.ACCOUNT
        self.state.clear_drill_down_and_selection()
        if self.state.sort_by not in [SortMode.ACCOUNT, SortMode.COUNT, SortMode.AMOUNT]:
            self.state.sort_by = SortMode.AMOUNT
        self.refresh_view()

    def switch_to_time_view(self):
        """Switch to time aggregation view."""
        self.state.view_mode = ViewMode.TIME
        self.state.clear_drill_down_and_selection()
        # Default to chronological ascending for TIME view
        self.state.sort_by = SortMode.TIME_PERIOD
        self.state.sort_direction = SortDirection.ASC
        self.refresh_view()

    def switch_to_detail_view(self, set_default_sort: bool = True):
        """
        Switch to transaction detail view (ungrouped).

        Saves current state to navigation history if switching from an aggregate view,
        so that pressing Esc or 'g' can restore the previous view.

        Args:
            set_default_sort: If True, set default sort (Date descending)
        """
        # Save current state to navigation history if we're in an aggregate view
        # This allows Esc/'g' to return to the correct aggregate view
        if self.state.view_mode in [
            ViewMode.MERCHANT,
            ViewMode.CATEGORY,
            ViewMode.GROUP,
            ViewMode.ACCOUNT,
            ViewMode.TIME,
        ]:
            self.state.navigation_history.append(
                NavigationState(
                    view_mode=self.state.view_mode,
                    cursor_position=0,  # Don't preserve cursor when switching with 'd'
                    scroll_y=0.0,
                    sort_by=self.state.sort_by,
                    sort_direction=self.state.sort_direction,
                    selected_merchant=self.state.selected_merchant,
                    selected_category=self.state.selected_category,
                    selected_group=self.state.selected_group,
                    selected_account=self.state.selected_account,
                    sub_grouping_mode=self.state.sub_grouping_mode,
                )
            )

        self.state.view_mode = ViewMode.DETAIL
        self.state.clear_drill_down_and_selection()
        if set_default_sort:
            self.state.sort_by = SortMode.DATE
            self.state.sort_direction = SortDirection.DESC
        self.refresh_view()

    def cycle_grouping(self) -> Optional[str]:
        """
        Cycle through aggregation views (Merchant → Category → Group → Account → Time).

        Returns:
            View name if changed, None if at end of cycle
        """
        view_name = self.state.cycle_grouping()
        if view_name:
            self.refresh_view()
        return view_name

    def toggle_time_granularity(self) -> str:
        """
        Toggle between year and month granularity in TIME view.

        Returns:
            Name of new granularity ("Years" or "Months")
        """
        result = self.state.toggle_time_granularity()
        self.refresh_view()
        return result

    # Sorting operations
    def toggle_sort_field(self) -> str:
        """
        Toggle to next sort field based on current view mode.

        Returns:
            Display name of new sort field
        """
        # Determine effective view mode (sub_grouping_mode takes precedence when drilled down)
        effective_view_mode = self.state.view_mode
        if self.state.sub_grouping_mode and self.state.is_drilled_down():
            # In subgroup view - use sub_grouping_mode to determine sort options
            effective_view_mode = self.state.sub_grouping_mode

        new_sort, display = self.get_next_sort_field(effective_view_mode, self.state.sort_by)
        self.state.sort_by = new_sort
        self.refresh_view()
        return display

    def reverse_sort(self) -> str:
        """
        Reverse the current sort direction.

        Returns:
            Display name of new direction ("Ascending" or "Descending")
        """
        self.state.reverse_sort()
        self.refresh_view()
        return "Descending" if self.state.sort_direction == SortDirection.DESC else "Ascending"

    # Time navigation operations
    def select_month(self, month: int) -> str:
        """
        Select a specific month of the current year.

        Args:
            month: Month number (1-12)

        Returns:
            Description of the selected time range
        """
        today = date_type.today()
        date_range = TimeNavigator.get_month_range(today.year, month)

        self.state.start_date = date_range.start_date
        self.state.end_date = date_range.end_date
        self.refresh_view()
        return date_range.description

    def navigate_prev_period(self) -> tuple[bool, Optional[str]]:
        """
        Navigate to previous time period.

        Returns:
            Tuple of (should_fallback_to_year, description)
            - should_fallback_to_year: True if in all-time view (no prev period)
            - description: Time range description if navigated
        """
        if self.state.start_date is None:
            # In all-time view, signal to fallback to current year
            return (True, None)

        date_range = TimeNavigator.previous_period(self.state.start_date, self.state.end_date)
        self.state.start_date = date_range.start_date
        self.state.end_date = date_range.end_date
        self.refresh_view()
        return (False, date_range.description)

    def navigate_next_period(self) -> tuple[bool, Optional[str]]:
        """
        Navigate to next time period.

        Returns:
            Tuple of (should_fallback_to_year, description)
            - should_fallback_to_year: True if in all-time view (no next period)
            - description: Time range description if navigated
        """
        if self.state.start_date is None:
            # In all-time view, signal to fallback to current year
            return (True, None)

        date_range = TimeNavigator.next_period(self.state.start_date, self.state.end_date)
        self.state.start_date = date_range.start_date
        self.state.end_date = date_range.end_date
        self.refresh_view()
        return (False, date_range.description)

    # Data access methods (read-only)
    def get_filtered_df(self):
        """Get filtered DataFrame for current view."""
        return self.state.get_filtered_df()

    def get_current_data(self):
        """Get current view data (aggregated or detail)."""
        return self.state.current_data

    def get_merchant_suggestions(self) -> list[str]:
        """
        Get list of all merchants for autocomplete.

        Returns merchants from both:
        - Cached historical merchants (refreshed daily)
        - Currently loaded transactions (includes recent edits)
        """
        return self.data_manager.get_all_merchants_for_autocomplete()

    def get_categories(self) -> dict:
        """Get category map."""
        return self.data_manager.categories

    def get_pending_changes_count(self) -> int:
        """Get count of pending edits."""
        return self.data_manager.get_stats()["pending_changes"]

    def get_pending_edits(self):
        """Get pending edits for review."""
        return self.data_manager.pending_edits

    def has_unsaved_changes(self) -> bool:
        """Check if there are unsaved changes."""
        return self.get_pending_changes_count() > 0

    def get_view_mode(self) -> ViewMode:
        """Get current view mode."""
        return self.state.view_mode

    def get_selected_ids(self) -> set:
        """Get currently selected transaction IDs."""
        return self.state.selected_ids

    # Search and filtering operations
    def apply_search(self, query: str) -> int:
        """
        Apply search query.

        Args:
            query: Search query string

        Returns:
            Count of filtered results
        """
        self.state.set_search(query)
        self.refresh_view()
        filtered = self.state.get_filtered_df()
        return len(filtered) if filtered is not None else 0

    def clear_search(self):
        """Clear search query."""
        self.state.set_search("")
        self.refresh_view()

    def apply_filters(self, show_transfers: bool, show_hidden: bool):
        """Apply visibility filters."""
        self.state.show_transfers = show_transfers
        self.state.show_hidden = show_hidden
        self.refresh_view()

    def toggle_selection(self, txn_id: str) -> int:
        """
        Toggle transaction selection.

        Args:
            txn_id: Transaction ID to toggle

        Returns:
            Total count of selected transactions
        """
        self.state.toggle_selection(txn_id)
        return len(self.state.selected_ids)

    def clear_selection(self):
        """Clear all selections."""
        self.state.clear_selection()

    def toggle_selection_at_row(self, row_idx: int) -> tuple[int, str]:
        """
        Toggle selection of the item at the given row index.

        Handles both aggregate views (groups) and detail views (transactions).
        Automatically determines the selection type based on current view mode.

        Args:
            row_idx: Row index in current_data to toggle selection for

        Returns:
            Tuple of (count, item_type) where:
            - count: Total number of items currently selected
            - item_type: "group" or "transaction"
        """
        if self.state.current_data is None or row_idx < 0:
            return (0, "transaction")

        row_data = self.state.current_data.row(row_idx, named=True)

        # Check if we're in aggregate view or sub-grouped detail view
        if self.state.view_mode in [
            ViewMode.MERCHANT,
            ViewMode.CATEGORY,
            ViewMode.GROUP,
            ViewMode.ACCOUNT,
        ] or (
            self.state.view_mode == ViewMode.DETAIL
            and self.state.is_drilled_down()
            and self.state.sub_grouping_mode
        ):
            # Group selection (aggregate or sub-grouped view)
            group_name = str(row_data.get(self.state.current_data.columns[0]))
            self.state.toggle_group_selection(group_name)
            return (len(self.state.selected_group_keys), "group")
        else:
            # Transaction selection (detail view)
            txn_id = row_data.get("id")
            if txn_id:
                self.state.toggle_selection(txn_id)
            return (len(self.state.selected_ids), "transaction")

    def toggle_select_all_visible(self) -> tuple[int, bool, str]:
        """
        Toggle select/deselect all items in the current view.

        If all items are already selected, deselects all.
        If some or no items are selected, selects all.

        Returns:
            Tuple of (count, all_selected, item_type) where:
            - count: Number of items now selected (0 if deselected all)
            - all_selected: True if all items are now selected, False if deselected
            - item_type: "group" or "transaction"
        """
        if self.state.current_data is None:
            return (0, False, "transaction")

        total_rows = len(self.state.current_data)

        # Check if we're in aggregate view or sub-grouped detail view
        if self.state.view_mode in [
            ViewMode.MERCHANT,
            ViewMode.CATEGORY,
            ViewMode.GROUP,
            ViewMode.ACCOUNT,
        ] or (
            self.state.view_mode == ViewMode.DETAIL
            and self.state.is_drilled_down()
            and self.state.sub_grouping_mode
        ):
            # Group selection
            all_selected = len(self.state.selected_group_keys) == total_rows

            if all_selected:
                # Deselect all
                self.state.selected_group_keys.clear()
                return (0, False, "group")
            else:
                # Select all
                self.state.selected_group_keys.clear()
                for row_idx in range(total_rows):
                    row_data = self.state.current_data.row(row_idx, named=True)
                    group_name = str(row_data.get(self.state.current_data.columns[0]))
                    self.state.selected_group_keys.add(group_name)
                return (len(self.state.selected_group_keys), True, "group")
        else:
            # Transaction selection
            all_selected = len(self.state.selected_ids) == total_rows

            if all_selected:
                # Deselect all
                self.state.selected_ids.clear()
                return (0, False, "transaction")
            else:
                # Select all
                self.state.selected_ids.clear()
                for row_idx in range(total_rows):
                    row_data = self.state.current_data.row(row_idx, named=True)
                    txn_id = row_data.get("id")
                    if txn_id:
                        self.state.selected_ids.add(txn_id)
                return (len(self.state.selected_ids), True, "transaction")

    def drill_down(self, item_name: str, cursor_position: int, scroll_y: float = 0.0):
        """
        Drill down into an item (merchant/category/group/account).

        Args:
            item_name: Name of item to drill into
            cursor_position: Current cursor position to save for go_back
            scroll_y: Current scroll position to save for go_back
        """
        self.state.drill_down(item_name, cursor_position, scroll_y)
        self.refresh_view()

    def go_back(self) -> tuple[bool, int, float]:
        """
        Go back to previous view.

        Returns:
            Tuple of (success, cursor_position, scroll_y)
            - success: True if went back, False if already at top
            - cursor_position: Where to restore cursor
            - scroll_y: Where to restore scroll position
        """
        success, cursor_position, scroll_y = self.state.go_back()
        if success:
            self.refresh_view()
        return (success, cursor_position, scroll_y)

    def get_next_sort_field(
        self, view_mode: ViewMode, current_sort: SortMode
    ) -> tuple[SortMode, str]:
        """
        Determine the next sort field when user toggles sorting.

        This is pure business logic - a state machine for sort field cycling.
        Different cycling behavior for detail view vs aggregate views.

        Args:
            view_mode: Current view mode
            current_sort: Current sort field

        Returns:
            Tuple of (new_sort_mode, display_name)

        Detail view cycles through 5 fields:
            Date → Merchant → Category → Account → Amount → Date (loop)

        Aggregate views cycle through 3 fields:
            Name → Count → Amount → Name (loop)
            where Name is the grouping field (Merchant/Category/Group/Account)
        """
        if view_mode == ViewMode.DETAIL:
            # 5-field cycle for transaction detail view
            if current_sort == SortMode.DATE:
                return (SortMode.MERCHANT, "Merchant")
            elif current_sort == SortMode.MERCHANT:
                return (SortMode.CATEGORY, "Category")
            elif current_sort == SortMode.CATEGORY:
                return (SortMode.ACCOUNT, "Account")
            elif current_sort == SortMode.ACCOUNT:
                return (SortMode.AMOUNT, "Amount")
            else:  # AMOUNT or anything else
                return (SortMode.DATE, "Date")
        elif view_mode == ViewMode.TIME:
            # TIME view cycles: Time Period → Count → Amount → Time Period
            if current_sort == SortMode.TIME_PERIOD:
                return (SortMode.COUNT, "Count")
            elif current_sort == SortMode.COUNT:
                return (SortMode.AMOUNT, "Amount")
            else:  # AMOUNT or anything else
                return (SortMode.TIME_PERIOD, "Time Period")
        else:
            # Aggregate views cycle: Field name → Count → Amount → Field name
            # Map view mode to its field SortMode
            view_to_field_sort = {
                ViewMode.MERCHANT: (SortMode.MERCHANT, "Merchant"),
                ViewMode.CATEGORY: (SortMode.CATEGORY, "Category"),
                ViewMode.GROUP: (SortMode.GROUP, "Group"),
                ViewMode.ACCOUNT: (SortMode.ACCOUNT, "Account"),
            }

            field_sort, field_name = view_to_field_sort.get(view_mode, (SortMode.COUNT, "Count"))

            # Cycle through: Field → Count → Amount → Field
            if current_sort == field_sort:
                return (SortMode.COUNT, "Count")
            elif current_sort == SortMode.COUNT:
                return (SortMode.AMOUNT, "Amount")
            else:
                return (field_sort, field_name)

    def _get_action_hints(self) -> str:
        """Get action hints text based on current view mode."""
        sort_name = self.state.sort_by.value.capitalize()

        if self.state.view_mode == ViewMode.TIME:
            # TIME view - show granularity toggle
            # Determine next granularity in cycle: Year → Month → Day → Year
            if self.state.time_granularity == TimeGranularity.YEAR:
                toggle_to = "By Month"
            elif self.state.time_granularity == TimeGranularity.MONTH:
                toggle_to = "By Day"
            else:  # DAY
                toggle_to = "By Year"

            return f"Enter=Drill | t={toggle_to} | s=Sort({sort_name}) | g=Group"
        elif self.state.view_mode == ViewMode.MERCHANT:
            return f"Enter=Drill | Space=Select | m=✏️ Merchant (bulk) | c=✏️ Category (bulk) | s=Sort({sort_name}) | g=Group"
        elif self.state.view_mode in [ViewMode.CATEGORY, ViewMode.GROUP]:
            return f"Enter=Drill | Space=Select | m=✏️ Merchant (bulk) | c=✏️ Category (bulk) | s=Sort({sort_name}) | g=Group"
        elif self.state.view_mode == ViewMode.ACCOUNT:
            return f"Enter=Drill | Space=Select | m=✏️ Merchant (bulk) | c=✏️ Category (bulk) | s=Sort({sort_name}) | g=Group"
        else:  # DETAIL
            # Check if we're in a drilled-down view or ungrouped view
            if (
                self.state.selected_merchant
                or self.state.selected_category
                or self.state.selected_group
                or self.state.selected_account
            ):
                return "Esc/g=Back | m=✏️ Merchant | c=✏️ Category | h=Hide | x=Delete | Space=Select | Ctrl-A=SelectAll"
            else:
                return "Esc/g=Group | m=✏️ Merchant | c=✏️ Category | h=Hide | x=Delete | Space=Select | Ctrl-A=SelectAll"

    # Edit Orchestration Methods

    def determine_edit_context(self, field_name: str, cursor_row: int = 0) -> EditContext:
        """
        Determine edit context based on current view and selection state.

        This method encapsulates the complex logic for determining what to edit
        based on the current view (aggregate vs detail), selection state (single vs multi),
        and drill-down context (sub-grouped vs ungrouped).

        Args:
            field_name: Field to edit ("merchant" or "category")
            cursor_row: Current cursor row position (for single-item edits)

        Returns:
            EditContext with all information needed to execute the edit

        Examples:
            >>> # Merchant view, press m on Amazon
            >>> context = controller.determine_edit_context("merchant", cursor_row=5)
            >>> context.mode
            EditMode.AGGREGATE_SINGLE
            >>> len(context.transactions)  # All Amazon transactions
            50
        """
        # Determine view type
        is_aggregate = self.state.view_mode in [
            ViewMode.MERCHANT,
            ViewMode.CATEGORY,
            ViewMode.GROUP,
            ViewMode.ACCOUNT,
        ]
        is_detail = self.state.view_mode == ViewMode.DETAIL
        is_subgrouped = self.state.is_drilled_down() and self.state.sub_grouping_mode is not None

        # Determine selection state
        has_selected_ids = len(self.state.selected_ids) > 0
        has_selected_groups = len(self.state.selected_group_keys) > 0

        # Get filtered data
        filtered_df = self.state.get_filtered_df()
        if filtered_df is None or filtered_df.is_empty():
            # Return empty context
            return EditContext(
                mode=EditMode.DETAIL_SINGLE,
                transactions=pl.DataFrame(),
                current_value=None,
                field_name=field_name,
                is_multi_select=False,
                transaction_count=0,
                group_field=None,
            )

        # Handle aggregate views (Merchant, Category, Group, Account)
        if is_aggregate:
            return self._determine_aggregate_edit_context(
                field_name, cursor_row, filtered_df, has_selected_groups
            )

        # Handle sub-grouped views (drilled down with sub-grouping)
        if is_subgrouped:
            return self._determine_subgroup_edit_context(
                field_name, cursor_row, filtered_df, has_selected_groups
            )

        # Handle detail views
        if is_detail:
            return self._determine_detail_edit_context(
                field_name, cursor_row, filtered_df, has_selected_ids
            )

        # Fallback (shouldn't reach here)
        return EditContext(
            mode=EditMode.DETAIL_SINGLE,
            transactions=pl.DataFrame(),
            current_value=None,
            field_name=field_name,
            is_multi_select=False,
            transaction_count=0,
            group_field=None,
        )

    def _determine_aggregate_edit_context(
        self, field_name: str, cursor_row: int, filtered_df: pl.DataFrame, has_selected_groups: bool
    ) -> EditContext:
        """Determine edit context for aggregate views."""
        # Map view mode to field name
        field_map = {
            ViewMode.MERCHANT: "merchant",
            ViewMode.CATEGORY: "category",
            ViewMode.GROUP: "group",
            ViewMode.ACCOUNT: "account",
        }
        group_field = field_map[self.state.view_mode]

        if has_selected_groups:
            # Multi-select: get transactions from all selected groups
            transactions = self.get_transactions_from_selected_groups(group_field)
            return EditContext(
                mode=EditMode.AGGREGATE_MULTI,
                transactions=transactions,
                current_value="multiple",  # Special marker for multi-select
                field_name=field_name,
                is_multi_select=True,
                transaction_count=len(transactions),
                group_field=group_field,
            )
        else:
            # Single selection: get transactions from current row
            if self.state.current_data is None or cursor_row >= len(self.state.current_data):
                return EditContext(
                    mode=EditMode.AGGREGATE_SINGLE,
                    transactions=pl.DataFrame(),
                    current_value=None,
                    field_name=field_name,
                    is_multi_select=False,
                    transaction_count=0,
                    group_field=group_field,
                )

            current_row = self.state.current_data.row(cursor_row, named=True)
            group_name = str(current_row.get(self.state.current_data.columns[0]))

            # Get all transactions for this group
            if group_field == "merchant":
                transactions = self.data_manager.filter_by_merchant(filtered_df, group_name)
            elif group_field == "category":
                transactions = self.data_manager.filter_by_category(filtered_df, group_name)
            elif group_field == "group":
                transactions = self.data_manager.filter_by_group(filtered_df, group_name)
            elif group_field == "account":
                transactions = self.data_manager.filter_by_account(filtered_df, group_name)
            else:
                transactions = pl.DataFrame()

            # For merchant edits, current_value is the merchant name
            # For category edits in merchant view, current_value is the first category or None
            if field_name == group_field:
                current_value = group_name
            else:
                current_value = None

            return EditContext(
                mode=EditMode.AGGREGATE_SINGLE,
                transactions=transactions,
                current_value=current_value,
                field_name=field_name,
                is_multi_select=False,
                transaction_count=len(transactions),
                group_field=group_field,
            )

    def _determine_subgroup_edit_context(
        self, field_name: str, cursor_row: int, filtered_df: pl.DataFrame, has_selected_groups: bool
    ) -> EditContext:
        """Determine edit context for sub-grouped views (drilled down with sub-grouping)."""
        # Determine the field based on sub-grouping mode
        field_map = {
            ViewMode.MERCHANT: "merchant",
            ViewMode.CATEGORY: "category",
            ViewMode.GROUP: "group",
            ViewMode.ACCOUNT: "account",
        }
        group_field = (
            field_map[self.state.sub_grouping_mode]
            if self.state.sub_grouping_mode in field_map
            else "merchant"
        )

        if has_selected_groups:
            # Multi-select in sub-grouped view
            transactions = self.get_transactions_from_selected_groups(group_field)
            return EditContext(
                mode=EditMode.SUBGROUP_MULTI,
                transactions=transactions,
                current_value="multiple",
                field_name=field_name,
                is_multi_select=True,
                transaction_count=len(transactions),
                group_field=group_field,
            )
        else:
            # Single selection in sub-grouped view
            if self.state.current_data is None or cursor_row >= len(self.state.current_data):
                return EditContext(
                    mode=EditMode.SUBGROUP_SINGLE,
                    transactions=pl.DataFrame(),
                    current_value=None,
                    field_name=field_name,
                    is_multi_select=False,
                    transaction_count=0,
                    group_field=group_field,
                )

            current_row = self.state.current_data.row(cursor_row, named=True)
            group_name = str(current_row.get(self.state.current_data.columns[0]))

            # Get transactions for this sub-group
            if group_field == "merchant":
                transactions = self.data_manager.filter_by_merchant(filtered_df, group_name)
            elif group_field == "category":
                transactions = self.data_manager.filter_by_category(filtered_df, group_name)
            elif group_field == "group":
                transactions = self.data_manager.filter_by_group(filtered_df, group_name)
            elif group_field == "account":
                transactions = self.data_manager.filter_by_account(filtered_df, group_name)
            else:
                transactions = pl.DataFrame()

            return EditContext(
                mode=EditMode.SUBGROUP_SINGLE,
                transactions=transactions,
                current_value=group_name if field_name == group_field else None,
                field_name=field_name,
                is_multi_select=False,
                transaction_count=len(transactions),
                group_field=group_field,
            )

    def _determine_detail_edit_context(
        self, field_name: str, cursor_row: int, filtered_df: pl.DataFrame, has_selected_ids: bool
    ) -> EditContext:
        """Determine edit context for detail views (transaction list)."""
        if has_selected_ids:
            # Multi-select: get all selected transactions
            transactions = self.state.current_data.filter(
                pl.col("id").is_in(list(self.state.selected_ids))
            )
            # For multi-select, current_value is from first transaction or None
            if not transactions.is_empty():
                first_row = transactions.row(0, named=True)
                current_value = first_row.get(field_name)
            else:
                current_value = None

            return EditContext(
                mode=EditMode.DETAIL_MULTI,
                transactions=transactions,
                current_value=current_value,
                field_name=field_name,
                is_multi_select=True,
                transaction_count=len(transactions),
                group_field=None,
            )
        else:
            # Single transaction
            if self.state.current_data is None or cursor_row >= len(self.state.current_data):
                return EditContext(
                    mode=EditMode.DETAIL_SINGLE,
                    transactions=pl.DataFrame(),
                    current_value=None,
                    field_name=field_name,
                    is_multi_select=False,
                    transaction_count=0,
                    group_field=None,
                )

            # Get single transaction
            single_txn_row = self.state.current_data.row(cursor_row, named=True)
            txn_id = single_txn_row["id"]
            transactions = self.state.current_data.filter(pl.col("id") == txn_id)
            current_value = single_txn_row.get(field_name)

            return EditContext(
                mode=EditMode.DETAIL_SINGLE,
                transactions=transactions,
                current_value=current_value,
                field_name=field_name,
                is_multi_select=False,
                transaction_count=1,
                group_field=None,
            )

    def edit_merchant_current_selection(self, new_merchant: str, cursor_row: int = 0) -> int:
        """
        Edit merchant for current selection (context-aware).

        This method handles all merchant edit scenarios:
        - Aggregate view: Edit all transactions for selected merchant(s)
        - Detail view: Edit selected transaction(s)
        - Sub-grouped view: Edit transactions in selected sub-group(s)

        Uses determine_edit_context() to figure out what to edit, then queues the edits.

        Args:
            new_merchant: New merchant name to apply
            cursor_row: Current cursor row (for single-item edits)

        Returns:
            Number of edits queued (0 if validation failed or no-op)

        Examples:
            >>> # Merchant view, press m on Amazon, type "Amazon.com"
            >>> count = controller.edit_merchant_current_selection("Amazon.com", cursor_row=5)
            >>> count  # All Amazon transactions edited
            50
        """
        # Validate input
        if not new_merchant or not new_merchant.strip():
            return 0

        new_merchant = new_merchant.strip()

        # Determine what to edit
        context = self.determine_edit_context("merchant", cursor_row=cursor_row)

        # No transactions to edit
        if context.transactions.is_empty():
            return 0

        # Check for no-op: editing to same value
        # For multi-select, we still queue edits (user explicitly selected multiple items)
        # For single item, skip if value unchanged
        if not context.is_multi_select and context.current_value == new_merchant:
            return 0

        # Determine old_merchant value for queue_merchant_edits
        # For aggregate edits where we're renaming a merchant, old value is current_value
        # For other cases (bulk edit from different contexts), use current_value or "multiple"
        old_merchant = context.current_value if context.current_value else "multiple"

        # Queue the edits using existing helper
        count = self.queue_merchant_edits(context.transactions, old_merchant, new_merchant)

        return count

    def edit_category_current_selection(self, new_category_id: str, cursor_row: int = 0) -> int:
        """
        Edit category for current selection (context-aware).

        Handles all category edit scenarios using the same orchestration pattern as merchant edits.

        Args:
            new_category_id: New category ID to apply
            cursor_row: Current cursor row (for single-item edits)

        Returns:
            Number of edits queued (0 if validation failed or no transactions)
        """
        # Validate input
        if not new_category_id or not new_category_id.strip():
            return 0

        # Determine what to edit
        context = self.determine_edit_context("category", cursor_row=cursor_row)

        # No transactions to edit
        if context.transactions.is_empty():
            return 0

        # Queue the edits using existing helper
        count = self.queue_category_edits(context.transactions, new_category_id)

        return count

    def toggle_hide_current_selection(self, cursor_row: int = 0) -> tuple[int, bool]:
        """
        Toggle hide/unhide for current selection (context-aware with undo detection).

        Handles all hide/unhide scenarios:
        - If all transactions already have pending hide toggles → undo (remove edits)
        - Otherwise → queue new hide toggle edits

        This allows pressing 'h' twice on the same group to undo the first operation,
        which is better UX than requiring 50 'u' presses for a 50-transaction group.

        Args:
            cursor_row: Current cursor row (for single-item edits)

        Returns:
            Tuple of (count: int, was_undo: bool)
            - count: Number of edits queued or undone
            - was_undo: True if this was an undo operation, False if new toggles
        """
        # Determine what to edit (use "merchant" as placeholder - hide works on any context)
        context = self.determine_edit_context("merchant", cursor_row=cursor_row)

        # No transactions to edit
        if context.transactions.is_empty():
            return (0, False)

        # Check if all transactions in this selection already have pending hide toggles
        pending_hide_txn_ids = {
            edit.transaction_id
            for edit in self.data_manager.pending_edits
            if edit.field == "hide_from_reports"
        }
        current_txn_ids = set(context.transactions["id"].to_list())
        all_have_pending = current_txn_ids.issubset(pending_hide_txn_ids)

        if all_have_pending:
            # Undo: Remove all pending hide toggles for these transactions
            edits_to_remove = [
                edit
                for edit in self.data_manager.pending_edits
                if edit.field == "hide_from_reports" and edit.transaction_id in current_txn_ids
            ]

            for edit in edits_to_remove:
                self.data_manager.pending_edits.remove(edit)

            return (len(edits_to_remove), True)
        else:
            # Normal toggle: Queue new hide toggle edits
            count = self.queue_hide_toggle_edits(context.transactions)
            return (count, False)

    def queue_category_edits(self, transactions_df, new_category_id: str) -> int:
        """
        Queue category edits for a set of transactions.

        This is pure business logic - no UI dependencies. Can be tested independently.

        Args:
            transactions_df: Polars DataFrame of transactions to edit
            new_category_id: New category ID to apply

        Returns:
            int: Number of edits queued
        """
        # Create single timestamp for entire batch so undo recognizes them as a group
        batch_timestamp = datetime.now()
        count = 0
        for txn in transactions_df.iter_rows(named=True):
            self.data_manager.pending_edits.append(
                TransactionEdit(
                    transaction_id=txn["id"],
                    field="category",
                    old_value=txn["category_id"],
                    new_value=new_category_id,
                    timestamp=batch_timestamp,
                )
            )
            count += 1
        return count

    def queue_merchant_edits(self, transactions_df, old_merchant: str, new_merchant: str) -> int:
        """
        Queue merchant edits for a set of transactions.

        This is pure business logic - no UI dependencies. Can be tested independently.

        Args:
            transactions_df: Polars DataFrame of transactions to edit
            old_merchant: Original merchant name (for documentation, not used in logic)
            new_merchant: New merchant name to apply

        Returns:
            int: Number of edits queued
        """
        # Create single timestamp for entire batch so undo recognizes them as a group
        batch_timestamp = datetime.now()
        count = 0
        for txn in transactions_df.iter_rows(named=True):
            self.data_manager.pending_edits.append(
                TransactionEdit(
                    transaction_id=txn["id"],
                    field="merchant",
                    old_value=txn["merchant"],  # Use actual current value from transaction
                    new_value=new_merchant,
                    timestamp=batch_timestamp,
                )
            )
            count += 1
        return count

    def queue_hide_toggle_edits(self, transactions_df) -> int:
        """
        Queue hide/unhide toggle edits for a set of transactions.

        This toggles the hideFromReports flag for each transaction.
        This is pure business logic - no UI dependencies. Can be tested independently.

        Args:
            transactions_df: Polars DataFrame of transactions to toggle

        Returns:
            int: Number of edits queued
        """
        # Create single timestamp for entire batch so undo recognizes them as a group
        batch_timestamp = datetime.now()
        count = 0
        for txn in transactions_df.iter_rows(named=True):
            current_hidden = txn.get("hideFromReports", False)
            self.data_manager.pending_edits.append(
                TransactionEdit(
                    transaction_id=txn["id"],
                    field="hide_from_reports",
                    old_value=current_hidden,
                    new_value=not current_hidden,
                    timestamp=batch_timestamp,
                )
            )
            count += 1
        return count

    def handle_commit_result(
        self,
        success_count: int,
        failure_count: int,
        edits: List[TransactionEdit],
        saved_state: dict,
        cache_filters: dict = None,
    ) -> None:
        """
        Handle commit results and update local state accordingly.

        This is the CRITICAL data integrity logic that prevents corruption.
        Previously this was in _review_and_commit() in app.py, mixed with
        modal handling and retry logic.

        **The Rule:**
        - If ANY commits failed → DO NOT apply edits locally
        - Only if ALL succeed → Apply edits and clear pending list

        This separation allows testing the data integrity logic without
        dealing with network/session issues.

        Args:
            success_count: Number of successful commits
            failure_count: Number of failed commits
            edits: List of edits that were attempted
            saved_state: View state to restore after commit
            cache_filters: Optional dict with year/since filters for cache

        Side effects:
            - May update data_manager.df and state.transactions_df
            - May clear data_manager.pending_edits
            - May update cache
            - Calls refresh_view() with force_rebuild=False
        """
        logger.info(f"handle_commit_result: {success_count} succeeded, {failure_count} failed")

        # CRITICAL: Only apply changes locally if ALL commits succeeded
        if failure_count > 0:
            logger.warning(f"Commit had {failure_count} failures - NOT applying edits locally")
            # Some or all commits failed - DO NOT apply to local state
            # This prevents data corruption where UI shows changes that didn't save
            # Note: View already restored in app.py before commit started
            # Just refresh to ensure UI shows current (unchanged) state
            logger.debug("Failure path - refreshing view (state already restored in app.py)")
            self.refresh_view(force_rebuild=False)
        else:
            logger.info("All commits succeeded - applying edits locally")
            # All commits succeeded - safe to apply to local state

            # Apply edits to local DataFrames for instant UI update
            # Use CommitOrchestrator to apply all edits (fully tested)
            self.data_manager.df = CommitOrchestrator.apply_edits_to_dataframe(
                self.data_manager.df,
                edits,
                self.data_manager.categories,
                self.data_manager.apply_category_groups,
            )

            # Also update state DataFrame
            if self.state.transactions_df is not None:
                self.state.transactions_df = CommitOrchestrator.apply_edits_to_dataframe(
                    self.state.transactions_df,
                    edits,
                    self.data_manager.categories,
                    self.data_manager.apply_category_groups,
                )

            # Clear pending edits on success
            self.data_manager.pending_edits.clear()
            logger.info("Cleared pending edits")

            # Update cache with edited data (if caching is enabled)
            if self.cache_manager and cache_filters:
                try:
                    logger.debug("Updating cache with committed changes")
                    self.cache_manager.save_cache(
                        transactions_df=self.data_manager.df,
                        categories=self.data_manager.categories,
                        category_groups=self.data_manager.category_groups,
                        year=cache_filters.get("year"),
                        since=cache_filters.get("since"),
                    )
                except Exception as e:
                    # Cache update failed - not critical, just log
                    logger.warning(f"Cache update failed: {e}", exc_info=True)

            # Refresh to show updated data (smooth update)
            # Note: View already restored in app.py before commit started
            logger.debug(
                f"Success path - refreshing to show updated data. Current view_mode={self.state.view_mode}, selected_category={self.state.selected_category}"
            )
            self.refresh_view(force_rebuild=False)
            logger.debug(f"After refresh: view_mode={self.state.view_mode}")

    def get_transactions_from_selected_groups(self, group_by_field: str) -> pl.DataFrame:
        """
        Get all transactions from selected groups in aggregate view.

        Args:
            group_by_field: Field to filter by ('merchant', 'category', 'group', 'account')

        Returns:
            DataFrame of all transactions from selected groups
        """
        if not self.state.selected_group_keys:
            return pl.DataFrame()

        filtered_df = self.state.get_filtered_df()
        if filtered_df is None:
            return pl.DataFrame()

        # Filter to transactions in any of the selected groups
        all_txns = pl.DataFrame()
        for group_key in self.state.selected_group_keys:
            if group_by_field == "merchant":
                group_txns = self.data_manager.filter_by_merchant(filtered_df, group_key)
            elif group_by_field == "category":
                group_txns = self.data_manager.filter_by_category(filtered_df, group_key)
            elif group_by_field == "group":
                group_txns = self.data_manager.filter_by_group(filtered_df, group_key)
            elif group_by_field == "account":
                group_txns = self.data_manager.filter_by_account(filtered_df, group_key)
            else:
                continue

            if all_txns.is_empty():
                all_txns = group_txns
            else:
                all_txns = pl.concat([all_txns, group_txns])

        return all_txns
