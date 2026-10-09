package analytics

import (
	"errors"
	"fmt"

	"github.com/wesm/moneyflow/internal/domain"
)

// MaxInsightsKeys bounds requested series rows to one loaded browser window.
const MaxInsightsKeys = CrossTabMaxKeys

const compositionTop = 7

const (
	frameContext = iota
	frameTrend
	frameComparison
	frameScope
	frameCalendar
)

// InsightsSpec asks for the chart data of one view.
type InsightsSpec struct {
	// Query is the view's query. Currency is required. The date range and any time
	// drill-down define the scope; every other filter applies to all ranges.
	Query domain.QuerySpec
	Today domain.Date
	// Keys are aggregate keys of the view's merchant, category, group, or account grouping.
	Keys    []string
	Summary bool
	// SearchMatches, when non-nil, lists the IDs that satisfy Query.Search in place of the
	// regular expression, for searches that also match Amazon product names.
	SearchMatches map[string]struct{}
}

// InsightsTotals are exact totals for one range. Hidden transactions count but add no money.
type InsightsTotals struct {
	In           int64
	Out          int64
	Net          int64
	Count        int
	HiddenCount  int
	PendingCount int
}

// InsightsKPIs feed the KPI strip.
type InsightsKPIs struct {
	Scope      InsightsTotals
	Comparison *InsightsTotals
	// IncomeChange, SpendingChange, and NetChange are nil when the scope has no comparison.
	IncomeChange   *Change
	SpendingChange *Change
	NetChange      *Change
	// SavingsRateTenths is Net ÷ In in tenths of a percent, at top level without search and
	// only when In is positive.
	SavingsRateTenths *int
	// AveragePerTransaction is Net ÷ non-hidden count inside a drill, rounded half away
	// from zero to the minor unit.
	AveragePerTransaction *int64
}

// InsightsPeriod is one cash-flow column.
type InsightsPeriod struct {
	Period  domain.Period
	Label   string
	Start   domain.Date
	End     domain.Date
	Partial bool
	In      int64
	Out     int64
	Net     int64
	Count   int
}

// CompositionEntry is one ranked row, or the entry that folds the remaining rows together.
type CompositionEntry struct {
	Key         string
	Label       string
	Net         int64
	ShareTenths int
	// Others counts the rows folded into this entry. It is zero for a ranked row.
	Others int
}

// Composition ranks rows by net share: Spending lists negative Net and Income positive Net.
type Composition struct {
	Spending []CompositionEntry
	Income   []CompositionEntry
}

// PeriodComposition is the category-group composition of one chart-context period.
type PeriodComposition struct {
	Period      domain.Period
	Composition Composition
}

// CalendarDay is one heatmap cell.
type CalendarDay struct {
	Date domain.Date
	In   int64
	Out  int64
}

// InsightsSummary holds the chart row's data. Each part is present only where it applies.
type InsightsSummary struct {
	KPIs              InsightsKPIs
	Cashflow          []InsightsPeriod
	Composition       *Composition
	PeriodComposition []PeriodComposition
	Calendar          []CalendarDay
}

// TrendPoint is one month of a row's trend sparkline.
type TrendPoint struct {
	Net     int64
	Absent  bool
	Partial bool
}

// InsightsSeries is one table row's chart data.
type InsightsSeries struct {
	Key     string
	Trend   []TrendPoint
	Context []CrossTabCell
	// PriorNet and VsPrior are nil when the scope has no comparison.
	PriorNet *int64
	VsPrior  *Change
}

// Insights is the chart data for one view in one currency.
type Insights struct {
	Currency domain.Currency
	Scale    uint8
	Today    domain.Date
	// Latest is the newest transaction in the currency, or zero when there is none.
	Latest  domain.Date
	Ranges  Ranges
	Summary *InsightsSummary
	Series  []InsightsSeries
}

type insightsPlan struct {
	base     domain.QuerySpec
	grouping domain.Dimension
	ranges   Ranges
	first    domain.Date
	latest   domain.Date
	// calendar lists the heatmap days when the summary has a calendar.
	calendar []PeriodRange
}

// ComputeInsights derives chart data in one pass over the view's transactions. Callers pass
// committed transactions; pending edits never change insights.
func ComputeInsights(transactions []domain.Transaction, spec InsightsSpec) (Insights, error) {
	plan, err := planInsights(transactions, spec)
	if err != nil {
		return Insights{}, err
	}
	filtered, err := filterInsights(transactions, plan.base, spec.SearchMatches)
	if err != nil {
		return Insights{}, fmt.Errorf("insights: %w", err)
	}
	crossTab, err := ComputeCrossTab(filtered, plan.crossTabSpec(spec))
	if err != nil {
		return Insights{}, fmt.Errorf("insights: %w", err)
	}
	result := Insights{
		Currency: spec.Query.Currency.Currency, Scale: spec.Query.Currency.Scale,
		Today: spec.Today, Latest: plan.latest, Ranges: plan.ranges,
	}
	if result.Series, err = buildSeries(spec.Keys, plan.ranges, crossTab); err != nil {
		return Insights{}, fmt.Errorf("insights: %w", err)
	}
	if spec.Summary {
		if result.Summary, err = buildSummary(filtered, spec, plan, crossTab); err != nil {
			return Insights{}, fmt.Errorf("insights: %w", err)
		}
	}
	return result, nil
}

