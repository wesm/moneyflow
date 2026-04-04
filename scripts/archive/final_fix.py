from pathlib import Path


def final_fix():
    test_file = Path(__file__).resolve().parent.parent.parent / "tests" / "test_mcp_server.py"
    with open(test_file, "r") as f:
        content = f.read()

    # Revert TestSearchFunctionality assertions to use list
    content = content.replace(
        'assert len(response["transactions"]) == 2\n        assert all("Amazon" in r["merchant"] for r in response["transactions"])',
        'assert len(response) == 2\n        assert all("Amazon" in r["merchant"] for r in response)',
    )
    content = content.replace(
        'assert len(json.loads(content_list1[0].text)["transactions"]) == len(json.loads(content_list2[0].text)["transactions"])',
        "assert len(json.loads(content_list1[0].text)) == len(json.loads(content_list2[0].text))",
    )

    # Fix TestSpendingSummary assertions
    content = content.replace(
        'assert "summary" in response\n        categories = [item["category"] for item in response["summary"]]',
        'assert "by_category" in response\n        categories = [item["category"] for item in response["by_category"]]',
    )
    content = content.replace(
        'assert len(response["summary"]) > 0', 'assert len(response["by_category"]) > 0'
    )

    with open(test_file, "w") as f:
        f.write(content)


if __name__ == "__main__":
    final_fix()
