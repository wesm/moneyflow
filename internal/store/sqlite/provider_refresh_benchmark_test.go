package sqlite

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/wesm/moneyflow/internal/app"
	"github.com/wesm/moneyflow/internal/domain"
	"github.com/wesm/moneyflow/internal/store"
)

func TestProviderRefresh100KPerformance(t *testing.T) {
	skipEditingPerformance(t)

	ctx := context.Background()
	profileStore, err := Open(ctx, temporaryPaths(t), DefaultOptions)
	require.NoError(t, err)
	profile := profileStore.(*profile)
	t.Cleanup(func() { require.NoError(t, profile.Close()) })
	now := time.Date(2026, time.August, 15, 23, 55, 0, 0, time.UTC)
	candidate, proposedIDs := providerPerformanceCandidate(t, now, editingPerformanceRows)
	_, acquired, err := profile.AcquireRefreshLease(ctx, store.RefreshLease{
		OwnerID: "performance-owner", Renderer: "cli", ExpiresAt: now.Add(time.Minute),
	}, now)
	require.NoError(t, err)
	require.True(t, acquired)

	_, err = profile.ApplyProviderRefresh(ctx, store.AtomicRefreshRequest{
		ExpectedGeneration: 0, LeaseOwnerID: "performance-owner",
		Binding: &store.ProviderBinding{
			Kind: "monarch", Namespace: "monarch", RemoteProfileID: "subscription-example",
			BoundAt: now,
		},
		Candidate: candidate, ProposedIDs: proposedIDs, ObservedAt: now,
	}, app.BuildProviderRefreshPlanReference)
	require.NoError(t, err)

	refreshAt := now.Add(time.Minute)
	candidate.ObservedAt = refreshAt
	_, acquired, err = profile.AcquireRefreshLease(ctx, store.RefreshLease{
		OwnerID: "performance-owner", Renderer: "cli", ExpiresAt: refreshAt.Add(time.Minute),
	}, refreshAt)
	require.NoError(t, err)
	require.True(t, acquired)
	started := time.Now()
	commit, err := profile.ApplyProviderRefresh(ctx, store.AtomicRefreshRequest{
		ExpectedGeneration: 1, LeaseOwnerID: "performance-owner",
		Candidate: candidate, ObservedAt: refreshAt,
	}, app.BuildProviderRefreshPlanReference)
	require.NoError(t, err)
	duration := time.Since(started)
	t.Logf("provider refresh 100k write-locked reference path: %s", duration)
	require.Equal(t, editingPerformanceRows, commit.Summary.ImportedTransactions)
	require.Less(t, duration, time.Second)
}

func providerPerformanceCandidate(
	t testing.TB,
	observedAt time.Time,
	count int,
) (domain.ImportSnapshot, map[string]domain.EntityID) {
	t.Helper()
	date, err := domain.ParseDate("2026-08-15")
	require.NoError(t, err)
	candidate := domain.ImportSnapshot{
		ObservedAt: observedAt,
		Accounts: []domain.ImportEntity{{
			Kind: domain.EntityKindAccount, ExternalID: "account-example", Label: "Account Name",
		}},
		Merchants: []domain.ImportEntity{{
			Kind: domain.EntityKindMerchant, ExternalID: "merchant-example", Label: "Example Merchant",
		}},
		Groups: []domain.ImportEntity{{
			Kind: domain.EntityKindGroup, ExternalID: "group-example", Label: "Example Group",
		}},
		Categories: []domain.ImportEntity{{
			Kind: domain.EntityKindCategory, ExternalID: "category-example",
			ParentExternalID: "group-example", Label: "Example Category",
		}},
		Transactions: make([]domain.ImportTransaction, count),
	}
	proposed := make(map[string]domain.EntityID, count+4)
	for _, entity := range []struct {
		kind       domain.EntityKind
		externalID string
		localID    domain.EntityID
	}{
		{domain.EntityKindAccount, "account-example", "account_local"},
		{domain.EntityKindMerchant, "merchant-example", "merchant_local"},
		{domain.EntityKindGroup, "group-example", "group_local"},
		{domain.EntityKindCategory, "category-example", "category_local"},
	} {
		proposed[app.ProviderIdentityKey("monarch", entity.kind, entity.externalID)] = entity.localID
	}
	for index := range count {
		externalID := fmt.Sprintf("transaction-%06d", index)
		candidate.Transactions[index] = domain.ImportTransaction{
			ExternalID: externalID, AccountExternalID: "account-example",
			MerchantExternalID: "merchant-example", CategoryExternalID: "category-example",
			Date:   date,
			Amount: domain.Money{Minor: int64(-100 - index), Currency: "USD", Scale: 2},
		}
		proposed[app.ProviderIdentityKey("monarch", domain.EntityKindTransaction, externalID)] =
			domain.EntityID(fmt.Sprintf("transaction_local_%06d", index))
	}
	return candidate, proposed
}
