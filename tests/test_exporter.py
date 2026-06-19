"""Tests for export enums, metadata, and file writing."""

import json
import os
import re
from datetime import date
from pathlib import Path

import polars as pl
import pytest

from moneyflow.data.exporter import (
    ExportFormat,
    ExportMetadata,
    ExportScope,
    build_export_path,
    export_dataframe,
)


class TestExportFormat:
    """Tests for ExportFormat enum."""

    def test_parquet_value(self) -> None:
        """Verify PARQUET has the expected value."""
        assert ExportFormat.PARQUET.value == "parquet"

    def test_parquet_display_name(self) -> None:
        """Verify PARQUET has the expected display name."""
        assert ExportFormat.PARQUET.display_name == "Parquet"

    def test_enum_members(self) -> None:
        """Verify current member count (grows with subsequent issues)."""
        assert len(ExportFormat) == 1


class TestExportScope:
    """Tests for ExportScope enum."""

    def test_full_value(self) -> None:
        """Verify FULL has the expected value."""
        assert ExportScope.FULL.value == "full"

    def test_full_display_name(self) -> None:
        """Verify FULL has the expected display name."""
        assert ExportScope.FULL.display_name == "Full dataset"

    def test_enum_members(self) -> None:
        """Verify current member count (grows with subsequent issues)."""
        assert len(ExportScope) == 1


class TestExportMetadata:
    """Tests for ExportMetadata dataclass."""

    def test_constructs_with_all_fields(self) -> None:
        """Verify ExportMetadata can be constructed with all fields."""
        meta = ExportMetadata(
            app_version="1.0.0",
            export_timestamp="2026-06-19T12:00:00",
            transaction_count=150,
            earliest_date="2024-01-15",
            latest_date="2026-06-19",
            backend_type="monarch",
            category_groups=["Food & Dining", "Transport"],
        )
        assert meta.app_version == "1.0.0"
        assert meta.transaction_count == 150
        assert meta.backend_type == "monarch"
        assert len(meta.category_groups) == 2

    def test_optional_date_fields_none(self) -> None:
        """Verify date fields can be None for empty datasets."""
        meta = ExportMetadata(
            app_version="1.0.0",
            export_timestamp="2026-06-19T12:00:00",
            transaction_count=0,
            earliest_date=None,
            latest_date=None,
            backend_type="demo",
            category_groups=[],
        )
        assert meta.earliest_date is None
        assert meta.latest_date is None
        assert meta.category_groups == []

    def test_empty_category_groups(self) -> None:
        """Verify category_groups can be an empty list."""
        meta = ExportMetadata(
            app_version="1.0.0",
            export_timestamp="2026-06-19T12:00:00",
            transaction_count=0,
            earliest_date=None,
            latest_date=None,
            backend_type="demo",
            category_groups=[],
        )
        assert meta.category_groups == []


class TestBuildExportPath:
    """Tests for build_export_path helper."""

    def test_returns_path_with_correct_extension(self, tmp_path: Path) -> None:
        """Verify path ends with correct extension for format."""
        path = build_export_path(tmp_path, ExportFormat.PARQUET, ExportScope.FULL)
        assert path.suffix == ".parquet"

    def test_filename_contains_scope_label(self, tmp_path: Path) -> None:
        """Verify filename includes the scope value."""
        path = build_export_path(tmp_path, ExportFormat.PARQUET, ExportScope.FULL)
        assert "full-export" in path.name

    def test_filename_has_timestamp_prefix(self, tmp_path: Path) -> None:
        """Verify filename starts with a timestamp pattern."""
        path = build_export_path(tmp_path, ExportFormat.PARQUET, ExportScope.FULL)
        timestamp_pattern = r"^\d{4}-\d{2}-\d{2}_\d{6}-"
        assert re.match(timestamp_pattern, path.name) is not None

    def test_creates_exports_directory(self, tmp_path: Path) -> None:
        """Verify the exports directory is created with 0o700."""
        build_export_path(tmp_path, ExportFormat.PARQUET, ExportScope.FULL)
        exports_dir = tmp_path / "exports"
        assert exports_dir.is_dir()

    def test_exports_directory_permissions(self, tmp_path: Path) -> None:
        """Verify the exports directory has 0o700 permissions."""
        build_export_path(tmp_path, ExportFormat.PARQUET, ExportScope.FULL)
        exports_dir = tmp_path / "exports"
        mode = os.stat(exports_dir).st_mode & 0o777
        assert mode == 0o700

    def test_is_under_exports_subdirectory(self, tmp_path: Path) -> None:
        """Verify path is inside an 'exports' subdirectory."""
        path = build_export_path(tmp_path, ExportFormat.PARQUET, ExportScope.FULL)
        assert path.parent.name == "exports"
        assert path.parent.parent == tmp_path


