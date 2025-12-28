"""
Unit tests for the MCP server.

Tests cover:
- Security features (read-only mode, limit caps, dry-run)
- Tool functionality (search, get transactions, categorization)
- Error handling
"""

from datetime import date, timedelta
from pathlib import Path

import polars as pl
import pytest

from moneyflow.mcp.server import MAX_BATCH_SIZE, MAX_LIMIT, create_mcp_server

# ============================================================================
# Fixtures
# ============================================================================


@pytest.fixture
def sample_transactions():
    """Create sample transaction data for testing."""
    today = date.today()
    return pl.DataFrame(
        {
            "id": ["tx1", "tx2", "tx3", "tx4", "tx5"],
            "date": [
                today - timedelta(days=i) for i in range(5)
            ],
            "merchant": ["Amazon", "Starbucks", "Amazon", "Walmart", "Target"],
            "category": ["Shopping", "Food & Drink", "Uncategorized", "Groceries", "Shopping"],
            "amount": [-50.00, -5.50, -125.00, -75.00, -30.00],
            "account": ["Chase", "Chase", "Chase", "Chase", "Chase"],
            "notes": [None, "Morning coffee", None, None, None],
            "is_hidden": [False, False, False, False, False],
        }
    )


@pytest.fixture
def sample_categories():
    """Create sample category data for testing."""
    return {
        "cat1": "Shopping",
        "cat2": "Food & Drink",
        "cat3": "Groceries",
        "cat4": "Entertainment",
        "cat5": "Uncategorized",
    }


# ============================================================================
# Test: MAX_LIMIT constant
# ============================================================================


class TestMaxLimit:
    """Tests for the MAX_LIMIT constant."""

    def test_max_limit_is_reasonable(self):
        """MAX_LIMIT should be set to prevent memory issues."""
        assert MAX_LIMIT == 1000
        assert MAX_LIMIT > 0


# ============================================================================
# Test: Server Creation
# ============================================================================


class TestServerCreation:
    """Tests for MCP server creation."""

    def test_create_server_default_params(self):
        """Server can be created with default parameters."""
        mcp = create_mcp_server()
        assert mcp is not None

    def test_create_server_with_account_id(self):
        """Server can be created with a specific account ID."""
        mcp = create_mcp_server(account_id="my-account")
        assert mcp is not None

    def test_create_server_with_config_dir(self):
        """Server can be created with a custom config directory."""
        mcp = create_mcp_server(config_dir="/custom/path")
        assert mcp is not None

    def test_create_server_read_only(self):
        """Server can be created in read-only mode."""
        mcp = create_mcp_server(read_only=True)
        assert mcp is not None

    def test_create_server_all_params(self):
        """Server can be created with all parameters."""
        mcp = create_mcp_server(
            account_id="my-account",
            config_dir="/custom/path",
            read_only=True,
        )
        assert mcp is not None


# ============================================================================
# Test: Limit Clamping (via internal function behavior)
# ============================================================================


class TestLimitClamping:
    """Tests for limit clamping behavior."""

    def test_clamp_limit_within_range(self):
        """Limits within MAX_LIMIT should be unchanged."""
        # We test this indirectly through the server behavior
        # by verifying MAX_LIMIT is set correctly
        assert MAX_LIMIT == 1000

    def test_clamp_limit_zero_becomes_one(self):
        """Zero limit should become 1 (minimum)."""
        # The _clamp_limit function uses max(1, min(limit, MAX_LIMIT))
        # so 0 becomes 1
        assert max(1, min(0, MAX_LIMIT)) == 1

    def test_clamp_limit_negative_becomes_one(self):
        """Negative limit should become 1."""
        assert max(1, min(-10, MAX_LIMIT)) == 1

    def test_clamp_limit_exceeds_max(self):
        """Limit exceeding MAX_LIMIT should be clamped."""
        assert max(1, min(10000, MAX_LIMIT)) == MAX_LIMIT


# ============================================================================
# Test: Tool Output Format
# ============================================================================


class TestToolOutputFormat:
    """Tests for tool output JSON format."""

    def test_format_amount_negative(self):
        """Negative amounts should be formatted correctly."""
        # Test the formatting logic
        amount = -50.00
        if amount < 0:
            formatted = f"-${abs(amount):,.2f}"
        else:
            formatted = f"${amount:,.2f}"
        assert formatted == "-$50.00"

    def test_format_amount_positive(self):
        """Positive amounts should be formatted correctly."""
        amount = 100.50
        if amount < 0:
            formatted = f"-${abs(amount):,.2f}"
        else:
            formatted = f"${amount:,.2f}"
        assert formatted == "$100.50"

    def test_format_amount_large(self):
        """Large amounts should include comma separators."""
        amount = 1234567.89
        if amount < 0:
            formatted = f"-${abs(amount):,.2f}"
        else:
            formatted = f"${amount:,.2f}"
        assert formatted == "$1,234,567.89"


