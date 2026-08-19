# Go Port Transaction Deletion and Duplicate Review Design

**Date:** 2026-08-18
**Status:** Approved
**Branch:** `go-port`

## Summary

This slice closes two related Python-to-Go workflow gaps: staging transaction deletion with `x`
and finding and resolving exact duplicate transactions with `D`. Both workflows use the existing
renderer-neutral application service, durable journal, SQLite replay model, and Monarch write-back
worker. The TUI and web application expose the same keyboard-driven behavior.

Go deliberately improves on Python's destructive behavior. Python deletes a transaction remotely
immediately after confirmation and cannot undo it. Go records `transaction.delete` as durable
pending intent. The row disappears from effective analytical state immediately, `u` restores it,
and the provider is not mutated until the established `w`, then `Enter`, commit boundary.

Duplicate detection remains deliberately simple and faithful to Python's implementation. It groups
transactions only when date, money, un-suffixed merchant label, and account match exactly. It does
not implement fuzzy dates, approximate amounts, or configurable account matching in this slice.

## Relationship to Earlier Slices

This design extends:

- `2026-08-12-go-port-foundation-read-only-tui-design.md`
- `2026-08-13-go-port-read-only-web-design.md`
- `2026-08-14-go-port-sqlite-editing-design.md`
- `2026-08-15-go-port-monarch-read-refresh-design.md`
- `2026-08-17-go-port-profile-catalog-onboarding-design.md`
- `2026-08-18-go-port-monarch-write-back-design.md`
- `2026-08-18-go-tui-chrome-review-info-design.md`

Those contracts remain authoritative unless this document explicitly refines them. In particular:

- SQLite remains the source of truth.
- All money uses signed integer minor units plus currency and scale.
- Full journal replay remains the correctness reference for effective state.
- Provider network I/O never runs inside a SQLite transaction.
- The profile revision is the semantic compare-and-swap boundary.
- The provider-operation lease is for liveness and never substitutes for transactional guards.
- Monarch refresh remains a complete authoritative reconciliation.
- Stop and reconcile removes an entire frozen prefix rather than retaining failed intent.
- TUI and web renderers submit intent through the same application service.
- The Go v2 schema remains install-only and gains no migration machinery.

## Goals

- Implement the documented `x` transaction deletion action.
- Make deletion durable, reviewable, undoable, redoable, and crash-resumable.
- Extend Monarch write-back with safely repeatable transaction deletion.
- Implement the documented `D` duplicate-review action.
- Match Python's actual duplicate grouping and overlay keyboard workflow.
- Keep duplicate analysis as ordinary Go functions over domain slices.
- Keep TUI and web behavior aligned through shared application operations.
- Preserve exact selection, revision, privacy, and bounded-wire contracts.
- Maintain responsive behavior at 100,000 transactions.

## Non-Goals

- Fuzzy duplicate detection, date tolerances, approximate amounts, or scoring.
- User-configurable duplicate rules or Python's unused `strict_account_match` argument.
- Automatically deciding which member of a duplicate group should be deleted.
- Deduplication during provider import.
- Transaction deletion for providers other than Monarch.
- Editing amounts, dates, notes, review state, splits, or recurring rules.
- Export, YNAB, SimpleFIN, Amazon import or matching, or MCP support.
- Python profile migration or removal of the Python package.
- Schema or journal-payload migration machinery.
- Automatically testing deletion against real personal transactions.

## Named Behavioral Differences and Parity Notes

The consolidated parity list gains one deliberate behavior change:

1. Python sends transaction deletion immediately after its confirmation dialog. Go stages one
   durable deletion operation and sends it only through `w`, then `Enter`. This makes deletion
   undoable and places every destructive provider mutation behind the same review boundary.

The duplicate detector follows two Python implementation details that differ from broader possible
interpretations:

- Python's docstring says dates may be within one day, but its code groups on exact dates. Go uses
  exact dates. This is parity with executable behavior, not with the inaccurate docstring.
