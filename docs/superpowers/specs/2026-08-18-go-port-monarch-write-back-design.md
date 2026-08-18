# Go Port Monarch Write-Back Design

**Date:** 2026-08-18
**Status:** Draft for review
**Branch:** `go-port`

## Summary

This slice makes a Monarch-backed Go profile usable for ordinary editing by attaching remote
write-back to the existing review and commit boundary. It ports the proven Python transaction
mutation surface while replacing Python's ambiguous partial-failure behavior with a durable,
crash-resumable write batch.

The ordinary interaction stays fast: `w` opens review and `Enter` authorizes the commit. The review
then closes immediately while a bounded worker writes absolute transaction values to Monarch.
SQLite records every completed remote item, so a process exit, timeout, rate limit, or reconnect
never requires guessing which writes succeeded.

The slice is deliberately narrower than general Monarch administration. It writes merchant names,
existing Monarch category assignments, and report visibility. Monarch-owned category and group
management remains in Monarch, matching the Python application's provider-synced behavior.

## Relationship to Earlier Slices

This design extends:

- `2026-08-12-go-port-foundation-read-only-tui-design.md`
- `2026-08-13-go-port-read-only-web-design.md`
- `2026-08-14-go-port-sqlite-editing-design.md`
- `2026-08-15-go-port-monarch-read-refresh-design.md`
- `2026-08-17-go-port-profile-catalog-onboarding-design.md`

Those contracts remain authoritative unless this document names a refinement. In particular:

- SQLite remains the application source of truth.
- All accounting uses signed integer minor units.
- Journal replay remains the reference implementation for effective state.
- Provider network I/O never runs inside a SQLite transaction.
- Profile revision is semantic conflict detection; SQLite locking serializes physical writes.
- Complete Monarch refresh remains the correctness reference for provider reconciliation.
- Provider and store packages remain mutually isolated behind application orchestration.
- The Go v2 schema remains install-only; this slice adds no migration machinery.

## Goals

- Make `w`, then `Enter`, persist supported pending changes to Monarch.
- Preserve the existing review, keyboard, analytical-state, and exact-revision contracts.
- Make every remote write safe to resend after an unknown outcome.
- Persist partial progress so restart and cross-process hand-off are deterministic.
- Reconcile Monarch merchant identities returned from its name-addressed mutation.
- Treat provider rule overrides as authoritative data rather than terminal failures.
- Give every terminal batch state a non-wedging user resolution.
- Expose identical progress, attention, pause, resume, and reconnect behavior in TUI and web.
- Retain the privacy and no-auth web threat models from the earlier slices.

## Non-Goals

- Creating, renaming, moving, merging, or deleting Monarch categories or category groups.
- Creating a category through the transaction category selector on a Monarch profile.
- Deleting Monarch transactions.
- Editing transaction amount, date, notes, review state, goals, or splits.
- General outbound synchronization shared across unrelated providers.
- SimpleFIN, YNAB, Amazon, export, Python-state migration, or multi-profile transfer.
- Background services or daemons independent of a running Moneyflow process.
- Database or journal-payload migrations during v2 stabilization.
- Built-in web authentication, cookies, CORS, or credential entry outside onboarding.
- Removing Python, merging `go-port`, or pushing a branch.

## User-Visible Capability Scope

For a configured Monarch profile, write-back supports exactly these journal operation types:

- `merchant.label`
- `merchant.merge`
- `merchant.reassign`
- `category.assign`, only to an active mapped Monarch category
- `transaction.hide-toggle`

The provider capability projection disables:

- `category.create`
- `category.label`
- `category.move`
- `category.merge`
- `category.delete`
- `group.create`
- `group.label`
- `group.merge`
- `group.delete`

Accordingly, `C` and `G` remain visible where the action/help contract requires them but explain
that Monarch taxonomy is managed in Monarch. The `c` selector lists existing mapped Monarch
categories and does not offer create-on-the-fly. A protected local Uncategorized sentinel without
an active Monarch category identity is not a writable assignment destination.

Provider-specific validation happens at staging time, again at review, and authoritatively during
batch preparation. Preparation rejecting an unsupported operation is defense in depth: current-
schema clients should not be able to stage one on a Monarch profile.

## Named Behavioral Differences from Python

The consolidated Go editing divergence list gains these write-back items:

1. Partial remote success is durably recorded instead of leaving the application unable to know
   which writes succeeded.
