# Providers and data movement

Provider adapters own remote protocols. Application services own reconciliation and user intent.
Keep the distinction even when one provider has a convenient bulk endpoint.

## Network refresh

[Refresh orchestration][source-1] fetches outside SQLite and validates
identity and money settings. SimpleFIN adds new rows as described below. Monarch and YNAB
reconcile complete snapshots: the service folds a complete candidate against the latest journal inside
the store's transaction. Identity allocation, structural rebase, replay, and referential validation
are part of that fold. Optimized paths must agree with the reference planner.

The provider-operation lease coordinates active network work across processes. The fold also
checks refresh generation and the absence of an unfinished write batch. Lease expiry alone is
never authority to install a stale candidate or rebase a frozen write prefix.

Monarch and YNAB refresh can shrink pending operations and discard inactive redo history. Earlier retained
journal-created entities count as existing during sequential rebase. Structural operations sweep
new membership; transaction-targeted edits keep their resolved targets. No-op refresh accounting
must distinguish operational status from a changed semantic profile.

For complete snapshots, deletion plausibility requires confirmation when a nonempty profile becomes empty, when
removals are at least 25 and 10%, when at least 1,000 disappear, or when at least five and 50%
disappear. Integrity failures cannot be overridden by confirmation. Tokens identify process-local
candidates and are bound to the checked refresh generation. Confirmation folds the candidate
against the current journal, not a precomputed stale rebase.
Ordinary and confirmed folds recheck the candidate's captured credential fingerprint before
entering the store. A changed or removed credential file refuses the fold, releases the lease,
and requires reconnect; a consumed confirmation cannot be reused.

Monarch and YNAB use a six-hour cadence in long-lived TUI/web processes. SimpleFIN uses
24 hours and parks failed automatic attempts until explicit action. MCP refresh is explicit.
Amazon and bank CSV have no network refresh scheduler.
Read-only MCP refresh of Monarch or YNAB can still rebase pending intent and discard the redo tail.
Read-only excludes user-edit tools, not reconciliation with remote truth.

## Durable remote write-back

Monarch and YNAB supply writers behind the same durable worker. YNAB is configured only after
explicitly unlocking that process's credential vault.

[Write planning][source-2] derives absolute per-transaction items
from the reviewed effective state. Net no-ops generate no requests. Deletion supersedes updates.
Preparation checks revision and an owned write lease atomically, freezes the active prefix, and
records the batch. A simultaneous refresh must either finish first or fail the transactional guard.

[The worker][source-3] has a four-item concurrency ceiling, persists
successful results, and owns retries. One new-name leader must establish a remote merchant ID
before dependent items proceed. A process-local worker reservation and a cross-process operation
lease are both required. No adapter or renderer creates its own retrying write loop.

Durable phases cover writing, reconciling, pause, reconnect, rate limit, attention, and reconciliation
confirmation. Parked phases release their network lease. Pause and attention require explicit
action; opening another renderer is not permission to ignore them. Rate waits preserve next-eligible
time. Credentials and source fingerprints must be revalidated before a resumed worker sends data.

Unknown-outcome updates are not automatically resent after a crash. Deletes can be repeated within
their attempt budget. Keep known non-applied failures distinct from a request that may have reached
the provider. Recorded successes are never resent because finalization failed locally.

Finalization atomically installs the reviewed effective state adjusted by accepted provider
responses, retires the journal prefix, and schedules a full refresh. Ordinary mapped field overrides
are counted; strict new-merchant identity conflicts require attention. Stable merchant rotations
retain historical aliases and provider labels. An empty old alias does not create a phantom row;
its return with transactions can require a fresh local identity.

Stop and reconcile is the escape hatch: stop sending, obtain remote truth, and atomically remove
the whole frozen prefix with the refresh fold. Failed and unsent intent is abandoned. Successful
item facts support status/recovery until the fold; they do not force partial rotations into the
authoritative snapshot. Failed fetches leave the batch and intent available for another attempt.

While a batch exists, editing, undo/redo, refresh, and another commit are unavailable. Browsing,
details, export, and recovery remain available. This deliberate restriction avoids an unfrozen
editable suffix in the initial durable-batch model.

## Monarch

[Monarch][source-4] uses the small REST-login/GraphQL surface consumed by
Moneyflow. Session files and an encrypted credential vault support reconnect and TOTP without
persisting plaintext account passwords. Binding is to the selected household identity; a swapped
session must not silently redirect a profile.

