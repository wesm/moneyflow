package tui

import (
	"context"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/wesm/moneyflow/internal/app"
	"github.com/wesm/moneyflow/internal/provider"
	"github.com/wesm/moneyflow/internal/store"
)

func TestReviewProviderCommitStartsAsyncWriteAndPreservesFinanceState(t *testing.T) {
	t.Parallel()

	fixture := newProviderModel(t, 3)
	fixture.source.writer = tuiProviderWriter{identity: fixture.source.identity}
	model := press(t, fixture.model, keyRune('h'))
	identity := model.rowIdentity(model.cursor)
	model = press(t, model, keyRune('w'))
	require.Equal(t, overlayReview, model.overlay)

	updated, command := model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	model = updated.(Model)
	require.NotNil(t, command)
	assert.Equal(t, overlayProviderWrite, model.overlay)
	assert.Equal(t, identity, model.rowIdentity(model.cursor))
	assert.Equal(t, store.WritePhaseWriting, model.providerWrite.status.Phase)
	assert.NotContains(t, model.RenderScreen().Frame.RenderANSI(), "write-back is not implemented")

	updated, _ = model.Update(command())
	model = updated.(Model)
	assert.Equal(t, overlayNone, model.overlay)
	assert.Empty(t, model.providerWrite.status.Phase)
	assert.Zero(t, model.pending.ActiveOperations)
	assert.Contains(t, model.status, "Provider write complete")
}

func TestProviderWriteOverlayActionsAndEstimate(t *testing.T) {
	t.Parallel()

	model := newTestModel(t, app.NewSession())
	model.overlay = overlayProviderWrite
	model.clockAt = time.Date(2026, time.August, 18, 12, 0, 0, 0, time.UTC)
	model.providerWrite = providerWriteTUIState{
		status: app.ProviderWriteStatus{
			Phase: store.WritePhaseWriting, Version: 3, Total: 240, Completed: 40, Remaining: 200,
		},
		startedAt: model.clockAt.Add(-40 * time.Minute), startedCompleted: 0,
	}

	rendered := model.RenderScreen().Frame.RenderANSI()
	assert.Contains(t, rendered, "40 / 240")
	assert.Contains(t, rendered, "about 3h 20m remaining")
	assert.Contains(t, rendered, "p=Pause")

	model.providerWrite.status = app.ProviderWriteStatus{Phase: store.WritePhasePaused, Version: 4}
	assert.Contains(t, model.RenderScreen().Frame.RenderANSI(), "r=Resume")
	model.providerWrite.status = app.ProviderWriteStatus{
		Phase: store.WritePhaseAttentionRequired, Version: 5,
		AttentionClass:  store.WriteAttentionReconcileOnly,
		AttentionReason: store.WriteAttentionTargetNotFound,
	}
	rendered = model.RenderScreen().Frame.RenderANSI()
	assert.Contains(t, rendered, "s=Stop and reconcile")
	assert.NotContains(t, rendered, "r=Retry")
}

func TestProviderWriteStatusOpensWithWAndEscapeDoesNotPause(t *testing.T) {
	t.Parallel()

	model := newTestModel(t, app.NewSession())
	model.providerWrite.status = app.ProviderWriteStatus{
		Phase: store.WritePhasePaused, Version: 7, Total: 9, Completed: 4, Remaining: 5,
	}
	model.caps = map[app.ActionID]app.Capability{
		app.ActionReviewChanges: {Action: app.ActionReviewChanges, Available: true},
	}

	model = press(t, model, keyRune('w'))
	assert.Equal(t, overlayProviderWrite, model.overlay)
	model = press(t, model, tea.KeyPressMsg{Code: tea.KeyEscape})
	assert.Equal(t, overlayNone, model.overlay)
	assert.Equal(t, store.WritePhasePaused, model.providerWrite.status.Phase)
}

func TestProviderWriteStandingTickStartsOnlyAutomaticPhases(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.August, 18, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		name      string
		status    app.ProviderWriteStatus
		wantStart bool
	}{
		{name: "ownerless writing", status: app.ProviderWriteStatus{Phase: store.WritePhaseWriting, Version: 1}, wantStart: true},
		{name: "completed reconciling", status: app.ProviderWriteStatus{Phase: store.WritePhaseReconciling, ResumeTarget: store.WriteResumeWriting, Version: 1, Total: 2, Completed: 2}, wantStart: true},
		{name: "ownerless provider reconciliation", status: app.ProviderWriteStatus{Phase: store.WritePhaseReconciling, ResumeTarget: store.WriteResumeReconciling, Version: 1}, wantStart: true},
		{name: "eligible rate limit", status: app.ProviderWriteStatus{Phase: store.WritePhaseRateLimited, Version: 1, NextEligible: now}, wantStart: true},
		{name: "healed reconnect", status: app.ProviderWriteStatus{Phase: store.WritePhaseReconnectRequired, ResumeTarget: store.WriteResumeWriting, Version: 1, SessionChanged: true}, wantStart: true},
		{name: "healed reconnect during reconciliation", status: app.ProviderWriteStatus{Phase: store.WritePhaseReconnectRequired, ResumeTarget: store.WriteResumeReconciling, Version: 1, SessionChanged: true}, wantStart: true},
		{name: "confirmation waits", status: app.ProviderWriteStatus{Phase: store.WritePhaseReconcileConfirmationRequired, ResumeTarget: store.WriteResumeReconciling, Version: 1}},
		{name: "paused", status: app.ProviderWriteStatus{Phase: store.WritePhasePaused, Version: 1}},
		{name: "attention", status: app.ProviderWriteStatus{Phase: store.WritePhaseAttentionRequired, Version: 1, AttentionClass: store.WriteAttentionRetryable}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			model := newTestModel(t, app.NewSession())
			updated, command := model.Update(providerStatusMsg{
				writeStatus: test.status, at: now,
				timerGeneration: model.provider.timerGeneration,
			})
			model = updated.(Model)
			assert.Equal(t, test.wantStart, model.providerWrite.running)
			assert.NotNil(t, command)
		})
	}
}

type tuiProviderWriter struct {
	identity provider.ProfileIdentity
}

func (writer tuiProviderWriter) ProbeIdentity(context.Context) (provider.ProfileIdentity, error) {
	return writer.identity, nil
}

func (tuiProviderWriter) UpdateTransaction(
	_ context.Context,
	update provider.TransactionUpdate,
) (provider.TransactionUpdateResult, error) {
	result := provider.TransactionUpdateResult{TransactionExternalID: update.TransactionExternalID}
	if update.MerchantName.Present {
		result.MerchantExternalID = provider.Some("merchant-example")
		result.MerchantLabel = provider.Some(update.MerchantName.Value)
	}
	if update.CategoryExternalID.Present {
		result.CategoryExternalID = provider.Some(update.CategoryExternalID.Value)
	}
	if update.Hidden.Present {
		result.Hidden = provider.Some(update.Hidden.Value)
	}
	return result, nil
}