2. A confirmed batch resumes after process exit or reconnect without asking for confirmation again.
3. Editing, undo, redo, refresh, and another commit are temporarily disabled while a batch exists.
   Python can continue queuing edits while its asynchronous commit is running. The installed shape
   leaves room for a later unfrozen journal suffix, but that is outside this slice.
4. Provider rules may override requested fields. Go accepts and counts the echoed provider values;
   Python treats any successful mutation call as if the requested local values won.
5. Monarch category and group management is capability-disabled explicitly in both renderers,
   correcting the current Go profile-neutral capability exposure and matching Python's provider-
   synced gate.

The previously named Go editing and refresh divergences remain unchanged.

## Architecture and Dependency Direction

```text
cmd/moneyflow/                    runtime wiring and CLI session lifecycle
internal/tui/                     TUI review, progress, and batch actions
internal/api/                     profile-scoped mutation and status HTTP contract
web/src/                          web review, progress, and batch actions
internal/app/                     planning, orchestration, scheduling, reconciliation
internal/provider/                provider-neutral writer types and stable errors
internal/provider/monarch/        private GraphQL mutation and error translation
internal/store/                   durable batch, lease, and atomic plan contracts
internal/store/sqlite/            SQL implementation and installed schema
internal/domain/                  stable entities, journal, import, and write values
internal/replay/                  reference effective-state replay and provider rebase
```

Dependencies remain one-way:

```text
provider/monarch -> provider + domain
app              -> provider + store + domain + replay
store/sqlite     -> store + domain + replay
store            -X-> provider
provider         -X-> store
renderers        -X-> provider/monarch
```

`internal/store` owns SQL, durable batches, provider identity persistence, operation leases, and
atomic state changes. `internal/provider/monarch` owns GraphQL documents and wire values. Only
`internal/app` combines the two.

Architecture tests extend the current provider/store isolation checks. They also prove that only
application orchestration and command factory wiring import `internal/provider/monarch`, and that
write planners have no store handle or provider transport.

## Provider Writer Contract

`internal/provider` adds only the capability consumed by this slice:

```go
type Writer interface {
    ProbeIdentity(context.Context) (ProfileIdentity, error)
    UpdateTransaction(context.Context, TransactionUpdate) (TransactionUpdateResult, error)
}
```

`TransactionUpdate` contains:

- transaction external ID
- optional absolute merchant name
- optional absolute category external ID
- optional absolute hidden value

`TransactionUpdateResult` contains only the normalized response fields required for validation:

- transaction external ID
- merchant external ID and normalized provider label
- category external ID when returned
- hidden value
- field-presence information distinguishing omitted values from explicit zero values

Provider-neutral values contain no GraphQL types. Monarch's writer ports the Python
`Web_TransactionDrawerUpdateTransaction` mutation and reads its transaction and payload-error
fields. Raw payload errors are translated at the Monarch boundary into stable allowlisted provider
codes; raw bodies and provider messages never escape.

`Source` gains `Writer(context.Context, bool)` alongside `Reader(context.Context, bool)`. It returns
the same `SessionFingerprint` discipline used by refresh. Opening a writer never exposes session
material to the application.

`Writer.UpdateTransaction` performs exactly one HTTP attempt. It never invokes the read-side retry
path and never retries internally. The application worker is the only retry owner. This makes the
five-attempt bound observable and prevents nested retry multiplication.

## Monarch Name-Addressed Merchant Writes

The proven Monarch input accepts a merchant name, not a merchant ID. The response returns
`merchant { id name }`. Every write plan therefore distinguishes provider labels from local display
allocations.

- Reassigning or merging into a mapped provider merchant sends its persisted normalized provider
  label.
- A local deterministic suffix is presentation only and is never sent to Monarch.
- `merchant.label` sends the user's normalized new label.
- Reassigning into a newly created effective local merchant sends that merchant's unsuffixed user
  label.

Raw normalized provider labels are persisted uniformly for accounts, merchants, groups, and
categories, even though this slice writes only merchants. This closes a gap between the earlier
refresh specification and the installed implementation, whose allocation record currently stores
only the collision key and allocated display label.

### Name ambiguity

Name-addressed writes refuse nondeterminism before staging:

- a mapped reassign/merge destination is unwritable when its provider label collides with two or
  more active mapped Monarch merchants;
- a pure rename is refused when its desired provider label ambiguously identifies active provider
  merchants;
- a desired label matching a provider identity still actively mapped to a retired local merchant
  is refused, because redirecting that merchant's prior successor chain would invent intent;
- a label/merge source with no effective transactions is refused because it has nothing to write
  and a local-only result would revert on refresh.

