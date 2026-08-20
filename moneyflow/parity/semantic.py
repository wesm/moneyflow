"""Capture renderer-neutral semantic frames from the Python Textual application."""

import argparse
import asyncio
import json
import sqlite3
import tempfile
from dataclasses import replace
from datetime import date
from pathlib import Path
from typing import Any

import textual
from rich.text import Text
from textual.geometry import Region
from textual.widgets import Checkbox, DataTable, Input, RadioSet

from moneyflow.data.state import SortDirection, SortMode, TimeGranularity, ViewMode
from moneyflow.parity.backend import FixtureBackend
from moneyflow.parity.fixture import synthetic_group_id
from moneyflow.parity.logical import canonical_json
from moneyflow.tui.app import MoneyflowApp
from moneyflow.tui.backend_config import AMAZON_CONFIG, MONARCH_CONFIG
from moneyflow.tui.keybindings import get_help_text

REPOSITORY_ROOT = Path(__file__).resolve().parents[2]
DEFAULT_SCENARIOS = REPOSITORY_ROOT / "testdata/parity/frame_scenarios.json"
DEFAULT_OUTPUT = REPOSITORY_ROOT / "testdata/parity/semantic_frames"


def load_scenarios(path: Path) -> dict[str, Any]:
    """Load strict version-one semantic frame scenarios."""
    try:
        document = json.loads(path.read_text())
    except (OSError, json.JSONDecodeError) as error:
        raise ValueError(f"load frame scenarios: {error}") from error
    if not isinstance(document, dict) or document.keys() != {
        "schema_version",
        "fixture",
        "scenarios",
    }:
        raise ValueError("load frame scenarios: invalid document")
    if document["schema_version"] != 1 or not isinstance(document["fixture"], str):
        raise ValueError("load frame scenarios: unsupported document")
    scenarios = document["scenarios"]
    if not isinstance(scenarios, list) or not scenarios:
        raise ValueError("load frame scenarios: scenarios are required")
    names: set[str] = set()
    required = {"name", "width", "height", "theme", "initial", "keys"}
    allowed = required | {"fixture", "profile_kind"}
    for index, scenario in enumerate(scenarios):
        if (
            not isinstance(scenario, dict)
            or not required.issubset(scenario)
            or not set(scenario).issubset(allowed)
            or (
                "fixture" in scenario
                and (not isinstance(scenario["fixture"], str) or not scenario["fixture"])
            )
        ):
            raise ValueError(f"load frame scenarios: scenarios[{index}] is invalid")
        if scenario.get("profile_kind", "fixture") not in {
            "fixture",
            "amazon",
            "finance_with_amazon",
        }:
            raise ValueError(f"load frame scenarios: scenarios[{index}].profile_kind is invalid")
        name = scenario["name"]
        if not isinstance(name, str) or not name or name in names:
            raise ValueError(f"load frame scenarios: scenarios[{index}].name is invalid")
        names.add(name)
        if (
            not isinstance(scenario["width"], int)
            or not isinstance(scenario["height"], int)
            or scenario["width"] < 1
            or scenario["height"] < 1
            or not isinstance(scenario["keys"], list)
        ):
            raise ValueError(f"load frame scenarios: scenarios[{index}] has invalid dimensions")
    return document


async def generate_frames(scenarios_path: Path, working_root: Path) -> dict[str, dict[str, Any]]:
    """Run every scenario through Textual using only the committed fixture backend."""
    document = load_scenarios(scenarios_path)
    frames: dict[str, dict[str, Any]] = {}
    for index, scenario in enumerate(document["scenarios"]):
        scenario_root = working_root / f"scenario-{index}"
        profile_root = scenario_root / "profile"
        profile_root.mkdir(parents=True)
        _create_parity_amazon_database(scenario_root)
        fixture_path = REPOSITORY_ROOT / scenario.get("fixture", document["fixture"])
        profile_kind = scenario.get("profile_kind", "fixture")
        backend = FixtureBackend(
            fixture_path,
            backend_type="amazon" if profile_kind == "amazon" else "fixture",
        )
        base_config = AMAZON_CONFIG if profile_kind == "amazon" else MONARCH_CONFIG
        config = replace(
            base_config,
            backend_type="amazon" if profile_kind == "amazon" else "fixture",
            requires_auth=False,
        )
        app = MoneyflowApp(
            backend=backend,
            config=config,
            config_dir=str(scenario_root),
            profile_dir=profile_root,
            backend_type="amazon" if profile_kind == "amazon" else "fixture",
            cache_path=None,
            theme_override=scenario["theme"],
        )
        async with app.run_test(size=(scenario["width"], scenario["height"])) as pilot:
            await _wait_ready(app, pilot)
            _apply_initial(app, scenario["initial"], backend)
            app.refresh_view()
            await pilot.pause()
            await pilot.pause()
            for key_name in scenario["keys"]:
                await pilot.press(key_name)
                await pilot.pause()
                await pilot.pause()
            frames[scenario["name"]] = _extract_frame(app, scenario, backend)
    return frames


