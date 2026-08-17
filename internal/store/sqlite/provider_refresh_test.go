package sqlite

import (
	"context"
	"errors"
	"slices"
	"strings"
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
	candidate.Transactions[0].Notes = "Refreshed note"
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
		return passthroughRefreshPlanner(inputs)
	})
	require.NoError(t, err)
	assert.Equal(t, 1, plannerCalls)
	assert.Equal(t, before.Revision+1, commit.Revision)
	assert.Equal(t, uint64(1), commit.Generation)

	after, err := profile.Load(ctx)
	require.NoError(t, err)
	assert.Equal(t, "Refreshed note", providerTransactionByExternalID(
		t, after.Committed, "transaction-example",
	).Notes)
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
	binding := store.ProviderBinding{
		Kind: "monarch", Namespace: "monarch", RemoteProfileID: "subscription-example",
		Currency: "USD", Scale: 2, BoundAt: now,
	}

	_, err = profile.ApplyProviderRefresh(ctx, store.AtomicRefreshRequest{
		ExpectedGeneration: 0, LeaseOwnerID: "initial-owner", Binding: &binding,
		Candidate: providerRefreshCandidate(t, now), ObservedAt: now,
	}, func(inputs store.RefreshInputs) (store.RefreshPlan, error) {
		require.NotNil(t, inputs.Binding)
		assert.Equal(t, binding, *inputs.Binding)
		return passthroughRefreshPlanner(inputs)
	})
	require.NoError(t, err)
	state, err := profile.ProviderState(ctx)
	require.NoError(t, err)
	require.NotNil(t, state.Binding)
	assert.Equal(t, binding, *state.Binding)
	loaded, err := profile.Load(ctx)
	require.NoError(t, err)
	assert.Len(t, loaded.Committed.Transactions, 1)
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
		committed, allocations, planErr := materializeRefreshCandidate(inputs)
		if planErr != nil {
			return store.RefreshPlan{}, planErr
		}
		rebased, planErr := profilereplay.RebaseProviderJournal(
			inputs.Snapshot.Committed,
			committed,
			inputs.Snapshot.Journal,
			inputs.Snapshot.Cursor,
		)
		if planErr != nil {
			return store.RefreshPlan{}, planErr
		}
		plan := store.RefreshPlan{
			Committed: committed, Effective: committed.Clone(),
			Journal: rebased.Journal, Cursor: rebased.Cursor,
			KnownDrills: inputs.Snapshot.KnownDrills, Allocations: allocations,
			Summary: refreshCandidateSummary(inputs.Candidate),
		}
		plan.Summary.RemovedOperations = rebased.Summary.RemovedOperations
		plan.Summary.RemovedTargets = rebased.Summary.RemovedTargets
		plan.Summary.RetainedOperations = rebased.Summary.RetainedOperations
		return plan, planErr
	})
	require.NoErrorf(t, err, "cause: %v", errors.Unwrap(err))
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
	var planned store.RefreshPlan
	commit, err := handle.ApplyProviderRefresh(ctx, store.AtomicRefreshRequest{
		ExpectedGeneration: 0, LeaseOwnerID: "canonical-owner",
		Candidate: providerRefreshCandidate(t, now), ObservedAt: now,
	}, func(inputs store.RefreshInputs) (store.RefreshPlan, error) {
		planned, err = passthroughRefreshPlanner(inputs)
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
	committed, allocations, err := materializeRefreshCandidate(inputs)
	if err != nil {
		return store.RefreshPlan{}, err
	}
	rebased, err := profilereplay.RebaseProviderJournal(
		inputs.Snapshot.Committed,
		committed,
		inputs.Snapshot.Journal,
		inputs.Snapshot.Cursor,
	)
	if err != nil {
		return store.RefreshPlan{}, err
	}
	replayed, err := profilereplay.Replay(domain.ProfileSnapshot{
		Revision: inputs.Snapshot.Revision, Committed: committed,
		Journal: rebased.Journal, Cursor: rebased.Cursor,
		KnownDrills: inputs.Snapshot.KnownDrills,
	})
	if err != nil {
		return store.RefreshPlan{}, err
	}
	return store.RefreshPlan{
		Committed:   committed,
		Effective:   replayed.Effective,
		Journal:     rebased.Journal,
		Cursor:      rebased.Cursor,
		KnownDrills: inputs.Snapshot.KnownDrills,
		Allocations: allocations,
		Summary:     refreshCandidateSummary(inputs.Candidate),
	}, nil
}

func materializeRefreshCandidate(
	inputs store.RefreshInputs,
) (domain.CommittedProfile, []store.LabelAllocation, error) {
	committed := inputs.Snapshot.Committed.Clone()
	providerName := inputs.Binding.Kind
	identities := make(map[string]domain.ExternalIdentity, len(committed.ExternalIdentities))
	for _, identity := range committed.ExternalIdentities {
		identities[identity.Namespace+"\x00"+identity.ExternalID] = identity
	}
	resolve := func(kind domain.EntityKind, externalID string) domain.EntityID {
		key := providerName + "/" + string(kind) + "\x00" + externalID
		if identity, exists := identities[key]; exists {
			return identity.EntityID
		}
		id := domain.EntityID("refresh_" + string(kind) + "_" + externalID)
		identity := domain.ExternalIdentity{
			EntityType: kind, EntityID: id, Namespace: providerName + "/" + string(kind),
			ExternalID: externalID,
		}
		identities[key] = identity
		committed.ExternalIdentities = append(committed.ExternalIdentities, identity)
		return id
	}
	allocations := append([]store.LabelAllocation(nil), inputs.Allocations...)
	allocationKeys := make(map[string]struct{}, len(allocations))
	for _, allocation := range allocations {
		allocationKeys[allocation.Namespace+"\x00"+allocation.ExternalID] = struct{}{}
	}
	label := func(kind domain.EntityKind, externalID string) (string, string, error) {
		display := "Imported " + string(kind) + " " + externalID
		key, collisionErr := domain.CollisionKey(display)
		if collisionErr != nil {
			return "", "", collisionErr
		}
		allocationKey := providerName + "/" + string(kind) + "\x00" + externalID
		if _, exists := allocationKeys[allocationKey]; !exists {
			allocations = append(allocations, store.LabelAllocation{
				Kind: kind, Namespace: providerName + "/" + string(kind), ExternalID: externalID,
				BaseCollisionKey: key, DisplayLabel: display, Unsuffixed: true,
			})
			allocationKeys[allocationKey] = struct{}{}
		}
		return display, key, nil
	}
	for _, imported := range inputs.Candidate.Accounts {
		id := resolve(domain.EntityKindAccount, imported.ExternalID)
		display, key, labelErr := label(domain.EntityKindAccount, imported.ExternalID)
		if labelErr != nil {
			return domain.CommittedProfile{}, nil, labelErr
		}
		committed.Accounts = upsertRefreshAccount(committed.Accounts, domain.Account{
			ID: id, Label: display, CollisionKey: key,
		})
	}
	for _, imported := range inputs.Candidate.Merchants {
		id := resolve(domain.EntityKindMerchant, imported.ExternalID)
		display, key, labelErr := label(domain.EntityKindMerchant, imported.ExternalID)
		if labelErr != nil {
			return domain.CommittedProfile{}, nil, labelErr
		}
		committed.Merchants = upsertRefreshMerchant(committed.Merchants, domain.Merchant{
			ID: id, Label: display, CollisionKey: key,
		})
	}
	for _, imported := range inputs.Candidate.Groups {
		id := resolve(domain.EntityKindGroup, imported.ExternalID)
		display, key, labelErr := label(domain.EntityKindGroup, imported.ExternalID)
		if labelErr != nil {
			return domain.CommittedProfile{}, nil, labelErr
		}
		committed.Groups = upsertRefreshGroup(committed.Groups, domain.CategoryGroup{
			ID: id, Label: display, CollisionKey: key,
		})
	}
	for _, imported := range inputs.Candidate.Categories {
		id := resolve(domain.EntityKindCategory, imported.ExternalID)
		display, key, labelErr := label(domain.EntityKindCategory, imported.ExternalID)
		if labelErr != nil {
			return domain.CommittedProfile{}, nil, labelErr
		}
		committed.Categories = upsertRefreshCategory(committed.Categories, domain.Category{
			ID: id, GroupID: resolve(domain.EntityKindGroup, imported.ParentExternalID),
			Label: display, CollisionKey: key,
		})
	}
	for _, imported := range inputs.Candidate.Transactions {
		id := resolve(domain.EntityKindTransaction, imported.ExternalID)
		committed.Transactions = upsertRefreshTransaction(
			committed.Transactions,
			domain.TransactionRecord{
				ID: id, Provider: providerName, ProviderID: imported.ExternalID,
				AccountID:  resolve(domain.EntityKindAccount, imported.AccountExternalID),
				MerchantID: resolve(domain.EntityKindMerchant, imported.MerchantExternalID),
				CategoryID: resolve(domain.EntityKindCategory, imported.CategoryExternalID),
				Date:       imported.Date, Amount: imported.Amount, Notes: imported.Notes,
				Hidden: imported.Hidden, Pending: imported.Pending,
			},
		)
	}
	importedTransactions := make(map[string]struct{}, len(inputs.Candidate.Transactions))
	for _, imported := range inputs.Candidate.Transactions {
		importedTransactions[imported.ExternalID] = struct{}{}
	}
	committed.Transactions = slices.DeleteFunc(
		committed.Transactions,
		func(transaction domain.TransactionRecord) bool {
			_, retained := importedTransactions[transaction.ProviderID]
			return transaction.Provider == providerName && !retained
		},
	)
	slices.SortFunc(committed.Accounts, func(left, right domain.Account) int {
		return strings.Compare(string(left.ID), string(right.ID))
	})
	slices.SortFunc(committed.Merchants, func(left, right domain.Merchant) int {
		return strings.Compare(string(left.ID), string(right.ID))
	})
	slices.SortFunc(committed.Groups, func(left, right domain.CategoryGroup) int {
		return strings.Compare(string(left.ID), string(right.ID))
	})
	slices.SortFunc(committed.Categories, func(left, right domain.Category) int {
		return strings.Compare(string(left.ID), string(right.ID))
	})
	slices.SortFunc(committed.Transactions, func(left, right domain.TransactionRecord) int {
		return strings.Compare(string(left.ID), string(right.ID))
	})
	slices.SortFunc(committed.ExternalIdentities, compareRefreshIdentity)
	slices.SortFunc(allocations, compareRefreshAllocation)
	return committed, allocations, nil
}

func upsertRefreshAccount(values []domain.Account, next domain.Account) []domain.Account {
	for index := range values {
		if values[index].ID == next.ID {
			values[index] = next
			return values
		}
	}
	return append(values, next)
}

func upsertRefreshMerchant(values []domain.Merchant, next domain.Merchant) []domain.Merchant {
	for index := range values {
		if values[index].ID == next.ID {
			values[index] = next
			return values
		}
	}
	return append(values, next)
}

func upsertRefreshGroup(
	values []domain.CategoryGroup,
	next domain.CategoryGroup,
) []domain.CategoryGroup {
	for index := range values {
		if values[index].ID == next.ID {
			values[index] = next
			return values
		}
	}
	return append(values, next)
}

func upsertRefreshCategory(values []domain.Category, next domain.Category) []domain.Category {
	for index := range values {
		if values[index].ID == next.ID {
			values[index] = next
			return values
		}
	}
	return append(values, next)
}

func upsertRefreshTransaction(
	values []domain.TransactionRecord,
	next domain.TransactionRecord,
) []domain.TransactionRecord {
	for index := range values {
		if values[index].ID == next.ID {
			values[index] = next
			return values
		}
	}
	return append(values, next)
}

func compareRefreshIdentity(left, right domain.ExternalIdentity) int {
	if compared := strings.Compare(left.Namespace, right.Namespace); compared != 0 {
		return compared
	}
	return strings.Compare(left.ExternalID, right.ExternalID)
}

func compareRefreshAllocation(left, right store.LabelAllocation) int {
	if compared := strings.Compare(left.Namespace, right.Namespace); compared != 0 {
		return compared
	}
	return strings.Compare(left.ExternalID, right.ExternalID)
}

func providerTransactionByExternalID(
	t testing.TB,
	committed domain.CommittedProfile,
	externalID string,
) domain.TransactionRecord {
	t.Helper()
	for _, transaction := range committed.Transactions {
		if transaction.ProviderID == externalID && transaction.Provider == "monarch" {
			return transaction
		}
	}
	t.Fatalf("provider transaction %q not found", externalID)
	return domain.TransactionRecord{}
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
	loaded, err := profile.Load(context.Background())
	require.NoError(t, err)
	require.NotEmpty(t, loaded.Committed.Transactions)
	target := loaded.Committed.Transactions[0].ID
	_, err = profile.database.ExecContext(context.Background(), `
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
		SELECT id, 0, ? FROM journal_operations
		WHERE id LIKE 'operation_refresh_limit_%'`, target)
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

func TestProviderRefreshRejectsInvalidPlannerOutputWithoutChanges(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		mutate func(store.RefreshInputs, store.RefreshPlan) store.RefreshPlan
	}{
		{
			name: "candidate ignored",
			mutate: func(inputs store.RefreshInputs, _ store.RefreshPlan) store.RefreshPlan {
				replayed, err := profilereplay.Replay(inputs.Snapshot)
				require.NoError(t, err)
				return store.RefreshPlan{
					Committed: inputs.Snapshot.Committed, Effective: replayed.Effective,
					Journal: inputs.Snapshot.Journal, Cursor: inputs.Snapshot.Cursor,
					KnownDrills: inputs.Snapshot.KnownDrills, Allocations: inputs.Allocations,
					Summary: refreshCandidateSummary(inputs.Candidate),
				}
			},
		},
		{
			name: "candidate mapping removed",
			mutate: func(_ store.RefreshInputs, plan store.RefreshPlan) store.RefreshPlan {
				plan.Committed.ExternalIdentities = slices.DeleteFunc(
					plan.Committed.ExternalIdentities,
					func(identity domain.ExternalIdentity) bool {
						return identity.Namespace == "monarch/transaction"
					},
				)
				plan.Effective = plan.Committed.Clone()
				return plan
			},
		},
		{
			name: "candidate allocation removed",
			mutate: func(_ store.RefreshInputs, plan store.RefreshPlan) store.RefreshPlan {
				plan.Allocations = slices.DeleteFunc(
					plan.Allocations,
					func(allocation store.LabelAllocation) bool {
						return allocation.Namespace == "monarch/merchant"
					},
				)
				return plan
			},
		},
		{
			name: "durable dimension removed",
			mutate: func(_ store.RefreshInputs, plan store.RefreshPlan) store.RefreshPlan {
				plan.Committed.Merchants = plan.Committed.Merchants[1:]
				plan.Effective = plan.Committed.Clone()
				return plan
			},
		},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			ctx := context.Background()
			profile := openSeededProfile(t, DefaultOptions)
			now := time.Date(2026, time.August, 15, 21, 30, 0, 0, time.UTC)
			bindProviderForRefreshTest(t, profile, now)
			acquireProviderRefreshLease(t, profile, "invalid-planner-owner", now)
			before, err := profile.Load(ctx)
			require.NoError(t, err)
			stateBefore, err := profile.ProviderState(ctx)
			require.NoError(t, err)

			_, err = profile.ApplyProviderRefresh(ctx, store.AtomicRefreshRequest{
				ExpectedGeneration: 0, LeaseOwnerID: "invalid-planner-owner",
				Candidate: providerRefreshCandidate(t, now), ObservedAt: now,
			}, func(inputs store.RefreshInputs) (store.RefreshPlan, error) {
				valid, planErr := passthroughRefreshPlanner(inputs)
				return test.mutate(inputs, valid), planErr
			})
			assertStoreCode(t, err, store.CodeInvalidOperation)
			after, loadErr := profile.Load(ctx)
			require.NoError(t, loadErr)
			stateAfter, stateErr := profile.ProviderState(ctx)
			require.NoError(t, stateErr)
			assert.Equal(t, before, after)
			assert.Equal(t, stateBefore, stateAfter)
		})
	}
}

