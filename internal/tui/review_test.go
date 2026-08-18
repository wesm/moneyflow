package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/wesm/moneyflow/internal/app"
	"github.com/wesm/moneyflow/internal/domain"
	"github.com/wesm/moneyflow/internal/store"
)

func TestReviewSeparatesRedoLoadsBoundedDetailsAndCancelsExactly(t *testing.T) {
	t.Parallel()
	model := press(t, newPersistentModel(t, app.NewSession()).model, keyRune('h'))
	model = press(t, model, keyRune('j'))
	model = press(t, model, keyRune('h'))
	model = press(t, model, keyRune('u'))
	require.Equal(t, 1, model.pending.ActiveOperations)
	require.Equal(t, 1, model.pending.InactiveOperations)
	original := model.editorSnapshot()

	model = press(t, model, keyRune('w'))
	require.Equal(t, overlayReview, model.overlay)
	assert.Len(t, model.review.projection.ActiveOperations, 1)
	assert.Len(t, model.review.projection.InactiveOperations, 1)
	assert.NotEmpty(t, model.review.projection.Targets)
	assert.Contains(t, strings.Join(model.RenderScreen().Overlay, "\n"), "Inactive redo operations")
	assert.Contains(t, model.RenderScreen().Frame.RenderANSI(), "ACTIVE")
	assert.Contains(t, model.RenderScreen().Frame.RenderANSI(), "REDO")

	model = press(t, model, keyRune('i'))
	assert.Equal(t, reviewPhaseDetails, model.review.phase)
	assert.LessOrEqual(t, len(model.review.projection.Targets), app.MaxReviewTargetLimit)
	model = press(t, model, tea.KeyPressMsg{Code: tea.KeyEscape})
	assert.Equal(t, reviewPhaseSummary, model.review.phase)
	model = press(t, model, tea.KeyPressMsg{Code: tea.KeyEscape})
	assert.Equal(t, overlayNone, model.overlay)
	assert.Equal(t, original.session.ViewState(), model.session.ViewState())
	assert.Equal(t, original.cursor, model.cursor)
	assert.Equal(t, original.scroll, model.scroll)
}

func TestReviewCommitWarnsAboutRedoAndClearsPendingHistory(t *testing.T) {
	t.Parallel()
	model := press(t, newPersistentModel(t, app.NewSession()).model, keyRune('h'))
	model = press(t, model, keyRune('j'))
	model = press(t, model, keyRune('h'))
	model = press(t, model, keyRune('u'))
	model = press(t, model, keyRune('w'))
	require.Equal(t, reviewPhaseSummary, model.review.phase)
	assert.Contains(t, strings.Join(model.RenderScreen().Overlay, "\n"), "discard 1 redo operation")
	model = press(t, model, tea.KeyPressMsg{Code: tea.KeyEnter})
	assert.Equal(t, overlayNone, model.overlay)
	assert.Zero(t, model.pending.ActiveOperations)
	assert.Zero(t, model.pending.InactiveOperations)
	assert.Contains(t, model.status, "Committed 1 operation")
}

func TestReviewStaleConfirmationRefreshesWithoutReplayingCommit(t *testing.T) {
	t.Parallel()
	fixture := newPersistentModel(t, app.NewSession())
	model := press(t, fixture.model, keyRune('h'))
	model = press(t, model, keyRune('w'))
	reviewed := model.review.reviewedRevision
	_, err := model.service.Undo(fixture.ctx, model.service.Revision())
	require.NoError(t, err)

	model = press(t, model, tea.KeyPressMsg{Code: tea.KeyEnter})
	assert.Equal(t, overlayReview, model.overlay)
	assert.Equal(t, reviewPhaseSummary, model.review.phase)
	assert.Greater(t, model.review.reviewedRevision, reviewed)
	assert.Contains(t, model.review.err, "review")
	assert.Equal(t, 0, model.pending.ActiveOperations)
}

func TestReviewDetailPageMatchesCappedOverlayRows(t *testing.T) {
	t.Parallel()
	model := press(t, newPersistentModel(t, app.NewSession()).model, keyRune('d'))
	model = press(t, model, tea.KeyPressMsg{Code: 'a', Mod: tea.ModCtrl})
	model = press(t, model, keyRune('h'))
	model.height = 50
	model.width = 150
	model = press(t, model, keyRune('w'))
	model = press(t, model, keyRune('i'))

	rect := responsiveOverlayRect(model.width, model.height, 92, 36)
	assert.Equal(t, rect.Height-8, model.review.detailLimit)
	assert.Equal(t, rect.Height-8, len(model.review.projection.Targets))
}

