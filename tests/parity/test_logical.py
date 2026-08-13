"""Tests for committed Python-oracle logical expectations."""

import copy
import json
from pathlib import Path

import pytest

from moneyflow.parity.logical import check_expectations, generate_expectations, load_cases

TRANSACTIONS = Path("testdata/parity/transactions.json")
CASES = Path("testdata/parity/logical_cases.json")
EXPECTATIONS = Path("testdata/parity/logical_expectations.json")


def test_logical_cases_cover_required_behaviors() -> None:
    cases = load_cases(CASES)
    names = {case["name"] for case in cases}
    dimensions = {case.get("group_by") for case in cases}
    aggregate_sorts = {
        case["group_by"]: {
            candidate["sort"]["field"]
            for candidate in cases
            if candidate.get("group_by") == case["group_by"]
        }
        for case in cases
        if case["mode"] == "aggregate"
    }
    detail_sorts = {case["sort"]["field"] for case in cases if case["mode"] == "detail"}

    assert dimensions >= {"merchant", "category", "group", "account", "time"}
    assert aggregate_sorts == {
        "merchant": {"amount", "count", "merchant"},
        "category": {"amount", "count", "category"},
        "group": {"amount", "count", "group"},
        "account": {"amount", "count", "account"},
        "time": {"amount", "count", "time_period"},
    }
    assert detail_sorts == {"date", "amount", "merchant", "category", "account"}
    assert {"time_month_gaps", "time_day_leap_gap", "multi_level_drilldown"} <= names


def test_committed_expectations_match_python_oracle(tmp_path: Path) -> None:
    generated = generate_expectations(TRANSACTIONS, CASES, tmp_path)
    check_expectations(generated, EXPECTATIONS)


def test_logical_cases_reject_invalid_query_shapes(tmp_path: Path) -> None:
    base = load_cases(CASES)[0]
    invalid_cases: list[dict[str, object]] = []

    unknown = copy.deepcopy(base)
    unknown["unexpected"] = True
    invalid_cases.append(unknown)

    reverse_range = copy.deepcopy(base)
    reverse_range["date_range"] = {"start": "2025-01-02", "end": "2025-01-01"}
    invalid_cases.append(reverse_range)

    duplicate_drill = copy.deepcopy(base)
    duplicate_drill["drilldowns"] = [
        {"dimension": "merchant", "key": "merchant-a", "label": "A"},
        {"dimension": "merchant", "key": "merchant-b", "label": "B"},
    ]
    invalid_cases.append(duplicate_drill)

    incompatible_sort = copy.deepcopy(base)
    incompatible_sort["sort"] = {"field": "category", "direction": "asc"}
    invalid_cases.append(incompatible_sort)

    malformed_period = copy.deepcopy(base)
    malformed_period["mode"] = "detail"
    malformed_period.pop("group_by")
    malformed_period["sort"] = {"field": "date", "direction": "asc"}
    malformed_period["drilldowns"] = [
        {
            "dimension": "time",
            "period": {"granularity": "month", "year": 2024, "month": 13},
        }
    ]
    invalid_cases.append(malformed_period)

    for index, case in enumerate(invalid_cases):
        path = tmp_path / f"invalid-{index}.json"
        path.write_text(json.dumps({"schema_version": 1, "cases": [case]}))
        with pytest.raises(ValueError, match=r"cases\[0\]"):
            load_cases(path)
