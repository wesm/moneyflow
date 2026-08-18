package app_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/wesm/moneyflow/internal/app"
	"github.com/wesm/moneyflow/internal/domain"
	"github.com/wesm/moneyflow/internal/provider"
	"github.com/wesm/moneyflow/internal/store"
)

func TestProviderWritePlanProducesOneAbsoluteItemPerTransaction(t *testing.T) {
	t.Parallel()

	profile := providerWriteProfile(t)
	profile.Journal = []domain.Operation{
		providerWriteOperation("operation-label", 1, domain.OperationMerchantLabel,
			[]domain.EntityID{"merchant_a"}, &domain.LabelPayload{
				EntityID: "merchant_a", Label: "Normalized Name", CollisionKey: "normalized name",
			}, nil, nil, nil),
		providerWriteOperation("operation-category", 2, domain.OperationCategoryAssign,
			[]domain.EntityID{"transaction_a"}, nil, nil,
			&domain.ReassignPayload{DestinationID: "category_b"}, nil),
		providerWriteOperation("operation-hide", 3, domain.OperationTransactionHide,
			[]domain.EntityID{"transaction_b"}, nil, nil, nil, &domain.HideTogglePayload{}),
	}
	profile.Cursor = len(profile.Journal)

	plan, err := app.BuildProviderWritePlan(store.PrepareProviderWriteInputs{
		Snapshot: profile, ProviderState: providerWriteState(), ProposedBatchID: "batch-a",
		ProposedItemIDs: []string{"item-a", "item-b"}, ObservedAt: providerWriteTime(),
	})
	require.NoError(t, err)
	require.Len(t, plan.Items, 2)
	assert.Equal(t, []domain.EntityID{"transaction_b", "transaction_a"}, []domain.EntityID{
		plan.Items[0].TransactionID, plan.Items[1].TransactionID,
	})
	assert.Equal(t, []string{"10", "2"}, []string{
		plan.Items[0].TransactionExternalID, plan.Items[1].TransactionExternalID,
	})
	for _, item := range plan.Items {
		require.NotNil(t, item.RequestedMerchantName)
		assert.Equal(t, "Normalized Name", *item.RequestedMerchantName)
		assert.Equal(t, store.WriteExpectationNew, item.Expectation)
	}
	assert.Nil(t, plan.Items[0].RequestedCategoryExternalID)
	require.NotNil(t, plan.Items[0].RequestedHidden)
	assert.True(t, *plan.Items[0].RequestedHidden)
	require.NotNil(t, plan.Items[1].RequestedCategoryExternalID)
	assert.Equal(t, "category-provider-b", *plan.Items[1].RequestedCategoryExternalID)
	assert.Nil(t, plan.Items[1].RequestedHidden)
	require.Len(t, plan.Groups, 1)
	assert.Equal(t, "item-a", plan.Groups[0].LeaderItemID)
	assert.True(t, plan.Items[0].GroupLeader)
	assert.False(t, plan.Items[1].GroupLeader)
	assert.Equal(t, []string{"operation-label", "operation-hide"}, plan.Items[0].OriginatingOperationIDs)
	assert.Equal(t, []string{"operation-label", "operation-category"}, plan.Items[1].OriginatingOperationIDs)
	assert.Equal(t, []string{"operation-label", "operation-category", "operation-hide"}, plan.FrozenOperationIDs)
	assert.NotEmpty(t, plan.FrozenPrefixDigest)
}

func TestProviderWritePlanUsesProviderLabelsForExistingDestinations(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		operation   domain.Operation
		expectation store.WriteExpectationKind
		sentLabel   string
	}{
		{
			name: "merge", expectation: store.WriteExpectationMergeDestination,
			operation: providerWriteOperation("operation-merge", 1, domain.OperationMerchantMerge,
				[]domain.EntityID{"merchant_a"}, nil,
				&domain.MergePayload{SourceID: "merchant_a", DestinationID: "merchant_b"}, nil, nil),
			sentLabel: "Provider Merchant B",
		},
		{
			name: "reassign", expectation: store.WriteExpectationExisting,
			operation: providerWriteOperation("operation-reassign", 1, domain.OperationMerchantReassign,
				[]domain.EntityID{"transaction_a"}, nil, nil,
				&domain.ReassignPayload{DestinationID: "merchant_b"}, nil),
			sentLabel: "Provider Merchant B",
		},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			profile := providerWriteProfile(t)
			profile.Journal = []domain.Operation{test.operation}
			profile.Cursor = 1
			plan, err := app.BuildProviderWritePlan(store.PrepareProviderWriteInputs{
				Snapshot: profile, ProviderState: providerWriteState(), ProposedBatchID: "batch-a",
				ProposedItemIDs: []string{"item-a", "item-b"}, ObservedAt: providerWriteTime(),
			})
			if test.name == "reassign" {
				// Only one transaction is affected by the targeted reassign.
				plan, err = app.BuildProviderWritePlan(store.PrepareProviderWriteInputs{
					Snapshot: profile, ProviderState: providerWriteState(), ProposedBatchID: "batch-a",
					ProposedItemIDs: []string{"item-a"}, ObservedAt: providerWriteTime(),
				})
			}
			require.NoError(t, err)
			require.NotEmpty(t, plan.Items)
			for _, item := range plan.Items {
				require.NotNil(t, item.RequestedMerchantName)
				assert.Equal(t, test.sentLabel, *item.RequestedMerchantName)
				assert.NotContains(t, *item.RequestedMerchantName, "·")
				assert.Equal(t, test.expectation, item.Expectation)
				assert.Equal(t, "merchant-provider-b", item.ExpectedMerchantExternalID)
			}
		})
	}
}

