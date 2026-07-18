"""Shared enums and infrastructure for data export."""

import json
import os
import re
import sqlite3
from dataclasses import dataclass
from datetime import datetime
from enum import Enum
from pathlib import Path
from typing import Optional

import polars as pl

from .file_utils import (
    create_restrictive_temp_file,
    ensure_restrictive_directory,
    replace_restrictive_file,
    secure_write_file,
)

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

    @property
    def extension(self) -> str:
        """File extension to use for generated export paths."""
        extension_overrides = {
            self.SQLITE: "db",
        }
        return extension_overrides.get(self, self.value)


class ExportScope(Enum):
    """Scope of data to include in an export."""

    FULL = "full"
    SNAPSHOT = "snapshot"

    @property
    def display_name(self) -> str:
        """Human-readable label for UI display."""
        name_overrides = {
            self.FULL: "Full dataset",
            self.SNAPSHOT: "Filtered transactions",
        }
        return name_overrides.get(self, self.value.capitalize())


@dataclass
class ExportMetadata:
    """Metadata accompanying an export file.

    Contains export context and app configuration details.
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
    timestamp = datetime.now().strftime("%Y-%m-%d_%H%M%S_%f")
    filename = f"{timestamp}-{scope.value}-export.{fmt.extension}"
    exports_dir = config_dir / "exports"
    ensure_restrictive_directory(exports_dir, parents=True)
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
    fd, tmp_path_str = create_restrictive_temp_file(path.parent, prefix=".tmp_parquet_")
    tmp_path = Path(tmp_path_str)
    os.close(fd)
    try:
        df.write_parquet(str(tmp_path))
        replace_restrictive_file(tmp_path, path)
    except Exception:
        tmp_path.unlink(missing_ok=True)
        raise

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
    fd, tmp_path_str = create_restrictive_temp_file(path.parent, prefix=".tmp_sqlite_")
    tmp_path = Path(tmp_path_str)
    os.close(fd)

    conn = sqlite3.connect(str(tmp_path))
    try:
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
        tmp_path.unlink(missing_ok=True)
        raise

    conn.close()
    try:
        replace_restrictive_file(tmp_path, path)
    except Exception:
        tmp_path.unlink(missing_ok=True)
        raise
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


def _sanitize_csv_field(value: str) -> str:
    """Strip CSV row/cell delimiters and neutralize formula-capable metadata values."""
    sanitized = value.replace("\r\n", " ").replace("\r", " ").replace("\n", " ").replace(",", " ")
    if re.search(DANGEROUS_PREFIX_PATTERN, sanitized):
        return f"'{sanitized}"
    return sanitized


def _export_csv(
    df: pl.DataFrame,
    *,
    path: Path,
    metadata: ExportMetadata,
) -> Path:
    """Write DataFrame as CSV with a metadata comment header."""
    df = _sanitize_csv_cells(df)
    groups = (
        "; ".join(_sanitize_csv_field(group) for group in metadata.category_groups)
        if metadata.category_groups
        else "N/A"
    )
    header_lines = [
        f"# Export from moneyflow v{_sanitize_csv_field(metadata.app_version)}",
        f"# Date: {_sanitize_csv_field(metadata.export_timestamp)}",
        f"# Transactions: {metadata.transaction_count}",
        f"# Date range: {_sanitize_csv_field(metadata.earliest_date or 'N/A')} - {_sanitize_csv_field(metadata.latest_date or 'N/A')}",
        f"# Backend: {_sanitize_csv_field(metadata.backend_type)}",
        f"# Category groups: {groups}",
    ]
    header = "\n".join(header_lines) + "\n"
    data = df.write_csv()
    secure_write_file(path, header + data, mode="w")
    return path
