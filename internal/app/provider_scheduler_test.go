package app_test

import (
	"context"
	"os"
	"path/filepath"
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
	assert.True(t, result.ReadProvider, "a refresh whose read failed still read the provider")

	result, err = service.RefreshProvider(ctx, app.ProviderRefreshRequest{
		Manual: false, State: app.DefaultViewState(), Selection: app.EmptySelection(),
	})
	require.NoError(t, err)
	assert.Equal(t, provider.CodeRateLimited, result.Status.Code)
	assert.Equal(t, 1, source.fetchCalls(), "an ineligible status tick must not retry")
	assert.False(t, result.ReadProvider, "a refresh with nothing due reads nothing")
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
	healed, err = service.ProviderStatus(ctx)
	require.NoError(t, err)
	assert.Empty(t, healed.Code, "later status polls must not revive the old session error")
	assert.Equal(t, probeCalls, source.probeCalls())
	_, err = service.RefreshProvider(ctx, app.ProviderRefreshRequest{
		Manual: false, State: app.DefaultViewState(), Selection: app.EmptySelection(),
	})
	require.NoError(t, err)
	assert.Equal(t, probeCalls, source.probeCalls(), "reconnecting must not download Monarch history automatically")
	_, err = service.RefreshProvider(ctx, app.ProviderRefreshRequest{
		Manual: true, State: app.DefaultViewState(), Selection: app.EmptySelection(),
	})
	require.NoError(t, err)
	assert.Greater(t, source.probeCalls(), probeCalls)
	assert.GreaterOrEqual(t, source.reloadCalls(), 2)
}

func TestProviderWriteClearsOnlyObsoleteReconnectFailure(t *testing.T) {
	for _, test := range []struct {
		name       string
		refreshErr provider.ErrorCode
		writeFails bool
	}{
		{name: "reconnected", refreshErr: provider.CodeReconnectRequired},
		{name: "unrelated refresh failure", refreshErr: provider.CodeUnavailable},
		{name: "write rejected", refreshErr: provider.CodeReconnectRequired, writeFails: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			ctx := t.Context()
			service, handle := newProviderRefreshService(t)
			now := providerWriteTime()
			reader := &fakeProviderSource{
				identity: provider.ProfileIdentity{Kind: "monarch", RemoteID: "subscription-example"},
				snapshot: providerSnapshot(t, now, 1), fingerprint: "session-a",
			}
			writer := &scriptedProviderWriter{identity: reader.identity}
			if test.writeFails {
				writer.update = func(provider.TransactionUpdate) (provider.TransactionUpdateResult, error) {
					return provider.TransactionUpdateResult{}, provider.NewWriteFailure(provider.WriteRejected)
				}
			}
			source := &writeProviderSource{fakeProviderSource: reader, writer: writer}
			configureProviderRefreshService(t, service, source, now, "reconnect-write-test")
			request := app.ProviderRefreshRequest{Manual: true, State: app.DefaultViewState(), Selection: app.EmptySelection()}
			_, err := service.RefreshProvider(ctx, request)
			require.NoError(t, err)
			configureProviderRefreshService(t, service, source, now.Add(8*time.Hour), "reconnect-write-test")
			reader.setProbeError(provider.NewError(test.refreshErr))
			_, err = service.RefreshProvider(ctx, request)
			assertProviderAppCode(t, err, test.refreshErr)
			before, err := handle.ProviderState(ctx)
			require.NoError(t, err)
			reader.setProbeError(nil)
			reader.setFingerprint("session-b")
			_, err = service.ProviderStatus(ctx)
			require.NoError(t, err)

			loaded, err := handle.Load(ctx)
			require.NoError(t, err)
			_, err = service.Mutate(ctx, app.MutationRequest{
				Action: app.ActionToggleHidden, ExpectedRevision: loaded.Revision,
				State: detailViewState(), Selection: app.EmptySelection(),
				Target: &app.RowTarget{Kind: app.IdentityTransaction, Identity: string(loaded.Committed.Transactions[0].ID)},
			})
			require.NoError(t, err)
			_, err = service.Commit(ctx, app.CommitRequest{
				ExpectedRevision: service.Revision(), ReviewedRevision: service.Revision(),
				State: app.DefaultViewState(), Selection: app.EmptySelection(),
			})
			require.NoError(t, err)
			completed, err := service.RunProviderWrite(ctx)
			if test.writeFails {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				assert.Empty(t, completed.Phase)
			}
			assert.Equal(t, 1, writer.callCount())
			assert.Equal(t, 1, reader.fetchCalls(), "a write must not fetch transaction history")
			after, err := handle.ProviderState(ctx)
			require.NoError(t, err)
			wantRefresh := before.Refresh
			if test.refreshErr == provider.CodeReconnectRequired && !test.writeFails {
				wantRefresh.StatusCode = ""
			}
			assert.Equal(t, wantRefresh, after.Refresh, "keep cache freshness and unrelated errors")
			status, err := service.ProviderStatus(ctx)
			require.NoError(t, err)
			assert.Equal(t, provider.ErrorCode(wantRefresh.StatusCode), status.Code)
		})
	}
}

