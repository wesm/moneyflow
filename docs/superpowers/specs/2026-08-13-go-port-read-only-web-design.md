# Go Port Read-Only Web Application Design

**Date:** 2026-08-13

**Status:** Approved

**Branch:** `go-port`

## Purpose

moneyflow is becoming a full Go replacement with two first-class interfaces: the
existing keyboard-driven terminal user interface (TUI) and a browser interface.
The Go foundation already supplies exact accounting, analytics, application
sessions, a fixture-backed Bubble Tea TUI, and Python-to-Go parity artifacts.

This design covers the next independently verifiable slice: a stateless Huma
application programming interface (API) and an embedded, read-only Svelte web
application over the same fixture-backed Go service. The web application must
preserve the TUI's rapid keyboard refinement workflows while adding a web-native
layout and contextual charts.

The web interface is not a dashboard that happens to have shortcuts. The table is
the primary workspace, the keyboard is a primary input method, and charts are a
coordinated second representation of the current analytical result.

## Relationship to the Foundation Slice

This slice builds on the approved
`2026-08-12-go-port-foundation-read-only-tui-design.md` contract. That document
remains authoritative for exact money, normalized transactions, analytics,
stable identities, the synthetic parity corpus, cross-platform support, and the
long-lived `go-port` branch policy.

The Python TUI remains the product-behavior oracle during the replacement. The Go
application session and service are the implementation boundary shared by the Go
TUI and HTTP adapter. If an ordinary browser convention conflicts with an
existing refinement behavior, the TUI behavior wins unless this design names the
web-only difference explicitly.

This design does not authorize merging `go-port` into `main`, removing Python, or
connecting the new web server to real financial data.

## Goals

- Preserve every in-scope read-only TUI refinement workflow in the browser.
- Make the complete durable analytical view bookmarkable and refresh-safe.
- Keep all accounting, analytics, state validation, and transitions in Go.
- Add coordinated charts without making them an alternative state machine.
- Avoid transferring a complete 100,000-transaction dataset to the browser.
- Serve the application from one portable Go binary on Linux, macOS, and Windows.
- Support loopback use, an explicit tailnet listener, and a reverse-proxy base path.
- Reuse the shared `kit-ui` component system and developer conventions from Kata.
- Keep the API stateless so tabs, bookmarks, and restarts behave predictably.

## Non-Goals

- SQLite profiles, caches, migrations, or other persistence.
- Transaction edits, undo/redo, review, commit, deletion, or export.
- SimpleFIN, Monarch Money, YNAB, Amazon, or other provider adapters.
- Credentials, built-in authentication, authorization, or user accounts.
- A background daemon, service discovery, or multi-process coordination.
- Sending normalized raw datasets to the browser for client-side analytics.
- Reproducing terminal cell geometry, colors, or all seven terminal themes in CSS.
- Pie charts, Sankey diagrams, forecasting, budgets, or other richer analytics.
- Removing the Python behavioral oracle or changing the current parity corpus.

## Core Decisions

The slice uses these approved decisions:

1. The web interface has functional parity with every read-only TUI workflow but
   uses a web-native visual design.
2. The desktop workspace is a primary table with a contextual visualization rail.
3. The table and chart share one cursor, stable row identities, and drill actions.
4. Durable analytical state lives in a versioned URL. Cursor, scroll position,
   open overlays, and selection remain transient browser state.
5. The API is stateless and server-authoritative. It returns windowed table and
   chart projections instead of raw transactions.
6. Loopback is the safe default. Remote HTTP requires an explicit concrete listen
   address or a separately configured reverse proxy.
7. The entire application supports a configurable base path.
8. Svelte 5, Vite, Bun, `kit-ui`, and LayerChart 2 form the frontend stack.
9. Go remains the sole source of accounting, analytics, URL validation, and
   renderer-neutral transitions.

## Architecture

```text
Browser URL and transient interaction state
                    │
                    │ generated OpenAPI client
                    v
          Stateless Huma HTTP adapter
                    │
          URL codec and action transition
                    │
                    v
             Application service
                    │
           Pure domain and analytics
                    │
          Embedded synthetic fixture

Bubble Tea TUI ─────┘ uses the same application behavior directly
```

