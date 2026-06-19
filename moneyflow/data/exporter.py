"""Shared enums and infrastructure for data export."""

import json
import os
import sqlite3
from dataclasses import dataclass
from datetime import datetime
from enum import Enum
from pathlib import Path
from typing import Optional

import polars as pl

from .file_utils import secure_write_file

DANGEROUS_PREFIX_PATTERN = r"^\s*[=+\-@\t\r]"


class ExportFormat(Enum):
    """Supported export output formats."""

    PARQUET = "parquet"
    CSV = "csv"
    SQLITE = "sqlite"

    @property
    def display_name(self) -> str:
        """Human-readable label for UI display."""
        name_overrides = {
            self.PARQUET: "Parquet",
            self.CSV: "CSV",
            self.SQLITE: "SQLite",
        }
        return name_overrides.get(self, self.value)


class ExportScope(Enum):
    """Scope of data to include in an export."""

    FULL = "full"
    SNAPSHOT = "snapshot"

    @property
    def display_name(self) -> str:
        """Human-readable label for UI display."""
        name_overrides = {
            self.FULL: "Full dataset",
            self.SNAPSHOT: "Current view",
        }
        return name_overrides.get(self, self.value.capitalize())


@dataclass
class ExportMetadata:
    """Metadata accompanying an export file.

    Contains only non-sensitive app-level information.
    No credentials, tokens, or encrypted blobs are included.
    """

    app_version: str
    export_timestamp: str
    transaction_count: int
    earliest_date: Optional[str]
    latest_date: Optional[str]
    backend_type: str
    category_groups: list[str]


def build_export_path(config_dir: Path, fmt: ExportFormat, scope: ExportScope) -> Path:
    """Build the export file path with timestamp-based naming.

    Creates the exports directory with secure permissions if it doesn't exist.
    Naming pattern: ``<timestamp>-<scope>-export.<ext>``

    Args:
        config_dir: Application config directory (e.g. ``~/.moneyflow``).
        fmt: Target export format.
        scope: Export scope (full dataset, snapshot, etc.).

    Returns:
        Path to the export file.
    """
    timestamp = datetime.now().strftime("%Y-%m-%d_%H%M%S")
    filename = f"{timestamp}-{scope.value}-export.{fmt.value}"
    exports_dir = config_dir / "exports"
    exports_dir.mkdir(mode=0o700, parents=True, exist_ok=True)
    return exports_dir / filename


def export_dataframe(
    df: pl.DataFrame,
    *,
    path: Path,
    metadata: ExportMetadata,
    fmt: ExportFormat = ExportFormat.PARQUET,
    scope: ExportScope = ExportScope.FULL,
) -> Path:
    """Export a DataFrame to the specified format with metadata.

    Args:
        df: DataFrame to export.
        path: Destination file path.
        metadata: Export metadata to attach.
        fmt: Output format.
        scope: Export scope.

    Returns:
        The path to the exported file.

    Raises:
        ValueError: If the format is not supported.
    """
    if fmt == ExportFormat.PARQUET:
        return _export_parquet(df, path=path, metadata=metadata)
    if fmt == ExportFormat.CSV:
        return _export_csv(df, path=path, metadata=metadata)
    if fmt == ExportFormat.SQLITE:
        return _export_sqlite(df, path=path, metadata=metadata)
    raise ValueError(f"Unsupported export format: {fmt}")


def _export_parquet(
    df: pl.DataFrame,
    *,
    path: Path,
    metadata: ExportMetadata,
) -> Path:
    """Write DataFrame as Parquet with sidecar metadata."""
    df.write_parquet(str(path))
    os.chmod(path, 0o600)

    meta_path = path.with_suffix(".meta.json")
    meta_data = {
        "app_version": metadata.app_version,
        "export_timestamp": metadata.export_timestamp,
        "transaction_count": metadata.transaction_count,
        "earliest_date": metadata.earliest_date,
        "latest_date": metadata.latest_date,
        "backend_type": metadata.backend_type,
        "category_groups": metadata.category_groups,
        "export_file": path.name,
    }
    secure_write_file(meta_path, json.dumps(meta_data, indent=2), mode="w")

    return path


def _export_sqlite(
    df: pl.DataFrame,
    *,
    path: Path,
    metadata: ExportMetadata,
) -> Path:
    """Write DataFrame as SQLite database with transactions and metadata tables."""
    path.unlink(missing_ok=True)

    conn = sqlite3.connect(str(path))
    try:
        os.chmod(path, 0o600)

        conn.execute("CREATE TABLE metadata (key TEXT, value TEXT)")
        groups = ", ".join(metadata.category_groups) if metadata.category_groups else "N/A"
        meta_rows = [
            ("app_version", metadata.app_version),
            ("export_timestamp", metadata.export_timestamp),
            ("transaction_count", str(metadata.transaction_count)),
            ("earliest_date", metadata.earliest_date or ""),
            ("latest_date", metadata.latest_date or ""),
            ("backend_type", metadata.backend_type),
            ("category_groups", groups),
        ]
        conn.executemany("INSERT INTO metadata (key, value) VALUES (?, ?)", meta_rows)

        df_text = df.with_columns(pl.all().cast(pl.String))
        columns = df_text.columns
        escaped_cols = [c.replace('"', '""') for c in columns]
        col_defs = ", ".join(f'"{c}" TEXT' for c in escaped_cols)
        placeholders = ", ".join(["?"] * len(columns))
        col_list = ", ".join(f'"{c}"' for c in escaped_cols)

        conn.execute(f"CREATE TABLE transactions ({col_defs})")
        conn.executemany(
            f"INSERT INTO transactions ({col_list}) VALUES ({placeholders})",
            df_text.rows(),
        )
        conn.commit()
    except Exception:
        conn.close()
        path.unlink(missing_ok=True)
        raise

    conn.close()
    return path


def _sanitize_csv_cells(df: pl.DataFrame) -> pl.DataFrame:
    """Prefix dangerous leading characters with a single quote to prevent formula injection.

    Spreadsheet engines (Excel, Google Sheets, LibreOffice Calc) interpret cells
    starting with ``=``, ``+``, ``-``, ``@``, tab, or carriage return as formulas.
    Leading whitespace is included because some engines strip it before checking.
    """
    sanitized = []
    for col in df.columns:
        if df[col].dtype in (pl.Utf8, pl.String):
            sanitized.append(
                pl.when(pl.col(col).str.contains(DANGEROUS_PREFIX_PATTERN))
                .then(pl.lit("'") + pl.col(col))
                .otherwise(pl.col(col))
                .alias(col)
            )
        else:
            sanitized.append(pl.col(col))
    return df.with_columns(sanitized)


def _export_csv(
    df: pl.DataFrame,
    *,
    path: Path,
    metadata: ExportMetadata,
) -> Path:
    """Write DataFrame as CSV with a metadata comment header."""
    df = _sanitize_csv_cells(df)
    groups = ", ".join(metadata.category_groups) if metadata.category_groups else "N/A"
    header_lines = [
        f"# Export from moneyflow v{metadata.app_version}",
        f"# Date: {metadata.export_timestamp}",
        f"# Transactions: {metadata.transaction_count}",
        f"# Date range: {metadata.earliest_date or 'N/A'} - {metadata.latest_date or 'N/A'}",
        f"# Backend: {metadata.backend_type}",
        f"# Category groups: {groups}",
    ]
    header = "\n".join(header_lines) + "\n"
    data = df.write_csv()
    secure_write_file(path, header + data, mode="w")
    return path
