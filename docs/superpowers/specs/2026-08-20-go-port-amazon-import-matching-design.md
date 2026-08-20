# Go Port Amazon Import and Matching Design

**Date:** 2026-08-20
**Status:** Approved
**Branch:** `go-port`

## Summary

This slice ports Python Moneyflow's Amazon order-history profile, CSV importer, and
cross-profile purchase matching to Go. Amazon becomes a first-class profile kind backed by the
ordinary v2 SQLite schema. It uses the same accounting, analytics, journal, local commit,
duplicate review, export, TUI, and web experiences as every other Go profile.

The import path accepts Amazon's official `Retail.OrderHistory.*.csv` files. It parses files into
detached ordinary-Go values, reconciles repeated and overlapping exports deterministically, and
folds one atomic plan into SQLite. It never stores raw CSV files or the shipping, billing,
payment, gift, tracking, or serial-number fields that Moneyflow does not use.

The matching path computes read-only suggestions between committed finance transactions and
committed Amazon profiles. It preserves Python's three-pass exact, gift-card, and item matching
semantics while replacing floating-point arithmetic and database-order ties with exact integer
money and deterministic ordering. Suggestions never create durable links and never mutate a
finance profile.

All three presenters are in scope:

- Cobra imports a directory into a named Amazon profile.
- TUI onboarding and repeat import accept a directory path.
- Web onboarding and repeat import stream selected files into private disposable staging.

## Relationship to Earlier Slices

This design extends:

- `2026-08-12-go-port-foundation-read-only-tui-design.md`
- `2026-08-13-go-port-read-only-web-design.md`
- `2026-08-14-go-port-sqlite-editing-design.md`
- `2026-08-15-go-port-monarch-read-refresh-design.md`
- `2026-08-17-go-port-profile-catalog-onboarding-design.md`
- `2026-08-18-go-port-monarch-write-back-design.md`
- `2026-08-18-go-port-transaction-deletion-duplicates-design.md`
- `2026-08-18-go-tui-chrome-review-info-design.md`
- `2026-08-19-go-port-transaction-export-design.md`

Those contracts remain authoritative unless this document explicitly refines them. In
particular:

- SQLite remains the source of truth.
- SQL rows and driver types remain inside `internal/store`.
- Analytics and import planning consume detached ordinary-Go slices.
- All money uses signed integer minor units plus currency and scale.
- The browser API remains stateless, profile-scoped, and base-path aware.
- Logs use a positive allowlist and never contain financial labels, file contents, or search text.
- The Go v2 profile schema remains install-only, with no migrations before stabilization.

The installed schema changes in this slice. `CurrentSchemaVersion` increments from 7 to 8 in the
same commit as the schema change. Older preview profiles are refused before any version-eight
query runs and may be recreated through the existing recovery flow. No migration or payload
rewrite is added.

This design deliberately supersedes the earlier roadmap order that placed SimpleFIN before
Amazon. Monarch is already the first usable live provider. Amazon is the next retirement blocker
because its importer and cross-profile enrichment are independently testable without another
financial-provider account.

## Goals

- Import official Amazon order-history CSV exports into a first-class Go profile.
- Support overlapping, reordered, corrected, and repeated exports without moving user intent
  between physical purchases.
- Preserve local merchant, category, hidden, notes, and journal state during reimport.
- Correct stale rows when an observed order shrinks or becomes cancelled.
- Preserve stable local transaction identities through safe corrections and resurrection.
- Keep profile currency and scale explicit and immutable after the first successful import.
- Offer a one-time advanced taxonomy clone from another compatible profile.
- Match committed Amazon orders to committed finance transactions with exact integer arithmetic.
- Restore Python's Amazon column, product-name search, and transaction-detail enrichment in TUI.
- Make the same matching and information workflows first-class in web.
- Remain responsive at approximately 100,000 imported rows.

## Non-Goals

- Scraping Amazon, calling an Amazon API, or storing Amazon credentials.
- Inferring deletion for orders wholly absent from an incremental or partial export.
- Supporting more than one currency in one Amazon profile.
- Live taxonomy inheritance or taxonomy synchronization between profiles.
- Persisting a match from a finance transaction to an Amazon order.
- Automatically changing a finance transaction from a computed match.
- Importing non-Amazon purchase-history formats or arbitrary CSV column mappings.
- Retaining raw source archives after parsing or successful import.
- Provider write-back, provider refresh scheduling, reconnect handling, or background Amazon work.
- Python-state migration, Amazon `amazon.db` migration, or schema migration.
- Adding SimpleFIN, YNAB, split transactions, or transaction-note editing.
- Removing Python, adding the final Python shim, or merging `go-port`.

## Python Parity and Named Differences

Python remains the behavioral oracle where its behavior is deliberate and safe. The following
differences are named so future reviewers do not accidentally remove them.

1. Python assigns `amz_<ASIN>_<order>` identities plus row-order sequence suffixes. Go uses a
   persisted multiset ledger and deterministic pairing, so file and traversal order cannot move
   user intent.
2. Python skips cancelled rows completely. Go treats a cancelled row as evidence that its order
   was observed, excludes it from the active item multiset, and retires previously imported rows
   no longer present in that observed order.
3. Python silently skips many malformed active rows. Go rejects the complete candidate atomically
   and reports an actionable in-session record coordinate.
4. Python defaults Quantity only when the key is absent and silently skips an explicitly blank
   value. Go treats missing or blank Quantity as `1`.
5. Python creates a truncated MD5 pseudo-ASIN from product text. Go uses a full lowercase SHA-256
   digest over the normalized product label and never displays a fake ASIN.
6. Python parses floating-point amounts with no currency binding. Go requires exact minor units,
   an explicit currency and scale, and rejects a multi-currency candidate.
7. Python resolves `amazon_categories_source` dynamically on each launch. Go clones committed
   taxonomy once, after which the profiles are independent.
