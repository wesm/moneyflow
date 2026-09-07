package app_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wesm/moneyflow/internal/app"
	"github.com/wesm/moneyflow/internal/domain"
	"github.com/wesm/moneyflow/internal/provider"
	"github.com/wesm/moneyflow/internal/store"
)

func TestYNABMixedTransferSelectionCannotStageAnyTargets(t *testing.T) {
	ctx := context.Background()
	service, profile := newProviderRefreshService(t)
	now := providerWriteTime()
	snapshot := providerSnapshot(t, now, 2)
	snapshot.WriteRestrictions = []domain.ImportWriteRestriction{{Kind: domain.EntityKindTransaction, ExternalID: snapshot.Transactions[1].ExternalID, Reason: "transfer"}}
	source := &fakeProviderSource{identity: provider.ProfileIdentity{Kind: "ynab", RemoteID: "plan-example"}, snapshot: snapshot, fingerprint: "vault-a"}
	require.NoError(t, service.ConfigureProvider(app.ProviderRuntime{ReadSource: source, Provider: "ynab", Currency: "USD", Scale: 2, Renderer: "tui", InstanceID: "ynab-policy", Now: func() time.Time { return now }}))
	_, err := service.RefreshProvider(ctx, app.ProviderRefreshRequest{Manual: true, State: app.DefaultViewState(), Selection: app.EmptySelection()})
	require.NoError(t, err)
	before, err := profile.Load(ctx)
	require.NoError(t, err)
	selection, err := app.NewExplicitTransactionSelection([]domain.EntityID{before.Committed.Transactions[0].ID, before.Committed.Transactions[1].ID}, before.Revision)
	require.NoError(t, err)
	state := app.DefaultViewState()
	state.Current.Mode = domain.ResultModeDetail
	_, err = service.Mutate(ctx, app.MutationRequest{Action: app.ActionEditCategory, ExpectedRevision: before.Revision, State: state, Selection: selection, Input: app.EditInput{Scope: app.EditScopeTransactions, DestinationID: domain.UncategorizedCategoryID}})
	var failure *app.AppError
	require.ErrorAs(t, err, &failure)
	assert.Equal(t, app.AppProviderWriteUnsupported, failure.Code)
	after, err := profile.Load(ctx)
	require.NoError(t, err)
	assert.Equal(t, before, after)
}

func ynabWriteInputs(t *testing.T, operations ...domain.Operation) store.PrepareProviderWriteInputs {
	t.Helper()
	snapshot := providerWriteProfile(t)
	state := providerWriteState()
	state.Binding.Kind, state.Binding.Namespace = "ynab", "ynab"
	for index := range snapshot.Committed.ExternalIdentities {
		snapshot.Committed.ExternalIdentities[index].Namespace = strings.Replace(snapshot.Committed.ExternalIdentities[index].Namespace, "monarch/", "ynab/", 1)
	}
	for index := range snapshot.Committed.Transactions {
		snapshot.Committed.Transactions[index].Provider = "ynab"
	}
	for index := range state.Allocations {
		state.Allocations[index].Namespace = strings.Replace(state.Allocations[index].Namespace, "monarch/", "ynab/", 1)
	}
	snapshot.Journal, snapshot.Cursor = operations, len(operations)
	return store.PrepareProviderWriteInputs{Snapshot: snapshot, ProviderState: state, ProposedBatchID: "batch-ynab", ObservedAt: providerWriteTime()}
}

func TestYNABWritePlanClearAndIDAddressedCollision(t *testing.T) {
	categoryClear := providerWriteOperation("clear", 1, domain.OperationCategoryAssign, []domain.EntityID{"transaction_a"}, nil, nil, &domain.ReassignPayload{DestinationID: domain.UncategorizedCategoryID}, nil)
	inputs := ynabWriteInputs(t, categoryClear)
	inputs.ProposedItemIDs = []string{"item-clear"}
	plan, err := app.BuildProviderWritePlan(inputs)
	require.NoError(t, err)
	require.Len(t, plan.Items, 1)
	assert.True(t, plan.Items[0].ClearCategory)
	assert.Nil(t, plan.Items[0].RequestedCategoryExternalID)
	require.NoError(t, plan.Items[0].Validate())

	move := providerWriteOperation("move", 1, domain.OperationMerchantReassign, []domain.EntityID{"transaction_a"}, nil, nil, &domain.ReassignPayload{DestinationID: "merchant_b"}, nil)
	inputs = ynabWriteInputs(t, move)
	inputs.ProposedItemIDs = []string{"item-move"}
	inputs.ProviderState.Allocations[1].ProviderLabel = inputs.ProviderState.Allocations[0].ProviderLabel
	inputs.ProviderState.Allocations[1].BaseCollisionKey = inputs.ProviderState.Allocations[0].BaseCollisionKey
	plan, err = app.BuildProviderWritePlan(inputs)
	require.NoError(t, err)
	assert.Equal(t, "merchant-provider-b", plan.Items[0].ExpectedMerchantExternalID)
}

func TestYNABWritePlanRevalidatesRetainedRestrictions(t *testing.T) {
	label := providerWriteOperation("label", 1, domain.OperationMerchantLabel, []domain.EntityID{"merchant_a"}, &domain.LabelPayload{EntityID: "merchant_a", Label: "New Payee", CollisionKey: "new payee"}, nil, nil, nil)
	inputs := ynabWriteInputs(t, label)
	inputs.ProviderState.WriteRestrictions = []store.ProviderWriteRestriction{{Kind: domain.EntityKindTransaction, EntityID: "transaction_b", Reason: "transfer"}}
	_, err := app.CountProviderWriteItems(inputs)
	require.Error(t, err, "one transfer refuses a whole structural sweep")
	_, err = app.BuildProviderWritePlan(inputs)
	require.Error(t, err)

	categoryClear := providerWriteOperation("clear", 1, domain.OperationCategoryAssign, []domain.EntityID{"transaction_a"}, nil, nil, &domain.ReassignPayload{DestinationID: domain.UncategorizedCategoryID}, nil)
	inputs = ynabWriteInputs(t, categoryClear)
	inputs.Snapshot.Committed.Transactions[0].CategoryID = domain.SplitCategoryID
	_, err = app.BuildProviderWritePlan(inputs)
	require.Error(t, err)

	hide := providerWriteOperation("hide", 1, domain.OperationTransactionHide, []domain.EntityID{"transaction_a"}, nil, nil, nil, &domain.HideTogglePayload{})
	_, err = app.BuildProviderWritePlan(ynabWriteInputs(t, hide))
	require.Error(t, err)

	remove := providerWriteOperation("remove", 2, domain.OperationTransactionDelete, []domain.EntityID{"transaction_a", "transaction_b"}, nil, nil, nil, nil)
	remove.TransactionDelete = &domain.TransactionDeletePayload{}
	inputs = ynabWriteInputs(t, label, remove)
	count, err := app.CountProviderWriteItems(inputs)
	require.NoError(t, err)
	assert.Equal(t, 2, count, "vacuous label adds no update items")
}
