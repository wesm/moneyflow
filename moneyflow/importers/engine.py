"""Generic CSV import engine with pluggable institution mappings."""

import hashlib
import io
import json
import sqlite3
from dataclasses import dataclass
from pathlib import Path

import polars as pl

from moneyflow.backends.csv_backend import STANDARD_FIELDS, CsvFinanceBackend, _stable_category_id


def _safe_str(val: object) -> str:
    """Convert a value to string, treating None as empty."""
    if val is None:
        return ""
    return str(val)


def _hash_id(prefix: str, fields: list[str]) -> str:
    """Generate a collision-resistant transaction ID from field values."""
    joined = "\x00".join(_safe_str(f) for f in fields)
    digest = hashlib.sha256(joined.encode("utf-8", errors="replace")).hexdigest()[:16]
    return f"{prefix}{digest}"


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


def _prepare_dataframe(df: pl.DataFrame, mapping: InstitutionMapping) -> pl.DataFrame:
    """Apply column rename, date parsing, garbage filtering, and sign flip."""
    # Handle split Debit/Credit columns
    if mapping.debit_column and mapping.credit_column:
        debit_col = mapping.debit_column
        credit_col = mapping.credit_column
        if debit_col in df.columns and credit_col in df.columns:
            debit = df[debit_col].cast(pl.Float64, strict=False).fill_null(0)
            credit = df[credit_col].cast(pl.Float64, strict=False).fill_null(0)
            df = df.with_columns((credit - debit).alias("amount"))

    # Rename columns
    df = df.rename(mapping.column_map, strict=False)

    # Parse dates
    date_fields = mapping.date_columns or ("date",)
    for date_col in date_fields:
        if date_col in df.columns:
            if mapping.date_fmt:
                df = df.with_columns(
                    pl.col(date_col)
                    .str.to_date(mapping.date_fmt, strict=False)
                    .cast(pl.String)
                    .alias(date_col)
                )
            else:
                df = df.with_columns(
                    pl.col(date_col).str.to_date(strict=False).cast(pl.String).alias(date_col)
                )

    # Drop malformed-date rows (null after parse)
    for date_col in date_fields:
        if date_col in df.columns:
            df = df.filter(pl.col(date_col).is_not_null())

    # Filter trailing garbage rows
    core_cols = [c for c in ("date", "amount", "merchant") if c in df.columns]
    if core_cols:
        has_data = pl.lit(False)
        for col in core_cols:
            has_data = has_data | df[col].is_not_null()
        df = df.filter(has_data)

    # Flip amount sign
    if "amount" in df.columns:
        df = df.with_columns(
            (pl.col("amount").cast(pl.Float64, strict=False) * mapping.amount_sign).alias("amount")
        )

    return df