Historical aliases do not by themselves make a new name ambiguous. If Monarch resolves a new name
to an alias, response reconciliation follows the alias rules below.

An opt-in live characterization records whether Monarch exposes a supported ID-addressed
transaction merchant field. Production behavior in this slice does not depend on that result.

## Deterministic Batch Planning

Preparation compares the committed base with the authoritative replayed effective snapshot. It
emits at most one write item per transaction. An item contains:

- opaque item ID and bytewise deterministic position
- local and Monarch transaction identities
- local source/destination merchant identity needed for reconciliation
- optional desired merchant name
- optional desired category external ID
- optional desired hidden Boolean
- originating operation IDs
- merchant expectation kind: `existing`, `merge_destination`, or `new`
- expected external merchant ID for an existing or merge destination
- deterministic new-name group key for a new expectation
- attempt, state, and result metadata

The item stores the expectation as well as the request so any process can validate the response
without reconstructing process-local assumptions.

Fields are absolute final values, never toggles or relative transitions. Only fields whose
effective value differs from the committed base are included. A transaction whose net effect is a
no-op produces no item. Consequently, a crash after Monarch applies an item but before SQLite
persists its response may safely resend the same item.

Operations translate as follows:

- `merchant.label` sweeps every effective transaction belonging to the source merchant and writes
  the new user label.
- `merchant.merge` sweeps every effective source transaction and writes the destination's provider
  label.
- `merchant.reassign` writes its exact resolved transaction targets using the destination provider
  label or a newly created effective merchant's user label.
- `category.assign` writes its exact resolved targets using the active category external ID.
- `transaction.hide-toggle` resolves to the absolute effective hidden value.

The service validates an active category mapping at staging. Batch preparation revalidates it in
the same authoritative transaction. A category removed remotely after preparation produces a
reconcile-only item failure; it is never silently redirected.

### New-name leaders

Concurrent writes of the same previously unknown merchant name could make Monarch create multiple
merchant identities. Every `new` expectation group therefore has one deterministic leader: the
item with the bytewise-lowest transaction external-ID string. Ordering never parses Monarch IDs as
numbers or assumes equal length.

The leader runs alone. Its successful response and resulting merchant ID commit durably before the
remaining group members become eligible for the ordinary concurrency-four worker. Same-batch
operation chains targeting the same unmapped effective local merchant share one group key and must
resolve to one merchant ID.

## Durable State Machine

There is no durable `prepared` state. The preparation transaction either rolls back or commits a
batch directly in `writing`.

```text
writing -> reconciling -> complete
   |            |
   |            +-----> attention_required
   +----> paused --------------------> writing
   +----> reconnect_required --------> writing
   +----> rate_limited --------------> writing
   +----> attention_required --------> writing
                         |
                         +-----------> reconciling (Stop and reconcile)
```

The batch state and version are durable. Pause is durable. A new process honors it rather than
resuming automatically.

`writing` and `reconciling` hold the provider-operation lease. `paused`, `reconnect_required`,
`rate_limited`, and `attention_required` release it so either renderer can later act. Resume or
reconcile must reacquire the correct lease kind before changing state. Completion releases it.

While any unfinished batch exists:

- browsing and analytical navigation remain available;
- edit, undo, redo, manual/automatic refresh, and another commit are disabled;
- the six-hour refresh scheduler is suppressed regardless of the prior refresh timestamp;
- status and capability reads remain available;
- no new provider network operation may begin except resuming or reconciling that batch.

## Atomic Batch Preparation

`w`, then `Enter`, submits the expected profile revision and reviewed revision exactly as today.
Any fast check against the cached snapshot is courtesy only. The authoritative preparation occurs
inside one SQLite immediate transaction:

1. Read current revision, refresh generation, binding, lease, batch state, journal, and cursor.
2. Reject a stale expected/reviewed revision before constructing durable work.
3. Atomically acquire the provider-operation lease as kind `write`; if a live refresh or write owner
   holds it, return the corresponding in-progress code without changing anything.
4. Verify no unfinished batch exists.
5. Recompute authoritative replay and validate the exact reviewed active operation prefix.
6. Reject unsupported operations or invalid provider mappings before any network call.
7. Build and validate deterministic absolute items and new-name groups.
8. Permanently discard the inactive redo tail.
9. Persist the batch, frozen prefix identity, and items in `writing`.
10. Increment profile revision exactly once and commit.

The review closes after this transaction succeeds. If another renderer confirmed a stale review,
the existing `revision_conflict` is returned and no Monarch request occurs.

## Refresh and Preparation Race Guards

