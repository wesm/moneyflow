# Go Port YNAB Read, Import, and Refresh Implementation Plan

> **For inline execution:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement
> this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Connect one Moneyflow profile to one YNAB budget, import and refresh a complete
exact-money snapshot, retain split details, and expose the same offline-capable workflow through
CLI, TUI, web, and MCP reads.

**Architecture:** A direct `net/http` adapter in `internal/provider/ynab` returns provider-neutral
domain snapshots. The existing application refresh planner and SQLite transaction remain
authoritative; schema version 11 adds one protected Split category and provider split-detail
storage. The onboarding coordinator owns attempt lifecycle while provider-specific Monarch and
YNAB flows own authentication and secret fields.

**Tech Stack:** Go standard library HTTP/JSON/crypto composition, modernc SQLite, Huma v2 with
`humago`, Cobra, Bubble Tea v2/Bubbles v2, Svelte 5, kit-ui, Bun/Vitest/Playwright, Testify.

## Global Constraints

- Work only on the already checked-out `go-port` branch. Do not switch, create, pull, rebase, or
  push a branch without a current explicit user instruction.
- Use test-driven development: add a focused failing test, observe the expected failure, implement
  the smallest contract, and rerun focused tests before broad gates.
- Commit each task after its focused and task-level gates pass. Never amend an existing commit.
- Keep the Go v2 schema install-only. Schema version 11 and every query that requires it must land
  together; do not add a migration or compatibility reader.
- Preserve pure-Go, no-CGO builds on Linux, macOS, and Windows.
- Represent all accounting values as signed integer minor units. Never introduce `float32`,
  `float64`, or SQLite REAL money columns.
- Use Huma only for Moneyflow's inbound API. Use the standard `net/http` client for outbound YNAB requests.
- Never persist or log tokens, account passwords, budget IDs/names, account/payee/category labels,
  memos, transaction dates, amounts, split descriptions, response bodies, or vault paths.
- Tests and fixtures contain synthetic financial data only. Live YNAB checks use an explicit
  environment token and a disposable Moneyflow home, and never write provider data.
- Generated `web/dist`, `internal/web/dist`, and browser screenshots remain ignored and uncommitted.
- Use Testify `assert`/`require` consistently with existing Go tests.
- Run `$roborev-fix` only when the current user explicitly invokes it for this implementation.

---

## Task 1: Split Provider Read and Write Capabilities

**Files:**

- Modify: `internal/provider/provider.go`
- Modify: `internal/provider/provider_test.go`
- Modify: `internal/provider/errors.go`
- Modify: `internal/provider/errors_test.go`
- Modify: `internal/provider/monarch/client.go`
- Modify: `internal/provider/monarch/client_test.go`
- Modify: `internal/provider/monarch/session_file.go`
- Modify: `internal/app/provider_refresh.go`
- Modify: `internal/app/provider_refresh_test.go`
- Modify: `internal/app/provider_write.go`
- Modify: `internal/app/provider_write_test.go`
- Modify: `internal/app/provider_scheduler.go`
- Modify: `internal/app/provider_scheduler_test.go`
- Modify: `internal/app/errors.go`
- Modify: `internal/app/errors_test.go`
- Modify: `internal/api/errors.go`
- Modify: `internal/api/errors_test.go`
- Modify: `internal/onboarding/runtime.go`
- Modify: `cmd/moneyflow/provider.go`
- Modify: `cmd/moneyflow/provider_test.go`
- Modify: `cmd/moneyflow/tui_shell.go`
- Modify: `cmd/moneyflow/web_dependencies.go`
- Modify: `internal/tools/webtestserver/main.go`

**Interfaces:**

- Produces: `provider.SourceState`, `provider.ReaderSource`, `provider.WriterSource`, and `provider.SnapshotResult`.
- Produces: `provider.CodeMoneyMismatch` with manual-action scheduler classification.
- Changes: `provider.Reader.FetchSnapshot` returns identity and snapshot in one result; it no
  longer has a separate `ProbeIdentity` method.
- Changes: `app.ProviderRuntime` accepts separate `ReadSource` and optional `WriteSource` values.
- Preserves: Monarch still probes `subscription.id` before returning its snapshot and still
  supplies both read and write sources.

- [ ] **Step 1: Add compile-time tests for the capability split**

Add interface assertions and a one-fetch refresh fake in `internal/provider/provider_test.go` and
`internal/app/provider_refresh_test.go`:

```go
type readerOnlySource struct{}

func (readerOnlySource) Reader(
    context.Context,
    bool,
) (provider.Reader, provider.SessionFingerprint, error) {
    return snapshotReader{}, "reader-generation", nil
}

func (readerOnlySource) Changed(provider.SessionFingerprint) (bool, error) {
    return false, nil
}

var _ provider.ReaderSource = readerOnlySource{}
```

The refresh test records one `FetchSnapshot` call and asserts that the returned
`SnapshotResult.Identity` is compared with the SQLite binding before fold.

- [ ] **Step 2: Run the tests and observe the interface failure**

Run:

```bash
go test ./internal/provider ./internal/app -run 'Test.*(ReaderSource|SnapshotIdentity|MoneyMismatch)' -count=1
```

Expected: compilation fails because `ReaderSource`, `SnapshotResult`, and
`CodeMoneyMismatch` do not exist.

- [ ] **Step 3: Replace the combined source and two-step reader contracts**

Implement the exact provider surface in `internal/provider/provider.go`:

```go
type SnapshotResult struct {
    Identity ProfileIdentity
    Snapshot domain.ImportSnapshot
}

type Reader interface {
    FetchSnapshot(context.Context, ProgressFunc) (SnapshotResult, error)
}

type SourceState interface {
    Changed(SessionFingerprint) (bool, error)
}

type ReaderSource interface {
    SourceState
    Reader(context.Context, bool) (Reader, SessionFingerprint, error)
}

type WriterSource interface {
    SourceState
    Writer(context.Context, bool) (Writer, SessionFingerprint, error)
}
```

