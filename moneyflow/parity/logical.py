"""Generate deterministic logical expectations from the existing Python behavior."""

import argparse
import json
from datetime import date
from decimal import ROUND_HALF_UP, Decimal
from pathlib import Path
from typing import Any

import polars as pl

from moneyflow.data.state import AppState, SortDirection, SortMode, TimeGranularity, ViewMode
from moneyflow.parity.fixture import FixtureDocument, adapt_to_polars, load_document
from moneyflow.tui.formatters import ViewPresenter

REPOSITORY_ROOT = Path(__file__).resolve().parents[2]
DEFAULT_TRANSACTIONS = REPOSITORY_ROOT / "testdata/parity/transactions.json"
DEFAULT_CASES = REPOSITORY_ROOT / "testdata/parity/logical_cases.json"
DEFAULT_EXPECTATIONS = REPOSITORY_ROOT / "testdata/parity/logical_expectations.json"


def load_cases(path: Path) -> list[dict[str, Any]]:
    """Load the query-only logical case document."""
    try:
        raw = json.loads(path.read_text())
    except (OSError, json.JSONDecodeError) as error:
        raise ValueError(f"load logical cases: {error}") from error
    if not isinstance(raw, dict) or raw.keys() != {"schema_version", "cases"}:
        raise ValueError("load logical cases: invalid document")
    if raw["schema_version"] != 1 or not isinstance(raw["cases"], list):
        raise ValueError("load logical cases: unsupported document")
    names: set[str] = set()
    cases: list[dict[str, Any]] = []
    for index, value in enumerate(raw["cases"]):
        if not isinstance(value, dict) or not isinstance(value.get("name"), str):
            raise ValueError(f"load logical cases: cases[{index}] is invalid")
        if value["name"] in names:
            raise ValueError(f"load logical cases: cases[{index}].name is duplicate")
        names.add(value["name"])
        cases.append(value)
    return cases


def generate_expectations(
    transactions_path: Path, cases_path: Path, working_directory: Path
) -> dict[str, Any]:
    """Run every case through AppState, DataManager, and ViewPresenter behavior."""
    document = load_document(transactions_path)
    dataframe, manager = adapt_to_polars(document, working_directory)
    outputs = [_run_case(case, document, dataframe, manager) for case in load_cases(cases_path)]
    return {"schema_version": 1, "cases": outputs}


def check_expectations(generated: dict[str, Any], path: Path) -> None:
    """Fail when the committed expectations do not match the current oracle."""
    try:
        committed = json.loads(path.read_text())
    except (OSError, json.JSONDecodeError) as error:
        raise AssertionError(f"logical expectations are missing or invalid: {error}") from error
    if committed != generated:
        raise AssertionError(
            "logical expectations differ; inspect behavior and run with --update deliberately"
        )


def canonical_json(value: dict[str, Any]) -> str:
    """Encode canonical sorted, indented JSON with one trailing newline."""
    return json.dumps(value, indent=2, sort_keys=True) + "\n"


def _run_case(
    case: dict[str, Any], document: FixtureDocument, dataframe: pl.DataFrame, manager: Any
) -> dict[str, Any]:
    state = _state_for_case(case, dataframe)
    filtered = state.get_filtered_df()
    if filtered is None:
        raise AssertionError("fixture dataframe unexpectedly absent")

    statistics = _statistics(filtered, document)
    if case["mode"] == "detail":
        detail_rows = _detail_rows(filtered, case, document)
        aggregate_rows: list[dict[str, Any]] = []
    else:
        detail_rows = []
        aggregate_rows = _aggregate_rows(filtered, case, document, manager)

    dates = filtered.get_column("date").to_list() if not filtered.is_empty() else []
    output: dict[str, Any] = {
        "name": case["name"],
        "query": {key: value for key, value in case.items() if key != "name"},
        "result": {
            "aggregate_rows": aggregate_rows,
            "date_range": (
                {"start": min(dates).isoformat(), "end": max(dates).isoformat()} if dates else None
            ),
            "detail_rows": detail_rows,
            "filtered_count": filtered.height,
            "filtered_ids": filtered.get_column("id").to_list(),
            "statistics": statistics,
        },
    }
    return output


def _state_for_case(case: dict[str, Any], dataframe: pl.DataFrame) -> AppState:
    mode = case["mode"]
    group_by = case.get("group_by", "merchant")
    state = AppState(
        transactions_df=dataframe,
        view_mode=ViewMode.DETAIL if mode == "detail" else ViewMode(group_by),
        sort_by=SortMode(case["sort"]["field"]),
        sort_direction=SortDirection(case["sort"]["direction"]),
        time_granularity=TimeGranularity(case["time_granularity"]),
        search_query=case.get("search", ""),
        show_hidden=case["show_hidden"],
        show_transfers=case["show_transfers"],
    )
    if date_range := case.get("date_range"):
        state.start_date = date.fromisoformat(date_range["start"])
        state.end_date = date.fromisoformat(date_range["end"])
    for drilldown in case.get("drilldowns", []):
        dimension = drilldown["dimension"]
        if dimension == "time":
            period = drilldown["period"]
            state.selected_time_year = period["year"]
            state.selected_time_month = period.get("month")
            state.selected_time_day = period.get("day")
        else:
            setattr(state, f"selected_{dimension}", drilldown["label"])
    return state