The lease coordinates work but never supplies correctness. Two independent transaction guards
close the refresh/write race:

1. Batch preparation acquires the write-kind lease within its preparation transaction. It cannot
   prepare while a live refresh owner holds the lease.
2. Every provider refresh fold, including a fetch started before batch preparation, authoritatively
   asserts inside its SQLite transaction that no write batch exists in any phase. Its fold input is
   therefore `(expected refresh generation, lease owner/kind, no batch)`, not generation alone.

A refresh process that loses this race discards its already-fetched candidate and reports the
stable in-progress/stale outcome. It never rebases a frozen prefix. A write process that loses the
lease race returns `provider_refresh_in_progress`; the user can confirm again after refresh
completes.

## Worker, Retry, and Session Rules

The worker probes live `subscription.id` against the stored binding before its first write. A
mismatch changes nothing and parks for manual action. One provider-operation lease allows only one
TUI or web process to send at a time.

After new-name leaders, up to four independent transaction items run concurrently. Each successful
result commits before further dependent work is released. Operational item, lease, attempt,
progress, pause, and parked-state writes do not increment profile revision or refresh generation.

Unavailable or incomplete-response items receive at most five worker-owned attempts with jittered
exponential delays beginning at two seconds and capped at 60 seconds. A bounded
`provider_rate_limited` duration is copied into the batch's `next_eligible` timestamp and exposed as
counts/timing status. The batch releases its lease while rate-limited and cannot resume before that
timestamp. A long-lived process automatically attempts to reacquire the lease when the timestamp
arrives; a user does not have to press Resume after an ordinary rate limit.

On `provider_reconnect_required`, the worker follows the reader rule exactly:

1. Re-read the session file once.
2. Re-probe household identity.
3. Restart the absolute item once.
4. If authentication still fails, park in `reconnect_required` and release the lease.

Long-lived processes observe session-file replacement on their existing status tick. The TUI's
standing tick may resume a batch whether or not a web page is visible. Web polling remains
visibility-gated, so a background tab does not drive resumption; it catches up on visibility.
Session replacement automatically resumes `reconnect_required`; `paused` and either attention class
always require explicit user action.

`moneyflow provider connect monarch` installs or replaces the session but neither sends batch items
nor mutates batch status. A running TUI/web process, or the next one opened, resumes through the
normal tick. `moneyflow provider disconnect monarch` is refused while an unfinished batch exists
and explains that the user must pause and reconnect, or Stop and reconcile, first.

Shutdown may allow an in-flight HTTP request to finish or abandon it. Either is safe because an
unknown outcome resends an absolute item.

## Provider Response Reconciliation

The response transaction ID must equal the requested transaction ID. Missing, malformed, or
contradictory identity information never silently commits.

Monarch rules may alter requested or unrequested fields. This is ordinary provider behavior, not a
wedged batch:

- known echoed category and hidden values become authoritative for the transaction;
- a requested field that differs from its echo increments the provider-override count;
- an unrequested known field that differs also increments that count;
- an echoed category identity unknown locally waits for the immediately due full refresh;
- until that refresh, finalization folds the requested value when one exists, otherwise the prior
  effective value;
- renderers announce only `N fields overridden by provider`.

Merchant identity remains stricter because a wrong identity changes stable drill ownership:

- an `existing` expectation must return its expected active merchant ID;
- a `merge_destination` expectation must return the explicit mapped destination ID;
- a `new` group must return one consistent resulting ID and normalized provider label;
- contradictory group IDs, unexpected active owners, or invalid identities enter reconcile-only
  attention.

### Active mapping rotation and lineage

For a pure label rename whose response returns a new, unmapped merchant ID:

- the new external ID becomes the active mapping for the same stable local merchant ID;
- the old external ID moves, rather than disappears, into historical provider-identity lineage;
- the lineage record retains the provider label observed at rotation time;
- the old local drill stays attached to the renamed stable merchant.

If a `new` expectation returns an historical alias:

- an alias of the same local entity rotates back to active and the currently active external ID
  becomes the alias;
- an alias of another entity becomes active for the intended new local merchant, the prior alias
  disposition is retired, and lineage remains recorded;
- no retired successor chain is rewritten.

For an explicit merge, the source external mapping remains attached to the retired source and the
destination mapping remains active.

### Refresh behavior for aliases and retired sources

A complete refresh treats historical aliases and external identities mapped to retired merchant
tombstones by actual transaction membership:

- present in the provider merchant list with zero imported transactions: ignore it for active
  label/entity planning and preserve its alias or retired state;
