# Go Port SQLite Profiles and Editing Design

**Date:** 2026-08-14

**Status:** Awaiting assembled-spec review

**Branch:** `go-port`

## Purpose

moneyflow is becoming a full Go replacement with a shared application core, a
keyboard-driven Bubble Tea terminal user interface (TUI), and a first-class Svelte web
interface. The completed Go foundation and read-only web slices use a committed synthetic
fixture and hold no durable user state.

This design covers the next independently verifiable slice: one local SQLite profile and a
complete staged-editing workflow through both renderers. Transactions, accounts, merchants,
categories, category groups, pending operations, undo/redo, review, and local commit become
durable. Accounting and analytics remain ordinary Go functions over typed slices.

This is a deliberately narrow vertical slice. It proves the persistent entity model, editing
semantics, concurrent TUI/web access, and keyboard workflows before provider synchronization,
Python migration, export, or multi-profile management are introduced.

## Relationship to Earlier Slices

This slice builds on the approved designs:

- `2026-08-12-go-port-foundation-read-only-tui-design.md`
- `2026-08-13-go-port-read-only-web-design.md`

Those documents remain authoritative for exact integer money, analytical behavior, durable URL
state, windowed web projections, base-path support, the action registry, synthetic parity data,
cross-platform builds, and the long-lived `go-port` branch.

The Python implementation remains the behavioral oracle. Existing behavior wins unless this
design names a deliberate correction or divergence. Python and Go remain on the same branch, and
this slice does not authorize removing Python, merging `go-port`, or connecting to live provider
data.

## Goals

- Make SQLite the sole durable source of truth for the Go application.
- Preserve the TUI's focused-row and multi-select editing workflows in both renderers.
- Persist pending operations and profile-global undo/redo across process restarts.
- Support the TUI and web server using one profile concurrently.
- Make merchants, categories, and category groups first-class stable entities before providers
  arrive.
- Keep accounting and analytics independent of SQL, journal, HTTP, and renderer types.
- Make every mutation atomic, revision-checked, deterministic, and exactly reviewable.
- Preserve the no-CGO Linux, macOS, and Windows portability contract.
- Continue supporting loopback, a concrete tailnet address, and reverse-proxy paths such as
  `https://moneyflow.example/moneyflow`.
- Demonstrate interactive behavior with 100,000 transactions and large bulk edits.

## Non-Goals

- Multiple named profiles or profile-management user interfaces.
- SimpleFIN, Monarch Money, YNAB, Amazon, or other provider adapters.
- Provider credentials, synchronization queues, refresh policies, or conflict resolution.
- Python cache/profile migration.
- Export, backup, repair, or disaster-recovery tooling.
- Built-in authentication, authorization, user accounts, or CORS.
- Application-level database encryption or SQLCipher.
- Transaction deletion, splitting, or notes editing.
- Live browser push, WebSockets, or a background daemon.
- Moving accounting, analytics, or bulk target resolution into SQL or TypeScript.

## Core Decisions

1. SQLite is the only Go persistence format. There is no Parquet cache or shadow store.
2. The pure-Go `modernc.org/sqlite` driver preserves the no-CGO contract.
3. Committed entity tables plus a durable ordered operation journal represent profile state.
4. Effective state is committed state with the active journal prefix replayed in order.
5. Pending operations, the undo/redo cursor, and review state are profile-global.
6. The TUI and web server may open the same profile concurrently.
7. A monotonically increasing profile revision supplies semantic optimistic concurrency; SQLite
   locking supplies physical serialization.
8. All mutations resolve concrete stable local IDs at creation time and never replay predicates.
9. Commit atomically folds the active journal prefix into committed state and clears history.
10. Merchants, accounts, categories, and category groups use stable local identities distinct
    from external provider identities.
11. TUI and web use the same application operations and capabilities without routing the TUI
    through HTTP.
12. The web remains no-auth and cookie-free; canonical-origin checks and a signed mutation token
    protect persistent actions.

## Named Parity Decisions and Divergences

Parity reviewers need one consolidated list rather than scattered exceptions.

1. **Redo is new.** Python supports `u` undo only. Go adds `U` redo in both TUI and web. The key is
   unused by the Python and current Go key contracts.
