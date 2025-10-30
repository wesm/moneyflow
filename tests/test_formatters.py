"""
Tests for formatters module.

These tests verify the formatting logic is pure, deterministic,
and produces correct output for all view types.
"""

from datetime import date

import polars as pl
from rich.text import Text

from moneyflow.formatters import ViewPresenter
from moneyflow.state import SortDirection, SortMode


def normalize_label(label):
    """Convert Text label to plain string for comparison."""
    from rich.text import Text

    return label.plain if isinstance(label, Text) else label


def normalize_row(row: tuple) -> tuple:
    """Convert Text objects in row to plain strings for comparison."""
    return tuple(item.plain if isinstance(item, Text) else item for item in row)


class TestSortArrow:
    """Tests for get_sort_arrow method."""

    def test_descending_arrow_when_sorted_by_field(self):
        """Should return ↓ when field is sorted descending."""
        arrow = ViewPresenter.get_sort_arrow(SortMode.COUNT, SortDirection.DESC, SortMode.COUNT)
        assert arrow == "↓"

    def test_ascending_arrow_when_sorted_by_field(self):
        """Should return ↑ when field is sorted ascending."""
        arrow = ViewPresenter.get_sort_arrow(SortMode.COUNT, SortDirection.ASC, SortMode.COUNT)
        assert arrow == "↑"

    def test_no_arrow_when_not_sorted_by_field(self):
        """Should return empty string when different field is sorted."""
        arrow = ViewPresenter.get_sort_arrow(SortMode.COUNT, SortDirection.DESC, SortMode.AMOUNT)
        assert arrow == ""

    def test_amount_field_with_descending(self):
        """Should show correct arrow for amount field."""
        arrow = ViewPresenter.get_sort_arrow(SortMode.AMOUNT, SortDirection.DESC, SortMode.AMOUNT)
        assert arrow == "↓"


class TestShouldSortDescending:
    """Tests for should_sort_descending method."""

    def test_amount_field_inverts_direction(self):
        """Amount fields should invert direction for expense-first sorting."""
        # DESC direction for amount becomes ASC (so -1000 comes before -10)
        assert ViewPresenter.should_sort_descending("total", SortDirection.DESC) is False
        assert ViewPresenter.should_sort_descending("amount", SortDirection.DESC) is False

    def test_amount_field_asc_becomes_desc(self):
        """ASC direction for amount becomes DESC."""
        assert ViewPresenter.should_sort_descending("total", SortDirection.ASC) is True
        assert ViewPresenter.should_sort_descending("amount", SortDirection.ASC) is True

    def test_count_field_no_inversion(self):
        """Count field should not invert direction."""
        assert ViewPresenter.should_sort_descending("count", SortDirection.DESC) is True
        assert ViewPresenter.should_sort_descending("count", SortDirection.ASC) is False

    def test_other_fields_no_inversion(self):
        """Other fields should not invert direction."""
        assert ViewPresenter.should_sort_descending("name", SortDirection.DESC) is True
        assert ViewPresenter.should_sort_descending("date", SortDirection.ASC) is False


