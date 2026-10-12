package analytics

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/wesm/moneyflow/internal/domain"
)

func TestCrossTabKeepsEmptyPeriodsAndSeparatesInFromOut(t *testing.T) {
	t.Parallel()
	months := monthPeriods(t, "2026-07-01", "2026-09-30")
	transactions := []domain.Transaction{
		merchantTransaction(t, "1", "2026-07-03", "-480.00", "Online Megastore"),
		merchantTransaction(t, "2", "2026-07-20", "120.00", "Online Megastore"),
		merchantTransaction(t, "3", "2026-09-02", "-50.00", "Corner Grocery"),
	}
	result, err := ComputeCrossTab(transactions, CrossTabSpec{
		Dimension: domain.DimensionMerchant,
		Frames:    []CrossTabFrameSpec{{Periods: months, Series: true}},
	})
	require.NoError(t, err)
	totals := result.Frames[0].Totals
	require.Len(t, totals, 3)
	assert.Equal(t, CrossTabCell{Count: 2, In: 12000, Out: -48000}, totals[0])
	assert.Equal(t, CrossTabCell{}, totals[1], "August has no transactions but keeps its column")
	assert.Equal(t, int64(-5000), totals[2].Net())
	megastore := result.Frames[0].Series["merchant-online-megastore"]
	assert.Equal(t, int64(-36000), megastore[0].Net())
	assert.Equal(t, "Online Megastore", result.Labels["merchant-online-megastore"])
}

func TestCrossTabAppliesEachFrameBoundsInOnePass(t *testing.T) {
	t.Parallel()
	transactions := []domain.Transaction{
		merchantTransaction(t, "1", "2026-08-15", "-10.00", "Corner Grocery"),
		merchantTransaction(t, "2", "2026-09-15", "-20.00", "Corner Grocery"),
		merchantTransaction(t, "3", "2025-01-15", "-40.00", "Corner Grocery"),
	}
	result, err := ComputeCrossTab(transactions, CrossTabSpec{
		Dimension: domain.DimensionMerchant,
		Keys:      []string{},
		Frames: []CrossTabFrameSpec{
			{Periods: monthPeriods(t, "2026-08-01", "2026-09-30"), Series: true},
			{Periods: []PeriodRange{singlePeriod(t, "2026-09-01", "2026-09-30")}},
		},
	})
	require.NoError(t, err)
	assert.Equal(t, int64(-1000), result.Frames[0].Totals[0].Net())
	assert.Equal(t, int64(-2000), result.Frames[0].Totals[1].Net())
	assert.Equal(t, int64(-2000), result.Frames[1].Totals[0].Net(), "the September frame")
	assert.Empty(t, result.Frames[0].Series, "an empty key list collects no series")
}

func TestCrossTabCountsHiddenAndPendingWithoutHiddenMoney(t *testing.T) {
	t.Parallel()
	hidden := merchantTransaction(t, "1", "2026-09-03", "-25.00", "Noodle House")
	hidden.Hidden = true
	pending := merchantTransaction(t, "2", "2026-09-04", "-10.00", "Noodle House")
	pending.Pending = true
	result, err := ComputeCrossTab([]domain.Transaction{hidden, pending}, CrossTabSpec{
		Dimension: domain.DimensionMerchant,
		Frames: []CrossTabFrameSpec{
			{Periods: []PeriodRange{singlePeriod(t, "2026-09-01", "2026-09-30")}},
		},
	})
	require.NoError(t, err)
	assert.Equal(t, CrossTabCell{Count: 2, HiddenCount: 1, PendingCount: 1, Out: -1000},
		result.Frames[0].Totals[0])
}

func TestCrossTabRequestedKeysAlwaysHaveCells(t *testing.T) {
	t.Parallel()
	transactions := []domain.Transaction{
		merchantTransaction(t, "1", "2026-09-03", "-25.00", "Noodle House"),
		merchantTransaction(t, "2", "2026-09-04", "-10.00", "Corner Grocery"),
	}
	result, err := ComputeCrossTab(transactions, CrossTabSpec{
		Dimension: domain.DimensionMerchant,
		Keys:      []string{"merchant-noodle-house", "merchant-without-activity"},
		Frames: []CrossTabFrameSpec{
			{Periods: monthPeriods(t, "2026-08-01", "2026-09-30"), Series: true},
		},
	})
	require.NoError(t, err)
	series := result.Frames[0].Series
	require.Len(t, series, 2)
	assert.Equal(t, []CrossTabCell{{}, {Count: 1, Out: -2500}}, series["merchant-noodle-house"])
	assert.Equal(t, []CrossTabCell{{}, {}}, series["merchant-without-activity"])
	assert.Equal(t, int64(-3500), result.Frames[0].Totals[1].Net(), "totals ignore the key filter")
}

