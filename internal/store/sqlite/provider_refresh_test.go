package sqlite

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/wesm/moneyflow/internal/domain"
	profilereplay "github.com/wesm/moneyflow/internal/replay"
	"github.com/wesm/moneyflow/internal/store"
)

func TestProviderRefreshFoldsCommittedAndEffectiveStateAtomically(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	profile := openSeededProfile(t, DefaultOptions)
	now := time.Date(2026, time.August, 15, 18, 0, 0, 0, time.UTC)
	bindProviderForRefreshTest(t, profile, now)
	acquireProviderRefreshLease(t, profile, "refresh-owner", now)

	before, err := profile.Load(ctx)
	require.NoError(t, err)
	candidate := providerRefreshCandidate(t, now)
	plannerCalls := 0
	commit, err := profile.ApplyProviderRefresh(ctx, store.AtomicRefreshRequest{
		ExpectedGeneration: 0,
		LeaseOwnerID:       "refresh-owner",
		Candidate:          candidate,
		ProposedIDs:        map[string]domain.EntityID{"candidate": "proposed"},
		ProposedSuffixes:   map[string]string{"candidate": "a1b2"},
		ObservedAt:         now,
	}, func(inputs store.RefreshInputs) (store.RefreshPlan, error) {
		plannerCalls++
		assert.Equal(t, before, inputs.Snapshot)
		require.NotNil(t, inputs.Binding)
		assert.Equal(t, candidate, inputs.Candidate)
		assert.Equal(t, domain.EntityID("proposed"), inputs.ProposedIDs["candidate"])
		committed := inputs.Snapshot.Committed.Clone()
		committed.Transactions[0].Notes = "Refreshed note"
		effective, replayErr := profilereplay.Replay(domain.ProfileSnapshot{
			Committed: committed, Journal: inputs.Snapshot.Journal,
			Cursor: inputs.Snapshot.Cursor, KnownDrills: inputs.Snapshot.KnownDrills,
		})
		if replayErr != nil {
			return store.RefreshPlan{}, replayErr
		}
		return store.RefreshPlan{
			Committed: committed, Effective: effective.Effective,
			Journal: inputs.Snapshot.Journal, Cursor: inputs.Snapshot.Cursor,
			KnownDrills: inputs.Snapshot.KnownDrills,
			Summary:     refreshCandidateSummary(candidate),
		}, nil
	})
	require.NoError(t, err)
	assert.Equal(t, 1, plannerCalls)
	assert.Equal(t, before.Revision+1, commit.Revision)
	assert.Equal(t, uint64(1), commit.Generation)

	after, err := profile.Load(ctx)
	require.NoError(t, err)
	assert.Equal(t, "Refreshed note", after.Committed.Transactions[0].Notes)
	state, err := profile.ProviderState(ctx)
	require.NoError(t, err)
	assert.Equal(t, commit.Generation, state.Refresh.Generation)
	assert.Equal(t, now, state.Refresh.LastAttempt)
	assert.Equal(t, now, state.Refresh.LastSuccess)
	assert.Equal(t, len(candidate.Transactions), state.Refresh.ImportedTransactions)
	assert.Nil(t, state.Lease)
}

func TestProviderRefreshConcurrentGenerationCASAllowsExactlyOneFold(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	paths := temporaryPaths(t)
	firstStore, err := Open(ctx, paths, DefaultOptions)
	require.NoError(t, err)
	first := firstStore.(*profile)
	t.Cleanup(func() { require.NoError(t, first.Close()) })
	_, err = first.CreateSeededProfile(ctx, fixtureProfile(t))
	require.NoError(t, err)
	secondStore, err := Open(ctx, paths, DefaultOptions)
	require.NoError(t, err)
	second := secondStore.(*profile)
	t.Cleanup(func() { require.NoError(t, second.Close()) })
	now := time.Date(2026, time.August, 15, 18, 30, 0, 0, time.UTC)
	bindProviderForRefreshTest(t, first, now)
	acquireProviderRefreshLease(t, first, "shared-owner", now)
	request := store.AtomicRefreshRequest{
		ExpectedGeneration: 0, LeaseOwnerID: "shared-owner",
		Candidate: providerRefreshCandidate(t, now), ObservedAt: now,
	}

	start := make(chan struct{})
	results := make(chan error, 2)
	var waitGroup sync.WaitGroup
	for _, handle := range []*profile{first, second} {
		waitGroup.Add(1)
		go func(handle *profile) {
			defer waitGroup.Done()
			<-start
			_, applyErr := handle.ApplyProviderRefresh(ctx, request, passthroughRefreshPlanner)
			results <- applyErr
		}(handle)
	}
	close(start)
	waitGroup.Wait()
	close(results)

	var successes, conflicts int
	for applyErr := range results {
		if applyErr == nil {
			successes++
			continue
		}
		var failure *store.Error
		require.ErrorAs(t, applyErr, &failure)
		if failure.Code == store.CodeRevisionConflict {
			conflicts++
		}
	}
	assert.Equal(t, 1, successes)
	assert.Equal(t, 1, conflicts)
	state, err := first.ProviderState(ctx)
	require.NoError(t, err)
	assert.Equal(t, uint64(1), state.Refresh.Generation)
}

