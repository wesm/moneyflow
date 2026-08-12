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
currency instead of silently adding unlike currencies. The initial parity corpus
uses USD at scale two, matching the current two-decimal TUI contract.

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

Every sort has a final deterministic key, such as transaction ID or dimension
name. This is required for reproducible tests and stable cursor behavior when two
rows have equal primary values.

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

`testdata/parity/` contains four kinds of artifacts:

1. **Input fixtures:** normalized JSON transactions, categories, groups, and
   accounts loaded by both Python and Go.
2. **Logical expectations:** detail rows, aggregate rows, statistics, filter
   results, and deterministic ordering for representative query specifications.
3. **Interaction scenarios:** named initial state, key sequence, expected session
   transitions, cursor position, breadcrumb, and result identity.
4. **Canonical frames:** normalized terminal cells containing glyphs, colors, and
   text attributes for exact comparison across renderers.

Python characterization tests adapt the shared fixture into the existing Polars
pipeline. Go tests load the same source. Expected results are committed artifacts,
not generated and accepted during an ordinary test run. A deliberate regeneration
command may update them, but review must show the artifact diff.

## TUI Parity Contract

Parity has two layers.

### Behavioral parity

At every supported terminal size, the Go TUI preserves the in-scope keyboard
bindings, state transitions, calculations, navigation hierarchy, selection rules,
and information hierarchy of the Python TUI. Layout may adapt to available space.

### Canonical-frame parity

At canonical sizes, tests compare normalized cell grids rather than ANSI byte
streams or SVG files. The normalized format records each visible glyph and its
resolved foreground, background, and text attributes, while omitting renderer
implementation details.

- Primary size: 150 columns by 50 rows, default theme.
- Compact size: 150 columns by 30 rows, default theme.
- Theme coverage: the primary merchant frame in default, berg, nord, gruvbox,
  dracula, monokai, and solarized-dark.

The primary size covers every in-scope screen and interaction state represented by
the current documentation workflow. The compact size covers drill-down, search,
filter, and other layouts currently captured at 150 by 30. The normalized Python
frames are the initial reference. If framework differences make a cell-level
difference desirable, the Go frame must be reviewed and explicitly accepted as a
new canonical artifact rather than silently updated.

Noncanonical dimensions use behavioral and layout-invariant tests. These checks
include 80 by 24 and 120 by 40 terminals and verify that rendering does not panic,
overflow its declared frame, hide required navigation context, or leave the cursor
outside the visible result set.

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
- canonical normalized frames match their approved artifacts
- adaptive-layout checks pass at noncanonical sizes
- the 100,000-transaction performance contract is met
- tests and build checks pass on Linux, macOS, and Windows
- race tests, vet, formatting, and lint checks are clean
- the resulting dependency graph and diff contain no speculative later-slice code

Completion of this slice does not authorize merging `go-port` into `main` or
removing Python. It establishes the foundation for the next design, expected to
cover SQLite persistence and local editing.

## Later Replacement Slices

The remaining replacement is intentionally decomposed:

1. SQLite profiles, local edits, undo/redo, review, commit, export, and migration.
2. Provider adapters ported and verified one at a time: SimpleFIN, Monarch Money,
   YNAB, and Amazon.
3. Huma API and a first-class embedded Svelte web application.
4. Packaging, state migration, complete parity audit, Python removal, and cutover.

Each slice gets its own approved design and implementation plan. The boundaries in
this document remain stable unless a later design explicitly revises them.