8. Python stores imported filenames. Go import history stores counts, timings, revisions, and a
   canonical content digest only.
9. Python's fuzzy-matching docstring and comment say a minimum of `$10`, while the implementation
   uses `FUZZY_TOLERANCE_MIN = 15.0`. Go follows the implemented 15-major-unit rule.
10. Python accidentally appends fuzzy matches twice and relies on database order for some ties.
    Go deduplicates results and applies a total deterministic order.
11. Python returns every match. Go returns a stable first 20 plus an authoritative total count.
12. Python qualifies Amazon merchants from the current display label only. Go also consults the
    persisted raw provider label, so a user rename or collision suffix does not hide enrichment.
13. Python retains a stale transaction when a present order has fewer non-cancelled rows. Go treats
    the observed order's non-cancelled multiset as authoritative and retires the stale row.
14. Python can import mixed-currency history into one untyped amount column. Go refuses it because
    analytics and accounting require one exact currency/scale partition.

Missing orders remain untouched, matching the incremental nature of Amazon exports. Strict
account matching is not ported because the Python TUI does not expose it.

## Architecture and Dependencies

```text
cmd/moneyflow -----------------+
internal/tui ------------------+--> internal/amazonimport
web frontend --> internal/api -+            |
                                            +--> internal/profilecatalog
                                            +--> internal/importer/amazon
                                            +--> internal/app --> internal/analytics
                                                      |                  |
                                                      v                  v
                                                internal/domain <-------+

internal/app --> internal/store --> internal/store/sqlite
internal/app --> internal/profilecatalog --> internal/store/sqlite
internal/importer/amazon --> internal/domain
internal/home --> cross-platform private files and advisory locks
```

`internal/importer/amazon` owns CSV discovery, bounded streaming parse, field validation,
normalization, exact-money conversion, fingerprints, and canonical candidate ordering. It imports
no store, app, profile-catalog, API, TUI, web, Cobra, or provider package.

`internal/amazonimport` is the renderer-neutral workflow coordinator. It owns import attempt IDs,
progress, cancellation, lifecycle/import-lock acquisition, private web staging, parser invocation,
catalog rollback, and calls into the application import service. It imports
`internal/profilecatalog`, `internal/importer/amazon`, `internal/app`, and `internal/home`, but never
imports `internal/store/sqlite`. Cobra, TUI, and API presenters consume this coordinator rather than
assembling the workflow independently.

`internal/app` owns profile-kind policy, taxonomy capture, candidate reconciliation, journal
rebase, matching, and the complete fold plan. The planner is pure: it receives its snapshot,
candidate, source facts, proposed stable IDs, and committed taxonomy clone as arguments and
returns a complete plan as its only effect. It performs no SQL, filesystem I/O, network I/O,
clock reads, or callbacks into the store.

`internal/store` owns narrow atomic operations for loading Amazon state, installing an import
plan, recording counts-only operational history, and querying short-lived committed source
snapshots. SQL rows and driver types never leave it.

`internal/profilecatalog` accepts `amazon` as a manifest provider kind. It owns catalog resolution,
profile creation, rollback, and profile lifecycle locking. It does not import the Amazon parser.

Presenters collect sources, render progress and errors, and deliver bounded projections. They do
not parse CSV, calculate identities, pair rows, compare money, build store plans, or read another
profile database directly.

Architecture tests enforce:

- `internal/importer/amazon` imports only standard-library packages and `internal/domain`;
- only `internal/amazonimport` and the command factory wiring compose parser, catalog, home, and
  application import dependencies;
- `internal/store` imports no importer, app, provider, API, TUI, or web package;
- presenters never import `internal/store/sqlite` or implement reconciliation;
- matching and taxonomy source reads go through application/store interfaces;
- no Amazon code imports Monarch GraphQL or provider session packages.

## Profile and Storage Model

### Profile kind and local commit

An Amazon profile is an ordinary v2 profile whose manifest has `provider_kind: "amazon"`. It is
not a remote provider binding. It has no `provider_refresh_state`, provider-operation lease,
session file, credential vault, household identity, reconnect state, or write batch.

Amazon uses the local commit path. Merchant, category, hide, transaction-delete, category-manager,
and group-manager operations are journaled, undoable, redoable, and folded locally. Full `C` and
`G` taxonomy management and category creation are available. No operation calls Amazon.

### Amazon profile settings

Schema version 8 adds one strict singleton settings table:

```text
amazon_profile_settings
    singleton = 1
    currency: canonical three-letter uppercase code
    scale: integer 0..9
    taxonomy_source_profile_id: nullable informational profile ID
    created_at_unix_ms
```

The settings row is installed atomically with the first successful import. Currency and scale are
immutable after installation. The taxonomy source ID records provenance only; it never creates a
live dependency and may refer to a profile that is later removed.

### Source-facts ledger

Schema version 8 adds a strict Amazon item ledger independent of active transaction rows:

```text
amazon_order_items
    local_transaction_id: stable local transaction identity, primary key
    source_identity: opaque stable Amazon item token, unique
    order_id
    asin: nullable real ASIN
    asinless_key
    order_date
    product_name
    quantity
    amount_minor
    unit_price_minor: nullable
    currency
    scale
    order_status
    shipment_status
    identity_fingerprint
    full_fingerprint
    retired: boolean
```

`source_identity` is `amazon_item_` plus a lowercase base32 encoding of 128 random bits from the
injected cryptographic random source. It is opaque, never contains an order ID or product value,
and remains fixed for the lifetime of the ledger row. The external identity namespace is
`amazon/order-item`.

The ledger retains a retired row after its active transaction is removed. It is the source-side
tombstone that lets an exact reappearance recover the original local transaction ID. The ordinary
`external_identities` row is retained as well, matching the standing transaction-lineage rule.

An active ledger row has one ordinary committed transaction with the same local ID. A retired row
has none. Restoration clears `retired`, restores the ordinary transaction with the same local ID,
and refreshes its source facts.

