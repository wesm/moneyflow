# Go Port YNAB Write-Back Design

**Date:** 2026-09-07

**Status:** Approved; implementation in progress

**Branch:** `go-port`

## Purpose

Enable ordinary YNAB edits through Moneyflow's existing staging, review, and durable commit
workflow. The interaction remains `w`, then `Enter`. This slice closes a functional gap in the
Python replacement; it does not redesign the TUI or web application.

The implementation extends the shared provider-write machinery already used by Monarch. It does
not add an independent YNAB worker, direct renderer-to-provider writes, or a second commit path.
YNAB owns its HTTP encoding and response translation. The application owns resolved targets,
revision checks, durable items, leases, scheduling, and finalization.

The source baseline for this document is `3f43672`. At that revision, YNAB read/import/refresh is
implemented and the installed schema version is 11. Its ordinary edits can be staged, but no
YNAB writer is installed and provider commit remains unavailable.

## Existing Contracts

This design extends these documents:

- `2026-08-14-go-port-sqlite-editing-design.md`
- `2026-08-15-go-port-monarch-read-refresh-design.md`
- `2026-08-17-go-port-profile-catalog-onboarding-design.md`
- `2026-08-18-go-port-monarch-write-back-design.md`
- `2026-08-18-go-port-transaction-deletion-duplicates-design.md`
- `2026-08-21-go-port-mcp-design.md`
- `2026-08-30-go-port-ynab-read-refresh-design.md`

Later deletion and crash-recovery rules take precedence over the original Monarch document's
broader resend wording: an attempted update with no durable result is not blindly resent after
a crash. Stop and reconcile removes the entire frozen prefix; it does not rebase and preserve
failed or unsent edits from that prefix.

Other standing contracts remain unchanged: exact integer money, stable local IDs, full replay,
all-or-nothing selection revalidation, install-only SQLite schemas, provider/store isolation,
no network work inside SQLite transactions, and profile-scoped HTTP security.

## Goals and Scope

Support these ordinary journal operations on a YNAB profile:

- `merchant.label`, expressed as transaction-scoped payee changes.
- `merchant.reassign` to an existing payee or a newly named merchant.
- `merchant.merge`, expressed as reassignment of the affected transactions.
- `category.assign` to an existing mapped category or system Uncategorized.
- `transaction.delete`, including deletion from duplicate review.

Provide the same durable progress, pause, resume, attention, and reconciliation controls through
TUI, web, and the existing write-enabled MCP surface. The read-only MCP policy is unchanged.

Non-goals:

- Global payee rename, category/group management, or budget administration through YNAB.
- Creating categories on the fly or editing targets, scheduled transactions, or rules.
- Editing amounts, dates, memos, approval, clearing status, or flags.
- Creating transfers, changing existing transfers, or editing split lines.
- Replacing a split with an ordinary transaction or changing a split parent's category.
- Local-only visibility overlays for YNAB.
- Bulk HTTP mutations, delta refresh, OAuth, or a new Go SDK dependency.
- UX redesign, parity-fixture pruning, or historical-document consolidation.
- Schema migrations, removing Python, pushing branches, or live mutation tests without separate
  explicit authorization.

Parity-fixture pruning and historical-document consolidation are follow-up work after this spec,
not additions to this slice.

## API Grounding

