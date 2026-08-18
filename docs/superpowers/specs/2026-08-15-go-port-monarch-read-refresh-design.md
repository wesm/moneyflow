# Go Port Monarch Read, Import, and Refresh Design

**Date:** 2026-08-15

**Status:** Approved

**Branch:** `go-port`

## Purpose

moneyflow is becoming a full Go replacement with one shared application core, a
keyboard-driven Bubble Tea terminal user interface (TUI), and a first-class Svelte web
interface. The completed Go slices provide exact integer accounting, local analytics, durable
SQLite profiles, profile-global staged edits, undo/redo, review, and concurrent TUI/web access.

This design covers the next independently verifiable slice: connect one pristine Go profile to
Monarch Money, import a complete posted-transaction snapshot, browse it offline, and reconcile
later provider refreshes against durable pending edits. Both renderers receive the same provider
capabilities, refresh state, progress, and failure semantics.

This is intentionally the read/import/refresh half of provider support. Provider write-back is a
separate immediate follow-on slice. Until then, staged operations are the durable representation
of user intent and cannot be committed on a Monarch-backed profile.

## Relationship to Earlier Slices and Roadmap

This slice builds on the approved designs:

- `2026-08-12-go-port-foundation-read-only-tui-design.md`
- `2026-08-13-go-port-read-only-web-design.md`
- `2026-08-14-go-port-sqlite-editing-design.md`

Those documents remain authoritative for exact money, analytics, URL state, renderer-neutral
actions, SQLite durability, journal replay, profile revision checks, no-auth web protection,
privacy, cross-platform behavior, and the long-lived `go-port` branch.

This design deliberately supersedes two roadmap details in the earlier documents:

1. Monarch Money comes before SimpleFIN because it can be exercised against the maintainer's
   normal workflow. Providers remain independently designed and implemented one at a time.
2. Provider work begins before multi-profile management, export, and Python-state import. The
   single-profile limitation is explicit in the connection lifecycle rather than hidden.

The Python implementation remains a behavioral oracle where its model still applies. This design
names every intentional provider-related divergence. It does not authorize pushing or merging the
branch, removing Python, or beginning provider write-back without its own approved design and
implementation plan.

## Goals

- Port only the Monarch GraphQL and authentication surface required to connect, validate, import,
  and refresh.
- Store the last successful provider snapshot in the existing SQLite profile and support fully
  offline browsing and editing.
- Reconcile a complete remote snapshot without reintroducing Python's Parquet hot/cold cache.
- Preserve pending user intent deterministically when the remote committed base changes.
- Keep stable local identities distinct from Monarch identifiers.
- Make provider refresh available from both TUI and web through the shared action registry.
- Coordinate concurrent TUI, web, and CLI processes without using a lease as a correctness lock.
- Keep decrypted credentials and raw provider data out of SQLite, URLs, logs, browser state, and
  committed artifacts.
- Preserve the no-CGO Linux, macOS, and Windows portability contract.
- Demonstrate full reconciliation and journal rebase with 100,000 posted transactions.

## Non-Goals

- Writing, updating, deleting, or otherwise mutating Monarch data.
- Committing local operations for a Monarch-backed profile before write-back exists.
- A provider writer interface, outbound queue, distributed commit, or conflict recovery.
- SimpleFIN, YNAB, Amazon, or another provider adapter.
- Unattended or headless credential-vault unlock; reconnect remains an interactive CLI operation.
- Multiple named profiles, in-application profile replacement, or profile-management screens.
- Export, backup, Python-state import, or database repair.
- Database schema migrations before Go v2 storage stabilizes.
- Importing pending or unposted Monarch transactions.
- Web credential entry, browser cookies, built-in authentication, or a provider setup wizard.
- Incremental, date-windowed, or cursor-based transaction synchronization.
- A permanent daemon, WebSockets, server-sent events, or other push channel.
- Provider-specific analytics, GraphQL types in storage, or SQL types in provider code.

## Core Decisions

1. Monarch is implemented first as a read/import/refresh provider. Write-back follows in a
   separate slice.
2. SQLite remains the only application source of truth. A successful provider refresh atomically
   replaces the provider-owned committed base and rebases the existing operation journal.
3. Refresh fetches a complete remote snapshot. It does not recreate Python's hot and cold cache
   tiers.
4. Authentication is CLI-only. Moneyflow stores email, password, and a time-based one-time-password
   secret in a separate password-encrypted credential vault. It never persists a generated one-time
   code or stores credentials in the session or SQLite profile.
5. A profile binds to one provider and one remote household identity. Binding never replaces a
   populated profile.
6. Monarch identifiers map one-to-one to stable opaque local IDs. External labels never silently
   merge local entities.
7. Pending edits remain durable and reviewable, but commit is unavailable until provider
   write-back exists.
8. Network fetch occurs without a SQLite transaction. Identity mapping, journal rebase, replay,
   validation, and fold are authoritative inside one store transaction.
9. A refresh-generation compare-and-swap prevents stale candidates from folding. The refresh
   lease coordinates work but supplies no correctness guarantee.
10. Provider contracts and errors are renderer-neutral. TUI and web do not import Monarch code.

## Named Parity Decisions and Divergences

The SQLite editing design's consolidated parity decisions remain in force. This slice adds these
provider-specific decisions:

1. **The proven password-protected credential model is retained and hardened.** Like Python, Go
   stores email, password, and a multifactor secret so reconnect can generate TOTP codes
   automatically. Go permits only password-encrypted storage, uses a versioned Argon2id and
   AES-256-GCM vault, and keeps that vault separate from session material and SQLite. An expired
   session requires `moneyflow provider connect monarch` and the Moneyflow account password before
   refresh can resume; unattended unlock remains out of scope.
