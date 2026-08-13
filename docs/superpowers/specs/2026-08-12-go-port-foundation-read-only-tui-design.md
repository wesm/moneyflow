# Go Port Foundation and Read-Only TUI Design

**Date:** 2026-08-12

**Status:** Approved

**Branch:** `go-port`

## Purpose

moneyflow will be replaced by a Go implementation. The replacement will keep the
current terminal user experience, remove the runtime dependency on Python and
Polars, and establish a shared application core for a future first-class web
interface.

This design covers the first independently verifiable slice: a portable Go
foundation and a read-only, fixture-backed terminal user interface (TUI). It does
not cover persistence, editing, provider synchronization, or the web application.
Those areas require separate designs after this slice establishes the domain and
interaction contracts.

## Branch and Cutover Policy

All replacement work will live on the long-lived `go-port` branch until the Go
application is ready to replace the Python application. The Python implementation
will remain on that branch as a behavioral oracle during the port.

The branch will contain both implementations during the migration. Go commands
will use `go run ./cmd/moneyflow` or a binary written below the repository root,
because the existing `moneyflow/` Python package prevents a root-level file named
`moneyflow`.

The branch will not be merged into `main`, and the Python implementation will not
be removed, as part of this first slice. Updating `go-port` from `main` requires
explicit user permission each time; this design does not authorize automatic
merges or rebases.

## Goals

- Establish a small, idiomatic Go module that can grow into the full replacement.
- Express accounting and analytics as ordinary typed Go functions.
- Preserve the current read-only TUI workflows and keyboard behavior.
- Define exact, automated TUI parity at canonical terminal dimensions.
- Demonstrate interactive performance with 100,000 synthetic transactions.
- Support Linux, macOS, and Windows without CGO in this slice.
- Keep presentation, application behavior, analytics, and data sources isolated.

## Non-Goals

- SQLite persistence or migration of existing local state.
- Staged edits, undo/redo, review, commit, deletion, or export.
- Monarch Money, YNAB, SimpleFIN, or Amazon provider adapters.
- Credential storage or authentication.
- An HTTP API, background daemon, or web interface.
- Removal of Python, Polars, Textual, or the existing test suite.
- Premature interfaces or empty packages for later milestones.

## Architectural Direction

The replacement uses one typed application core with multiple adapters.

```text
Bubble Tea TUI ────────────────┐
                              │ direct Go calls
Embedded web server (later) ──┤ HTTP adapter
                              v
                       Application service
                              │
                    Pure domain and analytics
                         /             \
              SQLite store (later)   Provider adapters (later)
```

The TUI calls the application service in process. It does not communicate through
HTTP and does not require a daemon. A later `moneyflow web` command will wrap the
same application service in a loopback Huma API and serve an embedded Svelte
application, following the useful parts of Kata's web packaging model without
adopting its daemon-first lifecycle.

Provider payloads and storage records will normalize at adapter boundaries. No
provider, database, terminal, or HTTP types may appear in the public contracts of
the domain, analytics, or application packages.

### Why this direction

A daemon-first architecture creates process discovery, authentication, lifecycle,
and failure modes before moneyflow needs them. Allowing each interface to query
SQLite independently would duplicate workflows and application rules. A shared
in-process service keeps the TUI simple while giving the future web interface a
stable boundary.

## Go Foundation

The module path will be `github.com/wesm/moneyflow`. It will initially use Go
1.26.3, matching the current Kata and Docbank toolchain.

```text
cmd/moneyflow/       Cobra entry point
internal/domain/     Transactions, money, dates, dimensions, and query types
internal/analytics/  Pure filtering, grouping, sorting, and statistics
internal/app/        Application service and navigation session
internal/tui/        Bubble Tea model, rendering, key maps, and themes
internal/fixture/    Synthetic parity fixture loader
testdata/parity/     Shared inputs, expected results, scenarios, and frames
```

The first direct dependencies should be limited to Cobra, Bubble Tea v2, Bubbles
v2, Lip Gloss v2, and Testify. Packages from `go.kenn.io/kit` should be added only
when a specific capability is needed; shared ownership does not justify unused
infrastructure.

Kata and Docbank supply conventions and proven examples for command structure,
Bubble Tea v2, color handling, cross-platform testing, Make targets, version
metadata, and later Huma, SQLite, embedded-web, safe-file, logging, and release
work. Their product-specific daemon, federation, storage, and authentication
complexity will not be copied.

