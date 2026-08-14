"""Tests for the shared cross-language fixture boundary."""

import copy
import json
from pathlib import Path
from typing import Any

import polars as pl
import pytest

from moneyflow.data.data_manager import TRANSACTION_SCHEMA
from moneyflow.parity.fixture import adapt_to_polars, load_document, synthetic_group_id

PARITY_FIXTURE = Path("testdata/parity/transactions.json")


def test_synthetic_group_id_matches_go_fixture_adapter() -> None:
    assert synthetic_group_id("  LIVING  ") == ("group-synthetic-y5ivhjfvyvob7hmorb76h7vix4")


def test_loads_shared_fixture_and_adapts_existing_schema(tmp_path: Path) -> None:
    document = load_document(PARITY_FIXTURE)

    assert len(document.transactions) == 32
    assert document.transactions[0]["amount"] == "-54.20"
    assert document.transactions[-1]["id"] == "txn-032"

    dataframe, data_manager = adapt_to_polars(document, tmp_path)
    assert dataframe.height == 32
    expected_schema = TRANSACTION_SCHEMA | {"group": pl.String}
    assert all(dataframe.schema[name] == data_type for name, data_type in expected_schema.items())
    assert dataframe.get_column("group").to_list()[0] == "Living"
    assert data_manager.category_to_group["Transfer"] == "Transfers"


@pytest.mark.parametrize(
    "mutate",
    [
        lambda value: value.update(schema_version=2),
        lambda value: value.update(extra=True),
        lambda value: value["transactions"].append(copy.deepcopy(value["transactions"][0])),
        lambda value: value["transactions"][0].update(currency="EUR"),
        lambda value: value["transactions"][0].update(amount="-54.201"),
        lambda value: value["transactions"][0].update(date="2024-02-30"),
        lambda value: value["transactions"][0]["merchant"].update(name=""),
        lambda value: value["currencies"][0].update(code="U1D"),
        lambda value: value["transactions"][0].update(date="2024-02-29T00:00:00"),
        lambda value: value["transactions"][0].update(amount="1e2"),
        lambda value: value["transactions"][0].update(amount="92233720368547758.08"),
    ],
    ids=[
        "schema-version",
        "unknown-field",
        "duplicate-id",
        "undeclared-currency",
        "amount-precision",
        "date",
        "empty-label",
        "currency-digit",
        "noncanonical-date",
        "scientific-amount",
        "amount-overflow",
    ],
)
def test_rejects_invalid_fixture(tmp_path: Path, mutate: Any) -> None:
    raw = json.loads(PARITY_FIXTURE.read_text())
    mutate(raw)
    path = tmp_path / "transactions.json"
    path.write_text(json.dumps(raw))

    with pytest.raises(ValueError):
        load_document(path)
