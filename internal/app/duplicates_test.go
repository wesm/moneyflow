package app_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/wesm/moneyflow/internal/app"
	"github.com/wesm/moneyflow/internal/domain"
	"github.com/wesm/moneyflow/internal/store"
)

func TestProjectDuplicatesUsesCompleteFilteredResultAndProviderLabels(t *testing.T) {
	t.Parallel()

	profile := duplicateMemoryProfile(t, true)
	profile.advanceExternally(reassignOperation(
		1, domain.OperationCategoryAssign, "category_b", "transaction_a",
	))
	service, err := app.NewProfileService(context.Background(), profile)
	require.NoError(t, err)
	state := detailViewState()
	selection := selectedValue(
		t, effectiveSnapshotFromProfile(t, profile), state.Current, service.Revision(), "transaction_a",
	)

	projection, err := service.ProjectDuplicates(
		context.Background(), service.Revision(), state, selection,
		app.DuplicateWindowRequest{GroupLimit: 1, RowLimit: 1},
	)
	require.NoError(t, err)
	assert.Equal(t, 1, projection.TotalGroups)
	assert.Equal(t, 2, projection.TotalTransactions)
	assert.Equal(t, app.Window{Offset: 0, Limit: 1, Count: 1}, projection.GroupWindow)
	assert.Equal(t, app.Window{Offset: 0, Limit: 1, Count: 1}, projection.RowWindow)
	assert.Equal(t, 2, projection.WindowTransactions)
	require.Len(t, projection.Groups, 1)
	assert.Equal(t, 1, projection.Groups[0].Number)
	require.Len(t, projection.Groups[0].Rows, 1)
	assert.Equal(t, "Provider Merchant", projection.Groups[0].Rows[0].MatchingLabel)
	assert.Equal(t, "transaction_a", projection.Groups[0].Rows[0].Target.Identity)
	assert.True(t, projection.Groups[0].Rows[0].Flags.Selected)
	assert.True(t, projection.Groups[0].Rows[0].Flags.Pending)
	assert.Equal(t, 1, projection.SelectionCount)

	again, err := service.ProjectDuplicates(
		context.Background(), service.Revision(), state, selection,
		app.DuplicateWindowRequest{GroupLimit: 1, RowLimit: 1},
	)
	require.NoError(t, err)
	assert.Equal(t, projection, again)
}

func TestProjectDuplicatesBindsTransientSelectionToItsCheckedRevision(t *testing.T) {
	t.Parallel()

	profile := duplicateMemoryProfile(t, true)
	service, err := app.NewProfileService(context.Background(), profile)
	require.NoError(t, err)
	state := detailViewState()
	selection, err := service.ToggleSelection(
		state.Current, app.EmptySelection(), app.IdentityTransaction, "transaction_a",
	)
	require.NoError(t, err)

	projection, err := service.ProjectDuplicates(
		context.Background(), service.Revision(), state, selection,
		app.DuplicateWindowRequest{},
	)
	require.NoError(t, err)
	resolved, err := app.ResolveSelectionAtRevision(
		service, state.Current, projection.Selection, projection.Revision,
	)
	require.NoError(t, err)
	assert.Contains(t, resolved.IDs, "transaction_a")
}

func TestProjectDuplicatesRejectsSelectionBoundToAnOlderRevision(t *testing.T) {
	t.Parallel()

	profile := duplicateMemoryProfile(t, true)
	service, err := app.NewProfileService(context.Background(), profile)
	require.NoError(t, err)
	state := detailViewState()
	selection := selectedValue(
		t, effectiveSnapshotFromProfile(t, profile), state.Current, service.Revision(), "transaction_a",
	)
	profile.advanceExternally(reassignOperation(
		1, domain.OperationCategoryAssign, "category_b", "transaction_b",
	))

	_, err = service.ProjectDuplicates(
		context.Background(), service.Revision()+1, state, selection,
		app.DuplicateWindowRequest{},
	)
	assertAppCode(t, err, app.AppSelectionStale)
}

func TestProjectDuplicatesReturnsPartialGroupsAndValidatesBounds(t *testing.T) {
	t.Parallel()

	profile := duplicateMemoryProfile(t, true)
	service, err := app.NewProfileService(context.Background(), profile)
	require.NoError(t, err)
	state := detailViewState()
	projection, err := service.ProjectDuplicates(
		context.Background(), service.Revision(), state, app.EmptySelection(),
		app.DuplicateWindowRequest{GroupLimit: 1, RowOffset: 1, RowLimit: 1},
	)
	require.NoError(t, err)
	require.Len(t, projection.Groups, 1)
	require.Len(t, projection.Groups[0].Rows, 1)
	assert.Equal(t, "transaction_b", projection.Groups[0].Rows[0].Target.Identity)
	assert.Equal(t, 1, projection.Groups[0].Number)
	beyond, err := service.ProjectDuplicates(
		context.Background(), service.Revision(), state, app.EmptySelection(),
		app.DuplicateWindowRequest{GroupOffset: app.MaxWindowOffset, GroupLimit: 1, RowLimit: 1},
	)
	require.NoError(t, err)
	assert.Empty(t, beyond.Groups)
	assert.Zero(t, beyond.GroupWindow.Count)

	_, err = service.ProjectDuplicates(
		context.Background(), service.Revision()-1, state, app.EmptySelection(), app.DuplicateWindowRequest{},
	)
	assertAppCode(t, err, app.AppRevisionConflict)
	_, err = service.ProjectDuplicates(
		context.Background(), service.Revision(), state, app.EmptySelection(),
		app.DuplicateWindowRequest{GroupOffset: -1},
	)
	assertAppCode(t, err, app.AppInvalidOperation)
	_, err = service.ProjectDuplicates(
		context.Background(), service.Revision(), state, app.EmptySelection(),
		app.DuplicateWindowRequest{RowLimit: app.MaxWindowLimit + 1},
	)
	assertAppCode(t, err, app.AppInvalidOperation)
}

