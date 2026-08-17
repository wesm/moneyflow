# Go Port TUI Profile Selector and Onboarding Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development
> (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use
> checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make `moneyflow tui` provide the Python-quality profile selector, recovery, credential,
progress, and reconnect experience before entering the existing finance TUI.

**Architecture:** Add a top-level Bubble Tea shell that owns catalog/profile lifecycle and embeds
the existing `tui.Model` only while a profile is open. Build onboarding screens over the shared
Plan 2 coordinator. Extend semantic parity with isolated Python selector/credential frames and keep
Go styling in reviewed visual goldens.

**Tech Stack:** Go 1.26.3, Bubble Tea v2, Bubbles v2 text input, Lip Gloss v2, Textual oracle,
Testify, parity JSON artifacts

## Global Constraints

- Plans 1 and 2 and the approved design spec are required inputs.
- Ordinary `moneyflow tui` lists profiles before opening one; `--demo` and `--profile` preselect.
- Profile listing performs no network I/O.
- Secret inputs show bullets and never enter model snapshots, logs, parity artifacts, or errors.
- Python frames constrain semantic content/order/keys; Go frames own resolved colors and attributes.
- Minimum supported terminal size remains 80x24.
- Disabled YNAB/SimpleFIN choices are focusable and explain their status.
- Returning to selector closes the open profile and its shared lifecycle lock.
- Use TDD and Testify. Commit each verified task without amending earlier commits.
- Run branch binaries and tests only with isolated temporary profiles.

---

### Task 1: Capture Python Onboarding Semantic Frames Deliberately

**Files:**

- Create: `testdata/parity/onboarding_scenarios.json`
- Create: `testdata/parity/onboarding_semantic_frames/account_selector.json`
- Create: `testdata/parity/onboarding_semantic_frames/provider_selector.json`
- Create: `testdata/parity/onboarding_semantic_frames/credential_setup.json`
- Create: `testdata/parity/onboarding_semantic_frames/credential_unlock.json`
- Create: `moneyflow/parity/onboarding_semantic.py`
- Create: `tests/parity/test_onboarding_semantic.py`
- Create: `internal/parity/onboarding.go`
- Create: `internal/parity/onboarding_test.go`
- Modify: `Makefile`
- Modify: `testdata/parity/embed.go`

**Interfaces:**

- Produces: strict version-one onboarding scenario and semantic-frame documents
- Produces: `make parity-update-python` updates both finance and onboarding Python artifacts
- Produces: `make parity` checks onboarding artifacts without writes

- [ ] **Step 1: Write failing strict-document and isolated-screen tests**

Define scenarios with synthetic names only:

```json
{
  "schema_version": 1,
  "scenarios": [
    {"name":"account_selector","width":100,"height":30,"screen":"account_selector","keys":[]},
    {"name":"provider_selector","width":100,"height":30,"screen":"provider_selector","keys":[]},
    {"name":"credential_setup","width":100,"height":30,"screen":"credential_setup","keys":[]},
    {"name":"credential_unlock","width":100,"height":30,"screen":"credential_unlock","keys":[]}
  ]
}
```

Python tests assert strict keys, duplicate-name rejection, isolated temporary config/profile roots,
synthetic account values, and no network backend calls. Go tests assert strict artifact loading and
embedded files.

- [ ] **Step 2: Run parity tests and verify RED**

```bash
uv run pytest tests/parity/test_onboarding_semantic.py -v
go test ./internal/parity -run TestOnboarding -count=1
```

Expected: FAIL because the adapters and artifacts do not exist.

- [ ] **Step 3: Implement a screen-only Python extractor and strict Go loader**

Use a minimal Textual test app that pushes the real Python
`AccountSelectorScreen`, `BackendSelectionScreen`, `CredentialSetupScreen`, and
`CredentialUnlockScreen`. Supply a temp `AccountManager` with `Example Profile`, a fake backend
config, and no real credential values. Extract style-free region lines, focused row, field labels,
button labels, and key hints; password values must be empty.

In Go, define:

```go
type OnboardingSemanticFrame struct {
    SchemaVersion int `json:"schema_version"`
    Name string `json:"name"`
    Width, Height int
    Lines []string `json:"lines"`
    Focus string `json:"focus"`
    Fields []string `json:"fields"`
    Hints []string `json:"hints"`
}
```

Validate every list and geometry/value bound. Do not reuse finance `FrameInitial`, which assumes an
open application service.

- [ ] **Step 4: Generate and review the deliberate Python artifact diff**

Run:

```bash
make parity-update-python
git diff -- testdata/parity/onboarding_semantic_frames testdata/parity/onboarding_scenarios.json
make parity
```

Expected: update writes only the four synthetic onboarding frames plus the declared scenario file;
`make parity` passes. Inspect every added string for personal data before staging.

- [ ] **Step 5: Commit the reviewed oracle artifacts**

```bash
git add moneyflow/parity/onboarding_semantic.py tests/parity/test_onboarding_semantic.py \
  internal/parity/onboarding.go internal/parity/onboarding_test.go \
  testdata/parity/onboarding_scenarios.json testdata/parity/onboarding_semantic_frames \
  testdata/parity/embed.go Makefile
git commit -m "test: capture Python onboarding semantics"
```

### Task 2: Add the Top-Level TUI Shell and Runner Seam

**Files:**

- Create: `internal/tui/shell.go`
- Create: `internal/tui/shell_test.go`
- Create: `internal/tui/shell_view.go`
- Modify: `internal/tui/runner.go`
- Modify: `internal/tui/model.go`
- Modify: `cmd/moneyflow/root.go`
- Modify: `cmd/moneyflow/root_test.go`

**Interfaces:**

- Consumes: profile catalog/opener and onboarding coordinator from Plans 1-2
- Produces: `tui.ShellDependencies`, `tui.NewShell`, and selector-first `tui.Run`
- Preserves: `tui.NewModel` as the profile-scoped finance model constructor

- [ ] **Step 1: Write failing shell lifecycle tests**

```go
func TestShellStartsAtSelectorWithoutOpeningProfile(t *testing.T) {
    deps := fakeShellDependencies(t)
    shell, err := NewShell(context.Background(), deps, Options{ColorMode: ColorModeNone})
    require.NoError(t, err)
    assert.Equal(t, shellSelector, shell.screen)
    assert.Zero(t, deps.opens)
}

func TestShellClosesFinanceProfileBeforeReturningToSelector(t *testing.T) {
    shell, deps := shellWithOpenFinanceProfile(t)
    next, _ := shell.Update(switchProfileMsg{})
    assert.Equal(t, 1, deps.closes)
    assert.Equal(t, shellSelector, next.(Shell).screen)
}
```

Add preselected demo/profile cases, close error propagation, window-size propagation, force quit,
and child model command routing.

- [ ] **Step 2: Run shell tests and verify RED**

```bash
go test ./internal/tui ./cmd/moneyflow -run 'Test(Shell|TUICommand)' -count=1
```

Expected: FAIL because the TUI still requires an already-opened service.

- [ ] **Step 3: Implement a small routing model**

Define:

```go
type ShellDependencies struct {
    Catalog CatalogView
    OpenProfile ProfileOpener
    Onboarding OnboardingView
    Preselected *OpenedProfile
}

type shellScreen uint8
const (
    shellSelector shellScreen = iota + 1
    shellProvider
    shellName
    shellRecovery
    shellOnboarding
    shellFinance
)
```

`Shell` owns dimensions, palette, current child state, opened-profile close function, and context.
Finance remains the existing `Model`; the shell forwards messages/commands while in
`shellFinance`. Change `tui.Run` to accept `ShellDependencies`. Provide an internal
`RunOpenedProfile` helper for `--demo`/`--profile` tests and OpenAPI-independent callers.

Change `IOStreams.RunTUI` and command construction to inject catalog/opener dependencies rather
than one pre-opened service on ordinary startup. Do not implement selector rows yet.

- [ ] **Step 4: Run focused TUI/command tests and verify GREEN**

```bash
go test ./internal/tui ./cmd/moneyflow -run 'Test(Shell|TUICommand|CommandClosesProfile)' -count=1
```

Expected: PASS.

- [ ] **Step 5: Commit the shell boundary**

```bash
git add internal/tui/shell.go internal/tui/shell_test.go internal/tui/shell_view.go \
  internal/tui/runner.go internal/tui/model.go cmd/moneyflow/root.go cmd/moneyflow/root_test.go
git commit -m "feat: add profile-neutral TUI shell"
```

### Task 3: Implement the Keyboard-First Profile and Provider Selectors

**Files:**

- Create: `internal/tui/profile_selector.go`
- Create: `internal/tui/profile_selector_test.go`
- Create: `internal/tui/provider_selector.go`
- Create: `internal/tui/provider_selector_test.go`
- Modify: `internal/tui/shell.go`
- Modify: `internal/tui/shell_view.go`

**Interfaces:**

- Consumes: `profilecatalog.Entry` and local statuses only
- Produces: selector actions for open, demo, add, exit, recovery, and guidance

- [ ] **Step 1: Write failing key and status-routing tests**

```go
func TestProfileSelectorDirectKeysMatchPython(t *testing.T) {
    selector := selectorWithEntries(t)
    assert.Equal(t, selectorDemo, pressSelector(t, selector, "d").action)
    assert.Equal(t, selectorAdd, pressSelector(t, selector, "a").action)
    assert.Equal(t, selectorAdd, pressSelector(t, selector, "n").action)
    assert.Equal(t, selectorExit, pressSelector(t, selector, "q").action)
}

func TestProviderSelectorDisabledRowsExplainAvailability(t *testing.T) {
    selector := newProviderSelector()
    selected := pressProvider(t, selector, "y")
    assert.Equal(t, "YNAB is not available in Go yet.", selected.status)
    assert.Equal(t, providerYNAB, selected.focused)
}
```

Cover arrows, `j`/`k`, Home, Enter, Escape; alphabetical status labels; Ready/Reconnect/Setup
incomplete/Local only/Needs recovery/Requires newer/Unsupported routing; empty catalog; narrow
80x24 rendering; and no catalog network call.

- [ ] **Step 2: Run selector tests and verify RED**

```bash
go test ./internal/tui -run 'Test(ProfileSelector|ProviderSelector)' -count=1
```

Expected: FAIL because selectors are absent.

- [ ] **Step 3: Implement deterministic selector state and rendering**

Keep selector state free of services and provider clients:

```go
type profileSelectorState struct {
    entries []profilecatalog.Entry
    cursor int
    status string
}
```

Append fixed Demo/Add/Exit rows after catalog entries. Show provider and local status without
probing remote session validity. Demo opens directly; Add opens the provider selector; activating
Monarch then opens the display-name form, preserving the exact provider -> display name -> durable
profile order. Map existing-entry activation to shell messages; Local only opens a small Open
Offline/Back choice, and newer/unsupported states show guidance without destructive actions.

Provider selection lists Monarch, YNAB, SimpleFIN. Only Monarch advances. Disabled choices remain
in the focus ring and announce why they cannot continue.

- [ ] **Step 4: Run selectors and semantic projection tests and verify GREEN**

```bash
go test ./internal/tui -run 'Test(ProfileSelector|ProviderSelector|Shell)' -count=1
make parity
```

Expected: PASS; existing finance parity remains unchanged.

- [ ] **Step 5: Commit selectors**

```bash
git add internal/tui/profile_selector.go internal/tui/profile_selector_test.go \
  internal/tui/provider_selector.go internal/tui/provider_selector_test.go \
  internal/tui/shell.go internal/tui/shell_view.go
git commit -m "feat: add TUI profile selection"
```

### Task 4: Add Profile Naming, Cancel Rollback, and Recovery Screens

**Files:**

- Create: `internal/tui/profile_name.go`
- Create: `internal/tui/profile_name_test.go`
- Create: `internal/tui/profile_recovery.go`
- Create: `internal/tui/profile_recovery_test.go`
- Modify: `internal/tui/shell.go`
- Modify: `internal/tui/shell_view.go`

**Interfaces:**

- Consumes: `profilecatalog.Create`, `CancelNewProfile`, `RecoveryPlan`, and `Recreate`
- Produces: a selected pristine profile or return to selector with preserved backup guidance

- [ ] **Step 1: Write failing create/cancel/recovery screen tests**

```go
func TestCancelNameRemovesNothingAndCancelCredentialsRollsBackPristineProfile(t *testing.T) {
    shell, deps := shellAtMonarchNameForm(t)
    shell = submitProfileName(t, shell, "Primary")
    assert.Equal(t, 1, deps.creates)
    shell = pressShell(t, shell, "esc")
    assert.Equal(t, 1, deps.cancelNewCalls)
    assert.Equal(t, shellSelector, shell.screen)
}

func TestNewerProfileNeverOffersRecreate(t *testing.T) {
    screen := recoveryScreenFor(profilecatalog.StatusRequiresNewer)
    rendered := screen.View()
    assert.NotContains(t, rendered, "Recreate")
    assert.Contains(t, rendered, "newer Moneyflow")
}
```

Cover invalid/conflicting names, setup-incomplete resume, explicit recovery confirmation,
profile-busy refusal, progress through roll-forward, backup path display, incomplete recovery, and
successful transition to ordinary onboarding.

- [ ] **Step 2: Run screen tests and verify RED**

```bash
go test ./internal/tui -run 'Test(ProfileName|CancelName|Recovery|NewerProfile)' -count=1
```

Expected: FAIL because screens are absent.

- [ ] **Step 3: Implement forms and asynchronous catalog commands**

Use Bubbles text input for the profile name, with the domain/catalog validator as the authority.
Create only on Enter. Track whether this shell created the profile; Escape from onboarding calls
`CancelNewProfile` only for that exact profile and only before durable provider artifacts exist.

Recovery first renders plan/backup path and requires an explicit confirm key/button. Run Recreate
as a Tea command, render a busy phase, and route the returned pristine entry into onboarding after
the exclusive lifecycle lock has been released. Never expose Recreate for newer/unsupported state.

- [ ] **Step 4: Run screen and catalog integration tests and verify GREEN**

```bash
go test ./internal/tui ./internal/profilecatalog -run 'Test(ProfileName|Cancel|Recovery|Recreate)' -count=1
```

Expected: PASS.

- [ ] **Step 5: Commit profile creation and recovery UI**

```bash
git add internal/tui/profile_name.go internal/tui/profile_name_test.go \
  internal/tui/profile_recovery.go internal/tui/profile_recovery_test.go \
  internal/tui/shell.go internal/tui/shell_view.go
git commit -m "feat: add TUI profile recovery flow"
```

### Task 5: Add Masked Settings, Unlock, and Credential Forms

**Files:**

- Create: `internal/tui/onboarding_form.go`
- Create: `internal/tui/onboarding_form_test.go`
- Create: `internal/tui/secret_input.go`
- Create: `internal/tui/secret_input_test.go`
- Modify: `internal/tui/shell.go`
- Modify: `internal/tui/shell_view.go`

**Interfaces:**

- Consumes: onboarding input states and submits exact versioned actions
- Produces: visible keyboard editing with secret bullets and immediate buffer clearing

- [ ] **Step 1: Write failing masking, focus, and submit tests**

```go
func TestSecretInputShowsOneBulletPerCharacterAndNeverPlaintext(t *testing.T) {
    input := newSecretInput("Monarch password")
    input = typeKeys(t, input, "synthetic-secret")
    rendered := input.View()
    assert.NotContains(t, rendered, "synthetic-secret")
    assert.Equal(t, strings.Repeat("•", len("synthetic-secret")), visibleValue(rendered))
}

func TestCredentialSubmitClearsEverySecretField(t *testing.T) {
    form := populatedCredentialForm(t)
    _, command := form.Submit()
    assert.Empty(t, form.password.Value())
    assert.Empty(t, form.totp.Value())
    assert.Empty(t, form.accountPassword.Value())
    assert.Empty(t, form.confirmation.Value())
    require.NotNil(t, command)
}
```

Cover Tab/Shift-Tab, Up/Down, Enter, Escape, visible USD/2 defaults, email editing, wrong-unlock
announcement, mismatch validation, resize, clipboard/paste behavior, and no secret in `%#v` model
diagnostics.

- [ ] **Step 2: Run form tests and verify RED**

```bash
go test ./internal/tui -run 'Test(SecretInput|Credential|Unlock|SettingsForm)' -count=1
```

Expected: FAIL because the forms are absent.

- [ ] **Step 3: Implement forms as presenter-only adapters**

Use `textinput.EchoPassword` with `EchoCharacter = '•'`. Settings submits
`ActionConfirmSettings`; unlock submits `ActionUnlock`; credential setup submits
`ActionSubmitCredentials`. Copy secret text into byte slices only at submit, clear model strings
immediately, and let coordinator clear the request buffers. Preserve only nonsecret email/settings
after a validation failure.

Render Python labels and order, plus visible focused border/cursor and Go progress/error guidance.
Escape cancels the coordinator attempt and invokes narrow Add rollback when eligible.

- [ ] **Step 4: Run form and coordinator tests and verify GREEN**

```bash
go test ./internal/tui ./internal/onboarding -run 'Test(Secret|Credential|Unlock|Settings|Attempt)' -count=1
```

Expected: PASS.

- [ ] **Step 5: Commit credential forms**

```bash
git add internal/tui/onboarding_form.go internal/tui/onboarding_form_test.go \
  internal/tui/secret_input.go internal/tui/secret_input_test.go \
  internal/tui/shell.go internal/tui/shell_view.go
git commit -m "feat: add secure TUI credential setup"
```

### Task 6: Add Progress, Completion Handoff, and In-Place Reconnect

**Files:**

- Create: `internal/tui/onboarding_progress.go`
- Create: `internal/tui/onboarding_progress_test.go`
- Modify: `internal/tui/provider.go`
- Modify: `internal/tui/provider_test.go`
- Modify: `internal/tui/model.go`
- Modify: `internal/tui/update.go`
- Modify: `internal/tui/shell.go`

**Interfaces:**

- Consumes: onboarding status polling and `TakeOpenedProfile`
- Produces: no-restart finance handoff and Reconnect from an existing finance view

- [ ] **Step 1: Write failing progress/handoff/reconnect tests**

```go
func TestOnboardingProgressRendersPhaseCountsElapsedAndCancel(t *testing.T) {
    state := progressState(onboarding.Snapshot{State: onboarding.StateImporting,
        Progress: &onboarding.Progress{Phase: "fetching", Fetched: 5000, Total: 30793}})
    rendered := state.View()
    assert.Contains(t, rendered, "Fetching Monarch data")
    assert.Contains(t, rendered, "5,000 of 30,793")
    assert.Contains(t, rendered, "Esc Cancel")
}

func TestReconnectReturnsToPreservedFinanceSession(t *testing.T) {
    shell := shellWithExpiredFinanceProvider(t)
    before := shell.finance.session
    shell = activateReconnectAndComplete(t, shell)
    assert.Equal(t, shellFinance, shell.screen)
    assert.Equal(t, before, shell.finance.session)
}
```

Cover authentication phase, verification pass, retry, reconnect-required route, identity mismatch,
cancel-wait message, completed runtime refresh action without restart, and profile lock ownership.

- [ ] **Step 2: Run progress/provider tests and verify RED**

```bash
go test ./internal/tui -run 'Test(OnboardingProgress|Reconnect|Completion|Provider)' -count=1
```

Expected: FAIL because progress and reconnect routes are absent.

- [ ] **Step 3: Poll coordinator state and hand off the opened service**

Use one bounded Tea tick while jobs run. Ignore ticks carrying an old attempt/state version. Render
phase, elapsed time, partition, pass/attempt, and counts only. Escape submits cancel once and keeps
polling until the worker stops.

On complete, call `TakeOpenedProfile`, construct `NewModel` with a fresh or preserved `app.Session`,
and enter `shellFinance`. When provider refresh reports reconnect-required, send a shell message
that starts onboarding for the current profile while preserving finance session/cursor/scroll;
restore and refresh after success.

- [ ] **Step 4: Run TUI, coordinator, and race tests and verify GREEN**

```bash
go test ./internal/tui ./internal/onboarding -count=1
go test -race ./internal/tui ./internal/onboarding -count=1
```

Expected: PASS.

- [ ] **Step 5: Commit progress and reconnect**

```bash
git add internal/tui/onboarding_progress.go internal/tui/onboarding_progress_test.go \
  internal/tui/provider.go internal/tui/provider_test.go internal/tui/model.go \
  internal/tui/update.go internal/tui/shell.go
git commit -m "feat: enter TUI after profile onboarding"
```

### Task 7: Lock TUI Semantic/Visual Frames and Complete Plan 3

**Files:**

- Create: `internal/tui/onboarding_parity_test.go`
- Modify: `internal/tui/visual_golden_test.go`
- Create: `testdata/parity/go_frames/account_selector.json`
- Create: `testdata/parity/go_frames/provider_selector.json`
- Create: `testdata/parity/go_frames/credential_setup.json`
- Create: `testdata/parity/go_frames/credential_unlock.json`
- Modify: `cmd/moneyflow/profile_test.go`
- Modify: `Makefile`
- Modify: `README.md`

**Interfaces:**

- Produces: reviewed Go onboarding frames and final selector-first command contract

- [ ] **Step 1: Add failing semantic and visual comparisons**

Project shell frames into the onboarding semantic type and compare all four Python artifacts.
Assert password fields are empty in artifacts. Add visual cases for 100x30 plus one 80x24 selector
fallback and every local status badge.

- [ ] **Step 2: Run parity/TUI tests and verify RED**

```bash
go test ./internal/tui -run 'Test(OnboardingSemantic|VisualGoldens|TUICommand)' -count=1
```

Expected: FAIL because Go frames have not been reviewed/accepted.

- [ ] **Step 3: Align content and deliberately update Go visual artifacts**

Fix only semantic discrepancies with the Python contract. Keep Go palette/layout styling native.
Extend the existing deliberate updater so `make parity-update-go` writes the four onboarding frame
files through the same guarded update path as finance frames. Run:

```bash
make parity-update-go
PLAN3_PREVIEW_DIR="$(mktemp -d)"
MONEYFLOW_GO_FRAME_PREVIEW_DIR="$PLAN3_PREVIEW_DIR" \
  go test ./internal/tui -run TestVisualGoldens -count=1
find "$PLAN3_PREVIEW_DIR" -type f -print | sort
git diff -- testdata/parity/go_frames
```

Review the complete frame diff and every `.txt`/`.ansi` preview listed from
`$PLAN3_PREVIEW_DIR`. Confirm no secret or personal data is present. Update `make tui-demo`/README
guidance only as needed for selector-first startup.

- [ ] **Step 4: Run full required verification**

```bash
PLAN3_VERIFY_HOME="$(mktemp -d)"
MONEYFLOW_HOME="$PLAN3_VERIFY_HOME" MONEYFLOW_SKIP_PERF=1 make verify-go
MONEYFLOW_HOME="$PLAN3_VERIFY_HOME" uv run pytest -v
uv run pyright moneyflow/
uv run ruff format --check moneyflow/ tests/
uv run ruff check moneyflow/ tests/
npx --yes markdownlint-cli@0.47.0 --config .markdownlint.json README.md 'docs/**/*.md'
.github/scripts/check-arrow-lists.sh
```

Expected: all checks pass. Manually run `make tui-demo` against its temporary profile and verify
selector, masked entry feedback, progress layout, and finance handoff. Do not use a real account in
automated artifacts.

- [ ] **Step 5: Commit the reviewed TUI experience**

```bash
git add internal/tui/onboarding_parity_test.go internal/tui/visual_golden_test.go \
  testdata/parity/go_frames cmd/moneyflow/profile_test.go Makefile README.md
git commit -m "test: lock TUI onboarding experience"
```

Verify `git status --short` is empty before starting Plan 4. Live Monarch dogfooding happens only
after this automated change is committed; any correction is a new tested commit.
