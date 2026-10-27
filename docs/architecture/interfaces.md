# Interfaces and process boundaries

Moneyflow has three interfaces: the browser app, the terminal app (TUI), and the MCP server.
Each is an adapter over the same [application service][source-3] in `internal/app`. None of
them queries SQLite, calls a provider, or computes accounting itself, so an edit, a refresh, or a
commit behaves the same whichever interface starts it. A Go program can also
[embed Moneyflow](#embed-moneyflow-in-another-program) and serve the same API, tools, and
schedule.

```text
 browser (web/)      terminal (internal/tui)      MCP client
      |                     |                         |
 internal/api          (direct calls)            internal/mcp
      \                     |                         /
       +----------- internal/app.Service ------------+
                            |
          analytics, replay, store, provider contracts
```

## Shared application layer

[Cobra commands][source-1] compose dependencies and own process lifetime. Bare `moneyflow`
prints help. `moneyflow web`, `moneyflow tui`, and `moneyflow mcp` are sibling commands.
`--demo` and the hidden `--fixture` flag use temporary profiles. The TUI's initial date flags
(`--mtd`, `--since`, `--year`) filter the local view; they never turn a provider refresh into a
date-window reconciliation.

The [session][source-2] owns renderer-neutral navigation, search, sort, selection, and return
positions. The application service projects rows and coordinates mutations. Each presenter owns
its focus, scrolling, dialogs, and input mapping. The [action registry][source-4] gives every
action a stable identity, a key, and shared availability, so no renderer duplicates provider
policy. The browser rejects a server capability whose key display disagrees with its shortcut
table (`web/src/lib/shortcuts.ts`), so the two interfaces cannot drift apart silently.

Stable keys, row ordering, focused-target precedence, bulk-selection clearing, and `w` then
Enter are behavior contracts for both renderers. Widget chrome, cell colors, and padding are
not.

## Catalog and onboarding

Interactive startup selects a catalog profile, adds one, starts an unregistered demo, or offers
explicit recovery for an incompatible profile. The listing is local-only: manifest, schema,
binding, and credential or session presence do not prove that a live session is valid.

[Onboarding][source-5] is the shared connection coordinator. The CLI, the TUI, and the browser
present its versioned attempt state. Inspection and saved-session validation come before import
settings when saved state already supplies them. Binding must not orphan an unbound profile's
local journal or committed data.

- Connection attempts use advisory locking and expected state versions.
- Cancel stops any attempt that has not completed, even when the client's state version is
  behind, because import progress changes the version. A completed attempt keeps its result.
- Cancelling a new profile removes it only while it holds no saved state and no SimpleFIN claim
  has begun for it. A claim can consume its token even when its answer is lost.
- Passwords and tokens never appear in status snapshots, URLs, error bodies, or history.
- A valid saved session can skip credential entry. A failed import can retry from saved session
  material.
- A cancelled empty addition can roll back. A profile with durable session, vault, or import
  state stays recoverable.
- YNAB presents attempt-scoped plan choices and derives currency and scale from the chosen plan.
- Monarch, SimpleFIN, and Amazon setup ask only for a currency. The scale is its ISO 4217 decimal
  places, such as 2 for USD and 0 for JPY. An API or CLI caller can still name a scale.
- Amazon uses explicit currency confirmation, optional one-time taxonomy cloning, and file
  source selection.
- Recovery ends with a pristine database and preserved credential files. Reconnect is the normal
  onboarding flow, not a hidden import during recovery.

## Terminal

The TUI is a Bubble Tea program in `internal/tui` that calls the application service directly
in-process. It maps keys to registry actions and renders frames from service projections. Tests
drive it with key messages and assert on model state and rendered facts; see
[UI testing](../development/ui-testing.md#terminal-tests).

## Web

The [Huma API][source-6] sits on `net/http`. The browser app uses kit-ui components and the same
application transitions as the TUI. One single-page app serves the configured base path.
Profile URLs and API routes use `/p/<profile-id>/`; shared assets stay at the base path.

### View responses

Besides its rows and `currencies`, each view response carries:

- `selection_summary`: the selected transactions per currency.
- `staged` values on rows with pending edits.
- On aggregate rows: `savings_rate_tenths` and `net_ratio` on Time rows, and `group` on
  category rows. `net_ratio` is the row's net over the largest absolute net of the whole result,
  in basis points (−10000 to 10000). It does not change when the row window moves.
- `view.sort_fields`: the fields the current view can sort by, in the order the sort key cycles.
- `all_rows_selected`: true when the result has rows and the selection holds all of them.
  Selected rows outside the result, such as after a provider refresh, do not count.
- `view.drill_depth`: the number of drill frames in the current view.
- `view.root`: the view that `view.back-to` with depth 0 reaches, as its `mode`, `grouping`, and
  `time_granularity`. The server computes it by restoring a copy of the session from the view
  state and stepping it back to depth 0, so it matches that transition, and a Time root always
  names its unit. The breadcrumb trail reads both fields instead of parsing return frames.

A view request may carry an `anchor`, an aggregate row identity. The server returns the window
that holds that row and reports its global index as `anchor_index`. An identity that matches no
row leaves the window as requested, omits `anchor_index`, and adds the warning `anchor_missing`.

A projection reports a change the user did not ask for as the warning `view_notice`, for example
an undo or redo that returns to the parent view because the drilled target is gone, or a provider
refresh that clears the selection. The browser shows the warnings in a status line notice.

### Edit responses

- A merchant preview reports `source_merchant_id`, the merchant every affected transaction has
  now. Whole-merchant scope always sets it; chosen transactions set it only when they share one
  merchant. The editor compares it with the matched merchant's ID to decide whether a rename
  [merges into another merchant](../guide/editing.md#open-the-editor). A whole-merchant preview
  also reports how many affected transactions the view hides, with `outside_view` and
  `outside_currencies`.
- A mutate, undo, or redo response carries `operation`, from which the browser words its status
  line notices: the `kind` (`rename`, `recategorize`, `hide`, `show`, `delete`,
  `manage_category`, or `manage_group`), the review `label`, the `affected` transaction count,
  and `undoable`. A commit response has no `operation`.
- Each editor-catalog choice carries `transaction_count`, its committed non-deleted
  transactions, and `in_use`, which applies pending edits and makes a manager delete ask for a
  replacement.
- Review targets carry each transaction's exact `amount`.
- An `invalid_operation` problem (the `Problem` a refused operation returns) can carry
  `reason`. The server sets it only from the store's allowlisted reasons, which the schema
  lists as an enum. For example, `taxonomy_label_collision` marks a category or group name
  another entry already uses. The browser words a refusal from `reason`, never from `detail`
  text.

### Charts

Charts read from a separate operation, `POST /api/v1/profiles/{profile_id}/view/insights`. It
takes the view's query and optional row identities and returns committed chart data from
`Service.Insights`.

The browser keeps insights in `InsightsCache` (`web/src/lib/insights/cache.svelte.ts`):

- Each entry is keyed by the canonical query without its `sort` and by the profile revision, so
  a sort change reuses the entry.
- It keeps the eight most recently used entries.
- It loads the summary once and row series incrementally. It asks only for rows of the loaded
  and neighboring table windows that are neither loaded nor in flight, at most 200 per request,
  with the summary on the first request.
- Until the displayed view's entry arrives, the charts keep drawing the previous entry, dimmed
  and inert, as the [web guide](../guide/web.md#charts) describes.
- A response at a newer revision than the displayed projection asks the view controller to
  recheck the view, once per revision. Those insights show only after the projection reaches
  that revision.
- When the server's day changes, the cache drops every entry and loads the view again.

Charts are hand-written Svelte components; the app uses no chart library. The cash-flow chart
and the table's trend cells draw inline SVG. The composition bars, calendar, and KPI tiles are
HTML and CSS. Chart layout and scale logic lives in `web/src/lib/charts/`; the trend cells'
sparkline layout lives in `web/src/lib/table/trend.ts`.

### URL state and selection

The analytical URL is durable state: versioned view parameters, search, filters, drills, and
return frames. Cursor, scroll, and transient selection are not profile data. The server owns
canonical transitions and validation. The browser's history ledger is a navigation convenience,
not authority for a mutation or for deriving a parent view.

The [view codec][source-7] and [selection][source-8] bound decoded and encoded inputs. Windowed
projections never send every transaction ID to the browser. A complete-result selection keeps
its defining state and resolves through the service. Mutations carry the expected revision; a
partial stale selection is never applied.

### HTTP security and limits

The browser server is for local or private-network access, optionally behind a reverse proxy
such as Caddy. It is not an Internet multiuser authentication service. [API security][source-9]
and [HTTP security][source-10] implement canonical origin and base-path consistency, mutation
tokens, Origin and Fetch Metadata checks, no CORS, no query logging, and no-store HTML. Tokens
never enter analytical URLs or history. Every API request omits credentials, so a proxy that
authenticates with cookies blocks reads as well as edits.

Token-expiry refresh is separate from revision-conflict handling. The first can retry a
rejected, unevaluated request once; the second always needs explicit user action.

The API buffers ordinary successful responses up to 8 MiB before sending them. A larger
response returns HTTP 500 with `response_too_large`, never a truncated success. A response
failure does not undo an applied mutation; refresh the view before deciding whether to retry.
Buffering lets panic recovery replace a partial response with a safe error. Error sanitization
buffers at most 1 MiB. Data exports stream separately and are not subject to these limits.

## MCP

[MCP][source-11] uses the official Go SDK behind renderer-neutral services. A server resolves
one profile. Stdio keeps protocol output on stdout and diagnostics on stderr. Streamable HTTP is
stateless, with an exact endpoint, a bearer token, a canonical Host, and optional Origin
validation. This is separate from the browser's mutation-token model.

The SDK is pinned to v1.7.0 for the MCP 2026-07-28 contract: discovery, per-request metadata,
and complete-result envelopes. Moneyflow's document version is independent. Receiving
middleware marks profile lists and resources private with zero cache lifetime; the outer HTTP
writer keeps `no-store` even when the SDK sets transport cache headers.

### Read and write policy

- Read-only is the default. `--allow-write` adds staging tools, explicit commit, undo and redo,
  and batch controls. It never permits direct provider mutations.
- Dry run validates without appending.
- Read tools return exact money strings and minor units, not JSON floats.
- Literal search includes notes, matching the Python MCP server, and leaves the TUI's regular
  expression search unchanged.

`get_insights` returns the same chart data as the web insights operation, from the same
`Service.Insights` primitive over **committed** values. Transaction reads apply pending edits;
insights do not. The adapter builds an analytical state by navigating a shared `app.Session`:

- ID filters become drill-downs in the order merchant, category, group, account.
- `group_by` is the top-level grouping without filters and the sub-grouping below them. Omitting
  it requests a transaction view.
- Keys become aggregate row identities of that grouping and currency.
- Invalid views return `invalid_operation`, and its `detail` gives the reason.

The document mirrors the HTTP response's sections but carries every amount as an MCP money
object.

### Editing tools

Category and merchant assignment, whole-merchant rename and explicit merge, hide cancellation,
and transaction deletion reuse the application mutation planner and provider checks.
Transaction batches are atomic and bounded to 100 unique IDs. Whole-merchant edits report the
full affected count with a bounded preview. Shared replay produces merchant and delete previews.
Hide cancellation removes the parity of all originating active toggles. Deleted rows have
`after: null`.

`manage_category` and `manage_category_group` expose the C and G operations on local, Amazon,
and SimpleFIN profiles:

- The adapter checks action-specific input fields and allocates new local entity IDs. The
  application taxonomy planner owns identity, collision, protected-entity, reassignment, and
  provider validation. Dry run and staging both use it.
- The shared replay preview reports transaction changes and a separately bounded,
  stable-ID-ordered entity diff, including creation and retirement with no transaction rows.
  Each list is capped at 100 with full counts; the mutation itself is not truncated.
- Preview IDs are provisional. Reuse only the staged result's ID.
- Taxonomy creation on a pristine profile accepts revision zero with an exact application
  revision check. Every other MCP mutation and batch control requires a nonzero revision or
  version: zero must never activate the application's optional reconciliation-check bypass.
- Authoritative checks stay in the ordinary application and store path. No provider taxonomy
  API or schema change is involved.

### Export tools

`preview_export` and `export_transactions` reuse `Service.PreviewExport`,
`Service.PreviewTransactionExport`, `Service.CaptureExport`, and `internal/exporter.WriteFile`.

- They default to full committed scope. Explicit filtered scope shares the transaction-read
  input parser and application predicates, without pagination. Full scope rejects filter input.
- Category resolution uses committed taxonomy, not pending renames or creates.
- The optional application `TransactionFilter` replaces analytical `ViewState` predicates for
  that request; TUI and web callers keep using `ViewState`.
- Filtered metadata records deterministic JSON tagged `mcp_transactions_v1` in
  `canonical_query`, distinct from analytical URL encoding.
- No provider I/O runs, including during an unfinished batch.
- Preview has no filesystem side effects and takes no export lock. Execution captures the
  current revision under the export lock and atomically publishes a private Parquet (default),
  CSV, or SQLite file.
- The bounded reply gives a server-side path, captured revision, size, row count, and journal
  exclusion counts. It is not an attachment or a download. No caller-supplied path is accepted.
- Both tools register without `--allow-write`, but file creation carries a non-read-only
  protocol hint. Only the calling client's result may contain the export path, never
  diagnostics. Browser downloads remain a separate web flow.

### YNAB unlock and long operations

The MCP launcher opens YNAB offline by default. `--unlock` reads the vault password from the
controlling terminal, not protocol stdin or stdout, and configures that process's reader and
writer. It holds the provider-connect lock from inspection through runtime configuration, so a
concurrent reconnect cannot pair an old decrypted token with a replacement vault's fingerprint.

- Unlock does not fetch, refresh, or resume a batch at startup.
- `--allow-write` separately registers edit tools. Unlock alone still permits explicit refresh
  under the read-only contract.
- A wrong password, a missing vault, or a binding mismatch stops startup and releases the
  profile.
- A restart or a vault replacement needs explicit unlock again. A TUI or CLI unlock does not
  unlock MCP memory.
- Headless clients without a controlling terminal use an HTTP server started and unlocked from a
  terminal, or stay offline. Passwords never travel in tool parameters or environment flags.

Commit and refresh calls use a bounded supervisor and expose status and resume instead of
holding a tool call open through a many-minute provider operation. MCP has no automatic
refresh schedule. Explicit refresh and refresh-deletion confirmation stay available under the
read-only definition, including the documented rebase and redo side effects.

[Documents][source-12] cap row windows at 1,000 and combined structured and text response
content at 8 MiB. Oversize results return a bounded failure. HTTP request bodies have their own
1 MiB bound. Raw provider or credential data never appears in public status or error payloads.

## Embed Moneyflow in another program

The root Go package `github.com/wesm/moneyflow` runs Moneyflow inside a host process.
`moneyflow.Open` prepares one data directory, laid out like `MONEYFLOW_HOME`, and returns an
`Instance`. The standalone command builds its providers through the same `internal/assembly`
code.

| Method | What it gives the host |
| --- | --- |
| `Profiles` | Each profile's state, such as `ready`, `needs_setup`, `needs_unlock`, `refresh_failed`, or `write_attention` |
| `API` | The HTTP API under the host's `APIBasePath`; the host admits each request, so the instance checks no origin or mutation token |
| `RegisterTools` | The MCP tools on a host-built SDK server |
| `RunScheduler` | Due provider refreshes until its context ends |
| `ImportProfiles` | A copy of a standalone data directory's profiles; the source is not changed |
| `Backup`, `Verify` | A credential-free copy of the data directory, and a check of one |
| `Unlock` | A YNAB vault unlocked with a password the host supplies |
| `Close` | Stops background work and waits up to 30 seconds |

`Open` succeeds even when some profiles are broken. The API, the MCP tools, and the scheduler
share one refresh and one reconcile per profile; see
[refresh sharing](providers.md#refresh-sharing). After `Close` starts, API requests get a
retryable 503 `profile_busy`. A panic in background work, a tool call, or setup goes to the
host's logger, and the host keeps running.

Limits: `Unlock` covers YNAB only. Profile errors can include local paths. An instance keeps
every profile it opened until `Close`. `ImportProfiles` holds catalog changes while it copies,
and `Verify` takes no locks. The `embed-consumer` CI job builds a module outside the repository
that imports the package, and fails if the package pulls in the TUI, the CLI, or the web app.

A Svelte 5 host can render the browser workspace and provider setup from `web/src/lib/views`,
which exports `MoneyflowWorkspace`, `MoneyflowSetup`, `viewsFor`, and `historyViewState`. The
standalone app renders the same components. In a host:

- The views send plain requests through the host's `fetch` under the host's API base, with no
  token header. Standalone Moneyflow passes a fetch that adds its mutation tokens.
- Query and history state go through a `ViewStateAdapter`.
- With `chrome` off, the host owns the sidebar, landmarks, banner, and page-wide keys. Styles
  are scoped under `.mf-views`.
- `readOnly` turns off every change. `autoRefresh` decides only whether the workspace starts
  refreshes that are due.
- `MoneyflowSetup` creates a profile only with a name the host supplies.

Every API operation and MCP tool has one class: `read`, `stage`, or `commit`. The standalone web
server wraps the same API handler with its canonical-origin and mutation-token checks; an
embedded instance checks neither. The root `package.json` makes the repository the
`moneyflow-views` Svelte package, which a host installs from the same Moneyflow commit or tag as
the Go module. [Embedding Moneyflow](../development/embedding.md) owns the host contract.

## Credentials, cancellation, and process ownership

The profile database is not encrypted. The [credential vault][source-13] uses password-derived
encryption for secrets outside SQLite; provider-specific files hold each provider's session
format. Existing `internal/home` helpers own private publication and permissions. Do not add
platform keychain assumptions to an otherwise portable credential flow.

Every asynchronous job has an owner, cancellation behavior, and a cleanup path. SQLite commits
are atomic; a disconnected client never justifies a partially applied local fold. Network
responses can stay uncertain, which is why outbound intent and results are durable and
reconcilable. Closing a renderer is neither a rollback mechanism nor permission to abandon
recovery state.

Privacy is an allowlist: counts, timings, revisions, opaque operation and correlation IDs, and
stable reason codes where their contract permits them. File chooser, import, and export details
shown to the user are explicit exceptions, not permission to persist paths or financial labels
in logs.

[source-1]: https://github.com/wesm/moneyflow/tree/main/cmd/moneyflow
[source-2]: https://github.com/wesm/moneyflow/blob/main/internal/app/session.go
[source-3]: https://github.com/wesm/moneyflow/blob/main/internal/app/service.go
[source-4]: https://github.com/wesm/moneyflow/blob/main/internal/app/actions.go
[source-5]: https://github.com/wesm/moneyflow/tree/main/internal/onboarding
[source-6]: https://github.com/wesm/moneyflow/tree/main/internal/api
[source-7]: https://github.com/wesm/moneyflow/blob/main/internal/api/viewstate.go
[source-8]: https://github.com/wesm/moneyflow/blob/main/internal/app/selection.go
[source-9]: https://github.com/wesm/moneyflow/blob/main/internal/api/security.go
[source-10]: https://github.com/wesm/moneyflow/tree/main/internal/httpsecurity
[source-11]: https://github.com/wesm/moneyflow/tree/main/internal/mcp
[source-12]: https://github.com/wesm/moneyflow/blob/main/internal/mcp/documents.go
[source-13]: https://github.com/wesm/moneyflow/tree/main/internal/credentialvault
