package analytics

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/wesm/moneyflow/internal/domain"
)

type rangeCase struct {
	name         string
	scope        [2]string // empty selects All
	today        string
	first        string // empty means the currency has no transactions
	latest       string
	timeGrouping bool

	kind               ScopeKind
	contextGranularity domain.TimeGranularity
	context            [2]string // first and last period keys; empty when there are none
	contextCount       int
	contextLastPartial bool
	trend              [2]string
	trendLastPartial   bool
	trendAbsent        int
	comparison         [2]string
	comparisonLabel    string
}

func TestResolveRangesWorkedExamples(t *testing.T) {
	t.Parallel()
	// The spec's worked examples: today is 2026-10-08, Latest is 2026-10-07, and the first
	// transaction is 2024-01-01.
	tests := []rangeCase{
		{
			name: "month", scope: [2]string{"2026-09-01", "2026-09-30"},
			kind: ScopeMonth, contextGranularity: domain.TimeGranularityMonth,
			context: [2]string{"2025-09", "2026-09"}, contextCount: 13,
			trend:      [2]string{"2025-10", "2026-09"},
			comparison: [2]string{"2026-08-01", "2026-08-31"}, comparisonLabel: "vs Aug",
		},
		{
			name: "partial month", scope: [2]string{"2026-10-01", "2026-10-31"},
			kind: ScopeMonth, contextGranularity: domain.TimeGranularityMonth,
			context: [2]string{"2025-10", "2026-10"}, contextCount: 13, contextLastPartial: true,
			trend: [2]string{"2025-11", "2026-10"}, trendLastPartial: true,
			comparison: [2]string{"2026-09-01", "2026-09-08"}, comparisonLabel: "vs Sep 1–8",
		},
		{
			name: "year", scope: [2]string{"2025-01-01", "2025-12-31"},
			kind: ScopeYear, contextGranularity: domain.TimeGranularityMonth,
			context: [2]string{"2025-01", "2025-12"}, contextCount: 12,
			trend:      [2]string{"2025-01", "2025-12"},
			comparison: [2]string{"2024-01-01", "2024-12-31"}, comparisonLabel: "vs 2024",
		},
		{
			name: "partial year", scope: [2]string{"2026-01-01", "2026-12-31"},
			kind: ScopeYear, contextGranularity: domain.TimeGranularityMonth,
			context: [2]string{"2026-01", "2026-10"}, contextCount: 10, contextLastPartial: true,
			trend: [2]string{"2025-11", "2026-10"}, trendLastPartial: true,
			comparison:      [2]string{"2025-01-01", "2025-10-08"},
			comparisonLabel: "vs 2025 to date",
		},
		{
			name: "day", scope: [2]string{"2026-09-15", "2026-09-15"},
			kind: ScopeDay, contextGranularity: domain.TimeGranularityDay,
			context: [2]string{"2026-09-01", "2026-09-30"}, contextCount: 30,
			trend:      [2]string{"2025-10", "2026-09"},
			comparison: [2]string{"2026-09-14", "2026-09-14"}, comparisonLabel: "vs Sep 14",
		},
		{
			name: "custom days", scope: [2]string{"2026-03-10", "2026-04-09"},
			kind: ScopeCustom, contextGranularity: domain.TimeGranularityDay,
			context: [2]string{"2026-03-10", "2026-04-09"}, contextCount: 31,
			trend:           [2]string{"2025-05", "2026-04"},
			comparison:      [2]string{"2026-02-07", "2026-03-09"},
			comparisonLabel: "vs Feb 7 – Mar 9",
		},
		{
			name: "all", kind: ScopeAll, contextGranularity: domain.TimeGranularityMonth,
			context: [2]string{"2024-01", "2026-10"}, contextCount: 34, contextLastPartial: true,
			trend: [2]string{"2025-11", "2026-10"}, trendLastPartial: true,
		},
		{
			name: "time grouping month", scope: [2]string{"2026-09-01", "2026-09-30"},
			timeGrouping: true, kind: ScopeMonth, contextGranularity: domain.TimeGranularityDay,
			context: [2]string{"2026-09-01", "2026-09-30"}, contextCount: 30,
			comparison: [2]string{"2026-08-01", "2026-08-31"}, comparisonLabel: "vs Aug",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			test.today, test.first, test.latest = "2026-10-08", "2024-01-01", "2026-10-07"
			assertRanges(t, test)
		})
	}
}

