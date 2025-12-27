"""Textual integration smoke tests."""

import pytest
from textual.widgets import DataTable

from moneyflow.app import MoneyflowApp


async def _wait_for_app_ready(app: MoneyflowApp, pilot, attempts: int = 40) -> None:
    for _ in range(attempts):
        if app.controller is not None and app.state.current_data is not None:
            return
        await pilot.pause()
    raise AssertionError("App did not finish initializing in time")


@pytest.mark.integration
async def test_demo_mode_starts_and_populates_table(tmp_path):
    app = MoneyflowApp(demo_mode=True, config_dir=str(tmp_path))

    async with app.run_test() as pilot:
        await _wait_for_app_ready(app, pilot)

        table = app.query_one("#data-table", DataTable)
        assert len(table.columns) > 0
        assert table.row_count > 0
