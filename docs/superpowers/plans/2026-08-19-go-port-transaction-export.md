# Go Port Transaction Export Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development
> (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use
> checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make the existing `E` action export committed transactions from the Go TUI and web UI
as lossless CSV, SQLite, or Parquet documents, with Full and Filtered scopes matching Python's
committed-frame semantics.

**Architecture:** `internal/app` revalidates the profile cache and captures a detached, sorted,
committed-state export document. A new `internal/exporter` output adapter owns CSV, SQLite,
Parquet, private staging, no-overwrite publication, and web-download cleanup. TUI and web remain
thin presenters: they preview counts without taking the export lock, then execute through the same
application capture and exporter lifecycle. The profile database, journal, provider runtime, and
profile schema are never mutated.

**Tech Stack:** Go 1.26.3; `modernc.org/sqlite` v1.56.0; `parquet-go` v0.30.1 with Snappy;
Huma v2.38.0; Bubble Tea v2.0.8; Svelte 5.56.3; Bun 1.3.14; Testify; Vitest 4.1.10;
Playwright 1.61.1; Python 3.11 with Polars and PyArrow for cross-implementation Parquet checks.

## Global Constraints

- Work only on the checked-out `go-port` branch. Do not switch branches, pull, rebase, push,
  merge, amend, or remove Python without explicit user permission.
- Follow TDD for every behavior change: add the focused failing test, run it and observe the
  intended failure, implement the smallest behavior, then rerun focused and package tests.
- Commit every verified task before beginning the next. Stage only that task's files. Never commit
  browser screenshots, `web/dist`, or `internal/web/dist`.
- Do not change `CurrentSchemaVersion`. Export files have their own v2 document schema and do not
  alter the install-only profile schema.
- Export committed state only. Never replay active journal operations into exported rows. Preview
  and execution report active and inactive operation counts, and renderers warn that active edits
  are excluded.
- Never perform provider network I/O, acquire the provider-operation lease, append/rewrite the
  journal, or advance profile revision during preview or export.
- Keep all money as signed integer minor units plus exact decimal strings. Do not add `float32`,
  `float64`, SQLite `REAL`, or lossy JavaScript numeric money.
- CSV formula guarding applies only to free-text columns. Typed IDs, dates, flags, scale, currency,
  `amount`, and `amount_minor` are emitted verbatim; `-12.34` must remain `-12.34`.
- Preview never takes `export.lock`. An opened profile or HTTP profile lease already owns the
  shared lifecycle lock; execution then takes the exclusive advisory export lock, revalidates and
  captures, and releases the export lock only after publication or response cleanup. The exporter
  must not reacquire `profile.lock` through a second same-process descriptor.
- Persistent logs contain only codes, revision numbers, counts, timings, format/scope enums, and
  correlation IDs. Never log labels, notes, search text, transaction IDs, financial values, file
  contents, URLs, credentials, or request bodies. A completed TUI notification may show its path.
- Files and directories are private: exports directory `0700`, stages and published files `0600`.
  Never overwrite an existing export.
- Web export is a same-origin protected mutation returning a Blob download. It is the deliberate
  complete-document exception to the bounded JSON wire contract and remains `no-store`/no-CORS.
- The full browser workflow runs in Chromium. Only the tagged Blob-download and header smoke cases
  run in Firefox and WebKit through the existing browser matrix.

## Target File Map

```text
internal/app/export.go                         committed preview and detached document capture
internal/app/export_test.go                    projection, metadata, ordering, and isolation tests
internal/app/actions.go                        enable the existing transactions.export action
internal/app/capabilities.go                   renderer-neutral export availability
internal/home/lock.go                          export.lock name and acquisition
internal/home/private_publish_unix.go          no-overwrite publication on Unix
internal/home/private_publish_windows.go       no-overwrite publication on Windows
internal/home/private_export.go                private stage creation and managed cleanup helpers
internal/exporter/exporter.go                  execution lifecycle and stable failures
internal/exporter/csv.go                       Python-compatible guarded CSV writer
internal/exporter/sqlite.go                    standalone lossless SQLite writer
internal/exporter/parquet.go                   pure-Go Parquet writer
internal/api/export.go                         preview and streaming download endpoints
internal/api/profiles.go                       profile root/temporary metadata on leases
internal/api/server.go                         route, security, and success-stream response handling
internal/tui/export.go                         export chooser, async execution, and status
internal/tui/export_format.go                  responsive chooser rendering
web/src/lib/controller/export.ts               preview/download state and Blob lifecycle
web/src/components/editing/ExportDialog.svelte accessible export chooser
web/tests/export.spec.ts                       Chromium workflow and cross-browser smoke tests
internal/tools/exportfixture/main.go            deterministic Go Parquet fixture for Python readback
tests/parity/test_go_export.py                  Polars/PyArrow interoperability test
```

## Cross-Task Interfaces

Task 1 establishes the renderer-neutral application contract:

```go
// internal/app/export.go
const ExportDocumentSchemaVersion = 2

type ExportScope string

const (
    ExportScopeFull     ExportScope = "full"
    ExportScopeFiltered ExportScope = "filtered"
)

type ExportPreview struct {
    Revision                 uint64
    FullCount                int
    FilteredCount            int
    ActiveOperations         int
    InactiveOperations       int
    CommitAvailable          bool
}

type ExportRequest struct {
    Scope          ExportScope
    State          ViewState
    CanonicalQuery string
    ExportedAt     time.Time
    AppVersion     string
}

type ExportMetadata struct {
    SchemaVersion            int
    AppVersion               string
    ExportedAt               time.Time
    ProfileRevision          uint64
    JournalCursor            int
    ExcludedActiveOperations int
    InactiveRedoOperations   int
    Scope                    ExportScope
    CanonicalQuery           string
    TransactionCount         int
    EarliestDate             *domain.Date
    LatestDate               *domain.Date
    ProviderKinds            []string
}

type ExportRow struct {
    TransactionID            string
    Provider                 string
    ProviderTransactionID    string
    Date                     domain.Date
    Amount                   string
    AmountMinor              int64
    Currency                 string
    Scale                    uint8
    AccountID                string
    Account                  string
    MerchantID               string
    Merchant                 string
    CategoryID               string
    Category                 string
    GroupID                  string
    Group                    string
    Notes                    string
    Hidden                   bool
    TransactionMetadataJSON  string
}

type ExportDocument struct {
    Metadata ExportMetadata
    Rows     []ExportRow
}

func (service *Service) PreviewExport(
    ctx context.Context,
    state ViewState,
) (ExportPreview, error)

func (service *Service) CaptureExport(
    ctx context.Context,
    request ExportRequest,
) (ExportDocument, error)
```

Task 3 establishes the output-adapter lifecycle:

```go
// internal/exporter/exporter.go
type Format string

const (
    FormatCSV     Format = "csv"
    FormatSQLite  Format = "sqlite"
    FormatParquet Format = "parquet"
)

type CaptureFunc func(context.Context, time.Time) (app.ExportDocument, error)

type Request struct {
    ProfileRoot string
    Format      Format
    Scope       app.ExportScope
    Now         func() time.Time
    Capture     CaptureFunc
}

type Result struct {
    Path     string
    Filename string
    Size     int64
}

type Download struct {
    Reader      io.ReadSeeker
    Filename    string
    ContentType string
    Size        int64
}

func WriteFile(context.Context, Request) (Result, error)
func PrepareDownload(context.Context, Request) (*Download, error)
func (download *Download) Close() error

type ErrorCode string

const (
    CodeInvalid   ErrorCode = "export_invalid"
    CodeBusy      ErrorCode = "export_busy"
    CodeCancelled ErrorCode = "export_cancelled"
    CodeFailed    ErrorCode = "export_failed"
)

type Error struct {
    Code   ErrorCode
    Detail string
}
```

`PrepareDownload` retains the export lock until `Download.Close`. `Close` closes the file before
removing it, retries Windows sharing violations briefly, and always releases the lock. If removal
still fails, the managed stage remains for the next lock holder's stale-stage cleanup.

Task 5 establishes the HTTP wire contract:

```go
const ExportWireVersion = "2"

type ExportPreviewBody struct {
    Version string `json:"version"`
    Query   string `json:"query" maxLength:"65536"`
}

type ExportPreviewResponse struct {
    Version            string `json:"version"`
    Revision           string `json:"revision"`
    FullCount          int    `json:"full_count"`
    FilteredCount      int    `json:"filtered_count"`
    ActiveOperations   int    `json:"active_operations"`
    InactiveOperations int    `json:"inactive_operations"`
    CommitAvailable    bool   `json:"commit_available"`
    TemporaryProfile   bool   `json:"temporary_profile"`
    CanonicalQuery     string `json:"canonical_query"`
}

type ExportBody struct {
    Version string          `json:"version"`
    Format  exporter.Format `json:"format"`
    Scope   app.ExportScope `json:"scope"`
    Query   string          `json:"query" maxLength:"65536"`
}
```

The response body for a successful export is the file itself, not JSON. Errors use the stable
codes `export_invalid`, `export_empty`, `export_busy`, `export_cancelled`, and `export_failed`.

---

## Task 1: Capture Committed Export Documents in `internal/app`

**Files:**

- Create: `internal/app/export.go`
- Create: `internal/app/export_test.go`
- Modify: `internal/app/actions.go`
- Modify: `internal/app/actions_test.go`
- Modify: `internal/app/capabilities.go`
- Modify: `internal/app/capabilities_test.go`

- [ ] **Step 1: Write failing projection tests**

Add Testify tests that open an explicit temporary profile and assert:

- preview revalidates cached state against the profile revision without provider I/O;
- Full count uses all committed rows and Filtered count uses `analyticalQuerySpec(state.Current)`;
- active journal changes do not alter either exported value or membership;
- active and inactive operation counts are reported separately;
- export is still available during an active provider-write batch and reconnect-required state;
- empty committed input returns `AppExportEmpty` from capture;
- filtered empty input returns `AppExportEmpty`, even when Full is nonempty;
- captured rows are sorted date descending, then transaction ID by bytewise string order;
- exact negative money is `AmountMinor == -1234` and `Amount == "-12.34"`;
- metadata records revision, cursor, excluded-active count, inactive-redo count, scope, canonical
  query, row count/date range, provider kinds, application version, and injected UTC time;
- returned rows and metadata slices are detached from service-owned memory;
- preview and capture do not change revision, committed rows, journal, provider state, or any
  reopened logical profile value;
- a separately held cross-process export lock does not block `PreviewExport`.

Run the focused tests and observe failures:

```bash
go test ./internal/app -run 'Test(Preview|Capture)Export' -count=1
```

- [ ] **Step 2: Add stable application errors and document types**

Extend `internal/app/errors.go` with:

```go
AppExportInvalid   AppErrorCode = "export_invalid"
AppExportEmpty     AppErrorCode = "export_empty"
AppExportFailed    AppErrorCode = "export_failed"
```

Keep details allowlisted and free of paths/financial content. Define the exact Task 1 types in
`export.go`. Validate scope, canonical-query presence for Filtered, UTC millisecond export time,
app version, and `ViewState` before reading profile state. Filesystem-only failures use the typed
`internal/exporter.Error` contract from Task 3; API maps both types to the same stable public codes.

- [ ] **Step 3: Implement preview and capture under the interaction boundary**

Both methods acquire `service.interactions`, call `refreshLocked`, then clone the latest committed
snapshot. Capture must never use `service.transactions` (effective state). Build row strings from
typed domain values and canonical JSON metadata. Filter only the committed clone:

```go
selected := committed
if request.Scope == ExportScopeFiltered {
    selected, err = analytics.Filter(committed, analyticalQuerySpec(request.State.Current))
    if err != nil {
        return ExportDocument{}, newAppError(AppExportInvalid, revision, err)
    }
}
```

Use existing provider/local identity material already present on `domain.Transaction`; do not query
SQLite from the renderer or exporter. Canonicalize provider kinds as sorted unique strings.

- [ ] **Step 4: Enable the shared action and capability**

Set `ActionExport.Implemented` to true. Return its capability independently of journal/provider
write state. Keep it available on an empty profile so renderers can issue the parity notification
without opening a chooser; `PreviewExport` supplies the count.

- [ ] **Step 5: Verify and commit Task 1**

```bash
go test ./internal/app -count=1
go test ./internal/analytics ./internal/domain -count=1
go vet ./internal/app/...
git diff --check
```

Commit:

```text
feat: capture committed export documents
```

---

## Task 2: Add Private Export Locking and Publication Primitives

**Files:**

- Modify: `internal/home/lock.go`
- Modify: `internal/home/lock_test.go`
- Create: `internal/home/private_export.go`
- Create: `internal/home/private_export_test.go`
- Create: `internal/home/private_publish_unix.go`
- Create: `internal/home/private_publish_windows.go`
- Create: `internal/home/private_publish_test.go`

- [ ] **Step 1: Write failing lock/publication tests**

Cover:

- `LockExport` resolves to `<profile-root>/export.lock`;
- one executing process excludes another and subprocess death releases the OS advisory lock;
- same-process sequential acquire/release succeeds immediately;
- `exports/` and `exports/.tmp/` are `0700`, and stage files are `0600`;
- no-replace publication succeeds once and refuses the same path without changing either file;
- only Moneyflow-managed stage names are removed as stale;
- architecture tests prove every exporter caller owns an opened profile/lease before attempting
  the export lock, so no path reverses lifecycle/shared -> export/exclusive;
- Windows-style cleanup retries a sharing violation, then either removes the stage or deliberately
  leaves it for the next export cleanup.

Use helper subprocesses already established by `internal/home` tests. Never use the real default
Moneyflow home.

```bash
go test ./internal/home -run 'Test.*Export|Test.*Publish' -count=1
```

- [ ] **Step 2: Add the export lock name**

Extend the existing lock enum/switch rather than creating another locking implementation:

```go
const LockExport LockName = "export.lock"
```

Use the repository's Unix `flock` and Windows `LockFileEx` paths. Do not add persisted owner,
expiry, or lease records: process-death release is the correctness property here.

- [ ] **Step 3: Implement private staging and no-replace publication**

Expose narrow helpers in `internal/home`; do not let `internal/exporter` duplicate ACL/mode checks.
The shape should be:

```go
func EnsureExportDirectories(profileRoot string) (exportsDir, stageDir string, err error)
func CreatePrivateStage(stageDir, prefix string) (*os.File, string, error)
func PublishPrivateNoReplace(stagePath, finalPath string) error
func RemoveManagedExportStages(stageDir string, olderThan time.Time) error
```

On Unix, publish without overwrite on the same filesystem (link/unlink or an equivalent atomic
no-replace primitive). On Windows, use the existing syscall discipline without a replace flag.
Validate every root/path through the current hardened-path helpers.

- [ ] **Step 4: Verify portability and commit Task 2**

```bash
go test ./internal/home -count=1
GOOS=windows GOARCH=amd64 go test ./internal/home -run '^$'
go vet ./internal/home/...
git diff --check
```

Commit:

```text
feat: add private export publication
```

---

## Task 3: Implement the Exporter Lifecycle, CSV, and SQLite

**Files:**

- Create: `internal/exporter/exporter.go`
- Create: `internal/exporter/exporter_test.go`
- Create: `internal/exporter/csv.go`
- Create: `internal/exporter/csv_test.go`
- Create: `internal/exporter/sqlite.go`
- Create: `internal/exporter/sqlite_test.go`
- Create: `internal/exporter/metadata.go`
- Create: `internal/exporter/testdata_test.go`
- Modify: `internal/provider/architecture_test.go`

- [ ] **Step 1: Write failing lifecycle tests**

Use an injected capture closure and temporary profile root. The closure receives the one canonical
execution timestamp selected under the lock, so filenames and file metadata cannot disagree.
Assert:

- preview is outside this package and therefore never locks;
- `WriteFile` acquires lock, removes old managed stages, captures after acquisition, encodes, fsyncs,
  publishes without overwrite, syncs the parent directory, then releases;
- capture/encode/close/sync/publish failures leave no visible partial file;
- filename collisions produce `-2`, `-3`, reject bounded counter exhaustion, and never overwrite;
- filenames follow `YYYY-MM-DD_HHMMSS_microseconds-<scope>-export.<ext>` with a safe ASCII fallback;
- a second process sees `export_busy` only while execution owns the lock;
- `PrepareDownload` returns a seekable private stage with a known length and retains the lock;
- `Download.Close` removes on success, server error, context cancellation, and simulated client
  disconnect, then releases the lock;
- `export_cancelled` represents TUI cancellation or a server-observed abort only; a disconnected
  browser does not receive a problem response;
- errors and test logs do not contain document rows or paths.
- architecture tests enforce that `internal/exporter` imports only `internal/app`, `internal/home`,
  parquet-go, modernc SQLite, and the standard library; `internal/app` never imports exporter; and
  TUI/API do not import format-specific packages.

```bash
go test ./internal/exporter -run 'Test(WriteFile|PrepareDownload|Download)' -count=1
```

- [ ] **Step 2: Implement the common lifecycle**

Define the typed `exporter.Error` contract shown above. Validate the Task 3 request and acquire the
export lock while the caller-owned opened profile/lease
continues to hold the shared lifecycle lock. Do not reacquire the lifecycle lock. Call `Capture`
only after export-lock acquisition, passing it the one injected execution time. Select a writer
through a closed switch; unknown formats return `export_invalid`. Generate names under the lock.
Inject clock, remove, and sleep functions in unexported test seams, not public options.

- [ ] **Step 3: Write failing CSV conformance tests**

Assert fixed column order, deterministic row order, Python-compatible `# key: value` metadata
headers, RFC 4180 records, UTF-8, and formula protection only for:

```text
account, merchant, category, group, notes, transaction_metadata_json
```

Use the exact guard `^\s*[=+\-@\t\r]` and prefix matching free text with one apostrophe. Assert
typed encodings are verbatim, especially:

```text
amount=-12.34
amount_minor=-1234
transaction_id=txn-example
```

The negative amount must round-trip without an apostrophe. The literal metadata preamble syntax is
not passed through row-cell sanitization. Dynamic metadata values use their separate deterministic
header sanitizer: CR, LF, and comma become spaces, and dangerous leading formula text is guarded.

- [ ] **Step 4: Implement CSV**

Use `encoding/csv`. Centralize the 19-column schema so every writer uses the same field order and
meaning. Render booleans, dates, scale, minor units, and IDs through typed encoders rather than the
free-text sanitizer. Centralize these exact metadata keys, in this order for CSV:

```text
moneyflow_export_schema_version
moneyflow_app_version
exported_at_utc
source_revision
journal_cursor
excluded_pending_operation_count
inactive_redo_operation_count
scope
canonical_query
transaction_count
earliest_date
latest_date
provider_kinds
```

- [ ] **Step 5: Write failing SQLite conformance tests**

Open the standalone output with `modernc.org/sqlite` and assert:

- `export_metadata` contains every named metadata field exactly once;
- `transactions` has the fixed v2 column set and declared integer/text types;
- money columns are `amount_minor INTEGER` and `amount TEXT`, with no `REAL` columns;
- all rows round-trip exactly, including negative values, Unicode, empty optional labels, hidden,
  and canonical transaction metadata JSON;
- constraints reject malformed scale/currency/date/hidden data;
- output contains no source profile tables, journal, provider session, or write batch.

- [ ] **Step 6: Implement SQLite and verify Task 3**

Create the export DB only at the stage path. Use one transaction, `STRICT` tables, and `CHECK`
constraints. Commit, checkpoint/close, sync, and return control to the lifecycle publisher.

```bash
go test ./internal/exporter -count=1
go test ./internal/home ./internal/app -count=1
go vet ./internal/exporter/...
git diff --check
```

Commit:

```text
feat: export committed CSV and SQLite files
```

---

## Task 4: Add Pure-Go Parquet and Python Interoperability

**Files:**

- Modify: `go.mod`
- Modify: `go.sum`
- Modify: `pyproject.toml`
- Modify: `uv.lock`
- Create: `internal/exporter/parquet.go`
- Create: `internal/exporter/parquet_test.go`
- Create: `internal/exporter/performance_test.go`
- Create: `internal/tools/exportfixture/main.go`
- Create: `tests/parity/test_go_export.py`
- Modify: `Makefile`

- [ ] **Step 1: Pin dependencies before implementation**

Add `github.com/parquet-go/parquet-go v0.30.1` and its pure-Go transitive dependencies with
`go get`. Add PyArrow to the existing Python dev dependency group and run `uv lock`; do not install
anything outside the project environment.

```bash
go get github.com/parquet-go/parquet-go@v0.30.1
uv add --dev pyarrow
```

- [ ] **Step 2: Write failing Parquet conformance tests**

Define a writer-only row struct whose tags produce the fixed v2 names and typed columns. Assert via
`parquet.GenericReader`:

- 19 fields and exact row values;
- `amount_minor` is signed INT64 and `amount` is UTF-8 string, never FLOAT/DOUBLE;
- date has a Parquet date logical type;
- Snappy compression is used;
- every named metadata value round-trips through file key/value metadata;
- deterministic inputs produce the same logical rows, metadata, and pinned physical SHA-256 digest
  when export time is injected.

Use the release's concrete API:

```go
writer := parquet.NewGenericWriter[parquetRow](
    output,
    parquet.Compression(&snappy.Codec{}),
    parquet.MaxRowsPerRowGroup(8192),
)
writer.SetKeyValueMetadata(key, value)
```

The physical digest is valid only for the pinned dependency/configuration. A dependency or row-group
change must deliberately update and review both the physical digest and logical schema/readback.

- [ ] **Step 3: Benchmark the proposed row group before making it canonical**

Add 100k-row benchmarks for projection and each writer. Run Parquet with 8,192 rows/group and
confirm it remains under the design's writer target. If it does not, change the constant and its
tests in this task; do not retain 8,192 merely because the design proposed it.

```bash
go test ./internal/exporter -run '^$' -bench 'BenchmarkWrite(Parquet|CSV|SQLite)100K' -benchtime=3x
go test ./internal/app -run '^$' -bench BenchmarkCaptureExport100K -benchtime=3x
```

Gate: projection under 250 ms locally / 1 s CI; each writer under 1 s locally / 5 s CI. Report a
load-sensitive timing miss precisely and use the repository's non-performance verification path;
do not weaken correctness tests.

- [ ] **Step 4: Implement Parquet**

Set all file metadata before close, write deterministic 8,192-row-or-benchmarked row groups, and
use Snappy. The common lifecycle retains ownership of stage sync/publication.

- [ ] **Step 5: Add a cross-implementation reader test**

`internal/tools/exportfixture` accepts only an explicit output path and writes a fixed synthetic
document; it never opens the default profile. It uses the writer directly and therefore does not
need an opened profile lifecycle lock. The Python test runs the tool into `tmp_path`, then:

- reads rows with Polars;
- reads file metadata with PyArrow;
- asserts row count, `amount_minor`, exact `amount` strings, and all named metadata fields;
- proves the negative value stays exact.

```bash
uv run pytest tests/parity/test_go_export.py -v
```

- [ ] **Step 6: Add `make test-export`, verify, and commit Task 4**

The target runs exporter Go tests, the 100k non-performance correctness cases, and Python Parquet
readback. Performance remains a separately reportable target.

```bash
make test-export
go test ./internal/exporter -count=1
uv run pytest tests/parity/test_go_export.py -v
go mod tidy
git diff --check
```

Commit:

```text
feat: add lossless Parquet export
```

---

## Task 5: Add Profile-Scoped Preview and Streaming Download APIs

**Files:**

- Create: `internal/api/export.go`
- Create: `internal/api/export_test.go`
- Modify: `internal/api/profiles.go`
- Modify: `internal/api/profiles_test.go`
- Modify: `internal/api/server.go`
- Modify: `internal/api/server_test.go`
- Modify: `internal/api/errors.go`
- Modify: `internal/api/security_test.go`
- Modify: `cmd/moneyflow/root.go`
- Modify: `cmd/moneyflow/root_test.go`

- [ ] **Step 1: Write failing route and lease tests**

Extend `ProfileLease` with profile root and temporary-profile metadata:

```go
type ProfileLease interface {
    Service() *app.Service
    ProfileRoot() string
    Temporary() bool
    Release() error
}
```

Update the real lease and every fake. Assert both endpoints resolve the requested canonical profile
and never use another profile's root. Add `export/preview` and `export` to resolved and legacy route
sets.

- [ ] **Step 2: Write failing preview API tests**

Register:

```text
POST <base>/api/v1/profiles/{profile_id}/export/preview
```

Assert one valid query request returns canonical query, both Full and Filtered count estimates,
pending exclusion, commit availability, and temporary-profile status so a scope change needs no
second preview. Preview requires no mutation token but remains same-origin/no-CORS. Invalid
version/query returns a stable bounded problem without echoing request data. A profile with zero
rows returns a successful zero-count preview so the renderer can show `No data to export`;
`export_empty` is the execution-time download backstop.

- [ ] **Step 3: Write failing download/security tests**

Register:

```text
POST <base>/api/v1/profiles/{profile_id}/export
```

Assert:

- mutation token, Origin, and Fetch Metadata are required;
- CSRF/cross-origin attempts fail before capture;
- response is `no-store`, `nosniff`, no-CORS, and has exact `Content-Length`;
- `Content-Disposition` contains quoted safe ASCII fallback and RFC 5987 UTF-8 filename;
- CSV/SQLite/Parquet MIME types are correct;
- response bytes are a valid complete document;
- completion, handler error, and client disconnect all call `Download.Close`;
- no financial data, query, or filename enters problem responses or logs;
- execution revalidates current revision and does not compare against preview revision.

- [ ] **Step 4: Make successful responses stream without weakening problem sanitization**

`safeProblemResponses` currently buffers all responses. Change its response writer so successful
status headers/body pass through immediately, while status `>=400` remains bounded and sanitized.
Implement `Unwrap() http.ResponseWriter` for response-controller compatibility. Add tests proving a
large success stream is not buffered and a malicious error body is still replaced.

- [ ] **Step 5: Implement Huma streaming**

Use `huma.StreamResponse` (or the release's equivalent streaming response type) so OpenAPI records
the endpoint while the body callback copies from `Download.Reader` to the response. The callback
must `defer download.Close()` before copying. Once headers are sent, disconnect cleanup is local;
do not try to deliver `export_cancelled` to a disconnected browser.

Decode the canonical query with existing `DecodeViewQuery`; pass the resulting canonical string to
`app.CaptureExport`. Do not move the codec or import `internal/api` from TUI.

- [ ] **Step 6: Verify and commit Task 5**

```bash
go test ./internal/api ./cmd/moneyflow -run 'Test.*Export|Test.*Problem|Test.*Profile' -count=1
go test ./internal/api ./cmd/moneyflow -count=1
go vet ./internal/api/... ./cmd/moneyflow/...
git diff --check
```

Commit:

```text
feat: serve protected transaction exports
```

---

## Task 6: Implement the TUI Export Chooser

**Files:**

- Modify: `internal/tui/model.go`
- Modify: `internal/tui/update.go`
- Modify: `internal/tui/layout.go`
- Modify: `internal/tui/shell.go`
- Modify: `cmd/moneyflow/tui_shell.go`
- Create: `internal/tui/export.go`
- Create: `internal/tui/export_format.go`
- Create: `internal/tui/export_test.go`
- Modify: `internal/tui/semantic_parity_test.go`
- Modify: `moneyflow/parity/semantic.py`
- Modify: `tests/parity/test_semantic.py`
- Modify: `testdata/parity/frame_scenarios.json`

- [ ] **Step 1: Add profile/output dependencies without coupling TUI to API**

Extend `ShellOpenedProfile` and the finance model options with `ProfileRoot` and `Temporary`. Add an
injected canonical query encoder to `tui.Options`:

```go
type ViewQueryEncoder func(app.ViewState) (string, error)

type Options struct {
    // existing fields
    ProfileRoot     string
    Temporary       bool
    EncodeViewQuery ViewQueryEncoder
}
```

Wire `api.EncodeViewQuery` only from `cmd/moneyflow`; `internal/tui` must not import `internal/api`.
Demo/preselected profiles use their explicit temporary root.

- [ ] **Step 2: Write failing action/chooser tests**

Assert:

- `E` on an empty committed projection reports `No data to export` without opening a chooser;
- chooser defaults to Parquet + Full;
- Tab/Shift+Tab or arrows move between bounded options, Enter executes, Esc cancels;
- active pending edits show the excluded count and only say `commit them first` when commit is
  available; active provider write uses state-aware exclusion wording;
- temporary profiles warn that the file lives under a temporary profile root;
- export remains openable offline, reconnect-required, and during a provider write;
- Full and Filtered previews show the correct counts;
- terminal resize keeps chooser usable down to 80x24;
- cancel does not acquire the export lock or create directories/files;
- successful status includes the completed path, while internal logs remain path-free.

- [ ] **Step 3: Implement asynchronous execution**

Add `overlayExport`, export state, update routing, and a responsive renderer. Enter launches a
Bubble Tea command that calls `exporter.WriteFile`; never block `Update`. Capture closure calls
`service.CaptureExport` with the then-current view, injected time/version, and canonical query.
Pass an empty canonical query for Full and the injected encoder result for Filtered. Execution
revalidates after the export lock is acquired through the exporter callback.

- [ ] **Step 4: Add parity artifacts deliberately**

Drive Python's existing export chooser to add the missing semantic scenario, then add the matching
Go scenario. This is a deliberate artifact update:

```bash
make parity-update-python
git diff -- testdata/parity
make parity-update-go
git diff -- testdata/parity
make parity
```

Review every changed frame. Do not generate or commit screenshots.

- [ ] **Step 5: Verify and commit Task 6**

```bash
go test ./internal/tui ./cmd/moneyflow -run 'Test.*Export' -count=1
go test ./internal/tui ./cmd/moneyflow -count=1
make parity
git diff --check
```

Commit:

```text
feat: add TUI transaction export
```

---

## Task 7: Implement the Web Export Dialog and Blob Download

**Files:**

- Modify: `web/src/lib/api/client.ts`
- Modify: `web/src/lib/api/schema.ts`
- Create: `web/src/lib/controller/export.ts`
- Create: `web/src/lib/controller/export.test.ts`
- Modify: `web/src/lib/controller/view-controller.svelte.ts`
- Create: `web/src/components/editing/ExportDialog.svelte`
- Create: `web/src/components/editing/ExportDialog.test.ts`
- Modify: `web/src/components/AppShell.svelte`
- Modify: `web/src/components/AppShell.test.ts`
- Create: `web/tests/export.spec.ts`

- [ ] **Step 1: Regenerate the client and write failing API-client tests**

Run the deliberate generator after Task 5 changes OpenAPI:

```bash
make web-generate
```

Add client methods:

```ts
previewExport(request: ExportPreviewBody, signal?: AbortSignal): Promise<ExportPreviewResponse>
downloadExport(request: ExportBody, signal?: AbortSignal): Promise<Response>
```

The download method must preserve raw headers/body and use the existing mutation-token refresh
path. It must never JSON-decode a successful file response.

- [ ] **Step 2: Write the controller tests**

Model `idle | previewing | ready | exporting | complete | failed`. Assert:

- defaults are Parquet + Full;
- one preview returns both counts and scope changes select the corresponding count locally;
- projected counts are estimates and response metadata remains authoritative;
- mutation-token expiry refresh retries once before application; revision is not replayed;
- success buffers the response into a Blob, creates a temporary object URL, clicks a hidden anchor,
  removes the anchor, and revokes the URL in `finally`;
- filename comes from safe `Content-Disposition` with a fixed fallback;
- cancellation aborts fetch and never reports a false completed download;
- no credentials, tokens, or financial values enter controller status/errors.

- [ ] **Step 3: Write the dialog/component tests**

Use kit-ui components and accessible labels. Assert:

- `E` opens the dialog from the same static action registry;
- empty committed data shows `No data to export` and never mounts the chooser;
- format/scope controls are keyboard navigable and restore focus on close;
- pending-exclusion, state-aware commit, active-batch, and temporary-profile warnings render;
- Export disables while running, Esc cancels only before transfer starts, and errors preserve the
  dialog for retry;
- closing does not change analytical URL, cursor, scroll, or selection.

- [ ] **Step 4: Implement AppShell integration**

Add `export` to the overlay union and shortcut scope. Keep all transient format/scope state out of
the durable analytical URL. The server owns export-time revision/capture; the browser never sends
row data.

- [ ] **Step 5: Add browser tests without durable screenshots**

Chromium covers Full/Filtered and all formats against an explicit temporary profile. Tag only the
Blob/header checks `@smoke` so the existing Firefox/WebKit smoke projects run them. Assert:

- same-origin POST includes mutation protection;
- Content-Disposition, Content-Length, no-store, and nosniff headers;
- downloaded bytes parse and match exact values;
- empty data bypass, pending warning, keyboard path, cancellation, and server failure;
- no screenshots are written or committed.

- [ ] **Step 6: Verify and commit Task 7**

```bash
make web-check
make web-test
make web-build
make web-assets-check
make web-embed
make web-embed-check
make web-e2e
git status --short
git diff --check
```

Only source/generated schema changes are staged; `web/dist` and `internal/web/dist` remain ignored.

Commit:

```text
feat: add web transaction export
```

---

## Task 8: Run Full Gates and Close the Export Slice

**Files:**

- Modify if needed: `Makefile`
- Modify if behavior changed: `README.md`
- Modify if behavior changed: `docs/**`
- Modify: `docs/superpowers/plans/2026-08-19-go-port-transaction-export.md` (checkboxes only)

- [ ] **Step 1: Audit the implementation against completion criteria**

Confirm:

- `E` works in TUI and web;
- CSV, SQLite, and Parquet support Full/Filtered;
- exported rows are committed-only and pending operations are warned/excluded;
- exact-money, fixed v2 row schema, and named metadata contracts hold;
- preview never locks and execution uses lifecycle -> export lock order;
- filesystem stages are private/atomic/no-overwrite and web stages clean up;
- exports work offline, reconnect-required, and during active provider writes;
- no provider network I/O or profile mutation occurs;
- Windows compiles and cleanup fallback is covered;
- the diff contains no profile schema bump, provider mutation, credential persistence, screenshot,
  or built web asset.

- [ ] **Step 2: Run complete Go and web gates**

```bash
make test-export
make verify-go
make verify-web
make test-race
```

If the known load-sensitive heartbeat/performance gate fails in the full run, rerun its isolated
supported target three times, report both results, and do not conceal or redefine the threshold.

- [ ] **Step 3: Run complete Python and documentation gates**

```bash
uv run pytest -v
uv run pytest --cov --cov-report=term-missing
uv run pyright moneyflow/
uv run ruff format --check moneyflow/ tests/
uv run ruff check moneyflow/ tests/
npx --yes markdownlint-cli@0.47.0 --config .markdownlint.json README.md 'docs/**/*.md'
.github/scripts/check-arrow-lists.sh
```

- [ ] **Step 4: Run privacy and artifact audits**

Use the repository private-data scrub workflow. Additionally verify:

```bash
git status --short
git diff --check
git ls-files web/tests/screenshots internal/web/dist web/dist
rg -n 'float32|float64|\bREAL\b' internal/exporter internal/app/export.go
```

The tracked-assets command must print nothing. Review the complete diff for personal data,
generated binary content, request bodies, paths in logs, and unrelated changes.

- [ ] **Step 5: Commit any verification-only integration changes**

If Task 8 required Makefile, docs, or integration corrections, commit them separately after all
gates pass:

```text
test: close transaction export verification gaps
```

If it required no changes, do not create an empty commit.

- [ ] **Step 6: Report manual follow-up honestly**

Automated completion precedes manual dogfooding. Suggested manual checks use only demo/synthetic
profiles:

```bash
./bin/moneyflow tui --demo
./bin/moneyflow web --demo
```

Exercise E -> defaults -> export, Filtered scope, cancel, pending exclusion, and one browser Blob
download. Any issue found becomes a new tested and committed fix; do not leave a dirty worktree.