2. **All taxonomy operations are staged.** Python immediately persists independent category/group
   changes but defers changes coupled to transaction reassignment. Go journals every structural
   operation uniformly until `w` commit.
3. **Category-manager help is corrected.** Python behavior already supports `C` create, rename,
   move, merge, and delete, and `G` create, rename, merge, and delete. Its top-level help omits
   some of those operations. The Python and Go help artifacts are deliberately updated to describe
   the behavior accurately.
4. **Pending operations are durable at quit.** Python's confirmation warns about unsaved changes.
   Go retains a plain `q` confirmation but says pending operations are safely persisted and remain
   available next launch. `Ctrl+C` stays an immediate force-quit.
5. **Double-hide cancellation remains Python-compatible.** Pressing `h` again when every resolved
   target already has a pending hide effect cancels those effects. Go does not append a
   compensating unhide operation.
6. **Stable label renames preserve drills.** Python string-keyed merchant/category drills may empty
   after a rename. Go label-only renames preserve stable local IDs, keep the drill populated, and
   update its breadcrumb. Merges and deletes retire identities and therefore produce a valid empty
   drill instead.

Every other in-scope editing behavior remains subject to the Python characterization oracle.

## Architecture

```text
Bubble Tea TUI ─────────────────────┐
                                   │ direct application calls
Svelte web ── Huma HTTP adapter ───┤
                                   v
                         Application service
                         /                 \
               replay and capabilities   pure analytics
                         \                 /
                          effective snapshot
                                   │
                          persistence contract
                                   │
                         pure-Go SQLite store
                  committed entities + operation journal
```

The application service remains the only owner of editing semantics, target resolution,
capabilities, journal replay, and commit behavior. Renderers collect input and display returned
state. The store owns SQL and atomic persistence. Analytics consumes ordinary domain slices and
does not know how they were loaded or edited.

No request holds a database read transaction while rendering, waiting for input, or serving a
windowed projection. Each process holds an immutable effective snapshot outside SQLite and checks
the profile revision before using it.

## Package and Source Layout

```text
cmd/moneyflow/          command wiring, profile opening, demo seeding, and server lifecycle
internal/home/          canonical v2 paths and filesystem protection
internal/store/         application-owned persistence contract
internal/store/sqlite/  driver setup, schema, migrations, journal, and snapshots
internal/domain/        persistent entities, money, and typed operations
internal/app/           replay, target resolution, capabilities, review, and commit
internal/api/           Huma mutation types, security, and error mapping
internal/tui/           Bubble Tea editing overlays and review presentation
internal/fixture/       committed synthetic seed source
web/                    kit-ui editing, review, and conflict presentation
```

SQL rows, statements, transaction handles, and driver error types never escape
`internal/store`. The store returns domain records and renderer-neutral error values. The API never
imports terminal types, and frontend code never reimplements journal or analytics behavior.

## Profile Location and Filesystem Protection

`$MONEYFLOW_HOME` identifies the Go data root. When unset, it defaults to
`~/.moneyflow/v2`. The single profile database is:

```text
<moneyflow-home>/moneyflow.db
```

The `v2` directory prevents the Go port from reading or modifying Python state before the later
migration slice. A future migration can explicitly inspect the sibling v1 layout.

Path handling follows the hardened Docbank conventions:

- resolve an absolute canonical root without following a missing suffix through symlink tricks
- create and re-enforce `0700` directories and `0600` database files on Unix
- apply an owner-restricted DACL on Windows
- pre-create the private database file so SQLite WAL and SHM siblings inherit its protection
- reject a database path that resolves outside the selected root

Application-level database encryption is not a requirement. Transaction data uses ordinary
SQLite protected by filesystem permissions and the host's full-disk encryption where available.
Future credentials use a separate credential design and never enter this database implicitly.

## Profile Creation and Demo Lifecycle

A missing normal profile creates an empty current schema. Normal `moneyflow` and `moneyflow web`
open the same default profile.

`moneyflow demo` and `moneyflow web --demo` create a fresh uniquely named directory beneath the OS
temporary directory for each run, seed it from the committed synthetic fixture, and remove it on
clean shutdown. Demo edits never survive a new demo invocation and never touch the default profile.
Abnormal leftovers contain synthetic data only and may be removed by normal temporary-directory
cleanup.