def _create_parity_amazon_database(config_dir: Path) -> None:
    """Install one isolated synthetic order used by finance matching frames."""
    profile_dir = config_dir / "profiles" / "amazon"
    profile_dir.mkdir(parents=True, exist_ok=True)
    with sqlite3.connect(profile_dir / "amazon.db") as connection:
        connection.execute(
            """
            CREATE TABLE transactions (
                id TEXT PRIMARY KEY,
                order_id TEXT NOT NULL,
                date TEXT NOT NULL,
                merchant TEXT NOT NULL,
                amount REAL NOT NULL,
                quantity INTEGER DEFAULT 1,
                asin TEXT
            )
            """
        )
        connection.execute(
            """
            INSERT INTO transactions (id, order_id, date, merchant, amount, quantity, asin)
            VALUES (?, ?, ?, ?, ?, ?, ?)
            """,
            (
                "amazon-parity-item",
                "order-example",
                "2026-08-19",
                "Example Headphones",
                -12.34,
                1,
                "ASIN-EXAMPLE",
            ),
        )


def check_frames(generated: dict[str, dict[str, Any]], output_dir: Path) -> None:
    """Fail when any committed semantic frame differs from Python behavior."""
    expected_names = {f"{name}.json" for name in generated}
    actual_names = (
        {path.name for path in output_dir.glob("*.json")} if output_dir.exists() else set()
    )
    if actual_names != expected_names:
        raise AssertionError("semantic frame set differs; run --update deliberately and review it")
    for name, frame in generated.items():
        path = output_dir / f"{name}.json"
        try:
            committed = json.loads(path.read_text())
        except (OSError, json.JSONDecodeError) as error:
            raise AssertionError(
                f"semantic frame {name!r} is missing or invalid: {error}"
            ) from error
        if committed != frame:
            raise AssertionError(
                f"semantic frame {name!r} differs; run --update deliberately and review it"
            )


def update_frames(generated: dict[str, dict[str, Any]], output_dir: Path) -> None:
    """Write the deliberate canonical Python semantic artifacts."""
    output_dir.mkdir(parents=True, exist_ok=True)
    expected = {f"{name}.json" for name in generated}
    for existing in output_dir.glob("*.json"):
        if existing.name not in expected:
            existing.unlink()
    for name, frame in generated.items():
        (output_dir / f"{name}.json").write_text(canonical_json(frame))


async def _wait_ready(app: MoneyflowApp, pilot: Any) -> None:
    for _ in range(80):
        if app.controller is not None and app.state.current_data is not None:
            for _ in range(3):
                await pilot.pause()
            return
        await pilot.pause()
    raise AssertionError("Python semantic adapter: fixture app did not become ready")