The API does not hold browser sessions, set cookies, or assign session IDs. Each
request contains a complete durable view description plus a requested row window.
The response contains a normalized state and an immutable projection of that
view. Two identical requests against the same loaded dataset produce identical
responses.

The Svelte application never constructs an analytics query independently. It
sends the current canonical URL state or a named action to Go. Go decodes and
validates the state, applies the transition, queries the service, and returns the
canonical next state and projection.

## Package and Source Layout

This slice adds focused packages and frontend sources:

```text
cmd/moneyflow/       `web` command, flags, lifecycle, and browser opening
internal/app/        renderer-neutral action registry and durable web state
internal/api/        Huma server, URL codec, wire types, and route handlers
internal/web/        embedded distribution and hardened static handler
web/                 Svelte source, generated API types, tests, and Vite tooling
```

The TUI does not call HTTP. It continues to use the application service in
process. The API does not import terminal types. The frontend does not import or
reimplement Go domain logic.

The existing application session can be refactored where needed to expose a
serializable durable state and pure named transitions. The refactor must preserve
the existing Go interaction corpus and TUI behavior. Presentation-only cursor and
viewport positions remain outside the durable state.

## Command and Server Lifecycle

`moneyflow web` loads the same embedded synthetic fixture as the Go TUI, validates
it, builds the application service, starts the HTTP server, and opens the local
application URL. A browser-opening failure is a warning and does not stop a
healthy server.

The command supports:

- `--listen HOST:PORT`, defaulting to `127.0.0.1:8080`
- `--base-path PATH`, defaulting to `/`
- `--open=false` for a server that must not launch a local browser

The listen value must name a concrete host and port. Empty hosts and unspecified
or wildcard addresses such as `0.0.0.0` and `[::]` are rejected. A concrete
tailnet address is allowed. A same-host reverse proxy can instead forward to the
default loopback listener.

The server speaks HTTP. Transport encryption, public certificates, proxy
authentication, and tailnet policy remain external responsibilities. Starting a
non-loopback listener prints a concise warning that every peer able to reach that
address can read the served financial data because this slice has no built-in
authentication.

Interrupt or context cancellation starts graceful shutdown with a bounded
deadline. The server configures header, request-body, read, write, and idle
limits. Tests inject listeners and browser openers; they do not use fixed ports or
real browsers.

## Base-Path Contract

The base path is normalized to exactly one leading slash and one trailing slash.
It must not contain dot segments, a query, a fragment, backslashes, or encoded
path separators.

Every owned route lives below that prefix:

```text
<base-path>                         Svelte application
<base-path>assets/...               hashed frontend assets
<base-path>api/v1/...               JSON API
<base-path>openapi.json             OpenAPI document
<base-path>openapi.yaml             OpenAPI document
```

Vite emits relative `./assets/` references. Its index contains a non-executable
`<meta name="moneyflow-base-path">` placeholder. The Go handler replaces only
that placeholder with the HTML-escaped effective prefix; the external hashed
application script reads the meta value. The handler does not inject an inline
script, style, or event attribute, so `script-src 'self'` and `base-uri 'none'`
remain valid without a nonce or hash. Client-side navigation, API requests,
redirects, asset references, and OpenAPI server entries all use that value.

When the base path is not `/`, the server does not claim unrelated paths. This
allows a reverse proxy to host moneyflow beside other applications. Unknown asset
paths return `404`; only safe browser navigation requests under the application
prefix receive the single-page application shell.

## Durable View State and URL Contract

The URL query is the durable analytical state. It contains a schema version and
the information required to reconstruct the visible result:

- result mode and grouping dimension
- time granularity
- ordered drill-down keys or typed time periods
- inclusive date range
- hidden and transfer visibility
- committed search text
- sort field and direction
- ordered analytical return frames for drill-down and subgroup scopes

Drill-down URLs contain stable entity keys, not display labels. Go resolves the
current label from the loaded data when it builds the breadcrumb. Repeated
drill-down parameters preserve path order.

