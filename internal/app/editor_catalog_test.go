package app_test

import (
	"context"
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
	assert.Equal(t, domain.UncategorizedCategoryID, catalog.Categories[0].ID)
	assert.True(t, catalog.Categories[0].Protected)
	for _, values := range [][]app.EditorChoice{catalog.Merchants, catalog.Categories[1:], catalog.Groups} {
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
