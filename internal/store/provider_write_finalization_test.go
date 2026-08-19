package store

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/wesm/moneyflow/internal/domain"
)

func TestBuildProviderWriteFinalizationKeepsDeletedTransactionIdentityTombstone(t *testing.T) {
	t.Parallel()

	date, err := domain.ParseDate("2026-08-18")
	require.NoError(t, err)
	committed := domain.CommittedProfile{
		Accounts: []domain.Account{{ID: "account-a", Label: "Account", CollisionKey: "account"}},
		Merchants: []domain.Merchant{{
			ID: "merchant-a", Label: "Merchant", CollisionKey: "merchant",
		}},
		Groups: []domain.CategoryGroup{
			{ID: "group-a", Label: "Group", CollisionKey: "group"},
			{ID: domain.UncategorizedGroupID, Label: domain.UncategorizedLabel,
				CollisionKey: domain.UncategorizedCollisionKey, Protected: true},
		},
		Categories: []domain.Category{
			{ID: "category-a", GroupID: "group-a", Label: "Category", CollisionKey: "category"},
			{ID: domain.UncategorizedCategoryID, GroupID: domain.UncategorizedGroupID,
				Label: domain.UncategorizedLabel, CollisionKey: domain.UncategorizedCollisionKey,
				Protected: true},
		},
		Transactions: []domain.TransactionRecord{{
			ID: "transaction-a", ProviderID: "provider-transaction-a", Provider: "monarch",
			AccountID: "account-a", MerchantID: "merchant-a", CategoryID: "category-a",
			Date: date, Amount: domain.Money{Minor: -123, Currency: "USD", Scale: 2},
		}},
		ExternalIdentities: []domain.ExternalIdentity{{
			EntityType: domain.EntityKindTransaction, EntityID: "transaction-a",
			Namespace: "monarch/transaction", ExternalID: "provider-transaction-a",
		}},
	}
	require.NoError(t, committed.Validate())
	observedAt := time.Date(2026, time.August, 18, 20, 0, 0, 0, time.UTC)
	snapshot := domain.ProfileSnapshot{
		Revision: 2, Cursor: 1, Committed: committed,
		Journal: []domain.Operation{{
			ID: "operation-delete", Sequence: 1, Type: domain.OperationTransactionDelete,
			PayloadVersion: 1, CreatedRevision: 1, CreatedAt: observedAt,
			Targets:           []domain.EntityID{"transaction-a"},
			TransactionDelete: &domain.TransactionDeletePayload{},
		}},
	}
	inputs := FinalizeProviderWriteInputs{
		Snapshot: snapshot,
		ProviderState: ProviderState{Binding: &ProviderBinding{
			Kind: "monarch", Namespace: "monarch", RemoteProfileID: "remote-a",
			Currency: "USD", Scale: 2,
		}},
		WriteState: ProviderWriteState{
			Batch: &WriteBatch{
				ID: "batch-a", Phase: WritePhaseReconciling, Version: 2,
				FrozenOperationCount: 1, TotalItems: 1, CompletedItems: 1,
			},
			Items: []WriteItem{{
				ID: "item-a", BatchID: "batch-a", Kind: WriteItemDelete,
				TransactionID: "transaction-a", TransactionExternalID: "provider-transaction-a",
				OriginatingOperationIDs: []string{"operation-delete"}, State: WriteItemSucceeded,
			}},
			Results: []WriteResult{{
				Kind: WriteItemDelete, ItemID: "item-a",
				TransactionExternalID: "provider-transaction-a", AlreadyAbsent: true,
				RecordedAt: observedAt,
			}},
		},
		ObservedAt: observedAt,
	}

	plan, err := BuildProviderWriteFinalization(inputs)
	require.NoError(t, err)
	assert.Empty(t, plan.Effective.Transactions)
	assert.Contains(t, plan.Effective.ExternalIdentities, committed.ExternalIdentities[0])
	assert.Equal(t, 1, plan.Summary.OperationCount)
	assert.Equal(t, 1, plan.Summary.ItemCount)
}