- referenced by imported transactions: allocate a fresh local merchant ID, restore that external
  ID as active for the fresh entity, and preserve the previous lineage and retired tombstone;
- never reactivate a previously merged local merchant merely because its provider identity appears
  in an entity list.

This preserves resolve-once edit semantics: transactions that did not exist when the user confirmed
a rename are not silently captured by that old operation.

## Finalization and Commit Equivalence

When every item has a valid persisted result, the worker enters `reconciling`. One SQLite immediate
transaction:

1. Verifies batch ID/version/state, frozen operation IDs, current revision, refresh generation, and
   operation lease owner/kind.
2. Reloads committed state and journal and replays the frozen prefix authoritatively.
3. Applies accepted response overrides and validated identity rotations to the replay result.
4. Validates all financial, referential, collision, identity, and known-drill invariants.
5. Replaces the committed rows, identity mappings/lineage, provider labels, and known drills.
6. Stores a counts-only last-write summary containing completion time, committed revision, item
   count, and provider-override count.
7. Removes the frozen operations and durable batch/items.
8. Marks provider refresh due without claiming that a full refresh already succeeded.
9. Increments profile revision exactly once and commits.

The test oracle is:

```text
committed after finalization
=
effective immediately before preparation
+ accepted provider response adjustments
```

Allowed adjustments are exact identity rotations/lineage and echoed provider field overrides. No
other local state may differ. A fresh reopened profile must produce the same canonical logical
encoding.

After finalization, the initiating long-lived process schedules a complete reconciliation
immediately. Editing is no longer blocked; if another edit lands before the fetch folds, the
existing refresh rebase handles it. The write does not advance `LastSuccess`, so another process's
ordinary scheduler also sees refresh as due.

## Attention and Stop-and-Reconcile

After bounded attempts, an unresolved item stops unsent work and enters `attention_required`.
Persisted attention reasons are allowlisted and value-free.

Retryable reasons are:

- bounded provider unavailability exhausted
- incomplete or malformed response after a request with an unknown outcome

Reconcile-only reasons are:

- remote transaction or category not found
- deterministic provider validation rejection
- contradictory or unexpected merchant identity
- unexpected retired-identity resolution
- invalid persisted expectation or batch invariant

Rate limiting and reconnect retain their dedicated parked phases.

The renderer offers:

- **Resume** for `paused` and retryable `attention_required`; eligible `rate_limited` and healed
  `reconnect_required` resume automatically when a long-lived process is present, while the same
  action remains available as a harmless explicit nudge;
- **Stop and reconcile** for either attention class;
- no Resume action for reconcile-only attention.

Resume is one phase-dependent API action, not a separate retry endpoint. It always carries the
expected batch version and reacquires the operation lease before changing durable state.

Stop and reconcile explicitly abandons failed and unsent intent. It does not attempt to shrink
structural operations into transaction targets they never contained. Instead it:

1. Retains the durable batch and successful item facts.
2. Acquires the reconcile-kind provider-operation lease.
3. Probes household identity and fetches a complete snapshot through the existing reader contract.
4. Applies the existing snapshot-integrity retries, pending exclusion, account-scope rule, and
   deletion plausibility checks.
5. Atomically folds the verified remote snapshot while removing the entire frozen prefix and batch.

Already-applied remote changes therefore survive; failed and unsent changes are abandoned. If the
session expires, reconciliation parks in `reconnect_required`. A suspicious deletion candidate
uses the existing process-bound, expiring, refresh-generation-bound confirmation discipline.
Pagination or snapshot-integrity failures can never be confirmed away.

## Provider Operation Lease

The current singleton refresh lease becomes a singleton provider-operation lease with purpose
`refresh`, `write`, or `reconcile`. It retains owner renderer, opaque instance ID, and expiry.

- Acquisition, renewal, release, and operational status never increment profile revision.
- Lease expiry allows another process to resume; it does not make stale work correct.
- Every fold/finalization verifies its lease owner and purpose inside its transaction.
- Parked phases release the lease.
- A process-local in-flight request may finish after lease loss, but its result cannot persist
  unless the batch/version and lease CAS still succeed. The absolute item may safely be retried.

## Application Capabilities

Static action identity and help text remain unchanged. Availability becomes profile- and batch-
aware:

- unbound/local profiles retain the editing-slice behavior;
- bound Monarch profiles enable supported merchant/category/hide staging;
- Monarch taxonomy management and category creation are unavailable with fixed reasons;
- `commit` becomes available for a supported nonempty active prefix when no batch exists;
- while a batch exists, edit/undo/redo/refresh/commit expose one shared unavailable reason;
- batch pause/resume/reconcile availability derives solely from durable phase and attention class.

