# Go Port MCP Server Design

**Date:** 2026-08-21
**Status:** Approved
**Branch:** `go-port`

## Summary

This slice ports Moneyflow's Model Context Protocol server from Python to Go. It exposes one
resolved Moneyflow profile through the same renderer-neutral application service used by the TUI
and web application. The server supports stdio for local process integration and authenticated,
loopback-only streamable HTTP for use through a trusted reverse proxy such as Caddy on a tailnet.

The Go MCP server is not another application implementation. It does not query SQLite, call a
provider, calculate analytics, or reproduce mutation logic. Read tools project the current
effective application snapshot. Mutation tools append ordinary revision-checked journal
operations. Explicit review and commit tools use the same local fold or durable Monarch write
batch as the interactive renderers.

The Python MCP surface remains the compatibility baseline: all twelve tools and all five resources
are represented. Go adds the review, undo/redo, commit, refresh-status, and durable-batch controls
required by its staged editing architecture. Financial amounts use exact decimal strings and
integer minor units rather than JSON floating-point numbers.

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
- `2026-08-20-go-port-amazon-import-matching-design.md`

Those contracts remain authoritative unless this document explicitly refines them. In
particular:

- SQLite remains the source of truth.
- SQL rows and driver values never escape `internal/store`.
- Provider transports never escape `internal/provider` implementations.
- All accounting and wire amounts use integer minor units plus currency and scale.
- Effective state is committed state plus the active journal prefix in order.
- Profile revision checks remain the semantic concurrency guard.
- Provider leases are liveness aids and never replace authoritative transaction guards.
- Logs use the existing positive privacy allowlist.
- The v2 schema remains install-only, with no migration machinery before stabilization.

This slice does not change the installed SQLite schema. `CurrentSchemaVersion` remains 9. The MCP
HTTP token is a hardened private file, not profile database state.

## Goals

- Replace the Python MCP server with a first-class Go command.
- Preserve all twelve Python tools and all five Python resources.
- Make MCP reads use the same analytics and effective state as TUI and web.
- Make MCP edits participate in journal, review, undo/redo, commit, and provider write-back.
- Keep MCP client calls bounded even when Monarch refresh or write-back takes minutes.
- Support stdio without contaminating protocol stdout.
- Support authenticated streamable HTTP through a loopback listener and trusted reverse proxy.
- Keep every response profile-scoped, revision-aware, bounded, deterministic, and exact-money.
- Preserve offline reads and review when a provider requires reconnection.
- Remain responsive with approximately 100,000 transactions.

## Non-Goals

- Immediate MCP writes directly to Monarch or any future provider.
- An MCP-specific SQLite repository, analytics implementation, journal, or write queue.
- Automatic six-hour refresh scheduling in an MCP process.
- Automatic resumption of ownerless provider write batches when an MCP process starts.
- Interactive credential entry, provider onboarding, or profile creation through MCP.
- Amazon directory selection or Amazon import through MCP.
- Exposing provider external identities, credentials, tokens, raw provider errors, or SQL values.
- Mounting MCP inside `moneyflow web` or importing the Huma API as an internal client.
- Listening directly on wildcard, LAN, or tailnet interfaces.
- Browser sessions, cookies, CORS, or OAuth in this local/tailnet slice.
- YNAB, SimpleFIN, split transactions, transaction-note editing, or Python removal.
- Database or journal-payload migrations.
- Committing generated browser screenshots or `internal/web/dist`.

## Python Parity and Named Differences

Python remains the behavioral oracle where its behavior is deliberate and compatible with the Go
architecture. The following wire-visible or workflow differences are named:

1. Python enables MCP write tools by default. Go omits them unless `--allow-write` is explicit.
2. Python category tools immediately call the backend. Go appends ordinary journal operations and
   requires explicit review and commit.
3. Python has no MCP undo or redo tools. Go adds both because staging creates durable history.
4. Python category writes may partially succeed remotely. Go validates and stages a batch
   atomically as one undo unit.
5. Python `batch_update_category` accepts only a category name. Go accepts exactly one of a stable
   local category ID or an unambiguous category name.
6. Python exposes `dry_run` even when its server is read-only. Go omits the complete write-tool
   family in read-only mode, so `dry_run` is available only when `--allow-write` registers those
   tools.
7. Python MCP serializes amounts as JSON floats. Go returns exact decimal strings and signed
   integer minor units with currency and scale.
8. Python refresh blocks the tool call until provider work completes. Go starts an explicit
   process-owned attempt and returns promptly so MCP client timeouts cannot make progress
   ambiguous.
9. Python has no review, commit-status, pause, resume, or reconciliation tools. Go adds the controls
   required by the shared durable write-batch contract.