class TestPrepareAggregationColumns:
    """Tests for prepare_aggregation_columns method."""

    def test_merchant_columns(self):
        """Should create correct columns for merchant view."""
        cols = ViewPresenter.prepare_aggregation_columns(
            "merchant", SortMode.COUNT, SortDirection.DESC
        )

        assert len(cols) == 5  # merchant, count, total, top_category_display, flags
        assert cols[0]["label"] == "Merchant"
        assert cols[0]["key"] == "merchant"
        assert cols[0]["width"] == 40
        assert cols[3]["label"] == "Top Category"
        assert cols[3]["key"] == "top_category_display"
        assert cols[3]["width"] == 35
        assert cols[4]["key"] == "flags"

    def test_merchant_columns_with_custom_config(self):
        """Should use custom column widths when provided."""
        column_config = {"merchant_width_pct": 55}
        cols = ViewPresenter.prepare_aggregation_columns(
            "merchant", SortMode.COUNT, SortDirection.DESC, column_config
        )

        assert cols[0]["width"] == 55  # Custom width applied

    def test_merchant_columns_with_custom_labels(self):
        """Should use custom labels when provided."""
        display_labels = {"merchant": "Item Name", "account": "Order", "accounts": "Orders"}
        cols = ViewPresenter.prepare_aggregation_columns(
            "merchant", SortMode.COUNT, SortDirection.DESC, None, display_labels
        )

        assert cols[0]["label"] == "Item Name"

        assert cols[1]["label"] == "Count ↓"
        assert cols[1]["key"] == "count"

        assert normalize_label(cols[2]["label"]) == "Total ($)"
        assert cols[2]["key"] == "total"

        assert cols[3]["label"] == "Top Category"
        assert cols[3]["key"] == "top_category_display"

        assert cols[4]["label"] == ""
        assert cols[4]["key"] == "flags"

    def test_category_columns(self):
        """Should create correct columns for category view."""
        cols = ViewPresenter.prepare_aggregation_columns(
            "category", SortMode.AMOUNT, SortDirection.ASC
        )

        assert cols[0]["label"] == "Category"
        assert cols[1]["label"] == "Count"
        assert normalize_label(cols[2]["label"]) == "Total ($) ↑"

    def test_group_columns(self):
        """Should create correct columns for group view."""
        cols = ViewPresenter.prepare_aggregation_columns("group", SortMode.COUNT, SortDirection.ASC)

        assert cols[0]["label"] == "Group"
        assert cols[1]["label"] == "Count ↑"

    def test_account_columns(self):
        """Should create correct columns for account view."""
        cols = ViewPresenter.prepare_aggregation_columns(
            "account", SortMode.AMOUNT, SortDirection.DESC
        )

        assert cols[0]["label"] == "Account"
        assert normalize_label(cols[2]["label"]) == "Total ($) ↓"

    def test_merchant_sorted_by_merchant_desc(self):
        """Should show arrow in merchant column when sorted by merchant descending."""
        cols = ViewPresenter.prepare_aggregation_columns(
            "merchant", SortMode.MERCHANT, SortDirection.DESC
        )

        assert cols[0]["label"] == "Merchant ↓"
        assert cols[1]["label"] == "Count"
        assert normalize_label(cols[2]["label"]) == "Total ($)"

    def test_merchant_sorted_by_merchant_asc(self):
        """Should show arrow in merchant column when sorted by merchant ascending."""
        cols = ViewPresenter.prepare_aggregation_columns(
            "merchant", SortMode.MERCHANT, SortDirection.ASC
        )

        assert cols[0]["label"] == "Merchant ↑"

    def test_category_sorted_by_category_desc(self):
        """Should show arrow in category column when sorted by category descending."""
        cols = ViewPresenter.prepare_aggregation_columns(
            "category", SortMode.CATEGORY, SortDirection.DESC
        )

        assert cols[0]["label"] == "Category ↓"
        assert cols[1]["label"] == "Count"
        assert normalize_label(cols[2]["label"]) == "Total ($)"

    def test_category_sorted_by_category_asc(self):
        """Should show arrow in category column when sorted by category ascending."""
        cols = ViewPresenter.prepare_aggregation_columns(
            "category", SortMode.CATEGORY, SortDirection.ASC
        )

        assert cols[0]["label"] == "Category ↑"

    def test_group_sorted_by_group_desc(self):
        """Should show arrow in group column when sorted by group descending."""
        cols = ViewPresenter.prepare_aggregation_columns(
            "group", SortMode.GROUP, SortDirection.DESC
        )

        assert cols[0]["label"] == "Group ↓"

    def test_group_sorted_by_group_asc(self):
        """Should show arrow in group column when sorted by group ascending."""
        cols = ViewPresenter.prepare_aggregation_columns("group", SortMode.GROUP, SortDirection.ASC)

        assert cols[0]["label"] == "Group ↑"

    def test_account_sorted_by_account_desc(self):
        """Should show arrow in account column when sorted by account descending."""
        cols = ViewPresenter.prepare_aggregation_columns(
            "account", SortMode.ACCOUNT, SortDirection.DESC
        )

        assert cols[0]["label"] == "Account ↓"

    def test_account_sorted_by_account_asc(self):
        """Should show arrow in account column when sorted by account ascending."""
        cols = ViewPresenter.prepare_aggregation_columns(
            "account", SortMode.ACCOUNT, SortDirection.ASC
        )

        assert cols[0]["label"] == "Account ↑"