The ordinary transaction stores:

- provider `amazon`;
- the stable opaque source identity as `provider_id`;
- one order-backed account;
- one product-backed merchant;
- the selected local category;
- exact item amount and date;
- local notes and hidden state;
- a small canonical metadata map for user-visible Amazon fields.

Order IDs and real ASINs remain user-visible source facts. Opaque local and source identities are
not derived by embedding the order ID in a local ID.

### Accounts, products, and collision policy

Each observed order maps to a stable local account whose default label is the order ID. Each real
ASIN maps to a stable local product merchant. ASIN-less rows keep the transaction's current local
merchant when safe pairing preserves the transaction; a newly allocated ASIN-less row gets a new
product merchant, shared by newly allocated rows with the same ASIN-less key.

Imported product labels use the existing sticky provider-label allocation discipline:

- the first observed active source identity owns the unsuffixed collision label;
- later distinct source identities with the same normalized collision key receive deterministic
  suffixes;
- ties in one first import use bytewise source-identity order;
- raw Amazon product labels remain persisted separately from display allocations;
- import never overwrites a user-touched local merchant label;
- a pending merchant label operation overrides the imported label in effective state.

This avoids silently coalescing distinct ASINs while preserving the v2 unique active collision-key
invariant. The raw label, not a suffix-decorated display value, feeds matching and product search.

### Import history

Schema version 8 adds counts-only operational history:

```text
amazon_import_history
    import_id
    started_at_unix_ms
    completed_at_unix_ms
    source_revision
    resulting_revision
    candidate_digest
    file_count
    logical_record_count
    blank_record_count
    cancelled_record_count
    inserted_count
    updated_count
    restored_count
    retired_count
    unchanged_count
```

History never stores filenames, paths, product labels, order IDs, ASINs, amounts, currencies,
record coordinates, or failure details. A successful no-op import may append an operational
history row outside the semantic revision. The committed tables and revision remain unchanged.

Failed or cancelled imports append no visible import-history row.

## Source Discovery, Staging, and Limits

### Directory source

The CLI and TUI accept one directory. Discovery recursively selects regular files whose basename
matches `Retail.OrderHistory.*.csv`. Relative filenames are calculated from the selected root and
sorted bytewise. Directory traversal order is never significant.

Discovery rejects:

- a redirected or non-directory root;
- symbolic links at any traversed path;
- non-regular candidate files;
- a relative path that escapes the selected root;
- duplicate relative names;
- duplicate file contents within one candidate.

### Browser source

The web presenter accepts multiple files from either a directory-capable picker or an ordinary
multiple-file fallback. Each part supplies one logical relative filename. The server validates the
name before creating a stage.

Uploads stream directly from the request body into owner-only `0600` files beneath an Amazon
staging directory. The complete request is never buffered in memory. Stages use a fixed
Moneyflow-owned prefix. Normal completion, parse failure, cancellation, request failure, and client
disconnect close handles and remove stages. Windows cleanup retries briefly after handles close;
anything still left is eligible for the next import's age-bounded stale-stage cleanup.

The source chooser may show selected relative filenames while the attempt is active. Filenames do
not enter the URL, API status response, problem body, logs, profile state, or import history.

### Fixed bounds

One attempt is bounded to:

- 256 files;
- 64 MiB per file;
- 512 MiB across all files;
- 1,000,000 logical CSV records;
- 128 columns per file;
- 1 MiB per logical CSV record;
- 16 KiB per retained field after UTF-8 decoding.

The limits apply while streaming. A rejected request does not continue consuming an unbounded body.
Ordinary tests inject smaller limits rather than allocating the production maximums.

## CSV Contract

### Encoding and records

Files are UTF-8 with an optional UTF-8 byte-order mark. Parsing follows RFC 4180, including quoted
commas, escaped quotes, CRLF, and embedded newlines. All ordering and error coordinates use the
one-based logical data-record index after the header, not a physical line number.

A fully blank logical record is skipped and counted. A partially populated record is processed
normally and rejects the complete candidate if it violates an active-row rule.

### Headers

Required headers are:

- `Order ID`
- `Order Date`
- `Product Name`
- `Quantity`
- `Total Owed`
- `Order Status`
- `Shipment Status`

Optional compatibility headers are:

- `ASIN`
- `Currency`
- `Unit Price`

Unknown columns are discarded. A duplicate header, missing required header, invalid UTF-8 header,
or header set beyond the fixed bound rejects the complete candidate.

### Retention allowlist

Only these normalized source facts survive parsing:

- order ID;
- order date;
- product name;
- quantity;
- total owed;
- real ASIN when present;
- currency when present;
- unit price when present;
- order status;
- shipment status;
- relative filename and logical record index for in-memory deterministic ordering only.

Every other source column is discarded before the application candidate is constructed. This
includes website, purchase-order number, tax, shipping charge, discounts, shipment subtotal,
condition, payment instrument, ship date, shipping option, shipping address, billing address,
carrier/tracking data, gift data, recipient contact details, and item serial number.

### Active-row validation

Every non-cancelled row requires:

- a canonical nonempty order ID;
- a valid ISO timestamp, normalized to its UTC calendar date;
- a nonempty normalized product name;
- a positive bounded integer quantity;
- an exact `Total Owed` representable at the profile scale;
- a valid optional `Unit Price` representable at the profile scale;
- valid bounded order and shipment status text;
- a nonempty real ASIN or a valid ASIN-less key;
- currency equal to the profile binding after normalization.

Missing or blank Quantity is `1`. A nonblank invalid, zero, or negative quantity rejects the
candidate.

Money accepts an optional sign and correctly grouped ASCII commas. It rejects exponents, currency
symbols, whitespace within digits, `NaN`, infinity, excess fractional precision, overflow, and
noncanonical grouping. `Total Owed` is negated into Moneyflow's signed expense convention. A
negative Amazon total therefore becomes a positive refund.