func TestProviderRefreshRejectsRedoReactivationAndPayloadRewrite(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name   string
		mutate func(domain.ProfileSnapshot, store.RefreshPlan) store.RefreshPlan
	}{
		{
			name: "redo reactivation",
			mutate: func(snapshot domain.ProfileSnapshot, plan store.RefreshPlan) store.RefreshPlan {
				plan.Journal = snapshot.Journal
				plan.Cursor = len(plan.Journal)
				replayed, err := profilereplay.Replay(domain.ProfileSnapshot{
					Committed: plan.Committed, Journal: plan.Journal, Cursor: plan.Cursor,
					KnownDrills: plan.KnownDrills,
				})
				require.NoError(t, err)
				plan.Effective = replayed.Effective
				return plan
			},
		},
		{
			name: "payload rewrite",
			mutate: func(_ domain.ProfileSnapshot, plan store.RefreshPlan) store.RefreshPlan {
				plan.Journal[0].CreatedAt = plan.Journal[0].CreatedAt.Add(time.Millisecond)
				return plan
			},
		},
	} {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			ctx := context.Background()
			profile := openSeededProfile(t, DefaultOptions)
			now := time.Date(2026, time.August, 15, 21, 45, 0, 0, time.UTC)
			bindProviderForRefreshTest(t, profile, now)
			snapshot, err := profile.Load(ctx)
			require.NoError(t, err)
			revision, err := profile.Append(ctx, snapshot.Revision, draftHideOperation(
				"operation_active", snapshot.Revision, snapshot.Committed.Transactions[0].ID,
			))
			require.NoError(t, err)
			revision, err = profile.Append(ctx, revision, draftHideOperation(
				"operation_redo", revision, snapshot.Committed.Transactions[1].ID,
			))
			require.NoError(t, err)
			_, err = profile.MoveCursor(ctx, revision, -1)
			require.NoError(t, err)
			acquireProviderRefreshLease(t, profile, "rewrite-owner", now)
			before, err := profile.Load(ctx)
			require.NoError(t, err)

			_, err = profile.ApplyProviderRefresh(ctx, store.AtomicRefreshRequest{
				ExpectedGeneration: 0, LeaseOwnerID: "rewrite-owner",
				Candidate: providerRefreshCandidate(t, now), ObservedAt: now,
			}, func(inputs store.RefreshInputs) (store.RefreshPlan, error) {
				valid, planErr := passthroughRefreshPlanner(inputs)
				return test.mutate(inputs.Snapshot, valid), planErr
			})
			assertStoreCode(t, err, store.CodeInvalidOperation)
			after, loadErr := profile.Load(ctx)
			require.NoError(t, loadErr)
			assert.Equal(t, before, after)
		})
	}
}

