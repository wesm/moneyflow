package analytics

import (
	"errors"
	"fmt"
	"time"

	"github.com/wesm/moneyflow/internal/domain"
)

// ScopeKind classifies the period a user chose.
type ScopeKind string

// Supported scope kinds.
const (
	ScopeDay    ScopeKind = "day"
	ScopeMonth  ScopeKind = "month"
	ScopeYear   ScopeKind = "year"
	ScopeCustom ScopeKind = "custom"
	ScopeAll    ScopeKind = "all"
)

// HeatmapLayout selects calendar month grids or a 53-week strip.
type HeatmapLayout string

// Supported heatmap layouts.
const (
	HeatmapMonths HeatmapLayout = "months"
	HeatmapWeeks  HeatmapLayout = "weeks"
)

const (
	heatmapWeekDays      = 53 * 7
	trendMonths          = 12
	monthContextMonths   = 13
	customDayLimit       = 62
	customMonthLimit     = 36
	allTimeMonthLimit    = 60
	comparisonDayLayout  = "Jan 2"
	comparisonYearLayout = "Jan 2, 2006"
)

// PeriodRange is one chart period and the dates it covers.
type PeriodRange struct {
	Period domain.Period
	Start  domain.Date
	End    domain.Date
	// Partial marks a period that contains today before its last day, or a custom-range edge
	// period that the range covers only in part.
	Partial bool
	// Absent marks a trend month that ends before the currency's first transaction.
	Absent bool
}

// Heatmap is the day range a calendar heatmap draws.
type Heatmap struct {
	Layout HeatmapLayout
	Start  domain.Date
	End    domain.Date
}

// RangeInput holds everything that range resolution depends on.
type RangeInput struct {
	Scope        *domain.DateRange // nil selects All
	Today        domain.Date
	First        domain.Date // first committed transaction in the currency; zero when none
	Latest       domain.Date // newest committed transaction in the currency; zero when none
	TimeGrouping bool
}

// Ranges are the scope, chart context, trend, and comparison ranges of one view.
type Ranges struct {
	Kind               ScopeKind
	Scope              *domain.DateRange
	ContextGranularity domain.TimeGranularity
	Context            []PeriodRange
	Trend              []PeriodRange
	Comparison         *domain.DateRange
	ComparisonLabel    string
	Heatmap            *Heatmap
}

// ClassifyScope reports whether a date range is one day, one calendar month, one calendar
// year, another inclusive range, or All.
func ClassifyScope(scope *domain.DateRange) ScopeKind {
	switch {
	case scope == nil:
		return ScopeAll
	case scope.Start == scope.End:
		return ScopeDay
	case scope.Start == scope.Start.MonthStart() && scope.End == scope.Start.MonthEnd():
		return ScopeMonth
	case scope.Start == scope.Start.YearStart() && scope.End == scope.Start.YearEnd():
		return ScopeYear
	default:
		return ScopeCustom
	}
}

// ResolveRanges derives the four ranges of a view from its scope, following the rules and
// worked examples in the web redesign spec.
func ResolveRanges(input RangeInput) (Ranges, error) {
	scope, err := copyScope(input.Scope)
	if err != nil {
		return Ranges{}, err
	}
	kind := ClassifyScope(input.Scope)
	ranges := Ranges{Kind: kind, Scope: scope}
	ranges.ContextGranularity, ranges.Context, err = contextPeriods(kind, input)
	if err != nil {
		return Ranges{}, fmt.Errorf("resolve ranges: chart context: %w", err)
	}
	if !input.TimeGrouping {
		if ranges.Trend, err = trendPeriods(input); err != nil {
			return Ranges{}, fmt.Errorf("resolve ranges: trend: %w", err)
		}
	}
	ranges.Comparison, ranges.ComparisonLabel, err = comparisonRange(kind, input)
	if err != nil {
		return Ranges{}, fmt.Errorf("resolve ranges: comparison: %w", err)
	}
	if ranges.Heatmap, err = heatmapRange(kind, input); err != nil {
		return Ranges{}, fmt.Errorf("resolve ranges: heatmap: %w", err)
	}
	return ranges, nil
}

func copyScope(scope *domain.DateRange) (*domain.DateRange, error) {
	if scope == nil {
		return nil, nil
	}
	if scope.Start.Compare(scope.End) > 0 {
		return nil, errors.New("resolve ranges: scope starts after it ends")
	}
	copied := *scope
	return &copied, nil
}

