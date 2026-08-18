package store_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/wesm/moneyflow/internal/domain"
	"github.com/wesm/moneyflow/internal/store"
)

type fakeProfile struct{}

func (fakeProfile) CurrentRevision(context.Context) (uint64, error) { return 0, nil }
func (fakeProfile) Load(context.Context) (domain.ProfileSnapshot, error) {
	return domain.ProfileSnapshot{}, nil
}
func (fakeProfile) CreateSeededProfile(context.Context, domain.CommittedProfile) (uint64, error) {
	return 0, nil
}
func (fakeProfile) Append(context.Context, uint64, domain.Operation) (uint64, error) {
	return 0, nil
}
func (fakeProfile) MoveCursor(context.Context, uint64, int) (uint64, error) { return 0, nil }
func (fakeProfile) CancelHide(context.Context, uint64, []domain.EntityID) (uint64, error) {
	return 0, nil
}
func (fakeProfile) Fold(context.Context, uint64, store.FoldPlan) (uint64, error) { return 0, nil }
func (fakeProfile) ProviderState(context.Context) (store.ProviderState, error) {
	return store.ProviderState{}, nil
}
func (fakeProfile) AcquireProviderOperationLease(
	context.Context,
	store.ProviderOperationLease,
	time.Time,
) (store.ProviderOperationLease, bool, error) {
	return store.ProviderOperationLease{}, false, nil
}
func (fakeProfile) RenewProviderOperationLease(
	context.Context,
	string,
	store.ProviderOperationKind,
	time.Time,
	time.Time,
) (bool, error) {
	return false, nil
}
func (fakeProfile) ReleaseProviderOperationLease(
	context.Context,
	string,
	store.ProviderOperationKind,
) error {
	return nil
}
func (fakeProfile) ProviderWriteState(context.Context) (store.ProviderWriteState, error) {
	return store.ProviderWriteState{}, nil
}
func (fakeProfile) PrepareProviderWrite(
	context.Context,
	store.PrepareProviderWriteRequest,
	store.PrepareProviderWritePlanner,
) (store.PrepareProviderWriteCommit, error) {
	return store.PrepareProviderWriteCommit{}, nil
}
func (fakeProfile) ClaimProviderWriteItems(
	context.Context,
	store.ClaimProviderWriteRequest,
) ([]store.WriteItem, error) {
	return nil, nil
}
func (fakeProfile) RecordProviderWriteResult(
	context.Context,
	store.RecordProviderWriteResultRequest,
) (store.WriteBatch, error) {
	return store.WriteBatch{}, nil
}
func (fakeProfile) ParkProviderWrite(
	context.Context,
	store.ParkProviderWriteRequest,
) (store.WriteBatch, error) {
	return store.WriteBatch{}, nil
}
func (fakeProfile) ResumeProviderWrite(
	context.Context,
	store.ResumeProviderWriteRequest,
) (store.WriteBatch, error) {
	return store.WriteBatch{}, nil
}
func (fakeProfile) FinalizeProviderWrite(
	context.Context,
	store.FinalizeProviderWriteRequest,
	store.FinalizeProviderWritePlanner,
) (store.FinalizeProviderWriteCommit, error) {
	return store.FinalizeProviderWriteCommit{}, nil
}
func (fakeProfile) ReconcileProviderWrite(
	context.Context,
	store.ReconcileProviderWriteRequest,
	store.RefreshPlanner,
) (store.RefreshCommit, error) {
	return store.RefreshCommit{}, nil
}
func (fakeProfile) AcquireRefreshLease(
	context.Context,
	store.RefreshLease,
	time.Time,
) (store.RefreshLease, bool, error) {
	return store.RefreshLease{}, false, nil
}
func (fakeProfile) RenewRefreshLease(context.Context, string, time.Time, time.Time) (bool, error) {
	return false, nil
}
func (fakeProfile) ReleaseRefreshLease(context.Context, string) error { return nil }
func (fakeProfile) RecordRefreshFailure(context.Context, store.RefreshFailure) error {
	return nil
}
func (fakeProfile) ApplyProviderRefresh(
	context.Context,
	store.AtomicRefreshRequest,
	store.RefreshPlanner,
) (store.RefreshCommit, error) {
	return store.RefreshCommit{}, nil
}
func (fakeProfile) Close() error { return nil }