def _apply_initial(app: MoneyflowApp, initial: dict[str, Any], backend: FixtureBackend) -> None:
    state = app.state
    state.view_mode = (
        ViewMode.DETAIL if initial["mode"] == "detail" else ViewMode(initial["dimension"])
    )
    state.sort_by = SortMode(initial["sort"]["field"])
    state.sort_direction = SortDirection(initial["sort"]["direction"])
    state.time_granularity = TimeGranularity(initial["time_granularity"])
    search_query = initial.get("search", "")
    if search_query:
        amazon_match_ids = app.amazon_presentation.search_amazon_items_for_query(
            search_query,
            state.transactions_df,
            state.start_date,
            state.end_date,
        )
        state.set_search(search_query, amazon_match_ids)
    else:
        state.search_query = ""
    state.show_hidden = initial["show_hidden"]
    state.show_transfers = initial["show_transfers"]
    if raw_range := initial.get("date_range"):
        state.start_date = date.fromisoformat(raw_range["start"])
        state.end_date = date.fromisoformat(raw_range["end"])
    for drilldown in initial["drilldowns"]:
        dimension = drilldown["dimension"]
        if dimension == "time":
            period = drilldown["period"]
            state.selected_time_year = period["year"]
            state.selected_time_month = period.get("month")
            state.selected_time_day = period.get("day")
        else:
            setattr(state, f"selected_{dimension}", drilldown["label"])
    subgroup = initial.get("sub_grouping")
    state.sub_grouping_mode = ViewMode(subgroup) if subgroup else None
    state.selected_ids = set(initial["selected_transaction_ids"])
    state.selected_group_keys = {
        _aggregate_label(key, initial["dimension"], backend)
        for key in initial["selected_aggregate_keys"]
    }


def _aggregate_label(key: str, dimension: str, backend: FixtureBackend) -> str:
    key = _aggregate_key_from_identity(key, dimension)
    for transaction in backend.document.transactions:
        if dimension == "merchant" and transaction["merchant"]["id"] == key:
            return transaction["merchant"]["name"]
        if dimension == "category" and transaction["category"]["id"] == key:
            return transaction["category"]["name"]
        if dimension == "account" and transaction["account"]["id"] == key:
            return transaction["account"]["name"]
        if dimension == "group" and synthetic_group_id(transaction["category"]["group"]) == key:
            return transaction["category"]["group"]
    return key


def _aggregate_key_from_identity(identity: str, dimension: str) -> str:
    prefix = f"{dimension}:"
    if not identity.startswith(prefix):
        return identity
    length_text, separator, remainder = identity[len(prefix) :].partition(":")
    if not separator or not length_text.isdigit():
        return identity
    key_length = int(length_text)
    encoded = remainder.encode()
    if len(encoded) <= key_length or encoded[key_length : key_length + 1] != b":":
        return identity
    try:
        return encoded[:key_length].decode()
    except UnicodeDecodeError:
        return identity


def _extract_frame(
    app: MoneyflowApp, scenario: dict[str, Any], backend: FixtureBackend
) -> dict[str, Any]:
    compositor = getattr(app.screen, "_compositor", None)
    if compositor is None or not hasattr(compositor, "render_strips"):
        raise RuntimeError(
            f"Python semantic adapter requires update for Textual {textual.__version__}: "
            "screen compositor API is unavailable"
        )
    strips = compositor.render_strips()
    if len(strips) != scenario["height"]:
        raise RuntimeError(
            f"Python semantic adapter requires update for Textual {textual.__version__}: "
            f"expected {scenario['height']} compositor rows, got {len(strips)}"
        )

    table = app.query_one("#data-table", DataTable)
    table_region = table.content_region
    first_visible = max(0, int(table.scroll_y))
    visible_count = min(max(0, table.row_count - first_visible), max(0, table_region.height - 1))
    table_body = _strip_region(
        strips,
        "table_body",
        Region(table_region.x, table_region.y + 1, table_region.width, visible_count),
    )
    table_body["lines"] = [_strip_framework_scrollbar(line) for line in table_body["lines"]]
    regions = [
        _widget_region(app, strips, "breadcrumb", "#breadcrumb"),
        _widget_region(app, strips, "stats", "#stats"),
        _strip_region(
            strips,
            "table_header",
            Region(table_region.x, table_region.y, table_region.width, 1),
            strip_scrollbar=True,
        ),
        table_body,
        _widget_region(app, strips, "hints", "#action-hints"),
    ]
    overlay = _overlay_region(app, strips)
    if overlay is not None:
        # Modal screens obscure the base frame. Keep their semantic crop independent from
        # Textual's backdrop opacity and framework-owned underlay composition.
        regions = [overlay]

    row_values = [table.get_row_at(first_visible + index) for index in range(visible_count)]
    return {
        "schema_version": 1,
        "name": scenario["name"],
        "width": scenario["width"],
        "height": scenario["height"],
        "regions": regions,
        "columns": _column_starts(table),
        "visible_row_ids": _visible_row_ids(app, first_visible, visible_count, backend),
        "breadcrumb": str(app.query_one("#breadcrumb").render()),
        "stats": str(app.query_one("#stats").render()),
        "flags": [_plain(row[-1]) for row in row_values],
        "selection_ids": _selection_ids(app, backend),
        "hints": str(app.query_one("#action-hints").render()),
        "overlay": _overlay_semantics(app),
    }