func TestProviderRefreshPlansAgainstLatestJournalInsideTransaction(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	profile := openSeededProfile(t, DefaultOptions)
	now := time.Date(2026, time.August, 15, 19, 0, 0, 0, time.UTC)
	bindProviderForRefreshTest(t, profile, now)
	acquireProviderRefreshLease(t, profile, "latest-journal-owner", now)
	beforeFetch, err := profile.Load(ctx)
	require.NoError(t, err)

	// The provider fetch began at generation zero, then a renderer staged an edit before folding.
	revision, err := profile.Append(ctx, 1, draftHideOperation(
		"operation_during_fetch", 1, beforeFetch.Committed.Transactions[0].ID,
	))
	require.NoError(t, err)
	seenRevision := uint64(0)
	commit, err := profile.ApplyProviderRefresh(ctx, store.AtomicRefreshRequest{
		ExpectedGeneration: 0, LeaseOwnerID: "latest-journal-owner",
		Candidate: providerRefreshCandidate(t, now), ObservedAt: now,
	}, func(inputs store.RefreshInputs) (store.RefreshPlan, error) {
		seenRevision = inputs.Snapshot.Revision
		return passthroughRefreshPlanner(inputs)
	})
	require.NoError(t, err)
	assert.Equal(t, revision, seenRevision)
	assert.Equal(t, revision+1, commit.Revision)
	loaded, err := profile.Load(ctx)
	require.NoError(t, err)
	require.Len(t, loaded.Journal, 1)
	assert.Equal(t, "operation_during_fetch", loaded.Journal[0].ID)
	assert.Equal(t, 1, loaded.Cursor)
}

func TestProviderRefreshInitialBindingIsPartOfAtomicFold(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	profileStore, err := Open(ctx, temporaryPaths(t), DefaultOptions)
	require.NoError(t, err)
	profile := profileStore.(*profile)
	t.Cleanup(func() { require.NoError(t, profile.Close()) })
	now := time.Date(2026, time.August, 15, 19, 30, 0, 0, time.UTC)
	acquireProviderRefreshLease(t, profile, "initial-owner", now)
	committed := fixtureProfile(t)
	knownDrills, err := seededKnownDrills(committed)
	require.NoError(t, err)
	binding := store.ProviderBinding{
		Kind: "monarch", Namespace: "monarch", RemoteProfileID: "subscription-example",
		BoundAt: now,
	}

	_, err = profile.ApplyProviderRefresh(ctx, store.AtomicRefreshRequest{
		ExpectedGeneration: 0, LeaseOwnerID: "initial-owner", Binding: &binding,
		Candidate: providerRefreshCandidate(t, now), ObservedAt: now,
	}, func(inputs store.RefreshInputs) (store.RefreshPlan, error) {
		require.NotNil(t, inputs.Binding)
		assert.Equal(t, binding, *inputs.Binding)
		return store.RefreshPlan{
			Committed: committed, Effective: committed.Clone(), KnownDrills: knownDrills,
			Summary: refreshCandidateSummary(inputs.Candidate),
		}, nil
	})
	require.NoError(t, err)
	state, err := profile.ProviderState(ctx)
	require.NoError(t, err)
	require.NotNil(t, state.Binding)
	assert.Equal(t, binding, *state.Binding)
	loaded, err := profile.Load(ctx)
	require.NoError(t, err)
	assert.Equal(t, committed, loaded.Committed)
}

