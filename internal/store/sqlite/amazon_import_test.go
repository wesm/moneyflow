package sqlite

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/wesm/moneyflow/internal/domain"
	"github.com/wesm/moneyflow/internal/store"
)

func TestAmazonImportPersistsSettingsLedgerAndHistoryAtomically(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	handle := openSeededProfile(t, DefaultOptions)
	before, err := handle.Load(ctx)
	require.NoError(t, err)
	now := time.Date(2026, time.August, 20, 15, 0, 0, 0, time.UTC)
	transaction := before.Committed.Transactions[0]

	commit, err := handle.ApplyAmazonImport(ctx, store.AtomicAmazonImportRequest{
		ImportID: "amazon-import-test", StartedAt: now.Add(-time.Second), ImportedAt: now,
		CandidateDigest: strings.Repeat("a", 64), ProposedCounts: store.AmazonIDCounts{Sources: 1},
	}, func(state store.AmazonImportState, proposed store.ProposedAmazonIDs) (store.AmazonImportPlan, error) {
		require.Equal(t, before, state.Snapshot)
		require.Len(t, proposed.SourceIdentities, 1)
		committed := before.Committed.Clone()
		for index := range committed.Transactions {
			if committed.Transactions[index].ID == transaction.ID {
				committed.Transactions[index].Provider = "amazon"
				committed.Transactions[index].ProviderID = proposed.SourceIdentities[0]
			}
		}
		committed.ExternalIdentities = append(committed.ExternalIdentities, domain.ExternalIdentity{
			EntityType: domain.EntityKindTransaction, EntityID: transaction.ID,
			Namespace: "amazon/order-item", ExternalID: proposed.SourceIdentities[0],
		})
		return store.AmazonImportPlan{
			Committed: committed, Journal: append([]domain.Operation(nil), before.Journal...),
			Cursor: before.Cursor, KnownDrills: append([]domain.DrillIdentity(nil), before.KnownDrills...),
			Settings: &store.AmazonSettings{Currency: "USD", Scale: 2, CreatedAt: now},
			Items: []store.AmazonOrderItem{{
				LocalTransactionID: transaction.ID, SourceIdentity: proposed.SourceIdentities[0],
				OrderID: "example-order", ASIN: "EXAMPLEASIN", ProductName: "Example Product",
				OrderDate: transaction.Date, Quantity: 1, AmountMinor: transaction.Amount.Minor,
				Currency: "USD", Scale: 2, OrderStatus: "Closed", ShipmentStatus: "Delivered",
				IdentityFingerprint: strings.Repeat("b", 64), FullFingerprint: strings.Repeat("c", 64),
				LocalAccountID: transaction.AccountID, LocalMerchantID: transaction.MerchantID,
				LocalCategoryID: transaction.CategoryID, LocalNotes: transaction.Notes,
				LocalHidden: transaction.Hidden,
			}},
			History:        store.AmazonImportHistory{FileCount: 1, LogicalRecordCount: 1, InsertedCount: 1},
			SemanticChange: true,
		}, nil
	})
	require.NoError(t, err)
	assert.Equal(t, before.Revision+1, commit.Revision)
	assert.True(t, commit.SemanticChange)

	state, err := handle.LoadAmazonState(ctx)
	require.NoError(t, err)
	require.NotNil(t, state.Settings)
	assert.Equal(t, "USD", string(state.Settings.Currency))
	require.Len(t, state.Items, 1)
	assert.Equal(t, transaction.ID, state.Items[0].LocalTransactionID)
	assert.Equal(t, proposedSourcePattern, state.Items[0].SourceIdentity[:len("amazon_item_")])

	var histories int
	require.NoError(t, handle.database.QueryRowContext(ctx,
		"SELECT count(*) FROM amazon_import_history WHERE import_id = ?", "amazon-import-test").Scan(&histories))
	assert.Equal(t, 1, histories)
}

const proposedSourcePattern = "amazon_item_"

func TestAmazonImportNoOpAppendsHistoryWithoutRevisionChurn(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	profileStore, err := Open(ctx, temporaryPaths(t), DefaultOptions)
	require.NoError(t, err)
	handle := profileStore.(*profile)
	t.Cleanup(func() { require.NoError(t, handle.Close()) })
	initial := validAmazonStoreState(t)
	initial.Snapshot.Revision = 0
	now := time.Date(2026, time.August, 20, 16, 0, 0, 0, time.UTC)
	_, err = handle.ApplyAmazonImport(ctx, store.AtomicAmazonImportRequest{
		ImportID: "amazon-import-initial", StartedAt: now, ImportedAt: now,
		CandidateDigest: strings.Repeat("c", 64),
	}, func(store.AmazonImportState, store.ProposedAmazonIDs) (store.AmazonImportPlan, error) {
		plan := planFromAmazonStoreState(initial)
		plan.SemanticChange = true
		return plan, nil
	})
	require.NoError(t, err)
	before, err := handle.Load(ctx)
	require.NoError(t, err)

	commit, err := handle.ApplyAmazonImport(ctx, store.AtomicAmazonImportRequest{
		ImportID: "amazon-import-noop", StartedAt: now, ImportedAt: now,
		CandidateDigest: strings.Repeat("d", 64),
	}, func(state store.AmazonImportState, _ store.ProposedAmazonIDs) (store.AmazonImportPlan, error) {
		return store.AmazonImportPlan{
			Committed: state.Snapshot.Committed.Clone(), Journal: append([]domain.Operation(nil), state.Snapshot.Journal...),
			Cursor: state.Snapshot.Cursor, KnownDrills: append([]domain.DrillIdentity(nil), state.Snapshot.KnownDrills...),
			Settings: state.Settings, Items: state.Items, Allocations: state.Allocations,
			History: store.AmazonImportHistory{UnchangedCount: len(state.Items)}, SemanticChange: false,
		}, nil
	})
	if err != nil {
		t.Fatalf("apply no-op Amazon import: %v", errors.Unwrap(err))
	}
	assert.Equal(t, before.Revision, commit.Revision)
	assert.False(t, commit.SemanticChange)
}