func TestCrossTabGroupsByCategoryGroup(t *testing.T) {
	t.Parallel()
	transactions := []domain.Transaction{
		testTransaction(t, "1", "2026-09-03", "-25.00", "Noodle House", "Dining", "Food"),
		testTransaction(t, "2", "2026-09-04", "-75.00", "Corner Grocery", "Groceries", "Food"),
		testTransaction(t, "3", "2026-09-05", "-30.00", "City Transit", "Transit", "Transport"),
	}
	result, err := ComputeCrossTab(transactions, CrossTabSpec{
		Dimension: domain.DimensionGroup,
		Frames: []CrossTabFrameSpec{{
			Periods: []PeriodRange{singlePeriod(t, "2026-09-01", "2026-09-30")}, Series: true,
		}},
	})
	require.NoError(t, err)
	assert.Equal(t, int64(-10000), result.Frames[0].Series["group-food"][0].Net())
	assert.Equal(t, int64(-3000), result.Frames[0].Series["group-transport"][0].Net())
}

func TestCrossTabRejectsInvalidSpecs(t *testing.T) {
	t.Parallel()
	periods := monthPeriods(t, "2026-08-01", "2026-09-30")
	tooMany := make([]string, CrossTabMaxKeys+1)
	tests := map[string]CrossTabSpec{
		"time dimension": {Dimension: domain.DimensionTime},
		"too many keys":  {Dimension: domain.DimensionMerchant, Keys: tooMany},
		"periods out of order": {Dimension: domain.DimensionMerchant, Frames: []CrossTabFrameSpec{
			{Periods: []PeriodRange{periods[1], periods[0]}},
		}},
		"overlapping periods": {Dimension: domain.DimensionMerchant, Frames: []CrossTabFrameSpec{
			{Periods: []PeriodRange{
				singlePeriod(t, "2026-08-01", "2026-08-31"), singlePeriod(t, "2026-08-31", "2026-09-30"),
			}},
		}},
		"period ends before it starts": {Dimension: domain.DimensionMerchant, Frames: []CrossTabFrameSpec{
			{Periods: []PeriodRange{singlePeriod(t, "2026-09-02", "2026-09-01")}},
		}},
	}
	for name, spec := range tests {
		_, err := ComputeCrossTab(nil, spec)
		require.Error(t, err, name)
	}
	euro := merchantTransaction(t, "2", "2026-09-04", "-10.00", "Bistro du Port")
	euro.Amount.Currency = "EUR"
	_, err := ComputeCrossTab(
		[]domain.Transaction{merchantTransaction(t, "1", "2026-09-03", "-25.00", "Noodle House"), euro},
		CrossTabSpec{Dimension: domain.DimensionMerchant},
	)
	require.Error(t, err, "mixed currencies")
}

func TestCrossTabRejectsOverflow(t *testing.T) {
	t.Parallel()
	first := merchantTransaction(t, "1", "2026-09-03", "-25.00", "Noodle House")
	second := merchantTransaction(t, "2", "2026-09-04", "-10.00", "Noodle House")
	first.Amount.Minor, second.Amount.Minor = -1<<62, -1<<62
	third := second
	third.ID = "3"
	_, err := ComputeCrossTab([]domain.Transaction{first, second, third}, CrossTabSpec{
		Dimension: domain.DimensionMerchant,
		Frames: []CrossTabFrameSpec{
			{Periods: []PeriodRange{singlePeriod(t, "2026-09-01", "2026-09-30")}},
		},
	})
	require.ErrorIs(t, err, errMinorOverflow)
}

func TestPeriodIndexFindsBoundaryDaysAndSkipsGaps(t *testing.T) {
	t.Parallel()
	periods := []PeriodRange{
		singlePeriod(t, "2026-08-01", "2026-08-31"),
		singlePeriod(t, "2026-09-05", "2026-09-30"),
	}
	tests := map[string]int{
		"2026-07-31": -1, "2026-08-01": 0, "2026-08-31": 0, "2026-09-02": -1,
		"2026-09-05": 1, "2026-09-30": 1, "2026-10-01": -1,
	}
	for date, want := range tests {
		assert.Equal(t, want, periodIndex(periods, rangeDate(t, date)), date)
	}
}

func merchantTransaction(t *testing.T, id, date, amount, merchant string) domain.Transaction {
	t.Helper()
	transaction := testTransaction(t, id, date, amount, merchant, "Shopping", "Shopping")
	transaction.Merchant.ID = "merchant-" + strings.ToLower(strings.ReplaceAll(merchant, " ", "-"))
	return transaction
}

func monthPeriods(t *testing.T, start, end string) []PeriodRange {
	t.Helper()
	periods, err := enumeratePeriods(
		domain.DateRange{Start: rangeDate(t, start), End: rangeDate(t, end)},
		domain.TimeGranularityMonth, rangeDate(t, "2030-01-01"), false,
	)
	require.NoError(t, err)
	return periods
}

func singlePeriod(t *testing.T, start, end string) PeriodRange {
	t.Helper()
	return PeriodRange{Start: rangeDate(t, start), End: rangeDate(t, end)}
}