func TestMonarchRefreshDueChoosesRecentOrFullByAge(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.August, 15, 21, 0, 0, 0, time.UTC)
	day := 24 * time.Hour
	tests := []struct {
		name        string
		lastSuccess time.Duration
		lastFull    time.Duration
		unknownFull bool
		code        provider.ErrorCode
		eligibleIn  time.Duration
		want        app.ProviderRefreshScope
	}{
		{name: "fresh", lastSuccess: time.Hour, lastFull: time.Hour, want: app.ProviderRefreshNone},
		{name: "just under six hours", lastSuccess: 6*time.Hour - time.Millisecond,
			lastFull: 2 * day, want: app.ProviderRefreshNone},
		{name: "six hours", lastSuccess: 6 * time.Hour, lastFull: 2 * day,
			want: app.ProviderRefreshRecent},
		{name: "full just under thirty days", lastSuccess: 7 * time.Hour,
			lastFull: 30*day - time.Millisecond, want: app.ProviderRefreshRecent},
		{name: "full thirty days", lastSuccess: 7 * time.Hour, lastFull: 30 * day,
			want: app.ProviderRefreshFull},
		{name: "full due while recent is fresh", lastSuccess: time.Hour, lastFull: 31 * day,
			want: app.ProviderRefreshFull},
		{name: "no full record", lastSuccess: time.Hour, unknownFull: true,
			want: app.ProviderRefreshFull},
		{name: "never imported", unknownFull: true, want: app.ProviderRefreshNone},
		{name: "manual action", lastSuccess: 7 * time.Hour, lastFull: 31 * day,
			code: provider.CodeDeletionConfirmationRequired, want: app.ProviderRefreshNone},
		{name: "retry floor", lastSuccess: 7 * time.Hour, lastFull: 31 * day,
			code: provider.CodeUnavailable, eligibleIn: time.Minute, want: app.ProviderRefreshNone},
		{name: "retry floor passed", lastSuccess: 7 * time.Hour, lastFull: 2 * day,
			code: provider.CodeUnavailable, eligibleIn: -time.Minute,
			want: app.ProviderRefreshRecent},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			status := app.ProviderStatus{ProviderKind: "monarch", Code: test.code}
			if test.lastSuccess != 0 {
				status.LastSuccess = now.Add(-test.lastSuccess)
			}
			if !test.unknownFull {
				status.LastFullRefresh = now.Add(-test.lastFull)
			}
			if test.eligibleIn != 0 {
				status.NextEligible = now.Add(test.eligibleIn)
			}
			assert.Equal(t, test.want, app.ProviderRefreshDue(status, now))
		})
	}
}

// A manual Monarch refresh reads the recent window, unless that window could miss changes the
// local data has not seen: then it reads complete history, as the schedule would.
func TestManualRefreshScopeReadsRecentDataUnlessHistoryCouldBeMissed(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.August, 15, 21, 0, 0, 0, time.UTC)
	day := 24 * time.Hour
	tests := []struct {
		name        string
		kind        string
		lastSuccess time.Duration
		lastFull    time.Duration
		unknownFull bool
		code        provider.ErrorCode
		want        app.ProviderRefreshScope
	}{
		{name: "fresh", kind: "monarch", lastSuccess: time.Minute, lastFull: time.Minute,
			want: app.ProviderRefreshRecent},
		{name: "full just under thirty days", kind: "monarch", lastSuccess: time.Hour,
			lastFull: 30*day - time.Millisecond, want: app.ProviderRefreshRecent},
		{name: "full thirty days", kind: "monarch", lastSuccess: time.Hour, lastFull: 30 * day,
			want: app.ProviderRefreshFull},
		{name: "no full record", kind: "monarch", lastSuccess: time.Hour, unknownFull: true,
			want: app.ProviderRefreshFull},
		{name: "never imported", kind: "monarch", unknownFull: true,
			want: app.ProviderRefreshFull},
		{name: "after a failure", kind: "monarch", lastSuccess: 7 * time.Hour, lastFull: day,
			code: provider.CodeDeletionConfirmationRequired, want: app.ProviderRefreshRecent},
		{name: "YNAB", kind: "ynab", lastSuccess: time.Hour, lastFull: time.Hour,
			want: app.ProviderRefreshFull},
		{name: "SimpleFIN", kind: "simplefin", lastSuccess: time.Hour, lastFull: time.Hour,
			want: app.ProviderRefreshFull},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			status := app.ProviderStatus{ProviderKind: test.kind, Code: test.code}
			if test.lastSuccess != 0 {
				status.LastSuccess = now.Add(-test.lastSuccess)
			}
			if !test.unknownFull {
				status.LastFullRefresh = now.Add(-test.lastFull)
			}
			assert.Equal(t, test.want, app.ManualRefreshScope(status, now))
		})
	}
}