func TestProviderRefreshRejectsDroppingStillValidActiveOperation(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	profile := openSeededProfile(t, DefaultOptions)
	now := time.Date(2026, time.August, 15, 21, 50, 0, 0, time.UTC)
	bindProviderForRefreshTest(t, profile, now)
	snapshot, err := profile.Load(ctx)
	require.NoError(t, err)
	_, err = profile.Append(ctx, snapshot.Revision, draftHideOperation(
		"operation_must_survive", snapshot.Revision, snapshot.Committed.Transactions[0].ID,
	))
	require.NoError(t, err)
	acquireProviderRefreshLease(t, profile, "drop-owner", now)

	_, err = profile.ApplyProviderRefresh(ctx, store.AtomicRefreshRequest{
		ExpectedGeneration: 0, LeaseOwnerID: "drop-owner",
		Candidate: providerRefreshCandidate(t, now), ObservedAt: now,
	}, func(inputs store.RefreshInputs) (store.RefreshPlan, error) {
		plan, planErr := passthroughRefreshPlanner(inputs)
		plan.Journal = nil
		plan.Cursor = 0
		plan.Effective = plan.Committed.Clone()
		return plan, planErr
	})
	assertStoreCode(t, err, store.CodeInvalidOperation)
}

func TestProviderRefreshRejectsNewIdentityMappedOntoUnrelatedEntity(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	profile := openSeededProfile(t, DefaultOptions)
	now := time.Date(2026, time.August, 15, 21, 55, 0, 0, time.UTC)
	bindProviderForRefreshTest(t, profile, now)
	acquireProviderRefreshLease(t, profile, "identity-owner", now)
	candidate := providerRefreshCandidate(t, now)
	proposed := providerRefreshProposals(candidate)

	_, err := profile.ApplyProviderRefresh(ctx, store.AtomicRefreshRequest{
		ExpectedGeneration: 0, LeaseOwnerID: "identity-owner",
		Candidate: candidate, ProposedIDs: proposed, ObservedAt: now,
	}, func(inputs store.RefreshInputs) (store.RefreshPlan, error) {
		plan, planErr := passthroughRefreshPlanner(inputs)
		if planErr != nil {
			return store.RefreshPlan{}, planErr
		}
		unrelated := inputs.Snapshot.Committed.Merchants[0].ID
		var imported domain.EntityID
		for index := range plan.Committed.ExternalIdentities {
			identity := &plan.Committed.ExternalIdentities[index]
			if identity.EntityType == domain.EntityKindMerchant &&
				identity.ExternalID == "merchant-example" {
				imported = identity.EntityID
				identity.EntityID = unrelated
			}
		}
		plan.Committed.Merchants = slices.DeleteFunc(
			plan.Committed.Merchants,
			func(value domain.Merchant) bool { return value.ID == imported },
		)
		for index := range plan.Committed.Transactions {
			if plan.Committed.Transactions[index].ProviderID == "transaction-example" {
				plan.Committed.Transactions[index].MerchantID = unrelated
			}
		}
		plan.Effective = plan.Committed.Clone()
		return plan, nil
	})
	assertStoreCode(t, err, store.CodeInvalidOperation)
}

