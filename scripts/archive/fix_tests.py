from pathlib import Path


def fix_tests():
    test_file = Path(__file__).resolve().parent.parent.parent / "tests" / "test_mcp_server.py"
    with open(test_file, "r") as f:
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

    with open(test_file, "w") as f:
        f.write(content)


if __name__ == "__main__":
    fix_tests()
