package app

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/wesm/moneyflow/internal/domain"
)

func TestNewSessionDefaults(t *testing.T) {
	t.Parallel()

	session := NewSession()
	assert.Equal(t, domain.ResultModeAggregate, session.Mode)
	assert.Equal(t, domain.DimensionMerchant, session.Dimension)
	assert.Nil(t, session.SubGrouping)
	assert.Equal(t, domain.TimeGranularityYear, session.TimeGranularity)
	assert.Equal(t, domain.SortSpec{Field: domain.SortFieldAmount, Direction: domain.SortDirectionDesc}, session.Sort)
	assert.True(t, session.ShowHidden)
	assert.False(t, session.ShowTransfers)
	assert.NotNil(t, session.SelectedTransactionIDs)
	assert.NotNil(t, session.SelectedAggregateKeys)
}

func TestSessionTopLevelTransitions(t *testing.T) {
	t.Parallel()

	session := NewSession()
	want := []domain.Dimension{
		domain.DimensionCategory,
		domain.DimensionGroup,
		domain.DimensionAccount,
		domain.DimensionTime,
		domain.DimensionMerchant,
	}
	for _, dimension := range want {
		session.CycleGrouping()
		assert.Equal(t, dimension, session.Dimension)
		if dimension == domain.DimensionTime {
			assert.Equal(t, domain.SortSpec{Field: domain.SortFieldTimePeriod, Direction: domain.SortDirectionAsc}, session.Sort)
		}
	}
	assert.Equal(t, domain.SortSpec{Field: domain.SortFieldAmount, Direction: domain.SortDirectionDesc}, session.Sort)

	session.ShowAllDetail()
	assert.Equal(t, domain.ResultModeDetail, session.Mode)
	assert.Equal(t, domain.SortSpec{Field: domain.SortFieldDate, Direction: domain.SortDirectionDesc}, session.Sort)
	position, ok := session.Back()
	assert.True(t, ok)
	assert.Equal(t, ViewPosition{}, position)
	assert.Equal(t, domain.ResultModeAggregate, session.Mode)

	session.ShowAllDetail()
	session.SwitchAccounts()
	assert.Equal(t, domain.ResultModeAggregate, session.Mode)
	assert.Equal(t, domain.DimensionAccount, session.Dimension)
	assert.Empty(t, session.Drilldowns)
}

func TestSessionTimeAndFilterOperations(t *testing.T) {
	t.Parallel()

	session := NewSession()
	session.Dimension = domain.DimensionTime
	session.ToggleTimeGranularity()
	assert.Equal(t, domain.TimeGranularityMonth, session.TimeGranularity)
	session.ToggleTimeGranularity()
	assert.Equal(t, domain.TimeGranularityDay, session.TimeGranularity)
	session.ToggleTimeGranularity()
	assert.Equal(t, domain.TimeGranularityYear, session.TimeGranularity)

	start, err := domain.ParseDate("2024-02-01")
	require.NoError(t, err)
	end, err := domain.ParseDate("2024-01-01")
	require.NoError(t, err)
	err = session.SetFilters(Filters{DateRange: &domain.DateRange{Start: start, End: end}})
	assert.Error(t, err)
	assert.Nil(t, session.DateRange)

	end, err = domain.ParseDate("2024-02-29")
	require.NoError(t, err)
	dateRange := &domain.DateRange{Start: start, End: end}
	err = session.SetFilters(Filters{
		DateRange:     dateRange,
		ShowHidden:    false,
		ShowTransfers: true,
	})
	require.NoError(t, err)
	assert.False(t, session.ShowHidden)
	assert.True(t, session.ShowTransfers)
	assert.NotSame(t, dateRange, session.DateRange)
}

func TestServiceQueriesAfterSessionTransitions(t *testing.T) {
	t.Parallel()

	service, err := NewService([]domain.Transaction{
		appTransaction(t, "txn-1", "2024-01-01", "-1.00", "Example Grocer", "Groceries", "Living"),
	})
	require.NoError(t, err)
	session := NewSession()
	for range 5 {
		result, queryErr := service.Query(session)
		require.NoError(t, queryErr)
		assert.NotNil(t, result.AggregateRows)
		session.CycleGrouping()
	}
	session.ShowAllDetail()
	result, err := service.Query(session)
	require.NoError(t, err)
	assert.NotNil(t, result.DetailRows)
}
