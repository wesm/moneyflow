# Go Port Amazon Import and Matching Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development
> (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use
> checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make Amazon order-history CSVs first-class, durable, locally editable Go profiles and
restore Python's bounded cross-profile Amazon matching in Cobra, TUI, and web.

**Architecture:** A pure `internal/importer/amazon` package discovers and parses bounded CSV
sources into normalized exact-money candidates. `internal/app` reconciles those candidates against
the freshest committed snapshot and source ledger through a closed planner callback; the SQLite
store installs the resulting version-eight state atomically. A renderer-neutral
`internal/amazonimport` coordinator owns locks, private stages, progress, cancellation, and catalog
rollback, while matching reads short-lived committed Amazon snapshots through application/store
interfaces and returns one bounded deterministic projection to both renderers.

**Tech Stack:** Go 1.26.3; `modernc.org/sqlite` v1.56.0; Cobra 1.10.2; Huma v2.38.0;
Bubble Tea 2.0.8; Svelte 5.56.3; Bun 1.3.14; `@kenn-io/kit-ui`; Testify 1.11.1;
Vitest 4.1.10; Playwright 1.61.1; Python 3.11 with Polars for parity characterization.

## Global Constraints

- Work only on the checked-out `go-port` branch. Do not switch branches, pull, rebase, push,
  merge, amend, or remove Python without explicit user permission.
- Follow TDD for every behavior change: add the focused failing test, run it and observe the
  intended failure, implement the smallest behavior, then rerun focused and package tests.
- Commit every verified task before beginning the next. Stage only that task's files. Never commit
  browser screenshots, `web/dist`, or `internal/web/dist`.
- Install schema version 8 only into an empty database. Refuse version 7 and every other mismatch;
  do not add schema or journal-payload migrations.
- Keep pure-Go SQLite and the no-CGO Linux, macOS, and Windows contract.
- Keep all money as signed integer minor units. Do not add `float32`, `float64`, SQLite `REAL`, or
  lossy JavaScript numeric money.
- Treat an observed order's valid non-cancelled multiset as authoritative. A wholly absent order is
  untouched. Cancelled rows identify observations but never contribute financial facts.
- Apply pairing in this exact order: exact active fingerprint, exact retired fingerprint, one
  unequal active real-ASIN singleton, one unequal active order-wide ASIN-less singleton, then
  allocate/retire. Never positionally pair ambiguous unequal rows.
- Preserve user-owned category, hidden, notes, and safe merchant intent during reimport. Rewrite
  journal targets only through the existing deterministic target-removal contract.
- An unchanged import may append one counts-only operational history row, but it must not change
  committed tables or semantic revision.
- Persistent logs and status contain only profile ID, stable codes, revision, counts, durations,
  canonical candidate digest, and correlation ID. Never persist filenames, record coordinates,
  order IDs, ASINs, labels, amounts, search text, or CSV contents.
- The immediate initiating UI may show relative filename, logical record, column, and reason for
  one invalid row. Generic API problems, status polling, history, and logs remain counts-only.
- Amazon profiles are local-only: no session, credential, provider operation lease, background
  refresh, write batch, network call, or provider mutation.
- Keep `provider.refresh` as the action identity. Monarch remains scheduled and non-interactive;
  Amazon presents `Import Amazon orders` and always opens a user-driven source chooser.
- Do not add Amazon scraping, an Amazon API, credential storage, raw CSV retention, persisted match
  results, or cross-profile transactions.

## Target File Map

```text
internal/domain/amazon.go                         durable Amazon source-fact value types
internal/importer/amazon/types.go                 parser limits, candidate, progress, errors
internal/importer/amazon/discover.go              safe recursive directory discovery
internal/importer/amazon/csv.go                   bounded RFC 4180 parsing and validation
internal/importer/amazon/fingerprint.go           ASIN-less keys and versioned fingerprints
internal/importer/amazon/*_test.go                parser, limits, ordering, and privacy tests
internal/home/lock.go                              amazon-import.lock registration
internal/home/private_amazon.go                    owner-only upload stages and stale cleanup
internal/profilecatalog/{manifest,create}.go       Amazon catalog kind and rollback behavior
internal/store/store.go                            neutral Amazon atomic store interfaces
internal/store/sqlite/schema/profile.sql           installed version-eight tables and checks
internal/store/sqlite/initialize.go                 CurrentSchemaVersion = 8
internal/store/sqlite/amazon_state.go               committed Amazon state loading
internal/store/sqlite/amazon_import.go              atomic planner callback and installation
internal/app/amazon_reconcile.go                    pure tiered identity reconciliation
internal/app/amazon_import.go                       import service, clone, and journal rebase
internal/app/amazon_matching.go                     cached global three-pass matcher
internal/app/transaction_info.go                    bounded detail/match projection
internal/amazonimport/types.go                      renderer-neutral attempt protocol
internal/amazonimport/coordinator.go                locks, attempts, progress, cancellation
internal/amazonimport/staging.go                    web multipart stage lifecycle
cmd/moneyflow/amazon.go                             provider import amazon Cobra presenter
internal/tui/amazon_import.go                       path/settings/import workflow
internal/tui/transaction_info.go                    Amazon facts and matches in the info overlay
internal/api/amazon_import.go                       protected attempt/upload endpoints
internal/api/transaction_info.go                    read-only bounded detail POST
web/src/lib/controller/amazon-import.ts             upload attempt state and polling
web/src/components/profiles/AmazonImportWizard.svelte  onboarding and repeat import UI
web/src/components/editing/TransactionInfoDrawer.svelte  accessible Amazon detail UI
web/tests/amazon-import.spec.ts                     browser import/security/cleanup journeys
web/tests/amazon-matching.spec.ts                   detail, search, and keyboard journeys
```

## Cross-Task Interfaces

Task 1 establishes the parser contract:

```go
// internal/importer/amazon/types.go
type Settings struct {
    Currency domain.Currency
    Scale    uint8
}

type Limits struct {
    Files, Records, Columns int
    BytesPerFile, TotalBytes, BytesPerRecord, BytesPerField int64
}

var ProductionLimits = Limits{
    Files: 256, Records: 1_000_000, Columns: 128,
    BytesPerFile: 64 << 20, TotalBytes: 512 << 20,
    BytesPerRecord: 1 << 20, BytesPerField: 16 << 10,
}

type Coordinate struct {
    RelativeFilename string
    Record           int
    Column           string
    Reason           string
}

type SourceFile struct {
    RelativeName string
    Path         string
}

type Row struct {
    OrderID, ProductName, ASIN, ASINLessKey string
    OrderDate domain.Date
    Quantity int64
    AmountMinor int64
    UnitPriceMinor *int64
    Currency domain.Currency
    Scale uint8
    OrderStatus, ShipmentStatus string
    IdentityFingerprint, FullFingerprint string
    RelativeFilename string
    Record int
}

type Candidate struct {
    Rows []Row
    ObservedOrderIDs []string
    FileCount, LogicalRecordCount, BlankRecordCount, CancelledRecordCount int
    Digest string
}

type Progress struct { Phase string; Completed, Total int }
type ObserveFunc func(Progress)

func DiscoverDirectory(context.Context, string, Limits) ([]SourceFile, error)
func Parse(context.Context, []SourceFile, Settings, Limits, ObserveFunc) (Candidate, error)
```

Task 2 establishes the durable store contract without importing the parser:

```go
// internal/store/store.go
type AmazonSettings struct {
    Currency domain.Currency
    Scale uint8
    TaxonomySourceProfileID string
    CreatedAt time.Time
}

type AmazonOrderItem struct {
    LocalTransactionID domain.EntityID
    SourceIdentity string
    OrderID, ASIN, ASINLessKey, ProductName string
    OrderDate domain.Date
    Quantity, AmountMinor int64
    UnitPriceMinor *int64
    Currency domain.Currency
    Scale uint8
    OrderStatus, ShipmentStatus string
    IdentityFingerprint, FullFingerprint string
    Retired bool
}

type AmazonImportState struct {
    Snapshot domain.ProfileSnapshot
    Settings *AmazonSettings
    Items []AmazonOrderItem
    Allocations []LabelAllocation
}

type AmazonImportHistory struct {
    StartedAt, CompletedAt time.Time
    SourceRevision, ResultingRevision uint64
    CandidateDigest string
    FileCount, LogicalRecordCount, BlankRecordCount, CancelledRecordCount int
    InsertedCount, UpdatedCount, RestoredCount, RetiredCount, UnchangedCount int
}

type ProposedAmazonIDs struct {
    TransactionIDs, AccountIDs, MerchantIDs, SourceIdentities []string
    GroupIDs, CategoryIDs []string
}

type AmazonImportPlan struct {
    Committed domain.CommittedProfile
    Journal []domain.Operation
    Cursor int
    KnownDrills []domain.DrillIdentity
    Settings *AmazonSettings
    Items []AmazonOrderItem
    Allocations []LabelAllocation
    History AmazonImportHistory
    SemanticChange bool
}

type AmazonImportPlanner func(AmazonImportState, ProposedAmazonIDs) (AmazonImportPlan, error)

type AtomicAmazonImportRequest struct {
    ImportedAt time.Time
    CandidateDigest string
    TaxonomyClone *domain.CommittedProfile
}

type AmazonImportCommit struct {
    PreviousRevision, Revision uint64
    SemanticChange bool
    History AmazonImportHistory
}

type Profile interface {
    ApplyAmazonImport(
        context.Context, AtomicAmazonImportRequest, AmazonImportPlanner,
    ) (AmazonImportCommit, error)
    LoadAmazonState(context.Context) (AmazonImportState, error)
}
```

Task 3 establishes the application operation:

```go
// internal/app/amazon_import.go
type AmazonImportRequest struct {
    Candidate amazon.Candidate
    Settings amazon.Settings
    TaxonomyClone *TaxonomyClone
    ImportedAt time.Time
}

type AmazonImportResult struct {
    Revision uint64
    Inserted, Updated, Restored, Retired, Unchanged int
    RemovedJournalTargets, RemovedJournalOperations int
    NoOp bool
}

func (service *Service) ImportAmazon(
    context.Context, AmazonImportRequest,
) (AmazonImportResult, error)

func BuildAmazonImportPlan(
    store.AmazonImportState,
    store.ProposedAmazonIDs,
    AmazonImportRequest,
) (store.AmazonImportPlan, error)
```

Task 4 establishes the shared attempt protocol:

```go
// internal/amazonimport/types.go
const ProtocolVersion = uint16(1)

type State string
const (
    StateSettings State = "settings_required"
    StateSource State = "source_required"
    StateParsing State = "parsing"
    StateInstalling State = "installing"
    StateComplete State = "complete"
    StateFailed State = "failed"
    StateCanceled State = "canceled"
)

type Snapshot struct {
    ProtocolVersion uint16 `json:"protocol_version"`
    AttemptID, ProfileID string
    StateVersion uint64
    State State
    Progress *Progress
    Result *Result
    Failure *Failure
}

type Progress struct {
    Phase string `json:"phase"`
    Completed int `json:"completed"`
    Total int `json:"total"`
    Files int `json:"files"`
    Records int `json:"records"`
    ActiveRows int `json:"active_rows"`
    CancelledRows int `json:"cancelled_rows"`
}

type Result struct {
    Revision string `json:"revision"`
    Inserted int `json:"inserted"`
    Updated int `json:"updated"`
    Restored int `json:"restored"`
    Retired int `json:"retired"`
    Unchanged int `json:"unchanged"`
    NoOp bool `json:"no_op"`
}

type Failure struct {
    Code string `json:"code"`
    Message string `json:"message"`
    CanRetry bool `json:"can_retry"`
}

type StartRequest struct {
    ProfileID, Renderer string
    Settings *amazon.Settings
    CloneTaxonomyFrom string
}

type StageRequest struct {
    ProfileID, AttemptID string
    ExpectedStateVersion uint64
    RelativeName string
    Body io.Reader
}

type ExecuteRequest struct {
    ProfileID, AttemptID string
    ExpectedStateVersion uint64
}

type StatusRequest struct { ProfileID, AttemptID string }

type CancelRequest struct {
    ProfileID, AttemptID string
    ExpectedStateVersion uint64
}

type DirectoryRequest struct {
    ProfileID, Renderer, Directory string
    Settings amazon.Settings
    CloneTaxonomyFrom string
}

type Coordinator interface {
    Start(context.Context, StartRequest) (Snapshot, error)
    Stage(context.Context, StageRequest) (Snapshot, error)
    Execute(context.Context, ExecuteRequest) (Snapshot, *amazon.Coordinate, error)
    Status(context.Context, StatusRequest) (Snapshot, error)
    Cancel(context.Context, CancelRequest) (Snapshot, error)
    ImportDirectory(context.Context, DirectoryRequest) (Snapshot, *amazon.Coordinate, error)
}
```

Task 5 establishes the matching projection:

```go
// internal/app/amazon_matching.go
type AmazonMatchClass string
const (
    AmazonExactOrder AmazonMatchClass = "exact_order"
    AmazonFuzzyOrder AmazonMatchClass = "fuzzy_order"
    AmazonExactItem AmazonMatchClass = "exact_item"
)

type AmazonMatch struct {
    Class AmazonMatchClass
    Confidence string
    SourceProfileID, OrderID string
    Date domain.Date
    Amount domain.Money
    FirstProduct string
    TotalItems int
    Items []AmazonMatchItem
}

type AmazonMatchItem struct {
    TransactionID domain.EntityID
    ProductName, ASIN string
    Quantity int64
    Amount domain.Money
}

type AmazonFacts struct {
    OrderID, ASIN string
    ProductName string
    Quantity int64
    OrderStatus, ShipmentStatus string
    UnitPrice *domain.Money
}

type TransactionInfoRequest struct {
    ExpectedRevision uint64
    State ViewState
    TransactionID domain.EntityID
    MatchWindow, ItemWindow WindowRequest
}

type TransactionInfo struct {
    Revision uint64
    Transaction domain.Transaction
    AmazonFacts *AmazonFacts
    Matches []AmazonMatch
    TotalMatches int
    SkippedProfiles int
}

func (service *AmazonMatchService) TransactionInfo(
    context.Context, TransactionInfoRequest,
) (TransactionInfo, error)
```

## Required Commit Gate

Run each task's focused red/green command while developing. Before every task commit, run:

```bash
make verify-go
uv run pytest -v
uv run pyright moneyflow/
uv run ruff format --check moneyflow/ tests/
uv run ruff check moneyflow/ tests/
```

Expected: every command exits zero. If documentation changes, also run:

```bash
npx --yes markdownlint-cli@0.47.0 --config .markdownlint.json README.md 'docs/**/*.md'
.github/scripts/check-arrow-lists.sh
```

Beginning with Task 7, also run:

```bash
bun install --cwd web --frozen-lockfile
make verify-web
```

Never accept or commit generated browser screenshots, `web/dist`, or `internal/web/dist`. If a
supported performance gate is invalid because of host load, run the repository's supported
non-performance verification path, report the exact timing gate, and commit the otherwise verified
task.

---

## Task 1: Parse Bounded Amazon Order-History CSVs Deterministically

**Files:**

- Create: `internal/domain/amazon.go`
- Create: `internal/domain/amazon_test.go`
- Create: `internal/importer/amazon/types.go`
- Create: `internal/importer/amazon/discover.go`
- Create: `internal/importer/amazon/discover_test.go`
- Create: `internal/importer/amazon/csv.go`
- Create: `internal/importer/amazon/csv_test.go`
- Create: `internal/importer/amazon/fingerprint.go`
- Create: `internal/importer/amazon/fingerprint_test.go`
- Modify: `internal/provider/architecture_test.go`

**Interfaces:**

- Consumes: `domain.Date`, `domain.Currency`, integer minor-unit conventions, and standard-library
  `encoding/csv`.
- Produces: the exact `amazon.Settings`, `Limits`, `SourceFile`, `Row`, `Candidate`, `Coordinate`,
  and parser functions in Cross-Task Interfaces.

- [ ] **Step 1: Add failing discovery and architecture tests**

Use Testify to assert bytewise relative-name ordering, recursive matching of
`Retail.OrderHistory.*.csv`, symlink/non-regular/root-redirection rejection, duplicate name and
content rejection, and every file/byte boundary. Extend the architecture test so
`internal/importer/amazon` imports only the standard library and `internal/domain`.

Run:

```bash
go test ./internal/importer/amazon ./internal/provider -run 'TestAmazon|TestProviderArchitecture' -count=1
```

Expected: FAIL because the package and rules do not exist.

- [ ] **Step 2: Implement safe discovery and production bounds**

Implement `DiscoverDirectory` using rooted filesystem operations, reject every symlink encountered,
hash candidate contents for duplicate detection while enforcing limits, and return only bytewise
sorted `SourceFile` values. Return stable errors `amazon_import_empty`,
`amazon_import_too_large`, and `amazon_import_invalid`; keep the detailed path only in the returned
ephemeral coordinate.

Run the command from Step 1. Expected: discovery tests PASS.

- [ ] **Step 3: Add failing RFC 4180 and exact-money parser tests**

Cover BOM, CRLF, commas, escaped quotes, embedded newlines, logical record numbering, duplicate and
missing headers, unknown-column discard, UTF-8 failures, exponent/currency-symbol/grouping/overflow
rejection, sign inversion, scale 0 through 9, missing Quantity to `1`, invalid nonblank quantities,
blank-record skipping, and partially populated rejection. Assert missing Currency normalizes to the
binding and a conflicting active-row code is surfaced without an amount or label.

Run:

```bash
go test ./internal/importer/amazon -run 'Test(Parse|Money|Headers|Quantity|Blank)' -count=1
```

Expected: FAIL because `Parse` is incomplete.

- [ ] **Step 4: Implement the retention allowlist and cancelled-row boundary**

Decode only the required/optional fields from the spec, discard every other field before candidate
construction, and stop retaining source bytes after parsing. Treat a row with canonical
`Order Status == "Cancelled"` as observed when its UTF-8 order ID is valid, without parsing amount,
currency, date, product, quantity, ASIN, unit price, or shipment status. Add the explicit regression
where a cancelled row has garbage `Total Owed` and a conflicting currency but still yields an empty
authoritative observed order.

Run the Step 3 command. Expected: PASS.

- [ ] **Step 5: Add failing fingerprint and canonical-order tests**

Assert the ASIN-less key is `amazon:asinless:` plus 64 lowercase SHA-256 hex characters over the
normalized product collision key. Assert identity fingerprints exclude Unit Price and statuses,
full fingerprints include them, missing Currency is normalized before hashing, and candidate order
is bytewise relative filename then logical record regardless of directory traversal.

Run:

```bash
go test ./internal/importer/amazon -run 'Test(Fingerprint|ASINLess|Canonical)' -count=1
```

Expected: FAIL until digest construction is versioned and length-delimited.

- [ ] **Step 6: Implement fingerprints, candidate digest, cancellation, and progress**

Use SHA-256 over a version tag and length-prefixed UTF-8/binary fields. Check `ctx.Err()` between
records and files. Emit counts-only progress. Return `Candidate` with sorted unique observed order
IDs and a canonical digest that excludes filenames and record coordinates.

Run:

```bash
go test ./internal/domain ./internal/importer/amazon -count=1
```

Expected: PASS.

- [ ] **Step 7: Run the Required Commit Gate and commit**

Commit with subject:

```text
feat: parse Amazon order history safely
```

---

## Task 2: Install Version-Eight Amazon Storage and Catalog Support

**Files:**

- Modify: `internal/home/lock.go`
- Modify: `internal/home/lock_test.go`
- Create: `internal/home/private_amazon.go`
- Create: `internal/home/private_amazon_test.go`
- Modify: `internal/profilecatalog/manifest.go`
- Modify: `internal/profilecatalog/manifest_test.go`
- Modify: `internal/profilecatalog/create.go`
- Modify: `internal/profilecatalog/create_test.go`
- Modify: `internal/profilecatalog/discovery.go`
- Modify: `internal/profilecatalog/catalog_test.go`
- Modify: `internal/store/store.go`
- Modify: `internal/store/store_test.go`
- Modify: `internal/store/sqlite/initialize.go`
- Modify: `internal/store/sqlite/schema/profile.sql`
- Modify: `internal/store/sqlite/schema_test.go`
- Modify: `internal/store/sqlite/open.go`
- Create: `internal/store/sqlite/amazon_state.go`
- Create: `internal/store/sqlite/amazon_state_test.go`
- Create: `internal/store/sqlite/amazon_import.go`
- Create: `internal/store/sqlite/amazon_import_test.go`
- Create: `internal/store/sqlite/amazon_import_failure_test.go`

**Interfaces:**

- Consumes: current catalog lifecycle, `store.Profile`, strict SQLite schema, and home locking.
- Produces: version-eight tables plus all Task 2 Cross-Task store types and methods.

- [ ] **Step 1: Add failing lock and catalog-kind tests**

Assert `LockAmazonImport` maps to `amazon-import.lock`, is nonblocking, releases after process
death, supports immediate same-process reuse, and remains independent of `LockExport`. Extend
manifest/create/discovery tests so `amazon` is accepted and listed as setup-incomplete or ready,
while unknown kinds remain rejected.

Run:

```bash
go test ./internal/home ./internal/profilecatalog -run 'Test.*(Amazon|Lock)' -count=1
```

Expected: FAIL because the lock and kind do not exist.

- [ ] **Step 2: Implement the lock, private stage root, and catalog support**

Add `LockAmazonImport` without changing existing numeric lock identities. Add private stage helpers
that create `0700` directories and `0600` files with rooted no-symlink checks, bounded stale-stage
cleanup, close-before-remove, and brief Windows sharing-violation retry. Accept `amazon` in every
manifest and creation validator. Preserve cancel-new-profile's pristine-only guard.

Run the Step 1 command. Expected: PASS.

- [ ] **Step 3: Add failing version-eight schema tests**

Assert `CurrentSchemaVersion == 8`; version 7 and 9 are refused. Assert strict tables
`amazon_profile_settings`, `amazon_order_items`, and `amazon_import_history` exist with no `REAL`
columns. Add checks for singleton settings, currency/scale, opaque unique source identity,
active/retired shape, nullable unit price, canonical digests, nonnegative counts, and counts-only
history columns. Assert provider binding/lease/write rows are absent from an Amazon profile's local
state even though shared schema tables remain installed.

Run:

```bash
go test ./internal/store/sqlite -run 'Test(CurrentSchema|Schema|Amazon)' -count=1
```

Expected: FAIL on schema version and missing tables.

- [ ] **Step 4: Install the exact schema and neutral store types**

Increment `CurrentSchemaVersion` to 8 in the same change as `profile.sql`. Add the Task 2 interface
types to `internal/store`; extend test fakes explicitly rather than adding an adapter that hides
missing methods. Do not add any migration or compatibility decoder.

Run the Step 3 command. Expected: schema tests PASS.

- [ ] **Step 5: Add failing atomic load/apply tests**

Use a passthrough planner to prove first install, settings immutability, ledger retirement and
restoration, retained transaction external identities, canonical label allocations, history
append, no-op history outside revision, revision advancement on semantic change, and reopened
logical equality. Inject a failure before each table write and assert settings, committed rows,
ledger, journal, cursor, drills, allocations, revision, and history all remain unchanged.

Run:

```bash
go test ./internal/store/sqlite -run 'TestAmazon(Import|State|Failure)' -count=1
```

Expected: FAIL until atomic methods exist.

- [ ] **Step 6: Implement closed-callback load and atomic application**

Inside one `BEGIN IMMEDIATE`, load `AmazonImportState`, supply preallocated opaque entity/source IDs,
invoke the planner without SQL access, validate its complete plan, apply it, and append history.
Increment revision only when `SemanticChange` is true. Generate `amazon_item_` plus lowercase
base32 of 128 random bits for new source identities. Do not expose SQL rows or driver types.

Run:

```bash
go test ./internal/store ./internal/store/sqlite ./internal/profilecatalog ./internal/home -count=1
```

Expected: PASS.

- [ ] **Step 7: Run the Required Commit Gate and commit**

Commit with subject:

```text
feat: install Amazon profile storage
```

---

## Task 3: Reconcile Amazon Candidates Without Moving User Intent

**Files:**

- Create: `internal/app/amazon_reconcile.go`
- Create: `internal/app/amazon_reconcile_test.go`
- Create: `internal/app/amazon_reconcile_property_test.go`
- Create: `internal/app/amazon_import.go`
- Create: `internal/app/amazon_import_test.go`
- Create: `internal/app/amazon_import_performance_test.go`
- Modify: `internal/app/profile_service.go`
- Modify: `internal/app/capabilities.go`
- Modify: `internal/app/capabilities_test.go`
- Modify: `internal/app/actions.go`
- Modify: `internal/app/actions_test.go`
- Modify: `internal/replay/provider_rebase.go`
- Modify: `internal/replay/provider_rebase_test.go`

**Interfaces:**

- Consumes: Tasks 1-2 candidates and store callback, reference replay, label allocation, taxonomy,
  and deterministic target-removal journal rewrite.
- Produces: `Service.ImportAmazon`, `BuildAmazonImportPlan`, Amazon capability policy, and the exact
  reconciliation invariant.

- [ ] **Step 1: Add failing tier-order and observed-order tests**

Cover exact active matches, exact retired restoration, one unequal real-ASIN singleton, one
unequal order-wide ASIN-less singleton with changed key, ambiguous many-to-many replacement,
observed shrink, absent order, fully cancelled order, and partial cancellation. Add the load-bearing
case: exact retired A reappears beside changed active B; A restores before B can singleton-pair.

Run:

```bash
go test ./internal/app -run 'TestAmazonReconcile' -count=1
```

Expected: FAIL because the planner is missing.

- [ ] **Step 2: Implement the five pairing tiers and field authority**

Partition by order, consume incoming rows once, and apply the fixed tier order from Global
Constraints. For equal duplicates, pair bytewise local ID against bytewise filename/record order.
Allocate one merchant per real ASIN and one shared merchant per newly allocated ASIN-less key.
Refresh source-owned facts while retaining user category, hidden, notes, and user-touched merchant.
Restore retired ledger and external identity rows; never reuse a retired ID for a different row.

Run the Step 1 command. Expected: PASS.

- [ ] **Step 3: Add failing journal, taxonomy, and no-op tests**

Assert retired targets shrink operations without changing surviving order or operation identity;
empty operations disappear; cursor counts decrement correctly; structural operations sweep current
membership; inactive redo handling matches provider rebase. Assert first import installs default
sentinels or a fresh-ID committed-only clone, clone is rejected later, and no-op import preserves
every committed table and revision while appending one history row.

Run:

```bash
go test ./internal/app -run 'TestAmazon(Import|Journal|Taxonomy|NoOp)' -count=1
```

Expected: FAIL until `Service.ImportAmazon` composes the store callback.

- [ ] **Step 4: Implement the application import service and kind-aware capability**

Build the complete plan entirely from callback arguments and the captured candidate. Reuse the
existing rebase target-removal primitive, replay the result, and validate referential integrity
before returning. Configure Amazon profiles for local commit and full editing. Return
`provider.refresh` as interactive `Import Amazon orders`; do not schedule it or create provider
state. Keep Monarch metadata unchanged and local profiles unavailable with a reason.

Run the Step 3 command. Expected: PASS.

- [ ] **Step 5: Add the randomized reconciliation property test**

Generate status changes, one-row price changes, product-label edits, input reorderings, duplicate
multiplicity, retirement, and reappearance. Assert unequal fingerprints pair only through the two
singleton tiers, exact retired evidence wins before singleton inference, unchanged reimport is a
universal no-op, and stable local IDs never cross physical rows.

Run:

```bash
go test ./internal/app -run 'TestAmazonReconcileProperty' -count=20
```

Expected: PASS across randomized seeds.

- [ ] **Step 6: Add and meet the 100,000-row planning/fold gates**

Use deterministic synthetic rows. Require pure parse plus planning within one second and atomic
SQLite application within ten seconds under the repository performance mode; skip only through the
existing race/short mechanism.

Run:

```bash
go test ./internal/app ./internal/store/sqlite -run 'TestAmazon.*100KPerformance' -count=1
```

Expected: PASS in the supported performance environment.

- [ ] **Step 7: Run the Required Commit Gate and commit**

Commit with subject:

```text
feat: reconcile Amazon profiles atomically
```

---

## Task 4: Coordinate Imports and Add the Cobra Workflow

**Files:**

- Create: `internal/amazonimport/types.go`
- Create: `internal/amazonimport/errors.go`
- Create: `internal/amazonimport/coordinator.go`
- Create: `internal/amazonimport/coordinator_test.go`
- Create: `internal/amazonimport/attempts.go`
- Create: `internal/amazonimport/attempts_test.go`
- Create: `internal/amazonimport/staging.go`
- Create: `internal/amazonimport/staging_test.go`
- Modify: `internal/provider/architecture_test.go`
- Create: `cmd/moneyflow/amazon.go`
- Create: `cmd/moneyflow/amazon_test.go`
- Modify: `cmd/moneyflow/provider.go`
- Modify: `cmd/moneyflow/root.go`
- Modify: `cmd/moneyflow/profile.go`

**Interfaces:**

- Consumes: profile catalog/opener, home lifecycle/import locks and stages, parser, and
  `Service.ImportAmazon`.
- Produces: the Task 4 `Coordinator` protocol and
  `moneyflow provider import amazon <directory>`.

- [ ] **Step 1: Add failing coordinator lock/lifecycle tests**

Assert contention returns `amazon_import_busy` before directory traversal or body consumption;
locks acquire catalog during creation, shared lifecycle, then import and never reverse; process
death releases; sequential reuse succeeds; import and export locks can coexist. Assert attempt IDs
are bound to `(server instance, profile ID)`, state versions reject stale actions, 30-minute expiry
counts active jobs/uploads/status as activity, and status is coordinate-blind.

Run:

```bash
go test ./internal/amazonimport -run 'Test(Coordinator|Attempt|Lock)' -count=1
```

Expected: FAIL because the coordinator does not exist.

- [ ] **Step 2: Implement coordinator state, progress, and cancellation**

Acquire the import lock before touching a source. Keep detailed coordinates only on the initiating
`Execute` return path. If cancellation/disconnect is observed before `ImportAmazon` enters the
authoritative transaction, cancel and clean stages. Once the transaction starts, let it commit or
roll back and publish the terminal counts-only status. Roll back a newly created pristine profile
on cancel/failure; never remove an existing profile.

Run the Step 1 command. Expected: PASS.

- [ ] **Step 3: Add failing private upload-stage tests**

Stream multiple files through injected small bounds and assert `0600` files, valid relative names,
bounded memory, duplicate rejection, and cleanup after success, parse failure, cancellation,
request error, disconnect, Windows sharing violation, and next-attempt stale cleanup.

Run:

```bash
go test ./internal/amazonimport ./internal/home -run 'TestAmazon(Stage|Cleanup)' -count=1
```

Expected: FAIL until stage management is connected.

- [ ] **Step 4: Implement staging and the architecture boundary**

Keep the provider-specific stage format inside `internal/amazonimport`, using shared hardened home
helpers. Enforce that presenters do not parse and `internal/amazonimport` never imports
`internal/store/sqlite`; only this coordinator and command factory wiring may compose catalog,
parser, home, and app import dependencies.

Run the Step 3 command plus:

```bash
go test ./internal/provider -run TestProviderArchitecture -count=1
```

Expected: PASS.

- [ ] **Step 5: Add failing Cobra contract tests**

Cover create-on-missing, exact ID before unique normalized name, ambiguous-name refusal, explicit
currency confirmation with USD/2 defaults, existing immutable settings, conflicting flag refusal,
clone-only-on-create, progress counts, actionable active-row coordinate, cancellation, no-op
completion, profile rollback, and absence of labels/order IDs/amounts in captured logs.

Run:

```bash
go test ./cmd/moneyflow -run 'TestAmazonImportCommand' -count=1
```

Expected: FAIL because the command is absent.

- [ ] **Step 6: Implement the Cobra presenter**

Register `provider import amazon <directory>` with `--profile`, `--currency`, `--scale`, and
`--clone-taxonomy-from`. Prompt for missing creation settings, call only the coordinator, print
counts-only progress, show the allowed coordinate directly to the terminal, and print the next
`moneyflow tui --profile ...` step after success.

Run:

```bash
go test ./internal/amazonimport ./cmd/moneyflow -count=1
```

Expected: PASS.

- [ ] **Step 7: Run the Required Commit Gate and commit**

Commit with subject:

```text
feat: import Amazon profiles from Cobra
```

---

## Task 5: Build Cross-Profile Matching and Transaction Information

**Files:**

- Create: `internal/analytics/amazon_matching.go`
- Create: `internal/analytics/amazon_matching_test.go`
- Create: `internal/app/amazon_matching.go`
- Create: `internal/app/amazon_matching_test.go`
- Create: `internal/app/amazon_matching_performance_test.go`
- Create: `internal/app/transaction_info.go`
- Create: `internal/app/transaction_info_test.go`
- Modify: `internal/app/service.go`
- Modify: `internal/app/web.go`
- Modify: `internal/app/web_test.go`
- Modify: `internal/store/store.go`
- Modify: `internal/store/sqlite/amazon_state.go`

**Interfaces:**

- Consumes: committed finance transactions, committed Amazon source snapshots, catalog entries,
  short-lived profile openers, raw provider merchant labels, and exact integer money.
- Produces: cached match indexes, Task 5 `TransactionInfo`, product search enrichment, and bounded
  table match indicators.

- [ ] **Step 1: Add failing pure matching tests**

Test scales 0 through 9 for the 0.02 tolerance; inclusive ±7 days; expense-only fuzzy direction;
`15 * 10^scale` versus exact 10% cross-multiplication; global pass exclusivity across profiles;
confidence thresholds; deterministic ordering/deduplication; cap 20 plus total; and stable first
product. Name the Python `$10` docstring versus implemented `$15` rule in test comments.

Run:

```bash
go test ./internal/analytics -run 'TestAmazonMatch' -count=1
```

Expected: FAIL because the matcher is missing.

- [ ] **Step 2: Implement the pure three-pass matcher**

Build order totals and item indexes with overflow-checked integer arithmetic. Evaluate all usable
profiles for a pass before deciding whether to continue. Use exact order, fuzzy order, then exact
item. Assign `high`, `medium`, and `likely` exactly as specified; sort with bytewise profile/order/
transaction ID tie-breaks and return no more than 20 results plus the authoritative total.

Run the Step 1 command. Expected: PASS.

- [ ] **Step 3: Add failing source-selection/cache tests**

Assert only current-schema Amazon profiles with matching currency/scale participate; corrupt,
incompatible, newer, missing, and mismatched profiles are skipped with stable counts-only reasons.
Assert source handles close before projection, cache keys are `(profile ID, semantic revision)`,
catalog removal/revision invalidates, and concurrent source changes produce either old or new
committed snapshots rather than a mixture.

Run:

```bash
go test ./internal/app -run 'TestAmazonMatch(Service|Cache|Sources)' -count=1
```

Expected: FAIL until the application service exists.

- [ ] **Step 4: Implement the application matching service**

Read committed-only source facts through a narrow store method under a short-lived profile handle.
Never hold two profile transactions or a cross-profile lease. Qualify finance rows when either
effective display merchant or raw provider label contains Unicode-lowercased `amazon` or `amzn`.
Cache immutable indexes and evict by revision/catalog presence.

Run the Step 3 command. Expected: PASS.

- [ ] **Step 5: Add failing transaction-info, column, and search tests**

Assert Amazon profiles expose ASIN, quantity, status, shipment, and optional unit price without
showing ASIN-less digests. Assert non-Amazon details return bounded matches; the match column exists
only when every current detail row qualifies; the first product is deterministic. Assert product
search is a Unicode-lowercased substring over raw product names and combines with existing filters
identically in web and session projections.

Run:

```bash
go test ./internal/app -run 'Test(TransactionInfo|AmazonColumn|AmazonProductSearch)' -count=1
```

Expected: FAIL until projection wiring is complete.

- [ ] **Step 6: Implement bounded transaction information and search enrichment**

Return exact money, stable local targets, order facts, authoritative totals, and bounded match/item
windows. Keep order IDs out of cache keys, URLs, and logs. Extend the table projection with only a
bounded best-match indicator and first product string; do not persist selected matches.

Run:

```bash
go test ./internal/analytics ./internal/app ./internal/store/... -count=1
```

Expected: PASS.

- [ ] **Step 7: Add and meet the 100,000-row match/search gate**

Require index build/reuse plus one bounded projection within one second, and product search within
the existing one-second bounded projection ceiling.

Run:

```bash
go test ./internal/app -run 'TestAmazon(Matching|Search)100KPerformance' -count=1
```

Expected: PASS in the supported performance environment.

- [ ] **Step 8: Run the Required Commit Gate and commit**

Commit with subject:

```text
feat: match finance transactions to Amazon orders
```

---

## Task 6: Add Amazon Onboarding, Repeat Import, and Details to the TUI

**Files:**

- Modify: `internal/tui/provider_selector.go`
- Modify: `internal/tui/provider_selector_test.go`
- Create: `internal/tui/amazon_import.go`
- Create: `internal/tui/amazon_import_test.go`
- Create: `internal/tui/amazon_import_format.go`
- Create: `internal/tui/amazon_import_format_test.go`
- Modify: `internal/tui/shell.go`
- Modify: `internal/tui/shell_test.go`
- Modify: `internal/tui/model.go`
- Modify: `internal/tui/update.go`
- Modify: `internal/tui/keymap.go`
- Modify: `internal/tui/help_test.go`
- Modify: `internal/tui/columns.go`
- Modify: `internal/tui/columns_test.go`
- Modify: `internal/tui/transaction_info.go`
- Modify: `internal/tui/transaction_info_test.go`
- Modify: `internal/tui/search.go`
- Modify: `internal/tui/search_test.go`
- Modify: `cmd/moneyflow/tui_shell.go`
- Modify: `cmd/moneyflow/root_test.go`

**Interfaces:**

- Consumes: Task 4 coordinator snapshots and Task 5 transaction-information projections.
- Produces: selector-first Amazon creation, `r` repeat import, Product/Order columns, and enriched
  `i` overlay without renderer-owned parsing or matching.

- [ ] **Step 1: Add failing provider-selector and wizard tests**

Drive `a` to Amazon, profile name, always-visible currency/scale with USD/2 preselected, Advanced
taxonomy source, editable directory path, parsing/installing progress, Cancel, retryable error,
actionable coordinate, success open, and pristine-profile rollback. Assert secrets/credential
screens never appear.

Run:

```bash
go test ./internal/tui -run 'TestAmazon(Onboarding|Import)' -count=1
```

Expected: FAIL because Amazon is not selectable.

- [ ] **Step 2: Implement the TUI presenter over the coordinator**

Add Amazon to the provider selector and let the shell own the attempt lifecycle. Keep path text
only in the active form. Map coordinator progress/counts and failures into Bubble Tea messages.
Cancel before installation rolls back a new profile; completion opens the returned profile without
restart.

Run the Step 1 command. Expected: PASS.

- [ ] **Step 3: Add failing repeat-import and capability/help tests**

Assert `r` on Amazon opens the chooser, preserves analytical state/cursor/scroll after success,
clears all selection if any selected transaction retires, and never schedules itself. Assert
Monarch `r` still says `Refresh provider data` and follows the existing scheduler; local remains
disabled with a reason.

Run:

```bash
go test ./internal/tui ./internal/app -run 'Test.*(RefreshAction|AmazonRepeatImport|AmazonSelection)' -count=1
```

Expected: FAIL until kind-aware routing exists.

- [ ] **Step 4: Implement repeat import and kind-aware help**

Route the stable action by capability interaction kind. Reproject the current view after the
coordinator reports complete, restore cursor by stable identity when present, and apply the
all-or-nothing selection rule. Do not start a ticker or provider runtime for Amazon.

Run the Step 3 command. Expected: PASS.

- [ ] **Step 5: Add failing Product/Order, match-column, info, and search tests**

Assert Amazon detail columns read Product and Order. Assert the match column appears only for an
all-qualifying finance result. Drive `i` and close it while preserving cursor/scroll/selection;
assert bounded matches and Amazon facts. Verify product-name search parity and no ASIN-less digest
display.

Run:

```bash
go test ./internal/tui -run 'TestAmazon(Columns|Info|Match|Search)' -count=1
```

Expected: FAIL until presentation is wired.

- [ ] **Step 6: Implement responsive formatting and overlay behavior**

Use existing cell-width and overlay primitives. Render exact money and bounded match counts, and
show a stable `more` indicator when totals exceed the window. Keep match computation in app. Add
semantic frame fixtures only in Task 8.

Run:

```bash
go test ./internal/tui ./cmd/moneyflow -count=1
```

Expected: PASS.

- [ ] **Step 7: Run the Required Commit Gate and commit**

Commit with subject:

```text
feat: add Amazon workflows to the TUI
```

---

## Task 7: Add Protected Web Import and Accessible Matching Details

**Files:**

- Create: `internal/api/amazon_import.go`
- Create: `internal/api/amazon_import_test.go`
- Create: `internal/api/transaction_info.go`
- Create: `internal/api/transaction_info_test.go`
- Modify: `internal/api/server.go`
- Modify: `internal/api/security.go`
- Modify: `internal/api/openapi_test.go`
- Modify: `internal/api/profiles.go`
- Modify: `internal/web/server.go`
- Modify: `cmd/moneyflow/web_dependencies.go`
- Create: `web/src/lib/controller/amazon-import.ts`
- Create: `web/src/lib/controller/amazon-import.test.ts`
- Create: `web/src/components/profiles/AmazonImportWizard.svelte`
- Create: `web/src/components/profiles/AmazonImportWizard.test.ts`
- Create: `web/src/components/editing/TransactionInfoDrawer.svelte`
- Create: `web/src/components/editing/TransactionInfoDrawer.test.ts`
- Modify: `web/src/components/profiles/ProviderSelector.svelte`
- Modify: `web/src/components/FinanceTable.svelte`
- Modify: `web/src/lib/controller/view-controller.svelte.ts`
- Modify: `web/src/lib/api/client.ts`
- Modify: `web/src/lib/api/schema.d.ts`
- Create: `web/tests/amazon-import.spec.ts`
- Create: `web/tests/amazon-matching.spec.ts`

**Interfaces:**

- Consumes: Task 4 attempt protocol, Task 5 transaction info, existing profile-keyed routing,
  mutation security, read-only POST conventions, and kit-ui.
- Produces: the fixed `/amazon-import/...` HTTP workflow, web onboarding/reimport, and accessible
  transaction details.

- [ ] **Step 1: Add failing protected endpoint tests**

Cover start/files/execute/status/cancel paths exactly. Assert start, upload, execute, and cancel
require mutation token, canonical Origin, and Fetch Metadata; attempts are profile/instance-bound;
all mutations use state-version CAS; status is read-only/counts-only; uploads stream under limits;
and raw multipart values never enter Huma problems or logs.

Run:

```bash
go test ./internal/api -run 'TestAmazonImport' -count=1
```

Expected: FAIL because routes are absent.

- [ ] **Step 2: Implement the HTTP attempt lifecycle**

Register the five fixed endpoints beneath profile-scoped routes and extend the strict path/security
allowlists. Stream multipart files directly into coordinator stages. Return the one actionable
coordinate only from the initiating execute response. If disconnect occurs before the immediate
transaction, cancel; after it begins, let atomic completion appear in status.

Run the Step 1 command. Expected: PASS.

- [ ] **Step 3: Add failing read-only transaction-information API tests**

Assert the POST accepts wire version, canonical query, expected revision, stable local target, and
bounded windows without a mutation token. Assert same-origin/no-CORS/no-store behavior, exact money
strings, authoritative totals, bounded results, and no order IDs in URL, history, or logs.

Run:

```bash
go test ./internal/api -run 'TestTransactionInformation' -count=1
```

Expected: FAIL until the projection endpoint exists.

- [ ] **Step 4: Implement the bounded read endpoint and regenerate types**

Map app errors through stable generic problems and keep the response detached. Register the route
as read-only POST. Regenerate the OpenAPI and frontend schema through the repository target; inspect
the diff and do not hand-edit generated declarations.

Run:

```bash
make web-generate
go test ./internal/api -run 'Test(TransactionInformation|OpenAPI)' -count=1
```

Expected: PASS with generated API types current.

- [ ] **Step 5: Add failing Svelte controller/component tests**

Cover Amazon provider choice, currency confirmation, Advanced clone source, directory-capable and
multiple-file fallback, progress, cancel, retry, coordinate display, repeat import, profile open,
keyboard `i`, Enter, double-click, Details control, focus restoration, live announcements, bounded
match items, and query/cursor/selection preservation.

Run:

```bash
bun test --cwd web --filter 'amazon-import|TransactionInfoDrawer'
```

Expected: FAIL because the controller/components are missing.

- [ ] **Step 6: Implement the kit-ui wizard and detail drawer**

Use the controller as the only HTTP owner. Keep selected filenames in component memory only and
clear file handles after terminal state. Buffer no complete upload in JavaScript beyond browser
`File` objects. Add keyboard and accessible labels using kind-aware capability text. Reuse the
canonical analytical query and stable target for details; never store match selection in URL.

Run the Step 5 command. Expected: PASS.

- [ ] **Step 7: Add browser security, cleanup, and accessibility journeys**

Run full create/import/reimport/details/search flows in Chromium. Tag upload cleanup, keyboard `i`,
and accessibility cases `@smoke` for Firefox and WebKit. Assert a failed active row displays its
coordinate only in the initiating UI while status, problem, logs, history, and metadata contain no
filename, label, or coordinate.

Run:

```bash
make web-build
bunx --cwd web playwright test web/tests/amazon-import.spec.ts web/tests/amazon-matching.spec.ts --project=chromium
bunx --cwd web playwright test --grep @smoke --project=firefox --project=webkit
```

Expected: PASS with no tracked screenshots or distribution files.

- [ ] **Step 8: Run the Required Commit Gate and commit**

Commit with subject:

```text
feat: add Amazon import and matching to web
```

---

## Task 8: Close Parity, Privacy, Performance, and Repository Gates

**Files:**

- Modify: `tests/parity/test_semantic.py`
- Modify: `internal/parity/semantic_test.go`
- Modify: `internal/tui/semantic_parity_test.go`
- Modify: `internal/tui/visual_golden_test.go`
- Modify: `Makefile`
- Modify: `README.md`
- Modify: `AGENTS.md` only if command guidance needs correction
- Modify: reviewed parity artifacts produced by `make parity-update-python` and
  `make parity-update-go`

**Interfaces:**

- Consumes: completed Tasks 1-7 and every completion criterion in the approved design.
- Produces: reviewed semantic/visual parity artifacts, full cross-platform gates, documented user
  commands, and a clean implementation branch.

- [ ] **Step 1: Add and review Python semantic Amazon frames**

Drive synthetic non-private Python selector, Amazon detail, all-Amazon finance detail with match
column, product search, and matching-order information scenarios. Run the deliberate artifact
update and inspect every changed cell; do not update ordinary tests implicitly.

Run:

```bash
make parity-update-python
git diff -- tests/parity internal/parity
```

Expected: only the five approved synthetic frame families change.

- [ ] **Step 2: Add Go semantic and visual parity frames**

Capture the equivalent TUI states, including Product/Order labels, kind-aware `r`, match column,
and `i` details. Run the deliberate Go update and inspect previews. Do not commit screenshots.

Run:

```bash
make parity-update-go
make parity
git status --short
```

Expected: parity passes; no `web/tests/screenshots`, `web/dist`, or `internal/web/dist` is tracked.

- [ ] **Step 3: Add the final privacy and architecture scans**

Assert the initiating TUI/web coordinate split end-to-end and scan captured logs, status, Huma
problems, SQLite history, profile metadata, fixtures, and generated schemas. Extend architecture
tests to prove store/importer/presenter dependency directions and absence of Monarch/Amazon API
coupling.

Run:

```bash
go test ./internal/provider ./internal/amazonimport ./internal/api ./internal/tui -run 'Test.*(Architecture|Privacy|Coordinate)' -count=1
```

Expected: PASS with only the permitted direct UI coordinate.

- [ ] **Step 4: Run performance and race gates**

Run the 100,000-row parser/planner, store fold, matching-index, and search tests, then the race
detector. Record measured durations in the verification report, not commit messages.

Run:

```bash
make test-store
make test-race
```

Expected: all correctness gates pass; performance tests meet their supported ceilings outside
race/short mode.

- [ ] **Step 5: Update current user documentation**

Document `moneyflow provider import amazon <directory>`, Amazon profile creation in TUI/web,
repeat `r`, local-only commit, optional one-time taxonomy clone, supported official CSV fields,
currency restriction, and matching behavior. Do not modify historical specs/plans or describe
Python commands as Go commands.

Run:

```bash
npx --yes markdownlint-cli@0.47.0 --config .markdownlint.json README.md 'docs/**/*.md'
.github/scripts/check-arrow-lists.sh
```

Expected: PASS.

- [ ] **Step 6: Run every final repository gate**

Run:

```bash
make verify-go
make verify-web
make test-editing-e2e
make parity
uv run pytest -v
uv run pyright moneyflow/
uv run ruff format --check moneyflow/ tests/
uv run ruff check moneyflow/ tests/
npx --yes markdownlint-cli@0.47.0 --config .markdownlint.json README.md 'docs/**/*.md'
.github/scripts/check-arrow-lists.sh
git diff --check
```

Expected: every command exits zero. Inspect `git diff HEAD`, `git status --short`, tracked files,
and unpushed commits for private data, generated distributions, screenshots, raw CSVs, credentials,
and unintended Amazon network/write code.

- [ ] **Step 7: Commit the final integration checkpoint**

Commit with subject:

```text
feat: complete Amazon import and matching parity
```

## Final Completion Audit

Before reporting completion, read the design and this plan from top to bottom and verify:

- Cobra, TUI, and web all create and reimport the same version-eight Amazon profile through the
  shared coordinator.
- Overlapping, reordered, corrected, cancelled, retired, and restored orders follow the exact
  pairing tiers and never move user-owned state by weak inference.
- An unchanged reimport preserves committed data and revision while recording one counts-only
  operational success.
- Amazon profiles reopen offline and retain local edit/undo/redo/commit, duplicate review, export,
  Product/Order presentation, search, matching, and transaction information.
- Cross-profile matching is committed-only, exact-money, globally pass-exclusive, bounded,
  deterministic, and tolerant of unusable source profiles.
- Import lock, lifecycle order, private stages, process-death recovery, disconnect boundary,
  revision atomicity, rollback, and cleanup behave identically across presenters.
- Persistent outputs contain no filename, coordinate, order ID, ASIN, label, amount, raw row, or
  search text; only the initiating session receives an actionable coordinate.
- Version 8 is install-only and the repository contains no migration.
- The final diff contains no scraping, Amazon API, credential, provider-write, raw-retention,
  persisted-match, distribution, or screenshot artifact.

After the audit, use `superpowers:verification-before-completion` before claiming the slice is
finished. Keep live user CSV validation separate from automated completion: commit the verified
change first, then record any live-discovered correction in a new commit.