2. **Manual refresh is new.** Python has no top-level refresh key. Go adds global `r` to both
   renderers. The category-manager overlay continues to own its local `r` rename binding.
3. **The six-hour refresh is complete.** Python refreshes its recent 90-day tier every six hours
   and its historical tier after 30 days or on first launch. Go deliberately performs complete
   reconciliation every six hours.
4. **Pending transactions are excluded.** Python displays Monarch pending transactions. Go
   validates them as part of snapshot integrity but imports only posted transactions because
   provider IDs commonly change when pending transactions post.
5. **Colliding external labels remain distinct.** Python's string-keyed merchant model can
   conflate separate Monarch merchant IDs with identical labels. Go gives every external identity
   a stable local entity and applies a deterministic visible suffix where required.
6. **Provider-backed commit is deferred.** Python can write changes to Monarch. This slice permits
   durable staging and review but disables commit until the write-back slice defines remote
   durability and conflict semantics.

Stable local label-only renames continue to preserve drills as required by the editing design.
Remote merchant normalization into an existing identity is represented as reassignment or merge,
not as two active entities sharing a collision key.

## Architecture and Dependency Direction

```text
CLI connect ───────────────────────────────┐
                                          │
Bubble Tea TUI ───────────────────────────┤ renderer-neutral actions
                                          v
Svelte web ── Huma HTTP adapter ──> internal/app
                                      /       \
                         provider contracts   store contract
                                  │                 │
                         Monarch adapter      SQLite store
                         HTTP + GraphQL       profile + journal
```

`internal/provider` defines only the neutral contracts and stable provider error codes consumed by
this slice. It imports only `internal/domain`.

`internal/provider/monarch` implements those contracts. It imports `internal/provider` and
`internal/domain`; it never imports `internal/store`. Monarch owns its HTTP and GraphQL transport,
authentication, typed wire responses, normalization, and session-file schema.

`internal/app` orchestrates the provider and store. It converts Monarch-neutral snapshot values
into domain import records, performs refresh planning, and owns all rebase behavior.

`internal/store` imports no provider package. It owns SQL, external-identity persistence, sync
metadata, leases, refresh generations, and atomic folding. SQL rows, driver values, statements,
and transaction handles never escape it.

No provider-neutral GraphQL helper is introduced. Monarch is the only planned GraphQL consumer;
its private transport remains in `internal/provider/monarch/graphql.go` until a second real use
justifies extraction.

## Package and Source Layout

```text
cmd/moneyflow/                    provider CLI and renderer lifecycle wiring
internal/provider/                neutral read-side contracts and stable error codes
internal/provider/monarch/        authentication, GraphQL, mapping, and session format
internal/app/                     refresh orchestration, planning, rebase, and capabilities
internal/domain/                  provider-neutral import records and stable entities
internal/store/                   atomic refresh and operational-state contracts
internal/store/sqlite/            schema v3, binding, lease, mappings, and fold
internal/home/                    hardened directory and atomic-file helpers
internal/api/                     refresh/status/confirmation HTTP adapters
internal/tui/                     refresh commands, progress, status, and messages
web/                              refresh action, progress, errors, and confirmation UI
```

This tree lists only directories added or materially changed by this slice. Existing analytics,
parity, replay, version, fixture, and web-asset packages remain in place.

## Provider-Neutral Contracts

Slice 1 defines only capabilities with an implementation and consumer:

- session connection and validation
- remote profile-identity probe
- complete read-only snapshot fetch

The contracts return typed provider-neutral values and stable error codes. They do not expose
GraphQL documents, response maps, authorization headers, or Monarch identifiers as domain primary
keys.

No writer interface is declared. The write-back slice will design its real contract around
durable outbound work, partial provider failure, idempotency, and commit recovery rather than
guessing at a premature `Write` method.

Provider-neutral error codes live in `internal/provider`. The Monarch adapter translates HTTP,
GraphQL, authentication, and response-validation failures at its boundary. Translation also
redacts raw provider content so neither the application nor renderer can accidentally log it.

## Monarch Surface and Transport

The Go adapter ports only operations needed by this slice:

- REST-first login with the proven GraphQL fallback
- multifactor challenge completion
- session token and device-header injection
- `GetSubscriptionDetails` for `subscription.id`
- accounts required to construct and characterize reconciliation scope
- transaction categories and category groups
- merchants
- paginated transactions
- session validation and local session deletion

Provider mutation queries, transaction update/delete methods, and the rest of the vendored Python
client are not ported in this slice.

GraphQL documents select only fields needed for normalization, identity, integrity checks, and the
existing domain projection. They do not copy unused attachment, rule, drawer, or subscription
fields merely because the Python vendored client requests them.

The client receives an injected `http.Client`, endpoint set, clock, sleeper, and randomness source.
Production endpoints are fixed HTTPS origins. Tests may supply loopback endpoints. Requests use
contexts, bounded bodies, explicit timeouts, a stable user agent, and no cross-origin forwarding of
authorization headers. Safe reads may retry only under the bounded policy in this design. Login,
MFA submission, and future writes are never generalized through that retry path.

The adapter rejects missing or null GraphQL `data` and classifies a non-success HTTP status before
reading its body. Provider errors admit only the declared fixed codes. A rate-limit error may also
carry a validated duration capped at 24 hours; no raw header or remote response text crosses the
provider boundary. Credential-bearing REST and GraphQL requests refuse every redirect.