10. Python uses backend and account IDs directly in several results. Go exposes stable local
    profile, entity, and transaction IDs and never exposes provider identity mappings.

Keeping `refresh_data` available without `--allow-write` matches Python: its read-only guard does
not cover refresh. Go's read-only mode means user-intent mutation tools are absent, not that the
profile database can never change. A refresh may replace committed provider facts, rebase active
operations, shrink or remove pending targets, and permanently discard an inactive redo tail under
the existing refresh contract, including in read-only mode.

## Command and Process Model

The command is:

```text
moneyflow mcp [flags]
```

The server exposes exactly one persistent profile per process. Supported flags are:

```text
--profile <name-or-id>
--transport stdio|streamable-http
--allow-write
--listen 127.0.0.1:8081
--base-path /mcp
--external-url https://tailnet.host/moneyflow/mcp/
```

`stdio` is the default transport. `--listen`, `--base-path`, and `--external-url` are rejected with
stdio rather than silently ignored. `--listen` defaults to `127.0.0.1:8081` for streamable HTTP.
The normalized default endpoint is `/mcp/`; no redirect is required or emitted.

Profile resolution reuses `profilecatalog.ResolveEntries` exactly:

1. an exact opaque profile ID or catalog key wins;
2. otherwise one unique normalized display-name match wins;
3. ambiguous display names fail with candidate guidance;
4. an omitted profile succeeds only when exactly one persistent profile exists.

Demo and fixture profiles are out of scope. MCP never opens a selector or onboarding wizard. An
incompatible, corrupt, recovery-incomplete, or newer-schema profile returns the same stable catalog
or store failure used by the other commands.

Cobra resolves and opens the profile, wires any configured provider runtime and Amazon matching
directory, constructs `internal/app.Service`, and hands the ready dependencies to `internal/mcp`.
The server closes the service and any running operation supervisor on shutdown. It never changes
the current Git branch, profile selection, or provider binding.

For stdio, stdout contains MCP protocol frames only. Startup guidance and the privacy-safe logger
use stderr. For HTTP, normal startup output includes the canonical endpoint and bearer-token file
path, but never the bearer value.

## Architecture and Dependency Direction

```text
cmd/moneyflow --------------------> internal/profilecatalog
       |                                      |
       |                                      v
       +-----------------------------> internal/store/sqlite
       |
       +--> provider/runtime wiring --> internal/provider/monarch
       |
       +--> internal/mcp --> internal/app --> internal/analytics
                 |              |                   |
                 |              +--> internal/store |
                 |              +--> internal/domain
                 |              +--> internal/provider
                 |
                 +--> internal/httpsecurity
                 +--> official Go MCP SDK

internal/api ---------------------> internal/httpsecurity
internal/home --------------------> private token files and locks
```

`internal/mcp` owns MCP server construction, typed tool and resource registration, protocol result
encoding, process-local explicit-operation supervision, and HTTP middleware composition. It
imports `internal/app`, the provider-neutral error vocabulary needed for result mapping,
`internal/httpsecurity`, and the official MCP SDK. It never imports `internal/store`,
`internal/store/sqlite`, `internal/api`, `internal/tui`, or a concrete provider implementation.

`cmd/moneyflow` owns Cobra flags, catalog resolution, profile opening, provider runtime factory
wiring, token-file location, signal handling, and lifecycle cleanup. Command wiring is the only MCP
path allowed to import a concrete provider package.

`internal/app` remains the sole owner of projections, exact target resolution, capability checks,
journal planning, revision validation, refresh orchestration, and provider-write orchestration.
MCP-specific inputs are converted into existing application requests before application logic runs.

`internal/httpsecurity` is a narrow extraction of canonical base-path, listener, authority, Host,
and Origin validation currently embedded in `internal/api`. It owns no profile, mutation-token,
MCP-token, application, or renderer behavior. The web API continues to own its browser mutation
tokens and Fetch-Metadata policy. Existing web security behavior and tests must remain unchanged
through the extraction.

`internal/home` supplies the existing cross-platform owner-only directory, private-file,
atomic-replace, and advisory-lock mechanics. The MCP package does not reimplement Unix modes or
Windows ACL handling.

Architecture tests enforce these edges. In particular, MCP cannot import SQL, SQLite, Huma, TUI,
web, or Monarch packages; store cannot import MCP; and provider packages cannot import MCP.

## MCP SDK

The implementation uses the official SDK module:

```text
github.com/modelcontextprotocol/go-sdk v1.7.0
```

Version 1.7.0 was the current stable release during design, declares Go 1.25, and contains no CGO
surface. The implementation plan re-verifies the current stable version and API before adding the
dependency; any version change is recorded in the plan rather than guessed during implementation.
The direct dependency is justified because it supplies protocol negotiation, stdio framing,
streamable HTTP, JSON Schema generation, typed tool decoding, structured results, cancellation,
and official interoperability behavior that Moneyflow should not reproduce.

