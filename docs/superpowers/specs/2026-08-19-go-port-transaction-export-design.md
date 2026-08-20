# Go Port Transaction Export Design

**Date:** 2026-08-19
**Status:** Approved
**Branch:** `go-port`

## Summary

This slice ports Python Moneyflow's `E` transaction-export workflow to the Go TUI and web
application. It exports either the complete committed transaction set or the committed rows that
match the current analytical query. CSV, SQLite, and Parquet share one lossless v2 schema and one
renderer-neutral committed-snapshot projection.

Export is deliberately read-only. It does not replay the active journal, contact a provider,
acquire a provider-operation lease, advance the profile revision, or alter pending work. The TUI
publishes an owner-only file beneath the selected profile. The web application returns a protected
browser download without retaining a durable server-side copy.

The exported money representation is exact. Every row carries a formatted decimal amount, signed
integer minor units, currency, and scale. No export path uses `float32` or `float64` for money.

## Relationship to Earlier Slices

This design extends:

- `2026-08-12-go-port-foundation-read-only-tui-design.md`
- `2026-08-13-go-port-read-only-web-design.md`
- `2026-08-14-go-port-sqlite-editing-design.md`
- `2026-08-15-go-port-monarch-read-refresh-design.md`
- `2026-08-17-go-port-profile-catalog-onboarding-design.md`
- `2026-08-18-go-port-monarch-write-back-design.md`
- `2026-08-18-go-port-transaction-deletion-duplicates-design.md`
- `2026-08-18-go-tui-chrome-review-info-design.md`

Those contracts remain authoritative unless this document explicitly refines them. In
particular:

- SQLite remains the source of truth.
- SQL rows and driver types remain inside `internal/store`.
- Analytics remains ordinary Go over detached domain slices.
- All money uses signed integer minor units plus currency and scale.
- The browser API remains stateless and profile-scoped.
- Financial values, labels, search text, and file contents remain outside logs.
- The Go v2 profile schema remains install-only.

This slice does not change the installed profile schema and therefore does not increment
`CurrentSchemaVersion`.

## Goals

- Make the existing `E` / `transactions.export` action functional in TUI and web.
- Match Python's full-versus-filtered committed export semantics.
- Export CSV, SQLite, and Parquet from one fixed lossless schema.
- Use the same committed projection and format writers for both renderers.
- Preserve Python's export defaults: Parquet and Full dataset.
- Keep TUI output private, atomic, non-overwriting, and crash-safe.
- Provide a protected browser download with no durable server-side artifact.
- Remain responsive for profiles with 100,000 transactions.

## Non-Goals

- Exporting effective state with active journal operations applied.
- Exporting inactive redo operations, review history, write-batch internals, provider credentials,
  sessions, profile manifests, or database files as backups.
- Importing a Go export into Moneyflow.
- Replacing profile backup, restore, transfer, or Python-state migration.
- User-defined columns, delimiter selection, compression selection, or arbitrary output paths.
- Streaming rows directly from SQLite or holding a SQLite read transaction while serializing.
- Adding YNAB, SimpleFIN, Amazon, MCP, split-transaction, or notes-editing support.
- Removing Python, adding the Python shim, merging `go-port`, or pushing the branch.

## Python Parity and Named Clarifications

Python binds `E` to export and offers Parquet, CSV, and SQLite with Full dataset and Filtered
transactions scopes. Parquet and Full dataset are the defaults. Go preserves those choices.

Python exports `data_manager.df` or `state.get_filtered_df()`. Pending transaction edits are queued
separately and enter those frames only after an all-success commit. Go therefore exports committed
state as well. The `pending` column in Python is provider pending status, not a local edit marker.
Go imports posted provider transactions only, so the lossless v2 schema omits that constant-false
column.

Go's visible analytical state replays the active journal, while its export is committed state.
Consequently, a filtered export can differ from the current table when pending merchant, category,
hide, or delete operations change membership. Both renderers explicitly report the number of
excluded active operations. They suggest `w` only when commit is currently available.

CSV begins with `#` metadata lines, matching Python. This is a deliberate nonstandard-CSV
compatibility choice. Consumers such as Polars must use a comment prefix when reading it.