`cmd/moneyflow` decodes `internal/fixture` data and invokes the store's dedicated atomic
`CreateSeededProfile` operation. That operation is allowed only when the schema is current, the
profile is empty, and its revision is zero. It writes committed rows directly, establishes
revision one, and refuses to overwrite or append to a populated profile. Normal application code
cannot invoke the seeding path.

Mutable tests use explicit restrictive temporary roots, including tests that close and reopen the
same path. Tests and demos are prohibited from opening the default user profile.

## SQLite Configuration and Durability

The store uses `database/sql` with `modernc.org/sqlite`. Connection-local settings are encoded in
the driver data source name so every pooled connection receives them. Store open then verifies the
effective settings.

Required settings include:

- foreign keys enabled
- WAL journal mode
- `synchronous=FULL`
- a bounded busy timeout
- a bounded connection pool appropriate for one SQLite writer and concurrent readers

`FULL` is intentional: financial edits favor surviving power loss over the modest write-latency
benefit of `NORMAL`. Correctness does not depend on checkpointing. Clean close attempts a
truncating checkpoint after application work has stopped, but checkpoint failure cannot undo a
successful mutation.

Startup applies embedded forward-only migrations atomically. Concurrent startup lets one process
migrate while the other waits through the busy timeout and then rechecks the schema. A binary
refuses startup when the schema is newer, migration fails, or integrity checks report corruption.
There is no degraded read-only mode that might render unreliable financial results.

## Persistent Schema

The schema uses SQLite `STRICT` tables and explicit constraints. The logical tables are:

- schema migration history
- singleton profile state containing schema-independent profile revision and journal cursor
- committed accounts
- committed merchants
- committed category groups
- committed categories
- committed transactions
- external identities mapping stable local IDs to provider namespaces and IDs
- known analytical drill identities, including their currency and scale partitions
- journal operation headers
- versioned operation payloads
- explicit operation targets

All application entities use stable opaque local IDs. Provider identifiers never serve as local
primary keys. IDs are never reused. Retired merchants, categories, and groups remain as tombstones
with merge destinations where applicable.

Transactions reference stable account, merchant, and category IDs. Names and group membership live
on their entity records rather than being copied as mutable transaction identity. Loading a domain
transaction joins those records into the existing immutable value shape consumed by analytics.

External identity rows may represent the committed synthetic source in this slice, but no provider
adapter or synchronization state is added. Their purpose is to preserve the already-approved
separation between local and provider identity.

### Exact money

Every stored amount uses a signed integer minor-unit column plus required currency and scale. Money
tables contain no `REAL` columns. Constraints reject non-integer storage classes, invalid currency
codes, and unsupported scales. Schema-inspection tests enforce the rule in addition to application
validation.

### Entity labels and collisions

Entity display labels are trimmed, nonempty, and free of control characters. Go computes a
deterministic collision key by applying Unicode NFKC normalization, Unicode case folding, trimming
Unicode whitespace, and collapsing internal whitespace runs to one ASCII space. Punctuation is
preserved. The display text retains the user's non-control characters. Active entities of the same
type have unique collision keys; retired entities do not reserve a label. Go application validation
and stored unique keys enforce this policy without platform-dependent SQLite collations.

Merchants are first-class entities because merchant normalization is a central workflow. Accounts
are first-class but read-only in this slice. Categories and category groups are first-class and
editable.

A protected Uncategorized category and protected Uncategorized group preserve the existing domain
requirement that every transaction has a category and every category has a group. “Unassign” means
reassignment to those stable sentinels, never a `NULL` category/group reference. The sentinels
cannot be renamed, merged, retired, or deleted. Category creation through `c` may select a group or
explicitly accept Uncategorized.

### Known drill identities

The complete analytical drill identity remains `(dimension, currency, scale, key)`. A compact
registry records identities observed in committed or effective profile history. When a seed or
accepted operation first exposes an identity, the store inserts the registry row in the same
transaction; registry rows are never pruned. Retired entity records provide the corresponding
identity history for taxonomy and merchant merges.

After edits or commit:

- a stable entity whose label changed keeps the same key and remains populated
- an identity that existed but now has no effective rows is valid and renders the normal empty
  projection without changing the URL
- a syntactically valid identity with no profile history renders the invalid-view screen with Back
  and Reset