Tools use the SDK's typed input and output support. Every successful result provides
`structuredContent` and one JSON text content block representing the same canonical logical
document. Resources return the same JSON document as their corresponding tool projection. Binary,
image, audio, elicitation, sampling, prompts, and server-to-client roots are outside this slice.

The server supports the protocol versions implemented by the pinned SDK. Moneyflow does not add a
separate protocol-version compatibility layer.

## Result and Error Contract

Every Moneyflow tool document has schema version `1` and includes the profile revision used:

```json
{
  "version": "1",
  "status": "success",
  "revision": "42"
}
```

Revisions and batch versions are decimal strings so JavaScript clients cannot lose unsigned
64-bit precision. Counts are bounded JSON integers. Dates are ISO `YYYY-MM-DD`. Instants are UTC
RFC 3339 strings. IDs are stable local IDs.

Money uses all of these fields together:

```json
{
  "amount": "-12.34",
  "amount_minor": "-1234",
  "currency": "USD",
  "scale": 2
}
```

`amount` and `amount_minor` are strings. Input amount bounds are exact signed decimal strings and
must match the profile scale. JSON floating-point amounts are rejected.

Expected application, capability, provider, selection, and store failures return an MCP tool result
with `isError` set and a structured version-one error envelope. The envelope contains only a stable
code, safe detail, current revision when available, and bounded recovery fields such as a refreshed
selection, next-eligible instant, counts, or batch status. Raw Go errors, provider response bodies,
SQL text, credentials, labels, search text, and request bodies never appear.

Malformed MCP framing, unsupported protocol versions, invalid JSON Schema input, and unknown tools
remain protocol-level errors. Business validation failures are tool-result errors so an MCP client
can recover without treating the connection as broken.

## Read Semantics

Every read begins with the ordinary cheap profile-revision check. If another process advanced the
revision, the application service reloads before projection. No read holds a database transaction
while serializing an MCP result or waiting for client input.

Transaction, merchant, category, summary, and account tools project the effective snapshot. A
successfully staged MCP, TUI, or web operation therefore appears in later MCP reads immediately,
before commit. Pending status is exposed as a boolean or bounded summary, never as provider or SQL
implementation detail.

Results use existing application ordering and filtering rules. MCP does not create an independent
query language. It does add one renderer-neutral application search variant to preserve the Python
MCP contract: Unicode-lowercased literal substring matching across merchant, category, and notes.
The MCP variant never interprets the query as a regular expression. TUI and web retain their
existing merchant-and-category regular-expression search. Date ranges are inclusive. Hidden rows
are included or excluded only through an explicit input with the same default as the ordinary
application projection.

All row-producing tools are bounded. Defaults match Python where practical, and the hard limit is
1,000 rows. A response reports the complete matching count and returned window so truncation is
never silent. Invalid negative offsets, non-positive limits, limits over the maximum, malformed
dates, and reversed ranges fail before projection.

## Always-Registered Tools

### `search_transactions`

Inputs are `query`, optional `offset`, and optional `limit` with Python's default of 50. It searches
merchant, category, and notes through the MCP application-search variant. Matching is a
Unicode-lowercased literal substring comparison, so regular-expression metacharacters have no
special meaning. The result contains the total count and one deterministic transaction window.

### `get_transactions`

Optional inputs are inclusive `start_date`, `end_date`, stable `category_id`, category label,
merchant substring, exact-decimal `min_amount`, exact-decimal `max_amount`, hidden inclusion,
offset, and limit. A category ID and label are mutually exclusive. Category-label ambiguity fails.
The merchant filter uses the same Unicode-lowercased literal substring comparison as Python MCP;
it is not a regular expression. The default limit remains 100.

### `get_spending_summary`

Inputs are optional inclusive dates and `group_by`, which is exactly `category` or `merchant`.
Absent dates retain Python's trailing-30-day default. Only expenses participate. Groups and totals
come from the existing integer analytics path.

### `get_categories`

Returns active groups and categories in deterministic label-and-ID order, including stable local
IDs and bounded transaction counts. Retired entities are omitted.

### `get_merchants`

Returns active merchants with stable local IDs, counts, and exact totals, sorted by count then the
existing deterministic tie-break. The default remains 100 and the maximum remains 1,000.

### `get_account_info`

Returns the local profile ID and display name, profile kind, currency, scale, effective transaction
count, date range, category count, current revision, pending-operation summary, provider capability
state, last successful refresh, and durable write-batch summary when present. Provider household,
account, session, and external entity IDs are omitted.

