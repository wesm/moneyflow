package app_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/wesm/moneyflow/internal/app"
	"github.com/wesm/moneyflow/internal/provider"
	"github.com/wesm/moneyflow/internal/store"
)

func TestProviderSchedulerClassifiesEveryStableFailureExactlyOnce(t *testing.T) {
	t.Parallel()

	retryable := map[provider.ErrorCode]bool{
		provider.CodeSnapshotUnstable:  true,
		provider.CodeRateLimited:       true,
		provider.CodeUnavailable:       true,
		provider.CodeRefreshInProgress: true,
	}
	for _, code := range provider.ErrorCodes() {
		want := app.ProviderManualActionRequired
		if retryable[code] {
			want = app.ProviderBoundedRetry
		}
		assert.Equal(t, want, app.ProviderErrorRetryClass(code), code)
	}
	assert.Empty(t, app.ProviderErrorRetryClass(provider.ErrorCode("unknown")))
	for _, code := range []store.ErrorCode{
		store.CodeRevisionConflict, store.CodeInvalidOperation, store.CodeInvalidTarget,
		store.CodeStoreBusy, store.CodeStoreError, store.CodeSchemaNewer,
		store.CodeSchemaIncompatible, store.CodeStoreCorrupt, store.CodeJournalFull,
	} {
		assert.Equal(t, app.ProviderManualActionRequired, app.StoreErrorRetryClass(code), code)
	}
	assert.Empty(t, app.StoreErrorRetryClass(store.ErrorCode("unknown")))
}

func TestProviderSchedulerRecordsRetryAfterAndDoesNotSpinBeforeEligible(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	service, _ := newProviderRefreshService(t)
	now := time.Date(2026, time.August, 15, 23, 0, 0, 0, time.UTC)
	source := &fakeProviderSource{
		identity: provider.ProfileIdentity{Kind: "monarch", RemoteID: "subscription-example"},
		snapshot: providerSnapshot(t, now, 2), fingerprint: "session-a",
		fetchErr: provider.NewErrorWithRetry(provider.CodeRateLimited, 90*time.Minute),
	}
	configureProviderRefreshService(t, service, source, now, "instance-a")
	result, err := service.RefreshProvider(ctx, app.ProviderRefreshRequest{
		Manual: true, State: app.DefaultViewState(), Selection: app.EmptySelection(),
	})
	assertProviderAppCode(t, err, provider.CodeRateLimited)
	assert.Equal(t, now.Add(90*time.Minute), result.Status.NextEligible)
	assert.Equal(t, 1, source.fetchCalls())

	result, err = service.RefreshProvider(ctx, app.ProviderRefreshRequest{
		Manual: false, State: app.DefaultViewState(), Selection: app.EmptySelection(),
	})
	require.NoError(t, err)
	assert.Equal(t, provider.CodeRateLimited, result.Status.Code)
	assert.Equal(t, 1, source.fetchCalls(), "an ineligible status tick must not retry")
}

func TestProviderReconnectParkHealsOnlyAfterSessionFingerprintChanges(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	service, _ := newProviderRefreshService(t)
	now := time.Date(2026, time.August, 15, 23, 30, 0, 0, time.UTC)
	source := &fakeProviderSource{
		identity: provider.ProfileIdentity{Kind: "monarch", RemoteID: "subscription-example"},
		snapshot: providerSnapshot(t, now, 2), fingerprint: "session-a",
		probeErr: provider.NewError(provider.CodeReconnectRequired),
	}
	configureProviderRefreshService(t, service, source, now, "instance-a")
	_, err := service.RefreshProvider(ctx, app.ProviderRefreshRequest{
		Manual: true, State: app.DefaultViewState(), Selection: app.EmptySelection(),
	})
	assertProviderAppCode(t, err, provider.CodeReconnectRequired)
	probeCalls := source.probeCalls()
	healed, err := service.ProviderStatus(ctx)
	require.NoError(t, err)
	assert.Equal(t, provider.CodeReconnectRequired, healed.Code)
	assert.Equal(t, probeCalls, source.probeCalls(), "status checks must not repeat failed auth")
	_, err = service.RefreshProvider(ctx, app.ProviderRefreshRequest{
		Manual: false, State: app.DefaultViewState(), Selection: app.EmptySelection(),
	})
	require.NoError(t, err)
	assert.Equal(t, probeCalls, source.probeCalls(), "automatic refresh must remain parked")

	source.setProbeError(nil)
	source.setFingerprint("session-b")
	healed, err = service.ProviderStatus(ctx)
	require.NoError(t, err)
	assert.Empty(t, healed.Code)
	assert.Equal(t, probeCalls, source.probeCalls())
	_, err = service.RefreshProvider(ctx, app.ProviderRefreshRequest{
		Manual: false, State: app.DefaultViewState(), Selection: app.EmptySelection(),
	})
	require.NoError(t, err)
	assert.Greater(t, source.probeCalls(), probeCalls)
	assert.GreaterOrEqual(t, source.reloadCalls(), 2)
}

func TestProviderSchedulerSixHourStalenessAndNextEligible(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.August, 15, 21, 0, 0, 0, time.UTC)
	assert.True(t, app.ProviderRefreshDue(app.ProviderStatus{}, now))
	assert.False(t, app.ProviderRefreshDue(app.ProviderStatus{LastSuccess: now}, now))
	assert.False(t, app.ProviderRefreshDue(app.ProviderStatus{
		LastSuccess: now.Add(-7 * time.Hour), NextEligible: now.Add(time.Minute),
	}, now))
	assert.False(t, app.ProviderRefreshDue(app.ProviderStatus{
		LastSuccess: now.Add(-6*time.Hour + time.Millisecond),
	}, now))
	assert.True(t, app.ProviderRefreshDue(app.ProviderStatus{
		LastSuccess: now.Add(-6 * time.Hour),
	}, now))
	for _, code := range []provider.ErrorCode{
		provider.CodeReconnectRequired,
		provider.CodeIdentityMismatch,
		provider.CodeDeletionConfirmationRequired,
		provider.CodeConfirmationInvalid,
		provider.CodeDataInvalid,
		provider.CodeRefreshStale,
	} {
		assert.False(t, app.ProviderRefreshDue(app.ProviderStatus{Code: code}, now), code)
	}
}
