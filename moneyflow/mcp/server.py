"""
MCP server implementation for moneyflow.

Exposes personal finance data and operations through the Model Context Protocol,
allowing LLM applications like Claude Desktop to interact with your financial data.

Security Considerations:
- The MCP server runs locally or on a trusted network (e.g., Tailscale)
- Uses the same credential system as the TUI (encrypted or plaintext)
- No built-in authentication - relies on network-level security
- Sensitive financial data is exposed - only run on trusted networks

Usage:
    # Run with stdio transport (for local Claude Desktop)
    python -m moneyflow.mcp

    # Or programmatically
    from moneyflow.mcp import run_mcp_server
    run_mcp_server(account_id="your-account-id")
"""

import json
import logging
from datetime import date, datetime, timedelta
from pathlib import Path
from typing import Any, Dict, List, Optional

import polars as pl

logger = logging.getLogger(__name__)


def create_mcp_server(
    account_id: Optional[str] = None,
    config_dir: Optional[str] = None,
):
    """
    Create and configure the MCP server for moneyflow.

    Args:
        account_id: The account ID to use. If None, uses the last active account.
        config_dir: Custom config directory. Defaults to ~/.moneyflow

    Returns:
        Configured FastMCP server instance
    """
    from mcp.server.fastmcp import FastMCP

    from ..account_manager import AccountManager
    from ..backends import get_backend
    from ..credentials import CredentialManager
    from ..data_manager import DataManager

    mcp = FastMCP("moneyflow")

    # State that will be initialized on first use
    _state: Dict[str, Any] = {
        "data_manager": None,
        "transactions_df": None,
        "categories": None,
        "category_groups": None,
        "initialized": False,
        "account_id": account_id,
        "config_dir": config_dir,
    }

    async def _ensure_initialized():
        """Lazy initialization of the data manager and data."""
        if _state["initialized"]:
            return

        config_path = Path(_state["config_dir"]) if _state["config_dir"] else None
        account_manager = AccountManager(config_dir=config_path)

        # Get account to use
        if _state["account_id"]:
            account = account_manager.get_account(_state["account_id"])
            if not account:
                raise ValueError(f"Account '{_state['account_id']}' not found")
        else:
            account = account_manager.get_last_active_account()
            if not account:
                raise ValueError("No accounts configured. Run 'moneyflow' to set up.")

        profile_dir = account_manager.get_profile_dir(account.id)
        logger.info(f"Using account: {account.name} ({account.backend_type})")

        # Load credentials
        cred_manager = CredentialManager(config_dir=config_path, profile_dir=profile_dir)

        if not cred_manager.credentials_exist():
            raise ValueError(f"No credentials found for account '{account.id}'")

        creds, encryption_key = cred_manager.load_credentials()

        # Create backend
        backend = get_backend(account.backend_type)

        # Login to backend
        if account.backend_type == "monarch":
            await backend.login(
                email=creds["email"],
                password=creds["password"],
                mfa_secret_key=creds.get("mfa_secret"),
            )
        elif account.backend_type == "ynab":
            # YNAB uses the password field to store the access token
            # Cast to YNABBackend to access the budget_id parameter
            from ..backends.ynab import YNABBackend

            if isinstance(backend, YNABBackend):
                await backend.login(password=creds["password"], budget_id=account.budget_id)
            else:
                await backend.login(password=creds["password"])

        # Create data manager (caching handled separately if needed)
        config_dir_str = str(config_path) if config_path else str(Path.home() / ".moneyflow")
        data_manager = DataManager(
            mm=backend,
            config_dir=config_dir_str,
            profile_dir=profile_dir,
            backend_type=account.backend_type,
        )

        # Fetch data (uses cache if available)
        df, categories, category_groups = await data_manager.fetch_all_data()

        _state["data_manager"] = data_manager
        _state["transactions_df"] = df
        _state["categories"] = categories
        _state["category_groups"] = category_groups
        _state["initialized"] = True
        _state["account"] = account

        logger.info(f"Loaded {len(df)} transactions")

    def _format_amount(amount: float) -> str:
        """Format amount as currency string."""
        if amount < 0:
            return f"-${abs(amount):,.2f}"
        return f"${amount:,.2f}"

    def _df_to_records(df: pl.DataFrame, limit: int = 100) -> List[Dict[str, Any]]:
        """Convert DataFrame to list of records with formatting."""
        if len(df) == 0:
            return []

        # Limit results
        if len(df) > limit:
            df = df.head(limit)

        records = []
        for row in df.iter_rows(named=True):
            record = {
                "id": row.get("id", ""),
                "date": str(row.get("date", "")),
                "merchant": row.get("merchant", ""),
                "category": row.get("category", ""),
                "amount": row.get("amount", 0),
                "amount_formatted": _format_amount(row.get("amount", 0)),
                "account": row.get("account", ""),
            }
            if row.get("notes"):
                record["notes"] = row["notes"]
            if row.get("is_hidden"):
                record["is_hidden"] = True
            records.append(record)

        return records

    # ========== TOOLS ==========

    @mcp.tool()
    async def search_transactions(
        query: str,
        limit: int = 50,
    ) -> str:
        """
        Search transactions by merchant name, category, or notes.

        Args:
            query: Search query (searches merchant, category, and notes)
            limit: Maximum number of results to return (default 50)

        Returns:
            JSON array of matching transactions
        """
        await _ensure_initialized()

        dm = _state["data_manager"]
        df = _state["transactions_df"]

        results = dm.search_transactions(df, query)
        records = _df_to_records(results, limit=limit)

        return json.dumps(records, indent=2)

    @mcp.tool()
    async def get_transactions(
        start_date: Optional[str] = None,
        end_date: Optional[str] = None,
        category: Optional[str] = None,
        merchant: Optional[str] = None,
        min_amount: Optional[float] = None,
        max_amount: Optional[float] = None,
        limit: int = 100,
    ) -> str:
        """
        Get transactions with optional filters.

        Args:
            start_date: Filter transactions on or after this date (YYYY-MM-DD)
            end_date: Filter transactions on or before this date (YYYY-MM-DD)
            category: Filter by category name (exact match)
            merchant: Filter by merchant name (contains, case-insensitive)
            min_amount: Minimum amount (use negative for expenses)
            max_amount: Maximum amount (use negative for expenses)
            limit: Maximum number of results (default 100)

        Returns:
            JSON array of transactions
        """
        await _ensure_initialized()

        df = _state["transactions_df"]

        # Apply filters
        if start_date:
            df = df.filter(pl.col("date") >= start_date)
        if end_date:
            df = df.filter(pl.col("date") <= end_date)
        if category:
            df = df.filter(pl.col("category") == category)
        if merchant:
            df = df.filter(pl.col("merchant").str.to_lowercase().str.contains(merchant.lower()))
        if min_amount is not None:
            df = df.filter(pl.col("amount") >= min_amount)
        if max_amount is not None:
            df = df.filter(pl.col("amount") <= max_amount)

        records = _df_to_records(df, limit=limit)
        return json.dumps(records, indent=2)

    @mcp.tool()
    async def get_spending_summary(
        start_date: Optional[str] = None,
        end_date: Optional[str] = None,
        group_by: str = "category",
    ) -> str:
        """
        Get spending summary grouped by category or merchant.

        Args:
            start_date: Start date for the summary (YYYY-MM-DD). Defaults to 30 days ago.
            end_date: End date for the summary (YYYY-MM-DD). Defaults to today.
            group_by: Group spending by "category" or "merchant" (default: category)

        Returns:
            JSON object with spending summary
        """
        await _ensure_initialized()

        df = _state["transactions_df"]

        # Default date range: last 30 days
        if not end_date:
            end_date = date.today().isoformat()
        if not start_date:
            start_date = (date.today() - timedelta(days=30)).isoformat()

        # Filter by date
        df = df.filter((pl.col("date") >= start_date) & (pl.col("date") <= end_date))

        # Filter to expenses only (negative amounts)
        expenses = df.filter(pl.col("amount") < 0)

        # Group and sum
        group_col = "category" if group_by == "category" else "merchant"
        summary = (
            expenses.group_by(group_col)
            .agg([pl.col("amount").sum().alias("total"), pl.col("id").count().alias("count")])
            .sort("total")  # Sort by total (most negative first = highest spending)
        )

        # Format results
        results = {
            "period": {"start": start_date, "end": end_date},
            "total_spending": _format_amount(expenses["amount"].sum()),
            "transaction_count": len(expenses),
            "by_" + group_by: [],
        }

        for row in summary.iter_rows(named=True):
            results["by_" + group_by].append(
                {
                    group_by: row[group_col],
                    "total": _format_amount(row["total"]),
                    "count": row["count"],
                }
            )

        return json.dumps(results, indent=2)

    @mcp.tool()
    async def get_categories() -> str:
        """
        Get all available categories and their groups.

        Returns:
            JSON object with categories organized by group
        """
        await _ensure_initialized()

        categories = _state["categories"]
        category_groups = _state["category_groups"]

        # Organize categories by group
        by_group: Dict[str, List[str]] = {}
        for cat_id, cat_name in categories.items():
            group_name = category_groups.get(cat_id, "Other")
            if group_name not in by_group:
                by_group[group_name] = []
            by_group[group_name].append(cat_name)

        # Sort categories within groups
        for group in by_group:
            by_group[group].sort()

        return json.dumps(by_group, indent=2)

    @mcp.tool()
    async def get_merchants(limit: int = 100) -> str:
        """
        Get all merchants with transaction counts.

        Args:
            limit: Maximum number of merchants to return (default 100)

        Returns:
            JSON array of merchants with transaction counts, sorted by frequency
        """
        await _ensure_initialized()

        df = _state["transactions_df"]

        merchant_counts = (
            df.group_by("merchant")
            .agg(
                [
                    pl.col("id").count().alias("transaction_count"),
                    pl.col("amount").sum().alias("total_amount"),
                ]
            )
            .sort("transaction_count", descending=True)
            .head(limit)
        )

        results = []
        for row in merchant_counts.iter_rows(named=True):
            results.append(
                {
                    "merchant": row["merchant"],
                    "transaction_count": row["transaction_count"],
                    "total_amount": _format_amount(row["total_amount"]),
                }
            )

        return json.dumps(results, indent=2)

    @mcp.tool()
    async def get_account_info() -> str:
        """
        Get information about the connected account.

        Returns:
            JSON object with account information
        """
        await _ensure_initialized()

        account = _state["account"]
        df = _state["transactions_df"]

        # Calculate date range
        if len(df) > 0:
            date_range = {
                "earliest": str(df["date"].min()),
                "latest": str(df["date"].max()),
            }
        else:
            date_range = {"earliest": None, "latest": None}

        return json.dumps(
            {
                "account_id": account.id,
                "name": account.name,
                "backend_type": account.backend_type,
                "transaction_count": len(df),
                "date_range": date_range,
                "category_count": len(_state["categories"]),
            },
            indent=2,
        )

    @mcp.tool()
    async def refresh_data() -> str:
        """
        Refresh transaction data from the backend API.

        This bypasses the cache and fetches fresh data. Use sparingly
        as it may be rate-limited by the backend.

        Returns:
            JSON object with refresh status
        """
        await _ensure_initialized()

        dm = _state["data_manager"]

        # Force refresh from API
        df, categories, category_groups = await dm.fetch_all_data(force_refresh=True)

        _state["transactions_df"] = df
        _state["categories"] = categories
        _state["category_groups"] = category_groups

        return json.dumps(
            {
                "status": "success",
                "transaction_count": len(df),
                "refreshed_at": datetime.now().isoformat(),
            },
            indent=2,
        )

    # ========== RESOURCES ==========

    @mcp.resource("moneyflow://account")
    async def account_resource() -> str:
        """Account information for the connected financial account."""
        return await get_account_info()

    @mcp.resource("moneyflow://categories")
    async def categories_resource() -> str:
        """All available spending categories organized by group."""
        return await get_categories()

    @mcp.resource("moneyflow://merchants/top")
    async def top_merchants_resource() -> str:
        """Top 50 merchants by transaction frequency."""
        return await get_merchants(limit=50)

    @mcp.resource("moneyflow://spending/monthly")
    async def monthly_spending_resource() -> str:
        """Spending summary for the current month by category."""
        today = date.today()
        start_of_month = today.replace(day=1).isoformat()
        return await get_spending_summary(start_date=start_of_month, end_date=today.isoformat())

    @mcp.resource("moneyflow://transactions/recent")
    async def recent_transactions_resource() -> str:
        """Most recent 50 transactions."""
        return await get_transactions(limit=50)

    return mcp


def run_mcp_server(
    account_id: Optional[str] = None,
    config_dir: Optional[str] = None,
    transport: str = "stdio",
):
    """
    Run the MCP server.

    Args:
        account_id: Account ID to use (defaults to last active)
        config_dir: Custom config directory
        transport: Transport type - "stdio" or "streamable-http"
    """
    mcp = create_mcp_server(account_id=account_id, config_dir=config_dir)
    mcp.run(transport=transport)


if __name__ == "__main__":
    run_mcp_server()
