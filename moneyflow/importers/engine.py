"""Generic CSV import engine with pluggable institution mappings."""
from dataclasses import dataclass


@dataclass(frozen=True)
class InstitutionMapping:
    """Per-institution column mapping and CSV parsing configuration."""

    name: str
    display_name: str
    file_pattern: str
    id_prefix: str
    date_fmt: str | None

    column_map: dict[str, str]
    amount_sign: int
    skip_rows: int

    dedup_fields: tuple[str, ...]
    extra_columns: tuple[str, ...]

    date_columns: tuple[str, ...] | None
    id_fields: tuple[str, ...]

    currency: str
    default_category: str
    default_category_id: str

    encoding: str
    debit_column: str | None
    credit_column: str | None

    def validate(self) -> None:
        """Raise ValueError if the mapping is missing required fields."""
        required_standard = {"date", "amount", "merchant"}
        mapped_values = set(self.column_map.values())

        mapped_values = set(self.column_map.values())
        if self.debit_column is not None and self.credit_column is not None:
            if "amount" in mapped_values:
                raise ValueError(
                    "Cannot specify both 'amount' column and split debit/credit columns"
                )
        elif self.debit_column is not None or self.credit_column is not None:
            raise ValueError(
                "Must specify both debit_column and credit_column, or neither"
            )
        else:
            missing = required_standard - mapped_values
            if missing:
                raise ValueError(
                    f"Missing required column_map targets: {', '.join(sorted(missing))}"
                )