class TestFormatAggregationRows:
    """Tests for format_aggregation_rows method."""

    def test_formats_basic_merchant_rows(self):
        """Should format merchant aggregation rows correctly."""
        df = pl.DataFrame(
            {"merchant": ["Amazon", "Starbucks"], "count": [50, 30], "total": [-1234.56, -89.70]}
        )

        rows = ViewPresenter.format_aggregation_rows(df)

        assert len(rows) == 2
        assert normalize_row(rows[0]) == ("Amazon", "50", "-1,234.56", "")
        assert normalize_row(rows[1]) == ("Starbucks", "30", "-89.70", "")

    def test_formats_merchant_rows_with_top_category(self):
        """Should format merchant rows with top category column."""
        df = pl.DataFrame(
            {
                "merchant": ["Whole Foods", "Starbucks"],
                "count": [10, 20],
                "total": [-150.00, -80.00],
                "top_category": ["Groceries", "Coffee Shops"],
                "top_category_pct": [100, 85],
            }
        )

        rows = ViewPresenter.format_aggregation_rows(df, group_by_field="merchant")

        assert len(rows) == 2
        assert normalize_row(rows[0]) == ("Whole Foods", "10", "-150.00", "Groceries 100%", "")
        assert normalize_row(rows[1]) == ("Starbucks", "20", "-80.00", "Coffee Shops 85%", "")

    def test_formats_category_rows(self):
        """Should format category aggregation rows correctly."""
        df = pl.DataFrame(
            {"category": ["Groceries", "Dining"], "count": [100, 45], "total": [-2500.00, -567.89]}
        )

        rows = ViewPresenter.format_aggregation_rows(df)

        assert normalize_row(rows[0]) == ("Groceries", "100", "-2,500.00", "")
        assert normalize_row(rows[1]) == ("Dining", "45", "-567.89", "")

    def test_handles_null_names(self):
        """Should handle null merchant/category names."""
        df = pl.DataFrame(
            {"merchant": [None, "Amazon"], "count": [5, 10], "total": [-50.00, -100.00]}
        )

        rows = ViewPresenter.format_aggregation_rows(df)

        assert rows[0][0] == "Unknown"  # None becomes Unknown
        assert rows[1][0] == "Amazon"

    def test_handles_empty_dataframe(self):
        """Should return empty list for empty DataFrame."""
        df = pl.DataFrame(
            {"merchant": [], "count": [], "total": []},
            schema={"merchant": pl.Utf8, "count": pl.Int64, "total": pl.Float64},
        )

        rows = ViewPresenter.format_aggregation_rows(df)

        assert rows == []

    def test_formats_large_numbers(self):
        """Should format large numbers with commas."""
        df = pl.DataFrame({"merchant": ["BigCorp"], "count": [1000], "total": [-123456.78]})

        rows = ViewPresenter.format_aggregation_rows(df)

        assert normalize_row(rows[0]) == ("BigCorp", "1000", "-123,456.78", "")

    def test_formats_positive_amounts(self):
        """Should format positive amounts (income) correctly."""
        df = pl.DataFrame({"merchant": ["Employer"], "count": [2], "total": [5000.00]})

        rows = ViewPresenter.format_aggregation_rows(df)

        assert normalize_row(rows[0]) == ("Employer", "2", "+5,000.00", "")

    def test_shows_pending_edit_indicator(self):
        """Should show * for groups with pending edits."""
        # Aggregated data (merchant view includes top_category)
        agg_df = pl.DataFrame(
            {
                "merchant": ["Amazon", "Starbucks", "Target"],
                "count": [50, 30, 20],
                "total": [-1234.56, -89.70, -456.78],
                "top_category": ["Shopping", "Coffee Shops", "Shopping"],
                "top_category_pct": [80, 100, 90],
            }
        )

        # Detail data with full transactions
        detail_df = pl.DataFrame(
            {
                "id": ["txn1", "txn2", "txn3", "txn4"],
                "merchant": ["Amazon", "Amazon", "Starbucks", "Target"],
                "amount": [-100.0, -200.0, -89.70, -456.78],
            }
        )

        # Amazon has pending edits (txn1 and txn2)
        pending_edit_ids = {"txn1", "txn2"}

        rows = ViewPresenter.format_aggregation_rows(
            agg_df,
            detail_df=detail_df,
            group_by_field="merchant",
            pending_edit_ids=pending_edit_ids,
        )

        assert normalize_row(rows[0]) == (
            "Amazon",
            "50",
            "-1,234.56",
            "Shopping 80%",
            "*",
        )  # Has pending edits
        assert normalize_row(rows[1]) == (
            "Starbucks",
            "30",
            "-89.70",
            "Coffee Shops 100%",
            "",
        )  # No pending edits
        assert normalize_row(rows[2]) == (
            "Target",
            "20",
            "-456.78",
            "Shopping 90%",
            "",
        )  # No pending edits

    def test_no_pending_edits_shows_empty_flag(self):
        """Should show empty flags when no pending edits."""
        agg_df = pl.DataFrame(
            {"category": ["Groceries", "Dining"], "count": [100, 45], "total": [-2500.00, -567.89]}
        )

        detail_df = pl.DataFrame(
            {
                "id": ["txn1", "txn2"],
                "category": ["Groceries", "Dining"],
                "amount": [-2500.0, -567.89],
            }
        )

        # No pending edits
        pending_edit_ids = set()

        rows = ViewPresenter.format_aggregation_rows(
            agg_df,
            detail_df=detail_df,
            group_by_field="category",
            pending_edit_ids=pending_edit_ids,
        )

        assert normalize_row(rows[0]) == ("Groceries", "100", "-2,500.00", "")
        assert normalize_row(rows[1]) == ("Dining", "45", "-567.89", "")

    def test_pending_edits_without_detail_df(self):
        """Should handle missing detail_df gracefully."""
        agg_df = pl.DataFrame(
            {
                "merchant": ["Amazon"],
                "count": [50],
                "total": [-1234.56],
                "top_category": ["Shopping"],
                "top_category_pct": [85],
            }
        )

        # No detail_df provided
        rows = ViewPresenter.format_aggregation_rows(
            agg_df, detail_df=None, group_by_field="merchant", pending_edit_ids={"txn1"}
        )

        assert normalize_row(rows[0]) == (
            "Amazon",
            "50",
            "-1,234.56",
            "Shopping 85%",
            "",
        )  # No pending indicator without detail_df


