"""Tests for CSV import engine and InstitutionMapping."""
import dataclasses
import json
from pathlib import Path

import pytest

from moneyflow.backends.csv_backend import CsvFinanceBackend
from moneyflow.importers.engine import InstitutionMapping, import_csv


def _copy_mapping(mapping: InstitutionMapping, **overrides) -> InstitutionMapping:
    """Create a copy of an InstitutionMapping with field overrides."""
    fields = {f.name: getattr(mapping, f.name) for f in dataclasses.fields(mapping)}
    fields.update(overrides)
    return InstitutionMapping(**fields)


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


@pytest.fixture
def test_mapping():
    return InstitutionMapping(
        name="test_bank",
        display_name="Test Bank",
        file_pattern="test_*.csv",
        id_prefix="test_",
        date_fmt="%m/%d/%Y",
        column_map={
            "Transaction Date": "date",
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


@pytest.fixture
def test_csv_dir(tmp_path):
    csv_dir = tmp_path / "csvs"
    csv_dir.mkdir()
    csv_file = csv_dir / "test_data.csv"
    csv_file.write_text(
        "Transaction Date,Description,Amount\n"
        "7/12/2026,EXAMPLE GIFT SHOP,-50.00\n"
        "7/9/2026,EXAMPLE CAFE,-19.35\n"
    )
    return str(csv_dir)


@pytest.fixture
def test_backend(tmp_path):
    profile = tmp_path / "test_profile"
    profile.mkdir()
    config = tmp_path / "test_config"
    config.mkdir()
    return CsvFinanceBackend(
        profile_dir=profile,
        config_dir=str(config),
        institution_name="test_bank",
    )


class TestImportCsv:
    def test_imports_csv_into_backend(self, test_csv_dir, test_mapping, test_backend):
        result = import_csv(test_csv_dir, test_mapping, test_backend)
        assert result["imported"] == 2
        assert result["duplicates"] == 0
        assert result["skipped"] == 0

        conn = test_backend._get_connection()
        count = conn.execute("SELECT COUNT(*) FROM transactions").fetchone()[0]
        conn.close()
        assert count == 2

    def test_force_flag_reimports(self, test_csv_dir, test_mapping, test_backend):
        result1 = import_csv(test_csv_dir, test_mapping, test_backend)
        assert result1["imported"] == 2

        result2 = import_csv(test_csv_dir, test_mapping, test_backend)
        assert result2["imported"] == 0  # Already imported files skipped

        result3 = import_csv(test_csv_dir, test_mapping, test_backend, force=True)
        assert result3["imported"] == 2  # Force re-reads, same IDs hit INSERT OR IGNORE

        conn = test_backend._get_connection()
        count = conn.execute("SELECT COUNT(*) FROM transactions").fetchone()[0]
        conn.close()
        assert count == 2  # INSERT OR IGNORE prevents true duplicates

    def test_amount_sign_flipping(self, tmp_path, test_mapping, test_backend):
        csv_dir = tmp_path / "csvs2"
        csv_dir.mkdir()
        csv_file = csv_dir / "test_data.csv"
        csv_file.write_text("Transaction Date,Description,Amount\n7/12/2026,Buy Stuff,50.00\n")

        signed_mapping = _copy_mapping(test_mapping, file_pattern="test_data.csv", amount_sign=-1)

        result = import_csv(str(csv_dir), signed_mapping, test_backend)
        assert result["imported"] == 1

        conn = test_backend._get_connection()
        amount = conn.execute("SELECT amount FROM transactions LIMIT 1").fetchone()[0]
        conn.close()
        assert amount == -50.0  # Flipped from +50.0

    def test_trailing_empty_rows_filtered(self, tmp_path, test_mapping, test_backend):
        csv_dir = tmp_path / "csvs_trail"
        csv_dir.mkdir()
        csv_file = csv_dir / "test_data.csv"
        csv_file.write_text(
            "Transaction Date,Description,Amount\n"
            "7/12/2026,Real Transaction,-50.00\n"
            ",,,\n"
            ",,,\n"
        )

        trail_mapping = _copy_mapping(test_mapping, file_pattern="test_data.csv")

        result = import_csv(str(csv_dir), trail_mapping, test_backend)
        assert result["imported"] == 1

    def test_duplicate_ids_within_batch_get_suffixed(self, tmp_path, test_mapping, test_backend):
        csv_dir = tmp_path / "csvs_dup"
        csv_dir.mkdir()
        csv_file = csv_dir / "test_dup.csv"
        csv_file.write_text(
            "Transaction Date,Description,Amount\n"
            "7/12/2026,Coffee,-4.50\n"
            "7/12/2026,Coffee,-4.50\n"
        )

        dup_mapping = _copy_mapping(test_mapping, file_pattern="test_dup.csv",
                                     id_fields=("date", "amount", "merchant"))

        result = import_csv(str(csv_dir), dup_mapping, test_backend)
        assert result["imported"] == 2

        conn = test_backend._get_connection()
        ids = [row[0] for row in conn.execute("SELECT id FROM transactions ORDER BY id").fetchall()]
        conn.close()
        assert len(ids) == 2
        assert ids[0] != ids[1]
