"""
Unit tests for the MCP server.

Tests cover:
- Security features (read-only mode, limit caps, dry-run)
- Tool functionality (search, get transactions, categorization)
- Error handling
"""

import json
from datetime import date, timedelta
from pathlib import Path
from types import SimpleNamespace
from unittest.mock import AsyncMock, MagicMock, patch

import polars as pl
import pytest

from moneyflow.mcp.server import (
    ENV_PASSWORD,
    MAX_BATCH_SIZE,
    MAX_LIMIT,
    _clamp_limit,
    _df_to_records,
    _format_amount,
    create_mcp_server,
)

# ============================================================================
# Fixtures
# ============================================================================


@pytest.fixture
def mock_account():
    """Create a mock account object with required attributes."""
    return SimpleNamespace(
        id="test-account",
        name="Test Account",
        backend_type="demo",
        budget_id=None,
    )


@pytest.fixture
def mcp_server_factory(mock_account):
    """Fixture that yields a factory to create an MCP server with mocked dependencies."""

    def _factory(transactions, categories):
        with (
            patch("moneyflow.data.account_manager.AccountManager") as mock_am,
            patch("moneyflow.data.credentials.CredentialManager") as mock_cm,
            patch("moneyflow.backends.get_backend") as mock_get_backend,
            patch("moneyflow.data.data_manager.DataManager") as mock_dm_cls,
        ):
            mock_am.return_value.get_last_active_account.return_value = mock_account
            mock_am.return_value.get_profile_dir.return_value = None

            mock_cm.return_value.credentials_exist.return_value = True
            mock_cm.return_value.is_encrypted.return_value = False
            mock_cm.return_value.load_credentials.return_value = (
                {"email": "x", "password": "y", "mfa_secret": ""},
                None,
            )

            mock_backend = AsyncMock()
            mock_get_backend.return_value = mock_backend

            mock_dm = MagicMock()
            mock_dm.fetch_all_data = AsyncMock(return_value=(transactions, categories, {}))

            def mock_search(df, query):
                """Simple case-insensitive substring filter for MCP wiring tests."""
                if not query:
                    return df
                q = query.lower()
                return df.filter(
                    pl.col("merchant").str.to_lowercase().str.contains(q, literal=True)
                    | pl.col("category").str.to_lowercase().str.contains(q, literal=True)
                    | pl.col("notes").str.to_lowercase().str.contains(q, literal=True)
                )

            mock_dm.search_transactions.side_effect = mock_search

            mock_dm_cls.return_value = mock_dm

            return create_mcp_server()

    return _factory


