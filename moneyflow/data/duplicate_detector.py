"""
Duplicate transaction detection.

Identifies potential duplicate transactions based on:
- Same date (or within 1 day)
- Same amount (exact match)
- Same merchant (case-insensitive)
- Same account (optional)
"""

from typing import List

import polars as pl

from ..tui.formatters import ViewPresenter


class DuplicateDetector:
    """Detect potential duplicate transactions."""

    @staticmethod
    def find_duplicates(df: pl.DataFrame, strict_account_match: bool = True) -> pl.DataFrame:
        """
        Find potential duplicate transactions.

        Args:
            df: DataFrame of transactions
            strict_account_match: If True, only match duplicates in same account

        Returns:
            DataFrame with duplicate groups, sorted by date and amount.
            Contains 'ids' column with a list of transaction IDs in each group.
        """
        if df.is_empty():
            return pl.DataFrame()

        # Add normalized merchant for case-insensitive matching
        df_with_norm = df.with_columns(
            pl.col("merchant").str.to_lowercase().alias("merchant_lower")
        )

        # Group by: date, amount, merchant (case-insensitive), and optionally account
        group_cols = ["date", "amount", "merchant_lower"]
        if strict_account_match:
            group_cols.append("account")

        # Find groups with more than one transaction (duplicates)
        agg_cols = [
            pl.col("id").alias("ids"),
            pl.col("merchant").first().alias("merchant"),
        ]

        # Only add account to agg if not already in group_by
        if not strict_account_match:
            agg_cols.append(pl.col("account").first().alias("account"))

        duplicate_groups = (
            df_with_norm.group_by(group_cols).agg(agg_cols).filter(pl.col("ids").list.len() > 1)
        )

        if duplicate_groups.is_empty():
            return pl.DataFrame()

        return duplicate_groups.sort(["date", "amount"], descending=[True, False])

    @staticmethod
    def get_duplicate_groups(df: pl.DataFrame, duplicates: pl.DataFrame) -> List[List[str]]:
        """
        Extract groups of duplicate transaction IDs.

        Args:
            df: Original transactions DataFrame
            duplicates: DataFrame returned by find_duplicates

        Returns:
            List of lists, where each inner list is a group of duplicate IDs
        """
        if duplicates.is_empty():
            return []

        return duplicates["ids"].to_list()

    @staticmethod
    def format_duplicate_report(df: pl.DataFrame, duplicate_groups: List[List[str]]) -> str:
        """
        Format a human-readable duplicate report.

        Args:
            df: Original transactions DataFrame
            duplicate_groups: Groups of duplicate IDs

        Returns:
            Formatted report string
        """
        if not duplicate_groups:
            return "No duplicates found."

        lines = [
            "Duplicate Transaction Report",
            "=" * 60,
            f"Found {len(duplicate_groups)} duplicate group(s)",
            "",
        ]

        all_ids = [txn_id for group in duplicate_groups for txn_id in group]
        if not all_ids:
            return "No duplicates found."

        duplicate_df = df.filter(pl.col("id").is_in(all_ids))
        txn_dict = {row["id"]: row for row in duplicate_df.iter_rows(named=True)}

        for i, group in enumerate(duplicate_groups, 1):
            lines.append(f"Group {i}: {len(group)} transactions")
            lines.append("-" * 40)

            for txn_id in group:
                if txn_id in txn_dict:
                    txn = txn_dict[txn_id]
                    lines.append(
                        f"  ID: {txn_id[:12]}... | "
                        f"Date: {txn['date']} | "
                        f"Amount: {ViewPresenter.format_amount(txn['amount'])} | "
                        f"Merchant: {txn['merchant']} | "
                        f"Account: {txn['account']}"
                    )

            lines.append("")

        return "\n".join(lines)