## Domain Model

### Transaction

A transaction is a value with these normalized fields:

- moneyflow ID and provider transaction ID
- provider identifier
- account ID and display name
- posting date as a calendar date without a timezone or time of day
- merchant ID and display name
- category ID, category name, and group name
- signed amount and currency
- hidden and pending flags
- optional provider metadata as string key/value data

Expenses remain negative and income remains positive to preserve current behavior.
Domain values are immutable by convention: analytics returns new result values and
never edits the source transaction slice.

### Money

Money uses an integer minor-unit value, an ISO currency code, and an explicit
decimal scale. It never uses `float32` or `float64`. Parsing happens at the fixture,
provider, or storage boundary and rejects values that cannot be represented
exactly at the declared scale.

Addition requires matching currency and scale. Analytics partitions results by
the complete `(currency, scale)` pair instead of silently adding unlike money
representations. The initial parity corpus
uses USD at scale two, matching the current two-decimal TUI contract.

Exact integer arithmetic is canonical when it differs from the Python pipeline's
binary floating-point result. Shared fixtures encode amounts as decimal strings
that are exactly representable at their declared scale. The Python
characterization adapter still exercises the existing `pl.Float64` pipeline, then
normalizes each displayed total to the declared scale with decimal `ROUND_HALF_UP`
and converts it to integer minor units before comparison. Comparisons use exact
minor-unit equality with no epsilon tolerance. A remaining mismatch fails the
test and requires either correcting the Python adapter or changing a fixture that
depends on a binary floating-point artifact; it must not weaken the Go result or
silently update the expectation.

### Date

The domain date represents only year, month, and day, serializes as ISO
`YYYY-MM-DD`, and validates through Go's calendar rules. It does not use local
wall-clock time for comparisons. Timezone-aware timestamps can exist in later
sync metadata but do not determine the posting-date analytics contract.

### Query and Results

A `QuerySpec` contains all state needed to compute the visible data:

- inclusive date range
- normalized search text
- hidden and transfer visibility
- zero or more merchant, category, group, account, and time drill-down filters
- result mode: detail or aggregate
- aggregation dimension and time granularity
- sort field and direction

Query results are typed detail rows or aggregate rows plus per-currency statistics.
Aggregate rows include the dimension key, transaction count, total amount, flags,
and the merchant view's top-category calculation. Time rows include typed period
keys and display labels rather than encoding dates only in strings.

## Analytics

Analytics consists of focused functions over `[]domain.Transaction`. There is no
generic dataframe, expression language, reflection-based query engine, or SQL-like
abstraction.

A query performs the following deterministic pipeline:

1. Validate and normalize the query specification.
2. Filter transactions in one linear pass.
3. Accumulate statistics and requested groups in maps.
4. Materialize typed rows.
5. Sort with explicit stable tie-breakers.
6. Return values without modifying the input.

The initial implementation recomputes a query when visible session state changes.
This keeps ownership and invalidation simple. Lowercase searchable text may be
precomputed at load time, but indexes and incremental aggregate caches require
benchmark evidence before they are added.

Every sort has a final deterministic key, such as transaction ID or the composite
aggregate identity `(dimension, key, currency, scale)`. This is required for
reproducible tests and stable cursor behavior when two rows have equal primary
values. When transactions with the same stable entity ID disagree on a display
label, aggregation chooses the lexicographically smallest label so input order
cannot change the visible result.

Hidden-row behavior is mode-specific. Detail mode retains hidden rows regardless
of the aggregate visibility toggle. Aggregate mode removes them when the toggle
is off; when it is on, they contribute to row and statistics counts but never to
totals, In/Out/Net, shares, or merchant top-category activity. Time aggregation
fills every calendar period from the observed minimum through maximum separately
for every observed `(currency, scale)` partition.

## Application Service and Session

The application service owns the loaded normalized dataset and exposes operations
in product terms: query the current view, enter a drill-down, leave a drill-down,
cycle views or time granularity, change filters, sort, and search.

Each client owns an application session. A session contains navigation history,
the current query specification, selected rows or groups, and the current view.
Keeping this state outside the renderer makes key-sequence behavior testable
without a terminal and lets future interfaces choose whether to reuse the same
navigation operations.

