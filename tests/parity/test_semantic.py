"""Tests for Python semantic TUI characterization."""

from pathlib import Path

import pytest

from moneyflow.parity.backend import FixtureBackend
from moneyflow.parity.semantic import generate_frames, load_scenarios

TRANSACTIONS = Path("testdata/parity/transactions.json")
SCENARIOS = Path("testdata/parity/frame_scenarios.json")


@pytest.mark.asyncio
async def test_fixture_backend_is_read_only_and_paginated() -> None:
    backend = FixtureBackend(TRANSACTIONS)
    first = await backend.get_transactions(limit=2)
    second = await backend.get_transactions(limit=2, offset=2)

    assert first["allTransactions"]["totalCount"] == len(backend.transactions)
    assert len(first["allTransactions"]["results"]) == 2
    assert first["allTransactions"]["results"] != second["allTransactions"]["results"]
    assert await backend.get_all_merchants()
    with pytest.raises(RuntimeError, match="read-only"):
        await backend.delete_transaction("transaction-example")


def test_frame_scenarios_cover_required_states() -> None:
    document = load_scenarios(SCENARIOS)
    names = {scenario["name"] for scenario in document["scenarios"]}
    assert {
        "merchant",
        "category",
        "group",
        "account",
        "time_year",
        "time_month",
        "time_day",
        "detail",
        "subgroup",
        "multi_level",
        "selected_rows",
        "help",
        "merchant_150x40",
        "detail_150x40",
        "drilldown_150x30",
        "search_150x30",
        "filters_150x30",
    } == names


@pytest.mark.asyncio
async def test_python_semantic_extractor_uses_isolated_fixture(tmp_path: Path) -> None:
    frames = await generate_frames(SCENARIOS, tmp_path)
    merchant = frames["merchant"]
    assert merchant["width"] == 150
    assert merchant["height"] == 50
    assert merchant["visible_row_ids"][0] == "merchant-rent"
    assert {region["name"] for region in merchant["regions"]} == {
        "breadcrumb",
        "stats",
        "table_header",
        "table_body",
        "hints",
    }
    assert frames["search_150x30"]["regions"][-1]["name"] == "overlay"
    assert frames["search_150x30"]["regions"][-1]["lines"] == [
        "                   🔍 Search Transactions"
    ]
    time_body = next(
        region for region in frames["time_day"]["regions"] if region["name"] == "table_body"
    )
    assert all(
        not line.endswith(("▁", "▂", "▃", "▄", "▅", "▆", "▇", "█")) for line in time_body["lines"]
    )
