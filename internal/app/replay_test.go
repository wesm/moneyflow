package app_test

import (
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/wesm/moneyflow/internal/app"
	"github.com/wesm/moneyflow/internal/domain"
)

func TestApplyOperationHandlesEveryTypedTransition(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		operation domain.Operation
		assert    func(*testing.T, domain.CommittedProfile)
	}{
		"merchant label": {
			operation: labelOperation(1, domain.OperationMerchantLabel, "merchant_a", "Merchant A Renamed"),
			assert: func(t *testing.T, profile domain.CommittedProfile) {
				assert.Equal(t, "Merchant A Renamed", merchantByID(t, profile, "merchant_a").Label)
			},
		},
		"merchant merge": {
			operation: mergeOperation(1, domain.OperationMerchantMerge, "merchant_a", "merchant_b"),
			assert: func(t *testing.T, profile domain.CommittedProfile) {
				assert.True(t, merchantByID(t, profile, "merchant_a").Retired)
				assert.Equal(t, domain.EntityID("merchant_b"), transactionByID(t, profile, "transaction_a").MerchantID)
			},
		},
		"merchant reassign with create": {
			operation: merchantReassignOperation(1),
			assert: func(t *testing.T, profile domain.CommittedProfile) {
				assert.Equal(t, domain.EntityID("merchant_new"), transactionByID(t, profile, "transaction_a").MerchantID)
				assert.Equal(t, "New Merchant", merchantByID(t, profile, "merchant_new").Label)
			},
		},
		"category assign": {
			operation: reassignOperation(1, domain.OperationCategoryAssign, "category_b", "transaction_a"),
			assert: func(t *testing.T, profile domain.CommittedProfile) {
				assert.Equal(t, domain.EntityID("category_b"), transactionByID(t, profile, "transaction_a").CategoryID)
			},
		},
		"category create and assign": {
			operation: createCategoryOperation(1),
			assert: func(t *testing.T, profile domain.CommittedProfile) {
				assert.Equal(t, domain.EntityID("category_new"), transactionByID(t, profile, "transaction_a").CategoryID)
				assert.Equal(t, domain.EntityID("group_a"), categoryByID(t, profile, "category_new").GroupID)
			},
		},
		"category label": {
			operation: labelOperation(1, domain.OperationCategoryLabel, "category_a", "Category A Renamed"),
			assert: func(t *testing.T, profile domain.CommittedProfile) {
				assert.Equal(t, "Category A Renamed", categoryByID(t, profile, "category_a").Label)
			},
		},
		"category move": {
			operation: moveCategoryOperation(1),
			assert: func(t *testing.T, profile domain.CommittedProfile) {
				assert.Equal(t, domain.EntityID("group_b"), categoryByID(t, profile, "category_a").GroupID)
			},
		},
		"category merge": {
			operation: mergeOperation(1, domain.OperationCategoryMerge, "category_a", "category_b"),
			assert: func(t *testing.T, profile domain.CommittedProfile) {
				assert.True(t, categoryByID(t, profile, "category_a").Retired)
				assert.Equal(t, domain.EntityID("category_b"), transactionByID(t, profile, "transaction_a").CategoryID)
			},
		},
		"category delete": {
			operation: deleteOperation(1, domain.OperationCategoryDelete, "category_a", domain.UncategorizedCategoryID),
			assert: func(t *testing.T, profile domain.CommittedProfile) {
				assert.True(t, categoryByID(t, profile, "category_a").Retired)
				assert.Equal(t, domain.UncategorizedCategoryID, transactionByID(t, profile, "transaction_a").CategoryID)
			},
		},
		"group create": {
			operation: createGroupOperation(1),
			assert: func(t *testing.T, profile domain.CommittedProfile) {
				assert.Equal(t, "New Group", groupByID(t, profile, "group_new").Label)
			},
		},
		"group label": {
			operation: labelOperation(1, domain.OperationGroupLabel, "group_a", "Group A Renamed"),
			assert: func(t *testing.T, profile domain.CommittedProfile) {
				assert.Equal(t, "Group A Renamed", groupByID(t, profile, "group_a").Label)
			},
		},
		"group merge": {
			operation: mergeOperation(1, domain.OperationGroupMerge, "group_a", "group_b"),
			assert: func(t *testing.T, profile domain.CommittedProfile) {
				assert.True(t, groupByID(t, profile, "group_a").Retired)
				assert.Equal(t, domain.EntityID("group_b"), categoryByID(t, profile, "category_a").GroupID)
			},
		},
		"group delete": {
			operation: deleteOperation(1, domain.OperationGroupDelete, "group_a", domain.UncategorizedGroupID),
			assert: func(t *testing.T, profile domain.CommittedProfile) {
				assert.True(t, groupByID(t, profile, "group_a").Retired)
				assert.Equal(t, domain.UncategorizedGroupID, categoryByID(t, profile, "category_a").GroupID)
			},
		},
		"hide toggle": {
			operation: hideOperation(1, "transaction_a", "transaction_b"),
			assert: func(t *testing.T, profile domain.CommittedProfile) {
				assert.True(t, transactionByID(t, profile, "transaction_a").Hidden)
				assert.False(t, transactionByID(t, profile, "transaction_b").Hidden)
			},
		},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			committed := replayProfile(t)
			before := committed.Clone()
			effective, err := app.ApplyOperation(committed, test.operation)
			require.NoError(t, err)
			assert.Equal(t, before, committed)
			test.assert(t, effective)
		})
	}
}

