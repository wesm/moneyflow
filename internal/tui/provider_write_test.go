package tui

import (
	"context"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/wesm/moneyflow/internal/app"
	"github.com/wesm/moneyflow/internal/domain"
	"github.com/wesm/moneyflow/internal/provider"
	"github.com/wesm/moneyflow/internal/store"
)

func TestYNABReviewWEnterAndQuotaWait(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, time.September, 7, 12, 0, 0, 0, time.UTC)
	source := &tuiProviderSource{identity: provider.ProfileIdentity{Kind: "ynab", RemoteID: "plan-example"},
		snapshot: tuiProviderSnapshot(t, now, 1), fingerprint: "vault-example"}
	source.writer = tuiProviderWriter{identity: source.identity}
	model := newPristineProviderModel(t, source, now, "ynab")
	model = press(t, model, tea.KeyPressMsg{Code: tea.KeyEnter})
	require.Len(t, model.result.DetailRows, 1)
	_, err := model.service.Mutate(ctx, app.MutationRequest{
		Action: app.ActionEditCategory, ExpectedRevision: model.service.Revision(), State: model.session.ViewState(), Selection: app.EmptySelection(),
		Target: &app.RowTarget{Kind: app.IdentityTransaction, Identity: model.result.DetailRows[0].Transaction.ID},
		Input:  app.EditInput{Scope: app.EditScopeTransactions, DestinationID: domain.UncategorizedCategoryID},
	})
	require.NoError(t, err)
	model.refreshPreserving(model.rowIdentity(model.cursor))
	model = press(t, model, keyRune('w'))
	updated, command := model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	model = updated.(Model)
	require.NotNil(t, command)
	assert.Contains(t, model.RenderScreen().Frame.RenderANSI(), "YNAB Write")
	updated, _ = model.Update(command())
	model = updated.(Model)
	assert.Empty(t, model.providerWrite.status.Phase)
	assert.Zero(t, model.pending.ActiveOperations)
	model.overlay = overlayProviderWrite
	model.providerWrite.status = app.ProviderWriteStatus{Phase: store.WritePhaseRateLimited, Total: 3, Completed: 1, Remaining: 2, NextEligible: now.Add(time.Hour)}
	model.providerWrite.startedAt = now.Add(-time.Minute)
	model.clockAt = now
	rendered := model.RenderScreen().Frame.RenderANSI()
	assert.Contains(t, rendered, "13:00:00")
	assert.Contains(t, rendered, "YNAB asked")
	assert.NotContains(t, rendered, "remaining (estimated)")
	assert.Empty(t, model.providerWriteEstimate())
}

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

func TestProviderStatusCompletionRetainsAuditWarning(t *testing.T) {
	t.Parallel()
	model := newTestModel(t, app.NewSession())
	model.providerWrite.status.Phase = store.WritePhaseWriting
	warning := "Changes saved, but the audit log could not record completion."
	message := providerStatusMsg{writeStatus: app.ProviderWriteStatus{AuditWarning: warning}}
	for range 2 {
		updated, _ := model.Update(message)
		model = updated.(Model)
		assert.Contains(t, model.status, "Provider write complete")
		assert.Contains(t, model.status, warning)
	}
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
	assert.Contains(t, rendered, "Monarch Write")
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
	model.providerWrite.status.Phase = store.WritePhaseReconnectRequired
	model = press(t, model, keyRune('c'))
	assert.Contains(t, model.status, "Reconnect")
	assert.Contains(t, model.providerWriteGuidance(model.providerWrite.status), "Reconnect")
}