This distinction survives process restart and web bootstrap. Typed time-period validity remains
governed by the existing URL contract because this slice does not edit dates.
For historically known identities, this rule supersedes the read-only web slice's broader statement
that every stale bookmarked entity key opens the invalid-view screen.

## Profile Revision and Concurrent Processes

The profile revision is a monotonically increasing integer. Every journal append, undo, redo, hide
cancellation, taxonomy rewrite, and commit increments it exactly once in the same transaction as
the mutation.

The TUI and web server may open the same profile simultaneously. Before an interaction or request,
the service reads the revision row and discards a cached effective snapshot when it differs. The
web rechecks on ordinary reads, mutations, window focus, and visibility restoration. No live push
channel is added.

An application-level revision check against a cached snapshot is only a fast-fail courtesy. The
authoritative compare-and-increment occurs inside the SQLite write transaction. If the expected
revision differs there, the transaction returns `revision_conflict` without changing state. A
successful compare also proves that targets resolved from the associated snapshot were current.

SQLite WAL and locking serialize physical writes; the revision CAS prevents semantically stale
writes. Neither mechanism substitutes for the other.

## Operation Journal

The journal is an ordered sequence of typed pending operations. Each record has:

- a stable operation ID and sequence
- an operation type
- a payload-version discriminator
- a canonical forward payload containing every value needed for replay
- explicit stable local target IDs
- creation revision and non-sensitive timing metadata

Forward-only SQL migrations are not sufficient for payload evolution. Every supported pending
payload version must retain a compatible decoder or be transactionally rewritten by a migration.
An unknown operation type or payload version refuses mutable startup.

Operations resolve targets once at creation. Replay never re-evaluates a predicate such as “all
transactions currently named X.” Bulk operations store the exact transaction IDs resolved at the
submitted revision. Merge and delete payloads contain source, destination or explicit
unassignment, and every structural value required for deterministic replay.

### Cursor and redo

The journal cursor identifies the active prefix:

- operations at or before the cursor are active
- operations after the cursor form the inactive redo tail
- `u` moves the cursor backward by one operation
- `U` moves it forward by one operation
- appending while behind the head permanently truncates the inactive tail first

Commit while behind the head folds only the active prefix and permanently discards the inactive
tail. Review shows inactive redo operations separately and warns that commit discards them.

Journal entries are typed and normally immutable, but the pending journal is not append-only. Two
rewrites are permitted:

1. redo-tail truncation
2. Python-compatible hide cancellation

Both require the caller's expected revision and the authoritative transactional CAS.

### Hide cancellation

If every transaction in the resolved target set already has an active pending hide effect, another
`h` cancels those effects rather than appending a compensating operation. The transaction first
truncates any redo tail, removes the selected target effects from their originating operations,
and removes operations left with no targets. Partial batch cancellation replaces the original
operation at the same sequence with the same payload version and only its remaining targets.

The cursor decrements past each removed active operation so the next `u` reaches the preceding
visible operation. Cancellation increments the profile revision but creates no new undo unit.

If not every resolved target has a pending hide effect, `h` appends one ordinary hide-toggle
operation for the exact target set, matching Python's all-or-cancel decision.

## Effective State and Replay

Full replay is the reference implementation:

1. Load committed entities and transactions.
2. Validate and decode every operation in sequence.
3. Apply the active prefix mechanically to a fresh domain snapshot.
4. Validate referential, identity, and money invariants.
5. Hand immutable slices to the existing analytics and projection layers.

An implementation may incrementally apply a newly accepted operation to its cached effective
snapshot. Incremental application is only an optimization and must be provably identical to full
replay in domain values and deterministic ordering after every operation sequence.

Source slices and maps remain defensively copied. Neither replay nor analytics mutates the
committed snapshot returned by the store.

## Editing Operations

Persistent application operations are distinct from renderer dialogs and HTTP wire types.

### Target precedence

An explicit nonempty selection always wins. Otherwise the focused detail row or focused aggregate
row supplies the edit context. The operation records concrete transaction or entity IDs, not the
selection marker, focused index, query, or aggregate predicate.

After a successful operation originating from an explicit multi-selection, both renderers clear
that selection. Focus-targeted operations leave selection unchanged. Opaque web selection values
are therefore dropped after successful selected-target mutations.