- Python lowercases merchant labels for matching. Go uses Unicode lowercase only. It deliberately
  does not use the stronger label collision key, which also applies normalization and case folding.
  Changing to the collision key would alter grouping and requires a separate design.

The Python TUI does not expose its duplicate detector's `strict_account_match` option. Go therefore
does not port that option; account identity is always part of the grouping key.

## Architecture and Dependency Direction

```text
internal/tui --------------------+
                                 |
internal/api <- web frontend ----+--> internal/app --> internal/analytics
                                         |                    |
                                         |                    v
                                         +--------------> internal/domain
                                         |
                                         +--> internal/replay --> internal/domain
                                         |
                                         +--> internal/store
                                         |          ^
                                         |          |
                                         +-- closed transactional callbacks
                                         |
                                         +--> internal/provider
                                                    ^
                                                    |
                                         internal/provider/monarch

internal/store/sqlite --> internal/store + internal/domain
```

Duplicate detection belongs in `internal/analytics`. It receives domain transactions and returns
domain-level duplicate groups. It does not import SQLite, HTTP, TUI, web, provider, or journal
packages.

The application layer owns filtered-result resolution, duplicate projection, deletion target
resolution, journal creation, review summaries, and provider write planning. Renderers do not
implement matching or construct operations directly.

`internal/provider` adds a provider-neutral delete port. `internal/provider/monarch` implements the
single-attempt GraphQL mutation. It never imports the store. The store does not import provider
packages. Application orchestration remains the only layer that connects durable write items to a
provider writer.

## Domain Operation

### Operation shape

The domain operation registry adds:

```text
transaction.delete, payload version 1
```

The operation contains an ordered, unique list of stable local transaction IDs in `Targets`. Its
payload is otherwise empty. It never copies transaction values, provider IDs, merchant names,
search text, or filter predicates into the journal.

Targets are resolved once at operation creation. A nonempty submitted selection wins. With no
selection, the focused transaction row is the target. The action is unavailable from aggregate
rows or a transaction window without a focused row.

The mutation builder validates that every target exists in the effective snapshot at the accepted
revision and that no target is already effectively deleted. The authoritative revision comparison
and append occur in the same SQLite transaction. An inactive redo tail is permanently truncated
before appending, following the existing journal rule.

One confirmed bulk action produces one operation and one undo unit. Successful multi-select
deletion clears selection, matching the general bulk-edit contract. Single-row deletion does not
invent a selection.

### Replay

Replay applies operations in journal order. `transaction.delete` removes each target transaction
from the effective transaction slice. It does not remove or retire accounts, merchants, category
groups, or categories. Known drill identities remain durable, so a drill whose last transaction is
deleted renders the normal empty projection with the unchanged URL rather than an invalid view.

Undo moves the cursor before the deletion operation and restores rows from the unchanged committed
base. Redo reapplies the deletion. Process restart produces the same effective snapshot through
full replay.

Operations later in the active prefix cannot target an already deleted transaction. The normal
mutation builder prevents that sequence. Replay and stored-profile validation reject a malformed
journal that violates it rather than silently ignoring the later operation.

### Local commit

For an unbound local profile, atomic commit folds effective absence into committed state by deleting
the transaction rows named by the active operation. The existing fold invariant remains exact:

```text
freshly loaded committed state after commit
    == effective state immediately before commit
```

The fold removes the active journal and redo tail only after all committed rows and known-drill
state have been written successfully. Any failure leaves committed state, journal, cursor, and
revision unchanged.

Deleting a committed transaction does not delete its `external_identities` row. The mapping is a
durable transaction tombstone: if the same provider external ID reappears, refresh restores the
same stable local transaction ID. A provider row that reappears with a new external ID receives a
fresh local ID. This preserves the refresh contract that provider mappings are never reused for a
different identity and are not pruned merely because an entity is currently absent.

## Duplicate Detection

### Input scope