func TestReplayUsesOnlyActivePrefixAndPreservesInactiveTail(t *testing.T) {
	t.Parallel()

	committed := replayProfile(t)
	first := labelOperation(1, domain.OperationMerchantLabel, "merchant_a", "First")
	second := labelOperation(2, domain.OperationMerchantLabel, "merchant_a", "Second")
	snapshot := domain.ProfileSnapshot{
		Revision: 4, Cursor: 1, Committed: committed, Journal: []domain.Operation{first, second},
	}
	before := snapshot.Clone()
	effective, err := app.Replay(snapshot)
	require.NoError(t, err)
	assert.Equal(t, "First", merchantByID(t, effective.Effective, "merchant_a").Label)
	assert.Equal(t, before, snapshot)
	assert.Equal(t, []domain.Operation{first, second}, effective.Journal)
	assert.Equal(t, committed, effective.Committed)
}

func TestReplayRetargetsTombstonesAcrossChainedMerges(t *testing.T) {
	t.Parallel()

	tests := []struct {
		kind        domain.OperationType
		firstSource domain.EntityID
		middle      domain.EntityID
		destination domain.EntityID
		lookup      func(*testing.T, domain.CommittedProfile, domain.EntityID) *domain.EntityID
	}{
		{
			kind: domain.OperationMerchantMerge, firstSource: "merchant_a", middle: "merchant_b",
			destination: "merchant_c", lookup: func(t *testing.T, profile domain.CommittedProfile, id domain.EntityID) *domain.EntityID {
				return merchantByID(t, profile, id).MergeDestination
			},
		},
		{
			kind: domain.OperationCategoryMerge, firstSource: "category_a", middle: "category_b",
			destination: "category_c", lookup: func(t *testing.T, profile domain.CommittedProfile, id domain.EntityID) *domain.EntityID {
				return categoryByID(t, profile, id).MergeDestination
			},
		},
		{
			kind: domain.OperationGroupMerge, firstSource: "group_a", middle: "group_b",
			destination: "group_c", lookup: func(t *testing.T, profile domain.CommittedProfile, id domain.EntityID) *domain.EntityID {
				return groupByID(t, profile, id).MergeDestination
			},
		},
	}
	for _, test := range tests {
		t.Run(string(test.kind), func(t *testing.T) {
			t.Parallel()
			profile := replayProfileWithThirdEntities(t)
			first, err := app.ApplyOperation(profile, mergeOperation(1, test.kind, test.firstSource, test.middle))
			require.NoError(t, err)
			second, err := app.ApplyOperation(first, mergeOperation(2, test.kind, test.middle, test.destination))
			require.NoError(t, err)
			require.NotNil(t, test.lookup(t, second, test.firstSource))
			assert.Equal(t, test.destination, *test.lookup(t, second, test.firstSource))
		})
	}
}