func TestProviderRefreshAtJournalCeilingMayShrinkJournal(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	profile := openSeededProfile(t, DefaultOptions)
	now := time.Date(2026, time.August, 15, 20, 0, 0, 0, time.UTC)
	bindProviderForRefreshTest(t, profile, now)
	acquireProviderRefreshLease(t, profile, "ceiling-owner", now)
	insertJournalAtOperationCeiling(t, profile)

	commit, err := profile.ApplyProviderRefresh(ctx, store.AtomicRefreshRequest{
		ExpectedGeneration: 0, LeaseOwnerID: "ceiling-owner",
		Candidate: providerRefreshCandidate(t, now), ObservedAt: now,
	}, func(inputs store.RefreshInputs) (store.RefreshPlan, error) {
		assert.Len(t, inputs.Snapshot.Journal, maxJournalOperations)
		return store.RefreshPlan{
			Committed:   inputs.Snapshot.Committed,
			Effective:   inputs.Snapshot.Committed.Clone(),
			KnownDrills: inputs.Snapshot.KnownDrills,
			Summary: store.RefreshSummary{
				ImportedAccounts:     len(inputs.Candidate.Accounts),
				ImportedMerchants:    len(inputs.Candidate.Merchants),
				ImportedGroups:       len(inputs.Candidate.Groups),
				ImportedCategories:   len(inputs.Candidate.Categories),
				ImportedTransactions: len(inputs.Candidate.Transactions),
				RemovedOperations:    maxJournalOperations,
				RemovedTargets:       maxJournalOperations,
			},
		}, nil
	})
	require.NoError(t, err)
	assert.Equal(t, uint64(1), commit.Generation)
	loaded, err := profile.Load(ctx)
	require.NoError(t, err)
	assert.Empty(t, loaded.Journal)
	assert.Zero(t, loaded.Cursor)
}

func TestProviderRefreshCanonicalLogicalStateMatchesReopen(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	paths := temporaryPaths(t)
	profileStore, err := Open(ctx, paths, DefaultOptions)
	require.NoError(t, err)
	handle := profileStore.(*profile)
	_, err = handle.CreateSeededProfile(ctx, fixtureProfile(t))
	require.NoError(t, err)
	now := time.Date(2026, time.August, 15, 20, 30, 0, 0, time.UTC)
	bindProviderForRefreshTest(t, handle, now)
	acquireProviderRefreshLease(t, handle, "canonical-owner", now)
	allocation := store.LabelAllocation{
		Kind: domain.EntityKindMerchant, Namespace: "monarch/merchant",
		ExternalID: "merchant-example", BaseCollisionKey: "example merchant",
		DisplayLabel: "Example Merchant", Unsuffixed: true,
	}
	var planned store.RefreshPlan
	commit, err := handle.ApplyProviderRefresh(ctx, store.AtomicRefreshRequest{
		ExpectedGeneration: 0, LeaseOwnerID: "canonical-owner",
		Candidate: providerRefreshCandidate(t, now), ObservedAt: now,
	}, func(inputs store.RefreshInputs) (store.RefreshPlan, error) {
		planned, err = passthroughRefreshPlanner(inputs)
		planned.Allocations = []store.LabelAllocation{allocation}
		planned.Summary = refreshCandidateSummary(inputs.Candidate)
		return planned, err
	})
	require.NoError(t, err)
	require.NoError(t, handle.Close())

	reopenedStore, err := Open(ctx, paths, DefaultOptions)
	require.NoError(t, err)
	reopened := reopenedStore.(*profile)
	t.Cleanup(func() { require.NoError(t, reopened.Close()) })
	loaded, err := reopened.Load(ctx)
	require.NoError(t, err)
	providerState, err := reopened.ProviderState(ctx)
	require.NoError(t, err)
	replayed, err := profilereplay.Replay(loaded)
	require.NoError(t, err)
	reopenedPlan := store.RefreshPlan{
		Committed: loaded.Committed, Effective: replayed.Effective,
		Journal: loaded.Journal, Cursor: loaded.Cursor, KnownDrills: loaded.KnownDrills,
		Allocations: providerState.Allocations,
		Summary: store.RefreshSummary{
			ImportedTransactions: providerState.Refresh.ImportedTransactions,
			RemovedTransactions:  providerState.Refresh.RemovedTransactions,
		},
	}
	want, err := CanonicalRefreshPlan(planned)
	require.NoError(t, err)
	got, err := CanonicalRefreshPlan(reopenedPlan)
	require.NoError(t, err)
	assert.Equal(t, string(want), string(got))
	assert.Equal(t, commit.Revision, loaded.Revision)
}

func passthroughRefreshPlanner(inputs store.RefreshInputs) (store.RefreshPlan, error) {
	replayed, err := profilereplay.Replay(inputs.Snapshot)
	if err != nil {
		return store.RefreshPlan{}, err
	}
	return store.RefreshPlan{
		Committed:   inputs.Snapshot.Committed,
		Effective:   replayed.Effective,
		Journal:     inputs.Snapshot.Journal,
		Cursor:      inputs.Snapshot.Cursor,
		KnownDrills: inputs.Snapshot.KnownDrills,
		Allocations: inputs.Allocations,
		Summary:     refreshCandidateSummary(inputs.Candidate),
	}, nil
}