var _ store.Profile = fakeProfile{}

func TestFoldPlanValidatesRevisionOperationsEffectiveStateAndKnownDrills(t *testing.T) {
	t.Parallel()

	plan := validFoldPlan(t)
	require.NoError(t, plan.Validate(9))

	tests := map[string]func(*store.FoldPlan){
		"reviewed revision":  func(plan *store.FoldPlan) { plan.ReviewedRevision = 8 },
		"empty operation ID": func(plan *store.FoldPlan) { plan.ActiveOperationIDs[0] = "" },
		"duplicate operation": func(plan *store.FoldPlan) {
			plan.ActiveOperationIDs = append(plan.ActiveOperationIDs, plan.ActiveOperationIDs[0])
		},
		"invalid effective state": func(plan *store.FoldPlan) { plan.Effective.Merchants = nil },
		"unsorted drills": func(plan *store.FoldPlan) {
			plan.KnownDrills = []domain.DrillIdentity{
				{Dimension: domain.DimensionMerchant, Currency: "USD", Scale: 2, Key: "merchant_z"},
				{Dimension: domain.DimensionMerchant, Currency: "USD", Scale: 2, Key: "merchant_a"},
			}
		},
	}
	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			candidate := plan.Clone()
			mutate(&candidate)
			assert.Error(t, candidate.Validate(9))
		})
	}
}

func TestFoldPlanCloneOwnsNestedValues(t *testing.T) {
	t.Parallel()

	plan := validFoldPlan(t)
	clone := plan.Clone()
	clone.ActiveOperationIDs[0] = "changed"
	clone.Effective.Merchants[0].Label = "Changed"
	clone.KnownDrills[0].Key = "changed"

	assert.Equal(t, "operation_a", plan.ActiveOperationIDs[0])
	assert.Equal(t, "Example Merchant", plan.Effective.Merchants[0].Label)
	assert.Equal(t, "merchant_example", plan.KnownDrills[0].Key)
}

func validFoldPlan(t *testing.T) store.FoldPlan {
	t.Helper()
	date, err := domain.ParseDate("2026-08-01")
	require.NoError(t, err)
	profile := domain.CommittedProfile{
		Accounts:  []domain.Account{{ID: "account_primary", Label: "Account", CollisionKey: "account"}},
		Merchants: []domain.Merchant{{ID: "merchant_example", Label: "Example Merchant", CollisionKey: "example merchant"}},
		Groups: []domain.CategoryGroup{
			{ID: domain.UncategorizedGroupID, Label: "Uncategorized", CollisionKey: "uncategorized", Protected: true},
			{ID: "group_living", Label: "Living", CollisionKey: "living"},
		},
		Categories: []domain.Category{
			{ID: domain.UncategorizedCategoryID, GroupID: domain.UncategorizedGroupID, Label: "Uncategorized", CollisionKey: "uncategorized", Protected: true},
			{ID: "category_food", GroupID: "group_living", Label: "Food", CollisionKey: "food"},
		},
		Transactions: []domain.TransactionRecord{{
			ID: "transaction_1", Provider: "synthetic", ProviderID: "provider-1",
			AccountID: "account_primary", MerchantID: "merchant_example", CategoryID: "category_food",
			Date: date, Amount: domain.Money{Minor: -100, Currency: "USD", Scale: 2},
		}},
	}
	return store.FoldPlan{
		ReviewedRevision: 9, ActiveOperationIDs: []string{"operation_a"}, Effective: profile,
		KnownDrills: []domain.DrillIdentity{{
			Dimension: domain.DimensionMerchant, Currency: "USD", Scale: 2, Key: "merchant_example",
		}},
	}
}
