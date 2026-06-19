"""Shared enums and infrastructure for data export."""

from enum import Enum


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