Delete the combined `Source` interface. Change `ProviderRuntime` to contain:

```go
ReadSource  provider.ReaderSource
WriteSource provider.WriterSource
```

Refresh uses only `ReadSource`. Write preparation and execution require `WriteSource`; a missing
writer returns the existing write-unsupported result without a type assertion or nil method.

Change Monarch's `FetchSnapshot` implementation to run its existing identity probe and snapshot
fetch internally, then return both in `SnapshotResult`. Do not remove or weaken the live
subscription identity check.

- [ ] **Step 4: Add and classify `provider_money_mismatch`**

Add:

```go
const CodeMoneyMismatch ErrorCode = "provider_money_mismatch"
```

Its fixed detail is `the remote currency or scale does not match this profile`. Map it through
`internal/app/errors.go` and `internal/api/errors.go`. Add it to the exhaustive provider-code table
as manual-action-required and never-auto-retry.

- [ ] **Step 5: Update all factories and fakes without compatibility aliases**

Replace every `provider.Source` parameter with the narrow interface actually consumed. Monarch
factories pass the same concrete source as both `ReadSource` and `WriteSource`. Read-only test
providers implement only `ReaderSource`. Do not retain `type Source = ...` or an optional writer
adapter.

- [ ] **Step 6: Run focused and broad provider tests**

Run:

```bash
go test ./internal/provider/... ./internal/app/... ./internal/onboarding/... ./internal/api/... ./cmd/moneyflow/... -count=1
go test -race ./internal/provider/... ./internal/app/... -count=1
```

Expected: all packages pass; Monarch refresh still makes its identity probe; read-only source tests
compile without a writer.

- [ ] **Step 7: Commit the capability boundary**

```bash
git add internal/provider internal/app internal/api internal/onboarding cmd/moneyflow internal/tools/webtestserver
git commit -m "refactor: split provider read and write sources"
```

---

## Task 2: Extract the Shared Sealed Vault and Add the YNAB Vault

**Files:**

- Create: `internal/credentialvault/vault.go`
- Create: `internal/credentialvault/vault_test.go`
- Modify: `internal/provider/monarch/credentials.go`
- Modify: `internal/provider/monarch/credentials_test.go`
- Create: `internal/provider/ynab/vault.go`
- Create: `internal/provider/ynab/vault_test.go`
- Modify: `internal/provider/architecture_test.go`

**Interfaces:**

- Produces: `credentialvault.New(path, aad, options)` and authenticated `Seal`/`Open` operations.
- Produces: `ynab.CredentialVault`, `ynab.StoredCredentials`, and a provider-specific content fingerprint.
- Preserves: the exact Monarch version-1 envelope and payload bytes accepted before this task.

- [ ] **Step 1: Add shared-envelope and Monarch compatibility tests**

In `internal/credentialvault/vault_test.go`, cover fixed random bytes, wrong password, tamper,
truncation, trailing JSON, size bound, and atomic replacement. In Monarch tests, keep a committed
version-1 vault fixture and assert that the refactored loader returns the same credentials.

Use a provider-independent test payload:

```go
plaintext := []byte(`{"version":1,"value":"synthetic-secret"}`)
vault, err := credentialvault.New(path, []byte("moneyflow-test-v1"), options)
require.NoError(t, err)
require.NoError(t, vault.Seal(plaintext, []byte("account-password")))
decoded, err := vault.Open([]byte("account-password"))
require.NoError(t, err)
assert.Equal(t, plaintext, decoded)
```

- [ ] **Step 2: Run the tests and observe the missing package**

```bash
go test ./internal/credentialvault ./internal/provider/monarch -count=1
```

Expected: failure because `internal/credentialvault` does not exist.

- [ ] **Step 3: Move only envelope mechanics into `internal/credentialvault`**

Define:

```go
type Options struct {
    Random io.Reader
    Time uint32
    MemoryKiB uint32
    Parallelism uint8
    MaxBytes int64
}

type Vault struct {
    path string
    aad []byte
    options Options
}

func New(path string, aad []byte, options Options) (*Vault, error)
func (vault *Vault) Seal(plaintext, password []byte) error
func (vault *Vault) Open(password []byte) ([]byte, error)
func (vault *Vault) Exists() (bool, error)
type Fingerprint string

func (vault *Vault) Fingerprint() (Fingerprint, error)
func (vault *Vault) Delete() error
```

Use the existing envelope field names, Argon2id parameters, AES-256-GCM, hardened home helpers,
and `ErrUnlock`. Return independent byte slices and clear derived keys and plaintext buffers.
Provider packages continue to own payload JSON, AAD, path, validation, and error wording. The
shared package does not import `internal/provider`; each provider source converts the shared
fingerprint to `provider.SessionFingerprint` at its boundary.

- [ ] **Step 4: Refactor Monarch onto the shared envelope**

Keep Monarch's AAD exactly `moneyflow-monarch-credentials-v1` and its payload schema unchanged.
Remove duplicated cryptographic code. Update onboarding to recognize the shared unlock sentinel.
Do not add a second decoder, legacy branch, or migration.

- [ ] **Step 5: Add the YNAB payload and vault**

Implement the YNAB payload:

```go
type StoredCredentials struct {
    AccessToken string
    PlanID string
    Currency domain.Currency
    Scale uint8
}
```

The on-disk payload contains `version`, `access_token`, `plan_id`, `currency`, and `scale` under
AAD `moneyflow-ynab-credentials-v1`. Before selection, `PlanID`, `Currency`, and `Scale` may be
empty only in process memory; `Save` requires all fields. The file path is
`providers/ynab/credentials.enc`, maximum 16 KiB, with no plaintext session file.

- [ ] **Step 6: Add filesystem and separation tests**

Test two profiles with different tokens and plans, symlink refusal, owner-only permissions, atomic
replacement, deletion, and fingerprint change. Extend `architecture_test.go` so the shared vault
imports no provider implementation and the YNAB package imports no store/API/renderers.

- [ ] **Step 7: Run cross-platform-focused vault gates**

