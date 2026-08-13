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

func TestCycleGroupingFromAllDetailRestoresAndConsumesParent(t *testing.T) {
	t.Parallel()

	session := NewSession()
	session.Dimension = domain.DimensionCategory
	session.ShowAllDetail()
	session.CycleGrouping()
	assert.Equal(t, domain.ResultModeAggregate, session.Mode)
	assert.Equal(t, domain.DimensionCategory, session.Dimension)
	_, ok := session.Back()
	assert.False(t, ok)
}

func TestSearchAnchorSurvivesNavigationSnapshot(t *testing.T) {
	t.Parallel()

	session := NewSession()
	session.SetSearch("original")
	require.NoError(t, session.Drill(domain.AggregateRow{
		Dimension: domain.DimensionMerchant,
		Key:       "merchant-1",
		Label:     "Example Merchant",
	}, ViewPosition{}))
	session.SetSearch("nested")
	_, ok := session.Back()
	require.True(t, ok)
	assert.Empty(t, session.Search)
	_, ok = session.Back()
	require.True(t, ok)
	assert.Equal(t, "original", session.Search)
	_, ok = session.Back()
	require.True(t, ok)
	assert.Empty(t, session.Search)
}

func TestSessionTopLevelGroupingCarriesNameSort(t *testing.T) {
	t.Parallel()

	session := NewSession()
	session.Sort = domain.SortSpec{
		Field: domain.SortFieldMerchant, Direction: domain.SortDirectionAsc,
	}
	for _, want := range []domain.SortField{
		domain.SortFieldCategory,
		domain.SortFieldGroup,
		domain.SortFieldAccount,
		domain.SortFieldTimePeriod,
		domain.SortFieldAmount,
	} {
		session.CycleGrouping()
		assert.Equal(t, want, session.Sort.Field)
	}
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

func TestSessionSortCyclesAndReverse(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		session   Session
		wantCycle []domain.SortField
	}{
		{
			name: "detail",
			session: func() Session {
				session := NewSession()
				session.ShowAllDetail()
				return session
			}(),
			wantCycle: []domain.SortField{
				domain.SortFieldMerchant, domain.SortFieldCategory, domain.SortFieldAccount,
				domain.SortFieldAmount, domain.SortFieldDate,
			},
		},
		{
			name: "time",
			session: func() Session {
				session := NewSession()
				session.Dimension = domain.DimensionTime
				session.Sort = domain.SortSpec{
					Field: domain.SortFieldTimePeriod, Direction: domain.SortDirectionAsc,
				}
				return session
			}(),
			wantCycle: []domain.SortField{
				domain.SortFieldCount, domain.SortFieldAmount, domain.SortFieldTimePeriod,
			},
		},
		{
			name: "merchant",
			session: func() Session {
				session := NewSession()
				session.Sort = domain.SortSpec{
					Field: domain.SortFieldMerchant, Direction: domain.SortDirectionAsc,
				}
				return session
			}(),
			wantCycle: []domain.SortField{
				domain.SortFieldCount, domain.SortFieldAmount, domain.SortFieldMerchant,
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			for _, field := range test.wantCycle {
				test.session.CycleSort()
				assert.Equal(t, field, test.session.Sort.Field)
			}
			before := test.session.Sort.Direction
			test.session.ReverseSort()
			assert.NotEqual(t, before, test.session.Sort.Direction)
			test.session.ReverseSort()
			assert.Equal(t, before, test.session.Sort.Direction)
		})
	}
}

func TestSessionSelectionSetsAreIndependent(t *testing.T) {
	t.Parallel()

	session := NewSession()
	session.ToggleAggregateSelection("merchant-a")
	session.ToggleTransactionSelection("txn-1")
	assert.Contains(t, session.SelectedAggregateKeys, "merchant-a")
	assert.Contains(t, session.SelectedTransactionIDs, "txn-1")

	session.ToggleAggregateSelection("merchant-a")
	session.ToggleTransactionSelection("txn-1")
	assert.Empty(t, session.SelectedAggregateKeys)
	assert.Empty(t, session.SelectedTransactionIDs)
}

func TestSessionSelectAllVisibleOnly(t *testing.T) {
	t.Parallel()

	session := NewSession()
	aggregate := domain.QueryResult{AggregateRows: []domain.AggregateRow{{Key: "a"}, {Key: "b"}}}
	session.ToggleSelectAll(aggregate)
	assert.Equal(t, map[string]struct{}{
		AggregateIdentity(aggregate.AggregateRows[0]): {},
		AggregateIdentity(aggregate.AggregateRows[1]): {},
	}, session.SelectedAggregateKeys)
	session.SelectedAggregateKeys["not-visible"] = struct{}{}
	session.ToggleSelectAll(aggregate)
	assert.Equal(t, map[string]struct{}{"not-visible": {}}, session.SelectedAggregateKeys)

	detail := domain.QueryResult{DetailRows: []domain.DetailRow{
		{Transaction: domain.Transaction{ID: "txn-1"}},
		{Transaction: domain.Transaction{ID: "txn-2"}},
	}}
	session.ToggleSelectAll(detail)
	assert.Len(t, session.SelectedTransactionIDs, 2)
	session.ToggleSelectAll(detail)
	assert.Empty(t, session.SelectedTransactionIDs)
}
