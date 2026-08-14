# Go Port SQLite Profiles and Editing Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development
> (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use
> checkbox (`- [ ]`) syntax for tracking.

**Status:** Ready for execution

**Goal:** Replace the fixture-backed Go runtime with one durable SQLite profile and deliver the
same staged merchant, category, group, hide, undo/redo, review, and local-commit workflows through
both the Bubble Tea TUI and Svelte web application.

**Architecture:** A pure-Go SQLite store owns committed entities, the versioned operation journal,
the active-count cursor, revision compare-and-swap, and atomic fold. The application service owns
target resolution, deterministic replay, capabilities, and renderer-neutral mutation results;
analytics continues to consume immutable ordinary Go slices. The TUI calls the service directly,
while Huma exposes bounded mutation/review endpoints protected by canonical-origin checks and a
one-hour server-instance token.

**Tech Stack:** Go 1.26.3; `modernc.org/sqlite` v1.56.0; `golang.org/x/text` v0.41.0; Huma
v2.38.0 with `humago`; Cobra 1.10.2; Bubble Tea 2.0.8; Svelte 5.56.3 in runes mode; Bun 1.3.14;
`@kenn-io/kit-ui` commit `16db58ef8122dd00e21ce8ad90ba295b9174c6ef`; Vitest 4.1.10;
Playwright 1.61.1.

## Global Constraints

- Work only on the already checked-out `go-port` branch. Do not switch branches, pull, merge,
  rebase, push, remove Python, or merge to `main` without explicit user permission.
- SQLite is the sole durable Go profile format. Normal and demo commands must not bypass it with
  fixture-backed services, Parquet, shadow stores, SQLCipher, or application-level encryption.
- Keep providers, credentials, synchronization, Python migration, export, backup/repair,
  multi-profile UI, authentication, transaction deletion/splitting/notes, and live push out of this
  slice.
- Preserve the no-CGO contract on Linux, macOS, and Windows. Use `modernc.org/sqlite`; do not add a
  platform-native SQLite driver.
- Store and compute money only as signed integer minor units plus currency and scale. No Go money
  field or SQLite money column may use `float32`, `float64`, or `REAL`.
- Use stable opaque local IDs for accounts, merchants, categories, groups, and transactions.
  Provider IDs remain external mappings and never become local primary keys.
- Effective state is committed state plus the first `cursor` operations in sequence order. Cursor
  is an active-operation count; sequence values are immutable and may have gaps.
- Resolve exact stable targets at operation creation. Replay must never evaluate selection,
  search, merchant-label, or aggregate predicates again.
- Every journal append, cursor move, runtime journal rewrite, and commit must compare and advance
  the profile revision in the same SQLite transaction. A cached revision check is only fast-fail.
- Runtime journal rewrites are limited to redo-tail truncation and Python-compatible hide
  cancellation. Pre-stability payload versions are never rewritten in place.
- Pending-only drill identities are known through effective state but do not enter the permanent
  registry. Seeding and successful commit folds are the only registry insertion points.
- SQLite uses `STRICT` tables, foreign keys, WAL, `synchronous=FULL`, bounded pools and mutation
  waits, plus a separate 60-second first-start deadline with schema rechecks. Until Go v2 is
  declared stable, install the current schema only into an empty database and reject every schema
  version mismatch without upgrading it.
- Tests and demos use explicit restrictive temporary profile roots. They must never open the
  default profile. Every demo run gets a fresh synthetic profile that is removed on clean exit.
- The web remains cookie-free, has no CORS, and has one mutable canonical origin. Tokens live only
  in memory/headers and expire after one hour; URLs and browser history contain analytical state
  only.
- The no-auth slice trusts every process able to reach its listener. Bind it to loopback and use
  tailnet or reverse-proxy policy for access control. Filesystem hardening prevents accidental
  exposure but does not defend a profile root beneath ancestors controlled by a malicious local
  user; mutually untrusted same-host users are outside this slice.
- `--external-url` path and normalized `--base-path` must match. Direct listener access remains
  readable but noncanonical-origin mutations fail with a canonical-link explanation.
- Automatic mutation retry is allowed once for `token_expired` only. Revision conflict,
  `selection_stale`, `store_busy`, and `store_error` require explicit reinvocation.
- Preserve keyboard-first parity. Add `U` redo; correct `C`/`G` help; keep `q` confirmation with
  durable-pending copy and immediate `Ctrl+C`; do not leak global keys through dialogs.
- Reuse kit-ui dialogs, drawers, live regions, shortcut management, and tokens. Do not move
  accounting, replay, target resolution, or mutation semantics into TypeScript.
- Logs use the specification's positive allowlist only: operation type, revisions, bounded counts,
  timings, generated correlation IDs, and stable error codes. Never log labels, search, IDs,
  payloads, SQL values, full URLs, tokens, credentials, or financial values.
- Ordinary checks never rewrite parity frames, OpenAPI, generated TypeScript, embedded assets, or
  screenshots. Use the explicit update/generation targets and review their complete diffs.
- Use `apply_patch` for hand edits. Use `kenn:commit`, `kenn:scrub-private-data`, and
  `superpowers:verification-before-completion` before every task commit. Never amend.

## Target File Map

```text
go.mod / go.sum                    pure-Go SQLite and Unicode normalization dependencies
Makefile                           store, browser, parity, and final verification targets
cmd/moneyflow/profile.go           default/demo profile opening and cleanup
cmd/moneyflow/root.go              TUI command wiring against SQLite
cmd/moneyflow/web.go               profile, canonical URL, and server lifecycle wiring
internal/home/                     v2 path resolution and cross-platform private permissions
internal/domain/entities.go        stable committed entities and transaction records
internal/domain/labels.go          display-label validation and deterministic collision keys
internal/domain/operations.go      typed versioned pending operations
internal/domain/profile.go         committed/effective snapshots and drill identities
internal/store/store.go            application-owned persistence interface and fold plan
internal/store/errors.go           stable renderer-neutral storage failures
internal/store/sqlite/             open/config, schema install, seed/load, journal, and commit
internal/replay/                   pure reference replay shared by application and fold validation
internal/app/replay.go             application-facing replay wrapper
internal/app/mutations.go          capability, exact target resolution, and operation construction
internal/app/profile_service.go    revision-aware cache and store coordination
internal/app/review.go             bounded operation summaries and target windows
internal/tui/edit_*.go             merchant/category input and selectors
internal/tui/manage_*.go           category/group management overlays
internal/tui/review.go             pending review and commit confirmation
internal/api/security.go           canonical origin and mutation-token verification
internal/api/mutations.go          Huma mutation/review wire adapters
internal/web/                      bootstrap-token injection and composed server
api/openapi.yaml                   deliberately regenerated mutation contract
web/src/lib/controller/editing.ts  token, revision, mutation, conflict, and selection flow
web/src/components/editing/        kit-ui editors, managers, review, and announcements
web/tests/                         keyboard, restart, proxy, conflict, and visual workflows
docs/superpowers/benchmarks/       store/replay/bulk operation evidence
.github/workflows/                 portable SQLite, race, frontend, and browser gates
```

## Required Commit Gate

Run each task's focused red/green command while developing. Before every task commit, run from the
repository root:

```bash
make verify-go
uv run pytest -v
uv run pyright moneyflow/
uv run ruff format --check moneyflow/ tests/
uv run ruff check moneyflow/ tests/
npx --yes markdownlint-cli@0.47.0 --config .markdownlint.json README.md 'docs/**/*.md'
.github/scripts/check-arrow-lists.sh
```

Expected: every command exits zero; Go test/vet/lint/parity and Python tests pass; Pyright reports
zero errors; documentation checks report no findings. If the performance smoke fails on a
contended host, stop competing work and rerun unchanged on an idle host; do not weaken its ceiling.

Starting with Task 14, also run:

```bash
bun install --cwd web --frozen-lockfile
make verify-web
```

Expected: the lockfile is unchanged; generated API and embedded assets are current; frontend type,
format, lint, unit, audit, build, asset, browser, accessibility, and visual checks pass.

---

### Task 1: Define Stable Profile Entities, Labels, Operations, and Editing Actions

**Files:**

- Create: `internal/domain/entities.go`
- Create: `internal/domain/entities_test.go`
- Create: `internal/domain/labels.go`
- Create: `internal/domain/labels_test.go`
- Create: `internal/domain/operations.go`
- Create: `internal/domain/operations_test.go`
- Create: `internal/domain/profile.go`
- Create: `internal/domain/profile_test.go`
- Modify: `internal/domain/transaction.go`
- Modify: `internal/domain/transaction_test.go`
- Modify: `internal/analytics/aggregate.go`
- Modify: `internal/analytics/aggregate_test.go`
- Modify: `internal/analytics/filter.go`
- Modify: `internal/analytics/filter_test.go`
- Modify: `internal/fixture/document.go`
- Modify: `internal/fixture/document_test.go`
- Modify: `internal/fixture/generate.go`
- Modify: `internal/fixture/generate_test.go`
- Modify: `internal/app/actions.go`
- Modify: `internal/app/actions_test.go`
- Modify: `internal/tui/keymap.go`
- Modify: `internal/tui/help_test.go`
- Modify: `go.mod`
- Modify: `go.sum`

**Interfaces:**

- Consumes: existing `domain.Transaction`, `domain.Money`, `domain.Date`, `app.ActionDefinition`,
  and stable IDs from the committed fixture.
- Produces: `domain.Account`, `domain.Merchant`, `domain.CategoryGroup`, `domain.Category`,
  `domain.TransactionRecord`, `domain.CommittedProfile`, `domain.ProfileSnapshot`,
  `domain.NewEntityID`, `domain.NewOperationID`, `domain.CollisionKey(string)`,
  `domain.Operation`, `domain.OperationType`, `domain.DrillIdentity`, and `app.ActionRedo`.

- [ ] **Step 1: Add failing entity, collision, and operation tests.** Require defensive copies,
      nonempty opaque IDs, valid entity references, protected Uncategorized sentinels, NFKC plus
      case-folded/trimmed/collapsed collision keys, control-character rejection, one typed payload
      per operation, exact target IDs, payload version 1, and stable sequence ordering. Use this
      public shape:

  ```go
  type EntityID string

  type Account struct { ID EntityID; Label, CollisionKey string; Retired bool }
  type Merchant struct {
      ID EntityID; Label, CollisionKey string; Retired bool; MergeDestination *EntityID
  }
  type CategoryGroup struct {
      ID EntityID; Label, CollisionKey string; Protected, Retired bool
      MergeDestination *EntityID
  }
  type Category struct {
      ID, GroupID EntityID; Label, CollisionKey string; Protected, Retired bool
      MergeDestination *EntityID
  }
  type TransactionRecord struct {
      ID EntityID; Provider, ProviderID string
      AccountID, MerchantID, CategoryID EntityID
      Date Date; Amount Money; Notes string; Hidden, Pending bool
  }
  type DrillIdentity struct {
      Dimension Dimension; Currency Currency; Scale uint8; Key string
  }
  type ExternalIdentity struct {
      EntityType string; EntityID EntityID; Namespace, ExternalID string
  }
  type CommittedProfile struct {
      Accounts []Account; Merchants []Merchant; Groups []CategoryGroup; Categories []Category
      Transactions []TransactionRecord; ExternalIdentities []ExternalIdentity
  }
  type ProfileSnapshot struct {
      Revision uint64; Cursor int; Committed CommittedProfile
      Journal []Operation; KnownDrills []DrillIdentity
  }
  ```

  Run: `go test ./internal/domain ./internal/app ./internal/tui -run
  'Test(Profile|Collision|Operation|EditingAction)' -count=1`

  Expected: FAIL because profile entities, operation types, collision normalization, and redo do
  not exist.

- [ ] **Step 2: Implement label normalization and committed profile validation.** Add
      `golang.org/x/text v0.41.0`. Implement `ValidateDisplayLabel`, `CollisionKey`, `Clone`,
      `Validate`, and `MaterializeTransactions`. Materialization must join labels/group membership
      into the existing analytics `domain.Transaction` without mutating entity tables. Reject
      duplicate active collision keys, retired transaction references, missing money partitions,
      reused IDs, and sentinel mutation.

  Implement `NewEntityID(kind EntityKind, random io.Reader)` and
  `NewOperationID(random io.Reader)` from 128 bits of `crypto/rand`, unpadded lowercase base32, and
  a fixed non-label kind prefix. Tests inject deterministic readers, prove 128-bit consumption and
  valid prefixes, and require creation to fail rather than reuse an ID on collision.

- [ ] **Step 3: Make category-group identity stable in existing analytics.** Add `GroupID` to
      `CategoryRef`; validate it in `NewTransaction`; aggregate group rows by `GroupID` with
      `Group` as the display label; and match group drills against `GroupID`. Update fixture
      adapters to derive a deterministic synthetic group ID from the first 128 bits of SHA-256 over
      the normalized group label, encoded as unpadded lowercase base32 with a `group-synthetic-`
      prefix. Keep the committed fixture schema unchanged. Update logical expectations so a group
      label change preserves the drill key; visible labels and totals must remain unchanged.

- [ ] **Step 4: Implement the typed version-1 operation union.** Define these distinct operation
      types and require their matching payload only:

  ```go
  const (
      OperationMerchantLabel       OperationType = "merchant.label"
      OperationMerchantMerge       OperationType = "merchant.merge"
      OperationMerchantReassign    OperationType = "merchant.reassign"
      OperationCategoryAssign      OperationType = "category.assign"
      OperationCategoryCreate      OperationType = "category.create"
      OperationCategoryLabel       OperationType = "category.label"
      OperationCategoryMove        OperationType = "category.move"
      OperationCategoryMerge       OperationType = "category.merge"
      OperationCategoryDelete      OperationType = "category.delete"
      OperationGroupCreate         OperationType = "group.create"
      OperationGroupLabel          OperationType = "group.label"
      OperationGroupMerge          OperationType = "group.merge"
      OperationGroupDelete         OperationType = "group.delete"
      OperationTransactionHide     OperationType = "transaction.hide-toggle"
  )

  type Operation struct {
      ID string; Sequence int64; Type OperationType; PayloadVersion uint16
      CreatedRevision uint64; CreatedAt time.Time
      Targets []EntityID
      Label *LabelPayload; Create *CreatePayload; Move *MovePayload
      Merge *MergePayload; Reassign *ReassignPayload; Delete *DeletePayload
      HideToggle *HideTogglePayload
  }
  type LabelPayload struct {
      EntityID EntityID; Label, CollisionKey string
  }
  type CreatePayload struct {
      EntityType string; EntityID EntityID; Label, CollisionKey string; ParentID EntityID
  }
  type MovePayload struct { EntityID, DestinationID EntityID }
  type MergePayload struct { SourceID, DestinationID EntityID }
  type ReassignPayload struct { DestinationID EntityID; CreatedMerchant *Merchant }
  type DeletePayload struct { SourceID, ReplacementID EntityID }
  type HideTogglePayload struct{}
  ```

  `Operation.ValidateDraft` permits sequence zero before append; `ValidateStored` requires a
  positive immutable sequence. Both reject zero/multiple payloads, unordered or duplicate targets,
  unknown versions, predicates, and incomplete forward values. `Clone` owns every slice and
  pointer.

- [ ] **Step 5: Extend the static action contract without enabling storage yet.** Add `U` redo;
      correct `C` to “create, rename, move, merge, delete” and `G` to “create, rename, merge,
      delete”; keep editing actions statically listed and unavailable until Task 10 supplies local
      capabilities. Update key/help tests and prove no existing key changed.

- [ ] **Step 6: Run focused and portability checks.** Run:

  ```bash
  go test ./internal/domain ./internal/app ./internal/tui -count=1
  CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go test ./internal/domain ./internal/app
  CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go test ./internal/domain ./internal/app
  make parity
  ```

  Expected: all pass; parity changes only the deliberately corrected help artifacts after the
  explicit update flow, not from ordinary `make parity`.

- [ ] **Step 7: Deliberately update and review corrected help artifacts.** Run
      `make parity-update-python` and `make parity-update-go`, inspect every changed semantic frame
      and Go preview, then rerun `make parity`. If another artifact changes, correct the generator
      or source input before staging; do not discard unrelated user work.

- [ ] **Step 8: Run the Required Commit Gate and commit.** Stage only Task 1 files and reviewed
      help artifacts. Commit with subject `feat: define durable editing domain`.

### Task 2: Add Private v2 Paths and the Store Contract

**Files:**

- Create: `internal/home/root.go`
- Create: `internal/home/root_test.go`
- Create: `internal/home/permissions_unix.go`
- Create: `internal/home/permissions_windows.go`
- Create: `internal/home/permissions_test.go`
- Create: `internal/store/store.go`
- Create: `internal/store/store_test.go`
- Create: `internal/store/errors.go`
- Create: `internal/store/errors_test.go`

**Interfaces:**

- Consumes: Task 1 profile and operation types.
- Produces: `home.ResolveRoot`, `home.PrepareDatabase`, `store.Profile`, `store.FoldPlan`,
  `store.Error`, and stable `store.ErrorCode` values used by every later task.

- [ ] **Step 1: Write failing path and permission tests.** Cover `$MONEYFLOW_HOME`, the
      `~/.moneyflow/v2/moneyflow.db` default, absolute canonical roots, missing suffixes beneath an
      existing ancestor, symlink escape rejection, Unix `0700`/`0600`, re-enforcement on existing
      managed paths, and injectable Windows DACL calls. The public path API is:

  ```go
  type Paths struct { Root, Database string }
  func ResolveRoot(explicit string, lookupEnv func(string) (string, bool), userHome string) (Paths, error)
  func PrepareDatabase(paths Paths) error
  ```

  Run: `go test ./internal/home -count=1`

  Expected: FAIL because the v2 resolver and protection functions do not exist.

- [ ] **Step 2: Implement cross-platform private path preparation.** Pre-create the database file
      before SQLite opens it. Reject roots whose resolved existing ancestor plus missing suffix can
      escape through a symlink. On Windows, isolate DACL behavior behind a small injected native
      adapter so ordinary tests do not alter host permissions.

- [ ] **Step 3: Write the failing persistence-contract tests.** Define a compile-time fake that
      must implement this interface and verify error details never include labels, IDs, SQL, or
      filesystem paths:

  ```go
  type Profile interface {
      CurrentRevision(context.Context) (uint64, error)
      Load(context.Context) (domain.ProfileSnapshot, error)
      CreateSeededProfile(context.Context, domain.CommittedProfile) (uint64, error)
      Append(context.Context, uint64, domain.Operation) (uint64, error)
      MoveCursor(context.Context, uint64, int) (uint64, error)
      CancelHide(context.Context, uint64, []domain.EntityID) (uint64, error)
      Fold(context.Context, uint64, FoldPlan) (uint64, error)
      Close() error
  }

  type FoldPlan struct {
      ReviewedRevision uint64
      ActiveOperationIDs []string
      Effective domain.CommittedProfile
      KnownDrills []domain.DrillIdentity
  }
  ```

  Stable codes are `revision_conflict`, `invalid_operation`, `invalid_target`, `store_busy`,
  `store_error`, `schema_newer`, `schema_incompatible`, and `store_corrupt`. `Error` carries safe
  detail, optional reliable observed/current revisions, and an unexported diagnostic cause.

- [ ] **Step 4: Implement defensive store DTOs and errors.** `FoldPlan.Validate` must require a
      reviewed revision equal to the method expectation, ordered unique active operation IDs, a
      valid effective profile, and canonical sorted known identities. Map no driver-specific type
      here; `internal/store/sqlite` will translate those in Task 3.

- [ ] **Step 5: Run focused and cross-platform checks.** Run:

  ```bash
  go test ./internal/home ./internal/store -count=1
  CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go test ./internal/home ./internal/store
  CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go test ./internal/home ./internal/store
  ```

  Expected: all pass without reading or creating the default profile.

- [ ] **Step 6: Run the Required Commit Gate and commit.** Commit with subject
      `feat: define private profile store boundary`.

### Task 3: Open and Install the Current Pure-Go SQLite Profile

**Files:**

- Create: `internal/store/sqlite/open.go`
- Create: `internal/store/sqlite/open_test.go`
- Create: `internal/store/sqlite/errors.go`
- Create: `internal/store/sqlite/errors_test.go`
- Create: `internal/store/sqlite/initialize.go`
- Create: `internal/store/sqlite/initialize_test.go`
- Create: `internal/store/sqlite/schema/profile.sql`
- Create: `internal/store/sqlite/schema_test.go`
- Modify: `go.mod`
- Modify: `go.sum`

**Interfaces:**

- Consumes: Task 2 paths, permissions, store interface, and errors.
- Produces: `sqlite.Open(context.Context, home.Paths, sqlite.Options) (store.Profile, error)`, a
  current schema version constant, atomic empty-database installation, exact-version refusal, and
  driver-error mapping. No database migration or payload rewrite path exists before v2 stabilizes.

- [ ] **Step 1: Add the dependency and failing open/configuration tests.** Add
      `modernc.org/sqlite v1.56.0`. Require every connection to report foreign keys on, WAL,
      `synchronous=FULL`, the configured busy timeout, and bounded pool settings. Require
      `PRAGMA quick_check` success and clean close checkpoint attempts.

  ```go
  type Options struct {
      MaxOpenConnections int
      MutationBusyTimeout time.Duration
      StartupDeadline time.Duration
      Now func() time.Time
  }
  var DefaultOptions = Options{3, 5 * time.Second, 60 * time.Second, time.Now}
  ```

  Run: `go test ./internal/store/sqlite -run 'TestOpen' -count=1`

  Expected: FAIL because the SQLite package does not exist.

- [ ] **Step 2: Write schema-inspection tests before installation.** Require `STRICT` tables for
      singleton schema metadata, profile state, accounts, merchants, groups, categories, transactions,
      external identities, known drills, journal headers, operation payloads, and targets. Assert
      every money column is `INTEGER`, no money table declares `REAL`, all foreign keys exist, the
      singleton revision/cursor row is constrained, collision-key uniqueness applies only to
      active entities, and operation sequence/type/version constraints exist.

- [ ] **Step 3: Implement embedded current-schema installation atomically.** The core money constraint must be
      equivalent to:

  ```sql
  amount_minor INTEGER NOT NULL CHECK(typeof(amount_minor) = 'integer'),
  currency TEXT NOT NULL CHECK(length(currency) = 3 AND currency GLOB '[A-Z][A-Z][A-Z]'),
  scale INTEGER NOT NULL CHECK(typeof(scale) = 'integer' AND scale BETWEEN 0 AND 9)
  ```

  Pre-create private files through `home.PrepareDatabase`, put connection-local pragmas in the
  DSN, verify them after open, and never export `sql.DB`, `sql.Tx`, statements, rows, or modernc
  errors.

- [ ] **Step 4: Implement first-start concurrency and exact-version refusal.** One process acquires
      the schema-install write lock for an empty database. Other openers re-read schema state until
      the separate 60-second deadline. Deadline expiry maps to `store_busy`; older schema maps to
      `schema_incompatible`; newer schema maps to `schema_newer`; failed installation rolls back;
      failed integrity maps to `store_corrupt`. Never upgrade or rewrite an existing database and
      do not open a degraded read-only session.

- [ ] **Step 5: Run focused, race, and no-CGO checks.** Run:

  ```bash
  go test ./internal/store/sqlite -count=1
  go test -race ./internal/store/sqlite -count=1
  CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go test ./internal/store/sqlite
  CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go test ./internal/store/sqlite
  ```

  Expected: all pass; temp profiles remain under test-owned paths.

- [ ] **Step 6: Run the Required Commit Gate and commit.** Commit with subject
      `feat: add pure Go SQLite profile schema`.

### Task 4: Seed and Load Complete Committed Profiles

**Files:**

- Create: `internal/fixture/profile.go`
- Create: `internal/fixture/profile_test.go`
- Create: `internal/store/sqlite/seed.go`
- Create: `internal/store/sqlite/seed_test.go`
- Create: `internal/store/sqlite/load.go`
- Create: `internal/store/sqlite/load_test.go`
- Modify: `internal/store/sqlite/schema/profile.sql`

**Interfaces:**

- Consumes: fixture `Document`, Task 1 committed entities, and Task 3 SQLite profile.
- Produces: `fixture.CommittedProfile([]domain.Transaction)`, atomic
  `Profile.CreateSeededProfile`, and defensive `Profile.Load` with revision, cursor, committed
  rows, known drills, and an empty journal.

- [ ] **Step 1: Write failing fixture-to-profile tests.** Require deterministic stable entity IDs,
      first-class merchants/categories/groups, explicit external mappings for the synthetic source,
      protected Uncategorized sentinels, exact integer money, and collision validation. Two input
      labels with equal normalized collision keys must make conversion fail before SQLite writes.

  Run: `go test ./internal/fixture -run 'TestCommittedProfile' -count=1`

  Expected: FAIL because fixture conversion does not exist.

- [ ] **Step 2: Implement deterministic synthetic conversion.** Reuse fixture transaction IDs and
      derive entity IDs from canonical synthetic IDs, not display labels. Sort all entity and
      transaction slices by stable ID. Build the seed's known drill registry from materialized
      committed analytics partitions.

- [ ] **Step 3: Write failing seed/load and refusal tests.** Cover revision zero empty creation to
      revision one, exact round trip, restart, refuse-overwrite for any populated table/revision,
      rollback on a mid-seed constraint failure, no direct default-profile access, and defensive
      copies. Verify a second store handle observes the seed.

- [ ] **Step 4: Implement batched atomic seed and joined load.** Use prepared statements in one
      transaction. The seed path is the only direct committed-table writer outside fold. Load all
      rows, build Task 1 domain types, validate once, and return no database-backed slices. Never
      hold a read transaction after `Load` returns.

- [ ] **Step 5: Run focused tests and inspect the physical schema.** Run:

  ```bash
  go test ./internal/fixture ./internal/store/sqlite -run 'Test(CommittedProfile|Seed|Load)' -count=1
  go test -race ./internal/store/sqlite -run 'Test(Seed|Load)' -count=1
  ```

  Expected: all pass; seeded money values remain `integer` under `typeof` inspection.

- [ ] **Step 6: Run the Required Commit Gate and commit.** Commit with subject
      `feat: seed and load SQLite profiles`.

### Task 5: Implement Deterministic Full Replay

**Files:**

- Create: `internal/replay/replay.go`
- Create: `internal/app/replay.go`
- Create: `internal/app/replay_test.go`
- Create: `internal/app/replay_property_test.go`
- Create: `internal/app/drill_registry.go`
- Create: `internal/app/drill_registry_test.go`
- Modify: `internal/domain/profile.go`
- Modify: `internal/domain/profile_test.go`

**Interfaces:**

- Consumes: Task 1 typed operations and Task 4 loaded profile snapshots.
- Produces: `app.Replay(domain.ProfileSnapshot) (app.EffectiveSnapshot, error)`,
  `app.ApplyOperation`, `app.KnownDrillDisposition`, and the reference implementation against
  which every later incremental path is tested.

- [ ] **Step 1: Write table-driven failing replay tests.** Cover each operation type, operation
      order, active cursor prefix, inactive redo tail, merchant label/merge/reassignment,
      category/group create/rename/move/merge/delete, protected sentinels, hide toggles, retired-ID
      rejection, and exact target snapshots. Assert committed input is byte-for-byte unchanged.

  ```go
  type EffectiveSnapshot struct {
      Revision uint64
      Cursor int
      Committed domain.CommittedProfile
      Effective domain.CommittedProfile
      Journal []domain.Operation
      KnownDrills []domain.DrillIdentity
  }
  func Replay(domain.ProfileSnapshot) (EffectiveSnapshot, error)
  func ApplyOperation(domain.CommittedProfile, domain.Operation) (domain.CommittedProfile, error)
  type KnownDrillDisposition string
  const (
      DrillPopulated KnownDrillDisposition = "populated"
      DrillEmpty KnownDrillDisposition = "empty"
      DrillInvalid KnownDrillDisposition = "invalid"
  )
  ```

  Run: `go test ./internal/app -run 'Test(Replay|ApplyOperation)' -count=1`

  Expected: FAIL because replay does not exist.

- [ ] **Step 2: Implement full replay with deterministic ordering.** Clone committed state, apply
      exactly the first `cursor` ordered operations, validate after each operation, then materialize
      analytics transactions. Do not interpret labels or re-resolve predicates. Keep inactive
      operations decoded and visible for review but absent from effective state.

- [ ] **Step 3: Implement the three-way drill classification.** A key in committed known drills or
      retired entity history is valid even when empty. A key introduced by the active journal is
      valid only while effective. A syntactically valid key in neither source is invalid. Add the
      pending-create → undo → redo-tail-truncate case and prove it becomes invalid without a
      registry write.

- [ ] **Step 4: Add deterministic randomized sequence properties.** Generate valid operation
      sequences from a small synthetic profile. After every operation and cursor change compare
      replay from scratch with repeated `ApplyOperation` over the same prefix. Seed the generator
      explicitly and print the seed on failure.

- [ ] **Step 5: Run focused, race, and mutation-safety checks.** Run:

  ```bash
  go test ./internal/app -run 'Test(Replay|ApplyOperation|KnownDrill)' -count=1
  go test ./internal/app -run TestReplayRandomized -count=20
  go test -race ./internal/app -run 'Test(Replay|KnownDrill)' -count=1
  ```

  Expected: all pass with deterministic output ordering.

- [ ] **Step 6: Run the Required Commit Gate and commit.** Commit with subject
      `feat: replay pending profile operations`.

### Task 6: Persist Versioned Journal Appends, Undo, and Redo with Revision CAS

**Files:**

- Create: `internal/store/sqlite/codec.go`
- Create: `internal/store/sqlite/codec_test.go`
- Create: `internal/store/sqlite/journal.go`
- Create: `internal/store/sqlite/journal_test.go`
- Create: `internal/store/sqlite/revision_test.go`
- Modify: `internal/store/sqlite/load.go`

**Interfaces:**

- Consumes: Task 1 operation union, Task 2 store methods, and Task 3 exact-version schema.
- Produces: SQLite implementations of `Append` and `MoveCursor`, versioned typed payload codecs,
  authoritative revision CAS, redo-tail truncation, and journal-inclusive `Load`.

- [ ] **Step 1: Write failing codec and version-refusal tests.** Round-trip every operation type through
      `(type, payload_version, canonical JSON, ordered targets)`. Reject unknown types/versions,
      duplicate targets, payload/type mismatch, trailing JSON, and noncanonical data. Prove an
      unsupported stored payload refuses load as `schema_incompatible` without rewriting any row.

  Run: `go test ./internal/store/sqlite -run 'Test(OperationCodec|PayloadVersion)' -count=1`

  Expected: FAIL because the codec and payload-version refusal do not exist.

- [ ] **Step 2: Implement one canonical codec boundary.** Keep JSON bytes private to
      `internal/store/sqlite`. Decode into `domain.Operation`, call `Validate`, and encode structs
      with fixed field order. Loading any unsupported entry must fail mutable startup with
      `schema_incompatible`, never skip or rewrite the entry.

- [ ] **Step 3: Write failing append/cursor transaction tests.** Assert:

  ```go
  next, err := profile.Append(ctx, expectedRevision, operation)
  next, err := profile.MoveCursor(ctx, expectedRevision, -1) // undo
  next, err := profile.MoveCursor(ctx, expectedRevision, +1) // redo
  ```

  Each success increments revision once. Append at the head assigns the next immutable sequence;
  append behind the head first deletes the inactive tail. Undo at zero and redo at head return
  `invalid_operation` without mutation. A stale expected revision returns `revision_conflict`
  without sequence gaps or partial tail deletion.

- [ ] **Step 4: Implement authoritative compare-and-advance in each write transaction.** Start the
      SQLite write transaction, read and compare the singleton revision, perform the complete
      mutation, update revision/cursor once, and commit. Do not rely on a pre-transaction read.
      Preserve the active-count cursor when sequence numbers have gaps.

- [ ] **Step 5: Prove cross-handle and cross-process contention.** Use two handles and a helper
      subprocess to submit the same expected revision. Exactly one append succeeds; the other is
      `revision_conflict`. Hold a real write lock past the mutation busy timeout and require
      `store_busy`, no automatic retry, and unchanged rows/revision.

- [ ] **Step 6: Run focused and race checks.** Run:

  ```bash
  go test ./internal/store/sqlite -run 'Test(OperationCodec|PayloadMigration|Append|MoveCursor|RevisionCAS|StoreBusy)' -count=1
  go test -race ./internal/store/sqlite -run 'Test(Append|MoveCursor|RevisionCAS)' -count=1
  ```

  Expected: all pass; a fresh `Load` from either handle returns the same journal and cursor.

- [ ] **Step 7: Run the Required Commit Gate and commit.** Commit with subject
      `feat: persist revisioned operation journal`.

### Task 7: Resolve Exact Targets and Construct Merchant and Category Edits

**Files:**

- Create: `internal/app/mutations.go`
- Create: `internal/app/mutations_test.go`
- Create: `internal/app/targets.go`
- Create: `internal/app/targets_test.go`
- Create: `internal/app/edit_merchant.go`
- Create: `internal/app/edit_merchant_test.go`
- Create: `internal/app/edit_category.go`
- Create: `internal/app/edit_category_test.go`
- Modify: `internal/app/selection.go`
- Modify: `internal/app/actions.go`

**Interfaces:**

- Consumes: Task 5 effective snapshots, current `ViewState`/selection codec, and Task 6 append.
- Produces: `app.MutationRequest`, `app.EditInput`, `app.ResolveTargets`,
  `app.BuildMerchantOperation`, `app.BuildCategoryAssignment`, and typed renderer-neutral mutation
  failures.

- [ ] **Step 1: Write failing target-precedence tests.** Require explicit nonempty selection to
      win; otherwise use the focused detail row or focused aggregate row. Resolve aggregate targets
      from the submitted snapshot once, sort exact local transaction IDs, and store those IDs in
      the operation. Reject out-of-window focus, wrong identity kind, stale selection, partial
      revalidation, and retired targets.

  ```go
  type MutationRequest struct {
      Action ActionID
      ExpectedRevision uint64
      State ViewState
      Selection SelectionValue
      Target *RowTarget
      Input EditInput
  }
  type EditInput struct {
      Scope EditScope
      Label string
      DestinationID domain.EntityID
      GroupID domain.EntityID
      ReplacementID domain.EntityID
  }
  type ResolvedTargets struct {
      TransactionIDs []domain.EntityID
      EntityIDs []domain.EntityID
      FromSelection bool
  }
  ```

  Run: `go test ./internal/app -run 'TestResolveTargets' -count=1`

  Expected: FAIL because mutation target resolution does not exist.

- [ ] **Step 2: Implement deterministic selection revalidation.** When a selection's defining
      revision differs, re-resolve all identities against the current snapshot. Return a typed
      `selection_stale` result with current revision and either a fully refreshed selection or an
      empty selection; never append an operation in that response.

- [ ] **Step 3: Write failing merchant intent tests.** Require explicit scope:

  ```go
  const (
      EditScopeEntity EditScope = "entity"
      EditScopeTransactions EditScope = "transactions"
  )
  ```

  Entity rename to a fresh collision key builds `merchant.label` and preserves ID. Entity rename
  to an existing key builds `merchant.merge` only after explicit confirmation. Transaction scope
  builds `merchant.reassign` to an existing or newly created merchant and never relabels the
  source. Tests cover a selected subset, focused transaction, whole aggregate, and source-drill
  shrink/empty behavior.

- [ ] **Step 4: Implement merchant operation construction and category assignment.** `c` resolves
      exact transaction IDs, assigns an existing category, or includes a complete new-category
      entity in `category.create` before reassignment. Taxonomy rename collisions are not accepted
      here. Validate every label/collision through Task 1 before calling `Append`.

- [ ] **Step 5: Add selection disposition to successful results.** A mutation from explicit
      selection returns `SelectionCleared`; a focused mutation returns `SelectionPreserved`.
      Neither operation changes analytical `ViewState`.

- [ ] **Step 6: Run focused characterization and property checks.** Run:

  ```bash
  go test ./internal/app -run 'Test(ResolveTargets|Merchant|CategoryAssignment|SelectionStale)' -count=1
  go test ./internal/app -run TestReplayRandomized -count=20
  ```

  Expected: all pass; journal entries contain IDs only, never predicates or selection documents.

- [ ] **Step 7: Run the Required Commit Gate and commit.** Commit with subject
      `feat: construct exact merchant and category edits`.

### Task 8: Stage Taxonomy Management and Python-Compatible Hide Cancellation

**Files:**

- Create: `internal/app/taxonomy.go`
- Create: `internal/app/taxonomy_test.go`
- Create: `internal/app/hide.go`
- Create: `internal/app/hide_test.go`
- Create: `internal/store/sqlite/cancel_hide.go`
- Create: `internal/store/sqlite/cancel_hide_test.go`
- Modify: `internal/store/sqlite/journal.go`
- Modify: `internal/app/mutations.go`

**Interfaces:**

- Consumes: Task 7 mutation requests and Task 6 journal transactions.
- Produces: category/group create, label, move, merge, delete builders; hide append-or-cancel
  routing; and the SQLite `CancelHide` implementation.

- [ ] **Step 1: Write failing taxonomy behavior tests.** Cover category create/rename/move/merge/
      delete and group create/rename/merge/delete. Reject colliding taxonomy rename with an
      explicit-merge message. Require explicit replacement or Uncategorized on assigned category
      delete, destination group or protected Uncategorized on nonempty group delete, never-reused
      retired IDs, and sentinel protection. Every result must remain pending/reviewable.

  Run: `go test ./internal/app -run 'Test(CategoryManager|GroupManager)' -count=1`

  Expected: FAIL because taxonomy builders do not exist.

- [ ] **Step 2: Implement taxonomy builders with complete forward payloads.** Creation payloads
      include stable ID, label, collision key, and parent. Move includes source category and
      destination group. Merge/delete includes source, destination/unassignment, and exact
      transaction/category targets resolved at creation. No payload depends on later lookup by
      label.

- [ ] **Step 3: Write failing all-or-cancel hide tests.** Cover visible→pending hidden,
      committed-hidden→pending unhidden, `h h` cancellation for either direction, mixed targets
      appending an ordinary toggle, partial removal from a batch, complete removal of an operation,
      cancellation after undo with redo-tail truncation, stale expected revision, and sequence
      holes. Use the cursor example 10/20/30 → remove 20 → cursor 2 → undo deactivates 30.

- [ ] **Step 4: Implement `BuildHideMutation` and transactional `CancelHide`.** Determine the
      all-or-cancel choice from active effective journal effects. In one CAS transaction truncate
      inactive redo, remove selected targets from originating active hide operations, delete empty
      operations, decrement cursor once per fully removed active operation, and increment revision
      once. Partial target replacement keeps sequence and cursor unchanged.

- [ ] **Step 5: Prove cancellation is not a new undo unit.** After cancelling a hide batch, `u`
      must reach the preceding visible operation. Review contains neither an empty operation nor a
      compensating toggle. Cross-handle stale cancellation must change nothing.

- [ ] **Step 6: Run focused and randomized checks.** Run:

  ```bash
  go test ./internal/app ./internal/store/sqlite -run 'Test(CategoryManager|GroupManager|Hide|CancelHide)' -count=1
  go test ./internal/app -run TestReplayRandomized -count=50
  go test -race ./internal/store/sqlite -run TestCancelHide -count=1
  ```

  Expected: all pass with deterministic cursor and target ordering.

- [ ] **Step 7: Run the Required Commit Gate and commit.** Commit with subject
      `feat: stage taxonomy and hide edits`.

### Task 9: Fold Active Operations Atomically into Committed State

**Files:**

- Create: `internal/app/commit.go`
- Create: `internal/app/commit_test.go`
- Create: `internal/app/commit_property_test.go`
- Create: `internal/store/sqlite/fold.go`
- Create: `internal/store/sqlite/fold_test.go`
- Modify: `internal/store/sqlite/load.go`
- Modify: `internal/store/sqlite/schema/profile.sql`

**Interfaces:**

- Consumes: Task 5 replay, Task 6 journal, Task 8 entity operations, and Task 2 `FoldPlan`.
- Produces: `app.BuildFoldPlan`, SQLite `Profile.Fold`, exact commit-fold equivalence, permanent
  known-drill insertion, journal clearing, and one revision advance.

- [ ] **Step 1: Write failing fold-plan tests.** Build a plan only from a freshly replayed snapshot
      at `reviewedRevision`. Include the active operation IDs in order, the exact effective
      committed profile, and the union of old known drills plus identities exposed by the active
      fold, including an identity created and retired within the same committed prefix. Exclude
      identities introduced only by the inactive redo tail.

  Run: `go test ./internal/app -run 'TestBuildFoldPlan' -count=1`

  Expected: FAIL because fold planning does not exist.

- [ ] **Step 2: Write failing SQLite atomic-fold tests.** Cover transaction changes, label updates,
      merchant/taxonomy merge tombstones, category/group moves, creation, hidden flags, active
      prefix only, redo-tail discard, cursor reset, journal deletion, permanent known identities,
      revision increment once, and reviewed-revision conflict. Inject a constraint failure after
      partial writes and prove committed rows, registry, journal, cursor, and revision are all
      unchanged.

- [ ] **Step 3: Implement targeted batched fold in one transaction.** After authoritative CAS,
      verify active operation IDs still match the plan, diff committed versus effective entity and
      transaction rows, execute deterministic prepared upserts/retirements, insert new known
      drills, delete the complete journal including inactive tail, set cursor zero, and advance
      revision. Do not replace unchanged 100,000-row tables.

- [ ] **Step 4: Add the central randomized equivalence assertion.** For each generated valid
      history: capture effective state, call `Fold`, close the handle, reopen, and compare freshly
      loaded committed state exactly with the captured effective profile—including ordering,
      tombstones, taxonomy membership, flags, money, and known drills.

- [ ] **Step 5: Test the pending-only versus committed registry boundary.** A created identity is
      valid while active; undo plus tail truncation then restart makes it invalid. Commit the same
      creation and later retire it; restart and web-style lookup must classify it valid-empty.

- [ ] **Step 6: Run focused, randomized, and race checks.** Run:

  ```bash
  go test ./internal/app ./internal/store/sqlite -run 'Test(BuildFoldPlan|Fold|KnownDrillCommit)' -count=1
  go test ./internal/app ./internal/store/sqlite -run TestCommitFoldRandomized -count=30
  go test -race ./internal/store/sqlite -run TestFold -count=1
  ```

  Expected: all pass; failures leave every persisted table unchanged.

- [ ] **Step 7: Run the Required Commit Gate and commit.** Commit with subject
      `feat: fold pending edits into SQLite`.

### Task 10: Coordinate Revision-Aware Profile State, Capabilities, Review, and Errors

**Files:**

- Create: `internal/app/profile_service.go`
- Create: `internal/app/profile_service_test.go`
- Create: `internal/app/capabilities.go`
- Create: `internal/app/capabilities_test.go`
- Create: `internal/app/review.go`
- Create: `internal/app/review_test.go`
- Create: `internal/app/errors.go`
- Create: `internal/app/errors_test.go`
- Modify: `internal/app/service.go`
- Modify: `internal/app/web.go`
- Modify: `internal/app/actions.go`

**Interfaces:**

- Consumes: Tasks 4-9 store/replay/mutation/fold behavior and the existing read-only projection.
- Produces: `app.NewProfileService`, context-aware refresh/mutation/undo/redo/review/commit methods,
  profile revision and pending summaries in projections, capability availability, bounded review
  windows, and stable renderer-neutral failures.

- [ ] **Step 1: Write failing coordinator/cache tests.** Use this public surface:

  ```go
  func NewProfileService(context.Context, store.Profile) (*Service, error)
  func (s *Service) Refresh(context.Context) (bool, error)
  func (s *Service) Revision() uint64
  func (s *Service) Mutate(context.Context, MutationRequest) (MutationResult, error)
  func (s *Service) Undo(context.Context, uint64) (MutationResult, error)
  func (s *Service) Redo(context.Context, uint64) (MutationResult, error)
  func (s *Service) Review(context.Context, uint64, ReviewWindow) (ReviewProjection, error)
  func (s *Service) Commit(context.Context, CommitRequest) (MutationResult, error)
  type CommitRequest struct { ExpectedRevision, ReviewedRevision uint64 }
  ```

  Every interaction calls `CurrentRevision`; unchanged revision reuses immutable cached replay,
  changed revision reloads before target resolution, and authoritative transaction conflicts still
  win over the courtesy check. `NewService([]domain.Transaction)` may remain only as a read-only
  unit-test constructor; no command may use it after Task 11.

  Run: `go test ./internal/app -run 'TestProfileService' -count=1`

  Expected: FAIL because the persistent coordinator does not exist.

- [ ] **Step 2: Implement locked immutable cache replacement.** Guard snapshot pointers with
      `sync.RWMutex`; never mutate a snapshot held by a reader. `Refresh` reads revision first,
      loads/replays only on change, and performs no database work while analytics renders. After a
      successful mutation, full replay is the reference; optional incremental apply must be
      compared in randomized tests before use.

- [ ] **Step 3: Write and implement capability tests.** Define provider-neutral capabilities for
      local merchant, taxonomy, hide, undo, redo, review, and commit. Static action identity/help
      stays in `actions.go`; availability depends on profile mode, cursor/head, and pending count.
      Keep unsupported delete/export/duplicates visible according to the prior help contract.

- [ ] **Step 4: Implement bounded review projections.** Return operation summaries with order,
      active/inactive status, affected count, safe before/after display values, and taxonomy effect.
      `ReviewWindow{OperationID, Offset, Limit}` returns at most 400 target rows and never the full
      target set by default. Commit requires both expected current revision and the reviewed
      revision; stale review confirmation returns `revision_conflict` and preserves review state.

- [ ] **Step 5: Complete failure mapping and selection disposition.** Map store codes without
      exposing diagnostics. `selection_stale` carries current revision and refreshed-or-cleared
      selection in a typed failure result. `store_error` includes current revision only if safely
      read. No revision/store failure retries automatically.

- [ ] **Step 6: Extend read-only projections with effective state.** `Query`, `ProjectView`, and
      `TransitionView` use materialized effective transactions, decorate pending flags, return
      `Revision`, `PendingSummary{ActiveOperations, InactiveOperations, AffectedTransactions}`, and
      resolve the stable rename/retired-empty/never-known-invalid distinction.

- [ ] **Step 7: Run focused, race, and existing projection checks.** Run:

  ```bash
  go test ./internal/app -run 'Test(ProfileService|Capabilities|Review|AppError|ProjectView)' -count=1
  go test -race ./internal/app -run 'Test(ProfileService|Review)' -count=1
  make parity
  ```

  Expected: all pass; read-only analytical behavior is unchanged apart from effective pending flags
  and approved help/status additions.

- [ ] **Step 8: Run the Required Commit Gate and commit.** Commit with subject
      `feat: coordinate persistent profile editing`.

### Task 11: Route Normal and Demo Commands Exclusively Through SQLite

**Files:**

- Create: `cmd/moneyflow/profile.go`
- Create: `cmd/moneyflow/profile_test.go`
- Modify: `cmd/moneyflow/root.go`
- Modify: `cmd/moneyflow/root_test.go`
- Modify: `cmd/moneyflow/web.go`
- Modify: `cmd/moneyflow/web_test.go`
- Modify: `internal/tui/runner.go`
- Modify: `internal/web/server.go`

**Interfaces:**

- Consumes: Task 2 home paths, Task 3 SQLite open, Task 4 seeding, and Task 10 profile service.
- Produces: command-owned `OpenedProfile`, default profile creation, fresh demo lifecycle, and
  TUI/web runners that receive one persistent `*app.Service` and close the underlying store once.

- [ ] **Step 1: Write failing command-wiring tests.** Inject path resolution, temp-directory
      creation, store opening, fixture conversion, and cleanup. Require bare `moneyflow` to open or
      create the default empty SQLite profile; `moneyflow demo` to create one unique temp profile,
      seed revision one, and clean it on normal exit; `moneyflow web --demo` to do the same; and
      every demo invocation to receive a different path.

  ```go
  type OpenedProfile struct {
      Service *app.Service
      Close func() error
      Path string
      Demo bool
  }
  type ProfileOpener func(context.Context, ProfileOptions) (OpenedProfile, error)
  type ProfileOptions struct { Demo bool; ExplicitHome string }
  ```

  Run: `go test ./cmd/moneyflow -run 'Test(Profile|Demo|RootUsesSQLite|WebUsesSQLite)' -count=1`

  Expected: FAIL because commands still construct fixture-backed services.

- [ ] **Step 2: Implement the profile opener and lifecycle.** Normal missing profile installs an
      empty current schema without seeding. Demo creates a restrictive directory under the OS temp
      root, converts the embedded synthetic fixture, calls `CreateSeededProfile`, and returns an
      idempotent close that stops application work, closes SQLite, then removes that exact temp
      directory only after verifying it is the invocation-owned child returned by the temp API.
      Abnormal leftovers contain synthetic data only.

- [ ] **Step 3: Remove command fixture bypasses.** Replace `newEmbeddedService` and root preview
      fixture loading. Keep the hidden `--fixture` flag only if parity tooling still invokes it;
      when supplied, seed its decoded synthetic document into a fresh temp SQLite profile rather
      than constructing `app.NewService` directly.

  Thread the Cobra command context through `tui.Run(context.Context, ...)` and
  `tui.NewModel(context.Context, ...)` so revision checks and mutations stop with process
  cancellation. Update every runner/test seam explicitly; do not use `context.Background()` in
  production TUI interaction code.

- [ ] **Step 4: Make empty normal profiles render honestly.** TUI and web must start successfully
      with zero transactions, show the ordinary empty projection, and expose local taxonomy create
      only where its prerequisites are meaningful. They must not silently substitute demo data.

- [ ] **Step 5: Add restart persistence tests.** Run a command with an explicit temp home, append
      one pending edit through the service seam, close, reopen via a second command, and assert the
      revision/journal/effective row return. Assert the default home opener was never called by any
      test or demo.

- [ ] **Step 6: Run focused lifecycle and portability checks.** Run:

  ```bash
  go test ./cmd/moneyflow ./internal/tui ./internal/web -run 'Test(Profile|Demo|Lifecycle|Empty)' -count=1
  CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go test ./cmd/moneyflow
  CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go test ./cmd/moneyflow
  ```

  Expected: all pass; no test creates `~/.moneyflow/v2/moneyflow.db`.

- [ ] **Step 7: Run the Required Commit Gate and commit.** Commit with subject
      `feat: run Go commands from SQLite profiles`.

### Task 12: Add Core TUI Merchant, Category, Hide, Undo, and Redo Workflows

**Files:**

- Create: `internal/tui/edit_merchant.go`
- Create: `internal/tui/edit_merchant_test.go`
- Create: `internal/tui/edit_category.go`
- Create: `internal/tui/edit_category_test.go`
- Create: `internal/tui/edit_common.go`
- Create: `internal/tui/edit_common_test.go`
- Modify: `internal/tui/model.go`
- Modify: `internal/tui/update.go`
- Modify: `internal/tui/update_test.go`
- Modify: `internal/tui/frame.go`
- Modify: `internal/tui/frame_test.go`
- Modify: `internal/tui/table.go`
- Modify: `internal/tui/help.go`

**Interfaces:**

- Consumes: Task 10 profile service and capabilities; existing Bubble Tea overlay routing,
  identity-preserving refresh, selection, and table rendering.
- Produces: keyboard-owned merchant/category editors, synchronous `h`/`u`/`U` mutations, pending
  flags/counts, conflict announcements, and identity-preserving focus/scroll restoration.

- [ ] **Step 1: Write failing overlay-ownership tests.** `m` and `c` open focused overlays only
      when capability is available. Printable input, arrows, Enter, Tab, and Escape belong to the
      overlay; global grouping/search/quit/edit keys cannot leak through. Cancel restores the
      opening analytical state, stable focused identity, cursor, scroll, and selection.

  Run: `go test ./internal/tui -run 'Test(MerchantEditor|CategoryEditor|EditOverlayOwnership)' -count=1`

  Expected: FAIL because editing overlays do not exist.

- [ ] **Step 2: Implement merchant input and explicit scope.** Match Python's focused merchant and
      multi-select workflow: searchable existing destinations, new label input, and explicit
      entity-versus-selected-transactions scope. A collision that means merge shows and confirms
      “merge” before submit. Submit `MutationRequest` with the model's current revision; never build
      a journal payload in TUI code.

- [ ] **Step 3: Implement the category selector with create-on-the-fly.** Search active categories,
      preserve the protected Uncategorized choice, allow a new label plus group selection, and
      submit exact focused/selected context. Validation remains in the service; the overlay renders
      safe messages only.

- [ ] **Step 4: Write and implement direct-action tests.** `h` appends/cancels, `u` undoes, and `U`
      redoes without an overlay. Success updates effective tables/charts, pending flags, active and
      affected counts; bulk success clears selection; focused success preserves it. No action
      changes the analytical session.

- [ ] **Step 5: Handle revision and selection failures deterministically.** On conflict, refresh
      once without replaying the mutation, preserve focus/scroll where identities remain, and
      require the user to press the key again. On `selection_stale`, install the returned complete
      refreshed or cleared selection, announce it, and do not mutate.

  At the start of every non-force-quit key interaction, call `Service.Refresh` with the model's
  lifecycle context before using cached results or resolving a focused identity. If another process
  advanced the revision, replace effective rows and restore focus by stable identity before routing
  the key; never hold a database transaction while awaiting the next Bubble Tea message.

- [ ] **Step 6: Add frame and restart behavior tests.** Pending markers and counts render at
      150x50, 150x40, 150x30, and 80x24. Pure merchant label rename keeps the current drill and
      updates breadcrumb; merge/reassignment may produce a valid empty drill. Close/reopen restores
      the same pending result.

- [ ] **Step 7: Run focused and parity checks.** Run:

  ```bash
  go test ./internal/tui -run 'Test(MerchantEditor|CategoryEditor|EditOverlay|Hide|Undo|Redo|Pending)' -count=1
  make parity
  ```

  Expected: all pass; any visual update is limited to named editing/help/pending frames and uses
  the deliberate parity-update flow before commit.

- [ ] **Step 8: Run the Required Commit Gate and commit.** Commit with subject
      `feat: add core TUI editing workflows`.

### Task 13: Add TUI Taxonomy Managers, Review, Commit, and Durable Quit Copy

**Files:**

- Create: `internal/tui/manage_categories.go`
- Create: `internal/tui/manage_categories_test.go`
- Create: `internal/tui/manage_groups.go`
- Create: `internal/tui/manage_groups_test.go`
- Create: `internal/tui/review.go`
- Create: `internal/tui/review_test.go`
- Create: `internal/tui/quit.go`
- Create: `internal/tui/quit_test.go`
- Modify: `internal/tui/model.go`
- Modify: `internal/tui/update.go`
- Modify: `internal/tui/help.go`
- Modify: `internal/tui/overlay_test.go`

**Interfaces:**

- Consumes: Task 10 taxonomy/review/commit service methods and Task 12 overlay conventions.
- Produces: `C` category manager, `G` group manager, windowed `w` review, stale-safe commit
  confirmation, and plain `q` confirmation with durable-pending messaging.

- [ ] **Step 1: Write failing category/group manager workflow tests.** Match Python internal modal
      keys and search behavior. Require category create/rename/move/merge/delete and group create/
      rename/merge/delete, explicit destructive destinations, collision errors that direct users to
      merge, sentinel protection, cancel restoration, and capability-based disablement.

  Run: `go test ./internal/tui -run 'Test(ManageCategories|ManageGroups)' -count=1`

  Expected: FAIL because manager overlays do not exist.

- [ ] **Step 2: Implement managers as thin service clients.** Each accepted dialog submits one
      application mutation and closes only on success. Merge/delete confirmations name only values
      already visible in the dialog; terminal logs receive operation type/count/revision only.
      Stale conflicts refresh the list and require explicit re-entry.

- [ ] **Step 3: Write failing review tests.** `w` captures a reviewed revision, shows active
      operations in order and inactive redo entries separately, warns that committing behind head
      discards redo, lazily pages target details, and returns to the exact prior view on cancel.
      Review must not allocate or render all targets for a 100,000-row operation.

- [ ] **Step 4: Implement review and commit confirmation.** Use `ReviewWindow` requests of at most
      400 rows. Commit sends current expected plus captured reviewed revision. On conflict, refresh
      the review and require another explicit confirmation. On success, show committed count, clear
      pending flags/history, preserve analytical URL-equivalent session, and refresh focused
      identity where it still exists.

- [ ] **Step 5: Write and implement quit behavior tests.** `q` always opens a plain confirmation.
      With pending operations, copy says they are safely persisted and will return next launch; it
      never calls them unsaved. With an open unsubmitted dialog, quit/cancel discards only local
      draft input. `Ctrl+C` remains immediate.

- [ ] **Step 6: Update deliberate semantic/visual artifacts.** Add named frames for redo,
      managers, active/inactive review, commit warning, durable-pending quit, and conflicts. Run the
      explicit Python/Go parity updates only where Python help characterization changes, inspect the
      complete diff, and retain renderer-specific Go styling.

- [ ] **Step 7: Run focused and parity checks.** Run:

  ```bash
  go test ./internal/tui -run 'Test(Manage|Review|Commit|Quit|Overlay)' -count=1
  make parity
  ```

  Expected: all pass; help advertises the corrected existing Python behavior and `U` only as the
  named Go divergence.

- [ ] **Step 8: Run the Required Commit Gate and commit.** Commit with subject
      `feat: complete TUI review and taxonomy editing`.

### Task 14: Establish Canonical Web Origin and Signed Mutation Tokens

**Files:**

- Create: `internal/api/security.go`
- Create: `internal/api/security_test.go`
- Create: `internal/api/bootstrap.go`
- Create: `internal/api/bootstrap_test.go`
- Modify: `internal/api/server.go`
- Modify: `internal/api/errors.go`
- Modify: `internal/web/handler.go`
- Modify: `internal/web/handler_test.go`
- Modify: `internal/web/server.go`
- Modify: `internal/web/server_test.go`
- Modify: `cmd/moneyflow/web.go`
- Modify: `cmd/moneyflow/web_test.go`
- Modify: `web/index.html`
- Modify: `web/src/lib/api/client.ts`
- Modify: `web/src/lib/api/client.test.ts`

**Interfaces:**

- Consumes: existing normalized base path/static handler and Task 11 web profile service.
- Produces: canonical external URL validation, one-hour HMAC mutation tokens, no-store HTML token
  bootstrap and refresh, Origin/Fetch-Metadata enforcement, and direct-listener read-only warning.

- [ ] **Step 1: Write failing external URL tests.** Accept absent external URL for loopback and a
      normalized `https://moneyflow.example/moneyflow` URL whose path equals `/moneyflow/` after
      normalization. Reject userinfo, query, fragment, unsupported scheme, wildcard/invalid origin,
      and path mismatch at startup.

  ```go
  type OriginConfig struct { Canonical *url.URL; BasePath string }
  func ResolveOrigin(listen, basePath, externalURL string) (OriginConfig, error)
  ```

  Run: `go test ./internal/api ./cmd/moneyflow -run 'Test(Origin|ExternalURL)' -count=1`

  Expected: FAIL because canonical origin configuration does not exist.

- [ ] **Step 2: Write failing token tests.** Generate a random 32-byte secret per server instance.
      Sign version, canonical origin, base path, issued-at, and one-hour expiry with HMAC-SHA256.
      Verify signature in constant time. Reject expiry, wrong origin/path/instance/version,
      malformed/oversized tokens, and clock skew beyond the explicit five-minute allowance.

- [ ] **Step 3: Implement no-store bootstrap without weakening CSP.** Inject the escaped token into
      a non-executable `<meta name="moneyflow-mutation-token">` placeholder in each served index;
      do not add inline script. Add `GET <base>/api/v1/bootstrap` to return a fresh token, canonical
      URL, base path, base-10 string revision, and token expiry with `Cache-Control: no-store`.

- [ ] **Step 4: Implement mutation-request middleware.** Require custom
      `X-Moneyflow-Mutation-Token`, exact canonical `Origin`, and Fetch Metadata
      `Sec-Fetch-Site: same-origin` for persistent endpoints. Set no cookies and no CORS. Return
      `token_expired` only before operation evaluation; use separate safe codes for invalid origin
      and token.

- [ ] **Step 5: Make direct listener behavior explicit.** When `--external-url` is configured,
      direct listener GETs render a noncanonical warning and canonical link. Mutations from that
      origin fail without service invocation. Normal loopback without external URL derives its one
      canonical origin from the actual listener address.

- [ ] **Step 6: Implement browser-memory token refresh.** Read the meta token at startup, keep it
      outside URL/history/storage, refresh before expiry or on `token_expired`, and retry the exact
      unchanged request once. Do not retry any other code.

- [ ] **Step 7: Run focused security and frontend checks.** Run:

  ```bash
  go test ./internal/api ./internal/web ./cmd/moneyflow -run 'Test(Origin|ExternalURL|Token|Bootstrap|MutationSecurity)' -count=1
  bun run --cwd web test -- client.test.ts
  make verify-web
  ```

  Expected: all pass; production HTML contains no inline executable content and tokens never
  appear in URLs, logs, or history.

- [ ] **Step 8: Run both commit gates and commit.** Commit with subject
      `feat: protect web profile mutations`.

### Task 15: Publish Bounded Mutation and Review APIs

**Files:**

- Create: `internal/api/mutations.go`
- Create: `internal/api/mutations_test.go`
- Create: `internal/api/review.go`
- Create: `internal/api/review_test.go`
- Modify: `internal/api/types.go`
- Modify: `internal/api/types_test.go`
- Modify: `internal/api/server.go`
- Modify: `internal/api/server_test.go`
- Modify: `internal/api/errors.go`
- Modify: `internal/api/errors_test.go`
- Modify: `api/openapi.yaml`
- Modify: `web/src/lib/api/schema.d.ts`

**Interfaces:**

- Consumes: Task 10 service methods and Task 14 security middleware.
- Produces: versioned mutation/review wire envelopes, effective projections with revision/pending
  summaries, exact selection dispositions, and stable problem responses.

- [ ] **Step 1: Write failing wire-shape tests.** Define bounded request types:

  ```go
  type MutationBody struct {
      Version string `json:"version"`
      ExpectedRevision string `json:"expected_revision" pattern:"^[0-9]+$" maxLength:"20"`
      Query string `json:"query" maxLength:"65536"`
      Selection string `json:"selection,omitempty" maxLength:"1468006"`
      Action app.ActionID `json:"action"`
      Target *TransitionTarget `json:"target,omitempty"`
      Input MutationInput `json:"input"`
      Window Window `json:"window"`
  }
  type MutationInput struct {
      Scope string `json:"scope,omitempty"`
      Label string `json:"label,omitempty" maxLength:"512"`
      DestinationID string `json:"destination_id,omitempty" maxLength:"512"`
      GroupID string `json:"group_id,omitempty" maxLength:"512"`
      ReplacementID string `json:"replacement_id,omitempty" maxLength:"512"`
  }
  ```

  Responses include revision as a base-10 string, canonical query, effective projection, pending
  summary, and selection disposition. The adapter parses revisions to Go `uint64` with overflow
  rejection. Exact money remains strings. No raw operation payload, sequence, SQL row, provider
  metadata, or full dataset is exposed.

  Run: `go test ./internal/api -run 'Test(MutationTypes|ReviewTypes)' -count=1`

  Expected: FAIL because mutation wire types do not exist.

- [ ] **Step 2: Register persistent endpoints behind Task 14 middleware.** Add:

  ```text
  POST /api/v1/mutations
  POST /api/v1/undo
  POST /api/v1/redo
  POST /api/v1/commit
  POST /api/v1/review
  POST /api/v1/review/targets
  ```

  The server decodes analytical state and selection, builds `app.MutationRequest`, and lets the
  service construct operations. It never accepts journal IDs except the server-issued review
  operation identity used to page details.

- [ ] **Step 3: Implement total stable error mapping.** Map `revision_conflict`, `invalid_operation`,
      `invalid_target`, `selection_stale`, `store_busy`, and `store_error`. A `selection_stale`
      problem includes current revision and refreshed-or-cleared selection payload; it is not a 2xx
      mutation. Safe problems contain no labels, IDs, query text, SQL, or diagnostic paths.

- [ ] **Step 4: Add read revision checks and profile metadata.** Existing project/transition reads
      refresh revision before using cache. Projection adds revision and pending summary while
      preserving canonical URL semantics and windows; revision is a base-10 string on every wire
      type. Focus/visibility browser reads can use the same project endpoint—no push channel is
      added.

- [ ] **Step 5: Test stale review, size bounds, and no-evaluation security.** Prove expired-token
      requests never call the service, stale expected revisions never append, stale reviewed
      revision never folds, request bodies stay capped at 2 MiB, review target windows stay capped
      at 400, and an all-result selection never expands into an HTTP identity list.

- [ ] **Step 6: Deliberately regenerate contracts.** Run:

  ```bash
  make web-generate
  git diff -- api/openapi.yaml web/src/lib/api/schema.d.ts
  make web-check
  ```

  Expected: only the versioned bootstrap/mutation/review schema and new projection fields change;
  generated types contain no JavaScript numeric money.

- [ ] **Step 7: Run focused API, generation, and browser gates.** Run:

  ```bash
  go test ./internal/api -run 'Test(Mutation|Review|ProjectionRevision|Problem)' -count=1
  make web-check
  make verify-web
  ```

  Expected: all pass and ordinary verification reports generated files current without rewriting.

- [ ] **Step 8: Run both commit gates and commit.** Commit with subject
      `feat: expose profile editing API`.

### Task 16: Coordinate Browser Revision, Token, Mutation, and Conflict State

**Files:**

- Create: `web/src/lib/controller/editing.ts`
- Create: `web/src/lib/controller/editing.test.ts`
- Create: `web/src/lib/controller/review.ts`
- Create: `web/src/lib/controller/review.test.ts`
- Modify: `web/src/lib/controller/view-controller.svelte.ts`
- Modify: `web/src/lib/controller/view-controller.test.ts`
- Modify: `web/src/lib/controller/history.ts`
- Modify: `web/src/lib/controller/history.test.ts`
- Modify: `web/src/lib/api/client.ts`
- Modify: `web/src/App.svelte`
- Modify: `web/src/App.test.ts`

**Interfaces:**

- Consumes: Task 15 generated API client, existing URL/history/window controller, and Task 14
  in-memory token refresh.
- Produces: `EditingController`, `ReviewController`, revision rechecks on reads/focus/visibility,
  exact selection disposition, and mutation results that never alter analytical browser history.

- [ ] **Step 1: Write failing editing-controller tests.** Require this state machine:

  ```ts
  type MutationPhase = 'idle' | 'submitting' | 'conflict' | 'failed';
  type EditingState = {
    revision: bigint;
    phase: MutationPhase;
    pending: PendingSummary;
    announcement: string;
  };
  ```

  Parse the wire's base-10 revision string with `BigInt` and submit `revision.toString(10)` rather
  than lossy numeric arithmetic.
  Successful mutation replaces projection/revision/pending and applies the server's selection
  disposition. It must call neither `pushState` nor `replaceState` and must leave canonical query
  byte-for-byte unchanged.

  Run: `bun run --cwd web test -- editing.test.ts`

  Expected: FAIL because the editing controller does not exist.

- [ ] **Step 2: Implement one mutation pipeline.** Submit action/input/current analytical query,
      selection, target identity, and expected revision. Disable duplicate submit while in flight.
      On success update window cache from returned effective projection and restore cursor by stable
      identity. Never construct operation payloads or calculate target sets in TypeScript.

- [ ] **Step 3: Implement the exact retry matrix.** On `token_expired`, refresh and replay the
      identical request once. On `revision_conflict`, refresh projection, preserve cursor/selection
      only where exact identities remain, explain the conflict, and require explicit reinvocation.
      On `selection_stale`, install the returned refreshed or empty selection and stop. On busy/
      storage/validation error, announce and stop. Unit tests assert service call counts.

- [ ] **Step 4: Add revision checks to passive browser lifecycle.** Before ordinary project reads
      and on `window.focus` or `visibilitychange` to visible, fetch current projection/revision.
      Equal revision leaves controller-local cursor/scroll unchanged; changed revision replaces
      effective data and revalidates stable cursor/selection. Debounce duplicate focus/visibility
      events and add no polling or push channel.

- [ ] **Step 5: Implement bounded review state.** Fetch summaries at captured reviewed revision;
      fetch target pages only when expanded; retain at most current and adjacent detail windows;
      show active/inactive operations separately; and keep the commit reviewed revision distinct
      from the latest observed revision.

- [ ] **Step 6: Prove history and privacy invariants.** Tests inspect `location`, `history.state`,
      local/session storage, request URLs, and captured logs. None may contain token, pending state,
      mutation input, labels beyond existing visible analytical state, or revision-specific payload.

- [ ] **Step 7: Run focused and complete frontend checks.** Run:

  ```bash
  bun run --cwd web test -- editing.test.ts review.test.ts view-controller.test.ts history.test.ts
  make verify-web
  ```

  Expected: all pass; existing read-only history behavior remains unchanged.

- [ ] **Step 8: Run both commit gates and commit.** Commit with subject
      `feat: coordinate browser profile mutations`.

### Task 17: Build Keyboard-First Web Editors, Managers, and Review

**Files:**

- Create: `web/src/components/editing/MerchantDialog.svelte`
- Create: `web/src/components/editing/MerchantDialog.test.ts`
- Create: `web/src/components/editing/CategoryDialog.svelte`
- Create: `web/src/components/editing/CategoryDialog.test.ts`
- Create: `web/src/components/editing/CategoryManager.svelte`
- Create: `web/src/components/editing/CategoryManager.test.ts`
- Create: `web/src/components/editing/GroupManager.svelte`
- Create: `web/src/components/editing/GroupManager.test.ts`
- Create: `web/src/components/editing/ReviewDrawer.svelte`
- Create: `web/src/components/editing/ReviewDrawer.test.ts`
- Create: `web/src/components/editing/PendingStatus.svelte`
- Create: `web/src/components/editing/PendingStatus.test.ts`
- Modify: `web/src/components/AppShell.svelte`
- Modify: `web/src/components/AppShell.test.ts`
- Modify: `web/src/components/FinanceTable.svelte`
- Modify: `web/src/components/FinanceTable.test.ts`
- Modify: `web/src/lib/shortcuts.ts`
- Modify: `web/src/lib/shortcuts.test.ts`
- Modify: `web/src/app.css`

**Interfaces:**

- Consumes: Task 16 controllers, server capabilities, current focused/selection state, kit-ui
  dialogs/drawers/live regions/inputs, and existing shortcut manager.
- Produces: all `m`, `c`, `C`, `G`, `h`, `u`, `U`, and `w` workflows without a mouse, bounded
  review detail, pending presentation, focus restoration, and accessible announcements.

- [ ] **Step 1: Write failing shared shortcut and focus tests.** Editing keys dispatch only when the
      server capability is available and table scope owns the key. Dialog input owns printable
      keys; Escape cancels the innermost overlay; Tab stays trapped; close restores the invoking
      stable row; no `q`/`Ctrl+C` web action appears.

  Run: `bun run --cwd web test -- shortcuts.test.ts AppShell.test.ts`

  Expected: FAIL because editing actions are not routed.

- [ ] **Step 2: Implement merchant and category dialogs with kit-ui.** Merchant dialog offers
      explicit whole-entity versus selected/focused-transactions scope and clearly confirms merge
      collisions. Category dialog searches active categories and supports create-on-the-fly with a
      group picker. Both submit controller input only, preserve selection on validation failure,
      and announce clearing after successful bulk mutation.

- [ ] **Step 3: Implement category and group managers.** Match TUI/Python operations and internal
      keyboard paths: create, rename, move (category), merge, delete. Search/filter lists locally
      only over the bounded taxonomy catalog returned by the service. Require explicit destination
      for destructive operations and render protected sentinels unavailable with explanation.

- [ ] **Step 4: Implement direct `h`, `u`, and `U` plus pending status.** Use the same table target
      resolution inputs as other actions. Show active operation count, inactive redo count when
      present, affected transaction count, and pending row flags. Charts refresh from the returned
      projection; no browser money calculation is added.

- [ ] **Step 5: Implement the windowed review drawer.** Display ordered active operations,
      separately styled inactive redo operations, the commit-discard warning, before/after text,
      counts, and expand-on-demand target pages. Commit confirmation uses the captured review
      revision and remains open after conflict/failure; cancel restores exact table focus/scroll.

- [ ] **Step 6: Add live-region and responsive tests.** Announce validation, selection clearing,
      conflicts, busy/storage errors, pending counts, undo/redo, and commit results. At narrow
      widths managers/review use drawers without covering the primary keyboard table permanently.
      Visible focus and labels cannot rely only on color.

- [ ] **Step 7: Run component, accessibility, and full web gates.** Run:

  ```bash
  bun run --cwd web test -- MerchantDialog.test.ts CategoryDialog.test.ts CategoryManager.test.ts GroupManager.test.ts ReviewDrawer.test.ts PendingStatus.test.ts
  make verify-web
  ```

  Expected: all pass; axe reports no serious/critical issue and every workflow is keyboard-only.

- [ ] **Step 8: Run both commit gates and commit.** Commit with subject
      `feat: add keyboard-first web editing`.

### Task 18: Lock Cross-Renderer Parity, Restart, Proxy, and Identity Semantics

**Files:**

- Create: `internal/app/editing_characterization_test.go`
- Create: `internal/tui/editing_parity_test.go`
- Create: `internal/api/editing_integration_test.go`
- Create: `web/tests/editing.spec.ts`
- Create: `web/tests/review.spec.ts`
- Create: `web/tests/restart.spec.ts`
- Create: `web/tests/origin.spec.ts`
- Modify: `web/tests/fixtures.ts`
- Modify: `web/tests/keyboard.spec.ts`
- Modify: `web/tests/base-path.spec.ts`
- Modify: `web/tests/visual.spec.ts`
- Modify: `testdata/parity/scenarios.json`
- Modify: `testdata/parity/python/semantic.json`
- Modify: `testdata/parity/go/visual.json`
- Modify: `internal/tui/testdata/visual/*.txt`

**Interfaces:**

- Consumes: completed TUI/web workflows and Python characterization oracle.
- Produces: shared scenario assertions for behavior, renderer-specific visual canon, restart proof,
  `/moneyflow` proxy coverage, and the consolidated named-divergence evidence.

- [ ] **Step 1: Add shared characterization scenarios before artifact changes.** Cover merchant
      pure label rename, merchant merge, selected subset reassignment, category existing/new
      assignment, category/group manager operations, hide and `h h`, focused-target precedence,
      bulk selection clearing, undo, redo, review, and commit. Assert Python behavior where shared
      and explicit Go expectation for each named divergence.

  Run: `go test ./internal/app ./internal/tui -run 'TestEditingCharacterization' -count=1`

  Expected: FAIL until scenario adapters and approved Go divergences are encoded.

- [ ] **Step 2: Add the identity-boundary matrix.** In TUI, API bootstrap, and browser bookmark
      flows prove:

  ```text
  stable label rename        -> populated view, same key, updated breadcrumb
  active pending creation    -> valid effective view
  undone/truncated creation  -> invalid view after restart
  committed then retired key -> valid empty view after restart
  never-observed valid key   -> invalid view with Back and Reset
  ```

  Include category merge/delete and merchant merge while drilled into the source. URLs remain
  unchanged for valid-empty results.

- [ ] **Step 3: Add browser keyboard and review journeys.** Drive every edit with keys only through
      the real Huma server and temp SQLite profile. Verify dialog isolation, selection clearing,
      pending flags/counts, `u`/`U`, bounded target paging, commit behind redo head, conflicts, and
      focus restoration. Tests never call internal application helpers directly.

- [ ] **Step 4: Add restart and two-process journeys.** Stop/reopen the server on the same explicit
      temp profile and assert pending state survives. Open TUI/service and web process on one
      profile, submit the same revision, and prove exactly one succeeds. Restore browser focus and
      confirm revision refresh without automatic stale replay.

- [ ] **Step 5: Add canonical-origin and reverse-proxy journeys.** Exercise
      `https://moneyflow.example/moneyflow` semantics through the local test proxy: matching base
      path mutates, direct listener reads with warning but cannot mutate, wrong Origin/Fetch
      Metadata fails, token refresh retries once, and neither URL/history/referrer/log captures a
      token or mutation value.

- [ ] **Step 6: Deliberately update and inspect artifacts.** Run:

  ```bash
  make parity-update-python
  make parity-update-go
  bun run --cwd web test:e2e -- --update-snapshots
  git diff -- testdata/parity internal/tui/testdata/visual web/tests/screenshots
  ```

  Expected: artifact diffs show only approved help/editing/pending/review/quit/conflict states;
  synthetic labels and amounts remain generic. Inspect every screenshot at original resolution and
  rerun privacy scanning before staging.

- [ ] **Step 7: Run parity and full browser verification.** Run:

  ```bash
  make parity
  make verify-web
  go test ./internal/app ./internal/tui ./internal/api -run 'Test(Editing|Identity|Restart|Concurrent)' -count=1
  ```

  Expected: all pass without rewriting committed artifacts.

- [ ] **Step 8: Run both commit gates and commit.** Commit with subject
      `test: lock durable editing parity`.

### Task 19: Prove Failure Atomicity, Performance, Portability, Privacy, and Final Slice Completion

**Files:**

- Create: `internal/store/sqlite/failure_test.go`
- Create: `internal/store/sqlite/performance_test.go`
- Create: `internal/app/editing_performance_test.go`
- Create: `docs/superpowers/benchmarks/2026-08-14-sqlite-editing.md`
- Modify: `Makefile`
- Modify: `.github/workflows/go.yml`
- Modify: `.github/workflows/web.yml`
- Modify: `README.md`
- Modify: `AGENTS.md`
- Modify: `docs/superpowers/specs/2026-08-14-go-port-sqlite-editing-design.md`
- Modify: `docs/superpowers/plans/2026-08-14-go-port-sqlite-editing.md`

**Interfaces:**

- Consumes: the complete slice.
- Produces: real storage-failure proof, 100,000-row budgets, portable CI, stable Make targets,
  user/developer documentation, privacy evidence, and final approved status without pushing or
  merging.

- [ ] **Step 1: Add real bounded-database and I/O failure tests.** Use SQLite page-size/max-page
      limits to produce `SQLITE_FULL` during append and fold, a real held lock for `store_busy`,
      reopen permissions where portable, and a focused driver adapter only for OS errors that
      cannot be induced consistently. Assert every failure preserves committed rows, known drills,
      journal, cursor, and revision. Safe errors/logs must contain no SQL values, labels, IDs, paths,
      or amounts.

  Run: `go test ./internal/store/sqlite -run 'Test(FailureAtomicity|StoreFull|StoreBusy|StoreError)' -count=1`

  Expected: PASS with every post-failure snapshot exactly equal to its pre-failure snapshot.

- [ ] **Step 2: Add cold-load and bulk benchmarks plus generous regression tests.** Generate a
      deterministic 100,000-row temp SQLite profile. Benchmark open/load/replay, 100,000-target
      append, undo, redo, hide cancellation, and fold. The committed smoke requires cold
      open+load+representative replay under one second; retain the existing warm 50/100 ms analytics
      contracts. Record median and environment; do not claim noisy CI as a benchmark machine.

  ```bash
  go test ./internal/store/sqlite ./internal/app -run 'Test(ColdProfilePerformance|BulkEditingPerformance)' -count=1
  go test ./internal/store/sqlite ./internal/app -run '^$' -bench 'Benchmark(ColdProfile|Bulk)' -benchmem -count=5
  ```

  Expected: smoke passes; benchmark evidence includes allocations and each operation's median.

- [ ] **Step 3: Add stable Make targets and CI gates.** Make normal `make tui-demo` and
      `make web-demo` create fresh SQLite demo profiles. Add `make test-store`,
      `make test-editing-e2e`, and include them in `make verify-go`/`make verify-web` without artifact
      writes. CI runs CGO-disabled Linux/macOS/Windows builds, Go race tests, schema inspection,
      frontend/browser security, and parity against temp profiles only.

- [ ] **Step 4: Document the user-visible runtime.** Update README with default v2 profile path,
      unencrypted SQLite/full-disk-encryption expectation, durable pending edits, `U` redo,
      corrected managers, demo ephemerality, concurrent TUI/web access, tailnet/Caddy example using
      the reserved hostname, `--external-url`, direct-listener read-only behavior, and explicit
      non-goals. Update AGENTS commands without changing `CLAUDE.md` symlink status.

- [ ] **Step 5: Run the complete final gate on an idle host.** Run:

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
  CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go test ./...
  CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go test ./...
  ```

  Expected: every command exits zero; ordinary gates leave `git status --short` unchanged.

- [ ] **Step 6: Audit specification coverage and diff scope.** Check every goal, non-goal, parity
      decision, schema invariant, journal rule, renderer workflow, security rule, failure code,
      performance target, and test obligation against a passing test or reviewed artifact. Confirm
      the diff contains no provider, credential, export, Python migration, multi-profile, auth,
      SQLCipher, Parquet, daemon, WebSocket, or browser-side accounting code.

- [ ] **Step 7: Run the public-artifact privacy gate.** Scan the complete branch diff, unpushed
      commit messages, logs captured by tests, fixtures, benchmark document, generated OpenAPI,
      TUI frames, and screenshots against the private-term denylist and structural heuristics.
      Inspect final screenshots at original resolution and metadata. Require zero private hits.

- [ ] **Step 8: Mark documents complete only after fresh evidence.** Change the design and plan
      statuses to `Implemented and verified`, record exact command results and benchmark environment
      in the benchmark document, rerun markdown/parity/generated-asset checks, and review
      `git diff --check` plus the full staged diff.

- [ ] **Step 9: Run the Required Commit Gate and commit.** Commit with subject
      `chore: verify SQLite editing slice`. Do not push, merge `go-port`, remove Python, or begin a
      provider slice without explicit user direction and its own approved design/plan.