```bash
go test ./internal/credentialvault ./internal/provider/monarch ./internal/provider/ynab -count=1
go test -race ./internal/credentialvault ./internal/provider/monarch ./internal/provider/ynab -count=1
GOOS=windows GOARCH=amd64 go test ./internal/credentialvault ./internal/provider/ynab -run '^$'
```

Expected: all tests pass and the Windows compile-only command succeeds without CGO.

- [ ] **Step 8: Commit the vault boundary**

```bash
git add internal/credentialvault internal/provider/monarch internal/provider/ynab internal/provider/architecture_test.go
git commit -m "refactor: share encrypted provider vault mechanics"
```

---

## Task 3: Implement the Direct YNAB Reader and Exact Normalizer

**Files:**

- Create: `internal/provider/ynab/client.go`
- Create: `internal/provider/ynab/client_test.go`
- Create: `internal/provider/ynab/wire.go`
- Create: `internal/provider/ynab/money.go`
- Create: `internal/provider/ynab/money_test.go`
- Create: `internal/provider/ynab/normalize.go`
- Create: `internal/provider/ynab/normalize_test.go`
- Create: `internal/provider/ynab/source.go`
- Create: `internal/provider/ynab/source_test.go`
- Create: `internal/provider/ynab/testdata/full-plan.json`
- Modify: `internal/domain/provider.go`
- Modify: `internal/domain/provider_test.go`

**Interfaces:**

- Produces: `ynab.Client.ListPlans`, `ynab.Client.FetchPlan`, and a `provider.ReaderSource`.
- Produces: `domain.ImportTransactionSplit` and `ImportSnapshot.Splits`.
- Consumes: encrypted YNAB credentials from Task 2 and provider read contracts from Task 1.

- [ ] **Step 1: Add milliunit table tests before implementation**

Use a table covering scales 0 through 9, signs, divisibility, and overflow:

```go
tests := []struct {
    name string
    milliunits int64
    scale uint8
    want int64
    wantErr bool
}{
    {name: "scale two", milliunits: -12340, scale: 2, want: -1234},
    {name: "nondivisible", milliunits: 1, scale: 2, wantErr: true},
    {name: "scale three", milliunits: 1234, scale: 3, want: 1234},
    {name: "scale four", milliunits: 1234, scale: 4, want: 12340},
    {name: "overflow", milliunits: math.MaxInt64, scale: 4, wantErr: true},
}
```

Assert that errors map to `provider_data_invalid` with reason
`provider.DataInvalidTransactionAmount` and never include the amount.

- [ ] **Step 2: Add HTTP contract tests with `httptest.Server`**

Cover bearer headers, `/v1/plans`, escaped `/v1/plans/{id}`, five-minute deadline propagation,
512 MiB configurable body cap, content type, one JSON value, unknown fields, 401/403, 404, 429,
5xx, cancellation, and response-body redaction.

Assert that a bound refresh invokes only `GET /plans/{id}`. List plans is onboarding-only.

- [ ] **Step 3: Add synthetic normalization tests**

The fixture contains synthetic accounts, same-label payees, category groups, categories, one
uncategorized row, one transfer, one tracking-account row, cleared/uncleared/reconciled rows, a
split parent, split lines, closed accounts, and scheduled rows. Tests assert:

```go
assert.True(t, transactionByID(snapshot, "txn-uncleared").Pending)
assert.True(t, transactionByID(snapshot, "txn-transfer").Hidden)
assert.True(t, transactionByID(snapshot, "txn-tracking").Hidden)
assert.Equal(t, domain.SplitCategoryID,
    transactionByID(snapshot, "txn-split").SystemCategoryID)
assert.Len(t, snapshot.Splits, 2)
```

Add negative cases for missing arrays, unresolved references, unknown cleared status, duplicate IDs,
deleted rows/splits, invalid date, rune-overlong memo, split-sum mismatch, and money overflow.

- [ ] **Step 4: Run focused tests and observe missing symbols**

```bash
go test ./internal/provider/ynab ./internal/domain -run 'Test(Milliunits|Client|Normalize|ImportSnapshotSplits)' -count=1
```

Expected: compilation fails because the client, converter, and split types do not exist.

- [ ] **Step 5: Implement bounded wire decoding and exact conversion**

Use `http.NewRequestWithContext`, `url.PathEscape`, `io.LimitReader`, and `json.Decoder`. Do not
use `DisallowUnknownFields`; validate every consumed field after decoding. Define only the response
fields used by the spec.

Implement:

```go
func milliunitsToMoney(
    value int64,
    currency domain.Currency,
    scale uint8,
) (domain.Money, error)
```

Use checked integer division/multiplication and the shared exact formatter.

- [ ] **Step 6: Extend the domain snapshot with provider-neutral split details**

Add:

```go
type ImportTransactionSplit struct {
    ExternalID string
    ParentTransactionExternalID string
    Position int
    SourceAmount int64
    SourceScale uint8
    Amount Money
    Memo string
    PayeeExternalID string
    PayeeLabel string
    CategoryExternalID string
    CategoryLabel string
    TransferAccountExternalID string
    TransferTransactionExternalID string
}
```

Add `Splits []ImportTransactionSplit` to `ImportSnapshot`, clone it defensively, and validate stable
sorting, unique IDs, parent existence, exact money, and nonnegative positions. Existing providers
return an empty slice and keep their behavior.

Add this provider-neutral protected-category selector to `ImportTransaction`:

```go
SystemCategoryID EntityID
```

It is mutually exclusive with `CategoryExternalID`. Empty preserves the current rule: an empty
external category resolves to protected Uncategorized. The only nonempty value accepted in this
slice is `SplitCategoryID`; it resolves directly to that protected local sentinel and never enters
external-identity mapping or provider-label allocation. Add validation and planner tests for the
mutual-exclusion rule, the allowed sentinel, and unchanged Monarch behavior.

- [ ] **Step 7: Implement normalization and source lifecycle**

