# Providers and data movement

Provider adapters own remote protocols. The application service owns reconciliation and user
intent. Keep that split even when a provider offers a convenient bulk endpoint.

| Provider | Data input | Edits |
| --- | --- | --- |
| Monarch | Complete or 90-day-window snapshots | Durable remote write-back |
| YNAB | Complete plan snapshots | Durable remote write-back after explicit unlock |
| SimpleFIN | Additive fetch of new posted rows | Local only |
| Amazon | User-chosen order-history CSV files | Local only |
| Bank CSV | User-chosen Chase CSV files | Local only, with sticky overrides |

## Network refresh

[Refresh orchestration][source-1] fetches outside SQLite and validates identity and money
settings. SimpleFIN adds new rows, as [described below](#simplefin). Monarch and YNAB reconcile
snapshots: the service folds a candidate against the latest journal inside the store's
transaction. Identity allocation, structural rebase, replay, and referential validation are
part of that fold. Optimized paths must agree with the reference planner.

The provider-operation lease coordinates network work across processes. The fold also checks the
refresh generation and the absence of an unfinished write batch. Lease expiry alone is never
authority to install a stale candidate or rebase a frozen write prefix.

A Monarch or YNAB refresh can shrink pending operations and discard inactive redo history.
Earlier retained journal-created entities count as existing during sequential rebase. Structural
operations sweep new membership; transaction-targeted edits keep their resolved targets. No-op
refresh accounting must distinguish operational status from a changed semantic profile.

### Deletion plausibility

A snapshot refresh needs confirmation when any of these hold:

- A nonempty profile becomes empty.
- At least 25 transactions and 10% disappear.
- At least 1,000 disappear.
- At least five and 50% disappear.

Integrity failures cannot be overridden by confirmation. Tokens identify process-local
candidates and are bound to the checked refresh generation. Confirmation folds the candidate
against the current journal, not a precomputed stale rebase. Ordinary and confirmed folds
recheck the candidate's captured credential fingerprint before entering the store. A changed or
removed credential file refuses the fold, releases the lease, and requires reconnect; a consumed
confirmation cannot be reused.

### Refresh scope and schedule

`ProviderRefreshDue` returns `none`, `recent`, or `full`, and provider status reports it as
`refresh_due`. The server recomputes it for each scheduled request, so the TUI and the browser
do not copy the rule.

| Provider | Scheduled refresh in long-lived TUI and web processes | Manual refresh reads |
| --- | --- | --- |
| Monarch | Recent 6 hours after the last success; full 30 days after the last full one, or when that time is unknown | Recent window, unless the profile has no successful import or a full refresh is due |
| YNAB | Every 6 hours | Complete plan |
| SimpleFIN | 24 hours after the last success; failed automatic attempts park until explicit action | The feed from 14 days before the last import; only new rows are added |
| Amazon, bank CSV | None | Not applicable; users import files |

A profile with no successful import is never due. `ManualRefreshScope` chooses what a manual
refresh reads, and provider status reports it as `manual_refresh`. A request that sets
`CompleteHistory` (the API's and the MCP tool's `complete_history`, or the `R` key) reads
complete history. A month-to-date first import names its own window. MCP refresh is always
explicit.

The last full refresh time is advisory state in the profile's `provider-refresh.json`, outside
the schema and the JSONL transfer. Only a folded complete Monarch candidate writes it.
[Monarch refresh](../guide/monarch.md#refresh-data) owns the user-facing schedule.

A recent refresh fetches a candidate whose `TransactionWindow` is the 90 days ending on the
clock's local date. The Monarch reader sends it as `startDate` and `endDate` on every page and
reports it back; the service rejects a different window. Planning replaces only provider rows
dated inside the window, keeps the entities that retained rows use, and counts deletion
plausibility within the window. A month-to-date first import passes its range as the window of
that one manual refresh, so the provider source keeps no range and later refreshes are not
windowed.

Read-only MCP refresh of Monarch or YNAB can still rebase pending intent and discard the redo
tail. Read-only excludes user-edit tools, not reconciliation with remote truth.

### Refresh sharing

A process that serves MCP tools or embeds the Go package runs one refresh at a time for each
profile. A new request shares the running refresh when that refresh reads at least as much:

- A scheduled request reads nothing or what a manual request reads from the same provider
  status, so it shares any refresh.
- A manual request shares a manual or complete-history refresh.
- YNAB and SimpleFIN read complete data on every manual refresh, so for them a complete-history
  request is a manual request. A Monarch complete-history request shares only a
  complete-history refresh.

Any other request waits for one follow-up refresh. Once the running refresh stops, the follow-up
reads the most that any of its requests asked for.

- A refresh that read the provider read at least what a manual refresh reads. So when a
  scheduled refresh read, a manual follow-up behind it takes its outcome and does not read the
  same data again.
- A follow-up behind a scheduled refresh that read nothing still reads.
- A refresh that waits for deletion confirmation has read what a manual refresh reads, so only a
  Monarch complete-history request replaces it.
- Shutdown drops a follow-up that has not started.

Confirmation tokens are process-local, but another process can change the profile while a
refresh or reconcile here waits for confirmation. Before a request joins, starts, or is refused
because of a waiting attempt, the process compares that attempt with the profile's stored state:

- A waiting refresh is obsolete once the stored refresh generation or revision has moved past
  it, for example after another process refreshed or edited the profile.
- A waiting reconcile is obsolete once the stored write batch is a different one, or its version
  or the revision has moved past it, for example after another process finished the batch.

An obsolete attempt fails with `provider_confirmation_invalid`, as it does at its deadline, and
the request is handled as a new one. The service would also refuse an obsolete reconcile's
token, because its confirmation checks the batch ID, version, and revision.

## Durable remote write-back

Monarch and YNAB supply writers behind the same durable worker. YNAB's writer is configured
only after that process explicitly unlocks its credential vault.

[Write planning][source-2] derives absolute per-transaction items from the reviewed effective
state. Net no-ops generate no requests. Deletion supersedes updates. Preparation checks revision
and an owned write lease atomically, freezes the active prefix, and records the batch. A
simultaneous refresh must either finish first or fail the transactional guard.

[The worker][source-3] has a four-item concurrency ceiling, persists successful results, and
owns retries. One new-name leader must establish a remote merchant ID before dependent items
proceed. A process-local worker reservation and a cross-process operation lease are both
required. No adapter or renderer creates its own retrying write loop.

Durable phases cover writing, reconciling, pause, reconnect, rate limit, attention, and
reconciliation confirmation. Parked phases release their network lease. Pause and attention
require explicit action; opening another renderer is not permission to ignore them. Rate waits
keep the next-eligible time. Credentials and source fingerprints are revalidated before a resumed
worker sends data.

Unknown-outcome updates are never resent automatically after a crash. Deletes can repeat within
their attempt budget. Keep known non-applied failures distinct from a request that may have
reached the provider. Recorded successes are never resent because finalization failed locally.

An explicit Monarch resume can check uncertain updates through a bounded read of each
transaction's recorded date:

- A result matching the requested fields is recorded without another write.
- A result matching the previous values permits a retry through the durable worker.
- Any other result leaves the batch parked.

The check holds the write lease and records its observations in the audit log; it does not fetch
a profile snapshot. Startup and status polling never start these checks.

Finalization atomically installs the reviewed effective state, adjusted by accepted provider
responses, and retires the journal prefix. It keeps the refresh timestamp and retry floor;
writing a transaction neither invalidates nor freshens the rest of the cache. Ordinary mapped
field overrides are counted; strict new-merchant identity conflicts require attention. Stable
merchant rotations keep historical aliases and provider labels. An empty old alias does not
create a phantom row; its return with transactions can require a fresh local identity.

Stop and reconcile is the escape hatch: stop sending, obtain remote truth, and atomically remove
the whole frozen prefix with the refresh fold. Failed and unsent intent is abandoned. Successful
item facts support status and recovery until the fold; they do not force partial rotations into
the authoritative snapshot. A failed fetch leaves the batch and intent available for another
attempt.

While a batch exists, editing, undo and redo, refresh, and another commit are unavailable.
Browsing, details, export, and recovery stay available. This restriction avoids an unfrozen
editable suffix in the durable-batch model.

## Monarch

[Monarch][source-4] uses the small REST-login and GraphQL surface Moneyflow needs. Session files
and an encrypted credential vault support reconnect and TOTP without persisting plaintext account
passwords. Binding is to the selected household identity; a swapped session must not silently
redirect a profile.

Complete reads fetch visible and hidden partitions, with pagination and identity checks before
absence can imply deletion. Posted transactions are imported; pending bank rows are excluded.
The scoped first-import commands are for quick imports into pristine profiles, not ongoing
partial reconciliation. Read the adapter's tests before changing its consistency checks; they
encode real provider responses that broke earlier assumptions.

Merchant name-addressing is distinct from local display allocation. Never send a collision
suffix as a new provider merchant name. Taxonomy management stays in Monarch. Writes cover
merchant, mapped category, hide, and transaction deletion through the durable commit path.

## YNAB

[YNAB][source-5] is a small in-repository Go HTTP client, not a generated SDK. The full
`/plans/{id}` response is joined with transaction detail under matching server knowledge and
transaction and split facts. Categories missing from the taxonomy can be kept from consistent
detail labels; do not reject usable data merely because the collection omitted a category.

- Import rejects absent or null amounts and decimal digits rather than inventing zero.
- Plan currency and scale are checked even when the plan has no transactions.
- Conversion from milliunits is exact.
- Uncleared rows keep provider-pending status. Transfers and off-budget rows supply hidden
  state.
- A split parent stays one accounting row. Child details are kept separately and their sum is
  validated.

The selected plan's SQLite binding is authoritative over a vault copy. Unlock uses the encrypted
profile vault. Moneyflow never persists a plaintext token or unlocks automatically after a
restart. Readers check the captured vault fingerprint before each HTTP request, including the
second transaction-detail request. A failed onboarding vault save requires credential re-entry;
a retry cannot bypass saving and bind new settings against an old vault.

The [transaction writer][source-12] fresh-reads a parent before each PUT or DELETE:

- Updates carry only the requested payee and category fields plus the fresh approval value.
  YNAB's [ExistingTransaction schema](https://api.ynab.com/papi/open_api_spec.yaml) inherits
  optional fields; account, date, and amount are deliberately not rewritten.
- Omitted category, mapped category, and explicit clearing are distinct.
- Mapped payees use IDs. A new-name leader persists the resulting ID before followers proceed.
- Response validation keeps unrelated transaction and split facts. Ordinary payee and category
  overrides become counted provider truth. Unknown echoed identities stay provisional until the
  immediately due refresh.
- A new-name response that claims another active local payee's identity, including one just
  claimed by another group in the batch, stops in reconcile-only attention before the conflicting
  success is recorded or followers are sent.

Transfers are refused from persisted provider facts, not the generic hidden flag. ID-addressed
payee edits also fresh-read the destination payee before PUT, and reject transfer, deleted,
missing, or malformed destinations. Vault validation runs before that read and the PUT. These
preflights cannot rule out a provider-side change after the final GET. Ordinary off-budget rows
stay writable. Split parents support payee changes and whole-parent deletion, not category
changes. Hide toggles and taxonomy administration are refused before staging and rechecked
during preparation. Uncategorized is the protected system category.

The adapter makes one attempt; the shared worker owns retries. A 429 without a usable
Retry-After parks for one hour, capped by the common 24-hour ceiling. A possibly applied PUT is
reconcile-only, while a deletion can be resent within the attempt budget. An already-absent
deletion is accepted only after verifying access to the bound plan.

Unlocking with an unfinished batch configures its writer without refreshing the frozen prefix or
dispatching from the CLI. Bound profiles never reuse a pre-write unlock snapshot for refresh;
fresh fetching belongs under the provider-operation lease. TUI and web ticks resume eligible
work. Live mutation tests need separate permission and disposable targets.

## SimpleFIN

The [SimpleFIN adapter][source-13] fetches posted transactions and adds previously unseen remote
IDs. Refresh keeps existing transaction facts, local edits, deletion records, and the complete
journal, including inactive redo. Absence from a later feed is not a deletion instruction,
because the feed's limited history cannot establish one. This is additive import, not
Monarch or YNAB snapshot reconciliation.

Edits commit locally. The adapter never sends transaction mutations to SimpleFIN. Automated
contract and synthetic workflow tests are required, but the provider stays experimental until
maintainers review authorized live-bank evidence. The [SimpleFIN guide](../guide/simplefin.md)
covers connection and history limits.

## Amazon import and matching

[CSV parsing][source-6] and the [import coordinator][source-7] feed an ordinary profile, not a
parallel Amazon database. Import is user-initiated and serialized by an advisory lock before
directory traversal or upload consumption. Uploaded bytes stage to private files rather than a
whole-file buffer in the browser server.

Observed orders are authoritative; orders absent from the input are untouched. Cancelled rows
mark an order observed but contribute no active item. Active invalid rows reject the candidate.
In-session errors can name the file, record, and column, while persisted status and history stay
counts-only.

[Reconciliation][source-8] pairs exact active identities, then exact retired identities, then
unambiguous same-ASIN and per-order ASIN-less singletons. Ambiguous leftovers retire or allocate
instead of moving categories between different purchases. Status and optional unit price do not
define identity. Unchanged input keeps committed state and revision, while operational import
history can still record the attempt.

Currency and scale bind the profile. Taxonomy cloning is a one-time committed snapshot, not live
inheritance. Amazon edits commit locally. Matching reads other profiles through short-lived
read-only snapshots, skips incompatible sources, and caches by revision.

[Matching][source-9] keeps pass exclusivity across all candidate profiles. Exact matching comes
before fuzzy matching. The fuzzy minimum is 15 major units, Python's executable constant; its
source comment said 10. Provider and display label handling, date distance, caps, and
deterministic ordering belong in the matching code and its tests.

## Bank CSV import

The [bank CSV parser][source-14] produces bounded source facts with exact amounts. The CLI
discovers files and parses each once; [the shared import planner][source-15] owns
reconciliation. SQLite installs each file's committed rows, rebased journal, identities, and CSV
state in one write transaction. No bank network adapter or credential vault is involved.

The four tables are `csv_settings`, `csv_files`, `csv_rows`, and `csv_file_rows`:

- Opaque hashes identify source transactions and file paths.
- Per-file occurrence numbers keep repeated identical purchases distinct.
- File membership records prevent one overlapping export from removing another's rows.
- `present`, `superseded`, and user-`deleted` source rows are distinct.
- The shared fold records committed merchant and category overrides; they are not inferred
  from a label comparison. Active pending edits also protect missing rows.

[JSONL transfer](../getting-started/transition.md) carries this state and validates it against
the committed profile, using the current install-only schema. The
[CSV guide](../guide/bank-csv.md) owns corrected-file policy, skipped-row behavior, limits, and
identity limitations.

## Export

[Application export][source-10] captures committed state, not the effective view. Filtered
export applies the requested analytical filters to committed rows, so pending edits can change
the displayed view without changing the export. Warn about excluded operations, without
suggesting a commit that a write batch makes unavailable.

[Exporters][source-11] consume a detached document and write CSV, Parquet, or SQLite. They do
not query profiles or providers. Capture revalidates the revision without a network refresh.
Metadata records scope, revision, and counts. CSV formula protection covers free text, including
provider transaction IDs. Generated local IDs stay unchanged. A negative amount stays a numeric
decimal string without an apostrophe.

Preview needs no export lock. Execution uses the profile's advisory export lock, private
temporary files, atomic no-overwrite publication, and cleanup on failure. The TUI reports the
completed local path; the browser delivers a download and removes the server's temporary file.
Windows close and remove ordering and stale-file cleanup are tested. Export stays available
offline and during provider batches.

[source-1]: https://github.com/wesm/moneyflow/blob/main/internal/app/provider_refresh.go
[source-2]: https://github.com/wesm/moneyflow/blob/main/internal/app/provider_write_plan.go
[source-3]: https://github.com/wesm/moneyflow/blob/main/internal/app/provider_write.go
[source-4]: https://github.com/wesm/moneyflow/tree/main/internal/provider/monarch
[source-5]: https://github.com/wesm/moneyflow/tree/main/internal/provider/ynab
[source-6]: https://github.com/wesm/moneyflow/tree/main/internal/importer/amazon
[source-7]: https://github.com/wesm/moneyflow/tree/main/internal/amazonimport
[source-8]: https://github.com/wesm/moneyflow/blob/main/internal/app/amazon_reconcile.go
[source-9]: https://github.com/wesm/moneyflow/blob/main/internal/analytics/amazon_matching.go
[source-10]: https://github.com/wesm/moneyflow/blob/main/internal/app/export.go
[source-11]: https://github.com/wesm/moneyflow/tree/main/internal/exporter
[source-12]: https://github.com/wesm/moneyflow/blob/main/internal/provider/ynab/write.go
[source-13]: https://github.com/wesm/moneyflow/tree/main/internal/provider/simplefin
[source-14]: https://github.com/wesm/moneyflow/tree/main/internal/importer/bankcsv
[source-15]: https://github.com/wesm/moneyflow/blob/main/internal/app/bankcsv_import.go