class TestPrepareAggregationView:
    """Tests for prepare_aggregation_view method."""

    def test_complete_merchant_view(self):
        """Should prepare complete merchant view."""
        df = pl.DataFrame(
            {
                "merchant": ["Amazon", "Starbucks"],
                "count": [50, 30],
                "total": [-1234.56, -89.70],
                "top_category": ["Shopping", "Coffee Shops"],
                "top_category_pct": [90, 100],
            }
        )

        view = ViewPresenter.prepare_aggregation_view(
            df, "merchant", SortMode.COUNT, SortDirection.DESC
        )

        assert view["empty"] is False
        assert len(view["columns"]) == 5  # merchant, count, total, top_category_display, flags
        assert len(view["rows"]) == 2
        assert view["columns"][0]["label"] == "Merchant"
        assert normalize_row(view["rows"][0]) == ("Amazon", "50", "-1,234.56", "Shopping 90%", "")

    def test_empty_dataframe_view(self):
        """Should handle empty DataFrame gracefully."""
        df = pl.DataFrame(
            {"merchant": [], "count": [], "total": []},
            schema={"merchant": pl.Utf8, "count": pl.Int64, "total": pl.Float64},
        )

        view = ViewPresenter.prepare_aggregation_view(
            df, "merchant", SortMode.AMOUNT, SortDirection.ASC
        )

        assert view["empty"] is True
        assert len(view["columns"]) == 5  # Merchant view has 5 columns
        assert view["rows"] == []

    def test_category_view_with_sort_indicators(self):
        """Should include sort indicators in headers."""
        df = pl.DataFrame({"category": ["Groceries"], "count": [100], "total": [-2500.00]})

        view = ViewPresenter.prepare_aggregation_view(
            df, "category", SortMode.AMOUNT, SortDirection.DESC
        )

        # Find the Total column
        total_col = [c for c in view["columns"] if c["key"] == "total"][0]
        assert "↓" in total_col["label"]


