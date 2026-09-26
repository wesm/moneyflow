package sqlite

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/wesm/moneyflow/internal/app"
	"github.com/wesm/moneyflow/internal/domain"
	"github.com/wesm/moneyflow/internal/home"
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
	candidate, proposedIDs := providerPerformanceCandidate(
		t, now, editingPerformanceRows, "monarch", false,
	)
	_, acquired, err := profile.AcquireRefreshLease(ctx, store.RefreshLease{
		OwnerID: "performance-owner", Renderer: "cli", ExpiresAt: now.Add(time.Minute),
	}, now)
	require.NoError(t, err)
	require.True(t, acquired)

	_, err = profile.ApplyProviderRefresh(ctx, store.AtomicRefreshRequest{
		ExpectedGeneration: 0, LeaseOwnerID: "performance-owner",
		Binding: &store.ProviderBinding{
			Kind: "monarch", Namespace: "monarch", RemoteProfileID: "subscription-example",
			Currency: "USD", Scale: 2, BoundAt: now,
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
	// This is a cross-platform regression guard, not the controlled one-second target.
	// Loaded CI hosts need enough headroom to avoid turning scheduler variance into failures.
	require.Less(t, duration, 10*time.Second)
}

func TestYNABProviderRefresh100KPerformance(t *testing.T) {
	skipEditingPerformance(t)

	ctx := context.Background()
	paths := temporaryPaths(t)
	profileStore, err := Open(ctx, paths, DefaultOptions)
	require.NoError(t, err)
	profileDB := profileStore.(*profile)
	now := time.Date(2026, time.August, 30, 18, 30, 0, 0, time.UTC)
	candidate, proposedIDs := providerPerformanceCandidate(
		t, now, editingPerformanceRows, "ynab", true,
	)
	require.NoError(t, candidate.Validate())
	applyProviderPerformanceRefresh(
		ctx, t, profileDB, "ynab-performance-initial", 0, now,
		&store.ProviderBinding{
			Kind: "ynab", Namespace: "ynab", RemoteProfileID: "plan-performance",
			Currency: "USD", Scale: 2, BoundAt: now,
		}, candidate, proposedIDs,
	)

	refreshAt := now.Add(time.Minute)
	candidate.ObservedAt = refreshAt
	candidate.Transactions[1].Notes = "Synthetic update"
	started := time.Now()
	commit := applyProviderPerformanceRefresh(
		ctx, t, profileDB, "ynab-performance-update", 1, refreshAt,
		nil, candidate, nil,
	)
	duration := time.Since(started)
	t.Logf("YNAB refresh 100k fold with splits: %s", duration)
	require.True(t, commit.SemanticChange)
	require.Equal(t, editingPerformanceRows, commit.Summary.ImportedTransactions)
	require.Less(t, duration, 4*time.Second)
	require.NoError(t, profileDB.Close())

	started = time.Now()
	reopenedStore, err := Open(ctx, paths, DefaultOptions)
	require.NoError(t, err)
	reopened := reopenedStore.(*profile)
	t.Cleanup(func() { require.NoError(t, reopened.Close()) })
	_, err = app.NewProfileService(ctx, reopened)
	require.NoError(t, err)
	reopenDuration := time.Since(started)
	t.Logf("YNAB cold reopen and effective snapshot build: %s", reopenDuration)
	require.Less(t, reopenDuration, time.Second)
}

func BenchmarkYNABRefreshFold100K(b *testing.B) {
	ctx := context.Background()
	paths, err := home.ResolveRoot(b.TempDir()+"/profile", nil, "")
	require.NoError(b, err)
	profileStore, err := Open(ctx, paths, DefaultOptions)
	require.NoError(b, err)
	profile := profileStore.(*profile)
	b.Cleanup(func() { require.NoError(b, profile.Close()) })
	now := time.Date(2026, time.August, 30, 18, 45, 0, 0, time.UTC)
	candidate, proposedIDs := providerPerformanceCandidate(
		b, now, editingPerformanceRows, "ynab", true,
	)
	require.NoError(b, candidate.Validate())
	applyProviderPerformanceRefresh(
		ctx, b, profile, "ynab-benchmark-initial", 0, now,
		&store.ProviderBinding{
			Kind: "ynab", Namespace: "ynab", RemoteProfileID: "plan-performance",
			Currency: "USD", Scale: 2, BoundAt: now,
		}, candidate, proposedIDs,
	)

	b.ReportAllocs()
	b.ResetTimer()
	for index := range b.N {
		b.StopTimer()
		observedAt := now.Add(time.Duration(index+1) * time.Minute)
		candidate.ObservedAt = observedAt
		candidate.Transactions[1].Notes = fmt.Sprintf("Synthetic update %d", index)
		owner := fmt.Sprintf("ynab-benchmark-%d", index)
		_, acquired, acquireErr := profile.AcquireRefreshLease(ctx, store.RefreshLease{
			OwnerID: owner, Renderer: "cli", ExpiresAt: observedAt.Add(time.Minute),
		}, observedAt)
		require.NoError(b, acquireErr)
		require.True(b, acquired)
		b.StartTimer()
		commit, applyErr := profile.ApplyProviderRefresh(ctx, store.AtomicRefreshRequest{
			ExpectedGeneration: uint64(index + 1), LeaseOwnerID: owner,
			Candidate: candidate, ObservedAt: observedAt,
		}, app.BuildProviderRefreshPlanReference)
		require.NoError(b, applyErr)
		require.True(b, commit.SemanticChange)
	}
}

func applyProviderPerformanceRefresh(
	ctx context.Context,
	t testing.TB,
	profile *profile,
	owner string,
	expectedGeneration uint64,
	observedAt time.Time,
	binding *store.ProviderBinding,
	candidate domain.ImportSnapshot,
	proposedIDs map[string]domain.EntityID,
) store.RefreshCommit {
	t.Helper()
	_, acquired, err := profile.AcquireRefreshLease(ctx, store.RefreshLease{
		OwnerID: owner, Renderer: "cli", ExpiresAt: observedAt.Add(time.Minute),
	}, observedAt)
	require.NoError(t, err)
	require.True(t, acquired)
	commit, err := profile.ApplyProviderRefresh(ctx, store.AtomicRefreshRequest{
		ExpectedGeneration: expectedGeneration, LeaseOwnerID: owner, Binding: binding,
		Candidate: candidate, ProposedIDs: proposedIDs, ObservedAt: observedAt,
	}, app.BuildProviderRefreshPlanReference)
	require.NoError(t, err)
	return commit
}

func providerPerformanceCandidate(
	t testing.TB,
	observedAt time.Time,
	count int,
	providerName string,
	withSplits bool,
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
		proposed[app.ProviderIdentityKey(providerName, entity.kind, entity.externalID)] = entity.localID
	}
	for index := range count {
		externalID := fmt.Sprintf("transaction-%06d", index)
		candidate.Transactions[index] = domain.ImportTransaction{
			ExternalID: externalID, AccountExternalID: "account-example",
			MerchantExternalID: "merchant-example", CategoryExternalID: "category-example",
			Date:   date,
			Amount: domain.Money{Minor: int64(-100 - index), Currency: "USD", Scale: 2},
		}
		if withSplits && index%10 == 0 {
			candidate.Transactions[index].Amount.Minor = -3000
			candidate.Transactions[index].CategoryExternalID = ""
			candidate.Transactions[index].SystemCategoryID = domain.SplitCategoryID
			candidate.Splits = append(candidate.Splits,
				domain.ImportTransactionSplit{
					ExternalID:                  fmt.Sprintf("split-%06d-a", index),
					ParentTransactionExternalID: externalID, Position: 0,
					SourceAmount: -10000, SourceScale: 3,
					Amount: domain.Money{Minor: -1000, Currency: "USD", Scale: 2},
				},
				domain.ImportTransactionSplit{
					ExternalID:                  fmt.Sprintf("split-%06d-b", index),
					ParentTransactionExternalID: externalID, Position: 1,
					SourceAmount: -20000, SourceScale: 3,
					Amount:             domain.Money{Minor: -2000, Currency: "USD", Scale: 2},
					CategoryExternalID: "category-example",
				},
			)
		}
		proposed[app.ProviderIdentityKey(providerName, domain.EntityKindTransaction, externalID)] =
			domain.EntityID(fmt.Sprintf("transaction_local_%06d", index))
	}
	return candidate, proposed
}