The Bubble Tea model owns presentation-only state such as terminal dimensions,
cursor position, focus, open input or modal state, and detected color mode. It
translates key and resize messages into session operations, obtains typed view
results, and renders them. Analytics does not return terminal cells or formatted
strings.

## First-Slice TUI Scope

The read-only Go TUI includes:

- deterministic demo startup without credentials or network access
- merchant, category, group, account, and time aggregate views
- year, month, and day time granularities
- detail view and multi-level drill-down
- sub-grouping within a drill-down
- date, hidden, and transfer filters
- incremental merchant/category search
- count, amount, date, and dimension sorting where valid
- selection behavior needed to preserve read-only navigation, without edit actions
- breadcrumbs, statistics, column headers, flags, hints, and help
- all seven current themes and `NO_COLOR`
- resize-safe layouts and a clear narrow-terminal fallback

Editing modals may appear only as explicit unavailable actions if needed to keep a
key contract understandable. They must not stage or persist changes in this slice.

## Parity Corpus

The port uses committed synthetic financial data only. No fixture, frame, example,
or expected value may contain personal financial information.

`testdata/parity/` contains five kinds of artifacts:

1. **Input fixtures:** normalized JSON transactions, categories, groups, and
   accounts loaded by both Python and Go.
2. **Logical expectations:** detail rows, aggregate rows, statistics, filter
   results, and deterministic ordering for representative query specifications.
3. **Interaction scenarios:** named initial state, key sequence, expected session
   transitions, cursor position, breadcrumb, and result identity.
4. **Semantic frames:** renderer-neutral named regions containing exact text,
   glyph positions, column boundaries, visible row order, breadcrumbs, flags,
   selection state, and hints.
5. **Go visual frames:** complete Go-rendered cell grids containing glyphs,
   resolved colors, and text attributes.

Python characterization tests adapt the shared fixture into the existing Polars
pipeline. Go tests load the same source. Both the Python extractor and Go semantic
projection execute every committed frame scenario and compare against the same
semantic artifacts; the Go renderer is not allowed to substitute its own semantic
baseline. Expected results are committed artifacts, not generated and accepted
during an ordinary test run. A deliberate regeneration command may update them,
but review must show the artifact diff.

## TUI Parity Contract

Parity uses three complementary checks.

### Behavioral parity

At every supported terminal size, the Go TUI preserves the in-scope keyboard
bindings, state transitions, calculations, navigation hierarchy, selection rules,
and information hierarchy of the Python TUI. Layout may adapt to available space.

### Semantic-frame parity

At canonical sizes, Python is the reference for renderer-neutral content and
layout invariants. The comparison covers exact visible text, semantic glyphs,
visible row ordering, column starts and alignment, breadcrumbs, flags, selection
state, statistics, and hints. The artifact represents named screen regions rather
than raw ANSI, SVG, or a complete Textual cell grid. It excludes framework-owned
chrome such as scrollbars and borders and excludes resolved color and text-style
attributes. This preserves the product contract without requiring Bubble Tea to
reimplement Textual's CSS layout and style-resolution engine.

### Go visual-frame parity

Go-generated cell grids are canonical for the Go renderer's complete appearance,
including glyphs, resolved foreground and background colors, and text attributes.
The initial Go visual frames require one-time review before acceptance. Later
changes compare exactly against those committed artifacts and may not regenerate
them silently. Theme tests therefore protect moneyflow's Go visual language
without pretending that Textual and Lip Gloss resolve styles identically.

The canonical dimensions are:

- Primary: 150 columns by 50 rows for every in-scope read-only screen and
  interaction state.
- Intermediate: 150 columns by 40 rows for representative aggregate and detail
  states. The existing Python captures at this size are account and backend setup
  screens, which are outside this slice and will enter parity with provider setup.
- Compact: 150 columns by 30 rows for drill-down, search, filter, and other compact
  layouts.
- Theme coverage: the primary merchant frame in default, berg, nord, gruvbox,
  dracula, monokai, and solarized-dark.

The minimum supported terminal is 80 columns by 24 rows. Supported noncanonical
dimensions, including 80 by 24 and 120 by 40, use behavioral and layout-invariant
tests. These checks verify that rendering does not panic, overflow its declared
frame, hide required navigation context, or leave the cursor outside the visible
result set. Below 80 by 24, the TUI may show a resize message; it must still exit
cleanly and must not panic, but full behavioral parity is not promised.