Complete reads fetch visible and hidden partitions, with pagination/identity checks before absence
can imply deletion. Posted transactions are imported; pending bank rows are excluded. The explicit
scoped initial-import commands are for quick/pristine imports, not ongoing partial reconciliation.
Read the adapter's current tests before changing its consistency checks: early port designs were
corrected after real provider responses exposed assumptions that Python did not make.

Merchant name-addressing is distinct from local display allocation. Never send a collision suffix
as a new provider merchant name. Taxonomy management remains in Monarch; writes cover merchant,
mapped category, hide, and transaction deletion through the durable commit path.

## YNAB

[YNAB][source-5] is a small in-repository Go HTTP client, not a generated Go SDK.
The full `/plans/{id}` response is joined with transaction detail under matching server knowledge
and transaction/split facts. Categories missing from taxonomy can be retained from consistent
detail labels; do not reject usable data merely because the collection omitted a category.

Import rejects absent/null amounts and decimal digits rather than inventing zero. Budget currency/scale is checked
even when the budget has no transactions. Conversion from milliunits is exact. Uncleared rows
retain provider-pending status; transfers and off-budget rows supply hidden state. Split parents
remain one accounting row, with child details preserved separately and their sum validated.

The selected plan's SQLite binding is authoritative over a vault copy. Unlock uses the encrypted
profile vault. No automatic plaintext-token persistence or process-restart unlock is provided.
Readers check the captured vault fingerprint before each HTTP request, including the second
transaction-detail request. A failed onboarding vault save requires credential re-entry; retry
cannot bypass saving and bind new settings against an old vault.