### `get_uncategorized_transactions`

Inputs are optional merchant substring, offset, and limit with Python's default of 100. It uses the
active Uncategorized identity rather than comparing a display string. The result reports complete
and returned counts.

### `get_amazon_order_details`

Input is one stable local finance transaction ID. It calls the existing transaction-information and
cross-profile Amazon matching service. Results use the established 20-match bound, deterministic
ordering, confidence, source profile display name, product facts, and exact amounts. The MCP adapter
does not open other SQLite profiles or calculate matches itself.

### `get_transaction_details`

Input is one stable local transaction ID. It returns the same committed provider facts and
effective merchant, category, visibility, notes, pending marker, and Amazon enrichment used by the
interactive information view.

### `review_changes`

Inputs are `expected_revision` and an optional operation ID, offset, and limit. It calls
`Service.Review` and returns active and redo operation summaries plus at most 400 requested target
rows. Review never mutates the journal.

### `get_commit_status`

Returns the current counts-only durable provider-write status, if any, without provider I/O. It is
available in read-only mode so a client can observe work initiated by another renderer.

### `refresh_data`

This tool is available only when the profile has a configured Monarch runtime. Amazon returns a
capability-unavailable result explaining that import requires the TUI, web, or Cobra source
workflow. Local profiles return a kind-specific no-provider reason.

The call starts one explicit process-owned refresh attempt and returns promptly with an opaque
attempt ID and initial counts-only status. It does not wait for the complete network fetch. The
attempt uses the MCP server lifetime rather than the tool-request lifetime and follows the ordinary
identity probe, two-partition fetch, integrity retries, pending exclusion, plausibility guard,
generation CAS, journal rebase, and atomic fold contract.

### `get_refresh_status`

Input is the opaque process-local attempt ID returned by `refresh_data`. It reports running,
completed, failed, or deletion-confirmation-required state, plus counts, generation, revision,
timings, and safe recovery guidance. It is the only MCP result that returns the process-local
refresh confirmation token.

### `confirm_refresh_deletions`

Inputs are the attempt ID and exact confirmation token. It calls `Service.ConfirmProviderRefresh`
and accepts the already fetched candidate after the existing expiry, process ownership, generation,
identity, and transaction guards revalidate. It is always registered, like `refresh_data`, because
it accepts reviewed provider truth rather than creating user edit intent.

Pagination-integrity failures, identity mismatches, expired tokens, wrong-process tokens, and
generation conflicts cannot be overridden. An MCP process that exits loses its confirmation
candidate; the next process must fetch a new candidate.

## Write Tools and Dry Run

The following tools are registered only when the process starts with `--allow-write`. Their absence,
rather than a runtime permission failure, makes the server's capabilities truthful to MCP clients.

### `update_transaction_category`

Inputs are `expected_revision`, one stable local transaction ID, exactly one of stable local
`category_id` or category label, and optional `dry_run`. ID resolution is exact. Label resolution
requires one unique normalized active category. The tool targets the transaction explicitly and
never derives a broader predicate.

### `batch_update_category`

Inputs are `expected_revision`, one through 100 unique stable local transaction IDs, exactly one of
stable local `category_id` or category label, and optional `dry_run`. Input order is irrelevant and
canonical target order is bytewise local-ID order.

The batch is all-or-nothing. Duplicate, missing, retired, unwritable, or stale targets reject the
entire request without a revision or journal change. Success appends one `category.assign`
operation, so review and undo treat it as one unit.

For either tool, `dry_run=true` performs the complete current-revision, capability, category, target,
and provider-writability validation but does not allocate an operation ID, append or rewrite the
journal, change selection, or increment revision. It returns the prospective affected count and a
bounded before/after window. The application layer owns a pure validation/planning entry point so
the MCP adapter does not approximate `Service.Mutate` behavior.

With `dry_run=false`, the tool calls `Service.Mutate`. It returns the new revision, pending summary,
selection disposition, and bounded affected rows. It never calls a provider.

### `undo_changes` and `redo_changes`

Each requires the current expected revision and calls the ordinary application interaction. They
preserve the established linear cursor, redo-tail, hide-cancellation, and provider-batch capability
rules. Go adds these tools because staged MCP mutations otherwise have no MCP-native correction
path; Python has no equivalents.

### `commit_changes`

Inputs are `expected_revision` and `reviewed_revision`. Both must equal the current profile revision.
A stale review fails without preparing or folding anything.

Local and Amazon profiles perform the ordinary atomic local fold and return the completed revision.
A Monarch profile durably prepares the reviewed prefix as the ordinary provider-write batch. Once
preparation commits, the tool starts one background worker and returns the batch ID, version, phase,
and counts immediately. It never describes preparation as remote completion.

