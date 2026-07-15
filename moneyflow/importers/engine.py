"""Generic CSV import engine with pluggable institution mappings."""

import json
import re
from dataclasses import dataclass
from pathlib import Path

import polars as pl

from moneyflow.backends.csv_backend import STANDARD_FIELDS, CsvFinanceBackend, _stable_category_id


def _safe_str(val: object) -> str:
    """Convert a value to string, treating None as empty."""
    if val is None:
        return ""
    return str(val)


def _slugify(text: str) -> str:
    """Convert a string into a safe identifier fragment."""
    return re.sub(r"[^a-zA-Z0-9_.-]", "_", str(text))


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
        if self.debit_column is not None and self.credit_column is not None:
            if "amount" in mapped_values:
                raise ValueError(
                    "Cannot specify both 'amount' column and split debit/credit columns"
                )
        elif self.debit_column is not None or self.credit_column is not None:
            raise ValueError("Must specify both debit_column and credit_column, or neither")
        else:
            missing = required_standard - mapped_values
            if missing:
                raise ValueError(
                    f"Missing required column_map targets: {', '.join(sorted(missing))}"
                )

    def validate_csv_columns(self, csv_columns: set[str]) -> list[str]:
        """Validate CSV has required columns. Returns list of missing column issues."""
        issues: list[str] = []
        for csv_col, txn_field in self.column_map.items():
            if csv_col not in csv_columns:
                if txn_field in ("date", "amount", "merchant"):
                    issues.append(f"Required column '{csv_col}' not found in CSV")
        if self.debit_column and self.debit_column not in csv_columns:
            issues.append(f"Required debit column '{self.debit_column}' not found in CSV")
        if self.credit_column and self.credit_column not in csv_columns:
            issues.append(f"Required credit column '{self.credit_column}' not found in CSV")
        return issues


