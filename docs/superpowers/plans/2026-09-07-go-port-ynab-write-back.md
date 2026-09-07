# YNAB Write-Back Implementation Plan

> **For agentic workers:** Use superpowers:executing-plans for the requested inline execution.
> Work directly on `go-port`; do not create branches or worktrees.

**Goal:** Commit supported YNAB edits through the existing durable provider-write workflow.

**Architecture:** Extend neutral requests and durable items, then implement a single-attempt YNAB
writer behind the existing worker. Shared policy validates staging/review/preparation; SQLite
persists exact intent, transfer restrictions, and response facts. No second worker or commit path.

**Tech Stack:** Existing Go toolchain, net/http, Testify, modernc SQLite, Cobra, Huma, and MCP SDK.
No new dependencies or schema migrations.

**Spec:** [Approved YNAB write-back design](../specs/2026-09-07-go-port-ynab-write-back-design.md).

## Global constraints and review resolutions

- Schema 12 installs with the first shape change; schema 11 is refused, never migrated/reset.
- Money stays integer minor units locally and integer milliunits on the YNAB wire.
- Four concurrent items, five attempts, 24-hour maximum Retry-After; YNAB fallback is one hour.
- The adapter supplies the one-hour fallback through `provider.NewErrorWithRetry`; the worker
  remains provider-neutral. Parse seconds and HTTP dates using an injected clock, without overflow.
- Restriction orphan checks use SQLite insert/update triggers for the polymorphic target and
  delete/retirement cleanup triggers. Snapshot replacement restores retained restrictions within
  the same transaction; refresh replaces them from the candidate. Test actual SQL rejection and
  application reopen, not schema text.