If the Currency column is absent or blank, the row is normalized to the profile binding before
fingerprinting. If it is present, it must be a canonical three-letter code equal to the binding.
The candidate is rejected atomically when non-cancelled rows contain a conflicting currency. The
user-facing error may name the conflicting currency codes because codes are not private labels.

### Cancelled-row leniency

A case-sensitive canonical `Cancelled` order-status value marks a cancelled row. It must contain
valid UTF-8, a valid order ID, and a status value sufficient to identify the observation. It does
not need a parseable date, product name, quantity, amount, unit price, ASIN, shipment status, or
matching currency.

A cancelled row imports no transaction but marks its order ID observed. Its garbage or missing
financial fields cannot reject the candidate or contribute a currency mismatch. The authoritative
non-cancelled multiset for a fully cancelled observed order is empty.

### Actionable errors and privacy

Any invalid active row rejects the complete candidate. The immediate TUI, Cobra, or web attempt
may show:

- relative filename;
- logical record number;
- column name;
- stable reason code.

This is a narrow user-facing diagnostic exception. The coordinate and filename never enter logs,
status polling, Huma problem responses, profile data, or import history. The web presenter obtains
the detailed error only through the initiating same-origin request or an instance-bound ephemeral
attempt channel; the credential-blind/counts-only status shape remains private by construction.

## Stable Identity and Reconciliation

### ASIN-less key

For a missing, blank, or `_ASINLESS_` ASIN, the parser computes:

```text
amazon:asinless:<64 lowercase hexadecimal SHA-256 characters>
```

The digest input is the product label after the same NFKC and whitespace normalization used for
durable display-label collision keys. SHA-256 is used as a stable non-secret content identifier,
not for authentication. No pseudo-ASIN is displayed.

### Two fingerprints

The canonical identity fingerprint contains:

- order ID;
- real ASIN or ASIN-less key;
- order date;
- normalized product name;
- quantity;
- signed minor amount;
- normalized profile currency;
- scale.

Unit Price and status fields are deliberately absent. This keeps identity stable when a newer
export gains the optional Unit Price column or an order advances from shipped to delivered.

The full fingerprint contains the identity fields plus:

- optional unit price in minor units;
- order status;
- shipment status.

The identity fingerprint drives pairing. The full fingerprint only decides whether provider facts
changed after a row was paired. Both are versioned canonical SHA-256 digests over length-delimited
UTF-8 fields, never delimiter-joined ambiguous text.

### Observed-order authority

An order wholly absent from an import is untouched. Absence across files is not evidence of
deletion.

When an order ID appears in any valid or cancelled record, the import's valid non-cancelled rows
for that order are authoritative. Existing active rows not paired to that multiset retire. A fully
cancelled observed order therefore retires all of its active rows. A partially cancelled order
retires only rows absent from the remaining active multiset.

### Pairing tiers

Pairing occurs independently within each observed order:

1. Pair exact identity fingerprints. Existing active rows sort by bytewise stable local
   transaction ID. Incoming equals sort by bytewise relative filename and then logical record
   number. Equal fingerprints describe interchangeable source rows, so positional pairing is safe.
2. Pair an incoming row to a retired source-facts row only on an exact identity fingerprint,
   choosing the bytewise lowest stable local ID among exact retired equals. This restores a known
   purchase before any weaker singleton inference can move an active row's user-owned state.
3. For each real ASIN, pair unequal fingerprints only when exactly one unpaired active existing
   row and one unpaired incoming row remain for that ASIN. This preserves identity through one
   unambiguous price or product correction.
4. Across the whole order, pair unequal ASIN-less rows only when exactly one unpaired active
   ASIN-less row and one unpaired incoming ASIN-less row remain. The derived key may differ after a
   product-label correction; the singleton rule deliberately ignores it.
5. Allocate fresh stable local IDs for every remaining incoming row and retire every remaining
   active existing row.

Ambiguous unequal many-to-many leftovers are never positionally paired. Losing a local association
through retirement and allocation is preferable to moving a user's category or notes to a
different physical purchase.

No pairing may link unequal identity fingerprints except through tier 3 or tier 4. This is a named
property-test invariant.

### Field authority

For a paired active transaction, import refreshes source-owned facts:

- order and ASIN facts;
- product source label;
- quantity;
- amount;
- unit price;
- order and shipment statuses;
- source date and fingerprints.

It preserves user-owned fields:

- current local merchant choice or user-touched merchant label;
- category;
- hidden state;
- notes;
- stable local transaction ID.

A source product label may update the default imported product entity only when the existing
provider-label allocation proves the entity was not user-touched and no pending label operation
overrides it.

### Journal rebase

Reconciliation loads and replays the current journal in operation order inside the authoritative
write transaction. It uses the same deterministic target-removal rewrite as provider refresh:

- a pending target whose transaction remains keeps its stable ID;
- a target whose transaction retires is removed;
- a partial batch keeps its operation identity and target order;
- an empty operation is removed;
- the count cursor decrements for every removed active operation before or at the cursor;
- inactive redo operations are treated consistently with the existing rewrite contract;
- structural operations sweep effective current membership at replay time.

The import rewrite is a named runtime journal rewrite. It carries no caller revision CAS because
parsing is outside SQLite and authoritative planning occurs against the freshest snapshot inside
the write transaction. The store still compares and advances the semantic revision atomically.

### No-op imports

If reconciliation changes no committed row, source fact, tombstone, label allocation, journal
operation, cursor, or known drill, the semantic profile revision does not increment. A counts-only
operational history record may still be appended outside the revision.

Tests assert both halves: committed tables and revision remain identical, while exactly one
successful no-op history record is appended.

## Taxonomy Initialization

Fresh Amazon profiles default to the ordinary protected Uncategorized group and category. The
advanced flow may clone taxonomy from one existing compatible profile.