func TestGroupRetirementMovesRetiredCategories(t *testing.T) {
	t.Parallel()

	profile := replayProfileWithThirdEntities(t)
	retired, err := app.ApplyOperation(
		profile,
		mergeOperation(1, domain.OperationCategoryMerge, "category_a", "category_b"),
	)
	require.NoError(t, err)
	moved, err := app.ApplyOperation(
		retired,
		mergeOperation(2, domain.OperationGroupMerge, "group_a", "group_b"),
	)
	require.NoError(t, err)
	assert.Equal(t, domain.EntityID("group_b"), categoryByID(t, moved, "category_a").GroupID)
}

func TestApplyOperationRejectsProtectedRetiredAndMismatchedTargets(t *testing.T) {
	t.Parallel()

	tests := []domain.Operation{
		labelOperation(1, domain.OperationCategoryLabel, domain.UncategorizedCategoryID, "Changed"),
		deleteOperation(1, domain.OperationGroupDelete, domain.UncategorizedGroupID, "group_a"),
		mergeOperation(1, domain.OperationMerchantMerge, "merchant_a", "merchant_a"),
		reassignOperation(1, domain.OperationCategoryAssign, "category_missing", "transaction_a"),
		hideOperation(1, "transaction_missing"),
	}
	for _, operation := range tests {
		_, err := app.ApplyOperation(replayProfile(t), operation)
		assert.Error(t, err)
	}
}

func storedOperation(sequence int64, kind domain.OperationType, targets ...domain.EntityID) domain.Operation {
	return domain.Operation{
		ID: fmt.Sprintf("operation_%d", sequence), Sequence: sequence, Type: kind,
		PayloadVersion: 1, CreatedRevision: 1,
		CreatedAt: time.Date(2026, time.August, 14, 12, 0, 0, 0, time.UTC),
		Targets:   targets,
	}
}

func labelOperation(sequence int64, kind domain.OperationType, id domain.EntityID, label string) domain.Operation {
	operation := storedOperation(sequence, kind, id)
	key, _ := domain.CollisionKey(label)
	operation.Label = &domain.LabelPayload{EntityID: id, Label: label, CollisionKey: key}
	return operation
}

func mergeOperation(sequence int64, kind domain.OperationType, source, destination domain.EntityID) domain.Operation {
	operation := storedOperation(sequence, kind, source)
	operation.Merge = &domain.MergePayload{SourceID: source, DestinationID: destination}
	return operation
}

func merchantReassignOperation(sequence int64) domain.Operation {
	operation := storedOperation(sequence, domain.OperationMerchantReassign, "transaction_a")
	operation.Reassign = &domain.ReassignPayload{
		DestinationID: "merchant_new",
		CreatedMerchant: &domain.Merchant{
			ID: "merchant_new", Label: "New Merchant", CollisionKey: "new merchant",
		},
	}
	return operation
}

func reassignOperation(
	sequence int64,
	kind domain.OperationType,
	destination domain.EntityID,
	targets ...domain.EntityID,
) domain.Operation {
	operation := storedOperation(sequence, kind, targets...)
	operation.Reassign = &domain.ReassignPayload{DestinationID: destination}
	return operation
}

func createCategoryOperation(sequence int64) domain.Operation {
	operation := storedOperation(sequence, domain.OperationCategoryCreate, "transaction_a")
	operation.Create = &domain.CreatePayload{
		EntityType: string(domain.EntityKindCategory), EntityID: "category_new",
		Label: "New Category", CollisionKey: "new category", ParentID: "group_a",
	}
	return operation
}

func moveCategoryOperation(sequence int64) domain.Operation {
	operation := storedOperation(sequence, domain.OperationCategoryMove, "category_a")
	operation.Move = &domain.MovePayload{EntityID: "category_a", DestinationID: "group_b"}
	return operation
}

func deleteOperation(
	sequence int64,
	kind domain.OperationType,
	source, replacement domain.EntityID,
) domain.Operation {
	operation := storedOperation(sequence, kind, source)
	operation.Delete = &domain.DeletePayload{SourceID: source, ReplacementID: replacement}
	return operation
}

func createGroupOperation(sequence int64) domain.Operation {
	operation := storedOperation(sequence, domain.OperationGroupCreate, "group_new")
	operation.Create = &domain.CreatePayload{
		EntityType: string(domain.EntityKindGroup), EntityID: "group_new",
		Label: "New Group", CollisionKey: "new group",
	}
	return operation
}