func TestProjectDuplicatesDerivesDetailRowsAndRejectsAggregateSelection(t *testing.T) {
	t.Parallel()

	profile := duplicateMemoryProfile(t, true)
	service, err := app.NewProfileService(context.Background(), profile)
	require.NoError(t, err)
	state := app.DefaultViewState()

	projection, err := service.ProjectDuplicates(
		context.Background(), service.Revision(), state, app.EmptySelection(),
		app.DuplicateWindowRequest{},
	)
	require.NoError(t, err)
	assert.Equal(t, 1, projection.TotalGroups)
	assert.Equal(t, 2, projection.TotalTransactions)

	result, err := service.Query(app.NewSession())
	require.NoError(t, err)
	require.NotEmpty(t, result.AggregateRows)
	aggregateSelection, err := service.ToggleSelection(
		state.Current, app.EmptySelection(), app.IdentityAggregate,
		app.AggregateIdentity(result.AggregateRows[0]),
	)
	require.NoError(t, err)
	aggregateSelection, err = app.BindSelectionRevision(aggregateSelection, service.Revision())
	require.NoError(t, err)
	_, err = service.ProjectDuplicates(
		context.Background(), service.Revision(), state, aggregateSelection,
		app.DuplicateWindowRequest{},
	)
	assertAppCode(t, err, app.AppInvalidOperation)
}

func TestProjectDuplicatesReportsNoResults(t *testing.T) {
	t.Parallel()

	profile := duplicateMemoryProfile(t, false)
	service, err := app.NewProfileService(context.Background(), profile)
	require.NoError(t, err)
	projection, err := service.ProjectDuplicates(
		context.Background(), service.Revision(), detailViewState(), app.EmptySelection(),
		app.DuplicateWindowRequest{},
	)
	require.NoError(t, err)
	assert.Zero(t, projection.TotalGroups)
	assert.Zero(t, projection.TotalTransactions)
	assert.NotEmpty(t, projection.Status)
}

type duplicateProfile struct {
	*memoryProfile
	state store.ProviderState
}

func (profile *duplicateProfile) ProviderState(context.Context) (store.ProviderState, error) {
	return profile.state.Clone(), nil
}

func duplicateMemoryProfile(t *testing.T, matching bool) *duplicateProfile {
	t.Helper()
	base := newMemoryProfile(t, 5)
	committed := replayProfile(t)
	committed.Transactions[1].Hidden = false
	committed.Transactions[1].Date = committed.Transactions[0].Date
	committed.Transactions[1].Amount = committed.Transactions[0].Amount
	committed.Merchants[0].Label = "Provider Merchant · a1"
	committed.Merchants[0].CollisionKey = "provider merchant · a1"
	committed.Merchants[1].Label = "Provider Merchant · b2"
	committed.Merchants[1].CollisionKey = "provider merchant · b2"
	if !matching {
		committed.Transactions[1].Amount.Minor--
	}
	for index := 0; index < app.DefaultWindowLimit+5; index++ {
		committed.Transactions = append(committed.Transactions, domain.TransactionRecord{
			ID:       domain.EntityID(fmt.Sprintf("transaction_unique_%03d", index)),
			Provider: "fixture", ProviderID: fmt.Sprintf("provider-unique-%03d", index),
			AccountID: "account_a", MerchantID: "merchant_a", CategoryID: "category_a",
			Date:   committed.Transactions[0].Date,
			Amount: domain.Money{Minor: int64(-10_000 - index), Currency: "USD", Scale: 2},
		})
	}
	committed.ExternalIdentities = []domain.ExternalIdentity{
		{EntityType: domain.EntityKindMerchant, EntityID: "merchant_a", Namespace: "monarch/merchant", ExternalID: "merchant-a"},
		{EntityType: domain.EntityKindMerchant, EntityID: "merchant_b", Namespace: "monarch/merchant", ExternalID: "merchant-b"},
	}
	require.NoError(t, committed.Validate())
	base.snapshot.Committed = committed
	return &duplicateProfile{
		memoryProfile: base,
		state: store.ProviderState{Allocations: []store.LabelAllocation{
			{Kind: domain.EntityKindMerchant, Namespace: "monarch/merchant", ExternalID: "merchant-a", ProviderLabel: "Provider Merchant"},
			{Kind: domain.EntityKindMerchant, Namespace: "monarch/merchant", ExternalID: "merchant-b", ProviderLabel: "Provider Merchant"},
		}},
	}
}

func effectiveSnapshotFromProfile(t *testing.T, profile *duplicateProfile) app.EffectiveSnapshot {
	t.Helper()
	snapshot, err := profile.Load(context.Background())
	require.NoError(t, err)
	effective, err := app.Replay(snapshot)
	require.NoError(t, err)
	return effective
}