The clone source is resolved by catalog ID first, then by a unique normalized display name. An
ambiguous display name is rejected. The application opens a short-lived read-only snapshot and
captures committed groups and categories only. Pending taxonomy operations in the source are
excluded. The source handle closes before the target install transaction begins.

A source taxonomy change after capture is acceptable and unobserved. There is no cross-database
transaction, cross-profile lease, or future synchronization.

Stable IDs cannot be copied across profiles because profile-local identities must remain
independent. The clone creates fresh target IDs while preserving labels, group membership,
protected-sentinel semantics, retirement exclusions, and collision ordering. Retired source
taxonomy is not cloned.

`--clone-taxonomy-from` and its wizard equivalent are valid only for first profile installation.
They are rejected clearly against an existing Amazon profile.

## Matching Engine

### Candidate profiles and qualification

Matching is a renderer-neutral, read-only application service. It reads committed finance
transactions and committed Amazon source snapshots only. Pending operations in either profile are
excluded.

An Amazon source profile must:

- be cataloged with kind `amazon`;
- open at the current schema;
- have matching currency and scale;
- yield a valid committed snapshot.

Corrupt, incompatible, newer-schema, or mismatched-money profiles are skipped with stable
in-session reason codes and counts-only diagnostics. One unusable Amazon profile never fails the
complete finance view.

A finance transaction qualifies when either its effective display merchant or its persisted raw
provider merchant label contains Unicode-lowercased `amazon` or `amzn`. Qualification deliberately
uses simple Unicode lowercasing and substring matching, not NFKC or full case folding, matching
Python's user-visible rule while preserving collision-suffixed provider identities.

### Exact money tolerances

The date range is inclusive and spans seven calendar days before through seven calendar days after
the finance transaction date.

The exact tolerance represents `0.02` major units:

- scale 0 or 1: zero minor units because `0.02` is not representable;
- scale 2: two minor units;
- scale 3 through 9: `2 * 10^(scale-2)` minor units.

The fuzzy minimum is 15 major units, exactly `15 * 10^scale` minor units. The 10% comparison uses
integer cross-multiplication rather than division or rounding. All intermediate arithmetic is
overflow-checked. No matching path converts money to floating point.

### Three global passes

For each qualifying finance transaction, candidates from every usable Amazon profile participate
in one global pass:

1. Exact order-total matches within the date and exact-money tolerances.
2. Only when pass 1 found no match in any profile, fuzzy order-total matches.
3. Only when passes 1 and 2 found no match in any profile, exact item-amount matches.

Fuzzy matching applies only when both values are expenses, the finance charge is smaller in
magnitude than the Amazon order total, and the positive difference is no more than the greater of
15 major units and 10% of the absolute order total.

Pass exclusivity is global across profiles, matching Python. A single exact match in one Amazon
profile suppresses fuzzy and item matching in every other Amazon profile.

### Confidence

Exact order matches are:

- `high` when date distance is at most two days and amount difference is strictly less than `0.01`
  major units;
- `medium` otherwise.

At scale 2, strictly less than `0.01` means an exact zero-minor-unit difference. At higher scales it
uses the exact representable one-cent threshold. At scale 0 or 1, only a zero difference qualifies.

Exact item matches are `high` within two days and `medium` otherwise. This preserves Python's
implemented behavior, which does not feed the permitted item amount difference into confidence.
Fuzzy matches are always `likely`.

### Ordering and bounds

Results are deduplicated and ordered by:

1. match class: exact order, fuzzy order, exact item;
2. absolute date distance;
3. absolute amount difference;
4. source profile ID, bytewise;
5. order ID, bytewise;
6. stable local Amazon transaction ID, bytewise.

Pass exclusivity means one response currently contains only one match class. The first key is
future-proofing and keeps the total order defined if the pass contract changes deliberately.

One projection returns at most 20 matches plus an authoritative total count. Each match returns a
bounded product-item window plus its total item count. The first product displayed in the table is
the first product of the best match under this complete deterministic order.

### Search and caching

Product search uses a Unicode-lowercased substring over the retained raw product name. It does not
use NFKC, collision-key case folding, stemming, tokenization, or fuzzy text matching.

An immutable source index may be cached by `(profile ID, semantic revision)`. Cache construction
uses a short-lived read-only committed snapshot and closes the source profile before projection.
An index is discarded when the catalog entry disappears or its revision changes.

TUI Amazon-column projection, transaction information, web detail, and search all consume this
one matching service. No renderer performs an N-by-database scan or implements its own threshold.

## Workflow and Presentation

### One action identity

`provider.refresh` remains the stable action-registry identity for refreshing a profile from its
source. Capability metadata supplies kind-specific text and interaction:

- Monarch: `Refresh provider data`, non-interactive, eligible for the six-hour scheduler.
- Amazon: `Import Amazon orders`, interactive source chooser, user-initiated only.
- Local: unavailable with a reason.

Amazon has no staleness scheduler, automatic import, provider lease, reconnect state, or background
poll. Pressing `r` always opens the source chooser. Help, action hints, web controls, and
accessibility labels show the kind-specific description.

### Add-profile workflow

Amazon is an available profile choice in TUI and web. `a` selects it within the provider chooser.
The flow is:

1. Choose Amazon.
2. Enter a profile display name.
3. Confirm currency and scale, with USD and scale 2 preselected.
4. Optionally choose a compatible taxonomy source under Advanced.
5. Select the order-history source.
6. Parse, reconcile, and atomically install settings, optional clone, facts, and rows.
7. Open the completed profile.

The currency step is always shown. A non-US user can change the preselected value even when an
older export has no Currency column.

Profile creation installs only the pristine version-eight shell and manifest before parsing. A
cancel or failure before successful installation removes a newly created profile when it contains
no durable state beyond that pristine shell and has no retained import stages. Atomic first import
prevents a half-cloned or half-bound profile.

The command is:

```text
moneyflow provider import amazon <directory> \
  [--profile <name-or-id>] [--currency USD] [--scale 2] \
  [--clone-taxonomy-from <name-or-id>]
```

Catalog ID resolution always wins. Otherwise a unique normalized display-name match is accepted.
An ambiguous display name errors rather than guessing.

When `--profile` names no existing profile, the command creates it and collects missing currency
or scale interactively. Flags override prompts. When it resolves an existing Amazon profile, the
stored currency and scale win; conflicting flags are rejected. `--clone-taxonomy-from` is rejected
for an existing profile.

Progress reports files discovered, logical records parsed, valid active rows, cancelled rows, and
insert/update/restore/retire counts. It never prints product labels, order IDs, ASINs, amounts, or
source filenames after discovery.

### TUI

TUI source selection accepts an editable directory path. The import view shows bounded counts,
elapsed time, a progress indicator when known, Cancel, and Retry after a correctable error.
Actionable CSV errors may show the relative filename, logical record, column, and reason.

On an existing Amazon profile, `r` opens the same chooser. Import success reprojects the current
analytical state while preserving the URL-equivalent query, cursor identity when it survives,
scroll when possible, and the existing all-or-nothing selection rule when a selected transaction
retires.

Amazon profiles relabel ordinary detail columns:

- Merchant becomes Product.
- Account becomes Order.

Transaction information includes real ASIN when present, quantity, order status, shipment status,
and unit price when present. It never displays an ASIN-less digest as an ASIN.

For a non-Amazon finance profile, the Amazon match column appears only when every transaction in
the current detail result qualifies. It shows the best deterministic indicator and first product.
`i` augments the existing transaction-information overlay with bounded matching orders and items.

### Web

Web follows the same profile-keyed `/p/<id>/` routing. Amazon onboarding uses the existing selector
and profile-name flow, adds exact-money and taxonomy-source steps, then accepts a directory-capable
file picker with a multiple-file fallback.

Upload start, cancellation, and installation are protected mutations. The mutation token, Origin,
and Fetch Metadata rules apply. Raw request bodies and multipart values never enter Huma problems
or logs.

The fixed web workflow is:

```text
POST /p/<id>/api/v1/amazon-import/start
POST /p/<id>/api/v1/amazon-import/<attempt-id>/files
POST /p/<id>/api/v1/amazon-import/<attempt-id>/execute
GET  /p/<id>/api/v1/amazon-import/<attempt-id>/status
POST /p/<id>/api/v1/amazon-import/<attempt-id>/cancel
```

Start records settings and the optional clone source, returning an instance-bound attempt ID and
state version. Files is a bounded multipart streaming request. Execute starts parsing and waits for
the terminal result while status exposes counts-only progress. Cancel is idempotent. Every mutation
carries the expected state version; stale or foreign attempts are rejected.

The protected execute response may carry the one ephemeral actionable CSV coordinate. A generic
Huma problem and the read-only status response carry only the stable code and counts. If the
execute connection disappears, the coordinate is not recoverable from disk.

A client disconnect observed before the authoritative immediate transaction begins cancels the
attempt. Once that transaction begins, connection loss never interrupts it: the fold completes or
rolls back atomically, and its terminal result remains visible through status.

Attempts are bound to `(server instance, profile ID)`. One profile has at most one active Amazon
attempt. An attempt expires after 30 minutes without a running job, upload, execute request, status
poll, or cancellation activity. A running parse/fold and active file stream count as activity and
cannot expire themselves.

Repeat import appears as `Import Amazon orders` in profile status. There is no scheduled Amazon
polling and no reconnect banner.

Web enables `transaction.show-info`. Pressing `i`, pressing Enter on a detail row, double-clicking,
or using the explicit Details control opens an accessible kit-ui drawer. Closing it restores
cursor, scroll, selection, and analytical URL state.

The transaction-information endpoint is a bounded read-only POST projection. It carries:

- wire version;
- canonical analytical query;
- expected semantic revision;
- stable local transaction target;
- match window;
- item window.

It does not require a mutation token. It follows the same-origin, no-CORS, no-store read contract
used by other read-only POST projections. It returns exact money strings, bounded Amazon order
facts, and authoritative totals. Order IDs may appear because they are user-visible order facts,
but they never enter URLs, logs, status records, or browser history.

Search uses the same matcher and includes product names from computed matches. The table match
column and information drawer never persist a selected match.

## Locking, Concurrency, and Atomicity

### Lock order

Schema version 8 adds `home.LockAmazonImport`, backed by `amazon-import.lock` on every platform.
The fixed order is:

```text
catalog lock during creation only
    -> shared profile lifecycle lock
        -> exclusive Amazon import lock
```

No code acquires these locks in reverse. Taxonomy-source and matching snapshots acquire only the
source profile's shared lifecycle lock and close before a target write transaction. Import and
export locks are independent siblings beneath the lifecycle lock and are never co-acquired.

The Amazon import lock is a nonblocking OS advisory lock. It is acquired before directory
traversal or HTTP body consumption. Contention returns `amazon_import_busy` without reading the
source. Process death releases it automatically. Successful, failed, and cancelled attempts release
it, allowing another import in the same process immediately.

### Transaction boundary

Directory discovery or upload staging and CSV parsing occur outside SQLite. They hold no read or
write transaction.

The authoritative fold is one immediate SQLite transaction:

1. Load the current committed profile, Amazon settings, source ledger, journal, cursor, known
   drills, and label allocations.
2. Validate first-import versus existing-profile preconditions.
3. Capture proposed stable IDs supplied by the store as planner input.
4. Invoke the closed pure reconciliation callback.
5. Replay and validate the complete effective snapshot.
6. Apply settings, taxonomy clone, source facts, transactions, entities, journal rewrite, known
   drills, and semantic revision atomically.
7. Append the counts-only operational history row.

The authoritative revision comparison and advance live inside this transaction. Any courtesy
revision check before parsing is diagnostic only.