class TestPrepareTransactionColumns:
    """Tests for prepare_transaction_columns method."""

    def test_creates_all_transaction_columns(self):
        """Should create all 6 transaction columns."""
        cols = ViewPresenter.prepare_transaction_columns(SortMode.DATE, SortDirection.DESC)

        assert len(cols) == 6
        keys = [c["key"] for c in cols]
        assert keys == ["date", "merchant", "category", "account", "amount", "flags"]

    def test_date_sort_indicator(self):
        """Should show arrow on date column when sorted by date."""
        cols = ViewPresenter.prepare_transaction_columns(SortMode.DATE, SortDirection.DESC)

        assert "↓" in cols[0]["label"]  # Date column
        assert "↓" not in cols[1]["label"]  # Merchant column

    def test_merchant_sort_indicator(self):
        """Should show arrow on merchant column when sorted by merchant."""
        cols = ViewPresenter.prepare_transaction_columns(SortMode.MERCHANT, SortDirection.ASC)

        merchant_col = [c for c in cols if c["key"] == "merchant"][0]
        assert "↑" in merchant_col["label"]

    def test_amount_sort_indicator(self):
        """Should show arrow on amount column when sorted by amount."""
        cols = ViewPresenter.prepare_transaction_columns(SortMode.AMOUNT, SortDirection.DESC)

        amount_col = [c for c in cols if c["key"] == "amount"][0]
        assert "↓" in amount_col["label"]

    def test_flags_column_empty_label(self):
        """Flags column should have empty label."""
        cols = ViewPresenter.prepare_transaction_columns(SortMode.DATE, SortDirection.DESC)

        flags_col = [c for c in cols if c["key"] == "flags"][0]
        assert flags_col["label"] == ""


class TestComputeTransactionFlags:
    """Tests for compute_transaction_flags method."""

    def test_no_flags(self):
        """Should return empty string when no flags apply."""
        flags = ViewPresenter.compute_transaction_flags("txn1", set(), False, set())
        assert flags == ""

    def test_selected_flag(self):
        """Should show ✓ when transaction is selected."""
        flags = ViewPresenter.compute_transaction_flags("txn1", {"txn1"}, False, set())
        assert flags == "✓"

    def test_hidden_flag(self):
        """Should show H when transaction is hidden."""
        flags = ViewPresenter.compute_transaction_flags("txn1", set(), True, set())
        assert flags == "H"

    def test_pending_edit_flag(self):
        """Should show * when transaction has pending edit."""
        flags = ViewPresenter.compute_transaction_flags("txn1", set(), False, {"txn1"})
        assert flags == "*"

    def test_all_flags_combined(self):
        """Should combine all flags in correct order."""
        flags = ViewPresenter.compute_transaction_flags("txn1", {"txn1"}, True, {"txn1"})
        assert flags == "✓H*"

    def test_selected_and_hidden(self):
        """Should combine selected and hidden flags."""
        flags = ViewPresenter.compute_transaction_flags("txn1", {"txn1"}, True, set())
        assert flags == "✓H"

    def test_hidden_and_pending(self):
        """Should combine hidden and pending flags."""
        flags = ViewPresenter.compute_transaction_flags("txn1", set(), True, {"txn1"})
        assert flags == "H*"


