"""Tests for Amazon matching behavior in MoneyflowApp."""

import sqlite3
from pathlib import Path

from moneyflow.app import MoneyflowApp


def _create_amazon_db(profile_dir: Path, orders: list[dict]) -> None:
    db_path = profile_dir / "amazon.db"
    conn = sqlite3.connect(db_path)

    conn.execute(
        """
        CREATE TABLE IF NOT EXISTS transactions (
            id TEXT PRIMARY KEY,
            date TEXT NOT NULL,
            merchant TEXT NOT NULL,
            category TEXT NOT NULL DEFAULT 'Uncategorized',
            category_id TEXT NOT NULL DEFAULT 'cat_uncategorized',
            amount REAL NOT NULL,
            quantity INTEGER NOT NULL,
            asin TEXT NOT NULL,
            order_id TEXT NOT NULL,
            account TEXT NOT NULL,
            order_status TEXT,
            shipment_status TEXT,
            notes TEXT,
            hideFromReports INTEGER DEFAULT 0,
            imported_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
        )
        """
    )

    for order in orders:
        order_id = order["order_id"]
        order_date = order["date"]
        for item in order["items"]:
            clean_order = order_id.replace("-", "").replace(" ", "")
            txn_id = f"amz_{item['asin']}_{clean_order}"

            conn.execute(
                """
                INSERT INTO transactions
                (id, date, merchant, amount, quantity, asin, order_id, account, order_status, shipment_status)
                VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
                """,
                (
                    txn_id,
                    order_date,
                    item["name"],
                    item["amount"],
                    item["quantity"],
                    item["asin"],
                    order_id,
                    order_id,
                    "Closed",
                    "Delivered",
                ),
            )

    conn.commit()
    conn.close()


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
    matches, searched = app._find_amazon_matches(
        {"merchant": "Amazon", "amount": -12.99, "date": "2025-01-10"}
    )

    assert searched is True
    assert len(matches) == 1
    assert matches[0].order_id == "113-1234567-8901234"