Monarch decimal strings are parsed directly into signed integer minor units with an explicit
currency and scale supplied on the first connection and persisted immutably with both the provider
binding and session. The SQLite binding remains authoritative if the session is disconnected or
replaced. No provider, domain, store, or test-fixture path represents money with
`float32` or `float64`. Unsupported precision, currency, or numeric syntax is
`provider_data_invalid` and cannot partially import.

## Session Material and Filesystem Protection

The provider-specific session file lives under the v2 moneyflow home:

```text
<moneyflow-home>/providers/monarch/session.json
```

It may contain the session token, device UUID, bound `subscription.id`, explicit import currency
and scale, issue/validation timing, and a format version. It never contains email, password, a
time-based one-time-password secret, a one-time code, transaction data, labels, or search text.

The provider-specific encrypted credential vault lives beside it:

```text
<moneyflow-home>/providers/monarch/credentials.enc
```

It contains a versioned authenticated-encryption envelope. Argon2id derives a 256-bit key from the
user's Moneyflow account password and a random per-vault salt; AES-256-GCM encrypts and authenticates
the versioned email, Monarch password, and normalized Base32 TOTP secret payload. The account
password and generated one-time codes are never persisted. Wrong-password and tamper failures are
intentionally indistinguishable. No plaintext-storage mode exists.

The Monarch package owns the file schema. It reuses `internal/home` for owner-only directory and
file creation, validation of existing permissions and file types, bounded reads, atomic
replacement, and cross-platform behavior. Temporary and replacement files are protected before
content is written. Session content never enters SQLite implicitly.

An expired or invalid session produces `provider_reconnect_required`. The last successful SQLite
snapshot, pending journal, and offline renderer workflows remain available. TUI and web show a
reconnect-via-CLI message; they never prompt for credentials.

After an authentication failure, a long-lived process rereads the session file once. If an atomic
CLI reconnect replaced it, the process restarts the read-only provider operation once. If the file
is unchanged or authentication still fails, the process parks on reconnect-required without
further provider requests.

A parked TUI or web process checks only for session-file replacement during its existing bounded
status tick. Observing a replacement clears the parked state and resumes normal stale-data
scheduling. This makes CLI reconnect a complete recovery action without blind authentication
retries.

## CLI Connection and Binding Lifecycle

The connection command is:

```text
moneyflow provider connect monarch --currency USD --scale 2
```

The first connection requires both an explicit three-letter currency and a scale from zero through
nine. They are validated and persisted with the session so every subsequent refresh interprets
provider decimals identically. A retained session that still validates supplies its persisted
settings, so an import retry or reconnect does not require the flags again.

When the default profile file is missing, the command creates the exact current empty schema before
evaluating the pristine predicate. It does not seed the synthetic demo fixture.

On first setup it prompts locally for the Monarch email, Monarch password, Base32 TOTP secret, a
Moneyflow account password, and confirmation. Secret prompts display one mask character per entered
character, support ordinary terminal editing and cancellation, and never echo the value. Moneyflow
validates the TOTP secret, generates a fresh six-digit code for the initial REST or GraphQL request,
and generates another code itself if Monarch returns an MFA challenge. It never asks the user to
transcribe a one-time code.

After a saved session expires, the command prompts only for the Moneyflow account password, unlocks
the stored Monarch credentials, generates TOTP automatically, and replaces the session after
identity validation. The bound profile's currency and scale remain authoritative; reconnect does
not require those flags again and rejects either a conflicting saved session or conflicting values
if they are supplied. Login and import
emit bounded stage and count progress so a slow provider request never appears idle. No sensitive
value is printed or logged.

Initial binding requires the exact pristine-profile predicate used by `CreateSeededProfile`:

- current exact schema
- profile revision zero and journal cursor zero
- protected sentinel taxonomy only
- no committed account, merchant, non-sentinel taxonomy, or transaction rows
- no external identities or known non-sentinel drill identities
- no journal operations or targets
- no existing provider binding

A journal-only profile is not pristine. Connection never orphans pending local state.

A populated default profile is refused; there is no `--replace` option. The error prints the exact
profile path and explains that, until profile management exists, the user must stop moneyflow and
move or remove that file outside the application before connecting. The CLI does not delete or
overwrite the profile.

The binding identity is `subscription.id` from `GetSubscriptionDetails`. The design assumes this
value is stable for one Monarch household across sessions and devices; an opt-in live
characterization test must verify that assumption. The subscription probe also validates a
reconnected session.

The first successful import atomically stores provider kind, provider namespace, and
`subscription.id` with the imported base. A validated session may remain if that initial import
fails, but the profile remains empty and unbound. Re-running the connect command with that valid
session skips email, password, and MFA prompts and goes directly to another import attempt.

Every refresh probes `subscription.id` before fetching or folding. A mismatch returns
`provider_identity_mismatch` and changes nothing. Replacing an existing session file also requires
the replacement session to match the SQLite binding before atomic installation.

`moneyflow provider disconnect monarch` removes only the session file. It preserves the SQLite
binding, committed snapshot, known identities, refresh state, and pending journal. A later
same-household reconnect restores refresh. Another household or provider cannot replace the
binding in this slice.

## External Identity and Label Policy

Every Monarch account, merchant, group, category, and posted transaction maps to one stable local
ID through `external_identities`. Provider IDs never serve as application primary keys, and local
IDs are never reused. External namespaces include the provider and entity kind, such as
`monarch/transaction`, so unrelated entity types never rely on provider-wide ID uniqueness.

Accounts, merchants, category groups, and categories retain their provider label separately from
the local display allocation needed to satisfy active collision-key uniqueness. Import never
silently coalesces two provider identities.

When normalized labels collide:

1. The first-observed identity permanently owns the unsuffixed display label for that collision
   allocation.
2. Later identities receive a visible deterministic suffix derived from their external identity.
3. Colliders first observed in the same import are ordered by external ID; the first owns the
   unsuffixed form.
4. A suffix begins with a short stable hexadecimal token and lengthens deterministically if needed.
   The raw external ID is never displayed.
5. Allocation decisions are persisted and never recomputed from the current collision set, so a
   later collider cannot cause label churn.

The same lossless policy applies to accounts and taxonomy as well as merchants.

User-created or user-renamed effective entities outrank imported labels. A new provider identity
that collides with a user-owned effective label receives the suffix; import never relabels the
user's entity. A pending label operation continues to override a refreshed provider label for its
entity.

When Monarch changes a label and no pending local label operation overrides it, refresh updates
the provider-backed entity label while preserving its stable local ID and drill. If the new label
collides, the renamed provider entity receives a suffix and the incumbent keeps its display label.

An external entity absent from a verified complete entity list is retired rather than erased.
External-identity mappings, label allocations, tombstones, and known drills are never reused or
deleted by refresh. Reappearance of the same provider ID therefore restores the same local ID.

Label allocation is deterministic input to refresh planning. Proposed local IDs and suffix tokens
are materialized in the candidate input so planning never reads clocks or randomness.

## Complete Snapshot Fetch

Every refresh fetches a complete remote snapshot rather than maintaining hot and cold cache tiers.
At the supported scale, avoiding a second cache state machine is more valuable than reducing the
number of pages. The complete path also remains the correctness reference for any future
optimization.

The Python-compatible `moneyflow provider connect monarch --mtd` option is a bounded initial-load
exception. It applies the inclusive first-of-current-month through current-date filter to both
transaction partitions and may seed only a pristine profile, where absence cannot delete prior
provider data. It is rejected for every populated or already-bound profile. The scope is
process-local and never enters the session file; the next ordinary refresh remains a complete
reconciliation.

The transaction query uses no date window, search, category, or ordinary account filter. Monarch
excludes hidden transactions when `hideFromReports` is omitted, so Go fetches two explicit
partitions:

1. visible transactions with `hideFromReports: false`
2. hidden transactions with `hideFromReports: true`

Each attempt performs two complete reads of accounts, merchants, groups, categories, and both
transaction partitions. Canonical imported fields and external identities from the two reads must
match exactly before absence can mean deletion. This deliberately doubles read traffic so stable
counts cannot conceal equal-cardinality offset churn or a changing authoritative entity list.
Each read is normalized and validated as soon as it completes. Deterministically invalid data from
the first read fails immediately instead of downloading a verification read that cannot make it
valid.

Each complete read must also satisfy all of these conditions:

- The first page records the partition's `totalCount`.
- Every page returns the same `totalCount`.
- Results are deduplicated by external transaction ID.
- The fetched unique count equals the recorded `totalCount`.
- Visible and hidden external-ID sets are disjoint.
- After both partitions finish, Go re-queries both counts and requires the final pair to equal the
  corresponding fetched unique counts.
- Account, merchant, group, and category lists contain no duplicate external IDs, and every
  imported transaction reference resolves within the same candidate.

Monarch's account-list and merchant-aggregate surfaces may omit identities referenced only by
hidden transactions. The complete transaction rows already carry stable account and merchant IDs
plus their display labels, so each read supplements its account and merchant candidate from those
inline references before relationship validation. An explicit list or aggregate row remains
authoritative for an ID it contains. Repeated inline references for an otherwise omitted ID must
agree on the label or the attempt is unstable. This is lossless completion of the same provider
snapshot, not silent identity coalescing.

Monarch also permits a category without a group and a transaction without a category. Go preserves
a groupless category's external identity and attaches it to the protected Uncategorized group. A
transaction with no category references the protected Uncategorized category directly. These
sentinel references create no fabricated provider identity. Decimal amounts may contain fractional
digits beyond the configured scale only when every excess digit is zero; the adapter removes those
zeroes exactly and never rounds a nonzero digit.

Provider display labels are canonicalized before they enter the domain snapshot: surrounding
Unicode whitespace is trimmed and control characters are replaced with ordinary spaces. This
matches Python's tolerance of provider-owned labels while preventing terminal-control data from
entering renderers. External IDs and transaction values are never changed by label normalization.

The final count probes catch a transaction whose hidden flag changed during the window between
partition fetches. The matching second read catches insert/delete churn that preserves counts.
Any integrity failure discards both reads and retries from page zero. A refresh receives at most
three complete attempts with bounded, cancellable backoff. Exhaustion is
`provider_snapshot_unstable`; nothing folds, and no confirmation token can override it.

A missing related entity is treated as snapshot instability because separate provider queries can
observe a changing remote between calls. Deterministically invalid scalar data, such as malformed
money after a stable fetch, remains `provider_data_invalid`.

The query must also exhaust transactions belonging to closed or hidden accounts before absence can
mean deletion. An opt-in live characterization test verifies that property. If the unfiltered feed
is not exhaustive, the adapter must use an explicit account scope proven to be exhaustive. Until a
scope is verified, absence outside it cannot tombstone an existing posted transaction.

Pending rows participate in total counts, deduplication, partition disjointness, and final-count
validation. After integrity succeeds, pending rows are removed from the import candidate. A later
posted row is treated according to its posted external ID; no pending-row edit exists to sweep.

## Full Reconciliation and Deletion Plausibility

For the verified posted-transaction scope, absence from a valid complete snapshot means remote
deletion. Ordinary single deletions fold without confirmation. A refresh requires explicit
confirmation when any of these independent conditions holds:

- a nonempty posted profile would become empty
- at least 25 posted transactions and at least 10 percent of the existing posted base disappear
- at least 1,000 posted transactions disappear
- at least 5 posted transactions and at least 50 percent of the existing posted base disappear

The final arm protects small profiles without prompting for routine changes.

The first suspicious attempt does not fold. It returns counts, percentage, and an opaque,
short-lived, process-local confirmation token. The raw validated candidate remains only in that
process's bounded memory; it is not written to SQLite, browser state, a URL, or a temporary file.

The token is bound to:

- the owning process instance
- token expiry
- the profile's refresh generation used by the plausibility check
- the exact in-memory candidate

Wrong-process, expired, missing-candidate, and generation-mismatched tokens are
`provider_confirmation_invalid`. Integrity failures never create a token and can never be
confirmed.

Entering confirmation-required releases the refresh lease. The persisted status names only the
owning renderer class and an opaque instance ID. Another renderer can instruct the user to confirm
in the original interface or invoke refresh itself to fetch and evaluate a new candidate.

Confirmation reuses the validated raw candidate but does not reuse a precomputed fold. It acquires
the write transaction and reruns identity mapping, journal rebase, replay, and validation against
the journal and revision then current. A successful intervening refresh increments the refresh
generation and invalidates the token; journal-only revisions do not invalidate the candidate but
are incorporated by the new rebase.

## Refresh and Journal Rebase

Network fetch and preliminary wire normalization occur without an SQLite read or write
transaction. Refresh has no caller-supplied profile-revision CAS because it resolves no user
targets before the transaction. The authoritative fold reads and rebases the latest journal after
acquiring the write lock.

The fold proceeds in this order:

1. Verify the live subscription identity against the stored binding.
2. Fetch and validate the complete raw snapshot.
3. Evaluate deletion plausibility against a committed base and record its refresh generation.
4. Acquire the immediate SQLite transaction.
5. Authoritatively compare the candidate's refresh generation with the stored generation.
6. Load current committed state, active and inactive journal operations, external mappings, label
   allocations, known drills, profile revision, and refresh generation.
7. Map external identities and labels, then rebase active operations in journal order.
8. Fully replay and validate the resulting effective snapshot.
9. Atomically write the complete refresh plan and advance profile revision and refresh generation
   exactly once.

A generation mismatch rolls back as `provider_refresh_stale`. The lease is only work
coordination; SQLite locking serializes physical writes; refresh-generation and profile-revision
checks protect semantic inputs. None substitutes for another.

The inactive redo tail is permanently discarded before rebase and announced. This is the same
runtime journal-rewrite mechanism used by redo-tail truncation and hide cancellation; provider
rebase is the third named runtime rewrite.

### Transaction targets

Transaction-scoped operations retain exact resolved stable transaction IDs. They never rerun a
merchant-name, selection, or search predicate after refresh.

When a target transaction disappears remotely, rebase removes that target. A partially surviving
batch keeps its operation ID, sequence, order, payload, and undo grouping with only the remaining
targets. An empty operation is removed. The cursor is the count of active operations and decreases
for every removed operation in the active prefix.

### Structural targets

Merchant/category/group merge and delete operations target structural entity IDs, not a frozen
transaction list. Their defined replay semantics sweep the current membership of that entity.
Newly imported transactions referencing a source entity therefore participate in the retained
structural operation.

This narrows the editing design's broad statement that operations never re-evaluate predicates:
transaction-scoped operations never do; structural operations always apply their explicitly typed
current-membership semantics.

Rebase validates entity existence sequentially. An entity created by a retained earlier journal
operation exists for later operations. A missing entity source or destination removes any
dependent label, reassignment, assignment, move, merge, or delete operation rather than silently
redirecting it.

### Hide intent

The active journal contains at most one hide-toggle effect per transaction. The existing
all-or-cancel `h` behavior maintains this invariant.

For each retained hide target, rebase preserves the effective hidden value intended before
refresh. If the refreshed base already equals that intended value, the target is removed; if it
differs, the toggle remains. The rule applies uniformly to pending hide and unhide effects.

### Rebase reporting and limits

The persisted refresh summary and logs contain counts only: removed operations, removed targets,
retained operations, rebased toggles, imported entities, and elapsed time. The initiating renderer
may show ephemeral operation IDs and operation types for removed or changed edits, but never
labels, search text, notes, provider payloads, or transaction descriptions.

The pre-stability journal ceiling is 10,000 operations or 1,000,000 explicit targets.
Append refuses `journal_full` without changing state. Browsing, review, undo, redo, and refresh
remain available. Refresh is permitted at the ceiling because rebase may retain or shrink existing
operations and targets but never append a bookkeeping operation.

## Provider-Backed Editing Capability

All existing edit actions remain available when their domain capability permits them. Pending
operations are profile-global, durable across restart, and immediately visible in both renderers.

`w` opens the ordinary review projection, including the active prefix and inactive redo tail. The
confirmation action is unavailable for a Monarch-backed profile in this slice. Its capability
message states that pending operations are safely stored as user intent and cannot be committed
until Monarch write-back is implemented. The application never performs a local-only commit that
a later refresh could overwrite.

Refresh may shrink or remove operations only under the deterministic rebase rules. It never drops
the entire journal merely because pending edits exist.

## Persistent Schema and Installation Policy

This slice installs schema version 4. Before Go v2 storage stabilizes, startup performs no schema
migration. An older preview profile is `schema_incompatible`; a newer profile remains
`schema_newer`. The user recreates the disposable preview profile.

In addition to the existing committed entities, journal, external identities, and known drills,
the logical schema adds:

- singleton provider binding with provider kind, namespace, remote `subscription.id`, immutable
  currency and scale, and bounded non-sensitive timing metadata
- singleton provider refresh state with refresh generation, last-success timing, stable status
  code, and counts-only summaries
- provider refresh lease with opaque owner ID, renderer class, and expiry
- provider label allocations that persist first-observed collision ownership and suffix decisions

The session token is never stored in SQLite. Provider metadata tables contain no labels, notes,
search text, raw response fragments, email addresses, or credentials.

Refresh generation changes only after a successful fold. Profile revision changes once for a
successful fold and once for each existing semantic journal mutation. Lease acquisition, renewal,
release, last-attempt timing, and failure-status writes are operational bookkeeping and change
neither generation nor profile revision.

## Atomic Refresh Callback

The store exposes an atomic refresh boundary without exposing a transaction handle. The
application supplies a callback whose complete inputs include:

- committed and effective domain snapshots
- active and inactive journal records
- external-identity mappings
- label allocations
- known drill identities
- current profile revision and refresh generation
- the validated provider-neutral candidate
- proposed local IDs and suffix material
- precomputed non-sensitive timestamps required by the plan

The callback returns one complete refresh plan as its only effect. It must not call back into the
store, perform network or filesystem I/O, read global state, consult a clock, or obtain randomness.
Identical inputs produce an identical plan.

The reference path loads all authoritative inputs after acquiring the immediate transaction,
executes the callback, validates the plan, and writes it atomically. A successful plan updates the
provider-owned committed base, tombstones and known drills, external mappings, label allocations,
rewritten journal and cursor, counts-only summary, refresh generation, and profile revision.

An optimized path may load and plan from a prior read snapshot. After acquiring the write lock it
must compare both profile revision and refresh generation. If either changed, it discards the plan
and either recomputes from newly loaded inputs or returns stale. It never patches a plan derived
from different inputs. Reference and optimized paths must produce byte-equivalent canonical
persisted-state encodings. This compares ordered logical rows and payloads, not physical SQLite or
write-ahead-log file bytes.

Any load, planning, validation, or write failure rolls back the complete fold. The previous
committed state, mappings, allocations, journal, cursor, generation, and revision remain intact.
Lease release and failure status use separate operational bookkeeping after rollback.

## Refresh Lease and Scheduling

A SQLite-backed expiring lease ensures that only one process performs provider network work for a
profile at a time. The lease contains an opaque instance ID, renderer class (`cli`, `tui`, or
`web`), and expiry. The owner renews it during a long fetch. A crashed owner's lease can expire and
be recovered.

Clock skew, process death, or a lease implementation error may permit competing candidates. The
refresh-generation CAS still allows at most one candidate based on a particular generation to
fold.

Both long-lived TUI and web processes use the same cadence:

- render the latest SQLite snapshot immediately at startup
- if the last successful refresh is older than six hours, request background refresh
- while alive, reevaluate the six-hour staleness policy on a bounded local tick
- let manual `r` bypass staleness but not identity, integrity, lease, plausibility, or CAS guards

Python's six-hour precedent applies only to its recent 90-day hot tier; Go deliberately applies it
to complete reconciliation. The full-fetch cost is accepted to keep one correctness path.

No database transaction remains open during provider requests, backoff, progress display, user
confirmation, or renderer input. Cancellation during network work releases the lease and changes
nothing. Once the atomic fold begins, it completes or rolls back rather than exposing a partial
cancel.

Automatic refresh never confirms a suspicious deletion candidate. Entering confirmation-required
releases the lease so another renderer can proceed.

## Renderer Experience

`provider.refresh` is a stable action-registry entry with global key `r`. The category manager's
rename overlay continues to own `r` while open. On an unbound profile, refresh is omitted or
disabled through the same capability result with a clear reason; renderers do not dispatch a
doomed provider call.

The CLI and TUI display page/partition progress and counts-only status. CLI progress distinguishes
the first complete read from the verification read so the deliberate second pass never looks like
an unexplained restart. The TUI runs refresh as a cancellable command; its standing scheduler uses
a bounded local tick and never blocks Bubble Tea update or rendering.

The web uses the existing protected mutation path for manual refresh and confirmation. It exposes
a read-only counts/status projection and polls only while the document is visible. It adds no push
channel and transfers no raw provider rows to the browser.

Refresh never changes the analytical URL. Both renderers preserve view mode, time range, committed
search, drill path, focus, and scroll where stable identities still resolve. Focus restores by
stable row identity, then falls back to the nearest valid row.

Selection revalidation is exact and all-or-nothing. If every selected identity still resolves,
the complete selection remains. If any identity vanished, the entire selection clears with an
announcement. Refresh never silently prunes survivors; therefore renderer timing cannot change the
target of a later mutation.

Stable provider label changes preserve entity IDs, keep drills populated, and update breadcrumbs.
A merged, deleted, or otherwise retired known identity renders the normal empty projection with an
unchanged URL. A syntactically valid identity never present in profile history renders the existing
invalid-view screen.

While another process holds the lease, status identifies only the renderer class and opaque
instance. When deletion confirmation is pending elsewhere, the renderer says to confirm in that
interface or invoke refresh here to fetch its own candidate.

## Error and Recovery Contract

Every provider failure uses a stable renderer-neutral code and preserves the previous committed
snapshot and journal unless this design explicitly says otherwise:

- `provider_reconnect_required`: authentication still fails after one session-file reload
- `provider_identity_mismatch`: live `subscription.id` differs from the SQLite binding
- `provider_snapshot_unstable`: snapshot integrity failed after three complete attempts; renderers
  state that no financial data changed and recommend one explicit retry followed by reporting the
  counts if the failure repeats