### Merchant editing

Merchant editing distinguishes entity intent from transaction reassignment:

- Whole-merchant rename to a fresh normalized label updates the merchant entity label, preserves
  its stable ID, and keeps a drill on that merchant populated with an updated breadcrumb.
- Whole-merchant rename to an existing normalized label is an explicit merchant merge. Effective
  transactions move to the existing destination, the source ID retires, and a source drill becomes
  a valid empty view. The renderer labels the consequence as a merge and requires confirmation;
  the service records a merchant-merge operation rather than a label update.
- A focused transaction or explicit selection rename reassigns only those concrete transactions to
  an existing merchant or a newly created merchant. It never changes the source entity label. The
  source drill remains valid and may shrink or become empty.

The request carries an explicit entity-versus-transaction scope; the service does not infer intent
from whether a selected target set happens to contain every current transaction for a merchant.
The journal uses separate label-update, merge, and transaction-reassignment operation types.

### Category assignment and taxonomy

`c` assigns an existing category or creates and assigns a new category on the fly. A selection or
focused context resolves exact transaction IDs.

`C` manages categories through create, label rename, group move, merge, and delete. `G` manages
groups through create, label rename, merge, and delete.

Taxonomy label renames preserve stable IDs. A category or group rename whose normalized label
collides with an existing entity is rejected and directs the user to the explicit merge command.
Taxonomy is never silently merged by rename.

Category merge reassigns every effective transaction from source to destination and retires the
source. Deleting an assigned category requires an explicit replacement or reassignment to the
protected Uncategorized category. Deleting a nonempty group requires moving its categories to a
destination group or the protected Uncategorized group. Group merge moves its categories and
retires the source. Retired IDs are never reused.

All taxonomy operations are pending, reviewable, undoable, and redoable until commit.

### Hide, undo, redo, and review

`h` toggles hide state or performs the cancellation rule above. `u` and `U` move the profile-global
journal cursor. `w` opens review over a specific profile revision.

Review groups changes by operation rather than presenting thousands of unrelated row edits. It
shows operation order, active/inactive state, affected counts, before/after values, taxonomy
effects, and bounded expandable transaction detail.

Review does not freeze the profile. Confirming commit carries the reviewed revision. A stale
confirmation refreshes state and requires the user to review and invoke commit again.

## Commit Semantics

Local commit is one SQLite transaction:

1. Compare the reviewed expected revision authoritatively.
2. Replay and validate the active prefix.
3. Fold active transaction and entity changes into committed tables.
4. Retire merged/deleted entities and update known drill identities.
5. Delete the active journal and inactive redo tail.
6. Reset the cursor and increment the profile revision once.

A successful fold must satisfy the central equivalence invariant: a freshly loaded committed
snapshot after commit is exactly equal to the effective snapshot immediately before commit,
including taxonomy membership, retired identities, ordering, flags, and money.

Commit failure changes none of the committed rows, journal, cursor, or revision. This slice has no
provider side effect; later provider designs must attach synchronization to this application
boundary without weakening local atomicity.

## Application Mutation Flow

A renderer mutation follows this sequence:

1. Submit the action, validated input, durable analytical state, selection, and expected revision.
2. Fast-fail against the loaded snapshot, validate capability, and resolve exact stable targets.
3. Execute the authoritative revision CAS and journal mutation in one store transaction.
4. Fully replay or equivalently increment the effective snapshot.
5. Return canonical analytical state, effective projection, pending summary, selection
   disposition, capabilities, and new revision.

The store owns step 3. The service owns the other steps. SQL cannot leak into the service's domain
contracts, and renderers cannot construct journal payloads directly.

## Capabilities

Action IDs, keys, categories, and help text remain static in the shared action registry.
Availability derives from provider-neutral profile capabilities returned by the application
service.

This slice enables local merchant, category, group, hide, undo, redo, review, and commit
capabilities. Future providers can disable locally managed taxonomy without changing action IDs,
key handling, API types, or renderer code.

Unavailable actions remain visible only where the existing capability/help contract calls for
them and explain why they cannot run. Renderer-local lifecycle actions remain outside web
capabilities.

## TUI Editing Experience

The Bubble Tea TUI reproduces the Python workflows while using application operations:

- `m` opens merchant input and scope selection.
- `c` opens the searchable category selector with create-on-the-fly.
- `C` opens the searchable category manager with create, rename, move, merge, and delete.
- `G` opens the searchable group manager with create, rename, merge, and delete.
- `h`, `u`, and `U` act without opening overlays.
- `w` opens review.

Internal modal keys follow the Python screens. Inputs and overlays own their keystrokes; global
actions never leak through. Cancel, conflict, and successful return preserve analytical state,
focused identity, cursor, and scroll where those identities still exist.

Pending flags appear on affected rows, and the shell reports active-operation and
affected-transaction counts. Effective tables and charts update immediately after an accepted
operation.

`q` opens a confirmation. If pending operations exist, the message says they are durable and will
be restored next launch. Quitting during an open, unsubmitted dialog discards only renderer-local
input. `Ctrl+C` remains immediate.

## Web Editing Experience

The web uses kit-ui dialogs and drawers but preserves the same primary key paths. Every operation
is possible without a mouse. Dialogs trap and restore focus, inputs own printable keys, and live
regions announce validation, pending counts, conflicts, selection clearing, and commit results.

Pending profile state never enters the analytical URL. Editing, undo, redo, and commit do not push
browser history or change the canonical view URL. Tables and charts consume the server-returned
effective projection.

Review receives operation summaries first and loads transaction details in bounded windows. The
browser never receives the complete dataset or every identity in an all-result selection.

There is no live push. The browser checks revision on ordinary reads, mutation attempts, focus, and
visibility restoration. A stale mutation is never automatically replayed. The browser refreshes
while preserving cursor and selection identities where all remain valid, explains the conflict,
and requires explicit reinvocation.

Drill behavior follows stable identity:

- merchant/category/group label updates preserve the URL key, rows, and updated breadcrumb
- retired or historically known keys with no effective rows render the normal empty projection
- never-observed keys render the invalid-view screen

Parity scenarios cover pure label rename, merchant merge, subset merchant reassignment, category
merge, and category deletion while drilled into the affected entity. Bookmark reload and process
restart must preserve the same classification.

## HTTP Mutation API

The existing Huma read API and durable URL codec remain. This slice adds bounded endpoints for:

- no-store browser bootstrap and mutation-token refresh
- persistent action mutation
- journal/review summaries
- windowed review targets

Persistent actions use one versioned mutation envelope containing:

- action ID and action-specific typed input
- expected profile revision
- canonical analytical state
- opaque selection state where applicable

The server constructs journal operations after validation; clients never submit raw journal
payloads or operation sequence numbers. Mutation responses include the new revision, canonical
view, effective projection, pending summary, selection disposition, and stable error code.

Money remains exact decimal strings and integer-minor-unit strings on the wire. The API does not
send SQLite rows, provider metadata, complete datasets, or private operation payloads.

## Web Origin and Mutation Security

The server sets no authentication cookies and supports no CORS. Tailnet policy, TLS, and optional
proxy authentication remain external responsibilities.

The web command supports the existing listener and base path plus:

```text
moneyflow web \
  --listen 127.0.0.1:8080 \
  --base-path /moneyflow \
  --external-url https://moneyflow.example/moneyflow
```

When supplied, the normalized external URL path must equal the normalized base path exactly.
Mismatch, user information, query, fragment, unsupported scheme, or invalid origin is a startup
error. Without an external URL, direct loopback use derives the canonical origin automatically.

The external URL establishes one mutable origin. Direct listener access while it is configured may
read the application but displays a noncanonical-origin warning and rejects mutations with a link
to the canonical URL. The server does not maintain an origin allowlist.

Each genuine HTML bootstrap is `no-store` and obtains a one-hour signed mutation token bound to:

- the server-instance secret
- canonical origin
- normalized base path
- expiry

The token is held in browser memory, never placed in a URL or history, and sent through a custom
header. Mutation requests also require a matching canonical `Origin`, same-origin Fetch Metadata,
and the expected profile revision.

Before expiry, and after `token_expired`, the frontend calls a same-origin no-store bootstrap
endpoint for a replacement token. It retries the unchanged request once because token rejection
occurs before operation evaluation. A server restart invalidates old tokens and follows the same
refresh path without losing cursor, scroll, or selection.

