"""Tests for Amazon matching behavior in MoneyflowApp."""

from pathlib import Path

from moneyflow.app import MoneyflowApp
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