Go uses a lossless v2 row schema instead of preserving Python's floating-point amount column.
This is a deliberate export-format improvement required by the standing exact-money contract.

## Architecture and Dependencies

```text
internal/tui -------------------+
                                +--> internal/app --------> internal/analytics
internal/api <- web frontend ---+         |                       |
                                |         v                       v
                                |    internal/domain <------------+
                                |
                                +--> internal/exporter --> internal/home
                                             |
                                             +--> parquet-go
                                             +--> modernc SQLite

internal/store/sqlite --> internal/store + internal/domain
```

`internal/app` owns the committed export projection. It may use the cached profile snapshot and
existing analytics predicates, but it never imports `internal/exporter`.

`internal/exporter` accepts one detached application export document. It imports no store,
provider, API, TUI, or web packages. It owns format encoding, private staging, export locking,
filename allocation, persistent publication, and disposable-download cleanup.

The renderers own presentation and delivery. They never build rows, format money, sanitize CSV,
open SQLite, or call Parquet APIs directly.

`github.com/parquet-go/parquet-go` is the only new runtime dependency. It is a maintained pure-Go
reader/writer and preserves the no-CGO build contract. The implementation plan pins the current
stable version rather than using an unbounded upgrade. `pyarrow` is a test-only Python dependency
for independent Parquet metadata and row interoperability checks.

## Application Export Contract

### Requests and previews

The application layer defines:

```text
ExportScope = full | filtered

ExportPreviewRequest {
    scope
    analytical view state
}

ExportPreview {
    source revision
    transaction count estimate
    active pending operation count
    inactive redo operation count
    commit currently available
}

ExportRequest {
    scope
    analytical view state
}
```

Preview is bounded and contains counts only. The web preview endpoint accepts the current canonical
analytical query and returns both Full and Filtered counts so the chooser need not make a second
request when the scope changes. The TUI calls the same application operation directly.

An action-level preview count of zero produces `No data to export` without opening the chooser,
matching Python. `export_empty` remains an execution-time backstop because another process can
change committed state after preview.

### Snapshot acquisition

Building a document performs these steps:

1. Acquire the service's normal interaction mutex.
2. Revalidate the cache against the local SQLite profile revision.
3. Clone the complete committed profile, revision, journal cursor, and operation counts.
4. Release all application and store locks.
5. Materialize and filter detached committed transactions.
6. Sort and map them into export rows.

Revision revalidation is local storage work only. It performs no provider network I/O, identity
probe, provider lease acquisition, refresh fold, journal mutation, or revision increment. Export
works offline and while the provider is reconnect-required, rate-limited, refreshing, or writing.

Another process may commit after step 3. The export remains a coherent snapshot of the recorded
source revision. The execution-time metadata count and revision are authoritative; preview values
are estimates.

### Scope semantics

Full ignores analytical filters and exports every committed transaction.

Filtered applies the existing analytical transaction predicates to committed rows:

- committed search text;
- date range and time drill;
- transfer and hidden visibility;
- merchant, category, group, and account drill identities; and
- any future committed transaction predicate already represented by the canonical query.

It does not apply result-window pagination, selection, cursor, aggregate grouping, or the visible
sort order. A syntactically invalid, noncanonical, or oversized web query is rejected through the
existing view-state codec before projection.

Rows are ordered by date descending, then by stable local transaction ID using bytewise string
ordering. The order does not depend on map iteration, provider ordering, locale, or renderer.

## Lossless Export Document

### Semantic row schema

Export schema version 2 has these columns in this exact order:

1. `transaction_id` — stable local transaction ID, string.
2. `provider` — provider kind, string.
3. `provider_transaction_id` — provider-owned transaction ID, string.
4. `date` — calendar date.
5. `amount` — exact signed decimal string at `scale`.
6. `amount_minor` — exact signed 64-bit minor-unit integer.
7. `currency` — three-letter uppercase currency code.
8. `scale` — integer from zero through nine.
9. `account_id` — stable local account ID.
10. `account` — account display label.
11. `merchant_id` — stable local merchant ID.
12. `merchant` — merchant display label.
13. `category_id` — stable local category ID.
14. `category` — category display label.
15. `group_id` — stable local category-group ID.
16. `group` — category-group display label.
17. `notes` — transaction notes, possibly empty.
18. `hidden` — hide-from-reports flag.
19. `transaction_metadata_json` — canonical JSON object of provider transaction metadata.

