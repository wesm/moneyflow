"""Shared enums and infrastructure for data export."""

import json
import os
from dataclasses import dataclass
from datetime import datetime
from enum import Enum
from pathlib import Path
from typing import Optional

import polars as pl

from .file_utils import secure_write_file


class ExportFormat(Enum):
    """Supported export output formats."""

    PARQUET = "parquet"

    @property
    def display_name(self) -> str:
        """Human-readable label for UI display."""
        name_overrides = {
            self.PARQUET: "Parquet",
        }
        return name_overrides.get(self, self.value.capitalize())


class ExportScope(Enum):
    """Scope of data to include in an export."""

    FULL = "full"

    @property
    def display_name(self) -> str:
        """Human-readable label for UI display."""
        name_overrides = {
            self.FULL: "Full dataset",
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