## Themes and Formatting

Theme names and visible palettes remain compatible with the Python application.
Styles resolve from an explicit theme and color mode so golden tests do not depend
on the test runner's terminal. Automatic color detection occurs only at the TUI
boundary. `NO_COLOR` takes precedence.

Amount and date formatting are pure presentation functions. They preserve the
current signs, grouping separators, two-decimal USD display, column alignment, and
sort indicators. Unicode width calculations use terminal cell width rather than
byte or rune count.

## Error Handling

Fixture loading and application construction validate all external data before
the alternate screen opens. Errors include an operation and safe field context,
and startup prints one concise message to standard error with a nonzero exit code.

After validation, pure analytics functions return results for valid query values.
Invalid query combinations are rejected at the application boundary rather than
producing partial results. User input, empty datasets, missing optional labels,
and narrow terminals must not panic. Panics are reserved for internal invariants
that indicate a programming defect.

Bubble Tea commands accept cancellation contexts. Any asynchronous response that
could outlive its initiating view carries a generation identifier so stale results
cannot overwrite newer state. The fixture-backed first slice should remain mostly
synchronous; this contract prevents later provider work from changing ownership
semantics.

## Performance Contract

The intended scale is hundreds of thousands of transactions. The first slice
includes a deterministic 100,000-transaction dataset and benchmarks for common
full queries, search, each aggregation dimension, and multi-level drill-down.

The reference target is less than 50 milliseconds for a complete filter,
aggregate, materialize, and sort operation on a documented development machine.
A performance smoke test uses a generous 500-millisecond ceiling to catch gross
regressions in continuous integration without treating noisy benchmark deltas as
correctness failures. Benchmark results and allocations are reported during the
slice handoff. Optimization must preserve the simple public function contracts.

## Developer Workflow

The Go workflow follows the useful conventions shared by Kata and Docbank:

- write a failing test before production behavior
- use a Makefile for stable build, test, lint, format, and TUI demo commands
- run `go test -shuffle=on ./...`
- run race tests, `go vet`, and golangci-lint
- inject version metadata at build time
- keep demo and test homes isolated from real user state
- run the complete suite on Linux, macOS, and Windows
- review dependency and generated-artifact diffs before commits

Python characterization commands continue to use `uv run`. Neither Go tests nor
demo commands may read real moneyflow profiles, caches, credentials, or a user's
home directory. Test helpers receive explicit temporary roots.

## Verification and Completion

This slice is complete when fresh evidence shows all of the following:

- domain and analytics unit tests pass
- property tests cover money arithmetic, date boundaries, and stable ordering
- Python and Go pass the same logical parity corpus
- application-session key scenarios pass without a terminal
- Bubble Tea interaction scenarios pass
- Python-derived semantic frames and Go semantic projections match the same approved artifacts
- Go visual cell grids match their separately approved artifacts
- adaptive-layout checks pass at noncanonical sizes
- the 100,000-transaction performance contract is met
- tests and build checks pass on Linux, macOS, and Windows
- race tests, vet, formatting, and lint checks are clean
- the resulting dependency graph and diff contain no speculative later-slice code

Completion of this slice does not authorize merging `go-port` into `main` or
removing Python. It establishes the foundation for the next design, expected to
cover the read-only Huma API and embedded Svelte web application.

## Later Replacement Slices

The remaining replacement is intentionally decomposed:

1. Huma API and a first-class read-only embedded Svelte web application over the
   fixture-backed application service.
2. SQLite profiles, local edits, undo/redo, review, commit, export, and migration,
   extended through both the TUI and web application.
3. Provider adapters ported and verified one at a time: SimpleFIN, Monarch Money,
   YNAB, and Amazon.
4. Packaging, state migration, complete parity audit, Python removal, and cutover.

The read-only web slice comes before persistence and providers because the web
experience is a primary reason for the port and is the earliest independent test
of the shared application-service boundary. Starting it with the same fixtures as
the TUI avoids coupling web architecture to storage or provider decisions. Later
slices add persistence, editing, and real data to both interfaces through that
validated service.

Each slice gets its own approved design and implementation plan. The boundaries in
this document remain stable unless a later design explicitly revises them.