func TestMonarchMissingOrCorruptRefreshRecordMakesFullRefreshDue(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	service, paths := newProviderRefreshServiceAt(t)
	now := time.Date(2026, time.August, 15, 12, 0, 0, 0, time.UTC)
	source := &fakeProviderSource{
		identity: provider.ProfileIdentity{Kind: "monarch", RemoteID: "subscription-example"},
		snapshot: providerSnapshot(t, now, 2), fingerprint: "session-a",
	}
	configureProviderRefreshService(t, service, source, now, "instance-a")
	_, err := service.RefreshProvider(ctx, app.ProviderRefreshRequest{
		Manual: true, State: app.DefaultViewState(), Selection: app.EmptySelection(),
	})
	require.NoError(t, err)
	status, err := service.ProviderStatus(ctx)
	require.NoError(t, err)
	assert.Equal(t, now, status.LastFullRefresh)
	assert.Equal(t, app.ProviderRefreshNone, status.RefreshDue)

	record := filepath.Join(paths.Root, "provider-refresh.json")
	require.NoError(t, os.WriteFile(record, []byte("{not json"), 0o600))
	status, err = service.ProviderStatus(ctx)
	require.NoError(t, err)
	assert.True(t, status.LastFullRefresh.IsZero())
	assert.Equal(t, app.ProviderRefreshFull, status.RefreshDue)

	require.NoError(t, os.Remove(record))
	status, err = service.ProviderStatus(ctx)
	require.NoError(t, err)
	assert.Equal(t, app.ProviderRefreshFull, status.RefreshDue)
}

func TestProviderSchedulerSixHourStalenessAndNextEligible(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.August, 15, 21, 0, 0, 0, time.UTC)
	none, full := app.ProviderRefreshNone, app.ProviderRefreshFull
	assert.Equal(t, full, app.ProviderRefreshDue(app.ProviderStatus{}, now))
	assert.Equal(t, none, app.ProviderRefreshDue(app.ProviderStatus{LastSuccess: now}, now))
	assert.Equal(t, none, app.ProviderRefreshDue(app.ProviderStatus{
		LastSuccess: now.Add(-7 * time.Hour), NextEligible: now.Add(time.Minute),
	}, now))
	assert.Equal(t, none, app.ProviderRefreshDue(app.ProviderStatus{
		LastSuccess: now.Add(-6*time.Hour + time.Millisecond),
	}, now))
	assert.Equal(t, full, app.ProviderRefreshDue(app.ProviderStatus{
		LastSuccess: now.Add(-6 * time.Hour),
	}, now))
	for _, code := range []provider.ErrorCode{
		provider.CodeReconnectRequired,
		provider.CodeIdentityMismatch,
		provider.CodeMoneyMismatch,
		provider.CodeDeletionConfirmationRequired,
		provider.CodeConfirmationInvalid,
		provider.CodeDataInvalid,
		provider.CodeRefreshStale,
	} {
		assert.Equal(t, none, app.ProviderRefreshDue(app.ProviderStatus{Code: code}, now), code)
	}
}