`D` scans the complete effective filtered transaction result represented by the current analytical
state. It is not limited to the current table window, viewport, API page, or duplicate-overlay
window. This matches Python's use of `state.get_filtered_df()`.

Search, time range, account, merchant, category, group, visibility, and any other committed
refinement therefore affect the scan exactly as they affect the finance table. Pending journal
effects are included because the detector receives effective transactions.

An empty input reports that there are no transactions to check. A nonempty result with no groups
reports that no duplicates were found and does not open an empty overlay.

### Exact grouping key

Two transactions belong to the same potential-duplicate group only when all of these values match:

1. exact calendar date;
2. exact signed minor-unit amount, currency, and scale;
3. Unicode-lowercased un-suffixed merchant label; and
4. effective account display label.

For a provider-backed merchant, the un-suffixed value is the persisted raw provider label from the
active label allocation. It is not the locally allocated display label, which may contain a
collision suffix. For a local merchant without a provider allocation, the current effective user
label is used.

This rule intentionally groups two distinct Monarch merchant identities whose provider labels are
both `Example Merchant`, even if their local display labels are `Example Merchant` and
`Example Merchant · a1b2`. Such pairs are a real duplicate source and match Python's string-label
behavior.

Lowercasing does not trim, normalize Unicode, apply NFKC, or use the domain collision key. Exact
date means exact date; no one-day tolerance is applied. Account matching is always enabled.

### Output and ordering

The pure detector returns groups with at least two effective transactions. It never marks or
deletes a preferred member automatically.

Ordering is deterministic:

- groups sort by date descending, then money ascending by currency, scale, and minor units, then
  lowercased merchant label, account display label, and the smallest transaction ID;
- rows within a group sort by stable local transaction ID using bytewise string ordering.

The application projection assigns display group numbers after sorting. Group numbers are
presentation values, not durable identities and not mutation targets.

## Deletion Interaction

Pressing `x` from a transaction row or duplicate review opens a confirmation surface containing
only the affected transaction count. It does not display provider IDs or copy financial labels into
logs.

Confirmation appends one operation. The accepted response immediately replays and reprojects the
current view:

- deleted rows disappear;
- aggregates, totals, bars, and counts update;
- duplicate groups recompute;
- any group with fewer than two remaining effective rows disappears;
- bulk selection clears;
- the status announces the number of staged deletions and directs the user to `w`.

Cancellation changes nothing. A stale confirmation receives `revision_conflict`; the renderer
does not replay it automatically.

Deletion confirmation is distinct from commit confirmation. `x`, then confirmation, stages intent.
The normal destructive remote boundary remains `w`, then `Enter`, with no additional ceremony.

## Duplicate Review Experience

### Shared projection

The application exposes a bounded duplicate projection containing:

- current profile revision;
- total duplicate group count;
- total transaction count across groups;
- a bounded ordered window of groups and rows;
- transaction-local presentation fields already allowed in the finance view;
- pending edit flags for effective rows; and
- enough stable opaque targeting information for selection and focused actions.

An actively deleted transaction is absent from effective state and therefore absent from duplicate
rows. Deletion status appears in pending review, not as a contradictory flag on a removed row.

Exact amounts travel over HTTP as canonical money strings with currency and scale. Provider
identities, label-allocation suffix material, credentials, session data, and raw journal payloads
never enter the wire response.

### TUI

`D` opens a responsive full-screen duplicate review over the current effective filtered result.
The header reports accurate transaction and group counts. Each row shows:

- selection and pending flags;
- group number;
- date;
- un-suffixed merchant label for matching context while retaining the ordinary display label where
  useful;
- amount;
- account; and
- ordinary pending-edit state for rows that remain effective.

The overlay owns these keys:

- `↑`/`↓` or `j`/`k`: move the cursor;
- `Home`, `PageUp`, and `PageDown`: navigate the bounded result;
- `Space`: toggle the focused row in the overlay selection;
- `i` or `Enter`: open transaction information;
- `h`: stage the ordinary hide toggle for the selection or focused row;
- `x`: confirm and stage deletion for the selection or focused row; and
- `Esc`: close duplicate review.