- `provider_refresh_in_progress`: another process owns an unexpired refresh lease
- `provider_deletion_confirmation_required`: a valid candidate exceeded a plausibility threshold
- `provider_confirmation_invalid`: confirmation expired, belongs to another process, lost its
  candidate, or references another refresh generation
- `provider_refresh_stale`: the fold transaction's generation CAS rejected the candidate
- `provider_rate_limited`: the bounded in-attempt rate-limit policy could not proceed
- `provider_unavailable`: bounded transport or provider-server retries were exhausted
- `provider_data_invalid`: data violates money, identity, label, or referential invariants. An
  ephemeral allowlisted reason identifies the invariant class without including provider values;
  it is never persisted in refresh status or logs.

Existing `store_busy`, `store_error`, `schema_incompatible`, `schema_newer`, and `store_corrupt`
retain their established meanings.

Every status has exactly one scheduler classification.

Retryable by bounded policy:

- `provider_snapshot_unstable`
- `provider_rate_limited`
- `provider_unavailable`
- `provider_refresh_in_progress` only after the current lease completes or expires

Manual or external action required:

- `provider_reconnect_required`
- `provider_identity_mismatch`
- `provider_deletion_confirmation_required`
- `provider_confirmation_invalid`
- `provider_refresh_stale`
- `provider_data_invalid`
- storage, corruption, and schema failures

The classification applies to the failed attempt. A later normal six-hour cycle is a new refresh,
not an automatic replay of the failed candidate.

Rate-limited reads honor a valid `Retry-After` only within a configured maximum. A longer delay is
recorded as a non-sensitive next-eligible time and yields the lease. Reconnect-required waits for
session-file replacement rather than retrying authentication.

After bounded in-attempt retries are exhausted, snapshot-instability and provider-unavailable
statuses also record a bounded next-eligible time. The scheduler does not retry them on every
status tick or retain the lease while waiting.

A stale candidate is not retried automatically because its generation failure proves that another
process successfully folded after the candidate's plausibility check. The profile is already
fresher than the discarded work. The next normal schedule or manual `r` covers any residual gap
without creating a two-process refresh loop.

Operational failure handling releases the lease without advancing profile revision or refresh
generation. No client automatically replays a refresh confirmation, an identity decision, or a
storage mutation after an error.

Lease renewal is monotonic. Renewal, release, failure status, and the successful fold compare the
opaque lease owner inside their write transaction. Work from an expired former owner cannot clear
a successor's lease or overwrite its status.

## Performance Contract

Provider network time is observed but is not a deterministic CI performance gate. Page fetches use
bounded concurrency only if Monarch's ordering and rate behavior remain correct; a sequential
implementation is acceptable for the first slice.

For 100,000 posted transactions and a representative pending journal, the complete write-locked
phase targets less than 250 milliseconds on the documented reference machine. A committed CI
smoke test uses a generous one-second ceiling. The measurement includes authoritative snapshot
load when using the reference path, identity allocation, journal rebase, full replay, validation,
and SQLite writes. It excludes provider network work and preliminary wire parsing.

Recorded benchmarks compare the reference transactional path with any optimized precomputed plan.
Optimization cannot weaken revision/generation checks, change replay order, alter label allocation,
or produce different persisted bytes.

## Privacy and Security

The existing no-auth web threat model remains unchanged. Provider refresh mutations use the signed
instance-bound mutation token, canonical-origin check, Fetch Metadata validation, and cookie-free
API. Tokens and provider confirmation values never enter URLs, referrers, or logs.

Logs and persisted status use an allowlist. They may contain only:

- stable error codes
- profile revision and refresh generation
- counts and percentages
- bounded timings and next-eligible timestamps
- renderer class and opaque process instance ID
- correlation IDs

They never contain merchant, category, group, or account labels; transaction descriptions or
notes; search text; provider IDs; household IDs; GraphQL variables or bodies; HTTP response
fragments; email; credentials; session tokens; device UUIDs; filesystem home paths; or complete
URLs.

Synthetic provider fixtures use reserved example names and fake identifiers. Live data is never
copied into tests, screenshots, documentation, parity artifacts, browser recordings, or failure
messages.

## Testing Strategy

### Pure and provider tests

- parse exact positive and negative decimal amounts at supported scales without floating point
- accept excess fractional zeroes without rounding and reject excess nonzero precision
- reject unsupported precision, currency, numeric syntax, dates, labels, and required identities
- exercise GraphQL request/response behavior through injected loopback transports
- cover REST-first login, GraphQL fallback, generated TOTP on the initial request and fallback
  challenge, session validation, session replacement, and redacted error translation
- prove the encrypted credential vault round-trips with owner-only permissions, rejects wrong
  passwords and tampering identically, contains no plaintext credentials, and is never created
  after failed authentication
- exercise masked terminal input, editing, cancellation, secret-redacted output, reconnect unlock,
  persisted currency/scale reuse, bounded CLI import progress, and Python-compatible `--mtd`
  pristine seeding with both transaction partitions date-bounded
- validate visible and hidden pagination, duplicate IDs, changing per-page counts, final count
  probes, cross-partition flag changes, duplicate entity IDs, missing related entities, and
  three-attempt exhaustion
- prove pending rows participate in integrity checks and never enter the import candidate
- prove hidden-only account and merchant identities are completed from transaction-inline fields
  without merging external IDs, and reject conflicting inline labels
- prove groupless categories and missing transaction categories map to protected Uncategorized
  sentinels while preserving all available provider identities
