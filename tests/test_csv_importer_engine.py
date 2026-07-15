"""Tests for CSV import engine and InstitutionMapping."""
import pytest

from moneyflow.importers.engine import InstitutionMapping


class TestInstitutionMapping:
    def test_minimal_valid_mapping_passes_validation(self):
        mapping = InstitutionMapping(
            name="test_bank",
            display_name="Test Bank",
            file_pattern="test_*.csv",
            id_prefix="test_",
            date_fmt="%m/%d/%Y",
            column_map={
                "Date": "date",
                "Description": "merchant",
                "Amount": "amount",
            },
            amount_sign=1,
            skip_rows=0,
            dedup_fields=("date", "amount", "merchant"),
            extra_columns=(),
            date_columns=("date",),
            id_fields=("date", "amount", "merchant"),
            currency="USD",
            default_category="Uncategorized",
            default_category_id="cat_uncategorized",
            encoding="utf-8",
            debit_column=None,
            credit_column=None,
        )
        mapping.validate()

    def test_missing_required_column_map_raises_value_error(self):
        mapping = InstitutionMapping(
            name="bad_bank",
            display_name="Bad Bank",
            file_pattern="*.csv",
            id_prefix="bad_",
            date_fmt=None,
            column_map={"Date": "date"},
            amount_sign=1,
            skip_rows=0,
            dedup_fields=("date",),
            extra_columns=(),
            date_columns=None,
            id_fields=("date",),
            currency="USD",
            default_category="Uncategorized",
            default_category_id="cat_uncategorized",
            encoding="utf-8",
            debit_column=None,
            credit_column=None,
        )
        with pytest.raises(ValueError, match="Missing required.*amount"):
            mapping.validate()

    def test_both_amount_and_split_columns_raises_value_error(self):
        mapping = InstitutionMapping(
            name="conflict",
            display_name="Conflict",
            file_pattern="*.csv",
            id_prefix="c_",
            date_fmt=None,
            column_map={
                "Date": "date",
                "Description": "merchant",
                "Amount": "amount",
            },
            amount_sign=1,
            skip_rows=0,
            dedup_fields=("date", "amount", "merchant"),
            extra_columns=(),
            date_columns=("date",),
            id_fields=("date", "amount", "merchant"),
            currency="USD",
            default_category="Uncategorized",
            default_category_id="cat_uncategorized",
            encoding="utf-8",
            debit_column="Debit",
            credit_column="Credit",
        )
        with pytest.raises(ValueError, match="Cannot specify both"):
            mapping.validate()
