package app_test

import (
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/wesm/moneyflow/internal/app"
	"github.com/wesm/moneyflow/internal/domain"
	"github.com/wesm/moneyflow/internal/provider"
)

func TestMonarchRecentRefreshKeepsOlderHistoryAndItsEdits(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	service, handle, _ := openProviderRefreshProfile(t)
	start := time.Date(2026, time.August, 15, 12, 0, 0, 0, time.UTC)
	snapshot := monarchHistorySnapshot(t, start, 10, 3)
	source := &fakeProviderSource{
		identity: monarchTestIdentity(),
		snapshot: snapshot, fingerprint: "session-a",
	}
	configureProviderRefreshService(t, service, source, start, "instance-a")
	_, err := service.RefreshProvider(ctx, manualProviderRefresh())
	require.NoError(t, err)
	loaded, err := handle.Load(ctx)
	require.NoError(t, err)
	older := committedProviderRow(t, loaded.Committed, "transaction-old-00")
	merchantID := providerEntityID(
		t, loaded.Committed, domain.EntityKindMerchant, "merchant-example",
	)
	_, err = handle.Append(ctx, loaded.Revision, domain.Operation{
		ID: "operation_old_reassign", Type: domain.OperationMerchantReassign, PayloadVersion: 1,
		CreatedRevision: loaded.Revision, CreatedAt: start, Targets: []domain.EntityID{older.ID},
		Reassign: &domain.ReassignPayload{DestinationID: merchantID},
	})
	require.NoError(t, err)
	_, err = service.Refresh(ctx)
	require.NoError(t, err)

	// Monarch edits one recent row and no longer lists the older rows' merchant.
	later := start.Add(7 * time.Hour)
	changed := monarchHistorySnapshot(t, later, 10, 3)
	changed.Transactions[1].Notes = "Edited in Monarch"
	changed.Merchants = changed.Merchants[:1]
	changed.Transactions = changed.Transactions[:10]
	source.setSnapshot(changed)
	configureProviderRefreshService(t, service, source, later, "instance-a")
	result, err := service.RefreshProvider(ctx, scheduledProviderRefresh())
	require.NoError(t, err)

	requests := source.fetchRequests()
	require.Len(t, requests, 2)
	assert.Equal(t, &domain.DateRange{
		Start: mustProviderDate(t, "2026-05-17"), End: mustProviderDate(t, "2026-08-15"),
	}, requests[1].TransactionWindow)
	refreshed, err := handle.Load(ctx)
	require.NoError(t, err)
	assert.Equal(t, older, committedProviderRow(t, refreshed.Committed, "transaction-old-00"))
	assert.Equal(t, "Edited in Monarch",
		committedProviderRow(t, refreshed.Committed, changed.Transactions[1].ExternalID).Notes)
	oldMerchant := providerEntityID(
		t, refreshed.Committed, domain.EntityKindMerchant, "merchant-old",
	)
	for _, merchant := range refreshed.Committed.Merchants {
		if merchant.ID == oldMerchant {
			assert.False(t, merchant.Retired, "a merchant used by an older row stays active")
		}
	}
	assert.Len(t, refreshed.Journal, 1, "the edit to an older row survives")
	assert.Zero(t, result.Status.Summary.RemovedTransactions)
	assert.Zero(t, result.Status.Summary.RemovedOperations)
	assert.Equal(t, start, result.Status.LastFullRefresh,
		"a recent refresh does not move the record")
	assert.Equal(t, later, result.Status.LastSuccess)
}

func TestMonarchRecentRefreshCountsRemovalsOnlyInsideTheWindow(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name    string
		removed int
		confirm bool
	}{
		{name: "a few rows fold directly", removed: 3},
		{name: "most rows need confirmation", removed: 6, confirm: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			ctx := t.Context()
			service, handle, _ := openProviderRefreshProfile(t)
			start := time.Date(2026, time.August, 15, 12, 0, 0, 0, time.UTC)
			source := &fakeProviderSource{
				identity: monarchTestIdentity(),
				snapshot: monarchHistorySnapshot(t, start, 10, 20), fingerprint: "session-a",
			}
			configureProviderRefreshService(t, service, source, start, "instance-a")
			_, err := service.RefreshProvider(ctx, manualProviderRefresh())
			require.NoError(t, err)

			later := start.Add(7 * time.Hour)
			changed := monarchHistorySnapshot(t, later, 10, 20)
			changed.Transactions = changed.Transactions[test.removed:]
			source.setSnapshot(changed)
			configureProviderRefreshService(t, service, source, later, "instance-a")
			result, err := service.RefreshProvider(ctx, scheduledProviderRefresh())
			if test.confirm {
				assertProviderAppCode(t, err, provider.CodeDeletionConfirmationRequired)
				assert.Equal(t, test.removed, result.Status.Summary.RemovedTransactions)
				request := manualProviderRefresh()
				request.ConfirmationToken = result.Status.ConfirmationToken
				result, err = service.ConfirmProviderRefresh(ctx, request)
			}
			require.NoError(t, err)
			assert.Equal(t, test.removed, result.Status.Summary.RemovedTransactions)
			refreshed, err := handle.Load(ctx)
			require.NoError(t, err)
			assert.Len(t, refreshed.Committed.Transactions, 30-test.removed)
			committedProviderRow(t, refreshed.Committed, "transaction-old-19")
		})
	}
}