After a successful bulk hide or delete, the overlay selection clears. The overlay reprojects from
the service response rather than mutating a private copy of financial data. If all groups resolve,
it shows the success state and may close back to the preserved finance view. Undo and redo remain
finance-view actions after closing the overlay; the duplicate overlay does not create a second
history model.

### Web

The web application exposes the same workflow as an accessible dialog or routed overlay. It uses
kit-ui components, focus trapping, accessible labels, and announced status changes. Keyboard
behavior matches the TUI for `D`, navigation, `Space`, `i`, `h`, `x`, and `Esc`. Visible buttons
provide equivalent pointer and assistive-technology operation.

Opening and closing the duplicate surface does not discard the analytical URL, cursor restoration
state, scroll restoration state, or committed refinements. Duplicate overlay selection remains
transient browser state encoded through the existing opaque selection contract, never a durable
URL field.

The web client never performs duplicate grouping itself. It requests a bounded projection from Go
and re-requests after every accepted mutation or observed revision change.

## Review and Commit Presentation

Pending review describes the operation as `Delete transaction` or `Delete transactions` and shows
the affected target count. Its bounded target detail uses the existing review window and never
returns an unbounded identity list.

When same-prefix deletion leaves a merchant merge or label operation with zero effective member
transactions, review annotates that structural operation as `affects 0 transactions`. It remains
part of the reviewed prefix and one undo history. The network item count and completion message
count only provider requests actually sent; they do not imply that a vacuous structural operation
produced an item.

## Provider Write Item Union

### Durable shape

Provider write items become a discriminated union with `item_kind`:

- `update`: the existing absolute merchant, category, and hidden request fields;
- `delete`: one stable local transaction ID and one external provider transaction ID.

The installed schema enforces the union:

- a delete row has null merchant, category, hidden, expectation, and new-name group fields;
- an update row has at least one requested update field;
- an update row obeys the existing merchant expectation constraints;
- a delete row never belongs to a new-name leader group; and
- every row remains unique by batch and transaction.

The schema version advances from 6 to 7 in the same commit as the installed shape. There is no
migration. Existing preview profiles are refused as incompatible and use the already approved
catalog recovery and provider reimport flow.

### Planning

Preparation compares the committed base with the authoritative replayed effective snapshot. A
transaction present in committed state and absent from effective state produces exactly one delete
item.

Deletion supersedes field updates for the same transaction. When an active prefix renames,
recategorizes, or hides a transaction and later deletes it, the planner emits only its delete item.
The item retains the ordered originating operation IDs so frozen-prefix accounting and atomic
finalization still cover the entire reviewed prefix.

Delete items are independent of new-merchant leader groups and run at the ordinary bounded worker
concurrency alongside update items. They never wait for or establish a merchant identity.

No-op operations produce no provider item. Preparation still validates the full frozen prefix and
its digest before creating the batch.

### Vacuous merchant operations

Staging-time validation continues to refuse a merchant label or merge whose source already has no
effective transactions. This gives immediate guidance when the action has nothing to write.

Preparation applies a narrower rule for an operation that was productive when staged but became
empty because later same-prefix deletions removed every source transaction:

- a merchant merge is vacuous, produces no update item, and folds locally;
- a merchant label is vacuous, produces no update item, and folds locally; and
- neither condition makes the reviewed prefix unsupported.

The next successful complete refresh definitively restores the provider's unchanged label for a
vacuously relabeled provider-owned merchant. The refresh identity planner has no pending label
operation after finalization, so it rebuilds the merchant from the persisted raw provider label.
This outcome is a named test obligation.

## Monarch Delete Adapter

### Provider contract

The provider-neutral writer gains one single-attempt method equivalent to:

```text
DeleteTransaction(context, transaction external ID) -> normalized delete result
```