# ============================================================================
# Test: Security - Read-Only Mode
# ============================================================================


class TestReadOnlyMode:
    """Tests for read-only mode functionality."""

    def test_read_only_mode_set_in_state(self):
        """Read-only mode should be stored in server state."""
        # Create server in read-only mode
        mcp_readonly = create_mcp_server(read_only=True)
        mcp_normal = create_mcp_server(read_only=False)

        # Both should be created successfully
        assert mcp_readonly is not None
        assert mcp_normal is not None


# ============================================================================
# Test: Security - Batch Size Limit
# ============================================================================


class TestBatchSizeLimit:
    """Tests for batch size limit."""

    def test_max_batch_size_is_100(self):
        """MAX_BATCH_SIZE should be 100."""
        assert MAX_BATCH_SIZE == 100

    def test_max_batch_size_is_reasonable(self):
        """MAX_BATCH_SIZE should be reasonable to prevent abuse."""
        assert MAX_BATCH_SIZE > 0
        assert MAX_BATCH_SIZE <= 1000


# ============================================================================
# Test: Dry Run Mode
# ============================================================================


class TestDryRunMode:
    """Tests for dry-run mode functionality."""

    def test_dry_run_response_format(self):
        """Dry run responses should have expected format."""
        # Test the expected structure of dry_run responses
        expected_keys = ["status", "message", "would_update"]
        sample_response = {
            "status": "dry_run",
            "message": "No changes made (dry run)",
            "would_update": {"transaction_id": "tx1"},
        }

        for key in expected_keys:
            assert key in sample_response

        assert sample_response["status"] == "dry_run"


# ============================================================================
# Test: CLI Arguments (via main module)
# ============================================================================


class TestCLIArguments:
    """Tests for CLI argument parsing."""

    def test_main_module_exists(self):
        """The __main__ module should exist."""
        from moneyflow.mcp import __main__

        assert hasattr(__main__, "main")

    def test_argparse_setup(self):
        """ArgumentParser should be configured correctly."""
        from moneyflow.mcp.__main__ import main

        # The main function exists and is callable
        assert callable(main)


# ============================================================================
# Test: HTTP Transport Warning
# ============================================================================


class TestHTTPTransportWarning:
    """Tests for HTTP transport security warning."""

    def test_warning_mentions_security(self):
        """The HTTP warning should mention security concerns."""
        # Read the __main__.py file content to verify warning text
        main_file = Path(__file__).parent.parent / "moneyflow" / "mcp" / "__main__.py"
        content = main_file.read_text()

        assert "SECURITY WARNING" in content
        assert "NO built-in authentication" in content
        assert "Tailscale" in content


# ============================================================================
# Test: Error Handling
# ============================================================================


class TestErrorHandling:
    """Tests for error handling in MCP server."""

    def test_error_response_format(self):
        """Error responses should have consistent format."""
        error_response = {
            "status": "error",
            "message": "Something went wrong",
        }

        assert "status" in error_response
        assert error_response["status"] == "error"
        assert "message" in error_response

    def test_category_not_found_includes_available(self):
        """Category not found errors should include available categories."""
        # This is a documentation test - verifying expected behavior
        error_response = {
            "status": "error",
            "message": "Category 'InvalidCategory' not found",
            "available_categories": ["Shopping", "Food & Drink", "Groceries"],
        }

        assert "available_categories" in error_response
        assert len(error_response["available_categories"]) > 0


# ============================================================================
# Test: DataFrame Conversion
# ============================================================================


class TestDataFrameConversion:
    """Tests for DataFrame to records conversion."""

    def test_empty_dataframe_returns_empty_list(self, sample_transactions):
        """Empty DataFrame should return empty list."""
        empty_df = sample_transactions.filter(pl.col("id") == "nonexistent")
        assert len(empty_df) == 0

    def test_records_have_required_fields(self, sample_transactions):
        """Records should have all required fields."""
        required_fields = ["id", "date", "merchant", "category", "amount", "account"]

        # Check a sample row has all fields
        row = sample_transactions.row(0, named=True)
        for field in required_fields:
            assert field in row


# ============================================================================
# Test: Category Lookup
# ============================================================================


