package app

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/wesm/moneyflow/internal/domain"
)

func merchantRow() domain.AggregateRow {
	return domain.AggregateRow{
		Dimension: domain.DimensionMerchant, Key: "Example", Label: "Example",
		Total: domain.Money{Currency: "USD", Scale: 2},
	}
}

func TestToggleTimeGroupsTheTopLevelByTime(t *testing.T) {
	t.Parallel()

	session := NewSession()
	require.True(t, session.ToggleTime(ViewPosition{}))
	assert.Equal(t, domain.DimensionTime, session.Dimension)
	assert.Nil(t, session.SubGrouping)
	assert.Equal(t, domain.TimeGranularityYear, session.TimeGranularity, "the first press keeps the unit")
}

func TestToggleTimeSubGroupsADrilledView(t *testing.T) {
	t.Parallel()

	for _, subGrouped := range []bool{false, true} {
		session := NewSession()
		require.NoError(t, session.Drill(merchantRow(), ViewPosition{}))
		if subGrouped {
			require.NoError(t, session.SetSubGrouping(domain.DimensionCategory))
		}
		require.True(t, session.ToggleTime(ViewPosition{}))
		assert.Equal(t, domain.DimensionMerchant, session.Dimension)
		require.NotNil(t, session.SubGrouping)
		assert.Equal(t, domain.DimensionTime, *session.SubGrouping)
		assert.Equal(t, domain.TimeGranularityYear, session.TimeGranularity)
	}
}

func TestToggleTimeCyclesTheUnitInsideATimeView(t *testing.T) {
	t.Parallel()

	topLevel := NewSession()
	require.True(t, topLevel.ToggleTime(ViewPosition{}))
	subGrouped := NewSession()
	require.NoError(t, subGrouped.Drill(merchantRow(), ViewPosition{}))
	require.True(t, subGrouped.ToggleTime(ViewPosition{}))
	for _, session := range []*Session{&topLevel, &subGrouped} {
		before := session.QuerySpec()
		for _, want := range []domain.TimeGranularity{
			domain.TimeGranularityMonth, domain.TimeGranularityDay, domain.TimeGranularityYear,
			domain.TimeGranularityMonth,
		} {
			require.True(t, session.ToggleTime(ViewPosition{}))
			assert.Equal(t, want, session.TimeGranularity)
			assert.Equal(t, before.GroupBy, session.QuerySpec().GroupBy, "the grouping stays Time")
			assert.Equal(t, before.Drilldowns, session.QuerySpec().Drilldowns)
		}
	}
}

func TestToggleTimeLeavesATimeDrillUnchanged(t *testing.T) {
	t.Parallel()

	session := NewSession()
	require.True(t, session.ToggleTime(ViewPosition{}))
	require.NoError(t, session.Drill(domain.AggregateRow{
		Dimension: domain.DimensionTime, Key: "2026", Label: "2026",
		Period: &domain.Period{Granularity: domain.TimeGranularityYear, Year: 2026},
		Total:  domain.Money{Currency: "USD", Scale: 2},
	}, ViewPosition{}))
	require.NoError(t, session.SetSubGrouping(domain.DimensionMerchant))
	before := session.QuerySpec()
	assert.False(t, session.ToggleTime(ViewPosition{}))
	assert.Equal(t, before, session.QuerySpec())
}

func toggleWebTime(t *testing.T, service *Service, state ViewState) (ViewState, WebProjection) {
	t.Helper()
	next, _, projection, err := service.TransitionView(
		state, EmptySelection(), TransitionRequest{Action: ActionToggleTime},
		WindowRequest{Limit: MaxWindowLimit},
	)
	require.NoError(t, err)
	return next, projection
}

