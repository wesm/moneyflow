# Go Port MCP Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development
> (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use
> checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the Python MCP server with a profile-scoped Go MCP server that exposes exact,
bounded reads, stages category edits through the ordinary journal, explicitly commits through the
local or Monarch write path, and supports authenticated stdio and loopback streamable HTTP.

**Architecture:** `internal/mcp` is a direct adapter over renderer-neutral `internal/app.Service`
operations. It owns MCP registration, wire documents, response bounds, explicit background-attempt
supervision, and transport composition, while SQL, provider APIs, and presentation code remain
behind existing application interfaces. `cmd/moneyflow` remains the composition root for catalog
resolution, profile lifecycle, concrete Monarch runtime wiring, and process shutdown.

**Tech Stack:** Go 1.26.3; official `github.com/modelcontextprotocol/go-sdk` v1.6.1; Cobra
1.10.2; `net/http`; `log/slog`; Testify 1.11.1; pure-Go `modernc.org/sqlite` v1.56.0.

## Global Constraints

- Work only on the checked-out `go-port` branch. Do not switch branches, pull, rebase, push,
  merge, amend, or remove Python without explicit user permission.
- Follow TDD for every behavior change: add the focused failing test, run it and observe the
  intended failure, implement the smallest behavior, then rerun focused and package tests.
- Commit every verified task before beginning the next. Stage only that task's files. Never commit
  browser screenshots, `web/dist`, or `internal/web/dist`.
- Do not change the installed SQLite schema or journal payloads. `CurrentSchemaVersion` remains 9;
  add no migration or compatibility path.
- Keep the no-CGO Linux, macOS, and Windows contract. Verify the new dependency with
  `CGO_ENABLED=0`, not by inspecting configuration text alone.
- Keep money exact: signed integer minor units internally and decimal strings plus minor-unit
  strings on the MCP wire. Never add `float32`, `float64`, SQLite `REAL`, or JSON numeric money.
- Keep each MCP process bound to exactly one resolved profile. Resolve `--profile` through the
  existing catalog rules and hold the normal shared lifecycle lock until shutdown.
- Keep `internal/mcp` free of store, SQLite, API, TUI, web, and concrete provider imports. Only the
  Cobra composition root may wire Monarch into the opened profile service.
- Never call a provider directly from an MCP handler. Reads, edits, review, commit, refresh, pause,
  resume, and reconciliation all go through `internal/app.Service`.
- Read-only mode omits user-intent write tools from registration. Explicit Monarch refresh and
  deletion confirmation remain registered because they accept provider truth and match Python.
- The MCP process never starts the six-hour refresh scheduler and never automatically resumes an
  ownerless write batch. Only explicit tool calls start network work.
- Background work is owned by the MCP server lifetime, not a tool-request context. Every goroutine
  has one owner, a shutdown cancellation path, and a wait path.
- Keep every collection window bounded to 1,000 and all review target windows bounded to 400.
  Enforce the 8 MiB combined structured-content plus JSON-text response ceiling before SDK framing.
- Logs use the spec's positive allowlist only. Never log labels, search text, notes, amounts,
  financial dates, transaction details, external provider IDs, request or response bodies, token
  values, credentials, or query strings.
- Stdio writes MCP frames only to stdout. Human diagnostics go to stderr. Streamable HTTP binds
  only to loopback and requires the exact profile token, Host, and optional Origin before decoding.
- Do not add direct provider mutation, Amazon import, YNAB, SimpleFIN, the Python shim, prompts,
  sampling, elicitation, binary content, generated web assets, or committed screenshots.

## Dependency Pin Decision

The approved design required a plan-time recheck instead of blindly using its design-time pin.
The official release page checked on 2026-08-29 lists v1.6.1 as the latest stable release and
v1.7.0-pre.3 as a prerelease. Pin v1.6.1. Its `go.mod` declares Go 1.25.0, which is below this
repository's Go 1.26.3 toolchain. Recheck immediately before Task 1; if the official stable release
has changed, update this plan's pin in a normal commit before implementation rather than selecting a
different version silently.

Use the SDK's published APIs at this pin:

```go
server := mcp.NewServer(
    &mcp.Implementation{Name: "moneyflow", Version: version.Version},
    &mcp.ServerOptions{Logger: logger},
)
mcp.AddTool(server, tool, handler)
server.AddResource(resource, handler)
server.Run(ctx, &mcp.StdioTransport{})
mcp.NewStreamableHTTPHandler(
    func(*http.Request) *mcp.Server { return server },
    &mcp.StreamableHTTPOptions{Stateless: true, JSONResponse: true, Logger: logger},
)
```

Use `mcp.NewInMemoryTransports()` and the official `mcp.Client` for in-process interoperability
tests. Do not write a Moneyflow transport, JSON-RPC implementation, schema generator, or protocol
version shim.

## Target File Map

```text
go.mod / go.sum                                  official MCP SDK pin
internal/mcp/documents.go                        canonical success/error and exact-money documents
internal/mcp/results.go                          bounded structured + JSON-text result construction
internal/mcp/server.go                           server construction and conditional registration
internal/mcp/stdio.go                            stdio lifecycle and panic-safe diagnostics
internal/mcp/tools_read.go                       fourteen always-registered tool handlers
internal/mcp/tools_write.go                      allow-write journal and batch-control handlers
internal/mcp/resources.go                        five bounded resource aliases
internal/mcp/supervisor.go                       explicit refresh/write attempt ownership and status
internal/mcp/http.go                             stateless handler composition and HTTP server lifecycle
internal/mcp/token.go                            private profile token creation/read/reveal/rotation
internal/mcp/*_test.go                           protocol, tools, bounds, supervisor, HTTP, and privacy tests
internal/app/tool_projection.go                  bounded renderer-neutral MCP read projections
internal/app/tool_projection_test.go             literal search, windows, summaries, and catalog tests
internal/app/mutation_preview.go                 pure category assignment validation and before/after rows
internal/app/mutation_preview_test.go             dry-run equivalence and no-allocation/no-write tests
internal/app/selection.go                         explicit transaction selection constructor
internal/app/provider_refresh.go                  mcp renderer identity
internal/app/provider_write.go                    reserved short transition plus one-shot write execution
internal/app/provider_write_test.go               preserved writeRuns and transition/worker tests
internal/httpsecurity/basepath.go                 neutral base-path and canonical-origin validation
internal/httpsecurity/request.go                  Host and Origin request validation
internal/httpsecurity/*_test.go                   extracted contract tests
internal/api/basepath.go                          forwarding removal after extraction
internal/api/security.go                          web mutation security using neutral origin config
internal/api/*_test.go                            unchanged web security behavior
internal/home/lock.go                             mcp-http-token.lock registration
internal/provider/architecture_test.go            MCP and HTTP-security dependency enforcement
cmd/moneyflow/mcp.go                              mcp command, options, token subcommands, lifecycle
cmd/moneyflow/mcp_dependencies.go                 profile and provider composition
cmd/moneyflow/root.go                             command registration and injectable MCP runner
cmd/moneyflow/*_test.go                           Cobra, profile, stdout/stderr, and subprocess tests
Makefile                                          focused test-mcp target and verification integration
README.md                                         MCP client setup and safe transport examples
docs/guide/mcp.md                                 tools, resources, staging, commit, and recovery guide
```

## Cross-Task Interfaces

Task 1 establishes the adapter shell and canonical document contract:

```go
// internal/mcp/documents.go
const (
    DocumentVersion        = "1"
    MaxResponseContentBytes = 8 << 20
    MaxRows                 = 1_000
)

type Header struct {
    Version  string `json:"version"`
    Status   string `json:"status"`
    Revision string `json:"revision"`
}

type Money struct {
    Amount      string `json:"amount"`
    AmountMinor string `json:"amount_minor"`
    Currency    string `json:"currency"`
    Scale       uint8  `json:"scale"`
}

type ErrorDocument struct {
    Header
    Code            string `json:"code"`
    Detail          string `json:"detail"`
    CurrentRevision string `json:"current_revision,omitempty"`
    NextEligible    string `json:"next_eligible,omitempty"`
    CorrelationID   string `json:"correlation_id,omitempty"`
}

type Dependencies struct {
    Service     *app.Service
    ProfileID   string
    ProfileName string
    ProfileRoot string
    Clock       func() time.Time
    Random      io.Reader
    Logger      *slog.Logger
}

type Options struct {
    AllowWrite bool
}

type Server struct {
    SDK        *mcp.Server
    supervisor *Supervisor
}

func New(dependencies Dependencies, options Options) (*Server, error)
func (server *Server) RunStdio(context.Context) error
func (server *Server) Close(context.Context) error
```

Task 2 adds neutral application projections rather than exposing service-owned slices:

```go
// internal/app/tool_projection.go
const MaxToolRows = 1_000

type TransactionFilter struct {
    StartDate, EndDate *domain.Date
    LiteralQuery       string
    CategoryID         domain.EntityID
    CategoryLabel      string
    MerchantSubstring  string
    MinAmount          *domain.Money
    MaxAmount          *domain.Money
    IncludeHidden      bool
}

type TransactionWindowRequest struct {
    ExpectedRevision uint64
    Filter           TransactionFilter
    Offset           int
    Limit            int
}

type TransactionWindow struct {
    Revision uint64
    Total    int
    Offset   int
    Limit    int
    Rows     []domain.Transaction
    Pending  PendingSummary
}

type CollectionWindowRequest struct {
    Offset int
    Limit  int
}

type CatalogWindowRequest struct {
    ExpectedRevision uint64
    Groups           CollectionWindowRequest
    Categories       CollectionWindowRequest
    Merchants        CollectionWindowRequest
}

func (service *Service) TransactionWindow(
    context.Context,
    TransactionWindowRequest,
) (TransactionWindow, error)

type CatalogProjection struct {
    Revision       uint64
    GroupTotal     int
    GroupOffset    int
    Groups         []domain.CategoryGroup
    CategoryTotal  int
    CategoryOffset int
    Categories     []domain.Category
    MerchantTotal  int
    MerchantOffset int
    Merchants      []MerchantProjection
}

func (service *Service) CatalogProjection(
    context.Context,
    CatalogWindowRequest,
) (CatalogProjection, error)

type AccountProjection struct {
    Revision         uint64
    ProfileKind      string
    MoneyPartitions  []MoneyPartition
    TransactionCount int
    DateRange        *domain.DateRange
    CategoryCount    int
    Pending          PendingSummary
    Capabilities     []Capability
    Provider         ProviderStatus
    Write            ProviderWriteStatus
}

type MoneyPartition struct {
    Currency         domain.Currency
    Scale            uint8
    TransactionCount int
}

func (service *Service) AccountProjection(
    context.Context,
    uint64,
) (AccountProjection, error)
```

`TransactionWindow` performs the cheap revision reload under the service interaction lock, copies
the effective transactions, releases all locks, and then filters/sorts/windows ordinary slices.
Literal matching is `strings.Contains(strings.ToLower(value), strings.ToLower(query))` over
merchant, category, and notes. Merchant-only filtering uses the same literal rule. Existing
`analytics.Filter` and TUI/web regex behavior do not change.

Every projection collection has its own offset, limit, complete count, and deterministic ordering.
This includes summary groups, category groups, categories, merchants, review operations, and review
targets. Limits are at most 1,000 except for the existing 400-row review-target ceiling. Transaction
amount bounds arrive as `domain.Money`; the MCP input requires currency and scale whenever either
bound is present, and the application projection restricts the comparison to that exact money
partition. `AccountProjection` returns all partitions rather than choosing one profile-wide pair.

Task 3 adds an exact-selection constructor and pure preview path:

```go
// internal/app/selection.go
func NewExplicitTransactionSelection(
    transactionIDs []string,
    revision uint64,
) (SelectionValue, error)

// internal/app/mutation_preview.go
type MutationPreviewRequest struct {
    Mutation MutationRequest
    Limit    int
}

type MutationPreviewRow struct {
    TransactionID string
    Before        domain.CategoryRef
    After         domain.CategoryRef
}

type MutationPreview struct {
    Revision      uint64
    AffectedCount int
    Rows          []MutationPreviewRow
}

func (service *Service) PreviewMutation(
    context.Context,
    MutationPreviewRequest,
) (MutationPreview, error)
```

Refactor category assignment into an internal intent that resolves and validates targets,
destination, and provider writability before operation metadata exists. `Mutate` materializes that
intent with a random operation ID and canonical time only after validation. `PreviewMutation`
returns the validated intent projection without reading randomness, allocating an operation ID, or
calling any store mutation. Do not add a fake or sentinel operation ID.

Task 4 separates the short durable transition from long execution without relinquishing the
single-worker reservation:

```go
// internal/app/provider_write.go
type ProviderWriteExecution struct {
    run     func(context.Context) (ProviderWriteStatus, error)
    release func()
}

func (service *Service) ReserveProviderWriteExecution(
    context.Context,
    *uint64,
) (ProviderWriteStatus, *ProviderWriteExecution, error)

func (execution *ProviderWriteExecution) Run(
    context.Context,
) (ProviderWriteStatus, error)

func (execution *ProviderWriteExecution) Release()
```

With a non-nil expected version, reservation performs the current resume phase/version checks and
durable `ResumeProviderWrite` transition. With nil, it reserves an already prepared writing batch.
Reservation increments `writeRuns`; `Run` or `Release` decrements it exactly once. Existing
`RunProviderWrite` and `ResumeProviderWrite` become compatibility-preserving wrappers over this
primitive for TUI/web callers. This is not a second provider worker implementation.

The MCP supervisor contract is:

```go
// internal/mcp/supervisor.go
type AttemptState string

const (
    AttemptRunning              AttemptState = "running"
    AttemptComplete             AttemptState = "complete"
    AttemptFailed               AttemptState = "failed"
    AttemptConfirmationRequired AttemptState = "confirmation_required"
)

type AttemptSnapshot struct {
    ID              string
    State           AttemptState
    Revision        string
    Generation      string
    StartedAt       time.Time
    FinishedAt      time.Time
    Fetched         int
    Total           int
    Code            string
    Detail          string
    Confirmation    string
}

type Supervisor struct {
    ctx    context.Context
    cancel context.CancelFunc
}

func NewSupervisor(context.Context, io.Reader, func() time.Time) *Supervisor
func (supervisor *Supervisor) StartRefresh(
    func(context.Context) (app.ProviderRefreshResult, error),
) (AttemptSnapshot, error)
func (supervisor *Supervisor) StartWrite(
    *app.ProviderWriteExecution,
) (app.ProviderWriteStatus, error)
func (supervisor *Supervisor) RefreshStatus(string) (AttemptSnapshot, error)
func (supervisor *Supervisor) StartReconcile(
    func(context.Context) (app.ProviderRefreshResult, error),
) (AttemptSnapshot, error)
func (supervisor *Supervisor) ReconcileStatus(string) (AttemptSnapshot, error)
func (supervisor *Supervisor) Close(context.Context) error
```

An empty status ID selects the current retained attempt of that kind for the profile. Starting a
refresh or reconciliation while one is active returns its existing ID and snapshot. The concrete
implementation adds mutexes, one refresh slot, one reconcile slot, one write slot, a wait group,
bounded terminal retention, and random opaque IDs. Attempt snapshots contain counts and stable
codes only; only the matching status path may include a process-local confirmation token.

Task 6 establishes the neutral HTTP security and token contracts:

```go
// internal/httpsecurity/basepath.go
type OriginConfig struct {
    Canonical *url.URL
    BasePath  string
}

func NormalizeBasePath(string) (string, error)
func ResolveOrigin(string, string, string) (OriginConfig, error)
func ValidateLoopbackListener(string) (string, error)
func ValidateRequestAuthority(*http.Request, OriginConfig) error
func ValidateOptionalOrigin(*http.Request, OriginConfig) error

// internal/mcp/token.go
const TokenFilename = "http-token"

type TokenStore struct {
    ProfileRoot string
    Random      io.Reader
}

func (store TokenStore) Path() string
func (store TokenStore) ReadOrCreate(context.Context) ([]byte, error)
func (store TokenStore) Reveal(context.Context) (string, error)
func (store TokenStore) Rotate(context.Context) (string, error)
func (store TokenStore) Verify(context.Context, string) error
```

`internal/home` gains `LockMCPHTTPToken`, mapped only to `mcp-http-token.lock`. Token bytes are
32 random bytes represented as exactly 43 unpadded base64url characters. `Verify` rereads the file,
bounds and strictly decodes the bearer, and uses `subtle.ConstantTimeCompare` on decoded bytes.

## Task 1: Pin the Official SDK and Build the Server Shell

**Files:**

- Modify: `go.mod`
- Modify: `go.sum`
- Create: `internal/mcp/documents.go`
- Create: `internal/mcp/results.go`
- Create: `internal/mcp/server.go`
- Create: `internal/mcp/stdio.go`
- Create: `internal/mcp/server_test.go`
- Create: `internal/mcp/results_test.go`
- Modify: `internal/provider/architecture_test.go`

- [ ] **Step 1: Recheck and pin the official stable SDK**

  Check the official releases page and tagged `go.mod`. If v1.6.1 is still the latest stable, run:

  ```bash
  go get github.com/modelcontextprotocol/go-sdk@v1.6.1
  go mod tidy
  git diff -- go.mod go.sum
  ```

  Expected: one direct SDK dependency and its transitive graph; no unrelated upgrades. If the
  stable tag changed, update the Dependency Pin Decision before running `go get`.

- [ ] **Step 2: Write failing architecture and server-lifecycle tests**

  Extend `internal/provider/architecture_test.go` to require that production files under
  `internal/mcp` import only standard packages, `internal/app`, `internal/domain`,
  `internal/provider`, `internal/httpsecurity`, `internal/home`, `internal/version`, and the MCP
  SDK. Also reject `internal/mcp` imports from store and provider trees.

  Add `internal/mcp/server_test.go` using `mcp.NewInMemoryTransports()` to connect an official
  client, list the initially registered surface, close the client, and wait for the server session.

  Run:

  ```bash
  go test ./internal/provider ./internal/mcp -run 'Test(MCPDependencyBoundary|ServerLifecycle)' -count=1
  ```

  Expected: fail because the MCP package and server constructor do not exist.

- [ ] **Step 3: Implement canonical documents and bounded results**

  Add the version-one header, exact-money converter, application-error mapper, and
  `MaxResponseContentBytes`. The result helper must marshal the logical document once, reject when
  `2 * len(canonicalJSON)` exceeds 8 MiB, and return both `StructuredContent` and one JSON
  `TextContent`. For business failures, return the same bounded document with `IsError = true`;
  return Go errors only for genuine protocol/server failures.

  Add focused tests for exact money, revision strings, structured/text equivalence, bounded success,
  bounded error, and preservation of a prior specific error over `mcp_response_too_large`.

- [ ] **Step 4: Implement server construction and stdio lifecycle**

  Validate non-nil service, stable profile ID, absolute profile root, clock, randomness, and logger.
  Create one SDK server and one process-lifetime supervisor. Use the SDK logger on stderr only.
  `RunStdio` delegates to `mcp.StdioTransport`; it emits no startup text. `Close` cancels the
  supervisor, waits for owned work, and returns consequential cleanup errors.

  Register no placeholder tools. Task 2 adds the always-registered surface and Task 3 adds optional
  write tools.

- [ ] **Step 5: Verify the shell and no-CGO build**

  ```bash
  gofmt -w internal/mcp internal/provider/architecture_test.go
  go test ./internal/mcp ./internal/provider -count=1
  CGO_ENABLED=0 go test ./internal/mcp -count=1
  go mod verify
  ```

  Expected: all pass; the official client completes one lifecycle against the Moneyflow server.

- [ ] **Step 6: Commit Task 1**

  ```bash
  git add go.mod go.sum internal/mcp internal/provider/architecture_test.go
  git commit -m "feat: add Go MCP server foundation"
  ```

## Task 2: Add Effective Reads, Resources, and Exact Wire Results

**Files:**

- Create: `internal/app/tool_projection.go`
- Create: `internal/app/tool_projection_test.go`
- Create: `internal/mcp/types_read.go`
- Create: `internal/mcp/tools_read.go`
- Create: `internal/mcp/tools_read_test.go`
- Create: `internal/mcp/resources.go`
- Create: `internal/mcp/resources_test.go`
- Modify: `internal/mcp/server.go`
- Modify: `internal/mcp/results.go`
- Modify: `internal/app/transaction_info.go`

- [ ] **Step 1: Write failing literal-search and window tests**

  In `internal/app/tool_projection_test.go`, seed effective transactions whose notes contain a term
  absent from merchant/category, whose labels contain regexp metacharacters, and whose categories
  have ambiguous normalized labels. Assert:

  - literal search includes merchant, category, and notes;
  - `.` and `[` are literal and never compile as regex;
  - merchant-only filtering is literal and case-insensitive;
  - dates are inclusive and amounts use exact `domain.Money` comparisons;
  - amount bounds require explicit currency and scale, restrict results to that partition, and a
    mixed-currency account projection returns every partition;
  - category ID and unique label agree, while ambiguous label fails;
  - offsets, non-positive limits, limits above 1,000, and reversed dates fail;
  - transaction, summary-group, category-group, category, merchant, review-operation, and
    review-target total counts are complete while every returned window is bounded and
    deterministic;
  - an external revision advance is observed before projection.

  Run:

  ```bash
  go test ./internal/app -run 'TestTool(TransactionWindow|LiteralSearch|Catalog|Account)' -count=1
  ```

  Expected: fail because the neutral projection methods do not exist.

- [ ] **Step 2: Implement neutral read projections**

  Add `TransactionWindow`, catalog, account, summary, and uncategorized projection methods. Reuse
  existing `domain`, analytics, pending-summary, capabilities, provider status, write status, and
  transaction-information logic. Do not expose service-owned maps or slices. Copy under the
  service lock, then filter, sort, and serialize after releasing it.

  Add source-profile display-name resolution to the existing Amazon matcher boundary rather than
  opening SQLite from MCP. Keep the 20-match and 100-item information bounds already enforced by
  `TransactionInfo`.

- [ ] **Step 3: Write failing official-client read-tool tests**

  Connect the official SDK client over in-memory transports and assert exact schemas and behavior
  for all 14 always-registered tools:

  ```text
  search_transactions
  get_transactions
  get_spending_summary
  get_categories
  get_merchants
  get_account_info
  get_uncategorized_transactions
  get_amazon_order_details
  get_transaction_details
  review_changes
  get_commit_status
  refresh_data
  get_refresh_status
  confirm_refresh_deletions
  ```

  At this checkpoint the three refresh tools may return the exact capability-unavailable result
  from a local test profile; Task 5 replaces their handlers with supervised Monarch behavior.

  Assert exact amount strings, minor-unit strings, currency, scale, revision strings, totals,
  offsets, limits, truncation, pending values, and structured/text equality.

  Run:

  ```bash
  go test ./internal/mcp -run 'Test(ReadTools|ToolWindows|ExactMoney|AlwaysRegistered)' -count=1
  ```

  Expected: fail because read handlers and registrations do not exist.

- [ ] **Step 4: Implement the 14 read handlers**

  Define typed SDK inputs with JSON Schema descriptions and strict limits. Parse dates with
  `domain.ParseDate`; require currency and scale with either amount bound and parse strings with
  `domain.ParseMoney` for that exact partition. Reject JSON numeric amounts through typed string
  schemas. Convert app projections into version-one documents with stable local IDs only.

  `get_spending_summary` defaults to the trailing 30 calendar days from the injected clock and
  includes expenses only. `get_uncategorized_transactions` resolves the active Uncategorized
  identity, not its label. `get_account_info` adds profile ID/name and every active money partition
  at the adapter boundary and omits external provider identity.

- [ ] **Step 5: Add all five resource aliases**

  Register:

  ```text
  moneyflow://account
  moneyflow://categories
  moneyflow://merchants/top
  moneyflow://spending/monthly
  moneyflow://transactions/recent
  ```

  Each resource calls the same projection function as its tool, revalidates revision, and returns
  the same canonical JSON document. Monthly uses the injected current calendar month; top merchants
  and recent transactions use limit 50. Category and account resources use explicit bounded
  collection windows and include complete counts. Add equality tests at one revision.

- [ ] **Step 6: Verify Task 2**

  ```bash
  gofmt -w internal/app internal/mcp
  go test ./internal/app ./internal/mcp -count=1
  go test ./internal/mcp -run 'TestOfficialClient(ReadTools|Resources)' -count=1
  ```

- [ ] **Step 7: Commit Task 2**

  ```bash
  git add internal/app internal/mcp
  git commit -m "feat: expose Moneyflow MCP reads"
  ```

## Task 3: Add Dry Run, Journal Mutations, Review, Undo, and Redo

**Files:**

- Modify: `internal/app/edit_category.go`
- Modify: `internal/app/mutations.go`
- Modify: `internal/app/profile_service.go`
- Modify: `internal/app/selection.go`
- Create: `internal/app/mutation_preview.go`
- Create: `internal/app/mutation_preview_test.go`
- Create: `internal/mcp/types_write.go`
- Create: `internal/mcp/tools_write.go`
- Create: `internal/mcp/tools_write_test.go`
- Modify: `internal/mcp/server.go`

- [ ] **Step 1: Write failing pure-preview tests**

  Inject randomness that fails on read and a profile spy whose mutation methods fail the test.
  Assert `PreviewMutation` performs current-revision, exact-target, active-category, capability,
  batch-size, and Monarch-writability validation while leaving revision, journal, cursor,
  selection, and committed state unchanged. Assert its before/after rows equal a subsequent real
  mutation's affected rows.

  Run:

  ```bash
  go test ./internal/app -run 'TestMutationPreview' -count=1
  ```

  Expected: fail because the preview path does not exist.

- [ ] **Step 2: Refactor category planning into intent then materialization**

  Split `BuildCategoryAssignment` so target/destination/provider validation occurs without
  `OperationMetadata`. Keep the current public builder as a thin materializing wrapper for existing
  tests and renderers. `Mutate` generates operation identity/time only after intent validation.
  `PreviewMutation` runs the same intent and provider checks, applies it to a cloned effective
  snapshot for the bounded row diff, and never touches randomness or store mutation APIs.

  Add `NewExplicitTransactionSelection`: sort and reject duplicate/empty IDs, enforce existing
  selection bounds, create an explicit transaction selection bound to the supplied revision, and
  return the canonical opaque value. Do not expose selection JSON to `internal/mcp`.

- [ ] **Step 3: Write failing registration and all-or-nothing tests**

  Assert read-only mode omits these tools and `--allow-write` adds exactly:

  ```text
  update_transaction_category
  batch_update_category
  undo_changes
  redo_changes
  commit_changes
  pause_commit
  resume_commit
  stop_and_reconcile
  get_reconcile_status
  confirm_reconcile
  ```

  At this checkpoint commit/control handlers may return a typed capability result; Task 4 supplies
  the supervisor behavior. Fully test category tools, review, undo, and redo now. Cover one target,
  100 targets, 101-target refusal, duplicates, missing/retired targets, ID/name equivalence,
  ambiguous labels, stale revision, dry run, single-operation grouping, and no provider call.

- [ ] **Step 4: Implement category, review, undo, and redo handlers**

  Resolve category ID or unique normalized label through the application catalog projection. Build
  a detail-state explicit selection at the checked revision, then call `PreviewMutation` for dry run
  or `Service.Mutate` otherwise. Use `Service.Review`, `UndoInteraction`, and `RedoInteraction`
  directly. Window active and inactive operation summaries with complete counts, then return a
  separately windowed target detail collection with the existing 400-target maximum.

  Keep transaction IDs unique and canonicalize bytewise before creating the selection. Do not let
  MCP handlers construct `domain.Operation` values.

- [ ] **Step 5: Verify Task 3**

  ```bash
  gofmt -w internal/app internal/mcp
  go test ./internal/app ./internal/mcp -run 'Test(MutationPreview|CategoryTool|ReviewTool|UndoRedo|WriteRegistration)' -count=1
  go test ./internal/app ./internal/mcp -count=1
  ```

- [ ] **Step 6: Commit Task 3**

  ```bash
  git add internal/app internal/mcp
  git commit -m "feat: stage category edits through MCP"
  ```

## Task 4: Add Explicit Commit Supervision and Batch Controls

**Files:**

- Modify: `internal/app/provider_write.go`
- Modify: `internal/app/provider_write_test.go`
- Create: `internal/mcp/supervisor.go`
- Create: `internal/mcp/supervisor_test.go`
- Modify: `internal/mcp/tools_write.go`
- Modify: `internal/mcp/tools_write_test.go`
- Modify: `internal/mcp/server.go`

- [ ] **Step 1: Write failing reservation/worker tests**

  Cover an already prepared batch and a paused/retryable batch. Assert reservation performs the
  phase/version/lease transition synchronously, returns the current status, and keeps `writeRuns`
  reserved until its one-shot execution is run or released. Two concurrent reservations must start
  at most one worker. Calling `Run` or `Release` twice must not double-decrement or start work.

  Preserve existing `RunProviderWrite`, `ResumeProviderWrite`, pause, idle notification, lease,
  crash-uncertain update, and crash-uncertain delete tests unchanged.

  Run:

  ```bash
  go test ./internal/app -run 'TestProviderWrite(Reservation|SingleWorker|Resume)' -count=1
  ```

  Expected: fail because the reservable one-shot execution does not exist.

- [ ] **Step 2: Implement the short-transition execution primitive**

  Move the current resume checks and store transition into `ReserveProviderWriteExecution`. Reserve
  `writeRuns` before the authoritative transition and release it on every failed path. Return a
  one-shot execution whose `Run` delegates to `runProviderWriteOwned`; its `Release` is the launch
  failure/shutdown path. Rebuild existing synchronous methods as wrappers so TUI and web behavior
  stays unchanged.

- [ ] **Step 3: Write failing supervisor tests**

  Use blocked fake write and reconciliation executions to prove both starts return promptly, the
  request context can be canceled without canceling accepted work, only one process-local operation
  of each kind runs, a second start returns the active attempt, empty-ID status recovers it, terminal
  status is retained without financial fields, and server shutdown cancels then waits. Exercise the
  race detector on the supervisor package.

- [ ] **Step 4: Implement commit and batch-control tools**

  `commit_changes` calls `Service.Commit` with equal expected/reviewed revisions. Local and Amazon
  return after atomic fold. Monarch uses the returned prepared status, reserves the already
  prepared execution, launches it through the supervisor, and returns immediately without claiming
  remote completion.

  Implement `get_commit_status`, `pause_commit`, `resume_commit`, `stop_and_reconcile`,
  `get_reconcile_status`, and `confirm_reconcile`. Resume performs reservation synchronously and
  launches the worker only after the transition succeeds. Stop-and-reconcile starts a supervised
  server-owned attempt and returns its opaque ID immediately; status accepts that ID or an empty ID
  for lost-response recovery and is the only path that returns the matching confirmation token.
  Confirmation remains a short authoritative transition and requires the attempt ID. Tool-request
  cancellation after acceptance does not cancel the provider fetch; transaction-boundary rules
  still govern the final fold.

- [ ] **Step 5: Verify Task 4**

  ```bash
  gofmt -w internal/app internal/mcp
  go test ./internal/app ./internal/mcp -run 'Test(ProviderWrite|CommitTool|BatchControl|Supervisor)' -count=1
  MONEYFLOW_SKIP_PERF=1 go test -race ./internal/app ./internal/mcp -run 'Test(ProviderWrite|Supervisor)' -count=1
  make test-provider-write
  ```

- [ ] **Step 6: Commit Task 4**

  ```bash
  git add internal/app/provider_write.go internal/app/provider_write_test.go internal/mcp
  git commit -m "feat: supervise MCP provider commits"
  ```

## Task 5: Add Explicit Asynchronous Monarch Refresh

**Files:**

- Modify: `internal/app/provider_refresh.go`
- Modify: `internal/app/provider_refresh_test.go`
- Modify: `internal/mcp/supervisor.go`
- Modify: `internal/mcp/supervisor_test.go`
- Modify: `internal/mcp/tools_read.go`
- Modify: `internal/mcp/tools_read_test.go`

- [ ] **Step 1: Write failing renderer and refresh-attempt tests**

  Add `mcp` to the accepted provider renderer identities and assert it appears only in lease/status
  ownership, never a scheduler. With a blocked fake Monarch source, assert `refresh_data` returns an
  opaque attempt ID promptly, `get_refresh_status` observes running then terminal state, empty-ID
  status recovers a lost acceptance response, and a second start returns the active attempt ID and
  status without starting another fetch.

  Cover local and Amazon capability reasons, read-only registration, journal rebase/redo-tail
  behavior, request cancellation after acceptance, and server shutdown.

- [ ] **Step 2: Implement refresh supervision**

  `refresh_data` asks the supervisor to run exactly one `Service.RefreshProvider` with
  `Manual: true`, default view state, empty selection, and a bounded window. The worker context is
  the server lifetime. The terminal snapshot stores stable code, revision, generation, counts,
  timestamps, and safe guidance only.

  Do not call `ProviderRefreshDue`, start a ticker, or inspect staleness on startup. Do not resume a
  write batch while starting or polling refresh.

- [ ] **Step 3: Implement deletion-confirmation status and tool**

  When the provider result parks on deletion confirmation, retain its process-local token only in
  the matching attempt. `get_refresh_status` is the only read that exposes it; an omitted attempt ID
  selects the current retained refresh attempt for this profile and process.
  `confirm_refresh_deletions` requires both attempt ID and exact token, calls
  `Service.ConfirmProviderRefresh`, and replaces the attempt terminal state.

  Test expiry, wrong process, stale generation, identity mismatch, integrity failure, and a server
  restart losing the candidate. Read-only mode retains this tool.

- [ ] **Step 4: Verify Task 5**

  ```bash
  gofmt -w internal/app internal/mcp
  go test ./internal/app ./internal/mcp -run 'Test(MCPRenderer|RefreshTool|RefreshAttempt|RefreshConfirmation)' -count=1
  MONEYFLOW_SKIP_PERF=1 go test -race ./internal/mcp -run 'TestRefresh' -count=1
  make test-provider
  ```

- [ ] **Step 5: Commit Task 5**

  ```bash
  git add internal/app/provider_refresh.go internal/app/provider_refresh_test.go internal/mcp
  git commit -m "feat: expose explicit Monarch refresh over MCP"
  ```

## Task 6: Extract HTTP Security and Add Token-Protected Streamable HTTP

**Files:**

- Create: `internal/httpsecurity/basepath.go`
- Create: `internal/httpsecurity/request.go`
- Create: `internal/httpsecurity/basepath_test.go`
- Create: `internal/httpsecurity/request_test.go`
- Modify: `internal/api/basepath.go`
- Modify: `internal/api/security.go`
- Modify: `internal/api/security_test.go`
- Modify: `cmd/moneyflow/web.go`
- Modify: `cmd/moneyflow/web_test.go`
- Modify: `internal/home/lock.go`
- Modify: `internal/home/lock_test.go`
- Create: `internal/mcp/token.go`
- Create: `internal/mcp/token_test.go`
- Create: `internal/mcp/http.go`
- Create: `internal/mcp/http_test.go`
- Create: `cmd/moneyflow/mcp.go`
- Create: `cmd/moneyflow/mcp_dependencies.go`
- Create: `cmd/moneyflow/mcp_test.go`
- Modify: `cmd/moneyflow/root.go`
- Modify: `cmd/moneyflow/root_test.go`
- Modify: `internal/provider/architecture_test.go`

- [ ] **Step 1: Write characterization tests for existing web security**

  Before moving code, add focused tests that exercise the existing public behavior: strict base
  paths, listener/external-URL agreement, canonical authority, direct-listener rejection,
  same-origin mutation acceptance, cross-origin rejection, and absent CORS credentials. Run them
  against the current package and observe green; these are refactor guards, not new hardening.

  ```bash
  go test ./internal/api ./cmd/moneyflow -run 'Test(Origin|BasePath|Web.*External|Mutation.*Origin)' -count=1
  ```

- [ ] **Step 2: Extract neutral validation without behavior change**

  Move `NormalizeBasePath`, `OriginConfig`, `ResolveOrigin`, canonicalization, authority validation,
  and loopback-listener validation into `internal/httpsecurity`. Add exact Host and optional-Origin
  request validators there. Keep browser mutation tokens, Fetch Metadata, Huma problems, and web
  response policy in `internal/api`.

  Update web command/server callers and type aliases only where needed. Rerun the characterization
  tests before adding MCP HTTP behavior.

- [ ] **Step 3: Write failing token lifecycle tests**

  Add `LockMCPHTTPToken` and assert independent exclusive locking, sequential reuse, and process-
  death release using the existing home lock test pattern. Test first-use creation, owner-only file
  handling, strict 43-character canonical encoding, malformed refusal, bounded reads, atomic rotate,
  reveal, per-request reread, and constant-time decoded comparison behavior owned by `TokenStore`.

  Run:

  ```bash
  go test ./internal/home ./internal/mcp -run 'Test(MCPHTTPToken|TokenStore)' -count=1
  ```

  Expected: fail because the lock and token store do not exist.

- [ ] **Step 4: Implement the private token store**

  Use `home.EnsurePrivateSubdirectory`, `home.WritePrivateFile`, `home.ReadPrivateFile`, and
  `home.TryLockExisting`; do not reproduce permissions or atomic replacement. Never return the
  token in ordinary startup/status results. Reveal and rotate return it only to their explicit
  Cobra stdout caller.

- [ ] **Step 5: Write failing HTTP middleware tests**

  Through `httptest`, cover default exact path, not-found other paths, loopback-only listener
  validation, canonical Host, external URL, direct authority, absent/present exact Origin,
  malformed/multiple/null/mismatched Origin, missing/malformed/oversized/wrong/query bearer,
  preflight rejection, no cookies/CORS credentials, no-store/nosniff, and body-not-read before
  security succeeds. Use the official streamable client for one authenticated initialize/list/call.

- [ ] **Step 6: Implement stateless streamable HTTP**

  Wrap `mcp.NewStreamableHTTPHandler` with exact-path, token, authority, optional-Origin, request-body
  limit, and response-header middleware in that order. Configure `Stateless: true` and
  `JSONResponse: true`. Use bounded `http.Server` timeouts and graceful shutdown. Ignore forwarded
  host/scheme headers; the trusted proxy must preserve canonical Host.

- [ ] **Step 7: Add the Cobra command and token subcommands**

  Register:

  ```text
  moneyflow mcp --profile <name-or-id>
  moneyflow mcp --profile <name-or-id> --allow-write
  moneyflow mcp --profile <name-or-id> --transport streamable-http
  moneyflow mcp token reveal --profile <name-or-id>
  moneyflow mcp token rotate --profile <name-or-id>
  ```

  Default transport is `stdio`. HTTP defaults to `127.0.0.1:8081`, `/mcp/`, and no external URL.
  Resolve and open one profile through `openProfile`, configure Monarch with renderer `mcp`, and
  configure Amazon matching through the catalog matcher. The runner owns signals, server close,
  profile close, and joined cleanup errors. Startup prints only the token path and canonical HTTP
  endpoint to stderr; stdio prints nothing outside frames.

  Add injectable `MCPRunner` and dependency builder seams to `IOStreams`. Profile resolution tests
  cover ID precedence, unique normalized name, ambiguous name, sole-profile default, and explicit
  failure guidance. HTTP command tests cover the exact default `/mcp/` endpoint. Token subcommands
  never start an MCP server.

- [ ] **Step 8: Verify Task 6**

  ```bash
  gofmt -w internal/httpsecurity internal/api internal/home internal/mcp cmd/moneyflow
  go test ./internal/httpsecurity ./internal/api ./internal/home ./internal/mcp ./cmd/moneyflow -count=1
  go test ./internal/provider -run 'TestMCPDependencyBoundary' -count=1
  make verify-web
  ```

- [ ] **Step 9: Commit Task 6**

  ```bash
  git add internal/httpsecurity internal/api internal/home internal/mcp internal/provider cmd/moneyflow
  git commit -m "feat: serve authenticated MCP over HTTP"
  ```

## Task 7: Complete Interoperability, Privacy, Performance, and Documentation

**Files:**

- Create: `internal/mcp/subprocess_test.go`
- Create: `internal/mcp/performance_test.go`
- Modify: `internal/mcp/*_test.go`
- Modify: `cmd/moneyflow/*_test.go`
- Modify: `Makefile`
- Modify: `README.md`
- Create: `docs/guide/mcp.md`

- [ ] **Step 1: Add subprocess stdio and HTTP interoperability tests**

  Build the real command to a test-owned temporary path. Start stdio with an isolated temporary
  `MONEYFLOW_HOME`, drive it with the official SDK client, and assert stdout contains protocol
  frames only. Capture stderr and assert every line fits the privacy allowlist.

  Start loopback HTTP on an ephemeral port with a test token, use the official streamable client to
  list resources and call representative read/dry-run tools, rotate the token, and prove the old
  value fails while the new value works without restart.

- [ ] **Step 2: Add cross-process and restart tests**

  Exercise external revision advance, stale mutation/review/commit/control, refresh/write lease
  ownership, one process exiting during an attempt, durable write status observation from a new
  process, and process-local confirmation loss. Assert MCP never auto-resumes ownerless work after
  restart.

- [ ] **Step 3: Add privacy and response-ceiling scans**

  Use synthetic sentinel labels, notes, search text, amounts, provider IDs, and token values. Assert
  they appear only in the explicitly requested authenticated tool result and never in logs,
  background status, error envelopes, correlation IDs, stdout diagnostics, or HTTP URLs.

  Generate results just below and above the 8 MiB combined-content limit. Assert oversized
  success becomes bounded `mcp_response_too_large`, while an existing business failure stays the
  business failure and remains within the ceiling.

- [ ] **Step 4: Add owned performance gates**

  With 100,000 synthetic transactions, measure cached local search and list projections below the
  existing 50 ms local / 100 ms CI interaction ceiling, dry run and 100-target staging under the
  same ceiling, and serialization memory bounded by the requested window plus response document.
  Keep network timing out of benchmarks.

  Run each new gate in isolation three times before adding it to `test-mcp`.

- [ ] **Step 5: Add `test-mcp` and documentation**

  Add a focused Make target that runs MCP package, architecture, command, no-CGO, subprocess,
  race, and performance checks. Add it to `verify-go` after existing provider-write checks.

  Document stdio client configuration, explicit profile selection, read-only default,
  `--allow-write`, staged edits, review/undo/redo/commit, explicit refresh/status/confirmation,
  HTTP token reveal/rotate, loopback/Caddy use, and reconnect guidance. Examples use placeholders
  only and never include a real token, hostname, profile name, or financial value.

- [ ] **Step 6: Run focused and broad verification**

  ```bash
  make test-mcp
  make verify-go
  make verify-web
  make test-race
  uv run pytest -v
  uv run pyright moneyflow/
  uv run ruff format --check moneyflow/ tests/
  uv run ruff check moneyflow/ tests/
  npx --yes markdownlint-cli@0.47.0 --config .markdownlint.json README.md 'docs/**/*.md'
  .github/scripts/check-arrow-lists.sh
  ```

  If the repository-supported non-performance path is needed for an environmental timing failure,
  run it, rerun the isolated timing gate three times, and report the exact noisy gate. Do not leave
  otherwise verified work uncommitted.

- [ ] **Step 7: Review scope and private-data safety**

  ```bash
  git diff --check
  git status --short
  git diff --stat
  rg -n 'internal/(store|store/sqlite|api|tui|web|provider/monarch)' internal/mcp
  rg -n 'float32|float64|REAL' internal/mcp internal/app/tool_projection.go internal/app/mutation_preview.go
  git ls-files internal/web/dist web/tests/screenshots
  ```

  Expected: no forbidden production imports, floating-point money, generated assets, screenshots,
  schema changes, migrations, direct provider mutation, Amazon import, YNAB, SimpleFIN, or Python
  shim code.

- [ ] **Step 8: Optional non-destructive live verification**

  Only after the automated commit, use an already connected disposable or explicitly approved
  profile to verify stdio initialization, read tools, category dry run, staging, review, undo/redo,
  and one small category commit. Exercise HTTP only through loopback and the configured canonical
  proxy URL. Do not manufacture destructive deletion or provider-failure states on real data.

  Any finding from live verification becomes a new tested commit.

- [ ] **Step 9: Commit Task 7**

  ```bash
  git add Makefile README.md docs/guide/mcp.md internal/mcp cmd/moneyflow
  git commit -m "docs: complete Go MCP integration"
  ```

## Final Plan Audit

Before implementation is declared complete, verify every approved design obligation maps to a
test or diff check:

- fourteen always-registered tools, five resources, and write-only conditional tools;
- exact money, string revisions, bounded windows, and the 8 MiB combined ceiling;
- literal notes-inclusive MCP search without changing TUI/web regex search;
- pure dry run, one-operation batch staging, review, undo, redo, and explicit commit;
- local/Amazon atomic fold and Monarch durable batch preparation;
- short resume transition, preserved `writeRuns`, and explicit server-owned worker lifetime;
- explicit-only Monarch refresh, process-local status/confirmation, and no scheduler;
- stdio framing isolation and authenticated loopback streamable HTTP;
- token creation/reveal/rotation and exact Host/Origin rules;
- unchanged web security after extraction;
- privacy allowlist, cross-process CAS/lease behavior, race coverage, and performance gates;
- no schema change, migration, direct provider mutation, generated assets, screenshots, or
  out-of-scope provider/shim work.