func TestResolveRangesEdges(t *testing.T) {
	t.Parallel()
	tests := []rangeCase{
		{
			name: "time rows stop at today", scope: [2]string{"2026-10-01", "2026-10-31"},
			today: "2026-10-08", first: "2024-01-01", latest: "2026-10-07", timeGrouping: true,
			kind: ScopeMonth, contextGranularity: domain.TimeGranularityDay,
			context: [2]string{"2026-10-01", "2026-10-08"}, contextCount: 8,
			comparison: [2]string{"2026-09-01", "2026-09-08"}, comparisonLabel: "vs Sep 1–8",
		},
		{
			name: "leap day year to date", scope: [2]string{"2028-01-01", "2028-12-31"},
			today: "2028-02-29", first: "2024-01-01", latest: "2028-02-28",
			kind: ScopeYear, contextGranularity: domain.TimeGranularityMonth,
			context: [2]string{"2028-01", "2028-02"}, contextCount: 2, contextLastPartial: false,
			trend:           [2]string{"2027-03", "2028-02"},
			comparison:      [2]string{"2027-01-01", "2027-02-28"},
			comparisonLabel: "vs 2027 to date",
		},
		{
			name: "partial month clamps to a shorter month", scope: [2]string{"2026-03-01", "2026-03-31"},
			today: "2026-03-30", first: "2024-01-01", latest: "2026-03-29",
			kind: ScopeMonth, contextGranularity: domain.TimeGranularityMonth,
			context: [2]string{"2025-03", "2026-03"}, contextCount: 13, contextLastPartial: true,
			trend: [2]string{"2025-04", "2026-03"}, trendLastPartial: true,
			comparison: [2]string{"2026-02-01", "2026-02-28"}, comparisonLabel: "vs Feb 1–28",
		},
		{
			name: "partial month on its first day", scope: [2]string{"2026-10-01", "2026-10-31"},
			today: "2026-10-01", first: "2024-01-01", latest: "2026-09-30",
			kind: ScopeMonth, contextGranularity: domain.TimeGranularityMonth,
			context: [2]string{"2025-10", "2026-10"}, contextCount: 13, contextLastPartial: true,
			trend: [2]string{"2025-11", "2026-10"}, trendLastPartial: true,
			comparison: [2]string{"2026-09-01", "2026-09-01"}, comparisonLabel: "vs Sep 1",
		},
		{
			name: "custom months", scope: [2]string{"2026-01-01", "2026-03-31"},
			today: "2026-10-08", first: "2024-01-01", latest: "2026-10-07",
			kind: ScopeCustom, contextGranularity: domain.TimeGranularityMonth,
			context: [2]string{"2026-01", "2026-03"}, contextCount: 3,
			trend:           [2]string{"2025-04", "2026-03"},
			comparison:      [2]string{"2025-10-03", "2025-12-31"},
			comparisonLabel: "vs Oct 3 – Dec 31",
		},
		{
			name: "custom years", scope: [2]string{"2020-01-15", "2024-06-30"},
			today: "2026-10-08", first: "2019-01-01", latest: "2026-10-07",
			kind: ScopeCustom, contextGranularity: domain.TimeGranularityYear,
			context: [2]string{"2020", "2024"}, contextCount: 5, contextLastPartial: true,
			trend:           [2]string{"2023-07", "2024-06"},
			comparison:      [2]string{"2015-07-31", "2020-01-14"},
			comparisonLabel: "vs Jul 31, 2015 – Jan 14, 2020",
		},
		{
			name: "custom across a year boundary", scope: [2]string{"2026-01-05", "2026-01-10"},
			today: "2026-10-08", first: "2024-01-01", latest: "2026-10-07",
			kind: ScopeCustom, contextGranularity: domain.TimeGranularityDay,
			context: [2]string{"2026-01-05", "2026-01-10"}, contextCount: 6,
			trend:           [2]string{"2025-02", "2026-01"},
			comparison:      [2]string{"2025-12-30", "2026-01-04"},
			comparisonLabel: "vs Dec 30, 2025 – Jan 4, 2026",
		},
		{
			name:  "all beyond sixty months uses years",
			today: "2026-10-08", first: "2019-01-01", latest: "2026-10-07",
			kind: ScopeAll, contextGranularity: domain.TimeGranularityYear,
			context: [2]string{"2019", "2026"}, contextCount: 8, contextLastPartial: true,
			trend: [2]string{"2025-11", "2026-10"}, trendLastPartial: true,
		},
		{
			name: "all without data", today: "2026-10-08",
			kind: ScopeAll, contextGranularity: domain.TimeGranularityMonth,
		},
		{
			name:  "trend months before the first transaction are absent",
			scope: [2]string{"2026-09-01", "2026-09-30"},
			today: "2026-10-08", first: "2026-03-15", latest: "2026-10-07",
			kind: ScopeMonth, contextGranularity: domain.TimeGranularityMonth,
			context: [2]string{"2025-09", "2026-09"}, contextCount: 13,
			trend: [2]string{"2025-10", "2026-09"}, trendAbsent: 5,
			comparison: [2]string{"2026-08-01", "2026-08-31"}, comparisonLabel: "vs Aug",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			assertRanges(t, test)
		})
	}
}

