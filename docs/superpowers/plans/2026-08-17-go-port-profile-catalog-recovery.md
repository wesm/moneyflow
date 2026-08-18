# Go Port Profile Catalog and Recovery Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development
> (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use
> checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add the portable profile catalog, lifecycle locks, and crash-resumable preview-profile
recovery without changing the current interactive startup paths.

**Architecture:** Keep all SQL inside `internal/store/sqlite` behind three narrow maintenance
helpers. Build a provider-neutral `internal/profilecatalog` over private manifests, fixed advisory
locks, and filesystem discovery. Preserve the existing root-level profile in place while new
profiles use opaque-ID subdirectories.

**Tech Stack:** Go 1.26.3, modernc SQLite, `golang.org/x/sys`, Testify, Cobra test seams, Markdown

## Global Constraints

- The approved design is
  `docs/superpowers/specs/2026-08-17-go-port-profile-catalog-onboarding-design.md`.
- `MONEYFLOW_HOME` means catalog root; the legacy profile root is that catalog root.
- Profile IDs are `profile_` plus exactly 26 lowercase unpadded base32 characters from 128 random
  bits.
- Manifest version is `1`; unknown versions are refused without rewrite or recovery.
- Lock order is catalog, then lifecycle, then provider-connect; never acquire in reverse.
- The Go v2 SQLite schema remains install-only. Do not add a schema or payload migration.
- Catalog code imports no provider package and handles no SQL rows or driver types.
- Recovery never applies to `schema_newer` or an unknown manifest version.
- Use TDD and Testify. Commit each verified task without amending earlier commits.
- Build and test only with an isolated temporary `MONEYFLOW_HOME`.

---

### Task 1: Add Catalog Paths and Portable Advisory Locks

**Files:**

- Create: `internal/home/catalog.go`
- Create: `internal/home/lock.go`
- Create: `internal/home/lock_unix.go`
- Create: `internal/home/lock_windows.go`
- Create: `internal/home/lock_test.go`
- Modify: `internal/home/root.go`
- Modify: `internal/home/root_test.go`

**Interfaces:**

- Produces: `home.CatalogPaths{Root, Profiles string}`
- Produces: `home.ResolveCatalogRoot(explicit string, lookupEnv func(string) (string, bool),`
  `userHome string) (CatalogPaths, error)`
- Produces: `home.TryLock(root string, name LockName, mode LockMode) (*Lock, error)`
- Produces: `(*home.Lock).Release() error`
- Preserves: `home.Paths` and `home.ResolveRoot` for existing callers until Plan 2 migrates them

- [ ] **Step 1: Write failing catalog-path and cross-process lock tests**

Add tests that pin the fixed names and shared/exclusive behavior:

```go
func TestResolveCatalogRootKeepsLegacyProfileAtRoot(t *testing.T) {
    base := t.TempDir()
    got, err := ResolveCatalogRoot(base, nil, "")
    require.NoError(t, err)
    assert.Equal(t, base, got.Root)
    assert.Equal(t, filepath.Join(base, "profiles"), got.Profiles)
    assert.Equal(t, filepath.Join(base, "moneyflow.db"), got.LegacyProfile().Database)
}

func TestLifecycleLockAllowsReadersAndRejectsWriter(t *testing.T) {
    root := t.TempDir()
    first, err := TryLock(root, LockProfile, LockShared)
    require.NoError(t, err)
    defer first.Release()
    second, err := TryLock(root, LockProfile, LockShared)
    require.NoError(t, err)
    defer second.Release()
    _, err = TryLock(root, LockProfile, LockExclusive)
    assert.ErrorIs(t, err, ErrLockBusy)
}
```

Use a helper-process test with `os/exec` for cross-process conflict and process-death release. Add
Windows and Unix path/permission assertions behind the repository's existing platform helpers.

- [ ] **Step 2: Run the focused tests and verify RED**

Run:

```bash
go test ./internal/home -run 'Test(ResolveCatalogRoot|LifecycleLock|ProviderConnectLock|LockReleased)' -count=1
```

Expected: FAIL because catalog paths and lock APIs do not exist.

- [ ] **Step 3: Implement fixed-name identity-safe locks**

Define only the fixed public surface:

```go
type LockName uint8
const (
    LockCatalog LockName = iota + 1
    LockProfile
    LockProviderConnect
)

type LockMode uint8
const (
    LockShared LockMode = iota + 1
    LockExclusive
)

var ErrLockBusy = errors.New("home lock is held by another process")
```

Map names to `catalog.lock`, `profile.lock`, and `provider-connect.lock`; reject any other value.
Open the root through `os.OpenRoot`, create/open the fixed regular file with mode `0600`, reject
links and non-regular files, and enforce private permissions. Use nonblocking `unix.Flock` in
`lock_unix.go` and `windows.LockFileEx`/`UnlockFileEx` in `lock_windows.go`. `Release` must be
idempotent and join unlock/close errors.

Add `ResolveCatalogRoot` without changing `ResolveRoot`; both reuse `canonicalRoot` and
`PreparePrivateRoot`.

- [ ] **Step 4: Run focused and race tests and verify GREEN**

Run:

```bash
go test ./internal/home -count=1
go test -race ./internal/home -count=1
```

Expected: PASS on the current platform. Cross-compile the lock files:

```bash
GOOS=windows GOARCH=amd64 go test -c ./internal/home -o /tmp/moneyflow-home-windows.test.exe
GOOS=linux GOARCH=amd64 go test -c ./internal/home -o /tmp/moneyflow-home-linux.test
```

Expected: both compile successfully.

- [ ] **Step 5: Commit the home boundary**

Run `gofmt` on the changed Go files, inspect `git diff --check`, then commit:

```bash
git add internal/home/catalog.go internal/home/lock.go internal/home/lock_unix.go \
  internal/home/lock_windows.go internal/home/lock_test.go internal/home/root.go \
  internal/home/root_test.go
git commit -m "feat: add portable profile lifecycle locks"
```

### Task 2: Expose Narrow SQLite Maintenance Helpers

**Files:**

- Create: `internal/store/sqlite/maintenance.go`
- Create: `internal/store/sqlite/maintenance_test.go`
- Modify: `internal/store/sqlite/initialize.go`
- Modify: `internal/store/sqlite/open.go`

**Interfaces:**

- Consumes: `home.Paths`, `sqlite.Options`, current schema installer and driver-error mapping
- Produces: `sqlite.InspectProfile(context.Context, home.Paths, sqlite.Options) (Inspection, error)`
- Produces: `sqlite.CheckpointProfile(context.Context, home.Paths, sqlite.Options) error`
- Produces: `sqlite.InstallPristineProfile(context.Context, home.Paths, sqlite.Options) error`

- [ ] **Step 1: Write failing maintenance-helper tests**

Pin the public data shape and refusal rules:

```go
func TestInspectProfileClassifiesSchemaAndLocalState(t *testing.T) {
    paths := testPaths(t)
    got, err := InspectProfile(context.Background(), paths, DefaultOptions)
    require.NoError(t, err)
    assert.Equal(t, SchemaEmpty, got.Schema)

    profile, err := Open(context.Background(), paths, DefaultOptions)
    require.NoError(t, err)
    require.NoError(t, profile.Close())
    got, err = InspectProfile(context.Background(), paths, DefaultOptions)
    require.NoError(t, err)
    assert.Equal(t, SchemaCurrent, got.Schema)
    assert.True(t, got.Pristine)
}

func TestInstallPristineProfileRefusesNonemptyDatabase(t *testing.T) {
    paths := seededPaths(t)
    err := InstallPristineProfile(context.Background(), paths, DefaultOptions)
    assert.ErrorIs(t, err, ErrMaintenanceWouldOverwrite)
}
```

Add cases for older/newer metadata, malformed SQLite, provider binding, journal-only state, WAL
checkpoint, and an empty zero-byte database.

- [ ] **Step 2: Run the maintenance tests and verify RED**

Run:

```bash
go test ./internal/store/sqlite -run 'Test(InspectProfile|CheckpointProfile|InstallPristine)' -count=1
```

Expected: FAIL because the exported helper types/functions do not exist.

- [ ] **Step 3: Implement helpers while keeping SQL private**

Define:

```go
type SchemaStatus string
const (
    SchemaEmpty SchemaStatus = "empty"
    SchemaCurrent SchemaStatus = "current"
    SchemaOlder SchemaStatus = "older"
    SchemaNewer SchemaStatus = "newer"
)

type Inspection struct {
    Schema       SchemaStatus
    Pristine     bool
    Bound        bool
    ProviderKind string
}
```

`InspectProfile` first hardens and pins the existing database, then opens a bounded read-only,
query-only driver connection that cannot create sidecars. It calls the existing schema inspector,
and queries only revision, committed-row existence, journal existence, and provider binding for a
current schema. Map corrupt/invalid driver results through existing store errors. Do not load
transactions.

`CheckpointProfile` opens the database without calling `ensureCurrentSchema`, executes
`PRAGMA wal_checkpoint(TRUNCATE)`, closes every connection, and works for older schemas.
`InstallPristineProfile` accepts only a missing or zero-byte database, calls the exact current
installer, verifies `Inspection{Schema: SchemaCurrent, Pristine: true}`, and closes it.

- [ ] **Step 4: Run SQLite gates and verify GREEN**

Run:

```bash
go test ./internal/store/sqlite -run 'Test(InspectProfile|CheckpointProfile|InstallPristine|OpenInstallsOnlyCurrentSchema|OpenRejectsIncompatibleSchema)' -count=1
make test-store
```

Expected: PASS, including the existing install-only schema tests.

- [ ] **Step 5: Commit the maintenance boundary**

```bash
git add internal/store/sqlite/maintenance.go internal/store/sqlite/maintenance_test.go \
  internal/store/sqlite/initialize.go internal/store/sqlite/open.go
git commit -m "feat: expose profile maintenance operations"
```

### Task 3: Add Strict Profile IDs, Manifests, and Errors

**Files:**

- Create: `internal/profilecatalog/errors.go`
- Create: `internal/profilecatalog/id.go`
- Create: `internal/profilecatalog/manifest.go`
- Create: `internal/profilecatalog/manifest_test.go`

**Interfaces:**

- Produces: `profilecatalog.NewProfileID(io.Reader) (string, error)`
- Produces: `profilecatalog.Manifest`, `profilecatalog.ReadManifest`, a version probe, and private
  atomic writer
- Produces: stable catalog error codes and `profilecatalog.CodeOf(error)`

- [ ] **Step 1: Write failing ID, manifest, and error tests**

```go
func TestNewProfileIDUsesInjected128BitRandomness(t *testing.T) {
    random := bytes.NewReader(bytes.Repeat([]byte{0x42}, 16))
    id, err := NewProfileID(random)
    require.NoError(t, err)
    assert.Regexp(t, `^profile_[a-z2-7]{26}$`, id)
    assert.Zero(t, random.Len())
}

func TestReadManifestRejectsUnknownVersionAndFields(t *testing.T) {
    path := writeManifestFixture(t,
        `{"manifest_version":2,"profile_id":"profile_aaaaaaaaaaaaaaaaaaaaaaaaaa"}`)
    _, err := ReadManifest(path)
    assert.Equal(t, CodeManifestUnsupported, mustCode(t, err))
}
```

Cover duplicate JSON keys with a token-level decoder, trailing JSON, 16 KiB maximum size,
ID/directory mismatch, UTC canonical time, provider enum, 1-80 code points, 320-byte name limit,
controls, and NFKC/case-fold collision keys.

- [ ] **Step 2: Run the package tests and verify RED**

Run:

```bash
go test ./internal/profilecatalog -run 'Test(NewProfileID|ReadManifest|WriteManifest|DisplayName)' -count=1
```

Expected: FAIL because the package does not exist.

- [ ] **Step 3: Implement the strict version-one format**

Define the exact manifest:

```go
const ManifestVersion = uint16(1)
const ManifestFilename = "profile.json"

type Manifest struct {
    ManifestVersion uint16    `json:"manifest_version"`
    ProfileID       string    `json:"profile_id"`
    DisplayName     string    `json:"display_name"`
    ProviderKind    string    `json:"provider_kind"`
    CreatedAt       time.Time `json:"created_at"`
    CreatedByVersion string   `json:"created_by_version"`
}
```

Use raw unpadded base32 with `strings.ToLower`. Reuse domain display normalization and collision
keys after enforcing the profile-specific limits. Write manifests through `home.WritePrivateFile`.
Define the spec's stable codes as a typed error; messages contain no profile names or paths:

```go
const (
    CodeProfileNotFound = "profile_not_found"
    CodeProfileAmbiguous = "profile_ambiguous"
    CodeProfileNameConflict = "profile_name_conflict"
    CodeProfileInvalid = "profile_invalid"
    CodeManifestUnsupported = "profile_manifest_unsupported"
    CodeProfileBusy = "profile_busy"
    CodeRecoveryIncomplete = "profile_recovery_incomplete"
    CodeRecoveryUnavailable = "profile_recovery_unavailable"
)
```

The bounded token scanner exposes only `manifest_version` before full decoding. Version one must
pass `ReadManifest`; an unknown version returns `CodeManifestUnsupported` without interpreting its
remaining schema. Discovery lists an unknown-version nested profile under its canonical directory
ID and lists an unknown-version legacy root as `Moneyflow`; it never trusts or rewrites the unknown
manifest's display-name or provider fields.

- [ ] **Step 4: Run package tests and verify GREEN**

Run:

```bash
go test ./internal/profilecatalog -count=1
```

Expected: PASS.

- [ ] **Step 5: Commit the manifest contract**

```bash
git add internal/profilecatalog/errors.go internal/profilecatalog/id.go \
  internal/profilecatalog/manifest.go internal/profilecatalog/manifest_test.go
git commit -m "feat: define private profile manifests"
```

### Task 4: Discover, Inspect, Sort, and Resolve Profiles

**Files:**

- Create: `internal/profilecatalog/catalog.go`
- Create: `internal/profilecatalog/discovery.go`
- Create: `internal/profilecatalog/catalog_test.go`
- Modify: `internal/provider/architecture_test.go`

**Interfaces:**

- Consumes: Tasks 1-3 and an injected `SessionPresence` callback
- Produces: `profilecatalog.New(Config) (*Catalog, error)`
- Produces: `(*Catalog).List(context.Context) ([]Entry, error)`
- Produces: `(*Catalog).Resolve(context.Context, string) (Entry, error)`
- Produces: `Entry.ProfilePaths() home.Paths`

- [ ] **Step 1: Write failing discovery and resolution tests**

Use temp roots for manifest profiles and a root-level legacy database:

```go
func TestListIncludesManifestlessLegacyAndSortsByCollisionKey(t *testing.T) {
    catalog := testCatalog(t)
    installLegacyCurrentProfile(t, catalog.Root())
    createManifestProfile(t, catalog, "Zulu")
    createManifestProfile(t, catalog, "alpha")

    entries, err := catalog.List(context.Background())
    require.NoError(t, err)
    assert.Equal(t, []string{"alpha", "Moneyflow", "Zulu"}, entryNames(entries))
    assert.Equal(t, LegacyKey, entries[1].Key)
}

func TestResolveAcceptsExactNormalizedNameOrCanonicalID(t *testing.T) {
    catalog := testCatalog(t)
    entry := createManifestProfile(t, catalog, "Household")
    byName, err := catalog.Resolve(context.Background(), "  HOUSEHOLD ")
    require.NoError(t, err)
    byID, err := catalog.Resolve(context.Background(), entry.ID)
    require.NoError(t, err)
    assert.Equal(t, entry.ID, byName.ID)
    assert.Equal(t, entry.ID, byID.ID)
}
```

Add local status matrices for bound/session, bound/no-session, pristine, local-only, older,
newer, corrupt, unknown manifest, and active recovery. Assert the injected session inspector is
never a network operation.

- [ ] **Step 2: Run focused tests and verify RED**

Run:

```bash
go test ./internal/profilecatalog -run 'Test(List|Resolve|Status|Legacy)' -count=1
go test ./internal/provider -run TestProviderPackagesDoNotImportStore -count=1
```

Expected: catalog tests FAIL because discovery is absent; the existing architecture test passes.

- [ ] **Step 3: Implement provider-neutral discovery**

Define:

```go
type SessionPresence func(profileRoot, providerKind string) (bool, error)

type Config struct {
    Paths CatalogPaths
    Random io.Reader
    Now func() time.Time
    Version string
    InspectSession SessionPresence
}

type Entry struct {
    Key, ID, DisplayName, ProviderKind string
    Root string
    Status Status
}

type Status string
const (
    StatusReady Status = "ready"
    StatusReconnect Status = "reconnect"
    StatusSetupIncomplete Status = "setup_incomplete"
    StatusLocalOnly Status = "local_only"
    StatusNeedsRecovery Status = "needs_recovery"
    StatusRequiresNewer Status = "requires_newer_moneyflow"
    StatusManifestUnsupported Status = "manifest_unsupported"
)
```

Scan only the catalog root legacy file and direct children of `profiles/`. Reject redirected
directories and malformed canonical IDs. Use `sqlite.InspectProfile` for shallow status. Use the
injected callback only to check local session-file presence. Represent manifest-less legacy as
`Key == "legacy"` and `ID == ""`; do not accept `legacy` as a persisted manifest ID.

Extend `internal/provider/architecture_test.go` so production files below
`internal/profilecatalog` cannot import `internal/provider` or a child package.

- [ ] **Step 4: Run package and architecture tests and verify GREEN**

```bash
go test ./internal/profilecatalog ./internal/provider -count=1
```

Expected: PASS.

- [ ] **Step 5: Commit catalog discovery**

```bash
git add internal/profilecatalog/catalog.go internal/profilecatalog/discovery.go \
  internal/profilecatalog/catalog_test.go internal/provider/architecture_test.go
git commit -m "feat: discover named moneyflow profiles"
```

### Task 5: Create Profiles, Finalize Legacy Manifests, and Roll Back Canceled Adds

**Files:**

- Create: `internal/profilecatalog/create.go`
- Create: `internal/profilecatalog/create_test.go`
- Modify: `internal/profilecatalog/catalog.go`

**Interfaces:**

- Produces: `(*Catalog).Create(context.Context, CreateRequest) (Entry, error)`
- Produces: `(*Catalog).FinalizeLegacyManifest(context.Context, LegacyManifestRequest) (Entry, error)`
- Produces: `(*Catalog).CancelNewProfile(context.Context, string) (bool, error)`

- [ ] **Step 1: Write failing atomicity and lock-order tests**

```go
func TestCreateInstallsCurrentPristineProfileUnderOpaqueID(t *testing.T) {
    catalog := testCatalog(t)
    entry, err := catalog.Create(context.Background(), CreateRequest{
        DisplayName: "Primary", ProviderKind: "monarch",
    })
    require.NoError(t, err)
    assert.Regexp(t, `^profile_[a-z2-7]{26}$`, entry.ID)
    inspection, err := sqlite.InspectProfile(context.Background(), entry.ProfilePaths(), sqlite.DefaultOptions)
    require.NoError(t, err)
    assert.True(t, inspection.Pristine)
}

func TestCancelNewProfileRefusesSessionVaultOrLocalState(t *testing.T) {
    for _, artifact := range []string{"session.json", "credentials.enc", "journal", "committed"} {
        t.Run(artifact, func(t *testing.T) {
            catalog, entry := catalogProfileWithArtifact(t, artifact)
            before := snapshotProfileTree(t, entry.Root)
            removed, err := catalog.CancelNewProfile(context.Background(), entry.ID)
            require.NoError(t, err)
            assert.False(t, removed)
            assert.Equal(t, before, snapshotProfileTree(t, entry.Root))
        })
    }
}
```

Add a manifest-less legacy test that asserts catalog lock is acquired before shared lifecycle
lock, includes `Moneyflow` in conflict detection, and writes exactly one canonical manifest.

- [ ] **Step 2: Run create tests and verify RED**

```bash
go test ./internal/profilecatalog -run 'Test(Create|CancelNewProfile|FinalizeLegacy)' -count=1
```

Expected: FAIL because mutation methods are absent.

- [ ] **Step 3: Implement exact owned-root mutations**

Creation holds catalog exclusive then new-profile lifecycle exclusive, creates a private opaque
directory, installs the current schema, writes the manifest, fsyncs the parent, and removes only
the exact newly created root on failure.

Legacy finalization is a special open path: acquire catalog exclusive, then profile shared, repeat
discovery and conflict checks including the synthetic `Moneyflow` entry, open successfully, write
the manifest, and release in reverse order.

Cancel cleanup requires: nested catalog-owned profile ID, current pristine inspection, no
`session.json`, no `credentials.enc`, and no unexpected files beyond fixed lock/manifest/database
artifacts. Otherwise return `false` without removal.

- [ ] **Step 4: Run mutation and existing store tests and verify GREEN**

```bash
go test ./internal/profilecatalog ./internal/home ./internal/store/sqlite -count=1
```

Expected: PASS.

- [ ] **Step 5: Commit catalog mutations**

```bash
git add internal/profilecatalog/create.go internal/profilecatalog/create_test.go \
  internal/profilecatalog/catalog.go
git commit -m "feat: create and resume named profiles"
```

### Task 6: Implement Marker-Driven Recovery Roll-Forward

**Files:**

- Create: `internal/profilecatalog/recovery.go`
- Create: `internal/profilecatalog/recovery_test.go`
- Create: `internal/profilecatalog/recovery_fault_test.go`
- Modify: `internal/profilecatalog/discovery.go`

**Interfaces:**

- Produces: `(*Catalog).RecoveryPlan(context.Context, string) (RecoveryPlan, error)`
- Produces: `(*Catalog).Recreate(context.Context, RecoveryRequest) (RecoveryResult, error)`
- Produces: an injected fault boundary used only by tests

- [ ] **Step 1: Write failing decision-table and fault-injection tests**

Model every authoritative file combination:

```go
func TestRecoveryRollForwardUsesBackupMainAsDisambiguator(t *testing.T) {
    tests := []struct {
        name string
        backupMain, originalMain bool
        originalKind string
        want action
    }{
        {"old main not moved", false, true, "older", actionMoveOld},
        {"old main moved", true, false, "missing", actionInstall},
        {"empty replacement", true, true, "empty", actionInstall},
        {"current pristine replacement", true, true, "current-pristine", actionFinish},
        {"ambiguous replacement", true, true, "older", actionRefuse},
    }
    for _, test := range tests {
        t.Run(test.name, func(t *testing.T) {
            state := materializeRecoveryState(t, test.backupMain, test.originalMain,
                test.originalKind)
            assert.Equal(t, test.want, recoveryAction(state))
        })
    }
}
```

Add fault points after marker write, WAL rename, SHM rename, main rename, empty-file creation,
schema install, verification, and marker removal. For every point, restart repeatedly and assert
one current pristine database, one intact backup, unchanged session/vault bytes, and no active
marker.

- [ ] **Step 2: Run recovery tests and verify RED**

```bash
go test ./internal/profilecatalog -run 'TestRecovery' -count=1
```

Expected: FAIL because recovery is absent.

- [ ] **Step 3: Implement explicit confirmation and idempotent roll-forward**

Strictly encode marker version one with a canonical profile ID, UTC start time, application version, and the
original allowlisted store code. Under exclusive lifecycle lock:

For a manifestless legacy profile, generate that canonical ID before marker creation, persist it in
the marker, and adopt it unchanged when the recovered profile receives its manifest.

1. call `sqlite.CheckpointProfile` when safe;
2. create and fsync `recovery/<UTC nanosecond timestamp>/` and its marker;
3. rename WAL and SHM before main, syncing source and destination after every rename;
4. call `sqlite.InstallPristineProfile` only after backup main exists;
5. durably install and verify current/pristine inspection; and
6. remove and fsync the marker.

Treat backup-main presence as the sole old/new disambiguator. Refuse multiple/invalid markers,
redirected recovery paths, same-time backup collision, newer schema, unknown manifest, and any
nonempty noncurrent replacement once backup main exists. Store no display name or path in errors.

- [ ] **Step 4: Run recovery, race, and store tests and verify GREEN**

```bash
go test ./internal/profilecatalog -run 'TestRecovery' -count=1
go test -race ./internal/profilecatalog ./internal/home -count=1
make test-store
```

Expected: PASS.

- [ ] **Step 5: Commit recovery**

```bash
git add internal/profilecatalog/recovery.go internal/profilecatalog/recovery_test.go \
  internal/profilecatalog/recovery_fault_test.go internal/profilecatalog/discovery.go
git commit -m "feat: recover incompatible preview profiles"
```

### Task 7: Preserve Existing Commands and Close Plan 1

**Files:**

- Modify: `cmd/moneyflow/profile.go`
- Modify: `cmd/moneyflow/profile_test.go`
- Modify: `internal/provider/architecture_test.go`
- Modify: `AGENTS.md` only if a newly discovered durable rule is required

**Interfaces:**

- Consumes: the catalog without changing selector-first startup yet
- Produces: existing default command behavior backed by the catalog's sole-profile resolution

- [ ] **Step 1: Write failing compatibility tests**

Add command tests that set an isolated catalog root and prove the existing root-level profile still
opens, a sole nested profile resolves when legacy is absent, and multiple persistent profiles fail
headless default selection without opening either database.

```go
func TestOpenProfileUsesSolePersistentCatalogEntry(t *testing.T) {
    root := t.TempDir()
    entry := createCommandCatalogProfile(t, root, "Primary")
    opened, err := openProfile(context.Background(), ProfileOptions{ExplicitHome: root})
    require.NoError(t, err)
    defer opened.Close()
    assert.Equal(t, entry.ProfilePaths().Database, opened.Path)
}
```

- [ ] **Step 2: Run command and architecture tests and verify RED**

```bash
PLAN1_TEST_HOME="$(mktemp -d)"
MONEYFLOW_HOME="$PLAN1_TEST_HOME" go test ./cmd/moneyflow ./internal/provider -count=1
```

Expected: new compatibility tests FAIL while old tests remain green.

- [ ] **Step 3: Route existing default opens through catalog resolution**

Keep `ProfileOptions` source-compatible. Resolve catalog root, use legacy when it is the only
profile, use the sole nested profile when it is the only profile, and return a sanitized ambiguity
error for multiple profiles. Do not add selector UI, onboarding, or `--profile` flags in this plan.

Extend architecture assertions for the final Plan 1 import graph. Do not modify provider/store
rules.

- [ ] **Step 4: Run the complete required verification**

Run with a new temporary home:

```bash
PLAN1_VERIFY_HOME="$(mktemp -d)"
MONEYFLOW_HOME="$PLAN1_VERIFY_HOME" MONEYFLOW_SKIP_PERF=1 make verify-go
MONEYFLOW_HOME="$PLAN1_VERIFY_HOME" uv run pytest -v
uv run pyright moneyflow/
uv run ruff format --check moneyflow/ tests/
uv run ruff check moneyflow/ tests/
npx --yes markdownlint-cli@0.47.0 --config .markdownlint.json README.md 'docs/**/*.md'
.github/scripts/check-arrow-lists.sh
```

Expected: all commands pass. Inspect `git diff --check` and confirm the diff contains no schema
migration, provider write-back, or renderer wizard.

- [ ] **Step 5: Commit the Plan 1 integration**

```bash
git add cmd/moneyflow/profile.go cmd/moneyflow/profile_test.go \
  internal/provider/architecture_test.go AGENTS.md
git commit -m "feat: resolve profiles through the catalog"
```

Record the final commit and verify `git status --short` is empty before starting Plan 2.