The transaction metadata value uses sorted object keys, valid UTF-8, no insignificant whitespace,
and `{}` rather than null when metadata is absent. It is transaction data, not file-level export
metadata. It may contain provider-owned content and is protected exactly like every other exported
field.

There is no local-pending column and no floating-point column.

### File-level metadata

Every format records these named values:

- `moneyflow_export_schema_version`;
- `moneyflow_app_version`;
- `exported_at_utc` as RFC 3339 with nanoseconds;
- `source_revision`;
- `journal_cursor`;
- `excluded_pending_operation_count` for active operations through the cursor;
- `inactive_redo_operation_count`;
- `scope`;
- `canonical_query`, empty for Full;
- `transaction_count`;
- `earliest_date`, empty for no rows;
- `latest_date`, empty for no rows; and
- `provider_kinds` as a sorted JSON array.

Metadata never contains credentials, session material, profile display names, filesystem paths, or
selection values. Search text can appear only inside the user-requested filtered export's
`canonical_query`; it never enters logs or status records.

The clock and application version are injected in tests. Metadata round trips assert revision,
scope, excluded-operation count, transaction count, and date range by name.

## Format Contracts

### CSV

CSV is UTF-8 with LF newlines. It begins with a stable ordered `# key: value` metadata preamble,
then one header row and data rows in canonical order. The literal `#` preamble syntax is not passed
through row-cell sanitization. Dynamic header values use a separate sanitizer that replaces CR,
LF, and commas with spaces and applies the same formula guard where relevant.

The formula guard applies only to free-text columns: account, merchant, category, group, notes, and
`transaction_metadata_json`. A value in one of those columns matching Python's
`^\s*[=+\-@\t\r]` pattern receives a leading apostrophe. The transformation intentionally changes
dangerous free text. SQLite or Parquet is the exact-text choice for those fields.

Canonical typed encodings are emitted verbatim. This includes amount, amount minor units, scale,
date, hidden, currency, provider kind, and every local or provider ID. In particular, a negative
amount such as `-12.34` never receives an apostrophe and round trips as exact money. The CSV writer
handles delimiters, quotes, and embedded newlines after applying the column-specific policy.

### SQLite

SQLite export is a standalone database with `metadata` and `transactions` STRICT tables. It is not
a Moneyflow profile and contains no journal or provider credential tables.

`metadata` has a unique text key and non-null text value. `transactions` follows the exact column
order above with INTEGER storage for `amount_minor`, `scale`, and `hidden`; every other scalar is
TEXT. Checks enforce uppercase three-letter currency, scale from zero through nine, hidden in
`0,1`, nonempty required IDs, and canonical dates. There are no REAL columns.

The writer creates the database only at a private staging path, inserts all rows and metadata in
one transaction, runs `PRAGMA integrity_check`, closes it, and only then permits publication or
download.

### Parquet

Parquet uses schema version 2 key-value metadata and typed columns. `amount_minor` is INT64,
`scale` is an integer, `hidden` is BOOLEAN, and money is never DOUBLE or FLOAT. `date` uses the
Parquet DATE logical type. `amount` remains an exact UTF-8 decimal string.

Writer configuration is fixed: Snappy compression, an 8,192-row maximum row group, deterministic
column order, statistics enabled, and one pinned library version. Closing the writer and staging
file is mandatory before success.

The 100,000-row writer benchmark must confirm that the 8,192-row maximum remains within the local
and CI performance targets before tests pin it as the canonical setting.

Tests use an injected clock. Canonical logical digests exclude the export timestamp. Exact physical
Parquet bytes are tested only under the pinned dependency; dependency upgrades require deliberate
artifact review. A Python test reads a Go-written file with Polars and PyArrow, then asserts row
count, exact amount strings and minor units, schema types, and named key-value metadata.

