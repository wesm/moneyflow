# Go Port Foundation and Read-Only TUI Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development
> (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use
> checkbox (`- [ ]`) syntax for tracking.

**Goal:** Deliver a portable, fixture-backed Go executable whose accounting, analytics,
application-session behavior, and read-only Bubble Tea TUI satisfy the approved Python/Go parity
contracts at up to 100,000 transactions.

**Architecture:** A Cobra entry point loads validated synthetic transactions into an in-process
application service. The service owns immutable normalized data and evaluates pure typed analytics
for a client-owned session. Bubble Tea owns only terminal and overlay state, renders through a small
explicit cell-frame layer, and never imports Python, Polars, providers, persistence, or HTTP
concerns.

**Tech Stack:** Go 1.26.3; Cobra 1.10.2; Bubble Tea v2.0.8; Bubbles v2.1.1; Lip Gloss v2.0.5;
Testify v1.11.1; the existing Python 3.11/uv/Polars/Textual stack as the characterization oracle.

## Global Constraints

- Work only on the already checked-out `go-port` branch. Do not switch branches, pull, merge,
  rebase, push, remove Python, or merge to `main` without explicit user permission.
- Use only committed synthetic data under `testdata/parity/`. Tests, demos, and benchmarks must not
  read a real profile, credential, cache, home directory, or network service.
- Keep this slice read-only. Do not add SQLite, provider adapters, credentials, HTTP, Svelte, edits,
  undo/redo, commit, deletion, export, or speculative interfaces for those later slices.
- Use integer minor units everywhere after an input boundary. Production Go code must not represent
  money with `float32` or `float64`.
- Preserve source transaction slices. Constructors defensively copy slices and maps; analytics
  returns new values.
- Treat `moneyflow/tui/app.py:139-176` (`MoneyflowApp.BINDINGS`) as the current runtime key
  contract. `moneyflow/tui/keybindings.py` is stale: it documents `u` as detail and `M`/`c` as
  direct views, while runtime uses `d` for detail, `u` for undo, `c` for edit category, and has no
  `M` binding. Correct the Python help data before generating semantic artifacts; do not reproduce
  that documentation bug in Go.
- Preserve current read-only filtering details: search is a case-insensitive regular-expression
  match over merchant and category, following Polars `str.contains`; transfers are rows whose group
  is `Transfers`; hidden rows remain visible in detail even when the aggregate hidden toggle is off;
  hidden rows never contribute to totals or In/Out/Net.
- Exact minor-unit arithmetic is canonical. The Python adapter converts a Polars total with
  `Decimal(str(value)).quantize(quantum, rounding=ROUND_HALF_UP)` and then compares exact integers.
  Do not add epsilon comparisons.
- Ordinary tests are read-only with respect to committed artifacts. Artifact updates require an
  explicit update command and human review of the diff.
- Use `apply_patch` for hand-edited files. Generated parity artifacts may be written only by their
  deliberate generator commands.
- Keep `CLAUDE.md` as the existing symlink to `AGENTS.md`.

## Target File Map

```text
go.mod / go.sum                      Go module and locked dependencies
Makefile                             Stable Go build/test/parity/demo targets
.golangci.yml                        Small explicit lint policy
cmd/moneyflow/                       Cobra root, version, and TUI startup
internal/version/                    Linker-injected build metadata
internal/domain/                     Money, date, transaction, query/result values
internal/fixture/                    Shared JSON loader and synthetic 100k generator
internal/analytics/                  Pure filter, statistics, grouping, sort, query
internal/app/                        Dataset service and renderer-neutral sessions
internal/tui/                        Bubble Tea model, cells, layout, themes, overlays
internal/parity/                     Versioned artifact schemas and Go comparison helpers
moneyflow/parity/                    Python fixture/oracle/semantic-frame adapter
tests/parity/                        Python characterization tests
testdata/parity/                     Inputs, cases, scenarios, semantic and Go frames
.github/workflows/go.yml             Cross-platform Go, race, lint, parity CI
docs/superpowers/benchmarks/         Recorded first-slice performance evidence
```

Do not create storage, provider, API, or web directories in this slice.

## Required Commit Gate

Run the focused red/green command named in each task while developing. Before **every** task commit,
run this complete gate from the repository root:

```bash
go test -shuffle=on ./...
go vet ./...
test -z "$(gofmt -l cmd internal)"
uv run pytest -v
uv run pyright moneyflow/
uv run ruff format --check moneyflow/ tests/
uv run ruff check moneyflow/ tests/
npx --yes markdownlint-cli --config .markdownlint.json README.md 'docs/**/*.md'
.github/scripts/check-arrow-lists.sh
```

Expected: every command exits zero; pytest reports no failures; Pyright reports zero errors;
`gofmt -l` emits nothing. If `golangci-lint` is installed, also run
`golangci-lint run --config .golangci.yml`; CI will run the pinned action regardless of local
availability.

Never commit a regenerated artifact without reviewing `git diff -- testdata/parity`. Use the
`kenn:commit` skill before each `git commit`, stage only the task's files, and include the verified
attribution required by that skill.

---

## Task 1: Establish the Go Module, CLI, and Developer Commands

**Files:**

- Create: `go.mod`
- Create: `go.sum`
- Create: `Makefile`
- Create: `.golangci.yml`
- Create: `cmd/moneyflow/main.go`
- Create: `cmd/moneyflow/root.go`
- Create: `cmd/moneyflow/root_test.go`
- Create: `internal/version/version.go`
- Modify: `.gitignore`

- [ ] Create `go.mod` with module `github.com/wesm/moneyflow`, `go 1.26.3`, and only these direct
      requirements initially:

  ```text
  github.com/spf13/cobra v1.10.2
  github.com/stretchr/testify v1.11.1
  ```

  Add the three approved Charm dependencies at their pinned versions only when Tasks 9 and 11 first
  import them; `go mod tidy` must not retain unused dependencies.

- [ ] Write failing command tests first. Cover deterministic help, `version`, unknown-command
      failure, and dependency injection of stdout/stderr. The construction seam is:

  ```go
  func newRootCommand(streams IOStreams) *cobra.Command

  type IOStreams struct {
      In  io.Reader
      Out io.Writer
      Err io.Writer
  }
  ```

  Run `go test ./cmd/moneyflow -run 'TestRoot(CommandHelp|Version|RejectsUnknownCommand)' -count=1`.
  Expected: failure because `newRootCommand` and version metadata do not exist.

- [ ] Implement the smallest Cobra root. Until Task 11 wires the TUI, invoking with no arguments
      prints help and exits zero. Add `moneyflow version` backed by linker-set
      `internal/version.Version`, `Commit`, and `BuildDate`, each with safe development defaults.

- [ ] Add Make targets `build`, `test`, `test-race`, `vet`, `lint`, `fmt`, `tui-demo`, and `clean`.
      `build` writes `bin/moneyflow` (or `bin/moneyflow.exe` on Windows); it must never try to
      create a root file named `moneyflow`. Add `/bin/`, `/coverage.out`, and
      `/.cache/golangci-lint/` to `.gitignore`.

- [ ] Use a compact `.golangci.yml` with an explicit linter set; do not copy Kata's product-specific
      exclusions. Run `go mod tidy`, the focused tests, `make build`, and `./bin/moneyflow version`.
      Expected: tests pass and the command reports development metadata without reading user state.

- [ ] Run the Required Commit Gate, review `go.mod`/`go.sum` for unintended dependencies, and commit
      with message `feat: establish Go command foundation`.

## Task 2: Implement Exact Money and Calendar Dates

**Files:**

- Create: `internal/domain/money.go`
- Create: `internal/domain/money_test.go`
- Create: `internal/domain/date.go`
- Create: `internal/domain/date_test.go`

- [ ] Write table and property tests first for exact decimal parsing, negative and positive values,
      scale zero/two/four, leading zeros, malformed values, excess precision, `int64` overflow,
      mismatched currency/scale addition, addition overflow, comparison, absolute value, and
      canonical decimal serialization. Use `testing/quick` for `a + b - b == a` within
      non-overflowing ranges.

  The public value contract is:

  ```go
  type Currency string

  type Money struct {
      Minor    int64
      Currency Currency
      Scale    uint8
  }

  func ParseMoney(decimal string, currency Currency, scale uint8) (Money, error)
  func (m Money) Add(other Money) (Money, error)
  func (m Money) Sub(other Money) (Money, error)
  func (m Money) Abs() (Money, error)
  func (m Money) Cmp(other Money) (int, error)
  func (m Money) DecimalString() string
  ```

  Run `go test ./internal/domain -run 'Test(ParseMoney|Money)' -count=1`. Expected: compile failure
  because the types do not exist.

- [ ] Implement parsing with string/integer operations only. Reject signs without digits, scientific
      notation, commas, whitespace, more fractional digits than `scale`, empty currency, and
      overflow. Permit fewer fractional digits by right-padding to the declared scale. Use checked
      arithmetic for every sum, subtraction, negation, and absolute value.

- [ ] Write date tests first for ISO parsing/JSON, invalid days and leap years, ordering, equality,
      and `AddDays` across month/year/leap boundaries. Use this immutable API:

  ```go
  type Date struct { /* private year, month, day */ }

  func NewDate(year int, month time.Month, day int) (Date, error)
  func ParseDate(iso string) (Date, error)
  func (d Date) Year() int
  func (d Date) Month() time.Month
  func (d Date) Day() int
  func (d Date) Compare(other Date) int
  func (d Date) AddDays(delta int) (Date, error)
  func (d Date) String() string
  ```

- [ ] Implement `Date` by validating years 1-9999 through UTC `time.Date` round trips, while
      exposing no time zone or time-of-day in its value or JSON contract. `AddDays` returns an error
      rather than crossing that supported range. Run `go test ./internal/domain -count=1`. Expected:
      pass.

- [ ] Run the Required Commit Gate and commit with message `feat: add exact money and date values`.

## Task 3: Define Transactions, Queries, and Typed Results

**Files:**

- Create: `internal/domain/transaction.go`
- Create: `internal/domain/transaction_test.go`
- Create: `internal/domain/query.go`
- Create: `internal/domain/query_test.go`

- [ ] Write constructor tests for required IDs/labels, defensive copying of metadata, valid
      normalized transactions, and rejected empty IDs, invalid currency, or malformed references.
      Use value types rather than provider maps:

  ```go
  type EntityRef struct {
      ID   string `json:"id"`
      Name string `json:"name"`
  }

  type CategoryRef struct {
      ID    string `json:"id"`
      Name  string `json:"name"`
      Group string `json:"group"`
  }

  type Transaction struct {
      ID, ProviderID, Provider string
      Account                  EntityRef
      Date                     Date
      Merchant                 EntityRef
      Category                 CategoryRef
      Amount                   Money
      Notes                    string
      Hidden, Pending          bool
      Metadata                 map[string]string
  }
  ```

- [ ] Write query validation tests before implementation. Define string-backed `Dimension`,
      `ResultMode`, `TimeGranularity`, `SortField`, and `SortDirection` types with constants,
      `Valid` methods, and JSON text marshaling. The five aggregate dimensions are merchant,
      category, group, account, and time; detail is a result mode, not a sixth dimension.

- [ ] Implement these query values and reject incompatible combinations at the application boundary:

  ```go
  type DateRange struct { Start, End Date }
  type Period struct { Granularity TimeGranularity; Year, Month, Day int }
  type Drilldown struct { Dimension Dimension; Key, Label string; Period *Period }
  type SortSpec struct { Field SortField; Direction SortDirection }
  type QuerySpec struct {
      DateRange       *DateRange
      Search          string
      ShowHidden      bool
      ShowTransfers   bool
      Drilldowns      []Drilldown
      Mode            ResultMode
      GroupBy         Dimension
      TimeGranularity TimeGranularity
      Sort            SortSpec
  }
  ```

  Validation must require both date bounds with start no later than end, unique drill-down
  dimensions, a typed period only for time, a valid aggregation dimension in aggregate mode, and a
  sort field valid for the requested result shape.

- [ ] Define renderer-neutral output values: `DetailRow`, `AggregateRow`, `RowFlags`,
      `CurrencyStats`, and `QueryResult`. `AggregateRow` includes `Key`, `Label`, `Count`, exact
      `Total`, a typed optional `Period`, merchant-only `TopCategory`/whole-percent value, and a
      signed-share value in tenths of a percent. `QueryResult` includes exactly one populated row
      variant, filtered count, filtered date range, and currency-partitioned statistics.

- [ ] Add deep-copy helpers for `QuerySpec`, `Transaction`, and result slices so service/session
      boundaries cannot mutate owned data. Run `go test ./internal/domain -count=1`. Expected: pass.

- [ ] Run the Required Commit Gate and commit with message
      `feat: define Go transaction and query contracts`.

## Task 4: Build the Shared Fixture and Python Logical Oracle

**Files:**

- Create: `testdata/parity/transactions.json`
- Create: `testdata/parity/logical_cases.json`
- Create: `testdata/parity/logical_expectations.json` (generated deliberately)
- Create: `internal/fixture/document.go`
- Create: `internal/fixture/document_test.go`
- Create: `moneyflow/parity/__init__.py`
- Create: `moneyflow/parity/fixture.py`
- Create: `moneyflow/parity/logical.py`
- Create: `tests/parity/__init__.py`
- Create: `tests/parity/test_fixture.py`
- Create: `tests/parity/test_logical.py`

- [ ] Specify a versioned JSON document with declared currency scales and decimal-string amounts.
      Create 30-50 plainly synthetic transactions using labels such as `Example Grocer`,
      `Example Payroll`, `Everyday Card`, and `Transfers`; never copy demo or personal
      merchant/account data. Cover income, expense, zero, hidden, pending, transfer, uncategorized,
      multiple accounts/categories, year/month/day gaps, leap day, and equal-value sort ties. Keep
      the parity corpus USD/scale two.

- [ ] Write failing Go loader tests for valid loading and for duplicate IDs, undeclared currency,
      invalid amount/date, empty labels, unknown schema version, and metadata aliasing.
      `Load(path string) ([]domain.Transaction, error)` must wrap errors with the operation and safe
      JSON field/index, never a whole financial record.

- [ ] Implement the Go loader with `encoding/json.Decoder.DisallowUnknownFields`, domain
      constructors, deterministic order preservation, and defensive copies. Run
      `go test ./internal/fixture -count=1`. Expected: pass.

- [ ] Write failing Python tests that load the same document, validate the same boundary rules, and
      adapt it to the exact existing Polars schema in `moneyflow/data/data_manager.py`. The adapter
      must not create a second accounting implementation; it must invoke `AppState`, `DataManager`,
      and `ViewPresenter` behavior.

- [ ] Implement `logical_cases.json` with named cases for all five aggregate modes; year/month/day
      gaps; detail; inclusive date bounds; literal and regular-expression merchant/category search;
      transfers off/on; hidden off/on in aggregate; hidden retained in detail; multi-level
      drill-down; and every valid sort cycle. Each case contains only input query/session state,
      never expected output.

- [ ] Implement `uv run python -m moneyflow.parity.logical --check` to compare current oracle output
      with the committed expectation and `--update` to write it deliberately. Normalize every Polars
      amount through:

  ```python
  quantum = Decimal(1).scaleb(-scale)
  minor = int(
      Decimal(str(polars_value)).quantize(quantum, rounding=ROUND_HALF_UP)
      * (10**scale)
  )
  ```

  Canonical JSON uses sorted object keys and a trailing newline. Normal tests call check mode only.

- [ ] Run `uv run python -m moneyflow.parity.logical --update`, inspect the entire generated diff
      for generic data and correct expectations, then run `uv run pytest tests/parity -v` and
      `uv run python -m moneyflow.parity.logical --check`. Expected: pass with no file changes in
      check mode.

- [ ] Run the Required Commit Gate and commit with message
      `test: establish shared Go parity corpus`.

## Task 5: Implement Filtering, Detail Rows, and Statistics

**Files:**

- Create: `internal/analytics/filter.go`
- Create: `internal/analytics/filter_test.go`
- Create: `internal/analytics/statistics.go`
- Create: `internal/analytics/statistics_test.go`
- Create: `internal/analytics/detail.go`
- Create: `internal/analytics/detail_test.go`
- Create: `internal/analytics/testdata_test.go`

- [ ] Write failing table tests for the exact pipeline order and current semantics. Search compiles
      one case-insensitive Go regular expression and applies it to merchant and category only;
      invalid expressions return a validation error. Date bounds are inclusive. Transfer filtering
      uses category group `Transfers`. Drill-down predicates intersect. `ShowHidden=false` removes
      hidden rows only for aggregate mode; detail retains hidden rows. Verify the input slice and
      metadata are unchanged.

- [ ] Implement one linear filter pass:

  ```go
  func Filter(transactions []domain.Transaction, spec domain.QuerySpec) ([]domain.Transaction, error)
  ```

  Compile search once with the standard-library `regexp` package and case-insensitive semantics
  matching the Python oracle; do not build a custom regex engine or index. Return an empty non-nil
  slice for no matches.

- [ ] Write failing statistics tests for empty data, income only, expenses only, hidden exclusion
      from money totals, count inclusion, exact net, multiple currencies/scales partitioned and
      sorted by currency/scale, and overflow propagation.

- [ ] Implement:

  ```go
  func Statistics(filtered []domain.Transaction) ([]domain.CurrencyStats, error)
  ```

  `In` is the sum of positive non-hidden amounts, `Out` the signed sum of negative non-hidden
  amounts, and `Net = In + Out`. Never combine currencies or scales.

- [ ] Write detail materialization/sort tests for date, merchant, category, account, and amount.
      Preserve Python's amount direction convention: user `desc` puts the most-negative expenses
      first, while other fields use ordinary descending order. Use transaction ID ascending as the
      final tie-breaker for every detail sort.

- [ ] Implement `DetailRows(filtered, sort)` without mutating `filtered`. Add selected/hidden flags
      as a separate decoration step so analytics remains independent of session selection.

- [ ] Add Go tests that load each detail/filter/statistics case from `logical_expectations.json` and
      compare exact minor units and ordered IDs. Run
      `go test ./internal/analytics -run 'Test(Filter|Statistics|Detail|Logical)' -count=1`.
      Expected: pass.

- [ ] Run the Required Commit Gate and commit with message
      `feat: add pure transaction filtering and statistics`.

## Task 6: Implement Aggregation and the Complete Query Pipeline

**Files:**

- Create: `internal/analytics/aggregate.go`
- Create: `internal/analytics/aggregate_test.go`
- Create: `internal/analytics/sort.go`
- Create: `internal/analytics/sort_test.go`
- Create: `internal/analytics/query.go`
- Create: `internal/analytics/query_test.go`

- [ ] Write failing aggregation tests for merchant, category, group, and account. Counts include the
      visible rows; totals exclude hidden rows. Groups use display labels, matching the current
      Python behavior. Merchant top category uses the greatest sum of absolute, non-hidden activity;
      use label ascending as its deterministic tie-breaker. Avoid a half-way percentage tie in the
      parity fixture, and unit-test the chosen integer half-up rounding separately.

- [ ] Implement dimension-specific accumulators over the filtered slice. Do not create a generic
      expression engine. Accumulators are keyed by `(currency, scale, dimension key)` so unlike
      money is never combined. Materialize one row per partition and return checked-overflow errors.

- [ ] Write time aggregation tests first for year/month/day keys, leap day, gaps, one-row ranges,
      empty data, and filtered endpoints. Fill every period from the earliest through latest
      **filtered** transaction, inclusive, with zero-count/zero-total rows for every observed
      currency/scale partition. Typed `Period` values drive filtering and navigation; labels are
      `2024`, `Mar 2024`, and `2024-03-15` only at presentation time.

- [ ] Implement time gap filling with calendar increments over `domain.Date`; do not import
      `time.Location`, `dateutil`, or strings as the primary period key.

- [ ] Write sort tests for all compatible aggregate fields and deterministic equal-primary ordering.
      Preserve amount inversion (`desc` means most-negative expense first); ordinary count/name/time
      sorts follow their displayed direction. Always finish with currency, scale, label, and typed
      period keys as appropriate.

- [ ] Implement signed share-of-income/share-of-expense in tenths of a percent using integer
      division with explicit half-up rounding. Positive rows divide by positive totals; negative
      rows divide absolute values by total absolute expenses; zero is `0.0%`.

- [ ] Write `Query` tests that prove validation happens before work, the stage order is filter →
      statistics/group/detail → stable sort, only the requested row variant is populated, date
      range/count describe the filtered view, empty results are valid, and source input is
      unchanged:

  ```go
  func Query(transactions []domain.Transaction, spec domain.QuerySpec) (domain.QueryResult, error)
  ```

- [ ] Compare every case in `logical_expectations.json` from Go. A mismatch is a failing test; do
      not change expectations until the Python adapter and fixture have been inspected. Run
      `go test ./internal/analytics -count=1`. Expected: pass with exact minor-unit and ordering
      equality.

- [ ] Run the Required Commit Gate and commit with message
      `feat: implement deterministic financial analytics`.

## Task 7: Add the Application Service and Navigation Session

**Files:**

- Create: `internal/app/service.go`
- Create: `internal/app/service_test.go`
- Create: `internal/app/session.go`
- Create: `internal/app/session_test.go`
- Create: `internal/app/navigation.go`
- Create: `internal/app/navigation_test.go`

- [ ] Write service ownership tests first. `NewService` validates and defensively copies
      transactions; `Transactions` is not exposed; `Query` accepts a session snapshot and delegates
      to analytics. Mutation of caller data after construction or returned result data after a query
      must not affect later queries.

  ```go
  type Service struct { /* owned immutable transactions */ }

  func NewService(transactions []domain.Transaction) (*Service, error)
  func (s *Service) Query(session Session) (domain.QueryResult, error)
  ```

- [ ] Define a renderer-neutral `Session` with current mode/dimension, sub-grouping, time
      granularity, ordered drill-downs, date/search/visibility filters, sort, selection sets, and
      private navigation history. `NewSession()` starts in merchant aggregation, amount descending,
      year granularity, transfers hidden, and hidden rows shown.

- [ ] Write state-transition tests before methods. Top-level `CycleGrouping` is merchant → category
      → group → account → time → merchant. Entering time defaults to period ascending; leaving it
      resets amount descending. `ShowAllDetail` corresponds to runtime `d` and defaults to date
      descending. `SwitchAccounts` corresponds to runtime `A`. Do not add nonexistent runtime
      `M`/direct-category bindings.

- [ ] Implement pure session operations that return a new result or mutate only the receiver and
      never query the terminal:

  ```go
  func (s *Session) CycleGrouping()
  func (s *Session) ShowAllDetail()
  func (s *Session) SwitchAccounts()
  func (s *Session) ToggleTimeGranularity()
  func (s *Session) NavigatePeriod(delta int) bool
  func (s *Session) ClearTimePeriod() bool
  func (s *Session) SetSearch(query string)
  func (s *Session) SetFilters(filters Filters) error
  ```

- [ ] Write navigation-history tests for one-level, multi-level, time, and sub-grouping drill-downs.
      Renderer position crosses the boundary only as an opaque value supplied on entry and returned
      on back:

  ```go
  type ViewPosition struct { Cursor, Scroll int }
  func (s *Session) Drill(row domain.AggregateRow, position ViewPosition) error
  func (s *Session) Back() (ViewPosition, bool)
  ```

  Back first clears an unchanged active search, then leaves active sub-grouping while restoring its
  sort, then restores the full parent snapshot and cursor/scroll. The drill order must be retained
  for breadcrumbs.

- [ ] Implement sub-group cycling over dimensions not already selected, followed by detail. Validate
      sorts on each transition: count/amount remain valid; date only in detail; time defaults to
      period ascending; incompatible aggregate-field sorts reset to amount descending.

- [ ] Run `go test ./internal/app -count=1`. Expected: pass, including tests that query through the
      service after every transition.

- [ ] Run the Required Commit Gate and commit with message
      `feat: add application sessions and navigation`.

## Task 8: Lock Sorting, Selection, Breadcrumb, and Interaction Scenarios

**Files:**

- Create: `testdata/parity/interaction_scenarios.json`
- Create: `internal/parity/schema.go`
- Create: `internal/parity/schema_test.go`
- Create: `internal/app/interaction_test.go`
- Modify: `internal/app/session.go`
- Modify: `internal/app/session_test.go`

- [ ] Define a versioned interaction schema containing initial session state, named operations, row
      identity rather than row index, supplied cursor/scroll, and the exact expected state after
      each operation. Include scenarios for all top-level views, all sort cycles, reverse sort,
      detail, time drill/navigation, sub-grouping, multi-level drill/back, search/back, filters,
      single selection, and select-all toggle.

- [ ] Write failing schema tests for unknown versions/operations, invalid enum text, missing
      expected states, and nondeterministic row-index targeting. Implement strict JSON decoding in
      `internal/parity`; it may depend on domain/app values but must not be imported by production
      service or TUI code.

- [ ] Write failing session tests for the exact sort cycles:
  - detail: date → merchant → category → account → amount → date
  - time: period → count → amount → period
  - other aggregate: current dimension → count → amount → current dimension

  Reversing changes only direction. Aggregate-to-detail converts invalid count to date descending;
  time-period to detail converts to amount descending.

- [ ] Implement transaction and aggregate selection with independent sets. Toggle one visible
      identity; select-all selects every currently visible identity if any is unselected and clears
      all visible identities otherwise. Query decoration emits `✓` and `H`; it never emits
      pending-edit `*` in this read-only slice.

- [ ] Implement breadcrumb construction in `internal/app` from typed state and query date range.
      Preserve Python abbreviations and drill order: top-level plural view plus date range, then
      `T:`, `M:`, `C:`, `G:`, or `A:` segments joined by `>`. Append sub-group labels such as
      `(by Category)` without relying on renderer width.

- [ ] Replay every committed scenario against `Service`/`Session`, resolve target rows by identity,
      and compare state, result IDs, breadcrumb, selection, and returned cursor position after each
      step. Run `go test ./internal/app ./internal/parity -count=1`. Expected: pass.

- [ ] Run the Required Commit Gate, inspect the scenario data for synthetic-only labels, and commit
      with message `test: lock Go application interaction behavior`.

## Task 9: Implement Pure Formatting and Seven Explicit Themes

**Files:**

- Create: `internal/tui/format.go`
- Create: `internal/tui/format_test.go`
- Create: `internal/tui/columns.go`
- Create: `internal/tui/columns_test.go`
- Create: `internal/tui/theme.go`
- Create: `internal/tui/theme_test.go`
- Modify: `moneyflow/tui/keybindings.py`
- Modify: `tests/test_keybindings.py`

- [ ] First add a Python regression test that derives runtime key/action pairs from
      `MoneyflowApp.BINDINGS` and asserts the help dataset uses the same keys/actions. Fix
      `moneyflow/tui/keybindings.py` to describe the real runtime bindings (`d` detail, `u` undo; no
      `M`; `c` edit category). Keep edit/system entries because they remain part of current help,
      but later Go presentation marks their actions unavailable. Run
      `uv run pytest tests/test_keybindings.py -v`. Expected: red before the correction, green after
      it.

- [ ] Add `charm.land/lipgloss/v2 v2.0.5`, then write Go formatting tests for exact amount
      signs/grouping/two-decimal USD display (`-1,234.56`, `+5,000.00`, `+0.00`), month/day/year
      labels, whole/tenths percentages, transaction flags, aggregate flags, stats text, sort arrows,
      truncation, and empty-state text. Formatting accepts exact domain values only.

- [ ] Implement pure presentation functions. Stats must render
      `N txns | In: ... | Out: ... | Net: ...`; empty data renders `0 txns | No data in view`. No
      formatter reads environment variables or terminal capabilities.

- [ ] Write column tests for aggregate and detail layouts at 150, 120, and 80 columns. Preserve the
      visible columns and right alignment from `ViewPresenter`: aggregate
      name/count/total/%/merchant top-category/flags and detail
      date/merchant/category/account/amount/flags. Width calculations use `lipgloss.Width`, never
      bytes or raw rune count. The table algorithm must return explicit column starts for
      semantic-frame comparison.

- [ ] Define `ThemeName`, `ColorMode`, `Style`, and `Palette`. Add exactly `default`, `berg`,
      `nord`, `gruvbox`, `dracula`, `solarized-dark`, and `monokai`. Map semantic roles (background,
      panel, border, heading, text, muted, selection, positive, warning, hidden) from the
      corresponding TCSS palettes, but make Go values explicit rather than parsing TCSS at runtime.

- [ ] Test every theme name, unknown-theme errors, deterministic truecolor/256/16/no-color
      resolution, and `NO_COLOR` precedence in a boundary-only
      `ResolveColorMode(env, terminalProfile)` helper. The explicit color mode passed by tests
      always wins over auto-detection; no-color strips color while preserving glyphs and attributes
      that remain meaningful.

- [ ] Run `go test ./internal/tui -run 'Test(Format|Columns|Theme|Color)' -count=1`. Expected: pass.

- [ ] Run the Required Commit Gate and commit with message
      `feat: add Go presentation formatting and themes`.

## Task 10: Build the Deterministic Cell Frame Renderer

**Files:**

- Create: `internal/tui/cell.go`
- Create: `internal/tui/cell_test.go`
- Create: `internal/tui/frame.go`
- Create: `internal/tui/frame_test.go`
- Create: `internal/tui/table.go`
- Create: `internal/tui/table_test.go`

- [ ] Write tests first for fixed width/height, clipping, fills, overlay order, alignment, crop,
      composed Unicode glyphs used by fixtures, arrows/checkmarks, and style preservation. A wide
      glyph occupies its terminal width and records continuation cells so later writes cannot split
      it.

- [ ] Implement the owned renderer contract:

  ```go
  type Cell struct {
      Glyph      string
      Foreground string
      Background string
      Bold, Dim, Reverse, Continuation bool
  }

  type Frame struct { /* exact rectangular [][]Cell */ }

  func NewFrame(width, height int, fill Cell) Frame
  func (f *Frame) PutText(x, y int, text string, style Style)
  func (f Frame) Crop(rect Rect) Frame
  func (f Frame) RenderANSI() string
  ```

  Store resolved palette values in cells. `RenderANSI` groups adjacent cells with identical style
  and uses Lip Gloss for terminal escapes. The same cells are therefore the source of both terminal
  output and visual goldens; do not reverse-parse ANSI in tests.

- [ ] Write table-rendering tests that consume typed columns/rows and draw header, cursor,
      selection, blank rows, clipped labels, flags, and empty state into a frame. Tests assert exact
      glyph positions and column starts, not substring presence.

- [ ] Implement region metadata alongside rendering:

  ```go
  type NamedRegion struct { Name string; Rect Rect }
  type RenderedScreen struct { Frame Frame; Regions []NamedRegion; Columns []int }
  ```

  Region names are stable contract values: `breadcrumb`, `stats`, `table_header`, `table_body`,
  `hints`, and overlay-specific names. Framework borders and scrollbars are not region content.

- [ ] Add randomized no-panic tests over widths 1-200, heights 1-80, cursor values outside the row
      range, empty rows, and long Unicode labels. Run
      `go test ./internal/tui -run 'Test(Cell|Frame|Table)' -count=1`. Expected: pass.

- [ ] Run the Required Commit Gate and commit with message
      `feat: add deterministic terminal cell renderer`.

## Task 11: Wire the Core Bubble Tea TUI and CLI Startup

**Files:**

- Create: `internal/tui/model.go`
- Create: `internal/tui/model_test.go`
- Create: `internal/tui/update.go`
- Create: `internal/tui/update_test.go`
- Create: `internal/tui/layout.go`
- Create: `internal/tui/layout_test.go`
- Create: `internal/tui/keymap.go`
- Create: `internal/tui/runner.go`
- Modify: `cmd/moneyflow/root.go`
- Modify: `cmd/moneyflow/root_test.go`

- [ ] Add `charm.land/bubbletea/v2 v2.0.8` and `charm.land/bubbles/v2 v2.1.1`, then write
      model-construction tests first. Construction receives `*app.Service`, an `app.Session`,
      explicit `Options{Theme, ColorMode}`, and no filesystem/global state. Before the first resize
      it renders safely; after `tea.WindowSizeMsg` it owns width/height/cursor/scroll only.

  ```go
  type Options struct { Theme ThemeName; ColorMode ColorMode }
  func NewModel(service *app.Service, session app.Session, opts Options) (Model, error)
  func (m Model) Init() tea.Cmd
  func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd)
  func (m Model) View() tea.View
  ```

- [ ] Write key-update tests before behavior. Cover up/down and `j`/`k` cursor clamping, `g`, `d`,
      `A`, Enter, Escape, `t`, `a`, left/right, `s`, `v`, Space, Ctrl-A, `q`, and Ctrl-C. Resolve
      the selected aggregate row by stable identity before calling session methods. Assert
      state/result identity and cursor restoration, not only rendered text.

- [ ] Implement update routing with the actual runtime key contract. `q`/Ctrl-C always exit,
      including narrow and overlay states. Read-only out-of-scope runtime keys (`D`, `m`, `c`, `C`,
      `G`, `h`, `x`, `i`, `u`, `w`, `E`) set a concise transient
      `Unavailable in read-only Go preview` status and do not mutate session data.

- [ ] Implement `renderMain` using the explicit frame renderer. At canonical width, compose
      breadcrumb, stats, table header/body, flags, and hints with the established region names.
      Cursor and selected flags must come from model/session state, while row content comes only
      from `Service.Query`.

- [ ] Implement adaptive main layout. At 150 columns preserve canonical column starts; at 120/80
      proportionally shrink flexible name fields before fixed numeric/flag fields. Height controls
      visible rows and scroll window. At or above 80x24, breadcrumb, stats, at least one data/empty
      row, and hints remain visible. Below 80x24 render only a centered resize message plus quit
      hint.

- [ ] Modify Cobra so no-argument `moneyflow` and `moneyflow demo` both load
      `testdata/parity/transactions.json`, construct service/session, resolve theme/color at the
      boundary, and call `tea.NewProgram(model).Run()`. Add `--theme` and a hidden/internal
      `--fixture` test seam; production default remains the committed demo fixture in this slice.
      Startup validation occurs before alternate-screen entry and returns one wrapped error to
      stderr.

- [ ] Set `tea.View.AltScreen = true` on every normal model view, following Bubble Tea v2. Do not
      add goroutines or asynchronous commands to the synchronous fixture slice.

- [ ] Run
      `go test ./internal/tui ./cmd/moneyflow -run 'Test(Model|Update|Layout|RootDemo)' -count=1`,
      `make build`, and run `./bin/moneyflow --help`. Expected: tests/build pass and help requires
      no profile/network. Manually start `make tui-demo`, exercise navigation, and quit without
      touching user state.

- [ ] Run the Required Commit Gate and commit with message
      `feat: add fixture-backed Bubble Tea views`.

## Task 12: Add Search, Filters, Help, and Responsive Overlay Behavior

**Files:**

- Create: `internal/tui/search.go`
- Create: `internal/tui/search_test.go`
- Create: `internal/tui/filters.go`
- Create: `internal/tui/filters_test.go`
- Create: `internal/tui/help.go`
- Create: `internal/tui/help_test.go`
- Create: `internal/tui/overlay_test.go`
- Modify: `internal/tui/model.go`
- Modify: `internal/tui/update.go`
- Modify: `internal/tui/layout.go`
- Modify: `internal/tui/keymap.go`

- [ ] Write failing search tests for `/` opening a Bubbles v2 text input, incremental
      case-insensitive merchant/category regex results on every edit, inline invalid-expression
      errors, Enter accepting and returning focus to the table, Escape canceling the overlay, and
      later Escape clearing an unchanged accepted search before navigating back. Empty and no-match
      searches must not panic or leave an invalid cursor.

- [ ] Implement search as presentation state plus `Session.SetSearch`; Bubbles handles
      editing/cursor behavior, while regex compilation/filtering remains in analytics. Search text
      is user data and must never be interpreted as markup or included unsafely in an error.

- [ ] Write failing filter tests for `f`, keyboard focus order, date-range validation,
      hidden/transfer toggles, apply/cancel, current-value initialization, and error display. Use
      typed `domain.DateRange`; do not pass date strings below the overlay boundary.

- [ ] Implement the filter overlay with Bubbles controls or small local value models. Applying
      invalid dates leaves the overlay open with a safe message. Applying valid filters resets table
      cursor/scroll, retains navigation state, and re-queries synchronously.

- [ ] Generate the Go help content from one `[]Binding` source also used by update routing. Match
      the corrected Python runtime keys and descriptions exactly for semantic parity. Separately
      test that every runtime binding is either implemented or produces the explicit unavailable
      status, and that no duplicate active key has conflicting actions.

- [ ] Render search at 150x30, filters at 150x30, and help at 150x50 with named overlay regions. At
      80x24, overlays fit inside the frame, show their title/actions, keep focus visible, and exit
      cleanly. Below minimum size the resize screen takes precedence, but `q`/Ctrl-C still work.

- [ ] Add randomized message-sequence tests over resize, cursor, overlay open/close, empty results,
      and quit. Assert no panic, frame dimensions are exact, cursor is in the visible result range,
      and no named region exceeds the frame.

- [ ] Run `go test ./internal/tui -run 'Test(Search|Filter|Help|Overlay|Responsive)' -count=1`.
      Expected: pass.

- [ ] Run the Required Commit Gate and commit with message
      `feat: complete read-only TUI interactions`.

## Task 13: Capture Python Semantic Frames and Enforce Go Parity

**Files:**

- Create: `testdata/parity/frame_scenarios.json`
- Create: `testdata/parity/semantic_frames/*.json` (generated deliberately)
- Create: `moneyflow/parity/backend.py`
- Create: `moneyflow/parity/semantic.py`
- Create: `tests/parity/test_semantic.py`
- Create: `internal/parity/semantic.go`
- Create: `internal/parity/semantic_test.go`
- Create: `internal/tui/semantic_parity_test.go`
- Modify: `Makefile`

- [ ] Define named frame scenarios with fixture, theme, size, initial session, and key sequence.
      Required coverage is:
  - 150x50: merchant, category, group, account, year/month/day time, detail, subgroup, multi-level
    drill, selected rows, help
  - 150x40: representative merchant and detail
  - 150x30: drill-down, search, and filters

  Theme differences are excluded from semantic artifacts; default is enough here.

- [ ] Implement a test-only Python backend that returns the shared fixture through the existing
      backend protocol, using an explicit temporary config/profile root. It must make no network
      calls and must not read `Path.home()`.

- [ ] Write Python extractor tests before the generator. Run each scenario with
      `MoneyflowApp.run_test(size=...)`, apply keys with the Textual pilot, then obtain plain glyph
      rows from `app.screen._compositor.render_strips()`. Crop named widgets to their
      `content_region`; record absolute origin, dimensions, text lines, table column starts, visible
      row identities, breadcrumb, stats, flags, selection, and hints. Exclude borders, scrollbars,
      and all Rich/Textual style data.

- [ ] Implement versioned, canonical semantic JSON and commands:

  ```bash
  uv run python -m moneyflow.parity.semantic --check
  uv run python -m moneyflow.parity.semantic --update
  ```

  Because this uses Textual private compositor APIs, pin behavior with a focused failure message
  that names the installed Textual version and required adapter update rather than silently emitting
  empty frames.

- [ ] Generate with `--update`, inspect each artifact for synthetic data, expected positions, and
      excluded chrome/style, then run check mode twice. Expected: the second check makes no
      filesystem changes.

- [ ] Implement Go semantic projection from `RenderedScreen`: crop the same named regions, strip
      cell style, retain exact glyph/cell positions, and include typed row identities plus columns.
      Strictly decode the Python artifacts with unknown-field rejection.

- [ ] Compare every Go scenario with the Python semantic artifact. Failures print a compact
      named-region/row/column diff; tests never rewrite files. Iterate only the Go layout or a
      proven Python adapter bug until exact equality passes.

- [ ] Add `make parity-python`, `make parity-go`, and `make parity` check-only targets, plus clearly
      named `parity-update-python` for deliberate generation. Run `make parity`. Expected: Python
      artifacts are unchanged and all Go comparisons pass.

- [ ] Run the Required Commit Gate, review `git diff -- testdata/parity/semantic_frames`, and commit
      with message `test: enforce Python semantic TUI parity`.

## Task 14: Approve and Lock Go Visual Cell Frames

**Files:**

- Create: `testdata/parity/go_frames/*.json` (generated deliberately after review)
- Create: `internal/parity/visual.go`
- Create: `internal/parity/visual_test.go`
- Create: `internal/tui/visual_golden_test.go`
- Modify: `Makefile`

- [ ] Define a lossless, versioned visual artifact as width, height, and row-wise runs of
      consecutive cells. Every run stores glyphs/display width, resolved foreground/background, and
      bold/dim/reverse flags. Run-length encoding is storage only; decoding must reconstruct every
      cell in the complete rectangular grid.

- [ ] Write codec tests for round trips, continuation cells, malformed row widths, unknown
      versions/attributes, and stable canonical JSON. The codec must not discard styled spaces
      because background fills are part of the visual contract.

- [ ] Add visual scenarios for all semantic-frame states plus the primary 150x50 merchant frame in
      all seven themes and explicit no-color. The model receives an explicit color mode so CI
      terminal capabilities cannot alter results.

- [ ] Add a deliberately gated update path. Normal tests compare only. Updates require
      `MONEYFLOW_UPDATE_GO_FRAMES=1 go test ./internal/tui -run TestVisualGoldens -count=1`; expose
      that exact operation as `make parity-update-go` and print a warning that review is required.

- [ ] Generate the initial Go frames and render review-friendly ANSI/text previews into a temporary
      directory outside the repository. Inspect merchant/detail/overlays at all sizes and every
      theme for clipping, contrast roles, selection visibility, alignment, and faithful hierarchy.
      Do not commit the artifacts until this one-time visual review is explicitly accepted.

- [ ] After acceptance, retain only the approved frame JSON in the repository diff. Run the visual
      test twice without the update variable and prove `git status --short` is unchanged. Modify one
      cell in a temporary test fixture to prove the failure reports coordinate, expected/actual
      glyph, colors, and attributes.

- [ ] Run `make parity` and `go test ./internal/tui -run TestVisualGoldens -count=1`. Expected:
      exact pass for all cells/themes/sizes with no writes.

- [ ] Run the Required Commit Gate, review the complete artifact diff, and commit with message
      `test: lock approved Go TUI visual frames`.

## Task 15: Verify Performance, Portability, CI, and Handoff Documentation

**Files:**

- Create: `internal/fixture/generate.go`
- Create: `internal/fixture/generate_test.go`
- Create: `internal/analytics/benchmark_test.go`
- Create: `internal/analytics/performance_test.go`
- Create: `.github/workflows/go.yml`
- Create: `docs/superpowers/benchmarks/2026-08-12-go-port-foundation.md`
- Modify: `AGENTS.md`
- Modify: `Makefile`

- [ ] Write deterministic generator tests before implementation. `Generate(seed int64, count int)`
      returns exactly the requested number of generic synthetic transactions across known
      dates/dimensions, currencies/scales, hidden/transfer ratios, and stable IDs. It uses a local
      RNG and never current time or user data.

- [ ] Add benchmarks for full detail, search, merchant/category/group/account/time aggregation, and
      multi-level drill-down over exactly 100,000 transactions. Report allocations. Keep setup
      outside the timed loop and verify result identity so the compiler cannot eliminate work.

- [ ] Add a non-short performance smoke test that warms once, then requires each representative
      complete query to finish under 500ms. Include measured duration and query name on failure.
      Permit `MONEYFLOW_SKIP_PERF=1` only for the race job; ordinary local and CI Go tests must run
      it.

- [ ] Run benchmarks on the development machine with:

  ```bash
  go test ./internal/analytics -run '^$' -bench 'BenchmarkQuery100K' -benchmem -count=5
  ```

  Record date, OS/architecture, Go version, CPU description, command, median time, and allocations
  in the benchmark document. Confirm the reference target of less than 50ms or document the measured
  miss and optimize only with evidence while preserving public APIs.

- [ ] Add `.github/workflows/go.yml` with pinned actions and these jobs:
  - Go 1.26.3 build, `go test -shuffle=on ./...`, and `go vet ./...` on Linux, macOS, and Windows
  - Linux `go test -race -shuffle=on ./...` with only the performance smoke skipped
  - Linux golangci-lint using the pinned official action
  - Linux Python parity check using `uv sync`, focused `tests/parity`, and both artifact check
    commands

  Trigger on pushes to `go-port`/`main` and pull requests targeting `main`; grant read-only contents
  permission. Do not alter release, docs-deploy, or Nix workflows in this slice.

- [ ] Extend `AGENTS.md` with a concise Go-port development section: `make build`, `make test`,
      `make test-race`, `make lint`, `make parity`, `make tui-demo`, the explicit
      artifact-update/review commands, no root binary, and the integer-money rule. Do not rewrite
      the MEMANTO-managed section. `CLAUDE.md` must remain a symlink.

- [ ] Add `make verify-go` to run format check, test, vet, lint, and parity; keep destructive
      cleanup limited to explicit repository paths. Run it fresh along with the full Python gate.

- [ ] Perform the final scope/diff audit:

  ```bash
  git status --short
  git diff --stat HEAD
  go list -m all
  rg -n 'float32|float64' internal/domain internal/analytics internal/app
  rg -n 'sqlite|huma|http|svelte|monarch|ynab|simplefin|credential' internal cmd
  readlink CLAUDE.md
  ```

  Expected: only intended slice files; no money floats in core; no speculative later-slice code;
  direct dependencies remain the approved five; `CLAUDE.md` resolves to `AGENTS.md`.

- [ ] Run the complete Required Commit Gate plus:

  ```bash
  make verify-go
  go test -race -shuffle=on ./...
  GOOS=linux GOARCH=amd64 go build -o /tmp/moneyflow-linux-amd64 ./cmd/moneyflow
  GOOS=windows GOARCH=amd64 go build -o /tmp/moneyflow-windows-amd64.exe ./cmd/moneyflow
  ```

  Expected: every check passes. Cross-build outputs stay outside the repository.

- [ ] Use `superpowers:requesting-code-review` for a final spec/plan/diff review and address all
      verified findings. Re-run affected focused tests and the complete gates.

- [ ] Use `superpowers:verification-before-completion`, run the Required Commit Gate one final time,
      and commit with message `chore: verify Go port foundation slice`. Do not push or merge.

## Slice Completion Evidence

The handoff is complete only when the final response includes fresh results for:

- Go unit/property/interaction/parity/visual tests
- Python characterization and existing full suite
- Go race, vet, format, and lint
- Linux/macOS/Windows CI status, or an explicit statement that remote CI is still unverified
- 100,000-transaction benchmark medians and smoke threshold
- approved semantic and Go visual artifact status
- dependency/scope audit
- current branch and commit IDs

Do not call the first slice complete merely because implementation is committed locally. Do not
begin the Huma/Svelte slice until this evidence is green and its separate design is approved.