Automatic retry applies only to token expiry. Revision conflicts, store contention, selection
failure, and storage errors require explicit action.

## Selection Semantics

The existing opaque exact-selection format remains bounded and server-authoritative. A mutation
resolves it against its defining analytical state and expected profile revision.

If selection state is stale, Go attempts exact all-or-nothing revalidation against the current
revision. When every identity resolves, it returns the refreshed selection without applying the
mutation. When any identity cannot resolve, it clears the complete selection and announces that
outcome. It never applies an operation to a partial selection.

Successful selected-target mutations clear selection. Focused-row or focused-aggregate operations
leave selection unchanged.

## Failure Handling and Recovery

Every store mutation runs in one SQLite transaction and either advances the revision completely or
changes nothing. Process termination cannot leave partially applied journal entries, cursor
changes, taxonomy rewrites, or commits.

Stable renderer-neutral errors include:

- `revision_conflict`: refresh state; never retry automatically
- `token_expired`: refresh the token and retry once
- `invalid_operation`: reject invalid action or input without changing state
- `invalid_target`: reject unknown, retired-for-use, or incompatible targets
- `selection_stale`: perform deterministic revalidation/clearing without mutation
- `store_busy`: preserve state and require explicit retry after the bounded wait
- `store_error`: roll back disk-full, permission, and other runtime I/O failures
- `schema_newer`: refuse startup
- `migration_failed`: refuse startup after transactional rollback
- `store_corrupt`: refuse startup without repair or recreation

`store_error` reports the last reliably observed revision and the current revision only when it can
be read safely. `store_busy` is not automatically retried even though evaluation has not begun:
extending lock contention could apply a user action substantially later than intended.

The application never deletes, recreates, or repairs a damaged profile automatically. Backup,
restore, integrity-repair, and export tools require later designs.

## Logging and Privacy

Operational logs use an allowlist. They may contain:

- operation type
- revision numbers
- bounded row/operation counts
- timings
- generated correlation IDs
- stable non-sensitive error codes

They never contain merchant or category labels, search text, transaction or entity IDs, mutation
payloads, SQL values, complete URLs, CSRF tokens, credentials, provider payloads, or personal data.

Synthetic fixtures, tests, screenshots, documentation, and benchmarks use generic invented data.
Public-artifact privacy scanning remains mandatory before every slice commit.

## Performance Contract

The intended scale remains hundreds of thousands of transactions.

Cold opening the store, loading 100,000 committed transactions, and replaying a representative
pending journal targets 250 milliseconds on the documented reference machine. A committed CI
smoke test uses a generous one-second ceiling to catch gross regressions without treating noisy
hosts as benchmark machines.

Warm analytics retain the existing 50/100-millisecond reference targets and current CI smoke
budgets. A 100,000-target bulk append, undo, redo, hide cancellation, and commit receive recorded
benchmarks and explicit generous regression ceilings before this slice is declared complete.

Optimizations may add indexes, prepared statements, batching, and incremental snapshot updates.
They may not change the store contract, replay order, exact target semantics, or commit-fold
equivalence.

## Testing Strategy

Every invariant maps to a named test obligation.

### Schema and storage

- migrate from every committed schema version and reject newer versions
- verify `STRICT` tables, foreign keys, integer money, and absence of `REAL` money columns
- verify WAL, `synchronous=FULL`, busy timeout, pool bounds, checkpoint, and restrictive permissions
- create seeded profiles atomically and prove refuse-overwrite against a populated profile
- reject corrupt profiles and roll back failed migrations without mutation

### Journal and model properties

Deterministic randomized operation sequences compare incremental application with full replay after
every append, undo, redo, cancellation, taxonomy change, and commit. They cover:

- redo-tail truncation and commit behind the head
- partial-batch hide cancellation and cursor adjustment
- creation-time target snapshots with no predicate re-evaluation
- payload-version compatibility and migration
- label collision rules and stable identity
- merchant label update versus merge versus transaction reassignment
- taxonomy reassignment, retirement, explicit unassignment, and never-reused IDs

Every commit property test captures the effective snapshot immediately before commit, commits,
opens a fresh store handle, and compares freshly loaded committed state for exact fold equivalence.

### Concurrency and failure atomicity

