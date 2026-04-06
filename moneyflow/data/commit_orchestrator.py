"""
Commit orchestration logic for applying edits to DataFrames.

This module contains the critical business logic for:
1. Applying committed edits to local DataFrames (instant UI update)
2. Updating category names and groups after category changes
3. Pure, testable DataFrame transformations

All functions are fully typed and have no UI dependencies.
"""

from typing import Callable

import polars as pl

from .state import TransactionEdit


def apply_merchant_edit(df: pl.DataFrame, transaction_id: str, new_merchant: str) -> pl.DataFrame:
    """
    Apply merchant edit to a single transaction by ID.

    Use this for backends like Monarch Money that update transactions individually.

    Args:
        df: Transaction DataFrame
        transaction_id: ID of transaction to update
        new_merchant: New merchant name

    Returns:
        Updated DataFrame with merchant changed for the specific transaction

    Examples:
        >>> df = pl.DataFrame({
        ...     "id": ["txn1", "txn2"],
        ...     "merchant": ["Amazon", "Starbucks"]
        ... })
        >>> updated = apply_merchant_edit(df, "txn1", "Whole Foods")
        >>> updated.filter(pl.col("id") == "txn1")["merchant"][0]
        'Whole Foods'
        >>> updated.filter(pl.col("id") == "txn2")["merchant"][0]
        'Starbucks'
    """
    return df.with_columns(
        pl.when(pl.col("id") == transaction_id)
        .then(pl.lit(new_merchant))
        .otherwise(pl.col("merchant"))
        .alias("merchant")
    )


def apply_bulk_merchant_edit(
    df: pl.DataFrame, old_merchant: str, new_merchant: str
) -> pl.DataFrame:
    """
    Apply merchant edit to ALL transactions with the old merchant name.

    Use this for backends like YNAB that update payees (affecting all transactions).

    Args:
        df: Transaction DataFrame
        old_merchant: Old merchant name to match
        new_merchant: New merchant name

    Returns:
        Updated DataFrame with merchant changed for all matching transactions

    Examples:
        >>> df = pl.DataFrame({
        ...     "id": ["txn1", "txn2", "txn3"],
        ...     "merchant": ["Amazon", "Amazon", "Starbucks"]
        ... })
        >>> updated = apply_bulk_merchant_edit(df, "Amazon", "Whole Foods")
        >>> updated.filter(pl.col("id") == "txn1")["merchant"][0]
        'Whole Foods'
        >>> updated.filter(pl.col("id") == "txn2")["merchant"][0]
        'Whole Foods'
        >>> updated.filter(pl.col("id") == "txn3")["merchant"][0]
        'Starbucks'
    """
    return df.with_columns(
        pl.when(pl.col("merchant") == old_merchant)
        .then(pl.lit(new_merchant))
        .otherwise(pl.col("merchant"))
        .alias("merchant")
    )


def apply_category_edit(
    df: pl.DataFrame,
    transaction_id: str,
    new_category_id: str,
    category_name: str,
    category_group: str,
) -> pl.DataFrame:
    """
    Apply category edit to DataFrame.

    Args:
        df: Transaction DataFrame
        transaction_id: ID of transaction to update
        new_category_id: New category ID
        category_name: New category name (looked up from categories dict)
        category_group: New category group (looked up from categories dict)

    Returns:
        Updated DataFrame with category_id, category, and group updated

    Examples:
        >>> df = pl.DataFrame({
        ...     "id": ["txn1"],
        ...     "category_id": ["cat1"],
        ...     "category": ["Old Category"],
        ...     "group": ["Old Group"]
        ... })
        >>> updated = apply_category_edit(
        ...     df, "txn1", "cat2", "New Category", "New Group"
        ... )
        >>> updated["category"][0]
        'New Category'
        >>> updated["group"][0]
        'New Group'
    """
    return df.with_columns(
        pl.when(pl.col("id") == transaction_id)
        .then(pl.lit(new_category_id))
        .otherwise(pl.col("category_id"))
        .alias("category_id"),
        pl.when(pl.col("id") == transaction_id)
        .then(pl.lit(category_name))
        .otherwise(pl.col("category"))
        .alias("category"),
        pl.when(pl.col("id") == transaction_id)
        .then(pl.lit(category_group))
        .otherwise(pl.col("group"))
        .alias("group"),
    )