Capability checks are repeated inside store transactions. Renderer state never grants authority.

## TUI Experience

The review screen says that `Enter` writes the active operations to Monarch. No second ordinary
confirmation is added. `w`, then `Enter`, remains the fast path.

After preparation:

- review closes immediately;
- the current finance view, cursor, scroll, drill, and keyboard navigation remain active;
- a counts-only line reports writing, waiting, reconnect, reconciliation, or attention;
- pending markers remain until finalization;
- `w` opens batch status/actions instead of a second journal review;
- completion clears pending markers and announces operation/item counts;
- provider overrides are announced only as a count.

The batch status overlay provides Pause, Resume when eligible, Stop and reconcile, and Reconnect
when required. It explains that Pause stops future calls but cannot undo writes already accepted by
Monarch. Closing the overlay never pauses or cancels the batch.

The TUI's standing provider-status tick detects session replacement and may acquire the lease to
resume. `q` retains the durable-pending messaging: confirmed outbound work is safe on disk and may
resume next launch.

## Web Experience and HTTP API

The existing profile-scoped commit endpoint remains the ordinary entry point. For a Monarch-bound
profile it returns after durable batch preparation with:

- schema version and new profile revision
- canonical analytical projection and selection disposition
- batch phase/version
- total, completed, failed, and remaining item counts
- provider-override count

Additional profile-scoped routes are:

```text
GET  provider/write-status
POST provider/write/pause
POST provider/write/resume
POST provider/write/reconcile
POST provider/write/reconcile/confirm
```

There is no separate retry route. `resume` applies the phase-dependent rules above.

Every POST requires the existing mutation token, canonical-origin match, Fetch Metadata checks, and
cookie-free no-CORS policy. It carries expected batch version. Stale controls return conflict and
never act on a newer batch. Reconcile confirmation tokens never enter URLs or history.

`provider/write-status` is credential-blind and returns only:

- wire schema version
- profile revision, refresh generation, and batch version
- stable phase and allowlisted reason code
- total/completed/failed/remaining/override counts
- next-eligible time
- owner renderer and opaque instance ID while leased
- current capability actions

It never returns provider IDs, local entity IDs, operation IDs, labels, requested fields, or raw
errors.

The web review drawer uses the same `w`, then `Enter`, path. Its provider status controller polls
only while visible. A background tab does not resume work; it observes current durable status when
visible again. No push channel is added.

## Stable Failure Contract

Provider write failures extend the neutral error taxonomy with these stable value-free codes:

- `provider_write_in_progress`: another process holds a live write/reconcile lease
- `provider_write_attention_required`: the batch is parked with an allowlisted attention reason
- `provider_write_stale`: batch ID/version/state or frozen-prefix CAS failed
- `provider_write_paused`: the batch requires explicit Resume
- `provider_write_not_eligible`: a parked-until time has not arrived
- `provider_write_unsupported`: the reviewed prefix contains an unwritable provider operation

Attention status uses these stable value-free reason codes:

- retryable: `provider_write_unavailable_exhausted`, `provider_write_response_incomplete`
- reconcile-only: `provider_write_target_not_found`, `provider_write_rejected`,
  `provider_write_identity_conflict`, `provider_write_retired_identity`,
  `provider_write_expectation_invalid`

Every error and reason belongs to exactly one worker scheduler class and one HTTP/problem mapping.
Existing reconnect, identity-mismatch, rate-limit, unavailable, data-invalid, refresh-stale,
deletion-confirmation, refresh-in-progress, and storage errors retain their established meaning.

Neither renderer automatically replays a user mutation after revision conflict. Automatic resend
exists only inside an already-authorized durable batch and is safe because the item is absolute.

Storage failure while preparing changes nothing. Storage failure while recording a response leaves
the item unresolved and safe to resend. Finalization failure leaves the complete batch, journal,
and persisted item results intact.

## Privacy and Security

The profile database already contains financial labels and provider identities. Durable write items
may contain exact requested merchant labels and provider IDs only inside that private profile.
Session tokens, credentials, device UUIDs, account passwords, mutation tokens, and raw provider
payloads never enter SQLite.

Logs and persisted operational status retain an allowlist. They may contain only:

- stable error/reason codes
- profile revision, refresh generation, and batch version
- counts and percentages
- bounded timings and next-eligible timestamps
- renderer class and opaque process instance ID
- correlation IDs

