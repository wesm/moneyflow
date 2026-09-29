package app_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wesm/moneyflow/internal/app"
	"github.com/wesm/moneyflow/internal/domain"
	"github.com/wesm/moneyflow/internal/provider"
	"github.com/wesm/moneyflow/internal/store"
)

func TestAccountProjectionRejectsConcurrentProviderFinalization(t *testing.T) {
	for _, test := range []struct {
		name           string
		pinnedRevision bool
	}{
		{name: "current"},
		{name: "explicit revision", pinnedRevision: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			ctx := t.Context()
			writerService, profile := newProviderRefreshService(t)
			now := time.Date(2026, time.September, 9, 12, 0, 0, 0, time.UTC)
			reader := &fakeProviderSource{
				identity: provider.ProfileIdentity{Kind: "monarch", RemoteID: "subscription-example"},
				snapshot: providerSnapshot(t, now, 1), fingerprint: "session-a",
			}
			source := &writeProviderSource{fakeProviderSource: reader, writer: &scriptedProviderWriter{identity: reader.identity}}
			configureProviderRefreshService(t, writerService, source, now, "writer-process")
			_, err := writerService.RefreshProvider(ctx, app.ProviderRefreshRequest{
				Manual: true, State: app.DefaultViewState(), Selection: app.EmptySelection(),
			})
			require.NoError(t, err)
			loaded, err := profile.Load(ctx)
			require.NoError(t, err)
			revision, err := profile.Append(ctx, loaded.Revision, domain.Operation{
				ID: "operation-projection-race", Type: domain.OperationTransactionHide, PayloadVersion: 1,
				CreatedRevision: loaded.Revision, CreatedAt: now,
				Targets: []domain.EntityID{loaded.Committed.Transactions[0].ID}, HideToggle: &domain.HideTogglePayload{},
			})
			require.NoError(t, err)
			prepared, err := writerService.Commit(ctx, app.CommitRequest{
				ExpectedRevision: revision, ReviewedRevision: revision,
				State: app.DefaultViewState(), Selection: app.EmptySelection(),
			})
			require.NoError(t, err)
			require.NotNil(t, prepared.ProviderWrite)
			observerProfile := &afterRevisionReadProfile{Profile: profile}
			observer, err := app.NewProfileService(ctx, observerProfile)
			require.NoError(t, err)
			// Allow the ordinary worker to finalize after the observer's cache
			// check but before its independent operational-state read.
			observerProfile.afterRead = func() {
				status, runErr := writerService.RunProviderWrite(ctx)
				require.NoError(t, runErr)
				require.Empty(t, status.Phase)
			}
			request := app.AccountProjectionRequest{Partitions: app.CollectionWindowRequest{Limit: 1}}
			if test.pinnedRevision {
				request.ExpectedRevision = observer.Revision()
			}
			projection, err := observer.AccountProjection(ctx, request)
			var appErr *app.AppError
			require.ErrorAs(t, err, &appErr, "must not combine the old pending journal with finalized write status: %+v", projection)
			assert.Equal(t, app.AppRevisionConflict, appErr.Code)
			assert.Equal(t, writerService.Revision(), appErr.CurrentRevision)
			assert.Empty(t, projection)
			current, err := observer.AccountProjection(ctx, app.AccountProjectionRequest{Partitions: app.CollectionWindowRequest{Limit: 1}})
			require.NoError(t, err)
			assert.Equal(t, writerService.Revision(), current.Revision)
			assert.Zero(t, current.Pending.ActiveOperations)
			assert.Empty(t, current.Write.Phase)
		})
	}
}

type afterRevisionReadProfile struct {
	store.Profile
	once      sync.Once
	afterRead func()
}

func (profile *afterRevisionReadProfile) CurrentRevision(ctx context.Context) (uint64, error) {
	revision, err := profile.Profile.CurrentRevision(ctx)
	if err == nil && profile.afterRead != nil {
		profile.once.Do(profile.afterRead)
	}
	return revision, err
}