No SQLite transaction is held while walking a directory, reading an upload, parsing CSV, waiting
for a user, rendering, or matching another profile.

### Correctness invariant

After a successful import, freshly loaded committed state equals the prior committed state adjusted
only by:

- authoritative source-field changes for paired Amazon rows;
- insertion, restoration, and retirement required by observed-order reconciliation;
- the approved taxonomy clone on first installation;
- deterministic imported entity and label allocation;
- journal target removal required for retired transactions.

User-owned category, hidden, notes, and safe merchant intent remain unchanged. A failed import
changes none of those values or the operational history.

## Failure Handling

Stable application/API codes are:

- `amazon_import_busy`: another process owns the import lock;
- `amazon_import_empty`: no eligible file or active/cancelled record was found;
- `amazon_import_too_large`: a fixed source bound was exceeded;
- `amazon_import_invalid`: CSV shape, encoding, active row, or reconciliation input is invalid;
- `amazon_currency_mismatch`: active rows conflict with the immutable binding;
- `amazon_profile_invalid`: the target is not a usable Amazon profile;
- `amazon_taxonomy_source_invalid`: the clone source cannot provide a valid committed taxonomy;
- `revision_conflict`: another semantic mutation won the authoritative transaction;
- `import_cancelled`: the TUI user canceled or the server observed a client abort;
- `store_busy`: bounded SQLite contention expired;
- `store_error`: another durable store operation failed.

`amazon_import_busy`, `revision_conflict`, and `store_busy` are safe to retry explicitly. No import
is retried automatically because the source path or browser body may no longer exist and delayed
application of a local file is surprising.

An invalid-row problem shown to the initiating session carries only the allowed relative filename,
logical record, column, and reason. Status polling remains counts-only. A restarted process cannot
recover detailed coordinates from disk and reports only the stable failure class.

An import failure leaves no partial visible profile changes or unmanaged stage. Cleanup failure is
retried briefly and then handled by the next attempt's bounded stale-stage cleanup.

## Privacy and Logging

Persistent logs and status records use a positive allowlist. They may contain only:

- profile ID;
- operation and error code;
- semantic revision;
- counts;
- durations;
- canonical candidate digest;
- correlation ID.

They never contain:

- profile display name;
- source path or filename;
- record coordinate;
- order ID or ASIN;
- product, category, group, merchant, or account label;
- amount or currency-bearing row value;
- search text;
- CSV header or field contents;
- shipping, billing, payment, gift, tracking, contact, or serial information;
- raw HTTP or multipart bodies.

The immediate source chooser and actionable error UI are user-facing display surfaces, not logs.
Tests distinguish those surfaces explicitly.

## Performance Contracts

Performance tests use deterministic synthetic data and skip under the race detector or short mode
through the existing repository mechanism.

- Pure parse plus reconciliation planning for 100,000 logical rows completes within one second in
  the standard performance job.
- Atomic installation or reimport of 100,000 Amazon rows completes within ten seconds, matching the
  provider-fold regression-budget family.
- Building or reusing the cross-profile match index and producing one bounded projection over
  100,000 Amazon rows completes within one second.
- Search over the cached match index remains within the existing one-second bounded web projection
  ceiling.
- Browser uploads remain streaming and bounded; resident memory does not scale to the 512 MiB
  aggregate source limit.

Performance thresholds are regression gates, not promises that directory enumeration or disk I/O
on every filesystem finishes within the CPU-planning ceiling.

## Verification Strategy

### Parser and staging tests

- Parse BOM, CRLF, quoted commas, escaped quotes, and embedded newlines by logical record.
- Reject duplicate or missing headers, invalid UTF-8, partial invalid records, money overflow,
  exponent notation, excess precision, malformed grouping, and invalid quantities.
- Treat missing and blank Quantity as `1`.
- Normalize missing Currency to the binding and reject conflicting active-row currencies.
- Prove a cancelled row with an invalid amount and conflicting currency still marks its order
  observed and does not reject the candidate.
- Skip and count a fully blank logical record while rejecting a partially populated invalid one.
- Exercise every file, byte, record, column, row, and retained-field limit at and across its
  boundary with injected test limits.
- Reject symlinks, redirected roots, traversal, duplicate relative names, duplicate file contents,
  and non-regular candidates.
- Stream browser bodies into `0600` stages with bounded memory and remove them after success,
  parse failure, cancellation, request error, and disconnect.
- Cover Windows sharing-violation cleanup retry and subsequent stale-stage removal.

### Import-lock tests

- Contention returns `amazon_import_busy` before directory traversal or request-body consumption.
- A subprocess holding the lock can die and a new process can import immediately.
- One process can import, release, and import again immediately.
- Lock-order tests enforce catalog during creation, then lifecycle, then import, and reject or avoid
  every reverse acquisition.
- Import and export can proceed independently because their sibling locks are never co-acquired.

### Reconciliation and storage tests

- Reordered files, records, and directory enumeration produce identical canonical candidates.
- Unit Price appearing or disappearing changes only the full fingerprint.
- Status-only changes preserve stable IDs and update provider facts.
- One unambiguous real-ASIN price correction and one whole-order ASIN-less label correction preserve
  stable IDs.
- Ambiguous unequal many-to-many rows retire and allocate rather than cross-pair.
- An observed shrink retires unmatched rows; an absent order remains untouched.
- Fully and partially cancelled observed orders retire the correct active rows.
- Exact retired reappearance restores the original local ID; changed or new source identity gets a
  fresh ID.
- An exact retired reappearance wins before an unequal active singleton candidate, preserving the
  active row's user-owned state and restoring the retired row's original local ID.
- Retired transaction external identities and source facts survive restart.
- User category, hidden, notes, and safe merchant intent survive every reimport case.
- Sticky product-label collisions are deterministic and never silently merge distinct ASINs.
- Pending journal targets survive, shrink, or disappear with correct operation identity, order,
  cursor, and counts.