Normalize IDs and labels with the fixed bounds. Use the installed system Uncategorized category
for nil category, set `SystemCategoryID` to `domain.SplitCategoryID` for split parents, and use one
reserved external Unknown Payee merchant. Sort each parent's splits by external ID bytewise before
assigning positions.

`Source.Reader` returns a reader over the decrypted token and retained bound plan. `Changed` checks
the vault fingerprint. `FetchSnapshot` performs one full-plan request and returns:

```go
provider.SnapshotResult{
    Identity: provider.ProfileIdentity{Kind: "ynab", RemoteID: plan.ID},
    Snapshot: normalized,
}
```

- [ ] **Step 8: Run adapter, domain, fuzz, and race tests**

```bash
go test ./internal/provider/ynab ./internal/domain -count=1
go test -race ./internal/provider/ynab ./internal/domain -count=1
go test ./internal/provider/ynab -run '^$' -fuzz FuzzDecodePlan -fuzztime=10s
```

Expected: all fixed tests pass; fuzzing completes without panic or unbounded allocation.

- [ ] **Step 9: Commit the read adapter**

```bash
git add internal/provider/ynab internal/domain
git commit -m "feat: add exact YNAB full-plan reader"
```

---

## Task 4: Install Schema Version 11 and Fold Split Details Atomically

**Files:**

- Modify: `internal/domain/entities.go`
- Modify: `internal/domain/entities_test.go`
- Modify: `internal/domain/profile.go`
- Modify: `internal/domain/profile_test.go`
- Modify: `internal/store/store.go`
- Modify: `internal/store/sqlite/schema/profile.sql`
- Modify: `internal/store/sqlite/initialize.go`
- Modify: `internal/store/sqlite/schema_test.go`
- Modify: `internal/store/sqlite/load.go`
- Modify: `internal/store/sqlite/load_test.go`
- Modify: `internal/store/sqlite/provider_refresh.go`
- Modify: `internal/store/sqlite/provider_refresh_test.go`
- Modify: `internal/store/sqlite/provider_refresh_failure_test.go`
- Modify: `internal/store/sqlite/provider_refresh_property_test.go`
- Modify: `internal/store/sqlite/provider_refresh_benchmark_test.go`
- Modify: `internal/app/provider_refresh.go`
- Modify: `internal/app/provider_refresh_test.go`
- Modify: `internal/app/provider_integration_test.go`

**Interfaces:**

- Produces: protected `domain.SplitCategoryID`, store `YNABTransactionSplit`, and split-aware refresh plans.
- Changes: `store.RefreshInputs` and `RefreshPlan` carry existing/planned YNAB split rows.
- Changes: no-op refresh updates operational success state without semantic revision/generation churn.

- [ ] **Step 1: Write schema and protected-sentinel tests first**

Assert a newly opened profile contains:

```go
domain.Category{
    ID: domain.SplitCategoryID,
    GroupID: domain.UncategorizedGroupID,
    Label: "Split",
    CollisionKey: "split",
    Protected: true,
}
```

Assert `CurrentSchemaVersion == 11`, `schema_metadata == 11`, the split table is STRICT, money
columns are INTEGER, foreign keys exist, and a version-10 database is refused.

- [ ] **Step 2: Add atomic split-fold and no-op tests**

Create a YNAB candidate with two split rows, fold it, reopen, and compare every field. Then refresh
with an identical candidate and assert:

```go
assert.Equal(t, first.Revision, second.Revision)
assert.Equal(t, first.Generation, second.Generation)
assert.Equal(t, beforeCommitted, afterCommitted)
assert.Equal(t, beforeSplits, afterSplits)
assert.True(t, afterStatus.LastSuccess.After(beforeStatus.LastSuccess))
```

Fault inject before split replacement, after split replacement, and before commit; every failure
must leave committed rows, split rows, journal, binding, revision, and generation unchanged.

- [ ] **Step 3: Run focused tests and observe schema/version failures**

```bash
go test ./internal/domain ./internal/store/sqlite ./internal/app -run 'Test.*(SplitCategory|YNABSplit|RefreshNoOp|SchemaVersion)' -count=1
```

Expected: failures because the sentinel, table, store values, and no-op result do not exist.

- [ ] **Step 4: Install the exact schema in one change**

Add `SplitCategoryID = "category_system_split"` and immutable label/collision constants. Require
both system categories during committed-profile validation. Insert Split beside Uncategorized in
`profile.sql`.

Update both the application identity planner and the store's reference materializer to resolve
`ImportTransaction.SystemCategoryID` directly after validating it as a protected sentinel. They
must not manufacture an external-identity row for Split. The reference and optimized paths must
agree on this resolution.

Add `ynab_transaction_splits` exactly as approved, with parent cascade, unique external ID,
integer source/minor amounts, and no deleted column. Increment both schema metadata and
`CurrentSchemaVersion` to 11. Do not add a migration.

- [ ] **Step 5: Add store types and closed planner inputs**

Define:

```go
type YNABTransactionSplit struct {
    ParentTransactionID domain.EntityID
    Position int
    ExternalID string
    AmountMilliunits int64
    AmountMinor int64
    Memo string
    PayeeExternalID string
    PayeeLabel string
    CategoryExternalID string
    CategoryLabel string
    TransferAccountExternalID string
    TransferTransactionExternalID string
}
```

Add `YNABSplits` to `RefreshInputs` and `RefreshPlan`. Load current rows inside the authoritative
transaction. The app planner resolves each split parent through the candidate transaction's stable
local ID and returns a complete sorted replacement.

- [ ] **Step 6: Validate and replace split rows atomically**

Validation requires YNAB binding kind, complete candidate materialization, unique external IDs,
contiguous positions per parent, exact currency conversion, and a live parent transaction. Replace
rows only after the pure plan validates and before revision/generation update.

Do not expose SQL rows to the planner and do not permit the planner to query the store.

- [ ] **Step 7: Implement semantic no-op detection**