- prove provider labels containing surrounding Unicode whitespace and internal control characters
  become domain-valid display labels without exposing raw values
- prove deterministic first-read validation skips the verification read and returns only an
  allowlisted value-free reason
- verify sticky first-observed collision ownership, simultaneous-import external-ID ordering,
  deterministic suffix extension, ordinary provider renames, and pending user-label precedence
- prove identical closed callback inputs return identical refresh plans
- enforce package import directions and the callback's store-handle-free public shape with
  architecture tests
- compare randomized journal rebase and replay results against a full reference replay
- preserve structural current-membership sweeps and the at-most-one-active-hide-toggle invariant
- test deletion confirmation boundaries exactly: nonempty-to-empty; 24 versus 25 removals; 9.9
  versus 10 percent; 999 versus 1,000; 4 versus 5 at 50 percent; and the small-profile majority
  case the fourth arm protects
- table-test every provider error code so it belongs to exactly one scheduler class, including
  bounded `Retry-After` handling and next-eligible recording

### Store and concurrency tests

- install exact schema version 4 and reject older/newer schemas without migration
- cover pristine binding, journal-only refusal, populated refusal, same-identity reconnect, and
  different-identity refusal
- validate `subscription.id` on every synthetic refresh and prove mismatch cannot fold
- prove lease/status writes never advance profile revision or refresh generation
- reject stale ordinary and confirmed candidates through the generation CAS
- cover confirmation-token success, expiry, wrong-process, lost-candidate, and generation-change
  invalidation
- prove a pagination-integrity failure cannot fold even when presented with an otherwise valid
  confirmation token
- inject failure at each write stage and prove the complete prior state survives restart
- compare the effective pre-fold plan with freshly reopened committed and effective state
- require reference and optimized fold paths to persist byte-equivalent state
- persist and reopen binding, generation, known drills, external identities, and sticky labels
- race independent store handles for lease and fold; at most one stale-generation candidate folds
- stage edits during a slow fetch and prove the authoritative transaction rebases them
- exercise expired-lease recovery, cancellation, confirmation lease release, and process ownership
  status
- fill the journal to 10,000 operations and 1,000,000 targets; prove append refuses without change,
  browsing/review/undo remain available, and refresh still runs and can only retain or shrink the
  journal

### Renderer and API tests

- cover global `r`, modal-local `r`, unbound-profile availability reason, six-hour scheduling,
  cached startup, progress, cancellation, and manual refresh
- preserve or clear selection through exact all-or-nothing revalidation
- restore focus and scroll and distinguish stable-populated, retired-empty, and never-known-invalid
  drills
- heal reconnect-required after atomic session replacement without repeated failed authentication
- present deletion-confirmation ownership and cross-renderer guidance
- keep `w` review available while commit confirmation explains durable pending intent
- map every stable provider error identically in TUI and web
- preserve base-path, canonical-origin, mutation-token, Fetch Metadata, no-store, and no-cookie
  guarantees
- scan status payloads, logs, browser history, session errors, and fixtures for forbidden provider
  or personal data

### Performance and live characterization

- benchmark the complete 100,000-row write-lock phase against the 250-millisecond reference target
  and one-second CI ceiling
- record end-to-end fake-provider import and refresh timings without making network timing a CI
  contract
- run race, vet, format, lint, parity, web, security, cross-platform, and dependency checks

Live Monarch characterization is behind one explicit command and never runs during ordinary tests
or CI. It uses an explicit temporary profile, reports counts only, and leaves no committed fixture,
snapshot, recording, or documentation artifact. It verifies only assumptions that synthetic tests
cannot prove:

- `subscription.id` stability across sessions and devices
- exhaustive visible/hidden transaction coverage for closed and hidden accounts
- the observed pending-to-posted identity lifecycle

## Completion Criteria

This slice is complete only when fresh evidence shows all of the following:

- CLI connect creates a bound profile only from the exact pristine state
- initial import and six-hour/manual refresh use the complete validated two-partition snapshot
- offline TUI and web browsing work with the session absent or provider unavailable
- staged edits survive restart and refresh through deterministic journal rebase
- Monarch commit remains unavailable with an accurate durable-intent explanation
- subscription identity, generation CAS, leases, deletion plausibility, and confirmation tokens pass
  their synthetic integration tests
- all provider errors have one tested scheduler classification
- session files and replacements satisfy the hardened cross-platform filesystem contract
- the 100,000-row fold and write-lock performance contracts are met
- Linux/macOS/Windows build, Go race, vet, format, lint, parity, frontend, browser, security, and
  privacy checks pass
- the diff contains no provider write-back, provider mutation, outbound queue, plaintext credential
  persistence, unattended vault unlock, multi-profile management, export, or Python-state import
  code

The opt-in live dogfood flow covers connect, initial import, offline browse, staged edits,
refresh/rebase, and CLI reconnect recovery. It does not require destructive deletion confirmation.
That flow is proven by synthetic integration tests. If a naturally occurring isolated deletion is
observed during dogfooding, it may additionally confirm that an ordinary deletion remains below
the confirmation threshold; causing such a deletion is never a completion requirement.

Completion does not authorize pushing, merging `go-port`, removing Python, or implementing
provider mutation.

## Immediate Follow-On Slice

The next design adds Monarch write-back at the existing review/commit boundary. It must define a
real provider writer capability, durable outbound work, idempotency, partial remote failure,
reconciliation with provider responses, crash recovery, and the point at which staged operations
become committed locally.

SimpleFIN, YNAB, Amazon, multi-profile management, export, Python-state import, packaging, final
parity audit, and Python removal remain later independent slices.