class TestFormatTransactionRows:
    """Tests for format_transaction_rows method."""

    def test_formats_basic_transaction(self):
        """Should format basic transaction row."""
        df = pl.DataFrame(
            {
                "id": ["txn1"],
                "date": [date(2025, 1, 15)],
                "merchant": ["Amazon"],
                "category": ["Shopping"],
                "account": ["Chase"],
                "amount": [-99.99],
                "hideFromReports": [False],
            }
        )

        rows = ViewPresenter.format_transaction_rows(df, set(), set())

        assert len(rows) == 1
        assert normalize_row(rows[0]) == (
            "2025-01-15",
            "Amazon",
            "Shopping",
            "Chase",
            "-99.99",
            "",
        )

    def test_formats_multiple_transactions(self):
        """Should format multiple transactions."""
        df = pl.DataFrame(
            {
                "id": ["txn1", "txn2"],
                "date": [date(2025, 1, 15), date(2025, 1, 16)],
                "merchant": ["Amazon", "Starbucks"],
                "category": ["Shopping", "Dining"],
                "account": ["Chase", "Amex"],
                "amount": [-99.99, -5.50],
                "hideFromReports": [False, False],
            }
        )

        rows = ViewPresenter.format_transaction_rows(df, set(), set())

        assert len(rows) == 2
        assert normalize_row(rows[1]) == ("2025-01-16", "Starbucks", "Dining", "Amex", "-5.50", "")

    def test_includes_selected_flag(self):
        """Should include ✓ flag for selected transactions."""
        df = pl.DataFrame(
            {
                "id": ["txn1"],
                "date": [date(2025, 1, 15)],
                "merchant": ["Amazon"],
                "category": ["Shopping"],
                "account": ["Chase"],
                "amount": [-99.99],
                "hideFromReports": [False],
            }
        )

        rows = ViewPresenter.format_transaction_rows(df, {"txn1"}, set())

        assert rows[0][5] == "✓"  # Flags column

    def test_includes_hidden_flag(self):
        """Should include H flag for hidden transactions."""
        df = pl.DataFrame(
            {
                "id": ["txn1"],
                "date": [date(2025, 1, 15)],
                "merchant": ["Amazon"],
                "category": ["Shopping"],
                "account": ["Chase"],
                "amount": [-99.99],
                "hideFromReports": [True],
            }
        )

        rows = ViewPresenter.format_transaction_rows(df, set(), set())

        assert rows[0][5] == "H"

    def test_includes_pending_edit_flag(self):
        """Should include * flag for transactions with pending edits."""
        df = pl.DataFrame(
            {
                "id": ["txn1"],
                "date": [date(2025, 1, 15)],
                "merchant": ["Amazon"],
                "category": ["Shopping"],
                "account": ["Chase"],
                "amount": [-99.99],
                "hideFromReports": [False],
            }
        )

        rows = ViewPresenter.format_transaction_rows(df, set(), {"txn1"})

        assert rows[0][5] == "*"

    def test_all_flags_combined_in_row(self):
        """Should show all flags for a transaction."""
        df = pl.DataFrame(
            {
                "id": ["txn1"],
                "date": [date(2025, 1, 15)],
                "merchant": ["Amazon"],
                "category": ["Shopping"],
                "account": ["Chase"],
                "amount": [-99.99],
                "hideFromReports": [True],
            }
        )

        rows = ViewPresenter.format_transaction_rows(df, {"txn1"}, {"txn1"})

        assert rows[0][5] == "✓H*"

    def test_handles_null_merchant(self):
        """Should show 'Unknown' for null merchant."""
        df = pl.DataFrame(
            {
                "id": ["txn1"],
                "date": [date(2025, 1, 15)],
                "merchant": [None],
                "category": ["Shopping"],
                "account": ["Chase"],
                "amount": [-99.99],
                "hideFromReports": [False],
            }
        )

        rows = ViewPresenter.format_transaction_rows(df, set(), set())

        assert rows[0][1] == "Unknown"

    def test_handles_null_category(self):
        """Should show 'Uncategorized' for null category."""
        df = pl.DataFrame(
            {
                "id": ["txn1"],
                "date": [date(2025, 1, 15)],
                "merchant": ["Amazon"],
                "category": [None],
                "account": ["Chase"],
                "amount": [-99.99],
                "hideFromReports": [False],
            }
        )

        rows = ViewPresenter.format_transaction_rows(df, set(), set())

        assert rows[0][2] == "Uncategorized"

    def test_formats_large_amount(self):
        """Should format large amounts with commas."""
        df = pl.DataFrame(
            {
                "id": ["txn1"],
                "date": [date(2025, 1, 15)],
                "merchant": ["BigPurchase"],
                "category": ["Shopping"],
                "account": ["Chase"],
                "amount": [-12345.67],
                "hideFromReports": [False],
            }
        )

        rows = ViewPresenter.format_transaction_rows(df, set(), set())

        amount_field = rows[0][4]
        # Amount field is Text object when for_table=True
        assert hasattr(amount_field, "plain")
        assert amount_field.plain == "-12,345.67"  # type: ignore[union-attr]

    def test_formats_positive_amount(self):
        """Should format positive amounts (income)."""
        df = pl.DataFrame(
            {
                "id": ["txn1"],
                "date": [date(2025, 1, 15)],
                "merchant": ["Employer"],
                "category": ["Paycheck"],
                "account": ["Chase"],
                "amount": [5000.00],
                "hideFromReports": [False],
            }
        )

        rows = ViewPresenter.format_transaction_rows(df, set(), set())

        amount_field = rows[0][4]
        # Amount field is Text object when for_table=True
        assert hasattr(amount_field, "plain")
        assert amount_field.plain == "+5,000.00"  # type: ignore[union-attr]