func TestProviderWriteCheckAndResumeOnlyForMonarchUnknownOutcome(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name   string
		kind   string
		reason store.WriteAttentionReason
		target store.WriteResumeTarget
		resume bool
	}{
		{"Monarch unknown outcome", "monarch", store.WriteAttentionOutcomeUnknown, store.WriteResumeWriting, true},
		{"YNAB unknown outcome", "ynab", store.WriteAttentionOutcomeUnknown, store.WriteResumeWriting, false},
		{"Monarch rejected edit", "monarch", store.WriteAttentionRejected, store.WriteResumeWriting, false},
		{"Monarch reconciliation", "monarch", store.WriteAttentionOutcomeUnknown, store.WriteResumeReconciling, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			now := time.Date(2026, time.September, 7, 12, 0, 0, 0, time.UTC)
			source := &tuiProviderSource{
				identity: provider.ProfileIdentity{Kind: test.kind, RemoteID: "profile-example"},
				snapshot: tuiProviderSnapshot(t, now, 1), fingerprint: "session-example",
			}
			model := newPristineProviderModel(t, source, now, test.kind)
			model.overlay = overlayProviderWrite
			model.providerWrite.status = app.ProviderWriteStatus{
				Phase: store.WritePhaseAttentionRequired, Version: 7,
				AttentionClass:  store.WriteAttentionReconcileOnly,
				AttentionReason: test.reason, ResumeTarget: test.target,
			}
			frame := strings.Join(model.RenderScreen().Frame.PlainLines(), "\n")
			updated, command := model.Update(keyRune('r'))
			model = updated.(Model)
			if test.resume {
				assert.Contains(t, frame, "r=Check and resume")
				assert.Contains(t, frame, "history is not reloaded")
				require.NotNil(t, command)
				assert.True(t, model.providerWrite.running)
				assert.IsType(t, providerWriteMsg{}, command())
			} else {
				assert.NotContains(t, frame, "r=Check and resume")
				assert.Contains(t, frame, "s=Stop and reconcile")
				assert.Nil(t, command)
				assert.False(t, model.providerWrite.running)
			}
		})
	}
}

func TestProviderWriteShowsChangeAfterCommitAndReopening(t *testing.T) {
	t.Parallel()
	fixture := newProviderModel(t, 3)
	model := fixture.model
	_, err := model.service.Mutate(t.Context(), app.MutationRequest{
		Action: app.ActionEditMerchant, ExpectedRevision: model.service.Revision(),
		State: model.session.ViewState(), Selection: app.EmptySelection(),
		Target: model.focusedMutationTarget(),
		Input:  app.EditInput{Scope: app.EditScopeEntity, Label: "Renamed Example Merchant"},
	})
	require.NoError(t, err)
	model.refreshPreserving("")
	model = press(t, model, keyRune('w'))
	updated, command := model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	model = updated.(Model)
	require.NotNil(t, command)

	for _, reopen := range []bool{false, true} {
		if reopen {
			model, err = NewModel(t.Context(), model.service, app.NewSession(), Options{})
			require.NoError(t, err)
			model = press(t, model, keyRune('w'))
		}
		frame := strings.Join(model.RenderScreen().Frame.PlainLines(), "\n")
		assert.Contains(t, frame, "Rename merchant", "reopened=%v", reopen)
		assert.Contains(t, frame, "From: Example Merchant", "reopened=%v", reopen)
		assert.Contains(t, frame, "To:   Renamed Example Merchant", "reopened=%v", reopen)
		assert.Contains(t, frame, "3 transactions", "reopened=%v", reopen)
	}
}

