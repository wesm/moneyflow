package app

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/wesm/moneyflow/internal/domain"
)

func TestBreadcrumbTopLevelAndDetail(t *testing.T) {
	t.Parallel()

	start, err := domain.ParseDate("2024-01-10")
	require.NoError(t, err)
	end, err := domain.ParseDate("2025-12-31")
	require.NoError(t, err)
	dateRange := &domain.DateRange{Start: start, End: end}

	session := NewSession()
	assert.Equal(t, "Merchants (2024-01-10 to 2025-12-31)", session.Breadcrumb(dateRange))
	session.CycleGrouping()
	assert.Equal(t, "Categories (2024-01-10 to 2025-12-31)", session.Breadcrumb(dateRange))
	session.ShowAllDetail()
	assert.Equal(t, "Transactions", session.Breadcrumb(dateRange))
}

func TestBreadcrumbPreservesDrillOrderAndSubGrouping(t *testing.T) {
	t.Parallel()

	session := NewSession()
	require.NoError(t, session.Drill(domain.AggregateRow{
		Dimension: domain.DimensionMerchant, Key: "merchant", Label: "Example Grocer",
	}, ViewPosition{}))
	category := domain.DimensionCategory
	session.SubGrouping = &category
	assert.Equal(t, "M: Example Grocer > (by Category)", session.Breadcrumb(nil))
	require.NoError(t, session.Drill(domain.AggregateRow{
		Dimension: domain.DimensionCategory, Key: "category", Label: "Groceries",
	}, ViewPosition{}))
	assert.Equal(t, "M: Example Grocer > C: Groceries", session.Breadcrumb(nil))
}

func TestBreadcrumbFormatsTypedTimePeriod(t *testing.T) {
	t.Parallel()

	tests := []struct {
		period domain.Period
		want   string
	}{
		{domain.Period{Granularity: domain.TimeGranularityYear, Year: 2024}, "T: 2024"},
		{domain.Period{Granularity: domain.TimeGranularityMonth, Year: 2024, Month: 3}, "T: Mar 2024"},
		{domain.Period{Granularity: domain.TimeGranularityDay, Year: 2024, Month: 3, Day: 15}, "T: 2024-03-15"},
	}
	for _, test := range tests {
		session := NewSession()
		session.Mode = domain.ResultModeDetail
		session.Drilldowns = []domain.Drilldown{{
			Dimension: domain.DimensionTime,
			Period:    &test.period,
		}}
		assert.Equal(t, test.want, session.Breadcrumb(nil))
	}
}

func TestNavigatePeriodUsesTypedPeriodAsCanonicalState(t *testing.T) {
	t.Parallel()

	session := NewSession()
	session.Drilldowns = []domain.Drilldown{{
		Dimension: domain.DimensionTime,
		Period: &domain.Period{
			Granularity: domain.TimeGranularityMonth,
			Year:        2024,
			Month:       12,
		},
	}}
	require.True(t, session.NavigatePeriod(1))
	assert.Empty(t, session.Drilldowns[0].Key)
	assert.Empty(t, session.Drilldowns[0].Label)
	assert.Equal(t, "T: Jan 2025", session.Breadcrumb(nil))
}