func TestReviewSummaryScrollsToSelectedOperation(t *testing.T) {
	t.Parallel()
	model := newPersistentModel(t, app.NewSession()).model
	model.width, model.height = 150, 50
	model.overlay = overlayReview
	model.review.phase = reviewPhaseSummary
	for index := 0; index < 40; index++ {
		model.review.projection.Operations = append(model.review.projection.Operations, app.ReviewOperation{
			OperationID: "operation", Sequence: int64(index + 1),
			Type: domain.OperationTransactionHide, Active: true, AffectedCount: 1,
		})
	}
	model.review.selected = 35
	frame := model.RenderScreen().Frame.RenderANSI()
	assert.Contains(t, frame, "Toggle report visibility")
}

func TestReviewDashboardNavigationRefreshesBoundedPreview(t *testing.T) {
	t.Parallel()
	model := press(t, newPersistentModel(t, app.NewSession()).model, keyRune('h'))
	model = press(t, model, keyRune('j'))
	model = press(t, model, keyRune('h'))
	model.width, model.height = 150, 50
	model = press(t, model, keyRune('w'))
	revision := model.review.reviewedRevision
	require.NotEmpty(t, model.review.projection.Targets)
	first := model.review.projection.Targets[0].TransactionID

	model = press(t, model, tea.KeyPressMsg{Code: tea.KeyDown})

	assert.Equal(t, 1, model.review.selected)
	assert.Equal(t, revision, model.review.reviewedRevision)
	require.NotEmpty(t, model.review.projection.Targets)
	assert.NotEqual(t, first, model.review.projection.Targets[0].TransactionID)
	assert.LessOrEqual(t, len(model.review.projection.Targets), model.reviewPreviewLimit())
}

func TestReviewEnterWithOnlyRedoStaysOpen(t *testing.T) {
	t.Parallel()
	model := press(t, newPersistentModel(t, app.NewSession()).model, keyRune('h'))
	model = press(t, model, keyRune('u'))
	model = press(t, model, keyRune('w'))

	model = press(t, model, tea.KeyPressMsg{Code: tea.KeyEnter})

	assert.Equal(t, overlayReview, model.overlay)
	assert.Contains(t, model.review.err, "no active operations")
}

func TestReviewStorageFailureStaysVisibleInReview(t *testing.T) {
	t.Parallel()
	fixture := newPersistentModel(t, app.NewSession())
	model := press(t, fixture.model, keyRune('h'))
	model = press(t, model, keyRune('w'))
	require.NoError(t, fixture.profile.Close())

	model = press(t, model, tea.KeyPressMsg{Code: tea.KeyEnter})

	assert.Equal(t, overlayReview, model.overlay)
	assert.NotEmpty(t, model.review.err)
	assert.Contains(t, strings.Join(model.RenderScreen().Overlay, "\n"), model.review.err)
}

func TestQuitAlwaysConfirmsAndExplainsDurablePending(t *testing.T) {
	t.Parallel()
	model := newPersistentModel(t, app.NewSession()).model
	updated, command := model.Update(keyRune('q'))
	model = updated.(Model)
	assert.Nil(t, command)
	assert.Equal(t, overlayQuit, model.overlay)
	assert.Contains(t, model.RenderScreen().Overlay, "Quit moneyflow?")

	model = press(t, model, tea.KeyPressMsg{Code: tea.KeyEscape})
	model = press(t, model, keyRune('h'))
	model = press(t, model, keyRune('q'))
	overlay := model.RenderScreen().Overlay
	assert.Contains(t, strings.Join(overlay, "\n"), "safely persisted")
	assert.NotContains(t, strings.Join(overlay, "\n"), "unsaved")
	updated, command = model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	model = updated.(Model)
	require.NotNil(t, command)
	_, quitting := command().(tea.QuitMsg)
	assert.True(t, quitting)
}

func TestQuitExplainsDurableProviderWrite(t *testing.T) {
	t.Parallel()

	model := newTestModel(t, app.NewSession())
	model.providerWrite.status = app.ProviderWriteStatus{
		Phase: store.WritePhaseWriting, Version: 2, Total: 10, Completed: 3, Remaining: 7,
	}
	model = press(t, model, keyRune('q'))

	overlay := strings.Join(model.RenderScreen().Overlay, "\n")
	assert.Contains(t, overlay, "provider write is durable")
	assert.Contains(t, overlay, "resume")
}