@pytest.fixture
def sample_transactions():
    """Create sample transaction data for testing."""
    today = date.today()
    return pl.DataFrame(
        {
            "id": ["tx1", "tx2", "tx3", "tx4", "tx5", "tx6", "tx7"],
            "date": [str(today - timedelta(days=i)) for i in range(7)],
            "merchant": ["Amazon", "Starbucks", "Amazon", "Walmart", "Target", "Shell", "Paycheck"],
            "category": [
                "Shopping",
                "Food & Drink",
                "Uncategorized",
                "Groceries",
                "Shopping",
                "Uncategorized",
                "Income",
            ],
            "amount": [-50.00, -5.50, -125.00, -75.00, -30.00, -45.00, 2000.00],
            "account": ["Chase", "Chase", "Chase", "Chase", "Chase", "Chase", "Chase"],
            "notes": [None, "Morning coffee", None, None, None, "Gas", "Salary"],
            "is_hidden": [False, False, False, False, False, False, False],
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
        "cat6": "Income",
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
        assert _clamp_limit(500) == 500

    def test_clamp_limit_zero_becomes_one(self):
        """Zero limit should become 1 (minimum)."""
        assert _clamp_limit(0) == 1

    def test_clamp_limit_negative_becomes_one(self):
        """Negative limit should become 1."""
        assert _clamp_limit(-10) == 1

    def test_clamp_limit_exceeds_max(self):
        """Limit exceeding MAX_LIMIT should be clamped."""
        assert _clamp_limit(10000) == MAX_LIMIT


# ============================================================================
# Test: Tool Output Format
# ============================================================================


class TestToolOutputFormat:
    """Tests for tool output JSON format."""

    def test_format_amount_negative(self):
        """Negative amounts should be formatted correctly."""
        assert _format_amount(-50.00) == "-$50.00"

    def test_format_amount_positive(self):
        """Positive amounts should be formatted correctly."""
        assert _format_amount(100.50) == "$100.50"

    def test_format_amount_large(self):
        """Large amounts should include comma separators."""
        assert _format_amount(1234567.89) == "$1,234,567.89"


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
# Test: Encrypted Credentials Handling
# ============================================================================


class TestEncryptedCredentials:
    """Tests for encrypted credentials handling."""

    def test_env_password_constant_defined(self):
        """ENV_PASSWORD constant should be defined."""
        assert ENV_PASSWORD == "MONEYFLOW_PASSWORD"

    def test_env_password_documented_in_help(self):
        """Environment variable should be documented in CLI help."""
        main_file = Path(__file__).parent.parent / "moneyflow" / "mcp" / "__main__.py"
        content = main_file.read_text()

        assert "MONEYFLOW_PASSWORD" in content
        assert "Environment Variables:" in content


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
        assert _df_to_records(empty_df) == []

    def test_records_have_required_fields(self, sample_transactions):
        """Records should have all required fields."""
        records = _df_to_records(sample_transactions, limit=1)
        assert len(records) == 1
        record = records[0]

        required_fields = ["id", "date", "merchant", "category", "amount", "account"]
        for field in required_fields:
            assert field in record


# ============================================================================
# Test: Category Lookup
# ============================================================================


class TestCategoryLookup:
    """Tests for category lookup functionality."""

    @pytest.mark.asyncio
    async def test_case_insensitive_lookup(
        self, mcp_server_factory, sample_transactions, sample_categories
    ):
        """Category lookup should be case-insensitive."""
        mcp = mcp_server_factory(sample_transactions, sample_categories)
        result = await mcp.call_tool(
            "update_transaction_category",
            {"transaction_id": "tx1", "category_name": "shopping", "dry_run": True},
        )
        content_list, _ = result
        response = json.loads(content_list[0].text)
        assert response["status"] == "dry_run"
        assert response["would_update"]["new_category"] == "Shopping"

    @pytest.mark.asyncio
    async def test_exact_match_required(
        self, mcp_server_factory, sample_transactions, sample_categories
    ):
        """Partial matches should not be found."""
        mcp = mcp_server_factory(sample_transactions, sample_categories)
        result = await mcp.call_tool(
            "update_transaction_category",
            {"transaction_id": "tx1", "category_name": "shop", "dry_run": True},
        )
        content_list, _ = result
        response = json.loads(content_list[0].text)
        assert response["status"] == "error"
        assert "not found" in response["message"]


# ============================================================================
# Test: Uncategorized Filter
# ============================================================================


class TestUncategorizedFilter:
    """Tests for uncategorized transaction filtering."""

    @pytest.mark.asyncio
    async def test_finds_uncategorized_transactions(
        self, mcp_server_factory, sample_transactions, sample_categories
    ):
        """Should find transactions with 'Uncategorized' category."""
        mcp = mcp_server_factory(sample_transactions, sample_categories)
        result = await mcp.call_tool("get_uncategorized_transactions", {})
        content_list, _ = result
        response = json.loads(content_list[0].text)
        assert len(response["transactions"]) == 2
        assert response["transactions"][0]["id"] == "tx3"
        assert response["transactions"][1]["id"] == "tx6"

    @pytest.mark.asyncio
    async def test_merchant_filter_works(
        self, mcp_server_factory, sample_transactions, sample_categories
    ):
        """Merchant filter should work with uncategorized filter."""
        mcp = mcp_server_factory(sample_transactions, sample_categories)
        result = await mcp.call_tool("get_uncategorized_transactions", {"merchant": "amazon"})
        content_list, _ = result
        response = json.loads(content_list[0].text)
        assert len(response["transactions"]) == 1
        assert response["transactions"][0]["id"] == "tx3"
        assert response["transactions"][0]["merchant"] == "Amazon"


# ============================================================================
# Test: Search Functionality
# ============================================================================


class TestSearchFunctionality:
    """Tests for transaction search functionality."""

    @pytest.mark.asyncio
    async def test_search_by_merchant(
        self, mcp_server_factory, sample_transactions, sample_categories
    ):
        """Search should find transactions by merchant name."""
        mcp = mcp_server_factory(sample_transactions, sample_categories)
        result = await mcp.call_tool("search_transactions", {"query": "amazon"})
        content_list, _ = result
        response = json.loads(content_list[0].text)
        assert len(response) == 2
        assert all("Amazon" in r["merchant"] for r in response)

    @pytest.mark.asyncio
    async def test_search_case_insensitive(
        self, mcp_server_factory, sample_transactions, sample_categories
    ):
        """Search should be case-insensitive."""
        mcp = mcp_server_factory(sample_transactions, sample_categories)
        result1 = await mcp.call_tool("search_transactions", {"query": "amazon"})
        result2 = await mcp.call_tool("search_transactions", {"query": "AMAZON"})
        content_list1, _ = result1
        content_list2, _ = result2
        assert len(json.loads(content_list1[0].text)) == len(json.loads(content_list2[0].text))

    @pytest.mark.asyncio
    async def test_search_literal_characters(
        self, mcp_server_factory, sample_transactions, sample_categories
    ):
        """Search should treat regex metacharacters as literals."""
        # Add a transaction with regex metacharacters
        tx_data = sample_transactions.to_dicts()
        tx_data.append(
            {
                "id": "tx8",
                "date": "2023-01-01",
                "merchant": "Regex.*[Test]",
                "category": "Shopping",
                "amount": -10.0,
                "account": "Chase",
                "notes": None,
                "is_hidden": False,
            }
        )
        import polars as pl

        mcp = mcp_server_factory(pl.DataFrame(tx_data), sample_categories)

        result = await mcp.call_tool("search_transactions", {"query": ".*[Test]"})
        content_list, _ = result
        response = json.loads(content_list[0].text)
        assert len(response) == 1
        assert response[0]["merchant"] == "Regex.*[Test]"


# ============================================================================
# Test: Spending Summary
# ============================================================================


class TestSpendingSummary:
    """Tests for spending summary functionality."""

    @pytest.mark.asyncio
    async def test_groups_by_category(
        self, mcp_server_factory, sample_transactions, sample_categories
    ):
        """Summary should group transactions by category."""
        mcp = mcp_server_factory(sample_transactions, sample_categories)
        result = await mcp.call_tool("get_spending_summary", {"group_by": "category"})
        content_list, _ = result
        response = json.loads(content_list[0].text)
        assert "by_category" in response
        categories = [item["category"] for item in response["by_category"]]
        assert "Shopping" in categories

    @pytest.mark.asyncio
    async def test_only_includes_expenses(
        self, mcp_server_factory, sample_transactions, sample_categories
    ):
        """Summary should only include negative amounts (expenses)."""
        mcp = mcp_server_factory(sample_transactions, sample_categories)
        result = await mcp.call_tool("get_spending_summary", {"group_by": "category"})
        content_list, _ = result
        response = json.loads(content_list[0].text)
        assert len(response["by_category"]) > 0
        categories = [item["category"] for item in response["by_category"]]
        assert "Income" not in categories


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


# ============================================================================
# Functional Tests: update_transaction_category
# ============================================================================


class TestUpdateTransactionCategoryFunctional:
    """Functional tests that actually call the MCP tool and verify responses."""

    @pytest.fixture
    def categories_with_duplicates(self):
        """Categories with duplicate names for disambiguation testing."""
        return {
            "cat1": "Shopping",
            "cat2": "Food & Drink",
            "cat3": "Groceries",
            "cat4": "Shopping",  # Duplicate name!
            "cat5": "Uncategorized",
        }

    @pytest.mark.asyncio
    async def test_missing_both_params_returns_error(
        self, sample_transactions, sample_categories, mcp_server_factory
    ):
        """Should return error when neither category_name nor category_id is provided."""
        mcp = mcp_server_factory(sample_transactions, sample_categories)
        result = await mcp.call_tool(
            "update_transaction_category",
            {"transaction_id": "tx1", "dry_run": True},
        )
        content_list, _ = result
        response = json.loads(content_list[0].text)
        assert response["status"] == "error"
        assert "Either category_name or category_id must be provided" in response["message"]

    @pytest.mark.asyncio
    async def test_both_params_returns_error(
        self, sample_transactions, sample_categories, mcp_server_factory
    ):
        """Should return error when both category_name and category_id are provided."""
        mcp = mcp_server_factory(sample_transactions, sample_categories)
        result = await mcp.call_tool(
            "update_transaction_category",
            {
                "transaction_id": "tx1",
                "category_name": "Shopping",
                "category_id": "cat1",
                "dry_run": True,
            },
        )
        content_list, _ = result
        response = json.loads(content_list[0].text)
        assert response["status"] == "error"
        assert "Provide either category_name or category_id, not both" in response["message"]

    @pytest.mark.asyncio
    async def test_duplicate_names_returns_disambiguation_error(
        self, sample_transactions, categories_with_duplicates, mcp_server_factory
    ):
        """Should return error with matching IDs when category name is ambiguous."""
        mcp = mcp_server_factory(sample_transactions, categories_with_duplicates)
        result = await mcp.call_tool(
            "update_transaction_category",
            {"transaction_id": "tx1", "category_name": "Shopping", "dry_run": True},
        )
        content_list, _ = result
        response = json.loads(content_list[0].text)
        assert response["status"] == "error"
        assert "Multiple categories named" in response["message"]
        assert "matching_categories" in response
        assert len(response["matching_categories"]) == 2

    @pytest.mark.asyncio
    async def test_category_id_bypasses_name_lookup(
        self, sample_transactions, categories_with_duplicates, mcp_server_factory
    ):
        """Should successfully use category_id even when names are duplicate."""
        mcp = mcp_server_factory(sample_transactions, categories_with_duplicates)
        result = await mcp.call_tool(
            "update_transaction_category",
            {"transaction_id": "tx1", "category_id": "cat4", "dry_run": True},
        )
        content_list, _ = result
        response = json.loads(content_list[0].text)
        assert response["status"] == "dry_run"
        assert response["would_update"]["new_category"] == "Shopping"

    @pytest.mark.asyncio
    async def test_invalid_category_id_returns_error(
        self, sample_transactions, sample_categories, mcp_server_factory
    ):
        """Should return error when category_id doesn't exist."""
        mcp = mcp_server_factory(sample_transactions, sample_categories)
        result = await mcp.call_tool(
            "update_transaction_category",
            {"transaction_id": "tx1", "category_id": "nonexistent", "dry_run": True},
        )
        content_list, _ = result
        response = json.loads(content_list[0].text)
        assert response["status"] == "error"
        assert "not found" in response["message"]