func TestResolveRangesHeatmap(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		scope [2]string
		want  Heatmap
	}{
		{name: "month", scope: [2]string{"2026-09-01", "2026-09-30"},
			want: Heatmap{Layout: HeatmapMonths, Start: rangeDate(t, "2026-09-01"), End: rangeDate(t, "2026-09-30")}},
		{name: "day", scope: [2]string{"2026-09-15", "2026-09-15"},
			want: Heatmap{Layout: HeatmapMonths, Start: rangeDate(t, "2026-09-01"), End: rangeDate(t, "2026-09-30")}},
		{name: "year", scope: [2]string{"2025-01-01", "2025-12-31"},
			want: Heatmap{Layout: HeatmapWeeks, Start: rangeDate(t, "2025-01-01"), End: rangeDate(t, "2025-12-31")}},
		{name: "short custom", scope: [2]string{"2026-03-10", "2026-04-09"},
			want: Heatmap{Layout: HeatmapMonths, Start: rangeDate(t, "2026-03-01"), End: rangeDate(t, "2026-04-30")}},
		{name: "long custom", scope: [2]string{"2026-01-01", "2026-03-31"},
			want: Heatmap{Layout: HeatmapWeeks, Start: rangeDate(t, "2025-03-26"), End: rangeDate(t, "2026-03-31")}},
		{name: "all",
			want: Heatmap{Layout: HeatmapWeeks, Start: rangeDate(t, "2025-10-02"), End: rangeDate(t, "2026-10-07")}},
	}
	for _, test := range tests {
		ranges, err := ResolveRanges(RangeInput{
			Scope: rangeScope(t, test.scope), Today: rangeDate(t, "2026-10-08"),
			First: rangeDate(t, "2024-01-01"), Latest: rangeDate(t, "2026-10-07"),
		})
		require.NoError(t, err, test.name)
		require.NotNil(t, ranges.Heatmap, test.name)
		assert.Equal(t, test.want, *ranges.Heatmap, test.name)
	}
	ranges, err := ResolveRanges(RangeInput{Today: rangeDate(t, "2026-10-08")})
	require.NoError(t, err)
	assert.Nil(t, ranges.Heatmap, "All without data has no heatmap")
}

func TestResolveRangesClipsCustomEdgePeriods(t *testing.T) {
	t.Parallel()
	ranges, err := ResolveRanges(RangeInput{
		Scope: rangeScope(t, [2]string{"2026-01-15", "2026-05-10"}), Today: rangeDate(t, "2026-10-08"),
		First: rangeDate(t, "2024-01-01"), Latest: rangeDate(t, "2026-10-07"),
	})
	require.NoError(t, err)
	require.Len(t, ranges.Context, 5)
	first, last := ranges.Context[0], ranges.Context[4]
	assert.Equal(t, "2026-01-15", first.Start.String())
	assert.Equal(t, "2026-01-31", first.End.String())
	assert.True(t, first.Partial)
	assert.False(t, ranges.Context[1].Partial)
	assert.Equal(t, "2026-05-01", last.Start.String())
	assert.Equal(t, "2026-05-10", last.End.String())
	assert.True(t, last.Partial)
}

func TestResolveRangesRejectsInvertedScope(t *testing.T) {
	t.Parallel()
	_, err := ResolveRanges(RangeInput{
		Scope: &domain.DateRange{Start: rangeDate(t, "2026-02-01"), End: rangeDate(t, "2026-01-01")},
		Today: rangeDate(t, "2026-10-08"),
	})
	require.Error(t, err)
}

func assertRanges(t *testing.T, test rangeCase) {
	t.Helper()
	input := RangeInput{
		Scope: rangeScope(t, test.scope), Today: rangeDate(t, test.today),
		TimeGrouping: test.timeGrouping,
	}
	if test.first != "" {
		input.First, input.Latest = rangeDate(t, test.first), rangeDate(t, test.latest)
	}
	ranges, err := ResolveRanges(input)
	require.NoError(t, err)
	assert.Equal(t, test.kind, ranges.Kind)
	assert.Equal(t, test.contextGranularity, ranges.ContextGranularity)
	assert.Equal(t, test.context, firstLastKeys(ranges.Context), "context")
	assert.Len(t, ranges.Context, test.contextCount, "context count")
	if len(ranges.Context) > 0 {
		assert.Equal(t, test.contextLastPartial, ranges.Context[len(ranges.Context)-1].Partial,
			"context last partial")
	}
	assert.Equal(t, test.trend, firstLastKeys(ranges.Trend), "trend")
	if test.trend[0] != "" {
		require.Len(t, ranges.Trend, 12)
		assert.Equal(t, test.trendLastPartial, ranges.Trend[11].Partial, "trend last partial")
	}
	absent := 0
	for _, period := range ranges.Trend {
		if period.Absent {
			absent++
		}
	}
	assert.Equal(t, test.trendAbsent, absent, "absent trend months")
	var comparison [2]string
	if ranges.Comparison != nil {
		comparison = [2]string{ranges.Comparison.Start.String(), ranges.Comparison.End.String()}
	}
	assert.Equal(t, test.comparison, comparison, "comparison")
	assert.Equal(t, test.comparisonLabel, ranges.ComparisonLabel)
}

func firstLastKeys(periods []PeriodRange) [2]string {
	if len(periods) == 0 {
		return [2]string{}
	}
	return [2]string{periodKey(periods[0].Period), periodKey(periods[len(periods)-1].Period)}
}

func rangeScope(t *testing.T, scope [2]string) *domain.DateRange {
	t.Helper()
	if scope[0] == "" {
		return nil
	}
	return &domain.DateRange{Start: rangeDate(t, scope[0]), End: rangeDate(t, scope[1])}
}