The [transaction writer][source-12] fresh-reads a parent before each PUT or DELETE. Updates carry
only requested payee/category fields plus the fresh approval value. YNAB's
[ExistingTransaction schema](https://api.ynab.com/papi/open_api_spec.yaml) inherits optional fields;
account, date, and amount are deliberately not rewritten. Omitted category, mapped
category, and explicit clearing are distinct. Mapped payees use IDs; a new-name leader persists
the resulting ID before followers proceed. Response validation preserves unrelated transaction
and split facts; ordinary payee/category overrides become counted provider truth. Unknown echoed
identities remain provisional until the immediately due refresh.
New-name responses that claim another active local payee's identity, including an identity just
claimed by another group in the batch, stop in reconcile-only attention before the conflicting
success is recorded or followers are sent.

Transfers are refused using persisted provider facts, not the generic hidden flag.
ID-addressed payee edits also fresh-read the destination payee before PUT and reject transfer,
deleted, missing, or malformed destinations. Vault validation runs before that read and the PUT.
These preflights cannot eliminate a provider-side change after the final GET. Ordinary
off-budget rows remain writable. Split parents support payee changes and whole-parent deletion,
but not category changes. Hide toggles and taxonomy administration are refused before staging
and rechecked during preparation. Uncategorized is the protected system category.

The adapter performs one attempt; the shared worker owns retries. A 429 without a usable
Retry-After parks for one hour, capped by the common 24-hour ceiling. A possibly applied PUT is
reconcile-only, while deletion can be resent within the attempt budget. Already-absent deletion
is accepted only after verifying access to the bound plan.

Unlock with an unfinished batch configures its writer without refreshing the frozen prefix or
dispatching from the CLI. Bound profiles never reuse a pre-write unlock snapshot for refresh;
fresh fetching belongs under the provider-operation lease. TUI/web ticks resume eligible work.
Live mutation characterization requires separate permission for disposable targets.

## SimpleFIN

The [SimpleFIN adapter][source-13] fetches posted transactions and adds previously unseen
remote IDs. Refresh preserves existing transaction facts, local edits, deletion records,
and the complete journal, including inactive redo. Absence from a later feed is not a
deletion instruction. This is additive import, not Monarch/YNAB snapshot reconciliation.

Edits commit locally. The adapter never sends transaction mutations to SimpleFIN. Automated
contract and synthetic workflow tests are required, but the provider remains experimental
until maintainers review authorized live-bank evidence. See the
[SimpleFIN guide](../guide/simplefin.md) for connection and history limits.

## Amazon import and matching

[CSV parsing][source-6] and the
[import coordinator][source-7] feed a unified profile, not a parallel Amazon
database. Import is user-initiated and serialized by an advisory lock before traversal/upload
consumption. Uploaded bytes stage to private files rather than a whole-file browser-server buffer.

Observed orders are authoritative; orders absent from the input are untouched. Cancelled rows
mark an order observed but contribute no active item. Active invalid rows reject the candidate;
in-session errors can identify file/record/column, while persisted status/history stay counts-only.

[Reconciliation][source-8] pairs exact active identities, then exact
retired identities, then unambiguous same-ASIN and per-order ASIN-less singletons. Ambiguous
leftovers retire/allocate instead of moving categories between different purchases. Status and
optional unit price do not define identity. Unchanged input preserves committed state/revision
while operational import history can still record the attempt.

Currency/scale binds the profile; taxonomy cloning is a one-time committed snapshot, not live
inheritance. Amazon edits commit locally. Matching reads other profiles through short-lived
read-only snapshots, skips incompatible sources, and uses revision-keyed caching.

[Matching][source-9] preserves pass exclusivity across all candidate
profiles. Exact matching precedes fuzzy matching; the fuzzy minimum is 15 major units, following
Python's constant rather than its outdated 10-unit comment. Provider/display label handling,
date distance, caps, and deterministic ordering belong in the matching implementation and tests.

## Bank CSV import

The [bank CSV parser][source-14] produces bounded source facts with exact amounts. CLI
orchestration discovers files and parses each once; [the shared import planner][source-15]
owns reconciliation. SQLite installs each file's committed rows, rebased journal, identities,
and CSV state in one write transaction. No bank network adapter or credential vault is used.

The four tables are `csv_settings`, `csv_files`, `csv_rows`, and `csv_file_rows`.
Opaque hashes identify source transactions and file paths. Per-file occurrence numbers
preserve repeated identical purchases; file membership records prevent one overlapping
export from removing another's rows. `present`, `superseded`, and user-`deleted` source
rows are distinct. Committed merchant/category overrides are recorded by the shared fold,
not inferred from a label comparison. Active pending edits also protect missing rows.

[JSONL transfer](../getting-started/transition.md) carries this state and validates it against
the committed profile. This uses the current install-only schema, not a migration or a
second database. The [CSV guide](../guide/bank-csv.md) owns corrected-file policy, skipped-row
behavior, limits, and identity limitations.

## Export

[Application export][source-10] captures committed state, not the effective view.
Filtered export applies the requested analytical filters to committed rows; pending edits can
therefore change the displayed view without changing the export. Warn about excluded operations,
without suggesting an unavailable commit during a write batch.

[Exporters][source-11] consume a detached document and write CSV, Parquet, or SQLite.
They do not query profiles or providers. Capture revalidates revision without network refresh.
Metadata records scope/revision/counts. CSV formula protection covers free text, including provider
transaction IDs. Generated local IDs stay unchanged. A negative amount must remain a numeric
decimal string without an apostrophe.

Preview needs no export lock. Execution uses the profile advisory export lock, private temporary
files, atomic no-overwrite publication, and cleanup on failure. TUI reports the completed local
path; web delivers a download and cleans the server temporary file. Windows close/remove ordering
and stale-file cleanup are tested. Export remains available offline and during provider batches.

[source-1]: https://github.com/wesm/moneyflow/blob/go-port/internal/app/provider_refresh.go
[source-2]: https://github.com/wesm/moneyflow/blob/go-port/internal/app/provider_write_plan.go
[source-3]: https://github.com/wesm/moneyflow/blob/go-port/internal/app/provider_write.go
[source-4]: https://github.com/wesm/moneyflow/tree/go-port/internal/provider/monarch
[source-5]: https://github.com/wesm/moneyflow/tree/go-port/internal/provider/ynab
[source-6]: https://github.com/wesm/moneyflow/tree/go-port/internal/importer/amazon
[source-7]: https://github.com/wesm/moneyflow/tree/go-port/internal/amazonimport
[source-8]: https://github.com/wesm/moneyflow/blob/go-port/internal/app/amazon_reconcile.go
[source-9]: https://github.com/wesm/moneyflow/blob/go-port/internal/app/amazon_matching.go
[source-10]: https://github.com/wesm/moneyflow/blob/go-port/internal/app/export.go
[source-11]: https://github.com/wesm/moneyflow/tree/go-port/internal/exporter
[source-12]: https://github.com/wesm/moneyflow/blob/go-port/internal/provider/ynab/write.go
[source-13]: https://github.com/wesm/moneyflow/tree/go-port/internal/provider/simplefin
[source-14]: https://github.com/wesm/moneyflow/tree/go-port/internal/importer/bankcsv
[source-15]: https://github.com/wesm/moneyflow/blob/go-port/internal/app/bankcsv_import.go
