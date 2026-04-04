from pathlib import Path


def refactor_test():
    test_file = Path(__file__).resolve().parent.parent.parent / "tests" / "test_mcp_server.py"
    with open(test_file, "r") as f:
        lines = f.readlines()

    cutoff_index = 0
    for i, line in enumerate(lines):
        if line.startswith("# Test: Tool Function Signatures"):
            cutoff_index = i - 1  # Remove the "===" line too
            break

    if cutoff_index == 0:
        print("Could not find cutoff!")
        return

    new_content = "".join(lines[:cutoff_index])

    new_block = """
# ============================================================================
# Functional Tests: update_transaction_category
# ============================================================================


class TestUpdateTransactionCategoryFunctional:
    \"\"\"Functional tests that actually call the MCP tool and verify responses.\"\"\"

    @pytest.fixture
    def categories_with_duplicates(self):
        \"\"\"Categories with duplicate names for disambiguation testing.\"\"\"
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
        \"\"\"Should return error when neither category_name nor category_id is provided.\"\"\"
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
        \"\"\"Should return error when both category_name and category_id are provided.\"\"\"
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
        \"\"\"Should return error with matching IDs when category name is ambiguous.\"\"\"
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
        \"\"\"Should successfully use category_id even when names are duplicate.\"\"\"
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
        \"\"\"Should return error when category_id doesn't exist.\"\"\"
        mcp = mcp_server_factory(sample_transactions, sample_categories)
        result = await mcp.call_tool(
            "update_transaction_category",
            {"transaction_id": "tx1", "category_id": "nonexistent", "dry_run": True},
        )
        content_list, _ = result
        response = json.loads(content_list[0].text)
        assert response["status"] == "error"
        assert "not found" in response["message"]
"""
    with open(test_file, "w") as f:
        f.write(new_content + new_block)


if __name__ == "__main__":
    refactor_test()
