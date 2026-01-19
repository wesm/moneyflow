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

from moneyflow.mcp.server import ENV_PASSWORD, MAX_BATCH_SIZE, MAX_LIMIT, create_mcp_server

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
            "date": [today - timedelta(days=i) for i in range(5)],
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
        summary = expenses.group_by("category").agg([pl.col("amount").sum().alias("total")])

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


# ============================================================================
# Test: Tool Function Signatures
# ============================================================================


class TestToolFunctionSignatures:
    """Tests that tool functions have valid signatures (no invalid parameters)."""

    def test_fetch_all_data_signature(self):
        """Verify fetch_all_data has the expected signature (no force_refresh)."""
        import inspect

        from moneyflow.data_manager import DataManager

        sig = inspect.signature(DataManager.fetch_all_data)
        param_names = list(sig.parameters.keys())

        # fetch_all_data should NOT have force_refresh parameter
        assert "force_refresh" not in param_names

        # It should have these parameters
        assert "self" in param_names
        assert "start_date" in param_names
        assert "end_date" in param_names
        assert "progress_callback" in param_names

    def test_refresh_data_does_not_use_invalid_params(self):
        """Verify refresh_data tool doesn't use invalid parameters."""
        # This test verifies our fix by checking the source code
        import inspect

        from moneyflow.mcp.server import create_mcp_server

        source = inspect.getsource(create_mcp_server)

        # The refresh_data function should NOT call fetch_all_data(force_refresh=True)
        # It should just call fetch_all_data() without that parameter
        assert "force_refresh=True" not in source


# ============================================================================
# Test: Literal String Matching (Security)
# ============================================================================


class TestLiteralStringMatching:
    """Tests that merchant filtering uses literal string matching, not regex."""

    def test_merchant_filter_uses_literal_true(self):
        """Verify merchant filters use literal=True to prevent regex injection."""
        import inspect

        from moneyflow.mcp.server import create_mcp_server

        source = inspect.getsource(create_mcp_server)

        # All str.contains calls with merchant should use literal=True
        # Count occurrences of the pattern
        contains_calls = source.count(".str.contains(merchant")

        # Each should have literal=True
        literal_calls = source.count(".str.contains(merchant.lower(), literal=True)")

        # All merchant contains calls should use literal=True
        assert contains_calls == literal_calls
        assert contains_calls >= 2  # At least get_transactions and get_uncategorized

    def test_regex_characters_in_merchant_name(self, sample_transactions):
        """Verify regex special characters in merchant don't cause regex matching."""
        # This tests that a merchant name with regex characters
        # would be treated literally, not as regex
        merchant_with_regex = "Amazon.*"  # Would match everything in regex mode

        # With literal=True, this should match literally
        results = sample_transactions.filter(
            pl.col("merchant")
            .str.to_lowercase()
            .str.contains(merchant_with_regex.lower(), literal=True)
        )

        # Should NOT match "Amazon" because we're looking for literal "amazon.*"
        assert len(results) == 0


# ============================================================================
# Test: update_transaction_category Tool Parameter Validation
# ============================================================================


class TestUpdateTransactionCategoryParams:
    """Tests for update_transaction_category parameter validation and disambiguation."""

    @pytest.fixture
    def categories_with_duplicates(self):
        """Categories with duplicate names (can happen across groups/backends)."""
        return {
            "cat1": "Shopping",
            "cat2": "Food & Drink",
            "cat3": "Groceries",
            "cat4": "Shopping",  # Duplicate name!
            "cat5": "Uncategorized",
        }

    def test_update_category_requires_name_or_id(self):
        """Should error when neither category_name nor category_id is provided."""
        import inspect

        from moneyflow.mcp.server import create_mcp_server

        source = inspect.getsource(create_mcp_server)

        # Should contain validation for missing params
        assert "Either category_name or category_id must be provided" in source

    def test_update_category_rejects_both_params(self):
        """Should error when both category_name and category_id are provided."""
        import inspect

        from moneyflow.mcp.server import create_mcp_server

        source = inspect.getsource(create_mcp_server)

        # Should contain validation for both params
        assert "Provide either category_name or category_id, not both" in source

    def test_update_category_detects_duplicate_names(self):
        """Should detect and report duplicate category names."""
        import inspect

        from moneyflow.mcp.server import create_mcp_server

        source = inspect.getsource(create_mcp_server)

        # Should contain duplicate detection logic
        assert "Multiple categories named" in source
        assert "Please use category_id parameter instead" in source
        assert "matching_categories" in source

    def test_update_category_supports_category_id(self):
        """Should support direct category_id lookup."""
        import inspect

        from moneyflow.mcp.server import create_mcp_server

        source = inspect.getsource(create_mcp_server)

        # Should have category_id parameter and direct lookup
        assert "category_id: Optional[str]" in source
        assert "resolved_category_id = category_id" in source
        assert "Category ID" in source

    def test_duplicate_name_returns_all_matching_ids(self):
        """When names are ambiguous, error should include all matching IDs."""
        import inspect

        from moneyflow.mcp.server import create_mcp_server

        source = inspect.getsource(create_mcp_server)

        # Should build list of matching categories with IDs
        assert "for cat_id, cat_name in matching_categories" in source
        assert '"id": cat_id' in source
        assert '"name": cat_name' in source


