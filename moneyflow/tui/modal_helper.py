"""
Modal parameter helpers for consistent and testable modal dialogs.

This module provides functions that prepare parameters for modal dialogs,
making the logic testable without requiring the UI to be running.

Each function returns a TypedDict of parameters that can be unpacked
when creating modal screens.
"""

import sys
from typing import Any, Dict, Optional, TypedDict

import polars as pl

if sys.version_info >= (3, 11):
    from typing import NotRequired
else:
    from typing_extensions import NotRequired


# ==================== Edit Merchant ====================


class EditMerchantParams(TypedDict):
    current_merchant: str
    transaction_count: int
    all_merchants: list[str]
    bulk_summary: NotRequired[Dict[str, Any]]
    txn_details: NotRequired[Dict[str, Any]]


def get_edit_merchant_params(
    merchant_name: str,
    transaction_count: int,
    all_merchants: list[str],
    bulk_summary: Optional[Dict[str, Any]] = None,
    txn_details: Optional[Dict[str, Any]] = None,
) -> EditMerchantParams:
    """
    Prepare parameters for Edit Merchant modal.

    Args:
        merchant_name: Current merchant name
        transaction_count: Number of transactions affected
        all_merchants: List of all merchants for autocomplete
        bulk_summary: Optional dict with "total_amount" for bulk edits
        txn_details: Optional dict with "date", "amount", "category" for single edit

    Returns:
        Dictionary ready to unpack into EditMerchantScreen constructor
    """
    params = {
        "current_merchant": merchant_name,
        "transaction_count": transaction_count,
        "all_merchants": all_merchants,
    }

    if bulk_summary is not None:
        params["bulk_summary"] = bulk_summary

    if txn_details is not None:
        params["txn_details"] = txn_details

    return params  # type: ignore


# ==================== Select Category ====================


class SelectCategoryParams(TypedDict):
    categories: dict
    current_category_id: Optional[str]
    txn_details: NotRequired[Dict[str, Any]]


def get_select_category_params(
    categories: dict,
    current_category_id: Optional[str] = None,
    txn_details: Optional[Dict[str, Any]] = None,
) -> SelectCategoryParams:
    """
    Prepare parameters for Category Selection modal.

    Args:
        categories: Dictionary of category_id -> category info
        current_category_id: Currently selected category (if any)
        txn_details: Optional dict with "date", "amount", "merchant" for context

    Returns:
        Dictionary ready to unpack into SelectCategoryScreen constructor
    """
    params = {
        "categories": categories,
        "current_category_id": current_category_id,
    }

    if txn_details is not None:
        params["txn_details"] = txn_details

    return params  # type: ignore


# ==================== Review Changes ====================


class ReviewChangesParams(TypedDict):
    edits: list
    categories: dict


def get_review_changes_params(edits: list, categories: dict) -> ReviewChangesParams:
    """
    Prepare parameters for Review Changes modal.

    Args:
        edits: List of TransactionEdit objects
        categories: Dictionary of category_id -> category info

    Returns:
        Dictionary ready to unpack into ReviewChangesScreen constructor
    """
    return {
        "edits": edits,
        "categories": categories,
    }


# ==================== Delete Confirmation ====================


class DeleteConfirmationParams(TypedDict):
    transaction_count: int


def get_delete_confirmation_params(transaction_count: int = 1) -> DeleteConfirmationParams:
    """
    Prepare parameters for Delete Confirmation modal.

    Args:
        transaction_count: Number of transactions to delete

    Returns:
        Dictionary ready to unpack into DeleteConfirmationScreen constructor
    """
    return {
        "transaction_count": transaction_count,
    }


# ==================== Quit Confirmation ====================


class QuitConfirmationParams(TypedDict):
    has_unsaved_changes: bool


def get_quit_confirmation_params(has_unsaved_changes: bool) -> QuitConfirmationParams:
    """
    Prepare parameters for Quit Confirmation modal.

    Args:
        has_unsaved_changes: Whether there are pending changes

    Returns:
        Dictionary ready to unpack into QuitConfirmationScreen constructor
    """
    return {
        "has_unsaved_changes": has_unsaved_changes,
    }


# ==================== Filter Settings ====================


class FilterParams(TypedDict):
    show_transfers: bool
    show_hidden: bool


def get_filter_params(show_transfers: bool, show_hidden: bool) -> FilterParams:
    """
    Prepare parameters for Filter Settings modal.

    Args:
        show_transfers: Whether to show transfer transactions
        show_hidden: Whether to show hidden transactions

    Returns:
        Dictionary ready to unpack into FilterScreen constructor
    """
    return {
        "show_transfers": show_transfers,
        "show_hidden": show_hidden,
    }


# ==================== Search ====================


class SearchParams(TypedDict):
    current_query: str


def get_search_params(current_query: str = "") -> SearchParams:
    """
    Prepare parameters for Search modal.

    Args:
        current_query: Current search query

    Returns:
        Dictionary ready to unpack into SearchScreen constructor
    """
    return {
        "current_query": current_query,
    }


# ==================== Cache Prompt ====================


class CachePromptParams(TypedDict):
    age: str
    transaction_count: int
    filter_desc: str


def get_cache_prompt_params(
    age: str, transaction_count: int, filter_desc: str
) -> CachePromptParams:
    """
    Prepare parameters for Cache Prompt modal.

    Args:
        age: Human-readable cache age (e.g., "2 hours ago")
        transaction_count: Number of transactions in cache
        filter_desc: Description of filters applied to cache

    Returns:
        Dictionary ready to unpack into CachePromptScreen constructor
    """
    return {
        "age": age,
        "transaction_count": transaction_count,
        "filter_desc": filter_desc,
    }


# ==================== Transaction Details ====================


class TransactionDetailParams(TypedDict):
    transaction: Dict[str, Any]


def get_transaction_detail_params(transaction: Dict[str, Any]) -> TransactionDetailParams:
    """
    Prepare parameters for Transaction Detail modal.

    Args:
        transaction: Dictionary with transaction data

    Returns:
        Dictionary ready to unpack into TransactionDetailScreen constructor
    """
    return {
        "transaction": transaction,
    }


# ==================== Duplicates ====================


class DuplicatesParams(TypedDict):
    duplicates: pl.DataFrame
    groups: list
    all_transactions: pl.DataFrame


def get_duplicates_params(
    duplicates_df: pl.DataFrame, duplicate_groups: list, all_transactions_df: pl.DataFrame
) -> DuplicatesParams:
    """
    Prepare parameters for Duplicates modal.

    Args:
        duplicates_df: DataFrame of duplicate transactions
        duplicate_groups: List of duplicate groups
        all_transactions_df: Full DataFrame for context

    Returns:
        Dictionary ready to unpack into DuplicatesScreen constructor
    """
    return {
        "duplicates": duplicates_df,
        "groups": duplicate_groups,
        "all_transactions": all_transactions_df,
    }
