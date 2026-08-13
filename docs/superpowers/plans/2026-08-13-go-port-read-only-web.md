# Go Port Read-Only Web Application Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development
> (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use
> checkbox (`- [ ]`) syntax for tracking.

**Goal:** Deliver a stateless Huma API and embedded Svelte application that preserves every
implemented read-only TUI refinement workflow, adds coordinated bar charts, and runs from the
portable fixture-backed Go binary over loopback or an explicit tailnet listener.

**Architecture:** The browser owns a canonical versioned URL plus bounded opaque selection and
cursor state. Huma decodes that state, invokes pure renderer-neutral actions over the existing
application service, and returns 200-row table/chart projections with exact money strings. Svelte
composes `kit-ui` controls and LayerChart views; production assets are validated, copied into
`internal/web`, and embedded in the Go binary.

**Tech Stack:** Go 1.26.3; Huma v2.38.0 with `humago`; Cobra 1.10.2; Svelte 5.56.3 in runes mode;
Bun 1.3.14; Vite 8.1.3; `@kenn-io/kit-ui` commit
`16db58ef8122dd00e21ce8ad90ba295b9174c6ef`; LayerChart 2.0.2; OpenAPI TypeScript 7.13.0;
OpenAPI Fetch 0.17.0; Vitest 4.1.10; Testing Library 5.4.2; Playwright 1.61.1; axe 4.10.2.

## Global Constraints

- Work only on the already checked-out `go-port` branch. Do not switch branches, pull, merge,
  rebase, push, remove Python, or merge to `main` without explicit user permission.
- Keep this slice fixture-backed and read-only. Do not add SQLite, profiles, providers,
  credentials, authentication, edits, undo/redo, review, commit, deletion, export, or a daemon.
- Tests, demos, screenshots, and benchmarks use only the committed synthetic parity corpus. They
  must not read a real profile, cache, credential, home-directory data file, or provider service.
- Go remains the only source of accounting, analytics, durable-state validation, breadcrumbs,
  supported actions, exact selection membership, and analytical transitions.
- Continue to represent money as signed integer minor units in Go. Wire minor units are base-10
  strings; TypeScript must never convert them to `number` for math, comparisons, totals, or labels.
- The API remains stateless: no cookies, session identifiers, server-side browser selection, or
  cross-origin resource sharing headers. Equal inputs over the same service produce equal outputs.
- Preserve the existing TUI bindings exactly. Web omits `q` and `Ctrl+C`; `End` is not added as a
  cursor shortcut. Text-input and overlay scopes take precedence over table shortcuts.
- URL limits are 64 KiB encoded query, 2 KiB UTF-8 committed search, 512 bytes per entity key,
  five unique drill dimensions, and six return frames. Reject without truncation.
- Selection limits are 8,192 combined identities, 512 bytes per identity, 1 MiB decoded document,
  1.4 MiB encoded value, and 2 MiB request body. Reject an over-limit toggle without changing the
  URL, projection, or prior selection.
- Request windows default to 200 rows, have a maximum limit of 400 rows, and have a maximum offset
  of 1,000,000. The frontend retains only the current and adjacent windows.
- Normalize the base path to one leading and trailing slash. Reject dot segments, query,
  fragment, backslash, encoded slash/backslash, and paths outside the configured prefix.
- Default to `127.0.0.1:8080`. Reject empty or wildcard hosts. A concrete non-loopback address is
  allowed only with the documented unauthenticated-data warning.
- Production HTML uses a non-executable `moneyflow-base-path` meta placeholder and relative Vite
  assets. Do not add inline scripts, inline styles, inline event handlers, remote assets, or a
  browser API-documentation application.
- Reuse `kit-ui` components, tokens, breakpoints, shortcut manager, and virtualization helpers.
  Run `kit-ui-check`; do not recreate a shared primitive that satisfies the required contract. The
  interactive remote-window ARIA grid remains the spec-approved product-specific `FinanceTable`;
  current `kit-ui` Table/VirtualList do not expose that combined role or key contract.
- Ordinary checks never rewrite OpenAPI, generated TypeScript, embedded assets, parity artifacts,
  or screenshots. Updates use explicit targets and require full diff or visual review.
- Keep Linux, macOS, and Windows builds free of CGO. Keep `CLAUDE.md` as the symlink to `AGENTS.md`.
- Use `apply_patch` for hand edits. Generated files may be written only by their reviewed generator.
- Use the `kenn:commit` and `kenn:scrub-private-data` skills before every task commit. Never amend.

## Target File Map

```text
go.mod / go.sum                 Huma and locked Go dependencies
Makefile                        API/frontend/generation/verification targets
cmd/moneyflow/                  web command, fixture loading, listeners, browser opening
internal/app/actions.go         shared action IDs, scopes, keys, descriptions, availability
internal/app/view_state.go      serializable durable state and session conversion
internal/app/selection.go       exact opaque selection codec and set operations
internal/app/web.go             stateless projection and transition engine
internal/api/viewstate.go       strict readable URL codec and bounds
internal/api/types.go           wire-only request, projection, money, chart, and problem types
internal/api/server.go          Huma setup, routes, limits, headers, OpenAPI
internal/web/                   embedded distribution and hardened static handler
api/openapi.yaml                deliberately generated canonical API document
web/                            Svelte source, generated types, tests, tooling, production build
web/src/lib/controller/         API, URL/history, request generation, window cache
web/src/components/             product-specific shell, table, refinements, and charts
web/tests/                      Playwright keyboard, history, responsive, visual, and a11y tests
docs/superpowers/benchmarks/    recorded projection and first-production-bundle evidence
.github/workflows/go.yml        existing cross-platform binary checks with embedded assets
.github/workflows/web.yml       pinned Bun/frontend/browser checks
```

## Required Commit Gate

Run each task's focused red/green command while developing. Before every task commit, run this core
gate from the repository root:

```bash
go test -shuffle=on ./...
go vet ./...
test -z "$(gofmt -l cmd internal)"
golangci-lint run --config .golangci.yml
make parity
uv run pytest -v
uv run pyright moneyflow/
uv run ruff format --check moneyflow/ tests/
uv run ruff check moneyflow/ tests/
npx --yes markdownlint-cli@0.47.0 --config .markdownlint.json README.md 'docs/**/*.md'
.github/scripts/check-arrow-lists.sh
```

Expected: every command exits zero; Go and Python report no failures; Pyright reports zero errors;
`gofmt -l` emits nothing. If the host is too contended for the existing 500 ms performance smoke,
stop competing work and rerun it on an idle host; do not weaken the ceiling.

Starting with Task 7, also run:

```bash
bun install --cwd web --frozen-lockfile
bun run --cwd web check
bun run --cwd web test
bun run --cwd web audit
```

Expected: the lock is unchanged, generated types are current, all checks and tests pass, and the
audit finds no high-severity production dependency issue. Tasks 12-15 add their named production
asset and Playwright commands to this gate.

---

### Task 1: Replace the TUI-Private Key List with a Shared Action Registry

**Files:**

- Create: `internal/app/actions.go`
- Create: `internal/app/actions_test.go`
- Modify: `internal/tui/keymap.go`
- Modify: `internal/tui/update.go`
- Modify: `internal/tui/help.go`
- Modify: `internal/tui/update_test.go`
- Modify: `internal/tui/help_test.go`

**Interfaces:**

- Consumes: existing `Session` methods and the bindings currently in `internal/tui/keymap.go`.
- Produces: `app.ActionID`, `app.ActionScope`, `app.ActionDefinition`, `app.ReadOnlyActions()`, and
  `app.ActionByID(ActionID)` for the TUI, API, and generated web capability projection.

- [ ] **Step 1: Write failing registry contract tests.** Define tests that require every action ID
      to be unique; every key to be unique inside its scope; the current key/description/category
      tuples to remain byte-for-byte equal; `q` and `Ctrl+C` to have lifecycle scope and TUI-only
      availability; unavailable write actions to remain listed; and web-visible actions to exclude
      lifecycle keys. Use this public shape:

  ```go
  type ActionID string
  type ActionScope string

  const (
      ScopeAnalytical ActionScope = "analytical"
      ScopeSelection  ActionScope = "selection"
      ScopeCursor     ActionScope = "cursor"
      ScopeOverlay    ActionScope = "overlay"
      ScopeLifecycle  ActionScope = "lifecycle"
  )

  type ActionDefinition struct {
      ID          ActionID
      Keys        []string
      KeyDisplay  string
      Description string
      Category    string
      Scope       ActionScope
      Implemented bool
      Web         bool
  }
  ```

  Run: `go test ./internal/app ./internal/tui -run 'Test(ActionRegistry|BindingsDeriveFromRegistry)' -count=1`

  Expected: FAIL because the application registry does not exist and TUI bindings are private data.

- [ ] **Step 2: Implement the immutable registry.** Add named IDs for cursor up/down/home, group,
      detail, accounts, drill, back, time granularity, clear/previous/next period, sort/reverse,
      select-one/select-all, filters, search, help, all unavailable actions, and quit/force-quit.
      Also define non-keyed `search.apply` and `filters.apply` analytical actions for the two staged
      overlays. Return defensive copies from `ReadOnlyActions`; make `ActionByID` reject the zero
      value.

- [ ] **Step 3: Derive Bubble Tea bindings and help from the registry.** Keep a small renderer-only
      map from `app.ActionID` to `key.Binding`; replace the local action enum in update routing with
      `app.ActionID`. Do not change runtime behavior, shortcut priority, or help ordering.

- [ ] **Step 4: Run regression and parity checks.** Run:

  ```bash
  go test ./internal/app ./internal/tui -count=1
  make parity
  ```

  Expected: both commands pass and committed semantic/visual artifacts remain unchanged.

- [ ] **Step 5: Run the Required Commit Gate and commit.** Stage only the registry and TUI files.
      Commit with subject `refactor: share read-only action registry`.

### Task 2: Expose Durable Analytical State Without Changing Session Behavior

**Files:**

- Create: `internal/app/view_state.go`
- Create: `internal/app/view_state_test.go`
- Modify: `internal/app/session.go`
- Modify: `internal/app/navigation.go`
- Modify: `internal/app/session_test.go`
- Modify: `internal/app/interaction_test.go`

**Interfaces:**

- Consumes: `domain.QuerySpec`, the existing private history stack, and all current `Session`
  transitions.
- Produces: `app.AnalyticalState`, `app.ReturnFrame`, `app.ViewState`,
  `app.DefaultViewState()`, `app.NewSessionFromViewState(ViewState)`, and
  `Session.ViewState()`.

- [ ] **Step 1: Write failing durable-state tests.** Cover defaults, defensive copies, validation,
      search-anchor reconstruction, ordered navigation/subgroup frames, and a full round trip after
      every committed interaction scenario. Assert that return frames contain no cursor, scroll,
      selection, labels, or provider data. Use these types:

  ```go
  const ViewStateSchemaVersion uint8 = 1

  type ReturnKind string

  const (
      ReturnNavigation ReturnKind = "navigation"
      ReturnSubgroup   ReturnKind = "subgroup"
  )

  type AnalyticalState struct {
      Mode            domain.ResultMode
      Dimension       domain.Dimension
      SubGrouping     *domain.Dimension
      TimeGranularity domain.TimeGranularity
      Drilldowns      []domain.Drilldown // stable key or typed period; labels omitted
      DateRange       *domain.DateRange
      Search          string
      SearchAnchor    *NavigationScope
      ShowHidden      bool
      ShowTransfers   bool
      Sort            domain.SortSpec
  }

  type ReturnFrame struct {
      Kind  ReturnKind
      State AnalyticalState
  }

  type ViewState struct {
      Version uint8
      Current AnalyticalState
      Returns []ReturnFrame
  }

  type NavigationScope struct {
      Mode          domain.ResultMode
      Dimension     domain.Dimension
      SubGrouping   *domain.Dimension
      DrilldownSize int
  }
  ```

  Run: `go test ./internal/app -run 'Test(ViewState|CommittedInteractionScenarios)' -count=1`

  Expected: FAIL because the durable values and conversion methods do not exist.

- [ ] **Step 2: Refactor private snapshots around `AnalyticalState`.** Keep TUI-only
      `ViewPosition` and selection snapshots in `historyEntry`, but store its analytical portion in
      the new value. `Session.ViewState()` strips labels and transient values; reconstruction
      creates private history entries with empty selection and position because browser history
      owns those values.

- [ ] **Step 3: Implement strict state validation and reconstruction.** Validate enums, compatible
      sorts, dates, unique drill dimensions, typed time periods, return kinds, and a maximum of six
      frames. When committed search is nonempty, reconstruct its anchor from the current navigation
      marker so direct bookmarks make `Esc` clear search first.

- [ ] **Step 4: Prove no TUI regression.** Run:

  ```bash
  go test ./internal/app ./internal/tui -count=1
  make parity
  ```

  Expected: all tests pass and no parity artifact changes.

- [ ] **Step 5: Run the Required Commit Gate and commit.** Commit with subject
      `refactor: expose durable application view state`.

### Task 3: Implement the Strict Canonical URL Codec

**Files:**

- Create: `internal/api/viewstate.go`
- Create: `internal/api/viewstate_test.go`
- Create: `internal/api/errors.go`
- Create: `internal/api/errors_test.go`

**Interfaces:**

- Consumes: `app.ViewState` and domain validators from Task 2.
- Produces: `api.DecodeViewQuery(string) (app.ViewState, string, error)`,
  `api.EncodeViewQuery(app.ViewState) (string, error)`, and typed `api.SafeError` codes.

- [ ] **Step 1: Lock the query grammar in failing table tests.** Use scalar fields `v`, `mode`,
      `group`, `subgroup`, `time`, `from`, `to`, `hidden`, `transfers`, `q`, and `sort`; repeated
      `drill` and `return` fields retain slice order. The optional scalar `search-at` is
      `mode:dimension:subgroup-or-_:drilldown-count` and must appear exactly when `q` is nonempty. A
      drill value is `dimension:value`, split only
      at the first colon; time uses `time:granularity:YYYY[-MM[-DD]]`. A `return` value is
      `kind:canonical-frame-query`, split only at the first colon; the frame query contains neither
      `v` nor nested `return` fields.

  Canonical defaults are omitted: aggregate, merchant, year, show-hidden true, transfers false,
  empty search/drills/returns, and the state shape's default sort. `url.Values.Encode` supplies
  stable field ordering while repeated values retain analytical order.

  Run: `go test ./internal/api -run TestViewQuery -count=1`

  Expected: FAIL because no codec exists.

- [ ] **Step 2: Implement decoding with duplicate and unknown-field rejection.** Parse the raw
      query without accepting a leading `?`. Reject duplicate scalar keys, unknown keys/versions,
      invalid booleans/enums/dates/periods/sorts, duplicate drill dimensions, recursive return
      fields, invalid return kinds, and noncanonical values. Return the normalized state and
      canonical query for every success.

- [ ] **Step 3: Enforce exact bounds before and after canonicalization.** Define:

  ```go
  const (
      MaxEncodedViewQuery = 64 << 10
      MaxSearchBytes      = 2 << 10
      MaxEntityKeyBytes   = 512
      MaxDrilldowns       = 5
      MaxReturnFrames     = 6
  )
  ```

  Return code `invalid_view_state` for malformed direct input and
  `view_state_too_large` when encoding a valid transition would cross a bound. Never truncate.

- [ ] **Step 4: Add property and fuzz coverage.** Use `testing/quick` for
      `Encode(Decode(Encode(state))) == Encode(state)` and native fuzz tests for arbitrary query
      bytes. Include Unicode search, `%2F` inside keys, repeated drill order, leap dates, boundary
      sizes, and canonical output idempotence.

- [ ] **Step 5: Run focused checks.** Run:

  ```bash
  go test ./internal/api -run 'TestViewQuery|FuzzViewQuery' -count=1
  go test ./internal/app ./internal/api -count=1
  ```

  Expected: pass without writing artifacts.

- [ ] **Step 6: Run the Required Commit Gate and commit.** Commit with subject
      `feat: add canonical web view URLs`.

### Task 4: Encode Exact Browser-Held Selection State

**Files:**

- Create: `internal/app/selection.go`
- Create: `internal/app/selection_test.go`
- Modify: `internal/app/service.go`
- Modify: `internal/app/service_test.go`

**Interfaces:**

- Consumes: `app.AnalyticalState`, `Service.Query`, stable transaction IDs, and
  `AggregateIdentity`.
- Produces: `app.SelectionValue`, `app.SelectionSnapshot`,
  `Service.ResolveSelection`, `Service.ToggleSelection`, and `Service.ToggleAllSelection`.

- [ ] **Step 1: Write failing codec and normalization tests.** Define the wire value as
      `mfsel1.` plus unpadded base64url of canonical JSON. Its logical document has version `1`,
      identity kind `transaction` or `aggregate`, base kind `explicit` or `all`, sorted explicit
      IDs or a defining `AnalyticalState` with no return frames, plus sorted `include` and `exclude`
      lists. Duplicate or overlapping delta identities are invalid.

  ```go
  type SelectionValue string
  type IdentityKind string

  type SelectionSnapshot struct {
      Kind IdentityKind
      IDs  map[string]struct{}
  }

  func EmptySelection() SelectionValue
  func decodeSelection(SelectionValue) (selectionDocument, error)
  func encodeSelection(selectionDocument) (SelectionValue, error)

  func (service *Service) ResolveSelection(
      state AnalyticalState,
      value SelectionValue,
  ) (SelectionSnapshot, error)

  func (service *Service) ToggleSelection(
      state AnalyticalState,
      value SelectionValue,
      kind IdentityKind,
      identity string,
  ) (SelectionValue, error)

  func (service *Service) ToggleAllSelection(
      state AnalyticalState,
      value SelectionValue,
  ) (SelectionValue, error)
  ```

  Run: `go test ./internal/app -run TestSelection -count=1`

  Expected: FAIL because selection values do not exist.

- [ ] **Step 2: Implement canonical validation and limits.** Enforce 8,192 combined IDs, 512 bytes
      per ID, 1 MiB decoded JSON, and 1.4 MiB encoded value. Require sorted unique arrays, one base
      payload matching its kind, a valid defining analytical state for `all`, and no labels or
      return frames. Map malformed input to `invalid_selection`; map a valid exact result that
      cannot be encoded to `selection_too_large`.

- [ ] **Step 3: Resolve `all` bases through the service.** Query the defining analytical state,
      rebuild the complete concrete stable-identity set, apply exclusions then inclusions, and
      reject a token whose identity kind does not match the defining result. Do not resolve only a
      requested row window.

- [ ] **Step 4: Implement exact toggles and smallest representation.** For Space, resolve the old
      set and toggle the stable target. For `Ctrl+A`, resolve the old set and complete current
      result, then remove all current IDs when all are already selected or add them otherwise.
      Encode candidate representations for explicit IDs, current-result `all` plus deltas, and the
      existing base plus deltas. Choose the shortest encoded value; break equal-length ties by
      canonical byte order. If every exact candidate exceeds a bound, return the old value with
      `selection_too_large` and no mutation.

- [ ] **Step 5: Cover preservation semantics.** Add cases for select-all followed by narrower and
      broader search/filter/time states; mixed current/out-of-view selections; aggregate partition
      identities; sorting and paging; explicit inclusions/exclusions; group/detail/drill clear
      transitions; and invalid hydration resetting to `EmptySelection()` with warning code
      `selection_reset`.

- [ ] **Step 6: Run focused checks.** Run:

  ```bash
  go test ./internal/app -run 'TestSelection|TestService' -count=1
  go test ./internal/app -count=1
  ```

  Expected: all tests pass, including the existing concrete-ID selection tests.

- [ ] **Step 7: Run the Required Commit Gate and commit.** Commit with subject
      `feat: preserve exact stateless web selections`.

### Task 5: Build the Stateless Projection and Transition Engine

**Files:**

- Create: `internal/app/web.go`
- Create: `internal/app/web_test.go`
- Modify: `internal/app/service.go`
- Modify: `internal/app/breadcrumb.go`
- Modify: `internal/app/breadcrumb_test.go`

**Interfaces:**

- Consumes: the action registry, durable state, selection service, analytics service, and stable
  aggregate identities from Tasks 1-4.
- Produces: `app.WindowRequest`, `app.RowTarget`, `app.TransitionRequest`, `app.WebProjection`,
  `Service.ProjectView`, and `Service.TransitionView`.

- [ ] **Step 1: Write failing projection tests.** Require a default 200-row window, limit range
      `1..400`, offset range `0..1_000_000`, total row count, stable window indexes, exact selected
      flags, complete partitioned statistics, server-derived breadcrumb segments, available action
      IDs, safe empty status, and chart marks only for returned rows. Use these application values:

  ```go
  const (
      DefaultWindowLimit = 200
      MaxWindowLimit     = 400
      MaxWindowOffset    = 1_000_000
      PlotRatioScale     = 10_000
  )

  type WindowRequest struct {
      Offset int
      Limit  int
  }

  type RowTarget struct {
      Kind     IdentityKind
      Identity string
  }

  type TransitionRequest struct {
      Action  ActionID
      Target  *RowTarget
      Search  *string
      Filters *Filters
  }

  func (service *Service) ProjectView(
      state ViewState,
      selection SelectionValue,
      window WindowRequest,
  ) (WebProjection, error)

  func (service *Service) TransitionView(
      state ViewState,
      selection SelectionValue,
      transition TransitionRequest,
      window WindowRequest,
  ) (ViewState, SelectionValue, WebProjection, error)
  ```

  Run: `go test ./internal/app -run 'Test(ProjectView|TransitionView)' -count=1`

  Expected: FAIL because the web engine does not exist.

- [ ] **Step 2: Resolve stable drill labels at projection time.** For each key drill, query the
      prefix state and locate the unique aggregate identity in its complete result. Fill the current
      display label only for breadcrumb construction; never persist it into URL or selection state.
      Return `stale_view_target` when a key no longer resolves instead of treating it as empty data.

- [ ] **Step 3: Implement window and chart projection.** Query the complete result once, decorate it
      from the resolved selection set, then copy only the requested window into the projection.
      Build chart partitions by `(currency, scale)`. Compute each signed plot ratio with exact
      integer arithmetic using `math/big`, normalize the largest absolute value in each partition to
      `10_000`, and return zero for an all-zero partition. Detail mode returns exact income, outflow,
      and net summaries rather than per-transaction marks.

- [ ] **Step 4: Write failing transition matrix tests.** Cover every analytical and selection action,
      exact argument requirements, unsupported/local/lifecycle action rejection, stable-target
      resolution, selection-clear/preserve behavior, URL-state bounds, invalid regular expressions,
      and no-change failures. Reuse `testdata/parity/interaction_scenarios.json` to assert state,
      breadcrumb, row order, flags, and back restoration.

- [ ] **Step 5: Implement server-authoritative transitions.** Reconstruct a session from durable
      state, resolve the selection into its concrete maps, apply exactly one existing `Session`
      method, and export the next durable state. Add non-keyed registry actions `search.apply` and
      `filters.apply` for modal submission. Resolve drill and selection targets against the complete
      canonical result, never against a browser index. Return the prior state/selection on every
      rejected transition.

- [ ] **Step 6: Add deterministic and performance checks.** Assert that two equal requests produce
      deeply equal projections. Add benchmarks for decode-independent query/window/chart work over
      `fixture.Generate(100_000)` and report allocations without setting a new threshold yet.

- [ ] **Step 7: Run focused checks.** Run:

  ```bash
  go test ./internal/app -run 'Test(ProjectView|TransitionView|WebInteractionCorpus)' -count=1
  go test ./internal/app -run '^$' -bench BenchmarkProjectView100K -benchmem -count=3
  ```

  Expected: tests pass; benchmark output includes time, bytes, and allocations.

- [ ] **Step 8: Run the Required Commit Gate and commit.** Commit with subject
      `feat: add stateless web projections and transitions`.

### Task 6: Publish the Strict Huma API and OpenAPI Contract

**Files:**

- Create: `internal/api/types.go`
- Create: `internal/api/types_test.go`
- Create: `internal/api/basepath.go`
- Create: `internal/api/basepath_test.go`
- Create: `internal/api/server.go`
- Create: `internal/api/server_test.go`
- Create: `internal/api/openapi_test.go`
- Modify: `internal/api/errors.go`
- Modify: `go.mod`
- Modify: `go.sum`

**Interfaces:**

- Consumes: `app.Service.ProjectView`, `app.Service.TransitionView`, URL codec, version metadata,
  and configured base path.
- Produces: `api.Config`, `api.New`, Huma routes below the base path, deterministic OpenAPI, and
  wire-only request/response types.

- [ ] **Step 1: Add only the approved API dependency.** Add
      `github.com/danielgtaylor/huma/v2 v2.38.0`, run `go mod tidy`, and review `go.mod` so Huma is
      the only new direct Go requirement.

- [ ] **Step 2: Write failing wire-conversion tests.** Define JSON values distinct from domain
      structs. Require no notes, provider/provider ID, metadata, private history, or raw dataset in
      serialized output. Use exact money and opaque selection shapes:

  ```go
  type Money struct {
      Minor    string `json:"minor"`
      Currency string `json:"currency"`
      Scale    uint8  `json:"scale"`
      Decimal  string `json:"decimal"`
      Display  string `json:"display"`
  }

  type Window struct {
      Offset int `json:"offset" minimum:"0" maximum:"1000000"`
      Limit  int `json:"limit" minimum:"1" maximum:"400" default:"200"`
  }

  type ViewBody struct {
      Query     string `json:"query" maxLength:"65536"`
      Selection string `json:"selection,omitempty" maxLength:"1468006"`
      Window    Window `json:"window"`
  }

  type TransitionBody struct {
      Query     string                `json:"query" maxLength:"65536"`
      Selection string                `json:"selection,omitempty" maxLength:"1468006"`
      Action    app.ActionID          `json:"action"`
      Target    *app.RowTarget        `json:"target,omitempty"`
      Search    *string               `json:"search,omitempty"`
      Filters   *app.Filters          `json:"filters,omitempty"`
      Window    Window                `json:"window"`
  }
  ```

  Run: `go test ./internal/api -run TestWire -count=1`

  Expected: FAIL because the wire types and converters do not exist.

- [ ] **Step 3: Implement exact projection wire types.** Include schema versions, canonical query,
      normalized selection, breadcrumb segments/text, filters, capabilities, total/window metadata,
      typed detail or aggregate rows, flags, statistics, chart partitions, and safe status/warnings.
      Serialize `int64` minor units with `strconv.FormatInt`. Treat decimal/display strings from Go
      as final text. Chart ratios are the only numeric quantitative field consumed for geometry.

- [ ] **Step 4: Implement the shared base-path normalizer.** Export
      `api.NormalizeBasePath(string) (string, error)` and cover the exact accepted/rejected contract
      from the global constraints. API, static handler, command, OpenAPI server entries, and tests
      must all call this one function; no package independently cleans paths.

- [ ] **Step 5: Write failing handler tests with `httptest`.** Cover:

  ```text
  GET  <base>api/v1/health
  POST <base>api/v1/view
  POST <base>api/v1/view/transition
  GET  <base>openapi.json
  GET  <base>openapi.yaml
  ```

  Assert strict unknown-field and trailing-value rejection, 2 MiB request-body cap, content types,
  `Cache-Control: no-store`, no CORS/cookies, normalized base path in health/OpenAPI servers,
  problem details, stable safe codes, panic recovery, GET/HEAD method handling, and no raw query or
  financial payload in captured logs.

- [ ] **Step 6: Implement Huma setup and safe errors.** Use `humago.New` on a dedicated
      `http.ServeMux`. Configure documentation paths only for JSON/YAML, disable the docs UI, register
      the three operations, enforce a `http.MaxBytesReader` before body decoding, and use one RFC
      9457-compatible problem envelope with `code`, concise `detail`, and no echoed request values.
      Map invalid hydration selection to a successful projection with `selection_reset`; map a
      rejected toggle to `409 selection_too_large` with the old state in no response body.

- [ ] **Step 7: Lock deterministic OpenAPI.** Add tests that sort/compare JSON and YAML output,
      require all operations and safe problem responses, forbid write verbs/routes, and confirm
      money minor units are strings. Expose:

  ```go
  func (server *Server) OpenAPIJSON() ([]byte, error)
  func (server *Server) OpenAPIYAML() ([]byte, error)
  ```

- [ ] **Step 8: Run focused checks.** Run:

  ```bash
  go test ./internal/api -count=1
  go test ./internal/app ./internal/api -race -count=1
  ```

  Expected: pass with no data races or leaked raw request state.

- [ ] **Step 9: Run the Required Commit Gate and commit.** Commit with subject
      `feat: serve stateless Huma view API`.

### Task 7: Scaffold the Svelte Application and Generated Client

**Files:**

- Create: `web/package.json`
- Create: `web/bun.lock`
- Create: `web/index.html`
- Create: `web/tsconfig.json`
- Create: `web/vite.config.ts`
- Create: `web/vitest.config.ts`
- Create: `web/eslint.config.js`
- Create: `web/.prettierrc.json`
- Create: `web/src/app.d.ts`
- Create: `web/src/main.ts`
- Create: `web/src/App.svelte`
- Create: `web/src/app.css`
- Create: `web/src/test/setup.ts`
- Create: `web/src/App.test.ts`
- Create: `web/scripts/generate-api.ts`
- Create: `web/src/lib/api/schema.d.ts` (generated)
- Create: `web/src/lib/api/client.ts`
- Create: `web/src/lib/api/client.test.ts`
- Create: `api/openapi.yaml` (generated)
- Modify: `cmd/moneyflow/root.go`
- Modify: `cmd/moneyflow/root_test.go`
- Modify: `Makefile`
- Modify: `.gitignore`

**Interfaces:**

- Consumes: deterministic `Server.OpenAPIYAML`, API paths/types from Task 6, and the pinned
  `kit-ui`/LayerChart packages.
- Produces: `moneyflow openapi --format yaml`, `make web-generate`, generated `paths`, a typed
  same-origin API client, and the checked frontend toolchain.

- [ ] **Step 1: Add an OpenAPI export command test first.** Inject a fixture-backed API server and
      require `moneyflow openapi --format yaml` to write only deterministic YAML to stdout. Reject
      unknown formats. The command must not bind a listener or open a browser.

  Run: `go test ./cmd/moneyflow -run TestOpenAPICommand -count=1`

  Expected: FAIL because the command is absent.

- [ ] **Step 2: Implement the export seam.** Add an injectable `OpenAPIWriter` to `IOStreams` and
      build the default service from the embedded synthetic fixture. Keep the command read-only and
      independent of TUI presentation settings.

- [ ] **Step 3: Create `package.json` with exact versions.** Use Bun 1.3.14 and these production
      dependencies:

  ```json
  {
    "@kenn-io/kit-ui": "github:kenn-io/kit-ui#16db58ef8122dd00e21ce8ad90ba295b9174c6ef",
    "@lucide/svelte": "1.21.0",
    "layerchart": "2.0.2",
    "openapi-fetch": "0.17.0",
    "svelte": "5.56.3"
  }
  ```

  Pin the development versions named in the Tech Stack and Kata's current lint/format baseline:
  axe 4.10.2, Playwright 1.61.1, Svelte Vite plugin 7.1.2, Testing Library 5.4.2, TypeScript 5.9.3,
  ESLint 9.39.2, eslint-plugin-svelte 3.15.0, Prettier 3.8.3, prettier-plugin-svelte 3.4.1,
  OpenAPI TypeScript 7.13.0, jsdom 29.1.1, Vite 8.1.3, and Vitest 4.1.10. Generate and commit
  `bun.lock`; do not use caret or tilde ranges.

- [ ] **Step 4: Write the failing generation check.** `generate-api.ts` runs the Go OpenAPI command,
      canonicalizes its trailing newline, invokes `openapi-typescript`, and supports `--check` by
      comparing bytes without rewriting. Test that editing either generated artifact makes check
      mode fail.

- [ ] **Step 5: Scaffold the smallest mounted Svelte app.** `index.html` contains
      `<meta name="moneyflow-base-path" content="__MONEYFLOW_BASE_PATH__">` and only an external
      module script. `main.ts` reads and validates that meta value before mounting. `app.css`
      imports `@kenn-io/kit-ui/theme.css`, `themes.css`, and `fonts.css`; it adds only semantic
      Moneyflow layout/accent variables. `App.svelte` uses `TopBar`, `ThemeToggle`, `StatusBar`, and
      a fixture-data loading state.

- [ ] **Step 6: Implement a generated-client adapter.** Brand the opaque selection without parsing:

  ```ts
  declare const selectionBrand: unique symbol;
  export type SelectionValue = string & { readonly [selectionBrand]: true };

  export interface MoneyflowClient {
    view(body: ViewBody, signal?: AbortSignal): Promise<ViewProjection>;
    transition(body: TransitionBody, signal?: AbortSignal): Promise<ViewProjection>;
  }
  ```

  Use `openapi-fetch` with the runtime base path. Preserve exact strings and reject non-problem
  malformed errors. Tests must prove the adapter transports but never decodes selection or money.

- [ ] **Step 7: Add frontend scripts and Make targets.** Provide `generate`, `generate:check`,
      `typecheck`, `check:svelte --fail-on-warnings`, `lint --max-warnings 0`, `format`,
      `format:check`, `check:usage`, `check`, `audit`, `test`, `build`, and `dev`. Add Make targets
      `web-install`, `web-generate`, `web-check`, `web-audit`, `web-test`, `web-build`, and
      `web-dev`. Ignore `web/node_modules`, `web/dist`, coverage, test results, and Playwright caches.

- [ ] **Step 8: Generate and verify.** Run:

  ```bash
  bun install --cwd web
  make web-generate
  make web-check
  make web-test
  make web-build
  ```

  Expected: generated artifacts are present, all frontend checks/tests pass, and Vite emits
  relative `./assets/` references plus the production meta placeholder.

- [ ] **Step 9: Run the expanded Required Commit Gate and commit.** Review `go.mod`, `bun.lock`,
      `api/openapi.yaml`, and `schema.d.ts`. Commit with subject
      `feat: scaffold generated Svelte web client`.

### Task 8: Coordinate Canonical URLs, Requests, Windows, and Browser History

**Files:**

- Create: `web/src/lib/controller/base-path.ts`
- Create: `web/src/lib/controller/base-path.test.ts`
- Create: `web/src/lib/controller/history.ts`
- Create: `web/src/lib/controller/history.test.ts`
- Create: `web/src/lib/controller/windows.ts`
- Create: `web/src/lib/controller/windows.test.ts`
- Create: `web/src/lib/controller/view-controller.svelte.ts`
- Create: `web/src/lib/controller/view-controller.test.ts`
- Modify: `web/src/App.svelte`

**Interfaces:**

- Consumes: `MoneyflowClient`, generated projection types, canonical query strings, and opaque
  selection values.
- Produces: `createViewController`, a three-window bounded cache, and safe history coordination for
  every component and shortcut.

- [ ] **Step 1: Write pure base-path and history tests first.** Validate `/` and nested prefixes,
      reject malformed meta content, construct API/application URLs without double slashes, and
      preserve canonical queries. Define owned history state as:

  ```ts
  interface MoneyflowHistoryState {
    owner: "moneyflow-web-v1";
    instance: string;
    sequence: number;
    query: string;
    cursorIdentity?: string;
    cursorIndex: number;
    scrollTop: number;
    selection: SelectionValue;
  }
  ```

  Run: `bun run --cwd web test -- src/lib/controller`

  Expected: FAIL because controller modules do not exist.

- [ ] **Step 2: Implement the in-memory owned-entry ledger.** Record only entries created by the
      current page instance. A direct `Esc` jump is allowed only when the target and every crossed
      sequence are present and owned. Reload, gaps, a mismatched instance, or foreign state returns
      `undefined`, forcing `history.replaceState` with Go's canonical parent. Never call
      `history.go()` from a persisted or unverified index.

- [ ] **Step 3: Implement the bounded window cache.** Key entries by canonical analytical query and
      aligned 200-row window offset. Keep current, previous, and next offsets only. Merge no rows
      across different canonical queries. Preserve cursor by stable identity when present; otherwise
      clamp its absolute index into `[0,total-1]`.

- [ ] **Step 4: Write controller race and failure tests.** Cover initial hydration, direct invalid
      URL, selection reset warning, push versus replace, Back/Forward restoration, guaranteed and
      optimized `Esc`, adjacent prefetch, abort, stale response generations, network retry, empty
      results, and `view_state_too_large`/`selection_too_large` retaining the last good state.

- [ ] **Step 5: Implement the runes controller.** Expose read-only reactive state and named methods:

  ```ts
  interface ViewController {
    readonly projection: ViewProjection | undefined;
    readonly loading: boolean;
    readonly announcement: string;
    readonly cursorIdentity: string | undefined;
    readonly cursorIndex: number;
    hydrate(): Promise<void>;
    moveCursor(delta: -1 | 1): Promise<void>;
    moveHome(): Promise<void>;
    apply(action: TransitionAction): Promise<void>;
    restore(event: PopStateEvent): Promise<void>;
    retry(): Promise<void>;
  }

  interface TransitionAction {
    action: TransitionBody["action"];
    target?: TransitionBody["target"];
    search?: TransitionBody["search"];
    filters?: TransitionBody["filters"];
  }
  ```

  Cursor movement inside a loaded window is synchronous. Boundary movement fetches or consumes an
  adjacent window while preserving focus identity. Each request has an `AbortController` and local
  generation; obsolete responses cannot change state.

- [ ] **Step 6: Integrate only loading/error/invalid shells.** `App.svelte` calls `hydrate`, listens
      for `popstate`, renders a safe invalid-view screen with Back and Reset, and announces errors in
      a live region. It does not render server problem detail as HTML.

- [ ] **Step 7: Run focused checks.** Run:

  ```bash
  bun run --cwd web test -- src/lib/controller
  bun run --cwd web check
  ```

  Expected: all controller tests and zero-warning checks pass.

- [ ] **Step 8: Run the expanded Required Commit Gate and commit.** Commit with subject
      `feat: coordinate bookmarkable browser views`.

### Task 9: Build the Keyboard-First Shell, Refinement Bar, and Finance Table

**Files:**

- Create: `web/src/components/AppShell.svelte`
- Create: `web/src/components/AppShell.test.ts`
- Create: `web/src/components/RefinementBar.svelte`
- Create: `web/src/components/RefinementBar.test.ts`
- Create: `web/src/components/FinanceTable.svelte`
- Create: `web/src/components/FinanceTable.test.ts`
- Create: `web/src/lib/shortcuts.ts`
- Create: `web/src/lib/shortcuts.test.ts`
- Modify: `web/src/App.svelte`
- Modify: `web/src/app.css`

**Interfaces:**

- Consumes: the controller, server capabilities, detail/aggregate row unions, `kit-ui` virtual-slice
  helper, TopBar, StatusBar, KbdBadge, EmptyState, tokens, breakpoints, and shortcut manager.
- Produces: the permanent primary ARIA-grid focus surface and local cursor/click activation bridge.

- [ ] **Step 1: Write failing shortcut-map tests from server capabilities.** Require the browser
      keys `ArrowUp/k`, `ArrowDown/j`, `Home`, `g`, `d`, `A`, Enter, Escape, `t`, `a`, ArrowLeft,
      ArrowRight, `s`, `v`, Space, `Ctrl+A`, `f`, `/`, and `?`. Assert no `End`, `q`, or `Ctrl+C`
      handler; mark the last two TUI-only in help. Reject any capability whose key/action metadata
      conflicts with the compiled local routing table.

  Run: `bun run --cwd web test -- src/lib/shortcuts.test.ts`

  Expected: FAIL because the shortcut adapter does not exist.

- [ ] **Step 2: Implement shortcut scopes.** Use `createShortcutManager` from `kit-ui`. Route cursor
      and overlay actions locally; route analytical and selection actions to `controller.apply`.
      Register a root table scope and distinct search, filters, help, date-control, and menu scopes.
      Native input editing wins unless a registered modifier shortcut explicitly allows input focus.

- [ ] **Step 3: Write failing FinanceTable behavior and accessibility tests.** Cover detail and all
      aggregate column sets, stable row keys, server flags, exact money strings, selected state,
      sort direction, sticky headers, visible focus, Home/j/k movement, no End binding, Space,
      `Ctrl+A`, Enter, click/double activation, empty state, boundary-window request, and focus
      preservation after async projection replacement. Assert grid/row/gridcell/columnheader roles,
      `aria-rowcount`, `aria-rowindex`, `aria-selected`, and `aria-sort`.

- [ ] **Step 4: Compose the product-specific virtual grid with `kit-ui` infrastructure.** Use
      `virtualSlice`, semantic tokens, and shared breakpoints for a fixed-height visible range inside
      the at-most three cached windows. Render top/bottom spacers, never the full result. Keep focus
      on one `role="grid"` container with `aria-activedescendant` so a virtual row can unmount safely.
      Do not use `VirtualList` because its listbox role and built-in `End` key conflict with this
      approved grid contract; do not copy its math or shared styling.

- [ ] **Step 5: Write and implement the refinement bar.** Render only server breadcrumb segments,
      result count, grouping, filters, committed search, sort, and clear actions. Button activation
      calls the same controller action as the matching key. Do not parse URL fields into display
      text or compute breadcrumbs in TypeScript.

- [ ] **Step 6: Compose the application shell.** Keep `FinanceTable` as the large primary region at
      all widths. Add theme/system controls, a labelled chart-rail toggle preference, safe status
      messages, retry, and an offscreen polite live region. Restore table focus after every async
      transition and closed overlay.

- [ ] **Step 7: Run focused checks.** Run:

  ```bash
  bun run --cwd web test -- src/lib/shortcuts.test.ts src/components/FinanceTable.test.ts src/components/RefinementBar.test.ts src/components/AppShell.test.ts
  bun run --cwd web check:usage
  bun run --cwd web check
  ```

  Expected: all tests pass and `kit-ui-check` reports no hand-built shared primitives, palette
  values, or ad hoc breakpoints.

- [ ] **Step 8: Run the expanded Required Commit Gate and commit.** Commit with subject
      `feat: add keyboard-first finance table`.

### Task 10: Add Search, Filters, Help, and Shortcut Isolation

**Files:**

- Create: `web/src/components/SearchOverlay.svelte`
- Create: `web/src/components/SearchOverlay.test.ts`
- Create: `web/src/components/FiltersDialog.svelte`
- Create: `web/src/components/FiltersDialog.test.ts`
- Create: `web/src/components/HelpDialog.svelte`
- Create: `web/src/components/HelpDialog.test.ts`
- Create: `web/src/lib/controller/search.ts`
- Create: `web/src/lib/controller/search.test.ts`
- Modify: `web/src/components/AppShell.svelte`
- Modify: `web/src/lib/controller/view-controller.svelte.ts`
- Modify: `web/src/lib/shortcuts.ts`

**Interfaces:**

- Consumes: controller transitions `search.apply`/`filters.apply`, server action capabilities,
  `kit-ui` SearchInput, DateRangePicker, Checkbox/Toggle, Modal, KbdBadge, and focus/shortcut scopes.
- Produces: complete TUI-equivalent overlay lifecycle with local staging and safe live preview.

- [ ] **Step 1: Write failing search-session tests.** On `/`, snapshot canonical URL, projection,
      selection, cursor identity/index, and scroll. Debounce preview by 150 ms and replace history
      only after a valid response. Enter commits exactly one pushed history entry. Escape cancels
      pending work, restores the complete snapshot, and returns table focus. Invalid regular
      expressions retain the last good projection and canonical URL while announcing validation.

  Run: `bun run --cwd web test -- src/lib/controller/search.test.ts`

  Expected: FAIL because no search coordinator exists.

- [ ] **Step 2: Implement the search coordinator.** Use the `kit-ui` debounce helper, one abortable
      request per preview, and an explicit generation. Live input is never written to the URL before
      Go accepts and canonicalizes it. The 2 KiB server bound remains authoritative; local byte
      counting only gives earlier accessible feedback.

- [ ] **Step 3: Write failing component tests.** Assert search and filter inputs receive focus,
      overlay scopes suspend all table letter/arrow keys, Escape closes only the top overlay, focus
      returns to the invoking table, filter Cancel makes no request, and Apply makes one transition
      with inclusive date range plus hidden/transfer values. Help is generated from server
      capabilities, includes unavailable actions, and marks `q`/`Ctrl+C` as TUI-only.

- [ ] **Step 4: Implement with `kit-ui` components.** `SearchOverlay` composes `SearchInput` and safe
      validation/status text. `FiltersDialog` stages a cloned form using `DateRangePicker` and shared
      toggles. `HelpDialog` groups actions by category and renders key badges. Do not add placeholder
      edit actions or HTTP endpoints.

- [ ] **Step 5: Add browser-history search integration tests.** Simulate preview replacements,
      commit push, browser Back/Forward, cancel after multiple previews, a reload without in-memory
      search snapshot, and `Esc` analytical clear from a direct committed-search bookmark.

- [ ] **Step 6: Run focused checks.** Run:

  ```bash
  bun run --cwd web test -- src/lib/controller/search.test.ts src/components/SearchOverlay.test.ts src/components/FiltersDialog.test.ts src/components/HelpDialog.test.ts
  bun run --cwd web check
  ```

  Expected: all overlay, history, focus, and zero-warning checks pass.

- [ ] **Step 7: Run the expanded Required Commit Gate and commit.** Commit with subject
      `feat: preserve TUI refinement overlays on web`.

### Task 11: Add Coordinated LayerChart Views and Responsive Layout

**Files:**

- Create: `web/src/components/VisualizationRail.svelte`
- Create: `web/src/components/VisualizationRail.test.ts`
- Create: `web/src/components/AggregateBars.svelte`
- Create: `web/src/components/AggregateBars.test.ts`
- Create: `web/src/components/TimeBars.svelte`
- Create: `web/src/components/TimeBars.test.ts`
- Create: `web/src/components/DetailSummary.svelte`
- Create: `web/src/components/DetailSummary.test.ts`
- Create: `web/src/lib/chart.ts`
- Create: `web/src/lib/chart.test.ts`
- Modify: `web/src/components/AppShell.svelte`
- Modify: `web/src/app.css`

**Interfaces:**

- Consumes: server chart partitions/ratios, exact money text, stable row identities, current
  cursor, controller cursor/drill methods, LayerChart, and `kit-ui` breakpoints/tokens/drawer controls.
- Produces: linked aggregate horizontal bars, chronological time bars, and detail summaries without
  a second state model.

- [ ] **Step 1: Write failing pure chart-adapter tests.** Preserve the server's `(currency, scale)`
      partitions, row sort order for ordinary aggregates, chronological order for time charts, exact
      labels, signed integer plot ratios, and stable identities. Reject a duplicate identity or a
      datum placed in a mismatched money partition. Do not parse `minor` or `decimal` strings.

  Run: `bun run --cwd web test -- src/lib/chart.test.ts`

  Expected: FAIL because the adapter does not exist.

- [ ] **Step 2: Implement the chart adapter as validation only.** Return immutable view models whose
      quantitative geometry field is the server ratio. Tooltip, screen-reader, and visible labels
      come from exact server text. No reducer, selected set, drill path, sort, or filter lives in
      this module.

- [ ] **Step 3: Write failing chart component tests.** Cover one labelled section per partition,
      horizontal aggregate bars, chronological vertical time bars, income/outflow/net detail cards,
      cursor highlight, click-to-cursor, Enter/double-activation-to-drill, keyboard-focusable marks,
      concise chart descriptions, non-color active cues, and no-chart empty states.

- [ ] **Step 4: Implement LayerChart 2 compositions.** Use `Chart`, `Layer`, `Axis`, `Bars`,
      `Highlight`, and `Tooltip` from `layerchart`. Use CSS variables and `currentColor` so `kit-ui`
      light/dark/system themes remain authoritative. Moneyflow owns only projection mapping,
      identity callbacks, labels, and accent tokens.

- [ ] **Step 5: Implement responsive rail behavior.** Use exported `kit-ui` breakpoints. Desktop
      shows the rail to the right of the larger table pane; medium moves it below or leaves the
      user's collapsed preference; narrow keeps the table full-width and opens charts in a labelled
      drawer. Breakpoint changes never alter URL, cursor, selection, or query. Lower-priority table
      columns move to accessible row detail rather than cards.

- [ ] **Step 6: Honor accessibility preferences.** A chart is always redundant with the table.
      Disable nonessential LayerChart/overlay animation under `prefers-reduced-motion`; expose exact
      labels to assistive technology; preserve visible focus and shape/border/text cues in addition
      to color.

- [ ] **Step 7: Run focused checks.** Run:

  ```bash
  bun run --cwd web test -- src/lib/chart.test.ts src/components/AggregateBars.test.ts src/components/TimeBars.test.ts src/components/DetailSummary.test.ts src/components/VisualizationRail.test.ts
  bun run --cwd web check:usage
  bun run --cwd web check
  ```

  Expected: all chart, responsive, accessibility, usage, and zero-warning checks pass.

- [ ] **Step 8: Run the expanded Required Commit Gate and commit.** Commit with subject
      `feat: link LayerChart views to the finance table`.

### Task 12: Validate and Embed the Production Frontend

**Files:**

- Create: `web/scripts/validate-assets.ts`
- Create: `web/scripts/validate-assets.test.ts`
- Create: `web/scripts/embed-assets.ts`
- Create: `web/scripts/embed-assets.test.ts`
- Create: `internal/web/embed.go`
- Create: `internal/web/handler.go`
- Create: `internal/web/handler_test.go`
- Create: `internal/web/dist/index.html` (generated)
- Create: `internal/web/dist/.moneyflow-production.json` (generated)
- Create: `internal/web/dist/assets/*` (generated)
- Modify: `web/vite.config.ts`
- Modify: `Makefile`
- Modify: `.gitignore`

**Interfaces:**

- Consumes: `web/dist`, normalized base path, and the fixed index meta placeholder.
- Produces: `web.ValidateDistribution`, `web.NewHandler`, committed embedded assets, and explicit
  update/check targets.

- [ ] **Step 1: Write failing asset-validator tests.** Test temporary distributions for missing
      index/manifest/production marker, a compilation stub, unreferenced or missing hashed assets,
      absolute asset URLs, inline script/style/event attributes, remote URLs, duplicate base-path
      placeholders, unsafe filenames, source maps, and malformed content hashes. Require one index,
      one marker, and every manifest reference to exist.

  Run: `bun run --cwd web test -- scripts/validate-assets.test.ts`

  Expected: FAIL because the validator does not exist.

- [ ] **Step 2: Make production builds self-identifying.** Configure Vite with relative base `./`,
      hashed assets, manifest output, no production source maps, and a build plugin that writes:

  ```json
  {"schema_version":1,"kind":"moneyflow-production","entry":"index.html"}
  ```

  The validator canonicalizes and checks that marker; a hand-written placeholder distribution must
  fail release validation.

- [ ] **Step 3: Implement deliberate embedding.** `embed-assets.ts` validates `web/dist`, replaces
      the contents of `internal/web/dist` deterministically, and supports `--check` by comparing all
      names and bytes without writes. Unignore only `internal/web/dist/**`; keep `web/dist` ignored.
      Add Make targets `web-assets-check`, `web-embed`, and `web-embed-check`.

- [ ] **Step 4: Write failing Go handler tests.** Cover GET and HEAD, fixed safe MIME types,
      immutable caching for hashed manifest assets, `no-store` for HTML/nonhashed files, no bodies
      on HEAD, unknown asset 404, safe navigation fallback, API/OpenAPI route reservation, base-path
      isolation, hidden/credential-like files, dot segments, encoded traversal/separators, method
      rejection, exact meta replacement/HTML escaping, and these headers:

  ```text
  Content-Security-Policy: default-src 'self'; script-src 'self'; style-src 'self';
    img-src 'self' data:; font-src 'self'; connect-src 'self'; object-src 'none';
    base-uri 'none'; frame-ancestors 'none'; form-action 'self'
  X-Content-Type-Options: nosniff
  X-Frame-Options: DENY
  Referrer-Policy: no-referrer
  Cache-Control: no-store
  ```

- [ ] **Step 5: Implement the embedded handler.** Use `//go:embed dist` and validate the embedded
      filesystem once during construction. Replace only the exact HTML meta content placeholder
      with an HTML-escaped normalized prefix. Serve no directory listings. Treat only GET requests
      with `Accept: text/html` and safe extensionless paths as SPA navigation; never fall back for
      `assets`, `api/v1`, `openapi.json`, or `openapi.yaml`.

- [ ] **Step 6: Build, review, embed, and verify.** Run:

  ```bash
  make web-build
  make web-assets-check
  make web-embed
  git diff -- internal/web/dist
  make web-embed-check
  go test ./internal/web -count=1
  ```

  Expected: the generated diff contains only validated production files; check mode is read-only;
  Go handler tests pass.

- [ ] **Step 7: Add production checks to the commit gate.** From this task onward run
      `make web-embed-check` after `make web-check` and before any commit.

- [ ] **Step 8: Run the expanded Required Commit Gate and commit.** Review every generated file and
      commit with subject `feat: embed hardened web distribution`.

### Task 13: Add the Web Command, Safe Listener, and Graceful Lifecycle

**Files:**

- Create: `cmd/moneyflow/web.go`
- Create: `cmd/moneyflow/web_test.go`
- Create: `internal/web/server.go`
- Create: `internal/web/server_test.go`
- Modify: `cmd/moneyflow/root.go`
- Modify: `cmd/moneyflow/root_test.go`
- Modify: `Makefile`

**Interfaces:**

- Consumes: fixture-backed `app.Service`, Huma handler, static handler, version metadata, and Cobra
  streams.
- Produces: `moneyflow web`, injectable listener/browser opener, bounded `http.Server`, and
  `make web-demo`.

- [ ] **Step 1: Write failing listen/base-path validation tests.** Accept `127.0.0.1:8080`,
      `[::1]:8080`, concrete private/tailnet IPs, `localhost:8080`, and a concrete DNS name. Reject
      missing/zero/out-of-range ports, empty hosts, `0.0.0.0`, `[::]`, malformed IPv6, and hosts
      containing URL syntax. Normalize `/`, `/moneyflow`, and `/finance/tools/`; reject dot segments,
      encoded separators, query, fragment, and backslash.

  Run: `go test ./cmd/moneyflow -run 'Test(WebListen|BasePath)' -count=1`

  Expected: FAIL because web command validation does not exist.

- [ ] **Step 2: Define injectable lifecycle seams.** Extend command dependencies with:

  ```go
  type ListenerFactory func(context.Context, string, string) (net.Listener, error)
  type BrowserOpener func(string) error
  type SignalContext func(context.Context) (context.Context, context.CancelFunc)
  type WebRunner func(context.Context, *app.Service, WebOptions, IOStreams) error

  type WebOptions struct {
      Listen   string
      BasePath string
      Open     bool
  }
  ```

  Production uses `net.ListenConfig.Listen`, platform browser commands with argument arrays, and
  `signal.NotifyContext`; tests inject all three and never open a real browser or fixed port.

- [ ] **Step 3: Write failing composed-server tests.** Require API/OpenAPI paths to win over SPA
      fallback, unrelated paths outside a non-root prefix to remain unclaimed, no query-string
      logging, healthy startup on an injected listener, browser-open failure as a warning only,
      serve failure propagation, and context cancellation invoking graceful shutdown.

- [ ] **Step 4: Implement the bounded server.** Compose the API and static handlers under one
      `http.ServeMux`. Configure:

  ```go
  http.Server{
      ReadHeaderTimeout: 5 * time.Second,
      ReadTimeout:       15 * time.Second,
      WriteTimeout:      30 * time.Second,
      IdleTimeout:       60 * time.Second,
      MaxHeaderBytes:    1 << 20,
  }
  ```

  Graceful shutdown has a five-second deadline. Do not log request targets, raw URLs, bodies, or
  responses. The browser URL contains the actual injected listener address and normalized base
  path but no query.

- [ ] **Step 5: Add Cobra behavior.** `moneyflow web` accepts `--listen` (default
      `127.0.0.1:8080`), `--base-path` (default `/`), and `--open` (default true). Load the same
      embedded fixture as the TUI. Print a concise unauthenticated-data warning before binding a
      non-loopback address. Browser-open failure writes a warning to stderr and keeps serving.

- [ ] **Step 6: Test transport behavior.** Use an injected `net.Listen("tcp", "127.0.0.1:0")`, make
      real HTTP requests to root and nested base paths, cancel the context, and assert clean server
      exit. Run under the race detector.

  Run:

  ```bash
  go test ./cmd/moneyflow ./internal/web -count=1
  go test -race ./cmd/moneyflow ./internal/api ./internal/web -count=1
  ```

  Expected: all lifecycle, handler, and race tests pass.

- [ ] **Step 7: Add `web-demo`.** Build `bin/moneyflow` and run
      `bin/moneyflow web --open=false` without writing at repository root. Document the URL in Make
      help output, but do not add daemon/service-install behavior.

- [ ] **Step 8: Run the expanded Required Commit Gate and commit.** Commit with subject
      `feat: serve web app on explicit private listener`.

### Task 14: Prove Keyboard, History, Responsive, Visual, and Accessibility Behavior

**Files:**

- Create: `web/playwright.config.ts`
- Create: `web/scripts/e2e-server.ts`
- Create: `web/tests/fixtures.ts`
- Create: `web/tests/keyboard.spec.ts`
- Create: `web/tests/history.spec.ts`
- Create: `web/tests/base-path.spec.ts`
- Create: `web/tests/responsive.spec.ts`
- Create: `web/tests/accessibility.spec.ts`
- Create: `web/tests/visual.spec.ts`
- Create: `web/tests/screenshots/*.png` (generated and reviewed)
- Modify: `web/package.json`
- Modify: `Makefile`

**Interfaces:**

- Consumes: the actual embedded Go server, synthetic fixture, committed interaction corpus, and
  all browser components.
- Produces: Chromium/Firefox/WebKit behavioral evidence, Chromium visual baselines, and automated
  accessibility checks.

- [ ] **Step 1: Create an ephemeral embedded-server harness.** The Bun harness obtains an available
      loopback port, launches `go run ./cmd/moneyflow web --open=false` with the chosen concrete
      address and requested base path, retries selection if the bind loses a race, waits on
      `api/v1/health`, and terminates the child after the suite. It never passes a fixture path,
      reads a profile, or launches an external browser.

- [ ] **Step 2: Write failing keyboard workflow tests.** Drive the real table with keys for every
      top-level grouping, all-detail, account direct view, multi-level drill/subgroup/back, time
      granularity/previous/next/clear, sort cycle/reverse, Space/select-all, search apply/cancel and
      invalid regex, filters apply/cancel, help, and overlay isolation. Reuse expected state,
      breadcrumb, row identity/order, flags, and restoration from
      `testdata/parity/interaction_scenarios.json` where operations align; keep web-only history and
      layout expectations in the Playwright file.

  Run: `bun run --cwd web test:e2e -- tests/keyboard.spec.ts --project=chromium`

  Expected: FAIL until selectors, focus restoration, and any missed workflow wiring are complete.

- [ ] **Step 3: Fix only observed integration gaps and rerun.** Keep the table as the primary focus
      surface. Do not weaken assertions, add sleeps, or move analytical logic into test helpers or
      TypeScript. Use role/name selectors and wait for live-region/projection state, not arbitrary
      timeouts.

- [ ] **Step 4: Add history and base-path suites.** Cover push/replace behavior, browser Back and
      Forward, optimized and fallback `Esc`, refresh, direct valid/invalid bookmarks, committed
      search, selection snapshots, root `/`, and nested `/moneyflow/`. Verify a nested server does
      not claim unrelated root paths and all assets/API/OpenAPI requests remain prefixed.

- [ ] **Step 5: Add responsive and chart linkage suites.** At desktop, medium, and narrow widths,
      assert table primacy, chart right/below/drawer placement, column priority, no card conversion,
      unchanged analytical state across resize, chart click-to-cursor, chart/table highlight, and
      chart drill parity. Run all keyboard workflows at the narrow width.

- [ ] **Step 6: Add accessibility checks.** Use axe on the initial view and every overlay; verify
      focus order/trap/return, roving grid focus, selected/sort state, polite status announcements,
      exact chart labels/descriptions, non-color cues, dark/light contrast, reduced motion, and a
      complete keyboard-only path. Axe exclusions require a written code comment naming the false
      positive and a direct replacement assertion.

- [ ] **Step 7: Generate and review visual baselines deliberately.** Capture Chromium light/dark
      screenshots at `1440x900`, `1024x768`, and `390x844` for aggregate, detail, search, filters,
      help, and narrow chart drawer states. Use only synthetic fixture values. Run the private-data
      scrub, inspect every image at original resolution, and commit no metadata beyond required PNG
      fields.

- [ ] **Step 8: Run all browser projects.** Run:

  ```bash
  bun run --cwd web test:e2e -- --project=chromium
  bun run --cwd web test:e2e -- --project=firefox --grep @smoke
  bun run --cwd web test:e2e -- --project=webkit --grep @smoke
  ```

  Expected: all Chromium workflows/visual/a11y checks and Firefox/WebKit smoke checks pass.

- [ ] **Step 9: Add `web-e2e` to Make and the commit gate.** From this task onward, run
      `make web-e2e` before every completed-slice commit. Keep screenshot update behind
      `MONEYFLOW_UPDATE_WEB_SCREENSHOTS=1`; ordinary E2E is check-only.

- [ ] **Step 10: Run the expanded Required Commit Gate and commit.** Review every screenshot and
      commit with subject `test: lock web refinement workflows`.

### Task 15: Lock Performance Budgets, CI, Documentation, and the Final Slice Gate

**Files:**

- Create: `internal/api/performance_test.go`
- Create: `web/scripts/check-budgets.ts`
- Create: `web/scripts/check-budgets.test.ts`
- Create: `web/budgets.json`
- Create: `docs/superpowers/benchmarks/2026-08-13-go-web-slice.md`
- Create: `.github/workflows/web.yml`
- Modify: `.github/workflows/go.yml`
- Modify: `Makefile`
- Modify: `README.md`
- Modify: `AGENTS.md`

**Interfaces:**

- Consumes: the complete embedded binary, 100,000-row synthetic generator, production frontend,
  browser suite, and all existing Go/Python parity gates.
- Produces: repeatable projection/bundle budgets, cross-platform/CI checks, developer workflow, and
  final completion evidence.

- [ ] **Step 1: Write failing 100,000-row API performance smoke.** Generate the deterministic corpus,
      build one service, and benchmark URL decode, complete query, 200-row projection, chart
      projection, and JSON encoding. Assert the measured combined operation is below 100 ms on the
      documented reference machine and below a deliberately generous 1 second CI smoke ceiling.
      Honor `MONEYFLOW_SKIP_PERF=1` only for the race detector, matching the existing analytics test.

  Run: `go test ./internal/api -run TestProjectionPerformance100K -count=1`

  Expected: FAIL before the benchmark/smoke and any required optimization exist.

- [ ] **Step 2: Measure before optimizing.** Run five isolated benchmark samples on an idle host:

  ```bash
  go test ./internal/api -run '^$' -bench BenchmarkProjection100K -benchmem -count=5
  go test ./internal/analytics -run '^$' -bench Benchmark -benchmem -count=5
  ```

  Record machine, Go version, commit, median latency, bytes, and allocations. Optimize only a
      measured projection bottleneck; preserve exact results and rerun all focused tests after each
      change.

- [ ] **Step 3: Establish measured frontend budgets.** Build production assets, record Brotli and
      gzip bytes for initial JavaScript/CSS, and record p95 chart cursor/drill latency over 100
      Playwright activations. Set each committed budget to the next 10 KiB (assets) or 10 ms
      (latency) above `1.25 * measured value`. `check-budgets.ts` rejects missing assets, a larger
      value, or a budget file not matching schema version 1. This implements the spec's
      measurement-first rule without an arbitrary pre-link threshold.

- [ ] **Step 4: Write tests for the budget checker.** Cover exact boundary pass, one-byte/one-ms
      failure, missing manifest entry, wrong compression mode, malformed schema, and stable sorted
      output. Add `web-budgets` and include it in `web-check` after `web-build`.

- [ ] **Step 5: Extend stable Make workflows.** The final targets are:

  ```text
  web-install       frozen Bun install
  web-generate      deliberate OpenAPI and TypeScript update
  web-check         generation, types, Svelte, lint, format, kit-ui, budgets
  web-test          Vitest component/unit tests
  web-audit         high-severity dependency audit
  web-build         production Vite build
  web-assets-check  validate web/dist
  web-embed         deliberate copy into internal/web/dist
  web-embed-check   read-only committed distribution comparison
  web-e2e           embedded Chromium plus Firefox/WebKit smoke
  web-demo          build and run loopback server
  verify-web        all read-only frontend, asset, API, and browser checks
  ```

  `make verify-go` continues to run Go format, randomized tests, vet, lint, and parity. Neither
  verification target updates generated artifacts.

- [ ] **Step 6: Add pinned CI.** `web.yml` installs Go 1.26.3 and Bun 1.3.14, uses the frozen
      lockfile, runs generation check, frontend check/test/audit/build/embed check, installs pinned
      Playwright browsers, and runs browser suites. Update the Linux/macOS/Windows Go build job to
      verify the committed embedded distribution and build the web-enabled binary without Bun/Node.
      Preserve pinned action SHAs and least-privilege permissions.

- [ ] **Step 7: Document supported use and trust boundaries.** Add concise README commands for
      loopback, `--open=false`, an explicit private/tailnet address, and a Caddy reverse proxy base
      path. State plainly that non-loopback HTTP has no built-in authentication or transport
      encryption and that proxy access logs should omit query strings. Use only reserved example
      hosts/IPs and synthetic names. Update `AGENTS.md` with the stable web targets and generation
      discipline; preserve the `CLAUDE.md` symlink.

- [ ] **Step 8: Run the public-artifact privacy gate.** Scan the full slice diff, commit messages,
      generated OpenAPI, frontend bundle strings, fixtures, docs, logs captured by tests, and every
      screenshot. Reject home paths, private host/tailnet names, personal email, real financial
      values, credentials, source maps, and provider payload fields.

- [ ] **Step 9: Run the complete final gate on an idle host.** Run:

  ```bash
  make verify-go
  make verify-web
  make test-race
  go build ./...
  GOOS=linux GOARCH=amd64 go build ./cmd/moneyflow
  GOOS=darwin GOARCH=arm64 go build ./cmd/moneyflow
  GOOS=windows GOARCH=amd64 go build ./cmd/moneyflow
  uv run pytest -v
  uv run pyright moneyflow/
  uv run ruff format --check moneyflow/ tests/
  uv run ruff check moneyflow/ tests/
  npx --yes markdownlint-cli@0.47.0 --config .markdownlint.json README.md 'docs/**/*.md'
  .github/scripts/check-arrow-lists.sh
  ```

  Expected: every command exits zero; performance ceilings pass without skip; all generated checks
  report current artifacts; builds do not modify the tree.

- [ ] **Step 10: Review scope and working tree.** Confirm the diff contains no SQLite, provider,
      credential, authentication, edit, undo/redo, daemon, or Python-removal work. Run
      `git status --short`, inspect all generated diffs, and require a clean tree after the commit.

- [ ] **Step 11: Commit the verified slice.** Use the required commit/privacy skills, stage only
      Task 15 files, and commit with subject `chore: verify read-only web slice`. Do not push or merge.

## Completion Handoff

After Task 15, stop. Report the commits, exact verification evidence, benchmark/bundle measurements,
and local run command. Do not push, merge `go-port`, remove Python, start SQLite/persistence, or add
providers without a new explicit user instruction and the next approved design/plan.