- A concurrent semantic mutation and import produce exactly one authoritative revision transition;
  the loser changes nothing.
- Failure injection at every transaction step leaves committed data, settings, taxonomy, source
  facts, journal, revision, and history unchanged.
- First import installs settings, optional clone, facts, rows, and history atomically.
- An unchanged reimport leaves committed tables and semantic revision identical and appends exactly
  one counts-only no-op operational record.
- Schema inspection finds no REAL money column and enforces strict currency/scale checks.
- Version-seven and version-nine profiles are refused by a version-eight binary with the existing
  schema error codes.

A property test generates random status changes, one-row price changes, product-label edits,
record reorderings, and duplicate multiplicities. It asserts:

- no unequal identity fingerprints pair except through the real-ASIN or whole-order ASIN-less
  singleton rules;
- unchanged reimport is universally idempotent;
- every stable local ID remains attached to the same safely paired physical row.

### Taxonomy and catalog tests

- Catalog manifests accept `amazon`, show the correct local status, and never create reconnect or
  scheduled-refresh state.
- ID-first and unique-name resolution work; ambiguous normalized names fail.
- Create-on-missing and existing-profile CLI paths enforce immutable settings.
- Clone captures committed active taxonomy only and excludes pending and retired source taxonomy.
- Source changes after capture do not affect the target transaction.
- Clone allocates fresh target-local IDs and preserves group structure and sentinels.
- Clone is rejected against an existing Amazon profile.
- Cancel before a successful first import removes the pristine added profile and all stages.

### Matching tests

- Exact amount tolerances are correct for every scale from 0 through 9.
- Date bounds include exactly minus and plus seven days and exclude the adjacent days.
- Fuzzy matching is expense-only, requires a smaller-magnitude finance charge, and covers the
  15-major-unit and 10% crossover boundaries with integer arithmetic.
- One exact result in any source profile globally suppresses every fuzzy and item result.
- One fuzzy result globally suppresses every item result.
- Exact order, exact item, and fuzzy confidence follow the named thresholds.
- Results deduplicate, sort deterministically, cap at 20, and report the complete total.
- The displayed first product is stable across process and source iteration order.
- Product search uses Unicode-lowercased substring matching only.
- Display merchant and raw provider label qualification both work.
- Cache entries invalidate on source revision or catalog removal.
- A corrupt, incompatible, newer-schema, or mismatched-money Amazon profile is skipped with a stable
  reason while valid profiles still produce results.

### Error-coordinate privacy test

One invalid active row must surface relative filename, logical record, column, and reason in the
initiating TUI or web session. The same attempt must leave no filename, product label, coordinate,
or row detail in logs, status polling, Huma problems, SQLite history, or profile metadata.

### Renderer and API tests

- Cobra covers create-on-missing, existing import, prompts, flag conflicts, name ambiguity, clone,
  progress, actionable errors, cancellation, and no-op completion.
- TUI covers provider selection, currency confirmation, advanced clone, path entry, progress,
  cancel rollback, repeat `r`, failure retry, profile opening, and query/cursor preservation.
- Capability/help tests prove Monarch `r` remains scheduled refresh while Amazon `r` is interactive
  import with no background scheduler.
- Amazon profile tables use Product and Order labels and show source metadata in `i`.
- Finance TUI shows the match column only for an all-qualifying detail result and restores position
  after `i`.
- Web upload endpoints enforce mutation token, Origin, Fetch Metadata, body limits, staging cleanup,
  and profile scoping.
- Transaction-information POST accepts no mutation token, validates canonical query and expected
  revision, returns bounded no-store data, and never puts order IDs in URLs or logs.
- Web `i`, Enter, double-click, Details, close, keyboard focus, live announcements, and cursor/URL
  restoration are covered with kit-ui accessibility checks.
- Product search produces the same transaction set in TUI and web.
- Full upload/detail journeys run in Chromium. Upload, cleanup, keyboard `i`, and accessibility
  smoke tests run in Firefox and WebKit.

### Parity artifacts

Python semantic capture adds synthetic, non-private frames for:

- Amazon in the profile selector;
- an Amazon profile detail table;
- an all-Amazon finance detail table with the match column;
- product-name search;
- matching-order transaction information.

The update is deliberate through `make parity-update-python`, and the artifact diff is reviewed.
Go semantic and visual frames are updated through `make parity-update-go`. Content, ordering,
labels, key behavior, and layout invariants are compared to Python. Go-specific web output uses
DOM and browser assertions; browser screenshots and `internal/web/dist` remain ignored and are
never committed.

### Performance and repository gates

- Run the 100,000-row parser/reconciliation, SQLite fold, matching-index, and search gates.
- Run `make verify-go`, `make verify-web`, `make test-race`, and the focused store/browser suites.
- Run the complete Python test, type, format, lint, and coverage checks required by `AGENTS.md`.
- Run markdown lint and arrow-list checks for documentation.
- Scan the diff, fixtures, generated artifacts, and unpushed commits for private data.

## Completion Criteria

This slice is complete when:

- a user can create an Amazon profile and import official order-history CSVs through Cobra, TUI,
  and web;
- the profile reopens offline after restart with stable rows, exact money, local editing, local
  commit, duplicate review, and export;
- overlapping and reordered imports are deterministic and unchanged imports do not churn revision;
- cancelled and corrected observed orders reconcile without moving user intent;
- product matching, search, the match column, and transaction information work in both renderers;
- profile currency/scale and optional one-time taxonomy clone behave identically across presenters;
- all parser, lock, atomicity, privacy, property, browser, parity, and performance obligations pass;
- the installed schema is version 8 and no migration exists;
- the diff contains no Amazon scraping, network API, credential, provider-write, raw-retention,
  persisted-match, generated distribution, or screenshot code/artifact.

The next parity slice may port YNAB or SimpleFIN, or add the final Python command shim after the
remaining provider gaps close. This document does not choose that later order.