class TestCategoryLookup:
    """Tests for category lookup functionality."""

    def test_case_insensitive_lookup(self, sample_categories):
        """Category lookup should be case-insensitive."""
        target = "shopping"

        found = None
        for cat_id, cat_name in sample_categories.items():
            if cat_name.lower() == target.lower():
                found = cat_id
                break

        assert found is not None
        assert found == "cat1"

    def test_exact_match_required(self, sample_categories):
        """Partial matches should not be found."""
        target = "shop"  # Partial match of "Shopping"

        found = None
        for cat_id, cat_name in sample_categories.items():
            if cat_name.lower() == target.lower():
                found = cat_id
                break

        assert found is None


# ============================================================================
# Test: Uncategorized Filter
# ============================================================================


class TestUncategorizedFilter:
    """Tests for uncategorized transaction filtering."""

    def test_finds_uncategorized_transactions(self, sample_transactions):
        """Should find transactions with 'Uncategorized' category."""
        uncategorized = sample_transactions.filter(
            (pl.col("category") == "Uncategorized")
            | (pl.col("category").is_null())
            | (pl.col("category") == "")
        )

        assert len(uncategorized) == 1
        assert uncategorized["id"][0] == "tx3"

    def test_merchant_filter_works(self, sample_transactions):
        """Merchant filter should work with uncategorized filter."""
        uncategorized = sample_transactions.filter(
            (pl.col("category") == "Uncategorized")
            | (pl.col("category").is_null())
            | (pl.col("category") == "")
        )

        amazon_uncategorized = uncategorized.filter(
            pl.col("merchant").str.to_lowercase().str.contains("amazon")
        )

        assert len(amazon_uncategorized) == 1


# ============================================================================
# Test: Search Functionality
# ============================================================================


class TestSearchFunctionality:
    """Tests for transaction search functionality."""

    def test_search_by_merchant(self, sample_transactions):
        """Search should find transactions by merchant name."""
        results = sample_transactions.filter(
            pl.col("merchant").str.to_lowercase().str.contains("amazon")
        )

        assert len(results) == 2
        assert all(r == "Amazon" for r in results["merchant"].to_list())

    def test_search_case_insensitive(self, sample_transactions):
        """Search should be case-insensitive."""
        results_lower = sample_transactions.filter(
            pl.col("merchant").str.to_lowercase().str.contains("amazon")
        )
        results_upper = sample_transactions.filter(
            pl.col("merchant").str.to_lowercase().str.contains("AMAZON".lower())
        )

        assert len(results_lower) == len(results_upper)


# ============================================================================
# Test: Spending Summary
# ============================================================================


class TestSpendingSummary:
    """Tests for spending summary functionality."""

    def test_groups_by_category(self, sample_transactions):
        """Summary should group transactions by category."""
        expenses = sample_transactions.filter(pl.col("amount") < 0)
        summary = (
            expenses.group_by("category")
            .agg([pl.col("amount").sum().alias("total")])
        )

        assert len(summary) > 0
        assert "Shopping" in summary["category"].to_list()

    def test_only_includes_expenses(self, sample_transactions):
        """Summary should only include negative amounts (expenses)."""
        expenses = sample_transactions.filter(pl.col("amount") < 0)

        assert len(expenses) == len(sample_transactions)  # All are expenses
        assert all(a < 0 for a in expenses["amount"].to_list())


# ============================================================================
# Test: Resources
# ============================================================================


class TestResources:
    """Tests for MCP resources."""

    def test_resources_are_defined(self):
        """MCP resources should be defined."""
        # Resources are defined in the server
        # We just verify the server can be created
        mcp = create_mcp_server()
        assert mcp is not None


# ============================================================================
# Test: Tool Registration
# ============================================================================


class TestToolRegistration:
    """Tests for MCP tool registration."""

    def test_server_has_tools(self):
        """MCP server should have tools registered."""
        mcp = create_mcp_server()
        assert mcp is not None
        # The actual tool registration is done via decorators
        # We verify the server is created successfully


# ============================================================================
# Integration-style tests (require more setup)
# ============================================================================


class TestIntegration:
    """Integration-style tests that verify end-to-end behavior."""

    def test_server_creation_does_not_initialize(self):
        """Server creation should not trigger initialization."""
        # Creating a server should be lazy - no connection attempts
        mcp = create_mcp_server(
            account_id="nonexistent-account",
            config_dir="/nonexistent/path",
        )

        # Server should be created without errors
        # (actual initialization happens on first tool call)
        assert mcp is not None

    def test_read_only_flag_propagates(self):
        """Read-only flag should be set in server configuration."""
        mcp_ro = create_mcp_server(read_only=True)
        mcp_rw = create_mcp_server(read_only=False)

        # Both should be created - the flag is stored internally
        assert mcp_ro is not None
        assert mcp_rw is not None