// filterInsights applies the view's filters. Search matches, when given, replace the search
// expression; transactions outside them are dropped.
func filterInsights(
	transactions []domain.Transaction, base domain.QuerySpec, matches map[string]struct{},
) ([]domain.Transaction, error) {
	if matches == nil {
		return filterTransactions(transactions, base, false)
	}
	base.Search = ""
	filtered, err := filterTransactions(transactions, base, false)
	if err != nil {
		return nil, err
	}
	kept := filtered[:0]
	for _, transaction := range filtered {
		if _, ok := matches[transaction.ID]; ok {
			kept = append(kept, transaction)
		}
	}
	return kept, nil
}

func planInsights(transactions []domain.Transaction, spec InsightsSpec) (insightsPlan, error) {
	if err := spec.Query.Validate(); err != nil {
		return insightsPlan{}, fmt.Errorf("insights: %w", err)
	}
	if spec.Query.Currency == nil {
		return insightsPlan{}, errors.New("insights: a currency is required")
	}
	if spec.Today.IsZero() {
		return insightsPlan{}, errors.New("insights: today is required")
	}
	plan := insightsPlan{}
	if spec.Query.Mode == domain.ResultModeAggregate {
		plan.grouping = spec.Query.GroupBy
	}
	if err := validateInsightsKeys(spec.Keys, plan.grouping); err != nil {
		return insightsPlan{}, err
	}
	base, scope, err := effectiveScope(spec.Query)
	if err != nil {
		return insightsPlan{}, err
	}
	plan.base = base
	plan.first, plan.latest = currencyBounds(transactions, *spec.Query.Currency)
	if err := plan.resolveRanges(scope, spec); err != nil {
		return insightsPlan{}, fmt.Errorf("insights: %w", err)
	}
	return plan, nil
}

// resolveRanges derives the view's ranges and, for a summary, its calendar days.
func (plan *insightsPlan) resolveRanges(scope *domain.DateRange, spec InsightsSpec) error {
	var err error
	plan.ranges, err = ResolveRanges(RangeInput{
		Scope: scope, Today: spec.Today, First: plan.first, Latest: plan.latest,
		TimeGrouping: plan.grouping == domain.DimensionTime,
	})
	if err != nil || !spec.Summary {
		return err
	}
	plan.calendar, err = plan.calendarDays()
	return err
}

func validateInsightsKeys(keys []string, grouping domain.Dimension) error {
	if len(keys) > MaxInsightsKeys {
		return fmt.Errorf("insights: at most %d series rows", MaxInsightsKeys)
	}
	if len(keys) > 0 && (grouping == "" || grouping == domain.DimensionTime) {
		return errors.New("insights: series need a merchant, category, group, or account grouping")
	}
	return nil
}

// effectiveScope removes date bounds from the filter query and folds any time drill-down into
// the scope, so a drill into September of 2026 scopes the charts to that month. A drill-down
// outside the date range leaves an empty view, as in the table: the scope stays the date range
// and the drill-down keeps filtering every range.
func effectiveScope(query domain.QuerySpec) (domain.QuerySpec, *domain.DateRange, error) {
	base := query.Clone()
	base.DateRange = nil
	scope := query.Clone().DateRange
	kept := make([]domain.Drilldown, 0, len(base.Drilldowns))
	for _, drilldown := range base.Drilldowns {
		if drilldown.Dimension != domain.DimensionTime {
			kept = append(kept, drilldown)
			continue
		}
		start, end, err := periodBounds(*drilldown.Period)
		if err != nil {
			return domain.QuerySpec{}, nil, fmt.Errorf("insights: time drill-down: %w", err)
		}
		narrowed, overlaps := intersectScope(scope, domain.DateRange{Start: start, End: end})
		if !overlaps {
			return base, query.Clone().DateRange, nil
		}
		scope = narrowed
	}
	base.Drilldowns = kept
	return base, scope, nil
}

// intersectScope narrows a scope to a period; a nil scope becomes the period.
func intersectScope(scope *domain.DateRange, period domain.DateRange) (*domain.DateRange, bool) {
	if scope == nil {
		return &period, true
	}
	narrowed := domain.DateRange{
		Start: laterDate(scope.Start, period.Start), End: earlierDate(scope.End, period.End),
	}
	return &narrowed, narrowed.Start.Compare(narrowed.End) <= 0
}