The Monarch adapter ports only the existing `Common_DeleteTransactionMutation` surface. It submits
the external transaction ID and normalizes the `deleted` flag and allowlisted error class. Raw
payload errors and message text never escape the adapter or enter logs.

The adapter never retries. The durable application worker owns the only bounded retry policy.

### Idempotency and not-found behavior

The desired state of a delete item is absolute: the provider transaction is absent. Therefore:

- `deleted: true` is success;
- a positively characterized provider not-found payload result is also success.

An HTTP 404 from the fixed GraphQL endpoint does not prove that the requested transaction is
absent. It enters reconcile-only attention through the ordinary target-not-found classification.

Correctness does not depend on Monarch returning a distinguishable not-found payload. Python reads
`deleteTransaction { deleted, errors }`, and an absent transaction may appear as `deleted: false`
with an error whose only useful distinction is message text. Go does not inspect or persist that
text. An unclassifiable payload rejection therefore enters reconcile-only attention rather than
being guessed as success.

An unknown transport outcome may have applied remotely. The worker can resend an absolute delete
within its bounded attempt policy. If the retry receives a proven not-found result, it records
success. If the result remains unclassifiable, the batch parks for authoritative reconciliation.

Monarch may refuse deletion of a bank-synced transaction or a bank sync may recreate it later. Go
does not promise that provider deletion is permanent. A later complete refresh always installs
provider truth and may show a resurrected transaction with its stable or newly mapped identity.

## Write-Back Lifecycle and Failure Handling

The existing durable batch phases, lease ownership, session reload, identity probe, pause, rate
limit, reconnect, and attention behavior remain unchanged. Crash-uncertain dispatch differs by item
kind because only deletion is safely repeatable:

- an attempted update item with no persisted normalized result parks for authoritative
  reconciliation before another network call; and
- an attempted delete item with no persisted normalized result remains eligible for resend within
  the bounded attempt budget because absence is its absolute desired state.

The worker persists item kind and attempt state before dispatch so a new process applies this rule
without inferring it from optional update fields. Resume owns the same process-local run guard as
the worker while it resumes, inspects, and parks an uncertain update, so a concurrent local runner
cannot claim that item in between. An incomplete response is outcome-uncertain and never qualifies
for explicit resend merely because its durable attention class is retryable.

Delete-specific outcomes map as follows:

- success or proven not-found: persist item success;
- bounded provider unavailability: retryable attention after the existing attempt budget;
- rate limit: the dedicated rate-limited phase with bounded retry time;
- reconnect required: the dedicated reconnect phase;
- unknown outcome after the bounded policy: reconcile-only attention;
- unclassifiable payload rejection: reconcile-only attention;
- deterministic refusal of a transaction still present remotely: reconcile-only attention; and
- malformed or contradictory response: reconcile-only attention.

Stop and reconcile retains the approved write-back meaning. It fetches and validates a complete
provider snapshot, then atomically folds that snapshot while removing the entire frozen prefix and
batch. It never rebases failed or unsent deletion intent back into the journal.

Already-applied deletions survive because the authoritative snapshot no longer contains those
transactions. Failed and unsent deletions are abandoned along with the rest of the frozen prefix.
If a refused transaction remains remotely, it remains locally after the fold and the profile is no
longer wedged behind an uncommittable pending deletion.

The response-adjusted commit invariant becomes:

```text
freshly loaded committed state after finalization
    == effective state immediately before commit
       adjusted by accepted provider responses and successful transaction absence
```

Vacuous structural operations remain honest to the reviewed prefix and fold locally. Provider
completion counts include only actual update and delete items.

## Refresh Rebase

Refresh validates and rewrites pending deletion targets inside the same authoritative transaction
as the provider fold:

- a target still present in the refreshed committed base remains pending and effectively absent;
- a target absent from the refreshed base is already satisfied and is removed from the operation;
- a partially surviving bulk operation retains its operation ID, order, and surviving targets;
- an empty deletion operation is removed; and
- the cursor decrements for every removed active operation, using the existing count-based cursor
  rule.