The codec uses explicit, readable, versioned query fields. It rejects unknown
versions, duplicate scalar fields, invalid enum values, impossible dates,
duplicate drill dimensions, malformed periods, incompatible sorts, and unsafe
or oversized inputs. Successful decoding always returns one canonical encoding.
Canonical encoding has stable parameter order, omits default values, and is
idempotent.

The percent-encoded query is at most 64 KiB. A committed search is at most 2 KiB
of UTF-8 text, a decoded entity key is at most 512 bytes, the drill path contains
at most the five unique supported dimensions, and the return stack contains at
most six frames. The decoder checks both the raw input and canonical output.

A direct URL that violates a bound opens the invalid-view screen with Back and
Reset actions. If live search or an analytical transition would cross a bound,
the API returns `view_state_too_large`; the browser retains the last good URL and
projection and announces that the search or refinement is too large to bookmark.
It never truncates, drops a return frame, or commits a URL that cannot reproduce
the view.

Cursor, scroll offset, loaded row windows, selection, chart visibility, open
dialogs, theme preference, and live uncommitted search text do not appear in the
URL. Browser history state may retain cursor and scroll for exact restoration
within the current tab, but a bookmark does not depend on them.

Each return frame contains only the prior result mode, grouping or subgroup,
time granularity, drill path, filters, committed search, and sort needed by the
existing application `Back` transition. It does not contain cursor, scroll,
selection, display labels, or provider data. A directly opened bookmark can
therefore reproduce the same analytical parent transitions as the TUI without a
server session or hidden browser history.

Search text is visible in the URL because committed search is bookmarkable. The
server therefore sets `Referrer-Policy: no-referrer`, never logs a raw request
query, and documents that reverse-proxy access logs should omit query strings.

## Browser History and Back Behavior

Every committed analytical transition pushes its canonical URL:

- grouping and detail changes
- drill-down and subgroup changes
- time granularity and period navigation
- applied filters
- sort field or direction
- completed search

Browser Back and Forward restore the exact prior projection. Each owned history
entry carries an application marker plus transient cursor and scroll state when
available.

`Esc` invokes the same Go `Back` transition as the TUI. It clears search at its
anchor, leaves a subgroup, or returns through a drill frame in the same priority
order. It does not undo an ordinary top-level grouping, filter, time-granularity,
or sort change merely because that change created a browser entry.

Go always returns the canonical analytical parent for `Esc`. Replacing the
current entry with that parent is the guaranteed behavior. As a best-effort
optimization, the controller may jump to a matching earlier moneyflow entry and
restore its cursor, scroll, and selection. It does so only while an in-memory
ledger proves that the target and every crossed entry belong to the current app
instance. After reload, a missing or inconsistent marker, or any other doubt, it
uses the guaranteed replacement path. It never follows an index that could reach
an unmarked external entry. Browser Back and Forward remain ordinary
chronological navigation and can traverse every committed URL change.

Opening search snapshots the current URL, cursor, and scroll. Debounced live
search uses history replacement for previews. Enter commits one new entry. Esc
restores the complete snapshot. Invalid regular expressions keep the last good
projection visible and do not change the canonical URL.

Filters are staged locally in a modal. Apply sends one transition and pushes one
URL. Cancel changes neither the URL nor the projection.

## Renderer-Neutral Action Registry

The Go application layer gains one registry for named read-only actions,
categories, availability, descriptions, default keys, and action scope. The
scope distinguishes analytical transitions, selection transitions,
renderer-local cursor or overlay behavior, and process lifecycle. The Bubble Tea
key map and the web capability projection derive from this registry.
Renderer-specific routing can add platform mechanics but cannot redefine an
analytical or selection action.

The browser handles cursor movement and opening or closing overlays locally. It
sends analytical and selection actions to the transition endpoint. This avoids a
network round trip for `j`/`k` while keeping grouping, drill, sort, filter,
search, time, and select-all behavior authoritative in Go.

The browser preserves these implemented TUI bindings:

- `Up`/`k`, `Down`/`j`, and `Home` for table movement
- `g`, `d`, and `A` for grouping, detail, and account views
- Enter for drill-down and Esc for analytical back
- `t`, `a`, Left, and Right for time refinement
- `s` and `v` for sort field and reverse direction
- Space and `Ctrl+A` for selection
- `f` and `/` for filters and search
- `?` for help