func contextPeriods(
	kind ScopeKind, input RangeInput,
) (domain.TimeGranularity, []PeriodRange, error) {
	if input.TimeGrouping {
		return timeRowPeriods(kind, input)
	}
	switch kind {
	case ScopeDay:
		start := input.Scope.Start
		periods, err := enumeratePeriods(
			domain.DateRange{Start: start.MonthStart(), End: start.MonthEnd()},
			domain.TimeGranularityDay, input.Today, false,
		)
		return domain.TimeGranularityDay, periods, err
	case ScopeMonth:
		first, err := input.Scope.Start.AddMonthsClamped(1 - monthContextMonths)
		if err != nil {
			return "", nil, err
		}
		periods, err := enumeratePeriods(
			domain.DateRange{Start: first, End: input.Scope.End},
			domain.TimeGranularityMonth, input.Today, false,
		)
		return domain.TimeGranularityMonth, periods, err
	case ScopeYear:
		periods, err := enumeratePeriods(
			throughToday(*input.Scope, input.Today), domain.TimeGranularityMonth, input.Today, false,
		)
		return domain.TimeGranularityMonth, periods, err
	case ScopeCustom:
		granularity := customGranularity(*input.Scope)
		periods, err := enumeratePeriods(*input.Scope, granularity, input.Today, true)
		return granularity, periods, err
	default:
		return allTimePeriods(input)
	}
}

// timeRowPeriods returns the Time grouping's rows, which are also its chart columns.
func timeRowPeriods(
	kind ScopeKind, input RangeInput,
) (domain.TimeGranularity, []PeriodRange, error) {
	switch kind {
	case ScopeDay, ScopeMonth:
		periods, err := enumeratePeriods(
			throughToday(*input.Scope, input.Today), domain.TimeGranularityDay, input.Today, false,
		)
		return domain.TimeGranularityDay, periods, err
	case ScopeYear:
		periods, err := enumeratePeriods(
			throughToday(*input.Scope, input.Today), domain.TimeGranularityMonth, input.Today, false,
		)
		return domain.TimeGranularityMonth, periods, err
	case ScopeCustom:
		granularity := customGranularity(*input.Scope)
		periods, err := enumeratePeriods(*input.Scope, granularity, input.Today, true)
		return granularity, periods, err
	default:
		return allTimePeriods(input)
	}
}

func allTimePeriods(input RangeInput) (domain.TimeGranularity, []PeriodRange, error) {
	if input.First.IsZero() || input.Latest.IsZero() {
		return domain.TimeGranularityMonth, nil, nil
	}
	bounds := domain.DateRange{Start: input.First, End: input.Latest}
	granularity := domain.TimeGranularityMonth
	if monthsSpanned(bounds) > allTimeMonthLimit {
		granularity = domain.TimeGranularityYear
	}
	periods, err := enumeratePeriods(bounds, granularity, input.Today, false)
	return granularity, periods, err
}

// throughToday ends a scope that contains today at today, so periods never extend past it.
func throughToday(scope domain.DateRange, today domain.Date) domain.DateRange {
	if contains(scope, today) {
		scope.End = today
	}
	return scope
}

func customGranularity(scope domain.DateRange) domain.TimeGranularity {
	if domain.DaysBetween(scope.Start, scope.End)+1 <= customDayLimit {
		return domain.TimeGranularityDay
	}
	if monthsSpanned(scope) <= customMonthLimit {
		return domain.TimeGranularityMonth
	}
	return domain.TimeGranularityYear
}

func monthsSpanned(bounds domain.DateRange) int {
	return (bounds.End.Year()-bounds.Start.Year())*12 +
		int(bounds.End.Month()) - int(bounds.Start.Month()) + 1
}

// enumeratePeriods lists every period that overlaps bounds, including periods without data.
// Clipped periods cover only the part of the period inside bounds and are marked partial.
func enumeratePeriods(
	bounds domain.DateRange, granularity domain.TimeGranularity, today domain.Date, clip bool,
) ([]PeriodRange, error) {
	periods, err := periodsBetween(bounds.Start, bounds.End, granularity)
	if err != nil {
		return nil, err
	}
	result := make([]PeriodRange, 0, len(periods))
	for _, period := range periods {
		start, end, err := periodBounds(period)
		if err != nil {
			return nil, err
		}
		value := PeriodRange{
			Period: period, Start: start, End: end,
			Partial: contains(domain.DateRange{Start: start, End: end}, today) && today != end,
		}
		if clip && (start.Compare(bounds.Start) < 0 || end.Compare(bounds.End) > 0) {
			value.Start, value.End = laterDate(start, bounds.Start), earlierDate(end, bounds.End)
			value.Partial = true
		}
		result = append(result, value)
	}
	return result, nil
}

func trendPeriods(input RangeInput) ([]PeriodRange, error) {
	last, ok := trendLastDay(input)
	if !ok {
		return nil, nil
	}
	first, err := last.MonthStart().AddMonthsClamped(1 - trendMonths)
	if err != nil {
		return nil, err
	}
	periods, err := enumeratePeriods(
		domain.DateRange{Start: first, End: last.MonthEnd()},
		domain.TimeGranularityMonth, input.Today, false,
	)
	if err != nil {
		return nil, err
	}
	for index := range periods {
		periods[index].Absent = input.First.IsZero() || periods[index].End.Compare(input.First) < 0
	}
	return periods, nil
}

// trendLastDay picks the day the trend ends on. It reports false for All with no data.
func trendLastDay(input RangeInput) (domain.Date, bool) {
	switch {
	case input.Scope == nil:
		return input.Latest, !input.Latest.IsZero()
	case contains(*input.Scope, input.Today) && input.Today != input.Scope.End:
		return input.Today, true
	default:
		return input.Scope.End, true
	}
}