They never contain merchant, category, group, or account labels; transaction descriptions or
notes; search text; provider IDs; household IDs; GraphQL variables or bodies; HTTP response
fragments; email; credentials; session tokens; device UUIDs; filesystem home paths; or complete
URLs.

Synthetic tests use reserved example values only. Live account data never enters fixtures,
screenshots, docs, parity artifacts, browser recordings, or error messages.

## Installed Schema and Version Policy

The installed schema gains:

- normalized raw provider label on provider label allocations
- provider identity lineage/alias records with provider label and disposition
- generalized provider-operation lease purpose
- one unfinished provider write batch per profile
- deterministic write items, expectations, results, attempts, and dispositions
- a singleton counts-only last-write summary retained after completed batch/item details are removed

Constraints enforce supported phases, expectation kinds, lease purposes, nonnegative counts, and
the presence/absence relationship of optional fields. No money column is added. All tables remain
STRICT.

Lineage rows key `(namespace, external_id)` and store entity kind, prior local entity ID, current
successor local entity ID, normalized provider label at rotation, disposition
`alias|reactivated`, and batch version. Write batches store opaque batch ID, phase, version,
reviewed/prepared revisions, refresh generation, frozen cursor/prefix digest, retry class/reason,
counts, timestamps, and next-eligible time. Item rows store the deterministic request/expectation
shape described above; their uniqueness key prevents two items for one transaction in one batch.
The last-write summary stores no item or entity identities.

`CurrentSchemaVersion` increments in the same commit as the shape change. Existing preview profiles
receive `schema_incompatible` recovery guidance and are never queried through the new shape. No SQL
or journal-payload migration is added.

## Testing Strategy

### Pure planning and provider tests

- Port the exact Monarch update mutation through an injected loopback HTTP transport.
- Verify one writer call makes one HTTP attempt and emits only selected non-nil fields.
- Redact HTTP, GraphQL, and payload errors at the Monarch boundary.
- Derive at most one absolute item per transaction and omit net no-ops.
- Cover every writable operation type and reject every unsupported type.
- Prove category and transaction external mappings are required at staging and preparation.
- Prove local display suffixes never reach the provider; provider labels do.
- Cover provider-label ambiguity and zero-transaction merchant refusals.
- Order new-name leaders bytewise and release followers only after the leader result persists.
- Cover same-batch chains targeting one newly created effective merchant.
- Treat same-entity and different-entity historical-alias results according to the rotation rules.
- Refuse retired-label matches and contradictory merchant response groups.
- Accept known provider rule overrides and count requested/unrequested changed fields.
- Hold unknown echoed category identities for the following refresh while folding the requested or
  prior effective value temporarily.
- Test the opt-in live characterization for merchant ID addressing and empty-merchant retention
  without making ordinary automated verification depend on a live account.

### Store and crash tests

- Preparation performs revision CAS, lease acquisition, redo discard, prefix freeze, batch/item
  insertion, and one revision increment atomically.
- A failed preparation changes no lease, journal, batch, item, or revision.
- Exactly one of concurrent TUI/web preparations succeeds.
- A live refresh lease causes preparation to return refresh-in-progress unchanged.
- A refresh fold refuses whenever any batch exists, including a fetch that began first.
- Item result/status writes do not increment revision or refresh generation.
- Crash before send leaves the item eligible once.
- Crash after send but before persist resends the absolute item and finalizes idempotently.
- Crash after result persistence skips that item on resume.
- Lease expiry hands work from TUI to web and from web to TUI without duplicate finalization.
- Parked phases release the lease; writing/reconciling phases require it.
- Pause persists across reopen.
- Rate-limit duration persists into `next_eligible` and prevents early resume.
- Finalization is atomic; injected failure at every write boundary preserves the batch and journal.
- Reopened canonical logical state equals the reference finalization plan.
- Schema constraints and inspection tests cover every new table/index and the schema-version bump.

### Identity and refresh tests

- A pure merchant rename rotates the active external ID while preserving the local ID and drill.
- Old external IDs move into lineage with their provider labels and do not create phantom merchants
  when listed without transactions.
- An alias or retired source referenced by transactions promotes to a fresh local merchant without
  reactivating the old tombstone.
- An explicit merge retains its retired source and active destination semantics.
- A returned retired active mapping enters reconcile-only attention.
- Complete refresh after finalization preserves all validated write-back identity decisions.
- Stop and reconcile removes the frozen prefix atomically with the authoritative provider fold.
- Reconcile inherits identity probe, snapshot-integrity retries, deletion plausibility confirmation,
  account scope, pending exclusion, and integrity-cannot-confirm behavior.