## Filesystem, Locking, and Naming

### Export lock

`internal/home` adds `LockExport`, backed by the existing Unix and Windows advisory-lock helpers.
Lock order is:

```text
shared profile lifecycle lock -> exclusive export lock
```

No path acquires those locks in reverse order. The export lock is nonblocking. Contention returns
`export_busy`.

The lock is an open operating-system advisory lock, not a marker and not a persisted SQLite lease.
Closing the handle releases it. Process death releases it automatically, so a crashed owner cannot
wedge future export behind stale ownership. Tests cover contention, process-death recovery, and
same-process export-complete-export success to catch forgotten unlocks.

Preview never acquires the export lock; multiple processes may preview the same profile
concurrently. Execution acquires the lock before revision revalidation and document capture, then
holds it through encoding, writer close, and either final publication or response cleanup. It does
not change profile revision.

### Private directories and staging

Persistent and download exports use `<profile-root>/exports/`; directories are `0700`. Temporary
files use `<profile-root>/exports/.tmp/`, are `0600`, have fixed Moneyflow prefixes, and contain no
labels or search text in their names. Existing `internal/home` path hardening is reused and extended
with narrow private-temporary-file and no-replace publication helpers.

TUI publication uses a same-directory staging file, flushes and closes it, then atomically installs
the destination without overwrite and syncs the parent directory. Failure exposes no partial final
file.

Web generation closes the writer, stats the staging file for `Content-Length`, reopens it through
the hardened private-file helper, and streams it. Cleanup closes the handle before removal and runs
on success, writer failure, request cancellation, response failure, and client disconnect.
The safe-problem middleware passes successful bodies through only for the exact protected export
route. Ordinary successful API responses remain buffered until their handlers return, so panic
recovery can still replace them without exposing a partial response.

Windows can report a sharing violation if removal races a still-open response handle. Cleanup
closes first and retries briefly with bounded backoff. A remaining file is safe owner-only staging
and is removed as stale work by a later export. Windows-semantic fault tests cover this path.

Before creating new staging work, the lock holder removes only regular files with the exact managed
prefix that exceed the fixed stale age. It never follows links or deletes unknown files.

### Filenames

The base name matches Python:

```text
YYYY-MM-DD_HHMMSS_microseconds-<scope>-export.<extension>
```

SQLite uses `.db`; CSV and Parquet use `.csv` and `.parquet`. The name is ASCII and safe as the
plain `Content-Disposition` fallback. It contains no profile or transaction data.

If a destination exists, allocation tries `-2`, `-3`, and so on before the extension while holding
the export lock. It never overwrites. Exhausting the bounded counter space returns `export_failed`
without altering existing files.

Disposable demo and fixture profiles use their temporary profile export directory. TUI tells the
user that those exports disappear when the session closes.

## TUI Experience

`E` is routed through the existing action registry. When the committed preview is empty, the TUI
shows `No data to export` and does not open an overlay.

Otherwise the chooser mirrors Python:

- formats: Parquet, CSV, SQLite;
- scopes: Full dataset, Filtered transactions;
- defaults: Parquet and Full dataset;
- visible estimated transaction count for the selected scope;
- active pending-operation exclusion warning; and
- Enter to export, Escape to cancel, plus normal focus/navigation keys.

If commit is available, the warning says that active operations are excluded and may be committed
with `w` first. During an active provider write batch or another state where commit is unavailable,
it reports only that the operations are excluded.

Generation runs asynchronously. The overlay reports counts-only progress and remains visibly
busy. Escape cancels the context; cancellation removes staging and leaves the finance view,
selection, cursor, scroll, and URL-equivalent session state unchanged.

Success reports the exact completed path and row count to the user. A displayed path is permitted
user-facing output. Structured logs and persistent status remain path-free. Failure reports a safe
message and stable code without provider or financial content.

Export remains available offline and during reconnect-required, rate-limited, refresh, and
provider-write states. If another live process exports the same profile, the action announces that
an export is already running.

## Web Experience and API

### Preview

The profile API adds:

```text
POST <base-path>/api/v1/profiles/<profile-id>/export/preview
```