func TestWebToggleTimeCyclesFromTheDerivedUnit(t *testing.T) {
	t.Parallel()

	service := workedExampleService(t, "2024-01-01")
	state := timeGroupingState()
	state.Current.Dimension = domain.DimensionMerchant
	state.Current.Sort = amountDescending()

	state, projection := toggleWebTime(t, service, state)
	assert.Equal(t, domain.DimensionTime, state.Current.Dimension)
	assert.Equal(t, domain.TimeGranularityMonth, state.Current.TimeGranularity, "All derives months")
	assert.Equal(t, 34, projection.TotalRows)

	for _, want := range []struct {
		granularity domain.TimeGranularity
		rows        int
	}{
		{domain.TimeGranularityDay, 1011},
		{domain.TimeGranularityYear, 3},
		{domain.TimeGranularityMonth, 34},
	} {
		state, projection = toggleWebTime(t, service, state)
		assert.Equal(t, want.granularity, state.Current.TimeGranularity)
		assert.Equal(t, want.rows, projection.TotalRows, want.granularity)
		require.NotEmpty(t, projection.AggregateRows)
		assert.Equal(t, want.granularity, projection.AggregateRows[0].Row.Period.Granularity)
	}
}

func TestWebChosenTimeUnitLastsUntilThePeriodChanges(t *testing.T) {
	t.Parallel()

	service := workedExampleService(t, "2024-01-01")
	state, _ := setWebPeriod(t, service, timeGroupingState(),
		matrixRange(t, "2025-01-01", "2025-12-31"))
	require.Equal(t, domain.TimeGranularityMonth, state.Current.TimeGranularity)
	state, projection := toggleWebTime(t, service, state)
	assert.Equal(t, domain.TimeGranularityDay, state.Current.TimeGranularity)
	assert.Equal(t, 365, projection.TotalRows)

	sorted, _, projection, err := service.TransitionView(state, EmptySelection(),
		TransitionRequest{Action: ActionReverseSort}, WindowRequest{})
	require.NoError(t, err)
	assert.Equal(t, domain.TimeGranularityDay, sorted.Current.TimeGranularity)
	assert.Equal(t, 365, projection.TotalRows)

	next, _, projection, err := service.TransitionView(sorted, EmptySelection(),
		TransitionRequest{Action: ActionNextPeriod}, WindowRequest{})
	require.NoError(t, err)
	assert.Equal(t, domain.TimeGranularityMonth, next.Current.TimeGranularity, "2026 derives months")
	assert.Equal(t, 10, projection.TotalRows)
}

func TestWebToggleTimeInsideATimeDrillIsNoChange(t *testing.T) {
	t.Parallel()

	service := workedExampleService(t, "2024-01-01")
	state := timeGroupingState()
	state.Current.Mode = domain.ResultModeDetail
	state.Current.Drilldowns = []domain.Drilldown{{
		Dimension: domain.DimensionTime, Currency: "USD", Scale: 2,
		Period: &domain.Period{Granularity: domain.TimeGranularityYear, Year: 2026},
	}}
	state.Current.Sort = dateDescending()
	next, _, _, err := service.TransitionView(state, EmptySelection(),
		TransitionRequest{Action: ActionToggleTime}, WindowRequest{})
	assertWebCode(t, err, WebNoChange)
	assert.Equal(t, state, next)
}

func TestADateFilterForgetsTheChosenTimeUnit(t *testing.T) {
	t.Parallel()

	session := NewSession()
	session.Currency = &domain.CurrencyScope{Currency: "USD", Scale: 2}
	require.True(t, session.ToggleTime(ViewPosition{}))
	require.True(t, session.ToggleTime(ViewPosition{}))
	require.True(t, session.ViewState().Current.TimeGranularityChosen)

	require.NoError(t, session.SetFilters(Filters{ShowHidden: false}))
	assert.True(t, session.ViewState().Current.TimeGranularityChosen, "the period did not change")
	require.NoError(t, session.SetFilters(Filters{
		DateRange: matrixRange(t, "2026-01-01", "2026-12-31"),
	}))
	assert.False(t, session.ViewState().Current.TimeGranularityChosen)
}