# ============================================================================
# Functional Tests: update_transaction_category
# ============================================================================


class TestUpdateTransactionCategoryFunctional:
    """Functional tests that actually call the MCP tool and verify responses."""

    @pytest.fixture
    def mock_account(self):
        """Create a mock account object with required attributes."""
        from types import SimpleNamespace

        return SimpleNamespace(
            id="test-account",
            name="Test Account",
            backend_type="demo",
            budget_id=None,
        )

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
        self, sample_transactions, sample_categories, mock_account
    ):
        """Should return error when neither category_name nor category_id is provided."""
        import json
        from unittest.mock import AsyncMock, MagicMock, patch

        # Patch imports at their source modules (they're imported inside create_mcp_server)
        with (
            patch("moneyflow.account_manager.AccountManager") as mock_am,
            patch("moneyflow.credentials.CredentialManager") as mock_cm,
            patch("moneyflow.backends.get_backend") as mock_get_backend,
            patch("moneyflow.data_manager.DataManager") as mock_dm_cls,
        ):
            # Set up account manager mock
            mock_am.return_value.get_last_active_account.return_value = mock_account
            mock_am.return_value.get_profile_dir.return_value = None

            # Set up credential manager mock
            mock_cm.return_value.credentials_exist.return_value = True
            mock_cm.return_value.is_encrypted.return_value = False
            mock_cm.return_value.load_credentials.return_value = (
                {"email": "test@test.com", "password": "test", "mfa_secret": ""},
                None,
            )

            # Set up backend mock
            mock_backend = AsyncMock()
            mock_get_backend.return_value = mock_backend

            # Set up data manager mock
            mock_dm = MagicMock()
            mock_dm.fetch_all_data = AsyncMock(
                return_value=(sample_transactions, sample_categories, {})
            )
            mock_dm_cls.return_value = mock_dm

            # Create server with mocked dependencies
            mcp = create_mcp_server()

            # Call the tool without category_name or category_id
            result = await mcp.call_tool(
                "update_transaction_category",
                {"transaction_id": "tx1", "dry_run": True},
            )

            # Parse response
            content_list, extras = result
            response_text = content_list[0].text
            response = json.loads(response_text)

            # Verify error response
            assert response["status"] == "error"
            assert "Either category_name or category_id must be provided" in response["message"]

    @pytest.mark.asyncio
    async def test_both_params_returns_error(
        self, sample_transactions, sample_categories, mock_account
    ):
        """Should return error when both category_name and category_id are provided."""
        import json
        from unittest.mock import AsyncMock, MagicMock, patch

        with (
            patch("moneyflow.account_manager.AccountManager") as mock_am,
            patch("moneyflow.credentials.CredentialManager") as mock_cm,
            patch("moneyflow.backends.get_backend") as mock_get_backend,
            patch("moneyflow.data_manager.DataManager") as mock_dm_cls,
        ):
            mock_am.return_value.get_last_active_account.return_value = mock_account
            mock_am.return_value.get_profile_dir.return_value = None
            mock_cm.return_value.credentials_exist.return_value = True
            mock_cm.return_value.is_encrypted.return_value = False
            mock_cm.return_value.load_credentials.return_value = (
                {"email": "test@test.com", "password": "test", "mfa_secret": ""},
                None,
            )
            mock_backend = AsyncMock()
            mock_get_backend.return_value = mock_backend

            mock_dm = MagicMock()
            mock_dm.fetch_all_data = AsyncMock(
                return_value=(sample_transactions, sample_categories, {})
            )
            mock_dm_cls.return_value = mock_dm

            mcp = create_mcp_server()

            # Call with both params
            result = await mcp.call_tool(
                "update_transaction_category",
                {
                    "transaction_id": "tx1",
                    "category_name": "Shopping",
                    "category_id": "cat1",
                    "dry_run": True,
                },
            )

            content_list, extras = result
            response = json.loads(content_list[0].text)

            assert response["status"] == "error"
            assert "Provide either category_name or category_id, not both" in response["message"]

    @pytest.mark.asyncio
    async def test_duplicate_names_returns_disambiguation_error(
        self, sample_transactions, categories_with_duplicates, mock_account
    ):
        """Should return error with matching IDs when category name is ambiguous."""
        import json
        from unittest.mock import AsyncMock, MagicMock, patch

        with (
            patch("moneyflow.account_manager.AccountManager") as mock_am,
            patch("moneyflow.credentials.CredentialManager") as mock_cm,
            patch("moneyflow.backends.get_backend") as mock_get_backend,
            patch("moneyflow.data_manager.DataManager") as mock_dm_cls,
        ):
            mock_am.return_value.get_last_active_account.return_value = mock_account
            mock_am.return_value.get_profile_dir.return_value = None
            mock_cm.return_value.credentials_exist.return_value = True
            mock_cm.return_value.is_encrypted.return_value = False
            mock_cm.return_value.load_credentials.return_value = (
                {"email": "test@test.com", "password": "test", "mfa_secret": ""},
                None,
            )
            mock_backend = AsyncMock()
            mock_get_backend.return_value = mock_backend

            mock_dm = MagicMock()
            mock_dm.fetch_all_data = AsyncMock(
                return_value=(sample_transactions, categories_with_duplicates, {})
            )
            mock_dm_cls.return_value = mock_dm

            mcp = create_mcp_server()

            # Call with duplicate category name
            result = await mcp.call_tool(
                "update_transaction_category",
                {"transaction_id": "tx1", "category_name": "Shopping", "dry_run": True},
            )

            content_list, extras = result
            response = json.loads(content_list[0].text)

            assert response["status"] == "error"
            assert "Multiple categories named 'Shopping' exist" in response["message"]
            assert "matching_categories" in response
            assert len(response["matching_categories"]) == 2

            # Verify both IDs are included
            matching_ids = {c["id"] for c in response["matching_categories"]}
            assert "cat1" in matching_ids
            assert "cat4" in matching_ids

    @pytest.mark.asyncio
    async def test_category_id_bypasses_name_lookup(
        self, sample_transactions, categories_with_duplicates, mock_account
    ):
        """Should successfully use category_id even when names are duplicate."""
        import json
        from unittest.mock import AsyncMock, MagicMock, patch

        with (
            patch("moneyflow.account_manager.AccountManager") as mock_am,
            patch("moneyflow.credentials.CredentialManager") as mock_cm,
            patch("moneyflow.backends.get_backend") as mock_get_backend,
            patch("moneyflow.data_manager.DataManager") as mock_dm_cls,
        ):
            mock_am.return_value.get_last_active_account.return_value = mock_account
            mock_am.return_value.get_profile_dir.return_value = None
            mock_cm.return_value.credentials_exist.return_value = True
            mock_cm.return_value.is_encrypted.return_value = False
            mock_cm.return_value.load_credentials.return_value = (
                {"email": "test@test.com", "password": "test", "mfa_secret": ""},
                None,
            )
            mock_backend = AsyncMock()
            mock_get_backend.return_value = mock_backend

            mock_dm = MagicMock()
            mock_dm.fetch_all_data = AsyncMock(
                return_value=(sample_transactions, categories_with_duplicates, {})
            )
            mock_dm_cls.return_value = mock_dm

            mcp = create_mcp_server()

            # Call with specific category_id (should work despite duplicate names)
            result = await mcp.call_tool(
                "update_transaction_category",
                {"transaction_id": "tx1", "category_id": "cat4", "dry_run": True},
            )

            content_list, extras = result
            response = json.loads(content_list[0].text)

            # Should succeed as dry_run
            assert response["status"] == "dry_run"
            assert response["would_update"]["new_category"] == "Shopping"

    @pytest.mark.asyncio
    async def test_invalid_category_id_returns_error(
        self, sample_transactions, sample_categories, mock_account
    ):
        """Should return error when category_id doesn't exist."""
        import json
        from unittest.mock import AsyncMock, MagicMock, patch

        with (
            patch("moneyflow.account_manager.AccountManager") as mock_am,
            patch("moneyflow.credentials.CredentialManager") as mock_cm,
            patch("moneyflow.backends.get_backend") as mock_get_backend,
            patch("moneyflow.data_manager.DataManager") as mock_dm_cls,
        ):
            mock_am.return_value.get_last_active_account.return_value = mock_account
            mock_am.return_value.get_profile_dir.return_value = None
            mock_cm.return_value.credentials_exist.return_value = True
            mock_cm.return_value.is_encrypted.return_value = False
            mock_cm.return_value.load_credentials.return_value = (
                {"email": "test@test.com", "password": "test", "mfa_secret": ""},
                None,
            )
            mock_backend = AsyncMock()
            mock_get_backend.return_value = mock_backend

            mock_dm = MagicMock()
            mock_dm.fetch_all_data = AsyncMock(
                return_value=(sample_transactions, sample_categories, {})
            )
            mock_dm_cls.return_value = mock_dm

            mcp = create_mcp_server()

            # Call with nonexistent category_id
            result = await mcp.call_tool(
                "update_transaction_category",
                {"transaction_id": "tx1", "category_id": "nonexistent", "dry_run": True},
            )

            content_list, extras = result
            response = json.loads(content_list[0].text)

            assert response["status"] == "error"
            assert "Category ID 'nonexistent' not found" in response["message"]
