# State, accounting, and storage

## Exact accounting

[Domain money][source-1] carries signed minor units, currency, and scale.
Parsing is the boundary for converting external numeric representations; aggregation, filtering,
sorting, replay, and serialization do not use floating-point money. YNAB's milliunits must convert
exactly to the profile's scale or the import fails. Negative and zero amounts are real values;
missing money must not become zero.

Keep currency/scale partitions separate when aggregating. Stable ordering and explicit tie-breaks
make navigation and exports reproducible. The analytics package has no knowledge of SQLite,
journal storage, credentials, or HTTP.

[Query filtering][source-2] is part of the product contract:

- Ordinary TUI/web search is a case-insensitive regular expression over merchant and category.
- Dates and drills apply to the requested analytical state; drills use stable entity IDs.
- Hidden-row handling differs between aggregate and detail modes. Do not replace it with an
  unconditional hide predicate.
- The transfer filter has its own predicate; provider-derived hidden state is not a substitute.
- MCP literal search and Amazon product search are explicit application request variants, not
  changes to ordinary regular-expression search.

Named tests and query types are authoritative for individual edge cases. Preserve
[logical expectations][source-3] when refactoring the engine.

## Committed and effective state

`internal/domain` owns stable entities and typed operations.
`internal/replay` computes effective state from the committed profile and active journal prefix.
`internal/app` uses it for projections and mutation validation.

```text
committed rows + journal operations before cursor = effective snapshot
```

The cursor counts active operations; it is not a sequence number. Sequence gaps after rewrites
are valid. Undo moves the cursor back; redo moves it forward. Appending while behind the journal
head permanently discards the redo tail. Bulk edits remain one undo unit.

[Operation payloads][source-4] identify stable local targets and carry
the forward data needed for replay. Transaction-targeted edits retain their resolved IDs; replay
does not re-run the user's search or selection. Structural operations such as merchant merge
target an entity and sweep its current membership during replay. That distinction is essential
when refresh introduces transactions into a pending merge source.

Permitted runtime journal rewrites include redo-tail truncation, Python-compatible double-hide
cancellation, and provider-refresh rebase. Double-hide cancellation removes applicable pending
toggle effects, shrinks partial batches, removes empty operations, and adjusts the active cursor.
It is a revision-checked mutation, not a second compensating undo unit.

Staged transaction deletion removes the row from effective state while remaining undoable. Local
commit folds the active prefix atomically, discards redo history, and clears pending history.
The invariant is: freshly loaded committed state equals the effective snapshot before the fold,
including structural retirement effects. Provider write-back uses a response-adjusted version of
that invariant described in [providers](providers.md).

## Identity and bookmarks

Transactions, merchants, accounts, categories, and groups have stable local IDs. External provider
IDs live in separate mappings; labels are not primary keys. Collision allocation preserves distinct
provider identities rather than silently merging them. Retired IDs are not reused.

Known drill identities survive retirement and process restart. Renaming a stable entity keeps its
drill populated and updates its label. A historically known retired identity gives an ordinary
empty view. A syntactically valid identity with no profile history gives invalid view. Registry
entries can outlive pending edits that first exposed an identity; commit is not the only durable
writer of identity bookkeeping.

Deleting an imported transaction retains its external mapping. Reappearance with the same provider
ID restores the same local ID; a new provider ID receives a new local identity.

## Revision and concurrency

[Profile service][source-5] revalidates the profile revision before
using cached effective state. Mutation-time expected revision is checked authoritatively inside
the SQLite write transaction. A cached pre-check is only a fast failure path. Target resolution
from a snapshot is valid only if that transaction's revision comparison succeeds.

SQLite locking serializes writes; revision checks prevent semantically stale actions. Neither
replaces the other. Store errors leave the transaction rolled back; report the current revision
only when it can be read safely. Do not automatically replay a revision conflict or store-busy
user action later. Selection revalidation is exact: if the full selection cannot be resolved,
clear it and announce; never apply to an accidental partial subset.

Provider refresh generation, write-batch version, and lease expiry are different counters with
different purposes. Operational polling/lease maintenance does not invalidate analytical caches
by incrementing the profile revision.

## Profiles, files, and recovery

[Profile catalog][source-6] discovers per-profile manifests under
the v2 catalog root. `MONEYFLOW_HOME` selects that root; the default is `~/.moneyflow/v2`.
New profiles live under `profiles/<opaque-id>/`. A root-level legacy Go database remains in place;
downstream code receives its root just as it does any other profile.

Selection resolves ID/key first, then a unique normalized display name. Without an explicit choice,
CLI commands use the sole persistent profile only when exactly one exists. Interactive commands
provide the selector. Temporary demo/fixture profiles are unregistered and never reuse the default
financial profile.

[Home locks][source-7] are OS advisory locks, not marker-file ownership guesses.
Acquire catalog before profile lifecycle when both are needed, then the operation-specific lock.
Import and export are independent operations under lifecycle protection; never acquire catalog
in reverse order while holding lifecycle. SQLite operation leases serve network workers, not file
recovery. Their semantics are different from process-death-released advisory locks.

SQLite uses STRICT tables, integer money, WAL, bounded busy handling, and `synchronous=FULL`.
The database is not encrypted. Credentials are separately encrypted; private-directory and file
permissions still matter. Use existing `internal/home` helpers for publication and cleanup.

[Recovery][source-8] requires exclusive lifecycle ownership.
The web cache must close its own service before recovery, not contend against its shared lock.
Recreate backs up the old database and sidecars on the same filesystem, installs a pristine schema,
and preserves credential/session files. It does not migrate or silently reconnect/import.

Recovery markers support idempotent roll-forward. Backup-file presence distinguishes an unmoved
old main file from a newly installed file. A partially moved database is not silently opened as a
healthy profile. Newer-schema profiles require a newer application; recovery must not offer to
erase them as an ordinary incompatibility fix.

Source and test entry points: [schema installation][source-9],
[installed SQL][source-10],
[replay equivalence][source-11], and
[catalog recovery tests][source-12].

[source-1]: https://github.com/wesm/moneyflow/blob/go-port/internal/domain/money.go
[source-2]: https://github.com/wesm/moneyflow/blob/go-port/internal/analytics/filter.go
[source-3]: https://github.com/wesm/moneyflow/blob/go-port/testdata/parity/logical_expectations.json
[source-4]: https://github.com/wesm/moneyflow/blob/go-port/internal/domain/operations.go
[source-5]: https://github.com/wesm/moneyflow/blob/go-port/internal/app/profile_service.go
[source-6]: https://github.com/wesm/moneyflow/blob/go-port/internal/profilecatalog/catalog.go
[source-7]: https://github.com/wesm/moneyflow/blob/go-port/internal/home/lock.go
[source-8]: https://github.com/wesm/moneyflow/blob/go-port/internal/profilecatalog/recovery.go
[source-9]: https://github.com/wesm/moneyflow/blob/go-port/internal/store/sqlite/initialize.go
[source-10]: https://github.com/wesm/moneyflow/blob/go-port/internal/store/sqlite/schema/profile.sql
[source-11]: https://github.com/wesm/moneyflow/blob/go-port/internal/replay/replay_equivalence_test.go
[source-12]: https://github.com/wesm/moneyflow/blob/go-port/internal/profilecatalog/recovery_test.go