The bounded request contains export schema version and the current canonical analytical query. It
uses the same query codec and size limits as view and duplicate projection. The bounded response
contains source revision, Full count, Filtered count, operation counts, and commit availability.
Like the existing read-only view POST, preview does not require a mutation token.

Pressing `E` requests preview. Zero rows for both scopes produce `No data to export` without opening
the chooser. Otherwise the web chooser uses the same options, defaults, warnings, and keyboard
contract as TUI. The displayed count is explicitly an estimate.

### Download

The profile API adds:

```text
POST <base-path>/api/v1/profiles/<profile-id>/export
```

The body contains export schema version, format, scope, and the canonical analytical query. The
request carries the profile-scoped mutation token in the established custom header and must pass
the existing Origin and Fetch Metadata checks. No token, financial value, format, scope, query, or
filename appears in the URL. GET is not supported.

Execution always uses the committed revision current when the export lock is acquired. It does not
accept an expected revision and does not return `revision_conflict` merely because preview became
stale.

The response carries:

- the format-specific `Content-Type`;
- exact `Content-Length`;
- `Content-Disposition: attachment` with a safe ASCII filename;
- `Cache-Control: no-store`;
- the existing no-referrer and security headers; and
- no CORS permission.

The frontend uses authenticated `fetch`, reads one `Blob`, creates a temporary object URL, clicks a
temporary anchor with the download filename, removes the anchor, and revokes the object URL. This
buffers one complete file in browser memory, an accepted tradeoff at the 100,000-row target. The
behavior is browser-neutral; Chromium is the primary full-suite harness.

Download never changes the analytical URL, history, selection, cursor, scroll, or focused finance
row. Cancellation aborts the request and returns to the chooser with a safe announcement.

The binary response is a deliberate exception to the bounded JSON wire contract. It is available
only through the protected export operation, one per profile, and carries explicitly requested
financial data rather than application control state.

## Failure and Privacy Contract

Stable application/API codes are:

- `export_invalid` — unsupported schema version, format, scope, or query;
- `export_empty` — the execution-time committed scope contains no rows;
- `export_busy` — another live process holds the profile export lock;
- `export_cancelled` — the TUI caller cancelled, or the server observed a web client abort before
  delivery completed; a disconnected HTTP client cannot receive this code; and
- `export_failed` — safe projection, encoding, staging, flush, close, stat, publish, or delivery
  failure.

Export is read-only. Every failure leaves profile revision, committed tables, journal, cursor,
provider state, refresh generation, and write batch unchanged.

Persistent final files appear only after complete successful generation. Failed TUI exports expose
no final file. Web staging is removed on every normal exit path; crash leftovers remain `0600` and
are removed by the next lock holder.

Logs and durable status may contain only format, scope, revision, counts, byte size, duration,
stable error code, and correlation ID. They never contain paths, filenames, profile display names,
provider IDs, transaction IDs, labels, notes, metadata JSON, search text, canonical queries, row
contents, or file bytes.

The TUI may display the completed local path. The browser may receive the safe generated filename.
Those are explicit user-facing results, not log fields.

## Performance Contract

At 100,000 transactions on the committed synthetic performance profile:

- document projection targets less than 250 ms locally with a 1 second CI ceiling;
- each writer targets less than 1 second locally with a 5 second CI ceiling;
- no SQLite read transaction or application mutex is held during encoding;
- at most one document and one writer's bounded buffers are resident per profile export; and
- web may additionally hold one completed file in the browser Blob.

Performance tests report rows, bytes, duration, and a canonical digest. Load-sensitive timing gates
use the repository's supported non-performance path when the environment is unsuitable, and the
exact skipped gate is reported before commit.

## Testing Strategy

### Application projection

Tests prove:

- Full and Filtered use committed state;
- pending merchant, category, hidden, delete, taxonomy, and redo operations are excluded;
- those operations do not change committed filtered membership;
- operation counts and journal cursor are captured correctly;
- all committed query predicates match existing analytics behavior;
- selection, cursor, grouping, and pagination do not affect rows;
- revision revalidation uses local SQLite only and makes no provider call;
- a concurrent later commit does not alter the detached document;
- ordering is date descending then bytewise local ID;
- multiple currencies and scales preserve exact values; and
- active provider write and reconnect states do not disable export.

