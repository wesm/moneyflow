"""Strict loader and existing-Polars adapter for the shared parity fixture."""

import json
import re
from dataclasses import dataclass
from datetime import date
from decimal import Decimal, InvalidOperation
from pathlib import Path
from typing import Any, cast

import polars as pl

from moneyflow.backends.base import FinanceBackend
from moneyflow.data.data_manager import DataManager

ROOT_KEYS = {"schema_version", "currencies", "transactions"}
CURRENCY_KEYS = {"code", "scale"}
TRANSACTION_KEYS = {
    "id",
    "provider_id",
    "provider",
    "account",
    "date",
    "merchant",
    "category",
    "amount",
    "currency",
    "hidden",
    "pending",
    "notes",
    "metadata",
}
ENTITY_KEYS = {"id", "name"}
CATEGORY_KEYS = {"id", "name", "group"}


@dataclass(frozen=True)
class FixtureDocument:
    """A validated fixture document that retains exact decimal strings."""

    currencies: dict[str, int]
    transactions: tuple[dict[str, Any], ...]


def load_document(path: Path) -> FixtureDocument:
    """Load a version-one fixture while rejecting unknown or unsafe values."""
    try:
        raw = json.loads(path.read_text())
    except (OSError, json.JSONDecodeError) as error:
        raise ValueError(f"load fixture: {error}") from error
    root = _mapping(raw, "document")
    _exact_keys(root, ROOT_KEYS, "document")
    if root["schema_version"] != 1:
        raise ValueError("load fixture: unsupported schema version")

    currencies_raw = _list(root["currencies"], "currencies")
    currencies: dict[str, int] = {}
    for index, value in enumerate(currencies_raw):
        field = f"currencies[{index}]"
        currency = _mapping(value, field)
        _exact_keys(currency, CURRENCY_KEYS, field)
        code = _nonempty_string(currency["code"], f"{field}.code")
        if re.fullmatch(r"[A-Z]{3}", code) is None:
            raise ValueError(f"load fixture: {field}.code is invalid")
        scale = currency["scale"]
        if isinstance(scale, bool) or not isinstance(scale, int) or not 0 <= scale <= 255:
            raise ValueError(f"load fixture: {field}.scale is invalid")
        if code in currencies:
            raise ValueError(f"load fixture: {field}.code is duplicate")
        currencies[code] = scale

    transactions_raw = _list(root["transactions"], "transactions")
    transactions: list[dict[str, Any]] = []
    seen_ids: set[str] = set()
    for index, value in enumerate(transactions_raw):
        field = f"transactions[{index}]"
        transaction = _mapping(value, field)
        _allowed_keys(transaction, TRANSACTION_KEYS, field)
        required = TRANSACTION_KEYS - {"notes", "metadata"}
        missing = required - transaction.keys()
        if missing:
            raise ValueError(f"load fixture: {field} is missing {sorted(missing)[0]}")
        normalized = _validate_transaction(transaction, currencies, field)
        transaction_id = normalized["id"]
        if transaction_id in seen_ids:
            raise ValueError(f"load fixture: {field}.id is duplicate")
        seen_ids.add(transaction_id)
        transactions.append(normalized)
    return FixtureDocument(currencies=dict(currencies), transactions=tuple(transactions))


def adapt_to_polars(
    document: FixtureDocument, config_dir: Path
) -> tuple[pl.DataFrame, DataManager]:
    """Adapt through DataManager so Python remains the behavioral oracle."""
    manager = DataManager(
        cast(FinanceBackend, object()),
        config_dir=str(config_dir),
        merchant_cache_dir=str(config_dir),
    )
    manager.category_to_group = {
        transaction["category"]["name"]: transaction["category"]["group"]
        for transaction in document.transactions
    }
    raw_transactions: list[dict[str, Any]] = []
    for transaction in document.transactions:
        raw_transactions.append(
            {
                "id": transaction["id"],
                "provider_id": transaction["provider_id"],
                "provider": transaction["provider"],
                "account": {
                    "id": transaction["account"]["id"],
                    "displayName": transaction["account"]["name"],
                },
                "date": transaction["date"],
                "merchant": dict(transaction["merchant"]),
                "category": {
                    "id": transaction["category"]["id"],
                    "name": transaction["category"]["name"],
                },
                "amount": float(Decimal(transaction["amount"])),
                "currency": transaction["currency"],
                "scale": document.currencies[transaction["currency"]],
                "notes": transaction.get("notes", ""),
                "hideFromReports": transaction["hidden"],
                "pending": transaction["pending"],
                "isRecurring": False,
            }
        )
    dataframe = manager._transactions_to_dataframe(raw_transactions, {})
    return manager.apply_category_groups(dataframe), manager