- Refresh generation and batch/version CAS reject stale candidates.

### Application and renderer tests

- `w`, then `Enter`, closes review after durable preparation without extra confirmation.
- View state, cursor, scroll, drill, and selection survive preparation and completion where their
  identities survive.
- Pending markers remain during writing and clear after finalization.
- `w` opens batch status while a batch exists.
- TUI and web show identical phase, counts, reasons, actions, and capability-disabled text.
- Pause, eligible Resume, reconcile-only attention, Stop and reconcile, and confirmation work in
  both renderers.
- Session replacement heals a parked batch through the TUI standing tick and visible web polling.
- A background web tab performs no resume work and catches up on visibility.
- Stale review and stale batch controls produce conflict without provider calls.
- Field overrides produce a counts-only announcement.
- Unsupported taxonomy is unavailable at staging and still rejected at preparation.
- `provider disconnect monarch` refuses an unfinished batch with actionable guidance.
- Status/problem payloads contain no request body, identity, label, credential, or raw provider
  error.

### Property and equivalence tests

- For randomized supported journal histories, batch items reproduce the effective transaction
  differences exactly.
- Randomized crash/resume schedules produce the same final canonical state as uninterrupted work.
- Final committed state equals the reviewed effective state adjusted only by generated accepted
  response overrides and identity rotations.
- Full refresh after finalized write-back is idempotent modulo genuine provider changes.
- Reference and optimized finalization paths have identical canonical logical encodings.

### Performance and verification gates

- Batch planning for 100,000 committed transactions and a 10,000-operation journal remains within
  the existing cold-load performance family: 250 ms reference target and 1 s CI ceiling on the
  supported benchmark host.
- Finalization of 100,000 rows remains within the same 1 s CI ceiling excluding provider network
  time.
- Four-way fake-provider write throughput is measured but network timing is not a CI gate.
- `make test`, `make test-store`, `make test-race`, `make lint`, `make parity`, `make verify-go`, and
  `make verify-web` remain green through the repository's supported non-performance path when host
  load invalidates a timing gate.
- Python tests, pyright, Ruff formatting/linting, and markdown checks remain green because Python is
  still the behavioral oracle on this branch.
- Privacy scanning covers source, fixtures, generated API schema, logs, status payloads, and docs.

Live dogfooding occurs only after the automated-verified implementation commits. It covers a small
merchant edit, category assignment, hide/unhide, multi-transaction merchant normalization, rule
override if naturally available, process exit/resume, reconnect recovery, and subsequent full
refresh. Destructive remote deletion and forced rate limiting are synthetic-only tests.

## Completion Criteria

This slice is complete when:

- a Monarch user can stage, review, and persist merchant, mapped-category, and hidden-state edits
  through both TUI and web;
- `w`, then `Enter`, remains the ordinary commit gesture with no extra confirmation;
- partial success, crash, pause, rate limiting, and reconnect survive restart deterministically;
- no terminal error can permanently wedge the profile;
- provider rule overrides become counted authoritative state;
- merchant identity rotation preserves stable local drills without phantom reactivation;
- refresh and batch preparation cannot cross-fold through their transaction guards;
- every provider/write error and attention reason has one tested scheduler/action class;
- finalization satisfies the response-adjusted commit equivalence invariant;
- generated API and renderer behavior remain counts-only outside ordinary finance projections;
- the installed schema version advances with no migration code;
- the diff contains no provider taxonomy mutation, transaction deletion, unrelated provider,
  export, Python migration, committed screenshot, or committed web build output;
- all required Go, web, Python, lint, type, parity, privacy, and formatting checks pass;
- every agent-authored implementation change is committed before live dogfooding or handoff.

Completion does not authorize pushing, merging `go-port`, removing Python, or beginning another
provider slice.

## Implementation Decomposition

The implementation plan should keep independently green checkpoints:

1. Installed schema, provider-operation lease, write batch/item store contracts, and atomic race
   guards.
2. Provider-neutral writer plus Monarch GraphQL mutation, response normalization, and error
   translation.
3. Pure batch planning, provider capability restrictions, leader grouping, and identity lineage.
4. Application worker, retries, session reload, attention/reconcile, and finalization.
5. TUI review/progress/status actions.
6. HTTP contract, generated client, and web review/progress/status actions.
7. Cross-process, crash, property, performance, privacy, parity, and live dogfood gates.

Each checkpoint follows test-first implementation and ends in a verified commit. The detailed plan
is written only after this assembled specification is reviewed and approved.