The rewrite increments profile revision and participates in the same refresh-generation and
no-active-write-batch guards as every provider rebase. It records only counts in durable refresh
summary and logs. In-session renderers may announce which visible pending edits were satisfied,
subject to the established privacy boundary.

Refresh never reintroduces a transaction into effective state merely because its remote deletion
has not finalized. A target retained by rebase remains hidden by journal replay. A transaction may
reappear only after the deletion operation is committed or abandoned and a later authoritative
provider snapshot contains it.

## HTTP Contract

### Duplicate projection

The API adds a versioned profile-scoped duplicate projection endpoint:

```text
POST /api/v1/profiles/{profile_id}/duplicates
```

The request carries the canonical analytical query, expected revision, and a bounded group/row
window. The endpoint is read-only but uses POST because the complete view state is bounded request
data rather than an oversized query string. It requires no mutation token and performs no profile
write.

The response contains schema version, profile revision, canonical query, total group and row
counts, a bounded ordered group window, exact money strings, ordinary presentation flags, and
opaque row targets. It never contains external provider IDs, provider labels separate from the
displayed matching label, operation payloads, or an unbounded target list.

Revision mismatch returns the existing stale projection response and requires reprojection. Limits,
offsets, request size, labels, and view state use the existing strict validation and problem
envelopes.

### Deletion mutation

Deletion uses the existing protected mutation endpoint and action registry:

```text
POST /api/v1/profiles/{profile_id}/mutations
action = transaction.delete
```

The request carries expected revision, analytical query, opaque selection, optional focused row
target, and bounded response window. The custom mutation token, Origin, Fetch Metadata, no-cookie,
no-CORS, base-path, and profile-scope rules apply unchanged.

The response is the normal mutation response with a new revision, reprojected analytical window,
pending summary, selection disposition, and provider status where applicable. It contains no raw
deleted target list.

## Capability Registry

`ActionDeleteTransaction` becomes implemented for local and writable Monarch profiles. It remains
visible but unavailable with an explicit reason when:

- the current projection does not focus transaction rows;
- the profile is in a provider write batch phase that disables editing;
- the profile store is read-only or incompatible; or
- a future provider lacks deletion capability.

`ActionFindDuplicates` becomes implemented for every open profile with transaction data. It is an
analytical action and remains available offline. Provider connectivity does not affect duplicate
scanning.

The action registry remains the single source for TUI keys, help text, web keyboard dispatch, and
capability projection.

## Privacy and Logging

Persistent logs retain the existing allowlist: error codes, revisions, counts, timings, bounded
attempt numbers, phases, and correlation IDs. They never contain transaction IDs, provider IDs,
merchant or account labels, amounts, dates, search text, duplicate grouping keys, or deletion
targets.

Durable write state necessarily stores local and external transaction identities inside the
owner-only SQLite profile. It stores no raw GraphQL payload or provider error message. HTTP problem
responses remain credential-blind and target-blind.

Duplicate review is financial data and inherits the normal no-store browser headers. Browser
selection is memory-only and never enters URLs, referrers, or general request logs.

## Performance Contracts

Duplicate detection is linear in the number of effective filtered transactions plus deterministic
sorting of the resulting groups. The reference implementation uses ordinary maps and slices and
does not introduce a dataframe or query engine.

Named performance gates use the committed 100,000-transaction synthetic corpus:

- complete duplicate scan: 100 ms reference target and 500 ms shared-CI ceiling;
- replay of a 100,000-target deletion operation: within the existing effective-snapshot budget;
- planning a large mixed update/delete prefix: within the existing provider-write planning budget;
- bounded duplicate API projection: within the existing 50 ms reference and 100 ms shared-CI API
  projection family after the scan result is available; and
- write-lock hold time remains within the existing provider finalization ceiling.

Performance checks validate output counts and a deterministic digest so a fast empty or incomplete
result cannot pass.