The browser does not bind `q` or `Ctrl+C`. Those are terminal lifecycle controls,
and stopping a server shared over a private network from an incidental browser
tab would be surprising. Web help marks both keys as TUI-only.

When a text input, date control, menu, or modal owns focus, native text editing
and the component's shortcut scope take precedence. Closing the overlay restores
table focus and the prior cursor.

Unavailable read-only actions remain visible in the shared help contract where
needed for parity, with a clear unavailable status. They do not create placeholder
write endpoints.

## HTTP API

Huma describes the API and produces deterministic OpenAPI JSON and YAML. This
slice serves the documents but does not add a browser API-documentation
application or load documentation assets from a content-delivery network. The
initial operations are:

### `GET api/v1/health`

Returns the program version, API schema version, read-only capability, effective
base path, and fixture-backed data status. It exposes no transaction values or
host details.

### `POST api/v1/view`

Accepts the URL query text, an optional browser-held selection state, and a
bounded row-window request. It decodes and normalizes the durable state, queries
the application service, and returns the canonical state plus one view
projection.

### `POST api/v1/view/transition`

Accepts the current canonical URL query, browser-held selection state, a named
action, any typed action argument or stable row target, and a row-window request.
It applies one renderer-neutral transition and returns the next canonical state,
next selection state, and view projection.

POST is used for these read-only calculations because the request state and
action payloads are structured and can be larger than a safe URL. The operations
do not persist server state. API and HTML responses use `Cache-Control: no-store`.

Huma validation is strict. JSON bodies reject unknown fields, trailing values,
oversized strings, invalid row limits, unsupported actions, and action arguments
that do not match the action. A row-targeted transition resolves the stable
identity against the canonical result instead of trusting a browser row index.

## View Projection

A successful view response contains:

- API and projection schema versions
- the normalized durable state and canonical URL query
- the normalized browser-held selection state
- breadcrumb segments and display text
- active filters and supported actions
- total filtered row count and requested row-window metadata
- typed detail or aggregate table rows with stable identities and server-derived
  hidden, pending, and selected flags
- per-partition statistics
- chart projections for the returned row window
- an empty-state or safe status message when applicable

Wire data is distinct from the internal domain structs. The API does not expose
provider metadata, unused notes, private application history, or fields the
read-only TUI does not display.

Dates use ISO `YYYY-MM-DD`. Enums use their validated string forms. Stable
aggregate identities retain the complete dimension, entity key, currency, and
scale partition.

### Exact money on the wire

JavaScript cannot exactly represent every signed 64-bit integer. Every wire money
value therefore contains:

- signed minor units as a base-10 string
- currency
- decimal scale
- canonical decimal text
- server-produced display text where presentation parity requires it

The browser never converts minor units to a JavaScript `number` for accounting,
totals, comparisons, or labels. It renders exact strings supplied by Go.

Each chart datum additionally contains a bounded signed integer plot ratio. Go
computes the ratio from exact values within one `(currency, scale)` partition.
LayerChart uses that ratio only for geometry and uses the exact money text for
labels and accessible descriptions. Presentation coordinates may use browser
floating point after this normalization; money does not.

## Windowing, Cursor, and Selection

The browser requests a default window of 200 rows. The API caps both offset and
limit and returns the total row count. The frontend prefetches at most the
adjacent windows and keeps a bounded cache keyed by canonical analytical state.
It never fetches the complete result merely to implement scrolling.

`FinanceTable` composes `kit-ui` table and virtualization primitives into one
ARIA grid. It has sticky headers, deterministic column priority, roving focus,
and a single canonical cursor. Moving past a loaded boundary fetches the next
window and retains focus by stable identity.

Sorting or refetching preserves the focused identity if that row remains in the
result. Otherwise the cursor clamps to the nearest valid row. Browser Back and
Forward restore the saved identity or index and scroll position when available.

Selection state is transient and browser-held but opaque to TypeScript. The wire
field is a branded string produced and consumed only by Go. Its versioned logical
document contains:

- a base that is either a sorted explicit stable-identity list or an `all` marker
  with the canonical query-producing fields of the defining analytical state;
  return frames are omitted because they do not change result membership
- the identity kind: normalized transaction ID or composite aggregate identity
- sorted inclusion identities added to that base
- sorted exclusion identities removed from that base

For an `all` base, Go re-runs the defining state and resolves its complete stable
identity set before it applies deltas. A later search, filter, time change, or
window request therefore tests membership against the same concrete result that
was selected originally, matching the current session's ID-set behavior. The
browser sends the opaque value with projection and transition requests; the
stateless API returns a canonical next value and decorates only the current row
window. No selection is retained by the server or placed in the URL.

After Space or `Ctrl+A`, Go resolves the old selection and the complete current
result, applies the existing `toggleSetValue` or `toggleAll` semantics, and picks
the smallest exact canonical representation: current-result `all` plus deltas,
the existing base plus deltas, or an explicit list. It never substitutes a
different approximate set.

The decoded selection document is limited to 8,192 combined explicit, inclusion,
and exclusion identities, 512 bytes per identity, and 1 MiB total. Its encoded
wire string is limited to 1.4 MiB, and a view request body is limited to 2 MiB.
If an otherwise valid toggle cannot fit an exact representation, the API returns
`selection_too_large`; the browser retains the prior selection, URL, and
projection and announces that no additional rows were selected. An invalid or
oversized value received during initial hydration opens the view with selection
reset and an announced warning; it does not invalidate the analytical URL.

`Ctrl+A` selects or clears every row in the complete current result, matching the
TUI rather than limiting the operation to the loaded 200-row window. Space
toggles the focused stable identity. Selection survives paging, sort, search,
filters, time-granularity changes, and period navigation exactly where the
existing session preserves it. Top-level grouping, all-detail, direct-account,
drill, and subgroup transitions clear it. Analytical Back and browser
Back/Forward restore the selection snapshot associated with the target entry.

## Frontend Stack and Dependency Policy

The frontend uses:

- Svelte 5 in runes mode
- TypeScript with strict checking
- Vite for development and production builds
- Bun as the package manager and script runner
- `@kenn-io/kit-ui` for shared components, tokens, themes, and shortcut scopes
- LayerChart 2 for composable Svelte charts
- generated OpenAPI types and client helpers
- Vitest and Testing Library for unit and component tests
- Playwright for browser workflows

Dependencies use exact versions or immutable Git commits and are recorded in
`bun.lock`. `kit-ui` is consumed as source through its Svelte export and pinned to
an immutable commit. The frontend imports existing `kit-ui` components instead
of recreating their equivalents. `kit-ui-check` runs in continuous integration
to catch hand-built modals, inputs, dropdowns, raw palette values, or ad hoc
breakpoints.

Moneyflow-owned Svelte components are limited to product-specific composition:
the finance table, refinement bar, breadcrumb, statistics, chart projections,
and view controller. The application does not add a Moneyflow component merely
to rename a `kit-ui` primitive.

The package follows Kata's proven frontend layout and generation checks without
copying Kata-specific authentication, daemon discovery, event streaming, or
product components.

## Web Components

### Application shell

The shell uses `kit-ui` top-bar, theme, overlay, shortcut, and status primitives.
It owns the effective base path, generated API client, browser history marker,
global error state, and responsive workspace.

### Refinement bar

The refinement bar shows the server-derived breadcrumb, grouping, active
filters, committed search, sort, result count, and clear actions. Controls are
both clickable and keyboard reachable. The breadcrumb is never independently
assembled from URL strings in the browser.

### Finance table

The table is the permanent primary focus surface. It renders the same information
hierarchy and flags as the existing TUI, adapted to semantic HTML. Row movement,
drill-down, sorting, selection, and restoration use stable identities rather than
DOM positions.

### Visualization rail

The desktop rail shows the chart that corresponds to the current table and
result mode. It is collapsible with a labelled web-only control. The control does
not claim `v` or any other TUI key. Its visibility is a browser preference, not
durable analytical state.

### Search, filters, and help

