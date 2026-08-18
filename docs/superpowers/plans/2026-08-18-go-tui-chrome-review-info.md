# Go TUI Chrome, Review, and Transaction Info Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development
> (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use
> checkbox (`- [ ]`) syntax for tracking.

**Goal:** Restore Python-quality top chrome and transaction details while replacing the sparse Go
pending-change overlay with a dense `w` then `Enter` commit dashboard.

**Architecture:** Keep all new presentation and key routing inside `internal/tui`. Pass build
version through existing TUI options, consume the provider-neutral last-success timestamp, and use
renderer-local clock messages for the live time. Continue using `app.Service.Review` for bounded
operation windows; do not add renderer state to the application service or domain model.

**Tech Stack:** Go 1.26.3, Bubble Tea v2, Lip Gloss v2, Cobra, Testify, SQLite-backed application
service, semantic and cell-frame parity fixtures

## Global Constraints

- Keep the fast local commit path exactly `w` then `Enter`; do not add a second confirmation phase.
- Keep review grouped by journal operation and request at most `app.MaxReviewTargetLimit` targets.
- Use the focused transaction for `i` even when a multi-selection exists; aggregate rows cannot
  open transaction information.
- Keep `transaction.show-info` out of web capabilities until the web presenter implements it.
- Use the existing blank row zero for chrome; do not reduce the current table viewport.
- Use signed integer minor units and the existing `FormatAmount`; never introduce floating money.
- Do not change the database schema, journal payloads, provider contracts, or web UI.
- Do not commit raster screenshots, `web/dist`, `internal/web/dist`, or brainstorm companion files.
- Write tests before implementation and commit each verified task without amending earlier commits.
- Run branch binaries only with synthetic temporary profiles; never open the live default profile.

---

### Task 1: Render Version and Time Chrome

**Files:**

- Create: `internal/tui/clock.go`
- Create: `internal/tui/clock_test.go`
- Modify: `internal/tui/model.go`
- Modify: `internal/tui/update.go`
- Modify: `internal/tui/layout.go`
- Modify: `internal/tui/layout_test.go`
- Modify: `cmd/moneyflow/root.go`
- Modify: `cmd/moneyflow/root_test.go`

**Interfaces:**

- Consumes: `Options.Now func() time.Time`, `app.ProviderStatus.LastSuccess`, `version.Version`
- Produces: `Options.Version string`, `clockTickMsg`, `clockTickCommand() tea.Cmd`
- Produces: a `chrome` named region on row zero

- [ ] **Step 1: Write failing command, clock, and layout tests**

Add a version assertion to `TestTUICommandStartsPersistentAndTemporaryProfiles` in
`cmd/moneyflow/root_test.go`:

```go
assert.Equal(t, version.Version, options.Version)
```

Import `internal/version` in that test. Add `internal/tui/clock_test.go`:

```go
func TestClockTickUpdatesOnlyRendererTimeAndReschedules(t *testing.T) {
    model := newTestModel(t, app.NewSession())
    revision := model.service.Revision()
    at := time.Date(2026, time.August, 18, 9, 41, 0, 0, time.Local)

    updated, command := model.Update(clockTickMsg{at: at})
    model = updated.(Model)

    assert.Equal(t, at, model.clockAt)
    assert.Equal(t, revision, model.service.Revision())
    assert.NotNil(t, command)
}
```

Add a table-driven chrome test to `internal/tui/layout_test.go` that sets `Version`, `clockAt`, and
`provider.status.LastSuccess`, renders `150x50` and `80x24`, and asserts that row zero contains
`moneyflow v9.8.7`, `Last update 9:05 AM`, and `9:41 AM`. The supplied version is already
formatted and must not gain another `v` prefix. Add a local-only case that asserts
`Last update —`.

- [ ] **Step 2: Run the focused tests and verify RED**

Run:

```bash
MONEYFLOW_SKIP_PERF=1 go test ./internal/tui ./cmd/moneyflow \
  -run 'Test(ClockTick|Chrome|TUICommandStartsPersistent)' -count=1
```

Expected: FAIL because `Options.Version`, `Model.clockAt`, `clockTickMsg`, and the chrome region do
not exist.

- [ ] **Step 3: Add the renderer clock and row-zero chrome**

Extend `Options` and `Model` in `internal/tui/model.go`:

```go
type Options struct {
    Theme            ThemeName
    ColorMode        ColorMode
    Version          string
    InitialDateRange *domain.DateRange
    Now              func() time.Time
}

type Model struct {
    // existing fields remain unchanged
    now     func() time.Time
    clockAt time.Time
}
```

Default an empty version to `dev`, initialize `clockAt` from `options.Now()`, and make `Init` always
include the clock command. Preserve the provider command when available:

```go
func (model Model) Init() tea.Cmd {
    clock := clockTickCommand()
    if _, available := model.capability(app.ActionRefreshProvider); !available {
        return clock
    }
    return tea.Batch(clock, model.providerStatusCommand(model.now()))
}
```

When the selector opens a profile after `Shell.Init`, batch `shell.finance.Init()` with the initial
resize command so both the clock and provider scheduler start on that path too.

Create `internal/tui/clock.go`:

```go
package tui

import (
    "time"

    tea "charm.land/bubbletea/v2"
)

const clockInterval = time.Minute

type clockTickMsg struct{ at time.Time }

func clockTickCommand() tea.Cmd {
    return tea.Tick(clockInterval, func(at time.Time) tea.Msg {
        return clockTickMsg{at: at}
    })
}

func formatClock(at time.Time) string {
    if at.IsZero() {
        return "—"
    }
    return at.Local().Format("3:04 PM")
}
```

Route `clockTickMsg` before key handling in `internal/tui/update.go`, assign `model.clockAt`, and
return another `clockTickCommand()`.

In `RenderScreen`, draw a `chrome` region at `{X: 1, Y: 0, Width: contentWidth, Height: 1}`. Put
`moneyflow` plus the injected version on the left. Put current time on the right, then include the
`Last update <time> |` prefix when it fits; otherwise drop that prefix before truncating the brand.
Add the region to `RenderedScreen.Regions` without changing the breadcrumb or table rectangles.

Set `Version: version.Version` in `previewOptions` in `cmd/moneyflow/root.go`.

- [ ] **Step 4: Run focused and package tests and verify GREEN**

Run:

```bash
gofmt -w cmd/moneyflow/root.go cmd/moneyflow/root_test.go internal/tui/clock.go \
  internal/tui/clock_test.go internal/tui/model.go internal/tui/update.go \
  internal/tui/layout.go internal/tui/layout_test.go
MONEYFLOW_SKIP_PERF=1 go test ./internal/tui ./cmd/moneyflow -count=1
```

Expected: PASS. Existing `Init` tests must now assert a non-nil clock command for unbound models.

- [ ] **Step 5: Commit the chrome task**

Review `git diff --check`, stage only the eight task files, and commit with subject:

```text
feat: add live TUI application chrome
```

### Task 2: Implement Focused Transaction Information

**Files:**

- Create: `internal/tui/transaction_info.go`
- Create: `internal/tui/transaction_info_test.go`
- Modify: `internal/tui/model.go`
- Modify: `internal/tui/layout.go`
- Modify: `internal/tui/update.go`
- Modify: `internal/app/actions.go`
- Modify: `internal/app/actions_test.go`
- Modify: `internal/tui/help_test.go`

**Interfaces:**

- Consumes: the focused `domain.DetailRow.Transaction` and `domain.Transaction.Clone()`
- Produces: `transactionInfoState`, `openTransactionInfo`, `routeTransactionInfo`, and
  `renderTransactionInfo`
- Produces: `transactionInfoLines(domain.Transaction) []string` with sorted metadata

- [ ] **Step 1: Write failing transaction-info and registry tests**

Create `internal/tui/transaction_info_test.go` with four behavior tests:

```go
func TestTransactionInfoUsesFocusedDetailRowAndSortedMetadata(t *testing.T) {
    model := press(t, newTestModel(t, app.NewSession()), keyRune('d'))
    model.cursor = 1
    model.result.DetailRows[1].Transaction.Metadata = map[string]string{"zeta": "last", "alpha": "first"}
    focused := model.result.DetailRows[1].Transaction.Clone()

    model = press(t, model, keyRune('i'))

    require.Equal(t, overlayTransactionInfo, model.overlay)
    assert.Equal(t, focused.ID, model.transactionInfo.transaction.ID)
    rendered := strings.Join(model.RenderScreen().Frame.PlainLines(), "\n")
    assert.Contains(t, rendered, focused.Merchant.Name)
    assert.Contains(t, rendered, FormatAmount(focused.Amount))
    assert.Less(t, strings.Index(rendered, "alpha"), strings.Index(rendered, "zeta"))
}
```

Add tests that select a different transaction while focus remains on row one, refuse `i` in an
aggregate view with the exact status `Transaction information is available from a transaction
row.`, and prove `PageDown` changes the overlay scroll while `Esc`, `Enter`, and `i` close without
changing the finance cursor or scroll.

Update the expected `ActionShowInfo` row in `internal/app/actions_test.go` to
`Implemented: true, Web: false`. Add help assertions that `i` is implemented and detail hints
contain `i=Info`.

- [ ] **Step 2: Run the focused tests and verify RED**

Run:

```bash
MONEYFLOW_SKIP_PERF=1 go test ./internal/tui ./internal/app \
  -run 'Test(TransactionInfo|ActionRegistry|Help)' -count=1
```

Expected: FAIL because the action remains unimplemented and no transaction overlay is routed.

- [ ] **Step 3: Add the read-only overlay and key routing**

Add `overlayTransactionInfo` and `transactionInfo transactionInfoState` to
`internal/tui/model.go`. Create `internal/tui/transaction_info.go` with this state boundary:

```go
type transactionInfoState struct {
    transaction domain.Transaction
    scroll      int
}

func (model *Model) openTransactionInfo() tea.Cmd {
    if model.result.DetailRows == nil || model.cursor < 0 || model.cursor >= len(model.result.DetailRows) {
        model.status = "Transaction information is available from a transaction row."
        return nil
    }
    model.transactionInfo = transactionInfoState{
        transaction: model.result.DetailRows[model.cursor].Transaction.Clone(),
    }
    model.overlay = overlayTransactionInfo
    model.status = ""
    return nil
}
```

Build `transactionInfoLines` from labeled date, amount, merchant, category, group, account, notes,
posted/pending, visible/hidden, local IDs, provider, provider transaction ID, and stable sorted
metadata keys. Render missing optional values as `—`. Use `responsiveOverlayRect`, a titled box,
and the current frame's bounded height. Clamp scroll after `↑`/`↓`, `j`/`k`, `PageUp`, and
`PageDown`; close on `Esc`, `Enter`, or `i`.

Route `ActionShowInfo` in `routeKey`, route the new overlay in `routeOverlay`, and render it from
`renderOverlay`. Add `i=Info` to detail-view action hints. In `internal/app/actions.go`, change only
the show-info definition to:

```go
{ActionShowInfo, []string{"i"}, "i", "Show transaction info/details", "Actions", ScopeOverlay, true, false},
```

- [ ] **Step 4: Run focused and package tests and verify GREEN**

Run:

```bash
gofmt -w internal/app/actions.go internal/app/actions_test.go internal/tui/model.go \
  internal/tui/layout.go internal/tui/update.go internal/tui/transaction_info.go \
  internal/tui/transaction_info_test.go internal/tui/help_test.go
MONEYFLOW_SKIP_PERF=1 go test ./internal/app ./internal/tui -count=1
```

Expected: PASS, including action-registry completeness and focus-restoration tests.

- [ ] **Step 5: Commit the transaction-info task**

Review `git diff --check`, stage only the eight task files, and commit with subject:

```text
feat: show focused transaction details in TUI
```

### Task 3: Replace Review Ceremony with the Dense Dashboard

**Files:**

- Create: `internal/tui/review_format.go`
- Create: `internal/tui/review_format_test.go`
- Modify: `internal/tui/review.go`
- Modify: `internal/tui/review_test.go`
- Modify: `internal/tui/provider_test.go`
- Modify: `internal/tui/overlay_test.go`

**Interfaces:**

- Consumes: `app.ReviewProjection`, `app.ReviewWindow`, and `app.MaxReviewTargetLimit`
- Produces: `reviewOperationLabel(domain.OperationType) string`
- Produces: `loadReviewPreview()`, which keeps the review in dashboard phase
- Preserves: `loadReviewDetails(int)` for the explicit `i` detail phase

- [ ] **Step 1: Write failing formatter, dashboard, and fast-commit tests**

Create a table-driven `TestReviewOperationLabel` in `internal/tui/review_format_test.go` covering all
fourteen operation types, including `transaction.hide-toggle` → `Hide transactions`,
`merchant.label` → `Rename merchant`, and `category.assign` → `Change category`.

Replace the old review interaction expectations in `internal/tui/review_test.go` with:

```go
func TestReviewDashboardSeparatesRedoAndLoadsBoundedPreview(t *testing.T) {
    model := press(t, newPersistentModel(t, app.NewSession()).model, keyRune('h'))
    model = press(t, model, keyRune('j'))
    model = press(t, model, keyRune('h'))
    model = press(t, model, keyRune('u'))
    model = press(t, model, keyRune('w'))

    require.Equal(t, reviewPhaseSummary, model.review.phase)
    assert.NotEmpty(t, model.review.projection.Targets)
    assert.LessOrEqual(t, len(model.review.projection.Targets), app.MaxReviewTargetLimit)
    rendered := strings.Join(model.RenderScreen().Frame.PlainLines(), "\n")
    assert.Contains(t, rendered, "ACTIVE")
    assert.Contains(t, rendered, "REDO")
    assert.Contains(t, rendered, "Hide transactions")
    assert.Contains(t, rendered, "commit will discard 1 redo operation")
}

func TestReviewEnterCommitsWithoutIntermediatePhase(t *testing.T) {
    model := press(t, newPersistentModel(t, app.NewSession()).model, keyRune('h'))
    model = press(t, model, keyRune('w'))

    model = press(t, model, tea.KeyPressMsg{Code: tea.KeyEnter})

    assert.Equal(t, overlayNone, model.overlay)
    assert.Zero(t, model.pending.ActiveOperations)
    assert.Contains(t, model.status, "Committed 1 operation")
}
```

Add tests that `i` enters details, `Esc` returns to the dashboard, `Enter` from detail also commits,
selection movement reloads a preview for the newly focused operation, provider-bound Enter stays in
review with the write-back message, and a stale direct Enter refreshes review without replay.

- [ ] **Step 2: Run the focused tests and verify RED**

Run:

```bash
MONEYFLOW_SKIP_PERF=1 go test ./internal/tui \
  -run 'Test(Review|ProviderBoundReview|OverlayRegions)' -count=1
```

Expected: FAIL because Enter opens details, `c` opens confirmation, the dashboard has no preview,
and operation names are raw identifiers.

- [ ] **Step 3: Implement friendly labels and the dense dashboard**

Create `internal/tui/review_format.go` with an exhaustive switch:

```go
func reviewOperationLabel(operationType domain.OperationType) string {
    switch operationType {
    case domain.OperationMerchantLabel:
        return "Rename merchant"
    case domain.OperationMerchantMerge:
        return "Merge merchant"
    case domain.OperationMerchantReassign:
        return "Reassign merchant"
    case domain.OperationCategoryAssign:
        return "Change category"
    case domain.OperationCategoryCreate:
        return "Create category"
    case domain.OperationCategoryLabel:
        return "Rename category"
    case domain.OperationCategoryMove:
        return "Move category"
    case domain.OperationCategoryMerge:
        return "Merge category"
    case domain.OperationCategoryDelete:
        return "Delete category"
    case domain.OperationGroupCreate:
        return "Create group"
    case domain.OperationGroupLabel:
        return "Rename group"
    case domain.OperationGroupMerge:
        return "Merge group"
    case domain.OperationGroupDelete:
        return "Delete group"
    case domain.OperationTransactionHide:
        return "Hide transactions"
    default:
        return string(operationType)
    }
}
```

Remove `reviewPhaseConfirm`. In dashboard phase, route `Enter` to the existing commit guards and
`commitReview`, route `i` to `loadReviewDetails(0)`, and keep `Esc`, arrow, and `j`/`k` behavior. In
detail phase, keep paging and `Esc`, add `Enter` commit, and allow `i` to return to the dashboard.

After `openReview`, request the first selected operation with a small bounded preview window. On
dashboard selection movement, request the new operation at offset zero while preserving
`reviewedRevision` and dashboard phase. Keep the explicit detail limit capped by
`app.MaxReviewTargetLimit`.

Render the dashboard as:

1. summary counts, labeling the distinct count as transactions affected by active operations;
2. `ACTIVE` operation rows;
3. `REDO` operation rows;
4. the selected operation's bounded transaction preview; and
5. one footer: `↑/↓=Choose | i=Details | Enter=Commit | Esc=Cancel`.

Use aligned Change, Affected, and Before → After columns when the overlay is wide enough. Fall back
to one truncated line per operation at the minimum terminal size. Show taxonomy effect alongside
before/after text. Always show the redo-tail discard warning in dashboard state when inactive
operations exist. Keep the provider-unavailable and no-active-operation guards in the dashboard.

Update stale-review, provider-bound, and overlay tests to use direct Enter instead of `c` plus
Enter.

- [ ] **Step 4: Run focused and package tests and verify GREEN**

Run:

```bash
gofmt -w internal/tui/review.go internal/tui/review_test.go internal/tui/review_format.go \
  internal/tui/review_format_test.go internal/tui/provider_test.go internal/tui/overlay_test.go
MONEYFLOW_SKIP_PERF=1 go test ./internal/tui -count=1
```

Expected: PASS. The review service still receives only bounded windows, and no application or store
package changes.

- [ ] **Step 5: Commit the review task**

Review `git diff --check`, stage only the six task files, and commit with subject:

```text
feat: streamline TUI pending-change review
```

### Task 4: Lock Visual Contracts and Verify the Slice

**Files:**

- Modify: `internal/tui/visual_golden_test.go`
- Modify: `testdata/parity/go_frames/*.json` through the deliberate update target

**Interfaces:**

- Consumes: real TUI key routing and `RenderedScreen` cell frames
- Produces: reviewed, deterministic Go frame fixtures for chrome, info, review, and direct commit

- [ ] **Step 1: Update scenario drivers before regenerating artifacts**

Change `goOnlyEditingScenarios` so `commit_redo_warning` stops on the `w` dashboard and
`stale_review_conflict` uses `h`, `w`, `external_undo`, `enter`. Add scenarios:

```go
{"transaction_info", []string{"d", "i"}},
{"transaction_info_aggregate_refusal", []string{"i"}},
{"review_immediate_commit", []string{"h", "w", "enter"}},
```

Ensure the common 150x50 and minimum 80x24 scenarios carry deterministic `Options.Version` and
`Options.Now` inputs so chrome artifacts never depend on wall-clock time.

- [ ] **Step 2: Run parity checks and verify the expected artifact mismatch**

Run:

```bash
make parity
```

Expected: FAIL only because the committed Go frame artifacts still describe the old blank header,
old review phases, unavailable help action, and absent transaction-info overlay.

- [ ] **Step 3: Deliberately regenerate and inspect Go cell frames**

Run:

```bash
make parity-update-go
git diff --stat -- testdata/parity/go_frames
git diff -- testdata/parity/go_frames
```

Expected: cell-frame JSON changes only. Do not run a screenshot generator and do not add any PNG,
HTML preview, `web/dist`, or `internal/web/dist` file.

- [ ] **Step 4: Run the complete verification gates**

Run:

```bash
make verify-go
uv run pytest -v
uv run pyright moneyflow/
uv run ruff format --check moneyflow/ tests/
uv run ruff check moneyflow/ tests/
npx --yes markdownlint-cli@0.47.0 --config .markdownlint.json README.md 'docs/**/*.md'
.github/scripts/check-arrow-lists.sh
```

Expected: PASS. If only a documented load-sensitive Go performance ceiling is noisy, record the
measurement and run `MONEYFLOW_SKIP_PERF=1 make verify-go`; every non-performance gate must pass.

- [ ] **Step 5: Review privacy, generated-output, and scope boundaries**

Run:

```bash
git status --short
git diff --check
git diff --name-only
```

Confirm that the diff contains no schema, provider, API, web source, raster screenshot, or generated
distribution change. Run the repository private-data scrub over every pending artifact.

- [ ] **Step 6: Commit the reviewed visual contract**

Stage only `internal/tui/visual_golden_test.go` and the reviewed parity JSON files. Commit with
subject:

```text
test: lock richer Go TUI workflows
```