### `pause_commit`

Input is the current batch version. It requests an ordinary safe pause and reports the resulting or
pending counts-only state. Already completed remote items remain durable.

### `resume_commit`

Input is the current batch version. A short authoritative application transition validates the
phase and version and reacquires the write lease. MCP then starts the background worker and returns
immediately. Retry is offered only for the existing retryable attention class.

### `stop_and_reconcile`

Inputs are the current batch version and current revision. It uses the approved provider-write
reconcile contract: fetch remote truth, remove the entire frozen prefix atomically with the fold,
and preserve completed remote effects through that truth. It may return deletion-confirmation-
required with a process-local token.

### `confirm_reconcile`

Inputs are the current batch version, current revision, and exact process-local confirmation token.
It confirms only the matching stop-and-reconcile candidate. Expired, wrong-process, wrong-batch,
wrong-generation, and stale-revision tokens fail without a fold.

## Explicit Operation Supervisor

`internal/mcp` owns a small process-local supervisor for user-initiated long-running work. It is not
a scheduler and never starts work merely because a profile is stale or a durable batch is
ownerless.

The supervisor provides:

- one active refresh attempt per process;
- one active provider-write worker per process;
- opaque attempt identity for refresh status;
- bounded credential-blind terminal status retention;
- process-local retention of refresh and reconcile confirmation tokens;
- cancellation through the server-lifetime context on orderly shutdown;
- no persistence outside the existing profile/provider state.

The provider operation lease remains the cross-process liveness guard. The refresh generation,
profile revision, and batch version remain authoritative correctness guards in SQLite.

The provider-write refactor must preserve the existing `writeRuns` single-worker-per-process
serialization. No MCP path may start a second worker while one is running, even if two MCP calls
arrive concurrently. The short resume transition and long worker execution are separable, but the
existing pause request, idle notification, lease ownership, and crash-uncertain item rules remain
unchanged.

Request cancellation before a mutation or commit transaction begins cancels that request. Once a
journal append, batch preparation, local fold, refresh fold, or reconciliation transaction begins,
it completes or rolls back atomically. A disconnected MCP client can recover the authoritative
result through a fresh read, review, refresh-status, or commit-status call.

An explicitly started background refresh or provider write is not canceled when its initiating tool
request ends normally or is canceled after acceptance. Server shutdown cancels network work at safe
boundaries. Durable refresh status, leases, journal state, and write-batch facts determine recovery;
process-local candidate tokens intentionally do not survive restart.

## Resources

Both modes register all five Python resource URIs:

- `moneyflow://account`
- `moneyflow://categories`
- `moneyflow://merchants/top`
- `moneyflow://spending/monthly`
- `moneyflow://transactions/recent`

Resources are bounded aliases of the corresponding read tools:

- account uses `get_account_info`;
- categories uses `get_categories`;
- merchants uses `get_merchants` with limit 50;
- monthly spending uses the current calendar month in the configured process clock;
- recent transactions uses the ordinary deterministic transaction order with limit 50.

They revalidate the profile revision on every read. Resource content is effective state, just like
the corresponding tools. Resources accept no query string or templated identity in this slice.

## Streamable HTTP Security

Streamable HTTP is a standalone MCP endpoint, not part of `moneyflow web`. It deliberately uses the
SDK's stateless handler mode: requests require no `Mcp-Session-Id`, the server creates no session
affinity, and every tool invocation carries all durable input needed for revision validation.
Statelessness is a Moneyflow deployment decision, not a requirement of the MCP transport.

### Listener and canonical URL

The listener must resolve to loopback. `0.0.0.0`, `[::]`, wildcard hostnames, LAN addresses, and
tailnet addresses are refused without an override flag; this slice defines no override flag.
Remote access is through a trusted reverse proxy to the loopback listener.

`--base-path` uses the same strict normalization as the web command. `--external-url`, when present,
must use HTTP or HTTPS, contain no user info, query, or fragment, and have a path exactly equal to
the normalized base path. A typical supported invocation is:

```text
moneyflow mcp --profile Household --transport streamable-http \
  --listen 127.0.0.1:8081 \
  --base-path /moneyflow/mcp \
  --external-url https://tailnet.host/moneyflow/mcp/
```

The exact normalized base path serves the MCP handler. Other paths return not found. Forwarded host,
scheme, and origin headers are ignored. The reverse proxy must preserve the canonical Host header.
When an external URL is configured, direct access using the listener authority is rejected rather
than becoming a second origin.

### Bearer token

Every HTTP request, including initialize, resources, and read-only tools, requires:

```text
Authorization: Bearer <token>
```

The token is 32 random bytes encoded as unpadded base64url. It is profile-scoped and stored at:

```text
<profile-root>/mcp/http-token
```

The directory is owner-only and the file is an owner-only private file on Unix and Windows. The
first HTTP server start, `token reveal`, or `token rotate` creates the directory through
`internal/home`. Creation and rotation use the existing private temporary-file plus atomic-publish
discipline under a dedicated token lock. Existing content must be canonical and exactly sized;
malformed or insecure content is refused rather than repaired silently.

The HTTP middleware rereads and validates the token file for each request, bounds the header before
decoding, and compares secrets in constant time. Rotation therefore invalidates the previous token
without restarting the server. `token reveal` is the only command that prints the value, and does so
to stdout after an explicit invocation. Ordinary startup prints only the file path to stderr.

Bearer material never appears in process arguments, environment-generated examples, URLs, MCP
results, errors, logs, metrics, or correlation IDs. The documentation shows a placeholder and token
file path, never a generated value.

### Origin and request rules

- A present `Origin` header must exactly equal the canonical scheme and authority.
- An absent `Origin` is accepted for non-browser MCP clients after bearer authentication.
- Multiple, opaque, `null`, malformed, or mismatched Origin values are rejected.
- The Host authority must match the canonical authority.
- CORS is not enabled and preflight requests are rejected.
- Cookies and browser credentials are ignored and never set.
- Query-string bearer tokens are rejected.
- Request bodies are bounded before JSON decoding.
- Responses use `Cache-Control: no-store` and `X-Content-Type-Options: nosniff`.
- Request URLs and query strings are not logged.

Authentication and Origin validation run before the SDK handler or request-body decoding. The same
policy applies whether `--allow-write` is present or absent.

## Provider Lifecycle

An MCP process never participates in the six-hour Monarch scheduler. It does not refresh on startup,
on visibility, or on a timer. It does not notice an ownerless write batch and resume it merely
because the process is alive.

Only explicit tool calls start network work:

- `refresh_data` starts one refresh attempt;
- `commit_changes` starts the freshly prepared write batch;
- `resume_commit` resumes eligible durable work;
- `stop_and_reconcile` starts its explicit authoritative refresh;
- confirmation tools accept an exact already-reviewed candidate.

Provider session reload follows the existing rule: after an authentication failure, reread a
replaced session file once, probe identity, and retry once. A persistent failure returns
`provider_reconnect_required` with CLI guidance. MCP never prompts for an account password,
provider password, TOTP secret, or verification code.

If another process owns a refresh or write lease, MCP returns the existing in-progress status and
owner renderer. It never steals a live lease. If a long-lived TUI or web process later completes or
resumes the work, MCP observes that through ordinary durable status calls.

Amazon import remains interactive and outside MCP. Cross-profile Amazon matching remains available
as a read because its existing service uses short-lived committed snapshots and stable skip reasons.

## Privacy and Logging

The command wires a dedicated `log/slog` logger to stderr. There is no pre-existing global logger
assumed by this design. Log fields are selected from this positive allowlist:

- stable error or status code;
- local profile ID;
- revision, generation, and batch version;
- counts and bounded sizes;
- durations and timestamps;
- transport kind;
- safe operation or tool name;
- random correlation ID.

Logs never contain merchant, category, group, account, product, or profile display names; search
text; notes; transaction or order details; amounts; dates from financial rows; external provider
IDs; MCP request or response bodies; bearer tokens; credentials; raw provider errors; file contents;
or query strings.

Tool results may contain requested financial information because they are the authenticated product
surface. Status tools and background-attempt records remain counts-only. A tool result is never
copied into a log message.

The stdio transport writes no logger, startup banner, progress line, or panic text to stdout. Panic
recovery emits one safe correlation ID to stderr and one internal-error envelope when the protocol
still permits a response.

## Failure Handling

Application and provider failures retain their existing stable codes. MCP adds only transport or
adapter codes that have no application equivalent:

- `mcp_auth_required`
- `mcp_auth_invalid`
- `mcp_origin_invalid`
- `mcp_host_invalid`
- `mcp_http_token_invalid`
- `mcp_attempt_not_found`
- `mcp_attempt_busy`
- `mcp_response_too_large`

HTTP authentication failures use ordinary HTTP 401 or 403 before MCP framing. Tool errors use the
structured MCP result envelope. Unsupported profile-kind operations use the existing capability-
unavailable result and include the kind-specific safe reason.

There is no automatic replay after `revision_conflict`, `selection_stale`, identity mismatch,
confirmation invalidation, data invalidity, store failure, or provider reconnect. Existing bounded
provider retry and rate-limit rules remain inside explicitly started refresh and write workers.