def apply_hide_from_reports_edit(
    df: pl.DataFrame, transaction_id: str, new_hide_value: bool
) -> pl.DataFrame:
    """
    Apply hide_from_reports edit to DataFrame.

    Args:
        df: Transaction DataFrame
        transaction_id: ID of transaction to update
        new_hide_value: New hide_from_reports value

    Returns:
        Updated DataFrame with hideFromReports changed

    Examples:
        >>> df = pl.DataFrame({
        ...     "id": ["txn1", "txn2"],
        ...     "hideFromReports": [False, False]
        ... })
        >>> updated = apply_hide_from_reports_edit(
        ...     df, "txn1", True
        ... )
        >>> updated.filter(pl.col("id") == "txn1")["hideFromReports"][0]
        True
        >>> updated.filter(pl.col("id") == "txn2")["hideFromReports"][0]
        False
    """
    return df.with_columns(
        pl.when(pl.col("id") == transaction_id)
        .then(pl.lit(new_hide_value))
        .otherwise(pl.col("hideFromReports"))
        .alias("hideFromReports")
    )


def apply_edit_to_dataframe(
    df: pl.DataFrame,
    edit: TransactionEdit,
    categories: dict[str, dict],
    *,
    bulk_merchant_renames: set[tuple[str, str]] | None = None,
) -> pl.DataFrame:
    """
    Apply a single edit to DataFrame.

    Dispatcher function that calls the appropriate edit function
    based on edit.field.

    Args:
        df: Transaction DataFrame
        edit: Edit to apply
        categories: Category lookup dict (id -> {name, group, ...})
        bulk_merchant_renames: Set of (old_merchant, new_merchant) tuples that
            were batch-updated on the backend (e.g., YNAB payee updates).
            If the edit matches one of these, all transactions with that
            merchant will be updated. If None or not matching, only the
            specific transaction is updated.

    Returns:
        Updated DataFrame

    Raises:
        ValueError: If edit.field is unknown
    """
    match edit.field:
        case "merchant":
            is_bulk = (
                bulk_merchant_renames is not None
                and (edit.old_value, edit.new_value) in bulk_merchant_renames
            )
            if is_bulk:
                return apply_bulk_merchant_edit(df, edit.old_value, edit.new_value)
            else:
                return apply_merchant_edit(df, edit.transaction_id, edit.new_value)
        case "category":
            cat = categories.get(edit.new_value, {})
            cat_name = cat.get("name", "Unknown")
            cat_group = cat.get("group", "Unknown")
            return apply_category_edit(df, edit.transaction_id, edit.new_value, cat_name, cat_group)
        case "hide_from_reports":
            return apply_hide_from_reports_edit(df, edit.transaction_id, edit.new_value)
        case _:
            raise ValueError(f"Unknown edit field: {edit.field}")


def apply_edits_to_dataframe(
    df: pl.DataFrame,
    edits: list[TransactionEdit],
    categories: dict[str, dict],
    apply_groups_func: Callable[[pl.DataFrame], pl.DataFrame],
    bulk_merchant_renames: set[tuple[str, str]] | None = None,
) -> pl.DataFrame:
    """
    Apply multiple edits to DataFrame.

    This is a pure function - it doesn't mutate the input DataFrame.

    Args:
        df: Transaction DataFrame
        edits: List of edits to apply
        categories: Category lookup dict
        apply_groups_func: Function to reapply category groups
        bulk_merchant_renames: Set of (old_merchant, new_merchant) tuples that
            were batch-updated on the backend (e.g., YNAB payee updates).
            Merchant edits matching these tuples will update ALL transactions
            with that merchant name. For backends like Monarch Money, pass None
            to update only the specific transaction.

    Returns:
        New DataFrame with all edits applied

    Examples:
        >>> df = pl.DataFrame({
        ...     "id": ["txn1", "txn2"],
        ...     "merchant": ["Amazon", "Starbucks"],
        ...     "category_id": ["cat1", "cat2"],
        ...     "category": ["Shopping", "Dining"],
        ...     "group": ["Retail", "Food"],
        ...     "hideFromReports": [False, False]
        ... })
        >>> edits = [
        ...     TransactionEdit("txn1", "merchant", "Amazon", "Whole Foods", ...),
        ...     TransactionEdit("txn2", "hide_from_reports", False, True, ...)
        ... ]
        >>> def mock_apply_groups(df): return df
        >>> updated = apply_edits_to_dataframe(
        ...     df, edits, {}, mock_apply_groups
        ... )
        >>> updated["merchant"][0]
        'Whole Foods'
        >>> updated["hideFromReports"][1]
        True
    """
    result_df = df
    has_category_edit = False

    for edit in edits:
        if edit.field == "category":
            has_category_edit = True
        result_df = apply_edit_to_dataframe(
            result_df, edit, categories, bulk_merchant_renames=bulk_merchant_renames
        )

    if has_category_edit:
        result_df = apply_groups_func(result_df)

    return result_df
