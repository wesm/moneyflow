def fix_tests():
    with open("tests/test_mcp_server.py", "r") as f:
        content = f.read()

    # 1. Fix sample_transactions to use string dates
    content = content.replace(
        '"date": [today - timedelta(days=i) for i in range(5)]',
        '"date": [str(today - timedelta(days=i)) for i in range(5)]',
    )

    # 2. Fix mock_dm in mcp_server_factory
    old_mock_dm = """            mock_dm = MagicMock()
            mock_dm.fetch_all_data = AsyncMock(return_value=(transactions, categories, {}))
            mock_dm_cls.return_value = mock_dm"""

    new_mock_dm = """            mock_dm = MagicMock()
            mock_dm.fetch_all_data = AsyncMock(return_value=(transactions, categories, {}))

            def mock_search(df, query):
                query_lower = query.lower()
                return df.filter(
                    pl.col("merchant").str.to_lowercase().str.contains(query_lower) |
                    pl.col("category").str.to_lowercase().str.contains(query_lower)
                )
            mock_dm.search_transactions.side_effect = mock_search

            mock_dm_cls.return_value = mock_dm"""
    content = content.replace(old_mock_dm, new_mock_dm)

    # 3. Fix TestUncategorizedFilter assertions
    content = content.replace(
        'assert len(response) == 1\n        assert response[0]["id"] == "tx3"',
        'assert len(response["transactions"]) == 1\n        assert response["transactions"][0]["id"] == "tx3"',
    )
    content = content.replace(
        "assert len(response) == 1\n\n", 'assert len(response["transactions"]) == 1\n\n'
    )

    # 4. Fix TestSearchFunctionality assertions
    content = content.replace(
        'assert len(response) == 2\n        assert all("Amazon" in r["merchant"] for r in response)',
        'assert len(response["transactions"]) == 2\n        assert all("Amazon" in r["merchant"] for r in response["transactions"])',
    )
    content = content.replace(
        "assert len(json.loads(content_list1[0].text)) == len(json.loads(content_list2[0].text))",
        'assert len(json.loads(content_list1[0].text)["transactions"]) == len(json.loads(content_list2[0].text)["transactions"])',
    )

    with open("tests/test_mcp_server.py", "w") as f:
        f.write(content)


if __name__ == "__main__":
    fix_tests()
