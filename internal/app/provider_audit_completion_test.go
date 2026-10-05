package app_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wesm/moneyflow/internal/app"
	"github.com/wesm/moneyflow/internal/domain"
	"github.com/wesm/moneyflow/internal/provider"
	"github.com/wesm/moneyflow/internal/store"
)

func TestProviderCompletionAuditFailurePreservesSuccessAndWarning(t *testing.T) {
	for _, phase := range []string{"net-noop", "finalize", "reconcile"} {
		t.Run(phase, func(t *testing.T) {
			ctx := context.Background()
			_, profile := newProviderRefreshService(t)
			failing := &providerCompletionAuditProfile{Profile: profile, phase: phase}
			service, err := app.NewProfileService(ctx, failing)
			require.NoError(t, err)
			now := providerWriteTime()
			reader := &fakeProviderSource{
				identity: provider.ProfileIdentity{Kind: "monarch", RemoteID: "synthetic-profile"},
				snapshot: providerSnapshot(t, now, 1), fingerprint: "synthetic-session",
			}
			writer := &scriptedProviderWriter{identity: reader.identity}
			if phase == "reconcile" {
				writer.update = func(provider.TransactionUpdate) (provider.TransactionUpdateResult, error) {
					return provider.TransactionUpdateResult{}, provider.NewWriteFailure(provider.WriteRejected)
				}
			}
			source := &writeProviderSource{fakeProviderSource: reader, writer: writer}
			configureProviderRefreshService(t, service, source, now, "audit-completion-instance")
			_, err = service.RefreshProvider(ctx, app.ProviderRefreshRequest{
				Manual: true, State: app.DefaultViewState(), Selection: app.EmptySelection(),
			})
			require.NoError(t, err)
			before, err := profile.Load(ctx)
			require.NoError(t, err)
			target := before.Committed.Transactions[0]
			operation := domain.Operation{
				ID: "audit-completion-edit", Type: domain.OperationTransactionHide, PayloadVersion: 1,
				CreatedRevision: before.Revision, CreatedAt: now, Targets: []domain.EntityID{target.ID},
				HideToggle: &domain.HideTogglePayload{},
			}
			if phase == "net-noop" {
				operation.Type = domain.OperationCategoryAssign
				operation.HideToggle = nil
				operation.Reassign = &domain.ReassignPayload{DestinationID: target.CategoryID}
			}
			revision, err := profile.Append(ctx, before.Revision, operation)
			require.NoError(t, err)
			committed, err := service.Commit(ctx, app.CommitRequest{
				ExpectedRevision: revision, ReviewedRevision: revision,
				State: app.DefaultViewState(), Selection: app.EmptySelection(),
			})
			require.NoError(t, err)
			if phase == "net-noop" {
				assert.Nil(t, committed.ProviderWrite)
				assert.Greater(t, committed.Revision, revision)
				assert.Zero(t, committed.Pending.ActiveOperations)
				assert.Zero(t, writer.callCount())
			} else {
				require.NotNil(t, committed.ProviderWrite)
				type outcome struct {
					status app.ProviderWriteStatus
					err    error
				}
				completed := make(chan outcome, 1)
				go func() {
					status, runErr := service.RunProviderWrite(ctx)
					completed <- outcome{status: status, err: runErr}
				}()
				ran := <-completed
				if phase == "finalize" {
					require.NoError(t, ran.err)
					assert.Empty(t, ran.status.Phase)
					assert.NotEmpty(t, ran.status.AuditWarning)
				} else {
					require.Error(t, ran.err)
					reconciled, reconcileErr := service.StopAndReconcileProviderWrite(ctx, app.ProviderWriteReconcileRequest{
						ExpectedVersion: ran.status.Version, State: app.DefaultViewState(), Selection: app.EmptySelection(),
					})
					require.NoError(t, reconcileErr)
					assert.Empty(t, reconciled.Status.Phase)
					assert.NotEmpty(t, reconciled.Status.AuditWarning)
				}
			}
			require.True(t, failing.reported)
			persisted, err := profile.Load(ctx)
			require.NoError(t, err)
			assert.Empty(t, persisted.Journal)
			assert.Greater(t, persisted.Revision, revision)
			assert.Equal(t, persisted.Revision, service.Revision())
			require.Len(t, persisted.Committed.Transactions, 1)
			assert.Equal(t, phase == "finalize", persisted.Committed.Transactions[0].Hidden)
			for range 2 {
				status, statusErr := service.ProviderWriteStatus(ctx)
				require.NoError(t, statusErr)
				assert.Empty(t, status.Phase)
				assert.NotEmpty(t, status.AuditWarning)
				projection, projectionErr := service.ProjectView(app.DefaultViewState(), app.EmptySelection(), app.WindowRequest{})
				require.NoError(t, projectionErr)
				assert.Equal(t, persisted.Revision, projection.Revision)
				assert.Zero(t, projection.Pending.ActiveOperations)
				assert.Zero(t, projection.Pending.InactiveOperations)
				assert.Equal(t, status.AuditWarning, projection.AuditWarning)
			}
		})
	}
}

// Complete the real store operation before injecting the audit export failure.
type providerCompletionAuditProfile struct {
	store.Profile
	phase    string
	reported bool
}

func (profile *providerCompletionAuditProfile) PrepareProviderWrite(ctx context.Context, request store.PrepareProviderWriteRequest, planner store.PrepareProviderWritePlanner) (store.PrepareProviderWriteCommit, error) {
	result, err := profile.Profile.PrepareProviderWrite(ctx, request, planner)
	if err == nil && profile.phase == "net-noop" {
		profile.reported = true
		return result, store.NewAuditCompletionError(errors.New("synthetic audit sink failure"))
	}
	return result, err
}

func (profile *providerCompletionAuditProfile) FinalizeProviderWrite(ctx context.Context, request store.FinalizeProviderWriteRequest, planner store.FinalizeProviderWritePlanner) (store.FinalizeProviderWriteCommit, error) {
	result, err := profile.Profile.FinalizeProviderWrite(ctx, request, planner)
	if err == nil && profile.phase == "finalize" {
		profile.reported = true
		return result, store.NewAuditCompletionError(errors.New("synthetic audit sink failure"))
	}
	return result, err
}

func (profile *providerCompletionAuditProfile) ReconcileProviderWrite(ctx context.Context, request store.ReconcileProviderWriteRequest, planner store.RefreshPlanner) (store.RefreshCommit, error) {
	result, err := profile.Profile.ReconcileProviderWrite(ctx, request, planner)
	if err == nil && profile.phase == "reconcile" {
		profile.reported = true
		return result, store.NewAuditCompletionError(errors.New("synthetic audit sink failure"))
	}
	return result, err
}