def _widget_region(
    app: MoneyflowApp, strips: list[Any], name: str, selector: str
) -> dict[str, Any]:
    return _strip_region(strips, name, app.query_one(selector).content_region)


def _overlay_region(app: MoneyflowApp, strips: list[Any]) -> dict[str, Any] | None:
    selectors = (
        ("#delete-dialog", "#delete-dialog"),
        ("#detail-dialog", "#detail-dialog"),
        ("#duplicates-container", "#duplicates-title"),
        ("#search-dialog", "#search-title"),
        ("#filter-dialog", "#filter-title"),
        ("#help-dialog", "#help-title"),
        ("#export-container", "#export-title"),
    )
    for dialog_selector, semantic_selector in selectors:
        matches = list(app.screen.query(dialog_selector))
        if matches:
            return _strip_region(
                strips, "overlay", app.screen.query_one(semantic_selector).content_region
            )
    return None


def _overlay_semantics(app: MoneyflowApp) -> list[str]:
    if list(app.screen.query("#delete-dialog")):
        return [
            str(app.screen.query_one("#delete-title").render()),
            str(app.screen.query_one("#delete-message").render()),
            str(app.screen.query_one("#delete-instructions").render()),
        ]
    if list(app.screen.query("#detail-dialog")):
        return [str(app.screen.query_one("#detail-title").render())]
    if list(app.screen.query("#duplicates-container")):
        table = app.screen.query_one("#duplicates-table", DataTable)
        duplicate_screen = app.screen
        selected_ids = getattr(duplicate_screen, "selected_ids", set())
        pending_hide = sum(
            edit.field == "hide_from_reports" for edit in app.data_manager.pending_edits
        )
        rows = [
            " | ".join(_plain(value) for value in table.get_row_at(index))
            for index in range(table.row_count)
        ]
        return [
            str(app.screen.query_one("#duplicates-title").render()),
            str(app.screen.query_one("#status-line").render()),
            f"selected={len(selected_ids)}",
            f"hidden={pending_hide}",
            *rows,
        ]
    if list(app.screen.query("#search-dialog")):
        return [
            "🔍 Search Transactions",
            "Type to search merchant or category names",
            "Press Enter with empty search to clear filter",
            app.screen.query_one("#search-input", Input).value,
        ]
    if list(app.screen.query("#filter-dialog")):
        return [
            "🔍 Filter Options",
            "h=Toggle hidden | t=Toggle transfers | Enter=Apply | Esc=Cancel",
            f"show_hidden={str(app.screen.query_one('#show-hidden-checkbox', Checkbox).value).lower()}",
            f"show_transfers={str(app.screen.query_one('#show-transfers-checkbox', Checkbox).value).lower()}",
            "Apply (Enter)",
            "Cancel (Esc)",
        ]
    if list(app.screen.query("#help-dialog")):
        return [
            *get_help_text().splitlines(),
            "j/k=Scroll | Esc/Enter=Close",
            "Close (Enter)",
        ]
    if list(app.screen.query("#export-container")):
        format_set = app.screen.query_one("#format-select", RadioSet)
        scope_set = app.screen.query_one("#scope-select", RadioSet)
        return [
            "Export Data",
            str(format_set.pressed_button.label),
            str(scope_set.pressed_button.label),
            "Export",
            "Cancel",
        ]
    return []


def _strip_framework_scrollbar(line: str) -> str:
    """Exclude Textual-owned scrollbar cells that overlap table content geometry."""
    return line.rstrip(" ▁▂▃▄▅▆▇█")


def _strip_region(
    strips: list[Any], name: str, region: Region, *, strip_scrollbar: bool = False
) -> dict[str, Any]:
    start_y = max(0, region.y)
    end_y = min(len(strips), region.y + region.height)
    lines = [
        strips[y].crop(region.x, region.x + region.width).text.rstrip()
        for y in range(start_y, end_y)
    ]
    if strip_scrollbar:
        lines = [_strip_framework_scrollbar(line) for line in lines]
    if region.width > 0 and region.height > 0 and not lines:
        raise RuntimeError(
            f"Python semantic adapter requires update for Textual {textual.__version__}: "
            f"region {name!r} rendered no rows"
        )
    return {
        "name": name,
        "origin": {"x": region.x, "y": start_y},
        "width": region.width,
        "height": len(lines),
        "lines": lines,
    }


