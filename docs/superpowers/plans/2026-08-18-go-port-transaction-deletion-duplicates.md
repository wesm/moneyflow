# Go Port Transaction Deletion and Duplicate Review Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development
> (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use
> checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add durable, undoable transaction deletion and exact duplicate review to the shared Go
application, TUI, web UI, and Monarch write-back path.

**Architecture:** Add `transaction.delete` to the typed journal and replay it as effective row
absence. Detect duplicates with a pure slices-and-maps analytics function, project bounded results
through `internal/app`, and keep TUI/web renderers as intent presenters. Extend the existing durable
provider write batch into an `update|delete` union; Monarch performs one-attempt absolute deletes,
while the application worker owns retries, crash recovery, reconciliation, and finalization.

**Tech Stack:** Go 1.26.3; `modernc.org/sqlite` v1.56.0; Huma v2.38.0 with `humago`; Bubble Tea
2.0.8; Cobra 1.10.2; Svelte 5.56.3 in runes mode; Bun 1.3.14;
`@kenn-io/kit-ui` commit `16db58ef8122dd00e21ce8ad90ba295b9174c6ef`; Testify; Vitest
4.1.10; Playwright 1.61.1.

## Global Constraints

- Work only on the checked-out `go-port` branch. Do not switch branches, pull, rebase, push,
  merge, amend, or remove Python without explicit user permission.
- Follow TDD for every behavior change: write the test, observe the intended failure, implement the
  smallest behavior, and rerun the focused and package tests.
- Commit every verified task before starting the next. Stage only that task's files and never
  commit screenshots, `web/dist`, or `internal/web/dist`.
- Install schema version 7 only into an empty database. Refuse version 6 and every mismatch; do not
  add schema or journal-payload migrations while Go v2 is unstable.
- Keep pure-Go SQLite and the existing no-CGO Linux, macOS, and Windows contract.
- Keep all money in signed integer minor units. Do not add `float32`, `float64`, or SQLite `REAL`.
- `transaction.delete` payload version 1 stores sorted stable local transaction IDs and an empty
  typed payload. It never stores predicates, provider IDs, labels, search text, or money.
- Duplicate matching uses exact date, exact minor/currency/scale, Unicode-lowercased un-suffixed
  merchant label, and stable local account ID. Do not trim, NFKC-normalize, case-fold, score, or use
  a date tolerance.
- Provider-backed duplicate labels come from persisted `LabelAllocation.ProviderLabel`; local
  merchants use their effective user label.
- `x` stages deletion. Only the existing `w`, then `Enter`, boundary performs remote deletion.
- An attempted update item without a persisted result parks for reconciliation. An attempted delete
  item may be resent within the existing five-attempt budget because absence is absolute intent.
- Keep deleted transaction external identities as tombstones. Same external-ID resurrection reuses
  the local ID; a new external ID receives a fresh local ID.
- Stop and reconcile removes the entire frozen prefix. It never restores failed or unsent deletion
  intent to the journal.
- Provider network I/O never runs inside a SQLite transaction. The provider writer performs one
  HTTP attempt; only the durable application worker owns retries.
- Persistent logs remain limited to codes, revisions, counts, timings, bounded attempt numbers,
  phases, and correlation IDs. Never log financial values, dates, labels, search text, local or
  provider transaction IDs, GraphQL messages, credentials, sessions, paths, or URLs.
- Live Monarch checks require explicitly supplied disposable transaction IDs and run only after the
  automated change is committed.

## Target File Map

```text
internal/analytics/duplicates.go                 pure exact duplicate grouping and ordering
internal/domain/operations.go                    transaction.delete typed operation
internal/replay/replay.go                        deletion replay in reference and indexed paths
internal/replay/provider_rebase.go                provider refresh target shrinking
internal/store/sqlite/codec.go                    version-one deletion payload encoding
internal/store/sqlite/schema/profile.sql          schema v7 write-item union
internal/store/sqlite/provider_write.go           union persistence and claim rules
internal/app/delete.go                            selection/focus deletion mutation builder
internal/app/duplicates.go                        filtered complete-result bounded projection
internal/app/review.go                            deletion and vacuous-operation review summaries
internal/app/provider_write_plan.go               update/delete item planning
internal/app/provider_write.go                    per-kind dispatch and crash recovery
internal/provider/provider.go                     provider-neutral delete method/result
internal/provider/monarch/write.go                one-attempt Monarch delete adapter
internal/tui/delete.go                            count-only confirmation
internal/tui/duplicates.go                        duplicate overlay controller and renderer
internal/api/duplicates.go                        profile-scoped bounded POST projection
web/src/lib/controller/duplicates.ts              browser duplicate projection state
web/src/components/editing/DuplicateReview.svelte accessible keyboard-driven review dialog
```

## Cross-Task Interfaces

Task 1 establishes these exact shared types:

```go
// internal/domain/operations.go
type TransactionDeletePayload struct{}

// internal/analytics/duplicates.go
type DuplicateGroup struct {
    Date             domain.Date
    Amount           domain.Money
    MatchingLabel    string
    AccountID        domain.EntityID
    TransactionIDs   []domain.EntityID
}

func FindDuplicates(
    transactions []domain.Transaction,
    matchingMerchantLabels map[domain.EntityID]string,
) []DuplicateGroup
```

Task 3 establishes the renderer-neutral projection:

```go
type DuplicateWindowRequest struct {
    GroupOffset int
    GroupLimit  int
    RowOffset   int
    RowLimit    int
}

type DuplicateRow struct {
    GroupNumber  int
    Target       RowTarget
    Transaction  domain.Transaction
    MatchingLabel string
    Flags        domain.RowFlags
}

type DuplicateGroupProjection struct {
    Number int
    Rows   []DuplicateRow
}

type DuplicateProjection struct {
    Revision         uint64
    State            ViewState
    Selection        SelectionValue
    SelectionCount   int
    TotalGroups      int
    TotalTransactions int
    GroupWindow      Window
    RowWindow        Window
    Groups           []DuplicateGroupProjection
    Status           string
}

func (service *Service) ProjectDuplicates(
    ctx context.Context,
    expectedRevision uint64,
    state ViewState,
    selection SelectionValue,
    window DuplicateWindowRequest,
) (DuplicateProjection, error)

func (service *Service) CapabilitiesForState(state ViewState) []Capability

type ReviewOperation struct {
    // Existing fields remain unchanged.
    Annotation string
}
```

Task 4 extends the provider port:

```go
type TransactionDeleteResult struct {
    TransactionExternalID string
    AlreadyAbsent         bool
}

type Writer interface {
    ProbeIdentity(context.Context) (ProfileIdentity, error)
    UpdateTransaction(context.Context, TransactionUpdate) (TransactionUpdateResult, error)
    DeleteTransaction(context.Context, string) (TransactionDeleteResult, error)
}
```

Task 2 makes durable write items a union:

```go
type WriteItemKind string

const (
    WriteItemUpdate WriteItemKind = "update"
    WriteItemDelete WriteItemKind = "delete"
)

type WriteItem struct {
    Kind WriteItemKind
    // existing fields remain unchanged
}

type WriteResult struct {
    Kind           WriteItemKind
    AlreadyAbsent bool
    // existing normalized update fields remain unchanged
}
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

Expected: every command exits zero. If documentation changes in the task, also run:

```bash
npx --yes markdownlint-cli@0.47.0 --config .markdownlint.json README.md 'docs/**/*.md'
.github/scripts/check-arrow-lists.sh
```

Beginning with Task 7, also run:

```bash
bun install --cwd web --frozen-lockfile
make verify-web
```

Expected: the lockfile is unchanged and generated API/types/assets are current. If a supported
performance gate is invalid because of host load, run the repository's non-performance
verification path, report the exact timing gate skipped, and commit the otherwise verified task.

---

### Task 1: Add the Typed Delete Operation and Pure Duplicate Analytics

**Files:**

- Create: `internal/analytics/duplicates.go`
- Create: `internal/analytics/duplicates_test.go`
- Create: `internal/analytics/duplicates_benchmark_test.go`
- Modify: `internal/domain/operations.go`
- Modify: `internal/domain/operations_test.go`

**Interfaces:**

- Consumes: validated `domain.Transaction`, `domain.Money`, and caller-selected un-suffixed
  merchant labels.
- Produces: `domain.OperationTransactionDelete`, `domain.TransactionDeletePayload`,
  `analytics.DuplicateGroup`, and `analytics.FindDuplicates` exactly as declared above.

- [ ] **Step 1: Write failing deletion-operation validation tests**

Add table tests proving a draft deletion requires an empty typed payload plus nonempty strictly
sorted unique transaction targets:

```go
func TestTransactionDeleteOperationValidation(t *testing.T) {
    operation := validOperation(domain.OperationTransactionDelete, "transaction-a")
    operation.TransactionDelete = &domain.TransactionDeletePayload{}
    require.NoError(t, operation.ValidateDraft())

    for name, mutate := range map[string]func(*domain.Operation){
        "missing payload": func(value *domain.Operation) { value.TransactionDelete = nil },
        "empty targets": func(value *domain.Operation) { value.Targets = nil },
        "duplicate targets": func(value *domain.Operation) {
            value.Targets = []domain.EntityID{"transaction-a", "transaction-a"}
        },
    } {
        t.Run(name, func(t *testing.T) {
            candidate := operation.Clone()
            mutate(&candidate)
            require.Error(t, candidate.ValidateDraft())
        })
    }
}
```

Extend the operation clone and valid-operation tables so the delete payload participates in the
exactly-one-payload invariant.

- [ ] **Step 2: Write failing exact duplicate tests**

Create fixtures that separately vary date by one day, minor units, currency, scale, account ID,
whitespace, NFKC form, and a stronger case-fold pair. Assert only Unicode lowercase joins groups.
Add the collision-allocation case using the labels map:

```go
func TestFindDuplicatesUsesRawMatchingLabelsBehindLocalSuffixes(t *testing.T) {
    rows := []domain.Transaction{
        duplicateTransaction("transaction-a", "merchant-a", "Example Merchant"),
        duplicateTransaction("transaction-b", "merchant-b", "Example Merchant · a1b2"),
    }
    groups := analytics.FindDuplicates(rows, map[domain.EntityID]string{
        "merchant-a": "Example Merchant",
        "merchant-b": "Example Merchant",
    })
    require.Len(t, groups, 1)
    assert.Equal(t,
        []domain.EntityID{"transaction-a", "transaction-b"},
        groups[0].TransactionIDs,
    )
}
```

Randomize input order repeatedly and compare a canonical JSON digest. Verify group sort is date
descending, then currency/scale/minor ascending, lowercase matching label, account ID, and smallest
transaction ID; verify member rows use bytewise local-ID order.

- [ ] **Step 3: Run the focused tests and verify RED**

```bash
go test ./internal/domain ./internal/analytics \
  -run 'Test(TransactionDelete|FindDuplicates)' -count=1
```

Expected: FAIL because the operation constant/payload and duplicate detector do not exist.

- [ ] **Step 4: Implement the typed operation**

Add `OperationTransactionDelete = "transaction.delete"`, the empty payload pointer on `Operation`,
clone it, count it, and validate it:

```go
case OperationTransactionDelete:
    if operation.TransactionDelete == nil {
        return errors.New("validate operation: transaction delete has wrong payload")
    }
```

Do not reuse taxonomy `DeletePayload`; the distinct empty type prevents transaction deletion from
accepting a source or replacement entity by mistake.

- [ ] **Step 5: Implement exact duplicate grouping**

Use a private comparable key and ordinary maps/slices:

```go
type duplicateKey struct {
    date, currency, label string
    scale                 uint8
    minor                 int64
    accountID             domain.EntityID
}

label := transaction.Merchant.Name
if raw, ok := matchingMerchantLabels[domain.EntityID(transaction.Merchant.ID)]; ok {
    label = raw
}
key := duplicateKey{
    date: transaction.Date.String(), currency: string(transaction.Amount.Currency),
    scale: transaction.Amount.Scale, minor: transaction.Amount.Minor,
    label: strings.ToLower(label), accountID: domain.EntityID(transaction.Account.ID),
}
```

Preserve a deterministic display/matching label, omit singletons, deep-copy IDs, then apply the
approved deterministic group comparator. When inputs differ only by letter case, choose the
bytewise-smallest original un-suffixed label so randomized input order cannot change output.

- [ ] **Step 6: Add and run the 100,000-row benchmark**

The benchmark must assert group count, row count, and digest before reporting time:

```bash
go test ./internal/analytics -run '^$' -bench BenchmarkFindDuplicates100K -count=1
```

Expected: PASS; the reference target is 100 ms and the shared-CI ceiling is 500 ms.

- [ ] **Step 7: Run the task gate and commit**

Run the required commit gate, inspect `git diff --check` and `git diff`, then:

```bash
git add internal/analytics/duplicates.go internal/analytics/duplicates_test.go \
  internal/analytics/duplicates_benchmark_test.go internal/domain/operations.go \
  internal/domain/operations_test.go
git commit -m "feat: add deletion operation and duplicate analytics"
```

---

### Task 2: Replay, Persist, Fold, and Rebase Transaction Deletion

**Files:**

- Modify: `internal/domain/entities_test.go`
- Modify: `internal/replay/replay.go`
- Create: `internal/replay/replay_test.go`
- Modify: `internal/replay/replay_equivalence_test.go`
- Modify: `internal/replay/drills.go`
- Modify: `internal/replay/provider_rebase.go`
- Create: `internal/replay/provider_rebase_test.go`
- Modify: `internal/store/sqlite/codec.go`
- Modify: `internal/store/sqlite/codec_test.go`
- Modify: `internal/store/sqlite/schema/profile.sql`
- Modify: `internal/store/sqlite/initialize.go`
- Modify: `internal/store/sqlite/initialize_test.go`
- Modify: `internal/store/sqlite/schema_test.go`
- Modify: `internal/store/provider_write.go`
- Create: `internal/store/provider_write_test.go`
- Modify: `internal/store/sqlite/provider_write.go`
- Modify: `internal/store/sqlite/provider_write_test.go`
- Modify: `internal/store/sqlite/provider_write_failure_test.go`
- Modify: `internal/app/provider_write_plan.go`
- Modify: `internal/app/provider_write_plan_test.go`
- Modify: `internal/app/provider_write_property_test.go`
- Modify: `internal/app/provider_write_plan_benchmark_test.go`
- Modify: `internal/app/provider_write_performance_test.go`
- Modify: `internal/app/provider_write_test.go`
- Modify: `internal/store/sqlite/fold_test.go`
- Modify: `internal/store/sqlite/fold_property_test.go`
- Modify: `internal/store/sqlite/journal_test.go`
- Modify: `internal/store/sqlite/performance_test.go`

**Interfaces:**

- Consumes: Task 1's operation type and payload.
- Produces: reference/indexed replay deletion, JSON `{}` payload codec, transaction mapping
  tombstones, the complete schema-version-7 durable item union, fold equivalence, and
  refresh-rebase shrinking.

- [ ] **Step 1: Write failing replay and cursor tests**

Add a two-transaction snapshot and assert replay removes exactly the target, undo restores it, redo
removes it, and replay rejects a later hide/category operation targeting the deleted row. Add the
delete operation to randomized reference-vs-indexed sequences.

```go
func TestReplayTransactionDeleteUndoRedo(t *testing.T) {
    snapshot := deletionSnapshot(t)
    deleted, err := replay.Replay(snapshot)
    require.NoError(t, err)
    assert.Equal(t, []domain.EntityID{"transaction-b"}, transactionIDs(deleted.Effective))

    snapshot.Cursor = 0
    restored, err := replay.Replay(snapshot)
    require.NoError(t, err)
    assert.Equal(t,
        []domain.EntityID{"transaction-a", "transaction-b"},
        transactionIDs(restored.Effective),
    )
}
```

- [ ] **Step 2: Write failing codec, fold, and tombstone tests**

Assert the deletion payload round-trips as `{}` and malformed extra fields are refused. Capture the
effective snapshot before local commit, fold, reopen, and compare the fresh committed state with
the pre-commit effective state while asserting the deleted transaction's `external_identities` row
remains.

Lock the already-supported transaction tombstone rule with the deletion-fold fixture:

```go
func TestCommittedProfileAllowsDeletedTransactionExternalIdentityTombstone(t *testing.T) {
    profile := validProfile(t)
    deletedID := profile.Transactions[0].ID
    profile.Transactions = profile.Transactions[1:]
    require.NoError(t, profile.Validate())
    assert.Equal(t, deletedID, profile.ExternalIdentities[0].EntityID)
}
```

Do not broaden the existing exception: unknown account, merchant, category, and group mappings
remain invalid, as do duplicate external and duplicate local-provider mappings.

- [ ] **Step 3: Write failing refresh-rebase tests**

Table-test present, absent, partial, and empty target sets; assert operation identity/order survives
partial shrink, the count cursor decrements when an active operation disappears, and redo-tail
discard remains unchanged. Replay every rebased result and assert a retained deletion never flashes
back into effective state.

- [ ] **Step 4: Run focused tests and verify RED**

```bash
MONEYFLOW_SKIP_PERF=1 go test ./internal/domain ./internal/replay ./internal/store/sqlite \
  -run 'Test(ReplayTransactionDelete|CommittedProfileAllowsDeleted|DeletePayload|Fold.*Delete|ProviderRebase.*Delete|Schema|WriteItemUnion)' \
  -count=1
```

Expected: FAIL because replay, codec, tombstone validation, rebase, and schema v7 are absent.

- [ ] **Step 5: Implement reference and indexed replay deletion**

Add the operation to both replay switches. Validate every target against the current intermediate
state before filtering. The indexed path filters once per operation and rebuilds the transaction
index after removal so later operations cannot resolve stale slice offsets:

```go
func deleteTransactions(
    profile domain.CommittedProfile,
    targets []domain.EntityID,
) (domain.CommittedProfile, error) {
    targetSet := make(map[domain.EntityID]struct{}, len(targets))
    for _, target := range targets {
        targetSet[target] = struct{}{}
    }
    kept := make([]domain.TransactionRecord, 0, len(profile.Transactions)-len(targets))
    for _, transaction := range profile.Transactions {
        if _, deleting := targetSet[transaction.ID]; deleting {
            delete(targetSet, transaction.ID)
            continue
        }
        kept = append(kept, transaction)
    }
    if len(targetSet) != 0 {
        return domain.CommittedProfile{}, errors.New("replay delete: target is missing")
    }
    profile.Transactions = kept
    return profile, nil
}
```

Drill identities remain unchanged; no taxonomy entity is retired.

- [ ] **Step 6: Implement codec, tombstone validation, and rebase**

Encode/decode `TransactionDeletePayload` as the strict version-one empty object. Preserve the
existing profile-validation rule that an absent transaction mapping is a valid tombstone, while
collisions with a live entity or second mapping remain invalid.

Include `OperationTransactionDelete` in `providerTransactionScoped`; ordinary surviving-target
filtering supplies the exact rebase rule. Do not apply hide-intent cancellation to delete.

- [ ] **Step 7: Install the complete schema-version-7 write-item union**

Set `CurrentSchemaVersion = 7`, add `transaction.delete` to the journal operation-type CHECK, add
`item_kind TEXT NOT NULL CHECK (item_kind IN ('update', 'delete'))`, and install the complete union
CHECK from the approved spec. Add `WriteItemKind`, `WriteItem.Kind`, `WriteResult.Kind`, and
`WriteResult.AlreadyAbsent`; include them in clone/validate/digest/SQL insert/scan/result paths.

Use the existing nullable column representation in this exact logical shape:

```sql
CHECK (
  (item_kind = 'delete'
    AND requested_merchant_local_id IS NULL
    AND requested_merchant_name IS NULL
    AND requested_category_external_id IS NULL
    AND requested_hidden IS NULL
    AND expectation_kind IS NULL
    AND expected_merchant_external_id IS NULL
    AND new_group_key IS NULL
    AND group_leader = 0)
  OR
  (item_kind = 'update'
    AND (requested_merchant_name IS NOT NULL
      OR requested_category_external_id IS NOT NULL
      OR requested_hidden IS NOT NULL))
)
```

Add `already_absent INTEGER NOT NULL CHECK(already_absent IN (0, 1))` to
`provider_write_results`; the Go validator requires it to be false for update results. The result
kind is loaded through the existing item/result join and must agree with the item.

All existing write planners and synthetic fixtures must explicitly create `WriteItemUpdate`; zero
kind is invalid. Delete rows must reject every update/expectation/group field; update rows must
require at least one requested update field and retain the existing merchant expectation rules.
Add direct invalid-insert tests for both union arms.

Do not add an `ALTER`, copy, or migration path. Add a test that a version-6 database returns
`schema_incompatible` before querying version-7 objects.

- [ ] **Step 8: Run replay/store property and performance gates**

```bash
MONEYFLOW_SKIP_PERF=1 go test ./internal/domain ./internal/replay ./internal/store/sqlite -count=1
make test-store
```

Expected: PASS, including fold failure atomicity, randomized replay equivalence, restart, journal
ceiling, 100,000-target replay, and retained external mapping checks.

- [ ] **Step 9: Run the task gate and commit**

```bash
git add internal/domain/entities_test.go internal/replay \
  internal/store/provider_write.go internal/store/provider_write_test.go \
  internal/store/sqlite/codec.go internal/store/sqlite/codec_test.go \
  internal/store/sqlite/schema/profile.sql internal/store/sqlite/initialize.go \
  internal/store/sqlite/initialize_test.go internal/store/sqlite/schema_test.go \
  internal/store/sqlite/provider_write.go internal/store/sqlite/provider_write_test.go \
  internal/store/sqlite/provider_write_failure_test.go internal/app/provider_write_plan.go \
  internal/app/provider_write_plan_test.go internal/app/provider_write_property_test.go \
  internal/app/provider_write_plan_benchmark_test.go \
  internal/app/provider_write_performance_test.go internal/app/provider_write_test.go \
  internal/store/sqlite/fold_test.go internal/store/sqlite/fold_property_test.go \
  internal/store/sqlite/journal_test.go internal/store/sqlite/performance_test.go
git commit -m "feat: replay and persist transaction deletion"
```

---

### Task 3: Add Application Deletion, Duplicate Projection, Review, and Capabilities

**Files:**

- Create: `internal/app/delete.go`
- Create: `internal/app/delete_test.go`
- Create: `internal/app/duplicates.go`
- Create: `internal/app/duplicates_test.go`
- Modify: `internal/app/actions.go`
- Modify: `internal/app/actions_test.go`
- Modify: `internal/app/profile_service.go`
- Modify: `internal/app/profile_service_test.go`
- Modify: `internal/app/capabilities.go`
- Modify: `internal/app/capabilities_test.go`
- Modify: `internal/app/review.go`
- Modify: `internal/app/review_test.go`
- Modify: `internal/app/provider_write_plan.go`
- Modify: `internal/app/provider_write_plan_test.go`

**Interfaces:**

- Consumes: Task 1 duplicate groups, Task 2 replay/store support, existing `MutationRequest`,
  `ResolveTargets`, `SelectionValue`, `ViewState`, and `ReviewProjection`.
- Produces: `BuildDeleteMutation`, `ProjectDuplicates`, implemented action/capability entries, and
  deletion/vacuous review annotations.

- [ ] **Step 1: Write failing mutation-builder tests**

Table-test selection winning over focus, focused detail fallback, aggregate/no-focus rejection,
aggregate selection rejection, already-deleted rejection, sorted bulk targets, one operation/undo
unit, and selection disposition:

```go
func TestBuildDeleteMutationUsesSelectionBeforeFocus(t *testing.T) {
    snapshot := snapshotWithTransactions(t)
    plan, err := app.BuildDeleteMutation(snapshot, app.MutationRequest{
        Action: app.ActionDeleteTransaction,
        ExpectedRevision: snapshot.Revision,
        State: detailViewState(),
        Selection: exactSelection(t, "transaction-b", "transaction-a"),
        Target: &app.RowTarget{Kind: app.IdentityTransaction, Identity: "transaction-c"},
    }, fixedOperationMetadata())
    require.NoError(t, err)
    assert.Equal(t,
        []domain.EntityID{"transaction-a", "transaction-b"},
        plan.Operation.Targets,
    )
    assert.Equal(t, app.SelectionCleared, plan.SelectionDisposition)
}
```

Add an end-to-end service test that appends at the authoritative revision, reprojects totals and
aggregates immediately, clears bulk selection only, then proves undo/redo/restart.

- [ ] **Step 2: Write failing duplicate-projection tests**

Use a filtered result larger than `DefaultWindowLimit`; place duplicates outside the current table
window and assert `ProjectDuplicates` still finds them. Test raw provider labels by constructing
`ProviderState.Allocations` for two suffixed local merchants. Assert strict group/row bounds,
expected-revision conflict, exact transient selection decoration, pending flags, no-result status,
and deterministic output.

- [ ] **Step 3: Write failing review and capability tests**

Assert action registry entries for `D` and `x` become implemented. `ActionFindDuplicates` is
available offline; `ActionDeleteTransaction` is available only on transaction detail projections
and becomes unavailable during any provider write batch.

Review must produce singular/plural deletion labels, bounded target details, and this exact vacuous
annotation contract:

```go
assert.Equal(t, 0, operation.AffectedCount)
assert.Equal(t, "affects 0 transactions", operation.Annotation)
```

- [ ] **Step 4: Run focused tests and verify RED**

```bash
go test ./internal/app \
  -run 'Test(BuildDeleteMutation|Mutate.*Delete|ProjectDuplicates|DeleteCapability|Review.*Delete|Review.*Vacuous)' \
  -count=1
```

Expected: FAIL because builders, projection, registry implementation, and review labels are absent.

- [ ] **Step 5: Implement `BuildDeleteMutation` and wire `Service.Mutate`**

Use `ResolveTargets`, verify targets exist in `snapshot.Effective.Transactions`, and return one
typed operation:

```go
operation := domain.Operation{
    ID: metadata.OperationID, Type: domain.OperationTransactionDelete,
    PayloadVersion: 1, CreatedRevision: snapshot.Revision, CreatedAt: metadata.CreatedAt,
    Targets: targets, TransactionDelete: &domain.TransactionDeletePayload{},
}
```

Require a transaction-detail analytical state and resolved transaction targets. Reject every
nonempty `ResolvedTargets.EntityIDs`; deletion never expands an aggregate row or aggregate
selection even though other edit builders may do so.

Return `SelectionCleared` only when the submitted selection is nonempty. Add the action to
`buildMutationPlan` and provider mutation validation.

- [ ] **Step 6: Implement `ProjectDuplicates` over the complete filtered result**

Validate state/window, lock `service.interactions`, refresh, compare expected revision, resolve the
same session/query as `ProjectView`, and call `service.Query` without slicing first. Build the
matching-label map by joining active merchant `ExternalIdentity` rows to
`LabelAllocation{Namespace, ExternalID}` and indexing `ProviderLabel` by the identity's local
merchant ID; fall back to the effective merchant label. Call `analytics.FindDuplicates`, take the
bounded group slice, flatten only that slice, take its bounded row slice, and reconstruct partial or
complete projected groups with their original stable presentation numbers. Return both normalized
group and row windows so renderers never infer offsets.

Never expose `ExternalIdentity`, allocation suffix fields, provider IDs, or operation payloads in
the projection. Return copied `domain.Transaction` values only to in-process renderers; Task 7's
wire mapper selects the approved presentation subset.

- [ ] **Step 7: Implement action, capability, and review behavior**

Set `Implemented: true` for `ActionFindDuplicates` and `ActionDeleteTransaction` without changing
their keys/help text. Add both capabilities, preserve visible-unavailable reasons, disable deletion
during write batches, and keep duplicate analysis offline. Implement `CapabilitiesForState` by
starting with the profile-global snapshot capabilities and marking delete unavailable unless
`state.Current.Mode == domain.ResultModeDetail`; use it from `ProjectView`, mutation results, and
TUI state refresh so renderer availability cannot drift.

Teach review traversal that deletion affects its current targets before applying the operation.
Change `buildReviewProjection` to capture bounded `ReviewTarget` values from the intermediate
pre-operation state rather than returning IDs that are later looked up in the final effective
snapshot; deleted rows no longer exist in that final snapshot. Existing operation details retain
their current before-operation meaning.

For merchant label/merge, compute affected membership from the intermediate state; if later active
deletions make it vacuous at preparation/review, retain the operation and annotate zero rather than
marking it unsupported.

- [ ] **Step 8: Run package tests and commit**

```bash
go test ./internal/app -count=1
```

Run the required task gate, then:

```bash
git add internal/app/actions.go internal/app/actions_test.go internal/app/delete.go \
  internal/app/delete_test.go internal/app/duplicates.go internal/app/duplicates_test.go \
  internal/app/profile_service.go internal/app/profile_service_test.go \
  internal/app/capabilities.go internal/app/capabilities_test.go internal/app/review.go \
  internal/app/review_test.go internal/app/provider_write_plan.go \
  internal/app/provider_write_plan_test.go
git commit -m "feat: expose deletion and duplicate projections"
```

---

### Task 4: Port the One-Attempt Monarch Delete Adapter

**Files:**

- Modify: `internal/provider/provider.go`
- Modify: `internal/provider/provider_test.go`
- Modify: `internal/provider/architecture_test.go`
- Modify: `internal/provider/monarch/queries.go`
- Modify: `internal/provider/monarch/queries_test.go`
- Modify: `internal/provider/monarch/write.go`
- Modify: `internal/provider/monarch/write_test.go`
- Modify: `internal/provider/monarch/live_test.go`

**Interfaces:**

- Consumes: existing Monarch authenticated GraphQL client and provider write failure taxonomy.
- Produces: `provider.TransactionDeleteResult` and the third `provider.Writer` method declared in
  Cross-Task Interfaces.

- [ ] **Step 1: Write failing provider-contract and request-shape tests**

Extend all fake/contract writers with `DeleteTransaction`. Add a loopback server assertion that the
request contains only the external transaction ID and the exact operation name/query:

```go
result, err := client.DeleteTransaction(context.Background(), "provider-transaction-a")
require.NoError(t, err)
assert.Equal(t, "provider-transaction-a", result.TransactionExternalID)
assert.False(t, result.AlreadyAbsent)
assert.Equal(t, 1, requests.Load(), "adapter performs exactly one attempt")
```

Assert `deleted: true`, characterized payload not-found, and HTTP 404 normalize to success. Assert
`deleted: false` with unclassifiable payload errors maps to the existing rejected/incomplete write
classification without exposing raw message text.

- [ ] **Step 2: Write failing transport/error tests**

Table-test authentication, rate limit with bounded retry-after, unavailable, timeout/unknown
outcome, malformed JSON, missing payload, and invalid empty local ID. Assert no adapter retry and
assert `failure.Error()` contains no GraphQL message or external ID.

- [ ] **Step 3: Run focused tests and verify RED**

```bash
go test ./internal/provider ./internal/provider/monarch \
  -run 'Test(DeleteTransaction|WriterContract|MonarchMutationSurface)' -count=1
```

Expected: FAIL because the writer method, mutation, and response normalizer do not exist.

- [ ] **Step 4: Implement the provider-neutral method and Monarch mutation**

Port only `Common_DeleteTransactionMutation` from the vendored Python client. Decode only
`deleteTransaction.deleted` plus the existing allowlisted error classification. Do not parse error
message text to guess absence.

```go
func (client *Client) DeleteTransaction(
    ctx context.Context,
    externalID string,
) (provider.TransactionDeleteResult, error) {
    if externalID == "" {
        return provider.TransactionDeleteResult{}, provider.NewWriteFailure(provider.WriteRequestInvalid)
    }
    // one authenticated GraphQL request; no retry
}
```

Keep `internal/provider/monarch` independent of store/app and update the architecture test's allowed
mutation surface to update plus delete only.

- [ ] **Step 5: Add the opt-in live characterization harness**

Require `MONEYFLOW_MONARCH_LIVE_DELETE_TRANSACTION_ID` for a user-designated disposable row and an
independent explicit opt-in flag. Allow an optional
`MONEYFLOW_MONARCH_LIVE_BANK_DELETE_TRANSACTION_ID`. The test must refuse to choose from imported
data, print only classifications/counts, and perform complete refresh after each supplied outcome.

```bash
go test ./internal/provider/monarch -run TestLiveDeleteDisposableTransaction -count=1
```

Expected without explicit environment inputs: SKIP. Do not run it with real credentials during the
automated task.

- [ ] **Step 6: Run package tests and commit**

```bash
go test ./internal/provider ./internal/provider/monarch -count=1
```

Run the task gate, then:

```bash
git add internal/provider/provider.go internal/provider/provider_test.go \
  internal/provider/architecture_test.go internal/provider/monarch/queries.go \
  internal/provider/monarch/queries_test.go internal/provider/monarch/write.go \
  internal/provider/monarch/write_test.go internal/provider/monarch/live_test.go
git commit -m "feat: add Monarch transaction deletion"
```

---

### Task 5: Plan, Execute, and Finalize Provider Delete Items

**Files:**

- Modify: `internal/store/sqlite/provider_write.go`
- Modify: `internal/store/sqlite/provider_write_test.go`
- Modify: `internal/store/sqlite/provider_write_failure_test.go`
- Modify: `internal/app/provider_write_plan.go`
- Modify: `internal/app/provider_write_plan_test.go`
- Modify: `internal/app/provider_write_property_test.go`
- Modify: `internal/app/provider_write_plan_benchmark_test.go`
- Modify: `internal/app/provider_write_performance_test.go`
- Modify: `internal/app/provider_write.go`
- Modify: `internal/app/provider_write_test.go`
- Modify: `internal/store/provider_write_finalization.go`
- Create: `internal/store/provider_write_finalization_test.go`

**Interfaces:**

- Consumes: Tasks 2-4 replay absence, deletion operation attribution, and provider delete method.
- Produces: deterministic delete planning over Task 2's schema-enforced union, per-kind
  dispatch/recovery, response-adjusted finalization, and whole-prefix reconciliation.

- [ ] **Step 1: Extend failing union round-trip and claim tests for delete items**

Using the schema contract established in Task 2, add valid delete item/result prepare, load, claim,
record, and failure-rollback cases. Prove batch/transaction uniqueness applies across both kinds,
delete items never wait behind new-name leaders, and item kind/attempt state survive reopen.

- [ ] **Step 2: Write failing deterministic planner tests**

Cover one committed/effective absence, update then delete, mixed item positions, operation
attribution, leader-group exclusion, and provider-external-ID requirement:

```go
func TestPlanProviderWriteDeleteSupersedesUpdates(t *testing.T) {
    plan := planPrefix(t,
        hideOperation("operation-hide", "transaction-a"),
        deleteOperation("operation-delete", "transaction-a"),
    )
    require.Len(t, plan.Items, 1)
    item := plan.Items[0]
    assert.Equal(t, store.WriteItemDelete, item.Kind)
    assert.Equal(t,
        []string{"operation-hide", "operation-delete"},
        item.OriginatingOperationIDs,
    )
    assert.Nil(t, item.RequestedHidden)
    assert.Empty(t, item.NewGroupKey)
}
```

Add same-prefix delete-all cases for merchant merge and label. They must remain in the frozen
prefix, produce no update item, show zero affected transactions, and not fail preparation.

- [ ] **Step 3: Write failing worker crash/failure tests**

Use a scripted writer and real temporary SQLite profile. Assert:

- delete success and proven not-found persist success;
- crash after claim/send but before result allows delete resend;
- the same attempted-without-result state for update parks reconcile-only before network;
- delete unknown outcome resends only within the five-attempt budget;
- rate limit/reconnect/lease loss/identity mismatch preserve existing behavior;
- unclassifiable refusal parks reconcile-only; and
- stop-and-reconcile removes the entire prefix, leaving an absent applied delete absent and a
  present refused/unsent delete present.

- [ ] **Step 4: Run focused tests and verify RED**

```bash
MONEYFLOW_SKIP_PERF=1 go test ./internal/store ./internal/store/sqlite ./internal/app \
  -run 'Test(WriteItemUnion|PlanProviderWrite.*Delete|ProviderWrite.*Delete|ProviderWrite.*Crash|ProviderWrite.*Reconcile|Vacuous)' \
  -count=1
```

Expected: FAIL because item kind, union constraints, delete planning, and dispatch are absent.

- [ ] **Step 5: Complete delete-item persistence behavior**

Use Task 2's `Kind` columns in claim eligibility and result recording. Delete items are ordinary
concurrency candidates and never participate in the SQL new-group leader dependency. Preserve
kind, `AlreadyAbsent`, attempt count, and normalized result across process reopen. Do not change
the installed schema in this task.

- [ ] **Step 6: Implement delete planning and vacuous structural rules**

Build committed/effective transaction maps. Iterate committed transaction IDs deterministically;
when absent from effective, emit one delete item and suppress update fields. Preserve all ordered
originating operation IDs that affected the transaction before deletion.

The authoritative productivity check accepts a merchant label/merge that originally had members
but reaches zero only through later same-prefix deletion. It emits no provider item and remains in
the frozen digest. A label on such an empty provider-owned merchant folds locally; the next complete
refresh restores `LabelAllocation.ProviderLabel`.

- [ ] **Step 7: Implement per-kind worker dispatch and recovery**

Dispatch without optional-field inference:

```go
switch item.Kind {
case store.WriteItemUpdate:
    result, err = writer.UpdateTransaction(ctx, providerTransactionUpdate(item))
case store.WriteItemDelete:
    deleted, deleteErr := writer.DeleteTransaction(ctx, item.TransactionExternalID)
    result, err = normalizeProviderDeleteResult(item, deleted, deleteErr)
default:
    err = provider.NewWriteFailure(provider.WriteRequestInvalid)
}
```

Replace `firstAttemptedPendingWriteItem` with a check that parks only uncertain update items. A
pending delete with attempts remaining stays claimable. Persist `AlreadyAbsent` as normalized
counts-only result state; never persist provider error messages.

- [ ] **Step 8: Implement finalization and identity tombstones**

Successful delete results need no field override; the prepared effective snapshot already lacks the
row. Finalization compares fresh committed state against the pre-commit effective state adjusted by
normalized update overrides and successful absence. Retain transaction `ExternalIdentity` rows in
the fold plan.

Do not derive partial mappings from stop-and-reconcile results. The authoritative refresh snapshot
alone decides identity state while the transaction removes the whole frozen prefix and batch.

- [ ] **Step 9: Run property, failure, concurrency, and performance gates**

```bash
MONEYFLOW_SKIP_PERF=1 go test ./internal/store ./internal/store/sqlite ./internal/app -count=1
go test ./internal/app -run '^$' \
  -bench 'Benchmark(PlanProviderWrite|FinalizeProviderWrite).*100K' -count=1
make test-store
make test-race
```

Expected: PASS with deterministic positions/digests, SQL rollback equivalence, cross-process lease
handoff, no refresh-fold race, and the existing planning/finalization ceiling.

- [ ] **Step 10: Run the task gate and commit**

```bash
git add internal/store/provider_write_finalization.go internal/store/provider_write_finalization_test.go \
  internal/store/sqlite/provider_write.go \
  internal/store/sqlite/provider_write_test.go internal/store/sqlite/provider_write_failure_test.go \
  internal/app/provider_write_plan.go internal/app/provider_write_plan_test.go \
  internal/app/provider_write_property_test.go internal/app/provider_write_plan_benchmark_test.go \
  internal/app/provider_write_performance_test.go internal/app/provider_write.go \
  internal/app/provider_write_test.go
git commit -m "feat: write transaction deletions durably"
```

---

### Task 6: Add TUI Duplicate Review and Deletion Confirmation

**Files:**

- Create: `internal/tui/delete.go`
- Create: `internal/tui/delete_test.go`
- Create: `internal/tui/duplicates.go`
- Create: `internal/tui/duplicates_format.go`
- Create: `internal/tui/duplicates_test.go`
- Create: `internal/tui/duplicates_format_test.go`
- Modify: `internal/tui/model.go`
- Modify: `internal/tui/update.go`
- Modify: `internal/tui/layout.go`
- Modify: `internal/tui/keymap.go`
- Modify: `internal/tui/help_test.go`
- Modify: `internal/tui/transaction_info.go`
- Modify: `internal/tui/review_format.go`
- Modify: `internal/tui/review_format_test.go`
- Modify: `internal/tui/overlay_test.go`
- Modify: `internal/tui/model_test.go`

**Interfaces:**

- Consumes: Task 3 `ProjectDuplicates`, shared mutation endpoint/service method, selection token,
  transaction-info overlay, and review projection.
- Produces: `overlayDuplicates`, `overlayDeleteConfirmation`, keyboard routing, responsive bounded
  rendering, and count-only staging confirmation.

- [ ] **Step 1: Write failing overlay lifecycle and keyboard tests**

Drive the model with Bubble Tea messages. Assert `D` scans the full filtered result and opens only
when groups exist; no-input/no-group announces status without an empty overlay. In the overlay test
`j/k`, arrows, Home, PageUp/PageDown, Space, `i`/Enter, `h`, `x`, and Esc.

```go
func TestDuplicateOverlayStagesSelectedDeletionAndReprojects(t *testing.T) {
    model := duplicateModel(t)
    model = press(t, model, "D")
    model = press(t, model, "space")
    model = press(t, model, "x")
    require.Equal(t, overlayDeleteConfirmation, model.overlay)
    assert.Contains(t, strings.Join(model.RenderScreen().Overlay, "\n"), "1 transaction")

    model = press(t, model, "enter")
    assert.Equal(t, overlayDuplicates, model.overlay)
    assert.Equal(t, 0, model.duplicates.projection.SelectionCount)
    assert.Less(t, model.duplicates.projection.TotalTransactions, 2)
}
```

Test cancellation and revision conflict change nothing; stale confirmation reprojects and requires
explicit re-invocation. Close the overlay before testing `u`/`U` restore/reapply.

- [ ] **Step 2: Write failing formatting/responsiveness tests**

Assert accurate group/transaction counts, matching label plus ordinary display context, exact money,
account, selection/pending flags, deterministic rows, narrow terminal clipping, minimum 80x24
behavior, and paging. Keep semantic overlay data financial but do not create screenshot files.

- [ ] **Step 3: Run focused tests and verify RED**

```bash
go test ./internal/tui \
  -run 'Test(Duplicate|DeleteConfirmation|Review.*Delete|Keymap.*Duplicates)' -count=1
```

Expected: FAIL because overlay states/routes/renderers do not exist.

- [ ] **Step 4: Implement overlay state and projection loading**

Add focused state structs:

```go
type duplicateState struct {
    projection app.DuplicateProjection
    cursor, groupOffset int
    selection app.SelectionValue
    err string
}

type deleteConfirmationState struct {
    returnOverlay overlayKind
    request app.MutationRequest
    count int
    err string
}
```

`openDuplicates` requests bounded groups at the service's current revision. Every accepted hide or
delete uses the shared service mutation and reloads from `ProjectDuplicates`; it never edits a
private transaction copy.

- [ ] **Step 5: Implement key routing and count-only confirmation**

Add the two overlays to `routeOverlay`. Enter behaves as transaction info in duplicate review and
as accept in delete confirmation. `x` resolves overlay selection before focused row, opens the
count-only surface, and does not append until Enter. On success, announce the staged count and
direct the user to `w` without extra commit ceremony.

Render deletion review as `Delete transaction(s)` and zero-item structural operations as
`affects 0 transactions`; provider progress counts only actual requests.

- [ ] **Step 6: Run TUI and parity-safe focused gates**

```bash
go test ./internal/tui -count=1
make parity
```

Expected: PASS. `make parity` must not write artifacts. Do not run a parity-update target in this
task.

- [ ] **Step 7: Run the task gate and commit**

```bash
git add internal/tui/delete.go internal/tui/delete_test.go internal/tui/duplicates.go \
  internal/tui/duplicates_format.go internal/tui/duplicates_test.go \
  internal/tui/duplicates_format_test.go internal/tui/model.go internal/tui/update.go \
  internal/tui/layout.go internal/tui/keymap.go internal/tui/help_test.go \
  internal/tui/transaction_info.go internal/tui/review_format.go \
  internal/tui/review_format_test.go internal/tui/overlay_test.go internal/tui/model_test.go
git commit -m "feat: add TUI duplicate deletion workflow"
```

---

### Task 7: Add the Bounded HTTP Contract and Accessible Web Workflow

**Files:**

- Create: `internal/api/duplicates.go`
- Create: `internal/api/duplicates_test.go`
- Modify: `internal/api/server.go`
- Modify: `internal/api/profiles.go`
- Modify: `internal/api/openapi_test.go`
- Modify: `internal/api/performance_test.go`
- Modify: `internal/api/mutations_test.go`
- Modify: `internal/api/security_test.go`
- Modify: `internal/api/review.go`
- Modify: `internal/api/review_test.go`
- Modify: `web/src/lib/api/client.ts`
- Modify: `web/src/lib/api/client.test.ts`
- Modify: `web/src/lib/api/schema.d.ts`
- Create: `web/src/lib/controller/duplicates.ts`
- Create: `web/src/lib/controller/duplicates.test.ts`
- Create: `web/src/components/editing/DuplicateReview.svelte`
- Create: `web/src/components/editing/DuplicateReview.test.ts`
- Create: `web/src/components/editing/DeleteConfirmation.svelte`
- Create: `web/src/components/editing/DeleteConfirmation.test.ts`
- Modify: `web/src/lib/shortcuts.ts`
- Modify: `web/src/lib/shortcuts.test.ts`
- Modify: `web/src/components/AppShell.svelte`
- Modify: `web/src/components/AppShell.test.ts`
- Modify: `web/src/components/editing/ReviewDrawer.svelte`
- Modify: `web/src/components/editing/ReviewDrawer.test.ts`
- Modify: `web/src/lib/controller/view-controller.svelte.ts`
- Modify: `web/tests/keyboard.spec.ts`
- Modify: `web/tests/editing.spec.ts`
- Modify: `web/tests/accessibility.spec.ts`
- Modify: `web/tests/restart.spec.ts`

**Interfaces:**

- Consumes: Task 3 `ProjectDuplicates`, existing query codec/selection/mutation controller, and
  Task 6 keyboard contract.
- Produces: `POST /api/v1/profiles/{profile_id}/duplicates`, generated TypeScript wire types,
  `DuplicateController`, and accessible duplicate/delete dialogs.

- [ ] **Step 1: Write failing API contract and security tests**

Define a request with version, expected revision, canonical query, selection, group window, and row
window. Define a response with string revision, canonical query, group/row totals, bounded groups,
exact `Money`, flags, and opaque targets.

Test profile scope, base path, no-store, request/body limits, stale revision, invalid query/window,
and absence of provider identities. Prove POST duplicate projection succeeds without a mutation
token while `transaction.delete` through `/mutations` still requires token, Origin, and Fetch
Metadata.

```go
func TestDuplicateProjectionIsReadOnlyPost(t *testing.T) {
    response := requestJSON(t, server, "/api/v1/duplicates", duplicateBody(t))
    assert.Equal(t, http.StatusOK, response.Code)
    assert.Equal(t, "no-store", response.Header().Get("Cache-Control"))
    assert.NotContains(t, response.Body.String(), "provider-transaction")
}
```

- [ ] **Step 2: Write failing web controller/component tests**

Mock the bounded projection and mutation API. Assert `D` opens, keyboard/pointer actions match,
Space selection is transient, `i`/Enter displays details, hide/delete re-fetch groups, groups
collapse below two, Esc restores finance-grid focus, and analytical URL/cursor/scroll state remain.

Use Testing Library plus axe for focus trap, accessible name, visible buttons, and announced status.
Delete confirmation displays count only and a stale mutation never retries automatically.
Assert the existing pending-review drawer renders `affects 0 transactions` without adding to its
provider item count.

- [ ] **Step 3: Run focused tests and verify RED**

```bash
go test ./internal/api -run 'Test(Duplicate|DeleteMutation|OpenAPI)' -count=1
bun test --cwd web \
  src/lib/controller/duplicates.test.ts \
  src/components/editing/DuplicateReview.test.ts \
  src/components/editing/DeleteConfirmation.test.ts
```

Expected: FAIL because the route, wire types, controller, and components do not exist.

- [ ] **Step 4: Implement the read-only POST endpoint**

Register `duplicates` in both legacy and `/p/{profile_id}` route allowlists but not in mutation
security route sets. Parse revision/query/selection with existing helpers, call
`ProjectDuplicates`, and map only approved fields. Use canonical money strings through the existing
`moneyToWire` function.

```go
const DuplicateSchemaVersion = "1"

type DuplicateBody struct {
    Version          string `json:"version"`
    ExpectedRevision string `json:"expected_revision" pattern:"^[0-9]+$" maxLength:"20"`
    Query            string `json:"query" maxLength:"65536"`
    Selection        string `json:"selection,omitempty" maxLength:"1468006"`
    GroupWindow      Window `json:"group_window"`
    RowWindow        Window `json:"row_window"`
}
```

Apply `MaxViewBodyBytes` and existing safe problem envelopes. No response field may contain raw
provider IDs or an unbounded target list.

- [ ] **Step 5: Implement client/controller and accessible dialogs**

Add typed `projectDuplicates`, a controller that uses the current profile route/query/revision and
re-fetches after accepted mutations, and `D`/`x` local shortcut IDs. Build dialogs with kit-ui
focus trapping and buttons matching all keyboard actions.

Do not duplicate grouping in TypeScript. Do not put selection in the URL. Close restores the
existing grid focus and leaves owned history unchanged.

- [ ] **Step 6: Regenerate/check API types without committing embedded assets**

```bash
make web-generate
make web-check
git status --short
```

Expected: only source/schema files intended by this task are tracked. `web/dist` and
`internal/web/dist` remain ignored and absent from the staged diff.

- [ ] **Step 7: Run API/web integration and browser tests**

```bash
go test ./internal/api -count=1
bun test --cwd web
make test-editing-e2e
make verify-web
```

Expected: PASS for base-path/profile routing, no-token read projection, protected deletion,
keyboard equivalence, accessibility, restart, and cross-renderer revision observation. No browser
screenshots may be staged.

- [ ] **Step 8: Run the task gate and commit**

```bash
git add internal/api/duplicates.go internal/api/duplicates_test.go internal/api/server.go \
  internal/api/profiles.go internal/api/openapi_test.go internal/api/performance_test.go \
  internal/api/mutations_test.go internal/api/security_test.go internal/api/review.go \
  internal/api/review_test.go web/src/lib/api/client.ts \
  web/src/lib/api/client.test.ts web/src/lib/api/schema.d.ts \
  web/src/lib/controller/duplicates.ts web/src/lib/controller/duplicates.test.ts \
  web/src/components/editing/DuplicateReview.svelte \
  web/src/components/editing/DuplicateReview.test.ts \
  web/src/components/editing/DeleteConfirmation.svelte \
  web/src/components/editing/DeleteConfirmation.test.ts web/src/lib/shortcuts.ts \
  web/src/lib/shortcuts.test.ts web/src/components/AppShell.svelte \
  web/src/components/AppShell.test.ts web/src/components/editing/ReviewDrawer.svelte \
  web/src/components/editing/ReviewDrawer.test.ts \
  web/src/lib/controller/view-controller.svelte.ts \
  web/tests/keyboard.spec.ts web/tests/editing.spec.ts web/tests/accessibility.spec.ts \
  web/tests/restart.spec.ts
git commit -m "feat: add web duplicate deletion workflow"
```

---

### Task 8: Close Cross-Renderer, Performance, Parity, Privacy, and Documentation Gates

**Files:**

- Modify: `internal/app/editing_characterization_test.go`
- Modify: `internal/app/provider_integration_test.go`
- Modify: `internal/api/editing_integration_test.go`
- Modify: `internal/api/performance_test.go`
- Modify: `internal/tui/editing_parity_test.go`
- Modify: `internal/tui/semantic_parity_test.go`
- Modify: `internal/tui/visual_golden_test.go`
- Modify: `tests/parity/test_semantic.py`
- Modify: `testdata/parity/frame_scenarios.json`
- Modify: `Makefile`
- Modify: `README.md`
- Modify: `docs/guide/keyboard-shortcuts.md`

**Interfaces:**

- Consumes: all completed slice interfaces.
- Produces: end-to-end correctness evidence, deterministic performance digests, reviewed semantic
  parity artifacts, privacy scanning, user documentation, and a fully green branch.

- [ ] **Step 1: Add failing cross-renderer and restart journeys**

Use one temporary profile opened by TUI/service and HTTP server. Stage deletion in one renderer,
observe revision/reprojection in the other, undo/redo after duplicate review closes, restart, and
commit. Add local and fake-Monarch paths; assert update-plus-delete yields only one delete request.

Add drill cases: delete the last row of a known merchant/category/account and expect normal empty
projection with unchanged URL; use a never-known key and expect invalid view.

- [ ] **Step 2: Add resurrection and reconcile integration tests**

With fake provider snapshots, prove:

```text
same external transaction ID after commit/abandon -> original local transaction ID
new external transaction ID for equivalent values -> fresh local transaction ID
retained pending delete during refresh -> row remains effectively absent
stop-and-reconcile remote absence -> absent, prefix removed
stop-and-reconcile remote presence -> present, prefix removed
```

Exercise refresh generation/revision/no-active-batch guards and process lease handoff.

- [ ] **Step 3: Add performance and privacy gates**

Make the 100,000-row duplicate benchmark/test assert exact groups, rows, and digest. Add mixed
update/delete planning and bounded API projection gates. Scan logs and safe problem bodies after
duplicate/delete failures for synthetic canaries representing labels, IDs, dates, search text, and
money; assert none appears.

```bash
MONEYFLOW_SKIP_PERF=1 go test ./internal/app ./internal/api ./internal/tui -count=1
make test-store
```

Expected: PASS. Then run supported timed gates on a quiet host and record any repository-supported
performance skip exactly.

- [ ] **Step 4: Add semantic parity characterization**

Drive the Python duplicate overlay to lock actual exact-date, Unicode-lowercase, same-account
grouping and its Space/i/h/x/Esc flow. Add Go semantic scenarios for staged deletion, confirmation
cancel/accept, group collapse, and review. Explicitly retain the named divergence: Python deletes
immediately; Go stages until `w`, then Enter.

If artifacts must change, use only:

```bash
make parity-update-python
make parity-update-go
```

Review the complete textual artifact diff and generated local previews. Never add generated
screenshots; they remain ignored output. Then run `make parity` and expect PASS without writes.

- [ ] **Step 5: Update current user documentation**

Document `D`, duplicate exactness, `x` staging, `u`/`U`, and `w` then Enter. State that deletion can
reappear if provider truth restores it and that no fuzzy matching or automatic winner selection is
performed. Do not rewrite historical specs/plans or Python invocation docs unrelated to Go v2.

- [ ] **Step 6: Run the full verification matrix**

```bash
make verify-go
make verify-web
make test-race
make parity
uv run pytest -v
uv run pyright moneyflow/
uv run ruff format --check moneyflow/ tests/
uv run ruff check moneyflow/ tests/
npx --yes markdownlint-cli@0.47.0 --config .markdownlint.json README.md 'docs/**/*.md'
.github/scripts/check-arrow-lists.sh
```

Expected: every command exits zero, except a repository-supported performance skip reported with
its exact gate. Run `git diff --check`, inspect the entire diff and staged file list, and prove no
`web/tests/screenshots`, `web/dist`, `internal/web/dist`, credentials, sessions, or personal data is
tracked.

- [ ] **Step 7: Commit the completed slice**

```bash
git add internal/app/editing_characterization_test.go \
  internal/app/provider_integration_test.go internal/api/editing_integration_test.go \
  internal/api/performance_test.go internal/tui/editing_parity_test.go \
  internal/tui/semantic_parity_test.go internal/tui/visual_golden_test.go \
  tests/parity/test_semantic.py testdata/parity/frame_scenarios.json \
  Makefile README.md docs/guide/keyboard-shortcuts.md
git commit -m "test: verify deletion and duplicate workflows"
```

- [ ] **Step 8: Perform optional live characterization only after the commit**

Run the opt-in live test only when the user supplies explicit disposable transaction IDs. Do not
select an arbitrary personal transaction. If the live behavior differs, add a focused failing
synthetic test, fix it, rerun the full gate, and create a new commit; never amend the verified slice
commit.

## Final Completion Audit

Before declaring the implementation complete, confirm each statement from the approved spec:

- `D` and `x` are implemented by the shared action registry and capability projection.
- Duplicate matching uses raw provider labels behind suffixes and exact approved key semantics.
- TUI and web expose the same selection, info, hide, delete, cancel, and navigation flow.
- Deletion is durable, reviewable, undoable, redoable, restart-safe, and remote only after commit.
- Local fold and response-adjusted provider finalization equivalence tests pass.
- Delete supersedes update, vacuous structural operations emit no request, and counts remain honest.
- Crash-uncertain updates park; crash-uncertain deletes may resend within the bounded policy.
- Stop and reconcile removes the entire prefix and cannot wedge refused deletion intent.
- Refresh present/absent/partial/empty/resurrection cases and external-ID tombstones pass.
- Schema v7 enforces the union and refuses v6 without migration.
- HTTP is bounded, exact-money, no-store, profile-scoped, and provider-identity blind.
- Performance tests validate nonempty deterministic digests.
- The final diff contains no fuzzy matching, export, unrelated provider work, MCP, Python shim,
  migration code, generated screenshots, or embedded web dist assets.
- Every automated-verified change is committed on `go-port` and nothing is pushed.
