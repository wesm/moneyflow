# Interfaces and process boundaries

## Commands and application state

[Cobra commands][source-1] compose dependencies and manage process lifetime. Bare
`moneyflow` prints help. `moneyflow tui` and `moneyflow web` are explicit sibling commands.
`--demo` and `--fixture` use temporary profiles. Initial date flags filter the local view; they
do not turn subsequent full provider refresh into a date-window reconciliation.

[Session][source-2] owns renderer-neutral navigation, search, sort, selection,
and return positions. [Application service][source-3] projects rows and
coordinates mutations. Presenters own focus, scroll presentation, dialogs, and input mapping.
The [action registry][source-4] gives actions stable identities and shared
availability rather than duplicating provider policy in each renderer.

Stable keys, row ordering, focused-target precedence, bulk selection clearing, and `w` then Enter
are behavior contracts. Textual widget chrome, cell colors, and every padding choice are not.
TUI behavior is now checked directly with model/key events and rendered facts, not Python frames.

## Catalog and onboarding

Interactive startup selects a catalog profile, adds a new one, starts an unregistered demo, or
offers explicit recovery for an incompatible profile. Status listing is local-only: manifest,
schema, binding, and credential/session presence do not prove a live session is valid.

[Onboarding][source-5] is the shared connection coordinator. The CLI, TUI, and
web are presenters for its versioned attempt state. Inspection and retained-session validation
precede import settings where saved state already provides them. Binding must not orphan an
unbound profile's existing local journal or committed data.

Connection attempts use advisory locking and expected state versions. Passwords and tokens do not
appear in status snapshots, URLs, error bodies, or history. A valid saved session can skip credential
entry; failed import can retry from saved session material. Cancelled empty additions can roll back,
while a profile with durable session/vault/import state remains recoverable.

YNAB presents attempt-scoped plan choices and derives currency/scale from the selected plan. Amazon
uses explicit currency confirmation, optional one-time taxonomy cloning, and file source selection.
Recovery ends with a pristine database and preserved credential files; reconnect is the normal
onboarding flow, not a hidden import during recovery.

## Web

[Huma API][source-6] sits on `net/http`. The web app uses kit-ui components and the same
application transitions as the TUI. One SPA serves the configured base path; profile application
URLs and API routes use `/p/<profile-id>/`. Shared assets remain at the base path.

The analytical URL is durable state: versioned view parameters, search, filters, drills, and return
frames. Cursor/scroll and transient selection are not financial profile data. The server owns
canonical transitions and validation; the browser history ledger is a navigation convenience,
not authority for mutation or parent derivation.

[View codec][source-7] and [selection][source-8]
bound decoded and encoded inputs. Windowed projections do not send every transaction ID to the
browser. Complete-result selection retains its defining state and resolves through the service.
Mutations carry expected revision; no partial stale selection is applied.

The browser server is intended for local/private-network access, optionally through a reverse
proxy such as Caddy. It is not an Internet multiuser authentication service. Canonical origin and
base-path consistency, mutation tokens, Origin/Fetch Metadata checks, no CORS, no query logging,
and no-store HTML are implemented in [API security][source-9] and
[HTTP security][source-10]. Tokens do not enter analytical URLs or history.

Token-expiry refresh and retry is separate from revision-conflict handling. The former can retry
a rejected, unevaluated request once; the latter always requires explicit user action. Data export
is an intentional large-download exception to bounded projection responses.

## MCP

[MCP][source-11] uses the official Go SDK behind renderer-neutral services. It resolves
one profile per server. Stdio keeps protocol output on stdout and diagnostics on stderr.
Streamable HTTP is deliberately stateless, with an exact endpoint, bearer token, canonical Host,
and optional Origin validation. This is distinct from the browser mutation-token model.

The SDK is pinned to v1.7.0 for the MCP 2026-07-28 contract: discovery, per-request metadata,
and complete-result envelopes. The Moneyflow document version remains independent. Receiving
middleware marks profile lists/resources private with zero cache lifetime; the outer HTTP writer
retains `no-store` even when the SDK sets transport cache headers.

Read-only is the default. `--allow-write` adds staging tools, explicit commit, undo/redo, and batch
controls; it does not permit direct provider mutations. Dry run validates without appending.
Read tools return exact money strings/minor units, not JSON floats. Literal search includes notes,
preserving Python MCP's search behavior without altering the TUI's regex search.

Category and merchant assignment, whole-merchant rename/explicit merge, hide cancellation, and
transaction deletion reuse the application mutation planner and provider checks. Transaction batches
are atomic and bounded to 100 unique IDs. Whole-merchant edits report the full affected count with
a bounded preview. Shared replay produces merchant/delete previews; hide cancellation removes the
parity of all originating active toggles. Deleted rows have `after: null`.

