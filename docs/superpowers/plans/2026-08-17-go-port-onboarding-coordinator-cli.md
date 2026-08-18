# Go Port Onboarding Coordinator and CLI Migration Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development
> (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use
> checkbox (`- [ ]`) syntax for tracking.

**Goal:** Move Monarch connection out of Cobra into a renderer-neutral, resumable state machine and
migrate the advanced provider commands onto the shared workflow.

**Architecture:** `internal/onboarding` owns versioned attempts, credential/session decisions,
authentication, import progress, and runtime installation. It accepts injected profile and Monarch
runtime factories. Cobra becomes a synchronous presenter that prompts or waits according to the
same snapshots that TUI and web will consume later.

**Tech Stack:** Go 1.26.3, Cobra, existing Monarch GraphQL adapter, Argon2id/AES-GCM vault,
Bubble Tea-independent state, Testify

## Global Constraints

- Plan 1 and the approved design spec are required inputs.
- State-machine protocol version is exactly `1`.
- State names and stable errors must match the design spec exactly.
- Transition order is inspect, retained-session validation, settings if absent, credentials only
  when authentication is required, authentication, import, complete.
- Import configuration precedence is binding, saved session, then confirmed user input.
- A valid retained session proceeds without a credential vault.
- Secrets never enter snapshots, errors, logs, URLs, manifests, or SQLite.
- One OS advisory provider-connect lock is held for each running attempt.
- Existing provider refresh revision/generation rules remain authoritative.
- Use TDD and Testify. Commit each verified task without amending earlier commits.
- Run all tests with an isolated temporary catalog root.

---

### Task 1: Define Versioned Attempts, Snapshots, Actions, and Errors

**Files:**

- Create: `internal/onboarding/errors.go`
- Create: `internal/onboarding/types.go`
- Create: `internal/onboarding/attempts.go`
- Create: `internal/onboarding/attempts_test.go`

**Interfaces:**

- Produces: `onboarding.ProtocolVersion == 1`
- Produces: the exact `State`, `ActionType`, `Snapshot`, `Progress`, and `Failure` types
- Produces: `Start(context.Context, StartRequest) (Snapshot, error)`
- Produces: `Status(context.Context, StatusRequest) (Snapshot, error)`
- Produces: `Submit(context.Context, SubmitRequest) (Snapshot, error)`
- Produces: `Cancel(context.Context, CancelRequest) (Snapshot, error)`

- [ ] **Step 1: Write failing attempt-lifecycle tests**

```go
func TestAttemptRejectsStaleAndDuplicateTransition(t *testing.T) {
    coordinator, started := coordinatorAtRetryableFailure(t)

    next, err := coordinator.Submit(context.Background(), SubmitRequest{
        ProfileID: testProfileID, AttemptID: started.AttemptID,
        ExpectedStateVersion: started.StateVersion, Action: ActionRetry,
    })
    require.NoError(t, err)
    _, err = coordinator.Submit(context.Background(), SubmitRequest{
        ProfileID: testProfileID, AttemptID: started.AttemptID,
        ExpectedStateVersion: started.StateVersion, Action: ActionRetry,
    })
    assert.Equal(t, CodeOnboardingStale, mustCode(t, err))
    assert.Greater(t, next.StateVersion, started.StateVersion)
}

func TestRunningJobAndStatusPollKeepAttemptActive(t *testing.T) {
    coordinator, started, clock, finishJob := coordinatorWithRunningJob(t)
    clock.Advance(31 * time.Minute)
    _, err := coordinator.Status(context.Background(), StatusRequest{
        ProfileID: testProfileID, AttemptID: started.AttemptID,
    })
    require.NoError(t, err)
    finishJob()

    clock.Advance(29 * time.Minute)
    _, err = coordinator.Status(context.Background(), StatusRequest{
        ProfileID: testProfileID, AttemptID: started.AttemptID,
    })
    require.NoError(t, err)
    clock.Advance(31 * time.Minute)
    _, err = coordinator.Status(context.Background(), StatusRequest{
        ProfileID: testProfileID, AttemptID: started.AttemptID,
    })
    assert.Equal(t, CodeOnboardingExpired, mustCode(t, err))
}
```

Add tests for profile mismatch, wrong coordinator instance, cancel idempotence, 30-minute idle
expiry, server restart (new coordinator), and snapshots containing no secret-bearing fields.

- [ ] **Step 2: Run focused tests and verify RED**

```bash
go test ./internal/onboarding -run 'Test(Attempt|RunningJob|StatusPoll)' -count=1
```

Expected: FAIL because `internal/onboarding` does not exist.

- [ ] **Step 3: Implement the public state vocabulary and guarded map**

Define the exact states:

```go
const ProtocolVersion = uint16(1)

type State string
const (
    StateInspect State = "inspect"
    StateValidateSession State = "validate_session"
    StateSettingsRequired State = "settings_required"
    StateUnlockRequired State = "unlock_required"
    StateCredentialsRequired State = "credentials_required"
    StateAuthenticating State = "authenticating"
    StateImporting State = "importing"
    StateComplete State = "complete"
    StateLocalOnly State = "local_only"
    StateIdentityMismatch State = "identity_mismatch"
    StateFailed State = "failed"
    StateCanceled State = "canceled"
)

type ActionType string
const (
    ActionConfirmSettings ActionType = "confirm_settings"
    ActionUnlock ActionType = "unlock"
    ActionSubmitCredentials ActionType = "submit_credentials"
    ActionRetry ActionType = "retry"
    ActionReauthenticate ActionType = "reauthenticate"
)

const (
    CodeOnboardingStale = "onboarding_stale"
    CodeOnboardingExpired = "onboarding_expired"
    CodeOnboardingCanceled = "onboarding_canceled"
    CodeOnboardingLocalOnly = "onboarding_local_only"
    CodeCredentialUnlockFailed = "credential_unlock_failed"
    CodeCredentialInputInvalid = "credential_input_invalid"
    CodeProviderConnectInProgress = "provider_connect_in_progress"
)

type Settings struct {
    Currency domain.Currency
    Scale uint8
}

type Progress struct {
    Phase, Partition string
    Fetched, Total, Attempt, Pass int
    Elapsed time.Duration
}

type Failure struct {
    Code, Message string
    CanRetry, CanReenter bool
}

type Snapshot struct {
    ProtocolVersion uint16
    AttemptID, ProfileID string
    StateVersion uint64
    State State
    ProviderKind string
    Settings *Settings
    Progress *Progress
    Failure *Failure
}

type SettingsInput struct {
    Currency domain.Currency
    Scale uint8
}

type UnlockInput struct {
    AccountPassword []byte
}

type CredentialInput struct {
    Email, Password, TOTPSecret, AccountPassword, Confirmation []byte
}

type SubmitRequest struct {
    ProfileID, AttemptID string
    ExpectedStateVersion uint64
    Action ActionType
    Settings *SettingsInput
    Unlock *UnlockInput
    Credentials *CredentialInput
}

type StartRequest struct {
    ProfileID string
}

type StatusRequest struct {
    ProfileID, AttemptID string
}

type CancelRequest struct {
    ProfileID, AttemptID string
    ExpectedStateVersion uint64
}
```

`Snapshot` contains protocol/attempt/profile/state versions, state, provider kind, optional exact
settings, counts-only progress, and sanitized failure. It contains no credential fields. Store
attempts behind a mutex; bind lookup to both profile and attempt IDs. Generate 128-bit random IDs.
Treat a successful poll, submit, or running job as activity. Cancel the attempt context but never
reverse durable effects.

- [ ] **Step 4: Run package and race tests and verify GREEN**

```bash
go test ./internal/onboarding -count=1
go test -race ./internal/onboarding -count=1
```

Expected: PASS.

- [ ] **Step 5: Commit the state-machine shell**

```bash
git add internal/onboarding/errors.go internal/onboarding/types.go \
  internal/onboarding/attempts.go internal/onboarding/attempts_test.go
git commit -m "feat: define resumable onboarding attempts"
```

### Task 2: Implement Inspect, Session Validation, and Settings Precedence

**Files:**

- Create: `internal/onboarding/runtime.go`
- Create: `internal/onboarding/flow.go`
- Create: `internal/onboarding/flow_test.go`
- Modify: `internal/onboarding/attempts.go`

**Interfaces:**

- Consumes: `profilecatalog.Catalog`, `app.Service.ProviderConnection`, Monarch session/vault types
- Produces: `ProfileOpener`, `RuntimeFactory`, and renderer-neutral `Runtime`
- Produces: automatic transitions through inspect/session validation to the next input/job state

- [ ] **Step 1: Write failing precedence and terminal-outcome tests**

```go
func TestConfigurationPrecedenceIsBindingThenSessionThenInput(t *testing.T) {
    tests := []struct {
        name string
        binding, session, input *monarch.ImportConfig
        want monarch.ImportConfig
    }{
        {"binding", usd2(), eur2(), gbp2(), *usd2()},
        {"session", nil, eur2(), gbp2(), *eur2()},
        {"input", nil, nil, gbp2(), *gbp2()},
    }
    for _, test := range tests {
        t.Run(test.name, func(t *testing.T) {
            got, err := selectImportConfig(test.binding, test.session, test.input)
            require.NoError(t, err)
            assert.Equal(t, test.want, got)
        })
    }
}

func TestValidSessionWithoutVaultContinuesToImport(t *testing.T) {
    runtime := fakeRuntime{session: validSession(), vaultExists: false}
    snapshot := startAndWait(t, coordinatorWith(runtime))
    assert.Equal(t, StateImporting, snapshot.State)
    assert.Zero(t, runtime.vaultLoads)
}
```

Add: unbound non-pristine becomes `local_only`; absent/expired session routes to unlock or new
credentials; conflicting explicit config fails; identity mismatch enters its named state with no
profile mutation; session config fills absent settings before prompting.

- [ ] **Step 2: Run focused flow tests and verify RED**

```bash
go test ./internal/onboarding -run 'Test(Configuration|ValidSession|LocalOnly|IdentityMismatch|ExpiredSession)' -count=1
```

Expected: FAIL because the workflow is not implemented.

- [ ] **Step 3: Add injected profile/runtime boundaries and transition driver**

Define focused boundaries:

```go
type OpenedProfile struct {
    ID string
    Paths home.Paths
    Service *app.Service
    Close func() error
}

type ProfileOpener func(context.Context, string) (OpenedProfile, error)

type Runtime struct {
    Sessions SessionStore
    Credentials CredentialVault
    NewConnector func(monarch.ImportConfig) (provider.Connector, error)
    NewSource func(monarch.ImportConfig) (provider.Source, error)
    InstanceID string
    Now func() time.Time
}

type RuntimeFactory func(home.Paths) (Runtime, error)
```

Move the session-store and credential-vault interfaces from `cmd/moneyflow/provider.go` into this
package. `Start` opens the profile, takes the provider-connect lock, reads provider connection
state, loads the retained session, applies config precedence, then constructs the connector with
the selected config and validates the session. Construct the source only after config is selected.
This removes the current factory's hidden session read and makes the coordinator the sole owner of
precedence. Advance until input or network work is required. Treat a missing session file as
absence, not a raw failure.

Do not authenticate or refresh in this task.

- [ ] **Step 4: Run flow and existing provider tests and verify GREEN**

```bash
go test ./internal/onboarding ./internal/provider/... -count=1
```

Expected: onboarding tests pass and provider contracts remain unchanged.

- [ ] **Step 5: Commit inspect and session decisions**

```bash
git add internal/onboarding/runtime.go internal/onboarding/flow.go \
  internal/onboarding/flow_test.go internal/onboarding/attempts.go
git commit -m "feat: resolve onboarding session state"
```

### Task 3: Add Credential Unlock, Setup, TOTP, and Authentication

**Files:**

- Create: `internal/onboarding/credentials.go`
- Create: `internal/onboarding/credentials_test.go`
- Create: `internal/onboarding/authenticate.go`
- Create: `internal/onboarding/authenticate_test.go`
- Modify: `internal/onboarding/flow.go`

**Interfaces:**

- Consumes: `ActionUnlock` and `ActionSubmitCredentials` secret payloads
- Produces: automatic TOTP authentication, remote identity validation, and hardened saves

- [ ] **Step 1: Write failing secret and authentication tests**

```go
func TestUnlockClearsSubmittedPasswordAndNeverCopiesItToSnapshot(t *testing.T) {
    coordinator, started := coordinatorAtUnlock(t)
    secret := []byte("account-password")
    request := SubmitRequest{
        ProfileID: testProfileID,
        AttemptID: started.AttemptID,
        ExpectedStateVersion: started.StateVersion,
        Action: ActionUnlock,
        Unlock: &UnlockInput{AccountPassword: secret},
    }
    snapshot, err := coordinator.Submit(context.Background(), request)
    require.NoError(t, err)
    assert.Equal(t, make([]byte, len(secret)), secret)
    encoded, err := json.Marshal(snapshot)
    require.NoError(t, err)
    assert.NotContains(t, string(encoded), "account-password")
}

func TestAuthenticateGeneratesTOTPAndAnswersOneMFAChallenge(t *testing.T) {
    connector := &capturingConnector{challenge: provider.Challenge{Kind: "mfa"}}
    coordinator, started := coordinatorAtCredentials(t, connector, time.Unix(59, 0).UTC())
    submitted := validCredentialRequest(started,
        []byte("GEZDGNBVGY3TQOJQGEZDGNBVGY3TQOJQ"))
    snapshot, err := coordinator.Submit(context.Background(), submitted)
    require.NoError(t, err)
    waitForState(t, coordinator, snapshot, StateImporting)
    assert.Equal(t, "287082", connector.credentials.OneTimeCode)
    assert.Equal(t, "287082", connector.challengeResponse)
    assert.NotContains(t, mustJSON(t, snapshot), "287082")
}
```

Cover wrong vault password (`credential_unlock_failed` and state unchanged), invalid/mismatched
new credential input, authentication failure returning to credential entry with cleared secrets,
binding mismatch before save, and session/vault save order.

- [ ] **Step 2: Run credential tests and verify RED**

```bash
go test ./internal/onboarding -run 'Test(Unlock|Credential|Authenticate|Binding)' -count=1
```

Expected: FAIL because secret transitions do not exist.

- [ ] **Step 3: Implement secret-only actions and authentication job**

Use the action-specific request structs from Task 1 so public snapshots cannot accidentally gain
secret fields. Require exactly one payload matching the action: settings for
`ActionConfirmSettings`, unlock for `ActionUnlock`, credentials for
`ActionSubmitCredentials`, and no payload for retry or reauthenticate.

Validate email/secret/password confirmation, convert to `monarch.StoredCredentials` only inside
the job, generate codes with `monarch.GenerateTOTPCode`, and answer only `mfa` challenges. Validate
the remote identity against any binding before saving. Save session first, then the encrypted vault
when new credentials were supplied. Clear every caller-owned and local mutable secret buffer on all
returns. Map provider errors to sanitized state failures.

- [ ] **Step 4: Run credential, race, and Monarch tests and verify GREEN**

```bash
go test ./internal/onboarding ./internal/provider/monarch -count=1
go test -race ./internal/onboarding -count=1
```

Expected: PASS.

- [ ] **Step 5: Commit credential orchestration**

```bash
git add internal/onboarding/credentials.go internal/onboarding/credentials_test.go \
  internal/onboarding/authenticate.go internal/onboarding/authenticate_test.go \
  internal/onboarding/flow.go
git commit -m "feat: authenticate Monarch through onboarding"
```

### Task 4: Add Import Progress, Retry, Cancellation, Resume, and Runtime Installation

**Files:**

- Create: `internal/onboarding/import.go`
- Create: `internal/onboarding/import_test.go`
- Create: `internal/onboarding/progress.go`
- Modify: `internal/onboarding/flow.go`
- Modify: `internal/onboarding/attempts.go`

**Interfaces:**

- Consumes: `app.Service.ConfigureProvider` and `RefreshProvider`
- Produces: counts-only asynchronous progress and a complete bound `OpenedProfile`

- [ ] **Step 1: Write failing import and resume tests**

```go
func TestImportFailureRetainsSessionAndRetriesWithoutCredentials(t *testing.T) {
    runtime := runtimeFailingFirstRefresh(t)
    failed := authenticateAndWait(t, coordinatorWith(runtime))
    assert.Equal(t, StateFailed, failed.State)
    assert.True(t, failed.Failure.CanRetry)

    complete := submitRetryAndWait(t, coordinator, failed)
    assert.Equal(t, StateComplete, complete.State)
    assert.Equal(t, 1, runtime.connectCalls)
    assert.Equal(t, 2, runtime.refreshCalls)
}

func TestReconnectRequiredDuringImportReturnsToAuthentication(t *testing.T) {
    coordinator, importing, runtime := coordinatorImportingWithReconnectFailure(t)
    failed := waitForVersionAfter(t, coordinator, importing)
    require.NotNil(t, failed.Failure)
    assert.False(t, failed.Failure.CanRetry)
    assert.Equal(t, StateUnlockRequired, failed.State)
    assert.Equal(t, 1, runtime.refreshCalls)
}
```

Add cancellation during fetch, atomic fold behavior, progress phase/count mapping, completed
runtime usable without restart, and crash/resume from a previously saved session.

- [ ] **Step 2: Run import tests and verify RED**

```bash
go test ./internal/onboarding -run 'Test(Import|ReconnectRequired|Cancel|Progress|Runtime)' -count=1
```

Expected: FAIL because import orchestration is absent.

- [ ] **Step 3: Implement the asynchronous import job**

Construct the source from the selected config, then configure the service with that source,
currency, scale, renderer, instance ID, and a progress callback. Translate provider progress into
immutable snapshot copies. Run manual refresh with default view/empty selection. On success retain
the open service and transition to `complete`; the presenter takes ownership through an explicit
`TakeOpenedProfile` method that can succeed only once.

On cancellation, cancel network context and wait for job exit before releasing the provider lock.
Never interrupt or partially undo a SQLite fold. On reconnect-required, invalidate the retained
session path in attempt state and return to validation/authentication. On other retryable import
failures, retain validated session state.

- [ ] **Step 4: Run onboarding, app provider, and race tests and verify GREEN**

```bash
go test ./internal/onboarding ./internal/app -run 'Test.*(Import|Provider|Refresh|Runtime|Cancel)' -count=1
go test -race ./internal/onboarding -count=1
```

Expected: PASS.

- [ ] **Step 5: Commit import orchestration**

```bash
git add internal/onboarding/import.go internal/onboarding/import_test.go \
  internal/onboarding/progress.go internal/onboarding/flow.go internal/onboarding/attempts.go
git commit -m "feat: import Monarch through onboarding"
```

### Task 5: Migrate the Cobra Presenter and Add Profile-Aware Provider Commands

**Files:**

- Create: `cmd/moneyflow/onboarding_presenter.go`
- Create: `cmd/moneyflow/onboarding_presenter_test.go`
- Modify: `cmd/moneyflow/provider.go`
- Modify: `cmd/moneyflow/provider_test.go`
- Modify: `cmd/moneyflow/profile.go`
- Modify: `cmd/moneyflow/root.go`

**Interfaces:**

- Consumes: `onboarding.Coordinator` snapshots/actions and `profilecatalog.Resolve`
- Produces: `--profile NAME_OR_ID` for connect and disconnect
- Preserves: one stdout import summary and stderr prompts/progress/guidance

- [ ] **Step 1: Rewrite command tests against the shared coordinator contract**

Add/adjust tests for:

```go
func TestProviderConnectPromptsToConfirmUSD2WhenConfigIsAbsent(t *testing.T) {
    prompts := &recordingPrompt{answers: []string{"", "", /* credential answers */}}
    stdout, stderr, err := executeProviderConnect(t, prompts, "--profile", "Primary")
    require.NoError(t, err)
    assert.Contains(t, stderr, "Import currency [USD]")
    assert.Contains(t, stderr, "Minor-unit scale [2]")
    assert.Equal(t, "Imported 1 posted transaction.\n", stdout)
}

func TestProviderCommandsResolveSameNameOrID(t *testing.T) {
    catalog, entry, factory := commandCatalogWithProfile(t, "Primary")
    require.NoError(t, executeConnectWithCatalog(t, catalog, factory, "Primary"))
    require.NoError(t, executeDisconnectWithCatalog(t, catalog, factory, entry.ID))
    require.Len(t, factory.paths, 2)
    assert.Equal(t, factory.paths[0], factory.paths[1])
}
```

Retain tests for `--mtd`, explicit config conflicts, valid saved session, credential setup/unlock,
TOTP, progress, initial import failure, and success guidance. Change assertions to observe the
coordinator rather than private Cobra helpers.

- [ ] **Step 2: Run command tests and verify RED**

```bash
PLAN2_CLI_HOME="$(mktemp -d)"
MONEYFLOW_HOME="$PLAN2_CLI_HOME" go test ./cmd/moneyflow -run 'TestProvider' -count=1
```

Expected: FAIL because Cobra still owns connection and lacks profile flags/default prompts.

- [ ] **Step 3: Replace Cobra business logic with a snapshot-driving presenter**

The presenter loop:

```go
for snapshot.State != onboarding.StateComplete {
    switch snapshot.State {
    case onboarding.StateSettingsRequired:
        snapshot = submitCLISettings(...)
    case onboarding.StateUnlockRequired:
        snapshot = submitCLIUnlock(...)
    case onboarding.StateCredentialsRequired:
        snapshot = submitCLICredentials(...)
    case onboarding.StateAuthenticating, onboarding.StateImporting:
        renderCLIProgress(command.ErrOrStderr(), snapshot)
        snapshot = waitForNextSnapshot(...)
    default:
        return cliOnboardingError(snapshot)
    }
}
```

Delete `connectMonarchProfile`, `validateRetainedMonarchSession`, `authenticateMonarch`, and
`monarchLoginCredentials` after their tests pass through the coordinator. Keep factory wiring in
`cmd/moneyflow`, as allowed by the architecture contract. Add identical `--profile` resolution to
disconnect. Omitted profile selects exactly one persistent profile and errors on zero/multiple.
Disconnect holds the same exclusive per-profile provider-connect lock as connect, after the shared
lifecycle lock, so the two operations cannot replace or remove the session concurrently.

- [ ] **Step 4: Run all command and onboarding tests and verify GREEN**

```bash
PLAN2_CLI_HOME="$(mktemp -d)"
MONEYFLOW_HOME="$PLAN2_CLI_HOME" go test ./cmd/moneyflow ./internal/onboarding -count=1
```

Expected: PASS with existing CLI output contracts intact except the approved interactive USD/2
prompt improvement.

- [ ] **Step 5: Commit the Cobra migration**

```bash
git add cmd/moneyflow/onboarding_presenter.go cmd/moneyflow/onboarding_presenter_test.go \
  cmd/moneyflow/provider.go cmd/moneyflow/provider_test.go cmd/moneyflow/profile.go \
  cmd/moneyflow/root.go
git commit -m "refactor: share Monarch onboarding with CLI"
```

### Task 6: Enforce Architecture, Privacy, and Plan 2 Completion

**Files:**

- Modify: `internal/provider/architecture_test.go`
- Create: `internal/onboarding/privacy_test.go`
- Modify: `cmd/moneyflow/provider_test.go`
- Modify: `README.md` only if advanced command examples require `--profile`

**Interfaces:**

- Produces: compile-time-adjacent package boundary and credential-blind failure proof

- [ ] **Step 1: Add failing architecture and privacy scans**

Extend the AST import test so only production files under `internal/onboarding` and the named
factory-wiring files in `cmd/moneyflow` may import `internal/provider/monarch`. Explicitly reject
Monarch imports from TUI/API/web/profilecatalog/store.

Add a table test that submits unique synthetic secrets through every failed coordinator path,
marshals snapshots/errors, captures logs and CLI output, then asserts none contains the secrets,
email, profile display name, or temporary root.

- [ ] **Step 2: Run architecture/privacy tests and verify RED**

```bash
go test ./internal/provider ./internal/onboarding ./cmd/moneyflow \
  -run 'Test(ProviderPackages|OnboardingImports|CredentialBlind|ProviderOutput)' -count=1
```

Expected: FAIL until import allowlists and all sanitized mappings are complete.

- [ ] **Step 3: Tighten boundaries and sanitized mappings**

Move any remaining authentication helper out of `cmd`, ensure typed errors are mapped without raw
causes, and keep logs to the approved allowlist: code, opaque profile ID, revision/generation,
counts, timings, correlation ID. Do not log profile paths or display names.

- [ ] **Step 4: Run the full required verification**

```bash
PLAN2_VERIFY_HOME="$(mktemp -d)"
MONEYFLOW_HOME="$PLAN2_VERIFY_HOME" MONEYFLOW_SKIP_PERF=1 make verify-go
MONEYFLOW_HOME="$PLAN2_VERIFY_HOME" uv run pytest -v
uv run pyright moneyflow/
uv run ruff format --check moneyflow/ tests/
uv run ruff check moneyflow/ tests/
npx --yes markdownlint-cli@0.47.0 --config .markdownlint.json README.md 'docs/**/*.md'
.github/scripts/check-arrow-lists.sh
```

Expected: all checks pass. Confirm the diff contains no TUI wizard, web wizard, provider write-back,
schema migration, or YNAB/SimpleFIN adapter.

- [ ] **Step 5: Commit the Plan 2 gate**

```bash
git add internal/provider/architecture_test.go internal/onboarding/privacy_test.go \
  cmd/moneyflow/provider_test.go README.md
git commit -m "test: enforce shared onboarding boundaries"
```

Record the final commit and verify `git status --short` is empty before starting Plan 3.