Add `SemanticChange bool` to `RefreshPlan` and `RefreshCommit`. It is true when committed rows,
journal/cursor, known drills, allocations, lineage, binding, or YNAB split rows change. On false,
update last-attempt/last-success and release the lease without incrementing profile revision or
refresh generation. Validate that the planner's flag equals an authoritative deep comparison.

- [ ] **Step 8: Run store, property, race, and benchmark gates**

```bash
go test ./internal/domain ./internal/store/sqlite ./internal/app -count=1
go test -race ./internal/store/sqlite ./internal/app -count=1
go test ./internal/store/sqlite -run TestProviderRefreshReferenceMatchesOptimized -count=20
go test ./internal/store/sqlite -run '^$' -bench 'BenchmarkProviderRefresh100K' -benchtime=1x
```

Expected: all correctness/race gates pass and the 100,000-row fold remains below the 4-second CI
ceiling recorded by the benchmark assertion.

- [ ] **Step 9: Commit schema 11 and every dependent query together**

```bash
git add internal/domain internal/store internal/app
git commit -m "feat: retain YNAB split details in schema 11"
```

---

## Task 5: Generalize Onboarding and Add CLI YNAB Connect

**Files:**

- Modify: `internal/onboarding/types.go`
- Modify: `internal/onboarding/attempts.go`
- Modify: `internal/onboarding/runtime.go`
- Modify: `internal/onboarding/flow.go`
- Modify: `internal/onboarding/authenticate.go`
- Modify: `internal/onboarding/credentials.go`
- Modify: `internal/onboarding/import.go`
- Create: `internal/onboarding/driver.go`
- Create: `internal/onboarding/monarch_flow.go`
- Create: `internal/onboarding/ynab_flow.go`
- Modify: `internal/onboarding/flow_test.go`
- Modify: `internal/onboarding/credentials_test.go`
- Modify: `internal/onboarding/privacy_test.go`
- Create: `internal/onboarding/ynab_flow_test.go`
- Modify: `cmd/moneyflow/provider.go`
- Modify: `cmd/moneyflow/provider_test.go`
- Modify: `cmd/moneyflow/onboarding_presenter.go`
- Modify: `cmd/moneyflow/onboarding_presenter_test.go`
- Modify: `cmd/moneyflow/root_test.go`

**Interfaces:**

- Produces: onboarding protocol version 2, attempt-scoped remote choices, and YNAB credentials.
- Produces: `moneyflow provider connect ynab` and `disconnect ynab`.
- Preserves: every existing Monarch state transition and CLI behavior.

- [ ] **Step 1: Add protocol-v2 and privacy tests**

Add tests for zero, one, and multiple plans; opaque choice IDs; cross-attempt choice rejection;
SQLite-authoritative binding; vault mismatch; first-import retry; and cancel boundaries.

Assert snapshots use:

```go
RemoteProfileChoice{
    ChoiceID: "choice_opaque",
    DisplayName: "Example Budget",
    LastModified: "2026-08-01T00:00:00Z",
}
```

and contain no remote plan ID or token. Privacy tests serialize every state/failure and scan it for
the synthetic token, plan ID, and account password.

- [ ] **Step 2: Add CLI behavior tests**

Test command help, missing/ambiguous profile resolution, masked token prompt, numbered plan
selection, derived currency confirmation, imported count, unlock-only retry, disconnect
idempotence, and absence of `--currency`, `--scale`, `--budget`, or token flags.

- [ ] **Step 3: Run tests and observe protocol/command failures**

```bash
go test ./internal/onboarding ./cmd/moneyflow -run 'Test.*(YNAB|ProtocolVersion|RemoteProfile)' -count=1
```

Expected: failures because protocol v2, the YNAB flow, and Cobra commands do not exist.

- [ ] **Step 4: Introduce a small provider-flow interface**

Keep attempt ownership in the coordinator and define the internal flow seam:

```go
type providerFlow interface {
    Start(context.Context, progressSink) (flowResult, error)
    Submit(context.Context, providerSubmission, progressSink) (flowResult, error)
    Cancel() error
    Close() error
}

type providerFlowFactory func(
    context.Context,
    OpenedProfile,
    *home.Lock,
    StartRequest,
) (providerFlow, error)
```

`progressSink` accepts only the existing counts-only progress value. `providerSubmission` is the
validated provider-specific subset of the public submit union. `flowResult` carries only stable
state, optional settings, bounded remote choices, fixed failure, imported count, and an optional
configured `app.ProviderRuntime`. The coordinator continues to own attempt IDs, state versions,
jobs, status activity, cancellation, expiry, and secret clearing.

Move existing Monarch logic behind `monarch_flow.go` without changing its public behavior.

- [ ] **Step 5: Add protocol-v2 wire types**

Set `ProtocolVersion = 2`. Add `StateRemoteProfileRequired`,
`ActionSelectRemoteProfile`, `RemoteProfileChoice`, and:

```go
type YNABCredentialInput struct {
    AccessToken []byte
    AccountPassword []byte
    Confirmation []byte
}

type ProviderSubmission struct {
    Action ActionType
    Settings *SettingsInput
    Unlock *UnlockInput
    MonarchCredentials *CredentialInput
    YNABCredentials *YNABCredentialInput
    RemoteProfileChoiceID string
}
```

Reject multiple provider secret envelopes and clear every secret on all return paths.

- [ ] **Step 6: Implement the YNAB flow**

The flow performs: inspect pristine/binding, load/unlock or request token, list plans for unbound
profiles, select one/auto-select one, fetch the complete selected plan, show derived settings,
save the vault after confirmation, and call ordinary provider refresh with an initial binding.

For a bound profile, use SQLite plan/currency/scale as authority and call the exact plan directly.
A mismatched vault returns identity or money mismatch and never rewrites SQLite.

- [ ] **Step 7: Wire Cobra through the shared coordinator**

Add `connect ynab` and `disconnect ynab` subcommands with `--profile` only. Extend the CLI presenter
to render remote choices and YNAB secret prompts. Keep progress on stderr and the final imported
count on stdout. Print both TUI and web next-step commands after success.