func TestMonarchScheduledFullRefreshFetchesCompleteHistory(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	service, _, _ := openProviderRefreshProfile(t)
	start := time.Date(2026, time.August, 15, 12, 0, 0, 0, time.UTC)
	source := &fakeProviderSource{
		identity: monarchTestIdentity(),
		snapshot: monarchHistorySnapshot(t, start, 2, 2), fingerprint: "session-a",
	}
	configureProviderRefreshService(t, service, source, start, "instance-a")
	_, err := service.RefreshProvider(ctx, scheduledProviderRefresh())
	require.NoError(t, err)
	assert.Zero(t, source.fetchCalls(), "an unimported profile does not download in the background")

	_, err = service.RefreshProvider(ctx, manualProviderRefresh())
	require.NoError(t, err)
	configureProviderRefreshService(t, service, source, start.Add(time.Hour), "instance-a")
	_, err = service.RefreshProvider(ctx, scheduledProviderRefresh())
	require.NoError(t, err)
	assert.Equal(t, 1, source.fetchCalls(), "fresh data is not fetched again")

	monthLater := start.Add(30 * 24 * time.Hour)
	source.setSnapshot(monarchHistorySnapshot(t, monthLater, 2, 2))
	configureProviderRefreshService(t, service, source, monthLater, "instance-a")
	result, err := service.RefreshProvider(ctx, scheduledProviderRefresh())
	require.NoError(t, err)
	assert.Equal(t, monthLater, result.Status.LastFullRefresh)

	requests := source.fetchRequests()
	require.Len(t, requests, 2)
	for _, request := range requests {
		assert.Nil(t, request.TransactionWindow)
	}
}

// Pressing refresh reads the last 90 days. Complete history stays one explicit request away,
// and a manual refresh still reads it when the last complete refresh is 30 or more days old.
func TestManualMonarchRefreshReadsRecentDataUnlessCompleteHistoryIsAskedOrDue(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	service, _, _ := openProviderRefreshProfile(t)
	start := time.Date(2026, time.August, 15, 12, 0, 0, 0, time.UTC)
	source := &fakeProviderSource{
		identity: monarchTestIdentity(),
		snapshot: monarchHistorySnapshot(t, start, 2, 2), fingerprint: "session-a",
	}
	refreshAt := func(at time.Time, request app.ProviderRefreshRequest) app.ProviderRefreshResult {
		t.Helper()
		source.setSnapshot(monarchHistorySnapshot(t, at, 2, 2))
		configureProviderRefreshService(t, service, source, at, "instance-a")
		result, err := service.RefreshProvider(ctx, request)
		require.NoError(t, err)
		return result
	}
	complete := manualProviderRefresh()
	complete.CompleteHistory = true

	refreshAt(start, manualProviderRefresh())
	status, err := service.ProviderStatus(ctx)
	require.NoError(t, err)
	assert.Equal(t, app.ProviderRefreshRecent, status.ManualRefresh)
	recent := refreshAt(start.Add(time.Hour), manualProviderRefresh())
	asked := refreshAt(start.Add(2*time.Hour), complete)
	monthLater := start.Add(2*time.Hour + 30*24*time.Hour)
	configureProviderRefreshService(t, service, source, monthLater, "instance-a")
	status, err = service.ProviderStatus(ctx)
	require.NoError(t, err)
	assert.Equal(t, app.ProviderRefreshFull, status.ManualRefresh)
	due := refreshAt(monthLater, manualProviderRefresh())

	requests := source.fetchRequests()
	require.Len(t, requests, 4)
	assert.Nil(t, requests[0].TransactionWindow, "the first manual refresh has no complete record")
	assert.Equal(t, &domain.DateRange{
		Start: mustProviderDate(t, "2026-05-17"), End: mustProviderDate(t, "2026-08-15"),
	}, requests[1].TransactionWindow, "pressing refresh reads the last 90 days")
	assert.Equal(t, start, recent.Status.LastFullRefresh, "a recent refresh is not complete")
	assert.Nil(t, requests[2].TransactionWindow, "complete history on request")
	assert.Equal(t, start.Add(2*time.Hour), asked.Status.LastFullRefresh)
	assert.Nil(t, requests[3].TransactionWindow, "complete history once 30 days old")
	assert.Equal(t, monthLater, due.Status.LastFullRefresh)
}

