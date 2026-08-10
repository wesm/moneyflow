"""Tests for CSV import engine and InstitutionMapping."""

import dataclasses
import hashlib
import json
from pathlib import Path

import pytest

from moneyflow.backends.csv_backend import CsvFinanceBackend
from moneyflow.importers import engine
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
            column_map={"Date": "date", "Description": "merchant", "Amount": "amount"},
            amount_sign=1,
            skip_rows=0,
            dedup_fields=("date", "amount", "merchant"),
            extra_columns=(),
            date_columns=("date",),
            currency="USD",
            default_category="Uncategorized",
            default_category_id="cat_uncategorized",
            encoding="utf-8",
            debit_column=None,
            credit_column=None,
        )
        mapping.validate()

    def test_account_label_defaults_to_empty(self):
        """account_label is opt-in; existing mappings keep current behavior."""
        mapping = InstitutionMapping(
            name="test_bank",
            display_name="Test Bank",
            file_pattern="test_*.csv",
            id_prefix="test_",
            date_fmt="%m/%d/%Y",
            column_map={"Date": "date", "Description": "merchant", "Amount": "amount"},
            amount_sign=1,
            skip_rows=0,
            dedup_fields=("date", "amount", "merchant"),
            extra_columns=(),
            date_columns=("date",),
            currency="USD",
            default_category="Uncategorized",
            default_category_id="cat_uncategorized",
            encoding="utf-8",
            debit_column=None,
            credit_column=None,
        )
        assert mapping.account_label == ""

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
            column_map={"Date": "date", "Description": "merchant", "Amount": "amount"},
            amount_sign=1,
            skip_rows=0,
            dedup_fields=("date", "amount", "merchant"),
            extra_columns=(),
            date_columns=("date",),
            currency="USD",
            default_category="Uncategorized",
            default_category_id="cat_uncategorized",
            encoding="utf-8",
            debit_column="Debit",
            credit_column="Credit",
        )
        with pytest.raises(ValueError, match="Cannot specify both"):
            mapping.validate()

    def test_split_mapping_requires_date_and_merchant(self):
        mapping = InstitutionMapping(
            name="split_bank",
            display_name="Split Bank",
            file_pattern="*.csv",
            id_prefix="split_",
            date_fmt=None,
            column_map={},
            amount_sign=1,
            skip_rows=0,
            dedup_fields=("date", "merchant"),
            extra_columns=(),
            date_columns=("date",),
            currency="USD",
            default_category="Uncategorized",
            default_category_id="cat_uncategorized",
            encoding="utf-8",
            debit_column="Debit",
            credit_column="Credit",
        )

        with pytest.raises(ValueError, match="date.*merchant"):
            mapping.validate()

    def test_validate_rejects_empty_dedup_fields(self):
        """A typo or missing dedup_fields entry that resolves to an empty
        list would silently produce empty dedup keys, collapsing every row
        into one. Catch that at validate time."""
        mapping = InstitutionMapping(
            name="empty_dedup",
            display_name="Empty Dedup",
            file_pattern="*.csv",
            id_prefix="e_",
            date_fmt=None,
            column_map={"Date": "date", "Description": "merchant", "Amount": "amount"},
            amount_sign=1,
            skip_rows=0,
            dedup_fields=(),
            extra_columns=(),
            date_columns=("date",),
            currency="USD",
            default_category="Uncategorized",
            default_category_id="cat_uncategorized",
            encoding="utf-8",
            debit_column=None,
            credit_column=None,
        )
        with pytest.raises(ValueError, match="dedup_fields"):
            mapping.validate()


@pytest.fixture
def test_mapping():
    return InstitutionMapping(
        name="test_bank",
        display_name="Test Bank",
        file_pattern="test_*.csv",
        id_prefix="test_",
        date_fmt="%m/%d/%Y",
        column_map={"Transaction Date": "date", "Description": "merchant", "Amount": "amount"},
        amount_sign=1,
        skip_rows=0,
        dedup_fields=("date", "amount", "merchant"),
        extra_columns=(),
        date_columns=("date",),
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
        "Transaction Date,Description,Amount\n1/15/2024,EXAMPLE GIFT SHOP,-12.34\n"
        "1/12/2024,EXAMPLE CAFE,-8.90\n"
    )
    return str(csv_dir)