- `Payee.transfer_account_id` must traverse wire, normalization, domain import, and store contracts.
- Recheck [canonical YNAB API documentation](https://api.ynab.com/v1) and the installed SDK request
  model before transport implementation. Live omission behavior remains unverified without an
  explicitly authorized disposable plan/transaction; no live writes in ordinary checks.
- Use `MONEYFLOW_YNAB_LIVE=0` for ordinary tests. Existing harnesses supply temporary profiles and
  synthetic transports. Do not source credentials or open the default financial profile.
- Preserve Monarch behavior, process-local worker reservation, leases, and transactional guards.
- Update living architecture as behavior lands. The completed frame-pruning work is not repeated.
- Commit independently verified checkpoints. Do not push, amend, or start manual RoboRev reviews
  without a current request authorizing them.

## Checkpoint 1: Durable request semantics and install-only schema

**Files:** `internal/provider/provider.go`, `internal/store/provider_write.go`,
`internal/store/store.go`, `internal/domain/provider.go`,
`internal/store/sqlite/{initialize.go,provider_write.go,provider_state.go,provider_refresh.go}`,
`internal/store/sqlite/schema/profile.sql`; their existing contract/schema/write tests.

**Interfaces:** Add `TransactionUpdate.MerchantExternalID Optional[string]`, `ClearCategory bool`,
`TransactionUpdateResult.CategoryCleared bool`, `WriteItem.ClearCategory bool`, and
`WriteResult.CategoryCleared bool`. Existing expected merchant IDs remain the durable ID source.
Define `domain.ImportWriteRestriction{Kind, ExternalID, Reason}` and
`store.ProviderWriteRestriction{Kind, EntityID, Reason}`; reason is the closed value `transfer`.
Carry slices through import clone/validation, provider state, and refresh inputs/plans.

- [x] Add failing union tests: clear-only update accepted; clear plus category ID and any delete
  clear/result-clear rejected. Add SQLite rejection/persistence tests and schema-11 refusal.

  ```go
  item := store.WriteItem{Kind: store.WriteItemUpdate, ClearCategory: true}
  require.NoError(t, item.Validate())
  item.RequestedCategoryExternalID = new("category-a")
  require.Error(t, item.Validate())
  ```

- [x] Run focused store/domain tests and observe failure before changing contracts.
- [x] Add fields, clone/validation, SQL columns and CHECKs, restriction table/triggers, and schema
  bump together. Include insert/scan/result persistence so existing writer behavior stays green.
- [x] Persist restrictions through store state; preserve them during committed-row replacement,
  filtering deleted/retired owners. Validate pure callback output before writing it.
- [x] Run focused store/schema tests, full Go checks, and commit the independently green boundary.

## Checkpoint 2: Transfer facts and shared writable-target policy

**Files:** `internal/provider/ynab/{wire.go,normalize.go,normalize_test.go}`,
`internal/app/{provider_refresh.go,provider_write_identity.go,provider_write_plan.go,actions.go}`,
`internal/store/sqlite/{provider_refresh.go,provider_write_reconcile.go}`; refresh/policy tests.

**Interfaces:** Normalization emits `ImportSnapshot.WriteRestrictions`; refresh maps external IDs
to `ProviderState.WriteRestrictions`. Extend existing write identity indexing with provider kind.
One application policy serves staging and final-prefix validation; no renderer duplicates it.

- [ ] Add normalization/refresh reopen tests for transfer parents, transfer-bearing children,
  transfer payees, and writable off-budget nontransfers.
- [ ] Add failing staging/planning tables covering hide/taxonomy refusal, mixed selection rejection,
  Split assignment refusal, split-parent payee/delete acceptance, and system Uncategorized clear.

  ```go
  code, ok := provider.CodeOf(err)
  require.True(t, ok)
  assert.Equal(t, provider.CodeWriteUnsupported, code)
  ```

- [ ] Implement kind-selected supported operations and exact-ID indexing. Preserve Monarch's
  label-collision checks; YNAB active mapped destinations are addressable despite equal labels.
- [ ] Cover effective membership sweeps, pending-created destinations, vacuous operations after
  deletion, and existing retired/alias label rules with literal expected items.
- [ ] Run normalize, refresh, app planning/property tests; commit.

## Checkpoint 3: Single-attempt YNAB HTTP writer

**Files:** create `internal/provider/ynab/write.go`, `write_test.go`, and focused wire helpers if
needed; modify `client.go`, `source.go`, and their tests. Reuse existing vault and HTTP options.

**Interfaces:** `Source.Writer(context.Context, bool) (provider.Writer,
provider.SessionFingerprint, error)` returns a writer with `ProbeIdentity`, `UpdateTransaction`,
and `DeleteTransaction`. Writer reads current transaction before each mutation; no adapter retry.

- [ ] Test real HTTP request bodies with synthetic endpoints. Assert ID vs name, null vs omitted
  category, current approval true/false, and omission of amount/date/account/memo/cleared/flags/splits.

  ```go
  assert.Equal(t, "payee-a", patch["payee_id"])
  assert.NotContains(t, patch, "payee_name")
  assert.Contains(t, patch, "category_id")
  assert.Nil(t, patch["category_id"])
  assert.Equal(t, true, patch["approved"])
  assert.NotContains(t, patch, "amount")
  ```

- [ ] Observe failing tests, then implement bounded GET/preflight plus minimal PUT, identity/money
  probe, exact protected-field comparison, split comparison by external ID, and explicit nullable
  response state. A split parent's null category must not become Uncategorized.
- [ ] Test GET failures as non-dispatched, PUT transport/5xx/malformed-success as unknown outcome,
  deterministic rejection, 429 on both phases, revoked credentials, and wrong plan/transaction.
- [ ] Implement delete preflight; transaction not-found requires accessible-plan verification.
  Test not-found at GET and DELETE, valid deletion, inaccessible plan, and transfer refusal.
- [ ] Add seconds/date/invalid/overflow Retry-After tests with injected clock and one-hour fallback.
- [ ] Run adapter/source/vault tests and Monarch regression suite; commit.

## Checkpoint 4: Provider-neutral planning, worker, and finalization

**Files:** `internal/app/{provider_write_plan.go,provider_write.go}`,
`internal/store/provider_write_finalization.go`, `internal/store/sqlite/provider_write.go`;
existing writer/planner/finalization integration tests plus YNAB cases.

**Interfaces:** Existing `BuildProviderWritePlan`, `RunProviderWrite`, and
`BuildProviderWriteFinalization` remain entry points. Worker builds YNAB ID requests from persisted
expectations/leader results and forwards explicit category clearing/result state.

- [ ] Add failing plan tests for exact mapped ID, equal labels, clear-only update, net no-op,
  delete superseding update, and chained new merchants with one bytewise-first leader.
- [ ] Add result-fold tests for mapped/unmapped overrides, nonsplit clear vs split null,
  lineage rotation, preserved split details, and deleted-parent restriction cleanup.
- [ ] Implement kind-selected request/result policy without changing Monarch wire requests.
- [ ] Exercise real service/store worker with fake HTTP writer through prepare, result persistence,
  reopen, and finalization. Assert response-adjusted committed truth against hand-built expectations.
- [ ] Test rate-limit resume versus true crash-uncertain update, delete resend, recorded-success
  no-resend, quota wait surviving restart, and stale batch-version rejection.
- [ ] Run shared writer/property/cross-process regression suites and commit.

## Checkpoint 5: Runtime activation and supported UI/MCP actions

**Files:** `cmd/moneyflow` provider runtime factories; `internal/app/actions.go`,
`internal/tui` review/provider tests, `internal/api` editing tests, `internal/mcp` write tests,
and `web/tests` existing provider/editing journeys. Edit production presenters only where needed.

**Interfaces:** Existing `ConfigureProvider` receives the now writer-capable YNAB source. Existing
commit/review, pause/resume/reconcile APIs and action IDs remain unchanged.

- [ ] Add failing service/action tests for available supported commits and truthful disabled
  hide/taxonomy/split/transfer behavior. Locked/offline runtimes must not dispatch.
- [ ] Configure the shared writer after unlock; preserve vault fingerprint/wrong-plan checks.
- [ ] Drive TUI `w` then Enter; exercise web review/commit and MCP write-enabled commit/status.
  Keep read-only MCP tools and web mutation protections unchanged.
- [ ] Test long quota waits, pause/resume, reconnect/unlock, and stop/reconcile using existing
  status fields. Provider copy must say YNAB rather than Monarch.
- [ ] Run focused renderer/API/MCP/browser checks; commit.

## Checkpoint 6: Recovery, portability, and performance proof

**Files:** shared provider write concurrency/failure/performance suites; YNAB integration tests;
Makefile only if a necessary owned gate is missing.

- [ ] Test refresh vs preparation guards, lease takeover, single-worker reservation, and heartbeat.
- [ ] Test failed finalization then reopen without resend, stop/reconcile failure preserving the
  frozen prefix, successful reconcile removing all frozen intent, and deletion confirmation.
- [ ] Cover original external-ID restoration, new-ID allocation, stale credentials, and privacy
  through real synthetic transport/service/store paths.
- [ ] Add YNAB cases to the existing 100k planning/finalization gates; run timing separately from
  race/load-sensitive validation, retaining exact-money and state assertions.
- [ ] Run Go verification/race, Python checks, web verification, Linux/macOS/Windows no-CGO builds,
  Markdown/site build, and public-diff privacy checks. Record exact skipped timing/live gates.
- [ ] Update living architecture, mark this plan complete, commit. Live-write characterization
  remains a separately authorized follow-up and must not be described as already proven.