func bindProviderForRefreshTest(t *testing.T, profile *profile, boundAt time.Time) {
	t.Helper()
	_, err := profile.database.ExecContext(context.Background(), `
		INSERT INTO provider_binding(
			singleton, kind, namespace, remote_profile_id, currency, scale, bound_at_unix_ms
		) VALUES (1, 'monarch', 'monarch', 'subscription-example', 'USD', 2, ?)`, boundAt.UnixMilli())
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

func providerRefreshProposals(candidate domain.ImportSnapshot) map[string]domain.EntityID {
	result := make(map[string]domain.EntityID)
	for _, batch := range []struct {
		kind     domain.EntityKind
		entities []domain.ImportEntity
	}{
		{domain.EntityKindAccount, candidate.Accounts},
		{domain.EntityKindMerchant, candidate.Merchants},
		{domain.EntityKindGroup, candidate.Groups},
		{domain.EntityKindCategory, candidate.Categories},
	} {
		for _, entity := range batch.entities {
			result["monarch/"+string(batch.kind)+"\x00"+entity.ExternalID] =
				domain.EntityID("refresh_" + string(batch.kind) + "_" + entity.ExternalID)
		}
	}
	for _, transaction := range candidate.Transactions {
		result["monarch/transaction\x00"+transaction.ExternalID] =
			domain.EntityID("refresh_transaction_" + transaction.ExternalID)
	}
	return result
}
