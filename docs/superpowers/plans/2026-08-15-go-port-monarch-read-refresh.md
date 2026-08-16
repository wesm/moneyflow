# Go Port Monarch Read, Import, and Refresh Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development
> (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use
> checkbox (`- [ ]`) syntax for tracking.

**Status:** Ready for implementation

**Goal:** Connect one pristine Go profile to Monarch Money, import and fully reconcile posted
transactions, preserve durable pending edits across refresh, and expose the same safe refresh
workflow through the CLI, Bubble Tea TUI, and Svelte web application.

**Architecture:** `internal/provider` defines provider-neutral read-side values and errors;
`internal/provider/monarch` owns authentication, session files, HTTP, GraphQL, and wire decoding.
`internal/app` maps stable identities, rebases the journal, and coordinates leases and refreshes.
`internal/store/sqlite` installs schema version 3 and atomically folds a closed, deterministic
refresh plan after revision and refresh-generation checks.

**Tech Stack:** Go 1.26.3; `modernc.org/sqlite` v1.56.0; Huma v2.38.0 with `humago`; Cobra
1.10.2; Bubble Tea 2.0.8; Svelte 5.56.3 in runes mode; Bun 1.3.14; `@kenn-io/kit-ui` commit
`16db58ef8122dd00e21ce8ad90ba295b9174c6ef`; Vitest 4.1.10; Playwright 1.61.1.

## Global Constraints

- Work only on the checked-out `go-port` branch. Do not switch branches, pull, rebase, push,
  merge, or remove Python without explicit user permission.
- This slice is read/import/refresh only. Do not add provider mutations, a writer interface,
  outbound queues, local-only provider commit, or remote delete/update queries.
- Keep the existing single default profile. There is no `--replace` option and no profile manager.
- Install schema version 3 only into an empty database. Reject version 2 and every other mismatch;
  do not add schema or journal-payload migrations before Go v2 stabilizes.
- Preserve the pure-Go, no-CGO Linux, macOS, and Windows contract.
- Parse provider decimals directly to signed integer minor units. Do not use `float32`, `float64`,
  or SQLite `REAL` for money.
- Never persist a generated one-time code or plaintext credentials. Store hardened Monarch session
  material and a separate account-password-encrypted credential vault outside SQLite.
- Keep GraphQL and Monarch wire types inside `internal/provider/monarch`; keep SQL and driver types
  inside `internal/store/sqlite`.
- `internal/provider` imports only `internal/domain`; Monarch never imports store; store never
  imports provider; application code is the only orchestrator.
- Fetch a complete visible and hidden transaction snapshot. Pending rows participate in integrity
  checks but never enter committed state.
- A refresh lease coordinates work only. Refresh-generation compare-and-swap is authoritative for
  candidate freshness, while profile revision protects journal inputs.
- Refresh planning is closed and deterministic. It performs no I/O and reads no clock, randomness,
  global state, or store handle.
- Provider refresh may retain or shrink a journal but never append bookkeeping operations. Keep the
  10,000-operation and 1,000,000-target ceilings.
- Persist and log only allowlisted codes, revisions, generations, counts, percentages, timings,
  renderer class, opaque instance IDs, and correlation IDs. Never log labels, search text,
  provider IDs, GraphQL bodies, credentials, paths, URLs, or financial values.
- Use synthetic provider data in tests and artifacts. Live characterization is explicit, local,
  counts-only, and never runs in CI.
- Ordinary checks never rewrite parity, OpenAPI, generated TypeScript, embedded assets, or
  screenshots. Use deliberate update targets and inspect complete generated diffs.
- Use TDD for every behavior change. Use `apply_patch` for hand edits. Before every task commit,
  use the commit, privacy-scrub, and verification workflows. Never amend.
- After Task 10, invoke `$roborev-fix` and resolve its review before Task 11. Invoke it again after
  the complete implementation and rerun every affected verification gate.

## Target File Map

```text
internal/domain/provider.go                 provider-neutral import records
internal/provider/provider.go               read-side contracts and progress
internal/provider/errors.go                 stable errors and scheduler classes
internal/provider/monarch/client.go         read-only client composition
internal/provider/monarch/graphql.go        bounded GraphQL transport
internal/provider/monarch/queries.go        minimal query documents
internal/provider/monarch/auth.go           REST-first login and GraphQL fallback
internal/provider/monarch/session.go        provider-owned session schema
internal/provider/monarch/session_file.go   hardened atomic session persistence
internal/provider/monarch/snapshot.go       complete two-partition snapshot reader
internal/home/private_file*.go              shared atomic owner-only file mechanics
internal/app/provider_identity.go            stable ID and sticky-label planning
internal/app/provider_rebase.go              pure journal rewrite
internal/app/provider_refresh.go             lease, guard, fold, and confirmation flow
internal/app/provider_scheduler.go           six-hour cadence and retry classification
internal/store/store.go                      provider state and atomic refresh contract
internal/store/sqlite/schema/profile.sql     exact schema version 3
internal/store/sqlite/provider_state.go      binding, generation, status, and lease
internal/store/sqlite/provider_refresh.go    transactional callback and fold
cmd/moneyflow/provider.go                    connect/disconnect commands
internal/tui/provider.go                     refresh command, progress, and scheduler
internal/api/provider.go                     status, refresh, and confirmation endpoints
web/src/lib/controller/provider.ts           browser refresh state machine
web/src/components/ProviderStatus.svelte     status, progress, and confirmation UI
web/tests/provider.spec.ts                   browser provider journeys
docs/superpowers/benchmarks/                 100k refresh evidence
```

## Required Commit Gate

Run each task's focused red/green command while developing. Before every task commit, run:

```bash
make verify-go
uv run pytest -v
uv run pyright moneyflow/
uv run ruff format --check moneyflow/ tests/
uv run ruff check moneyflow/ tests/
npx --yes markdownlint-cli@0.47.0 --config .markdownlint.json README.md 'docs/**/*.md'
.github/scripts/check-arrow-lists.sh
```

Expected: every command exits zero. Beginning with Task 12, also run:

```bash
bun install --cwd web --frozen-lockfile
make verify-web
```

Expected: the lockfile is unchanged; generated API/types/assets are current; frontend type,
format, lint, unit, audit, build, browser, accessibility, security, and visual checks pass.

---

### Task 1: Define Provider-Neutral Read Contracts and Import Records

**Files:**

- Create: `internal/domain/provider.go`
- Create: `internal/domain/provider_test.go`
- Create: `internal/provider/provider.go`
- Create: `internal/provider/errors.go`
- Create: `internal/provider/errors_test.go`
- Create: `internal/provider/architecture_test.go`

**Interfaces:**

- Consumes: `domain.Money`, `domain.Date`, `domain.EntityKind`, and `context.Context`.
- Produces: `domain.ImportSnapshot`, `provider.Reader`, `provider.Source`, `provider.Connector`,
  `provider.Error`, and the complete stable provider error-code set.

- [ ] **Step 1: Write failing contract and architecture tests.** Require defensive cloning,
      duplicate external-ID rejection, posted/pending distinction, safe error strings, complete
      codes, and import-direction checks that reject store imports from provider packages.

  ```go
  func TestErrorStringContainsOnlyCodeAndSafeDetail(t *testing.T) {
      failure := provider.NewError(provider.CodeReconnectRequired)
      assert.Equal(t, "provider_reconnect_required: reconnect through the CLI", failure.Error())
      assert.Nil(t, errors.Unwrap(failure))
  }
  ```

  Feed synthetic raw transport text into Monarch's translation tests and prove neither the
  returned neutral error nor any unwrap chain contains it.

- [ ] **Step 2: Run the focused tests and verify RED.**

  ```bash
  go test ./internal/domain ./internal/provider -run 'Test(Import|Error|Architecture)' -count=1
  ```

  Expected: FAIL because provider import types and contracts do not exist.

- [ ] **Step 3: Add the minimal closed public shapes.**

  ```go
  // internal/domain/provider.go
  type ImportEntity struct {
      Kind EntityKind
      ExternalID string
      Label string
      ParentExternalID string
  }

  type ImportTransaction struct {
      ExternalID string
      AccountExternalID string
      MerchantExternalID string
      CategoryExternalID string
      Date Date
      Amount Money
      Notes string
      Hidden bool
      Pending bool
  }

  type ImportSnapshot struct {
      Accounts []ImportEntity
      Merchants []ImportEntity
      Groups []ImportEntity
      Categories []ImportEntity
      Transactions []ImportTransaction
      ObservedAt time.Time
  }
  ```

  ```go
  // internal/provider/provider.go
  type ProfileIdentity struct { Kind, RemoteID string }
  type Progress struct { Partition string; Fetched, Total, Attempt int }
  type ProgressFunc func(Progress)

  type Credentials struct { Login, Password string }
  type Challenge struct { Kind, Prompt string }
  type ChallengeResponder func(context.Context, Challenge) (string, error)
  type Session interface { ProviderKind() string }

  type Connector interface {
      Connect(context.Context, Credentials, ChallengeResponder) (Session, error)
      Validate(context.Context, Session) (ProfileIdentity, error)
  }

  type Reader interface {
      ProbeIdentity(context.Context) (ProfileIdentity, error)
      FetchSnapshot(context.Context, ProgressFunc) (domain.ImportSnapshot, error)
  }

  type SessionFingerprint string
  type Source interface {
      Reader(context.Context, bool) (Reader, SessionFingerprint, error)
      Changed(SessionFingerprint) (bool, error)
  }
  ```

  Define all spec codes as typed constants:

  ```go
  const (
      CodeReconnectRequired ErrorCode = "provider_reconnect_required"
      CodeIdentityMismatch ErrorCode = "provider_identity_mismatch"
      CodeSnapshotUnstable ErrorCode = "provider_snapshot_unstable"
      CodeRefreshInProgress ErrorCode = "provider_refresh_in_progress"
      CodeDeletionConfirmationRequired ErrorCode = "provider_deletion_confirmation_required"
      CodeConfirmationInvalid ErrorCode = "provider_confirmation_invalid"
      CodeRefreshStale ErrorCode = "provider_refresh_stale"
      CodeRateLimited ErrorCode = "provider_rate_limited"
      CodeUnavailable ErrorCode = "provider_unavailable"
      CodeDataInvalid ErrorCode = "provider_data_invalid"
  )
  ```

  `Error.Error` returns only code plus its fixed allowlisted detail. The neutral error exposes no
  raw cause or unwrap chain; Monarch classifies raw HTTP and GraphQL failures and discards their
  content at the adapter boundary. Construction replaces unknown codes with `CodeDataInvalid`.
  Rate-limit errors alone may carry a validated duration capped at 24 hours through
  `NewErrorWithRetry` and `RetryAfterOf`; raw header text never crosses the boundary.

- [ ] **Step 4: Run the focused tests and verify GREEN.**

  ```bash
  go test ./internal/domain ./internal/provider -count=1
  ```

  Expected: PASS.

- [ ] **Step 5: Run the required commit gate and commit.**

  ```bash
  git add internal/domain/provider.go internal/domain/provider_test.go internal/provider
  git commit -m "feat: define provider read contracts"
  ```

### Task 2: Build the Minimal Monarch GraphQL Reader

**Files:**

- Create: `internal/provider/monarch/client.go`
- Create: `internal/provider/monarch/client_test.go`
- Create: `internal/provider/monarch/graphql.go`
- Create: `internal/provider/monarch/graphql_test.go`
- Create: `internal/provider/monarch/queries.go`
- Create: `internal/provider/monarch/queries_test.go`
- Create: `internal/provider/monarch/decode.go`
- Create: `internal/provider/monarch/decode_test.go`

**Interfaces:**

- Consumes: Task 1 provider values and existing `domain.ParseMoney`.
- Produces: `monarch.Client`, injected `monarch.Options`, typed GraphQL responses, and exact wire
  normalization without a shared GraphQL package.

- [ ] **Step 1: Add failing transport, query-surface, and decoder tests.** Cover HTTPS-only
      production endpoints, loopback test overrides, bounded bodies, context cancellation,
      authorization stripping on cross-origin read redirects, missing/null GraphQL data,
      non-success status classification before body decoding, bounded `Retry-After`, GraphQL
      errors, malformed money, and query documents that omit unused attachments, rules, and drawer
      fields.

  ```go
  func TestDecodeAmountNeverUsesFloat(t *testing.T) {
      got, err := decodeMoney(json.RawMessage(`"-12.34"`), "USD", 2)
      require.NoError(t, err)
      assert.Equal(t, int64(-1234), got.Minor)
  }
  ```

- [ ] **Step 2: Run the tests and verify RED.**

  ```bash
  go test ./internal/provider/monarch -run 'Test(GraphQL|Query|Decode|Amount)' -count=1
  ```

  Expected: FAIL because the Monarch package does not exist.

- [ ] **Step 3: Implement the bounded private transport.**

  ```go
  type Options struct {
      HTTPClient *http.Client
      LoginURL *url.URL
      GraphQLURL *url.URL
      Now func() time.Time
      Sleep func(context.Context, time.Duration) error
      Random io.Reader
      MaxBodyBytes int64
  }

  type Client struct {
      options Options
      authorization string
      deviceUUID string
  }
  ```

  Add only `GetSubscriptionDetails`, accounts, merchants, category groups, categories, and
  paginated transaction documents. Decode through typed structs and `domain.ParseMoney`; never
  decode a monetary value into a floating-point field. Task 3 constructs this client from a
  validated provider-owned `Session`; Task 2 does not depend on that later session type.

- [ ] **Step 4: Run package and money-boundary tests and verify GREEN.**

  ```bash
  go test ./internal/provider/monarch ./internal/domain -count=1
  ```

  Expected: PASS.

- [ ] **Step 5: Run the required commit gate and commit.**

  ```bash
  git add internal/provider/monarch
  git commit -m "feat: add Monarch read client"
  ```

### Task 3: Add Hardened Monarch Sessions and Authentication

**Files:**

- Create: `internal/home/private_file.go`
- Create: `internal/home/private_file_test.go`
- Create: `internal/home/private_file_unix_test.go`
- Create: `internal/home/private_file_windows_test.go`
- Create: `internal/provider/monarch/session.go`
- Create: `internal/provider/monarch/session_file.go`
- Create: `internal/provider/monarch/session_file_test.go`
- Create: `internal/provider/monarch/auth.go`
- Create: `internal/provider/monarch/auth_test.go`
- Modify: `internal/home/root.go`
- Modify: `internal/home/root_test.go`
- Create: `internal/provider/monarch/credentials.go`
- Create: `internal/provider/monarch/credentials_test.go`
- Create: `internal/provider/monarch/totp.go`
- Create: `internal/provider/monarch/totp_test.go`

**Interfaces:**

- Consumes: Task 2 client, `home.Paths`, and injected clock/random/transport.
- Produces: `monarch.Authenticator`, `monarch.SessionStore`, `monarch.Source`, and reusable private
  atomic-file helpers.

- [ ] **Step 1: Add failing filesystem and authentication tests.** Cover private pre-creation,
      trusted-root and intermediate-symlink refusal, existing-file permission tightening,
      non-regular refusal, bounded reads, atomic replacement, REST-first success, GraphQL fallback,
      generated TOTP on both login paths, credential-redirect refusal, invalid session, one reload
      after replacement, serialized session JSON that cannot contain credential fields, and a
      password-encrypted vault that rejects wrong passwords and tampering without revealing which
      occurred.

  ```go
  type Session struct {
      Version uint16 `json:"version"`
      Token string `json:"token"`
      DeviceUUID string `json:"device_uuid"`
      RemoteProfileID string `json:"remote_profile_id"`
      IssuedAt time.Time `json:"issued_at"`
      ValidatedAt time.Time `json:"validated_at"`
  }
  ```

- [ ] **Step 2: Run the tests and verify RED.**

  ```bash
  go test ./internal/home ./internal/provider/monarch -run 'Test(Private|Session|Auth|MFA)' -count=1
  ```

  Expected: FAIL because hardened session storage is absent.

- [ ] **Step 3: Implement session persistence and authentication.** Add
      `<moneyflow-home>/providers/monarch/session.json`, owner-only parent creation, atomic replace,
      validated root anchoring, existing-file permission enforcement, format validation, session
      fingerprinting, `Source.Reader(ctx, forceReload)`, and `Source.Changed`. Keep REST login first
      and invoke GraphQL login only for the proven fallback response. Credential-bearing requests
      refuse redirects. Add a separate versioned Argon2id and AES-256-GCM credential vault plus
      RFC 6238 TOTP generation. Never persist the account password, generated one-time codes, or
      plaintext credential input.

- [ ] **Step 4: Run focused tests and verify GREEN on the current platform.** Portable build
      coverage remains in the repository's Linux/macOS/Windows CI jobs; do not try to execute a
      cross-compiled test binary locally.

  ```bash
  go test ./internal/home ./internal/provider/monarch -count=1
  ```

  Expected: native tests pass.

- [ ] **Step 5: Run the required commit gate and commit.**

  ```bash
  git add internal/home internal/provider/monarch
  git commit -m "feat: protect Monarch sessions"
  ```

### Task 4: Install Schema Version 3 and Provider Operational State

**Files:**

- Modify: `internal/store/store.go`
- Modify: `internal/store/store_test.go`
- Modify: `internal/store/sqlite/initialize.go`
- Modify: `internal/store/sqlite/initialize_test.go`
- Modify: `internal/store/sqlite/schema/profile.sql`
- Modify: `internal/store/sqlite/schema_test.go`
- Modify: `internal/store/sqlite/open.go`
- Create: `internal/store/sqlite/provider_state.go`
- Create: `internal/store/sqlite/provider_state_test.go`

**Interfaces:**

- Consumes: existing `store.Profile`, schema installer, and SQLite immediate transactions.
- Produces: schema version 3, binding/refresh/lease/label-allocation values, and operational store
  methods whose writes do not advance semantic revisions.

- [ ] **Step 1: Add failing schema and operational-state tests.** Require exact version 3 install,
      version 2 rejection as `schema_incompatible`, strict tables and constraints, pristine-state
      detection including journal-only refusal, lease expiry/recovery, and unchanged profile
      revision/generation after acquire/renew/release/status writes.

  ```go
  type ProviderBinding struct { Kind, Namespace, RemoteProfileID string; BoundAt time.Time }
  type RefreshState struct {
      Generation uint64
      LastSuccess time.Time
      StatusCode string
      ImportedTransactions int
      RemovedTransactions int
  }
  type RefreshLease struct {
      OwnerID string
      Renderer string
      ExpiresAt time.Time
  }
  type LabelAllocation struct {
      Kind domain.EntityKind
      Namespace string
      ExternalID string
      BaseCollisionKey string
      DisplayLabel string
      SuffixToken string
      Unsuffixed bool
  }
  ```

- [ ] **Step 2: Run store tests and verify RED.**

  ```bash
  go test ./internal/store/sqlite -run 'Test(Provider|Lease|Schema|Pristine)' -count=1
  ```

  Expected: FAIL because schema version 3 and provider tables are missing.

- [ ] **Step 3: Add exact schema and store contracts.** Add singleton `provider_binding`,
      `provider_refresh_state`, `provider_refresh_lease`, and strict `provider_label_allocations`.
      Extend `store.Profile` with `ProviderState`, `AcquireRefreshLease`, `RenewRefreshLease`,
      `ReleaseRefreshLease`, and `RecordRefreshFailure`. Operational methods update no profile
      revision or refresh generation. Unsuffixed label ownership is unique per entity type and
      collision key. Lease renewal is monotonic, and failure status carries and compares the
      current opaque owner ID so expired work cannot overwrite a successor.

- [ ] **Step 4: Run schema and reopen tests and verify GREEN.**

  ```bash
  go test ./internal/store ./internal/store/sqlite -count=1
  ```

  Expected: PASS, including exact rejection of an existing version 2 test database.

- [ ] **Step 5: Run the required commit gate and commit.**

  ```bash
  git add internal/store
  git commit -m "feat: store provider refresh state"
  ```

### Task 5: Fetch and Validate a Complete Monarch Snapshot

**Files:**

- Create: `internal/provider/monarch/snapshot.go`
- Create: `internal/provider/monarch/snapshot_test.go`
- Create: `internal/provider/monarch/pagination.go`
- Create: `internal/provider/monarch/pagination_test.go`
- Modify: `internal/provider/monarch/client.go`
- Modify: `internal/provider/monarch/client_test.go`

**Interfaces:**

- Consumes: Task 1 `provider.Reader`, Task 2 queries/decoder, and Task 3 sessions.
- Produces: a complete `provider.Reader` implementation with integrity-before-pending-exclusion.

- [ ] **Step 1: Add failing exhaustive-snapshot table tests.** Cover two matching complete reads,
      same-cardinality churn, authoritative entity-list changes, constant `totalCount`, unique
      count equality, visible/hidden disjointness, final count rechecks, offset insert/delete races,
      cross-partition hidden flips, duplicate entity IDs, missing related entities, pending-row
      integrity, cancellation, page progress, and exactly three complete attempts.

  ```go
  func TestHiddenFlipBetweenPartitionsRestartsWholeAttempt(t *testing.T) {
      server := newScriptedGraphQLServer(t, hiddenFlipScenario())
      reader := newTestReader(t, server)
      _, err := reader.FetchSnapshot(context.Background(), nil)
      require.NoError(t, err)
      assert.Equal(t, 2, server.CompleteAttempts())
  }
  ```

- [ ] **Step 2: Run the tests and verify RED.**

  ```bash
  go test ./internal/provider/monarch -run 'Test(Snapshot|Pagination|Partition|Pending)' -count=1
  ```

  Expected: FAIL because the complete reader is absent.

- [ ] **Step 3: Implement the two-partition reader.** Fetch accounts, merchants, groups,
      categories, `hideFromReports: false`, and `hideFromReports: true` twice with no
      date/search/category filter. Require canonical identity and imported-field equality between
      both complete reads, verify each page and all final counts, validate entity relationships,
      then remove pending rows. Treat relationship races as snapshot instability and invalid
      scalars as data invalid. Use cancellable bounded backoff and no SQLite access.

- [ ] **Step 4: Run focused provider tests and verify GREEN.**

  ```bash
  go test ./internal/provider/monarch -count=1
  ```

  Expected: PASS.

- [ ] **Step 5: Run the required commit gate and commit.**

  ```bash
  git add internal/provider/monarch
  git commit -m "feat: validate Monarch snapshots"
  ```

### Task 6: Plan Stable Identities and Sticky Display Labels

**Files:**

- Create: `internal/app/provider_identity.go`
- Create: `internal/app/provider_identity_test.go`
- Create: `internal/app/provider_labels.go`
- Create: `internal/app/provider_labels_test.go`
- Modify: `internal/domain/provider.go`
- Modify: `internal/domain/provider_test.go`

**Interfaces:**

- Consumes: `domain.ImportSnapshot`, current committed/effective entities, external identities,
  stored label allocations, and proposed local IDs/suffix material.
- Produces: deterministic `app.IdentityPlan` with a complete provider-owned committed candidate,
  retained tombstones/mappings/drills, and updated sticky allocations.

- [ ] **Step 1: Add failing identity property and collision tests.** Cover same-provider-ID reuse,
      entity-kind namespaces, first-observed unsuffixed ownership, sorted simultaneous colliders,
      later colliders, suffix extension, provider rename collision, user-effective-label priority,
      pending label override, entity disappearance retirement, and reappearance with the old ID.

  ```go
  type IdentityPlanningInput struct {
      Import domain.ImportSnapshot
      Committed domain.CommittedProfile
      Effective domain.CommittedProfile
      Allocations []store.LabelAllocation
      ProposedIDs map[string]domain.EntityID
      ProposedSuffixes map[string]string
  }
  ```

- [ ] **Step 2: Run tests and verify RED.**

  ```bash
  go test ./internal/app -run 'TestProvider(Identity|Label|Collision|Rename)' -count=1
  ```

  Expected: FAIL because identity planning is absent.

- [ ] **Step 3: Implement pure identity planning.** Use namespaces such as
      `monarch/transaction`; retain mappings and allocations forever; retire absent verified
      identities; compute collision keys with existing domain normalization; and consume only
      proposed IDs/tokens supplied in input. Sort every returned collection canonically.

- [ ] **Step 4: Run focused and randomized tests and verify GREEN.**

  ```bash
  go test ./internal/app -run 'TestProvider(Identity|Label|Collision|Rename)' -count=20
  ```

  Expected: PASS with deterministic output across repetitions.

- [ ] **Step 5: Run the required commit gate and commit.**

  ```bash
  git add internal/app/provider_identity.go internal/app/provider_identity_test.go internal/app/provider_labels.go internal/app/provider_labels_test.go internal/domain/provider.go internal/domain/provider_test.go
  git commit -m "feat: preserve provider identities"
  ```

### Task 7: Rebase the Pending Journal Over Refreshed Data

**Files:**

- Create: `internal/app/provider_rebase.go`
- Create: `internal/app/provider_rebase_test.go`
- Create: `internal/app/provider_rebase_property_test.go`
- Modify: `internal/store/sqlite/journal.go`
- Modify: `internal/store/sqlite/journal_test.go`
- Modify: `internal/store/errors.go`
- Modify: `internal/store/errors_test.go`

**Interfaces:**

- Consumes: old committed state, refreshed committed state, ordered journal, and active-count
  cursor.
- Produces: `app.RebaseResult` with rewritten operations, cursor, counts-only summary, and ephemeral
  operation details.

- [ ] **Step 1: Add failing example and property tests.** Cover redo-tail discard, vanished exact
      targets, partial batch identity/order preservation, cursor decrement, sequential
      journal-created entities, missing source/destination removal, structural current-membership
      sweep, hide-intent preservation, and at most one active hide toggle per transaction.

  ```go
  type RebaseResult struct {
      Journal []domain.Operation
      Cursor int
      Summary RebaseSummary
      Details []RebaseDetail
  }

  type RebaseSummary struct {
      RemovedOperations int
      RemovedTargets int
      RetainedOperations int
      RebasedHideTargets int
      DiscardedRedoOperations int
  }

  type RebaseDetail struct {
      OperationID string
      OperationType domain.OperationType
      RemovedTargets int
      Removed bool
  }

  func RebaseProviderJournal(
      oldBase domain.CommittedProfile,
      newBase domain.CommittedProfile,
      journal []domain.Operation,
      cursor int,
  ) (RebaseResult, error)
  ```

- [ ] **Step 2: Run tests and verify RED.**

  ```bash
  go test ./internal/app ./internal/store/sqlite -run 'Test(Rebase|JournalLimit)' -count=1
  ```

  Expected: FAIL because rebase and journal ceilings are absent.

- [ ] **Step 3: Implement the third runtime rewrite and ceilings.** Process active operations in
      sequence order, never re-evaluate a transaction predicate, apply typed structural sweeps,
      preserve operation IDs, and return counts separately from ephemeral details. Make `Append`
      reject a post-truncation result above 10,000 operations or 1,000,000 targets as
      `journal_full` without changing state.

- [ ] **Step 4: Prove replay equivalence and verify GREEN.**

  ```bash
  go test ./internal/app -run 'TestRebase' -count=50
  go test ./internal/store/sqlite -run 'TestJournalLimit' -count=1
  ```

  Expected: PASS; replaying rewritten operations over the new base equals the intended effective
  state for every generated sequence.

- [ ] **Step 5: Run the required commit gate and commit.**

  ```bash
  git add internal/app/provider_rebase.go internal/app/provider_rebase_test.go internal/app/provider_rebase_property_test.go internal/store
  git commit -m "feat: rebase pending provider edits"
  ```

### Task 8: Fold Refresh Plans Atomically with Generation CAS

**Files:**

- Modify: `internal/store/store.go`
- Modify: `internal/store/store_test.go`
- Create: `internal/store/sqlite/provider_refresh.go`
- Create: `internal/store/sqlite/provider_refresh_test.go`
- Create: `internal/store/sqlite/provider_refresh_failure_test.go`
- Create: `internal/store/sqlite/provider_refresh_property_test.go`
- Modify: `internal/store/sqlite/load.go`
- Modify: `internal/store/sqlite/fold.go`

**Interfaces:**

- Consumes: Task 4 state, Task 6 identity plan inputs, Task 7 rebase, and a validated import
  candidate.
- Produces: `store.ApplyProviderRefresh`, the pure callback boundary, atomic fold result, and
  canonical persisted-state encoding for reference/optimized comparison.

- [ ] **Step 1: Add failing atomicity, CAS, purity-shape, and equivalence tests.** Race two store
      handles from one generation; inject failure at every write; mutate the journal during a slow
      fetch; verify lease state cannot bypass CAS; compare reopened state with the effective plan;
      and prove refresh remains available at `journal_full`.

  ```go
  type RefreshInputs struct {
      Snapshot domain.ProfileSnapshot
      Binding *ProviderBinding
      Refresh RefreshState
      Allocations []LabelAllocation
      Candidate domain.ImportSnapshot
      ProposedIDs map[string]domain.EntityID
      ProposedSuffixes map[string]string
      ObservedAt time.Time
  }

  type RefreshPlan struct {
      Committed domain.CommittedProfile
      Effective domain.CommittedProfile
      Journal []domain.Operation
      Cursor int
      KnownDrills []domain.DrillIdentity
      Allocations []LabelAllocation
      Summary RefreshSummary
  }

  type RefreshSummary struct {
      ImportedAccounts int
      ImportedMerchants int
      ImportedGroups int
      ImportedCategories int
      ImportedTransactions int
      RemovedTransactions int
      RemovedOperations int
      RemovedTargets int
      RetainedOperations int
      RebasedHideTargets int
      DiscardedRedoOperations int
  }

  type RefreshPlanner func(RefreshInputs) (RefreshPlan, error)

  type AtomicRefreshRequest struct {
      ExpectedGeneration uint64
      LeaseOwnerID string
      Binding *ProviderBinding
      Candidate domain.ImportSnapshot
      ProposedIDs map[string]domain.EntityID
      ProposedSuffixes map[string]string
      ObservedAt time.Time
  }

  type RefreshCommit struct { Revision, Generation uint64; Summary RefreshSummary }

  type RefreshApplier interface {
      ApplyProviderRefresh(
          context.Context,
          AtomicRefreshRequest,
          RefreshPlanner,
      ) (RefreshCommit, error)
  }
  ```

  Add the `RefreshApplier` method to the existing `store.Profile` interface; the named narrow
  interface exists for focused application and store tests, not as a second persistence boundary.

- [ ] **Step 2: Run tests and verify RED.**

  ```bash
  go test ./internal/store/sqlite -run 'TestProviderRefresh' -count=1
  ```

  Expected: FAIL because atomic provider fold is missing.

- [ ] **Step 3: Implement the reference transaction.** `ApplyProviderRefresh` begins immediate,
      compares expected generation, loads all inputs, invokes the callback once, validates full
      replay, proves `Effective` is the replay of `Committed` plus the rewritten journal, writes
      the refreshed `Committed` base separately from `Effective`, writes mappings, allocations,
      journal, drills, and summary, advances revision and generation once, conditionally releases
      the caller-owned lease, and commits. The callback receives no SQL handle.

- [ ] **Step 4: Add canonical logical encoding and optional precompute path.** Sort and encode
      logical rows/payloads, excluding physical SQLite/WAL layout. If precomputation is enabled,
      compare both revision and generation under the write lock and discard mismatched plans.

- [ ] **Step 5: Run failure/property/concurrency tests and verify GREEN.**

  ```bash
  go test ./internal/store/sqlite -run 'TestProviderRefresh' -count=20
  go test -race ./internal/store/sqlite -run 'TestProviderRefreshConcurrent' -count=1
  ```

  Expected: PASS with exactly one same-generation fold and unchanged prior state after failures.

- [ ] **Step 6: Run the required commit gate and commit.**

  ```bash
  git add internal/store
  git commit -m "feat: fold provider refresh atomically"
  ```

### Task 9: Coordinate Refresh, Confirmation, Capabilities, and Scheduling

**Files:**

- Create: `internal/app/provider_refresh.go`
- Create: `internal/app/provider_refresh_test.go`
- Create: `internal/app/provider_confirmation_test.go`
- Create: `internal/app/provider_scheduler.go`
- Create: `internal/app/provider_scheduler_test.go`
- Modify: `internal/app/profile_service.go`
- Modify: `internal/app/profile_service_test.go`
- Modify: `internal/app/actions.go`
- Modify: `internal/app/actions_test.go`
- Modify: `internal/app/capabilities.go`
- Modify: `internal/app/capabilities_test.go`
- Modify: `internal/app/errors.go`
- Modify: `internal/app/errors_test.go`

**Interfaces:**

- Consumes: provider source/reader, store lease/fold, identity planning, journal rebase, clock,
  sleeper, randomness, renderer class, and opaque process ID.
- Produces: `Service.RefreshProvider`, `Service.ConfirmProviderRefresh`, `Service.ProviderStatus`,
  `ActionRefreshProvider`, deletion guard, confirmation cache, and scheduler policy.

- [ ] **Step 1: Add failing orchestration tests.** Cover all four deletion thresholds and exact
      boundaries; token success/expiry/wrong-process/lost-candidate/generation invalidation;
      integrity-cannot-confirm; per-refresh identity mismatch; no transaction during network;
      lease release on confirmation; counts-only status; reconnect file healing; and all-or-nothing
      selection clearing.

  Implement percentage comparisons with integer cross-multiplication. Confirmation is required
  when `existing > 0 && remaining == 0`, `removed >= 25 && removed*100 >= existing*10`,
  `removed >= 1000`, or `removed >= 5 && removed*100 >= existing*50`.

  ```go
  type ProviderRefreshRequest struct {
      Manual bool
      ConfirmationToken string
      State ViewState
      Selection SelectionValue
      Window WindowRequest
  }

  type ProviderRefreshResult struct {
      Revision uint64
      Generation uint64
      Status ProviderStatus
      Selection SelectionValue
      SelectionDisposition SelectionDisposition
      Projection WebProjection
      RebaseDetails []RebaseDetail
  }

  type ProviderStatus struct {
      Code provider.ErrorCode
      Generation uint64
      LastSuccess time.Time
      NextEligible time.Time
      OwnerRenderer string
      OwnerInstanceID string
      Fetched, Total int
      ConfirmationToken string
      Summary store.RefreshSummary
  }

  func (service *Service) RefreshProvider(
      context.Context,
      ProviderRefreshRequest,
  ) (ProviderRefreshResult, error)

  func (service *Service) ConfirmProviderRefresh(
      context.Context,
      ProviderRefreshRequest,
  ) (ProviderRefreshResult, error)
  ```

- [ ] **Step 2: Add the exhaustive scheduler table test.** Put snapshot-unstable, rate-limited,
      unavailable, and refresh-in-progress-after-lease-expiry in bounded retry policy. Put
      reconnect-required, identity-mismatch, deletion-confirmation-required,
      confirmation-invalid, refresh-stale, data-invalid, and all store/schema failures in
      manual-or-external-action-required. The latter set includes `store_busy`, `store_error`,
      `schema_incompatible`, `schema_newer`, and `store_corrupt`. Assert every provider code and
      applicable existing store code appears exactly once. Verify bounded `Retry-After`,
      next-eligible recording, lease yielding, six-hour staleness, and no tick spin. A stale
      candidate is not retried because its rejected generation proves another fold already made
      the profile fresher.

- [ ] **Step 3: Run tests and verify RED.**

  ```bash
  go test ./internal/app -run 'TestProvider(Refresh|Deletion|Confirmation|Scheduler|Selection)' -count=1
  ```

  Expected: FAIL because refresh orchestration is absent.

- [ ] **Step 4: Implement orchestration and capability policy.** Probe identity every refresh,
      fetch outside SQLite, evaluate plausibility against generation, acquire/release the lease,
      renew the lease periodically throughout network work, call the atomic fold, reload the
      service cache, and project the caller's exact state. Every renew, release, status write, and
      successful fold compares the current opaque owner; an expired former owner cannot affect a
      successor. Add `provider.refresh` with key `r`. Keep edit/review actions available, but
      reject provider commit with the durable-intent explanation.

- [ ] **Step 5: Implement in-memory confirmation and parked reconnect state.** Bind candidate
      tokens to owner, expiry, generation, and exact candidate. On reconnect-required, inspect only
      session fingerprint changes during status ticks; reload once after replacement.

- [ ] **Step 6: Run focused and race tests and verify GREEN.**

  ```bash
  go test ./internal/app -run 'TestProvider' -count=20
  go test -race ./internal/app -run 'TestProviderRefreshConcurrent' -count=1
  ```

  Expected: PASS.

- [ ] **Step 7: Run the required commit gate and commit.**

  ```bash
  git add internal/app
  git commit -m "feat: coordinate provider refresh"
  ```

### Task 10: Add CLI Connect and Disconnect Lifecycle

**Files:**

- Create: `cmd/moneyflow/provider.go`
- Create: `cmd/moneyflow/provider_test.go`
- Modify: `cmd/moneyflow/root.go`
- Modify: `cmd/moneyflow/root_test.go`
- Modify: `cmd/moneyflow/profile.go`
- Modify: `cmd/moneyflow/profile_test.go`
- Modify: `cmd/moneyflow/main.go`

**Interfaces:**

- Consumes: Monarch authenticator/session store/source, application binding refresh, default
  profile opener, and injected CLI streams.
- Produces: `moneyflow provider connect monarch` and `moneyflow provider disconnect monarch`.

- [ ] **Step 1: Add failing Cobra lifecycle tests.** Cover missing-profile current-schema creation,
      pristine bind, journal-only refusal, populated refusal containing the exact profile path,
      absence of `--replace`, password-protected credential setup, masked input, automatic TOTP,
      explicit login/import progress, valid retained-session import retry without prompts,
      saved-settings reconnect with one vault-password prompt, same-household reconnect,
      different-household refusal, initial-import rollback with retained session and credentials,
      and disconnect preserving SQLite state.

  ```go
  func TestProviderConnectHasNoReplaceFlag(t *testing.T) {
      command := newRootCommand(testStreams(t))
      command.SetArgs([]string{"provider", "connect", "monarch", "--replace"})
      err := command.Execute()
      require.ErrorContains(t, err, "unknown flag: --replace")
  }
  ```

- [ ] **Step 2: Run tests and verify RED.**

  ```bash
  go test ./cmd/moneyflow -run 'TestProvider(Connect|Disconnect)' -count=1
  ```

  Expected: FAIL because provider commands are absent.

- [ ] **Step 3: Implement injected command orchestration.** Add prompt seams to `IOStreams`, never
      echo secrets, authenticate and validate before atomic session installation, call initial
      refresh/bind, and print only safe status. Disconnect removes only the session file.

- [ ] **Step 4: Run command and reopen tests and verify GREEN.**

  ```bash
  go test ./cmd/moneyflow ./internal/provider/monarch -run 'TestProvider|TestSession' -count=1
  ```

  Expected: PASS.

- [ ] **Step 5: Run the required commit gate and commit.**

  ```bash
  git add cmd/moneyflow
  git commit -m "feat: connect Monarch profiles"
  ```

- [ ] **Step 6: Run the required post-Task-10 review checkpoint.** Invoke `$roborev-fix` against
      the Task 1-10 implementation. Apply valid findings with focused red/green tests, commit the
      resulting fixes without amending, rerun the required commit gate, and require a clean review
      before Task 11.

### Task 11: Add Bubble Tea Refresh and Standing Cadence

**Files:**

- Create: `internal/tui/provider.go`
- Create: `internal/tui/provider_test.go`
- Modify: `internal/tui/model.go`
- Modify: `internal/tui/model_test.go`
- Modify: `internal/tui/update.go`
- Modify: `internal/tui/update_test.go`
- Modify: `internal/tui/help.go`
- Modify: `internal/tui/help_test.go`
- Modify: `internal/tui/manage_categories_test.go`
- Modify: `internal/tui/semantic_parity_test.go`
- Modify: `internal/tui/visual_golden_test.go`
- Modify: `testdata/parity/go/**`

**Interfaces:**

- Consumes: Task 9 service methods/action/capabilities/status and Bubble Tea commands/messages.
- Produces: cancellable manual `r`, six-hour TUI scheduler, progress/status UI, and deliberate
  parity artifacts for the new top-level key.

- [ ] **Step 1: Add failing TUI interaction tests.** Cover unbound disabled reason, global `r`,
      category-overlay `r` isolation, async progress, escape cancellation before fold, six-hour
      tick, reconnect healing, selection all-clear, cursor restoration, retired-empty drill,
      confirmation ownership text, and `w` review with disabled commit.

- [ ] **Step 2: Run tests and verify RED.**

  ```bash
  go test ./internal/tui -run 'TestProvider|TestRefreshKey' -count=1
  ```

  Expected: FAIL because provider messages and routing are absent.

- [ ] **Step 3: Implement Bubble Tea commands and routing.** Start provider work in `tea.Cmd`,
      translate progress into bounded messages, keep SQLite/network off the update loop, preserve
      analytical state, and schedule only while the model lives. Overlay routing consumes local
      `r` before global action lookup.

- [ ] **Step 4: Deliberately update reviewed Go parity artifacts.** The Python oracle has no
      top-level `r`; do not update Python semantic artifacts or shared scenarios for this named
      divergence.

  ```bash
  make parity-update-go
  git diff -- internal/tui/visual_golden_test.go testdata/parity/go
  ```

  Expected: only reviewed help/key/status scenarios change; no frame updates occur through ordinary
  tests.

- [ ] **Step 5: Run TUI and parity tests and verify GREEN.**

  ```bash
  go test ./internal/tui -count=1
  make parity
  ```

  Expected: PASS.

- [ ] **Step 6: Run the required commit gate and commit.**

  ```bash
  git add internal/tui testdata/parity
  git commit -m "feat: refresh Monarch from the TUI"
  ```

### Task 12: Add Protected Web Refresh and Confirmation Experience

**Files:**

- Create: `internal/api/provider.go`
- Create: `internal/api/provider_test.go`
- Modify: `internal/api/server.go`
- Modify: `internal/api/server_test.go`
- Modify: `internal/api/types.go`
- Modify: `internal/api/types_test.go`
- Modify: `internal/api/errors.go`
- Modify: `internal/api/errors_test.go`
- Modify: `api/openapi.yaml`
- Create: `web/src/lib/controller/provider.ts`
- Create: `web/src/lib/controller/provider.test.ts`
- Create: `web/src/components/ProviderStatus.svelte`
- Create: `web/src/components/ProviderStatus.test.ts`
- Modify: `web/src/App.svelte`
- Modify: `web/src/App.test.ts`
- Modify: `web/src/lib/shortcuts.ts`
- Modify: `web/src/lib/shortcuts.test.ts`
- Modify: `web/src/lib/api/schema.d.ts`
- Create: `web/tests/provider.spec.ts`

**Interfaces:**

- Consumes: Task 9 renderer-neutral refresh/status results and existing mutation security.
- Produces: protected refresh/confirmation endpoints, visible-only status polling, keyboard `r`,
  and accessible deletion/reconnect/progress UI.

- [ ] **Step 1: Add failing API tests.** Define and test:

  ```text
  GET  /api/v1/provider/status
  POST /api/v1/provider/refresh
  POST /api/v1/provider/refresh/confirm
  ```

  Require base-path composition, mutation-token/origin/Fetch-Metadata protection, counts-only
  payloads, opaque confirmation tokens, stable error mapping, no cookies, and no automatic replay
  after generation, identity, storage, or confirmation failure.

- [ ] **Step 2: Run API tests and verify RED.**

  ```bash
  go test ./internal/api -run 'TestProvider' -count=1
  ```

  Expected: FAIL because endpoints are absent.

- [ ] **Step 3: Implement the Huma adapters and regenerate contracts deliberately.** Protect both
      POST routes in `persistentMutationSecurity`; keep GET read-only; expose revision/generation,
      safe status, counts, renderer owner, and selection disposition. Then run:

  ```bash
  make web-generate
  git diff -- api/openapi.yaml web/src/lib/api/schema.d.ts
  ```

  Expected: only provider endpoints/types and stable provider errors are added.

- [ ] **Step 4: Add failing Svelte unit and browser tests.** Cover shortcut `r`, unbound disabled
      reason, visible-only polling, progress announcements, reconnect park/heal, confirmation
      ownership, refetch-after-invalid-token, exact selection clearing, base path, keyboard-only
      flow, and no provider data in browser history.

- [ ] **Step 5: Implement the browser controller and kit-ui presentation.** Keep the controller
      server-authoritative; retry only token expiry once; never replay stale/identity/storage
      failures; preserve URL/cursor/scroll; and use an accessible dialog/live region for deletion
      confirmation and status.

- [ ] **Step 6: Build and embed assets deliberately, then verify GREEN.**

  ```bash
  make web-test
  make web-build
  make web-embed
  make web-e2e
  ```

  Expected: all frontend and browser tests pass and embedded assets match the built distribution.

- [ ] **Step 7: Run both required commit gates and commit.**

  ```bash
  git add internal/api api/openapi.yaml web internal/web/dist
  git commit -m "feat: refresh Monarch from the web"
  ```

### Task 13: Lock Integration, Performance, Privacy, and Live Characterization

**Files:**

- Create: `internal/app/provider_integration_test.go`
- Create: `internal/store/sqlite/provider_refresh_benchmark_test.go`
- Create: `internal/store/sqlite/provider_refresh_concurrency_test.go`
- Create: `internal/provider/monarch/live_test.go` with build tag `monarchlive`
- Modify: `cmd/moneyflow/provider_test.go`
- Modify: `web/tests/provider.spec.ts`
- Modify: `Makefile`
- Modify: `.github/workflows/go.yml`
- Modify: `.github/workflows/web.yml`
- Create: `docs/superpowers/benchmarks/2026-08-15-monarch-refresh.md`
- Modify: `README.md`
- Modify: `SECURITY.md`

**Interfaces:**

- Consumes: the complete Tasks 1-12 vertical slice.
- Produces: stable verification targets, 100k evidence, cross-process proof, explicit live harness,
  user documentation, and final negative-scope audit.

- [ ] **Step 1: Add process-level and concurrency journeys.** Cover connect/import/reopen/offline,
      simultaneous TUI/web store handles, edit during network fetch, one same-generation fold,
      expired lease recovery, confirmation release, session replacement healing, journal-full
      refresh, and crash/reopen after each durable boundary.

- [ ] **Step 2: Add the 100k performance smoke.** Generate synthetic provider records in memory,
      exclude network parsing, measure the complete write-locked reference path, and enforce one
      second in CI while recording the 250-millisecond reference-machine target.

  ```bash
  go test ./internal/store/sqlite -run '^TestProviderRefresh100KPerformance$' -count=1
  ```

  Expected: PASS on an idle supported host.

- [ ] **Step 3: Add explicit Make targets.** Add `test-provider`, `test-provider-e2e`, and
      `monarch-live-test`; include synthetic provider/store checks in `verify-go` and browser
      provider journeys in `verify-web`. `monarch-live-test` requires an explicit environment
      opt-in, runs `go test -tags=monarchlive`, and uses an isolated temporary moneyflow home. It
      emits counts only and is absent from CI.

- [ ] **Step 4: Implement live characterization assertions.** Verify only `subscription.id`
      stability, closed/hidden-account exhaustiveness, and pending-to-posted lifecycle. Do not
      serialize raw responses, labels, transactions, screenshots, or fixtures.

- [ ] **Step 5: Document the read-only boundary and recovery commands.** Explain CLI connect and
      disconnect, no-`--replace`, offline browsing, `r`, six-hour complete refresh, reconnect via
      CLI, pending-intent commit limitation, reverse-proxy use, session location/permissions, and
      version-2 profile recreation.

- [ ] **Step 6: Run the complete non-live verification matrix.**

  ```bash
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

  Expected: every command exits zero.

- [ ] **Step 7: Run the privacy and negative-scope audits.** Scan the complete branch diff,
      generated assets, test fixtures, logs captured by tests, and commit messages. Confirm no
      personal data, session material, provider payload, mutation query, writer interface,
      outbound queue, plaintext credential persistence, unattended vault unlock, multi-profile
      feature, export, or Python-state import entered the slice.

- [ ] **Step 8: Commit the final integration evidence and documentation.**

  ```bash
  git add Makefile .github/workflows/go.yml .github/workflows/web.yml internal cmd web README.md SECURITY.md docs/superpowers/benchmarks
  git commit -m "test: verify Monarch refresh slice"
  ```

- [ ] **Step 9: Run the required final implementation review.** Invoke `$roborev-fix` over the
      complete Task 1-13 implementation. Apply valid findings with focused red/green tests and
      separate commits, never amend. Rerun every gate affected by review fixes, then rerun the
      complete Step 6 matrix and privacy/negative-scope audit. Stop with a clean worktree and do not
      push.