- [ ] **Step 8: Run onboarding, CLI, privacy, and race tests**

```bash
go test ./internal/onboarding ./cmd/moneyflow -count=1
go test -race ./internal/onboarding ./cmd/moneyflow -count=1
```

Expected: all flows pass, existing Monarch cases remain unchanged, and no serialized status
contains a test secret or remote plan ID.

- [ ] **Step 9: Commit onboarding and CLI support**

```bash
git add internal/onboarding cmd/moneyflow
git commit -m "feat: connect YNAB profiles through shared onboarding"
```

---

## Task 6: Configure YNAB Runtime, Catalog Status, and Refresh Scheduling

**Files:**

- Modify: `internal/profilecatalog/catalog.go`
- Modify: `internal/profilecatalog/discovery.go`
- Modify: `internal/profilecatalog/catalog_test.go`
- Modify: `internal/app/capabilities.go`
- Modify: `internal/app/capabilities_test.go`
- Modify: `internal/app/provider_refresh.go`
- Modify: `internal/app/provider_refresh_test.go`
- Modify: `internal/app/provider_scheduler.go`
- Modify: `internal/app/provider_scheduler_test.go`
- Modify: `cmd/moneyflow/profile.go`
- Modify: `cmd/moneyflow/tui_shell.go`
- Modify: `cmd/moneyflow/web_dependencies.go`
- Modify: `cmd/moneyflow/web_test.go`
- Modify: `cmd/moneyflow/mcp_dependencies.go`
- Modify: `cmd/moneyflow/mcp_test.go`

**Interfaces:**

- Consumes: YNAB source and vault from Tasks 2-3 and coordinator completion runtime from Task 5.
- Produces: local Ready/Reconnect/Setup-incomplete states and six-hour unlocked refresh cadence.
- Preserves: offline application and MCP reads without credential prompts.

- [ ] **Step 1: Add catalog and capability tests**

Test bound-plus-vault as Ready, bound-without-vault as Reconnect, unbound-plus-vault as Setup
incomplete, and unbound non-pristine as Local only. Assert catalog listing performs no network I/O.

Test YNAB capabilities before and after runtime unlock:

```go
assert.False(t, locked.Available(app.ActionRefreshProvider))
assert.False(t, locked.Available(app.ActionCommit))
assert.True(t, unlocked.Available(app.ActionRefreshProvider))
assert.False(t, unlocked.Available(app.ActionCommit))
```

- [ ] **Step 2: Add scheduler and runtime replacement tests**

Use an injected clock to assert full refresh at six hours only while unlocked. Replace/delete the
vault file and assert the next status tick clears the reader runtime and stops network scheduling.
MCP against the same profile must continue reading committed data and return unlock guidance from
`refresh_data`.

- [ ] **Step 3: Run focused tests and observe unavailable YNAB runtime**

```bash
go test ./internal/profilecatalog ./internal/app ./cmd/moneyflow -run 'Test.*YNAB' -count=1
```

Expected: failures because discovery and process factories still know only Monarch/Amazon/local.

- [ ] **Step 4: Add local vault presence and runtime factories**

Add `ynab.CredentialFilePresent(profileRoot)` using hardened non-following inspection. Extend
catalog provider-kind validation and Huma enums to `ynab` without probing the network.

TUI/web factories create a YNAB flow and, after successful unlock, install:

```go
app.ProviderRuntime{
    ReadSource: source,
    Provider: "ynab",
    Currency: binding.Currency,
    Scale: binding.Scale,
    Renderer: renderer,
}
```

Leave `WriteSource` nil. Close runtime/token memory when the profile service closes or is evicted.

- [ ] **Step 5: Make refresh and scheduling kind-neutral**

Use provider kind only for user-facing wording and normalized namespace. The existing lease,
generation CAS, deletion confirmation, rebase, retry classification, and six-hour scheduler run
unchanged for YNAB. A YNAB full response does not invoke Monarch pagination checks.

- [ ] **Step 6: Run app, command, MCP, race, and 100k gates**

```bash
go test ./internal/profilecatalog ./internal/app ./cmd/moneyflow ./internal/mcp -count=1
go test -race ./internal/app ./cmd/moneyflow -count=1
go test ./internal/app -run '^$' -bench 'Benchmark.*Provider.*100K' -benchtime=1x
```

Expected: offline/MCP reads pass without unlock, unlocked refresh passes, commit stays unavailable,
and the benchmark remains within the spec ceiling.

- [ ] **Step 7: Commit runtime and scheduling support**

```bash
git add internal/profilecatalog internal/app cmd/moneyflow internal/mcp
git commit -m "feat: run YNAB refresh from unlocked profiles"
```

---

## Task 7: Add the YNAB TUI Wizard and Offline Recovery

**Files:**

- Modify: `internal/tui/provider_selector.go`
- Modify: `internal/tui/provider_selector_test.go`
- Modify: `internal/tui/onboarding_form.go`
- Modify: `internal/tui/onboarding_form_test.go`
- Modify: `internal/tui/onboarding_progress.go`
- Modify: `internal/tui/onboarding_progress_test.go`
- Modify: `internal/tui/shell.go`
- Modify: `internal/tui/shell_view.go`
- Modify: `internal/tui/shell_test.go`
- Modify: `internal/tui/onboarding_parity_test.go`
- Modify: `internal/tui/onboarding_preview_test.go`
- Modify: `internal/tui/semantic_parity_test.go`
- Modify: `internal/tui/visual_golden_test.go`
- Modify: `cmd/moneyflow/tui_shell.go`

**Interfaces:**

- Consumes: onboarding protocol v2 and runtime installation from Tasks 5-6.
- Produces: keyboard-complete YNAB connect, unlock, budget-selection, settings-confirmation,
  import, reconnect, and offline flows.

- [ ] **Step 1: Add TUI state and input tests first**

Test `y` selection, secret entry indicators, Tab/Shift+Tab, Enter submission, Esc cancellation,
numbered plan choice, derived read-only currency/scale, progress, offline open, and successful
transition to finance without restart.

