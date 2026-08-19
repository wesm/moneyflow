"""Tests for Python semantic TUI characterization."""

from pathlib import Path

import polars as pl
import pytest

from moneyflow.data.duplicate_detector import DuplicateDetector
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
        "time_day_scrolled_150x30",
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
        "duplicates",
        "duplicates_selected",
        "duplicates_info",
        "duplicates_hidden",
        "duplicates_delete_confirmation",
        "duplicates_delete_cancel",
        "duplicates_closed",
    } == names


def test_python_duplicate_characterization_is_exact_date_unicode_lower_and_same_account() -> None:
    rows = [
        {
            "id": "accent-upper",
            "date": "2026-01-15",
            "amount": -1234,
            "merchant": "CAFÉ",
            "account": "Example Account",
        },
        {
            "id": "accent-lower",
            "date": "2026-01-15",
            "amount": -1234,
            "merchant": "café",
            "account": "Example Account",
        },
        {
            "id": "different-date",
            "date": "2026-01-16",
            "amount": -1234,
            "merchant": "café",
            "account": "Example Account",
        },
        {
            "id": "different-account",
            "date": "2026-01-15",
            "amount": -1234,
            "merchant": "café",
            "account": "Other Account",
        },
        {
            "id": "casefold-sharp-s",
            "date": "2026-01-17",
            "amount": -1234,
            "merchant": "Straße",
            "account": "Example Account",
        },
        {
            "id": "casefold-ss",
            "date": "2026-01-17",
            "amount": -1234,
            "merchant": "STRASSE",
            "account": "Example Account",
        },
    ]

    duplicates = DuplicateDetector.find_duplicates(pl.DataFrame(rows))

    assert duplicates["ids"].to_list() == [["accent-upper", "accent-lower"]]


@pytest.mark.asyncio
async def test_python_semantic_extractor_uses_isolated_fixture(tmp_path: Path) -> None:
    frames = await generate_frames(SCENARIOS, tmp_path)
    merchant = frames["merchant"]
    assert merchant["width"] == 150
    assert merchant["height"] == 50
    assert merchant["visible_row_ids"][0] == "merchant:13:merchant-rent:2:USD"
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
    assert (
        frames["time_day_scrolled_150x30"]["visible_row_ids"][0]
        != frames["time_day"]["visible_row_ids"][0]
    )
    assert frames["selected_rows"]["selection_ids"] == ["merchant:15:merchant-grocer:2:USD"]
    assert frames["duplicates"]["overlay"][:2] == [
        "🔍 Found 1 potential duplicates in 1 groups",
        "Space=Select | i=Details | x=Delete | h=Hide | Esc=Close",
    ]
    assert "selected=1" in frames["duplicates_selected"]["overlay"]
    assert frames["duplicates_info"]["overlay"][0] == "Transaction Details"
    assert "hidden=1" in frames["duplicates_hidden"]["overlay"]
    assert frames["duplicates_delete_confirmation"]["overlay"] == [
        "⚠️  Delete Transaction?",
        "Are you sure you want to delete this transaction?\nThis action CANNOT be undone!",
        "Enter=Delete | Esc=Cancel",
    ]
    assert frames["duplicates_delete_cancel"]["overlay"][0].startswith("🔍 Found 1")
    assert frames["duplicates_closed"]["overlay"] == []