`internal/mcp` owns `MaxResponseContentBytes`, fixed at 8 MiB. The bound applies to the combined
serialized structured result and its equivalent JSON-text fallback, before SDK framing. If that
combined content would exceed the ceiling, the tool returns a bounded `mcp_response_too_large`
error rather than dropping content while reporting success. Row windows should make this
exceptional. Any prior, more specific failure is preserved instead of being replaced by a
size-limit failure.

## Testing Strategy

### SDK and protocol tests

- Pin the official SDK version and verify the module introduces no CGO requirement.
- Use the official Go client against an in-memory stdio pair to initialize, enumerate tools and
  resources, call representative tools, and decode structured content.
- Run a subprocess stdio test proving stdout contains protocol frames only and stderr contains only
  allowlisted diagnostics.
- Assert structured content and JSON text decode to the same canonical logical document.
- Assert protocol negotiation errors remain protocol errors while business failures are tool
  results with `isError`.
- Assert every successful and failed result respects the exact 8 MiB combined-content ceiling.

### Tool registration and read tests

- Read-only mode exposes exactly the 14 always-registered tools, including refresh status and
  deletion confirmation, and all five resources while omitting every write and batch-control
  mutation tool.
- `--allow-write` adds exactly the named write tools without changing read schemas.
- All twelve Python tool names are present across the appropriate mode.
- Resource documents equal their corresponding tool projections at one revision.
- Every read reloads after an external revision change and holds no database transaction during
  serialization.
- Effective staged values appear in search, transaction, category, merchant, summary, details, and
  resources before commit.
- Windows report complete totals, deterministic ordering, offsets, and truncation.
- MCP search uses Unicode-lowercased literal substring matching across merchant, category, and
  notes; regular-expression metacharacters remain literal. TUI and web retain their existing
  merchant-and-category regular-expression search.
- `get_transactions` merchant filtering uses Unicode-lowercased literal substring matching rather
  than regular-expression matching.
- Exact decimal input rejects floats, excess scale, invalid syntax, and overflow.
- Exact amounts, minor units, currency, and scale round-trip without floating point.
- Amazon detail calls use the matching service and skip incompatible sources safely.

### Mutation and review tests

- A category dry run performs full validation but allocates no operation ID and leaves revision,
  journal, selection, and committed state unchanged.
- Single category update appends one exact-target journal operation and performs no provider call.
- A 100-target category update appends one operation; 101 targets fail.
- Missing, duplicate, stale, retired, ambiguous, and unwritable batch inputs reject the complete
  operation with no change.
- Category ID and unique normalized name resolution produce the same destination; an ambiguous name
  requires the ID.
- Review returns active and inactive operations plus bounded details at the checked revision.
- Undo and redo preserve cursor, redo-tail, hide-cancellation, and revision behavior.
- Local and Amazon commit satisfy committed-after-fold equals reviewed-effective-before-fold.
- Stale mutation, review, commit, and control requests make no change and no provider call.

### Refresh and write supervision tests

- `refresh_data` returns promptly with an attempt ID while a blocked fake provider continues in the
  background.
- Tool-request cancellation after acceptance does not cancel the server-owned attempt.
- Server shutdown cancels network work at a safe boundary and leaves durable state recoverable.
- Only Monarch can refresh; Amazon and local profiles return exact capability reasons.
- Read-only refresh can rebase pending targets and discard the redo tail exactly as the refresh
  contract specifies.
- Deletion confirmation is returned only through matching process-local refresh status and can be
  accepted through `confirm_refresh_deletions` in read-only mode.
- Expired, wrong-process, stale-generation, identity-mismatched, and integrity-failed refresh
  candidates cannot be confirmed.
- Monarch commit returns after durable preparation, not remote completion.
- Local commit returns only after its atomic fold.
- Background provider writing records completion or every parked phase durably.
- Resume performs an authoritative short version/phase transition before background execution.
- The refactor preserves `writeRuns`: concurrent commit/resume calls start at most one worker in one
  process.
- Cross-process leases still permit at most one network worker while revision, generation, and batch
  CAS reject stale work independently.
- Pause, retryable resume, reconnect, rate limit, reconcile-only attention, stop-and-reconcile, and
  confirmation retain the write-back specification's behavior.
- Process restart never auto-resumes from MCP; an explicit resume or a TUI/web standing scheduler
  can take over safely.

### HTTP security and token tests

- Default HTTP bind is `127.0.0.1:8081`; IPv4 and IPv6 loopback are accepted.
- Wildcard, LAN, and tailnet listener addresses are refused.
- Base-path normalization and external-URL matching reuse the web contract.
- The canonical endpoint works behind a proxy-preserved Host, while direct listener authority is
  rejected when an external URL is configured.
