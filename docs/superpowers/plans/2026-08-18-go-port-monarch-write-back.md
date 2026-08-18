# Go Port Monarch Write-Back Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development
> (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use
> checkbox (`- [ ]`) syntax for tracking.

**Status:** Ready for implementation

**Goal:** Make `w`, then `Enter`, durably write supported pending merchant, mapped-category, and
hidden-state edits to Monarch from both the TUI and web interface.

**Architecture:** Freeze the reviewed journal prefix and deterministic absolute transaction items
inside SQLite before network work begins. A lease-guarded application worker sends one-attempt
provider writes, persists each normalized response, and atomically finalizes the response-adjusted
effective state; complete refresh remains the authority for deferred provider identity overrides.

**Tech Stack:** Go 1.26.3; `modernc.org/sqlite` v1.56.0; Huma v2.38.0 with `humago`; Cobra
1.10.2; Bubble Tea 2.0.8; Svelte 5.56.3 in runes mode; Bun 1.3.14;
`@kenn-io/kit-ui` commit `16db58ef8122dd00e21ce8ad90ba295b9174c6ef`; Testify; Vitest
4.1.10; Playwright 1.61.1.

## Global Constraints

- Work only on the checked-out `go-port` branch. Do not switch branches, pull, rebase, push,
  merge, amend, or remove Python without explicit user permission.
- Follow TDD for every behavior change: write the test, observe the intended failure, implement the
  smallest behavior, and rerun the focused and package tests.
- Commit every verified task before starting the next. Stage only that task's files and never
  commit screenshots, `web/dist`, or `internal/web/dist`.
- Install schema version 5 only into an empty database. Refuse version 4 and every mismatch; do not
  add schema or journal-payload migrations while Go v2 is unstable.
- Keep pure-Go SQLite and the existing no-CGO Linux, macOS, and Windows contract.
- Keep all money in signed integer minor units. Do not add `float32`, `float64`, or SQLite `REAL`.
- `internal/provider/monarch` imports provider/domain but never store. Store imports domain/replay
  but never provider. Renderers never import Monarch.
- A provider writer performs one HTTP attempt. Only the durable application worker owns retries.
- Network I/O never runs inside a SQLite transaction. Every write/fold transaction validates the
  batch version, lease owner/kind, and relevant revision/generation before changing state.
- The operation lease is liveness only. Transactional compare-and-swap guards are correctness.
- Batch requests are absolute desired values and are safe to resend after an unknown outcome.
- Existing and merge-destination merchant response mismatches are provider overrides. New-name
  groups remain strict because their response establishes mapping ownership.
- Keep `w`, then `Enter`, as the only ordinary confirmation. Do not add a second confirmation.
- Persist/log only allowlisted codes, revisions, generations, batch versions, counts, bounded
  timings, renderer class, opaque instance IDs, and correlation IDs.
- Never expose or log labels, transaction/provider/household IDs, requested fields, GraphQL data,
  credentials, session tokens, paths, URLs, search text, or financial values.
- Live Monarch dogfooding happens only after all automated changes are committed. Use synthetic
  loopback transports and temporary profiles for automated work.

## Target File Map

```text
internal/store/store.go                         durable lease/batch/item contracts
internal/store/sqlite/schema/profile.sql        exact schema version 5
internal/store/sqlite/provider_state.go         generalized operation lease and status
internal/store/sqlite/provider_write.go         prepare/result/finalize/reconcile transactions
internal/store/sqlite/provider_refresh.go       no-batch refresh-fold guard
internal/provider/provider.go                   provider-neutral Writer request/result
internal/provider/errors.go                     stable write error classifications
internal/provider/monarch/queries.go             exact update-transaction mutation
internal/provider/monarch/write.go               one-attempt mutation and normalization
internal/provider/monarch/session_file.go        Source.Writer session reuse
internal/app/provider_write_plan.go              pure absolute item planning
internal/app/provider_write_identity.go          provider response and lineage planning
internal/app/provider_write.go                   worker, retry, pause/resume, finalization
internal/app/provider_write_reconcile.go         stop-and-reconcile and confirmation
internal/app/provider_scheduler.go               exhaustive write scheduling classes
internal/app/capabilities.go                     Monarch and active-batch availability
internal/tui/provider_write.go                   async worker/status commands and overlay
internal/tui/review.go                           direct provider preparation on Enter
internal/api/provider_write.go                   protected profile-scoped write endpoints
web/src/lib/controller/provider-write.ts         browser write state machine
web/src/components/editing/WriteStatusDrawer.svelte  accessible status/actions
web/src/components/editing/ReviewDrawer.svelte   direct preparation and progress handoff
web/tests/provider-write.spec.ts                 browser write/restart/security journeys
```

## Required Commit Gate

Run each task's focused red/green command while developing. Before every task commit, run:

```bash
make verify-go
uv run pytest -v
uv run pyright moneyflow/
uv run ruff format --check moneyflow/ tests/
uv run ruff check moneyflow/ tests/
npx --yes markdownlint-cli@0.47.0 --config .markdownlint.json README.md 'docs/**/*.md'
.github/scripts/check-arrow-lists.sh
```

Expected: every command exits zero. Beginning with Task 6, also run:

```bash
bun install --cwd web --frozen-lockfile
make verify-web
```

Expected: the lockfile is unchanged and generated API/types/assets are current. If the supported
performance gate is invalid because of host load, run the repository's non-performance verification
path, record the exact skipped timing gate, and commit the otherwise verified task.

---

### Task 1: Install Durable Write State and Close the Refresh Race

**Files:**

- Modify: `internal/store/store.go`
- Modify: `internal/store/store_test.go`
- Modify: `internal/store/sqlite/schema/profile.sql`
- Modify: `internal/store/sqlite/initialize.go`
- Modify: `internal/store/sqlite/initialize_test.go`
- Modify: `internal/store/sqlite/schema_test.go`
- Modify: `internal/store/sqlite/provider_state.go`
- Modify: `internal/store/sqlite/provider_state_test.go`
- Create: `internal/store/sqlite/provider_write.go`
- Create: `internal/store/sqlite/provider_write_test.go`
- Create: `internal/store/sqlite/provider_write_failure_test.go`
- Create: `internal/store/sqlite/provider_write_concurrency_test.go`
- Modify: `internal/store/sqlite/provider_refresh.go`
- Modify: `internal/store/sqlite/provider_refresh_concurrency_test.go`
- Modify: `internal/store/sqlite/failure_test.go`

**Interfaces:**

- Consumes: `domain.ProfileSnapshot`, the active journal cursor, refresh generation, and the
  existing SQLite immediate-transaction helper.
- Produces: `store.ProviderOperationLease`, `store.WriteBatch`, `store.WriteItem`,
  `store.PrepareProviderWrite`, `store.RecordProviderWriteResult`, `store.FinalizeProviderWrite`,
  and generalized lease methods on `store.Profile`.

- [ ] **Step 1: Write failing schema, lease, preparation, and race tests**

Add schema assertions for version 5 and these exact durable objects:

```go
func TestSchemaInstallsProviderWriteObjects(t *testing.T) {
    profile := openTemporaryProfile(t)
    names := schemaObjectNames(t, profile)
    assert.Subset(t, names, []string{
        "provider_operation_lease", "provider_identity_lineage",
        "provider_write_batches", "provider_write_batch_operations",
        "provider_write_items", "provider_write_results", "provider_last_write_summary",
    })
    assert.NotContains(t, names, "provider_refresh_lease")
}
```

Add table-driven lease tests that accept only `refresh`, `write`, and `reconcile`; prove lease
metadata never changes profile revision; and prove wrong owner/kind cannot renew, release, prepare,
record, finalize, or reconcile.

Add atomicity tests using a two-operation active prefix and one inactive redo operation:

```go
func TestPrepareProviderWriteFreezesReviewedPrefixAtomically(t *testing.T) {
    profile, revision := seededPendingProfile(t)
    request := store.PrepareProviderWriteRequest{
        ExpectedRevision: revision, ReviewedRevision: revision,
        ExpectedGeneration: 0, Lease: writeLease("owner-a"), ObservedAt: fixedNow,
    }
    prepared, err := profile.PrepareProviderWrite(context.Background(), request, deterministicPlan)
    require.NoError(t, err)
    assert.Equal(t, revision+1, prepared.Revision)
    assert.Equal(t, store.WritePhaseWriting, prepared.Batch.Phase)
    assert.Equal(t, 2, prepared.Batch.FrozenOperationCount)
    assert.Equal(t, 2, prepared.Batch.TotalItems)
}
```

Use the existing injected-failure harness to fail every SQL boundary. Assert the lease, batch,
items, redo tail, journal cursor, and revision are unchanged after rollback.

Add the authoritative race tests:

```go
func TestRefreshFoldRefusesAnyWriteBatch(t *testing.T) {
    profile := profileWithBatchInPhase(t, store.WritePhasePaused)
    _, err := profile.ApplyProviderRefresh(context.Background(), validRefreshRequest(t), validPlanner)
    assertStoreInvalidReason(t, err, store.InvalidOperationProviderWriteBatch)
}

func TestPrepareProviderWriteRefusesLiveRefreshLease(t *testing.T) {
    profile, revision := seededPendingProfile(t)
    acquireLease(t, profile, refreshLease("refresh-owner"))
    _, err := profile.PrepareProviderWrite(context.Background(), prepareRequest(revision), validWritePlan)
    assertStoreInvalidReason(t, err, store.InvalidOperationProviderRefreshLease)
}
```

Run the refresh-fold case as a table over every unfinished batch phase, not only `paused`.

- [ ] **Step 2: Run focused tests and verify RED**

```bash
MONEYFLOW_SKIP_PERF=1 go test ./internal/store ./internal/store/sqlite \
  -run 'Test(SchemaInstallsProviderWrite|ProviderOperationLease|PrepareProviderWrite|RefreshFoldRefuses|WriteFailure)' \
  -count=1
```

Expected: FAIL because schema version 5, generalized leases, batch tables, and write-store methods do
not exist.

- [ ] **Step 3: Add the installed schema and closed store contracts**

Replace the refresh-only lease shape with:

```go
type ProviderOperationKind string

const (
    ProviderOperationRefresh   ProviderOperationKind = "refresh"
    ProviderOperationWrite     ProviderOperationKind = "write"
    ProviderOperationReconcile ProviderOperationKind = "reconcile"
)

type ProviderOperationLease struct {
    OwnerID   string
    Renderer  string
    Kind      ProviderOperationKind
    ExpiresAt time.Time
}

type WriteBatchPhase string

const (
    WritePhaseWriting           WriteBatchPhase = "writing"
    WritePhaseReconciling       WriteBatchPhase = "reconciling"
    WritePhasePaused            WriteBatchPhase = "paused"
    WritePhaseReconnectRequired WriteBatchPhase = "reconnect_required"
    WritePhaseRateLimited       WriteBatchPhase = "rate_limited"
    WritePhaseAttentionRequired WriteBatchPhase = "attention_required"
)
```

Define typed `WriteExpectationKind` values `existing`, `merge_destination`, and `new`; typed item
states `pending`, `succeeded`, and `failed`; typed attention class/reason values from the spec; and
clone/validate methods for every slice-bearing value. `WriteBatch` stores opaque ID, phase, version,
reviewed/prepared revisions, refresh generation, frozen cursor/prefix digest, counts, attention,
timestamps, and `NextEligible`. `WriteItem` stores the exact request and expectation fields from the
spec. `WriteResult` stores normalized field-presence values, never raw payloads.

Define the planner boundary before adding it to `Profile`:

```go
type PrepareProviderWriteInputs struct {
    Snapshot domain.ProfileSnapshot
    ProviderState ProviderState
    ProposedBatchID string
    ProposedItemIDs []string
    ObservedAt time.Time
}

type PrepareProviderWritePlan struct {
    FrozenOperationIDs []string
    FrozenPrefixDigest string
    Items []WriteItem
    Groups []WriteItemGroup
}

type PrepareProviderWritePlanner func(PrepareProviderWriteInputs) (PrepareProviderWritePlan, error)
```

Extend `ProviderState` with cloned lineage, operation lease, unfinished batch status, and last-write
summary. Keep raw item/request/result detail available only through the write-specific store methods,
never the general status projection.

Add allowlisted store validation stages `provider_write_request`, `provider_write_planner`,
`provider_write_plan`, `provider_write_batch`, and `provider_refresh_lease`. SQLite returns these
through `NewInvalidOperationError`; application orchestration maps them to the public provider/app
codes. Do not introduce exported sentinel errors with raw diagnostic text.

Extend `store.Profile` with these exact methods:

```go
AcquireProviderOperationLease(context.Context, ProviderOperationLease, time.Time) (ProviderOperationLease, bool, error)
RenewProviderOperationLease(context.Context, string, ProviderOperationKind, time.Time, time.Time) (bool, error)
ReleaseProviderOperationLease(context.Context, string, ProviderOperationKind) error
ProviderWriteState(context.Context) (ProviderWriteState, error)
PrepareProviderWrite(context.Context, PrepareProviderWriteRequest, PrepareProviderWritePlanner) (PrepareProviderWriteCommit, error)
ClaimProviderWriteItems(context.Context, ClaimProviderWriteRequest) ([]WriteItem, error)
RecordProviderWriteResult(context.Context, RecordProviderWriteResultRequest) (WriteBatch, error)
ParkProviderWrite(context.Context, ParkProviderWriteRequest) (WriteBatch, error)
ResumeProviderWrite(context.Context, ResumeProviderWriteRequest) (WriteBatch, error)
FinalizeProviderWrite(context.Context, FinalizeProviderWriteRequest, FinalizeProviderWritePlanner) (FinalizeProviderWriteCommit, error)
```

`PrepareProviderWritePlanner` receives a cloned snapshot, provider state, and caller-supplied opaque
IDs; it has no store handle. It returns frozen operation IDs, canonical prefix digest, deterministic
items, and lineage/allocation changes. Validate the returned plan inside the transaction.

Add STRICT tables and checks described by the design. Add `provider_label` to allocations. Add
lineage keyed by `(namespace, external_id)` with prior/current local IDs, provider label,
`alias|reactivated`, and batch version. Add a singleton counts-only last summary. Increment both
schema literals to 5 and make `currentSchemaObjectsPresent` verify a version-5-only index. Do not add
migration SQL.

In `ApplyProviderRefresh`, after loading the generalized refresh lease and before invoking the
planner, query for any unfinished write batch and return the typed in-progress store failure. Check
lease purpose `refresh` in the same transaction.

- [ ] **Step 4: Run focused and complete store tests and verify GREEN**

```bash
gofmt -w internal/store internal/store/sqlite
MONEYFLOW_SKIP_PERF=1 go test ./internal/store ./internal/store/sqlite -count=1
make test-store
```

Expected: PASS, including rollback injection, cross-process race, schema inspection, and existing
100k storage gates.

- [ ] **Step 5: Run the required commit gate and commit**

Stage only the Task 1 files and commit with subject:

```text
feat: add durable Monarch write batches
```

### Task 2: Port the One-Attempt Monarch Transaction Writer

**Files:**

- Modify: `internal/provider/provider.go`
- Modify: `internal/provider/provider_test.go`
- Modify: `internal/provider/errors.go`
- Modify: `internal/provider/errors_test.go`
- Modify: `internal/provider/architecture_test.go`
- Modify: `internal/provider/monarch/client.go`
- Modify: `internal/provider/monarch/queries.go`
- Modify: `internal/provider/monarch/session_file.go`
- Create: `internal/provider/monarch/write.go`
- Create: `internal/provider/monarch/write_test.go`
- Modify: `internal/provider/monarch/graphql_test.go`
- Modify: `internal/provider/monarch/live_test.go`

**Interfaces:**

- Consumes: the existing authenticated Monarch client/session source and stable provider errors.
- Produces: `provider.Writer`, `provider.TransactionUpdate`, `provider.TransactionUpdateResult`,
  and `Source.Writer(context.Context, bool)`.

- [ ] **Step 1: Write failing neutral-contract and loopback mutation tests**

Define contract tests around explicit field presence:

```go
request := provider.TransactionUpdate{
    TransactionExternalID: "txn-example-1",
    MerchantName: provider.Some("Example Merchant"),
    CategoryExternalID: provider.Some("category-example-1"),
    Hidden: provider.Some(true),
}
```

The loopback server asserts operation name `Web_TransactionDrawerUpdateTransaction`, exactly one
HTTP request, authenticated headers, and only non-nil requested variables. Its synthetic response
must normalize to:

```go
assert.Equal(t, "txn-example-1", result.TransactionExternalID)
assert.Equal(t, provider.Some("merchant-example-9"), result.MerchantExternalID)
assert.Equal(t, provider.Some("Example Merchant"), result.MerchantLabel)
assert.Equal(t, provider.Some("category-example-1"), result.CategoryExternalID)
assert.Equal(t, provider.Some(true), result.Hidden)
```

Add HTTP 401, 404, payload rejection, 429 with bounded Retry-After, 5xx, timeout, malformed JSON,
missing transaction, and transaction-ID mismatch cases. Assert no raw payload/provider/request value
appears in the returned error or unwrap chain. Count requests to prove no internal retry.

- [ ] **Step 2: Run focused tests and verify RED**

```bash
go test ./internal/provider ./internal/provider/monarch \
  -run 'Test(Writer|UpdateTransaction|WriteError|SourceWriter|Architecture)' -count=1
```

Expected: FAIL because the writer contract and mutation do not exist.

- [ ] **Step 3: Add the minimal neutral writer and Monarch adapter**

Add exact neutral shapes:

```go
type Optional[T any] struct { Value T; Present bool }
func Some[T any](value T) Optional[T] { return Optional[T]{Value: value, Present: true} }

type TransactionUpdate struct {
    TransactionExternalID string
    MerchantName Optional[string]
    CategoryExternalID Optional[string]
    Hidden Optional[bool]
}
type TransactionUpdateResult struct {
    TransactionExternalID string
    MerchantExternalID Optional[string]
    MerchantLabel Optional[string]
    CategoryExternalID Optional[string]
    Hidden Optional[bool]
}
type Writer interface {
    ProbeIdentity(context.Context) (ProfileIdentity, error)
    UpdateTransaction(context.Context, TransactionUpdate) (TransactionUpdateResult, error)
}
```

Add `Writer(context.Context, bool) (Writer, SessionFingerprint, error)` to `Source`; update all fake
sources explicitly. Port only this mutation:

```graphql
mutation Web_TransactionDrawerUpdateTransaction($input: UpdateTransactionMutationInput!) {
  updateTransaction(input: $input) {
    transaction { id merchant { id name } category { id } hideFromReports }
    errors { field messages }
  }
}
```

Implement one HTTP attempt, build only present input fields, normalize the returned label, require a
matching transaction ID, and preserve omission versus false. Map transport/HTTP/GraphQL/payload
failures directly to stable provider codes without raw causes. Keep retry ownership in the app.

Extend the stable code set with `provider_write_in_progress`,
`provider_write_attention_required`, `provider_write_stale`, `provider_write_paused`,
`provider_write_not_eligible`, and `provider_write_unsupported`. Add a typed, value-free
`WriteFailureReason` carried only by write failures, with the seven exact attention reasons from the
design and `WriteFailureReasonOf(error)`. HTTP not-found and deterministic payload rejection map to
target-not-found/rejected reasons; unavailable and malformed/partial successful responses remain
separate so Task 4 can assign retry policy without raw error inspection.

Add opt-in live characterization for merchant-ID input and empty-merchant retention. It skips unless
the existing live gate is enabled and emits counts/booleans only.

- [ ] **Step 4: Run focused and package tests and verify GREEN**

```bash
gofmt -w internal/provider
go test ./internal/provider ./internal/provider/monarch -count=1
```

Expected: PASS with each writer case making exactly one loopback request.

- [ ] **Step 5: Run the required commit gate and commit**

Stage only Task 2 files and commit with subject:

```text
feat: add Monarch transaction writer
```

### Task 3: Plan Absolute Items and Enforce Monarch Capabilities

**Files:**

- Create: `internal/app/provider_write_plan.go`
- Create: `internal/app/provider_write_plan_test.go`
- Create: `internal/app/provider_write_plan_property_test.go`
- Create: `internal/app/provider_write_plan_benchmark_test.go`
- Create: `internal/app/provider_write_identity.go`
- Create: `internal/app/provider_write_identity_test.go`
- Modify: `internal/app/capabilities.go`
- Modify: `internal/app/capabilities_test.go`
- Modify: `internal/app/edit_merchant.go`
- Modify: `internal/app/edit_merchant_test.go`
- Modify: `internal/app/edit_category.go`
- Modify: `internal/app/edit_category_test.go`
- Modify: `internal/app/taxonomy.go`
- Modify: `internal/app/taxonomy_test.go`
- Modify: `internal/app/profile_service.go`
- Modify: `internal/app/profile_service_test.go`
- Modify: `internal/app/provider_labels.go`
- Modify: `internal/app/provider_labels_test.go`
- Modify: `internal/app/provider_rebase.go`
- Modify: `internal/app/provider_rebase_test.go`

**Interfaces:**

- Consumes: freshly replayed committed/effective state, journal prefix, active mappings,
  allocations with provider labels, and identity lineage.
- Produces: `BuildProviderWritePlan(store.PrepareProviderWriteInputs)`, deterministic item/group
  order, staging-time `provider_write_unsupported` failures, and provider-aware capabilities.

- [ ] **Step 1: Write failing planning, capability, identity, and property tests**

Use one synthetic profile to cover all five supported operation types and assert no more than one
item per transaction. For each item, compare committed with effective and require only differing
absolute fields. Add a net-noop history that produces zero items.

Add table tests for exact merchant addressing:

```go
tests := []struct {
    operation domain.OperationType
    expectation store.WriteExpectationKind
    sentLabel string
}{
    {domain.OperationMerchantLabel, store.WriteExpectationNew, "Normalized Name"},
    {domain.OperationMerchantMerge, store.WriteExpectationMergeDestination, "Provider Label"},
    {domain.OperationMerchantReassign, store.WriteExpectationExisting, "Provider Label"},
}
```

Assert deterministic display suffixes never appear in `sentLabel`. Refuse active provider-label
collisions, retired-active-label matches, zero-transaction label/merge, unmapped category
assignment, category creation, category/group management, transaction deletion, and every other
unsupported operation at staging and preparation.

For a new-name group, assert bytewise external-ID ordering elects the leader (`"10"` precedes
`"2"`) and followers are blocked until the leader response persists. Add randomized journal
histories and assert absolute items reproduce every supported effective transaction difference.

Exercise exactly 10,000 active operations and 1,000,000 targets: preparation remains available,
creates no bookkeeping journal entry, and respects the existing `journal_full` behavior for any
new user edit. Browsing/review/undo and provider refresh remain available before preparation at the
ceiling; once prepared, the ordinary batch capability lock applies.

Add identity cases for same-entity alias rotation, different-entity alias activation, retired-label
refusal, historical aliases with zero/current membership, and refresh promotion to a fresh local
merchant.

- [ ] **Step 2: Run focused tests and verify RED**

```bash
MONEYFLOW_SKIP_PERF=1 go test ./internal/app \
  -run 'Test(ProviderWritePlan|MonarchCapabilities|ProviderWriteIdentity|ProviderLabel|ProviderRebase)' \
  -count=1
```

Expected: FAIL because provider-bound profiles still expose profile-neutral taxonomy and no write
planner exists.

- [ ] **Step 3: Implement the pure planner and provider-aware validation**

Expose one closed planner:

```go
func BuildProviderWritePlan(inputs store.PrepareProviderWriteInputs) (store.PrepareProviderWritePlan, error) {
    replayed, err := Replay(inputs.Snapshot)
    if err != nil { return store.PrepareProviderWritePlan{}, err }
    operations := replayed.Journal[:replayed.Cursor]
    items, groups, err := planAbsoluteWriteItems(
        replayed.Committed, replayed.Effective, operations,
        inputs.ProviderState, inputs.ProposedItemIDs,
    )
    if err != nil { return store.PrepareProviderWritePlan{}, err }
    return store.PrepareProviderWritePlan{
        FrozenOperationIDs: operationIDs(operations),
        FrozenPrefixDigest: digestOperations(operations),
        Items: items, Groups: groups,
    }, nil
}
```

The planner clones every input, performs no I/O, reads no clock/random/global state, and returns a
complete plan. Generate item/batch IDs outside the callback and supply them in inputs.

Calculate affected transactions by replay semantics: structural label/merge operations sweep
current membership; reassign/category/hide use resolved targets. Use active external identities and
persisted provider labels, never display labels. Encode `existing`, `merge_destination`, and `new`
expectations plus same-name group keys. Sort items bytewise by transaction external ID and elect the
first new-group item as leader.

Make `Service.Mutate`, `Service.Review`, and `Capabilities` batch/provider aware. Bound Monarch
profiles allow merchant edit, mapped category assignment, hide, undo, redo, review, and supported
commit. They disable category creation and all `C`/`G` actions with fixed guidance. Any unfinished
batch disables edit/undo/redo/refresh/commit with one shared fixed reason. Revalidate all rules in
the preparation planner.

Persist raw provider labels during refresh allocation for accounts, merchants, groups, and
categories. Extend refresh rebase to honor lineage:
zero-member alias/retired IDs stay ignored; a referenced ID becomes a fresh local merchant and never
reactivates a merged tombstone.

- [ ] **Step 4: Run focused, property, and performance tests and verify GREEN**

```bash
gofmt -w internal/app
MONEYFLOW_SKIP_PERF=1 go test ./internal/app -count=1
go test ./internal/app -run TestProviderWritePlanRandomized -count=10
go test ./internal/app -run '^$' -bench BenchmarkProviderWritePlan100K -benchtime=1x
```

Expected: PASS; the supported benchmark host stays below the 1 s CI ceiling.

- [ ] **Step 5: Run the required commit gate and commit**

Stage only Task 3 files and commit with subject:

```text
feat: plan Monarch write-back batches
```

### Task 4: Run, Reconcile, and Finalize Durable Batches

**Files:**

- Create: `internal/app/provider_write.go`
- Create: `internal/app/provider_write_test.go`
- Create: `internal/app/provider_write_worker_test.go`
- Create: `internal/app/provider_write_reconcile.go`
- Create: `internal/app/provider_write_reconcile_test.go`
- Create: `internal/app/provider_write_property_test.go`
- Create: `internal/app/provider_write_performance_test.go`
- Modify: `internal/app/profile_service.go`
- Modify: `internal/app/profile_service_test.go`
- Modify: `internal/app/provider_refresh.go`
- Modify: `internal/app/provider_refresh_test.go`
- Modify: `internal/app/provider_scheduler.go`
- Modify: `internal/app/provider_scheduler_test.go`
- Modify: `internal/app/errors.go`
- Modify: `internal/app/errors_test.go`
- Modify: `internal/store/sqlite/provider_write.go`
- Modify: `internal/store/sqlite/provider_write_test.go`
- Modify: `internal/store/sqlite/provider_write_failure_test.go`

**Interfaces:**

- Consumes: `provider.Source.Writer`, Task 3's planner, durable store claims/results, and the existing
  complete-refresh fetch/confirmation machinery.
- Produces: provider-aware `Service.Commit`, `RunProviderWrite`, `ProviderWriteStatus`,
  `PauseProviderWrite`, `ResumeProviderWrite`, `StopAndReconcileProviderWrite`, and
  `ConfirmProviderWriteReconcile`.

- [ ] **Step 1: Write failing worker, override, retry, crash, and finalization tests**

Create an injected fake writer with scripted results and a request counter:

```go
type scriptedWriter struct {
    results map[string][]provider.TransactionUpdateResult
    errors map[string][]error
    calls []provider.TransactionUpdate
}
```

Assert concurrency never exceeds four; a new-name leader persists before followers start;
unavailable/incomplete results attempt at most five times with jittered 2 s to 60 s delays; rate
limiting persists bounded `NextEligible`; reconnect reloads once, re-probes identity, restarts once,
then parks; and identity mismatch sends nothing.

Add crash points before send, after provider success/before result persist, after result persist,
and during finalization. Reopen SQLite and assert absolute resend, skip-after-persist, and exactly-once
finalization semantics.

Add the approved merchant override matrix:

```go
tests := []struct {
    expectation store.WriteExpectationKind
    returnedKind string
    wantAttention bool
    wantOverride bool
}{
    {store.WriteExpectationExisting, "active-other", false, true},
    {store.WriteExpectationExisting, "unmapped", false, true},
    {store.WriteExpectationMergeDestination, "alias", false, true},
    {store.WriteExpectationNew, "contradictory", true, false},
}
```

Mapped existing/merge overrides fold the returned local merchant. Unmapped/alias/retired results
temporarily fold the requested local merchant and mark full refresh due. Explicit merge still
retires its source. New-group contradictions enter reconcile-only attention.

Test requested/unrequested known category/hidden overrides, unknown category deferral, alias
rotations for strict new groups, and counts-only announcements. Capture the effective snapshot
before commit and assert final committed state equals it adjusted only by accepted responses.

Test every attention class. Target-not-found, rejection, new-group identity conflict, retired
identity, and expectation invalid are reconcile-only. Exhausted unavailability and incomplete
response are retryable. Every stable code/reason belongs to exactly one scheduler/action class.

Cover reconcile confirmation expiry, wrong-process token, refresh-generation change, batch-version
change, and batch removal. Integrity failures never produce an overridable confirmation token.

- [ ] **Step 2: Run focused tests and verify RED**

```bash
MONEYFLOW_SKIP_PERF=1 go test ./internal/app ./internal/store/sqlite \
  -run 'Test(ProviderWrite|WriteWorker|WriteOverride|WriteAttention|WriteFinalization|WriteReconcile)' \
  -count=1
```

Expected: FAIL because `Service.Commit` still refuses provider-bound profiles and no worker exists.

- [ ] **Step 3: Implement preparation, worker scheduling, response planning, and reconcile**

Change provider-bound `Service.Commit` to generate opaque IDs, call store preparation with
`BuildProviderWritePlan`, reload the new revision, and return immediately with batch status. Keep
local-profile `Fold` unchanged.

Define renderer-neutral write status/actions:

```go
type ProviderWriteStatus struct {
    Phase store.WriteBatchPhase
    Version uint64
    AttentionReason store.WriteAttentionReason
    Total, Completed, Failed, Remaining, Overrides int
    NextEligible time.Time
    OwnerRenderer, OwnerInstanceID string
}

func (service *Service) RunProviderWrite(context.Context) (ProviderWriteStatus, error)
func (service *Service) PauseProviderWrite(context.Context, uint64) (ProviderWriteStatus, error)
func (service *Service) ResumeProviderWrite(context.Context, uint64) (ProviderWriteStatus, error)
func (service *Service) StopAndReconcileProviderWrite(context.Context, ProviderWriteReconcileRequest) (ProviderWriteResult, error)
func (service *Service) ConfirmProviderWriteReconcile(context.Context, ProviderWriteReconcileRequest) (ProviderWriteResult, error)
```

`RunProviderWrite` acquires/renews kind `write`, probes `subscription.id`, claims only eligible
items, runs at most four calls, and persists each response in its own transaction. The worker owns
retry timing. Pause prevents future claims but may accept an in-flight response when version/lease
CAS still holds.

Implement response planning as a pure function. Existing/merge-destination mismatches follow the
override rule; strict new groups rotate/activate lineage only after consistent group responses.
Known category/hide echoes become truth; unknown identities defer to refresh. Persist counts, never
identifiers, in operational status.

Finalization replays the frozen prefix, applies accepted overrides/rotations, validates equivalence,
folds state, removes the prefix/batch/items, increments revision once, writes last summary, marks
refresh due without advancing `LastSuccess`, and releases the lease atomically.

Stop and reconcile changes to kind `reconcile`, probes identity, fetches a full snapshot, and uses
existing integrity/deletion-confirmation logic. The fold removes the entire frozen prefix and batch.
Successful item facts are audit/status input only until the fold; the snapshot alone installs truth.

Status observes session replacement. Ownerless writing/reconciling may resume on a long-lived tick;
paused/attention never do. Rate-limited work wakes when eligible and reconnect work after session
replacement. CLI connect only replaces the session; disconnect refuses while a batch exists.

- [ ] **Step 4: Run focused, randomized, race, and performance tests and verify GREEN**

```bash
gofmt -w internal/app internal/store/sqlite
MONEYFLOW_SKIP_PERF=1 go test ./internal/app ./internal/store/sqlite -count=1
go test -race ./internal/app ./internal/store/sqlite -run 'TestProviderWrite' -count=1
go test ./internal/app -run TestProviderWriteCrashSchedules -count=20
go test ./internal/app -run '^$' -bench BenchmarkProviderWriteFinalize100K -benchtime=1x
```

Expected: PASS; reference and optimized plans have identical canonical logical encodings and the
supported benchmark host stays below 1 s.

- [ ] **Step 5: Run the required commit gate and commit**

Stage only Task 4 files and commit with subject:

```text
feat: execute durable Monarch writes
```

### Task 5: Add TUI Write Progress and Batch Actions

**Files:**

- Create: `internal/tui/provider_write.go`
- Create: `internal/tui/provider_write_test.go`
- Create: `internal/tui/provider_write_format.go`
- Create: `internal/tui/provider_write_format_test.go`
- Modify: `internal/tui/model.go`
- Modify: `internal/tui/update.go`
- Modify: `internal/tui/layout.go`
- Modify: `internal/tui/overlay_test.go`
- Modify: `internal/tui/review.go`
- Modify: `internal/tui/review_test.go`
- Modify: `internal/tui/provider.go`
- Modify: `internal/tui/provider_test.go`
- Modify: `internal/tui/quit.go`
- Modify: `internal/tui/quit_test.go`
- Modify: `cmd/moneyflow/provider.go`
- Modify: `cmd/moneyflow/provider_test.go`
- Modify: `cmd/moneyflow/tui_shell.go`
- Modify: `cmd/moneyflow/web_dependencies.go`

**Interfaces:**

- Consumes: Task 4's write status/actions and the existing provider standing tick.
- Produces: `providerWriteTUIState`, asynchronous worker messages, and a bounded batch-status
  overlay with Pause, Resume, Stop and reconcile, confirmation, and Reconnect actions.

- [ ] **Step 1: Write failing direct-commit, progress, action, and lifecycle tests**

Update the provider-bound review test: `w`, then `Enter`, closes review, preserves finance state,
and starts an asynchronous write command after durable preparation. It must not show the old
“write-back is not implemented” text.

Add phase/action table tests for writing, paused, reconnect, rate-limited, retryable attention,
reconcile-only attention, reconciling, and completion. Assert `w` opens batch status while a batch
exists, closing it does not pause, and Pause affects only future calls.

Test duration rendering, including long batches:

```go
tests := []struct{ remaining time.Duration; want string }{
    {42 * time.Second, "about 42s remaining"},
    {17 * time.Minute, "about 17m remaining"},
    {3*time.Hour + 20*time.Minute, "about 3h 20m remaining"},
}
```

Assert a multi-hour batch shows advancing completed/total counts and a human duration rather than a
static line. Omit estimates when throughput is insufficient.

Test standing-tick auto-resume for ownerless writing/reconciling, eligible rate limits, and healed
reconnects, but never paused/attention. Test exit/reopen durability, CLI disconnect refusal, and
connect's deliberate non-resume behavior.

- [ ] **Step 2: Run focused tests and verify RED**

```bash
MONEYFLOW_SKIP_PERF=1 go test ./internal/tui ./cmd/moneyflow \
  -run 'Test(ProviderWrite|ReviewProvider|WriteProgress|WriteStatus|ProviderDisconnect|Quit)' -count=1
```

Expected: FAIL because review still refuses provider commits and no write overlay exists.

- [ ] **Step 3: Implement TUI worker commands, status overlay, and composition wiring**

Add state:

```go
type providerWriteTUIState struct {
    status app.ProviderWriteStatus
    running bool
    cancel context.CancelFunc
    confirmationToken string
    startedAt time.Time
    startedCompleted int
}
```

After preparation succeeds, close review and return an async `RunProviderWrite` command. Preserve
finance state and pending markers. Poll counts through the existing provider tick generation; start
a worker only for automatic phases. Suppress refresh commands while any batch exists.

Render phase, completed/total/remaining/override counts, bounded estimate, owner renderer, and fixed
attention guidance. Route `p` Pause, `r` Resume, `s` Stop and reconcile, `Enter` confirmation, `c`
Reconnect, and `Esc` close. Never render item/operation/provider/local identities or labels.

On completion reload metadata, clear pending markers, preserve surviving focus, and announce
operation/item/override counts. Refresh stale controls and require reinvocation.

Pass the same source-enabled runtime through TUI/web openers. Keep CLI connect status-only, and make
disconnect query write state before deleting the session.

- [ ] **Step 4: Run focused TUI, command, package, and parity tests and verify GREEN**

```bash
gofmt -w internal/tui cmd/moneyflow
MONEYFLOW_SKIP_PERF=1 go test ./internal/tui ./cmd/moneyflow -count=1
make parity
```

Expected: PASS. Update locked parity only through deliberate reviewed parity targets if required.

- [ ] **Step 5: Run the required commit gate and commit**

Stage only Task 5 files and reviewed parity artifacts. Commit with subject:

```text
feat: show Monarch write progress in TUI
```

- [ ] **Step 6: Run the five-task review checkpoint**

Invoke `$roborev-fix`, resolve applicable findings with test-first follow-up commits, rerun affected
gates, and do not amend Tasks 1–5.

### Task 6: Expose the Protected HTTP Contract and Web Workflow

**Files:**

- Create: `internal/api/provider_write.go`
- Create: `internal/api/provider_write_test.go`
- Modify: `internal/api/server.go`
- Modify: `internal/api/server_test.go`
- Modify: `internal/api/profiles.go`
- Modify: `internal/api/errors.go`
- Modify: `internal/api/errors_test.go`
- Modify: `internal/api/openapi_test.go`
- Modify: `internal/api/editing_integration_test.go`
- Modify: `internal/tools/webtestserver/main.go`
- Modify: `internal/tools/webtestserver/main_test.go`
- Modify: `web/src/lib/api/client.ts`
- Modify: `web/src/lib/api/client.test.ts`
- Modify: `web/src/lib/api/schema.d.ts`
- Create: `web/src/lib/controller/provider-write.ts`
- Create: `web/src/lib/controller/provider-write.test.ts`
- Create: `web/src/components/editing/WriteStatusDrawer.svelte`
- Create: `web/src/components/editing/WriteStatusDrawer.test.ts`
- Modify: `web/src/components/editing/ReviewDrawer.svelte`
- Modify: `web/src/components/editing/ReviewDrawer.test.ts`
- Modify: `web/src/App.svelte`
- Modify: `web/src/App.test.ts`
- Create: `web/tests/provider-write.spec.ts`
- Modify: `web/tests/editing.spec.ts`
- Modify: `web/tests/origin.spec.ts`
- Modify: `Makefile`

**Interfaces:**

- Consumes: Task 4's service methods and profile-scoped mutation security.
- Produces: provider write schema version 1, five specified routes, generated TypeScript contracts,
  a visibility-gated browser controller, and accessible kit-ui status/actions.

- [ ] **Step 1: Write failing API, security, controller, component, and browser tests**

Add Huma contract tests for:

```text
GET  /api/v1/profiles/{profile_id}/provider/write-status
POST /api/v1/profiles/{profile_id}/provider/write/pause
POST /api/v1/profiles/{profile_id}/provider/write/resume
POST /api/v1/profiles/{profile_id}/provider/write/reconcile
POST /api/v1/profiles/{profile_id}/provider/write/reconcile/confirm
```

Assert each POST rejects missing/wrong profile mutation token, wrong Origin, and cross-site Fetch
Metadata. Assert GET is credential-blind. Assert stale expected batch version returns 409 without
state change. Scan success/problem/status payloads for forbidden synthetic labels, IDs, raw errors,
and request bodies.

Define expected status JSON with strings for revision/generation/batch version, typed phase/reason,
counts, next-eligible timestamp, owner class/opaque instance, capability actions, and no item data.
Test commit returns immediately after preparation with the authoritative projection and batch.

In Vitest, cover visibility-gated polling, automatic eligibility, manual Pause/Resume/Reconcile,
reconcile-only hiding Resume, confirmation invalidation, reconnect, stale controls, and completion
reload. Assert `Enter` in review calls commit once and closes immediately.

In Playwright, run a fake Writer journey through edit → `w` → `Enter` → progress → completion,
restart mid-batch, session replacement, override count, and Stop and reconcile. Add accessibility
assertions for drawer names, live regions, keyboard focus, and disabled reasons. Do not capture or
retain screenshots.

- [ ] **Step 2: Run focused tests and verify RED**

```bash
MONEYFLOW_SKIP_PERF=1 go test ./internal/api ./internal/tools/webtestserver \
  -run 'Test(ProviderWrite|WriteSecurity|OpenAPI|WebTestServer)' -count=1
bun run --cwd web test -- --run provider-write ReviewDrawer App
```

Expected: FAIL because routes, schema, controller, and components do not exist.

- [ ] **Step 3: Implement the HTTP and browser state machines**

Add identity-free wire status:

```go
type ProviderWriteStatusResponse struct {
    Version string `json:"version"`
    Revision string `json:"revision" pattern:"^[0-9]+$"`
    Generation string `json:"generation" pattern:"^[0-9]+$"`
    BatchVersion string `json:"batch_version,omitempty" pattern:"^[0-9]+$"`
    Phase string `json:"phase,omitempty"`
    Reason string `json:"reason,omitempty"`
    Total int `json:"total"`
    Completed int `json:"completed"`
    Failed int `json:"failed"`
    Remaining int `json:"remaining"`
    Overrides int `json:"overrides"`
    NextEligible string `json:"next_eligible,omitempty" format:"date-time"`
    OwnerRenderer string `json:"owner_renderer,omitempty"`
    OwnerInstanceID string `json:"owner_instance_id,omitempty" maxLength:"128"`
    Actions []string `json:"actions"`
}
```

All controls carry wire version and expected batch version. Reconcile also carries the existing
view/query/selection/window contract; confirmation adds an opaque token. Register strict paths and
protect every POST with existing profile mutation security. Map stable failures to fixed Huma
problems without request bodies.

Regenerate OpenAPI/TypeScript through repository targets and review the full diff. Build a separate
`createProviderWriteController`. `poll()` runs only while visible. It automatically calls Resume
only for ownerless writing/reconciling, eligible rate limit, or healed reconnect. Paused/attention
wait for explicit action; revision conflicts are never replayed.

Use kit-ui `DetailDrawer`, `Button`, and status primitives. Reuse the review commit path. On prepared
response, close review, show write status, preserve finance projection, and poll counts. Render only
fixed reason/action copy. Reconcile confirmation is explicit; ordinary preparation stays
`w`, then `Enter`.

- [ ] **Step 4: Run API, frontend, browser, generated-asset, and security tests and verify GREEN**

```bash
gofmt -w internal/api internal/tools/webtestserver
MONEYFLOW_SKIP_PERF=1 go test ./internal/api ./internal/tools/webtestserver -count=1
make web-generate
make web-check
make web-test
make web-build
make web-embed
make web-embed-check
make web-e2e
make test-editing-e2e
```

Expected: PASS. `git status --short` shows no tracked screenshots or tracked files under `web/dist`
or `internal/web/dist`; generated changes are limited to deliberate contract artifacts.

- [ ] **Step 5: Run the required commit gate plus `make verify-web` and commit**

Stage only Task 6 source/tests and deliberate generated contracts. Commit with subject:

```text
feat: add Monarch write-back to web
```

### Task 7: Prove Cross-Process Safety and Complete the Slice

**Files:**

- Create: `internal/app/provider_write_cross_process_test.go`
- Modify: `internal/app/provider_write_property_test.go`
- Modify: `internal/app/provider_write_performance_test.go`
- Modify: `internal/store/sqlite/provider_write_concurrency_test.go`
- Modify: `internal/store/sqlite/provider_write_failure_test.go`
- Modify: `internal/api/editing_integration_test.go`
- Modify: `web/tests/provider-write.spec.ts`
- Modify: `internal/provider/architecture_test.go`
- Modify: `Makefile`
- Create: `docs/superpowers/benchmarks/2026-08-18-monarch-write-back.md`

**Interfaces:**

- Consumes: the complete TUI/web/store/provider implementation from Tasks 1–6.
- Produces: deterministic crash/cross-process/equivalence proof, 100k benchmark evidence, privacy
  and package-boundary gates, and the final automated acceptance record.

- [ ] **Step 1: Add cross-process, randomized, architecture, privacy, and performance gates**

Use two independently opened SQLite connections and app services sharing one fake provider. Add
`TestRefreshAndWritePreparationCannotCrossFold` with a channel-blocked fetch,
`TestWriteLeaseHandsOffBetweenTUIAndWeb` with an injected clock,
`TestCrashAfterSendBeforePersistConverges` with a persisted send counter,
`TestResponseAdjustedCommitEquivalence` with generated overrides/rotations, and
`TestStopAndReconcileIgnoresPartialIdentityFacts` with conflicting successful-item facts and a
different authoritative snapshot.

Extend randomized schedules across send/result-persist/finalize crashes and compare reopened
canonical state with uninterrupted execution. Inject mapped/unmapped merchant overrides, strict
new-group contradictions, unknown categories, not-found, reconnect, rate limit, and storage failure.

Extend architecture tests to reject provider↔store, renderer→Monarch, and planner→store/transport
dependencies. Add a source scan proving the only provider mutation is transaction update; category,
group, and delete mutations remain absent.

Add 100k transactions plus 10,000 operations/1,000,000 targets benchmarks. Planning uses the
250 ms reference target/1 s CI ceiling family; finalization has a 1 s ceiling excluding network.
Report four-way fake-provider throughput without a network timing gate.

Add logs/wire/generated-schema privacy scanning with reserved synthetic forbidden values. Assert
only allowlisted codes/counts/versions/timings/renderer/opaque IDs cross operational boundaries.

- [ ] **Step 2: Run the new gates and verify RED where coverage is absent**

```bash
MONEYFLOW_SKIP_PERF=1 go test ./internal/app ./internal/store/sqlite ./internal/provider ./internal/api \
  -run 'Test(RefreshAndWrite|WriteLease|CrashAfterSend|ResponseAdjusted|StopAndReconcile|Architecture|WritePrivacy)' \
  -count=1
```

Expected: missing obligations fail with behavioral assertions. If behavior already passes, locally
revert the relevant guard, observe the regression test fail, then restore it before continuing; do
not commit the temporary revert.

- [ ] **Step 3: Close only gaps exposed by Step 2**

Keep each correction in its owner: SQLite transaction guards in `internal/store/sqlite`, pure-plan
equivalence in `internal/app`, redaction at the Monarch boundary, and presentation drift in the
renderer. Add no features, retry classes, mutations, screenshots, or schema objects.

For each correction, rerun its failing test then its package. Preserve:

```text
refresh fold = generation + refresh lease owner/kind + no unfinished batch
write prepare = profile/review revision + generation + acquired write lease
final committed = reviewed effective + accepted response overrides/identity rotations
stop/reconcile committed = authoritative snapshot with frozen prefix removed
```

- [ ] **Step 4: Run the complete automated acceptance matrix and verify GREEN**

```bash
make test
make test-store
make test-race
make lint
make parity
make verify-go
make verify-web
uv run pytest -v
uv run pyright moneyflow/
uv run ruff format --check moneyflow/ tests/
uv run ruff check moneyflow/ tests/
npx --yes markdownlint-cli@0.47.0 --config .markdownlint.json README.md 'docs/**/*.md'
.github/scripts/check-arrow-lists.sh
```

Expected: every command exits zero, or only a documented supported performance gate is skipped via
the repository's non-performance path. Review `git diff --check`, generated contracts, and ignored
status; no screenshot or distribution output may be staged.

Record benchmark commands, host qualification, counts, and timings. Do not record provider IDs,
transaction values, labels, paths, or live account details.

- [ ] **Step 5: Commit the automated completion gates**

Stage only Task 7 tests, gates, and benchmark evidence. Commit with subject:

```text
test: prove Monarch write-back recovery
```

- [ ] **Step 6: Run the final review checkpoint**

Invoke `$roborev-fix`, address applicable findings with separate verified commits, and rerun the
affected acceptance matrix. Do not amend Tasks 1–7.

- [ ] **Step 7: Dogfood only after the automated tree is clean and committed**

Use the production-isolation workflow before running the branch binary. Back up the live Go v2
profile because schema 4 is intentionally incompatible with schema 5. Exercise only:

```text
small merchant edit -> w -> Enter -> completed full refresh
mapped category assignment -> w -> Enter
hide then unhide -> w -> Enter
multi-transaction merchant normalization -> w -> Enter
process exit during writing -> reopen -> automatic resume
CLI reconnect -> running TUI/web heals without restart
```

Observe a natural provider-rule override only if one already exists; do not create destructive
rules or delete remote data to force coverage. Never force rate limiting. If dogfooding finds a
defect, write a failing synthetic regression test, fix it, rerun gates, and create a new commit before
reporting completion.