The YNAB secret form submits:

```go
onboarding.YNABCredentialInput{
    AccessToken: token,
    AccountPassword: accountPassword,
    Confirmation: confirmation,
}
```

Assert semantic frames contain field IDs and masked indicators but not secret values.

- [ ] **Step 2: Run focused tests and observe YNAB unavailable**

```bash
go test ./internal/tui ./cmd/moneyflow -run 'Test.*(YNAB|ProviderSelector|Onboarding)' -count=1
```

Expected: existing selector reports `YNAB is not available in Go yet` and tests fail.

- [ ] **Step 3: Add provider-specific forms without duplicating shell lifecycle**

Replace the fixed Monarch credential form selection with provider-kind dispatch. Add
`ynabCredentialForm` and `remoteProfileForm`; reuse `secretInput`, attempt polling, cancellation,
progress, and completion messages. Currency and scale are display-only for YNAB.

Change help/status wording by provider kind. Do not add another Bubble Tea program or direct YNAB
adapter call.

- [ ] **Step 4: Add locked/reconnect finance actions**

Opening a locally Ready YNAB profile offers Unlock or Open Offline. A refresh that reports
reconnect-required exposes Reconnect in the finance view. Successful onboarding replaces the
process runtime and returns to the preserved finance state.

- [ ] **Step 5: Update reviewed semantic artifacts deliberately**

Add synthetic YNAB scenarios to the semantic frame generator. Run:

```bash
make parity-update-go
git diff -- internal/parity internal/tui
```

Review the complete artifact diff. It must contain only synthetic labels and no screenshot files.
Do not update Python frames unless a real Python selector scenario is missing and the artifact diff
is reviewed separately.

- [ ] **Step 6: Run TUI, parity, and race gates**

```bash
go test ./internal/tui ./cmd/moneyflow -count=1
go test -race ./internal/tui ./cmd/moneyflow -count=1
make parity
```

Expected: TUI tests and parity pass, YNAB is selectable, and secrets are absent from frames.

- [ ] **Step 7: Commit the TUI experience**

```bash
git add internal/tui internal/parity cmd/moneyflow
git commit -m "feat: add YNAB onboarding to the TUI"
```

---

## Task 8: Add Huma Protocol v2 and the Web YNAB Wizard

**Files:**

- Modify: `internal/api/onboarding.go`
- Modify: `internal/api/onboarding_test.go`
- Modify: `internal/api/profilecatalog.go`
- Modify: `internal/api/profilecatalog_test.go`
- Modify: `internal/api/openapi_test.go`
- Modify: `internal/api/errors.go`
- Modify: `internal/api/errors_test.go`
- Modify: `internal/api/server_test.go`
- Modify: `web/src/lib/api/schema.d.ts` through `make web-generate`
- Modify: `web/src/lib/controller/onboarding.svelte.ts`
- Modify: `web/src/lib/controller/onboarding.test.ts`
- Modify: `web/src/components/profiles/ProviderSelector.svelte`
- Modify: `web/src/components/profiles/ProviderSelector.test.ts`
- Modify: `web/src/components/profiles/OnboardingWizard.svelte`
- Modify: `web/src/components/profiles/OnboardingWizard.test.ts`
- Modify: `web/src/components/profiles/ProfileSelector.svelte`
- Modify: `web/src/components/profiles/ProfileSelector.test.ts`
- Modify: `web/tests/onboarding.spec.ts`
- Modify: `cmd/moneyflow/web_dependencies.go`
- Modify: `cmd/moneyflow/web_test.go`

**Interfaces:**

- Consumes: onboarding protocol v2 snapshots/actions from Task 5.
- Produces: Huma schemas and one kit-ui wizard for Monarch and YNAB.
- Preserves: existing mutation-token, exact-origin, Fetch-Metadata, no-store, and base-path behavior.

- [ ] **Step 1: Add Huma request/response and secret-blindness tests**

Test remote choices, selection action, YNAB credentials, cross-provider-field rejection, version-1
rejection, mutation security, and credential-free problem responses. The response shape includes:

```json
{
  "protocol_version": 2,
  "state": "remote_profile_required",
  "remote_profiles": [
    {
      "choice_id": "choice_opaque",
      "display_name": "Example Budget",
      "last_modified": "2026-08-01T00:00:00Z"
    }
  ]
}
```

Assert the remote plan ID and test token are absent from response bodies and Huma problems.

- [ ] **Step 2: Add frontend controller/component tests**

Test YNAB provider availability, masked token entry, plan keyboard selection, read-only settings,
reload during import, offline open, reconnect, focus restoration, completion redirect, and secret
clearing after submit.

- [ ] **Step 3: Run focused API/frontend tests and observe missing fields**

```bash
go test ./internal/api ./cmd/moneyflow -run 'Test.*(Onboarding|YNAB)' -count=1
bun test --cwd web src/lib/controller/onboarding.test.ts src/components/profiles/OnboardingWizard.test.ts src/components/profiles/ProviderSelector.test.ts
```

Expected: failures because protocol-v2 fields and YNAB UI states are absent.

- [ ] **Step 4: Extend Huma's provider-neutral onboarding schemas**

Add `remote_profiles`, `remote_profile_choice_id`, and `ynab_credentials` to the existing Huma
operations. Map them to the coordinator's union and reject fields not valid for `provider_kind`.
Add `ynab` to profile-catalog enums. Keep the same URLs and security middleware.

- [ ] **Step 5: Regenerate and review API types**

```bash
make web-generate
git diff -- web/src/lib/api/schema.d.ts internal/api
```

Review that only protocol-v2 and `ynab` enum changes appear. Do not hand-edit generated schema
types.

- [ ] **Step 6: Make the existing wizard provider-aware**

Use one `OnboardingWizard.svelte`. Render Monarch email/password/TOTP only for Monarch and YNAB
token/password-confirmation only for YNAB. Add a keyboard-selectable budget list and display-only
currency/scale confirmation. Reuse kit-ui `TextInput`, `Button`, `Card`, `Spinner`, and `StatusBar`.

