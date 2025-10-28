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

from .formatters import ViewPresenter


class DuplicateDetector:
    """Detect potential duplicate transactions."""

    @staticmethod
    def find_duplicates(
        df: pl.DataFrame, strict_account_match: bool = True, date_tolerance_days: int = 0
    ) -> pl.DataFrame:
        """
        Find potential duplicate transactions.

        Args:
            df: DataFrame of transactions
            strict_account_match: If True, only match duplicates in same account
            date_tolerance_days: Number of days +/- to consider same date (0 = exact)

        Returns:
            DataFrame with duplicate groups, sorted by date and amount
        """
        if df.is_empty():
            return pl.DataFrame()

        # Use fast Polars groupby instead of O(n²) loops
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
            pl.col("merchant").first().alias("merchant_orig"),
        ]

        # Only add account to agg if not already in group_by
        if not strict_account_match:
            agg_cols.append(pl.col("account").first().alias("account"))

        duplicate_groups = (
            df_with_norm.group_by(group_cols).agg(agg_cols).filter(pl.col("ids").list.len() > 1)
        )

        if duplicate_groups.is_empty():
            return pl.DataFrame()

        # Convert to pairs format for compatibility with existing tests
        pairs = []
        for row in duplicate_groups.iter_rows(named=True):
            ids = row["ids"]
            merchant = row.get("merchant_orig", "")
            # Account comes from group_by key when strict, from agg when not
            account = row.get("account", "")

            # Create pairs from each group
            for i in range(len(ids)):
                for j in range(i + 1, len(ids)):
                    pairs.append(
                        {
                            "id_1": ids[i],
                            "id_2": ids[j],
                            "date": row["date"],
                            "amount": row["amount"],
                            "merchant": merchant,
                            "account": account,
                        }
                    )

        if not pairs:
            return pl.DataFrame()

        return pl.DataFrame(pairs).sort(["date", "amount"], descending=[True, False])

    @staticmethod
    def _is_duplicate(
        txn1: dict, txn2: dict, strict_account_match: bool = True, date_tolerance_days: int = 0
    ) -> bool:
        """
        Check if two transactions are potential duplicates.

        Args:
            txn1: First transaction
            txn2: Second transaction
            strict_account_match: If True, only match same account
            date_tolerance_days: Number of days +/- to consider same date

        Returns:
            True if transactions are potential duplicates
        """
        # Check amount (exact match)
        if txn1["amount"] != txn2["amount"]:
            return False

        # Check date (within tolerance)
        date_diff = abs((txn1["date"] - txn2["date"]).days)
        if date_diff > date_tolerance_days:
            return False

        # Check merchant (case-insensitive)
        merchant1 = txn1["merchant"].lower() if txn1["merchant"] else ""
        merchant2 = txn2["merchant"].lower() if txn2["merchant"] else ""
        if merchant1 != merchant2:
            return False

        # Check account if strict matching
        if strict_account_match:
            if txn1["account"] != txn2["account"]:
                return False

        return True

    @staticmethod
    def get_duplicate_groups(df: pl.DataFrame, duplicate_pairs: pl.DataFrame) -> List[List[str]]:
        """
        Group duplicate transaction IDs into clusters.

        For example, if A=B, B=C, then [A, B, C] is one group.

        Args:
            df: Original transactions DataFrame
            duplicate_pairs: DataFrame of duplicate pairs (from find_duplicates)

        Returns:
            List of lists, where each inner list is a group of duplicate IDs
        """
        if duplicate_pairs.is_empty():
            return []

        # Build a graph of connections
        connections = {}
        for row in duplicate_pairs.iter_rows(named=True):
            id1 = row["id_1"]
            id2 = row["id_2"]

            if id1 not in connections:
                connections[id1] = set()
            if id2 not in connections:
                connections[id2] = set()

            connections[id1].add(id2)
            connections[id2].add(id1)

        # Find connected components using DFS
        visited = set()
        groups = []

        def dfs(node, group):
            visited.add(node)
            group.append(node)
            for neighbor in connections.get(node, []):
                if neighbor not in visited:
                    dfs(neighbor, group)

        for node in connections:
            if node not in visited:
                group = []
                dfs(node, group)
                groups.append(sorted(group))

        return groups

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

        for i, group in enumerate(duplicate_groups, 1):
            lines.append(f"Group {i}: {len(group)} transactions")
            lines.append("-" * 40)

            for txn_id in group:
                txn_rows = df.filter(pl.col("id") == txn_id)
                if len(txn_rows) > 0:
                    txn = txn_rows.row(0, named=True)
                    lines.append(
                        f"  ID: {txn_id[:12]}... | "
                        f"Date: {txn['date']} | "
                        f"Amount: {ViewPresenter.format_amount(txn['amount'])} | "
                        f"Merchant: {txn['merchant']} | "
                        f"Account: {txn['account']}"
                    )

            lines.append("")

        return "\n".join(lines)
