"""Tests for committed Python-oracle logical expectations."""

from pathlib import Path

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