def _column_starts(table: DataTable[Any]) -> list[int]:
    starts: list[int] = []
    start = 1
    for column in table.columns.values():
        starts.append(start)
        start += column.width + 2
    return starts


def _visible_row_ids(
    app: MoneyflowApp, start: int, count: int, backend: FixtureBackend
) -> list[str]:
    data = app.state.current_data
    if data is None or data.is_empty():
        return []
    rows = data.slice(start, count).to_dicts()
    if app.state.view_mode == ViewMode.DETAIL and app.state.sub_grouping_mode is None:
        return [str(row["id"]) for row in rows]
    dimension = app.state.sub_grouping_mode or app.state.view_mode
    return [_aggregate_identity(row, dimension, backend) for row in rows]


def _selection_ids(app: MoneyflowApp, backend: FixtureBackend) -> list[str]:
    state = app.state
    if state.view_mode == ViewMode.DETAIL and state.sub_grouping_mode is None:
        return sorted(state.selected_ids)
    data = state.current_data
    if data is None or data.is_empty():
        return []
    dimension = state.sub_grouping_mode or state.view_mode
    identities = [
        _aggregate_identity(row, dimension, backend)
        for row in data.to_dicts()
        if _aggregate_display_label(row, dimension) in state.selected_group_keys
    ]
    return sorted(identities)


def _aggregate_identity(row: dict[str, Any], dimension: ViewMode, backend: FixtureBackend) -> str:
    key = _aggregate_key(row, dimension)
    partitions = _partitions_for_key(key, dimension, backend)
    if len(partitions) != 1:
        raise RuntimeError(
            "Python semantic adapter cannot identify an aggregate row across multiple money partitions"
        )
    currency, scale = next(iter(partitions))
    return f"{dimension.value}:{len(key.encode())}:{key}:{scale}:{currency}"


def _aggregate_key(row: dict[str, Any], dimension: ViewMode) -> str:
    if dimension == ViewMode.TIME:
        return str(row["time_period_display"])
    if dimension == ViewMode.GROUP:
        return synthetic_group_id(str(row[dimension.value]))
    id_field = f"{dimension.value}_id"
    if id_field in row:
        return str(row[id_field])
    return str(row[dimension.value])


def _aggregate_display_label(row: dict[str, Any], dimension: ViewMode) -> str:
    if dimension == ViewMode.TIME:
        return str(row["time_period_display"])
    return str(row[dimension.value])


def _partitions_for_key(
    key: str, dimension: ViewMode, backend: FixtureBackend
) -> set[tuple[str, int]]:
    partitions: set[tuple[str, int]] = set()
    for transaction in backend.document.transactions:
        matches = (
            (dimension == ViewMode.MERCHANT and transaction["merchant"]["id"] == key)
            or (dimension == ViewMode.CATEGORY and transaction["category"]["id"] == key)
            or (dimension == ViewMode.ACCOUNT and transaction["account"]["id"] == key)
            or (
                dimension == ViewMode.GROUP
                and synthetic_group_id(transaction["category"]["group"]) == key
            )
            or dimension == ViewMode.TIME
        )
        if matches:
            currency = transaction["currency"]
            partitions.add((currency, backend.document.currencies[currency]))
    return partitions


def _plain(value: Any) -> str:
    return value.plain if isinstance(value, Text) else str(value)


def main() -> None:
    parser = argparse.ArgumentParser(description=__doc__)
    mode = parser.add_mutually_exclusive_group(required=True)
    mode.add_argument("--check", action="store_true")
    mode.add_argument("--update", action="store_true")
    args = parser.parse_args()
    with tempfile.TemporaryDirectory(prefix="moneyflow-semantic-") as temporary:
        generated = asyncio.run(generate_frames(DEFAULT_SCENARIOS, Path(temporary)))
    if args.update:
        update_frames(generated, DEFAULT_OUTPUT)
    else:
        check_frames(generated, DEFAULT_OUTPUT)


if __name__ == "__main__":
    main()