class TestExportDataframeParquet:
    """Tests for exporting DataFrame to Parquet."""

    @pytest.fixture
    def sample_df(self) -> pl.DataFrame:
        """Create a small DataFrame for export tests."""
        return pl.DataFrame(
            {
                "id": ["txn_1", "txn_2"],
                "date": [date(2024, 10, 1), date(2024, 10, 2)],
                "amount": [-45.67, -23.45],
                "merchant": ["Whole Foods", "Starbucks"],
                "category": ["Groceries", "Restaurants & Bars"],
                "account": ["Chase Checking", "Chase Checking"],
            }
        )

    @pytest.fixture
    def sample_metadata(self) -> ExportMetadata:
        """Create sample metadata for export tests."""
        return ExportMetadata(
            app_version="test-1.0.0",
            export_timestamp="2026-06-19T12:00:00",
            transaction_count=2,
            earliest_date="2024-10-01",
            latest_date="2024-10-02",
            backend_type="demo",
            category_groups=["Food & Dining"],
        )

    def test_writes_valid_parquet(self, sample_df, sample_metadata, tmp_path: Path) -> None:
        """Verify export_dataframe writes a valid Parquet file."""
        path = tmp_path / "exports" / "test.parquet"
        path.parent.mkdir(parents=True, exist_ok=True)
        result = export_dataframe(
            sample_df, path=path, metadata=sample_metadata, fmt=ExportFormat.PARQUET
        )
        assert result == path
        assert path.exists()
        assert path.stat().st_size > 0

        loaded = pl.read_parquet(str(path))
        assert len(loaded) == 2
        assert loaded["id"].to_list() == ["txn_1", "txn_2"]

    def test_parquet_file_permissions(self, sample_df, sample_metadata, tmp_path: Path) -> None:
        """Verify Parquet file has 0o600 permissions."""
        path = tmp_path / "exports" / "test.parquet"
        path.parent.mkdir(parents=True, exist_ok=True)
        export_dataframe(sample_df, path=path, metadata=sample_metadata, fmt=ExportFormat.PARQUET)
        mode = os.stat(path).st_mode & 0o777
        assert mode == 0o600

    def test_writes_sidecar_metadata(self, sample_df, sample_metadata, tmp_path: Path) -> None:
        """Verify a .meta.json sidecar file is written alongside the Parquet."""
        path = tmp_path / "exports" / "test.parquet"
        path.parent.mkdir(parents=True, exist_ok=True)
        export_dataframe(sample_df, path=path, metadata=sample_metadata, fmt=ExportFormat.PARQUET)
        meta_path = path.with_suffix(".meta.json")
        assert meta_path.exists()
        assert meta_path.stat().st_size > 0

    def test_sidecar_contains_all_keys(self, sample_df, sample_metadata, tmp_path: Path) -> None:
        """Verify sidecar JSON contains all expected metadata keys."""
        path = tmp_path / "exports" / "test.parquet"
        path.parent.mkdir(parents=True, exist_ok=True)
        export_dataframe(sample_df, path=path, metadata=sample_metadata, fmt=ExportFormat.PARQUET)
        meta_path = path.with_suffix(".meta.json")
        with open(meta_path) as f:
            meta_data = json.load(f)

        assert meta_data["app_version"] == "test-1.0.0"
        assert meta_data["transaction_count"] == 2
        assert meta_data["backend_type"] == "demo"
        assert meta_data["earliest_date"] == "2024-10-01"
        assert meta_data["category_groups"] == ["Food & Dining"]
        assert meta_data["export_file"] == "test.parquet"

    def test_sidecar_metadata_permissions(self, sample_df, sample_metadata, tmp_path: Path) -> None:
        """Verify the sidecar .meta.json file has 0o600 permissions."""
        path = tmp_path / "exports" / "test.parquet"
        path.parent.mkdir(parents=True, exist_ok=True)
        export_dataframe(sample_df, path=path, metadata=sample_metadata, fmt=ExportFormat.PARQUET)
        meta_path = path.with_suffix(".meta.json")
        mode = os.stat(meta_path).st_mode & 0o777
        assert mode == 0o600

    def test_empty_dataframe(self, tmp_path: Path) -> None:
        """Verify exporting an empty DataFrame produces valid files."""
        schema = {"id": pl.Utf8, "amount": pl.Float64}
        df = pl.DataFrame(schema=schema)
        meta = ExportMetadata(
            app_version="test",
            export_timestamp="2026-06-19T12:00:00",
            transaction_count=0,
            earliest_date=None,
            latest_date=None,
            backend_type="demo",
            category_groups=[],
        )
        path = tmp_path / "exports" / "empty.parquet"
        path.parent.mkdir(parents=True, exist_ok=True)
        export_dataframe(df, path=path, metadata=meta, fmt=ExportFormat.PARQUET)

        loaded = pl.read_parquet(str(path))
        assert len(loaded) == 0


class TestExportDataframeDispatcher:
    """Tests for the export_dataframe dispatcher."""

    def test_raises_value_error_for_unsupported_format(self, tmp_path: Path) -> None:
        """Verify unsupported format raises ValueError."""
        df = pl.DataFrame({"id": ["test"], "amount": [1.0]})
        meta = ExportMetadata(
            app_version="test",
            export_timestamp="2026-06-19T12:00:00",
            transaction_count=1,
            earliest_date=None,
            latest_date=None,
            backend_type="demo",
            category_groups=[],
        )
        path = tmp_path / "test.parquet"
        path.parent.mkdir(parents=True, exist_ok=True)
        with pytest.raises(ValueError, match="Unsupported export format"):
            export_dataframe(df, path=path, metadata=meta, fmt="csv")  # type: ignore[arg-type]


class TestExportDataframeEdgeCases:
    """Tests for edge cases in the export pipeline."""

    def test_unwritable_directory_raises_error(self, sample_metadata: ExportMetadata) -> None:
        """Verify export raises an error when the directory is not writable."""
        df = pl.DataFrame({"id": ["test"], "amount": [1.0]})
        path = Path("/dev/null/no-permission/test.parquet")
        with pytest.raises((PermissionError, OSError)):
            export_dataframe(df, path=path, metadata=sample_metadata, fmt=ExportFormat.PARQUET)

    @pytest.fixture
    def sample_metadata(self) -> ExportMetadata:
        """Create sample metadata for edge case tests."""
        return ExportMetadata(
            app_version="test",
            export_timestamp="2026-06-19T12:00:00",
            transaction_count=1,
            earliest_date=None,
            latest_date=None,
            backend_type="demo",
            category_groups=[],
        )