`manage_category` and `manage_category_group` expose the existing C/G operations on local/Amazon
profiles. The MCP adapter checks action-specific input fields and allocates new local entity IDs;
the application taxonomy planner owns identity, collision, protected-entity, reassignment, and
provider validation. Both dry run and staging use that planner. The shared replay preview reports
transaction changes and a separately bounded, stable-ID-ordered entity diff, including creation
and retirement with no transaction rows. Each MCP list is capped at 100 with full counts; the
mutation is not truncated. Preview IDs are provisional, and only the staged result's ID should
be reused. Taxonomy creation on pristine profiles accepts revision zero with an exact application
revision check. Other MCP mutations and batch controls require nonzero revisions/versions: zero
must never activate the application's optional reconciliation-check bypass. Authoritative checks
remain in the ordinary application/store path. No provider taxonomy API or schema change is involved.

`preview_export` and `export_transactions` reuse `Service.PreviewExport`,
`Service.PreviewTransactionExport`, `Service.CaptureExport`, and `internal/exporter.WriteFile`.
They default to full committed scope; explicit filtered scope shares the transaction-read input
parser and application predicates without pagination. Category resolution uses committed taxonomy,
not pending renames or creates. The optional application `TransactionFilter` replaces analytical
`ViewState` predicates for that request; existing TUI/web callers continue using `ViewState`.
Filtered MCP metadata records deterministic JSON tagged `mcp_transactions_v1` in `canonical_query`,
distinct from analytical URL encoding. Full scope rejects filter input. No provider I/O runs,
including during an unfinished batch. Preview has no filesystem side effects or export lock;
execution captures the current revision under the export lock and atomically publishes a private
Parquet (default), CSV, or SQLite file. The bounded reply identifies a server-side path, captured
revision, size, row count, and journal exclusion counts; it is not an attachment or a download.
No caller-supplied path is accepted. Both tools are registered without `--allow-write`, but file
creation carries a non-read-only protocol hint. Only the calling client's result may contain the
export path, not diagnostics. Browser downloads remain a separate web UI flow.

The MCP launcher opens YNAB offline by default. `--unlock` reads the existing vault password from
the controlling terminal, not protocol stdin/stdout, and configures that process's reader/writer.
It holds the provider-connect lock from inspection through runtime configuration so a concurrent
reconnect cannot pair an old decrypted token with a replacement vault's fingerprint.
It does not fetch, refresh, or resume a batch at startup. `--allow-write` separately registers edit
tools; unlock alone still permits explicit refresh under the read-only contract. Wrong passwords,
missing vaults, or binding mismatches stop startup and release the profile. Restart or vault
replacement requires explicit unlock again. A separate TUI/CLI unlock does not unlock MCP memory.
Headless clients without a controlling terminal must use an HTTP server started and unlocked from
a terminal, or retain offline access. Passwords never travel in tool parameters or environment flags.

Commit/refresh calls use the bounded supervisor and expose status/resume rather than holding a
tool call open until a many-minute provider operation finishes. MCP has no automatic freshness
scheduler. Explicit refresh and refresh-deletion confirmation remain available under its read-only
definition, including the documented rebase/redo side effects.

[Documents][source-12] cap row windows at 1,000 and combined structured/text
response content at 8 MiB. Oversize results return a bounded failure. HTTP request bodies have
their own 1 MiB bound. Raw provider or credential data is not part of public status/error payloads.

## Credentials, cancellation, and process ownership

The profile database is not encrypted. [Credential vault][source-13] uses
password-derived encryption for secrets outside SQLite; provider-specific files hold the provider's
session format. Existing home helpers own private publication and permissions. Do not add platform
keychain assumptions to an otherwise portable credential flow.

Every asynchronous job has an owner, cancellation behavior, and cleanup path. SQLite commits are
atomic; disconnected clients do not justify a partially applied local fold. Network responses can
remain uncertain, which is why outbound intent/results are durable and reconcilable. Closing a
renderer is neither a transaction rollback mechanism nor permission to abandon recovery state.

Privacy is an allowlist: counts, timings, revisions, opaque operation/correlation identifiers,
and stable reason codes where their contract permits them. User-facing file chooser/import/export
details are explicit exceptions, not permission to persist paths or financial labels in logs.

[source-1]: https://github.com/wesm/moneyflow/tree/go-port/cmd/moneyflow
[source-2]: https://github.com/wesm/moneyflow/blob/go-port/internal/app/session.go
[source-3]: https://github.com/wesm/moneyflow/blob/go-port/internal/app/service.go
[source-4]: https://github.com/wesm/moneyflow/blob/go-port/internal/app/actions.go
[source-5]: https://github.com/wesm/moneyflow/tree/go-port/internal/onboarding
[source-6]: https://github.com/wesm/moneyflow/tree/go-port/internal/api
[source-7]: https://github.com/wesm/moneyflow/blob/go-port/internal/api/viewstate.go
[source-8]: https://github.com/wesm/moneyflow/blob/go-port/internal/app/selection.go
[source-9]: https://github.com/wesm/moneyflow/blob/go-port/internal/api/security.go
[source-10]: https://github.com/wesm/moneyflow/tree/go-port/internal/httpsecurity
[source-11]: https://github.com/wesm/moneyflow/tree/go-port/internal/mcp
[source-12]: https://github.com/wesm/moneyflow/blob/go-port/internal/mcp/documents.go
[source-13]: https://github.com/wesm/moneyflow/tree/go-port/internal/credentialvault