class TestPrepareTransactionView:
    """Tests for prepare_transaction_view method."""

    def test_complete_transaction_view(self):
        """Should prepare complete transaction view."""
        df = pl.DataFrame(
            {
                "id": ["txn1", "txn2"],
                "date": [date(2025, 1, 15), date(2025, 1, 16)],
                "merchant": ["Amazon", "Starbucks"],
                "category": ["Shopping", "Dining"],
                "account": ["Chase", "Amex"],
                "amount": [-99.99, -5.50],
                "hideFromReports": [False, True],
            }
        )

        view = ViewPresenter.prepare_transaction_view(
            df, SortMode.DATE, SortDirection.DESC, set(), {"txn2"}
        )

        assert view["empty"] is False
        assert len(view["columns"]) == 6
        assert len(view["rows"]) == 2
        # Check flags are computed
        assert view["rows"][1][5] == "H*"  # txn2 has hidden + pending edit

    def test_empty_transaction_view(self):
        """Should handle empty transaction DataFrame."""
        df = pl.DataFrame(
            {
                "id": [],
                "date": [],
                "merchant": [],
                "category": [],
                "account": [],
                "amount": [],
                "hideFromReports": [],
            },
            schema={
                "id": pl.Utf8,
                "date": pl.Date,
                "merchant": pl.Utf8,
                "category": pl.Utf8,
                "account": pl.Utf8,
                "amount": pl.Float64,
                "hideFromReports": pl.Boolean,
            },
        )

        view = ViewPresenter.prepare_transaction_view(
            df, SortMode.DATE, SortDirection.DESC, set(), set()
        )

        assert view["empty"] is True
        assert len(view["columns"]) == 6
        assert view["rows"] == []

    def test_sort_indicators_in_headers(self):
        """Should include sort indicators in column headers."""
        df = pl.DataFrame(
            {
                "id": ["txn1"],
                "date": [date(2025, 1, 15)],
                "merchant": ["Amazon"],
                "category": ["Shopping"],
                "account": ["Chase"],
                "amount": [-99.99],
                "hideFromReports": [False],
            }
        )

        view = ViewPresenter.prepare_transaction_view(
            df, SortMode.AMOUNT, SortDirection.ASC, set(), set()
        )

        # Find amount column and check for arrow
        amount_col = [c for c in view["columns"] if c["key"] == "amount"][0]
        assert "↑" in amount_col["label"]