Search, filters, and help use `kit-ui` inputs, date-range controls, modals, focus
traps, and keyboard badges. Their lifecycle matches the TUI: open from the table,
preview or stage safely, apply or cancel explicitly, and restore focus.

## Visualization Contract

Charts complement the table and never create a separate selection, filter,
drill, or sorting model.

- Aggregate views use horizontal bars for the table rows visible in the current
  viewport. Their order follows the table sort.
- Time views use chronological vertical bars from the returned window. A mark
  still maps to the corresponding stable table row even when the table has a
  nonchronological sort.
- Detail views use compact income, outflow, and net summaries rather than
  inventing a per-transaction chart.
- Every `(currency, scale)` partition renders in a separate labelled section.
  Incompatible money is never added or plotted on one quantitative axis.
- The active table row highlights its chart mark. Clicking a mark moves the table
  cursor; Enter, double activation, or the labelled drill control applies the
  same Go transition as table drill-down.
- Tooltips and accessible labels use exact server strings. Hover may add a visual
  highlight but never changes analytical state.

LayerChart owns rendering mechanics, axes, tooltips, and responsive SVG layout.
Moneyflow owns the data projection, identities, interaction bridge, and theme
tokens. This slice standardizes only the bar and summary forms above. It does not
add an abstraction for hypothetical future chart types.

## Responsive Layout

The approved desktop layout gives the table the larger primary pane and places
the insight rail on the right.

At medium widths, the rail moves below the table or remains collapsed according
to the available height and the user's preference. At narrow widths, the table
fills the workspace and the chart opens as an explicit drawer. The analytical
state and cursor do not change when the layout crosses a breakpoint.

Table column priority follows the existing TUI's narrow-layout decisions. Lower
priority columns can disappear from the grid but remain available in an
accessible row detail. Required navigation context, amount, row identity, and
selection state remain visible.

Keyboard workflows operate at every supported browser width. Pointer and touch
are alternative inputs, never requirements. Narrow layouts do not replace the
table with cards that change row semantics.

## Themes and Visual Identity

The first web slice uses `kit-ui` light, dark, and system theme support with a
small Moneyflow accent layer expressed through shared semantic tokens. It does
not reproduce all seven terminal palettes.

Web behavioral parity covers content, actions, state, ordering, flags, and
information hierarchy. It does not require browser pixels to match Textual or
Bubble Tea cells. Web visual artifacts become canonical only for the web layout
after review.

## Accessibility

The browser interface must be complete without a pointer and understandable
without the chart.

- The table exposes correct grid, row, column-header, selected, and sort state.
- Focus is visible and restored after overlays and asynchronous projections.
- Modals trap focus and return it to the invoking control.
- Status, validation, loading, and request failures use appropriate live regions.
- Charts have concise descriptions and exact labels. Interactive marks expose
  stable focus and activation semantics, while the table remains the complete
  accessible data representation.
- Information does not depend on color alone. Active and selected states use
  shape, border, text, or pattern cues in addition to color.
- Reduced-motion preferences disable nonessential chart and overlay animation.
- `kit-ui` contrast and breakpoint tokens are used instead of local substitutes.

## Static Distribution and Embedding

Production Vite output, including its manifest and hashed assets, is copied into
the Go embedding package and committed. A normal source checkout can therefore
build a working web-enabled Go binary without Node or Bun.

The embedded handler:

- validates that `index.html`, the Vite manifest, and every referenced asset exist
- rejects compilation stubs as release distributions
- serves fixed safe content types
- gives hashed manifest assets immutable caching
- gives HTML and nonhashed files `no-store` or revalidation behavior
- supports GET and HEAD only
- rejects unsafe, hidden, credential-like, dotted-navigation, and traversal paths
- reserves API and OpenAPI paths from single-page navigation fallback
- adds a strict content security policy, `nosniff`, frame denial, and no-referrer
  policy

The frontend build writes a production marker that release validation checks.
Generation has an explicit update command. Ordinary Go and frontend tests verify
the committed distribution without rewriting it.

## Error Handling