func TestProviderWriteOverlayExplainsRejectedEditAndRecovery(t *testing.T) {
	t.Parallel()
	model := newTestModel(t, app.NewSession())
	model.overlay = overlayProviderWrite
	model.providerWrite.status = app.ProviderWriteStatus{
		Phase: store.WritePhaseAttentionRequired, AttentionClass: store.WriteAttentionReconcileOnly,
		AttentionReason: store.WriteAttentionRejected, Total: 1, Remaining: 1,
	}
	frame := model.RenderScreen().Frame
	rendered := strings.Join(frame.PlainLines(), "\n")
	assert.Contains(t, rendered, "rejected")
	assert.Contains(t, rendered, "discard")
	assert.Contains(t, rendered, "reload")
	assert.Contains(t, rendered, "Esc=Close")
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

func TestRejectedProviderWriteCanCloseAndReload(t *testing.T) {
	t.Parallel()
	fixture := rejectedProviderWriteModel(t)
	model := press(t, fixture.model, tea.KeyPressMsg{Code: tea.KeyEscape})
	assert.Equal(t, overlayNone, model.overlay)
	assert.Equal(t, 1, model.service.Pending().ActiveOperations)
	assert.Contains(t, strings.Join(model.RenderScreen().Frame.PlainLines(), "\n"), "w Write status")
	model = press(t, model, keyRune('w'))
	updated, command := model.Update(keyRune('s'))
	model = updated.(Model)
	require.NotNil(t, command)
	updated, _ = model.Update(command())
	model = updated.(Model)
	assert.Equal(t, overlayNone, model.overlay)
	status, err := model.service.ProviderWriteStatus(t.Context())
	require.NoError(t, err)
	assert.Empty(t, status.Phase)
	assert.Zero(t, model.service.Pending().ActiveOperations)
	assert.Equal(t, 2, model.result.FilteredCount)
	model = press(t, model, keyRune('m'))
	assert.Equal(t, overlayMerchantEditor, model.overlay, "editing resumes after recovery")
}

func TestProviderWriteReloadShowsActivityAndPreventsDuplicateRequests(t *testing.T) {
	t.Parallel()
	fixture := rejectedProviderWriteModel(t)
	model := fixture.model
	updated, command := model.Update(keyRune('s'))
	model = updated.(Model)
	require.NotNil(t, command)
	rendered := strings.Join(model.RenderScreen().Frame.PlainLines(), "\n")
	assert.Contains(t, rendered, "Reloading Monarch data")
	updated, duplicate := model.Update(keyRune('s'))
	model = updated.(Model)
	assert.Nil(t, duplicate, "a repeated key must not launch another reload")
	model = press(t, model, tea.KeyPressMsg{Code: tea.KeyEscape})
	assert.Equal(t, overlayNone, model.overlay, "reload must not trap the user in the dialog")
	updated, _ = model.Update(command())
	model = updated.(Model)
	assert.Zero(t, model.service.Pending().ActiveOperations)
}

func TestProviderWriteReloadFailureIsVisibleAndCanRetry(t *testing.T) {
	t.Parallel()
	fixture := rejectedProviderWriteModel(t)
	fixture.source.setFetch(func(context.Context, provider.ProgressFunc) (domain.ImportSnapshot, error) {
		return domain.ImportSnapshot{}, provider.NewError(provider.CodeUnavailable)
	})
	updated, command := fixture.model.Update(keyRune('s'))
	model := updated.(Model)
	require.NotNil(t, command)
	updated, _ = model.Update(command())
	model = updated.(Model)
	rendered := strings.Join(model.RenderScreen().Frame.PlainLines(), "\n")
	assert.Contains(t, rendered, "unavailable")
	assert.Equal(t, 1, model.service.Pending().ActiveOperations)
	assert.Contains(t, rendered, "s=")
	fixture.source.setSnapshot(tuiProviderSnapshot(t, fixture.now.Add(time.Minute), 2))
	updated, command = model.Update(keyRune('s'))
	model = updated.(Model)
	require.NotNil(t, command)
	updated, _ = model.Update(command())
	model = updated.(Model)
	assert.Equal(t, overlayNone, model.overlay)
	assert.Zero(t, model.service.Pending().ActiveOperations)
}

func TestProviderWriteStaleReloadKeepsRecoveryAvailable(t *testing.T) {
	t.Parallel()
	fixture := rejectedProviderWriteModel(t)
	model := fixture.model
	model.providerWrite.status.Version-- // A newer batch status has not reached this renderer yet.
	updated, command := model.Update(keyRune('s'))
	model = updated.(Model)
	require.NotNil(t, command)
	updated, _ = model.Update(command())
	model = updated.(Model)
	rendered := strings.Join(model.RenderScreen().Frame.PlainLines(), "\n")
	assert.Contains(t, rendered, "The requested operation is invalid.")
	assert.Contains(t, rendered, "s=")
	updated, command = model.Update(keyRune('s'))
	model = updated.(Model)
	require.NotNil(t, command)
	updated, _ = model.Update(command())
	model = updated.(Model)
	assert.Equal(t, overlayNone, model.overlay)
	assert.Zero(t, model.service.Pending().ActiveOperations)
}

func rejectedProviderWriteModel(t testing.TB) providerModelFixture {
	t.Helper()
	fixture := newProviderModel(t, 2)
	fixture.source.writer = rejectedTUIProviderWriter{tuiProviderWriter{identity: fixture.source.identity}}
	model := press(t, fixture.model, keyRune('h'))
	model = press(t, model, keyRune('w'))
	updated, command := model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	model = updated.(Model)
	require.NotNil(t, command)
	updated, _ = model.Update(command())
	fixture.model = updated.(Model)
	require.Equal(t, store.WriteAttentionRejected, fixture.model.providerWrite.status.AttentionReason)
	return fixture
}

type rejectedTUIProviderWriter struct{ tuiProviderWriter }

func (rejectedTUIProviderWriter) UpdateTransaction(context.Context, provider.TransactionUpdate) (provider.TransactionUpdateResult, error) {
	return provider.TransactionUpdateResult{}, provider.NewWriteFailure(provider.WriteRejected)
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
		{name: "ownerless provider reconciliation", status: app.ProviderWriteStatus{Phase: store.WritePhaseReconciling, ResumeTarget: store.WriteResumeReconciling, Version: 1}},
		{name: "eligible rate limit", status: app.ProviderWriteStatus{Phase: store.WritePhaseRateLimited, Version: 1, NextEligible: now}, wantStart: true},
		{name: "healed reconnect", status: app.ProviderWriteStatus{Phase: store.WritePhaseReconnectRequired, ResumeTarget: store.WriteResumeWriting, Version: 1, SessionChanged: true}, wantStart: true},
		{name: "healed reconnect during reconciliation", status: app.ProviderWriteStatus{Phase: store.WritePhaseReconnectRequired, ResumeTarget: store.WriteResumeReconciling, Version: 1, SessionChanged: true}},
		{name: "confirmation waits", status: app.ProviderWriteStatus{Phase: store.WritePhaseReconcileConfirmationRequired, ResumeTarget: store.WriteResumeReconciling, Version: 1}},
		{name: "paused", status: app.ProviderWriteStatus{Phase: store.WritePhasePaused, Version: 1}},
		{name: "attention", status: app.ProviderWriteStatus{Phase: store.WritePhaseAttentionRequired, Version: 1, AttentionClass: store.WriteAttentionRetryable}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			model := newTestModel(t, app.NewSession())
			model.profileKind = "monarch"
			updated, command := model.Update(providerStatusMsg{
				writeStatus: test.status, at: now,
				timerGeneration: model.provider.timerGeneration,
			})
			model = updated.(Model)
			assert.Equal(t, test.wantStart, model.providerWrite.running)
			assert.NotNil(t, command)
			if test.status.ResumeTarget == store.WriteResumeReconciling {
				model.overlay = overlayProviderWrite
				assert.Contains(t, model.RenderScreen().Frame.RenderANSI(), "s=", "recovery must remain discoverable")
				updated, command = model.Update(keyRune('s'))
				model = updated.(Model)
				assert.True(t, model.providerWrite.reconciling, "full reload requires an explicit recovery action")
				assert.NotNil(t, command)
			}
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
	result := provider.TransactionUpdateResult{TransactionExternalID: update.TransactionExternalID, CategoryCleared: update.ClearCategory}
	if update.MerchantExternalID.Present {
		result.MerchantExternalID = update.MerchantExternalID
	}
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

func (tuiProviderWriter) DeleteTransaction(
	_ context.Context,
	externalID string,
) (provider.TransactionDeleteResult, error) {
	return provider.TransactionDeleteResult{TransactionExternalID: externalID}, nil
}