class TestFormatAmount:
    """Tests for format_amount static method."""

    def test_formats_negative_amount_with_sign_outside(self):
        """Should format negative amount with - sign outside dollar sign."""
        result = ViewPresenter.format_amount(-1234.56)
        assert result == "-1,234.56"

    def test_formats_positive_amount_with_plus_sign(self):
        """Should format positive amount with + sign outside dollar sign."""
        result = ViewPresenter.format_amount(5000.00)
        assert result == "+5,000.00"

    def test_formats_zero_with_plus_sign(self):
        """Should format zero with + sign."""
        result = ViewPresenter.format_amount(0.00)
        assert result == "+0.00"

    def test_formats_small_negative_amount(self):
        """Should format small negative amounts correctly."""
        result = ViewPresenter.format_amount(-5.50)
        assert result == "-5.50"

    def test_formats_large_negative_with_commas(self):
        """Should format large negative amounts with commas."""
        result = ViewPresenter.format_amount(-123456.78)
        assert result == "-123,456.78"

    def test_formats_large_positive_with_commas(self):
        """Should format large positive amounts with commas."""
        result = ViewPresenter.format_amount(99999.99)
        assert result == "+99,999.99"

    def test_formats_with_two_decimal_places(self):
        """Should always format with exactly 2 decimal places."""
        result = ViewPresenter.format_amount(-99.9)
        assert result == "-99.90"

    def test_handles_very_small_negative(self):
        """Should handle amounts less than a dollar."""
        result = ViewPresenter.format_amount(-0.99)
        assert result == "-0.99"

    def test_handles_very_small_positive(self):
        """Should handle small positive amounts."""
        result = ViewPresenter.format_amount(0.01)
        assert result == "+0.01"


class TestViewPresenterIntegration:
    """Integration tests combining multiple presenter methods."""

    def test_aggregation_to_transaction_workflow(self):
        """Test typical workflow: aggregate view -> transaction view."""
        # Start with aggregated data
        agg_df = pl.DataFrame(
            {"merchant": ["Amazon", "Starbucks"], "count": [50, 30], "total": [-1234.56, -89.70]}
        )

        agg_view = ViewPresenter.prepare_aggregation_view(
            agg_df, "merchant", SortMode.AMOUNT, SortDirection.DESC
        )

        assert not agg_view["empty"]
        assert len(agg_view["rows"]) == 2

        # Drill down to transactions
        txn_df = pl.DataFrame(
            {
                "id": ["txn1", "txn2"],
                "date": [date(2025, 1, 15), date(2025, 1, 16)],
                "merchant": ["Amazon", "Amazon"],
                "category": ["Shopping", "Shopping"],
                "account": ["Chase", "Chase"],
                "amount": [-99.99, -134.57],
                "hideFromReports": [False, False],
            }
        )

        txn_view = ViewPresenter.prepare_transaction_view(
            txn_df, SortMode.DATE, SortDirection.DESC, set(), set()
        )

        assert not txn_view["empty"]
        assert len(txn_view["rows"]) == 2

    def test_handles_real_world_data_size(self):
        """Should handle realistic data volumes efficiently."""
        # Create 1000 transactions
        import random

        merchants = ["Amazon", "Starbucks", "Whole Foods", "Target", "Costco"]

        txn_df = pl.DataFrame(
            {
                "id": [f"txn{i}" for i in range(1000)],
                "date": [date(2025, 1, 15) for _ in range(1000)],
                "merchant": [random.choice(merchants) for _ in range(1000)],
                "category": ["Shopping" for _ in range(1000)],
                "account": ["Chase" for _ in range(1000)],
                "amount": [random.uniform(-200, -10) for _ in range(1000)],
                "hideFromReports": [False for _ in range(1000)],
            }
        )

        view = ViewPresenter.prepare_transaction_view(
            txn_df, SortMode.DATE, SortDirection.DESC, set(), set()
        )

        assert len(view["rows"]) == 1000
        assert not view["empty"]