func TestProviderStatusReportsOnlyALiveRefreshLeaseAsRunning(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	service, handle := newProviderRefreshService(t)
	now := time.Date(2026, time.August, 16, 9, 0, 0, 0, time.UTC)
	source := &fakeProviderSource{
		identity: provider.ProfileIdentity{Kind: "monarch", RemoteID: "subscription-example"},
		snapshot: providerSnapshot(t, now, 2), fingerprint: "session-a",
	}
	clock := now
	require.NoError(t, service.ConfigureProvider(app.ProviderRuntime{
		ReadSource: source, Provider: "monarch", Currency: "USD", Scale: 2,
		Renderer: "web", InstanceID: "instance-web",
		Now: func() time.Time { return clock }, Random: &incrementingReader{},
	}))
	_, err := service.RefreshProvider(ctx, app.ProviderRefreshRequest{
		Manual: true, State: app.DefaultViewState(), Selection: app.EmptySelection(),
	})
	require.NoError(t, err)
	status, err := service.ProviderStatus(ctx)
	require.NoError(t, err)
	assert.False(t, status.Refreshing, "a finished refresh released its lease")

	_, acquired, err := handle.AcquireRefreshLease(ctx, store.RefreshLease{
		OwnerID: "instance-terminal", Renderer: "tui", ExpiresAt: now.Add(time.Minute),
	}, now)
	require.NoError(t, err)
	require.True(t, acquired)
	status, err = service.ProviderStatus(ctx)
	require.NoError(t, err)
	assert.True(t, status.Refreshing)
	assert.Equal(t, "tui", status.OwnerRenderer)

	clock = now.Add(2 * time.Minute)
	status, err = service.ProviderStatus(ctx)
	require.NoError(t, err)
	assert.False(t, status.Refreshing, "an expired lease belongs to a process that stopped")
}

func TestStoredProviderStatusReportsFreshnessWithoutAProviderRuntime(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	configured, handle := newProviderRefreshService(t)
	refreshedAt := time.Date(2026, time.August, 16, 9, 0, 0, 0, time.UTC)
	source := &fakeProviderSource{
		identity: provider.ProfileIdentity{Kind: "monarch", RemoteID: "subscription-example"},
		snapshot: providerSnapshot(t, refreshedAt, 2), fingerprint: "session-a",
	}
	configureProviderRefreshService(t, configured, source, refreshedAt, "instance-a")
	_, err := configured.RefreshProvider(ctx, app.ProviderRefreshRequest{
		Manual: true, State: app.DefaultViewState(), Selection: app.EmptySelection(),
	})
	require.NoError(t, err)

	locked, err := app.NewProfileService(ctx, handle)
	require.NoError(t, err)
	locked.SetClock(func() time.Time { return refreshedAt.Add(7 * time.Hour) })
	status, err := locked.StoredProviderStatus(ctx)
	require.NoError(t, err)
	assert.Equal(t, "monarch", status.ProviderKind)
	assert.Equal(t, refreshedAt, status.LastSuccess.UTC())
	assert.Empty(t, status.Code)
	assert.Equal(t, app.ProviderRefreshRecent, status.RefreshDue)
	assert.False(t, status.Refreshing)
}

func TestStoredProviderStatusIsEmptyForAnUnboundProfile(t *testing.T) {
	t.Parallel()

	service, _ := newProviderRefreshService(t)
	status, err := service.StoredProviderStatus(t.Context())
	require.NoError(t, err)
	assert.Equal(t, app.ProviderStatus{}, status)
}

func TestProviderStatusStartsANewRefreshWithoutTheLastRefreshProgress(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	service, _ := newProviderRefreshService(t)
	now := time.Date(2026, time.August, 16, 9, 0, 0, 0, time.UTC)
	source := &fakeProviderSource{
		identity: provider.ProfileIdentity{Kind: "monarch", RemoteID: "subscription-example"},
		snapshot: providerSnapshot(t, now, 2), fingerprint: "session-a",
	}
	configureProviderRefreshService(t, service, source, now, "instance-a")
	request := app.ProviderRefreshRequest{
		Manual: true, State: app.DefaultViewState(), Selection: app.EmptySelection(),
	}
	_, err := service.RefreshProvider(ctx, request)
	require.NoError(t, err)
	finished, err := service.ProviderStatus(ctx)
	require.NoError(t, err)
	require.Equal(t, 2, finished.Total, "the finished refresh reported its progress")

	entered := make(chan struct{})
	release := make(chan struct{})
	source.setFetchHook(func() error {
		close(entered)
		<-release
		return nil
	})
	done := make(chan error, 1)
	go func() {
		_, refreshErr := service.RefreshProvider(ctx, request)
		done <- refreshErr
	}()
	<-entered
	running, err := service.ProviderStatus(ctx)
	require.NoError(t, err)
	assert.True(t, running.Refreshing)
	assert.Zero(t, running.Fetched, "a new refresh has fetched nothing yet")
	assert.Zero(t, running.Total)
	close(release)
	require.NoError(t, <-done)
}
