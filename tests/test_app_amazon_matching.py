"""Tests for Amazon matching behavior in MoneyflowApp."""

from pathlib import Path

import polars as pl
import pytest

from moneyflow.tui.app import MoneyflowApp
from tests.conftest import _create_amazon_db


def test_find_amazon_matches_uses_config_dir(tmp_path: Path) -> None:
    config_dir = tmp_path / "config"
    profile_dir = config_dir / "profiles" / "amazon"
    profile_dir.mkdir(parents=True)

    _create_amazon_db(
        profile_dir,
        [
            {
                "order_id": "113-1234567-8901234",
                "date": "2025-01-10",
                "items": [
                    {"name": "USB Cable", "amount": -12.99, "quantity": 1, "asin": "B001"},
                ],
            }
        ],
    )

    app = MoneyflowApp(config_dir=str(config_dir))
    matches, searched = app.amazon_presentation.find_amazon_matches(
        {"merchant": "Amazon", "amount": -12.99, "date": "2025-01-10"}
    )

    assert searched is True
    assert len(matches) == 1
    assert matches[0].order_id == "113-1234567-8901234"


def test_amazon_filtered_view_refresh_no_attribute_error(tmp_path: Path) -> None:
    """Regression test for the AttributeError when rendering an Amazon-filtered view."""
    from unittest.mock import MagicMock

    from tests.mock_view import MockViewPresenter

    app = MoneyflowApp(config_dir=str(tmp_path))
    app.backend = MagicMock()
    app._initialize_managers()

    # Use MockViewPresenter so we don't need a running Textual app
    app.controller.view = MockViewPresenter()
    app.controller._get_display_labels = lambda: {}

    # Set up the data manager to contain only Amazon transactions
    app.data_manager.df = pl.DataFrame(
        {
            "id": ["1", "2"],
            "date": ["2025-01-10", "2025-01-11"],
            "amount": [-10.0, -20.0],
            "merchant": ["Amazon", "Amazon"],
            "category": ["Shopping", "Shopping"],
            "group": ["Shopping", "Shopping"],
            "account": ["Checking", "Checking"],
            "merchant_id": ["m1", "m1"],
            "category_id": ["c1", "c1"],
            "account_id": ["a1", "a1"],
            "notes": ["", ""],
            "hideFromReports": [False, False],
            "pending": [False, False],
        }
    )
    app.state.transactions_df = app.data_manager.df

    # Refresh view should not crash (especially with an AttributeError for amazon cache)
    try:
        app.controller.refresh_view()
    except AttributeError as e:
        pytest.fail(f"refresh_view raised AttributeError: {e}")