## Testing Strategy

### Pure analytics tests

- Empty input and no-duplicate input.
- Exact group formation and deterministic ordering.
- Date differs by one day: separate groups.
- Minor units, currency, or scale differ: separate groups.
- Account identity differs: separate groups.
- Unicode lowercase matches while whitespace, NFKC, and stronger case-fold differences do not.
- Provider-backed merchants with the same raw provider label but different suffixed display labels
  form one group when date, money, and account match.
- A local user label is used when no provider allocation exists.
- Three or more duplicates form one group; singleton groups are omitted.
- Input order randomization does not change the canonical result digest.

### Domain, replay, and store tests

- Operation validation requires nonempty, ordered, unique transaction targets.
- Append resolves selection before focused row and rejects aggregate targets.
- Replay removes exact targets; undo restores; redo removes again.
- Restart produces the same effective absence and cursor.
- Later operations cannot target an effectively deleted transaction.
- Bulk deletion is one operation and one undo unit.
- Multi-select clears after success; single-row targeting does not create selection.
- Empty-drill versus invalid-drill behavior survives deletion, commit, and restart.
- Local fold equivalence includes deleted rows and retained dimension identities.
- Fold failure leaves every committed and journal table unchanged.
- Journal operation and target ceilings reject append atomically while browsing, undo, review, and
  refresh remain available.
- Schema inspection proves the v7 item-union checks and rejects a v6 profile without migration.

### Refresh tests

- Present remote target remains pending and effectively absent.
- Absent remote target is removed as already satisfied.
- Partial bulk survival preserves operation identity and order.
- Empty operation removal adjusts the count-based cursor correctly.
- Redo-tail behavior follows the existing refresh rewrite contract.
- Refresh revision and generation guards reject stale folds.
- A pending deletion never flashes back into effective state during refresh.
- A provider-resurrected row returns only after pending intent has been committed or abandoned.
- A resurrected row with the same provider external ID restores the original stable local ID.
- A resurrected logical transaction with a new provider external ID receives a fresh local ID.

### Write planner and storage tests

- One committed/effective absence produces one delete item.
- Update followed by delete produces one delete item with all originating operation IDs.
- Delete items never enter new-name leader groups and can run at ordinary concurrency.
- Mixed update and delete items have deterministic positions.
- Schema checks reject delete rows with update fields or expectations.
- Schema checks reject update rows without requested fields.
- Preparation treats same-prefix emptied merge and label operations as vacuous rather than
  unsupported.
- Review marks vacuous operations as affecting zero transactions.
- Completion item counts exclude vacuous operations.
- The next complete refresh restores the unchanged provider label after a vacuous label fold.
- Response-adjusted finalization equivalence includes successful transaction absence.

### Monarch adapter and worker tests

- GraphQL request shape contains only the transaction external ID.
- `deleted: true` normalizes to success.
- A characterized not-found response normalizes to already-satisfied success.
- HTTP not-found normalizes to already-satisfied success.
- Unclassifiable `deleted: false` payload rejection becomes reconcile-only attention without
  exposing message text.
- Authentication, rate limit, unavailable, malformed, and unknown-outcome classifications.
- Crash after remote acceptance but before result persistence safely resends and completes on
  success or proven not-found.
- Crash-uncertain update items park without resend while crash-uncertain delete items remain
  eligible within their bounded attempt budget.
- Retry attempt counts, lease loss, pause, reconnect, and cross-process hand-off.
- Stop and reconcile removes the complete frozen prefix and installs remote truth.
- A remotely absent applied deletion remains absent after reconcile.
- A remotely present refused or unsent deletion is abandoned and remains present after reconcile.
- No update or delete runs after a lost operation lease or household identity mismatch.

### Application, TUI, web, and API tests