The current YNAB API offers transaction GET, PUT, and DELETE under
`/plans/{plan_id}/transactions/{transaction_id}`. DELETE documents a successful response containing
the transaction and a transaction-not-found response. This slice uses those individual-item
endpoints, not the bulk PATCH endpoint. See the official
[transaction API reference](https://api.ynab.com/v1#/Transactions).

YNAB accepts either an existing `payee_id` or name-based resolution through `payee_name`. Existing
split parents cannot be recategorized through `category_id`; credit-card-payment category
assignments may be ignored. The update model also documents that omitting `approved` defaults to
unapproved. These rules require explicit request-preservation and response tests. See the official
[existing-transaction model](https://github.com/ynab/ynab-sdk-python/blob/main/docs/ExistingTransaction.md).

The Python oracle is `moneyflow/backends/ynab_client.py`: `update_transaction` reads the current
transaction, resolves a payee by name, and submits an update. It deliberately ignores
`hide_from_reports`, despite the older docstring describing a deletion mapping. It also offers a
global payee rename optimization. These are source observations, not assumptions that every
Python write preserves unrelated remote values.

Implementation must verify the relevant request schema against the current official API again.
Synthetic HTTP contract tests are mandatory; disposable live characterization supplements them
only with explicit user permission. A documentation ambiguity never justifies experimenting on
ordinary financial transactions.

## Capability Rules

### Staging, review, and preparation

The application validates provider capabilities while staging, at review, and inside authoritative
batch preparation. All three use the same provider-kind policy. Renderers display the result;
they do not reproduce the policy.

Selection wins when present; otherwise the focused target applies. A mixed bulk selection with
an unsupported target rejects the entire operation. It never silently stages only the supported
subset. Review and commit use the existing expected-revision contract.

YNAB restrictions are explicit:

- `h` is unavailable with the reason that YNAB report visibility is derived from account/transfer
  state and Moneyflow does not write a hide flag for this provider.
- `C`, `G`, and create-on-the-fly in `c` explain that taxonomy is managed in YNAB in this slice.
- A category assignment requires an active mapped category, except that protected system
  Uncategorized has a dedicated clear-category representation. System Split is never a target.
- Split parents permit payee changes that preserve their children, and whole-parent deletion.
  Parent recategorization and individual split-line mutations are unavailable.
- Transfer transactions, parents containing transfer splits, and transfer-payee destinations
  are unavailable for mutation. This avoids changing another account's transaction outside the
  reviewed target set. Off-budget status alone is not a transfer and does not forbid payee edits
  or deletion.
- Missing or retired targets reject staging. Targets that disappear remotely after staging
  follow the attention/reconciliation rules below.

Transfer classification uses provider IDs and structural metadata, never localized labels or
merchant-name heuristics. The import path retains the minimum transfer markers needed for offline
capability checks. Live preflight repeats the checks before sending an item.

The category selector must not guess that a category is unwritable from its name or group name.
Known structural restrictions are gated; a provider-declined assignment that returns a valid
transaction is reported as an override rather than falsely reported as applied.

### Empty merchants and net no-ops

Keep the existing staging-time refusal for a merchant label/merge with no affected transactions.
At preparation, a later staged deletion may have emptied its source. That operation is vacuous,
not a batch-blocking error: it produces no request and folds with the reviewed prefix.

Review annotates it as affecting zero transactions. Completion counts count requests/results,
not vacuous operations. A label on an emptied merchant has no remote effect. As in the deletion
slice, the next successful full refresh restores the provider label for that provider-owned
merchant; a local-only label override does not survive that refresh.

An unchanged transaction produces no item. Deletion supersedes an update to the same transaction.
An entirely net-no-op prefix uses the existing no-item commit path, without inventing an empty
durable batch or making provider calls.

## Architecture and Changes to Shared Boundaries

The existing `provider.ReaderSource` / `provider.WriterSource` split stays in place. YNAB's
concrete source implements both and shares its existing encrypted-vault unlock and fingerprint
handling. No plaintext token enters the profile database or the batch.

The current `provider.TransactionUpdate` carries a merchant name but no requested merchant ID.
Add an optional merchant external ID to that neutral request. At the transport boundary a merchant
change has exactly one addressing mode: ID or name. Monarch keeps its current name-addressed
behavior. YNAB uses IDs whenever the durable plan already knows the destination.

The existing item expectation stores the expected external ID for mapped destinations. Use that
durable value for YNAB's request, rather than looking up a new destination by display name at
send time. For new-name followers, use the ID persisted by the group's leader.

The current optional category field cannot encode omitted versus explicit clearing through its
SQL constraints. Add a distinct clear-category flag to the neutral request and durable item.
Assignment and clearing are mutually exclusive. Never use a fake provider ID for Uncategorized.
The neutral response and durable result also gain an explicit category-cleared flag, distinct
from an unreported category. A result with a mapped category ID cannot also be category-cleared.

Current write identity indexing and supported-operation checks contain Monarch-specific behavior.
Make those selected by the bound provider, with only the two real provider policies. Do not turn
this into a configurable plugin framework. Keep common planning, item attribution, replay,
worker ownership, and failure handling shared.

Dependency directions remain:

- `provider/ynab` imports neutral provider/domain contracts and its existing vault support, not
  store, app, TUI, web, API, or MCP.
- `app` combines provider capabilities with domain and store snapshots.
- `store` and `store/sqlite` never import a provider package.
- Renderers and MCP invoke the application service, not YNAB authentication or HTTP methods.
- Analytics remains an ordinary consumer of domain slices, with no writer or SQLite awareness.

Extend existing architecture tests to enforce these boundaries and preserve the process-local
single-worker reservation during any shared-worker refactor.

## Transaction-Scoped Payee Semantics

Whole-merchant rename is intentionally not a YNAB global payee rename. The reviewed prefix
determines which imported transactions receive the new payee. Rows outside that snapshot, split
children, and future transactions are not implicitly renamed.

For a destination with a mapped external ID, send that exact ID. A collision-suffixed local display
label never appears as a new remote payee name. Two equally named but distinct YNAB payees remain
separately addressable; Monarch's name-ambiguity refusal does not apply to ID-addressed YNAB writes.

For an unmapped destination or fresh label, send the user's canonical label. Reuse the existing
new-name leader groups, keyed by the intended local merchant identity. The leader is the item with
the lowest transaction external ID under bytewise string ordering. Persist its validated resulting
payee ID before releasing followers, and address followers by that ID. The adapter does not
create payees through a separate entity-create request.

Persist and validate `existing`, `merge_destination`, and `new` expectations as with Monarch:

- Existing/merge expectations returning another active mapped payee are provider overrides for
  those transactions, not requests to rotate the original merchant's mapping.
- For unmapped, alias, or retired returned IDs in those non-strict cases, fold the requested
  merchant provisionally, count the override, and let the immediately due full refresh install
  the observed identity. Do not silently seize another local entity's mapping.
- A strict new group must agree on one resulting identity before finalization. Contradictory or
  unusable identity responses require reconciliation.
- Stable-ID rotation, same-entity alias rotation, historical alias promotion, and retirement of
  merged sources use the existing lineage rules, selected by the YNAB namespace.
- Empty historical provider identities do not create phantom merchants; their return with live
  transactions follows the existing promotion-to-fresh-local-identity rule.

Fresh-label staging retains the existing retired-label/lineage checks. ID-addressed assignment
to an active destination is not rejected merely because another provider payee shares its label.
Provider labels remain distinct from local display allocations.

## Request Fidelity

Each update attempt performs a bounded GET of the exact transaction before its PUT. This is a
single adapter attempt, with no hidden adapter retries. It checks target identity, required
fields, nondeleted state, split/transfer restrictions, and the fields whose preservation matters.

Build the PUT from the smallest explicit patch: requested payee addressing, requested category
or explicit `category_id: null`, plus the freshly read approval value to preserve YNAB's documented
default-sensitive field. Do not copy the whole response object into the request. In particular,
do not submit stale local amount, date, account, memo, cleared status, flags, import identity, or
subtransactions. The initial implementation must prove the omission behavior in contract tests
and the authorized live characterization before claiming preservation verified against YNAB.

New-name addressing explicitly clears `payee_id` while supplying `payee_name`. ID addressing
supplies `payee_id` without `payee_name`. When no category change is requested, omit the category
key entirely; uncategorization supplies JSON null. No request supplies `deleted` or a substitute
hide flag.

The GET and PUT are not a remote compare-and-swap transaction. Do not claim they prevent a user
editing YNAB concurrently. Re-read on a new permitted attempt, minimize overwritten fields, and
validate the returned facts. No application change to amount/date/account/split structure is
authorized by a payee/category edit.

For preservation checks, compare split children by stable external ID and facts, not response
array order. An unexpected change to protected fields, missing required result fields, or an
unusable transaction identity parks the batch for inspection/reconciliation; it is not a normal
payee/category override. This detects disagreement but cannot undo a remote mutation already sent.

All amounts remain integer milliunits on the YNAB wire and exact minor units in Moneyflow.
Preservation checks reject missing/null amounts, accept explicit zero, check currency/scale,
and use no floating-point arithmetic. The writer never changes an amount.

## Result Validation and Finalization

A successful HTTP status is insufficient by itself. A successful update must return the requested
transaction ID, a valid payee result for a payee change, an explicitly represented category state,
and the required transaction/split facts for preservation checks. JSON null category is distinct
from an omitted category field.

Accepted payee/category overrides are counted and displayed. Mapped returned categories become
the committed assignment. A returned null category on a nonsplit parent becomes system
Uncategorized, recorded with the explicit category-cleared result flag. On a split parent, null
is the expected provider encoding of the split: retain system Split and the unchanged children,
do not emit category-cleared, and do not count it as an override. An unknown
category keeps the requested assignment provisionally until refresh resolves it; the override
still counts. Unrequested echoed mapped payee/category changes follow the same authoritative-base
rule, without adding user journal operations.

Preserve the shared commit oracle:

```text
committed state after successful finalization
    = effective state immediately before preparation
      adjusted by accepted provider responses and identity reconciliation
```

This includes deleted rows, taxonomy retirement, mapping lineage, and accepted overrides. The
transaction-coupled YNAB split records must still reference existing parents after deletion, and
unchanged split detail must survive a payee-only update.

Each success is persisted before another process can rely on it. Finalization is atomic; a local
failure leaves the batch and successful item facts available for recovery and does not resend
already-recorded successes. Successful finalization makes refresh due immediately. That refresh
uses the established coherent full-read and rebase path, not a second YNAB reconciliation engine.

## Deletion

Deletion removes the entire selected parent transaction, including its retained split details.
The existing delete confirmation explains that scope before staging. Transfer-related parents
remain outside this slice's writable set.

Use DELETE for the explicit transaction ID. A valid successful deletion response satisfies the
item. Transaction-specific not-found satisfies an already-absent delete only after verifying that
the bound plan remains accessible to the token. A missing/inaccessible plan is not evidence that
every transaction was deleted.

Deletion also preflights the exact parent with GET so a transaction that became a transfer after
staging is refused before DELETE. If that GET finds the transaction already absent, verify plan
access and complete it as already absent without sending DELETE. A transfer change after the GET
cannot be prevented atomically; the same remote-concurrency limitation as updates applies.

Other deterministic rejections enter reconcile-only attention. Never parse raw provider message
text to manufacture success. An ambiguous delete response can be resolved by repeating the
absolute deletion within the existing five-attempt budget; ambiguous updates cannot.

Keep external transaction identity mappings after local deletion. If bank synchronization later
returns the same external ID, refresh restores the same local ID. A new external ID allocates a
new local ID. Moneyflow does not promise that a bank-synced deletion will remain absent forever.

## Durable Lifecycle and Concurrency

The authoritative preparation transaction checks the reviewed revision, freezes the active
prefix, discards the redo tail under the existing commit contract, records absolute items, and
requires an owned write-kind lease. A live refresh owner causes preparation to refuse with
`provider_refresh_in_progress`. A pre-check outside the transaction is only a courtesy.

Ordinary refresh fold must authoritatively require no batch, regardless of lease expiry. Stop and
reconcile uses its separate batch/version/generation-checked fold. The lease provides liveness,
not correctness. No SQLite transaction spans provider network work or renderer input.

Reuse all existing durable phases: `writing`, `reconciling`, `paused`, `reconnect_required`,
`rate_limited`, `attention_required`, and `reconcile_confirmation_required`. Preparation either
commits a writing batch or rolls back; it creates no durable prepared phase.

Writing and active reconciliation own and renew the operation lease. Parked phases release it.
Pause is durable. Resume requires the existing expected batch version and acquires the lease.
The process-local single-worker guard remains necessary in addition to the cross-process lease.

Long-lived TUI/web processes can resume orphaned writing/reconciling batches only after a writer
is unlocked and configured. A restarted process does not silently decrypt the YNAB vault. Paused
and attention-required batches never resume merely because a process opened the profile.
MCP keeps its explicit bounded supervisor contract and adds no background freshness scheduler.

The existing provider source fingerprint rules apply to both reads and writes: revalidate the
bound plan and currency/scale before any batch writes; observe credential replacement; never
use a replacement vault for another plan. A changed locked vault requires unlock, not repeated
blind authentication attempts. SQLite binding remains authoritative over the vault copy.

An attempted update without a persisted result enters unknown-outcome, reconcile-only attention
before another network mutation. An attempted delete may be resent within its attempt budget.
Graceful shutdown stops new work and permits only bounded in-flight cleanup; crash handling never
depends on receiving a final response from the exiting renderer.

### Stop and reconcile

Stop dispatching, wait for owned in-flight work to settle, and retain the durable batch while
fetching a coherent full snapshot. The fold removes the entire frozen prefix atomically with
installation of remote truth. Failed and unsent intent is abandoned; it is not retained by
ordinary rebase. Successful remote deletions remain absent because they are absent in the snapshot.

Recorded successful item facts remain for status/recovery until that fold. They do not contribute
partial identity rotations before the snapshot is installed. Snapshot identity mapping alone
determines the reconciliation result.

Inherit identity probing, money checks, snapshot-instability retries, deletion plausibility,
confirmation expiry, process-bound tokens, and generation checks. Auth expiry parks on reconnect;
invalid reads never clear the batch or prefix. A suspicious deletion candidate still requires the
existing explicit confirmation. Integrity failure is never overridable.

## Error and Rate-Limit Policy

The YNAB adapter translates errors at its boundary and never returns raw bodies or request values
to logs, status, or public problem responses. It performs one attempt; the shared worker owns all
retry and park decisions.

Use the existing stable classes:

- Invalid/revoked credentials: `provider_reconnect_required`, parked until explicit recovery.
- Inaccessible/wrong plan: `provider_identity_mismatch`, no further writes.
- Deterministically rejected mutation: `provider_write_rejected`, reconcile-only attention.
- Update target not found: `provider_write_target_not_found`, reconcile-only attention.
- Unknown update outcome, including send-before-result persistence on restart:
  `provider_write_outcome_unknown`, reconcile-only attention.
- Contradictory strict new-payee identity: `provider_write_identity_conflict`, reconcile-only.
- Exhausted known non-applied unavailability: existing retryable attention after at most five
  attempts. Do not classify every HTTP 5xx or transport error as proof of non-application.
- Storage failure: existing store error, no automatic network replay of recorded successes.

Malformed successful update responses do not authorize a resend merely because their JSON could
not be decoded. Treat the mutation outcome as unknown. A preflight GET failure, by contrast, has
not sent the mutation. Tests must distinguish those phases.

YNAB documents 200 requests per hour per token in a rolling window. That allowance includes reads
and activity from other profiles/processes using the token. This slice does not claim a local
per-profile counter can reserve the global allowance. See
[YNAB rate limits](https://api.ynab.com/#rate-limiting).

Keep the shared concurrency ceiling of four and the one-leader rule. On 429, stop dispatching,
settle already in-flight work, release the lease, and persist `rate_limited` plus `NextEligible`.
Use a valid positive Retry-After bounded by the existing 24-hour maximum. If absent, invalid, or
zero, use a one-hour YNAB fallback instead of the worker's generic one-minute fallback. The YNAB
adapter attaches this delay through the existing bounded provider error; the worker remains
provider-neutral. Parse
delta-seconds and HTTP dates against the injected clock without duration overflow. Persist
the same wait across restart and refuse early Resume; neither repeated Enter nor status polling
is permission to hammer the provider. A later 429 can extend the wait.

Progress shows completed/remaining counts, overrides, and the next retry time. Long waits are
reported as waiting on YNAB, not as stalled progress or an invented time-to-completion estimate.
Rate-limit responses before application must not strand an update as crash-uncertain on normal
eligible resumption. Adapter-attempt and mutation-dispatch bookkeeping must preserve that
distinction without weakening the true crash-uncertain rule.

## Installed Schema and Persistence

The slice installs schema version 12, bumping `CurrentSchemaVersion` in the same commit as the
first schema-shape change. Version 11 profiles are refused with existing recovery guidance.
No migration, dual reader, payload conversion, or automatic profile reset is introduced.

The installed shape must include:

- An explicit clear-category flag on write items; it is false for delete items, mutually
  exclusive with requested category ID, and counts as a field on update items.
- An explicit category-cleared flag on results; it is false for delete results and mutually
  exclusive with a returned mapped category ID. SQL null by itself still means unreported.
- The existing mapped-payee expectation and new-group durable result needed to reproduce exact
  ID-addressed requests after restart. Do not persist a second copy of the same destination ID
  solely for the new request field.
- Minimal persisted transfer restrictions for transaction and merchant entities, including
  transfer-bearing split parents. These are provider facts for capability validation, not label
  heuristics or new accounting operations. Store them in a dedicated
  `provider_write_restrictions` table keyed by entity kind/local ID, with the allowlisted reason
  `transfer`; refresh replaces this projection atomically with the base.
- Constraint-checked preservation of the update/delete union and required expectation fields.

The restriction table has `entity_type` (`transaction` or `merchant`), `entity_id`, and `reason`
(`transfer`), with a composite primary key over entity type and ID. YNAB parent transfer references,
transfer-bearing split children, and payee `transfer_account_id` supply these facts. Missing
nullable transfer references mean no transfer; a hidden/off-budget row alone supplies no marker.

Transfer markers travel through typed domain import/store contracts; raw YNAB DTOs never enter
store. Restriction rows do not orphan when transactions are deleted or merchants retire. Their
installation, refresh replacement, and deletion happen in the same semantic transaction as the
corresponding profile change.

SQLite triggers reject insert/update restrictions without an active owner and remove them on
owner deletion or merchant retirement. The store validates the complete refresh restriction
projection before replacing it inside the same transaction.

Credentials remain solely in the existing encrypted profile vault. Raw HTTP responses, tokens,
account passwords, and retry diagnostics containing financial values are not durable batch data.
All money-bearing columns remain INTEGER plus explicit currency/scale, never REAL.

Operational lease renewals and status polls do not bump profile revision. Preparation, recorded
semantic batch transitions, finalization, and reconciliation follow the existing revision/version
accounting. Refresh generation remains a separate successful-fold counter.

## Renderer and MCP Experience

`w` opens the existing review; `Enter` authorizes one revision-checked commit and closes review
into progress. No additional confirmation is inserted. Review warns about discarded redo history,
shows unsupported counts/reasons, and does not enable commit for an unsupported prefix.

Editing, undo/redo, refresh, and another commit remain disabled while any batch exists. Browsing,
details, export, status, and batch recovery remain available under their existing contracts.
Selection clearing and analytical URL/cursor preservation follow the current application service.

Pause, Resume, Stop and reconcile, and reconciliation confirmation use existing action identities
and API routes. Capability text is provider-specific; it must not say Monarch on a YNAB profile.
TUI/web share service outcomes. No frontend code encodes YNAB payloads or interprets raw errors.

MCP `commit_changes` and batch controls use the same durable service when writes are enabled.
The existing bounded initial run and status/resume behavior remains; no tool blocks indefinitely
waiting for the token quota. Read-only servers do not gain edit or commit tools.

Provider disconnect is refused while a batch is unfinished. Connect/unlock can restore writer
availability, but the connect command does not independently dispatch a batch. Opening explicitly
offline performs no provider work and retains all durable recovery state.

## Named Parity Decisions

1. Python's YNAB hide request is ignored. Go refuses it explicitly instead of reporting a write
   that has no remote effect. Durable local-only hiding remains deferred.
2. Go uses staged deletion and explicit commit rather than Python's immediate remote deletion.
3. Global payee rename optimization is not ported. Transaction-scoped writes limit side effects
   to reviewed rows, at the cost of more requests and possible quota waits.
4. Exact mapped payee IDs avoid Python's name-resolution ambiguity for reassignment.
5. Split recategorization, transfer mutations, and taxonomy administration are explicitly gated
   rather than submitted as edits the slice cannot faithfully apply.
6. Approval and other unrelated fields have explicit preservation obligations; Python's request
   construction is not treated as sufficient evidence of preservation.
7. Partial success, durable parked states, crash-uncertain handling, and atomic finalization use
   the Go write-back contract rather than Python's all-success local-apply assumption.
8. Provider payee/category overrides are counted and reconciled rather than treated as if the
   requested values necessarily won.
9. The temporary editing freeze during an unfinished batch remains the named Go divergence.

Exact money, stable drill identities, system Uncategorized/Split, pending-state interpretation,
and retained deleted-identity mappings keep the preceding Go slice contracts.

## Verification Obligations

Use Testify and the existing Go test harness. Keep provider requests synthetic by default, with
temporary profiles and encrypted test vaults. Tests exercise repository behavior, not a second
mock implementation of its planner. Do not add personal account facts to fixtures.

### HTTP request and response contracts

- Mapped destination sends its exact ID even when labels collide or display suffixes exist.
- Fresh name sends the canonical user label; followers use the persisted leader ID.
- Ordinary category assignment, explicit null uncategorization, and omitted category are three
  distinct wire cases.
- Payee/category edits preserve approval true and false, all clearing states, memo including
  null/empty, flags, date, account, explicit zero/negative amount, and split details.
- A split parent's null category stays system Split across result persistence/finalization and
  reopen; a nonsplit null category becomes system Uncategorized, not an omitted response field.
- Split child response reordering is harmless; changed/missing split facts are not.
- Preflight blocks transfer source/destination and transfer-bearing split parents before PUT.
- Remote target disappears or becomes a split/transfer between staging and execution: no
  unsupported request is sent after preflight detects it.
- Valid payee/category overrides, null category, unknown returned category/payee, strict identity
  conflicts, malformed JSON, missing fields, and wrong transaction ID follow their named classes.
- Delete success, transaction-not-found with accessible plan, inaccessible plan, deterministic
  rejection, and unknown-outcome resend have separate cases.
- Every adapter attempt sends at most one mutation and does not retry through the read path.

### Planning, SQLite, and crash recovery

- Supported/unsupported operation tables cover staging, review, and preparation, including mixed
  selections, system categories, empty merchant operations, and deleted final targets.
- Same-batch new-merchant chains share a leader and survive crash after leader-result persistence.
- Retired/alias rotations and empty/live historical identity handling preserve stable IDs.
- A crashed update after dispatch parks before a second mutation; a crashed delete can resend.
- A received rate-limit/preflight rejection can resume without being mistaken for an unknown
  applied update. A crash before that rejection is recorded remains conservatively uncertain.
- Success-result persistence, finalization failure, disk-full, busy handling, process hand-off,
  and stale batch-version rejection preserve the shared atomicity contract.
- Refresh/preparation races prove both authoritative guards; expired leases cannot bypass them.
- Stop and reconcile removes all frozen intent only on a successful fold and inherits deletion
  confirmation, identity/money mismatch, and snapshot-integrity failures without data loss.
- Finalization matches the response-adjusted equivalence oracle after fresh SQLite reopen.
- Deleted parents remove split detail and restriction rows, retain external identity mappings,
  and restore original local IDs if the same provider transaction reappears.
- Schema 12 installs exactly; version 11 is refused before new-shape queries; invalid union,
  request/result category-clear combinations, and orphan restriction rows are rejected.

### Scheduling and surfaces

- Four-request concurrency ceiling, one leader per group, single worker per process, lease
  hand-off, heartbeat, and pause/resume are exercised for both providers.
- 429 with and without Retry-After records the correct wait; early Resume, process restart,
  same-token independent activity, and later rate limits cannot erase that wait.
- Vault locked/replaced/wrong-plan cases cannot dispatch unauthorized-plan writes; offline open
  performs no provider calls.
- TUI `w`, Enter and the web review flow enable supported YNAB commit without new ceremony.
- Disabled hide/taxonomy/split/transfer actions explain why; deletion from duplicates follows
  the same target/capability checks.
- Progress, long rate-limit waits, attention, reconnect/unlock, and Stop and reconcile have
  consistent outcomes in TUI, web, and write-enabled MCP.
- Existing web mutation-token/origin/profile checks and MCP read-only/bounded-run tests continue
  to pass. Logs/status/errors expose only the established allowlisted metadata, never payloads.
- Monarch writer requests, overrides, retry classification, lineage, and existing parity gates
  remain unchanged except deliberate provider-neutral plumbing.

### Performance and portability

Retain the existing 100,000-row planning/finalization gates and add YNAB cases to those gates.
Measure local CPU/storage work independently of API latency and quota waits. Do not claim a
network completion-time guarantee for large batches. Existing YNAB normalization, full refresh,
split cold-load, analytics, and ordinary race checks remain required.

Build without CGO for Linux, macOS, and Windows. Windows cleanup and advisory-lock behavior keep
their existing obligations. Browser coverage follows the repository pattern: full Chromium
workflow coverage, with shared smoke journeys on Firefox and WebKit. No generated screenshots
or embedded distributions are committed.

### Authorized live characterization

Commit automated-verified changes before live tests. Existing read-only token permission is not
permission to mutate a budget. Obtain explicit authorization naming disposable parent transactions
and their budget before testing writes; use no ordinary financial rows and no implicit fallback
to a default plan. Mutation tests are opt-in and never part of normal CI.

Verify minimal PUT omission/preservation, approval, payee ID/name behavior, category clearing,
ordinary overrides, nontransfer split-parent payee updates, and deletion/not-found response shape.
If a documented behavior does not hold, report the evidence and correct the adapter in a new
commit. Do not mark live behavior verified solely from synthetic results. Live quota exhaustion
and crash/failure scenarios remain synthetic; do not consume the user's quota to demonstrate them.

## Completion and Handoff

The slice is complete when supported YNAB edits stage, review, commit, survive interruption, and
reconcile through the shared service; unsupported actions explain themselves before dispatch;
the response-adjusted fold and request-preservation tests pass; and Monarch regression gates stay
green. Report live characterization separately, including anything not yet authorized or observed.

The implementation plan should order work as: shared request/schema capabilities; pure planning
and YNAB adapter contracts; durable worker/finalization integration; renderer/MCP activation; then
cross-process, failure, portability, and authorized live verification. Keep checkpoints separately
verified and committed, with no agent-authored changes left uncommitted at handoff.

The subsequent parity-fixture pruning and living-architecture documentation work is a separate
maintenance task. This document neither deletes oracle evidence nor authorizes removing existing
test obligations while implementing YNAB write-back.
