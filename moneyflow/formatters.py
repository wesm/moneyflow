"""
View presentation logic for transforming data into UI-ready format.

This module contains pure functions that prepare data for display,
completely decoupled from UI framework (Textual). All functions are
fully typed and testable.
"""

from dataclasses import dataclass
from typing import Any, Dict, Literal, Optional, TypedDict, Union

import polars as pl
from rich.text import Text

from .state import SortDirection, SortMode, TimeGranularity

# Type definitions for better type safety
AggregationField = Literal["merchant", "category", "group", "account", "time_period_display"]
ColumnKey = Literal["name", "count", "total"]


class ColumnSpec(TypedDict):
    """Specification for a table column."""

    label: Union[
        str, Text
    ]  # Display label (may include sort arrow, can be Rich Text for alignment)
    key: str  # Data key
    width: Optional[int]  # Column width (None = auto-fit to content)


class PreparedView(TypedDict):
    """Prepared view data ready for UI rendering."""

    columns: list[ColumnSpec]
    rows: list[tuple[str, ...]]  # Each row is a tuple of strings
    empty: bool


class TransactionFlags(TypedDict):
    """Transaction display flags."""

    selected: bool
    hidden: bool
    has_pending_edit: bool


@dataclass(frozen=True)
class ViewPresenter:
    """
    Handles presentation logic for different views.

    This class is stateless and thread-safe. All methods are static
    to emphasize the pure function nature.
    """

    @staticmethod
    def format_amount(amount: float, for_table: bool = False) -> Union[str, Text]:
        """
        Format amount with sign and thousands separators.

        Currency symbol is shown in column header, not in cells, to reduce visual noise.

        Args:
            amount: The amount to format
            for_table: If True, return Rich Text with right justification for tables

        Returns:
            Formatted string like "-1,234.56" or "+5,000.00" (no currency symbol)
            If for_table=True, returns Rich Text object with right justification
            Positive amounts (credits) are styled in green

        Examples:
            >>> ViewPresenter.format_amount(-1234.56)
            '-1,234.56'
            >>> ViewPresenter.format_amount(5000.00)
            '+5,000.00'
            >>> ViewPresenter.format_amount(0.00)
            '+0.00'
        """
        sign = "-" if amount < 0 else "+"
        abs_amount = abs(amount)
        formatted = f"{sign}{abs_amount:,.2f}"

        if for_table:
            # Color positive amounts (credits) green for visual distinction
            if amount > 0:
                return Text(formatted, justify="right", style="green")
            else:
                return Text(formatted, justify="right")
        return formatted

    @staticmethod
    def format_time_period(
        year: int,
        month: Optional[int] = None,
        day: Optional[int] = None,
        granularity: TimeGranularity = TimeGranularity.YEAR,
    ) -> str:
        """
        Format time period for display.

        Args:
            year: The year
            month: The month (1-12), or None for year-only display
            day: The day (1-31), or None for year/month display
            granularity: TIME granularity (YEAR, MONTH, or DAY)

        Returns:
            Formatted period string

        Examples:
            >>> ViewPresenter.format_time_period(2024, None, None, TimeGranularity.YEAR)
            '2024'
            >>> ViewPresenter.format_time_period(2024, 3, None, TimeGranularity.MONTH)
            'Mar 2024'
            >>> ViewPresenter.format_time_period(2024, 3, 15, TimeGranularity.DAY)
            '2024-03-15'
        """
        if granularity == TimeGranularity.YEAR or month is None:
            return str(year)
        elif granularity == TimeGranularity.DAY and day is not None:
            # Format as ISO date
            return f"{year:04d}-{month:02d}-{day:02d}"
        else:  # MONTH
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
            return f"{month_names[month - 1]} {year}"

    @staticmethod
    def get_sort_arrow(sort_by: SortMode, sort_direction: SortDirection, field: SortMode) -> str:
        """
        Get sort arrow for a column header.

        Args:
            sort_by: Current sort field
            sort_direction: Current sort direction
            field: Field to check if it's being sorted

        Returns:
            "↓" if descending, "↑" if ascending, "" if not sorted

        Examples:
            >>> ViewPresenter.get_sort_arrow(
            ...     SortMode.COUNT, SortDirection.DESC, SortMode.COUNT
            ... )
            '↓'
            >>> ViewPresenter.get_sort_arrow(
            ...     SortMode.COUNT, SortDirection.DESC, SortMode.AMOUNT
            ... )
            ''
        """
        if sort_by != field:
            return ""
        return "↓" if sort_direction == SortDirection.DESC else "↑"

    @staticmethod
    def should_sort_descending(sort_field: str, sort_direction: SortDirection) -> bool:
        """
        Determine if sorting should be descending.

        For amount/total fields, we invert the direction so that largest
        expenses (most negative) appear first by default.

        Args:
            sort_field: Field being sorted ('count', 'total', etc.)
            sort_direction: User's selected direction

        Returns:
            True if should sort descending, False otherwise

        Examples:
            >>> # For total/amount, DESC means largest expenses first (inverted)
            >>> ViewPresenter.should_sort_descending("total", SortDirection.DESC)
            False  # We invert to ASC so -1000 comes before -10

            >>> # For count, DESC means as expected
            >>> ViewPresenter.should_sort_descending("count", SortDirection.DESC)
            True
        """
        # Amount sorting: invert direction so largest expenses (-1000) come first
        if sort_field in ("total", "amount"):
            return sort_direction == SortDirection.ASC
        else:
            return sort_direction == SortDirection.DESC

    @staticmethod
    def prepare_aggregation_columns(
        group_by_field: AggregationField,
        sort_by: SortMode,
        sort_direction: SortDirection,
        column_config: Optional[Dict[str, Any]] = None,
        display_labels: Optional[Dict[str, str]] = None,
    ) -> list[ColumnSpec]:
        """
        Prepare column specifications for aggregation views.

        Args:
            group_by_field: The field to group by
            sort_by: Current sort mode
            sort_direction: Current sort direction
            column_config: Backend-specific config (widths, currency_symbol)
            display_labels: Optional backend-specific display labels

        Returns:
            List of column specifications with proper headers and arrows

        Examples:
            >>> cols = ViewPresenter.prepare_aggregation_columns(
            ...     "merchant", SortMode.COUNT, SortDirection.DESC
            ... )
            >>> cols[0]["label"]  # Name column
            'Merchant'
            >>> cols[1]["label"]  # Count column with arrow
            'Count ↓'
        """
        # Use defaults if not provided
        if column_config is None:
            column_config = {}

        if display_labels is None:
            display_labels = {"merchant": "Merchant", "account": "Account", "accounts": "Accounts"}

        # Determine display name for first column
        name_labels: dict[AggregationField, str] = {
            "merchant": display_labels.get("merchant", "Merchant"),
            "category": "Category",
            "group": "Group",
            "account": display_labels.get("account", "Account"),
            "time_period_display": "Period",
        }
        name_label = name_labels[group_by_field]

        # Extract currency symbol from config (defaults to $ if not provided)
        currency_symbol = column_config.get("currency_symbol", "$") if column_config else "$"

        # Default name width
        name_width = 40

        # Get column width based on field type
        if group_by_field == "merchant":
            name_width = column_config.get("merchant_width_pct", name_width)
        elif group_by_field == "account":
            name_width = column_config.get("account_width_pct", name_width)

        # Map aggregation field to sort mode
        field_to_sort_mode: dict[AggregationField, SortMode] = {
            "merchant": SortMode.MERCHANT,
            "category": SortMode.CATEGORY,
            "group": SortMode.GROUP,
            "account": SortMode.ACCOUNT,
            "time_period_display": SortMode.TIME_PERIOD,
        }

        # Get arrows
        name_arrow = ViewPresenter.get_sort_arrow(
            sort_by, sort_direction, field_to_sort_mode[group_by_field]
        )
        count_arrow = ViewPresenter.get_sort_arrow(sort_by, sort_direction, SortMode.COUNT)
        amount_arrow = ViewPresenter.get_sort_arrow(sort_by, sort_direction, SortMode.AMOUNT)

        # Build column specs
        # Total column label - right-aligned to match the values, includes currency
        total_label = f"Total ({currency_symbol}) {amount_arrow}".strip()
        total_label_text = Text(total_label, justify="right")

        columns: list[ColumnSpec] = [
            {
                "label": f"{name_label} {name_arrow}".strip(),
                "key": group_by_field,
                "width": name_width,
            },
            {"label": f"Count {count_arrow}".strip(), "key": "count", "width": 10},
            {"label": total_label_text, "key": "total", "width": None},  # Auto-size to content
        ]

        # Add top category column for merchant view
        if group_by_field == "merchant":
            columns.append({"label": "Top Category", "key": "top_category_display", "width": 35})

        # Flags column (always last)
        columns.append({"label": "", "key": "flags", "width": 2})

        return columns

    @staticmethod
    def format_aggregation_rows(
        df: pl.DataFrame,
        detail_df: pl.DataFrame = None,
        group_by_field: str = None,
        pending_edit_ids: set[str] = None,
        selected_group_keys: set[str] = None,
    ) -> list[tuple]:
        """
        Format aggregation DataFrame rows for display.

        Args:
            df: Aggregated DataFrame with columns: [name_field, count, total, ...]
                First column can be merchant/category/group/account
                Merchant view includes: top_category, top_category_pct
            detail_df: Optional full detail DataFrame to check for pending edits
            group_by_field: Field being grouped by (merchant/category/etc)
            pending_edit_ids: Set of transaction IDs with pending edits
            selected_group_keys: Set of selected group names (for multi-select)

        Returns:
            List of tuples:
            - Merchant view: (name, count_str, total_str, top_category_display, flags_str)
            - Other views: (name, count_str, total_str, flags_str)

        Examples:
            >>> import polars as pl
            >>> df = pl.DataFrame({
            ...     "merchant": ["Amazon", "Starbucks"],
            ...     "count": [50, 30],
            ...     "total": [-1234.56, -89.70]
            ... })
            >>> rows = ViewPresenter.format_aggregation_rows(df)
            >>> rows[0]
            ('Amazon', '50', '$-1,234.56', '')
        """
        # Pre-compute which groups have pending edits (single Polars operation)
        groups_with_pending_edits = set()
        if detail_df is not None and group_by_field and pending_edit_ids:
            # Filter to only transactions with pending edits
            pending_transactions = detail_df.filter(pl.col("id").is_in(list(pending_edit_ids)))
            if not pending_transactions.is_empty():
                # Get unique group values that have pending edits
                groups_with_pending_edits = set(
                    pending_transactions[group_by_field].unique().to_list()
                )

        rows: list[tuple] = []

        for row_dict in df.iter_rows(named=True):
            # Get the name from first column (merchant/category/group/account/time_period_display)
            name = str(row_dict.get(df.columns[0], "Unknown") or "Unknown")

            # Special formatting for time periods
            if group_by_field == "time_period_display":
                # Format time period nicely: "2024" or "Mar 2024"
                year = row_dict.get("year")
                month = row_dict.get("month")
                day = row_dict.get("day")
                # Determine granularity from whether month/day are present
                from .state import TimeGranularity

                if day:
                    granularity = TimeGranularity.DAY
                elif month:
                    granularity = TimeGranularity.MONTH
                else:
                    granularity = TimeGranularity.YEAR
                name = ViewPresenter.format_time_period(year, month, day, granularity)

            count = row_dict["count"]
            total = row_dict["total"]

            # Build flags: ✓ for selected, * for pending edits
            flags = ""
            if selected_group_keys and name in selected_group_keys:
                flags += "✓"
            if name in groups_with_pending_edits:
                flags += "*"

            # For merchant view, include top category
            if group_by_field == "merchant":
                top_category = row_dict.get("top_category", "")
                top_category_pct = row_dict.get("top_category_pct", 0)
                top_category_display = f"{top_category} {top_category_pct}%" if top_category else ""
                rows.append(
                    (
                        name,
                        str(count),
                        ViewPresenter.format_amount(total, for_table=True),
                        top_category_display,
                        flags,
                    )
                )
            else:
                rows.append(
                    (name, str(count), ViewPresenter.format_amount(total, for_table=True), flags)
                )

        return rows

    @staticmethod
    def prepare_aggregation_view(
        df: pl.DataFrame,
        group_by_field: AggregationField,
        sort_by: SortMode,
        sort_direction: SortDirection,
        detail_df: pl.DataFrame = None,
        pending_edit_ids: set[str] = None,
        selected_group_keys: set[str] = None,
        column_config: Optional[Dict[str, Any]] = None,
        display_labels: Optional[Dict[str, str]] = None,
    ) -> PreparedView:
        """
        Prepare complete aggregation view data.

        This is the main entry point for aggregation views, consolidating
        all the column/row preparation logic.

        Args:
            df: Aggregated DataFrame (already grouped and aggregated)
            group_by_field: Field used for grouping
            sort_by: Sort mode
            sort_direction: Sort direction
            detail_df: Optional full detail DataFrame to check for pending edits
            pending_edit_ids: Set of transaction IDs with pending edits

        Returns:
            PreparedView with columns and formatted rows

        Examples:
            >>> df = pl.DataFrame({
            ...     "merchant": ["Amazon"],
            ...     "count": [50],
            ...     "total": [-1234.56]
            ... })
            >>> view = ViewPresenter.prepare_aggregation_view(
            ...     df, "merchant", SortMode.COUNT, SortDirection.DESC
            ... )
            >>> view["empty"]
            False
            >>> len(view["columns"])
            4
        """
        columns = ViewPresenter.prepare_aggregation_columns(
            group_by_field, sort_by, sort_direction, column_config, display_labels
        )

        if df.is_empty():
            return PreparedView(columns=columns, rows=[], empty=True)

        rows = ViewPresenter.format_aggregation_rows(
            df, detail_df, group_by_field, pending_edit_ids, selected_group_keys
        )

        return PreparedView(columns=columns, rows=rows, empty=False)

    @staticmethod
    def prepare_transaction_columns(
        sort_by: SortMode,
        sort_direction: SortDirection,
        column_config: Optional[Dict[str, Any]] = None,
        display_labels: Optional[Dict[str, str]] = None,
    ) -> list[ColumnSpec]:
        """
        Prepare column specifications for transaction detail view.

        Args:
            sort_by: Current sort mode
            sort_direction: Current sort direction
            column_config: Backend-specific config (widths, currency_symbol)
            display_labels: Optional backend-specific display labels

        Returns:
            List of column specifications for transaction view

        Examples:
            >>> cols = ViewPresenter.prepare_transaction_columns(
            ...     SortMode.DATE, SortDirection.DESC
            ... )
            >>> cols[0]["label"]
            'Date ↓'
            >>> cols[5]["label"]  # Flags column
            ''
        """
        # Use defaults if not provided
        if column_config is None:
            column_config = {
                "merchant_width_pct": 25,
                "account_width_pct": 30,
                "currency_symbol": "$",
            }
        if display_labels is None:
            display_labels = {"merchant": "Merchant", "account": "Account", "accounts": "Accounts"}

        # Extract currency symbol from config
        currency_symbol = column_config.get("currency_symbol", "$")

        # Get arrows for each field
        date_arrow = ViewPresenter.get_sort_arrow(sort_by, sort_direction, SortMode.DATE)
        merchant_arrow = ViewPresenter.get_sort_arrow(sort_by, sort_direction, SortMode.MERCHANT)
        category_arrow = ViewPresenter.get_sort_arrow(sort_by, sort_direction, SortMode.CATEGORY)
        account_arrow = ViewPresenter.get_sort_arrow(sort_by, sort_direction, SortMode.ACCOUNT)
        amount_arrow = ViewPresenter.get_sort_arrow(sort_by, sort_direction, SortMode.AMOUNT)

        # Get custom labels
        merchant_label = display_labels.get("merchant", "Merchant")
        account_label = display_labels.get("account", "Account")

        # Get custom widths
        merchant_width = column_config.get("merchant_width_pct", 25)
        account_width = column_config.get("account_width_pct", 30)

        # Amount column label - right-aligned to match the values, includes currency
        amount_label = f"Amount ({currency_symbol}) {amount_arrow}".strip()
        amount_label_text = Text(amount_label, justify="right")

        columns: list[ColumnSpec] = [
            {"label": f"Date {date_arrow}".strip(), "key": "date", "width": 12},
            {
                "label": f"{merchant_label} {merchant_arrow}".strip(),
                "key": "merchant",
                "width": merchant_width,
            },
            {"label": f"Category {category_arrow}".strip(), "key": "category", "width": 20},
            {
                "label": f"{account_label} {account_arrow}".strip(),
                "key": "account",
                "width": account_width,
            },
            {"label": amount_label_text, "key": "amount", "width": None},  # Auto-size to content
            {"label": "", "key": "flags", "width": 3},  # Flags column (✓ H *)
        ]

        return columns

    @staticmethod
    def compute_transaction_flags(
        txn_id: str,
        selected_ids: set[str],
        hide_from_reports: bool,
        pending_edit_ids: set[str],
    ) -> str:
        """
        Compute display flags for a transaction.

        Args:
            txn_id: Transaction ID
            selected_ids: Set of selected transaction IDs
            hide_from_reports: Whether transaction is hidden
            pending_edit_ids: Set of transaction IDs with pending edits

        Returns:
            Flag string: combination of ✓ (selected), H (hidden), * (pending edit)

        Examples:
            >>> ViewPresenter.compute_transaction_flags(
            ...     "txn1", {"txn1"}, True, {"txn1"}
            ... )
            '✓H*'
            >>> ViewPresenter.compute_transaction_flags(
            ...     "txn2", set(), False, set()
            ... )
            ''
        """
        flags = ""
        if txn_id in selected_ids:
            flags += "✓"  # Selected for bulk operation
        if hide_from_reports:
            flags += "H"  # Hidden from reports
        if txn_id in pending_edit_ids:
            flags += "*"  # Has pending edit
        return flags

    @staticmethod
    def format_transaction_rows(
        df: pl.DataFrame,
        selected_ids: set[str],
        pending_edit_ids: set[str],
    ) -> list[tuple[str, str, str, str, Union[str, Text], str]]:
        """
        Format transaction DataFrame rows for display.

        Args:
            df: Transaction DataFrame
            selected_ids: Set of selected transaction IDs
            pending_edit_ids: Set of transaction IDs with pending edits

        Returns:
            List of tuples (date, merchant, category, account, amount, flags)

        Examples:
            >>> df = pl.DataFrame({
            ...     "id": ["txn1"],
            ...     "date": [pl.Date(2025, 1, 15)],
            ...     "merchant": ["Amazon"],
            ...     "category": ["Shopping"],
            ...     "account": ["Chase"],
            ...     "amount": [-99.99],
            ...     "hideFromReports": [False]
            ... })
            >>> rows = ViewPresenter.format_transaction_rows(df, set(), set())
            >>> rows[0][0]  # date
            '2025-01-15'
        """
        rows: list[tuple[str, str, str, str, Union[str, Text], str]] = []

        for row_dict in df.iter_rows(named=True):
            date = str(row_dict["date"])
            merchant = row_dict["merchant"] or "Unknown"
            category = row_dict["category"] or "Uncategorized"
            account = row_dict.get("account", "Unknown")
            amount = row_dict["amount"]
            txn_id = row_dict["id"]
            hide_from_reports = row_dict.get("hideFromReports", False)

            # Compute flags
            flags = ViewPresenter.compute_transaction_flags(
                txn_id, selected_ids, hide_from_reports, pending_edit_ids
            )

            rows.append(
                (
                    date,
                    merchant,
                    category,
                    account,
                    ViewPresenter.format_amount(amount, for_table=True),
                    flags,
                )
            )

        return rows

    @staticmethod
    def prepare_transaction_view(
        df: pl.DataFrame,
        sort_by: SortMode,
        sort_direction: SortDirection,
        selected_ids: set[str],
        pending_edit_ids: set[str],
        column_config: Optional[Dict[str, Any]] = None,
        display_labels: Optional[Dict[str, str]] = None,
    ) -> PreparedView:
        """
        Prepare complete transaction detail view data.

        Args:
            df: Transaction DataFrame (already filtered)
            sort_by: Sort mode
            sort_direction: Sort direction
            selected_ids: Set of selected transaction IDs
            pending_edit_ids: Set of transaction IDs with pending edits

        Returns:
            PreparedView with columns and formatted rows

        Examples:
            >>> df = pl.DataFrame({
            ...     "id": ["txn1"],
            ...     "date": [pl.Date(2025, 1, 15)],
            ...     "merchant": ["Amazon"],
            ...     "category": ["Shopping"],
            ...     "account": ["Chase"],
            ...     "amount": [-99.99],
            ...     "hideFromReports": [False]
            ... })
            >>> view = ViewPresenter.prepare_transaction_view(
            ...     df, SortMode.DATE, SortDirection.DESC, set(), set()
            ... )
            >>> view["empty"]
            False
        """
        columns = ViewPresenter.prepare_transaction_columns(
            sort_by, sort_direction, column_config, display_labels
        )

        if df.is_empty():
            return PreparedView(columns=columns, rows=[], empty=True)

        rows = ViewPresenter.format_transaction_rows(df, selected_ids, pending_edit_ids)

        return PreparedView(columns=columns, rows=rows, empty=False)