func comparisonRange(kind ScopeKind, input RangeInput) (*domain.DateRange, string, error) {
	switch kind {
	case ScopeDay:
		previous, err := input.Scope.Start.AddDays(-1)
		if err != nil {
			return nil, "", err
		}
		label := "vs " + formatComparisonDate(previous, comparisonDayLayout)
		return &domain.DateRange{Start: previous, End: previous}, label, nil
	case ScopeMonth:
		return monthComparison(*input.Scope, input.Today)
	case ScopeYear:
		return yearComparison(*input.Scope, input.Today)
	case ScopeCustom:
		return customComparison(*input.Scope)
	default:
		return nil, "", nil
	}
}

func monthComparison(scope domain.DateRange, today domain.Date) (*domain.DateRange, string, error) {
	start, err := scope.Start.AddMonthsClamped(-1)
	if err != nil {
		return nil, "", err
	}
	month := start.Month().String()[:3]
	if !contains(scope, today) || today == scope.End {
		return &domain.DateRange{Start: start, End: start.MonthEnd()}, "vs " + month, nil
	}
	lastDay := min(today.Day(), start.MonthEnd().Day())
	end, err := start.AddDays(lastDay - 1)
	if err != nil {
		return nil, "", err
	}
	label := fmt.Sprintf("vs %s 1–%d", month, lastDay)
	if lastDay == 1 {
		label = fmt.Sprintf("vs %s 1", month)
	}
	return &domain.DateRange{Start: start, End: end}, label, nil
}

func yearComparison(scope domain.DateRange, today domain.Date) (*domain.DateRange, string, error) {
	start, err := scope.Start.AddMonthsClamped(-12)
	if err != nil {
		return nil, "", err
	}
	label := fmt.Sprintf("vs %04d", start.Year())
	if !contains(scope, today) || today == scope.End {
		return &domain.DateRange{Start: start, End: start.YearEnd()}, label, nil
	}
	end, err := today.AddMonthsClamped(-12)
	if err != nil {
		return nil, "", err
	}
	return &domain.DateRange{Start: start, End: end}, label + " to date", nil
}

func customComparison(scope domain.DateRange) (*domain.DateRange, string, error) {
	days := domain.DaysBetween(scope.Start, scope.End) + 1
	end, err := scope.Start.AddDays(-1)
	if err != nil {
		return nil, "", err
	}
	start, err := end.AddDays(1 - days)
	if err != nil {
		return nil, "", err
	}
	layout := comparisonDayLayout
	if start.Year() != end.Year() {
		layout = comparisonYearLayout
	}
	label := fmt.Sprintf(
		"vs %s – %s", formatComparisonDate(start, layout), formatComparisonDate(end, layout),
	)
	return &domain.DateRange{Start: start, End: end}, label, nil
}

func heatmapRange(kind ScopeKind, input RangeInput) (*Heatmap, error) {
	switch kind {
	case ScopeDay, ScopeMonth:
		start := input.Scope.Start
		return &Heatmap{Layout: HeatmapMonths, Start: start.MonthStart(), End: start.MonthEnd()}, nil
	case ScopeYear:
		return &Heatmap{Layout: HeatmapWeeks, Start: input.Scope.Start, End: input.Scope.End}, nil
	case ScopeCustom:
		if domain.DaysBetween(input.Scope.Start, input.Scope.End)+1 <= customDayLimit {
			return &Heatmap{
				Layout: HeatmapMonths,
				Start:  input.Scope.Start.MonthStart(), End: input.Scope.End.MonthEnd(),
			}, nil
		}
		return weekStrip(input.Scope.End)
	default:
		if input.Latest.IsZero() {
			return nil, nil
		}
		return weekStrip(input.Latest)
	}
}

func weekStrip(end domain.Date) (*Heatmap, error) {
	start, err := end.AddDays(1 - heatmapWeekDays)
	if err != nil {
		return nil, err
	}
	return &Heatmap{Layout: HeatmapWeeks, Start: start, End: end}, nil
}

func periodBounds(period domain.Period) (domain.Date, domain.Date, error) {
	month, day := time.Month(max(period.Month, 1)), max(period.Day, 1)
	start, err := domain.NewDate(period.Year, month, day)
	if err != nil {
		return domain.Date{}, domain.Date{}, err
	}
	switch period.Granularity {
	case domain.TimeGranularityYear:
		return start.YearStart(), start.YearEnd(), nil
	case domain.TimeGranularityMonth:
		return start, start.MonthEnd(), nil
	default:
		return start, start, nil
	}
}

func contains(bounds domain.DateRange, date domain.Date) bool {
	return bounds.Start.Compare(date) <= 0 && date.Compare(bounds.End) <= 0
}

func formatComparisonDate(date domain.Date, layout string) string {
	return time.Date(date.Year(), date.Month(), date.Day(), 0, 0, 0, 0, time.UTC).Format(layout)
}