func TestValidateAmazonImportPlanReplaysJournalAgainstPlannedBase(t *testing.T) {
	t.Parallel()
	state := validAmazonStoreState(t)
	plan := planFromAmazonStoreState(state)
	plan.Journal = []domain.Operation{{
		ID: "operation-missing-target", Sequence: 1, Type: domain.OperationTransactionHide,
		PayloadVersion: 1, CreatedRevision: state.Snapshot.Revision, CreatedAt: time.Now().UTC(),
		Targets: []domain.EntityID{"transaction-missing"}, HideToggle: &domain.HideTogglePayload{},
	}}
	plan.Cursor = 1
	plan.SemanticChange = true

	err := validateAmazonImportPlan(state, plan)
	assert.ErrorContains(t, err, "replay")
}

func TestValidateAmazonImportPlanKeepsInstalledSettingsImmutable(t *testing.T) {
	t.Parallel()
	state := validAmazonStoreState(t)
	plan := planFromAmazonStoreState(state)
	plan.Settings = &store.AmazonSettings{
		Currency: "EUR", Scale: 2, CreatedAt: state.Settings.CreatedAt,
	}
	plan.SemanticChange = true

	err := validateAmazonImportPlan(state, plan)
	assert.ErrorContains(t, err, "settings")
}

func TestCloneAmazonImportStateOwnsUnitPricePointers(t *testing.T) {
	t.Parallel()
	unitPrice := int64(1234)
	state := store.AmazonImportState{Items: []store.AmazonOrderItem{{UnitPriceMinor: &unitPrice}}}
	clone := cloneAmazonImportState(state)
	*clone.Items[0].UnitPriceMinor = 5678
	assert.Equal(t, int64(1234), *state.Items[0].UnitPriceMinor)
}

func validAmazonStoreState(t *testing.T) store.AmazonImportState {
	t.Helper()
	date, err := domain.ParseDate("2026-08-20")
	require.NoError(t, err)
	committed := domain.CommittedProfile{
		Accounts:  []domain.Account{{ID: "account-a", Label: "Order A", CollisionKey: "order a"}},
		Merchants: []domain.Merchant{{ID: "merchant-a", Label: "Product A", CollisionKey: "product a"}},
		Groups: []domain.CategoryGroup{{
			ID: domain.UncategorizedGroupID, Label: domain.UncategorizedLabel,
			CollisionKey: domain.UncategorizedCollisionKey, Protected: true,
		}},
		Categories: []domain.Category{{
			ID: domain.UncategorizedCategoryID, GroupID: domain.UncategorizedGroupID,
			Label: domain.UncategorizedLabel, CollisionKey: domain.UncategorizedCollisionKey, Protected: true,
		}},
		Transactions: []domain.TransactionRecord{{
			ID: "transaction-a", Provider: "amazon", ProviderID: "amazon_item_aaaaaaaaaaaaaaaaaaaaaaaaaa",
			AccountID: "account-a", MerchantID: "merchant-a", CategoryID: domain.UncategorizedCategoryID,
			Date: date, Amount: domain.Money{Minor: -1234, Currency: "USD", Scale: 2},
		}},
		ExternalIdentities: []domain.ExternalIdentity{{
			EntityType: domain.EntityKindTransaction, EntityID: "transaction-a",
			Namespace: "amazon/order-item", ExternalID: "amazon_item_aaaaaaaaaaaaaaaaaaaaaaaaaa",
		}},
	}
	require.NoError(t, committed.Validate())
	settings := &store.AmazonSettings{Currency: "USD", Scale: 2, CreatedAt: time.Now().UTC()}
	item := store.AmazonOrderItem{
		LocalTransactionID: "transaction-a", SourceIdentity: "amazon_item_aaaaaaaaaaaaaaaaaaaaaaaaaa",
		OrderID: "order-a", ASIN: "ASIN-A", ProductName: "Product A", OrderDate: date,
		Quantity: 1, AmountMinor: -1234, Currency: "USD", Scale: 2,
		OrderStatus: "Closed", ShipmentStatus: "Delivered",
		IdentityFingerprint: strings.Repeat("a", 64), FullFingerprint: strings.Repeat("b", 64),
		LocalAccountID: "account-a", LocalMerchantID: "merchant-a",
		LocalCategoryID: domain.UncategorizedCategoryID,
	}
	return store.AmazonImportState{
		Snapshot: domain.ProfileSnapshot{Revision: 1, Committed: committed}, Settings: settings,
		Items: []store.AmazonOrderItem{item},
	}
}

func planFromAmazonStoreState(state store.AmazonImportState) store.AmazonImportPlan {
	return store.AmazonImportPlan{
		Committed: state.Snapshot.Committed.Clone(), Journal: append([]domain.Operation(nil), state.Snapshot.Journal...),
		Cursor: state.Snapshot.Cursor, KnownDrills: append([]domain.DrillIdentity(nil), state.Snapshot.KnownDrills...),
		Settings: state.Settings, Items: append([]store.AmazonOrderItem(nil), state.Items...),
		Allocations: append([]store.LabelAllocation(nil), state.Allocations...),
	}
}
