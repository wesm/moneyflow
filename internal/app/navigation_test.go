package app

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/wesm/moneyflow/internal/domain"
)

func TestSessionDrillAndBackRestoresPosition(t *testing.T) {
	t.Parallel()

	session := NewSession()
	row := domain.AggregateRow{
		Dimension: domain.DimensionMerchant,
		Key:       "merchant-grocer",
		Label:     "Example Grocer",
	}
	require.NoError(t, session.Drill(row, ViewPosition{Cursor: 3, Scroll: 7}))
	assert.Equal(t, domain.ResultModeDetail, session.Mode)
	require.Len(t, session.Drilldowns, 1)
	assert.Equal(t, "Example Grocer", session.Drilldowns[0].Label)
	assert.Equal(t, domain.SortFieldAmount, session.Sort.Field)

	position, ok := session.Back()
	assert.True(t, ok)
	assert.Equal(t, ViewPosition{Cursor: 3, Scroll: 7}, position)
	assert.Equal(t, domain.ResultModeAggregate, session.Mode)
	assert.Empty(t, session.Drilldowns)
}

func TestSessionMultiLevelAndSubGroupingBack(t *testing.T) {
	t.Parallel()

	session := NewSession()
	require.NoError(t, session.Drill(domain.AggregateRow{
		Dimension: domain.DimensionMerchant, Key: "merchant", Label: "Example Grocer",
	}, ViewPosition{Cursor: 1, Scroll: 2}))

	session.CycleSubGrouping()
	require.NotNil(t, session.SubGrouping)
	assert.Equal(t, domain.DimensionCategory, *session.SubGrouping)
	position, ok := session.Back()
	assert.True(t, ok)
	assert.Equal(t, ViewPosition{}, position)
	assert.Nil(t, session.SubGrouping)
	assert.Equal(t, domain.ResultModeDetail, session.Mode)

	session.CycleSubGrouping()
	require.NoError(t, session.Drill(domain.AggregateRow{
		Dimension: domain.DimensionCategory, Key: "category", Label: "Groceries",
	}, ViewPosition{Cursor: 4, Scroll: 9}))
	assert.Equal(t, []domain.Dimension{domain.DimensionMerchant, domain.DimensionCategory}, drillDimensions(session.Drilldowns))
	position, ok = session.Back()
	assert.True(t, ok)
	assert.Equal(t, ViewPosition{Cursor: 4, Scroll: 9}, position)
	require.NotNil(t, session.SubGrouping)
	assert.Equal(t, domain.DimensionCategory, *session.SubGrouping)
}

func TestSessionSearchBackPriority(t *testing.T) {
	t.Parallel()

	session := NewSession()
	session.SetSearch("grocer")
	position, ok := session.Back()
	assert.True(t, ok)
	assert.Equal(t, ViewPosition{}, position)
	assert.Empty(t, session.Search)

	session.SetSearch("grocer")
	require.NoError(t, session.Drill(domain.AggregateRow{
		Dimension: domain.DimensionMerchant, Key: "merchant", Label: "Example Grocer",
	}, ViewPosition{Cursor: 2}))
	_, ok = session.Back()
	assert.True(t, ok)
	assert.Equal(t, "grocer", session.Search)
	_, ok = session.Back()
	assert.True(t, ok)
	assert.Empty(t, session.Search)
}

func TestSessionTimeDrillNavigateAndClear(t *testing.T) {
	t.Parallel()

	session := NewSession()
	session.Dimension = domain.DimensionTime
	session.TimeGranularity = domain.TimeGranularityMonth
	period := domain.Period{Granularity: domain.TimeGranularityMonth, Year: 2024, Month: 12}
	require.NoError(t, session.Drill(domain.AggregateRow{
		Dimension: domain.DimensionTime, Key: "2024-12", Label: "Dec 2024", Period: &period,
	}, ViewPosition{}))
	assert.Equal(t, domain.SortSpec{Field: domain.SortFieldAmount, Direction: domain.SortDirectionDesc}, session.Sort)
	assert.True(t, session.NavigatePeriod(1))
	require.NotNil(t, session.Drilldowns[0].Period)
	assert.Equal(t, 2025, session.Drilldowns[0].Period.Year)
	assert.Equal(t, 1, session.Drilldowns[0].Period.Month)
	assert.True(t, session.ClearTimePeriod())
	assert.False(t, session.ClearTimePeriod())
}

func drillDimensions(drilldowns []domain.Drilldown) []domain.Dimension {
	result := make([]domain.Dimension, len(drilldowns))
	for index, drilldown := range drilldowns {
		result[index] = drilldown.Dimension
	}
	return result
}