func TestProviderWritePlanReturnsNoItemsForNetNoop(t *testing.T) {
	t.Parallel()

	profile := providerWriteProfile(t)
	profile.Journal = []domain.Operation{
		providerWriteOperation("operation-hide-a", 1, domain.OperationTransactionHide,
			[]domain.EntityID{"transaction_a"}, nil, nil, nil, &domain.HideTogglePayload{}),
		providerWriteOperation("operation-hide-b", 2, domain.OperationTransactionHide,
			[]domain.EntityID{"transaction_a"}, nil, nil, nil, &domain.HideTogglePayload{}),
	}
	profile.Cursor = 2
	plan, err := app.BuildProviderWritePlan(store.PrepareProviderWriteInputs{
		Snapshot: profile, ProviderState: providerWriteState(), ProposedBatchID: "batch-a",
		ObservedAt: providerWriteTime(),
	})
	require.NoError(t, err)
	assert.Empty(t, plan.Items)
}

func TestProviderWritePlanRejectsUnsupportedAndUnmappedOperations(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		operation domain.Operation
		itemIDs   []string
	}{
		{
			name: "category create",
			operation: providerWriteOperation("operation-create", 1, domain.OperationCategoryCreate,
				[]domain.EntityID{"category_new"}, nil, nil, nil, nil),
			itemIDs: []string{"item-a"},
		},
		{
			name: "unmapped category",
			operation: providerWriteOperation("operation-category", 1, domain.OperationCategoryAssign,
				[]domain.EntityID{"transaction_a"}, nil, nil,
				&domain.ReassignPayload{DestinationID: domain.UncategorizedCategoryID}, nil),
			itemIDs: []string{"item-a"},
		},
		{
			name: "zero transaction merchant label",
			operation: providerWriteOperation("operation-label", 1, domain.OperationMerchantLabel,
				[]domain.EntityID{"merchant_b"}, &domain.LabelPayload{
					EntityID: "merchant_b", Label: "Unused Name", CollisionKey: "unused name",
				}, nil, nil, nil),
		},
		{
			name: "provider label collision",
			operation: providerWriteOperation("operation-label", 1, domain.OperationMerchantLabel,
				[]domain.EntityID{"merchant_a"}, &domain.LabelPayload{
					EntityID: "merchant_a", Label: "Provider Merchant B",
					CollisionKey: "provider merchant b",
				}, nil, nil, nil),
			itemIDs: []string{"item-a", "item-b"},
		},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			profile := providerWriteProfile(t)
			if test.name == "category create" {
				test.operation.Create = &domain.CreatePayload{
					EntityType: string(domain.EntityKindCategory), EntityID: "category_new",
					Label: "New Category", CollisionKey: "new category", ParentID: "group_a",
				}
			}
			profile.Journal = []domain.Operation{test.operation}
			profile.Cursor = 1
			_, err := app.BuildProviderWritePlan(store.PrepareProviderWriteInputs{
				Snapshot: profile, ProviderState: providerWriteState(), ProposedBatchID: "batch-a",
				ProposedItemIDs: test.itemIDs, ObservedAt: providerWriteTime(),
			})
			require.Error(t, err)
			code, ok := provider.CodeOf(err)
			require.True(t, ok)
			assert.Equal(t, provider.CodeWriteUnsupported, code)
		})
	}
}

