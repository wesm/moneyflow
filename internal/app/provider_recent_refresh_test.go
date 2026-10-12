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

func TestMonarchScheduledFullAndManualRefreshFetchCompleteHistory(t *testing.T) {
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

	recentDue := monthLater.Add(7 * time.Hour)
	configureProviderRefreshService(t, service, source, recentDue, "instance-a")
	_, err = service.RefreshProvider(ctx, manualProviderRefresh())
	require.NoError(t, err)
	requests := source.fetchRequests()
	require.Len(t, requests, 3)
	for _, request := range requests {
		assert.Nil(t, request.TransactionWindow)
	}
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