Huma emits one stable problem-details envelope for malformed JSON, URL-state
validation, unsupported actions, invalid regular expressions, missing targets,
and internal query failures. Errors include a safe code and concise detail but do
not echo a complete search query, URL, transaction, or provider payload.

The browser uses these rules:

- A live-search validation error leaves the last good projection visible.
- URL or selection bounds fail with their stable safe codes and the no-change or
  reset behavior specified above; the browser never truncates either state.
- A transient network or server failure retains the last good projection,
  announces the failure, and offers retry.
- A stale bookmarked entity key shows an invalid-view screen with Back and Reset.
  It is not silently interpreted as an empty or different drill-down.
- A stale response cannot overwrite a newer state. Each request carries a local
  generation, and obsolete results are discarded or aborted.
- An empty valid result is a normal projection with the existing TUI empty-state
  meaning, not an error.

Unexpected panics are recovered at the HTTP boundary, logged without request
queries or financial payloads, and returned as a generic error. The browser never
renders server detail as HTML.

## Security and Trust Boundary

The API and SPA are same-origin. The server sends no cross-origin resource
sharing headers, accepts no browser credentials, sets no cookies, and exposes no
write operations.

Loopback is the default trust boundary. An explicit tailnet listener trusts all
peers allowed by the surrounding network policy. A reverse proxy may add HTTPS
or authentication, but moneyflow does not infer or enforce proxy identity in this
slice. Forwarded headers do not change authorization because no authorization
decision exists.

The server does not log raw URLs, query strings, response bodies, or row targets.
Startup and error logs contain only safe operation context. Synthetic fixtures,
screenshots, tests, and documentation use clearly fictional financial data.

## Testing Strategy

Implementation follows test-driven development in both Go and TypeScript.

### Go application and API tests

- Action-registry tests keep TUI keys, help text, availability, and web
  capabilities synchronized.
- Existing session and interaction scenarios remain green through any state
  refactor.
- URL codec table and property tests cover canonical round trips, Unicode search,
  repeated drill paths, typed periods, defaults, malformed fields, exact size
  boundaries, no-change failures, and base paths.
- Selection-codec tests cover explicit and all bases, query changes after
  select-all, inclusion and exclusion deltas, smallest exact normalization,
  identity kinds, every size boundary, and rejected-toggle preservation.
- Huma handlers are exercised through `httptest` with strict body decoding,
  window boundaries, exact money strings, partition separation, stable targets,
  problem details, OpenAPI output, and headers.
- Command tests inject an ephemeral listener and fake browser opener. They cover
  default loopback, explicit concrete listeners, wildcard rejection, base-path
  normalization, startup failures, and graceful shutdown.
- Embedded-handler tests cover manifests, content types, caching, GET/HEAD,
  traversal, reserved routes, navigation fallback, security headers, and release
  validation.

### Frontend tests

- Pure tests cover URL/history coordination, generated client adapters, money
  display passthrough, opaque selection transport, projection caches, and cursor
  restoration.
- Component tests cover shortcut scopes, virtual focus, table sorting, overlays,
  live-search apply/cancel, filters, selection clearing, chart linkage, empty
  states, and responsive column priority.
- Generated API and embedded distribution checks fail when committed artifacts
  are stale.
- TypeScript, Svelte, lint, formatting, `kit-ui-check`, and dependency audit
  commands run with zero warnings treated as errors where supported.

### Browser and parity tests

Playwright drives the actual embedded server and complete keyboard workflows:

- every top-level grouping and all-detail view
- multi-level drill-down, subgroup cycles, and restoration
- time granularity, period navigation, and clear-time behavior
- sort cycles and direction reversal
- live search success, invalid input, commit, and cancel
- filter apply and cancel
- row and select-all behavior
- help and overlay shortcut isolation
- chart/table cursor and drill synchronization
- browser Back, Forward, refresh, and direct bookmarks
- root and non-root base paths
- desktop, medium, and narrow layouts

The committed renderer-neutral interaction corpus is reused wherever possible.
The browser must reach the same analytical state, breadcrumb, row ordering,
flags, identities, and restoration outcome as the Go session and TUI. Web-only
history and layout behaviors receive separate scenarios rather than changing the
TUI baseline.