@pytest.fixture
def test_backend(tmp_path):
    profile = tmp_path / "test_profile"
    profile.mkdir()
    config = tmp_path / "test_config"
    config.mkdir()
    return CsvFinanceBackend(
        profile_dir=profile, config_dir=str(config), institution_name="test_bank"
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

    def test_records_hash_for_exact_bytes_processed(
        self, test_csv_dir, test_mapping, test_backend, monkeypatch
    ):
        csv_file = Path(test_csv_dir) / "test_data.csv"
        original_contents = csv_file.read_bytes()
        original_process_file = engine._process_file

        def mutate_file_after_processing(*args, **kwargs):
            stats = original_process_file(*args, **kwargs)
            csv_file.write_text(
                "Transaction Date,Description,Amount\n1/16/2024,EXAMPLE CHANGED STORE,-99.99\n"
            )
            return stats

        monkeypatch.setattr(engine, "_process_file", mutate_file_after_processing)

        import_csv(test_csv_dir, test_mapping, test_backend)

        history = test_backend.get_import_history()
        assert history[0]["file_hash"] == hashlib.sha256(original_contents).hexdigest()

    def test_records_history_for_successful_file_before_later_file_fails(
        self, tmp_path, test_mapping, test_backend
    ):
        csv_dir = tmp_path / "csvs"
        csv_dir.mkdir()
        (csv_dir / "test_first.csv").write_text(
            "Transaction Date,Description,Amount\n1/15/2024,EXAMPLE FIRST STORE,-12.34\n"
        )
        (csv_dir / "test_second.csv").write_text(
            "Transaction Date,Description\n1/16/2024,EXAMPLE INVALID STORE\n"
        )

        with pytest.raises(ValueError, match="Column validation failed"):
            import_csv(str(csv_dir), test_mapping, test_backend)

        history = test_backend.get_import_history()
        assert len(history) == 1
        assert history[0]["record_count"] == 1

    def test_force_flag_reimports(self, test_csv_dir, test_mapping, test_backend):
        result1 = import_csv(test_csv_dir, test_mapping, test_backend)
        assert result1["imported"] == 2
        result2 = import_csv(test_csv_dir, test_mapping, test_backend)
        assert result2["imported"] == 0  # Already imported (same file size)
        result3 = import_csv(test_csv_dir, test_mapping, test_backend, force=True)
        assert result3["imported"] == 0  # Re-processed, but IDs already exist
        assert result3["duplicates"] == 2  # Counted accurately, not silently ignored
        conn = test_backend._get_connection()
        count = conn.execute("SELECT COUNT(*) FROM transactions").fetchone()[0]
        conn.close()
        assert count == 2  # INSERT OR IGNORE prevents duplicates

    def test_force_true_reports_actual_imports_after_content_change(
        self, tmp_path, test_mapping, test_backend
    ):
        """force=True re-processes a file, but counts only rows that are new."""
        csv_dir = tmp_path / "csvs_force"
        csv_dir.mkdir()
        csv_file = csv_dir / "test_data.csv"
        csv_file.write_text("Transaction Date,Description,Amount\n7/12/2026,First Coffee,-4.50\n")
        mapping = _copy_mapping(test_mapping, file_pattern="test_data.csv")

        result1 = import_csv(str(csv_dir), mapping, test_backend)
        assert result1["imported"] == 1

        # Append a new row (changes size, so it would be re-imported anyway)
        csv_file.write_text(
            "Transaction Date,Description,Amount\n"
            "7/12/2026,First Coffee,-4.50\n"
            "7/12/2026,Second Coffee,-5.00\n"
        )
        result2 = import_csv(str(csv_dir), mapping, test_backend, force=True)
        assert result2["imported"] == 1
        assert result2["duplicates"] == 1

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
        assert amount == -50.0

    def test_typo_in_dedup_field_raises(self, tmp_path, test_mapping, test_backend):
        """A typo in dedup_fields (e.g. 'datte' instead of 'date') would
        silently produce an empty key and collapse all rows. The engine
        must validate every field against the prepared dataframe."""
        csv_dir = tmp_path / "csvs_typo"
        csv_dir.mkdir()
        (csv_dir / "typo.csv").write_text(
            "Transaction Date,Description,Amount\n7/12/2026,Coffee,-4.50\n"
        )
        # 'datte' is a typo — not in the prepared dataframe.
        typo_mapping = _copy_mapping(
            test_mapping,
            file_pattern="typo.csv",
            dedup_fields=("datte", "amount", "merchant"),
        )
        with pytest.raises(ValueError, match="datte"):
            import_csv(str(csv_dir), typo_mapping, test_backend)

    def test_trailing_empty_rows_filtered_as_skipped(self, tmp_path, test_mapping, test_backend):
        csv_dir = tmp_path / "csvs_trail"
        csv_dir.mkdir()
        csv_file = csv_dir / "test_data.csv"
        csv_file.write_text(
            "Transaction Date,Description,Amount\n7/12/2026,Real,-50.00\n,,,\n,,,\n"
        )
        trail_mapping = _copy_mapping(test_mapping, file_pattern="test_data.csv")
        result = import_csv(str(csv_dir), trail_mapping, test_backend)
        assert result["imported"] == 1
        # 2 garbage rows after filtering = counted as skipped
        assert result["skipped"] == 2

    def test_malformed_dates_counted_as_skipped(self, tmp_path, test_mapping, test_backend):
        csv_dir = tmp_path / "csvs_bad_date"
        csv_dir.mkdir()
        csv_file = csv_dir / "test_data.csv"
        csv_file.write_text(
            "Transaction Date,Description,Amount\n"
            "7/12/2026,Real Transaction,-50.00\n"
            "NOT_A_DATE,Bad Row,-10.00\n"
        )
        bad_mapping = _copy_mapping(test_mapping, file_pattern="test_data.csv")
        result = import_csv(str(csv_dir), bad_mapping, test_backend)
        assert result["imported"] == 1
        # Malformed date row filtered out and counted
        assert result["skipped"] == 1

    def test_duplicate_ids_within_batch_get_suffixed(self, tmp_path, test_mapping, test_backend):
        csv_dir = tmp_path / "csvs_dup"
        csv_dir.mkdir()
        csv_file = csv_dir / "test_dup.csv"
        csv_file.write_text(
            "Transaction Date,Description,Amount\n7/12/2026,Coffee,-4.50\n7/12/2026,Coffee,-4.50\n"
        )
        dup_mapping = _copy_mapping(test_mapping, file_pattern="test_dup.csv")
        result = import_csv(str(csv_dir), dup_mapping, test_backend)
        assert result["imported"] == 2
        conn = test_backend._get_connection()
        ids = [r[0] for r in conn.execute("SELECT id FROM transactions ORDER BY id").fetchall()]
        conn.close()
        assert len(ids) == 2
        assert ids[0] != ids[1]

    def test_overlapping_files_deduplicated(self, tmp_path, test_mapping, test_backend):
        """Same transaction in two files should be deduplicated across files."""
        csv_dir = tmp_path / "csvs_overlap"
        csv_dir.mkdir()
        (csv_dir / "file_a.csv").write_text(
            "Transaction Date,Description,Amount\n7/12/2026,Shared TXN,-50.00\n"
        )
        (csv_dir / "file_b.csv").write_text(
            "Transaction Date,Description,Amount\n7/12/2026,Shared TXN,-50.00\n"
            "7/9/2026,Unique TXN,-19.35\n"
        )
        olap_mapping = _copy_mapping(test_mapping, file_pattern="file_*.csv")
        result = import_csv(str(csv_dir), olap_mapping, test_backend)
        # Shared TXN appears in both, dedup catches it — only 1 imported from file_b
        assert result["imported"] == 2  # Shared + Unique
        assert result["duplicates"] == 1  # second Shared from file_b

    def test_cross_file_dedup_counts_multiple_occurrences(
        self, tmp_path, test_mapping, test_backend
    ):
        """Later files with more occurrences of a key import only the extras."""
        csv_dir = tmp_path / "csvs_counts"
        csv_dir.mkdir()
        (csv_dir / "file_one.csv").write_text(
            "Transaction Date,Description,Amount\n7/12/2026,Same Coffee,-4.50\n"
        )
        (csv_dir / "file_two.csv").write_text(
            "Transaction Date,Description,Amount\n"
            "7/12/2026,Same Coffee,-4.50\n"
            "7/12/2026,Same Coffee,-4.50\n"
        )
        counts_mapping = _copy_mapping(test_mapping, file_pattern="file_*.csv")
        result = import_csv(str(csv_dir), counts_mapping, test_backend)
        # file_one imports 1; file_two has 2, but 1 was already imported, so 1 new
        assert result["imported"] == 2
        assert result["duplicates"] == 1
        conn = test_backend._get_connection()
        count = conn.execute("SELECT COUNT(*) FROM transactions").fetchone()[0]
        conn.close()
        assert count == 2

    def test_account_label_disambiguates_same_dedup_key(self, tmp_path, test_mapping):
        """Two mappings sharing a dedup key but with different account_label
        must not cross-dedup — each represents a distinct account."""
        profile = tmp_path / "shared_profile"
        profile.mkdir()
        config = tmp_path / "shared_config"
        config.mkdir()
        backend = CsvFinanceBackend(
            profile_dir=profile, config_dir=str(config), institution_name="test_bank"
        )
        csv_dir = tmp_path / "csvs_account"
        csv_dir.mkdir()
        csv_file = csv_dir / "card_export.csv"
        csv_file.write_text("Transaction Date,Description,Amount\n7/12/2026,Same Coffee,-4.50\n")
        mapping_a = _copy_mapping(
            test_mapping, file_pattern="card_export.csv", account_label="card_a"
        )
        mapping_b = _copy_mapping(
            test_mapping, file_pattern="card_export.csv", account_label="card_b"
        )
        result_a = import_csv(str(csv_dir), mapping_a, backend)
        # No force needed: import history is keyed by account label, so the
        # same file is re-processed under mapping_b. The dedup key and txn_id
        # both include account_label, so the row is treated as new.
        result_b = import_csv(str(csv_dir), mapping_b, backend)
        assert result_a["imported"] == 1
        assert result_b["imported"] == 1
        assert result_b["duplicates"] == 0
        conn = backend._get_connection()
        rows = conn.execute("SELECT id, amount, merchant FROM transactions ORDER BY id").fetchall()
        conn.close()
        assert len(rows) == 2
        assert rows[0][1:] == rows[1][1:]  # same amount, merchant
        assert rows[0][0] != rows[1][0]  # different IDs because account_label differs

    def test_dedup_persists_across_import_invocations(self, tmp_path, test_mapping, test_backend):
        """Overlapping data imported in a later invocation must be detected as
        duplicates: transaction IDs are derived from the dedup key, so the same
        key regenerates the same IDs already stored in the database."""
        csv_dir = tmp_path / "csvs_invocations"
        csv_dir.mkdir()
        (csv_dir / "test_january.csv").write_text(
            "Transaction Date,Description,Amount\n7/12/2026,Same Coffee,-4.50\n"
        )
        result1 = import_csv(str(csv_dir), test_mapping, test_backend)
        assert result1["imported"] == 1

        # A later export overlaps the first and adds a second occurrence of
        # the same dedup key. Only the extra occurrence is imported.
        (csv_dir / "test_february.csv").write_text(
            "Transaction Date,Description,Amount\n"
            "7/12/2026,Same Coffee,-4.50\n"
            "7/12/2026,Same Coffee,-4.50\n"
        )
        result2 = import_csv(str(csv_dir), test_mapping, test_backend)
        assert result2["imported"] == 1
        assert result2["duplicates"] == 1
        conn = test_backend._get_connection()
        count = conn.execute("SELECT COUNT(*) FROM transactions").fetchone()[0]
        conn.close()
        assert count == 2

    def test_import_history_records_account_and_mapping(
        self, test_csv_dir, test_mapping, test_backend
    ):
        mapping = dataclasses.replace(test_mapping, account_label="card_a")
        import_csv(test_csv_dir, mapping, test_backend)
        history = test_backend.get_import_history()
        assert len(history) == 1
        assert history[0]["account"] == "card_a"
        assert history[0]["mapping_name"] == "test_bank"

    def test_same_account_reimport_still_skipped(self, test_csv_dir, test_mapping, test_backend):
        mapping = dataclasses.replace(test_mapping, account_label="card_a")
        assert import_csv(test_csv_dir, mapping, test_backend)["imported"] == 2
        result = import_csv(test_csv_dir, mapping, test_backend)
        assert result["imported"] == 0
        assert result["duplicates"] == 0  # file skipped entirely, not re-processed
        assert len(test_backend.get_import_history()) == 1

    def test_account_label_persisted_to_transactions(
        self, test_csv_dir, test_mapping, test_backend
    ):
        """--account imports must label the stored transactions, not only
        namespace their IDs — otherwise multi-card data is indistinguishable."""
        mapping = dataclasses.replace(test_mapping, account_label="card_a")
        import_csv(test_csv_dir, mapping, test_backend)
        conn = test_backend._get_connection()
        accounts = {row[0] for row in conn.execute("SELECT account FROM transactions").fetchall()}
        conn.close()
        assert accounts == {"card_a"}

    def test_single_file_path_imports_only_that_file(self, tmp_path, test_mapping, test_backend):
        """Passing a file path imports exactly that file, so a multi-card
        directory can be imported one card at a time with --account."""
        csv_dir = tmp_path / "csvs_single"
        csv_dir.mkdir()
        (csv_dir / "test_card_a.csv").write_text(
            "Transaction Date,Description,Amount\n7/12/2026,CARD A ONLY,-4.50\n"
        )
        (csv_dir / "test_card_b.csv").write_text(
            "Transaction Date,Description,Amount\n7/12/2026,CARD B ONLY,-9.00\n"
        )
        result = import_csv(str(csv_dir / "test_card_a.csv"), test_mapping, test_backend)
        assert result["imported"] == 1
        conn = test_backend._get_connection()
        merchants = [r[0] for r in conn.execute("SELECT merchant FROM transactions").fetchall()]
        conn.close()
        assert merchants == ["CARD A ONLY"]

    def test_deleted_transaction_not_resurrected_by_reimport(
        self, tmp_path, test_mapping, test_backend
    ):
        """A transaction the user deleted must stay deleted when its source
        CSV is re-processed — deletion writes a tombstone that the import
        treats as an existing ID."""
        import asyncio

        csv_dir = tmp_path / "csvs_tombstone"
        csv_dir.mkdir()
        csv_file = csv_dir / "test_data.csv"
        csv_file.write_text(
            "Transaction Date,Description,Amount\n"
            "7/12/2026,Keep Me,-4.50\n"
            "7/13/2026,Delete Me,-9.00\n"
        )
        assert import_csv(str(csv_dir), test_mapping, test_backend)["imported"] == 2

        conn = test_backend._get_connection()
        doomed_id = conn.execute(
            "SELECT id FROM transactions WHERE merchant = 'Delete Me'"
        ).fetchone()[0]
        conn.close()
        assert asyncio.run(test_backend.delete_transaction(doomed_id)) is True

        # Re-process the same file (force bypasses the file-hash skip) and a
        # grown version of it — the deleted row must not come back either way.
        result = import_csv(str(csv_dir), test_mapping, test_backend, force=True)
        assert result["imported"] == 0

        csv_file.write_text(
            "Transaction Date,Description,Amount\n"
            "7/12/2026,Keep Me,-4.50\n"
            "7/13/2026,Delete Me,-9.00\n"
            "7/14/2026,New Row,-1.00\n"
        )
        result = import_csv(str(csv_dir), test_mapping, test_backend)
        assert result["imported"] == 1

        conn = test_backend._get_connection()
        merchants = {r[0] for r in conn.execute("SELECT merchant FROM transactions").fetchall()}
        conn.close()
        assert merchants == {"Keep Me", "New Row"}

    def test_category_alias_prevents_resurrection_on_reimport(
        self, tmp_path, test_mapping, test_backend
    ):
        """After a structural rename (recorded as an alias), a later import of
        the bank-provided category name must map to the renamed category
        instead of recreating the old one."""
        csv_dir = tmp_path / "csvs_alias"
        csv_dir.mkdir()
        (csv_dir / "test_data.csv").write_text(
            "Transaction Date,Description,Amount,Category\n7/12/2026,Store,-4.50,Shopping\n"
        )
        mapping = _copy_mapping(
            test_mapping,
            column_map={
                "Transaction Date": "date",
                "Description": "merchant",
                "Amount": "amount",
                "Category": "category",
            },
        )
        test_backend.record_category_alias("cat_shopping", "cat_fun_purchases", "Fun Purchases")

        assert import_csv(str(csv_dir), mapping, test_backend)["imported"] == 1
        conn = test_backend._get_connection()
        row = conn.execute("SELECT category, category_id FROM transactions").fetchone()
        conn.close()
        assert row == ("Fun Purchases", "cat_fun_purchases")

    def test_garbage_category_name_folds_into_default(self, tmp_path, test_mapping, test_backend):
        """A category name that normalizes to nothing (e.g. "!!!") must
        become the default category, not squat on the fallback id with its
        own display name."""
        csv_dir = tmp_path / "csvs_garbage_cat"
        csv_dir.mkdir()
        (csv_dir / "test_data.csv").write_text(
            "Transaction Date,Description,Amount,Category\n7/12/2026,Coffee,-4.50,!!!\n"
        )
        mapping = _copy_mapping(
            test_mapping,
            column_map={
                "Transaction Date": "date",
                "Description": "merchant",
                "Amount": "amount",
                "Category": "category",
            },
        )
        assert import_csv(str(csv_dir), mapping, test_backend)["imported"] == 1
        conn = test_backend._get_connection()
        row = conn.execute("SELECT category, category_id FROM transactions").fetchone()
        conn.close()
        assert row == ("Uncategorized", "cat_uncategorized")

    def test_non_finite_amounts_skipped(self, tmp_path, test_mapping, test_backend):
        """NaN and infinity must be skipped: SQLite stores NaN as NULL (which
        would violate NOT NULL and roll back the whole file) and infinity
        would corrupt totals."""
        csv_dir = tmp_path / "csvs_nonfinite"
        csv_dir.mkdir()
        (csv_dir / "test_amounts.csv").write_text(
            "Transaction Date,Description,Amount\n"
            "7/12/2026,Not A Number,NaN\n"
            "7/13/2026,Positive Infinity,inf\n"
            "7/14/2026,Negative Infinity,-inf\n"
            "7/15/2026,Valid,-4.50\n"
        )
        result = import_csv(str(csv_dir), test_mapping, test_backend)
        assert result["imported"] == 1
        assert result["skipped"] == 3
        conn = test_backend._get_connection()
        rows = conn.execute("SELECT merchant, amount FROM transactions").fetchall()
        conn.close()
        assert rows == [("Valid", -4.5)]

    def test_split_columns_blank_and_malformed_amounts_skipped(
        self, tmp_path, test_mapping, test_backend
    ):
        """Rows whose debit/credit values are both blank or contain unparsable
        text must be skipped, not imported as zero-dollar transactions."""
        csv_dir = tmp_path / "csvs_split"
        csv_dir.mkdir()
        (csv_dir / "test_split.csv").write_text(
            "Transaction Date,Description,Debit,Credit\n"
            "7/12/2026,Valid Debit,4.50,\n"
            "7/13/2026,Valid Credit,,5.00\n"
            "7/14/2026,Both Blank,,\n"
            "7/15/2026,Bad Debit,abc,\n"
            "7/16/2026,Bad Debit Valid Credit,abc,5.00\n"
        )
        split_mapping = _copy_mapping(
            test_mapping,
            file_pattern="test_split.csv",
            column_map={"Transaction Date": "date", "Description": "merchant"},
            debit_column="Debit",
            credit_column="Credit",
        )
        result = import_csv(str(csv_dir), split_mapping, test_backend)
        assert result["imported"] == 2
        assert result["skipped"] == 3
        conn = test_backend._get_connection()
        rows = conn.execute("SELECT merchant, amount FROM transactions ORDER BY amount").fetchall()
        conn.close()
        assert rows == [("Valid Debit", -4.5), ("Valid Credit", 5.0)]


class TestChaseCreditIntegration:
    def test_import_chase_csv(self, tmp_path):
        from moneyflow.importers.mappings.registry import INSTITUTION_MAPPINGS

        mapping = INSTITUTION_MAPPINGS["chase_credit"]
        profile = tmp_path / "chase_profile"
        profile.mkdir()
        config = tmp_path / "chase_config"
        config.mkdir()
        backend = CsvFinanceBackend(
            profile_dir=profile, config_dir=str(config), institution_name="chase_credit"
        )

        sample = Path(__file__).parent / "data" / "chase_sample.csv"
        csv_dir = tmp_path / "csvs"
        csv_dir.mkdir()
        import shutil

        shutil.copy(sample, csv_dir / "Chase1234_Activity.csv")

        result = import_csv(str(csv_dir), mapping, backend)
        assert result["imported"] == 5
        assert result["duplicates"] == 0
        assert result["skipped"] == 0

        conn = backend._get_connection()
        row = conn.execute(
            "SELECT id, date, amount, merchant, category, notes, extras "
            "FROM transactions WHERE amount = -25.0"
        ).fetchone()
        conn.close()
        assert row is not None
        assert row[1] == "2024-01-15"
        assert row[3] == "EXAMPLE MERCHANT 1"
        assert row[4] == "Shopping"
        assert row[5] == "Test memo one"
        extras = json.loads(row[6])
        assert extras["type"] == "Sale"

    def test_reimport_with_changed_file_size(self, tmp_path):
        """A file with the same name but different content should be re-imported."""
        from moneyflow.importers.mappings.registry import INSTITUTION_MAPPINGS

        mapping = INSTITUTION_MAPPINGS["chase_credit"]
        profile = tmp_path / "chase_profile"
        profile.mkdir()
        config = tmp_path / "chase_config"
        config.mkdir()
        backend = CsvFinanceBackend(
            profile_dir=profile, config_dir=str(config), institution_name="chase_credit"
        )

        csv_dir = tmp_path / "csvs"
        csv_dir.mkdir()

        # First import with 2 rows
        csv_file = csv_dir / "Chase_export.csv"
        csv_file.write_text(
            "Transaction Date,Post Date,Description,Category,Type,Amount,Memo\n"
            "01/15/2024,01/16/2024,MERCHANT A,Shopping,Sale,-25.00,Note\n"
            "01/12/2024,01/13/2024,MERCHANT B,Food,Sale,-10.00,\n"
        )
        result1 = import_csv(str(csv_dir), mapping, backend)
        assert result1["imported"] == 2

        # Same file, same size — skipped
        result2 = import_csv(str(csv_dir), mapping, backend)
        assert result2["imported"] == 0

        # Change content (different size) — re-imports
        csv_file.write_text(
            "Transaction Date,Post Date,Description,Category,Type,Amount,Memo\n"
            "01/15/2024,01/16/2024,MERCHANT A,Shopping,Sale,-25.00,Note\n"
            "01/12/2024,01/13/2024,MERCHANT B,Food,Sale,-10.00,\n"
            "01/10/2024,01/11/2024,MERCHANT C,Groceries,Sale,-5.00,\n"
        )
        result3 = import_csv(str(csv_dir), mapping, backend)
        assert result3["imported"] == 1  # New row only (others already in DB)

    def test_revert_to_original_size_not_reimported(self, tmp_path):
        """The most recent import history size wins, not the oldest."""
        from moneyflow.importers.mappings.registry import INSTITUTION_MAPPINGS

        mapping = INSTITUTION_MAPPINGS["chase_credit"]
        profile = tmp_path / "chase_profile"
        profile.mkdir()
        config = tmp_path / "chase_config"
        config.mkdir()
        backend = CsvFinanceBackend(
            profile_dir=profile, config_dir=str(config), institution_name="chase_credit"
        )

        csv_dir = tmp_path / "csvs"
        csv_dir.mkdir()
        csv_file = csv_dir / "Chase_export.csv"

        content_v1 = (
            "Transaction Date,Post Date,Description,Category,Type,Amount,Memo\n"
            "01/15/2024,01/16/2024,MERCHANT A,Shopping,Sale,-25.00,Note\n"
        )
        content_v2 = (
            "Transaction Date,Post Date,Description,Category,Type,Amount,Memo\n"
            "01/15/2024,01/16/2024,MERCHANT A,Shopping,Sale,-25.00,Note\n"
            "01/12/2024,01/13/2024,MERCHANT B,Food,Sale,-10.00,\n"
        )

        csv_file.write_text(content_v1)
        assert import_csv(str(csv_dir), mapping, backend)["imported"] == 1

        csv_file.write_text(content_v2)
        assert import_csv(str(csv_dir), mapping, backend)["imported"] == 1

        # Reverting to the original size should not be reprocessed, because the
        # newest history entry (content_v2 size) is retained.
        csv_file.write_text(content_v1)
        result = import_csv(str(csv_dir), mapping, backend)
        assert result["imported"] == 0

    def test_same_size_content_change_is_reimported(self, tmp_path):
        """Content changes are detected even when byte size stays the same."""
        from moneyflow.importers.mappings.registry import INSTITUTION_MAPPINGS

        mapping = INSTITUTION_MAPPINGS["chase_credit"]
        profile = tmp_path / "chase_profile"
        profile.mkdir()
        config = tmp_path / "chase_config"
        config.mkdir()
        backend = CsvFinanceBackend(
            profile_dir=profile, config_dir=str(config), institution_name="chase_credit"
        )

        csv_dir = tmp_path / "csvs"
        csv_dir.mkdir()
        csv_file = csv_dir / "Chase_export.csv"

        csv_file.write_text(
            "Transaction Date,Post Date,Description,Category,Type,Amount,Memo\n"
            "01/15/2024,01/16/2024,MERCHANT A,Shopping,Sale,-25.00,Note\n"
        )
        result1 = import_csv(str(csv_dir), mapping, backend)
        assert result1["imported"] == 1

        # Same byte size, different merchant. Hash must detect the change and
        # the new row (different ID) is imported as a new transaction.
        csv_file.write_text(
            "Transaction Date,Post Date,Description,Category,Type,Amount,Memo\n"
            "01/15/2024,01/16/2024,MERCHANT B,Shopping,Sale,-25.00,Note\n"
        )
        result2 = import_csv(str(csv_dir), mapping, backend)
        assert result2["imported"] == 1