- `D` scans the complete effective filtered result, not the visible window.
- No-result and empty-input announcements.
- TUI keys `Space`, `i`, `h`, `x`, `Esc`, navigation, and paging.
- Web keyboard and accessible-button equivalence.
- `x` confirmation cancellation and acceptance for focused and selected targets.
- Accepted deletion immediately updates tables, aggregates, bars, totals, flags, and duplicate
  groups.
- Groups collapse below two effective rows.
- Revision conflict never replays a stale confirmation.
- `u` and `U` restore and reapply deletion after closing duplicate review.
- Review summary, bounded targets, vacuous annotation, and `w`, then `Enter` commit.
- Duplicate projection exact-money encoding, bounds, revision, no-store, base path, profile scope,
  and absence of provider identities.
- Deletion mutation token, Origin, Fetch Metadata, request-size, and safe-problem behavior.
- TUI and web may observe each other's accepted deletion through ordinary revision checks.

### Parity characterization

Committed characterization fixtures lock Python's actual exact-date, lowercased-merchant,
same-account grouping. Semantic scenarios cover duplicate review navigation, details, hide,
deletion confirmation, cancellation, and group collapse.

The Go artifacts deliberately differ where deletion is staged rather than immediate. Any Python or
Go parity artifact update uses the explicit parity-update targets and receives a full reviewed diff.
Generated screenshots remain ignored build output and are never committed.

### Opt-in live Monarch characterization

Live tests are excluded from ordinary verification. They require explicit environment-provided IDs
for transactions the tester has designated as disposable. They never select an arbitrary imported
transaction.

The live checks record:

- the exact `deleted: true` response shape;
- the response shape for deleting an already absent external ID;
- whether a user-created disposable transaction can be deleted;
- whether a deliberately supplied bank-synced disposable transaction is refused or later
  resurrected; and
- complete refresh behavior after each outcome.

The test reports only allowlisted classifications and counts. It does not commit response payloads,
transaction identities, merchant labels, account labels, or screenshots.

## Implementation Checkpoints

The implementation plan should decompose this design into independently green checkpoints:

1. Pure duplicate analytics and domain deletion operation.
2. Replay, SQLite journal/fold/rebase support, schema v7, and store properties.
3. Application mutation, duplicate projection, review, and capability behavior.
4. Monarch delete adapter and live-characterization harness.
5. Durable union write planning, execution, finalization, and recovery.
6. TUI duplicate review and deletion confirmation.
7. HTTP contract and web duplicate review/deletion workflow.
8. Cross-renderer integration, performance, parity, privacy, and full verification.

Each checkpoint starts with failing tests, implements only its bounded surface, runs focused and
repository-required verification, reviews its diff, and commits the verified change before moving
on. Live dogfooding occurs only after the automated change is committed; findings land as new
commits.

## Completion Criteria

This slice is complete when:

- `D` and `x` are implemented in the shared action registry and no longer appear unavailable;
- duplicate matching follows the exact approved key, including raw provider labels behind local
  suffixes;
- TUI and web expose the same keyboard-driven duplicate workflow;
- deletion is durable, reviewable, undoable, redoable, and restart-safe, while its provider mutation
  remains deferred until commit;
- local commit and Monarch write-back satisfy their respective equivalence invariants;
- update-plus-delete produces one provider delete item;
- stop and reconcile abandons the entire frozen prefix and never wedges a refused deletion;
- not-found behavior is safe without parsing or persisting provider error text;
- refresh rebase handles present, absent, partial, empty, and resurrected targets correctly;
- deleted transaction mappings survive as tombstones so same-ID resurrection is stable;
- schema v7 enforces the item union and refuses v6 without migration;
- API responses are bounded, exact-money, no-store, and provider-identity blind;
- all pure, store, provider, application, TUI, web, API, performance, race, architecture, privacy,
  and parity gates pass;
- opt-in live characterization touches only explicitly supplied disposable transactions;
- the diff contains no export, fuzzy matching, provider migration, unrelated provider, MCP, Python
  shim, generated screenshot, or embedded `internal/web/dist` artifact; and
- every automated-verified change is committed on `go-port` without pushing.
