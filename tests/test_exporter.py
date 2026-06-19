"""Tests for export enums and shared infrastructure."""

from moneyflow.data.exporter import ExportFormat, ExportScope


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