def _detail_rows(
    filtered: pl.DataFrame, case: dict[str, Any], document: FixtureDocument
) -> list[dict[str, Any]]:
    if filtered.is_empty():
        return []
    field = case["sort"]["field"]
    descending = ViewPresenter.should_sort_descending(
        field, SortDirection(case["sort"]["direction"])
    )
    ordered = filtered.sort([field, "id"], descending=[descending, False])
    scales = document.currencies
    rows: list[dict[str, Any]] = []
    for row in ordered.iter_rows(named=True):
        currency = str(row["currency"])
        rows.append(
            {
                "account": {"id": row["account_id"], "name": row["account"]},
                "amount": _money(row["amount"], currency, scales[currency]),
                "category": {
                    "group": row["group"],
                    "id": row["category_id"],
                    "name": row["category"],
                },
                "date": row["date"].isoformat(),
                "hidden": row["hideFromReports"],
                "id": row["id"],
                "merchant": {"id": row["merchant_id"], "name": row["merchant"]},
                "pending": row["pending"],
            }
        )
    return rows


def _statistics(filtered: pl.DataFrame, document: FixtureDocument) -> list[dict[str, Any]]:
    statistics: list[dict[str, Any]] = []
    for currency, scale in sorted(document.currencies.items()):
        partition = filtered.filter(pl.col("currency") == currency)
        amounts = [
            _minor(value, scale)
            for value in partition.filter(~pl.col("hideFromReports")).get_column("amount")
        ]
        incoming = sum(value for value in amounts if value > 0)
        outgoing = sum(value for value in amounts if value < 0)
        statistics.append(
            {
                "count": partition.height,
                "currency": currency,
                "in": _money_minor(incoming, currency, scale),
                "net": _money_minor(incoming + outgoing, currency, scale),
                "out": _money_minor(outgoing, currency, scale),
                "scale": scale,
            }
        )
    return statistics


def _aggregate_rows(
    filtered: pl.DataFrame, case: dict[str, Any], document: FixtureDocument, manager: Any
) -> list[dict[str, Any]]:
    dimension = case["group_by"]
    granularity = TimeGranularity(case["time_granularity"])
    if dimension == "time":
        aggregate = manager.aggregate_by_time(filtered, granularity)
        label_field = "time_period_display"
    else:
        aggregate = getattr(manager, f"aggregate_by_{dimension}")(filtered)
        label_field = dimension
    if aggregate.is_empty():
        return []
    sort_field = case["sort"]["field"]
    sort_column = "total" if sort_field == "amount" else sort_field
    if sort_field == "time_period":
        sort_column = "time_period_display"
    elif sort_field in {"merchant", "category", "group", "account"}:
        sort_column = label_field
    descending = ViewPresenter.should_sort_descending(
        sort_column, SortDirection(case["sort"]["direction"])
    )
    aggregate = aggregate.sort([sort_column, label_field], descending=[descending, False])

    currency = next(iter(document.currencies))
    scale = document.currencies[currency]
    totals = [_minor(value, scale) for value in aggregate.get_column("total")]
    total_income = sum(total for total in totals if total > 0)
    total_expenses = -sum(total for total in totals if total < 0)
    rows: list[dict[str, Any]] = []
    for row, total in zip(aggregate.iter_rows(named=True), totals, strict=True):
        period = _period(row, case["time_granularity"]) if dimension == "time" else None
        label = str(row[label_field])
        if period is not None:
            label = ViewPresenter.format_time_period(
                period["year"], period.get("month"), period.get("day"), granularity
            )
        key = _aggregate_key(row, dimension)
        result = {
            "count": row["count"],
            "currency": currency,
            "dimension": dimension,
            "key": key,
            "label": label,
            "scale": scale,
            "share_tenths": _share_tenths(total, total_income, total_expenses),
            "total": _money_minor(total, currency, scale),
        }
        if period is not None:
            result["period"] = period
        if dimension == "merchant" and row.get("top_category") is not None:
            result["top_category"] = row["top_category"]
            result["top_category_percent"] = row["top_category_pct"]
        rows.append(result)
    return rows


def _aggregate_key(row: dict[str, Any], dimension: str) -> str:
    if dimension in {"merchant", "category", "account"}:
        return str(row[f"{dimension}_id"])
    if dimension == "time":
        return str(row["time_period_display"])
    return str(row[dimension])


def _period(row: dict[str, Any], granularity: str) -> dict[str, Any]:
    period: dict[str, Any] = {"granularity": granularity, "year": row["year"]}
    if granularity in {"month", "day"}:
        period["month"] = row["month"]
    if granularity == "day":
        period["day"] = row["day"]
    return period


def _minor(value: Any, scale: int) -> int:
    quantum = Decimal(1).scaleb(-scale)
    return int(Decimal(str(value)).quantize(quantum, rounding=ROUND_HALF_UP) * (10**scale))


def _money(value: Any, currency: str, scale: int) -> dict[str, Any]:
    return _money_minor(_minor(value, scale), currency, scale)


def _money_minor(minor: int, currency: str, scale: int) -> dict[str, Any]:
    return {"currency": currency, "minor": minor, "scale": scale}


def _share_tenths(total: int, income: int, expenses: int) -> int:
    denominator = income if total > 0 else expenses
    if total == 0 or denominator == 0:
        return 0
    numerator = abs(total) * 1000
    return (numerator + denominator // 2) // denominator


def main() -> None:
    """Run the deliberate update or read-only check command."""
    parser = argparse.ArgumentParser(description=__doc__)
    mode = parser.add_mutually_exclusive_group(required=True)
    mode.add_argument("--check", action="store_true")
    mode.add_argument("--update", action="store_true")
    args = parser.parse_args()
    generated = generate_expectations(DEFAULT_TRANSACTIONS, DEFAULT_CASES, Path.cwd())
    if args.update:
        DEFAULT_EXPECTATIONS.write_text(canonical_json(generated))
    else:
        check_expectations(generated, DEFAULT_EXPECTATIONS)


if __name__ == "__main__":
    main()