- Missing, malformed, oversized, query-string, and incorrect bearer values fail before body decode.
- A present exact Origin succeeds; absent Origin succeeds; mismatched, multiple, opaque, and null
  Origin values fail.
- CORS preflight fails and no response sets cookies or allows cross-origin credentials.
- First use creates one owner-only canonical token on Unix and Windows.
- Reveal prints the token only on explicit stdout; startup prints only its path to stderr.
- Rotation atomically invalidates the old token for a running server and preserves secure
  permissions.
- Concurrent creation/rotation serializes through the token lock; process death releases the lock;
  same-process sequential reuse succeeds.
- HTTP responses are no-store and contain no bearer value or request echo.
- Stateless requests require no session affinity or `Mcp-Session-Id`.
- The existing web listener, base-path, canonical-origin, Fetch-Metadata, and mutation-token suites
  remain unchanged and green after extracting `internal/httpsecurity`.

### Privacy, architecture, race, and performance tests

- Architecture tests enforce every dependency edge in this document.
- Go source, test fixtures, docs, logs, stderr captures, HTTP responses, and MCP errors contain no
  credentials, real personal data, provider payloads, or generated tokens.
- Race tests cover concurrent reads, refresh status, batch status, token rotation, server shutdown,
  and the single-worker supervisor.
- Query and encoding benchmarks cover 100,000 transactions with a 1,000-row response window.
- Read projection plus encoding targets 100 ms locally and a 500 ms CI ceiling, excluding process
  startup and provider network I/O.
- Dry-run and 100-target staging remain within the existing 50 ms local and 100 ms CI interaction
  contract.
- `make verify-go`, `make verify-web`, `make test-race`, store gates, lint, vet, parity, and the
  repository's Python verification remain green.
- Markdown lint and arrow-list checks cover this document.
- The committed diff and all commits being pushed are scanned for private data, generated browser
  screenshots, and `internal/web/dist`.

Live verification uses only synthetic or already connected non-destructive data. It covers stdio
initialization, tool enumeration, read queries, dry run, staging, review, undo/redo, and a small
category commit. HTTP is exercised through a loopback listener and a Caddy-compatible external URL
configuration. Destructive deletion-confirmation and provider failure states use injected
transports rather than manipulating real financial history.

## Completion Criteria

This slice is complete when:

- `moneyflow mcp --profile <name-or-id>` serves one resolved profile over stdio;
- streamable HTTP works through an authenticated loopback listener and canonical reverse-proxy URL;
- all twelve Python tools and five resources have tested Go equivalents;
- read-only mode omits user-intent mutation tools while retaining explicit Monarch refresh and
  refresh deletion confirmation;
- MCP reads expose effective state with stable local IDs and exact money;
- category edits stage, review, undo, redo, and commit through ordinary application services;
- local and Amazon commits fold atomically and Monarch commits use the durable write batch;
- refresh and write tool calls return promptly while explicit process-owned work remains observable;
- MCP startup never schedules refresh or resumes ownerless work automatically;
- HTTP bearer creation, reveal, rotation, Origin, Host, and loopback rules pass on supported
  platforms;
- the web security behavior remains unchanged after shared validation extraction;
- stdout, stderr, logs, errors, and status comply with the privacy contract;
- performance, race, architecture, parity, Go, web, and Python gates pass;
- the diff contains no schema migration, direct provider mutation, SQL-bearing MCP code, Amazon
  import, YNAB, SimpleFIN, Python shim, generated web distribution, or committed screenshot;
- every agent-authored implementation change is committed before handoff or live dogfooding.

Completion does not authorize merging `go-port`, removing Python, or beginning YNAB. The Python
shim remains a later finalization slice after remaining provider and workflow gaps close.

## Implementation Decomposition

The implementation plan should keep independently green checkpoints:

1. Pin the official SDK; add the direct `internal/mcp` server shell, canonical result/error types,
   stdio framing, and architecture tests.
2. Add application-backed read tools, exact-money inputs/results, all five resources, bounded
   windows, and official-client interoperability tests.
3. Add pure mutation validation, dry run, journal-backed category tools, review, and undo/redo.
4. Split provider-write transition from worker execution without weakening `writeRuns`; add commit,
   batch status/control tools, and the explicit-operation supervisor.
5. Add asynchronous explicit Monarch refresh, refresh status, deletion confirmation, and provider-
   kind capability behavior.
6. Extract neutral HTTP origin/listener validation with unchanged web tests; add token lifecycle,
   authenticated stateless streamable HTTP, and Cobra token commands.
7. Complete subprocess, cross-process, race, performance, privacy, negative-scope, and live
   verification gates.

Each checkpoint follows test-first implementation and ends in a verified commit. The detailed
implementation plan is written only after this assembled specification is reviewed and approved.