// A recent refresh cannot see that Monarch deleted an older transaction, so it keeps the row;
// the next complete-history refresh removes it, as it did before manual refresh read recent data.
func TestManualRecentRefreshLeavesOlderDeletionsToCompleteHistory(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	service, handle, _ := openProviderRefreshProfile(t)
	start := time.Date(2026, time.August, 15, 12, 0, 0, 0, time.UTC)
	source := &fakeProviderSource{
		identity: monarchTestIdentity(),
		snapshot: monarchHistorySnapshot(t, start, 10, 3), fingerprint: "session-a",
	}
	configureProviderRefreshService(t, service, source, start, "instance-a")
	_, err := service.RefreshProvider(ctx, manualProviderRefresh())
	require.NoError(t, err)

	later := start.Add(time.Hour)
	deleted := monarchHistorySnapshot(t, later, 10, 3)
	deleted.Transactions = deleted.Transactions[:10]
	source.setSnapshot(deleted)
	configureProviderRefreshService(t, service, source, later, "instance-a")
	recent, err := service.RefreshProvider(ctx, manualProviderRefresh())
	require.NoError(t, err)
	assert.Zero(t, recent.Status.Summary.RemovedTransactions)
	kept, err := handle.Load(ctx)
	require.NoError(t, err)
	assert.Len(t, kept.Committed.Transactions, 13)

	complete := manualProviderRefresh()
	complete.CompleteHistory = true
	full, err := service.RefreshProvider(ctx, complete)
	require.NoError(t, err)
	assert.Equal(t, 3, full.Status.Summary.RemovedTransactions)
	removed, err := handle.Load(ctx)
	require.NoError(t, err)
	assert.Len(t, removed.Committed.Transactions, 10)
}

func monarchTestIdentity() provider.ProfileIdentity {
	return provider.ProfileIdentity{Kind: "monarch", RemoteID: "subscription-example"}
}

func manualProviderRefresh() app.ProviderRefreshRequest {
	return app.ProviderRefreshRequest{
		Manual: true, State: app.DefaultViewState(), Selection: app.EmptySelection(),
	}
}

func scheduledProviderRefresh() app.ProviderRefreshRequest {
	return app.ProviderRefreshRequest{
		State: app.DefaultViewState(), Selection: app.EmptySelection(),
	}
}

// monarchHistorySnapshot returns recent rows dated 2026-08-15 and older rows dated 2026-01-10.
// The older rows use their own merchant, as Monarch reports for history it no longer lists.
func monarchHistorySnapshot(
	t *testing.T,
	observedAt time.Time,
	recent int,
	older int,
) domain.ImportSnapshot {
	t.Helper()
	snapshot := providerSnapshot(t, observedAt, recent)
	snapshot.Merchants = append(snapshot.Merchants, domain.ImportEntity{
		Kind: domain.EntityKindMerchant, ExternalID: "merchant-old", Label: "Older Merchant",
	})
	for index := range older {
		snapshot.Transactions = append(snapshot.Transactions, domain.ImportTransaction{
			ExternalID:         fmt.Sprintf("transaction-old-%02d", index),
			AccountExternalID:  "account-example",
			MerchantExternalID: "merchant-old", CategoryExternalID: "category-example",
			Date:   mustProviderDate(t, "2026-01-10"),
			Amount: domain.Money{Minor: int64(-500 - index), Currency: "USD", Scale: 2},
		})
	}
	return snapshot
}

func committedProviderRow(
	t *testing.T,
	committed domain.CommittedProfile,
	externalID string,
) domain.TransactionRecord {
	t.Helper()
	for _, transaction := range committed.Transactions {
		if transaction.ProviderID == externalID {
			return transaction
		}
	}
	require.FailNow(t, "committed provider row was not found", externalID)
	return domain.TransactionRecord{}
}

// Every refresh result says what the next manual refresh reads, so an interface can name it even
// right after a failed refresh.
func TestRefreshResultsSayWhatTheNextManualRefreshReads(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	service, _, _ := openProviderRefreshProfile(t)
	start := time.Date(2026, time.August, 15, 12, 0, 0, 0, time.UTC)
	source := &fakeProviderSource{
		identity: monarchTestIdentity(),
		snapshot: monarchHistorySnapshot(t, start, 2, 2), fingerprint: "session-a",
	}
	configureProviderRefreshService(t, service, source, start, "instance-a")
	succeeded, err := service.RefreshProvider(ctx, manualProviderRefresh())
	require.NoError(t, err)
	assert.Equal(t, app.ProviderRefreshRecent, succeeded.Status.ManualRefresh)

	source.mu.Lock()
	source.fetchErr = provider.NewError(provider.CodeUnavailable)
	source.mu.Unlock()
	configureProviderRefreshService(t, service, source, start.Add(time.Hour), "instance-a")
	failed, err := service.RefreshProvider(ctx, manualProviderRefresh())
	assertProviderAppCode(t, err, provider.CodeUnavailable)
	assert.Equal(t, app.ProviderRefreshRecent, failed.Status.ManualRefresh)
	assert.Equal(t, start, failed.Status.LastFullRefresh)
}