func providerWriteProfile(t testing.TB) domain.ProfileSnapshot {
	t.Helper()
	date, err := domain.ParseDate("2026-08-18")
	require.NoError(t, err)
	profile := domain.ProfileSnapshot{
		Revision: 7,
		Committed: domain.CommittedProfile{
			Accounts: []domain.Account{{ID: "account_a", Label: "Account", CollisionKey: "account"}},
			Merchants: []domain.Merchant{
				{ID: "merchant_a", Label: "Merchant A", CollisionKey: "merchant a"},
				{ID: "merchant_b", Label: "Merchant B · b2", CollisionKey: "merchant b · b2"},
			},
			Groups: []domain.CategoryGroup{
				{ID: "group_a", Label: "Group", CollisionKey: "group"},
				{ID: domain.UncategorizedGroupID, Label: domain.UncategorizedLabel,
					CollisionKey: domain.UncategorizedCollisionKey, Protected: true},
			},
			Categories: []domain.Category{
				{ID: "category_a", GroupID: "group_a", Label: "Category A", CollisionKey: "category a"},
				{ID: "category_b", GroupID: "group_a", Label: "Category B", CollisionKey: "category b"},
				{ID: domain.UncategorizedCategoryID, GroupID: domain.UncategorizedGroupID,
					Label: domain.UncategorizedLabel, CollisionKey: domain.UncategorizedCollisionKey, Protected: true},
			},
			Transactions: []domain.TransactionRecord{
				{ID: "transaction_a", ProviderID: "2", Provider: "monarch", AccountID: "account_a",
					MerchantID: "merchant_a", CategoryID: "category_a", Date: date,
					Amount: domain.Money{Minor: -100, Currency: "USD", Scale: 2}},
				{ID: "transaction_b", ProviderID: "10", Provider: "monarch", AccountID: "account_a",
					MerchantID: "merchant_a", CategoryID: "category_a", Date: date,
					Amount: domain.Money{Minor: -200, Currency: "USD", Scale: 2}},
			},
			ExternalIdentities: []domain.ExternalIdentity{
				{EntityType: domain.EntityKindAccount, EntityID: "account_a", Namespace: "monarch/account", ExternalID: "account-provider-a"},
				{EntityType: domain.EntityKindCategory, EntityID: "category_a", Namespace: "monarch/category", ExternalID: "category-provider-a"},
				{EntityType: domain.EntityKindCategory, EntityID: "category_b", Namespace: "monarch/category", ExternalID: "category-provider-b"},
				{EntityType: domain.EntityKindGroup, EntityID: "group_a", Namespace: "monarch/group", ExternalID: "group-provider-a"},
				{EntityType: domain.EntityKindMerchant, EntityID: "merchant_a", Namespace: "monarch/merchant", ExternalID: "merchant-provider-a"},
				{EntityType: domain.EntityKindMerchant, EntityID: "merchant_b", Namespace: "monarch/merchant", ExternalID: "merchant-provider-b"},
				{EntityType: domain.EntityKindTransaction, EntityID: "transaction_a", Namespace: "monarch/transaction", ExternalID: "2"},
				{EntityType: domain.EntityKindTransaction, EntityID: "transaction_b", Namespace: "monarch/transaction", ExternalID: "10"},
			},
		},
	}
	require.NoError(t, profile.Validate())
	return profile
}

func providerWriteState() store.ProviderState {
	return store.ProviderState{
		Binding: &store.ProviderBinding{Kind: "monarch", Namespace: "monarch", RemoteProfileID: "remote-a", Currency: "USD", Scale: 2},
		Allocations: []store.LabelAllocation{
			{Kind: domain.EntityKindMerchant, Namespace: "monarch/merchant", ExternalID: "merchant-provider-a", ProviderLabel: "Provider Merchant A", DisplayLabel: "Merchant A", BaseCollisionKey: "provider merchant a", Unsuffixed: true},
			{Kind: domain.EntityKindMerchant, Namespace: "monarch/merchant", ExternalID: "merchant-provider-b", ProviderLabel: "Provider Merchant B", DisplayLabel: "Merchant B · b2", BaseCollisionKey: "provider merchant b", SuffixToken: "b2"},
		},
	}
}

func providerWriteOperation(
	id string,
	sequence int64,
	typeID domain.OperationType,
	targets []domain.EntityID,
	label *domain.LabelPayload,
	merge *domain.MergePayload,
	reassign *domain.ReassignPayload,
	hide *domain.HideTogglePayload,
) domain.Operation {
	return domain.Operation{
		ID: id, Sequence: sequence, Type: typeID, PayloadVersion: 1,
		CreatedRevision: 7, CreatedAt: providerWriteTime(), Targets: targets,
		Label: label, Merge: merge, Reassign: reassign, HideToggle: hide,
	}
}

func providerWriteTime() time.Time {
	return time.Date(2026, time.August, 18, 12, 0, 0, 0, time.UTC)
}