def import_csv(
    path: str,
    mapping: InstitutionMapping,
    backend: CsvFinanceBackend,
    *,
    force: bool = False,
) -> dict[str, int]:
    """Import CSV files matching the institution's file pattern into the backend."""
    mapping.validate()

    csv_files = sorted(Path(path).rglob(mapping.file_pattern))
    if not csv_files:
        raise FileNotFoundError(f"No files matching '{mapping.file_pattern}' found in {path}")

    imported_filenames: set[str] = set()
    if not force:
        history = backend.get_import_history()
        imported_filenames = {h["filename"] for h in history}

    new_files = [f for f in csv_files if f.name not in imported_filenames]
    files_to_read = csv_files if force else new_files

    dfs = []
    for csv_file in files_to_read:
        try:
            df = pl.read_csv(
                str(csv_file),
                infer_schema_length=0,
                encoding=mapping.encoding,
                truncate_ragged_lines=True,
                skip_rows=mapping.skip_rows,
            )
        except Exception as e:
            raise ValueError(f"Failed to read {csv_file}: {e}") from e

        column_issues = mapping.validate_csv_columns(set(df.columns))
        if column_issues:
            raise ValueError(
                f"Column validation failed for {csv_file.name}: {'; '.join(column_issues)}"
            )
        dfs.append(df)

    if not dfs:
        return {"imported": 0, "duplicates": 0, "skipped": 0}

    combined = pl.concat(dfs)

    # Handle split Debit/Credit columns
    if mapping.debit_column and mapping.credit_column:
        debit_col = mapping.debit_column
        credit_col = mapping.credit_column
        if debit_col in combined.columns and credit_col in combined.columns:
            debit = combined[debit_col].cast(pl.Float64, strict=False).fill_null(0)
            credit = combined[credit_col].cast(pl.Float64, strict=False).fill_null(0)
            combined = combined.with_columns((credit - debit).alias("amount"))

    # Rename columns
    combined = combined.rename(mapping.column_map, strict=False)

    # Parse dates
    date_fields = mapping.date_columns or ("date",)
    for date_col in date_fields:
        if date_col in combined.columns:
            if mapping.date_fmt:
                combined = combined.with_columns(
                    pl.col(date_col)
                    .str.to_date(mapping.date_fmt, strict=False)
                    .cast(pl.String)
                    .alias(date_col)
                )
            else:
                combined = combined.with_columns(
                    pl.col(date_col).str.to_date(strict=False).cast(pl.String).alias(date_col)
                )

    # Drop rows where date parsing failed
    for date_col in date_fields:
        if date_col in combined.columns:
            combined = combined.filter(pl.col(date_col).is_not_null())

    # Filter trailing garbage rows: drop rows where all core columns are null
    core_cols = [c for c in ("date", "amount", "merchant") if c in combined.columns]
    if core_cols:
        has_data = pl.lit(False)
        for col in core_cols:
            has_data = has_data | combined[col].is_not_null()
        combined = combined.filter(has_data)

    # Flip amount sign
    if "amount" in combined.columns:
        combined = combined.with_columns(
            (pl.col("amount").cast(pl.Float64, strict=False) * mapping.amount_sign).alias("amount")
        )

    # Collect existing IDs for dedup
    existing_ids: set[str] = set()
    if not force:
        conn = backend._get_connection()
        rows = conn.execute("SELECT id FROM transactions").fetchall()
        conn.close()
        existing_ids = {row[0] for row in rows}

    imported = 0
    duplicates = 0
    skipped = 0
    id_counts: dict[str, int] = {}
    insert_batch: list[tuple] = []

    rows_iter = combined.iter_rows(named=True)
    for row in rows_iter:
        # Validate required values
        date_val = _safe_str(row.get("date"))
        merchant_val = _safe_str(row.get("merchant"))
        amount_raw = row.get("amount")

        if not date_val or not merchant_val or amount_raw is None:
            skipped += 1
            continue

        amount_val = float(amount_raw)

        # Generate ID from field values
        id_parts = []
        for field in mapping.id_fields:
            val = _safe_str(row.get(field)).strip()
            id_parts.append(val)
        raw_id = mapping.id_prefix + "_".join(_slugify(p) for p in id_parts)

        # Handle duplicate IDs within batch
        seq = id_counts.get(raw_id, 0) + 1
        id_counts[raw_id] = seq
        txn_id = f"{raw_id}_{seq}"

        if not force and txn_id in existing_ids:
            duplicates += 1
            continue

        category_val = _safe_str(row.get("category")) or mapping.default_category
        category_id_val = _safe_str(row.get("category_id"))
        if not category_id_val or category_id_val == mapping.default_category_id:
            if category_val and category_val != mapping.default_category:
                category_id_val = _stable_category_id(category_val)
            else:
                category_id_val = mapping.default_category_id

        account_val = _safe_str(row.get("account"))
        notes_val = _safe_str(row.get("notes"))

        extras = {
            col: _safe_str(row.get(col))
            for col in mapping.extra_columns
            if col in row and col not in STANDARD_FIELDS
        }

        insert_batch.append(
            (
                txn_id,
                date_val,
                amount_val,
                merchant_val,
                category_val,
                category_id_val,
                account_val,
                notes_val,
                json.dumps(extras),
            )
        )
        imported += 1

    if insert_batch:
        conn = backend._get_connection()
        if force:
            conn.execute(
                "DELETE FROM transactions WHERE id IN ({})".format(
                    ",".join("?" for _ in insert_batch)
                ),
                [row[0] for row in insert_batch],
            )
        conn.executemany(
            """INSERT OR IGNORE INTO transactions
               (id, date, amount, merchant, category, category_id, account, notes, extras)
               VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)""",
            insert_batch,
        )
        conn.commit()
        conn.close()

    # Record import history per file
    if new_files:
        import_conn = backend._get_connection()
        for csv_file in new_files:
            import_conn.execute(
                "INSERT INTO import_history (filename, record_count, duplicate_count, "
                "skipped_count) VALUES (?, ?, ?, ?)",
                (csv_file.name, imported, duplicates, skipped),
            )
        import_conn.commit()
        import_conn.close()

    return {"imported": imported, "duplicates": duplicates, "skipped": skipped}
