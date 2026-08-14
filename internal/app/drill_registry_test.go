package app_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/wesm/moneyflow/internal/app"
	"github.com/wesm/moneyflow/internal/domain"
)

func TestKnownDrillClassifiesPopulatedHistoricalPendingAndInvalid(t *testing.T) {
	t.Parallel()

	categoryCreate := createCategoryOperation(1)
	groupCreate := createGroupOperation(2)
	snapshot := domain.ProfileSnapshot{
		Revision: 3, Cursor: 2, Committed: replayProfile(t),
		Journal: []domain.Operation{categoryCreate, groupCreate},
	}
	effective, err := app.Replay(snapshot)
	require.NoError(t, err)
	assert.Equal(t, app.DrillPopulated, app.ClassifyKnownDrill(effective, drill(
		domain.DimensionCategory, "category_new",
	)))
	assert.Equal(t, app.DrillEmpty, app.ClassifyKnownDrill(effective, drill(
		domain.DimensionGroup, "group_new",
	)))
	assert.Equal(t, app.DrillInvalid, app.ClassifyKnownDrill(effective, drill(
		domain.DimensionCategory, "category_never",
	)))

	snapshot.Cursor = 0
	undone, err := app.Replay(snapshot)
	require.NoError(t, err)
	assert.Equal(t, app.DrillInvalid, app.ClassifyKnownDrill(undone, drill(
		domain.DimensionCategory, "category_new",
	)))
	snapshot.Journal = nil
	truncated, err := app.Replay(snapshot)
	require.NoError(t, err)
	assert.Equal(t, app.DrillInvalid, app.ClassifyKnownDrill(truncated, drill(
		domain.DimensionCategory, "category_new",
	)))
}

func TestKnownDrillKeepsRegistryAndRetiredEntityHistoryEmpty(t *testing.T) {
	t.Parallel()

	identity := drill(domain.DimensionCategory, "category_a")
	snapshot := domain.ProfileSnapshot{
		Revision: 2, Cursor: 1, Committed: replayProfile(t),
		Journal: []domain.Operation{
			mergeOperation(1, domain.OperationCategoryMerge, "category_a", "category_b"),
		},
		KnownDrills: []domain.DrillIdentity{identity},
	}
	effective, err := app.Replay(snapshot)
	require.NoError(t, err)
	assert.Equal(t, app.DrillEmpty, app.ClassifyKnownDrill(effective, identity))

	effective.KnownDrills = nil
	assert.Equal(t, app.DrillEmpty, app.ClassifyKnownDrill(effective, identity))
}

func TestKnownDrillRejectsInvalidCompleteIdentity(t *testing.T) {
	t.Parallel()

	effective, err := app.Replay(domain.ProfileSnapshot{Committed: replayProfile(t)})
	require.NoError(t, err)
	assert.Equal(t, app.DrillInvalid, app.ClassifyKnownDrill(effective, domain.DrillIdentity{
		Dimension: domain.DimensionTime, Currency: "USD", Scale: 2, Key: "2026",
	}))
	assert.Equal(t, app.DrillInvalid, app.ClassifyKnownDrill(effective, domain.DrillIdentity{
		Dimension: domain.DimensionMerchant, Currency: "usd", Scale: 2, Key: "merchant_a",
	}))
}

func drill(dimension domain.Dimension, key string) domain.DrillIdentity {
	return domain.DrillIdentity{Dimension: dimension, Currency: "USD", Scale: 2, Key: key}
}