Chromium supplies reviewed web visual screenshots in light and dark themes at
representative widths. Chromium, Firefox, and WebKit run behavioral smoke tests.
Automated accessibility checks use axe, supplemented by focus-order, modal,
announcement, chart-label, contrast, reduced-motion, and keyboard-only tests.

No browser test reads a real profile, home directory, credential, cache, or
provider. Every server uses the committed synthetic fixture and an ephemeral
listener.

## Performance Contract

The existing analytics target remains below 50 milliseconds for a complete
100,000-transaction query on the documented reference machine, with the existing
generous continuous-integration smoke ceiling.

The new reference target is below 100 milliseconds for URL decoding, query,
200-row window projection, chart projection, and JSON encoding on the same
machine. A looser smoke ceiling catches gross regressions in continuous
integration without treating noisy timing as correctness.

Benchmarks record response bytes, allocations, initial compressed JavaScript and
CSS size, and chart interaction latency. The implementation records realistic
budgets from its first reviewed production build, then turns those measurements
into regression thresholds. It must not invent an arbitrary bundle threshold
before `kit-ui` and LayerChart are linked.

The frontend keeps only the current and adjacent row windows. Cursor movement
within a loaded window is synchronous and does not wait for the network. Requests
are cancellable, and prefetch never blocks a user-initiated transition.

## Developer Workflow

The slice adds stable Make targets for frontend generation, frontend checking,
asset updating, web demo startup, and complete web verification. Existing Go and
Python verification remains part of the final gate.

The expected workflow is:

1. Write a failing focused test.
2. Implement one application, API, component, or workflow behavior.
3. Run its focused Go or frontend checks.
4. Run the relevant parity and browser scenarios.
5. Regenerate OpenAPI, TypeScript, or embedded assets only through explicit
   commands and review the artifact diff.
6. Run the complete Go, Python, frontend, browser, cross-platform, and privacy
   gates before committing a completed implementation slice.

Development Vite requests proxy the configured Go API under the same base-path
contract. Production and browser tests exercise the embedded distribution so the
development proxy cannot hide routing or asset mistakes.

## Completion Criteria

This slice is complete only when fresh evidence shows all of the following:

- the existing Go foundation, TUI, Python parity, and visual artifact checks pass
- every implemented read-only TUI refinement workflow works from the browser
- the Go action registry drives TUI help and web capabilities without drift
- URL state is strict, canonical, bookmarkable, refresh-safe, and base-path-safe
- URL and selection bounds reject without truncation or unintended state changes
- browser Back/Forward and `Esc` satisfy the approved restoration contract
- the API is stateless, strictly decoded, windowed, and server-authoritative
- exact money strings and `(currency, scale)` partitions survive every wire path
- no complete raw transaction dataset or provider metadata reaches the browser
- the table remains the primary focus surface at desktop and narrow widths
- LayerChart views track the table cursor and use only server chart projections
- keyboard-only and automated accessibility checks pass
- generated OpenAPI, TypeScript, and embedded assets are current and reviewed
- the production distribution passes hardened embedded-handler validation
- loopback, explicit tailnet address, reverse-proxy base path, and shutdown tests pass
- the 100,000-row analytics and 200-row projection performance contracts are met
- Go race, vet, format, lint, cross-platform build, and dependency checks pass
- frontend type, Svelte, lint, format, unit, browser, and dependency checks pass
- synthetic fixtures, screenshots, documentation, and logs contain no personal data
- the diff contains no SQLite, provider, credential, edit, or authentication code

Completion does not authorize pushing, merging `go-port`, removing Python, or
starting persistence work without its own approved design and implementation plan.

## Later Replacement Slices

After this slice, the replacement remains ordered as follows:

1. SQLite profiles, local edits, undo/redo, review, commit, export, and migration,
   extended through both the TUI and web application.
2. Provider adapters ported and verified one at a time: SimpleFIN, Monarch Money,
   YNAB, and Amazon.
3. Packaging, state migration, complete parity audit, Python removal, and cutover.

Richer charts can be added incrementally after the bar and summary contract is
proven. They must continue to consume server-authoritative projections and must
not move accounting or analytics into TypeScript.