### Format conformance

CSV tests cover exact preamble order, header order, Unicode, quoting, CR/LF, free-text formula
prefixes with and without whitespace, metadata sanitization, exact amounts, and comment-aware
readback. A negative-amount row round trips through CSV as `-12.34` without a leading apostrophe.

SQLite tests inspect `sqlite_schema`, enforce STRICT and CHECK constraints, assert no REAL money
columns, run `integrity_check`, and compare every logical row and named metadata value.

Parquet Go tests inspect physical/logical schema, compression, row-group boundaries, exact rows,
and named key-value metadata. A Python test invokes the Go fixture exporter into an explicit test
temporary directory, reads rows with Polars and metadata with PyArrow, and asserts row count, exact
money, schema, revision, scope, excluded-operation count, transaction count, and date range. Tests
never use the default profile or personal data.

Injected clock/version tests produce canonical logical digests. Parquet dependency upgrades must
update the pin and deliberately review any digest or schema change.

### Filesystem and concurrency

Fault injection covers create, permission hardening, encode, flush, close, stat, integrity check,
publish, directory sync, response copy, and cleanup failures. Assertions prove no partial final
file, bounded Windows cleanup retry, safe stale leftovers, and unchanged profiles.

Lock tests cover two processes, same-process contention, process death, immediate sequential reuse,
and lock release on every error and cancellation path. Preview remains available while another
process holds the execution lock, as well as concurrently with other previews. Filename tests cover
microsecond collision, counter suffixes,
counter exhaustion, link rejection, unknown-file preservation, and stale managed temporary
cleanup.

### Renderer and browser behavior

TUI tests cover `E`, empty bypass, defaults, scope changes, state-aware warnings, asynchronous
progress, cancellation, user-visible path, demo warning, active-batch availability, and finance
state preservation. Python's real export screen is captured through the deliberate
`make parity-update-python` flow; Go semantic and visual artifacts are reviewed explicitly.

Web component/controller/API tests cover preview, exact query body, current-revision execution,
empty race, Blob/anchor cleanup, errors, abort, safe headers, token/Origin/Fetch-Metadata rejection,
GET rejection, no CORS/cache, unchanged history, and credential/financial-data-blind problems.

The full export browser journey runs in Chromium. Blob download and attachment/header journeys are
tagged `@smoke`, matching the existing Playwright matrix so Firefox and WebKit run those focused
checks rather than the entire suite. No browser screenshots are committed.

### Full gates

The slice runs:

- focused Go tests for application, exporter, home, API, and TUI;
- `go test ./...` and the race detector;
- `make verify-go` and `make verify-web`;
- deliberate parity generation followed by normal parity verification;
- the 100,000-row export performance gate;
- Python `pytest`, Pyright, and Ruff checks including cross-language Parquet readback;
- markdown and arrow-list checks; and
- private-data scanning of source, fixtures, artifacts, and commit messages.

Generated web distributions, browser screenshots, live exports, temporary downloads, and test
files remain ignored and uncommitted.

## Completion Criteria

The slice is complete when:

- `E` exports committed Full or Filtered data from TUI and web;
- CSV, SQLite, and Parquet conform to the lossless v2 schema;
- exact money and named metadata round-trip across Go and Python readers;
- pending operations are excluded and announced correctly;
- TUI files are private, complete, atomic, and never overwritten;
- web downloads are protected, no-store, disposable, browser-neutral, and path-free;
- export works offline and during provider write/reconnect states;
- lock crash recovery, Windows cleanup, collisions, and cancellation are proven;
- the Python export chooser and Go workflow pass reviewed semantic parity;
- all performance, Go, web, race, parity, Python, formatting, privacy, and documentation gates pass;
  and
- the diff contains no profile migration, import, backup, new provider, Python shim, or committed
  generated browser asset.

Completion does not authorize pushing, merging `go-port`, removing Python, or beginning another
slice without the user's direction.