func hideOperation(sequence int64, targets ...domain.EntityID) domain.Operation {
	operation := storedOperation(sequence, domain.OperationTransactionHide, targets...)
	operation.HideToggle = &domain.HideTogglePayload{}
	return operation
}

func replayProfile(t *testing.T) domain.CommittedProfile {
	t.Helper()
	date, err := domain.ParseDate("2026-08-14")
	require.NoError(t, err)
	profile := domain.CommittedProfile{
		Accounts: []domain.Account{{ID: "account_a", Label: "Account A", CollisionKey: "account a"}},
		Merchants: []domain.Merchant{
			{ID: "merchant_a", Label: "Merchant A", CollisionKey: "merchant a"},
			{ID: "merchant_b", Label: "Merchant B", CollisionKey: "merchant b"},
		},
		Groups: []domain.CategoryGroup{
			{ID: "group_a", Label: "Group A", CollisionKey: "group a"},
			{ID: "group_b", Label: "Group B", CollisionKey: "group b"},
			{
				ID: domain.UncategorizedGroupID, Label: "Uncategorized",
				CollisionKey: "uncategorized", Protected: true,
			},
		},
		Categories: []domain.Category{
			{ID: "category_a", GroupID: "group_a", Label: "Category A", CollisionKey: "category a"},
			{ID: "category_b", GroupID: "group_b", Label: "Category B", CollisionKey: "category b"},
			{
				ID: domain.UncategorizedCategoryID, GroupID: domain.UncategorizedGroupID,
				Label: "Uncategorized", CollisionKey: "uncategorized", Protected: true,
			},
		},
		Transactions: []domain.TransactionRecord{
			{
				ID: "transaction_a", Provider: "fixture", ProviderID: "provider-a",
				AccountID: "account_a", MerchantID: "merchant_a", CategoryID: "category_a",
				Date: date, Amount: domain.Money{Minor: -100, Currency: "USD", Scale: 2},
			},
			{
				ID: "transaction_b", Provider: "fixture", ProviderID: "provider-b",
				AccountID: "account_a", MerchantID: "merchant_b", CategoryID: "category_b",
				Date: date, Amount: domain.Money{Minor: -200, Currency: "USD", Scale: 2},
				Hidden: true,
			},
		},
	}
	require.NoError(t, profile.Validate())
	return profile
}

func replayProfileWithThirdEntities(t *testing.T) domain.CommittedProfile {
	t.Helper()
	profile := replayProfile(t)
	profile.Merchants = append(profile.Merchants, domain.Merchant{
		ID: "merchant_c", Label: "Merchant C", CollisionKey: "merchant c",
	})
	profile.Groups = append(profile.Groups, domain.CategoryGroup{
		ID: "group_c", Label: "Group C", CollisionKey: "group c",
	})
	profile.Categories = append(profile.Categories, domain.Category{
		ID: "category_c", GroupID: "group_c", Label: "Category C", CollisionKey: "category c",
	})
	require.NoError(t, profile.Validate())
	return profile
}

func merchantByID(t *testing.T, profile domain.CommittedProfile, id domain.EntityID) domain.Merchant {
	t.Helper()
	for _, value := range profile.Merchants {
		if value.ID == id {
			return value
		}
	}
	t.Fatalf("merchant %q not found", id)
	return domain.Merchant{}
}

func groupByID(t *testing.T, profile domain.CommittedProfile, id domain.EntityID) domain.CategoryGroup {
	t.Helper()
	for _, value := range profile.Groups {
		if value.ID == id {
			return value
		}
	}
	t.Fatalf("group %q not found", id)
	return domain.CategoryGroup{}
}

func categoryByID(t *testing.T, profile domain.CommittedProfile, id domain.EntityID) domain.Category {
	t.Helper()
	for _, value := range profile.Categories {
		if value.ID == id {
			return value
		}
	}
	t.Fatalf("category %q not found", id)
	return domain.Category{}
}

func transactionByID(
	t *testing.T,
	profile domain.CommittedProfile,
	id domain.EntityID,
) domain.TransactionRecord {
	t.Helper()
	for _, value := range profile.Transactions {
		if value.ID == id {
			return value
		}
	}
	t.Fatalf("transaction %q not found", id)
	return domain.TransactionRecord{}
}
