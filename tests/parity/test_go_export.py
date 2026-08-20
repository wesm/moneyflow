"""Cross-implementation validation for the Go Parquet export."""

from __future__ import annotations

import subprocess
from pathlib import Path

import polars as pl
import pyarrow.parquet as pq


def test_go_parquet_export_round_trips_in_python(tmp_path: Path) -> None:
    output = tmp_path / "go-export.parquet"
    subprocess.run(
        [
            "go",
            "run",
            "./internal/tools/exportfixture",
            "--output",
            str(output),
        ],
        check=True,
        cwd=Path(__file__).parents[2],
    )

    frame = pl.read_parquet(output)
    assert frame.height == 1
    assert frame["transaction_id"].to_list() == ["txn-example"]
    assert frame["amount"].to_list() == ["-12.34"]
    assert frame["amount_minor"].to_list() == [-1234]
    assert str(frame.schema["amount_minor"]) == "Int64"

    raw_metadata = pq.read_metadata(output).metadata
    assert raw_metadata is not None
    metadata = {key.decode(): value.decode() for key, value in raw_metadata.items()}
    expected = {
        "moneyflow_export_schema_version": "2",
        "moneyflow_app_version": "v2-test",
        "exported_at_utc": "2026-08-19T15:30:00.123Z",
        "source_revision": "42",
        "journal_cursor": "2",
        "excluded_pending_operation_count": "2",
        "inactive_redo_operation_count": "1",
        "scope": "full",
        "canonical_query": "",
        "transaction_count": "1",
        "earliest_date": "2026-08-19",
        "latest_date": "2026-08-19",
        "provider_kinds": '["fixture"]',
    }
    for key, value in expected.items():
        assert metadata[key] == value