def _process_file(
    csv_contents: bytes,
    filename: str,
    mapping: InstitutionMapping,
    connection: sqlite3.Connection,
    existing_ids: set[str],
    seen_dedup_counts: dict[str, int],
) -> dict[str, int]:
    """Process a single CSV file, returning {imported, duplicates, skipped}."""
    df = pl.read_csv(
        io.BytesIO(csv_contents),
        infer_schema_length=0,
        encoding=mapping.encoding,
        truncate_ragged_lines=True,
        skip_rows=mapping.skip_rows,
    )

    column_issues = mapping.validate_csv_columns(set(df.columns))
    if column_issues:
        raise ValueError(
            f"Column validation failed for {filename}: {'; '.join(column_issues)}"
        )

    raw_row_count = df.height
    df = _prepare_dataframe(df, mapping)
    post_filter_count = df.height

    imported = 0
    duplicates = 0
    skipped = raw_row_count - post_filter_count
    dedup_counts: dict[str, int] = {}
    insert_batch: list[tuple] = []

    for row in df.iter_rows(named=True):
        date_val = _safe_str(row.get("date"))
        merchant_val = _safe_str(row.get("merchant"))
        amount_raw = row.get("amount")

        if not date_val or not merchant_val or amount_raw is None:
            skipped += 1
            continue

        amount_val = float(amount_raw)

        # Build dedup key from dedup_fields
        dedup_parts = [_safe_str(row.get(f)).strip() for f in mapping.dedup_fields]
        dedup_key = "\x00".join(dedup_parts)

        # In-file sequence number for this key (counts every row, including
        # cross-file duplicates, so that suffixes are stable across files).
        seq = dedup_counts.get(dedup_key, 0) + 1
        dedup_counts[dedup_key] = seq

        # Cross-file dedup: skip only occurrences already represented by earlier
        # files. If earlier files accepted N occurrences of this key, the first
        # N occurrences in this file are duplicates; any additional occurrences
        # are legitimate new transactions.
        prior_count = seen_dedup_counts.get(dedup_key, 0)
        if seq <= prior_count:
            duplicates += 1
            continue

        # Generate hash-based ID from id_fields
        id_parts = [_safe_str(row.get(f)).strip() for f in mapping.id_fields]
        id_suffix = f"_{seq}" if seq > 1 else ""
        txn_id = _hash_id(mapping.id_prefix, id_parts) + id_suffix

        if txn_id in existing_ids:
            duplicates += 1
            continue

        existing_ids.add(txn_id)

        category_val = _safe_str(row.get("category")) or mapping.default_category
        category_id_val = _safe_str(row.get("category_id"))
        if not category_id_val or category_id_val == mapping.default_category_id:
            if category_val and category_val != mapping.default_category:
                category_id_val = _stable_category_id(category_val)
            else:
                category_id_val = mapping.default_category_id

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
                _safe_str(row.get("account")),
                _safe_str(row.get("notes")),
                json.dumps(extras),
            )
        )
        imported += 1

    # After processing all rows in this file, update the global count for each
    # key to the maximum number of occurrences seen so far.
    for key, count in dedup_counts.items():
        seen_dedup_counts[key] = max(seen_dedup_counts.get(key, 0), count)

    if insert_batch:
        connection.executemany(
            """INSERT OR IGNORE INTO transactions
               (id, date, amount, merchant, category, category_id, account, notes, extras)
               VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)""",
            insert_batch,
        )

    return {"imported": imported, "duplicates": duplicates, "skipped": skipped}


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

    # Build lookup of already-imported files by (resolved_path, file_hash).
    # get_import_history returns newest-first, so setdefault preserves the
    # most recent hash for each filename rather than leaving the oldest entry.
    imported_snapshots: dict[str, str] = {}
    if not force:
        history = backend.get_import_history()
        for h in history:
            fname = h.get("filename", "")
            fhash = h.get("file_hash", "")
            if fname and fhash:
                imported_snapshots.setdefault(fname, fhash)

    # Load existing IDs once so duplicate detection is accurate even when
    # force=True re-processes files that were previously imported.
    conn = backend._get_connection()
    rows = conn.execute("SELECT id FROM transactions").fetchall()
    conn.close()
    existing_ids: set[str] = {row[0] for row in rows}

    seen_dedup_counts: dict[str, int] = {}
    total_imported = 0
    total_duplicates = 0
    total_skipped = 0
    for csv_file in csv_files:
        resolved = str(csv_file.resolve())
        csv_contents = csv_file.read_bytes()
        file_size = len(csv_contents)
        file_hash = hashlib.sha256(csv_contents).hexdigest()

        # Skip files already imported with the same content hash
        if resolved in imported_snapshots and imported_snapshots[resolved] == file_hash:
            continue

        import_conn = backend._get_connection()
        try:
            stats = _process_file(
                csv_contents,
                csv_file.name,
                mapping,
                import_conn,
                existing_ids,
                seen_dedup_counts,
            )
            import_conn.execute(
                "INSERT INTO import_history "
                "(filename, file_size, file_hash, record_count, duplicate_count, skipped_count) "
                "VALUES (?, ?, ?, ?, ?, ?)",
                (
                    resolved,
                    file_size,
                    file_hash,
                    stats["imported"],
                    stats["duplicates"],
                    stats["skipped"],
                ),
            )
            import_conn.commit()
        except Exception as e:
            import_conn.rollback()
            raise ValueError(f"Failed to process {csv_file}: {e}") from e
        finally:
            import_conn.close()

        total_imported += stats["imported"]
        total_duplicates += stats["duplicates"]
        total_skipped += stats["skipped"]

    return {"imported": total_imported, "duplicates": total_duplicates, "skipped": total_skipped}