func refreshCandidateSummary(candidate domain.ImportSnapshot) store.RefreshSummary {
	return store.RefreshSummary{
		ImportedAccounts: len(candidate.Accounts), ImportedMerchants: len(candidate.Merchants),
		ImportedGroups: len(candidate.Groups), ImportedCategories: len(candidate.Categories),
		ImportedTransactions: len(candidate.Transactions),
	}
}

func insertJournalAtOperationCeiling(t *testing.T, profile *profile) {
	t.Helper()
	_, err := profile.database.ExecContext(context.Background(), `
		WITH RECURSIVE numbers(value) AS (
			SELECT 1 UNION ALL SELECT value + 1 FROM numbers WHERE value < ?
		)
		INSERT INTO journal_operations(
			id, sequence, operation_type, payload_version, creation_revision, created_at_unix_ms
		)
		SELECT printf('operation_refresh_limit_%05d', value), value,
			'transaction.hide-toggle', 1, 1, 1786712400000 FROM numbers`, maxJournalOperations)
	require.NoError(t, err)
	_, err = profile.database.ExecContext(context.Background(), `
		INSERT INTO operation_payloads(operation_id, payload_version, payload_json)
		SELECT id, 1, '{}' FROM journal_operations WHERE id LIKE 'operation_refresh_limit_%'`)
	require.NoError(t, err)
	_, err = profile.database.ExecContext(context.Background(), `
		INSERT INTO operation_targets(operation_id, ordinal, entity_id)
		SELECT id, 0, 'transaction_a' FROM journal_operations
		WHERE id LIKE 'operation_refresh_limit_%'`)
	require.NoError(t, err)
	_, err = profile.database.ExecContext(context.Background(),
		"UPDATE profile_state SET journal_cursor = ? WHERE singleton = 1", maxJournalOperations)
	require.NoError(t, err)
}

func TestProviderRefreshRejectsPlannerFailureWithoutChanges(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	profile := openSeededProfile(t, DefaultOptions)
	now := time.Date(2026, time.August, 15, 21, 0, 0, 0, time.UTC)
	bindProviderForRefreshTest(t, profile, now)
	acquireProviderRefreshLease(t, profile, "failed-planner-owner", now)
	before, err := profile.Load(ctx)
	require.NoError(t, err)
	stateBefore, err := profile.ProviderState(ctx)
	require.NoError(t, err)

	_, err = profile.ApplyProviderRefresh(ctx, store.AtomicRefreshRequest{
		ExpectedGeneration: 0, LeaseOwnerID: "failed-planner-owner",
		Candidate: providerRefreshCandidate(t, now), ObservedAt: now,
	}, func(store.RefreshInputs) (store.RefreshPlan, error) {
		return store.RefreshPlan{}, errors.New("synthetic planning failure")
	})
	assertStoreCode(t, err, store.CodeInvalidOperation)
	after, loadErr := profile.Load(ctx)
	require.NoError(t, loadErr)
	stateAfter, stateErr := profile.ProviderState(ctx)
	require.NoError(t, stateErr)
	assert.Equal(t, before, after)
	assert.Equal(t, stateBefore, stateAfter)
}

func bindProviderForRefreshTest(t *testing.T, profile *profile, boundAt time.Time) {
	t.Helper()
	_, err := profile.database.ExecContext(context.Background(), `
		INSERT INTO provider_binding(
			singleton, kind, namespace, remote_profile_id, bound_at_unix_ms
		) VALUES (1, 'monarch', 'monarch', 'subscription-example', ?)`, boundAt.UnixMilli())
	require.NoError(t, err)
}

func acquireProviderRefreshLease(
	t *testing.T,
	profile *profile,
	owner string,
	now time.Time,
) {
	t.Helper()
	_, acquired, err := profile.AcquireRefreshLease(context.Background(), store.RefreshLease{
		OwnerID: owner, Renderer: "tui", ExpiresAt: now.Add(time.Minute),
	}, now)
	require.NoError(t, err)
	require.True(t, acquired)
}

func providerRefreshCandidate(t *testing.T, observedAt time.Time) domain.ImportSnapshot {
	t.Helper()
	date, err := domain.NewDate(2026, time.August, 15)
	require.NoError(t, err)
	return domain.ImportSnapshot{
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
		Transactions: []domain.ImportTransaction{{
			ExternalID: "transaction-example", AccountExternalID: "account-example",
			MerchantExternalID: "merchant-example", CategoryExternalID: "category-example",
			Date: date, Amount: domain.Money{Minor: -1234, Currency: "USD", Scale: 2},
		}},
	}
}