def _validate_transaction(
    transaction: dict[str, Any], currencies: dict[str, int], field: str
) -> dict[str, Any]:
    normalized = dict(transaction)
    for name in ("id", "provider_id", "provider"):
        normalized[name] = _nonempty_string(transaction[name], f"{field}.{name}")
    normalized["account"] = _entity(transaction["account"], f"{field}.account")
    normalized["merchant"] = _entity(transaction["merchant"], f"{field}.merchant")
    normalized["category"] = _category(transaction["category"], f"{field}.category")
    date_text = _nonempty_string(transaction["date"], f"{field}.date")
    if re.fullmatch(r"[0-9]{4}-[0-9]{2}-[0-9]{2}", date_text) is None:
        raise ValueError(f"load fixture: {field}.date is invalid")
    try:
        date.fromisoformat(date_text)
    except ValueError as error:
        raise ValueError(f"load fixture: {field}.date is invalid") from error
    normalized["date"] = date_text
    currency = _nonempty_string(transaction["currency"], f"{field}.currency")
    if currency not in currencies:
        raise ValueError(f"load fixture: {field}.currency is undeclared")
    amount = _nonempty_string(transaction["amount"], f"{field}.amount")
    if re.fullmatch(r"[+-]?[0-9]+(?:\.[0-9]+)?", amount) is None:
        raise ValueError(f"load fixture: {field}.amount is invalid")
    try:
        decimal = Decimal(amount)
    except InvalidOperation as error:
        raise ValueError(f"load fixture: {field}.amount is invalid") from error
    exponent = decimal.as_tuple().exponent
    if not decimal.is_finite() or not isinstance(exponent, int) or exponent < -currencies[currency]:
        raise ValueError(f"load fixture: {field}.amount exceeds currency scale")
    minor = decimal * (10 ** currencies[currency])
    if minor < -(2**63) or minor > 2**63 - 1:
        raise ValueError(f"load fixture: {field}.amount exceeds integer range")
    normalized["amount"] = amount
    normalized["currency"] = currency
    for name in ("hidden", "pending"):
        if not isinstance(transaction[name], bool):
            raise ValueError(f"load fixture: {field}.{name} is invalid")
    if "notes" in transaction and not isinstance(transaction["notes"], str):
        raise ValueError(f"load fixture: {field}.notes is invalid")
    metadata = transaction.get("metadata", {})
    if not isinstance(metadata, dict) or any(
        not isinstance(key, str) or not isinstance(value, str) for key, value in metadata.items()
    ):
        raise ValueError(f"load fixture: {field}.metadata is invalid")
    normalized["metadata"] = dict(metadata)
    return normalized


def _entity(value: Any, field: str) -> dict[str, str]:
    entity = _mapping(value, field)
    _exact_keys(entity, ENTITY_KEYS, field)
    return {
        "id": _nonempty_string(entity["id"], f"{field}.id"),
        "name": _nonempty_string(entity["name"], f"{field}.name"),
    }


def _category(value: Any, field: str) -> dict[str, str]:
    category = _mapping(value, field)
    _exact_keys(category, CATEGORY_KEYS, field)
    return {
        "id": _nonempty_string(category["id"], f"{field}.id"),
        "name": _nonempty_string(category["name"], f"{field}.name"),
        "group": _nonempty_string(category["group"], f"{field}.group"),
    }


def _mapping(value: Any, field: str) -> dict[str, Any]:
    if not isinstance(value, dict) or any(not isinstance(key, str) for key in value):
        raise ValueError(f"load fixture: {field} must be an object")
    return value


def _list(value: Any, field: str) -> list[Any]:
    if not isinstance(value, list):
        raise ValueError(f"load fixture: {field} must be an array")
    return value


def _nonempty_string(value: Any, field: str) -> str:
    if not isinstance(value, str) or not value:
        raise ValueError(f"load fixture: {field} must be a non-empty string")
    return value


def _exact_keys(value: dict[str, Any], expected: set[str], field: str) -> None:
    if value.keys() != expected:
        raise ValueError(f"load fixture: {field} has missing or unknown fields")


def _allowed_keys(value: dict[str, Any], allowed: set[str], field: str) -> None:
    if unknown := value.keys() - allowed:
        raise ValueError(f"load fixture: {field} has unknown field {sorted(unknown)[0]}")