func currencyBounds(
	transactions []domain.Transaction, scope domain.CurrencyScope,
) (domain.Date, domain.Date) {
	var first, latest domain.Date
	for _, transaction := range transactions {
		if !matchesCurrency(transaction, &scope) {
			continue
		}
		if first.IsZero() || transaction.Date.Compare(first) < 0 {
			first = transaction.Date
		}
		if latest.IsZero() || transaction.Date.Compare(latest) > 0 {
			latest = transaction.Date
		}
	}
	return first, latest
}

func (plan insightsPlan) crossTabSpec(spec InsightsSpec) CrossTabSpec {
	series := len(spec.Keys) > 0
	frames := make([]CrossTabFrameSpec, frameCalendar+1)
	frames[frameContext] = CrossTabFrameSpec{Periods: plan.ranges.Context, Series: series}
	frames[frameTrend] = CrossTabFrameSpec{Periods: plan.ranges.Trend, Series: series}
	if comparison := plan.ranges.Comparison; comparison != nil {
		frames[frameComparison] = CrossTabFrameSpec{
			Periods: []PeriodRange{{Start: comparison.Start, End: comparison.End}}, Series: series,
		}
	}
	if scope, ok := plan.scopeBounds(); ok {
		frames[frameScope] = CrossTabFrameSpec{
			Periods: []PeriodRange{{Start: scope.Start, End: scope.End}}, Series: series,
		}
	}
	if spec.Summary {
		frames[frameCalendar] = CrossTabFrameSpec{Periods: plan.calendar}
	}
	dimension, keys := plan.grouping, spec.Keys
	if !series {
		dimension, keys = domain.DimensionCategory, []string{}
	}
	return CrossTabSpec{Dimension: dimension, Keys: keys, Frames: frames}
}

// scopeBounds returns the scope's dates; All covers the currency's first to latest day.
func (plan insightsPlan) scopeBounds() (domain.DateRange, bool) {
	if plan.ranges.Scope != nil {
		return *plan.ranges.Scope, true
	}
	if plan.first.IsZero() {
		return domain.DateRange{}, false
	}
	return domain.DateRange{Start: plan.first, End: plan.latest}, true
}

// calendarDays lists heatmap days for transaction views and Day scopes.
func (plan insightsPlan) calendarDays() ([]PeriodRange, error) {
	heatmap := plan.ranges.Heatmap
	if heatmap == nil || (plan.grouping != "" && plan.ranges.Kind != ScopeDay) {
		return nil, nil
	}
	days, err := enumeratePeriods(
		domain.DateRange{Start: heatmap.Start, End: heatmap.End},
		domain.TimeGranularityDay, domain.Date{}, false,
	)
	if err != nil {
		return nil, fmt.Errorf("calendar days: %w", err)
	}
	return days, nil
}

func buildSeries(keys []string, ranges Ranges, crossTab CrossTab) ([]InsightsSeries, error) {
	if len(keys) == 0 {
		return nil, nil
	}
	result := make([]InsightsSeries, 0, len(keys))
	for _, key := range keys {
		series := InsightsSeries{
			Key:     key,
			Trend:   make([]TrendPoint, len(ranges.Trend)),
			Context: append([]CrossTabCell(nil), crossTab.Frames[frameContext].Series[key]...),
		}
		trend := crossTab.Frames[frameTrend].Series[key]
		for index, period := range ranges.Trend {
			series.Trend[index] = TrendPoint{
				Net: trend[index].Net(), Absent: period.Absent, Partial: period.Partial,
			}
		}
		if ranges.Comparison != nil {
			prior := crossTab.Frames[frameComparison].Series[key][0].Net()
			var current int64
			if cells := crossTab.Frames[frameScope].Series[key]; len(cells) > 0 {
				current = cells[0].Net()
			}
			change, err := NetChange(current, prior)
			if err != nil {
				return nil, fmt.Errorf("series %q vs prior: %w", key, err)
			}
			series.PriorNet, series.VsPrior = &prior, &change
		}
		result = append(result, series)
	}
	return result, nil
}

func buildSummary(
	filtered []domain.Transaction, spec InsightsSpec, plan insightsPlan, crossTab CrossTab,
) (*InsightsSummary, error) {
	kpis, err := buildKPIs(spec.Query, plan.ranges, crossTab)
	if err != nil {
		return nil, err
	}
	summary := &InsightsSummary{
		KPIs:     kpis,
		Cashflow: cashflow(plan.ranges.Context, crossTab.Frames[frameContext].Totals),
		Calendar: calendar(plan.calendar, crossTab.Frames[frameCalendar].Totals),
	}
	switch plan.grouping {
	case "":
		return summary, nil
	case domain.DimensionTime:
		summary.PeriodComposition, err = periodCompositions(filtered, plan.ranges.Context)
	default:
		var composition Composition
		composition, err = scopeComposition(filtered, plan)
		summary.Composition = &composition
	}
	if err != nil {
		return nil, err
	}
	return summary, nil
}