Use blob-free ordinary JSON onboarding requests; no secret enters URL/history/storage. Keep assets
at the base path and redirect completion to `/p/<profile-id>/`.

- [ ] **Step 7: Run web unit, API, build, and browser smoke gates**

```bash
go test ./internal/api ./cmd/moneyflow -count=1
bun test --cwd web
make web-check
make web-build
make web-e2e
```

Expected: all API/frontend tests pass; Chromium runs the full onboarding journey; Firefox/WebKit
smoke covers masked input, plan selection, and completion routing.

- [ ] **Step 8: Confirm generated assets remain untracked**

```bash
git status --short --ignored web/dist internal/web/dist web/tests/screenshots
git status --short
```

Expected: generated distributions and screenshots are ignored; only source, tests, and generated
API type source intended by `web-generate` are staged.

- [ ] **Step 9: Commit the web experience**

```bash
git add internal/api cmd/moneyflow web/src web/tests
git commit -m "feat: add YNAB onboarding to the web app"
```

---

## Task 9: Complete Integration, Performance, Documentation, and Live Characterization

**Files:**

- Create: `internal/provider/ynab/live_test.go`
- Create: `internal/provider/ynab/performance_test.go`
- Modify: `internal/provider/architecture_test.go`
- Modify: `internal/app/provider_integration_test.go`
- Modify: `internal/app/provider_rebase_property_test.go`
- Modify: `internal/store/sqlite/provider_refresh_concurrency_test.go`
- Modify: `internal/store/sqlite/provider_refresh_property_test.go`
- Modify: `internal/store/sqlite/provider_refresh_benchmark_test.go`
- Modify: `README.md`
- Modify: `AGENTS.md` only if stable YNAB developer commands are added
- Modify: `Makefile` only if a named YNAB live target is added

**Interfaces:**

- Verifies: the complete approved spec across process boundaries.
- Produces: operator documentation and an explicitly enabled read-only live test.
- Does not produce: provider mutation code, live fixtures, screenshots, generated SDKs, or migrations.

- [ ] **Step 1: Add end-to-end synthetic restart and concurrency tests**

Drive connect → choose budget → confirm settings → import → stage edits → refresh/rebase → restart →
offline browse. Add two-process generation races, deletion confirmation, selection clearing,
journal-ceiling refresh, split replacement, vault replacement, and no-op revision tests.

Use fake YNAB HTTP and explicit temporary profile paths. Assert exactly one concurrent fold changes
semantic state.

- [ ] **Step 2: Add normalization and fold performance gates**

Generate 100,000 synthetic transactions with representative split density. Assert decode/normalize,
planner, fold, and cold-reopen ceilings from the spec. Keep network time outside measurements.

Run:

```bash
go test ./internal/provider/ynab ./internal/app ./internal/store/sqlite -run '^$' -bench 'Benchmark.*YNAB.*100K' -benchtime=1x
```

- [ ] **Step 3: Add the opt-in read-only live test**

Gate the test on `MONEYFLOW_YNAB_LIVE=1` and `MONEYFLOW_YNAB_TOKEN`. Require a temporary home path
created by the test. List budgets, select the explicit test budget through a separate environment
selector when more than one exists, and fetch two complete snapshots.

The test logs only counts and checks plan-ID stability, currency/scale, closed/tracking account
coverage, transaction-ID stability, uncleared rows when present, split links/sums, and complete
references. It never calls a mutation endpoint or writes a fixture.

- [ ] **Step 4: Update user documentation**

Document `provider connect ynab`, profile-per-budget behavior, account-password unlock, offline
open, six-hour full refresh, manual `r`, staged-only edits until write-back, and the absence of token
flags. State that Huma serves Moneyflow's inbound API while the YNAB adapter uses direct REST.

Use only synthetic names and commands. Do not include a real token, plan ID, username, home path,
or financial value.

- [ ] **Step 5: Run the complete automated verification**

```bash
make verify-go
make verify-web
uv run pytest -v
uv run pyright moneyflow/
uv run ruff format --check moneyflow/ tests/
uv run ruff check moneyflow/ tests/
npx --yes markdownlint-cli@0.47.0 --config .markdownlint.json README.md 'docs/**/*.md'
.github/scripts/check-arrow-lists.sh
```

Expected: every command passes. If the repository's documented load-sensitive performance gate is
noisy, run its supported non-performance verification path and report the exact skipped gate; do
not leave verified changes uncommitted.

- [ ] **Step 6: Run privacy and negative-scope checks**

```bash
git diff --check
git status --short
rg -n 'UpdateTransaction|DeleteTransaction|server_knowledge.*query|float32|float64|REAL' internal/provider/ynab internal/store/sqlite
```

Review each hit. Expected production results: no YNAB mutation call, no delta query, no floating
money, no REAL money column, no committed live fixture, and no tracked generated distribution or
screenshot.

- [ ] **Step 7: Commit automated completion before live use**

```bash
git add README.md AGENTS.md Makefile internal web cmd
git commit -m "feat: complete YNAB read and refresh support"
```

Stage only paths actually changed. Omit `AGENTS.md` or `Makefile` when no stable command was added.

- [ ] **Step 8: Request the user's YNAB token for live characterization**

Ask only after Step 7 is committed. Instruct the user to provide it through the environment, not
chat or a command-line flag. Run the opt-in test with the disposable home and report counts only.

Any issue found by live characterization is fixed test-first, verified, and committed as a new
commit. Do not amend Step 7.

- [ ] **Step 9: Run the requested final review workflow when authorized**

If the current user instruction explicitly invokes `$roborev-fix`, run that skill now, address only
verified findings, rerun affected/full gates, and commit the fixes. Otherwise do not create a
RoboRev review manually; local automatic review remains separate.

- [ ] **Step 10: Verify the final repository state**

```bash
git status --short --branch
git log -10 --oneline
```

Expected: the working tree is clean, every implementation checkpoint is committed, and the branch
has not been pushed unless the user separately authorized a push.
