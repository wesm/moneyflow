package app_test

import (
	"context"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/wesm/moneyflow/internal/app"
	"github.com/wesm/moneyflow/internal/domain"
)

func TestEditorCatalogReturnsSortedActiveDetachedChoices(t *testing.T) {
	t.Parallel()
	profile := newMemoryProfile(t, 5)
	service, err := app.NewProfileService(context.Background(), profile)
	require.NoError(t, err)

	catalog, err := service.EditorCatalog()
	require.NoError(t, err)
	require.NotEmpty(t, catalog.Merchants)
	require.NotEmpty(t, catalog.Categories)
	require.NotEmpty(t, catalog.Groups)
	assert.Equal(t, domain.SplitCategoryID, catalog.Categories[0].ID)
	assert.True(t, catalog.Categories[0].Protected)
	assert.Equal(t, domain.UncategorizedCategoryID, catalog.Categories[1].ID)
	for _, values := range [][]app.EditorChoice{catalog.Merchants, catalog.Categories[2:], catalog.Groups} {
		for index := 1; index < len(values); index++ {
			assert.LessOrEqual(t, values[index-1].Label, values[index].Label)
		}
	}

	catalog.Merchants[0].Label = "changed outside service"
	again, err := service.EditorCatalog()
	require.NoError(t, err)
	assert.NotEqual(t, "changed outside service", again.Merchants[0].Label)
}

func TestEditorCatalogAtRefreshesBeforeCheckingRevision(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	profile := newMemoryProfile(t, 5)
	service, err := app.NewProfileService(ctx, profile)
	require.NoError(t, err)
	profile.advanceExternally(hideOperation(1, "transaction_a"))

	_, err = service.EditorCatalogAt(ctx, 5)
	assertAppCode(t, err, app.AppRevisionConflict)
	assert.Equal(t, uint64(6), service.Revision())

	catalog, err := service.EditorCatalogAt(ctx, 6)
	require.NoError(t, err)
	assert.NotEmpty(t, catalog.Categories)
}

func catalogCounts(t *testing.T, service *app.Service) map[domain.EntityID]int {
	t.Helper()
	catalog, err := service.EditorCatalog()
	require.NoError(t, err)
	counts := make(map[domain.EntityID]int)
	for _, choices := range [][]app.EditorChoice{
		catalog.Merchants, catalog.Categories, catalog.Groups,
	} {
		for _, choice := range choices {
			counts[choice.ID] = choice.TransactionCount
		}
	}
	return counts
}

func TestEditorCatalogCountsCommittedTransactionsPerChoice(t *testing.T) {
	t.Parallel()
	service, err := app.NewProfileService(t.Context(), newMemoryProfile(t, 5))
	require.NoError(t, err)

	counts := catalogCounts(t, service)

	// A hidden transaction still belongs to its merchant, category, and group.
	assert.Equal(t, 1, counts["merchant_a"])
	assert.Equal(t, 1, counts["merchant_b"])
	assert.Equal(t, 1, counts["category_a"])
	assert.Equal(t, 1, counts["category_b"])
	assert.Equal(t, 1, counts["group_a"])
	assert.Equal(t, 1, counts["group_b"])
	assert.Zero(t, counts[domain.UncategorizedCategoryID])
	assert.Zero(t, counts[domain.UncategorizedGroupID])
}

func TestEditorCatalogCountsIgnoreStagedEdits(t *testing.T) {
	t.Parallel()
	service, err := app.NewProfileService(t.Context(), newMemoryProfile(t, 5))
	require.NoError(t, err)
	before := catalogCounts(t, service)

	stagedMutate(t, service, app.ActionEditCategory, "transaction_a",
		app.EditInput{Scope: app.EditScopeTransactions, DestinationID: "category_b"})
	stagedMutate(t, service, app.ActionDeleteTransaction, "transaction_b", app.EditInput{})
	stagedMutate(t, service, app.ActionEditMerchant, "transaction_a",
		app.EditInput{Scope: app.EditScopeEntity, Label: "Renamed Merchant"})

	assert.Equal(t, before, catalogCounts(t, service))
	catalog, err := service.EditorCatalog()
	require.NoError(t, err)
	var labels []string
	for _, merchant := range catalog.Merchants {
		labels = append(labels, merchant.Label)
	}
	assert.Contains(t, labels, "Renamed Merchant", "the catalog still shows the staged label")
}

func TestEditorCatalogCountsDropDeletedTransactionsOnceCommitted(t *testing.T) {
	t.Parallel()
	service, err := app.NewProfileService(t.Context(), newMemoryProfile(t, 5))
	require.NoError(t, err)
	stagedMutate(t, service, app.ActionDeleteTransaction, "transaction_a", app.EditInput{})
	_, err = service.Commit(t.Context(), app.CommitRequest{
		ExpectedRevision: service.Revision(), ReviewedRevision: service.Revision(),
	})
	require.NoError(t, err)

	counts := catalogCounts(t, service)

	assert.Zero(t, counts["merchant_a"])
	assert.Zero(t, counts["category_a"])
	assert.Zero(t, counts["group_a"])
	assert.Equal(t, 1, counts["merchant_b"])
}

func TestEditorCatalogCountsSumSeveralTransactionsPerChoice(t *testing.T) {
	t.Parallel()
	profile := newMemoryProfile(t, 5)
	committed := &profile.snapshot.Committed
	committed.Categories = append(committed.Categories, domain.Category{
		ID: "category_a2", GroupID: "group_a", Label: "Category A2", CollisionKey: "category a2",
	})
	for index, categoryID := range []domain.EntityID{"category_a", "category_a2", "category_a2"} {
		extra := committed.Transactions[0]
		extra.ID = domain.EntityID("transaction_extra_" + strconv.Itoa(index))
		extra.ProviderID = "provider-extra-" + strconv.Itoa(index)
		extra.CategoryID = categoryID
		committed.Transactions = append(committed.Transactions, extra)
	}
	require.NoError(t, committed.Validate())
	service, err := app.NewProfileService(t.Context(), profile)
	require.NoError(t, err)

	counts := catalogCounts(t, service)

	assert.Equal(t, 4, counts["merchant_a"], "the original row and three extras")
	assert.Equal(t, 2, counts["category_a"])
	assert.Equal(t, 2, counts["category_a2"])
	assert.Equal(t, 4, counts["group_a"], "a group sums the transactions of its categories")
	assert.Equal(t, 1, counts["group_b"])
}