- launch separate processes against one profile and prove exactly one same-revision mutation wins
- exercise focus/visibility refresh, stale review confirmation, busy timeout, and crash/reopen
- simulate bounded-database exhaustion and other portable real SQLite failures
- complete driver-error mapping with focused adapters where the OS cannot portably induce a fault
- assert every failure preserves committed rows, journal, cursor, and revision

### Python characterization and parity

Shared characterization scenarios cover:

- merchant label update, merge, and selected transaction reassignment
- category assignment and create-on-the-fly
- category/group create, rename, move, merge, and delete
- hide, `h h` cancellation, undo, review, and commit
- focused-target precedence and explicit-selection clearing
- taxonomy edits that empty the current drill

Named Go expectations cover redo, uniform structural staging, corrected help, durable-pending quit
messaging, and stable-ID label renames. Corrected Python and Go help artifacts use the deliberate
`make parity-update-python` and `make parity-update-go` flows with a reviewed full artifact diff.
Ordinary tests never rewrite parity files.

### Empty versus invalid drill identity

Tests commit a merge/delete that retires a drilled identity, close and reopen the profile, and open
the bookmark through web bootstrap. The URL remains unchanged and the result is the normal empty
projection. A syntactically valid identity with no profile history must instead produce the
invalid-view screen. Pure label rename tests prove the stable drill remains populated with its new
breadcrumb across both renderers, bookmark reload, and restart.

### HTTP and browser security

- token issue, expiry, refresh, one-time retry, and server-restart invalidation
- canonical Origin and Fetch Metadata enforcement with no CORS
- external URL/base-path equality and direct-listener read-only behavior
- versioned bounded bodies, stale revisions, and deterministic selection revalidation
- no-store bootstrap and absence of tokens or mutation data in URLs/history

### Renderer workflows

Bubble Tea model/frame tests and Playwright browser tests exercise every operation without a mouse,
overlay key ownership, focus restoration, review windows, conflicts, announcements, selection
clearing, unchanged analytical URLs, and `/moneyflow` reverse-proxy routing.

Visual artifacts remain renderer-specific. Python semantic content guides shared workflow parity;
Go TUI frames are canonical for Go styling, and Chromium screenshots are canonical for web layout.

### Portability and privacy

- Linux, macOS, and Windows builds without CGO
- Go race, vet, format, lint, parity, and generated-artifact gates
- frontend type, Svelte, format, lint, unit, browser, and dependency gates
- privacy scanning of the complete diff, commits, fixtures, screenshots, logs, and generated assets

## Completion Criteria

This slice is complete only when fresh evidence shows all of the following:

- normal TUI and web commands read one SQLite profile rather than a fixture-backed service
- demo commands seed fresh isolated SQLite profiles and refuse overwrite
- pending edits and undo/redo survive restart and are shared across TUI/web processes
- all merchant, category, group, hide, review, and local commit workflows operate in both renderers
- the authoritative revision CAS prevents stale multi-process mutation
- replay is deterministic and incremental application equals full replay
- commit-fold equivalence holds for randomized transaction and taxonomy histories
- exact minor-unit money is enforced by domain and schema tests
- stable rename, retired-empty, and never-existed-invalid drill behavior survives commit and restart
- web mutations enforce token, canonical origin, Fetch Metadata, base path, and expected revision
- no complete dataset, journal payload, or personal data reaches browser history or logs
- the 100,000-row load, analytics, bulk edit, undo/redo, and commit contracts are met
- Linux/macOS/Windows, race, lint, parity, browser, security, and privacy gates pass
- the diff contains no provider, credential, export, Python-state migration, multi-profile, or
  authentication code

Completion does not authorize pushing, merging `go-port`, removing Python, or beginning providers
without the next approved design and implementation plan.

## Later Replacement Slices

After this slice, the replacement remains ordered as follows:

1. Multi-profile management, export, and migration from Python state.
2. Provider adapters ported and verified one at a time: SimpleFIN, Monarch Money, YNAB, and Amazon.
3. Provider-aware commit/synchronization, credential storage, and offline/conflict policies as each
   adapter requires.
4. Packaging, complete parity audit, Python removal, and cutover.

Richer charts may continue incrementally but must consume server-authoritative projections and may
not move accounting, analytics, or editing semantics into TypeScript.
